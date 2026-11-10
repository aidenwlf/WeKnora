package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// univerProxyPrefix is stripped before forwarding: the collaboration client is
// configured with this prefix, and the server itself serves /universer-api.
const univerProxyPrefix = "/api/v1/univer"

// UniverProxy forwards the browser's collaboration traffic (HTTP and the
// realtime WebSocket) to the collaboration server, so the browser keeps using
// the WeKnora origin and its path-scoped identity cookie.
type UniverProxy struct {
	service interfaces.UniverService
	target  *url.URL
	proxy   *httputil.ReverseProxy
	dialer  *websocket.Dialer
}

// NewUniverProxy builds the proxy for one collaboration server origin.
func NewUniverProxy(service interfaces.UniverService, internalURL string) (*UniverProxy, error) {
	target, err := url.Parse(strings.TrimRight(internalURL, "/"))
	if err != nil {
		return nil, err
	}
	p := &UniverProxy{
		service: service,
		target:  target,
		dialer:  websocket.DefaultDialer,
	}
	p.proxy = &httputil.ReverseProxy{
		Director:  p.direct,
		Transport: &http.Transport{DisableCompression: true},
	}
	return p, nil
}

func (p *UniverProxy) direct(req *http.Request) {
	req.URL.Scheme = p.target.Scheme
	req.URL.Host = p.target.Host
	req.Host = p.target.Host
	req.URL.Path = strings.TrimPrefix(req.URL.Path, univerProxyPrefix)
	req.URL.RawPath = ""
}

// authorize resolves the caller from the collaboration cookie.
func (p *UniverProxy) authorize(c *gin.Context) (string, bool) {
	if p == nil || p.service == nil {
		return "", false
	}
	raw, err := c.Cookie(p.service.CookieName())
	if err != nil || raw == "" {
		return "", false
	}
	return p.service.SessionUser(raw)
}

// Handle serves both the HTTP API calls and the WebSocket upgrade on the same
// route.
func (p *UniverProxy) Handle(c *gin.Context) {
	userID, ok := p.authorize(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		p.serveWebSocket(c, userID)
		return
	}
	for key, value := range p.service.ForwardHeaders(userID) {
		c.Request.Header.Set(key, value)
	}
	// The identity travels as a header; the browser cookie stops here.
	c.Request.Header.Del("Cookie")
	p.proxy.ServeHTTP(c.Writer, c.Request)
}

func (p *UniverProxy) serveWebSocket(c *gin.Context, userID string) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  32 << 10,
		WriteBufferSize: 32 << 10,
		CheckOrigin:     func(*http.Request) bool { return true },
	}
	client, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer client.Close()

	scheme := "ws"
	if p.target.Scheme == "https" {
		scheme = "wss"
	}
	path := strings.TrimPrefix(c.Request.URL.Path, univerProxyPrefix)
	targetURL := url.URL{
		Scheme:   scheme,
		Host:     p.target.Host,
		Path:     path,
		RawQuery: c.Request.URL.RawQuery,
	}
	headers := http.Header{}
	for key, value := range p.service.ForwardHeaders(userID) {
		headers.Set(key, value)
	}
	upstream, resp, err := p.dialer.Dial(targetURL.String(), headers)
	if err != nil {
		code := websocket.CloseInternalServerErr
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			code = websocket.ClosePolicyViolation
		}
		_ = client.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(code, "collaboration server unavailable"),
			time.Now().Add(time.Second))
		return
	}
	defer upstream.Close()

	done := make(chan struct{}, 2)
	pump := func(dst, src *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			messageType, data, err := src.ReadMessage()
			if err != nil {
				return
			}
			if err := dst.WriteMessage(messageType, data); err != nil {
				return
			}
		}
	}
	go pump(upstream, client)
	go pump(client, upstream)
	<-done
}
