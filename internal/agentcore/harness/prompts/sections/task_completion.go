package sections

import (
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	promiseGuidanceCN = "\n\n## 完成信号\n" +
		"任务完全完成后，在回复的最后一行输出 " +
		"<promise>{promise}</promise>。\n" +
		"在确认任务完成前，不要输出此标签。"

	promiseGuidanceEN = "\n\n## Completion Signal\n" +
		"When the task is fully completed, output " +
		"<promise>{promise}</promise> as the final " +
		"line of your response. Do not output this " +
		"tag until you are confident the task is " +
		"complete."

	// completionSignalPriority 完成信号节优先级
	completionSignalPriority = 85
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildCompletionSignalSection 构建完成信号节（Priority 85）
//
// promise 为完成信号令牌。
func BuildCompletionSignalSection(promise string, lang string) saprompt.PromptSection {
	var template string
	if lang == "en" {
		template = promiseGuidanceEN
	} else {
		template = promiseGuidanceCN
	}

	content := strings.ReplaceAll(template, "{promise}", promise)

	return saprompt.PromptSection{
		Name:     SectionCompletionSignal,
		Content:  map[string]string{lang: content},
		Priority: completionSignalPriority,
	}
}
