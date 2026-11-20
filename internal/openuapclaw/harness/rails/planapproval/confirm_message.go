// 确认中断文案的构建。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/code/rails/code_confirm_interrupt_rail.py
// 的 build_confirm_interrupt_message（L37-76）与 _format_args_preview（L28-34）。
//
// # 为什么要自己构建文案
//
// agent-core 的 ConfirmInterruptRail 只给一句通用问句。code 模式下用户需要
// 看到「要做什么、参数是什么」，尤其是 switch_mode 这类模式切换必须说清
// 是进 plan 还是退 plan，否则用户不知道批准后会发生什么。
package planapproval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// argsPreviewMaxLength 参数预览最大字符数
	//
	// Python: json.dumps(...)[:800]
	argsPreviewMaxLength = 800
	// switchModeToolName 模式切换工具名
	switchModeToolName = "switch_mode"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// planModeNames 进入 plan 模式的目标取值。
	//
	// Python: if target_mode == "plan"
	planModeNames = map[string]bool{"plan": true}

	// executionModeNames 退出 plan、回到执行的目标取值。
	//
	// Python: elif target_mode in {"normal", "auto"}
	executionModeNames = map[string]bool{"normal": true, "auto": true}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildConfirmInterruptMessage 为前端构建描述性的确认提示。
//
// Python: build_confirm_interrupt_message(tool_name, tool_args)
// (code_confirm_interrupt_rail.py L37-76)
//
// switch_mode 单独一套文案：必须说清是进 plan 还是退 plan，
// 否则用户不知道批准后会发生什么。其余工具走通用文案 + 参数预览。
func BuildConfirmInterruptMessage(toolName string, toolArgs map[string]any) string {
	args := toolArgs
	if args == nil {
		args = map[string]any{}
	}

	if toolName == switchModeToolName {
		targetMode := strings.TrimSpace(anyString(args["mode"]))
		if targetMode == "" {
			targetMode = strings.TrimSpace(anyString(args["target_mode"]))
		}

		var action string
		switch {
		case planModeNames[targetMode]:
			action = "enter plan mode (read-only planning phase)"
		case executionModeNames[targetMode]:
			action = "exit plan mode and return to execution"
		default:
			display := targetMode
			if display == "" {
				display = "unknown"
			}
			action = fmt.Sprintf("switch agent mode to `%s`", display)
		}

		lines := []string{
			fmt.Sprintf("**Agent wants to %s**", action),
			"",
			"Tool: `switch_mode`",
		}
		if targetMode != "" {
			lines = append(lines, fmt.Sprintf("Target mode: `%s`", targetMode))
		}
		lines = append(lines, "")
		lines = append(lines, "Approve to let the agent continue with this mode change.")
		return strings.Join(lines, "\n")
	}

	lines := []string{
		fmt.Sprintf("**Tool `%s` requires your approval**", toolName),
		"",
		"The agent is waiting for you to allow or reject this action.",
	}
	preview := formatArgsPreview(args)
	if preview != "" && preview != "{}" {
		lines = append(lines, "", "Arguments:", "```json\n"+preview+"\n```")
	}
	return strings.Join(lines, "\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatArgsPreview 把工具参数序列化为缩进 JSON 预览，最长 800 字符。
//
// Python: _format_args_preview(tool_args) (code_confirm_interrupt_rail.py L28-34)
//
// 序列化失败时回落到 fmt 的字符串形式，同样截断到 800——
// 参数可能含无法 JSON 序列化的值，此时宁可显示原始形态也不让确认框变空。
func formatArgsPreview(toolArgs map[string]any) string {
	if len(toolArgs) == 0 {
		return ""
	}
	encoded, err := json.MarshalIndent(toolArgs, "", "  ")
	if err != nil {
		text := fmt.Sprint(toolArgs)
		return truncateRunes(text, argsPreviewMaxLength)
	}
	return truncateRunes(string(encoded), argsPreviewMaxLength)
}

// truncateRunes 按字符（而非字节）截断，避免切断多字节字符。
func truncateRunes(text string, maxLength int) string {
	runes := []rune(text)
	if len(runes) <= maxLength {
		return text
	}
	return string(runes[:maxLength])
}

// anyString 把值转成字符串，nil 返回空串。
func anyString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
