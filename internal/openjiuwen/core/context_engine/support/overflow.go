package support

import (
	"errors"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 常量 ────────────────────────────

var contextOverflowPhrases = []string{
	"context length",
	"context window",
	"maximum context",
	"context limit",
	"prompt is too long",
	"prompt too long",
	"input is too long",
	"input too long",
	// 百炼（DashScope）原文：Range of input length should be [1, 129024]
	"range of input length",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsContextOverflowError 判断错误是否为「输入超出模型上下文窗口」。
// 扫描整条错误链（含 BaseError 的 Params/Details），再兜底看格式化输出，
// 因为不同厂商把原因塞在不同位置：message、body、error.error、response.text 等。
// Python: ContextEngine.is_context_overflow_error()
func IsContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	for current := err; current != nil; current = errors.Unwrap(current) {
		if containsOverflowPhrase(current.Error()) {
			return true
		}
		var baseErr *exception.BaseError
		if errors.As(current, &baseErr) {
			if containsOverflowPhrase(fmt.Sprint(baseErr.Params())) || containsOverflowPhrase(fmt.Sprint(baseErr.Details())) {
				return true
			}
		}
	}
	// 兜底：部分实现把详情塞进自定义 Unwrap 之外的字段，只能看整体格式化结果
	return containsOverflowPhrase(fmt.Sprintf("%+v", err))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsOverflowPhrase 小写匹配任一溢出短语。
func containsOverflowPhrase(text string) bool {
	lowered := strings.ToLower(text)
	for _, phrase := range contextOverflowPhrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}
