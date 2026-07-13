package schema

import (
	"encoding/json"
	"testing"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestNewTeamModelConfig 测试默认 TeamModelConfig 创建
func TestNewTeamModelConfig(t *testing.T) {
	cfg := NewTeamModelConfig()
	if cfg.ModelRequestConfig == nil {
		t.Error("ModelRequestConfig 不应为 nil")
	}
	if cfg.ModelClientConfig.Timeout != 60.0 {
		t.Errorf("期望 Timeout=60.0, 实际=%f", cfg.ModelClientConfig.Timeout)
	}
}

// TestTeamModelConfig_Build_留桩 测试 Build 留桩
func TestTeamModelConfig_Build_留桩(t *testing.T) {
	r, err := NewTeamModelConfig().Build()
	if r != nil || err != nil {
		t.Errorf("期望 (nil, nil), 实际=(%v, %v)", r, err)
	}
}

// TestNewLeaderSpec 测试默认 LeaderSpec
func TestNewLeaderSpec(t *testing.T) {
	_ = agentteams.SetLanguage(agentteams.LanguageCN)
	s := NewLeaderSpec()
	if s.MemberName != "team_leader" {
		t.Errorf("期望 'team_leader', 实际=%q", s.MemberName)
	}
	if s.DisplayName != "Team Leader" {
		t.Errorf("期望 'Team Leader', 实际=%q", s.DisplayName)
	}
}

// TestNewLeaderSpec_英文 测试英文 LeaderSpec
func TestNewLeaderSpec_英文(t *testing.T) {
	_ = agentteams.SetLanguage(agentteams.LanguageEN)
	defer func() { _ = agentteams.SetLanguage(agentteams.LanguageCN) }()
	if NewLeaderSpec().Persona != "Genius project management expert" {
		t.Error("期望英文 persona")
	}
}

// TestTransportSpec_Build_未注册 测试未注册传输类型报错
func TestTransportSpec_Build_未注册(t *testing.T) {
	_, err := TransportSpec{Type: "unknown"}.Build()
	if err == nil {
		t.Error("期望报错")
	}
}

// TestTransportSpec_Build_内置 测试内置传输类型
func TestTransportSpec_Build_内置(t *testing.T) {
	cfg, err := TransportSpec{Type: "inprocess"}.Build()
	if err != nil {
		t.Fatalf("报错: %v", err)
	}
	if cfg.Backend != "inprocess" {
		t.Errorf("期望 inprocess, 实际=%q", cfg.Backend)
	}
}

// TestStorageSpec_Build_未注册 测试未注册存储类型报错
func TestStorageSpec_Build_未注册(t *testing.T) {
	_, err := StorageSpec{Type: "unknown"}.Build()
	if err == nil {
		t.Error("期望报错")
	}
}

// TestStorageSpec_Build_内置 测试内置存储类型
func TestStorageSpec_Build_内置(t *testing.T) {
	cfg, err := StorageSpec{Type: "sqlite"}.Build()
	if err != nil {
		t.Fatalf("报错: %v", err)
	}
	dbCfg, ok := cfg.(database.DatabaseConfig)
	if !ok {
		t.Fatal("期望 DatabaseConfig")
	}
	if dbCfg.DBType != database.DatabaseTypeSQLite {
		t.Errorf("期望 sqlite, 实际=%q", dbCfg.DBType)
	}
}

// TestEnsureBuiltinInfraRegistered 测试内置基础设施注册
func TestEnsureBuiltinInfraRegistered(t *testing.T) {
	ensureBuiltinInfraRegistered()
	for _, n := range []string{"inprocess", "pyzmq"} {
		if _, ok := transportRegistry[n]; !ok {
			t.Errorf("传输 %q 未注册", n)
		}
	}
	for _, n := range []string{"sqlite", "postgresql", "mysql", "memory"} {
		if _, ok := storageRegistry[n]; !ok {
			t.Errorf("存储 %q 未注册", n)
		}
	}
}

// TestTeamAgentSpec_Validate_正常 测试正常验证
func TestTeamAgentSpec_Validate_正常(t *testing.T) {
	s := NewTeamAgentSpec()
	if err := (&s).Validate(); err != nil {
		t.Errorf("报错: %v", err)
	}
}

// TestTeamAgentSpec_Validate_PoolRouter互斥 测试互斥
func TestTeamAgentSpec_Validate_PoolRouter互斥(t *testing.T) {
	s := NewTeamAgentSpec()
	s.ModelPool = []map[string]any{{"model_name": "test"}}
	s.ModelRouter = map[string]any{"api_base_url": "http://localhost"}
	if err := (&s).Validate(); err == nil {
		t.Error("期望互斥报错")
	}
}

// TestTeamAgentSpec_DefaultTransportForSpawnMode_inprocess 测试自动填充
func TestTeamAgentSpec_DefaultTransportForSpawnMode_inprocess(t *testing.T) {
	s := NewTeamAgentSpec()
	s.SpawnMode = "inprocess"
	_ = (&s).Validate()
	if s.Transport == nil || s.Transport.Type != "inprocess" {
		t.Error("期望自动填充 inprocess")
	}
}

// TestTeamAgentSpec_DefaultTransportForSpawnMode_process 测试不填充
func TestTeamAgentSpec_DefaultTransportForSpawnMode_process(t *testing.T) {
	s := NewTeamAgentSpec()
	s.SpawnMode = "process"
	_ = (&s).Validate()
	if s.Transport != nil {
		t.Error("期望不填充")
	}
}

// TestTeamAgentSpec_ValidateReservedNames_正常 测试正常
func TestTeamAgentSpec_ValidateReservedNames_正常(t *testing.T) {
	s := NewTeamAgentSpec()
	s.PredefinedMembers = []TeamMemberSpec{{MemberName: "coder", RoleType: TeamRoleTeammate}}
	if err := (&s).validateReservedNames(); err != nil {
		t.Errorf("报错: %v", err)
	}
}

// TestTeamAgentSpec_ValidateReservedNames_保留名 测试保留名
func TestTeamAgentSpec_ValidateReservedNames_保留名(t *testing.T) {
	s := NewTeamAgentSpec()
	s.PredefinedMembers = []TeamMemberSpec{{MemberName: "user", RoleType: TeamRoleTeammate}}
	if err := (&s).validateReservedNames(); err == nil {
		t.Error("期望报错")
	}
}

// TestTeamAgentSpec_ValidateReservedNames_HumanAgent允许 测试 HITT 允许
func TestTeamAgentSpec_ValidateReservedNames_HumanAgent允许(t *testing.T) {
	s := NewTeamAgentSpec()
	s.EnableHITT = true
	s.PredefinedMembers = []TeamMemberSpec{{MemberName: "human_agent", RoleType: TeamRoleHumanAgent}}
	if err := (&s).validateReservedNames(); err != nil {
		t.Errorf("报错: %v", err)
	}
}

// TestTeamAgentSpec_ValidateReservedNames_Leader保留名 测试 Leader 保留名
func TestTeamAgentSpec_ValidateReservedNames_Leader保留名(t *testing.T) {
	s := NewTeamAgentSpec()
	s.Leader.MemberName = "user"
	if err := (&s).validateReservedNames(); err == nil {
		t.Error("期望报错")
	}
}

// TestTeamAgentSpec_ValidateHittConsistency_正常 测试 HITT 正常
func TestTeamAgentSpec_ValidateHittConsistency_正常(t *testing.T) {
	s := NewTeamAgentSpec()
	if err := (&s).validateHittConsistency(); err != nil {
		t.Errorf("报错: %v", err)
	}
}

// TestTeamAgentSpec_ValidateHittConsistency_未启用 测试 HITT 未启用
func TestTeamAgentSpec_ValidateHittConsistency_未启用(t *testing.T) {
	s := NewTeamAgentSpec()
	s.PredefinedMembers = []TeamMemberSpec{{MemberName: "human_agent", RoleType: TeamRoleHumanAgent}}
	if err := (&s).validateHittConsistency(); err == nil {
		t.Error("期望报错")
	}
}

// TestTeamAgentSpec_ValidateLeaderModelResolved_有模型 测试有模型
func TestTeamAgentSpec_ValidateLeaderModelResolved_有模型(t *testing.T) {
	a := NewDeepAgentSpec()
	a.Model = &TeamModelConfig{}
	ts := TeamSpec{ModelPool: []map[string]any{{"model_name": "test"}}}
	if err := ValidateLeaderModelResolved(a, nil, ts); err != nil {
		t.Errorf("报错: %v", err)
	}
}

// TestTeamAgentSpec_ValidateLeaderModelResolved_无模型 测试无模型
func TestTeamAgentSpec_ValidateLeaderModelResolved_无模型(t *testing.T) {
	a := NewDeepAgentSpec()
	ts := TeamSpec{ModelPool: []map[string]any{{"model_name": "test"}}}
	if err := ValidateLeaderModelResolved(a, nil, ts); err == nil {
		t.Error("期望报错")
	}
}

// TestTeamAgentSpec_ValidateLeaderModelResolved_分配模型 测试分配模型
func TestTeamAgentSpec_ValidateLeaderModelResolved_分配模型(t *testing.T) {
	a := NewDeepAgentSpec()
	m := &TeamModelConfig{}
	ts := TeamSpec{ModelPool: []map[string]any{{"model_name": "test"}}}
	if err := ValidateLeaderModelResolved(a, m, ts); err != nil {
		t.Errorf("报错: %v", err)
	}
}

// TestTeamAgentSpec_ValidateLeaderModelResolved_无池 测试无池
func TestTeamAgentSpec_ValidateLeaderModelResolved_无池(t *testing.T) {
	if err := ValidateLeaderModelResolved(NewDeepAgentSpec(), nil, TeamSpec{}); err != nil {
		t.Errorf("报错: %v", err)
	}
}

// TestTeamAgentSpec_ResolveDBConfig_默认 测试默认 DB
func TestTeamAgentSpec_ResolveDBConfig_默认(t *testing.T) {
	s := NewTeamAgentSpec()
	cfg := (&s).ResolveDBConfig()
	dbCfg, ok := cfg.(database.DatabaseConfig)
	if !ok {
		t.Fatal("期望 DatabaseConfig")
	}
	if dbCfg.DBType != database.DatabaseTypeSQLite {
		t.Errorf("期望 sqlite, 实际=%q", dbCfg.DBType)
	}
}

// TestTeamAgentSpec_ResolveDBConfig_存储配置 测试带存储
func TestTeamAgentSpec_ResolveDBConfig_存储配置(t *testing.T) {
	s := NewTeamAgentSpec()
	s.Storage = &StorageSpec{Type: "memory"}
	cfg := (&s).ResolveDBConfig()
	memCfg, ok := cfg.(database.MemoryDatabaseConfig)
	if !ok {
		t.Fatal("期望 MemoryDatabaseConfig")
	}
	if memCfg.DBType != database.DatabaseTypeMemory {
		t.Errorf("期望 memory, 实际=%q", memCfg.DBType)
	}
}

// TestTeamAgentSpec_Build_留桩 测试 Build 留桩
func TestTeamAgentSpec_Build_留桩(t *testing.T) {
	s := NewTeamAgentSpec()
	r, err := (&s).Build()
	if r != nil || err != nil {
		t.Errorf("期望 (nil, nil), 实际=(%v, %v)", r, err)
	}
}

// TestNewDeepAgentSpec 测试默认 DeepAgentSpec
func TestNewDeepAgentSpec(t *testing.T) {
	s := NewDeepAgentSpec()
	if s.MaxIterations != 15 {
		t.Errorf("期望 15, 实际=%d", s.MaxIterations)
	}
	if !s.AutoCreateWorkspace {
		t.Error("期望 true")
	}
	if s.CompletionTimeout != 600.0 {
		t.Errorf("期望 600.0, 实际=%f", s.CompletionTimeout)
	}
}

// TestDeepAgentSpec_JSON序列化 测试 JSON 往返
func TestDeepAgentSpec_JSON序列化(t *testing.T) {
	s := DeepAgentSpec{
		Card:                  agentschema.NewAgentCard(),
		SystemPrompt:          "test",
		MaxIterations:         20,
		EnableTaskLoop:        true,
		AutoCreateWorkspace:   false,
		CompletionTimeout:     300.0,
		Skills:                []string{"s1", "s2"},
		ApprovalRequiredTools: []string{"t1"},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var d DeepAgentSpec
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if d.SystemPrompt != s.SystemPrompt {
		t.Errorf("不匹配")
	}
	if d.MaxIterations != s.MaxIterations {
		t.Errorf("不匹配")
	}
}

// TestRegisterTransport 测试自定义传输注册
func TestRegisterTransport(t *testing.T) {
	nt := "custom_transport_test"
	RegisterTransport(nt, func(_ map[string]any) (messager.MessagerTransportConfig, error) {
		cfg := messager.NewMessagerTransportConfig()
		cfg.Backend = nt
		return cfg, nil
	})
	cfg, err := TransportSpec{Type: nt}.Build()
	if err != nil {
		t.Fatalf("报错: %v", err)
	}
	if cfg.Backend != nt {
		t.Errorf("不匹配")
	}
}

// TestRegisterStorage 测试自定义存储注册
func TestRegisterStorage(t *testing.T) {
	nt := "custom_storage_test"
	RegisterStorage(nt, func(_ map[string]any) (any, error) { return database.NewMemoryDatabaseConfig(), nil })
	cfg, err := StorageSpec{Type: nt}.Build()
	if err != nil {
		t.Fatalf("报错: %v", err)
	}
	memCfg, ok := cfg.(database.MemoryDatabaseConfig)
	if !ok {
		t.Fatal("期望 MemoryDatabaseConfig")
	}
	if memCfg.DBType != database.DatabaseTypeMemory {
		t.Errorf("不匹配")
	}
}

// TestWorkspaceSpec_JSON序列化 测试 WorkspaceSpec JSON
func TestWorkspaceSpec_JSON序列化(t *testing.T) {
	s := WorkspaceSpec{RootPath: "/tmp/ws", Language: "cn", StableBase: true}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var d WorkspaceSpec
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if d.RootPath != s.RootPath {
		t.Errorf("不匹配")
	}
}

// TestVisionModelSpec_JSON序列化 测试 VisionModelSpec JSON
func TestVisionModelSpec_JSON序列化(t *testing.T) {
	s := VisionModelSpec{APIKey: "k", BaseURL: "http://localhost", Model: "gpt-4-vision", MaxRetries: 3}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var d VisionModelSpec
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if d.Model != s.Model {
		t.Errorf("不匹配")
	}
}
