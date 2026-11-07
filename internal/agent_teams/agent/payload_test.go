package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
)

// TestNewSpawnPayloadBuilder 测试构造函数
func TestNewSpawnPayloadBuilder(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.NotNil(t, b)
}

// TestSpawnPayloadBuilder_BuildSpawnPayload 测试输出 map 键完整性
func TestSpawnPayloadBuilder_BuildSpawnPayload(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	teamSpec := &atschema.TeamSpec{
		TeamName:         "test_team",
		DisplayName:      "Test Team",
		LeaderMemberName: "leader_1",
	}
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "teammate_1",
		Persona:    "coder",
		TeamSpec:   teamSpec,
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "Hello team")

	// 验证顶层键
	assert.Contains(t, payload, "coordination")
	assert.Contains(t, payload, "query")

	// 验证 query
	assert.Equal(t, "Hello team", payload["query"])

	// 验证 coordination 内容
	coord, ok := payload["coordination"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "test_team", coord["team_name"])
	assert.Equal(t, "Test Team", coord["display_name"])
	assert.Equal(t, "leader_1", coord["leader_member_name"])
	assert.Equal(t, "teammate_1", coord["member_name"])
	assert.Equal(t, "teammate", coord["role"])
	assert.Equal(t, "coder", coord["persona"])
	assert.Nil(t, coord["transport"])
}

// TestSpawnPayloadBuilder_BuildSpawnPayload_默认Query 测试空 initialMessage 时使用默认 query
func TestSpawnPayloadBuilder_BuildSpawnPayload_默认Query(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleTeammate, MemberName: "t1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "")
	assert.Equal(t, "Join the team and wait for your first assignment.", payload["query"])
}

// TestSpawnPayloadBuilder_BuildSpawnPayload_NilTeamSpec 测试 TeamSpec 为 nil
func TestSpawnPayloadBuilder_BuildSpawnPayload_NilTeamSpec(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleTeammate, MemberName: "t1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "hi")
	coord := payload["coordination"].(map[string]any)
	assert.Equal(t, "", coord["team_name"])
	assert.Equal(t, "", coord["display_name"])
	assert.Equal(t, "", coord["leader_member_name"])
}

// TestSpawnPayloadBuilder_BuildMemberContext 测试成员上下文构造
func TestSpawnPayloadBuilder_BuildMemberContext(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	teamSpec := &atschema.TeamSpec{TeamName: "test_team"}
	ctx := atschema.TeamRuntimeContext{
		Role:     atschema.TeamRoleLeader,
		TeamSpec: teamSpec,
		DBConfig: database.NewDatabaseConfig(),
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	memberSpec := atschema.TeamMemberSpec{
		MemberName: "teammate_1",
		RoleType:   atschema.TeamRoleTeammate,
		Persona:    "coder",
	}

	result := b.BuildMemberContext(memberSpec)
	assert.Equal(t, atschema.TeamRoleTeammate, result.Role)
	assert.Equal(t, "teammate_1", result.MemberName)
	assert.Equal(t, "coder", result.Persona)
	assert.Equal(t, teamSpec, result.TeamSpec)
	assert.Equal(t, database.NewDatabaseConfig(), result.DBConfig)
}

// TestSpawnPayloadBuilder_BuildMemberMessagerConfig_无leader配置 测试 leader 传输配置
// 缺失时返回 nil（无基准无从派生，对齐 Python payload.py L90-92）
func TestSpawnPayloadBuilder_BuildMemberMessagerConfig_无leader配置(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.Nil(t, b.BuildMemberMessagerConfig("t1"))
}

// TestSpawnPayloadBuilder_BuildMemberMessagerConfig_端口分配 测试端口稳定分配与步长递增
func TestSpawnPayloadBuilder_BuildMemberMessagerConfig_端口分配(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	leaderCfg := &atschema.MessagerTransportConfig{
		Backend:             "inprocess",
		TeamName:            "t",
		PubsubPublishAddr:   "tcp://127.0.0.1:16001",
		PubsubSubscribeAddr: "tcp://127.0.0.1:16002",
		Metadata:            map[string]any{"pubsub_bind": "tcp://127.0.0.1:16003", "k": "v"},
	}
	ctx := atschema.TeamRuntimeContext{
		Role:           atschema.TeamRoleLeader,
		MemberName:     "team_leader",
		MessagerConfig: leaderCfg,
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	first := b.BuildMemberMessagerConfig("alice")
	assert.NotNil(t, first)
	assert.Equal(t, "alice", first.NodeID)
	assert.Equal(t, "tcp://127.0.0.1:16000", first.DirectAddr)
	// pubsub 地址沿用 leader
	assert.Equal(t, leaderCfg.PubsubPublishAddr, first.PubsubPublishAddr)
	assert.Equal(t, leaderCfg.PubsubSubscribeAddr, first.PubsubSubscribeAddr)
	// metadata 剔除 pubsub_bind，其余保留
	assert.NotContains(t, first.Metadata, "pubsub_bind")
	assert.Equal(t, "v", first.Metadata["k"])
	// 不改动 leader 配置本身（结构体拷贝）
	assert.Contains(t, leaderCfg.Metadata, "pubsub_bind")

	// 第二个成员按步长递增
	second := b.BuildMemberMessagerConfig("bob")
	assert.NotNil(t, second)
	assert.Equal(t, "tcp://127.0.0.1:16010", second.DirectAddr)

	// 同名成员复用稳定端口
	again := b.BuildMemberMessagerConfig("alice")
	assert.NotNil(t, again)
	assert.Equal(t, first.DirectAddr, again.DirectAddr)
}

// TestSpawnPayloadBuilder_BuildMemberMessagerConfig_并发成员分配 并发分配不 panic 且端口互不重复。
// S-03：memberPortMap 无锁时，SpawnManager 为每个不健康成员各起一条重启 goroutine，
// 两条同时进入这里就是 concurrent map writes（runtime fatal error，不可 recover）。
// 用 -race 运行时该用例会报撕裂读写；不带 -race 时至少验证端口唯一性与稳定性。
func TestSpawnPayloadBuilder_BuildMemberMessagerConfig_并发成员分配(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	leaderCfg := &atschema.MessagerTransportConfig{Backend: "inprocess", TeamName: "t"}
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "team_leader", MessagerConfig: leaderCfg}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	const members = 8
	results := make([][]string, members)
	var wg sync.WaitGroup
	for i := 0; i < members; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// 每个 goroutine 模拟同一成员被重复重启：同名两次必须拿到同一端口
			for round := 0; round < 3; round++ {
				cfg := b.BuildMemberMessagerConfig(fmt.Sprintf("member-%d", idx))
				results[idx] = append(results[idx], cfg.DirectAddr)
			}
			// 同时模拟与已有成员发生交叉分配
			b.BuildMemberMessagerConfig("member-0")
		}(i)
	}
	wg.Wait()

	portSet := make(map[string]int, members)
	for idx, addrs := range results {
		assert.Len(t, addrs, 3)
		for _, addr := range addrs {
			assert.Equal(t, addrs[0], addr, "成员 member-%d 的端口必须稳定", idx)
		}
		portSet[addrs[0]]++
	}
	assert.Len(t, portSet, members, "不同成员必须分配到不同端口，实际=%v", portSet)
}

// TestSpawnPayloadBuilder_BuildMemberMessagerConfig_自定义基准与步长 测试 metadata 覆盖默认值
func TestSpawnPayloadBuilder_BuildMemberMessagerConfig_自定义基准与步长(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	spec.Metadata = map[string]any{
		"teammate_base_port":   float64(20000),
		"teammate_port_offset": "5",
	}
	ctx := atschema.TeamRuntimeContext{
		Role:           atschema.TeamRoleLeader,
		MessagerConfig: &atschema.MessagerTransportConfig{Backend: "inprocess", TeamName: "t"},
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	first := b.BuildMemberMessagerConfig("alice")
	assert.Equal(t, "tcp://127.0.0.1:20000", first.DirectAddr)
	second := b.BuildMemberMessagerConfig("bob")
	assert.Equal(t, "tcp://127.0.0.1:20005", second.DirectAddr)
}

// TestSpawnPayloadBuilder_BuildMemberMessagerConfig_数值字段归一 测试 metadata
// 里不同数值类型（int/int64/float64/json.Number/字符串/非法值）都能被正确读取。
// YAML/JSON 反序列化产生的类型不固定，Python 靠动态类型，Go 需显式归一。
func TestSpawnPayloadBuilder_BuildMemberMessagerConfig_数值字段归一(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		expectOK bool
		wantAddr string
	}{
		{"int", 21000, true, "tcp://127.0.0.1:21000"},
		{"int32", int32(22000), true, "tcp://127.0.0.1:22000"},
		{"int64", int64(23000), true, "tcp://127.0.0.1:23000"},
		{"float64", float64(24000), true, "tcp://127.0.0.1:24000"},
		{"float32", float32(25000), true, "tcp://127.0.0.1:25000"},
		{"jsonNumber", json.Number("26000"), true, "tcp://127.0.0.1:26000"},
		{"数值字符串", " 27000 ", true, "tcp://127.0.0.1:27000"},
		{"非法字符串", "not-a-number", false, "tcp://127.0.0.1:16000"},
		{"bool", true, false, "tcp://127.0.0.1:16000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := atschema.NewTeamAgentSpec()
			spec.Metadata = map[string]any{"teammate_base_port": c.value}
			ctx := atschema.TeamRuntimeContext{
				Role:           atschema.TeamRoleLeader,
				MessagerConfig: &atschema.MessagerTransportConfig{Backend: "inprocess", TeamName: "t"},
			}
			b := agent.NewSpawnPayloadBuilder(spec, ctx)
			got := b.BuildMemberMessagerConfig("alice")
			if got == nil {
				t.Fatal("应返回非 nil 配置")
			}
			assert.Equal(t, c.wantAddr, got.DirectAddr)
		})
	}
}

// TestSpawnPayloadBuilder_BuildSpawnPayload_含transport 测试 coord.transport 序列化。
// Python: transport = member_transport.model_dump(mode="json")
func TestSpawnPayloadBuilder_BuildSpawnPayload_含transport(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:           atschema.TeamRoleTeammate,
		MemberName:     "alice",
		MessagerConfig: &atschema.MessagerTransportConfig{Backend: "inprocess", TeamName: "t"},
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "hi")
	coord, ok := payload["coordination"].(map[string]any)
	assert.True(t, ok)
	transport, ok := coord["transport"].(map[string]any)
	assert.True(t, ok, "leader 配置存在时 transport 应序列化为 map")
	assert.Equal(t, "alice", transport["node_id"])
	assert.Equal(t, "tcp://127.0.0.1:16000", transport["direct_addr"])
}

// TestSpawnPayloadBuilder_BuildSpawnConfig 测试返回 SpawnAgentConfig
func TestSpawnPayloadBuilder_BuildSpawnConfig(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	cfg := b.BuildSpawnConfig(ctx)
	assert.Equal(t, runnerspawn.SpawnAgentKindTeamAgent, cfg.AgentKind)
	// M-18: RunnerConfig 必须填父进程 Runner 快照（Python L121 serialize_runner_config）
	assert.NotNil(t, cfg.RunnerConfig, "RunnerConfig 应非 nil（子进程据此应用父进程配置）")
	assert.NotEmpty(t, cfg.RunnerConfig, "RunnerConfig 应非空 map")
	assert.NotNil(t, cfg.Payload, "Payload 应非 nil")
	// S-26: payload 形状是 FromSpawnPayload 公共契约——必须含 spec/context 键
	//（Python build_spawn_config: payload={"spec": model_dump, "context": model_dump}）
	assert.Contains(t, cfg.Payload, "spec", "payload 必须含 spec 键")
	assert.Contains(t, cfg.Payload, "context", "payload 必须含 context 键")
	specMap, ok := cfg.Payload["spec"].(map[string]any)
	assert.True(t, ok, "spec 应为 map[string]any（model_dump 等价）")
	if ok {
		assert.Contains(t, specMap, "team_name", "spec dump 应含 team_name")
	}
	ctxMap, ok := cfg.Payload["context"].(map[string]any)
	assert.True(t, ok, "context 应为 map[string]any（model_dump 等价）")
	if ok {
		assert.Contains(t, ctxMap, "role", "context dump 应含 role")
	}
	// payload 不应混入 coordination/query（那是 BuildSpawnPayload 的 inputs 形状）
	assert.NotContains(t, cfg.Payload, "coordination", "payload 不应含 coordination（属于 inputs）")
	assert.NotContains(t, cfg.Payload, "query", "payload 不应含 query（属于 inputs）")
}

// TestSpawnPayloadBuilder_BuildSpawnConfig_消费链端到端 测试 BuildSpawnConfig 产出
// 可被 FromSpawnPayload 消费（子进程路径闭环）。
// S-26/S-27: spawn payload wire 契约——BuildSpawnConfig → FromSpawnPayload 往返。
func TestSpawnPayloadBuilder_BuildSpawnConfig_消费链端到端(t *testing.T) {
	memberSpec := atschema.TeamMemberSpec{
		MemberName: "teammate_1",
		RoleType:   atschema.TeamRoleTeammate,
		Persona:    "coder",
	}
	spec := atschema.NewTeamAgentSpec()
	spec.TeamName = "team_e2e"
	spec.Agents = map[string]atschema.DeepAgentSpec{
		"leader": {},
	}
	leaderCtx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, TeamSpec: &atschema.TeamSpec{TeamName: "team_e2e"}}
	b := agent.NewSpawnPayloadBuilder(spec, leaderCtx)

	memberCtx := b.BuildMemberContext(memberSpec)
	cfg := b.BuildSpawnConfig(memberCtx)

	// FromSpawnPayload 消费（agentConfig.Payload 直传）
	ta, err := agent.FromSpawnPayload(context.Background(), cfg.Payload)
	assert.NoError(t, err)
	assert.NotNil(t, ta)
}
