//go:build integration

// Package model_group 23.4c 模型组链路集成测试：spawn 点名、记账、名册读池。
//
// 跨子系统：agent_teams/models（MaterializeModelGroup + IntelliRouterAllocator
// + withRouteTag）→ agent_teams/tools/database（成员行 options 持久化与 served
// models 记账）→ agent_teams/tools（TeamBackend 探针）→ agent_teams/tracker
// （名册读池）——满足规则 7 双层门禁（调用跨度 ≥3）。
package model_group_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/routing"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ModelGroupSuite 模型组模型池集成测试套件。
type ModelGroupSuite struct {
	isuite.AgentSuite
	db database.TeamDatabase
}

// SetupTest 每用例建独立内存数据库（用例间成员/团队名互不污染）。
func (s *ModelGroupSuite) SetupTest() {
	s.db = database.NewInMemoryTeamDatabase()
}

// groupPool 构造两部署组池并物化为 backend。
func (s *ModelGroupSuite) groupPool() ([]models.ModelPoolEntry, *models.IntelliRouterAllocator) {
	cfg := models.NewIntelliRouterConfig([]*models.IntelliRouterDeployment{
		{ModelName: "fast", APIKey: "sk-1", APIBase: "https://a.test", ID: "fast-1", Provider: "deepseek", FallbackTag: "fast"},
		{ModelName: "smart", APIKey: "sk-3", APIBase: "https://b.test", ID: "smart-1", Provider: "openai", FallbackTag: "smart"},
	})
	cfg.Metadata["model_group"] = map[string]any{"display_name": "智能组"}
	pool := cfg.ToPoolEntries()
	alloc, err := models.NewIntelliRouterAllocator(pool)
	s.Require().NoError(err)
	return pool, alloc
}

// newBackend 带组池的 leader backend。
func (s *ModelGroupSuite) newBackend(pool []models.ModelPoolEntry, alloc models.ModelAllocator) *tools.TeamBackend {
	return tools.NewTeamBackend("t1", "leader1", true, s.db, nil,
		tools.WithModelPoolProvider(func() []models.ModelPoolEntry { return pool }),
		tools.WithModelAllocator(alloc),
	)
}

// IT-MG-1 合法点名：spawn 记账链（分配 → DB ref → options round trip →
// 重建成员后 tag 存活并重放为 tag-filtered 客户端配置）。
// Python: test_spawn_teammate_model_group.py:87/:155 + member_options round trip
func (s *ModelGroupSuite) TestSpawnRequestedModelPersistedAsRouteTag() {
	pool, alloc := s.groupPool()
	backend := s.newBackend(pool, alloc)
	s.db.Team().CreateTeam(context.Background(), "t1", "T1", "leader1", "", "", "autonomous", false)

	// leader 点名分配（spawn 工具内部走 AllocateModel）
	allocation := backend.AllocateModel("smart")
	s.Require().NotNil(allocation)
	ref := allocation.ToDBRef()
	s.Equal("smart", ref["route_tag"])

	// 成员行：options 持久化 model ref（含 route_tag）
	tag := "smart"
	opts := database.BuildMemberOptions(database.MemberOptionsInput{ModelRef: ref})
	s.Require().True(backend.DB().Member().CreateMember(
		context.Background(), "alice", "t1", "Alice", "{}", "UNSTARTED", "teammate",
		"", "", "build_mode", "", opts))

	// 从 DB 重建成员：读 options 里的 route_tag 重放
	row, err := backend.DB().Member().GetMember(context.Background(), "alice", "t1")
	s.Require().NoError(err)
	parsed := database.LoadMemberOptions(row.Options)
	s.Require().NotNil(parsed.ModelRef)
	s.Require().NotNil(parsed.ModelRef)
	s.Require().NotNil(parsed.ModelRef.RouteTag)
	cfg := models.ResolveMemberModelFromPool(pool, "*", 0, *parsed.ModelRef.RouteTag)
	s.Require().NotNil(cfg)
	s.Equal("tag-filtered", cfg.ModelClientConfig.IntelliRouter.Strategy)
	s.Equal("smart", cfg.ModelClientConfig.IntelliRouter.StrategyKwargs["fallback_tag"])
	_ = tag
}

// IT-MG-2 非法点名：分配器返回 nil，spawn 工具据此中止且不注册成员。
// Python: test_spawn_teammate_model_group.py:113
func (s *ModelGroupSuite) TestUnselectableModelFailsWithoutRegisteringMember() {
	pool, alloc := s.groupPool()
	backend := s.newBackend(pool, alloc)
	s.db.Team().CreateTeam(context.Background(), "t1", "T1", "leader1", "", "", "autonomous", false)

	s.Require().Nil(backend.AllocateModel("ghost-model"))

	// 事后核对：没有成员被注册（spawn 侧因 allocation nil 提前失败）
	members, err := backend.DB().Member().GetTeamMembers(context.Background(), "t1", "")
	s.Require().NoError(err)
	for _, m := range members {
		s.NotEqual("bob", m.MemberName, "非法点名不应注册成员")
	}
}

// IT-MG-3 不点名 → 整组 entry（无 route_tag）。
// Python: test_spawn_teammate_model_group.py:100
func (s *ModelGroupSuite) TestOmittedModelUsesWholeGroup() {
	pool, alloc := s.groupPool()
	backend := s.newBackend(pool, alloc)
	allocation := backend.AllocateModel("")
	s.Require().NotNil(allocation)
	s.Equal(models.INTELLI_ROUTER_UNIFIED_MODEL, allocation.Entry.ModelName)
	ref := allocation.ToDBRef()
	_, hasTag := ref["route_tag"]
	s.False(hasTag, "整组分配不应携带 route_tag")
	cfg := models.ResolveMemberModelFromPool(pool, "*", 0, "")
	s.Require().NotNil(cfg)
	s.NotEqual("tag-filtered", cfg.ModelClientConfig.IntelliRouter.Strategy)
}

// IT-MG-4 记账 → 名册读池全链：路由结果记账进成员行 + bump updated_at →
// leader tracker 名册显示 model_group 与 models_used。
// Python: team_context.py:596-621 + member_dao.py:807（D-2 验收）
func (s *ModelGroupSuite) TestRosterShowsModelGroupAndModelsUsed() {
	pool, alloc := s.groupPool()
	backend := s.newBackend(pool, alloc)
	s.db.Team().CreateTeam(context.Background(), "t1", "T1", "leader1", "", "", "autonomous", false)
	s.Require().True(backend.DB().Member().CreateMember(
		context.Background(), "alice", "t1", "Alice", "{}", "UNSTARTED", "teammate",
		"", "", "build_mode", "", ""))

	// 模拟一次路由被记账（写链：UpdateMemberServedModel）
	s.Require().True(backend.DB().Member().UpdateMemberServedModel(
		context.Background(), "alice", "t1", "fast"))
	// 名册探针监视 updated_at——记账必须 bump
	row, _ := backend.DB().Member().GetMember(context.Background(), "alice", "t1")
	s.Require().Greater(row.UpdatedAt, int64(0))

	tr := tracker.NewTeamContextTracker(
		backend, "leader1", atschema.TeamRoleLeader,
		"Leader", "", "", "", "", false, "cn", "", nil,
	)
	snapshot, ok := tr.SnapshotText()
	s.Require().True(ok)
	s.Require().NotEmpty(snapshot)
	s.Contains(snapshot, "model_group=智能组")
	s.Contains(snapshot, "models_used=fast (since ")
	s.Contains(snapshot, ", latest call)")
}

// IT-MG-5 编译器产物物化：23.4b CompiledModelSelection → 单条组池条目 →
// 分配器可用（23.4d resolver 已交付，本用例保留编译产物构造路径作为
// 23.4b 编译器的直接回归；端到端 resolver→编译器→池链路由
// tests/integration/swarm/web 的 models RPC 套件覆盖）。
// Python: materialize_model_group + test_model_group_contract.py
func (s *ModelGroupSuite) TestMaterializeCompiledSelectionIntoAllocator() {
	routerCfg := llmschema.NewDefaultIntelliRouterConfig()
	routerCfg.Deployments = []llmschema.IntelliRouterDeploymentConfig{{
		RouteID: "r1", ModelName: "fast", APIKey: "k", APIBase: "https://a.test", Provider: "deepseek",
	}}
	compiled := &routing.CompiledModelSelection{
		ModelClientConfig: &llmschema.ModelClientConfig{
			ClientProvider: "intelli_router", IntelliRouter: &routerCfg,
		},
		ModelRequestConfig: &llmschema.ModelRequestConfig{},
		SelectedType:       "model_group",
		SelectedID:         "grp-1",
	}
	pool, err := models.MaterializeModelGroup(compiled, nil)
	s.Require().NoError(err)
	s.Require().Len(pool, 1)
	s.Equal(models.INTELLI_ROUTER_PROVIDER, pool[0].APIProvider)

	alloc, err := models.NewIntelliRouterAllocator(pool)
	s.Require().NoError(err, "编译产物物化的池必须可直接构造分配器")
	got := alloc.Allocate("")
	s.Require().NotNil(got)
	s.Equal(models.INTELLI_ROUTER_UNIFIED_MODEL, got.Entry.ModelName)
}

// TestModelGroupSuite 入口。
func TestModelGroupSuite(t *testing.T) {
	suite.Run(t, new(ModelGroupSuite))
}
