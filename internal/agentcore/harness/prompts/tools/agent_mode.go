package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// switchModeDescription switch_mode 工具双语描述
var switchModeDescription = map[string]string{
	"cn": `在 normal 与 plan 模式间切换当前会话模式。

何时使用：
- 用户明确要求只做规划、不做实现时，（e.g.切到 plan 模式）。
- 你判断当前模式不适合该任务。
- 任务的复杂度或需求发生显著变化。

模式说明：
- plan：规划优先。除 plan 文件外仅允许只读操作。
- normal：完整的开发权限，可修改文件并执行命令。

注意：
- 在意图不明确时先用 ask_user 澄清，再切换模式。`,
	"en": `Switch the current session between normal and plan modes.

When to use:
- Switch to plan when the user explicitly wants planning only and no implementation.
- You determine the current mode is inappropriate for the task
- A task's complexity or requirements have changed significantly.

Mode characteristics:
- plan: Structured planning before execution, read-only with plan file writing only.
- normal: Full development actions are allowed (editing files, running commands, etc.).

Note:
- If intent is ambiguous, call ask_user first before switching mode.`,
}

// enterPlanModeDescription enter_plan_mode 工具双语描述
var enterPlanModeDescription = map[string]string{
	"cn": "初始化 plan 文件并返回文件路径。在 plan 模式下，这必须是你的第一个操作。该工具会创建一个新的 plan 文件（幂等：若已存在则直接返回路径）。",
	"en": "Initialize the plan file and return its path. In plan mode this must be your very first action. Creates a new plan file (idempotent: returns the existing path if already created).",
}

// exitPlanModeDescription exit_plan_mode 工具双语描述
var exitPlanModeDescription = map[string]string{
	"cn": "读取 plan 文件全文并直接返回给用户，结束规划阶段，请求用户审批是否要切换到 normal 模式执行。当你对最终 plan 文件满意时，必须调用此工具结束规划阶段。tool_result 中包含完整计划内容。",
	"en": "Read the full plan file and return the plan directly, ending the planning phase. Request user approval before switching to normal mode for execution. Call this when you are satisfied with the final plan. The tool result contains the complete plan content.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// SwitchModeMetadataProvider switch_mode 工具元数据提供者
type SwitchModeMetadataProvider struct{}

// EnterPlanModeMetadataProvider enter_plan_mode 工具元数据提供者
type EnterPlanModeMetadataProvider struct{}

// ExitPlanModeMetadataProvider exit_plan_mode 工具元数据提供者
type ExitPlanModeMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSwitchModeMetadataProviderInputParams 构建 switch_mode 工具的参数 Schema
func GetSwitchModeMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"mode": {"cn": "目标模式：normal 或 plan", "en": "Target mode: normal or plan"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{"type": "string", "enum": []any{"normal", "plan"}, "description": d("mode")},
		},
		"required": []any{"mode"},
	}
}

// GetEnterPlanModeMetadataProviderInputParams 构建 enter_plan_mode 工具的参数 Schema
func GetEnterPlanModeMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	_ = lang // 无参数工具，language 保留以备扩展
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

// GetExitPlanModeMetadataProviderInputParams 构建 exit_plan_mode 工具的参数 Schema
func GetExitPlanModeMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	_ = lang // 无参数工具，language 保留以备扩展
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

func (p *SwitchModeMetadataProvider) GetName() string { return "switch_mode" }
func (p *SwitchModeMetadataProvider) GetDescription(language string) string {
	if d, ok := switchModeDescription[language]; ok {
		return d
	}
	return switchModeDescription["cn"]
}
func (p *SwitchModeMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSwitchModeMetadataProviderInputParams(language)
}

func (p *EnterPlanModeMetadataProvider) GetName() string { return "enter_plan_mode" }
func (p *EnterPlanModeMetadataProvider) GetDescription(language string) string {
	if d, ok := enterPlanModeDescription[language]; ok {
		return d
	}
	return enterPlanModeDescription["cn"]
}
func (p *EnterPlanModeMetadataProvider) GetInputParams(language string) map[string]any {
	return GetEnterPlanModeMetadataProviderInputParams(language)
}

func (p *ExitPlanModeMetadataProvider) GetName() string { return "exit_plan_mode" }
func (p *ExitPlanModeMetadataProvider) GetDescription(language string) string {
	if d, ok := exitPlanModeDescription[language]; ok {
		return d
	}
	return exitPlanModeDescription["cn"]
}
func (p *ExitPlanModeMetadataProvider) GetInputParams(language string) map[string]any {
	return GetExitPlanModeMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&SwitchModeMetadataProvider{})
	RegisterToolProvider(&EnterPlanModeMetadataProvider{})
	RegisterToolProvider(&ExitPlanModeMetadataProvider{})
}
