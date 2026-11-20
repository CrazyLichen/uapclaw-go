package subagents

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hcua "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cua"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CuaAgentFactoryName cua 桌面子代理工厂注册名。
	// Python: CUA_AGENT_FACTORY_NAME (harness/subagents/cua_agent.py:56)
	CuaAgentFactoryName = "cua_agent"

	// cuaToolPrefix cua 工具按裸名注册，rail 以空前缀匹配。
	// Python: _CUA_TOOL_PREFIX (cua_agent.py:59)
	cuaToolPrefix = ""

	// cuaOffloadPreviewChars 被卸载截图的内联占位字符数。
	// 占位符同时携带卸载句柄，完整结果仍可达；窗口头是旧截图的全部价值。
	// Python: _CUA_OFFLOAD_PREVIEW_CHARS (cua_agent.py:63-65)
	cuaOffloadPreviewChars = 200
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 以下提示词/描述文案一比一复刻 Python 原文（规则：提示词逐字抄，不翻译不改写）。

// cuaPromptEN 英文默认系统提示词。
// Python: _CUA_PROMPT_EN (cua_agent.py:67-101)
const cuaPromptEN = "You are a desktop automation agent that operates the host computer through screenshots and real " +
	"mouse and keyboard input. Plan and decide at this agent level, then observe and act on real " +
	"application windows. " +
	"Perception: start with list_windows (or list_apps) to find the target pid and window_id, then call " +
	"get_window_state(pid, window_id): it brings that window to the front and returns its screenshot. " +
	"Every x,y you pass to click, double_click, right_click, move_cursor, drag, scroll or type_text for " +
	"that window is a pixel in its latest screenshot, measured from the screenshot's top-left corner. " +
	"Use zoom to read small text or icons, then pass from_zoom=true with x,y read off the zoom image. " +
	"A window whose x,y is near -32000 is minimized; bring_to_front before capturing it. " +
	"When you act by x,y coordinates read off a screenshot, treat the target region's edges as " +
	"unreliable: your visual estimate of a boundary can be off by tens of pixels, and input that " +
	"starts outside the real region usually fails silently. Start clicks and drags well inside " +
	"the region (30+ px from every estimated edge), and read a silent no-effect result as a " +
	"likely aim miss. " +
	"Your input is real: it moves the user's mouse and types into whatever has keyboard focus, so " +
	"every window-scoped action brings its window to the front first. Do not touch windows the task " +
	"does not need. " +
	"Verification: input actions are not self-verifying. After clicks, keys, or typed text, capture " +
	"the window again with get_window_state and check the effect before claiming progress. " +
	"Prefer launch_app to start applications. " +
	"Browser tasks are not yours: web page automation belongs to the browser agent — report back " +
	"instead of driving a browser through desktop input. This means an actual web browser such as " +
	"Chrome, Edge, or Firefox. Electron desktop applications — Discord, Slack, VS Code, Spotify " +
	"and the like — are ordinary native application windows even though Chromium renders them, " +
	"and driving them IS your job; do not hand them to the browser agent, which cannot reach " +
	"them. " +
	"Avoid redundant actions, and only claim completion when the requested desktop outcome is " +
	"actually evidenced on screen."

// cuaPromptCN 中文默认系统提示词。
// Python: _CUA_PROMPT_CN (cua_agent.py:103-131)
const cuaPromptCN = "你是桌面自动化代理，通过截图和真实的鼠标键盘输入操作本机计算机。" +
	"请在当前代理层面规划和决策，然后基于真实应用窗口进行观察和操作。" +
	"感知：先用 list_windows（或 list_apps）找到目标 pid 和 window_id，" +
	"再调用 get_window_state(pid, window_id)：它会把该窗口置于前台并返回其截图。" +
	"你对该窗口调用 click、double_click、right_click、move_cursor、drag、scroll 或 type_text 时" +
	"传入的每个 x,y 都是其最新截图中的像素，从截图左上角起算。" +
	"需要看清小字或图标时用 zoom，之后传 from_zoom=true 并使用从缩放图上读取的 x,y。" +
	"x,y 接近 -32000 的窗口是被最小化了；截图前先调用 bring_to_front。" +
	"通过截图读取 x,y 坐标操作时，把目标区域的边缘当作不可靠信息：你对边界的视觉估计" +
	"可能偏差数十像素，而起点落在真实区域之外的输入通常会静默失效。" +
	"点击和拖拽的起点应落在区域内部足够深处（离每条估计边缘至少 30 像素）；" +
	"操作后毫无效果时，优先怀疑是瞄准偏差。" +
	"你的输入是真实的：它会移动用户的鼠标，并向当前拥有键盘焦点的位置输入，" +
	"所以每个针对窗口的操作都会先把该窗口置于前台。不要触碰任务不需要的窗口。" +
	"验证：输入类操作不会自我验证。点击、按键或输入文本后，" +
	"必须再次调用 get_window_state 截图并确认效果，然后才能声明进展。" +
	"启动应用优先使用 launch_app。" +
	"浏览器任务不属于你：网页自动化由浏览器代理负责——遇到此类任务应如实汇报，" +
	"而不是通过桌面输入去驱动浏览器。这里指的是真正的浏览器，例如 Chrome、Edge、Firefox。" +
	"Discord、Slack、VS Code、Spotify 等 Electron 桌面应用虽然由 Chromium 渲染，" +
	"但它们就是普通的原生应用窗口，操作它们正是你的职责；" +
	"不要把它们交给浏览器代理，浏览器代理无法访问这些窗口。" +
	"避免重复动作；只有屏幕上有具体证据证明任务完成时，才声明完成。"

// DefaultCuaAgentSystemPrompt 默认系统提示词双语文案。
// Python: DEFAULT_CUA_AGENT_SYSTEM_PROMPT (cua_agent.py:134-137)
var DefaultCuaAgentSystemPrompt = map[string]string{
	"cn": cuaPromptCN,
	"en": cuaPromptEN,
}

// CuaKillAppPromptSuffix 仅当 app_termination 能力授予 kill_app 时追加
// （默认提示词绝不教 agent 使用未被授予的工具）。
// Python: CUA_KILL_APP_PROMPT_SUFFIX (cua_agent.py:140-143)
var CuaKillAppPromptSuffix = map[string]string{
	"cn": "kill_app 只在协作式关闭失败后使用，因为未保存状态会丢失。",
	"en": " Use kill_app only after the cooperative close path failed, since unsaved state is lost.",
}

// CuaPermissionRules 合入每个 cua agent 自身权限策略的桌面规则。
//
// 不是包内置规则：工具名是裸名，其他 agent 的同名工具（mobile_gui 的
// type_text）不得继承。作为 user 层规则，DENY 仍压过任何调用方 allow、
// 默认值或审批覆盖。
// Python: CUA_PERMISSION_RULES (cua_agent.py:147-171)
var CuaPermissionRules = []map[string]any{
	{
		"id":          "cua_desktop_force_terminate_app",
		"description": "强制终止桌面进程（kill_app），未保存的数据会丢失",
		"tools":       []string{"kill_app"},
		"match_type":  "os_control",
		"pattern":     "*",
		"severity":    "CRITICAL",
		"action":      "deny",
	},
	{
		"id":          "cua_desktop_launch_app",
		"description": "启动桌面应用程序（launch_app），可能运行任意本机程序",
		"tools":       []string{"launch_app"},
		"match_type":  "os_control",
		"pattern":     "*",
		"severity":    "HIGH",
		"action":      "ask",
	},
	{
		"id":          "cua_desktop_type_text",
		"description": "向桌面窗口注入文本输入（type_text），可能写入密码或其他敏感字段",
		"tools":       []string{"type_text"},
		"match_type":  "os_control",
		"pattern":     "*",
		"severity":    "HIGH",
		"action":      "ask",
	},
}

// 默认描述双语文案。
// Python: DEFAULT_CUA_AGENT_DESCRIPTION_* (cua_agent.py:174-182)
const cuaDescriptionEN = "Dedicated desktop subagent that controls host applications through screenshots and real mouse and " +
	"keyboard input; requires a vision-capable model."
const cuaDescriptionCN = "专用桌面子代理，通过截图和真实的鼠标键盘输入操作本机应用程序；需要具备视觉能力的模型。"

// DefaultCuaAgentDescription 默认描述双语文案表。
// Python: DEFAULT_CUA_AGENT_DESCRIPTION (cua_agent.py:183-186)
var DefaultCuaAgentDescription = map[string]string{
	"cn": cuaDescriptionCN,
	"en": cuaDescriptionEN,
}

// ──────────────────────────── 结构体 ────────────────────────────

// CuaAgentConfigParams BuildCuaAgentConfig 的具名参数（对齐 Python
// build_cua_agent_config 的 keyword-only 参数列表，cua_agent.py:220-248）。
type CuaAgentConfigParams struct {
	// Card Agent 身份卡片（nil 时用默认 cua_agent 卡片）
	Card *agentschema.AgentCard
	// SystemPrompt 系统提示词（空时用默认）
	SystemPrompt string
	// Tools 工具卡片列表
	Tools []*tool.ToolCard
	// Mcps MCP 服务器配置列表
	Mcps []*mcptypes.McpServerConfig
	// Rails rail 列表
	Rails []sainterfaces.AgentRail
	// EnableTaskLoop 是否启用任务循环
	EnableTaskLoop bool
	// MaxIterations 最大迭代次数
	MaxIterations int
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// Skills 技能名称列表
	Skills []string
	// Backend 后端协议实例
	Backend any
	// SysOperation 系统操作实例
	SysOperation sysop.SysOperation
	// Language 语言偏好
	Language string
	// PromptMode 提示词模式
	PromptMode hschema.PromptMode
	// CuaCapabilities 能力选择（nil=默认授权）
	CuaCapabilities []string
	// CuaInstanceKey 实例键（仅 API 稳定性保留，无实际效果：
	// 每个 agent 拿到自己的工具实例，按 card id 限定注册）
	CuaInstanceKey string
	// CuaSnapshotKeepLastK 保留在上下文里的最近截图数（默认 3）
	CuaSnapshotKeepLastK int
	// CuaPauseOnUserInput 桌面操作在用户使用机器时挂起（默认关）
	CuaPauseOnUserInput bool
	// Permissions 权限策略（启用工具权限引擎；nil=无权限引擎）
	Permissions map[string]any
	// PermissionHost 权限确认宿主（ASK 策略需配对）
	PermissionHost any
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCuaSystemPrompt 默认系统提示词（按已允许工具名追加 kill_app 后缀）。
// Python: _default_system_prompt (cua_agent.py:189-194)
func defaultCuaSystemPrompt(language string, allowedToolNames []string) string {
	prompt, ok := DefaultCuaAgentSystemPrompt[language]
	if !ok || prompt == "" {
		prompt = DefaultCuaAgentSystemPrompt["cn"]
	}
	if containsStr(allowedToolNames, "kill_app") {
		suffix, ok := CuaKillAppPromptSuffix[language]
		if !ok || suffix == "" {
			suffix = CuaKillAppPromptSuffix["cn"]
		}
		prompt += suffix
	}
	return prompt
}

// WithCuaPermissionRules 调用方策略加桌面规则；调用方的 map 不被修改。
// 消费方是 create_cua_agent（物化半区 ⤴️ 领域十八）：
// `config_kwargs["permissions"] = _with_cua_permission_rules(...)`（:317-319）。
// Python: _with_cua_permission_rules (cua_agent.py:174-178)——Python 私有
// 但随 __all__ 的 CUA_PERMISSION_RULES 同域；Go 导出以供 create 半章复用。
func WithCuaPermissionRules(permissions map[string]any) map[string]any {
	merged := deepCopyMap(permissions)
	rawRules, ok := merged["rules"]
	var rules []map[string]any
	if ok {
		if typed, isSlice := rawRules.([]map[string]any); isSlice {
			rules = append(rules, typed...)
		}
	}
	for _, rule := range CuaPermissionRules {
		rules = append(rules, deepCopyMap(rule))
	}
	merged["rules"] = rules
	return merged
}

// deepCopyMap 深拷贝 string→any map（一层值递归）。
// Python: copy.deepcopy——本仓 map 值域为标量/字符串列表/嵌套 map，逐层拷贝。
func deepCopyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue 递归拷贝已知容器形态。
func deepCopyValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return deepCopyMap(typed)
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for i, item := range typed {
			out[i] = deepCopyMap(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = deepCopyValue(item)
		}
		return out
	default:
		return v
	}
}

// resolveCapabilitySelection 校验能力选择（对照受信任目录）。
// Python: _resolve_capability_selection (cua_agent.py:189-205)
func resolveCapabilitySelection(cuaCapabilities []string) (*hcua.ResolvedCuaCapabilities, error) {
	requested := cuaCapabilities
	if cuaCapabilities == nil {
		requested = hcua.DefaultCuaAgentCapabilityNames
	}
	resolved, err := hcua.ResolveCuaCapabilities(requested, hcua.DefaultCuaCapabilities)
	if err != nil {
		return nil, err
	}
	if len(resolved.RejectedNames) > 0 {
		rejected := strings.Join(resolved.RejectedNames, ", ")
		available := make([]string, 0, len(hcua.DefaultCuaCapabilities))
		for _, capability := range hcua.DefaultCuaCapabilities {
			available = append(available, capability.Name)
		}
		return nil, fmt.Errorf("Unsupported cua capabilities: %s. Available capabilities: %s",
			rejected, strings.Join(available, ", "))
	}
	return resolved, nil
}

// validateSnapshotKeepLastK 在配置期拒绝不可用的保留数（而非首次工具调用）。
// Python: _validate_snapshot_keep_last_k (cua_agent.py:208-215)：
// 必须是 int >= 1（bool 不是 int——Go 无该歧义，仅校验 >= 1）。
func validateSnapshotKeepLastK(cuaSnapshotKeepLastK int) error {
	if cuaSnapshotKeepLastK < 1 {
		return fmt.Errorf("cua_snapshot_keep_last_k must be an int >= 1, got %d", cuaSnapshotKeepLastK)
	}
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildCuaAgentConfig 构造物化为 CreateCuaAgent() 的 SubAgentConfig（配置半区）。
//
// Permissions/PermissionHost（可选）在物化后的子代理上启用工具权限引擎。
// task-tool 委派内建 ASK 中断无法到达外层会话，因此任何可能产生 ASK 的
// 策略都要配对 ToolPermissionHost.request_permission_confirmation 钩子
// 进程内作答；DENY 规则不需要宿主。
//
// Deviation D-15.16f-cua-create：create_cua_agent 物化半区（桌面工具构建 +
// CuaRuntimeRail 等五 rail 注入）依赖 harness/tools/cua 工具链
// （desktop_tools/backend/rails 2299 行）⤴️ 领域十八，本批不含——
// 本函数产出的 SubAgentConfig 持 factory_name="cua_agent"，物化在该章
// 回收后经 subagent 工厂分派生效。
//
// Python: build_cua_agent_config (harness/subagents/cua_agent.py:220-290)
func BuildCuaAgentConfig(model *llm.Model, params *CuaAgentConfigParams) (*hschema.SubAgentConfig, error) {
	if params == nil {
		params = &CuaAgentConfigParams{}
	}
	resolvedLanguage := hprompts.ResolveLanguage(params.Language)
	resolvedCapabilities, err := resolveCapabilitySelection(params.CuaCapabilities)
	if err != nil {
		return nil, err
	}
	// Deviation D-15.16f-cua-zero：Go 无 keyword 默认值，params 零值 0 与
	// Python 默认 3 无法区分；0 本身非法（<1），按「未传」语义兜底为 3。
	// Python: cua_snapshot_keep_last_k: int = 3（:243）
	snapshotKeepLastK := params.CuaSnapshotKeepLastK
	if snapshotKeepLastK == 0 {
		snapshotKeepLastK = 3
	}
	if err := validateSnapshotKeepLastK(snapshotKeepLastK); err != nil {
		return nil, err
	}

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认（card or AgentCard(name="cua_agent", ...)）
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := DefaultCuaAgentDescription[resolvedLanguage]
		if desc == "" {
			desc = DefaultCuaAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(CuaAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时用默认（按 allowed tool names 追加 kill_app 后缀）
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaultCuaSystemPrompt(resolvedLanguage, resolvedCapabilities.AllowedToolNames)
	}

	cfg.Tools = params.Tools
	cfg.Mcps = params.Mcps
	cfg.Model = model
	cfg.Rails = params.Rails
	cfg.Skills = params.Skills
	cfg.Backend = params.Backend
	cfg.Workspace = params.Workspace
	cfg.SysOperation = params.SysOperation
	cfg.Language = resolvedLanguage
	cfg.PromptMode = params.PromptMode
	cfg.EnableTaskLoop = params.EnableTaskLoop
	if params.MaxIterations > 0 {
		cfg.MaxIterations = params.MaxIterations
	} else {
		// Python: max_iterations=25（keyword 默认值）
		cfg.MaxIterations = 25
	}
	cfg.FactoryName = CuaAgentFactoryName

	// FactoryKwargs：仅在被提供时才带 permissions/permission_host 键——
	// 旧配置保持精确的 factory_kwargs 形状，默认值归 DeepAgentConfig。
	// Python: factory_kwargs={...}（:268-283）
	factoryKwargs := map[string]any{
		"cua_capabilities":         append([]string(nil), params.CuaCapabilities...), // nil 保持 nil
		"cua_instance_key":         params.CuaInstanceKey,
		"cua_snapshot_keep_last_k": snapshotKeepLastK,
		"cua_pause_on_user_input":  params.CuaPauseOnUserInput,
	}
	if params.Permissions != nil {
		factoryKwargs["permissions"] = params.Permissions
	}
	if params.PermissionHost != nil {
		factoryKwargs["permission_host"] = params.PermissionHost
	}
	cfg.FactoryKwargs = factoryKwargs

	return cfg, nil
}

// containsStr 判断列表是否含目标串。
func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
