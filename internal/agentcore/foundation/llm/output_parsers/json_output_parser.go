package output_parsers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent output_parsers 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// jsonCodeBlockRegexp 匹配 markdown 代码块中的 JSON。
// 对齐 Python: re.search(r"```json\n(.*?)```", text, re.DOTALL)
var jsonCodeBlockRegexp = regexp.MustCompile("(?s)```json\n(.*?)```")

// ──────────────────────────── 结构体 ────────────────────────────

// JsonOutputParser JSON 输出解析器，从 LLM 输出中提取 JSON 数据。
//
// 解析策略（优先级）：
//  1. 先尝试从 markdown 代码块（```json ... ```）中提取
//  2. 未找到代码块则直接解析文本
//
// 对应 Python: openjiuwen/core/foundation/llm/output_parsers/json_output_parser.py (JsonOutputParser)
type JsonOutputParser struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJsonOutputParser 创建 JSON 输出解析器。
func NewJsonOutputParser() *JsonOutputParser {
	return &JsonOutputParser{}
}

// Parse 解析 LLM 输出中的 JSON。
//
// 输入可以是 string 或 *AssistantMessage（对齐 Python Union[str, AssistantMessage]）。
// 解析成功返回 map/slice/基础类型，解析失败返回 nil, error。
// 空输入返回 nil, nil（语义：无内容可解析，不是错误）。
//
// 对应 Python: JsonOutputParser.parse()
func (p *JsonOutputParser) Parse(input any) (any, error) {
	text, modelName := ExtractText(input)
	if text == "" {
		return nil, nil
	}

	// 先尝试从 markdown 代码块提取 JSON
	jsonStr := ""
	match := jsonCodeBlockRegexp.FindStringSubmatch(text)
	if len(match) >= 2 {
		jsonStr = strings.TrimSpace(match[1])
	} else {
		jsonStr = strings.TrimSpace(text)
	}

	// 解析 JSON
	var parsed any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("model_name", modelName).
			Err(err).
			Msg("JSON 解析失败")
		return nil, fmt.Errorf("failed to decode JSON from LLM output: %w", err)
	}

	return parsed, nil
}

// StreamParse 流式解析 LLM 输出中的 JSON。
//
// 对齐 Python: JsonOutputParser.stream_parse()。
// chunks 支持 string 和 *AssistantMessageChunk 两种类型（对齐 Python Union[str, AssistantMessageChunk]）。
// 策略：
//  1. 逐 chunk 累积 content 到 buffer
//  2. 每个 chunk 后尝试两种解析：
//     a. markdown 代码块匹配 → 提取 JSON → 成功则输出 + 截断 buffer
//     b. buffer 以 { 开头且以 } 结尾 → 直接解析 → 成功则输出 + 清空 buffer
//  3. 流结束后处理残余 buffer
func (p *JsonOutputParser) StreamParse(chunks <-chan any) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult, 8)

	go func() {
		defer close(out)

		buffer := ""
		modelName := ""

		for chunk := range chunks {
			content := ""
			switch v := chunk.(type) {
			case *llmschema.AssistantMessageChunk:
				if v == nil {
					continue
				}
				content = v.Content.Text()
				if v.UsageMetadata != nil && v.UsageMetadata.ModelName != "" {
					modelName = v.UsageMetadata.ModelName
				}
			case string:
				content = v
			default:
				logger.Warn(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("model_name", modelName).
					Str("chunk_type", fmt.Sprintf("%T", chunk)).
					Msg("不支持的 chunk 类型，已跳过")
				continue
			}

			if content != "" {
				buffer += content
			}

			// 尝试解析策略 A：markdown 代码块
			match := jsonCodeBlockRegexp.FindStringSubmatch(buffer)
			if len(match) >= 2 {
				jsonStr := strings.TrimSpace(match[1])
				var parsed any
				if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
					out <- model_clients.StreamParsedResult{Content: parsed}
					// 截断 buffer：从 match 结束位置之后继续
					loc := jsonCodeBlockRegexp.FindStringIndex(buffer)
					if len(loc) >= 2 {
						buffer = strings.TrimSpace(buffer[loc[1]:])
					} else {
						buffer = ""
					}
				} else {
					// 代码块匹配但 JSON 解析失败，记录日志，继续累积
					logger.Error(logComponent).
						Str("event_type", "LLM_CALL_ERROR").
						Str("model_name", modelName).
						Err(err).
						Msg("流式 JSON 解析失败（代码块）")
				}
				continue
			}

			// 尝试解析策略 B：裸 JSON 对象
			trimmed := strings.TrimSpace(buffer)
			if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
				var parsed any
				if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
					out <- model_clients.StreamParsedResult{Content: parsed}
					buffer = ""
				} else {
					// 直接 JSON 解析失败，清空 buffer
					logger.Error(logComponent).
						Str("event_type", "LLM_CALL_ERROR").
						Str("model_name", modelName).
						Err(err).
						Msg("流式 JSON 解析失败（直接）")
					buffer = ""
				}
			}
		}

		// 处理残余 buffer
		if strings.TrimSpace(buffer) != "" {
			// 先尝试 markdown 代码块
			match := jsonCodeBlockRegexp.FindStringSubmatch(buffer)
			jsonStr := ""
			if len(match) >= 2 {
				jsonStr = strings.TrimSpace(match[1])
			} else {
				jsonStr = strings.TrimSpace(buffer)
			}

			var parsed any
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
				out <- model_clients.StreamParsedResult{Content: parsed}
			} else {
				logger.Warn(logComponent).
					Str("event_type", "LLM_CALL_ERROR").
					Str("model_name", modelName).
					Err(err).
					Msg("残余 buffer JSON 解析失败")
			}
		}
	}()

	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateForLog 截断长字符串用于日志输出。
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
