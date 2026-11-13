package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/agent"
	clirails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/storage"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/renderer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/tool_display"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// errExitREPL /exit、/quit 的哨兵错误（Python _ExitREPL）。
var errExitREPL = fmt.Errorf("exit repl")

// skillMDPaths 已注册技能命令：/name → SKILL.md 路径。
// Python: _SKILL_COMMANDS（映射到 Skill 对象；Go 存路径）
var skillMDPaths = map[string]string{}

// askUserRequestAlias AskUserRequest 的局部别名（ask_user 中断的扩展字段）。
type askUserRequestAlias = interrupt.AskUserRequest

// REPL 交互循环。
//
// Python: openjiuwen/harness/cli/ui/repl.py（1316 行）
//
// 全量符号：RunREPL 主循环 + InterruptManager 三层 Ctrl+C + slash 命令
// 处理器（/help /exit /quit /clear /status /cost /compact /sessions，
// /auto-harness 调用点做桩挂 ⏵ 领域十四）+ 交互应答渲染与收集。
//
// 已知偏差 D-prompt：Python 的 PromptSession（prompt_toolkit）提供行编辑、
// 历史浏览与 slash 补全；Go 侧 RunREPL 默认用 bufio 按行读取（TTY 与
// 管道同一路径），PromptFn 可注入测试。补全数据仍由 SortedSkillNames
// 暴露；go-prompt 接线 ⏵ 后续打磨（不影响功能正确性）。

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptManager 三层 Ctrl+C 处理器。
// 窗口期内第 1 次：中止当前流；第 2 次：警告；第 3 次：退出。
// Python: repl.py:49-80 InterruptManager
type InterruptManager struct {
	// count 窗口期内累计触发次数
	count int
	// lastTime 上次触发的单调时钟时刻
	lastTime float64
	// window 去抖窗口（秒），默认 2.0
	window float64
	// now 单调时钟（测试注入；nil 时用 time.Now 粗粒度替代）
	now func() float64
}

// REPL REPL 运行时依赖集合。
// Python: run_repl(backend, cfg, session_store) 的参数与闭包变量。
type REPL struct {
	// Backend agent 执行后端
	Backend agent.AgentBackend
	// Cfg CLI 配置
	Cfg agent.CLIConfig
	// Store 会话持久化
	Store *storage.SessionStore
	// Tracker token 跟踪（/status、/cost 数据源）；可 nil
	Tracker *clirails.TokenTrackingRail
	// In 输入源（默认 os.Stdin；测试注入 strings.Reader）
	In io.Reader
	// Out 输出目标（默认 os.Stdout）
	Out io.Writer
	// PromptFn 行读取函数；nil 时用默认 bufio 实现（偏差 D-prompt）
	PromptFn func(prompt string) (string, error)
	// now 单调时钟（测试注入）
	now func() float64
}

// ──────────────────────────── 常量 ────────────────────────────

// SlashCommands 内置 slash 命令集合。
// Python: repl.py:656-666 SLASH_COMMANDS
var SlashCommands = []string{
	"/help", "/exit", "/quit", "/clear", "/status",
	"/cost", "/compact", "/sessions", "/auto-harness",
}

// SlashDescriptions 人类可读的补全描述。
// /quit 刻意省略（隐藏别名）。
// Python: repl.py:668-678 _SLASH_DESCRIPTIONS
var SlashDescriptions = map[string]string{
	"/help":         "Show available commands",
	"/exit":         "Exit the REPL",
	"/clear":        "Clear the screen",
	"/status":       "Show model and token usage",
	"/cost":         "Show token cost breakdown",
	"/compact":      "Compact conversation history",
	"/sessions":     "List saved sessions",
	"/config":       "Show current configuration",
	"/auto-harness": "Run auto-harness optimization",
}

// skillCommands 启动期动态注册的技能命令：/skill-name → 描述。
// Python: repl.py:680-682 _SKILL_COMMANDS
var skillCommands = map[string]string{}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsSlashCommand 输入是否是已注册的 slash 命令（内置或技能）。
//
// 内置判定用 SlashCommands（含隐藏别名 /quit），而非
// SlashDescriptions——描述表刻意省略 /quit，但它真实可执行。
func IsSlashCommand(input string) bool {
	for _, name := range SlashCommands {
		if name == input {
			return true
		}
	}
	_, skill := skillCommands[input]
	return skill
}

// SkillCommands 返回已注册技能命令（名字 → 描述）的副本。
func SkillCommands() map[string]string {
	out := make(map[string]string, len(skillCommands))
	for name, description := range skillCommands {
		out[name] = description
	}
	return out
}

// RegisterSkillCommand 注册一个技能 slash 命令。
// Python: repl.py:821-846 _register_skill_commands（简化：名字+描述）
func RegisterSkillCommand(name, description string) {
	skillCommands[name] = description
}

// RegisterSkillMDPath 登记技能命令对应的 SKILL.md 路径。
// Python: _SKILL_COMMANDS[cmd] = skill_md
func RegisterSkillMDPath(name, path string) {
	skillMDPaths[name] = path
}

// ResetSkillMDPaths 清空技能路径表（测试隔离用）。
func ResetSkillMDPaths() {
	skillMDPaths = map[string]string{}
}

// ResetSkillCommands 清空技能命令（测试隔离用）。
func ResetSkillCommands() {
	skillCommands = map[string]string{}
}

// AnsweredAsTyped CLI 是否没有该 kind 的应答形态：答案是打字输入。
// Python: repl.py:960-966 _answered_as_typed
//
// 非审批 kind 且不是 ask_user 时答案是自由打字。
func AnsweredAsTyped(kind string) bool {
	return !interruptpkg.IsApprovalKind(kind) && kind != saschema.InterruptKindAskUser
}

// HandleShell 透传执行 shell 命令。
// Python: repl.py:725-751 _handle_shell
//
// 空命令或仅 `!` 时打印提示；退出码非 0 时返回错误。
func HandleShell(cmd string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(cmd, "!"))
	if trimmed == "" {
		return fmt.Errorf("usage: ! <command>")
	}
	command := exec.Command("sh", "-c", trimmed)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// ReadSkillDescription 读 SKILL.md 的第一段非标题正文作描述。
// Python: repl.py:752-777 _read_skill_description
//
// 跳过 frontmatter（--- 包裹）与标题行，取第一个非空段的首行。
func ReadSkillDescription(skillMdPath string) string {
	content, err := os.ReadFile(skillMdPath)
	if err != nil {
		return ""
	}
	inFrontmatter := false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return ""
}

// ScanSkillDirs 扫描 skill 目录并注册 slash 命令。
// Python: repl.py:778-820 _scan_skill_dirs + :876-886 _scan_skills
//
// 每个目录的子目录若含 SKILL.md，注册为 /<子目录名>；返回注册数。
func ScanSkillDirs(dirs []string) int {
	count := 0
	home, homeErr := os.UserHomeDir()
	for _, dir := range dirs {
		expanded := dir
		if strings.HasPrefix(dir, "~") && homeErr == nil {
			if dir == "~" {
				expanded = home
			} else if strings.HasPrefix(dir, "~/") {
				expanded = filepath.Join(home, dir[2:])
			}
		}
		entries, err := os.ReadDir(expanded)
		if err != nil {
			continue // 不存在的目录静默跳过
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillMd := filepath.Join(expanded, entry.Name(), "SKILL.md")
			if _, statErr := os.Stat(skillMd); statErr != nil {
				continue
			}
			description := ReadSkillDescription(skillMd)
			RegisterSkillCommand("/"+entry.Name(), description)
			skillMDPaths["/"+entry.Name()] = skillMd
			count++
		}
	}
	return count
}

// SortedSkillNames 补全用：排序后的全部 slash 命令名（内置 + 技能）。
func SortedSkillNames() []string {
	names := make([]string, 0, len(SlashDescriptions)+len(skillCommands))
	for name := range SlashDescriptions {
		names = append(names, name)
	}
	for name := range skillCommands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Handle 中断计数并返回动作："abort" / "warn" / "exit"。
// Python: repl.py:62-80 handle
func (m *InterruptManager) Handle(backend agent.AgentBackend) string {
	now := float64(time.Now().UnixNano()) / 1e9
	if m.now != nil {
		now = m.now()
	}
	window := m.window
	if window == 0 {
		window = 2.0
	}
	if now-m.lastTime > window {
		m.count = 0
	}
	m.count++
	m.lastTime = now

	if m.count == 1 {
		// Python: asyncio.ensure_future(backend.abort())——异步触发
		go func() { _ = backend.Abort(context.Background()) }()
		return "abort"
	} else if m.count == 2 {
		return "warn"
	}
	return "exit"
}

// RunREPL 运行交互式 REPL 主循环。
// Python: repl.py:1198-1316 run_repl
//
// 循环：读行 → 空/继续 → slash 分派（含技能命令展开）→ shell 透传 →
// 普通查询（流渲染 + 中断恢复循环）。EOF 或 /exit 退出。
func (r *REPL) RunREPL(ctx context.Context) error {
	out := r.Out
	if out == nil {
		out = os.Stdout
	}
	if r.In == nil {
		r.In = os.Stdin
	}
	r.now = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }

	// 历史文件路径（Python: ~/.openjiuwen_history；Go 行读取不落盘，偏差 D-prompt）

	interruptMgr := &InterruptManager{}

	// Ctrl+C 三层中断：SIGINT → InterruptManager.Handle（repl.py:1306-1314）
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	exited := false
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-sigCh:
				switch interruptMgr.Handle(r.Backend) {
				case "abort":
					_, _ = fmt.Fprintln(out, "\n\x1b[2m⏹ Interrupted\x1b[0m")
				case "warn":
					_, _ = fmt.Fprintln(out, "\n\x1b[2mPress Ctrl+C once more to exit.\x1b[0m")
				default:
					exited = true
					return
				}
			case <-done:
				return
			}
		}
	}()

	// 扫描默认技能目录并注册 slash 命令（repl.py:1222）
	ScanSkillDirs(DefaultSkillDirsForREPL())

	printWelcome(out, r.Cfg)

	reader := bufio.NewReader(r.In)
	promptFn := r.PromptFn
	if promptFn == nil {
		promptFn = func(prompt string) (string, error) {
			_, _ = fmt.Fprint(out, prompt)
			line, err := reader.ReadString('\n')
			if err != nil && line == "" {
				return "", err
			}
			return strings.TrimRight(line, "\r\n"), nil
		}
	}

	for {
		if exited {
			_, _ = fmt.Fprintln(out, "\x1b[2mGoodbye!\x1b[0m")
			return nil
		}
		userInput, err := promptFn("You> ")
		if err != nil {
			// EOF / 读错误 → 退出（Python: except (EOFError, KeyboardInterrupt)）
			break
		}

		text := strings.TrimSpace(userInput)
		if text == "" {
			continue
		}

		// ---- Slash 命令 ----
		if strings.HasPrefix(text, "/") {
			skillCmd, slashErr := r.handleSlash(ctx, text, out)
			if slashErr != nil {
				if slashErr == errExitREPL {
					_, _ = fmt.Fprintln(out, "\x1b[2mGoodbye!\x1b[0m")
					return nil
				}
				_, _ = fmt.Fprintf(out, "\x1b[31m✗ Error: %v\x1b[0m\n", slashErr)
				continue
			}
			if skillCmd == "" {
				continue
			}
			// 技能命令——构建结构化 query 并转发（repl.py:1255-1261）
			parts := strings.SplitN(text, " ", 2)
			skillArgs := ""
			if len(parts) > 1 {
				skillArgs = parts[1]
			}
			if skillMD, ok := skillMDPaths[skillCmd]; ok {
				text = buildSkillQuery(skillMD, skillArgs)
			} else {
				continue
			}
		}

		// ---- Shell 透传 ----
		if strings.HasPrefix(text, "!") {
			shellCmd := strings.TrimSpace(text[1:])
			if shellCmd != "" {
				_ = HandleShell(shellCmd)
			}
			continue
		}

		// ---- 普通查询 ----
		r.Store.AddMessage("user", text)
		chunks, streamErr := r.collect(ctx, text)
		if streamErr != nil {
			_, _ = fmt.Fprintf(out, "\x1b[31m✗ Error: %v\x1b[0m\n", streamErr)
			continue
		}

		result := renderer.RenderStream(out, chunks, renderer.Options{
			ShowReasoning: r.Cfg.Verbose,
			OnInteraction: func(_ string, question any) string {
				renderInteraction(question, out)
				return ""
			},
		})

		// 中断恢复循环（repl.py:1288-1303）
		for len(result.PendingInteractions) > 0 {
			interactiveInput := r.collectInteractionAnswers(result.PendingInteractions, promptFn)
			if interactiveInput == nil {
				break
			}
			resumeChunks, resumeErr := r.collect(ctx, interactiveInput)
			if resumeErr != nil {
				_, _ = fmt.Fprintf(out, "\x1b[31m✗ Error: %v\x1b[0m\n", resumeErr)
				break
			}
			result = renderer.RenderStream(out, resumeChunks, renderer.Options{
				ShowReasoning: r.Cfg.Verbose,
				OnInteraction: func(_ string, question any) string {
					renderInteraction(question, out)
					return ""
				},
			})
		}

		r.Store.AddMessage("assistant", result.Text)
	}
	_, _ = fmt.Fprintln(out, "\x1b[2mGoodbye!\x1b[0m")
	return nil
}

// handleSlash 分派一个 slash 命令。
// Python: repl.py:887-932 _handle_slash
//
// 返回技能命令名（调用方需构建 query 转发）；内置命令返回空串。
// /exit、/quit 返回 errExitREPL。
func (r *REPL) handleSlash(ctx context.Context, text string, out io.Writer) (string, error) {
	fields := strings.SplitN(text, " ", 2)
	cmdName := strings.ToLower(fields[0])
	rest := ""
	if len(fields) > 1 {
		rest = fields[1]
	}

	// 已知命令？
	if !IsSlashCommand(cmdName) {
		_, _ = fmt.Fprintf(out, "\x1b[31mUnknown command: %s\x1b[0m\n", cmdName)
		_, _ = fmt.Fprintln(out, "Type /help to see available commands.")
		return "", nil
	}

	switch cmdName {
	case "/help":
		cmdHelp(out)
	case "/exit", "/quit":
		return "", errExitREPL
	case "/clear":
		cmdClear(out)
	case "/status":
		cmdStatus(out, r.Tracker, r.Cfg)
	case "/cost":
		cmdCost(out, r.Tracker)
	case "/compact":
		cmdCompact(out)
	case "/sessions":
		cmdSessions(out, r.Store)
	case "/auto-harness":
		cmdAutoHarness(out, rest, r.Cfg)
	default:
		// 技能命令（Python: handler is None 分支）
		if _, ok := skillMDPaths[cmdName]; ok {
			return cmdName, nil
		}
	}
	_ = ctx
	return "", nil
}

// collect 后端流式执行并把帧收集成切片（渲染器吃切片）。
func (r *REPL) collect(ctx context.Context, query any) ([]stream.OutputSchema, error) {
	ch, err := r.Backend.RunStreaming(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	var chunks []stream.OutputSchema
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// collectInteractionAnswers 收集全部待答交互并组装 InteractiveInput。
// Python: repl.py:1083-1122 _collect_interaction_answers
//
// ToolCallInterruptRequest 按 interrupt_id 作答；workflow 交互按自身 id
// 以文本作答。无待答交互返回 nil。
func (r *REPL) collectInteractionAnswers(pending []renderer.PendingInteraction, promptFn func(string) (string, error)) *interaction.InteractiveInput {
	if len(pending) == 0 {
		return nil
	}
	interactiveInput := &interaction.InteractiveInput{UserInputs: map[string]any{}}
	for _, item := range pending {
		// renderer 把 __interaction__ 的 payload 整体挂到 Request；真正
		// 的中断请求对象在 payload["payload"]（对齐 renderer.py 提取序）。
		request := item.Request
		if m, ok := request.(map[string]any); ok {
			if inner, hasInner := m["payload"]; hasInner {
				request = inner
			}
		}
		if tcReq, ok := request.(*saschema.ToolCallInterruptRequest); ok {
			answer, err := r.answerForInteraction(tcReq, promptFn)
			if err != nil {
				return nil
			}
			if interactiveInput.InterruptAnswers == nil {
				interactiveInput.InterruptAnswers = map[string]any{}
			}
			interactiveInput.InterruptAnswers[interruptIDOf(tcReq)] = answer
		} else {
			answer, promptErr := promptFn("Answer> ")
			if promptErr != nil {
				return nil
			}
			_ = interactiveInput.Update(item.InteractionID, strings.TrimSpace(answer))
		}
	}
	return interactiveInput
}

// answerForInteraction 对一个中断按其 kind 形态提示用户作答。
// Python: repl.py:1069-1081 _answer_for_interaction
func (r *REPL) answerForInteraction(tcReq *saschema.ToolCallInterruptRequest, promptFn func(string) (string, error)) (any, error) {
	kind := kindOf(tcReq)
	if kind == saschema.InterruptKindAskUser {
		return r.answerQuestions(tcReq, promptFn)
	}
	if AnsweredAsTyped(kind) {
		return r.answerFreeForm(promptFn)
	}
	// 回车视为同意（对齐 CLI 历史行为）
	answer, err := promptFn("Approve? (y/a/n)> ")
	if err != nil {
		return nil, err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = "y"
	}
	return interruptpkg.InterruptAnswerFor(kind, interruptpkg.InterruptAnswerOptions{Text: &answer})
}

// answerQuestions ask_user 中断作答：每问题一问，或一个自由答案。
// Python: repl.py:1044-1055 _answer_questions
func (r *REPL) answerQuestions(tcReq *saschema.ToolCallInterruptRequest, promptFn func(string) (string, error)) (any, error) {
	questions := askQuestions(tcReq)
	if len(questions) == 0 {
		answer, err := promptFn("Answer> ")
		if err != nil {
			return nil, err
		}
		return map[string]any{"answer": strings.TrimSpace(answer)}, nil
	}
	answers := map[string]any{}
	for _, q := range questions {
		questionText, _ := q["question"].(string)
		answer, err := promptFn("Answer for '" + questionText + "'> ")
		if err != nil {
			return nil, err
		}
		answers[questionText] = strings.TrimSpace(answer)
	}
	return map[string]any{"answers": answers}, nil
}

// answerFreeForm CLI 不认识的 kind：能解析成 JSON 就用 JSON，否则原文。
// Python: repl.py:1058-1066 _answer_free_form
func (r *REPL) answerFreeForm(promptFn func(string) (string, error)) (any, error) {
	answer, err := promptFn("Answer> ")
	if err != nil {
		return nil, err
	}
	answer = strings.TrimSpace(answer)
	var parsed any
	if json.Unmarshal([]byte(answer), &parsed) == nil {
		return parsed, nil
	}
	return answer, nil
}

// ──────────────────────────── 非导出函数（slash 处理器） ────────────────────────────

// cmdHelp 显示可用命令。
// Python: repl.py:88-116 _cmd_help
func cmdHelp(out io.Writer) {
	rows := [][2]string{
		{"/help", "Show this help message"},
		{"/exit", "Exit OpenJiuWen"},
		{"/clear", "Clear screen"},
		{"/status", "Show token usage and model info"},
		{"/cost", "Show token cost summary"},
		{"/compact", "Compact conversation history"},
		{"/sessions", "List saved sessions"},
		{"/auto-harness", "Run auto-harness optimization"},
		{"! <cmd>", "Execute a shell command"},
	}
	for _, row := range rows {
		_, _ = fmt.Fprintf(out, "\x1b[1;36m%s\x1b[0m  %s\n", row[0], row[1])
	}
	skills := SkillCommands()
	if len(skills) > 0 {
		_, _ = fmt.Fprintln(out, "\n\x1b[1mSkills\x1b[0m \x1b[2m(invoke with /<skill-name>)\x1b[0m")
		names := make([]string, 0, len(skills))
		for name := range skills {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			_, _ = fmt.Fprintf(out, "\x1b[1;36m%s\x1b[0m  %s\n", name, skills[name])
		}
	}
}

// cmdClear 清屏（ANSI 全清屏 + 光标归位）。
// Python: repl.py:123-125 _cmd_clear
func cmdClear(out io.Writer) {
	_, _ = fmt.Fprint(out, "\x1b[2J\x1b[H")
}

// cmdStatus 显示模型与 token 用量。
// Python: repl.py:128-142 _cmd_status
func cmdStatus(out io.Writer, tracker *clirails.TokenTrackingRail, cfg agent.CLIConfig) {
	if cfg.Model != "" {
		_, _ = fmt.Fprintf(out, "\x1b[1mModel:\x1b[0m %s (%s)\n", cfg.Model, cfg.Provider)
	}
	if tracker != nil {
		s := tracker.GetSummary()
		_, _ = fmt.Fprintf(out, "\x1b[1mInput tokens:\x1b[0m  %s\n", formatInt(s["input_tokens"]))
		_, _ = fmt.Fprintf(out, "\x1b[1mOutput tokens:\x1b[0m %s\n", formatInt(s["output_tokens"]))
		_, _ = fmt.Fprintf(out, "\x1b[1mTotal tokens:\x1b[0m  %s\n", formatInt(s["total_tokens"]))
		_, _ = fmt.Fprintf(out, "\x1b[1mModel calls:\x1b[0m   %s\n", formatInt(s["model_calls"]))
	}
}

// cmdCost 显示 token 消耗汇总。
// Python: repl.py:145-159 _cmd_cost
func cmdCost(out io.Writer, tracker *clirails.TokenTrackingRail) {
	if tracker == nil {
		_, _ = fmt.Fprintln(out, "\x1b[2mNo token data available.\x1b[0m")
		return
	}
	s := tracker.GetSummary()
	_, _ = fmt.Fprintln(out, "\x1b[1mToken usage:\x1b[0m")
	_, _ = fmt.Fprintf(out, "  Input:  %s tokens\n", formatInt(s["input_tokens"]))
	_, _ = fmt.Fprintf(out, "  Output: %s tokens\n", formatInt(s["output_tokens"]))
	_, _ = fmt.Fprintf(out, "  Total:  %s tokens\n", formatInt(s["total_tokens"]))
	_, _ = fmt.Fprintf(out, "  Calls:  %s\n", formatInt(s["model_calls"]))
}

// cmdCompact 压缩会话（MVP：提示信息）。
// Python: repl.py:162-164 _cmd_compact
func cmdCompact(out io.Writer) {
	_, _ = fmt.Fprintln(out, "\x1b[2mConversation compaction is not yet implemented in MVP.\x1b[0m")
}

// cmdSessions 列出已保存会话。
// Python: repl.py:624-649 _cmd_sessions
func cmdSessions(out io.Writer, store *storage.SessionStore) {
	if store == nil {
		_, _ = fmt.Fprintln(out, "\x1b[2mNo session store.\x1b[0m")
		return
	}
	sessions := store.ListSessions()
	if len(sessions) == 0 {
		_, _ = fmt.Fprintln(out, "\x1b[2mNo saved sessions.\x1b[0m")
		return
	}
	_, _ = fmt.Fprintln(out, "Sessions")
	_, _ = fmt.Fprintln(out, "ID                     Model              Created              Turns")
	for _, s := range sessions {
		created := ""
		if v, ok := s["created_at"].(string); ok && len(v) >= 19 {
			created = v[:19]
		}
		_, _ = fmt.Fprintf(out, "%-22s %-18s %-20s %s\n",
			toString(s["id"]), toString(s["model"]), created, toString(s["turns"]))
	}
}

// cmdAutoHarness /auto-harness 分派。
// Python: repl.py:208-261 _cmd_auto_harness
//
// 待回填④：run / experience / gap-analyze / history 四个子命令依赖
// dev_tools/auto_harness（领域十四，未对齐），调用点做桩，标 ⏵ 领域十四。
func cmdAutoHarness(out io.Writer, text string, cfg agent.CLIConfig) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	subcmd := ""
	if len(parts) > 0 {
		subcmd = parts[0]
	}
	switch subcmd {
	case "run", "experience", "gap-analyze", "history":
		_, _ = fmt.Fprintln(out, "\x1b[33m/auto-harness 依赖 dev_tools/auto_harness（领域十四，尚未对齐）。\x1b[0m")
	default:
		autoHarnessHelp(out)
	}
	_ = cfg
}

// autoHarnessHelp 打印 /auto-harness 子命令用法。
// Python: repl.py:167-205 _auto_harness_help
func autoHarnessHelp(out io.Writer) {
	rows := [][2]string{
		{"/auto-harness run [--task TOPIC] [--goal TEXT] [--pipeline meta|extended|auto] [--dry-run] [--no-push] [--budget N]", "执行优化周期"},
		{"/auto-harness <自然语言目标>", "直接把自然语言作为本轮优化目标并执行全流程"},
		{"/auto-harness experience search <query>", "搜索经验库"},
		{"/auto-harness experience list [--type TYPE] [--limit N]", "列出经验库记录"},
		{"/auto-harness gap-analyze", "差距分析"},
		{"/auto-harness history [--limit N]", "查看优化历史"},
	}
	_, _ = fmt.Fprintln(out, "\x1b[1mUsage:\x1b[0m")
	for _, row := range rows {
		_, _ = fmt.Fprintf(out, "\x1b[1;36m%s\x1b[0m  %s\n", row[0], row[1])
	}
}

// printWelcome 显示 REPL 欢迎横幅（Claude Code 风格，纯文本降级）。
// Python: repl.py:1130-1190 _print_welcome
func printWelcome(out io.Writer, cfg agent.CLIConfig) {
	_, _ = fmt.Fprint(out, "\x1b[36m╔═══╗\x1b[0m  Welcome to OpenJiuWen\n")
	_, _ = fmt.Fprint(out, "\x1b[36m╚═╦═╝\x1b[0m\n")
	_, _ = fmt.Fprintf(out, "  %s (%s)\n", cfg.Model, cfg.Provider)
	_, _ = fmt.Fprintf(out, "  %s\n\n", cfg.Cwd)
	_, _ = fmt.Fprintln(out, "Tips for getting started")
	_, _ = fmt.Fprintln(out, "Create an OPENJIUWEN.md for project rules")
	_, _ = fmt.Fprintln(out, strings.Repeat("─", 40))
	_, _ = fmt.Fprintln(out, "Commands")
	_, _ = fmt.Fprintln(out, "  /help      Show available commands")
	_, _ = fmt.Fprintln(out, "  /status    Token usage & model info")
	_, _ = fmt.Fprintln(out, "  /exit      Exit OpenJiuWen")
	_, _ = fmt.Fprintln(out, "  ! <cmd>    Run a shell command")
	_, _ = fmt.Fprintln(out)
}

// buildSkillQuery 技能调用用的结构化 query。
// Python: repl.py:848-874 _build_skill_query
func buildSkillQuery(skillMdPath, args string) string {
	content, err := os.ReadFile(skillMdPath)
	if err != nil {
		return "Error reading skill file: " + skillMdPath + ". Please check the skill directory."
	}
	parts := []string{"<skill-instructions>", string(content), "</skill-instructions>"}
	if args != "" {
		parts = append(parts, "\nUser request: "+args)
	} else {
		parts = append(parts, "\nPlease follow the skill instructions above.")
	}
	return strings.Join(parts, "\n")
}

// kindOf 提取 ToolCallInterruptRequest 的 interrupt kind。
// Python: interrupt_kind_of(request)
func kindOf(tcReq *saschema.ToolCallInterruptRequest) string {
	// InterruptRequester 提供 GetInterruptKind；空串回落未声明
	kind := tcReq.Request.GetInterruptKind()
	if kind != "" {
		return kind
	}
	return saschema.InterruptKindUndeclared
}

// askQuestions 提取 ask_user 中断的问题列表。
func askQuestions(tcReq *saschema.ToolCallInterruptRequest) []map[string]any {
	if askReq, ok := tcReq.Request.(*askUserRequestAlias); ok {
		return askReq.Questions
	}
	return nil
}

// interruptIDOf 提取中断 ID（issue 后才有值）。
func interruptIDOf(tcReq *saschema.ToolCallInterruptRequest) string {
	switch req := tcReq.Request.(type) {
	case *saschema.InterruptRequest:
		return req.InterruptID
	case *askUserRequestAlias:
		return req.InterruptRequest.InterruptID
	default:
		return ""
	}
}

// printArgs 打印工具参数（JSON 对象串或 map），一行一个。
// Python: repl.py:940-957 _print_args
func printArgs(args any, out io.Writer) {
	if args == nil {
		return
	}
	var m map[string]any
	switch value := args.(type) {
	case string:
		if json.Unmarshal([]byte(value), &m) != nil {
			_, _ = fmt.Fprintf(out, "\x1b[2m  args: %s\x1b[0m\n", value)
			return
		}
	case map[string]any:
		m = value
	default:
		return
	}
	for k, v := range m {
		val := fmt.Sprint(v)
		if len(val) > 200 {
			val = val[:200] + "..."
		}
		_, _ = fmt.Fprintf(out, "\x1b[2m  %s: %s\x1b[0m\n", k, val)
	}
}

// renderInteraction 把交互请求渲染到终端。
// Python: repl.py:967-1041 _render_interaction
func renderInteraction(request any, out io.Writer) {
	tcReq, isToolCall := request.(*saschema.ToolCallInterruptRequest)
	if !isToolCall {
		_, _ = fmt.Fprintln(out, "\n\x1b[1mAgent needs your input:\x1b[0m")
		if s, ok := request.(string); ok {
			_, _ = fmt.Fprintln(out, s)
		} else {
			raw, _ := json.Marshal(request)
			_, _ = fmt.Fprintln(out, string(raw))
		}
		return
	}

	kind := kindOf(tcReq)

	if kind == saschema.InterruptKindAskUser {
		_, _ = fmt.Fprintln(out, "\n\x1b[1mAgent needs your input:\x1b[0m")
		for i, q := range askQuestions(tcReq) {
			header := fmt.Sprintf("Q%d", i+1)
			if h, ok := q["header"].(string); ok && h != "" {
				header = h
			}
			questionText, _ := q["question"].(string)
			_, _ = fmt.Fprintf(out, "\n\x1b[36m%s:\x1b[0m %s\n", header, questionText)
			if options, ok := q["options"].([]any); ok {
				for j, opt := range options {
					optMap, ok := opt.(map[string]any)
					if !ok {
						continue
					}
					label, _ := optMap["label"].(string)
					desc, _ := optMap["description"].(string)
					_, _ = fmt.Fprintf(out, "  \x1b[2m%d.\x1b[0m %s - %s\n", j+1, label, desc)
				}
			}
		}
		return
	}

	if kind == saschema.InterruptKindSkillEvolutionApproval {
		_, _ = fmt.Fprintln(out, "\n\x1b[1;33m⚠ Skill evolution approval\x1b[0m")
		if tcReq.Request.GetMessage() != "" {
			_, _ = fmt.Fprintln(out, tcReq.Request.GetMessage())
		}
		_, _ = fmt.Fprintln(out, "\x1b[2m  (y/yes=allow once, a/always=allow for this session, n/no=reject, or type feedback)\x1b[0m")
		return
	}

	if AnsweredAsTyped(kind) {
		_, _ = fmt.Fprintf(out, "\n\x1b[1;33m⚠ %s: the agent is waiting for an answer (%s)\x1b[0m\n", tcReq.ToolName, kind)
		if tcReq.Request.GetMessage() != "" {
			_, _ = fmt.Fprintln(out, tcReq.Request.GetMessage())
		}
		_, _ = fmt.Fprintln(out, "\x1b[2m  (type the answer as JSON, or as text)\x1b[0m")
		return
	}

	displayName := tool_display.GetDisplayName(tcReq.ToolName)
	_, _ = fmt.Fprintf(out, "\n\x1b[1;33m⚠ Approve %s?\x1b[0m\n", displayName)
	printArgs(tcReq.ToolArgs, out)
	_, _ = fmt.Fprintln(out, "\x1b[2m  (y/yes=approve, a/always=approve for this session, n/no=reject, or type feedback)\x1b[0m")
}

// formatInt 汇总字段转千分位串。
func formatInt(v any) string {
	switch value := v.(type) {
	case int:
		return strconv.FormatInt(int64(value), 10)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

// toString map 字段安全转串。
func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// DefaultSkillDirsForREPL REPL 启动期扫描的默认 skill 目录。
// Python: repl.py:744-749 _DEFAULT_SKILL_DIRS
func DefaultSkillDirsForREPL() []string {
	return []string{
		"~/.openjiuwen/workspace/skills",
		"~/.claude/skills",
		"~/.codex/skills",
		"~/.jiuwenclaw/workspace/skills",
	}
}
