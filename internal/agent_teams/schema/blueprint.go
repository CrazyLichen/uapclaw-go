package schema

import (
	"fmt"
	"path/filepath"
	"sync"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamModelConfig 可序列化的团队模型配置。
// 对齐 Python: TeamModelConfig (openjiuwen/agent_teams/schema/deep_agent_spec.py)
type TeamModelConfig struct {
	// ModelClientConfig 模型客户端配置
	ModelClientConfig llmschema.ModelClientConfig `json:"model_client_config"`
	// ModelRequestConfig 模型请求配置（可选）
	ModelRequestConfig *llmschema.ModelRequestConfig `json:"model_request_config,omitempty"`
}

// WorkspaceSpec 工作空间规格占位类型。
// ⤵️ 回填: 9.57
type WorkspaceSpec struct {
	RootPath   string `json:"root_path"`
	Language   string `json:"language"`
	StableBase bool   `json:"stable_base"`
}

// VisionModelSpec 视觉模型规格占位类型。
type VisionModelSpec struct {
	APIKey     string `json:"api_key"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	MaxRetries int    `json:"max_retries"`
}

// AudioModelSpec 音频模型规格占位类型。
type AudioModelSpec struct {
	APIKey             string `json:"api_key"`
	BaseURL            string `json:"base_url"`
	TranscriptionModel string `json:"transcription_model"`
	QAModel            string `json:"qa_model"`
	MaxRetries         int    `json:"max_retries"`
	HTTPTimeout        int    `json:"http_timeout"`
	MaxAudioBytes      int    `json:"max_audio_bytes"`
	ACRAccessKey       string `json:"acr_access_key"`
	ACRAccessSecret    string `json:"acr_access_secret"`
	ACRBaseURL         string `json:"acr_base_url"`
}

// ProgressiveToolSpec 渐进式工具规格占位类型。
type ProgressiveToolSpec struct {
	Enabled             bool     `json:"enabled"`
	AlwaysVisibleTools  []string `json:"always_visible_tools,omitempty"`
	DefaultVisibleTools []string `json:"default_visible_tools,omitempty"`
	MaxLoadedTools      int      `json:"max_loaded_tools"`
}

// SysOperationSpec 系统操作规格占位类型。
type SysOperationSpec struct {
	ID   string `json:"id"`
	Mode string `json:"mode"`
}

// RailSpec 约束规则规格占位类型。
type RailSpec struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// BuiltinToolSpec 内置工具规格占位类型。
type BuiltinToolSpec struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// SubAgentSpec 子代理规格占位类型。
type SubAgentSpec struct {
	AgentCard    any    `json:"agent_card"`
	SystemPrompt string `json:"system_prompt"`
}

// DeepAgentSpec 单角色 DeepAgent 规格。
// 对齐 Python: DeepAgentSpec
type DeepAgentSpec struct {
	Model                  *TeamModelConfig       `json:"model,omitempty"`
	Card                   *agentschema.AgentCard `json:"card,omitempty"`
	SystemPrompt           string                 `json:"system_prompt,omitempty"`
	Tools                  []any                  `json:"tools,omitempty"`
	Mcps                   []any                  `json:"mcps,omitempty"`
	Subagents              []any                  `json:"subagents,omitempty"`
	Rails                  []any                  `json:"rails,omitempty"`
	EnableTaskLoop         bool                   `json:"enable_task_loop"`
	EnableAsyncSubagent    bool                   `json:"enable_async_subagent"`
	AddGeneralPurposeAgent bool                   `json:"add_general_purpose_agent"`
	MaxIterations          int                    `json:"max_iterations"`
	Workspace              *WorkspaceSpec         `json:"workspace,omitempty"`
	Skills                 []string               `json:"skills,omitempty"`
	EnableSkillDiscovery   bool                   `json:"enable_skill_discovery"`
	SysOperation           *SysOperationSpec      `json:"sys_operation,omitempty"`
	Language               string                 `json:"language,omitempty"`
	PromptMode             string                 `json:"prompt_mode,omitempty"`
	VisionModel            *VisionModelSpec       `json:"vision_model,omitempty"`
	AudioModel             *AudioModelSpec        `json:"audio_model,omitempty"`
	EnableTaskPlanning     bool                   `json:"enable_task_planning"`
	RestrictToSandbox      bool                   `json:"restrict_to_sandbox"`
	AutoCreateWorkspace    bool                   `json:"auto_create_workspace"`
	CompletionTimeout      float64                `json:"completion_timeout"`
	ProgressiveTool        *ProgressiveToolSpec   `json:"progressive_tool,omitempty"`
	ApprovalRequiredTools  []string               `json:"approval_required_tools,omitempty"`
}

// LeaderSpec Leader 身份规格。
type LeaderSpec struct {
	MemberName  string `json:"member_name"`
	DisplayName string `json:"display_name"`
	Persona     string `json:"persona"`
	ModelName   string `json:"model_name,omitempty"`
}

// TransportSpec 可插拔传输层规格。
type TransportSpec struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// StorageSpec 可插拔存储层规格。
type StorageSpec struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// TeamAgentSpec 构造 TeamAgent 的完整 JSON 可序列化规格。
type TeamAgentSpec struct {
	Agents                       map[string]DeepAgentSpec            `json:"agents"`
	TeamName                     string                              `json:"team_name"`
	Lifecycle                    TeamLifecycle                       `json:"lifecycle"`
	EnableTeamPlan               bool                                `json:"enable_team_plan"`
	TeammateMode                 MemberMode                          `json:"teammate_mode"`
	SpawnMode                    string                              `json:"spawn_mode"`
	Leader                       LeaderSpec                          `json:"leader"`
	PredefinedMembers            []TeamMemberSpec                    `json:"predefined_members"`
	ModelPool                    []models.ModelPoolEntry             `json:"model_pool,omitempty"`
	ModelRouter                  *models.ModelRouterConfig           `json:"model_router,omitempty"`
	ModelPoolStrategy            string                              `json:"model_pool_strategy"`
	TeamMode                     string                              `json:"team_mode,omitempty"`
	Transport                    *TransportSpec                      `json:"transport,omitempty"`
	Storage                      *StorageSpec                        `json:"storage,omitempty"`
	Worktree                     *worktree.WorktreeConfig            `json:"worktree,omitempty"`
	Workspace                    *team_workspace.TeamWorkspaceConfig `json:"workspace,omitempty"`
	Metadata                     map[string]any                      `json:"metadata,omitempty"`
	EnableHITT                   bool                                `json:"enable_hitt"`
	ExposeHumanAgentsToTeammates bool                                `json:"expose_human_agents_to_teammates"`
	Language                     string                              `json:"language,omitempty"`
	AgentCustomizer              any                                 `json:"-"`
	Memory                       *memory.TeamMemoryConfig            `json:"memory,omitempty"`
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

// NewTeamModelConfig 创建默认 TeamModelConfig。
func NewTeamModelConfig() TeamModelConfig {
	return TeamModelConfig{
		ModelClientConfig:  *llmschema.NewModelClientConfig("", "", ""),
		ModelRequestConfig: llmschema.NewModelRequestConfig(),
	}
}

// Build 构建团队模型配置。⤵️ 回填: 9.57
func (c TeamModelConfig) Build() (any, error) { return nil, nil }

// NewDeepAgentSpec 创建默认 DeepAgentSpec。
func NewDeepAgentSpec() DeepAgentSpec {
	return DeepAgentSpec{MaxIterations: 15, AutoCreateWorkspace: true, CompletionTimeout: 600.0}
}

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
func ValidateLeaderModelResolved(leaderAgent DeepAgentSpec, leaderMemberModel *TeamModelConfig, teamSpec TeamSpec) error {
	if teamSpec.ModelPool == nil {
		return nil
	}
	if leaderMemberModel != nil || leaderAgent.Model != nil {
		return nil
	}
	return fmt.Errorf("leader 没有模型配置。当 model_pool 已配置时，Leader 必须通过 model_pool 分配或在 agents[\"leader\"].model 中显式指定模型")
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
