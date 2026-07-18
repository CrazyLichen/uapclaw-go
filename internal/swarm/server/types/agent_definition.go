package types

// ──────────────────────────── 结构体 ────────────────────────────

// AgentDefinition Agent 定义数据模型。
// 对齐 Python: jiuwenswarm/server/runtime/agent_config_service.py AgentDefinition dataclass
//
// 管理 Agent 的身份、行为和约束配置。
// 支持 YAML frontmatter + Markdown body 文件格式持久化。
type AgentDefinition struct {
	// Name 名称
	Name string `json:"name" yaml:"name"`
	// Description 描述
	Description string `json:"description" yaml:"description"`
	// Prompt 系统提示词（Markdown body 部分）
	Prompt string `json:"prompt,omitempty" yaml:"-"`
	// Source 来源（"builtin"/"user"/"project"/"local"）
	Source string `json:"source" yaml:"-"`
	// FilePath 文件路径（仅文件来源）
	FilePath string `json:"file_path,omitempty" yaml:"-"`
	// Model 模型名称
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// Tools 允许的工具列表，默认 ["*"]
	Tools []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	// DisallowedTools 禁止的工具列表
	DisallowedTools []string `json:"disallowed_tools,omitempty" yaml:"disallowed_tools,omitempty"`
	// Color 颜色标识
	Color string `json:"color,omitempty" yaml:"color,omitempty"`
	// PermissionMode 权限模式
	PermissionMode string `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
	// MemoryScope 记忆范围
	MemoryScope string `json:"memory_scope,omitempty" yaml:"memory_scope,omitempty"`
	// ShadowedBy 被哪个来源覆盖（空字符串=活跃版本）
	ShadowedBy string `json:"shadowed_by,omitempty" yaml:"-"`
	// Enabled 启用状态（nil=未在config.yaml中配置, true=显式启用, false=显式禁用）
	// 对齐 Python: enabled: bool | None = None
	Enabled *bool `json:"enabled,omitempty" yaml:"-"`
	// WhenToUse 调度描述（告诉 LLM 何时调度此 agent）
	WhenToUse string `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
	// MaxIterations 子 agent 最大迭代次数
	MaxIterations *int `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
	// Skills 子 agent 启动时预加载的 skill 名称
	Skills []string `json:"skills,omitempty" yaml:"skills,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentSource Agent 定义来源常量。
// 对齐 Python: AgentSource = Literal["builtin", "user", "project", "local"]
const (
	// AgentSourceBuiltin 内置
	AgentSourceBuiltin = "builtin"
	// AgentSourceUser 用户级
	AgentSourceUser = "user"
	// AgentSourceProject 项目级
	AgentSourceProject = "project"
	// AgentSourceLocal 本地级
	AgentSourceLocal = "local"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// BuiltinAgents 内置 Agent 定义列表。
// 对齐 Python: BUILTIN_AGENTS
var BuiltinAgents = []*AgentDefinition{
	{
		Name:        "general-purpose",
		Description: "通用多步任务 agent，适用于没有专用 agent 的各类任务",
		Prompt: "你是一个通用任务 agent。使用可用工具完成用户委派的任务。\n\n" +
			"工作原则：\n" +
			"1. 将复杂任务分解为可管理的步骤\n" +
			"2. 在每个步骤完成后汇报进展\n" +
			"3. 遇到阻塞时主动说明需要什么信息",
		Source: AgentSourceBuiltin,
		Tools:  []string{"*"},
	},
	{
		Name:        "Explore",
		Description: "快速只读代码库探索 agent，用于定位代码、搜索符号、查找文件",
		Prompt: "你是代码库探索专家。你的职责是快速定位代码、搜索符号和查找文件。\n\n" +
			"工作原则：\n" +
			"1. 只进行只读操作（搜索、读取、列出文件）\n" +
			"2. 通过多种搜索策略（文件名模式、grep 符号、目录遍历）确保覆盖全面\n" +
			"3. 返回精确的文件路径和行号\n" +
			"4. 当结果过多时，缩小搜索范围而不是截断输出",
		Source: AgentSourceBuiltin,
		Tools:  []string{"Read", "Bash", "Grep", "Glob"},
	},
	{
		Name:        "Plan",
		Description: "软件架构设计 agent，用于规划实现方案",
		Prompt: "你是软件架构师。分析代码库模式和约定，提供完整的实现蓝图。\n\n" +
			"工作原则：\n" +
			"1. 先理解现有代码库的架构模式和约定\n" +
			"2. 设计变更时考虑副作用和依赖关系\n" +
			"3. 输出包含：需要创建/修改的文件、组件设计、数据流和构建顺序\n" +
			"4. 不写实现代码，只提供设计蓝图",
		Source: AgentSourceBuiltin,
		Tools:  []string{"Read", "Bash", "Grep", "Glob"},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
