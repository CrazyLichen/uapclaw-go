package externalcli

// 第三方 CLI 成员逐家启动知识：对齐 Python
// openjiuwen/agent_teams/external/cli_agent/adapters.py（552 行全文一比一）。
//
// 每家第三方 CLI 在 spawn 路径必须知道的三件事上有差异：
//  1. 启动命令——二进制 + 权限绕过 flag + 流模式；
//  2. 输入框架——一轮的文本如何写入 CLI stdin；
//  3. 轮次完成——如何从 stdout 判断本轮结束。
//
// CliAgentAdapter 把这些捕获为数据，调优不需要改代码。各家置信度不同，
// 这里不针对真实二进制做校验——每个内置 adapter 的注释说明假设了什么、
// 真实环境中应验证什么。
//
// 只有持续读取 stdin 的 CLI（流式输入/交互模式）支持中途注入；one-shot CLI
// （prompt 经 argv flag 传入）置 SupportsStdinInjection=false，需要
// 每轮重起 runtime（ReinvokeCliRuntime）。

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"

	external "github.com/uapclaw/uapclaw-go/internal/agent_teams/external"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CliAgentAdapter 如何启动并与一个第三方 CLI agent 对话。
//
// Python: CliAgentAdapter (adapters.py:78)
type CliAgentAdapter struct {
	// Name adapter 键（"codex-exec"/"gemini"/...）
	Name string
	// Command 完整启动 argv（二进制 + flag）
	Command []string
	// InputFormat 输入框架策略（见 Input*）
	InputFormat string
	// Completion 轮次完成策略（见 Completion*）
	Completion string
	// StructuredOutput stdout 是否为结构化 JSON 事件流（codex/gemini）。
	// true 时 SummarizeOutputLine 从每个事件提取 assistant 文本/工具描述用于
	// 可观测性；false 时每行原文即叙述。
	StructuredOutput bool
	// SupportsStdinInjection 中途 stdin 写入是否被观察。true 的 CLI 跑在
	// 持续 stdin 流式 runtime 下；false（one-shot）CLI 跑在每轮重起 runtime 下
	// （每轮新进程、prompt 走 argv、无中途 steer）。
	SupportsStdinInjection bool
	// PromptFlag 仅 one-shot。经 <prompt_flag> <prompt> 传每轮 prompt
	// （如 openclaw --message）；空把 prompt 作为末位位置参数追加。
	PromptFlag string
	// SessionFlag 仅 one-shot。追加 <session_flag> <session_id> 以启动/标识
	// 成员会话（如 openclaw/gemini --session-id）。每轮都带，除非同时设置了
	// ResumeFlag——那样本 flag 只在首轮使用。
	SessionFlag string
	// ResumeFlag 仅 one-shot。设置时，首轮之后的轮次经 <resume_flag> <session_id>
	// 恢复同一会话而不是 SessionFlag（如 gemini 首轮 --session-id、后续 --resume）。
	ResumeFlag string
	// ContinueArgs 仅 one-shot。首轮之后追加的参数，无 session id 的跨轮连续性
	// （如 hermes ("--continue",)）。
	ContinueArgs []string
	// MCPInject 本 CLI 注册 stdio MCP server 的方式（见 MCPInject*）。
	// MCPInjectNone（默认）表示 spawn 路径无法自动注入团队工具。
	MCPInject string
	// SystemPromptInject 本 CLI 传成员 system prompt（其团队 rail 分段）的方式
	// （见 SystemPrompt*）。SystemPromptNone（默认）表示该 CLI 没有 system-prompt
	// flag；spawn 路径改为把 prompt 拼进成员首条用户消息。
	SystemPromptInject string
	// EnvStripPrefixes 从 spawn 的 CLI 环境中移除的环境变量名前缀。用于剥掉
	// 父 agent 会话的标记，让子进程作为全新独立实例运行而不是嵌套实例。
	// 空（默认）保留完整继承环境。
	EnvStripPrefixes []string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InputText 输入框架策略：原文文本。
	// Python: INPUT_TEXT
	InputText = "text"
	// InputCodexProto Codex proto：每轮在 stdin 写一条 JSONL "submission"。
	// Python: INPUT_CODEX_PROTO
	InputCodexProto = "codex_proto"

	// MCPInjectNone 无法自动注入团队 MCP 工具。
	// Python: MCP_INJECT_NONE
	MCPInjectNone = "none"
	// MCPInjectCodexOverride Codex：经重复 -c mcp_servers.<name>.<key>=<value> 设置配置键。
	// Python: MCP_INJECT_CODEX_OVERRIDE
	MCPInjectCodexOverride = "codex_override"
	// MCPInjectGeminiAdd Gemini CLI：带外注册 gemini mcp add <name> <bin> <args>。
	// 无启动 flag，MCPLaunchArgs 返回空，spawn 路径在成员首轮前执行一次 MCPRegisterCommand。
	// Python: MCP_INJECT_GEMINI_ADD
	MCPInjectGeminiAdd = "gemini_add"
	// MCPInjectHermesAdd Hermes Agent：带外注册 hermes mcp add <name> --command <bin>。
	// 与 gemini 同形状；MCPLaunchArgs 返回空。
	// Python: MCP_INJECT_HERMES_ADD
	MCPInjectHermesAdd = "hermes_add"

	// SystemPromptNone 无 system-prompt flag；spawn 路径把 prompt 拼进首条用户消息。
	// Python: SYSTEM_PROMPT_NONE
	SystemPromptNone = "none"
	// SystemPromptCodexDeveloper Codex exec 兼容后端：经 -c developer_instructions=<json>
	// 以 developer-message 层注入（增量——保留 codex 自身基础指令）。
	// 经 codex exec --strict-config 验证可接受。
	// Python: SYSTEM_PROMPT_CODEX_DEVELOPER
	SystemPromptCodexDeveloper = "codex_developer"

	// CompletionNone 无轮次完成判定。
	// Python: COMPLETION_NONE
	CompletionNone = "none"
	// CompletionCodexTaskComplete Codex proto（legacy）：msg.type == "task_complete" 的事件行。
	// Python: COMPLETION_CODEX_TASK_COMPLETE
	CompletionCodexTaskComplete = "codex_task_complete"
	// CompletionCodexJSON Codex exec --json：type == "turn.completed" 的 JSONL 事件行。
	// Python: COMPLETION_CODEX_JSON
	CompletionCodexJSON = "codex_json"
	// CompletionMarkerPrefix 子串标记：行包含前缀之后的文本。
	// Python: COMPLETION_MARKER_PREFIX
	CompletionMarkerPrefix = "marker:"

	// codexMCPStartupTimeoutS 并发外部成员会让 Python MCP server 冷启动超出
	// Codex 默认十秒启动超时。
	// Python: _CODEX_MCP_STARTUP_TIMEOUT_S
	codexMCPStartupTimeoutS = 120

	// summaryLimit surfaced 输出摘要的最大长度；超长叙述被截断。
	// Python: _SUMMARY_LIMIT
	summaryLimit = 500
)

// lifecycleEventTypes 结构化输出中不携带可呈现叙述的事件类型（codex/gemini
// JSON 模式的 session/turn 生命周期标记）。
// Python: _LIFECYCLE_EVENT_TYPES
var lifecycleEventTypes = map[string]struct{}{
	"result":         {},
	"turn.started":   {},
	"turn.completed": {},
	"thread.started": {},
	"session_init":   {},
	"system":         {},
	"task_started":   {},
	"task_complete":  {},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// builtinAdapters 内置 adapter。启动 flag 在已知处沿用 ClawTeam 的
// NativeCliAdapter 约定。未对真实二进制校验——按 CLI 版本逐一验证 command、
// 输入框架与完成判定。
// Python: _BUILTIN (adapters.py:393)
var builtinAdapters = map[string]*CliAgentAdapter{
	// Codex CLI 兼容后端——codex exec --json 非交互跑一个 prompt，流出 JSONL
	// 事件并退出（one-shot，跑在重起 runtime 下）。prompt 是末位位置参数；
	// 末条轮次事件是 {"type": "turn.completed"}，进程退出/stdout EOF 也结束轮次。
	// --dangerously-bypass-approvals-and-sandbox 无审批提示与 sandbox 运行；
	// --skip-git-repo-check 必须带上，因为团队工作空间不是 git 仓库（codex exec
	// 否则拒绝运行）。MCP server 经 -c mcp_servers.<name>...（codex_override）注册，
	// 成员 system prompt 经 -c developer_instructions=<json>（codex_developer），
	// 每次重起都施加。
	// 主 codex 后端是 cli_agent/codex 的长驻 Python SDK runtime。保留显式的
	// codex-exec 名作为旧版 Codex CLI 安装的 one-shot 回退。
	"codex-exec": {
		Name:                   "codex-exec",
		Command:                []string{"codex", "exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"},
		InputFormat:            InputText,
		Completion:             CompletionCodexJSON,
		StructuredOutput:       true,
		SupportsStdinInjection: false,
		MCPInject:              MCPInjectCodexOverride,
		SystemPromptInject:     SystemPromptCodexDeveloper,
	},
	// Gemini CLI——-p 非交互跑一个 prompt 并退出（one-shot，重起 runtime）；
	// prompt 经 -p 传入，进程退出/stdout EOF 结束轮次。-o stream-json 选择结构化
	// headless 输出，-y（yolo）自动批准工具使用。MCP 带外注册 gemini mcp add
	// （无启动 flag），spawn 路径执行一次。跨轮连续性（同 clowder --resume <id>）：
	// 首轮以客户端选定的 id 经 --session-id 启动会话，后续轮经 --resume 恢复。
	// VERIFY：对安装的 gemini 版本验证 flag（尤其 --resume 接受 --session-id 的 id）。
	"gemini": {
		Name:                   "gemini",
		Command:                []string{"gemini", "-o", "stream-json", "-y"},
		InputFormat:            InputText,
		Completion:             CompletionNone,
		StructuredOutput:       true,
		SupportsStdinInjection: false,
		PromptFlag:             "-p",
		SessionFlag:            "--session-id",
		ResumeFlag:             "--resume",
		MCPInject:              MCPInjectGeminiAdd,
		SystemPromptInject:     SystemPromptNone,
	},
	// OpenClaw CLI——one-shot（重起 runtime）。ClawTeam 以
	// openclaw --local --session-id <id> --message "<msg>" 驱动：prompt 经
	// --message flag，逐成员连续性经稳定的 --session-id。重起 runtime 对每条
	// 入站消息启动一次并读 stdout 到 EOF。VERIFY：对真实 CLI 验证 flag。
	"openclaw": {
		Name:                   "openclaw",
		Command:                []string{"openclaw", "--local"},
		InputFormat:            InputText,
		Completion:             CompletionNone,
		SupportsStdinInjection: false,
		PromptFlag:             "--message",
		SessionFlag:            "--session-id",
		MCPInject:              MCPInjectNone,
		SystemPromptInject:     SystemPromptNone,
	},
	// Hermes Agent（NousResearch/hermes-agent）——one-shot，非 stdin 流式
	// （自官方 CLI reference 调研）。hermes -z "<prompt>" 是程序化入口：读一个
	// prompt（argv 传入；stdin 是补充上下文），只打印最终答案为纯文本，然后退出
	// ——没有持续多 prompt 循环也没有结构化轮次分隔符（一轮在进程退出/stdout EOF
	// 结束）。--yolo 绕过危险命令审批提示。跨轮连续性用 --continue / --resume
	// <session_id>。团队 MCP server 带外注册：hermes mcp add <name> --command
	// uapclawmcpserver。prompt 是末位位置参数且进程每轮退出，所以跑在重起
	// runtime 下：每条入站消息一次 hermes -z --yolo [--continue] "<message>"，
	// 读 stdout 到 EOF。--continue 恢复最近会话做跨轮上下文——注意并发 hermes
	// 成员会竞争（该版本的命名会话支持可隔离；VERIFY）。
	"hermes": {
		Name:                   "hermes",
		Command:                []string{"hermes", "-z", "--yolo"},
		InputFormat:            InputText,
		Completion:             CompletionNone,
		SupportsStdinInjection: false,
		ContinueArgs:           []string{"--continue"},
		MCPInject:              MCPInjectHermesAdd,
		SystemPromptInject:     SystemPromptNone,
	},
	// 测试与简单集成用的行式 echo agent：一行输入，输出以显式轮次结束标记终止。
	"generic": {
		Name:                   "generic",
		Command:                []string{},
		InputFormat:            InputText,
		Completion:             CompletionMarkerPrefix + "<<END_OF_TURN>>",
		SupportsStdinInjection: true,
		MCPInject:              MCPInjectNone,
		SystemPromptInject:     SystemPromptNone,
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildCommand 返回启动 argv，可选追加额外参数。
// Python: CliAgentAdapter.build_command (adapters.py:136)
func (a *CliAgentAdapter) BuildCommand(extraArgs []string) []string {
	out := make([]string, 0, len(a.Command)+len(extraArgs))
	out = append(out, a.Command...)
	out = append(out, extraArgs...)
	return out
}

// BuildTurnCommand 为 one-shot（每轮重起）CLI 构建单轮启动 argv。
//
// Python: CliAgentAdapter.build_turn_command (adapters.py:140)
func (a *CliAgentAdapter) BuildTurnCommand(prompt string, sessionID string, firstTurn bool, extraArgs []string) []string {
	argv := append([]string{}, a.Command...)
	if sessionID != "" {
		// 首轮（或无 resume_flag）：用 session_flag 启动/标识会话；
		// 带 resume_flag 的后续轮恢复同一 id。（adapters.py:152-157）
		if !firstTurn && a.ResumeFlag != "" {
			argv = append(argv, a.ResumeFlag, sessionID)
		} else if a.SessionFlag != "" {
			argv = append(argv, a.SessionFlag, sessionID)
		}
	}
	if !firstTurn && len(a.ContinueArgs) > 0 {
		argv = append(argv, a.ContinueArgs...)
	}
	argv = append(argv, extraArgs...)
	if a.PromptFlag != "" {
		argv = append(argv, a.PromptFlag, prompt)
	} else {
		argv = append(argv, prompt)
	}
	return argv
}

// FormatInput 把一轮的输入文本框架化以便写入 CLI stdin。
// Python: CliAgentAdapter.format_input (adapters.py:172)
func (a *CliAgentAdapter) FormatInput(text string) string {
	if a.InputFormat == InputCodexProto {
		// Python uuid.uuid4().hex——无连字符 32 位十六进制（adapters.py:176）
		payload, _ := json.Marshal(map[string]any{
			"id": strings.ReplaceAll(uuid.New().String(), "-", ""),
			"op": map[string]any{
				"type":  "user_input",
				"items": []map[string]any{{"type": "text", "text": text}},
			},
		})
		return string(payload)
	}
	return text
}

// IsTurnComplete 判断一行 stdout 是否标志当前轮次结束。
// Python: CliAgentAdapter.is_turn_complete (adapters.py:186)
func (a *CliAgentAdapter) IsTurnComplete(line string) bool {
	if a.Completion == CompletionCodexTaskComplete {
		return jsonFieldEquals(line, []string{"msg", "type"}, "task_complete") ||
			jsonFieldEquals(line, []string{"type"}, "task_complete")
	}
	if a.Completion == CompletionCodexJSON {
		return jsonFieldEquals(line, []string{"type"}, "turn.completed")
	}
	if strings.HasPrefix(a.Completion, CompletionMarkerPrefix) {
		marker := strings.TrimPrefix(a.Completion, CompletionMarkerPrefix)
		return marker != "" && strings.Contains(line, marker)
	}
	return false
}

// SummarizeOutputLine 提取一行 stdout 的简短人类可读摘要。
// 用于可观测性（日志）与外部 CLI 正在做什么的可选 team-stream 呈现。纯文本 CLI
// 行本身即叙述；结构化输出 CLI（codex/gemini JSON 流）从事件里取 assistant 文本
// 或工具描述，跳过生命周期/轮次边界事件。无可呈现内容返回空串。从不报错。
//
// 跨 CLI JSON 模式尽力而为；codex/gemini 事件落到防御性通用字段提取
// （VERIFY：需对真实输出验证）。
//
// Python: CliAgentAdapter.summarize_output_line (adapters.py:200)
func (a *CliAgentAdapter) SummarizeOutputLine(line string) string {
	text := strings.TrimSpace(line)
	if text == "" {
		return ""
	}
	if !a.StructuredOutput {
		return text
	}
	if !strings.HasPrefix(text, "{") {
		return ""
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(text), &event); err != nil {
		return ""
	}
	if event == nil {
		return ""
	}
	return summarizeEvent(event)
}

// MCPLaunchArgs 构建向本 CLI 注册 stdio MCP server 的启动 argv。
// 返回应追加到启动命令的额外参数，使 CLI 启动指定名字的 stdio MCP server
// （从而拿到团队协作工具）。无注入策略（MCPInjectNone）的 CLI 返回空列表。
//
// Python: CliAgentAdapter.mcp_launch_args (adapters.py:246)
func (a *CliAgentAdapter) MCPLaunchArgs(serverName string, serverCommand []string) []string {
	if len(serverCommand) == 0 {
		return []string{}
	}
	binary := serverCommand[0]
	args := serverCommand[1:]
	if a.MCPInject == MCPInjectCodexOverride {
		// Codex 把 -c key=value 的值解析为 TOML/JSON，所以字符串加引号、args
		// 列表 JSON 编码。点号键必须用 bare-key 安全的 server 名（无需引号的字符）。
		key := strings.ReplaceAll(serverName, "-", "_")
		argv := []string{"-c", fmt.Sprintf("mcp_servers.%s.command=%s", key, jsonString(binary))}
		if len(args) > 0 {
			argv = append(argv, "-c", fmt.Sprintf("mcp_servers.%s.args=%s", key, jsonString(args)))
		}
		argv = append(argv,
			"-c", fmt.Sprintf("mcp_servers.%s.env_vars=%s", key, jsonString(MCPServerEnvVars)),
			"-c", fmt.Sprintf("mcp_servers.%s.startup_timeout_sec=%d", key, codexMCPStartupTimeoutS),
			"-c", fmt.Sprintf("mcp_servers.%s.required=true", key),
		)
		return argv
	}
	return []string{}
}

// MCPRegisterCommand 构建一次性向本 CLI 注册 stdio MCP server 的命令。
// 有些 CLI 没有 MCP 启动 flag，而是经持久化到自身配置的子命令注册
// （<cli> mcp add ...）。对这类 CLI，spawn 路径在成员首轮前执行一次本命令。
// 在启动时注入（用 MCPLaunchArgs）或无已知注册机制的 CLI 返回 nil。
//
// Python: CliAgentAdapter.mcp_register_command (adapters.py:280)
func (a *CliAgentAdapter) MCPRegisterCommand(serverName string, serverCommand []string) []string {
	if len(serverCommand) == 0 {
		return nil
	}
	binary := serverCommand[0]
	args := serverCommand[1:]
	cli := a.Name
	if len(a.Command) > 0 {
		cli = a.Command[0]
	}
	if a.MCPInject == MCPInjectGeminiAdd {
		// gemini mcp add <name> <commandOrUrl> [args...]
		out := []string{cli, "mcp", "add", serverName, binary}
		return append(out, args...)
	}
	if a.MCPInject == MCPInjectHermesAdd {
		// hermes mcp add <name> --command <bin> -- <args...>；确切的参数转发
		// 形式与版本相关（VERIFY：需对真实 CLI 验证），默认只转发二进制。
		return []string{cli, "mcp", "add", serverName, "--command", binary}
	}
	return nil
}

// SystemPromptArgs 构建为本 CLI 设置成员 system prompt 的启动 argv。
// CLI 无 system-prompt flag（SystemPromptNone）时返回空；调用方改为把 text
// 拼到成员首条用户消息前。
//
// Python: CliAgentAdapter.system_prompt_args (adapters.py:317)
func (a *CliAgentAdapter) SystemPromptArgs(text string) []string {
	if text == "" {
		return []string{}
	}
	if a.SystemPromptInject == SystemPromptCodexDeveloper {
		// codex 把 -c 值解析为 TOML；JSON 编码的字符串是合法 TOML basic string，
		// 能安全转义换行/引号。
		return []string{"-c", fmt.Sprintf("developer_instructions=%s", jsonString(text))}
	}
	return []string{}
}

// InjectsSystemPromptViaArg 本 CLI 是否接受经启动参数传 system prompt。
// false 表示调用方必须把 system prompt 拼到成员首条用户消息前（无专用 flag）。
//
// Python: CliAgentAdapter.injects_system_prompt_via_arg (adapters.py:334)
func (a *CliAgentAdapter) InjectsSystemPromptViaArg() bool {
	return a.SystemPromptInject != SystemPromptNone
}

// AvailableAdapters 返回已注册的 adapter 名单。
// Python: available_adapters (adapters.py:509)
func AvailableAdapters() []string {
	names := make([]string, 0, len(builtinAdapters))
	for name := range builtinAdapters {
		names = append(names, name)
	}
	return names
}

// BuildAdapter 按名字解析内置 adapter。
// commandOverride 可选：替换默认启动 argv 的完整 argv（如绝对二进制路径或额外 flag）。
// 未知 adapter 报 AGENT_TEAM_CONFIG_INVALID。
//
// Python: build_adapter (adapters.py:514)；Python raise_error 抛出，Go 返回 error。
func BuildAdapter(name string, commandOverride []string) (*CliAgentAdapter, error) {
	adapter, ok := builtinAdapters[name]
	if !ok {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg(fmt.Sprintf("unknown cli agent adapter '%s'; known: %s", name, strings.Join(AvailableAdapters(), ", "))))
	}
	copied := *adapter
	copied.Command = append([]string{}, adapter.Command...)
	if commandOverride != nil {
		copied.Command = append([]string{}, commandOverride...)
	}
	return &copied, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// summarizeEvent 从一个结构化输出事件提取简短叙述摘要。
// 依序尝试：assistant message.content 块（text + tool_use 名）、codex item 的
// text/command 字段、再通用顶层 text 字段（gemini 等）。生命周期事件或无
// 可呈现文本的事件返回空串。
//
// Python: _summarize_event (adapters.py:439)
func summarizeEvent(event map[string]any) string {
	if t, ok := event["type"].(string); ok {
		if _, lifecycle := lifecycleEventTypes[t]; lifecycle {
			return ""
		}
	}
	if message, ok := event["message"].(map[string]any); ok {
		if summary := summarizeContentBlocks(message["content"]); summary != "" {
			return summary
		}
	}
	if item, ok := event["item"].(map[string]any); ok {
		for _, key := range []string{"text", "command", "aggregated_output"} {
			if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
				return truncateSummary(strings.TrimSpace(value))
			}
		}
	}
	for _, key := range []string{"text", "content", "delta"} {
		if value, ok := event[key].(string); ok && strings.TrimSpace(value) != "" {
			return truncateSummary(strings.TrimSpace(value))
		}
	}
	return ""
}

// summarizeContentBlocks 摘要 assistant content 块列表（text + tool_use）。
//
// Python: _summarize_content_blocks (adapters.py:474)
func summarizeContentBlocks(content any) string {
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, blockAny := range blocks {
		block, ok := blockAny.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "text":
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		case "tool_use":
			if name, ok := block["name"].(string); ok {
				parts = append(parts, "→ "+name)
			}
		}
	}
	joined := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			joined = append(joined, s)
		}
	}
	result := strings.Join(joined, " ")
	if strings.TrimSpace(result) == "" {
		return ""
	}
	return truncateSummary(result)
}

// jsonFieldEquals 判断 line 是否为 path 处取值等于 expected 的 JSON 对象。
//
// Python: _json_field_equals (adapters.py:494)
func jsonFieldEquals(line string, path []string, expected string) bool {
	stripped := strings.TrimSpace(line)
	if !strings.HasPrefix(stripped, "{") {
		return false
	}
	var node any
	if err := json.Unmarshal([]byte(stripped), &node); err != nil {
		return false
	}
	for _, key := range path {
		m, ok := node.(map[string]any)
		if !ok {
			return false
		}
		node = m[key]
	}
	s, ok := node.(string)
	return ok && s == expected
}

// jsonString 对一个值做紧凑 JSON 编码（Codex -c TOML 值编码用）。
func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// truncateSummary 按 summaryLimit 截断摘要（Python [:_SUMMARY_LIMIT]）。
func truncateSummary(s string) string {
	r := []rune(s)
	if len(r) <= summaryLimit {
		return s
	}
	return string(r[:summaryLimit])
}
func init() {
	// 向 schema 校验器注册内置 adapter 名单（Python 校验器惰性 import
	// available_adapters 的 Go 等价物，见 schema.AdapterNamesFunc 注释）。
	schemaAdapterNamesHook()
}

// schemaAdapterNamesHook 注册 schema 包的 adapter 名单钩子。
func schemaAdapterNamesHook() {
	schema.AdapterNamesFunc = AvailableAdapters
}

func init() {
	// D-15.16-e14：adapter 面注入 external 包的 backends 缝
	// （Python 同包直调 available_adapters/build_adapter；Go 为消
	// tools → external → externalcli → tools 环，adapter 构造经缝回填）。
	external.SetAdaptersProvider(AvailableAdapters)
	external.SetAdaptersBuildProvider(func(name string) (*external.AdapterBuildResult, error) {
		adapter, err := BuildAdapter(name, nil)
		if err != nil {
			return nil, err
		}
		return &external.AdapterBuildResult{
			Name:    adapter.Name,
			Command: adapter.Command,
		}, nil
	})
}
