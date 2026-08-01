# 10.3.23 Hooks（executor + user_hook_rail）设计文档

## 概述

对齐 Python `jiuwenswarm/common/hooks_config.py` + `jiuwenswarm/server/hooks/executor.py` + `jiuwenswarm/server/hooks/user_hook_rail.py`，实现用户配置的 hooks 执行引擎。

Python 的 hooks 系统分为两层：
- **配置层**（common/hooks_config.py）：HookType/HookEvent 枚举 + CommandHookConfig/PromptHookConfig 配置模型 + HookMatcher 匹配逻辑 + HooksConfig 聚合 + load_hooks_config 加载
- **执行层**（server/hooks/executor.py）：HookExecutor 统一调度 command/prompt 两类 hook，返回 HookResult（SUCCESS/BLOCKING/NON_BLOCKING_ERROR）
- **Rail 层**（server/hooks/user_hook_rail.py）：UserHookRail(DeepAgentRail) 将 hooks 以 Rail 形态拦截工具调用和 Agent 生命周期

Go 中同样分为两层，但放在不同的包：
- `internal/common/hooks/`：配置模型（全局共享，Gateway 和 AgentServer 都能引用）
- `internal/swarm/server/hooks/`：执行器 + UserHookRail

## 文件目录

### internal/common/hooks/

```
common/hooks/
├── doc.go           # 包文档
├── types.go         # HookType 枚举 + HookEvent 常量 + AgentRailEvents/GatewayEvents 分组
└── config.go        # CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig
```

对应 Python 代码：`jiuwenswarm/common/hooks_config.py`

### internal/swarm/server/hooks/

```
server/hooks/
├── doc.go           # 包文档
├── executor.go      # HookOutcome/HookResult + HookExecutor(command/prompt) + ParseCommandOutput + ExtractJSONFromResponse
└── user_hook_rail.go # UserHookRail(embed DeepAgentRail) 覆盖 4 个钩子方法
```

对应 Python 代码：`jiuwenswarm/server/hooks/executor.py` + `user_hook_rail.py`

## 详细设计

### 1. common/hooks/types.go — HookType + HookEvent

对齐 Python `HookType(str, Enum)` + `HookEvent(str, Enum)` + `_AGENT_RAIL_EVENTS` + `_GATEWAY_EVENTS` + `is_rail_event()` + `is_gateway_event()`

```go
// HookType hook 类型枚举，对齐 Python HookType(COMMAND/PROMPT)
type HookType string

const (
    HookTypeCommand HookType = "command"
    HookTypePrompt  HookType = "prompt"
)

// HookEvent hook 事件常量，对齐 Python HookEvent(17 个)
// 统一为字符串常量，格式与 Python 一致（如 "PreToolUse"）
const (
    HookEventPreToolUse            = "PreToolUse"
    HookEventPostToolUse           = "PostToolUse"
    HookEventPostToolUseFailure    = "PostToolUseFailure"
    HookEventStop                  = "Stop"
    HookEventUserPromptSubmit      = "UserPromptSubmit"
    HookEventSessionStart          = "SessionStart"
    HookEventSessionEnd            = "SessionEnd"
    HookEventNotification          = "Notification"
    HookEventPermissionRequest     = "PermissionRequest"
    HookEventPermissionDenied      = "PermissionDenied"
    HookEventSubagentStart         = "SubagentStart"
    HookEventSubagentStop          = "SubagentStop"
    HookEventConfigChange          = "ConfigChange"
    HookEventInstructionsLoaded    = "InstructionsLoaded"
    HookEventSetup                 = "Setup"
    HookEventBeforeModelCall       = "BeforeModelCall"
    HookEventAfterModelCall        = "AfterModelCall"
)

// AgentRailEvents 需要在 AgentServer Rail 层执行的事件，对齐 Python _AGENT_RAIL_EVENTS
var AgentRailEvents = map[string]bool{
    HookEventPreToolUse:         true,
    HookEventPostToolUse:        true,
    HookEventPostToolUseFailure: true,
    HookEventStop:               true,
    HookEventPermissionRequest:  true,
    HookEventPermissionDenied:   true,
    HookEventSubagentStart:      true,
    HookEventSubagentStop:       true,
    HookEventBeforeModelCall:    true,
    HookEventAfterModelCall:     true,
}

// GatewayEvents 需要在 Gateway 层执行的事件，对齐 Python _GATEWAY_EVENTS
var GatewayEvents = map[string]bool{
    HookEventUserPromptSubmit:   true,
    HookEventSessionStart:       true,
    HookEventSessionEnd:         true,
    HookEventNotification:       true,
    HookEventConfigChange:       true,
    HookEventInstructionsLoaded: true,
    HookEventSetup:              true,
}

// IsRailEvent 判断事件是否属于 AgentServer Rail 层，对齐 Python is_rail_event()
func IsRailEvent(event string) bool { return AgentRailEvents[event] }

// IsGatewayEvent 判断事件是否属于 Gateway 层，对齐 Python is_gateway_event()
func IsGatewayEvent(event string) bool { return GatewayEvents[event] }
```

### 2. common/hooks/config.go — 配置数据模型 + matcher + load

对齐 Python `CommandHookConfig` + `PromptHookConfig` + `HookMatcher` + `HooksConfig` + `load_hooks_config`

```go
// CommandHookConfig command 类型 hook 配置，对齐 Python CommandHookConfig dataclass
type CommandHookConfig struct {
    Type          string `json:"type"`            // 默认 "command"
    Command       string `json:"command"`         // shell 命令
    Timeout       int    `json:"timeout"`          // 默认 30 秒
    Shell         string `json:"shell"`            // 默认 "bash"
    StatusMessage string `json:"status_message"`   // 状态消息
}

// PromptHookConfig prompt 类型 hook 配置，对齐 Python PromptHookConfig dataclass
type PromptHookConfig struct {
    Type          string `json:"type"`            // 默认 "prompt"
    Prompt        string `json:"prompt"`          // 模板字符串
    Timeout       int    `json:"timeout"`          // 默认 15 秒
    Model         string `json:"model"`            // LLM 模型名
    StatusMessage string `json:"status_message"`   // 状态消息
}

// HookMatcher hook 匹配器，对齐 Python HookMatcher dataclass
type HookMatcher struct {
    Matcher string           `json:"matcher"` // 匹配表达式，默认 "*"
    Hooks   []map[string]any `json:"hooks"`   // hook 配置列表
}

// Matches 检查 query 是否匹配此 matcher，对齐 Python HookMatcher.matches()
// 支持：*（匹配所有）| |分隔的 OR 匹配 | 正则匹配 | 精确匹配
func (m *HookMatcher) Matches(query string) bool { ... }

// matchSingle 单个模式匹配，对齐 Python HookMatcher._match_single()
func matchSingle(pattern, query string) bool { ... }

// HooksConfig hooks 配置聚合，对齐 Python HooksConfig dataclass
type HooksConfig struct {
    Events          map[string][]HookMatcher `json:"events"`           // 事件 → matcher 列表
    DisableAllHooks bool                     `json:"disable_all_hooks"` // 禁用所有 hooks
}

// Match 获取匹配该事件 + query 的所有 hook 配置，对齐 Python HooksConfig.match()
func (c *HooksConfig) Match(event, query string) []map[string]any { ... }

// GetEventSummary 返回各事件的 hook 数量摘要，对齐 Python HooksConfig.get_event_summary()
func (c *HooksConfig) GetEventSummary() []map[string]any { ... }

// LoadHooksConfig 从 config.yaml 的 hooks 段加载配置，对齐 Python load_hooks_config()
func LoadHooksConfig(configBase map[string]any) *HooksConfig { ... }
```

**HookMatcher.Matches 逻辑对齐 Python：**

1. `*` 或空字符串 → 匹配所有
2. 包含 `|` 且不以 `^` 开头 → OR 匹配（`|` 分隔多个 pattern，逐一 matchSingle）
3. 其他 → matchSingle 单模式匹配

**matchSingle 逻辑对齐 Python _match_single：**

1. 精确匹配 → `pattern == query`
2. 正则匹配 → `pattern` 以 `^` 开头或以 `$` 结尾或含 `.*` → `regexp.MatchString(pattern, query)`
3. 不匹配 → `false`

### 3. server/hooks/executor.go — HookExecutor

对齐 Python `HookOutcome` + `HookResult` + `HookExecutor` + `parse_command_output` + `extract_json_from_response`

```go
// ──────────────────────────── 常量 ────────────────────────────

// HookOutcome hook 执行结果类型，对齐 Python HookOutcome
const (
    HookOutcomeSuccess          = "success"
    HookOutcomeBlocking         = "blocking"
    HookOutcomeNonBlockingError = "non_blocking_error"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HookResult hook 执行结果，对齐 Python HookResult dataclass
type HookResult struct {
    Outcome          string         // success/blocking/non_blocking_error
    Error            string         // 错误/拦截原因
    ShowToModel      bool           // 是否展示给模型（blocking 时为 true）
    ModifiedInput    map[string]any // 修改后的输入（由 hook 修改）
    AdditionalContext string        // 附加上下文
}

// LLMConfig prompt hook 使用的 LLM 配置
// 对齐 Python _query_llm 中从 config 提取的 APIKey/APIBase/ClientProvider/DefaultModel
type LLMConfig struct {
    APIKey         string
    APIBase        string
    ClientProvider string
    DefaultModel   string
}

// HookExecutor hook 执行器，对齐 Python HookExecutor
// 统一调度 command/prompt 两类 hook，返回 HookResult 列表
type HookExecutor struct {
    llmConfig LLMConfig // 内部创建 ModelClient，对齐 Python _query_llm 动态创建
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHookExecutor 创建 HookExecutor，对齐 Python HookExecutor()
func NewHookExecutor(llmConfig LLMConfig) *HookExecutor { ... }

// RunAll 并行执行同一 matcher 下的所有 hooks，对齐 Python HookExecutor.run_all()
// Go 中使用 goroutine + WaitGroup 实现（Go 没有 asyncio.gather，用并发等价）
func (e *HookExecutor) RunAll(ctx context.Context, hookConfigs []map[string]any, hookInput map[string]any, sessionID string) []HookResult { ... }

// ParseCommandOutput 解析 command hook 的 stdout JSON 协议，对齐 Python HookExecutor.parse_command_output（静态方法）
// stdout 空 → SUCCESS
// JSON decision=block → BLOCKING(reason, showToModel=true)
// JSON modifiedInput/additionalContext/reason → SUCCESS + 附加字段
func ParseCommandOutput(stdout string) HookResult { ... }

// ExtractJSONFromResponse 从 LLM 响应中提取 JSON 对象，对齐 Python HookExecutor.extract_json_from_response（静态方法）
// 直接 JSON → 返回
// markdown fence ```json``` → 返回
// 嵌入式 { ... } → 返回
// 失败 → 空 map
func ExtractJSONFromResponse(text string) map[string]any { ... }

// ──────────────────────────── 非导出函数 ────────────────────────────

// runCommandHook 执行 command 类型 hook（子进程），对齐 Python _run_command_hook
// 设置 ARGUMENTS + TOOL_NAME 环境变量
// 通过 os/exec 执行 shell 命令
// exit 0 → ParseCommandOutput(stdout)
// exit 2 → blocking（stdout JSON 解析 reason，fallback stderr）
// 其他 → non_blocking_error(stderr)
// 超时 → kill 进程 + non_blocking_error
func (e *HookExecutor) runCommandHook(ctx context.Context, config map[string]any, hookInput map[string]any) HookResult { ... }

// runPromptHook 执行 prompt 类型 hook（LLM 审核），对齐 Python _run_prompt_hook
// 模板替换 $ARGUMENTS/$TOOL_NAME
// 调 queryLLM → ExtractJSONFromResponse
// decision=block → BLOCKING(reason, showToModel=true)
// modifiedInput/additionalContext → SUCCESS + 附加字段
// 超时 → non_blocking_error
func (e *HookExecutor) runPromptHook(ctx context.Context, config map[string]any, hookInput map[string]any) HookResult { ... }

// queryLLM 调用 LLM 执行 hook 审查，对齐 Python _query_llm
// 内部用 LLMConfig 创建 ModelClient 实例
func (e *HookExecutor) queryLLM(ctx context.Context, prompt, modelName string) (string, error) { ... }
```

**command hook 执行流程对齐 Python：**

1. 获取 `command` 字段 → 空 command 返回 NON_BLOCKING_ERROR
2. 获取 `timeout`(默认30) 和 `shell`(默认"bash")
3. 序列化 `hookInput` 为 JSON → 设置环境变量 `ARGUMENTS` + `TOOL_NAME`
4. `os/exec` 创建子进程：`shell -c command`，stdin 传入 JSON，设置 env
5. `proc.Wait()` 等待完成（带超时）：
   - 超时 → kill 进程 → 返回 NON_BLOCKING_ERROR
   - 其他异常 → 返回 NON_BLOCKING_ERROR
6. 退出码语义：
   - 0 → `ParseCommandOutput(stdout)`
   - 2 → blocking：尝试 ParseCommandOutput 获取 reason，fallback 到 stderr
   - 其他 → NON_BLOCKING_ERROR(stderr)

**prompt hook 执行流程对齐 Python：**

1. 获取 `prompt` 字段 → 空 prompt 返回 NON_BLOCKING_ERROR
2. 获取 `timeout`(默认15) 和 `model`
3. 序列化 `hookInput` 为 JSON → 替换 `$ARGUMENTS` → 替换 `$TOOL_NAME`
4. 调用 `queryLLM(prompt, model)` → 获取响应文本
5. `ExtractJSONFromResponse(text)` → 解析 decision/modifiedInput/additionalContext
6. 超时/异常 → NON_BLOCKING_ERROR

### 4. server/hooks/user_hook_rail.go — UserHookRail

对齐 Python `UserHookRail(DeepAgentRail, priority=60)`，覆盖 4 个钩子方法

```go
// ──────────────────────────── 结构体 ────────────────────────────

// UserHookRail 用户配置的 hooks 执行引擎，对齐 Python UserHookRail(DeepAgentRail, priority=60)
// 将用户配置的 hooks 以 Rail 形态注册到 DeepAgent，拦截工具调用和 Agent 生命周期
// Priority=60: 在 SecurityRail(80) 之后，JiuClawStreamEventRail(50) 之前
type UserHookRail struct {
    rails.DeepAgentRail
    config   hookscfg.HooksConfig    // 引用 common/hooks 包
    executor *HookExecutor
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUserHookRail 创建 UserHookRail，对齐 Python UserHookRail.__init__(hooks_config)
func NewUserHookRail(config hookscfg.HooksConfig, executor *HookExecutor) *UserHookRail { ... }

// BeforeToolCall 对齐 Python before_tool_call → HookEvent.PRE_TOOL_USE
// blocking → cbc.Extra["_skip_tool"]=true, cbc.Extra["_hook_feedback"]=error
// modifiedInput → 修改 cbc.Inputs.ToolArgs/ToolName
// additionalContext → cbc.Extra["_hook_additional_context"] 追加
func (r *UserHookRail) BeforeToolCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { ... }

// AfterToolCall 对齐 Python after_tool_call → HookEvent.POST_TOOL_USE
// blocking → cbc.Extra["_post_tool_hook_feedback"]=error
// additionalContext → 拼接到 cbc.Inputs.ToolResult
func (r *UserHookRail) AfterToolCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { ... }

// OnToolException 对齐 Python on_tool_exception → HookEvent.POST_TOOL_USE_FAILURE
// 仅通知收集（不改变处理流程）
func (r *UserHookRail) OnToolException(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { ... }

// AfterInvoke 对齐 Python after_invoke → HookEvent.STOP
// blocking → cbc.Extra["_stop_hook_feedback"]=error
func (r *UserHookRail) AfterInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { ... }
```

**每个钩子方法的执行流程对齐 Python：**

1. 从 `cbc` 获取 `tool_name`（BeforeToolCall/AfterToolCall/OnToolException）或 `result`（AfterInvoke）
2. 调 `config.Match(event, query)` 获取匹配的 hook 配置列表 → 无匹配则直接返回 nil
3. 构建 `hookInput` map（event/tool_name/tool_input/tool_result/session_id/error）
4. 调 `executor.RunAll(ctx, hookConfigs, hookInput, sessionID)` → 获取 HookResult 列表
5. 遍历 results：
   - `blocking` → 设置 Extra 字段 + 返回
   - `modifiedInput` → 修改 ToolArgs/ToolName
   - `additionalContext` → 追加到 Extra 或 ToolResult

### 5. 回填

**5.1 DeepAdapter 注册 UserHookRail**

对齐 Python `interface_deep.py` L2200-2211 的注册代码，在 DeepAdapter 构建 rails 列表时：

```go
hooksCfg := hookscfg.LoadHooksConfig(configBase)
if len(hooksCfg.Events) > 0 {
    llmCfg := hookscfg.LLMConfig{ ... }  // 从 config 提取
    hookExec := serverhooks.NewHookExecutor(llmCfg)
    userHookRail := serverhooks.NewUserHookRail(*hooksCfg, hookExec)
    railsList = append(railsList, userHookRail)
    logger.Info(logComponent).Int("event_types", len(hooksCfg.Events)).Msg("UserHookRail 加载完成")
}
```

**5.2 handleHooksList 回填**

`handle_extensions.go` 的 `handleHooksList` 从返回空列表改为返回 `HooksConfig.GetEventSummary()`：

```go
func (s *AgentServer) handleHooksList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
    hooksCfg := hookscfg.LoadHooksConfig(s.configBase)
    summary := hooksCfg.GetEventSummary()
    return schema.NewAgentResponse(request.RequestID, request.ChannelID,
        schema.WithPayload(map[string]any{"hooks": summary}),
    ), nil
}
```

### 6. IMPLEMENTATION_PLAN.md 更新

将 `10.3.23-26 ☐` 拆分为：

| 编号 | 状态 | 内容 | Python 文件 |
|------|------|------|------------|
| 10.3.23.1 | ☐ | HookType/HookEvent 常量 + AgentRailEvents/GatewayEvents | `hooks_config.py` HookType/HookEvent |
| 10.3.23.2 | ☐ | CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig | `hooks_config.py` 配置模型 |
| 10.3.23.3 | ☐ | HookOutcome/HookResult + HookExecutor(command/prompt) + ParseCommandOutput + ExtractJSONFromResponse | `executor.py` |
| 10.3.23.4 | ☐ | UserHookRail(embed DeepAgentRail) 4 个钩子方法 | `user_hook_rail.py` |
| 10.3.23.5 | ☐ | 回填：DeepAdapter 注册 + handleHooksList | `interface_deep.py` 注册 |
| 10.3.24 | ☐ | Sandbox | `sandbox/` |
| 10.3.25 | ☐ | Utils | `utils/` |
| 10.3.26 | ☐ | 入口 | `app_agentserver.py` |

### 7. 包依赖方向

```
internal/common/hooks/          ← 不依赖 server/ 或 agentcore/
       ↑           ↑
       |           |
server/hooks/      agentcore/harness/rails/
(executor+rail)    (DeepAgentRail 基类)
       ↑
       |
server/runtime/adapter/
(注册 UserHookRail)
```

不违反规则 6（gateway 不 import server）。

### 8. 测试策略

- `common/hooks/types_test.go`：HookType/HookEvent 常量值对齐 + AgentRailEvents/GatewayEvents 判断
- `common/hooks/config_test.go`：HookMatcher.Matches 各模式（*/OR/正则/精确）+ HooksConfig.Match + GetEventSummary + LoadHooksConfig
- `server/hooks/executor_test.go`：HookExecutor.RunAll + runCommandHook 各退出码 + runPromptHook 各分支 + ParseCommandOutput + ExtractJSONFromResponse + 超时/异常
- `server/hooks/user_hook_rail_test.go`：4 个钩子方法的 blocking/modifiedInput/additionalContext 行为

command hook 测试使用 `os/exec` 真实子进程（可用 `echo`/`cat` 等简单命令），不依赖外部环境，不需要 build tag。
prompt hook 测试中 `queryLLM` 使用 mock LLMConfig（或测试用 ModelClient mock注入），LLM 真实调用测试加 `//go:build llm` 标签。

覆盖率目标 ≥ 85%。
