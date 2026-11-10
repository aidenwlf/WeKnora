package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	// ToolUniverListUnits lists entries of collaborative knowledge bases.
	ToolUniverListUnits = "univer_list_units"
	// ToolUniverReadUnit reads one entry as text.
	ToolUniverReadUnit = "univer_read_unit"
	// ToolUniverUpdateUnit edits one entry.
	ToolUniverUpdateUnit = "univer_update_unit"
)

// defaultUnitRange bounds spreadsheet reads so a model can never ask for an
// unbounded dump.
const defaultUnitRange = "A1:Z200"

// sheetCellRefPattern matches one A1 cell reference: column letters then row
// digits. A bare "sheet1" must not pass as a range.
var sheetCellRefPattern = regexp.MustCompile(`^[A-Za-z]{1,3}[0-9]+$`)

var univerListUnitsTool = BaseTool{
	name: ToolUniverListUnits,
	description: "List entries of the workspace's collaborative knowledge bases (spreadsheets, documents, " +
		"multi-dimensional tables and slide decks). Every entry carries the handle the other collaborative " +
		"tools accept.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {}
}`),
}

var univerReadUnitTool = BaseTool{
	name: ToolUniverReadUnit,
	description: "Read one collaborative entry as text. Spreadsheets return a CSV matrix for the requested " +
		"range; documents return their text; multi-dimensional tables and slide decks return their structure " +
		"as JSON. Always pass a bounded range for spreadsheets.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "unit": {
      "type": "string",
      "description": "Entry handle, collaboration unit id, or name from univer_list_units",
      "minLength": 1
    },
    "range": {
      "type": "string",
      "description": "A1 range for spreadsheets, for example A1:F30. Defaults to A1:Z200"
    }
  },
  "required": ["unit"]
}`),
}

var univerUpdateUnitTool = BaseTool{
	name: ToolUniverUpdateUnit,
	description: "Edit one collaborative entry. Spreadsheets: replace the cells of `range` with `values`, a " +
		"row-major matrix. Documents: `values` rows are appended as lines of text. Multi-dimensional tables " +
		"and slide decks must be edited in the app. Changes are committed as collaborative edits, so everyone " +
		"viewing the entry sees them immediately.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "unit": {
      "type": "string",
      "description": "Entry handle, collaboration unit id, or name from univer_list_units",
      "minLength": 1
    },
    "range": {
      "type": "string",
      "description": "A1 range to replace (spreadsheets only), for example A2:C2"
    },
    "values": {
      "type": "array",
      "description": "Row-major matrix. Each inner array is one row.",
      "items": {"type": "array", "items": {}},
      "minItems": 1
    }
  },
  "required": ["unit", "values"]
}`),
}

// UniverListUnitsTool lists collaborative entries in scope.
type UniverListUnitsTool struct {
	BaseTool
	service interfaces.UniverService
	kbIDs   []string
}

// NewUniverListUnitsTool builds the list tool for the given knowledge bases.
func NewUniverListUnitsTool(service interfaces.UniverService, kbIDs []string) *UniverListUnitsTool {
	return &UniverListUnitsTool{BaseTool: univerListUnitsTool, service: service, kbIDs: kbIDs}
}

// Execute lists entries.
func (t *UniverListUnitsTool) Execute(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return &types.ToolResult{Success: false, Error: "workspace context is missing"}, fmt.Errorf("missing tenant")
	}
	var b strings.Builder
	total := 0
	b.WriteString("<units>\n")
	for _, kbID := range t.kbIDs {
		units, err := t.service.ListUnits(ctx, tenantID, kbID)
		if err != nil {
			return &types.ToolResult{Success: false, Error: err.Error()}, err
		}
		for _, unit := range units {
			total++
			fmt.Fprintf(&b, "  <unit id=%q name=%q kind=%q unit_id=%q knowledge_base_id=%q />\n",
				unit.ID, unit.Name, unit.Kind, unit.UnitID, unit.KnowledgeBaseID)
		}
	}
	b.WriteString("</units>")
	if total == 0 {
		return &types.ToolResult{Success: true, Output: "No collaborative entries in scope."}, nil
	}
	return &types.ToolResult{Success: true, Output: b.String()}, nil
}

// UniverReadUnitTool reads one entry.
type UniverReadUnitTool struct {
	BaseTool
	service interfaces.UniverService
	kbIDs   []string
}

// NewUniverReadUnitTool builds the read tool for the given knowledge bases.
func NewUniverReadUnitTool(service interfaces.UniverService, kbIDs []string) *UniverReadUnitTool {
	return &UniverReadUnitTool{BaseTool: univerReadUnitTool, service: service, kbIDs: kbIDs}
}

// Execute reads one entry.
func (t *UniverReadUnitTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input struct {
		Unit  string `json:"unit"`
		Range string `json:"range"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}, err
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return &types.ToolResult{Success: false, Error: "workspace context is missing"}, fmt.Errorf("missing tenant")
	}
	unit, err := t.service.ResolveUnit(ctx, tenantID, t.kbIDs, input.Unit)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}
	cellRange := ""
	if unit.Kind == types.UniverKindSheet {
		cellRange = normalizeSheetRange(input.Range)
	}
	text, err := t.service.ReadContent(ctx, actorFromContext(ctx), unit, cellRange)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<unit name=%q kind=%q unit_id=%q", unit.Name, unit.Kind, unit.UnitID)
	if cellRange != "" {
		fmt.Fprintf(&b, " range=%q", cellRange)
	}
	b.WriteString(">\n")
	b.WriteString(text)
	b.WriteString("\n</unit>")
	return &types.ToolResult{Success: true, Output: b.String()}, nil
}

// UniverUpdateUnitTool edits one entry.
type UniverUpdateUnitTool struct {
	BaseTool
	service interfaces.UniverService
	kbIDs   []string
}

// NewUniverUpdateUnitTool builds the update tool for the given knowledge bases.
func NewUniverUpdateUnitTool(service interfaces.UniverService, kbIDs []string) *UniverUpdateUnitTool {
	return &UniverUpdateUnitTool{BaseTool: univerUpdateUnitTool, service: service, kbIDs: kbIDs}
}

// Execute writes one entry.
func (t *UniverUpdateUnitTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input struct {
		Unit   string  `json:"unit"`
		Range  string  `json:"range"`
		Values [][]any `json:"values"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}, err
	}
	if len(input.Values) == 0 {
		return &types.ToolResult{Success: false, Error: "values must contain at least one row"},
			fmt.Errorf("empty values")
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return &types.ToolResult{Success: false, Error: "workspace context is missing"}, fmt.Errorf("missing tenant")
	}
	unit, err := t.service.ResolveUnit(ctx, tenantID, t.kbIDs, input.Unit)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}
	cellRange := ""
	if unit.Kind == types.UniverKindSheet {
		cellRange, err = normalizeSheetRangeStrict(input.Range)
		if err != nil {
			return &types.ToolResult{Success: false, Error: err.Error()}, err
		}
	}
	if err := t.service.WriteContent(
		ctx, actorFromContext(ctx), unit, cellRange, input.Values,
	); err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}
	return &types.ToolResult{
		Success: true,
		Output: fmt.Sprintf("Updated %s (%s); collaborators see the change immediately.",
			unit.Name, unit.Kind),
	}, nil
}

// actorFromContext names the user a change is attributed to.
func actorFromContext(ctx context.Context) string {
	userID, _ := types.UserIDFromContext(ctx)
	return userID
}

// normalizeSheetRange validates an A1 range, falling back to a bounded default.
func normalizeSheetRange(raw string) string {
	if r, err := normalizeSheetRangeStrict(raw); err == nil && r != "" {
		return r
	}
	return defaultUnitRange
}

func normalizeSheetRangeStrict(raw string) (string, error) {
	// A1 notation may carry absolute-reference markers; Univer's range parser
	// does not, so they are dropped here.
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), "$", "")
	if trimmed == "" {
		return "", fmt.Errorf("range is required")
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) > 2 {
		return "", fmt.Errorf("invalid cell range: %s", raw)
	}
	for _, part := range parts {
		if !sheetCellRefPattern.MatchString(part) {
			return "", fmt.Errorf("invalid cell range: %s", raw)
		}
	}
	return strings.ToUpper(trimmed), nil
}
