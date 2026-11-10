// Package univer integrates the Univer collaboration server with WeKnora.
//
// The collaboration server itself is a separate Node.js process built from the
// Univer Server SDK (see server/univer-collab). This package owns the WeKnora
// side: the knowledge-base entries that point at collaboration units, the HTTP
// client for the server's internal API, the process supervisor used in local
// deployments, and the identity cookie that lets the browser talk to the
// collaboration endpoints.
package univer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the collaboration server's internal API. That API is only
// reachable with the shared secret and acts on behalf of the user named in the
// request, so ACL checks still run inside the collaboration server.
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewClient builds a client for one collaboration server.
func NewClient(baseURL, secret string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// CreateUnitRequest describes one collaboration unit to create.
type CreateUnitRequest struct {
	UnitID   string `json:"unit_id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Sheet    string `json:"sheet_name,omitempty"`
	RowCount int    `json:"row_count,omitempty"`
	ColCount int    `json:"column_count,omitempty"`
}

func (c *Client) do(ctx context.Context, method, path, userID string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-WeKnora-User", userID)
	if c.secret != "" {
		req.Header.Set("X-WeKnora-Secret", c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("collaboration server %s %s: %s: %s", method, path, resp.Status, truncate(string(payload), 300))
	}
	if out != nil {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// CreateUnit creates a blank unit on the collaboration server.
func (c *Client) CreateUnit(ctx context.Context, userID string, req CreateUnitRequest) error {
	return c.do(ctx, http.MethodPost, "/internal/units", userID, req, nil)
}

// UnitMeta describes the structure behind one unit.
type UnitMeta struct {
	UnitID       string           `json:"unit_id"`
	Kind         string           `json:"kind"`
	HeadRevision int              `json:"head_revision"`
	Sheets       []map[string]any `json:"sheets,omitempty"`
	Tables       []map[string]any `json:"tables,omitempty"`
	Slides       []map[string]any `json:"slides,omitempty"`
	Title        string           `json:"title,omitempty"`
}

// Meta reads the structure of one unit.
func (c *Client) Meta(ctx context.Context, userID, unitID string) (*UnitMeta, error) {
	var out UnitMeta
	path := fmt.Sprintf("/internal/units/%s/meta", url.PathEscape(unitID))
	if err := c.do(ctx, http.MethodGet, path, userID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type contentResponse struct {
	UnitID       string `json:"unit_id"`
	Kind         string `json:"kind"`
	HeadRevision int    `json:"head_revision"`
	Text         string `json:"text"`
}

// ReadContent returns a kind-specific textual rendering of one unit.
func (c *Client) ReadContent(ctx context.Context, userID, unitID, cellRange string) (string, error) {
	path := fmt.Sprintf("/internal/units/%s/content", url.PathEscape(unitID))
	if strings.TrimSpace(cellRange) != "" {
		path += "?range=" + url.QueryEscape(cellRange)
	}
	var out contentResponse
	if err := c.do(ctx, http.MethodGet, path, userID, nil, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

// WriteValues replaces the cells of one A1 range in a spreadsheet unit.
func (c *Client) WriteValues(ctx context.Context, userID, unitID, cellRange string, values [][]any) (int, error) {
	path := fmt.Sprintf("/internal/units/%s/content", url.PathEscape(unitID))
	var out struct {
		HeadRevision int `json:"head_revision"`
	}
	err := c.do(ctx, http.MethodPut, path, userID, map[string]any{
		"range":  cellRange,
		"values": values,
	}, &out)
	return out.HeadRevision, err
}

// AppendText appends text to a document unit.
func (c *Client) AppendText(ctx context.Context, userID, unitID, text string) (int, error) {
	path := fmt.Sprintf("/internal/units/%s/content", url.PathEscape(unitID))
	var out struct {
		HeadRevision int `json:"head_revision"`
	}
	err := c.do(ctx, http.MethodPost, path, userID, map[string]any{"text": text}, &out)
	return out.HeadRevision, err
}
