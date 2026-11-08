package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
	// DispatchMode 任务如何到达执行成员（"autonomous" / "scheduled"）
	//
	// 与 TeamMode 正交（后者管名册能否增长）。"autonomous"（默认）把任务放进
	// 共享看板，成员认领与自己专长匹配的，leader 用 send_message 广播拉起；
	// "scheduled" 由 leader 事先把每个任务指派给具名成员，调度运行时（F_62）
	// 启动每个任务并执行全部交接——开工、验收派发、返工、验收通过——作为
	// leader 身份的信箱消息，成员失去 claim_task 而得到 member_complete_task。
	// 提示词与工具形态都在构建期按模式装配；运行期永不改变。
	//
	// Python: dispatch_mode (blueprint.py L350)
	DispatchMode string `json:"dispatch_mode,omitempty"`
	// EnableMemberMessaging 允许 scheduled 成员直接互发消息（F_126）
	//
	// scheduled 收窄了成员的 send_message 到 leader 与 user，所以 leader 要
	// 路由每次交换。开启后成员保留 autonomous 成员的完整触达——点对点、
	// 组播、对同伴广播——同时 "leader" 仍是合法的接收方。
	// 静态配置；在 dispatch_mode="autonomous" 下无意义（那里成员本就能互相触达）。
	//
	// Python: enable_member_messaging (blueprint.py L374)
	EnableMemberMessaging bool `json:"enable_member_messaging,omitempty"`
	// MaxSubtaskDepth 允许 scheduled 成员把任务拆成子任务（F_128）
	// 0（默认）让 create_task 只属于 leader。正值给 scheduled 成员一个
	// create_task，把它正在执行的任务停在 BLOCKED 直到新子任务完成，并限制
	// 子任务嵌套深度（leader 的任务是深度 0，其子任务是 1，……）。
	// 静态配置，build_team 不可覆盖；在 dispatch_mode="autonomous" 下无意义。
	//
	// Python: max_subtask_depth (blueprint.py L385)
	MaxSubtaskDepth int `json:"max_subtask_depth,omitempty"`
	// EnableTaskVerification 团队级「验证预期」开关（F_62）
	//
	// 纯提示词驱动：True 时 leader 的建任务指引会要求它按自己的判断给每个任务
	// 分配 0..N 个 reviewer（无硬校验——leader 认为琐碎的任务仍可不配）。
	// 验证者机制本身（列、守卫、投票）与该开关无关，始终可用。
	// 可在 build_team 时按团队实例覆盖（spec 对该开关是静默收窄，不像
	// enable_hitt 撞顶会报错）。
	//
	// Python: enable_task_verification (blueprint.py L364)
	EnableTaskVerification bool `json:"enable_task_verification,omitempty"`
	// BootstrapMode 谁物化团队行，以及 leader 如何收到它的政策（F_122）
	//
	// "leader"（默认）：leader 调 build_team 工具，该工具创建团队并在结果里
	// 披露协作政策（F_76）。"framework"：leader 的协调内核在 start() 时若
	// 团队行尚不存在就自己建，leader 的工具集不含 build_team，政策经
	// TeamContextTracker 以 <team-policy> 块在首条输入之前到达。
	// 只在与 dispatch_mode="scheduled" 同时使用时合法。
	//
	// Python: bootstrap_mode (blueprint.py L395)
	BootstrapMode string `json:"bootstrap_mode,omitempty"`
	// BoundGroupName 绑定的 AgentGroup 名（F_125）
	//
	// scheduled + 绑定专家团 + 专家工具接线时，leader 段会命名该组，指示
	// 先在这个团里找专家。非空即视为绑定。
	//
	// Python: expert_binding 的 group（blueprint.py）
	BoundGroupName string `json:"bound_group_name,omitempty"`
	// SwarmflowEnabled leader 是否持有 swarmflow 工具
	//
	// 偏差 D-3：上游不是 spec 字段，而是由工具工厂根据「是否注册了 swarmflow
	// 工具」推导后传给 rail 工厂。Go 侧 swarmflow 工具族属后续章节，工厂门控
	// 尚不存在，故暂作 spec 字段；swarmflow 落地后改为工厂推导。
	SwarmflowEnabled bool `json:"swarmflow_enabled,omitempty"`
	// ExposeHumanAgentsToTeammates 是否向队友暴露人类代理
	ExposeHumanAgentsToTeammates bool `json:"expose_human_agents_to_teammates"`
	// Language 团队默认语言
	Language string `json:"language,omitempty"`
	// AgentCustomizer 用户自定义配置钩子
	// S-01: 统一为具体类型（原 any + 类型断言反模式）。
	// 对齐 Python: agent_customizer: Callable[..., Awaitable[None]] | None
	AgentCustomizer AgentCustomizer `json:"-"`
	// Memory 团队记忆配置
	Memory *TeamMemoryConfig `json:"memory,omitempty"`
}

// AgentCustomizer 用户自定义 Agent 配置钩子（全项目唯一类型定义）。
// 对齐 Python: build_agent_customizer 返回的 customizer(agent, member_name, role) 闭包
// （team_manager.py L727-731: async def customizer(agent, member_name=None, role=None)）。
// Go 惯例：memberName/role 空字符串等价 Python None（触发 fallback）；
// ctx 传播取消信号；error 对应 Python 闭包内 await 调用的异常上抛。
type AgentCustomizer func(ctx context.Context, agent hinterfaces.DeepAgentInterface, memberName string, role string) error

// TransportBuilder 传输层构建器函数类型。
type TransportBuilder func(params map[string]any) (MessagerTransportConfig, error)

// StorageBuilder 存储层构建器函数类型。
type StorageBuilder func(params map[string]any) (any, error)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentTeam

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	transportRegistry map[string]TransportBuilder
	storageRegistry   map[string]StorageBuilder
	registryOnce      sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLeaderSpec 创建默认 LeaderSpec。
func NewLeaderSpec() LeaderSpec {
	persona, _ := T("blueprint.default_persona")
	return LeaderSpec{
		MemberName:  DefaultLeaderMemberName,
		DisplayName: "Team Leader",
		Persona:     persona,
	}
}

// Build 构建传输层配置。
func (t TransportSpec) Build() (MessagerTransportConfig, error) {
	ensureBuiltinInfraRegistered()
	b, ok := transportRegistry[t.Type]
	if !ok {
		return MessagerTransportConfig{}, fmt.Errorf("未注册的传输类型: %q", t.Type)
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

// NewTeamAgentSpecFromDict 从 map[string]any 字典构建 TeamAgentSpec。
// 对齐 Python: TeamAgentSpec(**spec_dict)
// Go 差异：Python 直接用 **dict 展开，Go 使用 JSON 序列化/反序列化桥接。
// S-61: 解析失败（nil 入参/marshal/unmarshal）返回 nil，不再回退默认 spec——
// 对齐 Python team_runner.py L648-657 model_validate 失败 → warning → None，
// 调用方（runner/swarm 路径）以 nil 守卫走降级分支。
func NewTeamAgentSpecFromDict(specDict map[string]any) *TeamAgentSpec {
	if specDict == nil {
		return nil
	}
	// JSON 桥接：dict → JSON → TeamAgentSpec
	data, err := json.Marshal(specDict)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("NewTeamAgentSpecFromDict: JSON marshal failed")
		return nil
	}
	var spec TeamAgentSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		logger.Warn(logComponent).Err(err).Msg("NewTeamAgentSpecFromDict: JSON unmarshal failed")
		return nil
	}
	// 确保 Agents map 不为 nil
	if spec.Agents == nil {
		spec.Agents = make(map[string]DeepAgentSpec)
	}
	return &spec
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
	// Python: TeamAgentSpec.build() 中调用 _validate_reserved_names() 和 _validate_hitt_consistency()
	if err := s.validateReservedNames(); err != nil {
		return err
	}
	if err := s.validateHittConsistency(); err != nil {
		return err
	}
	if err := s.validateBootstrapMode(); err != nil {
		return err
	}
	s.defaultTransportForSpawnMode()
	return nil
}

// ResolveDBConfig 解析数据库配置。
// Python: resolve_db_config()，当 db_type 为 sqlite 且 connection_string 为空时，
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

// Build 校验 TeamAgentSpec 并返回自身。
// 对齐 Python: TeamAgentSpec.build() → TeamAgent
// Go 差异：因循环依赖，schema 包无法导入 agent 包，
// 因此 Build() 仅做校验，实际 TeamAgent 构造由 TeamManager.CreateTeam 完成。
func (s *TeamAgentSpec) Build() (*TeamAgentSpec, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

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

	return fmt.Errorf("%s; 解决方式: %s", cause, tail)
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
	if s.Leader.MemberName == HumanAgentMemberName ||
		s.Leader.MemberName == UserPseudoMemberName {
		return fmt.Errorf("leader 不能使用保留名 %q", s.Leader.MemberName)
	}
	for _, m := range s.PredefinedMembers {
		if !ReservedMemberNames[m.MemberName] {
			continue
		}
		if m.MemberName == HumanAgentMemberName && s.EnableHITT {
			continue
		}
		return fmt.Errorf("预定义成员 %q 使用了保留名", m.MemberName)
	}
	return nil
}

// validateBootstrapMode 校验 bootstrap_mode 与 dispatch_mode 的组合合法性。
//
// framework 建队意味着「团队在 start() 时就建好、成员为零」，这种团队只有
// 靠调度指派（scheduled）才有可用性——任务驱动的交接是它唯一的工作来源。
// 所以 framework 只在 scheduled 下合法。
//
// Python: _validate_bootstrap_mode (blueprint.py L781-793)
func (s *TeamAgentSpec) validateBootstrapMode() error {
	if s.BootstrapMode == "framework" && s.DispatchMode != "scheduled" {
		return fmt.Errorf("bootstrap_mode='framework' requires dispatch_mode='scheduled'; got dispatch_mode=%q", s.DispatchMode)
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
		transportRegistry["inprocess"] = func(_ map[string]any) (MessagerTransportConfig, error) {
			cfg := NewMessagerTransportConfig()
			cfg.Backend = "inprocess"
			return cfg, nil
		}
		transportRegistry["pyzmq"] = func(_ map[string]any) (MessagerTransportConfig, error) {
			cfg := NewMessagerTransportConfig()
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
