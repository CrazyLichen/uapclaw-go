# 9.26 BrowserAgent 全栈实现设计

## 概述

实现 BrowserAgent（浏览器子代理）的完整 Go 代码，对齐 Python `openjiuwen/harness/subagents/browser_agent.py` 及其依赖的 `openjiuwen/harness/tools/browser_move/` 目录。

BrowserAgent 是 DeepAgent 的子代理之一，负责浏览器自动化任务：通过 Playwright MCP 工具执行网页导航、点击、输入、选择、检查和信息提取，并包含运行时生命周期管理、进度追踪与可恢复性、紧凑探测优化和守护护栏。

## 在 Agent 会话中的流程位置与作用

```
DeepAgent (主 Agent)
  ├── ResearchAgent (9.25 ✅) — 研究调查
  ├── BrowserAgent (9.26 🔄) — 浏览器自动化 ← 本设计
  ├── CodeAgent (9.27 🔄) — 编码
  ├── PlanAgent (9.28 🔄) — 规划
  ├── VerificationAgent (9.29 ☐) — 验证
  ├── ExploreAgent (9.30 🔄) — 探索
  └── MobileGUIAgent (9.31 ☐) — 移动端 GUI
```

**核心作用：**

1. **直接控制浏览器**：通过 Playwright MCP 工具执行网页导航、点击、输入、选择、检查和信息提取
2. **运行时生命周期管理**：BrowserAgentRuntime 管理浏览器服务的启动、心跳、关闭
3. **进度追踪与可恢复性**：BrowserRuntimeRail 实现会话级进度状态持久化，支持浏览器任务的断点续做
4. **紧凑探测优化**：提供 `browser_probe_interactives`（交互元素探测）和 `browser_probe_cards`（卡片结构探测），避免不必要的全页面快照
5. **守护护栏**：BrowserRunGuardrails 控制最大步数、最大失败数、超时、重试等安全边界

## 实现原则

1. **只实现 9.26 当前章节**，其他依赖章节用 `any` 占位，方法和步骤用注释标记 `⤵️` 等待回填
2. **提示词一比一复刻 Python 原文**，不做自行翻译（项目规则）
3. **逐层递进**：Layer 1 → Layer 2 → Layer 3，每层编译通过+测试覆盖后再进入下一层

## 总体架构与文件组织

```
internal/agentcore/harness/
├── subagents/
│   └── browser_agent.go              # Layer 1: BuildBrowserAgentConfig (改写现有骨架)
├── browser_agent_factory.go          # Layer 2: CreateBrowserAgent 工厂函数 (新建，依赖 Runtime/Rail/Tools)
│
└── tools/browser_move/               # 新建目录
    ├── doc.go                        # 包文档
    ├── env.go                        # Layer 1: 环境变量解析工具
    ├── config.go                     # Layer 1: RuntimeSettings + BrowserRunGuardrails + MCP配置工厂
    ├── parsing.go                    # Layer 1: JSON 解析工具 (extract_json_object, sanitize_json_schema)
    ├── progress.go                   # Layer 2: BrowserTaskProgressState 进度状态
    ├── service.go                    # Layer 2: BrowserService 后端服务
    ├── runtime.go                    # Layer 2: BrowserAgentRuntime 运行时内核
    ├── browser_rail.go               # Layer 2: BrowserRuntimeRail 进度追踪 Rail
    ├── runtime_tools.go              # Layer 2: 7个运行时工具
    ├── probes.go                     # Layer 3: JavaScript 探测代码生成
    ├── controllers.go                # Layer 3: BaseController 接口 + ActionController
    ├── agents.go                     # Layer 3: Worker Agent 构建器
    ├── profiles.go                   # Layer 3: BrowserProfile + BrowserProfileStore
    └── site_profiles.go              # Layer 3: 站点配置文件和选择器缓存
```

---

## Layer 1 — 配置层（~590 行 Go）

### 1.1 browser_agent.go（改写现有骨架，~120 行）

**Python 参考：** `openjiuwen/harness/subagents/browser_agent.py:145-192`

**改动要点：**

1. 参数签名升级：`BuildBrowserAgentConfig(model *llm.Model, config map[string]any, configBase map[string]any)` → `BuildBrowserAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams)`，对齐 ResearchAgent 已完成的升级模式
2. 新增常量 `BrowserAgentFactoryName = "browser_agent"`
3. 新增全局变量 `defaultBrowserAgentSystemPrompt` 和 `defaultBrowserAgentDescription`（中/英双语 map，一比一复刻 Python 原文）
4. 完整实现 AgentCard 默认值、SystemPrompt 默认值、MaxIterations=25（对齐 Python `max_iterations=25`）、FactoryName、FactoryKwargs（含 `"settings"` key 指向 `*RuntimeSettings`）
5. 导出辅助函数 `DefaultBrowserAgentSystemPrompt(language)` 和 `DefaultBrowserAgentDescription(language)`

**默认系统提示词（中/英双语，一比一复刻 Python）：**

- `DEFAULT_BROWSER_AGENT_SYSTEM_PROMPT_CN`：完整复制 `browser_agent.py:78-101`
- `DEFAULT_BROWSER_AGENT_SYSTEM_PROMPT_EN`：完整复制 `browser_agent.py:43-76`
- `DEFAULT_BROWSER_AGENT_DESCRIPTION_CN`：完整复制 `browser_agent.py:112`
- `DEFAULT_BROWSER_AGENT_DESCRIPTION_EN`：完整复制 `browser_agent.py:109-111`

**注意：** Layer 1 阶段只实现 BuildBrowserAgentConfig（纯配置构建），CreateBrowserAgent 工厂函数归入 Layer 2，因为它依赖 BrowserAgentRuntime/BrowserRuntimeRail/build_browser_runtime_tools。

### 1.2 config.go（~250 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/config.py` 全文

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// BrowserRunGuardrails 浏览器运行守护护栏
// 对齐 Python: BrowserRunGuardrails
type BrowserRunGuardrails struct {
    MaxSteps             int  `json:"max_steps"`
    MaxFailures          int  `json:"max_failures"`
    TimeoutS             int  `json:"timeout_s"`
    RetryOnce            bool `json:"retry_once"`
    ResumeOnMaxIterations bool `json:"resume_on_max_iterations"`
}

// RuntimeSettings Playwright 运行时配置
// 对齐 Python: RuntimeSettings
type RuntimeSettings struct {
    Provider   string               `json:"provider"`
    APIKey     string               `json:"api_key"`
    APIBase    string               `json:"api_base"`
    ModelName  string               `json:"model_name"`
    MCPCfg     *mcptypes.McpServerConfig `json:"mcp_cfg"`
    Guardrails *BrowserRunGuardrails     `json:"guardrails"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBrowserGuardrails 构建浏览器守护护栏，从环境变量读取
func BuildBrowserGuardrails() *BrowserRunGuardrails

// BuildPlaywrightMCPConfig 构建 Playwright MCP 服务器配置
func BuildPlaywrightMCPConfig() *mcptypes.McpServerConfig

// BuildRuntimeSettings 构建默认运行时配置
func BuildRuntimeSettings() *RuntimeSettings

// ResolveRuntimeSettings 解析运行时配置：优先使用传入的 settings，否则从 Model 推导
func ResolveRuntimeSettings(model *llm.Model, settings *RuntimeSettings) *RuntimeSettings

// ResolvePlaywrightMCPCwd 解析 MCP 工作目录
func ResolvePlaywrightMCPCwd() string
```

**关键实现细节：**

- `BuildPlaywrightMCPConfig()`：对齐 Python `build_playwright_mcp_config()`，支持 extension 模式、CDP endpoint、环境变量映射（PLAYWRIGHT_BROWSERS_PATH/HTTP_PROXY/HTTPS_PROXY/NO_PROXY）、PLAYWRIGHT_MCP_ENV_JSON 解析
- `BuildBrowserGuardrails()`：对齐 Python `build_browser_guardrails()`，环境变量 BROWSER_GUARDRAIL_MAX_STEPS/MAX_FAILURES/RETRY_ONCE/RESUME_ON_MAX_ITERATIONS
- `ResolveRuntimeSettings()`：对齐 Python `_resolve_runtime_settings()`，优先使用传入的 settings，否则从 Model 的 ModelClientConfig 推导 provider/api_key/api_base/model_name

### 1.3 env.go（~200 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/utils/env.py` 全文

**内容：**

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
    DefaultModelName           = "anthropic/claude-sonnet-4.5"
    DefaultBrowserTimeoutS     = 180
    DefaultGuardrailMaxSteps   = 20
    DefaultGuardrailMaxFailures = 2
    DefaultGuardrailRetryOnce  = true
    DefaultPlaywrightMCPCommand = "npx"
    DefaultPlaywrightMCPArgs   = "-y @playwright/mcp@latest"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FirstNonEmptyEnv 返回第一个非空的环境变量值
func FirstNonEmptyEnv(keys ...string) string

// NormalizeProvider 标准化模型提供者名称
func NormalizeProvider(provider string) string

// IsTruthyEnv 判断环境变量值是否为真
func IsTruthyEnv(value string) bool

// ResolveIntEnv 从环境变量解析整数
func ResolveIntEnv(keys []string, defaultVal int, minimum *int) int

// ResolveBoolEnv 从环境变量解析布尔值
func ResolveBoolEnv(keys []string, defaultVal bool) bool

// ResolveModelName 解析模型名称
func ResolveModelName() string

// ResolveBrowserTimeoutS 解析浏览器超时时间
func ResolveBrowserTimeoutS() int

// ResolveModelSettings 解析模型配置（provider, api_key, api_base）
func ResolveModelSettings() (provider, apiKey, apiBase string)

// ParseCommandArgs 解析命令行参数字符串
func ParseCommandArgs(value string) []string

// InferProviderFromAPIBase 从 API Base URL 推断 provider
func InferProviderFromAPIBase(apiBase string) string
```

**关键实现细节：**

- `ResolveModelSettings()`：对齐 Python `resolve_model_settings()`，支持 openai/openrouter/siliconflow/dashscope 四种 provider 的 key/base 环境变量优先级
- `NormalizeProvider()`：对齐 Python `normalize_provider()`，支持 "alibaba"→"dashscope"、"silicon-flow"→"siliconflow" 等别名
- `ParseCommandArgs()`：对齐 Python `parse_command_args()`，支持 JSON 数组格式和 shlex 风格拆分

### 1.4 parsing.go（~100 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/utils/parsing.py` 全文

**内容：**

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractJSONObject 从模型/工具文本中尽力提取 JSON 对象
// 对齐 Python: extract_json_object
func ExtractJSONObject(text any) map[string]any

// SanitizeJSONSchema 递归清除 OpenAI 兼容 API 不支持的 schema 关键字
// 对齐 Python: sanitize_json_schema
func SanitizeJSONSchema(schema any) any
```

**关键实现细节：**

- `ExtractJSONObject()`：对齐 Python 的多步提取策略：1) 直接 JSON 解析 → 2) ```json 代码块提取 → 3) 首尾花括号匹配。特殊处理 Playwright 的 `### Result`/`### Ran Playwright code` 标记
- `SanitizeJSONSchema()`：对齐 Python 的 schema 清理：1) 折叠 anyOf/oneOf nullable → plain type；2) 移除 `$schema/$id/$defs` 等关键字；3) null type → "object"

---

## Layer 2 — 运行时核心层（~2110 行 Go）

### 2.1 browser_agent_factory.go（新建，~80 行）

**Python 参考：** `openjiuwen/harness/subagents/browser_agent.py:195-262`

**职责：** `CreateBrowserAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error)`

**流程：**

1. 解析 `language`（ResolveLanguage）
2. 从 `params` 中提取或构建 `RuntimeSettings`（`_resolve_runtime_settings` 逻辑）
3. 创建 `BrowserAgentRuntime` 实例
4. 通过 `build_browser_runtime_tools(runtime, language)` 注入 7 个运行时工具
5. 注入 `BrowserRuntimeRail{runtime}` 到 Rails
6. 合并用户传入的 tools/mcps/rails 与注入的 tools/rails
7. 调用 `CreateDeepAgent(ctx, CreateDeepAgentParams{...})`

**同时修改 `deep_agent.go` 中的 switch 分支：** 将 `case "browser_agent", "browser_runtime":` 从返回 error 改为调用 `CreateBrowserAgent(ctx, kwargs)`

### 2.2 progress.go（~180 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:44-116`

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// BrowserTaskProgressState 浏览器任务进度状态
// 对齐 Python: BrowserTaskProgressState
type BrowserTaskProgressState struct {
    RequestID           string   `json:"request_id"`
    Status              string   `json:"status"`
    CompletedSteps      []string `json:"completed_steps"`
    RemainingSteps      []string `json:"remaining_steps"`
    NextStep            string   `json:"next_step"`
    CompletionEvidence  []string `json:"completion_evidence"`
    MissingRequirements []string `json:"missing_requirements"`
    RecentToolSteps     []string `json:"recent_tool_steps"`
    LastPageURL         string   `json:"last_page_url"`
    LastPageTitle       string   `json:"last_page_title"`
    LastScreenshot      any      `json:"last_screenshot"`
    LastWorkerFinal     string   `json:"last_worker_final"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserTaskProgressState 从字典创建进度状态
func NewBrowserTaskProgressStateFromDict(data map[string]any) *BrowserTaskProgressState

// IsEmpty 判断进度状态是否为空
func (s *BrowserTaskProgressState) IsEmpty() bool

// ToDict 转换为字典
func (s *BrowserTaskProgressState) ToDict() map[string]any
```

### 2.3 service.go（~800 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:118-1494`

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// BrowserService 浏览器后端服务，支持粘性逻辑会话
// 对齐 Python: BrowserService
type BrowserService struct {
    Provider          string
    APIKey            string
    APIBase           string
    ModelName         string
    MCPCfg            *mcptypes.McpServerConfig
    Guardrails        *BrowserRunGuardrails
    // ... 内部状态字段
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureRuntimeReady 确保运行时已就绪
func (s *BrowserService) EnsureRuntimeReady(ctx context.Context) error

// EnsureStarted 确保浏览器服务已启动
func (s *BrowserService) EnsureStarted(ctx context.Context) error

// RunTask 执行浏览器任务（含重试和恢复逻辑）
func (s *BrowserService) RunTask(ctx context.Context, task string, sessionID string, requestID string, timeoutS *int) (map[string]any, error)

// RequestCancel 请求取消任务
func (s *BrowserService) RequestCancel(ctx context.Context, sessionID string, requestID string) error

// ClearCancel 清除取消标记
func (s *BrowserService) ClearCancel(ctx context.Context, sessionID string, requestID string) error

// IsCancelled 检查是否已取消
func (s *BrowserService) IsCancelled(ctx context.Context, sessionID string, requestID string) (bool, error)

// Shutdown 关闭服务
func (s *BrowserService) Shutdown(ctx context.Context) error

// 进度状态管理方法
func (s *BrowserService) RecordToolProgress(sessionID, requestID, toolName string, toolResult any)
func (s *BrowserService) RecordWorkerProgress(sessionID, requestID string, parsed map[string]any)
func (s *BrowserService) GetProgressState(sessionID string) *BrowserTaskProgressState
func (s *BrowserService) ExportProgressState(sessionID string) map[string]any
func (s *BrowserService) SetProgressState(sessionID string, state *BrowserTaskProgressState)
func (s *BrowserService) ClearProgressState(sessionID string)
func (s *BrowserService) BuildProgressContext(state *BrowserTaskProgressState) string
func (s *BrowserService) BuildFailureSummary(task, error, pageURL, pageTitle, final string, screenshot any, attempt int, progressState *BrowserTaskProgressState) string
func (s *BrowserService) ShouldTreatAsCompleted(parsed map[string]any) bool
```

**关键实现细节：**

1. **粘性会话管理**：`_locks` map 实现会话级互斥，`_sessions` set 跟踪活跃会话
2. **守护护栏执行逻辑**：`RunTask` 方法包含完整重试链：retry_once + resume_on_max_iterations + transport error 重启
3. **进度状态持久化**：`_progress_by_session` map 存储每个会话的进度状态
4. **失败摘要构建**：`BuildFailureSummary` 对齐 Python `_build_failure_summary`，包含原始任务、失败尝试、错误信息、最后页面、进度上下文
5. **截图归一化**：`NormalizeScreenshotValue` 将本地文件路径转为 data URL，保持与 Python 行为一致

**占位标记：**

```go
// TODO: ⤵️ 9.38-49 回填 ManagedBrowserDriver 实现
// ManagedBrowserDriver 的启动/停止/健康检查逻辑待 9.38-49 实现后回填
var managedDriver any // ⤵️ 9.38-49

// TODO: ⤵️ 9.38-49 回填 MCP client ping 检查
// BrowserService._check_connection 中 MCP client 检查逻辑待回填
```

### 2.4 runtime.go（~350 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/runtime.py:56-537`

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// BrowserAgentRuntime 浏览器运行时内核
// 对齐 Python: BrowserAgentRuntime
type BrowserAgentRuntime struct {
    service    *BrowserService
    controller BaseController // ⤵️ Layer 3 回填
    // ... 内部字段
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserAgentRuntime 创建浏览器运行时实例
func NewBrowserAgentRuntime(provider, apiKey, apiBase, modelName string, mcpCfg *mcptypes.McpServerConfig, guardrails *BrowserRunGuardrails) *BrowserAgentRuntime

// EnsureRuntimeReady 确保运行时已就绪
func (r *BrowserAgentRuntime) EnsureRuntimeReady(ctx context.Context) error

// EnsureStarted 确保浏览器已启动
func (r *BrowserAgentRuntime) EnsureStarted(ctx context.Context) error

// CancelRun 取消正在运行的浏览器任务
func (r *BrowserAgentRuntime) CancelRun(ctx context.Context, sessionID string, requestID string) (map[string]any, error)

// ClearCancel 清除取消标记
func (r *BrowserAgentRuntime) ClearCancel(ctx context.Context, sessionID string, requestID string) (map[string]any, error)

// ProbeInteractives 探测页面交互元素
func (r *BrowserAgentRuntime) ProbeInteractives(ctx context.Context, maxItems int, viewportOnly bool, query string) (map[string]any, error)

// ProbeCards 探测页面卡片结构
func (r *BrowserAgentRuntime) ProbeCards(ctx context.Context, maxCards int, viewportOnly bool, includeButtons bool, query string) (map[string]any, error)

// RunCustomAction 运行自定义浏览器动作
func (r *BrowserAgentRuntime) RunCustomAction(ctx context.Context, action, sessionID, requestID string, params map[string]any) (map[string]any, error)

// ListActions 列出可用自定义动作
func (r *BrowserAgentRuntime) ListActions(ctx context.Context) (map[string]any, error)

// RuntimeHealth 返回运行时健康状态
func (r *BrowserAgentRuntime) RuntimeHealth(ctx context.Context) (map[string]any, error)

// Shutdown 关闭运行时
func (r *BrowserAgentRuntime) Shutdown(ctx context.Context) error
```

**关键实现细节：**

1. **MCP 工具解析**：`_getPlaywrightMCPTool` 方法通过 Runner.resource_mgr 查找注册的 Playwright MCP 工具，支持 server_id/server_name 多候选匹配
2. **代码执行器**：`_callPlaywrightRunCodeUnsafe` 通过 MCP 的 `browser_run_code_unsafe`/`browser_run_code` 工具执行 JavaScript
3. **MCP 文本结果解包**：`_unwrapMCPTextResult` 从 MCP 工具结果的 content/data/text 字段提取文本

**占位标记：**

```go
// TODO: ⤵️ 9.38-49 回填 ensure_browser_runtime_client_patch
// Python 侧此函数修补 MCP 客户端以支持 Playwright 特有的结果格式
```

### 2.5 browser_rail.go（~350 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/runtime.py:539-808`

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// BrowserRuntimeRail 浏览器运行时 Rail，实现可恢复性和完成感知
// 对齐 Python: BrowserRuntimeRail
type BrowserRuntimeRail struct {
    runtime *BrowserAgentRuntime
    // ...
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserRuntimeRail 创建浏览器运行时 Rail
func NewBrowserRuntimeRail(runtime *BrowserAgentRuntime) *BrowserRuntimeRail

// AgentRail 接口实现（10 个生命周期钩子）
func (r *BrowserRuntimeRail) Priority() int
func (r *BrowserRuntimeRail) Init(agent BaseAgent) error
func (r *BrowserRuntimeRail) Uninit(agent BaseAgent) error
func (r *BrowserRuntimeRail) BeforeInvoke(ctx context.Context, cbc *AgentCallbackContext) error
func (r *BrowserRuntimeRail) AfterInvoke(ctx context.Context, cbc *AgentCallbackContext) error
func (r *BrowserRuntimeRail) BeforeModelCall(ctx context.Context, cbc *AgentCallbackContext) error
func (r *BrowserRuntimeRail) AfterModelCall(ctx context.Context, cbc *AgentCallbackContext) error
func (r *BrowserRuntimeRail) BeforeToolCall(ctx context.Context, cbc *AgentCallbackContext) error
func (r *BrowserRuntimeRail) AfterToolCall(ctx context.Context, cbc *AgentCallbackContext) error
func (r *BrowserRuntimeRail) OnToolException(ctx context.Context, cbc *AgentCallbackContext) error
func (r *BrowserRuntimeRail) OnModelException(ctx context.Context, cbc *AgentCallbackContext) error
```

**关键实现细节：**

1. **BeforeInvoke**：调用 `runtime.EnsureRuntimeReady()`；注入 MCP ability 到 agent；从 session 恢复进度状态；记录任务文本
2. **BeforeModelCall**：注入 `browser_progress_format` PromptSection（指导 Agent 输出 `<browser_progress>` 标签）；注入 `browser_progress_continuation` PromptSection（包含已存储的进度上下文）
3. **AfterToolCall**：对 browser_ 前缀的工具调用记录进度到 BrowserService
4. **AfterInvoke**：提取 `<browser_progress>` 标签中的进度 payload；判断完成/失败；构建失败摘要；持久化进度到 session

**进度标签正则表达式（对齐 Python）：**

```go
var browserProgressTagRE = regexp.MustCompile(
    `<browser_progress>\s*(\{.*?\})\s*</browser_progress>`,
)
```

**进度状态键名（对齐 Python）：**

```go
const (
    browserProgressStateKey      = "__browser_subagent_progress_state__"
    browserProgressTaskKey       = "__browser_subagent_last_task__"
    browserProgressSectionName   = "browser_progress_continuation"
    browserProgressFormatSectionName = "browser_progress_format"
)
```

### 2.6 runtime_tools.go（~350 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/runtime_tools.py` 全文

**7 个 Tool 实现：**

| Tool 名称 | Go 类型 | 职责 |
|-----------|--------|------|
| `browser_cancel_run` | `BrowserCancelTool` | 取消正在进行的浏览器任务 |
| `browser_clear_cancel` | `BrowserClearCancelTool` | 清除取消标记 |
| `browser_custom_action` | `BrowserCustomActionTool` | 运行自定义浏览器动作 |
| `browser_list_custom_actions` | `BrowserListActionsTool` | 列出可用自定义动作 |
| `browser_probe_interactives` | `BrowserProbeInteractivesTool` | 探测页面交互元素 |
| `browser_probe_cards` | `BrowserProbeCardsTool` | 探测页面卡片结构 |
| `browser_runtime_health` | `BrowserRuntimeHealthTool` | 返回运行时健康状态 |

**工厂函数：**

```go
// BuildBrowserRuntimeTools 构建 browser 运行时辅助工具列表
// 对齐 Python: build_browser_runtime_tools
func BuildBrowserRuntimeTools(runtime *BrowserAgentRuntime, language string) []tool.Tool
```

**每个 Tool 实现方式：**

- 嵌入 `ToolCard`（通过 `tool.NewToolFunc` 或自定义 struct 实现 `tool.Tool` 接口）
- `Invoke` 方法调用 `BrowserAgentRuntime` 的对应方法
- `Stream` 返回 `ErrStreamNotSupported`
- 描述文本和参数 schema 一比一复刻 Python 中的 `_XXX_DESC` 和 `_XXX_PARAMS`

---

## Layer 3 — 探测与控制层（~1350 行 Go）

### 3.1 probes.go（~600 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/probes.py` 全文

**内容：**

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// BuildInteractiveProbeJS 生成交互元素探测 JavaScript 代码
// 对齐 Python: build_interactive_probe_js
func BuildInteractiveProbeJS(maxItems int, viewportOnly bool, query string) string

// BuildCardProbeJS 生成卡片结构探测 JavaScript 代码
// 对齐 Python: build_card_probe_js
func BuildCardProbeJS(maxCards int, viewportOnly bool, includeButtons bool, query string, siteProfiles []SiteProfile, selectorCacheRecords []SelectorCacheRecord) string
```

**关键实现细节：**

- JavaScript 代码以 Go raw string literal 存储，逻辑一比一复刻 Python 的 `build_interactive_probe_js` 和 `build_card_probe_js`
- 交互元素探测：查找 a/button/input/select/textarea 等元素，提取 role/text/aria-label/testid/bbox/selector_hint
- 卡片结构探测：识别重复的卡片容器（通过 getBoundingClientRect 聚类），提取 title/price/rating/review/availability/link/buttons/bbox/selector_hint

### 3.2 controllers.go（~400 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/controllers/base.py` + `controllers/action.py`

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// BaseController 动作控制器基接口
// 对齐 Python: BaseController
type BaseController interface {
    BindRuntime(runtime any)
    BindRuntimeRunner(runner any)
    ClearRuntimeRunner()
    BindCodeExecutor(executor CodeExecutorFunc)
    ClearCodeExecutor()
    RegisterAction(name string, handler ActionHandlerFunc, overwrite bool)
    RegisterActionSpec(name string, summary string, whenToUse string, params map[string]string)
    ListActions() []string
    DescribeActions() map[string]map[string]any
    RunAction(ctx context.Context, action string, sessionID string, requestID string, kwargs map[string]any) (map[string]any, error)
}

// ActionController 动作控制器实现
// 对齐 Python: ActionController
type ActionController struct {
    // ... 字段
}

// CodeExecutorFunc 代码执行器函数类型
type CodeExecutorFunc func(ctx context.Context, jsCode string) (any, error)

// ActionHandlerFunc 动作处理函数类型
type ActionHandlerFunc func(ctx context.Context, kwargs map[string]any) (map[string]any, error)
```

**占位标记：**

```go
// TODO: ⤵️ 9.38-49 回填 builtin actions 执行逻辑
// ActionController.RegisterBuiltinActions 注册 drag_drop/resolve_coordinates 等内置动作
// 具体动作的执行逻辑依赖 Playwright code executor，待 9.38-49 实现后回填
```

### 3.3 agents.go（~200 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/agents.py`

**内容：**

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBrowserWorkerAgent 构建浏览器 Worker Agent
// 对齐 Python: build_browser_worker_agent
func BuildBrowserWorkerAgent(provider, apiKey, apiBase, modelName string, mcpCfg *mcptypes.McpServerConfig, maxSteps int, screenshotSubdir string, artifactsSubdir string, toolResultObserver ToolResultObserverFunc) (*react_agent.ReActAgent, error)

// BuildBrowserWorkerSystemPrompt 构建浏览器 Worker 系统提示词
// 对齐 Python: build_browser_worker_system_prompt
func BuildBrowserWorkerSystemPrompt(screenshotSubdir string, artifactsSubdir string) string

// ToolResultObserverFunc 工具结果观察者函数类型
type ToolResultObserverFunc func(toolName string, toolResult any)
```

**关键实现细节：**

- Worker Agent 是一个独立的 ReActAgent，配备 Playwright MCP 工具和运行时辅助工具
- System prompt 一比一复刻 Python `build_browser_worker_system_prompt()`
- Temperature/top_p 从环境变量读取，默认 0.2/0.1

### 3.4 profiles.go（~150 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/profiles.py`

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// BrowserProfile 浏览器配置文件元数据
// 对齐 Python: BrowserProfile
type BrowserProfile struct {
    Name           string   `json:"name"`
    DriverType     string   `json:"driver_type"`
    CDPURL         string   `json:"cdp_url"`
    BrowserBinary  string   `json:"browser_binary"`
    UserDataDir    string   `json:"user_data_dir"`
    DebugPort      int      `json:"debug_port"`
    Host           string   `json:"host"`
    ExtraArgs      []string `json:"extra_args"`
}

// BrowserProfileStore JSON 文件存储的配置文件仓库
// 对齐 Python: BrowserProfileStore
type BrowserProfileStore struct {
    // ...
}
```

### 3.5 site_profiles.go（~200 行）

**Python 参考：** `openjiuwen/harness/tools/browser_move/playwright_runtime/site_profiles.py`

**内容：**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// SiteProfile 站点配置文件
type SiteProfile struct {
    Domain         string            `json:"domain"`
    CardSelector   string            `json:"card_selector"`
    TitleSelector  string            `json:"title_selector"`
    PriceSelector  string            `json:"price_selector"`
    // ...
}

// SelectorCache 选择器缓存
type SelectorCache struct {
    // ...
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuiltinSiteProfiles 返回内置站点配置
func BuiltinSiteProfiles() []SiteProfile

// GetSelectorCache 返回选择器缓存实例
func GetSelectorCache() *SelectorCache
```

---

## 占位与回填标记汇总

| 占位内容 | 所在文件 | 回填章节 |
|----------|---------|---------|
| ManagedBrowserDriver 启动/停止/健康检查 | `service.go` | `⤵️ 9.38-49` |
| BrowserService._check_connection 中 MCP client ping | `service.go` | `⤵️ 9.38-49` |
| ensure_browser_runtime_client_patch() | `runtime.go` | `⤵️ 9.38-49` |
| ActionController builtin actions 执行逻辑（drag_drop/resolve_coordinates） | `controllers.go` | `⤵️ 9.38-49` |
| Runner.resource_mgr.add_mcp_server / get_mcp_tool 调用 | `service.go`, `runtime.go` | 使用现有 Go 侧 Runner API |
| BrowserService.browser_agent (ReActAgent) setter | `service.go` | 依赖 `agents.go` Layer 3 |

## IMPLEMENTATION_PLAN.md 更新

完成后将 9.26 的 `🔄` 改为 `✅`，同时 `⤵️ 9.38-49` 标记保留在代码注释中，表明运行时工具的底层实现仍需回填。

## 测试策略

### Layer 1 测试

- `browser_agent_test.go`：测试 BuildBrowserAgentConfig 的默认值、参数覆盖、语言选择
- `config_test.go`：测试 BuildBrowserGuardrails/BuildPlaywrightMCPConfig/ResolveRuntimeSettings
- `env_test.go`：测试环境变量解析函数
- `parsing_test.go`：测试 ExtractJSONObject/SanitizeJSONSchema

### Layer 2 测试

- `progress_test.go`：测试 BrowserTaskProgressState 的 from_dict/to_dict/is_empty
- `service_test.go`：测试 BrowserService 的会话管理、取消、进度状态、失败摘要构建（MCP 连接部分用 mock）
- `runtime_test.go`：测试 BrowserAgentRuntime 的 MCP 工具解析、代码执行（mock code executor）
- `browser_rail_test.go`：测试 BrowserRuntimeRail 的 BeforeInvoke/AfterInvoke 进度追踪
- `runtime_tools_test.go`：测试 7 个运行时工具的 Invoke 方法（mock runtime）

### Layer 3 测试

- `probes_test.go`：测试 JavaScript 探测代码生成
- `controllers_test.go`：测试动作注册/调度
- `agents_test.go`：测试 Worker Agent 构建
- `profiles_test.go`：测试 BrowserProfileStore

### Build Tag

依赖真实浏览器环境的集成测试使用 `//go:build llm` 标签隔离。
