//go:build integration

package externalcli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfigRoundtripSuite 外部 CLI 配置面集成套件。
//
// 跨子系统：agent_teams/schema（ExternalCliAgentSpec 校验器群 + TeamAgentSpec
// 序列化往返）→ agentcore/harness/schema（BuildContextSeed 经注册工厂重建 →
// BuildContext 提取器取路径）。校验器红绿报文逐字对齐 team.py:284-525。
type ConfigRoundtripSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestConfigRoundtripSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestConfigRoundtripSuite(t *testing.T) {
	suite.Run(t, new(ConfigRoundtripSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestExternalCliSpec校验器_红绿抽样 ExternalCliAgentSpec.Validate 的红绿
// 抽样（spec §4 用例④）：数值域约束、后端归属互斥、builtin_models 约束，
// 报文逐字对齐 team.py。
func (s *ConfigRoundtripSuite) TestExternalCliSpec校验器_红绿抽样() {
	f64 := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }

	// 绿：openclaw + command（adapter 后端合法路径）
	green := schema.ExternalCliAgentSpec{CLIAgent: "openclaw", Command: []string{"/bin/echo"}}
	s.NoError(green.Validate())
	// 绿：claude 专属字段归位
	greenClaude := schema.ExternalCliAgentSpec{CLIAgent: "claude", ClaudeMaxBufferSize: i(1 << 20)}
	s.NoError(greenClaude.Validate())
	// 绿：codex 默认 bypass 注入（team.py:473-476 model_fields_set 语义）
	greenCodex := schema.ExternalCliAgentSpec{CLIAgent: "codex"}
	s.NoError(greenCodex.Validate())
	s.NotNil(greenCodex.CodexBypassApprovalsAndSandbox)
	s.True(*greenCodex.CodexBypassApprovalsAndSandbox, "codex 默认 bypass")

	reds := []struct {
		name string
		spec schema.ExternalCliAgentSpec
		want string
	}{
		{"codex_bin只限codex", schema.ExternalCliAgentSpec{CLIAgent: "openclaw", CodexBin: "/usr/bin/codex"},
			"codex_bin is only valid when cli_agent='codex'"},
		{"codex审批模式只限codex", schema.ExternalCliAgentSpec{CLIAgent: "openclaw", MCPDefaultToolsApprovalMode: "auto"},
			"mcp_default_tools_approval_mode is only valid when cli_agent='codex'"},
		{"codex_idle超时只限codex", schema.ExternalCliAgentSpec{CLIAgent: "openclaw", CodexTurnIdleTimeoutS: f64(1)},
			"codex_turn_idle_timeout_s is only valid when cli_agent='codex'"},
		{"codex_retries只限codex", schema.ExternalCliAgentSpec{CLIAgent: "openclaw", CodexTurnIdleRetries: i(1)},
			"codex_turn_idle_retries is only valid when cli_agent='codex'"},
		{"claude_idle超时只限claude", schema.ExternalCliAgentSpec{CLIAgent: "openclaw", ClaudeTurnIdleTimeoutS: f64(1)},
			"claude_turn_idle_timeout_s is only valid when cli_agent='claude'"},
		{"claude缓冲只限claude", schema.ExternalCliAgentSpec{CLIAgent: "openclaw", ClaudeMaxBufferSize: i(1)},
			"claude_max_buffer_size is only valid when cli_agent='claude'"},
		{"cli_path不配adapter后端", schema.ExternalCliAgentSpec{CLIAgent: "openclaw", CLIPath: "/bin/x"},
			"cli_path is not valid for adapter-backed CLI agents; use command instead"},
		{"model_config不配adapter后端", schema.ExternalCliAgentSpec{CLIAgent: "openclaw",
			ExternalModelConfig: &schema.ExternalCliModelConfig{Model: "m"}},
			"model_config is not valid for adapter-backed CLI agents"},
		{"builtin_models不配adapter后端", schema.ExternalCliAgentSpec{CLIAgent: "openclaw",
			BuiltinModels: []schema.ExternalCliBuiltinModel{{Name: "sonnet"}}},
			"builtin_models is not valid for adapter-backed CLI agents"},
		{"codex不接受command", schema.ExternalCliAgentSpec{CLIAgent: "codex", Command: []string{"/bin/x"}},
			"Codex SDK config does not support command; use cli_path to select a custom executable"},
		{"codex_idle必须大于0", schema.ExternalCliAgentSpec{CLIAgent: "codex", CodexTurnIdleTimeoutS: f64(0)},
			"codex_turn_idle_timeout_s must be greater than 0"},
		{"codex_retries必须非负", schema.ExternalCliAgentSpec{CLIAgent: "codex", CodexTurnIdleRetries: i(-1)},
			"codex_turn_idle_retries must be greater than or equal to 0"},
		{"claude缓冲至少1", schema.ExternalCliAgentSpec{CLIAgent: "claude", ClaudeMaxBufferSize: i(0)},
			"claude_max_buffer_size must be greater than or equal to 1"},
		{"builtin默认effort归属", schema.ExternalCliAgentSpec{CLIAgent: "claude",
			BuiltinModels: []schema.ExternalCliBuiltinModel{{Name: "sonnet", Efforts: []string{"low"}, DefaultEffort: "high"}}},
			"builtin model 'sonnet' default_effort must be one of its efforts"},
		{"builtin名单唯一", schema.ExternalCliAgentSpec{CLIAgent: "claude",
			BuiltinModels: []schema.ExternalCliBuiltinModel{{Name: "sonnet"}, {Name: "sonnet"}}},
			"builtin_models names must be unique"},
	}
	for _, tc := range reds {
		spec := tc.spec
		err := spec.Validate()
		s.Require().Error(err, "%s 必须报错", tc.name)
		s.Contains(err.Error(), tc.want, "%s 报文逐字对齐 team.py", tc.name)
	}

	// 绿：default_effort 归位 + claude 专属配置组合
	okClaude := schema.ExternalCliAgentSpec{CLIAgent: "claude",
		BuiltinModels: []schema.ExternalCliBuiltinModel{{Name: "sonnet", Efforts: []string{"low"}, DefaultEffort: "low"}}}
	s.NoError(okClaude.Validate())
}

// TestTeamAgentSpecValidate_嵌套校验与mcp默认命令 TeamAgentSpec.Validate 逐条
// 跑 external_cli_agents 子模型校验（blueprint.go:306），mcp_server_command
// 零值时施加 Python 默认 factory（team.py:458）。
func (s *ConfigRoundtripSuite) TestTeamAgentSpecValidate_嵌套校验与mcp默认命令() {
	spec := &schema.TeamAgentSpec{
		TeamName: "pay",
		ExternalCliAgents: []schema.ExternalCliAgentSpec{
			{CLIAgent: "openclaw", CodexBin: "/bin/codex"},
		},
	}
	err := spec.Validate()
	s.Require().Error(err, "嵌套子模型校验必须传导")
	s.Contains(err.Error(), "codex_bin is only valid when cli_agent='codex'")

	ok := &schema.TeamAgentSpec{
		TeamName:          "pay",
		ExternalCliAgents: []schema.ExternalCliAgentSpec{{CLIAgent: "openclaw"}},
	}
	s.Require().NoError(ok.Validate())
	s.Equal(schema.DefaultMCPServerCommand(), ok.ExternalCliAgents[0].MCPServerCommand,
		"mcp_server_command 零值施加默认 factory")
}

// TestBuildContextSeed_往返与materialize BuildContextSeed 的序列化往返：
// seed 随 JSON 往返保留（BuildContext 以 json:"-" 排除，blueprint.py:645/660），
// MaterializeBuildContext 经平台工厂（swarm.RegisterSwarmProviders（15.16f 改名，原 RegisterSwarmBuildContextFactory），
// registry.py:160-198）重建活载体，提取器取回路径。
//
// 15.16c：D-15.13-k 过渡默认工厂已回收——平台工厂是唯一注册面，
// per-member 键（language/member_name/role/member_card_id）不入 seed
// （context.py:199-205），由 derive() 填充。
//
// Python: materialize_build_context (blueprint.py:685-699)。
func (s *ConfigRoundtripSuite) TestBuildContextSeed_往返与materialize() {
	swarm.RegisterSwarmProviders()
	seed := map[string]any{
		"session_id":       "sess-1",
		"team_id":          "pay",
		"mode":             "team",
		"project_dir":      "/proj/dir",
		"team_outputs_dir": "/proj/outputs",
		"team_ws_root":     "/ws/root",
		"channel":          "web",
	}
	spec := &schema.TeamAgentSpec{TeamName: "pay", BuildContextSeed: seed}

	// JSON 往返：seed 保留，活载体缺席
	raw, err := json.Marshal(spec)
	s.Require().NoError(err)
	var received schema.TeamAgentSpec
	s.Require().NoError(json.Unmarshal(raw, &received))
	s.Equal(seed, received.BuildContextSeed, "seed 必须随序列化往返保留")
	s.Nil(received.BuildContext, "活载体不参与序列化")

	// materialize：平台工厂重建活载体，提取器取回路径
	s.Require().NoError(received.MaterializeBuildContext())
	s.Require().NotNil(received.BuildContext, "materialize 必须重建活载体")
	bc := received.BuildContext.(*buildctx.SwarmBuildContext)
	s.Equal("sess-1", bc.SessionID)
	s.Equal("pay", bc.TeamID)
	s.Equal("/proj/dir", bc.GetProjectDir(), "GetProjectDir 提取器（install_skills 门卫依赖）")
	s.Equal("/proj/outputs", bc.GetTeamOutputsDir(), "GetTeamOutputsDir 提取器")
	s.Equal("/ws/root", bc.TeamWsRoot)
	s.Equal("web", bc.Channel)
	s.Equal("team", bc.Mode)

	// 幂等：二次 materialize 不替换活载体
	first := received.BuildContext
	s.Require().NoError(received.MaterializeBuildContext())
	s.Same(first, received.BuildContext, "materialize 必须幂等")

	// 空 seed：materialize no-op
	empty := &schema.TeamAgentSpec{TeamName: "pay"}
	s.Require().NoError(empty.MaterializeBuildContext())
	s.Nil(empty.BuildContext, "无 seed 不得重建")
}
