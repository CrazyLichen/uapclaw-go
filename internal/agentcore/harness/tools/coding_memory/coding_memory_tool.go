package coding_memory

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	htools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryReadTool 编程记忆读取工具。对齐 Python CodingMemoryReadTool
type CodingMemoryReadTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 编程记忆工具上下文
	ctx *lite.CodingMemoryToolContext
}

// CodingMemoryWriteTool 编程记忆写入工具。对齐 Python CodingMemoryWriteTool
type CodingMemoryWriteTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 编程记忆工具上下文
	ctx *lite.CodingMemoryToolContext
}

// CodingMemoryEditTool 编程记忆编辑工具。对齐 Python CodingMemoryEditTool
type CodingMemoryEditTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 编程记忆工具上下文
	ctx *lite.CodingMemoryToolContext
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Card 返回工具配置卡片
func (t *CodingMemoryReadTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行编程记忆读取。对齐 Python CodingMemoryReadTool.invoke
func (t *CodingMemoryReadTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 提取 path（required）
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// 提取 offset（optional）
	var offset *int
	if v, ok := inputs["offset"]; ok {
		switch val := v.(type) {
		case int:
			offset = &val
		case float64:
			iv := int(val)
			offset = &iv
		}
	}

	// 提取 limit（optional）
	var limit *int
	if v, ok := inputs["limit"]; ok {
		switch val := v.(type) {
		case int:
			limit = &val
		case float64:
			iv := int(val)
			limit = &iv
		}
	}

	result := lite.CodingMemoryReadWithContext(ctx, t.ctx, path, offset, limit)
	return codingReadResultToMap(result), nil
}

// Stream CodingMemoryReadTool 不支持流式调用
func (t *CodingMemoryReadTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// Card 返回工具配置卡片
func (t *CodingMemoryWriteTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行编程记忆写入。对齐 Python CodingMemoryWriteTool.invoke
func (t *CodingMemoryWriteTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 提取 path（required）
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// 提取 content（required）
	content, _ := inputs["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	result := lite.CodingMemoryWriteWithContext(ctx, t.ctx, path, content)
	return result, nil
}

// Stream CodingMemoryWriteTool 不支持流式调用
func (t *CodingMemoryWriteTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// Card 返回工具配置卡片
func (t *CodingMemoryEditTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行编程记忆编辑。对齐 Python CodingMemoryEditTool.invoke
func (t *CodingMemoryEditTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 提取 path（required）
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// 提取 old_text（required）
	oldText, _ := inputs["old_text"].(string)
	if oldText == "" {
		return nil, fmt.Errorf("old_text is required")
	}

	// 提取 new_text（required）
	newText, _ := inputs["new_text"].(string)
	if newText == "" {
		return nil, fmt.Errorf("new_text is required")
	}

	result := lite.CodingMemoryEditWithContext(ctx, t.ctx, path, oldText, newText)
	return codingEditResultToMap(result), nil
}

// Stream CodingMemoryEditTool 不支持流式调用
func (t *CodingMemoryEditTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// CreateCodingMemoryTools 创建编程记忆工具集。对齐 Python create_coding_memory_tools
func CreateCodingMemoryTools(ctx *lite.CodingMemoryToolContext, language string, agentID string) []tool.Tool {
	// 对齐 Python: if ctx.workspace is not None: coding_memory_dir = str(ctx.workspace.get_node_path("coding_memory") or "")
	if ctx.Workspace != nil {
		if nodePath := ctx.Workspace.GetNodePath("coding_memory"); nodePath != nil {
			ctx.CodingMemoryDir = *nodePath
		}
		ctx.NodeName = "coding_memory"
	}

	// 对齐 Python: build_tool_card("coding_memory_read", "CodingMemoryReadTool", language, agent_id=agent_id)
	readCard, readErr := htools.BuildToolCard("coding_memory_read", "CodingMemoryReadTool", language, nil, agentID)
	if readErr != nil {
		readCard = tool.NewToolCardWithID("CodingMemoryReadTool", "coding_memory_read", "coding_memory_read", nil, nil)
	}
	writeCard, writeErr := htools.BuildToolCard("coding_memory_write", "CodingMemoryWriteTool", language, nil, agentID)
	if writeErr != nil {
		writeCard = tool.NewToolCardWithID("CodingMemoryWriteTool", "coding_memory_write", "coding_memory_write", nil, nil)
	}
	editCard, editErr := htools.BuildToolCard("coding_memory_edit", "CodingMemoryEditTool", language, nil, agentID)
	if editErr != nil {
		editCard = tool.NewToolCardWithID("CodingMemoryEditTool", "coding_memory_edit", "coding_memory_edit", nil, nil)
	}

	return []tool.Tool{
		&CodingMemoryReadTool{card: readCard, ctx: ctx},
		&CodingMemoryWriteTool{card: writeCard, ctx: ctx},
		&CodingMemoryEditTool{card: editCard, ctx: ctx},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// codingReadResultToMap 将 CodingReadResult 转换为 map[string]any
func codingReadResultToMap(r *lite.CodingReadResult) map[string]any {
	result := map[string]any{
		"success": r.Success,
		"path":    r.Path,
	}
	if r.Content != "" {
		result["content"] = r.Content
	}
	if r.TotalLines > 0 {
		result["total_lines"] = r.TotalLines
	}
	if r.StartLine > 0 {
		result["start_line"] = r.StartLine
	}
	if r.EndLine > 0 {
		result["end_line"] = r.EndLine
	}
	if r.Truncated {
		result["truncated"] = true
	}
	if r.Error != "" {
		result["error"] = r.Error
	}
	return result
}

// codingEditResultToMap 将 CodingEditResult 转换为 map[string]any
func codingEditResultToMap(r *lite.CodingEditResult) map[string]any {
	result := map[string]any{
		"success": r.Success,
	}
	if r.Path != "" {
		result["path"] = r.Path
	}
	if r.NewContent != "" {
		result["new_content"] = r.NewContent
	}
	if r.Error != "" {
		result["error"] = r.Error
	}
	return result
}
