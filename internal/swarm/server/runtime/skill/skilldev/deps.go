package skilldev

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDevDeps SkillDevService 的全部外部依赖（由 JiuWenClaw 构造并注入）。
//
// 设计原则：SkillDevService 不依赖 JiuWenClaw 实例，
// 只接收以下最小依赖集，由 JiuWenClaw 在初始化时注入。
//
// JiuWenClaw 内部的 SkillManager、EvolutionService、对话历史等
// 对 SkillDev 完全不可见，确保模块边界清晰。
type SkillDevDeps struct {
	// ModelName 模型名称
	ModelName string
	// ModelClientConfig 模型客户端配置
	ModelClientConfig map[string]any

	// MCPToolsFactory 返回当前可用 MCP 工具列表的工厂函数
	MCPToolsFactory func() []any
	// SysOpConfig 文件系统访问配置；nil 表示禁止文件操作
	SysOpConfig any

	// StateStore 任务状态存储
	StateStore *StateStore
	// WorkspaceProvider 工作区管理
	WorkspaceProvider *WorkspaceProvider
}
