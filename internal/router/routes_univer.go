package router

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterUniverRoutes wires collaborative knowledge-base entries.
//
// Entries live inside a knowledge base of type "univer", so every route hangs
// off the knowledge base and inherits its permissions.
func RegisterUniverRoutes(r *gin.RouterGroup, h *handler.UniverHandler, g *rbacGuards) {
	units := r.Group("/knowledge-bases/:id/univer")
	{
		units.GET("/units", g.Viewer(), g.KBAccessRead("id"), h.ListUnits)
		units.POST("/units", g.Contributor(), g.KBAccessWrite("id"), h.CreateUnit)
		units.GET("/units/:unit_id", g.Viewer(), g.KBAccessRead("id"), h.GetUnit)
		units.DELETE("/units/:unit_id", g.Contributor(), g.KBAccessWrite("id"), h.DeleteUnit)
		// Mints the collaboration identity cookie and returns the unit to open.
		units.POST("/units/:unit_id/session", g.Viewer(), g.KBAccessRead("id"), h.UnitSession)
	}
	r.GET("/univer/status", g.Viewer(), h.Status)
}

// RegisterUniverProxyRoutes mounts the collaboration client's HTTP and
// WebSocket traffic. It must be registered before the global auth middleware:
// the browser authenticates with the path-scoped collaboration cookie, which
// the proxy verifies and turns into a header for the collaboration server.
func RegisterUniverProxyRoutes(r *gin.Engine, proxy *handler.UniverProxy) {
	if proxy == nil {
		return
	}
	r.Any("/api/v1/univer/universer-api/*path", proxy.Handle)
}

// RegisterUniverInternalRoutes wires the collaboration server's ACL bridge.
//
// It must be registered before the global auth middleware: the caller is the
// collaboration server, which authenticates with the shared secret rather than
// a user session. Mounting it under /api/internal keeps it out of the public
// API tree and out of the API-key policy table.
func RegisterUniverInternalRoutes(r *gin.Engine, h *handler.UniverHandler) {
	secret := strings.TrimSpace(os.Getenv("UNIVER_SHARED_SECRET"))
	group := r.Group("/api/internal/univer")
	group.Use(func(c *gin.Context) {
		if secret == "" {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		provided := c.GetHeader("X-WeKnora-Secret")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	})
	group.POST("/acl", h.InternalACL)
}
