package tools

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	internalmcp "github.com/Tencent/WeKnora/internal/mcp"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	sdkmcp "github.com/mark3labs/mcp-go/mcp"
	sdkserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestScriptedMCPReadOnlyToolIsCallable(t *testing.T) {
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)

	var executions atomic.Int32
	server := sdkserver.NewMCPServer("erp", "1", sdkserver.WithToolCapabilities(false))
	server.AddTool(
		sdkmcp.NewTool("list_documents",
			sdkmcp.WithString("doctype", sdkmcp.Required()),
			sdkmcp.WithReadOnlyHintAnnotation(true),
		),
		func(context.Context, sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			executions.Add(1)
			return sdkmcp.NewToolResultText(`{"documents":[]}`), nil
		},
	)
	transport := httptest.NewServer(sdkserver.NewStreamableHTTPServer(server, sdkserver.WithStateLess(true)))
	defer transport.Close()

	manager := internalmcp.NewMCPManager(nil)
	defer manager.Shutdown()
	service := &types.MCPService{
		ID:            "erp-service",
		Name:          "erpnext",
		Enabled:       true,
		URL:           &transport.URL,
		TransportType: types.MCPTransportHTTPStreamable,
	}
	ctx := catalogTestContext()
	registry := NewToolRegistry()
	gate := staticPolicyGate{enabled: true}
	_, err := RegisterMCPTools(ctx, registry, []*types.MCPService{service}, manager, gate, 0, nil, nil)
	require.NoError(t, err)
	registry.PrepareMCPToolsDirect(ctx)
	name := MCPToolNamesByServiceID(registry)[service.ID][0]
	require.NotEmpty(t, name)

	ctx = WithToolExecContext(ctx, &ToolExecContext{
		EventBus:    event.NewEventBus(),
		ToolCallID:  "js-nested",
		SessionID:   "session",
		Mode:        types.RunModeUnattended,
		Scripted:    true,
		ApprovalCtx: ctx,
	})
	result, err := registry.ExecuteTool(ctx, name, json.RawMessage(`{"doctype":"Sales Invoice"}`))
	require.NoError(t, err)
	require.True(t, result.Success, result.Error)
	require.Equal(t, int32(1), executions.Load())
}
