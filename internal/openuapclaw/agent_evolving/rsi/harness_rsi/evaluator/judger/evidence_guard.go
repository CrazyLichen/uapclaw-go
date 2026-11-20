package judger

// 裁判专用证据读取工具：只读冻结工作区，分页上限，无本地请求容量拒绝。
// Python: openjiuwen/rsi/harness_rsi/evaluator/judger/evidence_guard.py（165 行）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	contextutils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ToolBytes 工具输出字节上限。
	ToolBytes = 12000
	// RequestBytes 请求字节预算（guard_messages 保留参数，不再据此拒绝）。
	RequestBytes = 262144
	// structuredReadLimit 结构化读取上限 64 MiB。
	structuredReadLimit = 64 * 1024 * 1024
	// evidenceInstructionEnv 分页续读指引（bounded_text 指令句）。
	boundedTextInstruction = "Use read_evidence with byte_offset or JSON pointer for more. " +
		"Unread evidence is not absent evidence."
	// previewInstruction 预览指引（bound_tool_content 指令句）。
	previewInstruction = "Tool output preview only, not a file page. Re-read the original file with " +
		"read_evidence using its path and JSON pointer or byte_offset=0. " +
		"Do not derive file offsets from this preview or infer missing evidence."
	// evidenceReadDescription read_evidence 卡片描述（Python 逐字）。
	evidenceReadDescription = "Read frozen evidence by byte page or JSON Pointer. JSON objects return child metadata; " +
		"arrays return a page. Use this for large single-line JSON instead of grep."
)

// ──────────────────────────── 结构体 ────────────────────────────

// JudgeEvidenceTool 只读冻结裁判工作区的证据读取工具，含 JSON Pointer 分页。
// Python: evidence_guard.py JudgeEvidenceTool(Tool)。Go Tool 契约返回
// (map[string]any, error)，Python ToolOutput(success/error) 由该契约承载（偏差登记
// spec §10 缝位 4）。
type JudgeEvidenceTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// workspace 冻结工作区绝对路径
	workspace string
}

// NewJudgeEvidenceTool 构造证据读取工具。
// Python: JudgeEvidenceTool.__init__（ToolCard id=f"{agent_id}.read_evidence"）。
func NewJudgeEvidenceTool(workspace, agentID string) *JudgeEvidenceTool {
	return &JudgeEvidenceTool{
		card: &tool.ToolCard{
			BaseCard: schema.BaseCard{
				ID:          agentID + ".read_evidence",
				Name:        "read_evidence",
				Description: evidenceReadDescription,
			},
			InputParams: []*schema.Param{
				schema.NewStringParam("path", "", true),
				schema.NewStringParam("pointer", "", false),
				schema.NewNumberParam("byte_offset", "", false),
				schema.NewNumberParam("item_offset", "", false),
			},
		},
		workspace: mustAbs(workspace),
	}
}

// Card 返回工具卡片。
func (t *JudgeEvidenceTool) Card() *tool.ToolCard { return t.card }

// GetCallTimeout 沿用默认超时。
func (t *JudgeEvidenceTool) GetCallTimeout(map[string]any, float64) float64 { return 0 }

// Invoke 执行读取（Python _read + invoke 错误包装）。
func (t *JudgeEvidenceTool) Invoke(ctx context.Context, inputs map[string]any,
	opts ...tool.ToolOption) (map[string]any, error) {
	content, err := t.read(inputs)
	if err != nil {
		bounded, boundErr := BoundedText(err.Error(), 0)
		if boundErr != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s", bounded)
	}
	return map[string]any{"content": content}, nil
}

// Stream 单块流（Python stream 直接 yield invoke 结果）。
func (t *JudgeEvidenceTool) Stream(ctx context.Context, inputs map[string]any,
	opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	ch := make(chan tool.StreamChunk, 1)
	go func() {
		defer close(ch)
		result, err := t.Invoke(ctx, inputs, opts...)
		if err != nil {
			ch <- tool.StreamChunk{Error: err}
			return
		}
		ch <- tool.StreamChunk{Data: result}
	}()
	return ch, nil
}

// read 读取主逻辑。Python: evidence_guard.py _read。
func (t *JudgeEvidenceTool) read(inputs map[string]any) (string, error) {
	pathValue, _ := inputs["path"].(string)
	if pathValue == "" {
		return "", fmt.Errorf("Evidence path must be a file inside the frozen workspace")
	}
	path := mustAbs(filepath.Join(t.workspace, pathValue))
	rel, err := filepath.Rel(t.workspace, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("Evidence path must be a file inside the frozen workspace")
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("Evidence path must be a file inside the frozen workspace")
	}
	offset, itemOffset, err := readOffsets(inputs)
	if err != nil {
		return "", err
	}
	if info.Size() > structuredReadLimit {
		return "", fmt.Errorf("Evidence exceeds the 64 MiB structured-read limit")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF") // utf-8-sig
	if pointer, present := inputs["pointer"]; present {
		pointerStr, _ := pointer.(string)
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return "", err
		}
		if pointerStr != "" && !strings.HasPrefix(pointerStr, "/") {
			return "", fmt.Errorf("JSON pointer must be empty or begin with /")
		}
		if pointerStr != "" {
			for _, token := range strings.Split(pointerStr[1:], "/") {
				key := strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
				switch node := value.(type) {
				case []any:
					if !isDigits(key) {
						return "", fmt.Errorf("Array pointer must use a non-negative index")
					}
					idx := 0
					fmt.Sscanf(key, "%d", &idx)
					if idx >= len(node) {
						return "", fmt.Errorf("list index out of range")
					}
					value = node[idx]
				case map[string]any:
					inner, present := node[key]
					if !present {
						return "", fmt.Errorf("key %q not found", key)
					}
					value = inner
				default:
					return "", fmt.Errorf("pointer into non-container")
				}
			}
		}
		switch node := value.(type) {
		case map[string]any:
			summarized := map[string]any{}
			for key, child := range node {
				summarized[key] = summaryValue(child)
			}
			value = summarized
		case []any:
			end := itemOffset + 10
			if end > len(node) {
				end = len(node)
			}
			page := node[itemOffset:end]
			value = map[string]any{"count": len(node), "offset": itemOffset, "items": page}
		}
		data, err := marshalJSONNoNaN(value)
		if err != nil {
			return "", err
		}
		text = string(data)
	}
	return BoundedText(text, offset)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BoundedText 以序列化 UTF-8 信封为界分页，而非仅按未转义内容截断。
// Python: evidence_guard.py bounded_text——超出时按 90% 递减重试以保住信封。
func BoundedText(text string, offset int) (string, error) {
	raw := []byte(text)
	end := offset + ToolBytes
	if end > len(raw) {
		end = len(raw)
	}
	for {
		part := string(raw[offset:end])
		nextOffset := offset + len(part)
		result, err := marshalJSONCompact(map[string]any{
			"content": part, "truncated": nextOffset < len(raw),
			"total_bytes": len(raw), "next_byte_offset": nextOffset,
			"instruction": boundedTextInstruction,
		})
		if err != nil {
			return "", err
		}
		if len(result) <= ToolBytes {
			return string(result), nil
		}
		end = offset + (end-offset)*9/10
	}
}

// BoundToolContent 保留源分页信封；预览不宣告文件偏移。
// Python: evidence_guard.py bound_tool_content。
func BoundToolContent(content any) any {
	s, ok := content.(string)
	if !ok || len(s) <= ToolBytes {
		return content
	}
	result, err := marshalJSONCompact(map[string]any{
		"truncated":   true,
		"preview":     s[:1000],
		"instruction": previewInstruction,
	})
	if err != nil {
		return content
	}
	return string(result)
}

// GuardMessages 对工具角色的消息内容分页；预算参数保留兼容但不据此拒绝请求。
// Python: evidence_guard.py guard_messages。消息以 map 行承载（role/content），
// 字符串输入包成单条 user 行（Python rows 逻辑）。
func GuardMessages(messages any) []map[string]any {
	var rows []map[string]any
	switch m := messages.(type) {
	case string:
		rows = []map[string]any{{"role": "user", "content": m}}
	case []map[string]any:
		rows = make([]map[string]any, 0, len(m))
		for _, row := range m {
			rows = append(rows, cloneMap(row))
		}
	case []any:
		rows = make([]map[string]any, 0, len(m))
		for _, item := range m {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, cloneMap(row))
			}
		}
	default:
		rows = []map[string]any{}
	}
	for _, row := range rows {
		if role, _ := row["role"].(string); role == "tool" {
			row["content"] = BoundToolContent(row["content"])
		}
	}
	return rows
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readOffsets 校验并提取 byte_offset/item_offset（bool 与负数拒绝）。
func readOffsets(inputs map[string]any) (int, int, error) {
	offset, err := readOffsetField(inputs, "byte_offset")
	if err != nil {
		return 0, 0, err
	}
	itemOffset, err := readOffsetField(inputs, "item_offset")
	if err != nil {
		return 0, 0, err
	}
	return offset, itemOffset, nil
}

// readOffsetField 单字段读取（Python any(isinstance(v, bool) or not int or v<0 ...)）。
func readOffsetField(inputs map[string]any, key string) (int, error) {
	raw, present := inputs[key]
	if !present || raw == nil {
		return 0, nil
	}
	switch v := raw.(type) {
	case bool:
		return 0, fmt.Errorf("Offsets must be non-negative integers")
	case int:
		if v < 0 {
			return 0, fmt.Errorf("Offsets must be non-negative integers")
		}
		return v, nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("Offsets must be non-negative integers")
		}
		return int(v), nil
	case float64:
		if v != float64(int(v)) || v < 0 {
			return 0, fmt.Errorf("Offsets must be non-negative integers")
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("Offsets must be non-negative integers")
	}
}

// summaryValue JSON Pointer 命中对象的子项摘要。Python: _summary。
func summaryValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]any, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		return map[string]any{"type": "object", "keys": keys}
	case []any:
		return map[string]any{"type": "array", "count": len(v)}
	default:
		return value
	}
}

// isDigits 纯数字串判断（Python key.isdigit()）。
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// mustAbs 绝对化路径（filepath.Abs 恒可行，失败回退原值）。
func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// marshalJSONCompact 紧凑序列化（json.dumps ensure_ascii=False 默认分隔符）。
func marshalJSONCompact(payload map[string]any) ([]byte, error) {
	return json.Marshal(payload)
}

// ──────────────────────────── GuardedJudgeModel（evidence_guard.py L132-166）────────────────────────────

// GuardedJudgeModel 守卫真实模型边界（而非 rail 前的消息预览）。
// Python: evidence_guard.py GuardedJudgeModel(Model)——构造期把 model_config 经
// with_rsi_output_budget 预算化；调用期 _prepare 以 guard_messages 边界化输入
// 证据，invoke/stream 后经 record_judge_model_response 记录完成元数据。
//
// 已知偏差（对齐登记）：Python _prepare 返回 rsi_output_options(kwargs) 处理后的
// kwargs；Go 调用点的 opts 由调用方构造（judge_runtime 各调用点均不携带
// tools/token 选项），保持原样透传即等价。
type GuardedJudgeModel struct {
	// Model 被守卫的底层模型
	*llm.Model
}

// NewGuardedJudgeModel 构造守卫裁判模型。
// Python: __init__——request = with_rsi_output_budget(
// {"model_request_config": model_config.model_dump()})["model_request_config"]；
// Go 侧 ModelRequestConfig 指针可选性已承载预算语义（同 BudgetedRsiModel D-1），
// 直接转调父类构造。
func NewGuardedJudgeModel(clientConfig *llmschema.ModelClientConfig, requestConfig *llmschema.ModelRequestConfig) (*GuardedJudgeModel, error) {
	model, err := llm.NewModel(clientConfig, requestConfig)
	if err != nil {
		return nil, err
	}
	return &GuardedJudgeModel{Model: model}, nil
}

// prepare 边界化输入证据，保留配置的请求选项。
// Python: _prepare——guarded = guard_messages(messages, tools)。
func (g *GuardedJudgeModel) prepare(messages model_clients.MessagesParam) model_clients.MessagesParam {
	// Python guard_messages 先把 BaseMessage/model_dump/str 统一成行，
	// 再对 role=tool 行做 bound_tool_content；Go 侧统一经 dict 行处理。
	rows := GuardMessages(dictsFromMessagesParam(messages))
	return model_clients.NewDictsMessagesParam(rows)
}

// Invoke 非流式调用（边界化后转调父类并记录完成元数据）。
// Python: invoke——messages, kwargs = self._prepare(messages, kwargs);
// response = await super().invoke(messages, **kwargs);
// record_judge_model_response(response)。
func (g *GuardedJudgeModel) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	response, err := g.Model.Invoke(ctx, g.prepare(messages), opts...)
	if err != nil {
		return nil, err
	}
	RecordJudgeModelResponse(ctx, response.FinishReason, response.UsageMetadata)
	return response, nil
}

// Stream 流式调用（边界化后转调父类并逐块记录完成元数据）。
// Python: stream——async for chunk in super().stream(messages, **kwargs):
// record_judge_model_response(chunk); yield chunk。
func (g *GuardedJudgeModel) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch, err := g.Model.Stream(ctx, g.prepare(messages), opts...)
	if err != nil {
		return nil, err
	}
	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		for chunk := range ch {
			RecordJudgeModelResponse(ctx, chunk.FinishReason, chunk.UsageMetadata)
			out <- chunk
		}
	}()
	return out, nil
}

// ContextBudget 与证据装配器共享模型容量。
// Python: context_budget——ContextUtils.resolve_context_max(model_name=...,
// fallback_context_window_tokens=self.model_config.context_window)。
func (g *GuardedJudgeModel) ContextBudget() int {
	var modelName string
	var fallback int
	if g.ModelConfig != nil {
		modelName = g.ModelConfig.ModelName
		if g.ModelConfig.ContextWindow != nil {
			fallback = *g.ModelConfig.ContextWindow
		}
	}
	// Python 未传 model_context_window_tokens 映射，Go 对齐传 nil。
	return contextutils.ResolveContextMax(modelName, fallback, nil)
}

// dictsFromMessagesParam 把 MessagesParam 展平成 OpenAI dict 行。
// Python guard_messages 的 model_dump(mode="json") 等价；仅覆盖裁判直评
// 调用点实际使用的 system/user 消息形态（tool 行由调用方以 dict 传入）。
func dictsFromMessagesParam(param model_clients.MessagesParam) []map[string]any {
	switch {
	case param.IsText():
		return []map[string]any{{"role": "user", "content": param.Text()}}
	case param.IsDicts():
		return param.Dicts()
	case param.IsMessages():
		rows := make([]map[string]any, 0, len(param.Messages()))
		for _, msg := range param.Messages() {
			rows = append(rows, map[string]any{
				"role":    msg.GetRole().String(),
				"content": msg.GetContent().String(),
			})
		}
		return rows
	default:
		return []map[string]any{}
	}
}
