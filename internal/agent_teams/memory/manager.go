package memory

import (
	"context"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const mgrLogComponent = logger.ComponentCommon

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SectionName 系统提示词段名称
	SectionName = "team_memory"
	// maxPersonalMemoryBytes 个人记忆注入最大字节数
	maxPersonalMemoryBytes = 10 * 1024
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemoryManager 团队记忆管理器。
// 对齐 Python TeamMemoryManager (manager.py)
//
// 生命周期：InitToolkit → RegisterTools → LoadAndInject → ExtractAfterRound → Close
type TeamMemoryManager struct {
	// memberName 成员名称
	memberName string
	// teamName 团队名称
	teamName string
	// role 角色
	role TeamRole
	// lifecycle 生命周期
	lifecycle TeamLifecycle
	// scenario 场景
	scenario TeamScenario
	// embeddingConfig 嵌入配置
	embeddingConfig *embedding.EmbeddingConfig
	// language 语言
	language TeamLanguage
	// promptMode 提示模式
	promptMode PromptMode
	// enableAutoExtract 是否自动提取
	enableAutoExtract bool
	// readOnlySource 只读来源工作空间路径
	readOnlySource *string
	// db 团队数据库。⤵️ 回填: 9.65a
	db database.TeamDatabase
	// taskManager 任务管理器。⤵️ 回填: 9.65a
	taskManager tools.TeamTaskManager
	// extractionModel 提取模型。⤵️ 回填: 9.65a
	extractionModel any
	// tzOffset 时区偏移
	tzOffset float64
	// sysOperation 系统操作接口。⤴️ 9.64 具体类型回填
	sysOperation sysop.SysOperation
	// workspace 工作空间
	workspace *workspace.Workspace
	// teamMemoryDir 团队记忆目录路径
	teamMemoryDir *string
	// toolkit 成员记忆工具集。⤵️ 回填: 7.2+7.3
	toolkit *MemberMemoryToolkit
	// ownedToolNames 已注册工具名集合。⤵️ 回填: 7.2
	ownedToolNames map[string]struct{}
	// ownedToolIDs 已注册工具ID集合。⤵️ 回填: 7.2
	ownedToolIDs map[string]struct{}
	// deepAgentForCleanup 用于清理的 DeepAgent。⤵️ 回填: 7.2
	deepAgentForCleanup any
	// sharedManager 共享记忆管理器
	sharedManager *SharedMemoryManager
	// cachedBaseSection 缓存的基础提示词段。⤵️ 回填: 7.2
	cachedBaseSection *saprompt.PromptSection
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMemoryManager 创建团队记忆管理器
func NewTeamMemoryManager(params TeamMemoryManagerParams) *TeamMemoryManager {
	mgr := &TeamMemoryManager{
		memberName:       params.MemberName,
		teamName:         params.TeamName,
		role:             params.Role,
		lifecycle:        params.Lifecycle,
		scenario:         params.Scenario,
		embeddingConfig:  params.EmbeddingConfig,
		language:         params.Language,
		promptMode:       params.PromptMode,
		enableAutoExtract: params.EnableAutoExtract,
		readOnlySource:   params.ReadOnlySourceWorkspace,
		db:               params.DB,
		taskManager:      params.TaskManager,
		extractionModel:  params.ExtractionModel,
		tzOffset:         params.TimezoneOffsetHours,
		sysOperation:     params.SysOperation,
		workspace:        params.Workspace,
		teamMemoryDir:    params.TeamMemoryDir,
		ownedToolNames:   make(map[string]struct{}),
		ownedToolIDs:     make(map[string]struct{}),
	}
	if params.TeamMemoryDir != nil && params.SharedMemory {
		mgr.sharedManager = NewSharedMemoryManager(*params.TeamMemoryDir, params.SysOperation)
	}
	return mgr
}

// InitToolkit 初始化成员记忆工具集。⤵️ 回填: 7.1+7.2+7.3 — 当前返回 false
func (m *TeamMemoryManager) InitToolkit(_ context.Context) (bool, error) {
	return false, nil
}

// RegisterTools 将记忆工具注册到 DeepAgent。⤵️ 回填: 7.2+9.65a — 当前空实现
func (m *TeamMemoryManager) RegisterTools(_ any) {}

// LoadAndInject 加载个人记忆+共享记忆→注入系统提示词。⤵️ 回填: 7.2 — 当前空实现
func (m *TeamMemoryManager) LoadAndInject(_ context.Context, _ any, _ string) error { return nil }

// ExtractAfterRound Leader 专属：提取 agent 蒸馏团队记忆。⤵️ 回填: 7.2+9.65a — 当前空实现
func (m *TeamMemoryManager) ExtractAfterRound(_ context.Context) error { return nil }

// Close 反注册工具+移除提示词段+关闭 toolkit。⤵️ 回填: 7.1+7.2 — 当前空实现
func (m *TeamMemoryManager) Close(_ context.Context) error { return nil }

// ExtractionModel 返回提取模型
func (m *TeamMemoryManager) ExtractionModel() any { return m.extractionModel }

// SetExtractionModel 设置提取模型
func (m *TeamMemoryManager) SetExtractionModel(model any) { m.extractionModel = model }

// SharedManager 返回共享记忆管理器
func (m *TeamMemoryManager) SharedManager() *SharedMemoryManager { return m.sharedManager }

// MemberName 返回成员名称
func (m *TeamMemoryManager) MemberName() string { return m.memberName }

// TeamName 返回团队名称
func (m *TeamMemoryManager) TeamName() string { return m.teamName }

// Role 返回角色
func (m *TeamMemoryManager) Role() TeamRole { return m.role }
