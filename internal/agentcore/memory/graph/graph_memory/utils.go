package graph_memory

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Msg2Dict 将 BaseMessage 列表或 dict 列表转为统一的 dict 列表
//
// 当 preserveMeta 为 true 时，保留 BaseMessage 的全部字段（role/content/name/metadata）；
// 当 preserveMeta 为 false 时，仅保留 role 和 content。
// 如果输入元素已经是 dict，则原样返回。
//
// Python: msg2dict(messages, preserve_meta, **kwargs)
func Msg2Dict(messages any, preserveMeta bool) ([]map[string]any, error) {
	// 校验输入为 []BaseMessage 或 []map[string]any
	switch msgs := messages.(type) {
	case []schema.BaseMessage:
		result := make([]map[string]any, 0, len(msgs))
		for _, msg := range msgs {
			if preserveMeta {
				d := map[string]any{
					"role":    msg.GetRole().String(),
					"content": contentToAny(msg.GetContent()),
				}
				if msg.GetName() != "" {
					d["name"] = msg.GetName()
				}
				if md := msg.GetMetadata(); len(md) > 0 {
					d["metadata"] = md
				}
				result = append(result, d)
			} else {
				result = append(result, map[string]any{
					"role":    msg.GetRole().String(),
					"content": contentToAny(msg.GetContent()),
				})
			}
		}
		return result, nil

	case []map[string]any:
		return msgs, nil

	default:
		return nil, exception.BuildError(
			exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", "graph memory"),
			exception.WithParam("error_msg", "输入不是 dict 列表或 BaseMessage"),
		)
	}
}

// UpdateEntity 根据 LLM 响应更新实体内容
//
// 解析 LLM 响应后，分别处理 summary 和 attributes：
//   - summary：列表拼接为换行分隔文本，过滤 null/none/empty 等占位词
//   - attributes：尝试解析为 dict，解析失败则不保存
//
// Python: update_entity(entity, response, extraction_schema)
func UpdateEntity(entity *graph.Entity, response string, extractionSchema map[string]any) {
	extractedEntityInfo := extraction.ParseJSON(response, extractionSchema)
	if extractedEntityInfo == nil {
		extractedEntityInfo = map[string]any{}
	}

	// 如果结果是列表，取第一个元素
	if list, ok := extractedEntityInfo.([]any); ok && len(list) > 0 {
		if m, ok := list[0].(map[string]any); ok {
			extractedEntityInfo = m
		}
	}

	// 如果结果是字符串，包装为 {summary: str}
	if s, ok := extractedEntityInfo.(string); ok {
		extractedEntityInfo = map[string]any{"summary": s}
	}

	// 确保为 map[string]any
	info, ok := extractedEntityInfo.(map[string]any)
	if !ok {
		return
	}

	parseSummary(entity, info)
	parseAttributes(entity, info)
}

// AssembleInvokeParams 组装 LLM 调用参数
//
// 将模板格式化后的消息列表转为 dict 列表，并可选附加 response_format。
//
// Python: assemble_invoke_params(kwargs, template, output_model)
func AssembleInvokeParams(kwargs map[string]any, tmpl *prompt.PromptTemplate, outputModel map[string]any) (map[string]any, error) {
	// 格式化模板
	formatted, err := tmpl.Format(kwargs)
	if err != nil {
		return nil, err
	}

	// 将格式化后的 content 转为消息列表
	var messages any
	switch content := formatted.Content.(type) {
	case string:
		um := schema.NewUserMessage(content)
		messages = []schema.BaseMessage{um}
	case []schema.BaseMessage:
		messages = content
	default:
		return nil, fmt.Errorf("模板格式化后的内容类型不支持: %T", formatted.Content)
	}

	// 转为 dict 列表
	dictMessages, err := Msg2Dict(messages, false)
	if err != nil {
		return nil, err
	}

	params := map[string]any{"messages": dictMessages}
	if outputModel != nil {
		params["response_format"] = outputModel
	}
	return params, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// contentToAny 将 MessageContent 转为 any（string 或 []ContentPart）
func contentToAny(c schema.MessageContent) any {
	if c.IsText() {
		return c.Text()
	}
	return c.Parts()
}

// parseSummary 处理提取的摘要信息
//
// Python: _parse_summary(entity, extracted_entity_info)
func parseSummary(entity *graph.Entity, info map[string]any) {
	summaryVal := info["summary"]
	if summaryVal == nil {
		return
	}

	var summary string
	switch s := summaryVal.(type) {
	case []any:
		// 列表类型：拼接为换行分隔的文本
		lines := make([]string, 0, len(s))
		for _, item := range s {
			line := strings.TrimSpace(fmt.Sprintf("%v", item))
			if line != "" {
				lines = append(lines, line)
			}
		}
		summary = strings.Join(lines, "\n")
	case string:
		summary = s
	default:
		if summaryVal != nil {
			summary = fmt.Sprintf("%v", summaryVal)
		}
	}

	summary = strings.TrimSpace(summary)
	summaryCleaned := strings.ToLower(summary)

	// 过滤 null/none/empty 等占位词
	if summary != "" && !containsNullWord(summaryCleaned) {
		entity.Content = summary
	}
}

// parseAttributes 处理提取的属性信息
//
// Python: _parse_attributes(entity, extracted_entity_info)
func parseAttributes(entity *graph.Entity, info map[string]any) {
	attributesVal := info["attributes"]
	if attributesVal == nil {
		return
	}

	var attributes map[string]any

	switch attr := attributesVal.(type) {
	case string:
		// 字符串类型：尝试二次 JSON 解析
		parsed := extraction.ParseJSON(attr, nil)
		if m, ok := parsed.(map[string]any); ok {
			attributes = m
		}
	case []any:
		// 列表类型：尝试转为 dict
		m := make(map[string]any)
		for i, item := range attr {
			if pair, ok := item.([]any); ok && len(pair) == 2 {
				if key, ok := pair[0].(string); ok {
					m[key] = pair[1]
				}
			}
			_ = i
		}
		if len(m) > 0 {
			attributes = m
		} else {
			logger.Info(logComponent).
				Str("event", "parse_attributes_failed").
				Msg("Graph Memory: Failed to parse extracted entity attribute from list")
		}
	case map[string]any:
		attributes = attr
	default:
		// 尝试通过 JSON 序列化/反序列化转换
		data, err := json.Marshal(attributesVal)
		if err == nil {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				attributes = m
			}
		}
	}

	if len(attributes) > 0 {
		entity.Attributes = attributes
	}
}

// containsNullWord 检查字符串中是否包含 null/none/empty 占位词
//
// Python: all(null_word not in summary_cleaned for null_word in ["null", "none", "empty"])
func containsNullWord(s string) bool {
	for _, word := range []string{"null", "none", "empty"} {
		if strings.Contains(s, word) {
			return true
		}
	}
	return false
}
