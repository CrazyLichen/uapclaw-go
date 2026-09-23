package ace

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// reMarkdownCodeBlock 提取 markdown code block 中的 JSON
	reMarkdownCodeBlock = regexp.MustCompile("```(?:json)?\\s*(\\{.*?\\})\\s*```")
	// reAnyJSONObject 提取任意 JSON 对象
	reAnyJSONObject = regexp.MustCompile("\\{.*\\}")
)

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// SafeJSONLoads 安全解析 JSON 字符串。
// 对齐 Python _safe_json_loads。
// 支持三种回退策略：直接解析 → markdown code block 提取 → 任意 JSON 对象提取。
func SafeJSONLoads(text string) (map[string]any, error) {
	// 策略 1：直接解析
	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err == nil {
		return result, nil
	}

	// 策略 2：提取 markdown code block 中的 JSON
	if match := reMarkdownCodeBlock.FindStringSubmatch(text); len(match) > 1 {
		if err := json.Unmarshal([]byte(match[1]), &result); err == nil {
			return result, nil
		}
	}

	// 策略 3：提取任意 JSON 对象
	if match := reAnyJSONObject.FindString(text); match != "" {
		if err := json.Unmarshal([]byte(match), &result); err == nil {
			return result, nil
		}
	}

	// 全部失败
	truncated := text
	if len(truncated) > 200 {
		truncated = truncated[:200]
	}
	logger.Error(logComponent).
		Str("text_preview", truncated).
		Msg("SafeJSONLoads: 无法从响应中解析有效 JSON")

	return nil, fmt.Errorf("could not parse valid JSON from response")
}
