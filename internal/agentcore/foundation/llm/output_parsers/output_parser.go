package output_parsers

import (
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractText 从 BaseOutputParser.Parse 的输入中提取文本和模型名称。
//
// 对齐 Python: BaseOutputParser.parse() 中 isinstance(llm_output, (str, AssistantMessage)) 的处理。
// 输入可以是 string 或 *AssistantMessage：
//   - string → 直接返回文本，modelName 为空
//   - *AssistantMessage → 提取 .Content 文本 + .UsageMetadata.ModelName
//   - 其他类型 → 记录 warning 日志，返回空字符串
//
// 返回值: (text, modelName)
func ExtractText(input any) (string, string) {
	switch v := input.(type) {
	case string:
		return v, ""
	case *llmschema.AssistantMessage:
		if v == nil {
			return "", ""
		}
		modelName := ""
		if v.UsageMetadata != nil {
			modelName = v.UsageMetadata.ModelName
		}
		return v.Content.Text(), modelName
	default:
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("input_type", fmt.Sprintf("%T", input)).
			Msg("不支持的 Parse 输入类型")
		return "", ""
	}
}
