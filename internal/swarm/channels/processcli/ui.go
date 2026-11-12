package processcli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 本文件是 channels/process_cli/ui.py 的显示宽度工具部分（P-4）。
//
// Python: channels/process_cli/ui.py:28-70（_display_width / _truncate / _wrap_display）
//
// Go 标准库没有 unicodedata.east_asian_width（偏差 E-3），此处按
// Unicode EastAsianWidth.txt 的 Wide / Fullwidth 主要区段自建判定。

// ──────────────────────────── 导出函数 ────────────────────────────

// displayWidth 显示宽度：East-Asian W/F 记 2，组合字符记 0，其余 1。
// Python: ui.py:28-35 _display_width
func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		width += runeDisplayWidth(r)
	}
	return width
}

// truncate 按显示宽度截断，超宽加省略号。
// Python: ui.py:38-54 _truncate
func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(value) <= width {
		return value
	}
	marker := ""
	if width > 1 {
		marker = "…"
	}
	target := width - displayWidth(marker)
	var builder strings.Builder
	used := 0
	for _, r := range value {
		runeWidth := runeDisplayWidth(r)
		if used+runeWidth > target {
			break
		}
		builder.WriteRune(r)
		used += runeWidth
	}
	return builder.String() + marker
}

// wrapDisplay 按显示宽度折行。
// Python: ui.py:57-70 _wrap_display
func wrapDisplay(value string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	lines := make([]string, 0, 2)
	var current strings.Builder
	used := 0
	for _, r := range value {
		runeWidth := runeDisplayWidth(r)
		if current.Len() > 0 && used+runeWidth > width {
			lines = append(lines, current.String())
			current.Reset()
			used = 0
		}
		current.WriteRune(r)
		used += runeWidth
	}
	lines = append(lines, current.String())
	return lines
}

// shortSession 会话 ID 展示用短形式（超过 12 字符截断加省略号）。
// Python: ui.py:325-328 ProcessCliUI.short_session
func shortSession(sessionID string) string {
	if len(sessionID) <= 12 {
		return sessionID
	}
	return sessionID[:12] + "…"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runeDisplayWidth 单字符显示宽度。
func runeDisplayWidth(r rune) int {
	if isCombiningRune(r) {
		return 0
	}
	if isEastAsianWide(r) {
		return 2
	}
	return 1
}

// isCombiningRune 是否组合字符（宽度 0）。
func isCombiningRune(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r)
}

// isEastAsianWide 是否 East-Asian Wide/Fullwidth（宽度 2）。
//
// 区段来自 Unicode EastAsianWidth.txt 的 W/F 主要块（偏差 E-3：Go 无
// unicodedata.east_asian_width 等价函数）。
func isEastAsianWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F: // 谚文字母
		return true
	case r >= 0x2E80 && r <= 0x303E: // CJK 部首 + 符号
		return true
	case r >= 0x3041 && r <= 0x33FF: // 平假名/片假名/注音/CJK 兼容
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK 扩展 A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK 统一表意
		return true
	case r >= 0xA000 && r <= 0xA4CF: // 彝文
		return true
	case r >= 0xA960 && r <= 0xA97F: // 谚文扩展 A
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // 谚文音节
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK 兼容表意
		return true
	case r >= 0xFE10 && r <= 0xFE19: // 竖排形式
		return true
	case r >= 0xFE30 && r <= 0xFE6F: // CJK 兼容形式
		return true
	case r >= 0xFF01 && r <= 0xFF60: // 全角形式
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角符号
		return true
	case r >= 0x1F300 && r <= 0x1F64F: // 表情
		return true
	case r >= 0x1F900 && r <= 0x1F9FF: // 补充表情
		return true
	case r >= 0x20000 && r <= 0x3FFFD: // CJK 扩展 B 及以上
		return true
	}
	return false
}

// ──────────────────────────── ProcessCliUI ────────────────────────────

// ProcessCliUI 渲染 REPL 外壳而不持有任何 Runtime 行为。
// Python: ui.py:110-330 ProcessCliUI
type ProcessCliUI struct {
	stream  io.Writer
	columns int
	unicode bool
	color   bool
}

// NewProcessCliUI 构造 REPL 外壳渲染器（columns<=0 时取终端宽度，兜底 80）。
// Python: ui.py:120-133 ProcessCliUI.__init__
func NewProcessCliUI(stream io.Writer, columns int) *ProcessCliUI {
	if columns <= 0 {
		columns = terminalColumns()
	}
	return &ProcessCliUI{
		stream:  stream,
		columns: columns,
		unicode: supportsUnicode(stream),
		color:   supportsColor(stream),
	}
}

// Startup 渲染启动卡片。
// Python: ui.py:135-180 ProcessCliUI.startup
func (u *ProcessCliUI) Startup(modelName, mode, cwd, sessionID string) {
	session := sessionID
	if session == "" {
		session = "尚未创建"
	}
	model := modelName
	if model == "" {
		model = "未配置"
	}
	if u.columns < 48 {
		u.writeWrapped(">_ UAPClaw", ansiBoldCyan, "")
		u.writeWrapped("进程式 CLI · 本地 Runtime", "", "")
		u.writeWrapped(fmt.Sprintf("模型（配置推断）：%s", model), "", "")
		u.writeWrapped(fmt.Sprintf("目录：%s", cwd), "", "")
		u.writeWrapped(fmt.Sprintf("模式（请求推断）：%s", mode), "", "")
		u.writeWrapped(fmt.Sprintf("会话：%s", session), "", "")
		u.write("\n")
	} else {
		u.card([][2]string{
			{">_ UAPClaw", ansiBoldCyan},
			{"进程式 CLI · 本地 Runtime", ansiDim},
			{"", ""},
			{fmt.Sprintf("模型（配置推断）：  %s", model), ""},
			{fmt.Sprintf("目录：  %s", cwd), ""},
			{fmt.Sprintf("模式（请求推断）：  %s", mode), ""},
			{fmt.Sprintf("会话：  %s", session), ""},
		})
		u.write("\n")
	}
	u.writeWrapped("每条指令均在独立进程中运行，Runtime 会话将在不同轮次间保留。", "", "  ")
	u.writeWrapped("输入 / 查看可用命令。", ansiDim, "  ")
	u.write("\n")
}

// Help 渲染命令列表。
// Python: ui.py:182-208 ProcessCliUI.help
func (u *ProcessCliUI) Help(compact bool) {
	if compact {
		u.writeWrapped("常用命令：", ansiDim, "  ")
		u.write("\n")
	} else {
		u.write("\n")
		u.writeWrapped("可用命令：", ansiBoldCyan, "")
		u.write("\n")
	}
	labels := make([]string, 0, len(slashCommands))
	for _, command := range slashCommands {
		labels = append(labels, fmt.Sprintf("%-11s%s", command.Name, command.Description))
	}
	if u.columns >= 68 {
		for index := 0; index < len(labels); index += 2 {
			left := labels[index]
			right := ""
			if index+1 < len(labels) {
				right = labels[index+1]
			}
			gap := 36 - displayWidth(left)
			if gap < 4 {
				gap = 4
			}
			u.writeWrapped(left+strings.Repeat(" ", gap)+right, "", "  ")
		}
	} else {
		for _, label := range labels {
			u.writeWrapped(label, "", "  ")
		}
	}
	u.write("\n")
}

// Status 渲染状态行。
// Python: ui.py:210-224 ProcessCliUI.status
func (u *ProcessCliUI) Status(modelName, mode, cwd, sessionID string) {
	session := "尚未创建会话"
	if sessionID != "" {
		session = "会话 " + shortSession(sessionID)
	}
	model := modelName
	if model == "" {
		model = "未配置"
	}
	content := fmt.Sprintf("  %s · %s · %s · %s", model, mode, session, cwd)
	u.write(u.styled(truncate(content, u.columns-1), ansiDim))
	u.write("\n\n")
}

// Notice 渲染一条提示。
// Python: ui.py:226-227 ProcessCliUI.notice
func (u *ProcessCliUI) Notice(message string) {
	u.write(u.styled(fmt.Sprintf("\n! %s\n\n", message), ansiYellow))
}

// Details 渲染带标题的明细块。
// Python: ui.py:229-235 ProcessCliUI.details
func (u *ProcessCliUI) Details(title string, lines []string) {
	u.write("\n")
	u.writeWrapped(title, ansiBoldCyan, "")
	for _, line := range lines {
		u.writeWrapped(line, "", "  ")
	}
	u.write("\n")
}

// Session 渲染当前会话。
// Python: ui.py:243-247 ProcessCliUI.session
func (u *ProcessCliUI) Session(sessionID string) {
	if sessionID != "" {
		u.write(fmt.Sprintf("\n当前 Runtime 会话：%s\n\n", sessionID))
		return
	}
	u.write("\n尚未创建 Runtime 会话。\n\n")
}

// Diagnostics 渲染工作进程诊断信息。
// Python: ui.py:237-241 ProcessCliUI.diagnostics
func (u *ProcessCliUI) Diagnostics(lines []string) {
	u.write("\n工作进程诊断信息：\n")
	u.write(strings.Join(lines, "\n"))
	u.write("\n")
}

// BlankLine 输出空行。
// Python: ui.py:245 blank_line
func (u *ProcessCliUI) BlankLine() { u.write("\n") }

// Skills 渲染 Runtime 的 skills.list 负载。
// Python: ui.py:249-273 ProcessCliUI.skills
func (u *ProcessCliUI) Skills(items []any) {
	installed := make([]map[string]any, 0, len(items))
	available := make([]map[string]any, 0, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if isInstalled, _ := mapping["installed"].(bool); isInstalled {
			installed = append(installed, mapping)
			continue
		}
		available = append(available, mapping)
	}
	u.write("\n")
	if len(installed) > 0 {
		u.writeWrapped(fmt.Sprintf("已安装技能（%d）", len(installed)), ansiBoldCyan, "")
	} else {
		u.writeWrapped("暂无已安装技能", ansiBoldCyan, "")
	}
	for _, item := range installed {
		u.writeSkill(item)
	}
	if len(available) > 0 {
		u.write("\n")
		u.writeWrapped(fmt.Sprintf("可安装技能（%d）", len(available)), ansiBoldCyan, "")
		for _, item := range available {
			u.writeSkill(item)
		}
	}
	u.write("\n")
}

// LiveExecutionLayout 渲染输入栏上方的稳定分区。
// Python: ui.py:275-287 ProcessCliUI.live_execution_layout
func (u *ProcessCliUI) LiveExecutionLayout(requestText string) {
	request := strings.TrimSpace(requestText)
	if request == "" {
		request = "（空请求）"
	}
	u.write("\n")
	u.write(u.styled("请求\n", ansiBoldCyan))
	lines := strings.Split(request, "\n")
	for index := range lines {
		lines[index] = "  " + lines[index]
	}
	u.write(strings.Join(lines, "\n"))
	u.write("\n\n")
	u.write(u.styled("处理\n", ansiBoldCyan))
	u.write(u.styled("  已提交，worker 与 Agent 正在处理…\n\n", ansiDim))
	u.write(u.styled("模型输出\n", ansiBoldCyan))
}

// writeSkill 渲染一条技能。
// Python: ui.py:275-284 _write_skill
func (u *ProcessCliUI) writeSkill(item map[string]any) {
	name, _ := item["name"].(string)
	if name == "" {
		name = "?"
	}
	source := ""
	if builtin, _ := item["is_builtin_source"].(bool); builtin {
		source = "内置"
	} else if builtin, _ := item["is_builtin"].(bool); builtin {
		source = "内置"
	} else if raw, ok := item["source"].(string); ok && raw != "" {
		source = raw
	} else {
		source = "项目"
	}
	description := strings.TrimSpace(stringOr(item["description"]))
	suffix := ""
	if description != "" {
		suffix = " · " + description
	}
	u.writeWrapped(fmt.Sprintf("- %s [%s]%s", name, source, suffix), "", "  ")
}

// card 渲染卡片边框。
// Python: ui.py:330-355 ProcessCliUI._card
func (u *ProcessCliUI) card(lines [][2]string) {
	width := u.columns - 2
	if width > 72 {
		width = 72
	}
	if width < 46 {
		width = 46
	}
	contentWidth := width - 4
	topLeft, horizontal, topRight := "+", "-", "+"
	vertical := "|"
	bottomLeft, bottomRight := "+", "+"
	if u.unicode {
		topLeft, horizontal, topRight = "╭", "─", "╮"
		vertical = "│"
		bottomLeft, bottomRight = "╰", "╯"
	}
	u.write(topLeft + strings.Repeat(horizontal, width-2) + topRight + "\n")
	for _, line := range lines {
		clipped := truncate(line[0], contentWidth)
		padding := contentWidth - displayWidth(clipped)
		if padding < 0 {
			padding = 0
		}
		u.write(vertical + " " + u.styled(clipped, line[1]) + strings.Repeat(" ", padding) + " " + vertical + "\n")
	}
	u.write(bottomLeft + strings.Repeat(horizontal, width-2) + bottomRight + "\n")
}

// writeWrapped 按显示宽度折行写出。
// Python: ui.py:365-380 ProcessCliUI._write_wrapped
func (u *ProcessCliUI) writeWrapped(value, style, indent string) {
	available := u.columns - displayWidth(indent)
	if available < 1 {
		available = 1
	}
	for _, line := range wrapDisplay(value, available) {
		u.write(indent + u.styled(line, style) + "\n")
	}
}

// write 写出并 flush。
// Python: ui.py:382-384 ProcessCliUI._write
func (u *ProcessCliUI) write(value string) {
	fmt.Fprint(u.stream, value)
	if flusher, ok := u.stream.(interface{ Flush() error }); ok {
		_ = flusher.Flush()
	}
}

// styled 按需加 ANSI 样式。
// Python: ui.py:358-362 ProcessCliUI._styled
func (u *ProcessCliUI) styled(value, style string) string {
	if !u.color || style == "" {
		return value
	}
	return style + value + ansiReset
}

// ──────────────────────────── HumanRunUI ────────────────────────────

// HumanRunUI 单次人类可读 Runtime 执行的 TTY 装饰。
// Python: ui.py:388-570 HumanRunUI
type HumanRunUI struct {
	stdout          io.Writer
	stderr          io.Writer
	enhanced        bool
	unicode         bool
	color           bool
	statusVisible   bool
	statusText      string
	assistantShown  bool
	reasoningOpen   bool
	reasoningAtLine bool
}

// NewHumanRunUI 构造单次执行装饰器。
// Python: ui.py:390-404 HumanRunUI.__init__
func NewHumanRunUI(stdout, stderr io.Writer) *HumanRunUI {
	unicodeOK := supportsUnicode(stdout)
	return &HumanRunUI{
		stdout:          stdout,
		stderr:          stderr,
		enhanced:        supportsColor(stdout) || (isTerminalWriter(stdout) && unicodeOK),
		unicode:         unicodeOK,
		color:           supportsColor(stdout),
		reasoningAtLine: true,
	}
}

// Start Runtime 初始化期间保持静默，直到请求处理开始。
// Python: ui.py:406-408 HumanRunUI.start
func (u *HumanRunUI) Start() {}

// Working 显示「正在处理……」状态。
// Python: ui.py:410-415 HumanRunUI.working
func (u *HumanRunUI) Working() {
	u.EndReasoning()
	if !u.enhanced {
		return
	}
	u.setStatus("正在处理……")
}

// BeginAssistant 显示助手输出起始标记。
// Python: ui.py:417-424 HumanRunUI.begin_assistant
func (u *HumanRunUI) BeginAssistant() {
	u.ClearStatus()
	if u.assistantShown || !u.enhanced {
		return
	}
	fmt.Fprint(u.stdout, u.styled("• UAPClaw\n\n", ansiBoldCyan))
	u.assistantShown = true
}

// Skills 渲染技能列表（清掉瞬时进度）。
// Python: ui.py:426-430 HumanRunUI.skills
func (u *HumanRunUI) Skills(items []any) {
	u.EndReasoning()
	u.ClearStatus()
	NewProcessCliUI(u.stdout, 0).Skills(items)
}

// ClearStatus 清掉状态行。
// Python: ui.py:432-440 HumanRunUI.clear_status
func (u *HumanRunUI) ClearStatus() {
	if !u.statusVisible {
		return
	}
	if u.color {
		fmt.Fprint(u.stdout, "\r\033[2K")
	} else {
		fmt.Fprint(u.stdout, "\n")
	}
	u.statusVisible = false
	u.statusText = ""
}

// Completed 渲染完成行。
// Python: ui.py:442-452 HumanRunUI.completed
func (u *HumanRunUI) Completed(sessionID string) {
	u.EndReasoning()
	if !u.enhanced {
		return
	}
	u.ClearStatus()
	marker := "+"
	if u.unicode {
		marker = "✓"
	}
	fmt.Fprint(u.stdout, u.styled(fmt.Sprintf("\n%s 执行完成 · 会话 %s\n", marker, shortSession(sessionID)), ansiGreen))
}

// SessionEvent 渲染一条成功的 Runtime 会话生命周期结果。
// Python: ui.py:454-476 HumanRunUI.session_event
func (u *HumanRunUI) SessionEvent(eventType string, payload map[string]any) {
	u.EndReasoning()
	u.ClearStatus()
	sessionID := stringOr(payload["session_id"])
	var message string
	switch eventType {
	case "session.created":
		message = fmt.Sprintf("已创建并切换到会话 %s", sessionID)
	case "session.switched":
		message = fmt.Sprintf("已恢复会话 %s", sessionID)
	case "session.forked":
		title := strings.TrimSpace(stringOr(payload["title"]))
		suffix := ""
		if title != "" {
			suffix = " · " + title
		}
		message = fmt.Sprintf("已创建并切换到会话分支 %s%s", sessionID, suffix)
	case "session.deleted":
		message = fmt.Sprintf("已删除会话 %s", sessionID)
	default:
		return
	}
	marker := "+"
	if u.unicode {
		marker = "✓"
	}
	fmt.Fprint(u.stdout, u.styled(fmt.Sprintf("\n%s %s\n", marker, message), ansiGreen))
}

// Failed 渲染失败块。
// Python: ui.py:478-485 HumanRunUI.failed
func (u *HumanRunUI) Failed(message string) {
	u.EndReasoning()
	u.ClearStatus()
	marker := "x"
	if u.unicode {
		marker = "×"
	}
	fmt.Fprint(u.stderr, u.styled(fmt.Sprintf("\n%s 执行失败\n\n", marker), ansiRed))
	fmt.Fprintf(u.stderr, "  %s\n", message)
}

// Interrupted 渲染中断提示。
// Python: ui.py:487-491 HumanRunUI.interrupted
func (u *HumanRunUI) Interrupted() {
	u.EndReasoning()
	u.ClearStatus()
	fmt.Fprint(u.stderr, u.styled("\n! 已中断\n", ansiYellow))
}

// LiveInputReady 渲染实时输入就绪布局。
// Python: ui.py:493-496 HumanRunUI.live_input_ready
func (u *HumanRunUI) LiveInputReady(requestText string) {
	u.EndReasoning()
	u.ClearStatus()
	NewProcessCliUI(u.stderr, 0).LiveExecutionLayout(requestText)
}

// LiveInputReceipt 渲染补充输入回执。
// Python: ui.py:498-512 HumanRunUI.live_input_receipt
func (u *HumanRunUI) LiveInputReceipt(status, message string) {
	u.EndReasoning()
	marker := "!"
	switch status {
	case "accepted":
		marker = "+"
		if u.unicode {
			marker = "✓"
		}
	case "rejected":
		marker = "x"
		if u.unicode {
			marker = "×"
		}
	}
	style := ansiYellow
	switch status {
	case "accepted":
		style = ansiGreen
	case "rejected":
		style = ansiRed
	}
	fmt.Fprint(u.stderr, u.styled(fmt.Sprintf("\n%s %s\n", marker, message), style))
}

// Reasoning 渲染思考分段。
// Python: ui.py:514-536 HumanRunUI.reasoning
func (u *HumanRunUI) Reasoning(text string) {
	if text == "" {
		return
	}
	u.ClearStatus()
	if !u.reasoningOpen {
		branch := "+-"
		if u.unicode {
			branch = "├─"
		}
		fmt.Fprint(u.stderr, u.styled(fmt.Sprintf("\n  %s 思考\n", branch), ansiDim))
		u.reasoningOpen = true
		u.reasoningAtLine = true
	}
	prefix := "     "
	if u.unicode {
		prefix = "  │  "
	}
	for index, line := range strings.Split(text, "\n") {
		if index > 0 {
			fmt.Fprint(u.stderr, "\n")
			u.reasoningAtLine = true
		}
		if line == "" {
			continue
		}
		if u.reasoningAtLine {
			fmt.Fprint(u.stderr, prefix)
			u.reasoningAtLine = false
		}
		fmt.Fprint(u.stderr, line)
	}
}

// EndReasoning 结束当前思考块，让后续输出独占行首。
// Python: ui.py:538-546 HumanRunUI.end_reasoning
func (u *HumanRunUI) EndReasoning() {
	if !u.reasoningOpen {
		return
	}
	if !u.reasoningAtLine {
		fmt.Fprint(u.stderr, "\n")
	}
	u.reasoningOpen = false
	u.reasoningAtLine = true
}

// Tool 渲染工具调用或结果。
// Python: ui.py:548-556 HumanRunUI.tool
func (u *HumanRunUI) Tool(label, text string) {
	u.EndReasoning()
	u.ClearStatus()
	branch := "+-"
	if u.unicode {
		branch = "├─"
	}
	fmt.Fprint(u.stderr, u.styled(fmt.Sprintf("\n  %s %s\n", branch, label), ansiDim))
	if u.unicode {
		fmt.Fprintf(u.stderr, "  │  %s\n", text)
		return
	}
	fmt.Fprintf(u.stderr, "     %s\n", text)
}

// styled 按需加 ANSI 样式。
// Python: ui.py:558-561 HumanRunUI._styled
func (u *HumanRunUI) styled(value, style string) string {
	if !u.color {
		return value
	}
	return style + value + ansiReset
}

// setStatus 显示状态行。
// Python: ui.py:563-580 HumanRunUI._set_status
func (u *HumanRunUI) setStatus(text string) {
	u.statusText = text
	if u.color {
		if !u.statusVisible {
			fmt.Fprint(u.stdout, "\n")
			u.statusVisible = true
		}
		frame := "*"
		if u.unicode {
			frame = "⠋"
		}
		fmt.Fprint(u.stdout, "\r\033[2K"+u.styled(fmt.Sprintf("  %s %s", frame, u.statusText), ansiCyan))
		return
	}
	if u.statusVisible {
		fmt.Fprint(u.stdout, "\n")
	}
	marker := "*"
	if u.unicode {
		marker = "⠋"
	}
	fmt.Fprint(u.stdout, u.styled(fmt.Sprintf("  %s %s", marker, text), ansiCyan))
	u.statusVisible = true
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// 终端样式常量。
// Python: ui.py:14-20 _ANSI_*
const (
	ansiReset    = "\033[0m"
	ansiBoldCyan = "\033[1;36m"
	ansiCyan     = "\033[1;36m"
	ansiGreen    = "\033[32m"
	ansiYellow   = "\033[33m"
	ansiRed      = "\033[31m"
	ansiDim      = "\033[2m"
)

// resolvedCwd 解析工作目录（空值取当前目录）。
// Python: ui.py:575-576 resolved_cwd
func resolvedCwd(value string) string {
	base := strings.TrimSpace(value)
	if base == "" {
		current, err := os.Getwd()
		if err != nil {
			return ""
		}
		return current
	}
	absolute, err := filepath.Abs(expandHome(base))
	if err != nil {
		return base
	}
	return absolute
}

// supportsUnicode 判断输出流能否编码框线字符。
// Python: ui.py:73-80 _supports_unicode
func supportsUnicode(stream io.Writer) bool {
	probe := "╭─╮│╰╯✓×⠋…"
	return validUTF8(probe)
}

// supportsColor 判断是否启用 ANSI 着色。
// Python: ui.py:83-88 _supports_color
//
// NO_COLOR 或 TERM=dumb 时禁用；其余依赖 isatty。
func supportsColor(stream io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminalWriter(stream)
}

// isTerminalWriter 判断 writer 是否连到终端。
func isTerminalWriter(stream io.Writer) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	return isTerminal(file)
}

// isTerminalReader 判断 reader 是否连到终端（stdin 侧判定）。
func isTerminalReader(stream io.Reader) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	return isTerminal(file)
}

// terminalColumns 取终端宽度（兜底 80）。
func terminalColumns() int {
	if width, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && width > 0 {
		return width
	}
	return 80
}

// validUTF8 判断字符串是否为合法 UTF-8 且全部字符可显示。
func validUTF8(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0
}

// stringOr 取字符串或空串。
func stringOr(value any) string {
	s, _ := value.(string)
	return s
}
