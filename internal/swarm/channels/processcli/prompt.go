package processcli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/elk-language/go-prompt"
	pstrings "github.com/elk-language/go-prompt/strings"
)

// 本文件是 channels/process_cli/prompt.py 的 Go 对应物。
//
// Python: channels/process_cli/prompt.py（139 行）
//
// 库选型偏差 E-2：上游用 prompt_toolkit，Go 用 elk-language/go-prompt
// （后者的 README 明确声明 inspired by python-prompt-toolkit，补全/多行/
// 历史/Emacs 快捷键/跨平台齐全）。
//
// 偏差 E-9：go-prompt 无 alternate screen 能力，live 模式退化为
// 「流式输出 + 底部输入行」，不设全屏。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// promptText REPL 主提示符
	// Python: prompt.py:22 PROMPT_TEXT
	promptText = "uapclaw> "
	// liveInputPrompt 运行中补充输入提示符
	// Python: prompt.py:23 LIVE_INPUT_PROMPT
	liveInputPrompt = "输入栏\n补充输入> "
	// commandColumnWidth 补全菜单命令行宽
	// Python: prompt.py:24 _COMMAND_COLUMN_WIDTH
	commandColumnWidth = 22
	// maxSuggestions 补全菜单最多显示条数
	maxSuggestions = 8
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newPromptSession 只在真实交互终端下创建增强读取器。
// Python: prompt.py:88-106 create_prompt_session（isatty 双条件，否则 nil）
//
// 非 TTY 时返回 nil，readPrompt 退化为逐行读 stdin——管道测试与
// `echo "..." | uapclaw-process` 依赖这条降级路径。
func newPromptSession(stdin io.Reader, stdout io.Writer) *prompt.Prompt {
	if !isTerminalReader(stdin) || !isTerminalWriter(stdout) {
		return nil
	}
	return prompt.New(
		func(string) {},
		prompt.WithPrefix(promptText),
		prompt.WithCompleter(slashCompleter),
		prompt.WithMaxSuggestion(maxSuggestions),
	)
}

// newLivePromptSession 为运行中的 worker 轮次创建读取器。
// Python: prompt.py:109-120 create_live_prompt_session
//
// 偏差 E-9：上游 full_screen=True 的 alternate screen 已退化，本会话只
// 提供底部输入行；请求/处理状态与模型输出以普通文本流式打印。
func newLivePromptSession() *prompt.Prompt {
	return prompt.New(
		func(string) {},
		prompt.WithPrefix(liveInputPrompt),
		prompt.WithMaxSuggestion(0),
	)
}

// readPrompt 读一条指令，保留管道/测试支持而不接管终端。
// Python: prompt.py:123-131 read_prompt
func readPrompt(session *prompt.Prompt, promptLabel string) (string, error) {
	return readPromptFrom(session, promptLabel, os.Stdin)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readPromptFrom 可注入 stdin 的读取实现（测试用）。
func readPromptFrom(session *prompt.Prompt, promptLabel string, stdin io.Reader) (string, error) {
	if session != nil {
		// go-prompt 无异步 API：Input 阻塞直到提交
		return strings.TrimSpace(session.Input()), nil
	}
	// 非 TTY 降级：逐行读 stdin
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		if strings.TrimSpace(line) == "" {
			return "", io.EOF
		}
		return strings.TrimSpace(line), nil
	}
	return strings.TrimSpace(line), nil
}

// slashCompleter 只在首个 token 以 "/" 开头时提供本地命令补全。
// Python: prompt.py:29-46 SlashCommandCompleter
func slashCompleter(d prompt.Document) ([]prompt.Suggest, pstrings.RuneNumber, pstrings.RuneNumber) {
	return slashCompleterText(d.TextBeforeCursor())
}

// slashCompleterText 可单测的补全核心。
//
// 两段（顺序即语义）：先补参数（`/mode ` 之后），再补命令（首 token）。
func slashCompleterText(prefix string) ([]prompt.Suggest, pstrings.RuneNumber, pstrings.RuneNumber) {
	argumentFragment := ""
	hasSpace := false
	if index := strings.LastIndex(prefix, " "); index >= 0 {
		argumentFragment = prefix[index+1:]
		hasSpace = true
	}

	suggests := make([]prompt.Suggest, 0, 4)
	for _, argument := range MatchingSlashArguments(prefix) {
		suggests = append(suggests, prompt.Suggest{
			Text:        argument.Value,
			Description: padDescription(argument.Description),
		})
	}
	if hasSpace {
		start := len([]rune(prefix)) - len([]rune(argumentFragment))
		return suggests, pstrings.RuneNumber(start), pstrings.RuneNumber(len([]rune(prefix)))
	}

	for _, command := range MatchingSlashCommands(prefix) {
		suggests = append(suggests, prompt.Suggest{
			Text:        command.Name,
			Description: padDescription(command.Description),
		})
	}
	return suggests, 0, pstrings.RuneNumber(len([]rune(prefix)))
}

// padDescription 把描述左侧补齐到命令列宽，避免中文错列。
// Python: prompt.py:36-43 display=command.name.ljust(_COMMAND_COLUMN_WIDTH)
func padDescription(description string) string {
	return fmt.Sprintf("%s%s", strings.Repeat(" ", commandColumnWidth), description)
}
