package memory

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemoryManagerParams 记忆管理器构造参数。
// 对齐 Python TeamMemoryManagerParams (manager_params.py)
type TeamMemoryManagerParams struct {
	// MemberName 成员名称
	MemberName string
	// TeamName 团队名称
	TeamName string
	// Role 角色
	Role TeamRole
	// Lifecycle 生命周期
	Lifecycle TeamLifecycle
	// Scenario 场景
	Scenario TeamScenario
	// EmbeddingConfig 嵌入配置
	EmbeddingConfig *embedding.EmbeddingConfig
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// SysOperation 系统操作接口
	SysOperation sysop.SysOperation
	// TeamMemoryDir 团队记忆目录路径
	TeamMemoryDir *string
	// Language 语言
	Language TeamLanguage
	// PromptMode 提示模式
	PromptMode PromptMode
	// EnableAutoExtract 是否自动提取记忆
	EnableAutoExtract bool
	// SharedMemory 是否启用共享记忆
	SharedMemory bool
	// ReadOnlySourceWorkspace 只读来源工作空间路径
	ReadOnlySourceWorkspace *string
	// DB 团队数据库。⤵️ 回填: 9.65a
	DB database.TeamDatabase
	// TaskManager 任务管理器。⤵️ 回填: 9.65a
	TaskManager *tools.TeamTaskManager
	// ExtractionModel 提取模型。⤵️ 回填: 9.65a
	ExtractionModel any
	// TimezoneOffsetHours 时区偏移小时数
	TimezoneOffsetHours float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamRole 团队角色类型别名
type TeamRole string

// TeamLifecycle 团队生命周期类型别名
type TeamLifecycle string

// TeamScenario 团队场景类型别名
type TeamScenario string

// TeamLanguage 团队语言类型别名
type TeamLanguage string

// PromptMode 提示模式类型别名
type PromptMode string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamRoleLeader Leader 角色
	TeamRoleLeader TeamRole = "leader"
	// TeamRoleTeammate Teammate 角色
	TeamRoleTeammate TeamRole = "teammate"
	// TeamLifecycleTemporary 临时生命周期
	TeamLifecycleTemporary TeamLifecycle = "temporary"
	// TeamLifecyclePersistent 持久生命周期
	TeamLifecyclePersistent TeamLifecycle = "persistent"
	// TeamScenarioGeneral 通用场景
	TeamScenarioGeneral TeamScenario = "general"
	// TeamScenarioCoding 编程场景
	TeamScenarioCoding TeamScenario = "coding"
	// TeamLanguageCN 中文
	TeamLanguageCN TeamLanguage = "cn"
	// TeamLanguageEN 英文
	TeamLanguageEN TeamLanguage = "en"
	// PromptModeProactive 主动模式
	PromptModeProactive PromptMode = "proactive"
	// PromptModePassive 被动模式
	PromptModePassive PromptMode = "passive"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
