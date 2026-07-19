package schema

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LeaderSpec Leader 身份规格。
type LeaderSpec struct {
	// MemberName 成员名
	MemberName string `json:"member_name"`
	// DisplayName 显示名
	DisplayName string `json:"display_name"`
	// Persona 人设描述
	Persona string `json:"persona"`
	// ModelName 模型名称（可选）
	ModelName string `json:"model_name,omitempty"`
}

// TransportSpec 可插拔传输层规格。
type TransportSpec struct {
	// Type 传输类型
	Type string `json:"type"`
	// Params 传输参数
	Params map[string]any `json:"params,omitempty"`
}

// StorageSpec 可插拔存储层规格。
type StorageSpec struct {
	// Type 存储类型
	Type string `json:"type"`
	// Params 存储参数
	Params map[string]any `json:"params,omitempty"`
}

// TeamAgentSpec 构造 TeamAgent 的完整 JSON 可序列化规格。
type TeamAgentSpec struct {
	// Agents 角色名到 DeepAgentSpec 的映射
	Agents map[string]DeepAgentSpec `json:"agents"`
	// TeamName 团队名
	TeamName string `json:"team_name"`
	// Lifecycle 生命周期模式
	Lifecycle TeamLifecycle `json:"lifecycle"`
	// EnableTeamPlan 是否启用团队计划模式
	EnableTeamPlan bool `json:"enable_team_plan"`
	// TeammateMode 队友交互模式
	TeammateMode MemberMode `json:"teammate_mode"`
	// SpawnMode 生成模式
	SpawnMode string `json:"spawn_mode"`
	// Leader Leader 规格
	Leader LeaderSpec `json:"leader"`
	// PredefinedMembers 预定义成员列表
	PredefinedMembers []TeamMemberSpec `json:"predefined_members"`
	// ModelPool LLM 端点池
	ModelPool []models.ModelPoolEntry `json:"model_pool,omitempty"`
	// ModelRouter 模型路由配置
	ModelRouter *models.ModelRouterConfig `json:"model_router,omitempty"`
	// ModelPoolStrategy 模型池分配策略
	ModelPoolStrategy string `json:"model_pool_strategy"`
	// TeamMode 团队模式（可选）
	TeamMode string `json:"team_mode,omitempty"`
	// Transport 传输层规格
	Transport *TransportSpec `json:"transport,omitempty"`
	// Storage 存储层规格
	Storage *StorageSpec `json:"storage,omitempty"`
	// Worktree 工作树配置
	Worktree *worktree.WorktreeConfig `json:"worktree,omitempty"`
	// Workspace 工作空间配置
	Workspace *team_workspace.TeamWorkspaceConfig `json:"workspace,omitempty"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
	// EnableHITT 是否启用 Human-in-the-Team
	EnableHITT bool `json:"enable_hitt"`
	// ExposeHumanAgentsToTeammates 是否向队友暴露人类代理
	ExposeHumanAgentsToTeammates bool `json:"expose_human_agents_to_teammates"`
	// Language 团队默认语言
	Language string `json:"language,omitempty"`
	// AgentCustomizer 用户自定义配置钩子
	AgentCustomizer any `json:"-"`
	// Memory 团队记忆配置
	Memory *memory.TeamMemoryConfig `json:"memory,omitempty"`
}

// TransportBuilder 传输层构建器函数类型。
type TransportBuilder func(params map[string]any) (messager.MessagerTransportConfig, error)

// StorageBuilder 存储层构建器函数类型。
type StorageBuilder func(params map[string]any) (any, error)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	transportRegistry map[string]TransportBuilder
	storageRegistry   map[string]StorageBuilder
	registryOnce      sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLeaderSpec 创建默认 LeaderSpec。
func NewLeaderSpec() LeaderSpec {
	return LeaderSpec{
		MemberName:  agentteams.DefaultLeaderMemberName,
		DisplayName: "Team Leader",
		Persona:     agentteams.T("blueprint.default_persona"),
	}
}

// Build 构建传输层配置。
func (t TransportSpec) Build() (messager.MessagerTransportConfig, error) {
	ensureBuiltinInfraRegistered()
	b, ok := transportRegistry[t.Type]
	if !ok {
		return messager.MessagerTransportConfig{}, fmt.Errorf("未注册的传输类型: %q", t.Type)
	}
	return b(t.Params)
}

// RegisterTransport 注册传输层构建器。
func RegisterTransport(name string, builder TransportBuilder) {
	ensureBuiltinInfraRegistered()
	transportRegistry[name] = builder
}

// Build 构建存储层配置。
func (s StorageSpec) Build() (any, error) {
	ensureBuiltinInfraRegistered()
	b, ok := storageRegistry[s.Type]
	if !ok {
		return nil, fmt.Errorf("未注册的存储类型: %q", s.Type)
	}
	return b(s.Params)
}

// RegisterStorage 注册存储层构建器。
func RegisterStorage(name string, builder StorageBuilder) {
	ensureBuiltinInfraRegistered()
	storageRegistry[name] = builder
}

// NewTeamAgentSpec 创建默认 TeamAgentSpec。
func NewTeamAgentSpec() TeamAgentSpec {
	return TeamAgentSpec{
		Agents:            make(map[string]DeepAgentSpec),
		TeamName:          "agent_team",
		Lifecycle:         TeamLifecycleTemporary,
		TeammateMode:      MemberModeBuildMode,
		SpawnMode:         "process",
		Leader:            NewLeaderSpec(),
		ModelPoolStrategy: "round_robin",
	}
}

// Validate 校验 TeamAgentSpec 配置合法性。
func (s *TeamAgentSpec) Validate() error {
	if err := s.validatePoolRouterExclusive(); err != nil {
		return err
	}
	// 对齐 Python: TeamAgentSpec.build() 中调用 _validate_reserved_names() 和 _validate_hitt_consistency()
	if err := s.validateReservedNames(); err != nil {
		return err
	}
	if err := s.validateHittConsistency(); err != nil {
		return err
	}
	s.defaultTransportForSpawnMode()
	return nil
}

// ResolveDBConfig 解析数据库配置。
// 对齐 Python: resolve_db_config()，当 db_type 为 sqlite 且 connection_string 为空时，
// 自动填充为 getAgentTeamsHome()/team.db。
func (s *TeamAgentSpec) ResolveDBConfig() any {
	var dbCfg database.DatabaseConfig
	if s.Storage != nil {
		if cfg, err := s.Storage.Build(); err == nil {
			if dc, ok := cfg.(database.DatabaseConfig); ok {
				dbCfg = dc
			} else {
				// 非 DatabaseConfig 类型（如 MemoryDatabaseConfig），直接返回
				return cfg
			}
		} else {
			dbCfg = database.NewDatabaseConfig()
		}
	} else {
		dbCfg = database.NewDatabaseConfig()
	}
	// SQLite 且 connection_string 为空时，自动填充默认路径
	if dbCfg.DBType == database.DatabaseTypeSQLite && dbCfg.ConnectionString == "" {
		dbCfg.ConnectionString = filepath.Join(workspace.AgentTeamsHomeDir(), "team.db")
	}
	return dbCfg
}

// Build 构建 TeamAgent。⤵️ 回填: 9.57
func (s *TeamAgentSpec) Build() (any, error) { return nil, nil }

// ValidateLeaderModelResolved 校验 Leader 模型是否已解析。
// 一比一复刻 Python: openjiuwen/agent_teams/schema/blueprint.py _validate_leader_model_resolved
func ValidateLeaderModelResolved(spec TeamAgentSpec, leaderMemberModel *models.TeamModelConfig, teamSpec TeamSpec) error {
	// 获取 leader 的 DeepAgentSpec
	var leaderAgent *DeepAgentSpec
	if spec.Agents != nil {
		if la, ok := spec.Agents["leader"]; ok {
			leaderAgent = &la
		}
	}
	if leaderMemberModel != nil || (leaderAgent != nil && leaderAgent.Model != nil) {
		return nil
	}
	if teamSpec.ModelPool == nil {
		return nil
	}

	availableNames := make([]string, 0)
	nameSet := make(map[string]bool)
	for _, entry := range teamSpec.ModelPool {
		if !nameSet[entry.ModelName] {
			nameSet[entry.ModelName] = true
			availableNames = append(availableNames, entry.ModelName)
		}
	}
	sort.Strings(availableNames)

	strategy := teamSpec.ModelPoolStrategy
	leaderName := spec.Leader.ModelName

	var cause string
	if leaderName != "" && !nameSet[leaderName] {
		scope := "pool"
		if strategy == "router" {
			scope = "router"
		}
		cause = fmt.Sprintf("leader.model_name='%s' is not present in the %s (available names: %v)", leaderName, scope, availableNames)
	} else if strategy == "by_model_name" {
		cause = "model_pool_strategy='by_model_name' requires leader.model_name to be set to one of the pool names"
	} else {
		cause = "the allocator did not produce a model for the leader"
	}

	var tail string
	if strategy == "router" {
		tail = fmt.Sprintf(
			"(1) leave leader.model_name unset to fall back on the router's first declared name, "+
				"(2) set leader.model_name to one of %v, "+
				"(3) provide an explicit agents['leader'].model in the spec",
			availableNames,
		)
	} else {
		tail = fmt.Sprintf(
			"(1) set leader.model_name to one of %v, "+
				"(2) provide an explicit agents['leader'].model in the spec, "+
				"(3) switch model_pool_strategy to 'round_robin' (always allocates)",
			availableNames,
		)
	}

	return fmt.Errorf("%s; resolve by either: %s", cause, tail)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validatePoolRouterExclusive 校验 model_pool 和 model_router 互斥。
func (s *TeamAgentSpec) validatePoolRouterExclusive() error {
	if s.ModelPool != nil && s.ModelRouter != nil {
		return fmt.Errorf("model_pool 和 model_router 互斥，不能同时配置")
	}
	return nil
}

// defaultTransportForSpawnMode 根据生成模式填充默认传输配置。
func (s *TeamAgentSpec) defaultTransportForSpawnMode() {
	if s.SpawnMode == "inprocess" && s.Transport == nil {
		s.Transport = &TransportSpec{Type: "inprocess"}
	}
}

// validateReservedNames 校验成员名是否使用了保留名。
func (s *TeamAgentSpec) validateReservedNames() error {
	if s.Leader.MemberName == agentteams.HumanAgentMemberName ||
		s.Leader.MemberName == agentteams.UserPseudoMemberName {
		return fmt.Errorf("leader 不能使用保留名 %q", s.Leader.MemberName)
	}
	for _, m := range s.PredefinedMembers {
		if !agentteams.ReservedMemberNames[m.MemberName] {
			continue
		}
		if m.MemberName == agentteams.HumanAgentMemberName && s.EnableHITT {
			continue
		}
		return fmt.Errorf("预定义成员 %q 使用了保留名", m.MemberName)
	}
	return nil
}

// validateHittConsistency 校验 HITT 配置一致性。
func (s *TeamAgentSpec) validateHittConsistency() error {
	if s.EnableHITT {
		return nil
	}
	for _, m := range s.PredefinedMembers {
		if m.RoleType == TeamRoleHumanAgent {
			return fmt.Errorf("预定义成员 %q 角色为 human_agent，但 enable_hitt 未启用", m.MemberName)
		}
	}
	return nil
}

// ensureBuiltinInfraRegistered 初始化内置基础设施注册表。
func ensureBuiltinInfraRegistered() {
	registryOnce.Do(func() {
		transportRegistry = make(map[string]TransportBuilder)
		storageRegistry = make(map[string]StorageBuilder)
		transportRegistry["inprocess"] = func(_ map[string]any) (messager.MessagerTransportConfig, error) {
			cfg := messager.NewMessagerTransportConfig()
			cfg.Backend = "inprocess"
			return cfg, nil
		}
		transportRegistry["pyzmq"] = func(_ map[string]any) (messager.MessagerTransportConfig, error) {
			cfg := messager.NewMessagerTransportConfig()
			cfg.Backend = "pyzmq"
			return cfg, nil
		}
		storageRegistry["sqlite"] = func(_ map[string]any) (any, error) {
			return database.NewDatabaseConfig(), nil
		}
		storageRegistry["postgresql"] = func(_ map[string]any) (any, error) {
			cfg := database.NewDatabaseConfig()
			cfg.DBType = database.DatabaseTypePostgreSQL
			return cfg, nil
		}
		storageRegistry["mysql"] = func(_ map[string]any) (any, error) {
			cfg := database.NewDatabaseConfig()
			cfg.DBType = database.DatabaseTypeMySQL
			return cfg, nil
		}
		storageRegistry["memory"] = func(_ map[string]any) (any, error) {
			return database.NewMemoryDatabaseConfig(), nil
		}
	})
}
