package skilldev

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDevDeps SkillDevService 的全部外部依赖（由 UapClaw 构造并注入）。
//
// 设计原则：SkillDevService 不依赖 UapClaw 实例，
// 只接收以下最小依赖集，由 UapClaw 在初始化时注入。
//
// UapClaw 内部的 SkillManager、EvolutionService、对话历史等
// 对 SkillDev 完全不可见，确保模块边界清晰。
//
// 对齐 Python: jiuwenswarm/server/runtime/skill/skilldev/deps.py
type SkillDevDeps struct {
	// ModelName 模型名称
	ModelName string
	// ModelClientConfig 模型客户端配置
	ModelClientConfig map[string]any

	// MCPToolsFactory 返回当前可用 MCP 工具列表的工厂函数。
	// 对齐 Python: Callable[[], list[Tool]]
	MCPToolsFactory func() []tool.Tool
	// SysOpConfig 文件系统访问配置；nil 表示禁止文件操作。
	// 对齐 Python: object | None（注释说 SysOperationCard，类型标注松散）
	SysOpConfig *sys_operation.SysOperationCard

	// StateStore 任务状态存储
	StateStore *StateStore
	// WorkspaceProvider 工作区管理
	WorkspaceProvider *WorkspaceProvider
}
