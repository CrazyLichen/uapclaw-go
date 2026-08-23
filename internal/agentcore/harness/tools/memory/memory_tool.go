package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	htools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySearchTool 语义搜索记忆工具。对齐 Python MemorySearchTool
type MemorySearchTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 记忆工具上下文
	ctx *lite.MemoryToolContext
}

// MemoryGetTool 按行号切片读取记忆文件工具。对齐 Python MemoryGetTool
type MemoryGetTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 记忆工具上下文
	ctx *lite.MemoryToolContext
}

// ReadMemoryTool 按 offset/limit 读取记忆文件工具。对齐 Python ReadMemoryTool
type ReadMemoryTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 记忆工具上下文
	ctx *lite.MemoryToolContext
}

// WriteMemoryTool 写入/追加记忆文件工具。对齐 Python WriteMemoryTool
type WriteMemoryTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 记忆工具上下文
	ctx *lite.MemoryToolContext
}

// EditMemoryTool 精确字符串替换记忆文件工具。对齐 Python EditMemoryTool
type EditMemoryTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ctx 记忆工具上下文
	ctx *lite.MemoryToolContext
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Card 返回工具配置卡片
func (t *MemorySearchTool) Card() *tool.ToolCard { return t.card }

// Invoke 语义搜索记忆。对齐 Python MemorySearchTool.invoke
func (t *MemorySearchTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	query, _ := inputs["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query 不能为空")
	}
	var maxResults *int
	if v, ok := inputs["max_results"]; ok {
		if n, ok := toInt(v); ok {
			maxResults = &n
		}
	}
	var minScore *float64
	if v, ok := inputs["min_score"]; ok {
		if f, ok := toFloat64(v); ok {
			minScore = &f
		}
	}
	sessionKey, _ := inputs["session_key"].(string)
	result := lite.MemorySearchWithContext(ctx, t.ctx, query, maxResults, minScore, sessionKey)
	return structToMap(result), nil
}

// Stream 不支持流式调用
func (t *MemorySearchTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// Card 返回工具配置卡片
func (t *MemoryGetTool) Card() *tool.ToolCard { return t.card }

// Invoke 按行号切片读取记忆文件。对齐 Python MemoryGetTool.invoke
func (t *MemoryGetTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path 不能为空")
	}
	var fromLine *int
	if v, ok := inputs["from_line"]; ok {
		if n, ok := toInt(v); ok {
			fromLine = &n
		}
	}
	var lines *int
	if v, ok := inputs["lines"]; ok {
		if n, ok := toInt(v); ok {
			lines = &n
		}
	}
	result := lite.MemoryGetWithContext(ctx, t.ctx, path, fromLine, lines)
	return structToMap(result), nil
}

// Stream 不支持流式调用
func (t *MemoryGetTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// Card 返回工具配置卡片
func (t *ReadMemoryTool) Card() *tool.ToolCard { return t.card }

// Invoke 按 offset/limit 读取记忆文件。对齐 Python ReadMemoryTool.invoke
func (t *ReadMemoryTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path 不能为空")
	}
	var offset *int
	if v, ok := inputs["offset"]; ok {
		if n, ok := toInt(v); ok {
			offset = &n
		}
	}
	var limit *int
	if v, ok := inputs["limit"]; ok {
		if n, ok := toInt(v); ok {
			limit = &n
		}
	}
	result := lite.ReadMemoryWithContext(ctx, t.ctx, path, offset, limit)
	return structToMap(result), nil
}

// Stream 不支持流式调用
func (t *ReadMemoryTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// Card 返回工具配置卡片
func (t *WriteMemoryTool) Card() *tool.ToolCard { return t.card }

// Invoke 写入/追加记忆文件。对齐 Python WriteMemoryTool.invoke
func (t *WriteMemoryTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path 不能为空")
	}
	content, _ := inputs["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content 不能为空")
	}
	appendMode := false
	if v, ok := inputs["append"].(bool); ok {
		appendMode = v
	}
	result := lite.WriteMemoryWithContext(ctx, t.ctx, path, content, appendMode)
	return structToMap(result), nil
}

// Stream 不支持流式调用
func (t *WriteMemoryTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// Card 返回工具配置卡片
func (t *EditMemoryTool) Card() *tool.ToolCard { return t.card }

// Invoke 精确字符串替换记忆文件。对齐 Python EditMemoryTool.invoke
func (t *EditMemoryTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path 不能为空")
	}
	oldText, _ := inputs["old_text"].(string)
	if oldText == "" {
		return nil, fmt.Errorf("old_text 不能为空")
	}
	newText, _ := inputs["new_text"].(string)
	if newText == "" {
		return nil, fmt.Errorf("new_text 不能为空")
	}
	result := lite.EditMemoryWithContext(ctx, t.ctx, path, oldText, newText)
	return structToMap(result), nil
}

// Stream 不支持流式调用
func (t *EditMemoryTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// CreateMemoryTools 创建记忆工具集。对齐 Python create_memory_tools(ctx, language, agent_id)
func CreateMemoryTools(ctx *lite.MemoryToolContext, language string, agentID string) []tool.Tool {
	// 对齐 Python: Python: if ctx.settings is None and ctx.workspace is not None:
	//   Python: memory_dir = str(ctx.workspace.get_node_path("memory") or "")
	//   Python: ctx.settings = create_memory_settings(memory_dir)
	if ctx.Settings == nil && ctx.Workspace != nil {
		memoryDir := ""
		if nodePath := ctx.Workspace.GetNodePath("memory"); nodePath != nil {
			memoryDir = *nodePath
		}
		ctx.Settings = lite.CreateMemorySettings(memoryDir, nil)
	}
	// 对齐 Python: 设置 NodeName
	if ctx.NodeName == "" {
		ctx.NodeName = "memory"
	}

	// 构建 5 个 Tool
	type toolDef struct {
		name   string
		toolID string
		build  func(card *tool.ToolCard) tool.Tool
	}
	defs := []toolDef{
		{"memory_search", "memory_search", func(card *tool.ToolCard) tool.Tool { return &MemorySearchTool{card: card, ctx: ctx} }},
		{"memory_get", "memory_get", func(card *tool.ToolCard) tool.Tool { return &MemoryGetTool{card: card, ctx: ctx} }},
		{"write_memory", "write_memory", func(card *tool.ToolCard) tool.Tool { return &WriteMemoryTool{card: card, ctx: ctx} }},
		{"edit_memory", "edit_memory", func(card *tool.ToolCard) tool.Tool { return &EditMemoryTool{card: card, ctx: ctx} }},
		{"read_memory", "read_memory", func(card *tool.ToolCard) tool.Tool { return &ReadMemoryTool{card: card, ctx: ctx} }},
	}

	var tools []tool.Tool
	for _, d := range defs {
		card, err := htools.BuildToolCard(d.name, d.toolID, language, nil, agentID)
		if err != nil {
			// 对齐 Python: BuildToolCard 返回错误时用默认卡片
			card = tool.NewToolCardWithID(d.toolID, d.name, d.name, nil, nil)
		}
		tools = append(tools, d.build(card))
	}
	return tools
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toInt 将任意值转换为 int（支持 float64/float32/int/int32/int64）
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// toFloat64 将任意值转换为 float64（支持 float64/float32/int/int32/int64）
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// structToMap 将结果结构体转换为 map[string]any。
// 使用 JSON 序列化/反序列化实现通用转换。
func structToMap(v any) map[string]any {
	// 通过 JSON round-trip 转换
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return result
}
