package output_parsers

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
)

// ──────────────────────────── 类型别名 ────────────────────────────

// BaseOutputParser 重新导出 model_clients.BaseOutputParser，方便使用者仅导入 output_parsers 包。
type BaseOutputParser = model_clients.BaseOutputParser

// StreamParsedResult 重新导出 model_clients.StreamParsedResult。
type StreamParsedResult = model_clients.StreamParsedResult

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractText 从 BaseOutputParser.Parse 的输入中提取文本。
//
// 对齐 Python: BaseOutputParser.parse() 中 isinstance(llm_output, (str, AssistantMessage)) 的处理。
// 输入可以是 string 或 *AssistantMessage：
//   - string → 直接返回
//   - *AssistantMessage → 提取 .Content 文本
//   - 其他类型 → 返回空字符串
func ExtractText(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case *llmschema.AssistantMessage:
		if v == nil {
			return ""
		}
		return v.Content.Text()
	default:
		return ""
	}
}
