//go:build integration

package externalclipanel_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"

	externalcli "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PanelPipelineSuite 15.16d 第三方面板配置集成套件（黑盒）。
//
// 跨子系统：swarm/common（写链+展平+注册表）→ externalcli（catalog 探测
// + plan_external_model + ModelChoice）→ harness_providers（注册缝 fake
// provider）→ swarm/agents/harness/team（装配预处理）→ swarm/agents/swarm
// （transport 装配段）→ schema（spec 往返）。
type PanelPipelineSuite struct {
	isuite.AgentSuite
}

// fakePanelProvider 集成用 fake provider：声明 MODEL_DISCOVERY，上报固定清单。
type fakePanelProvider struct {
	reported map[string][]string
}

func (f *fakePanelProvider) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{
		Name:         "fake-panel",
		Capabilities: map[harnessprotocol.HarnessCapability]bool{harnessprotocol.HarnessCapabilityModelDiscovery: true},
	}
}

func (f *fakePanelProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return &fakePanelHarness{reported: f.reported}, nil
}

type fakePanelHarness struct {
	reported map[string][]string
}

func (h *fakePanelHarness) Card() harnessprotocol.HarnessCard { return harnessprotocol.HarnessCard{} }
func (h *fakePanelHarness) State() harnessprotocol.HarnessState {
	return harnessprotocol.HarnessStateIdle
}
func (h *fakePanelHarness) ProviderSessionID() *string { return nil }
func (h *fakePanelHarness) EventBufferConfig() harnessprotocol.EventBufferConfig {
	return harnessprotocol.EventBufferConfig{}
}
func (h *fakePanelHarness) Start(context.Context, harnessprotocol.HarnessContext) error { return nil }
func (h *fakePanelHarness) Stop(context.Context) error                                  { return nil }
func (h *fakePanelHarness) Events() harnessprotocol.HarnessEventCursor                  { return nil }
func (h *fakePanelHarness) TurnEvents(_ *string) harnessprotocol.HarnessEventCursor     { return nil }
func (h *fakePanelHarness) Send(context.Context, harnessprotocol.HarnessInput, harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	return harnessprotocol.SendReceipt{}, nil
}
func (h *fakePanelHarness) Abort(context.Context, harnessprotocol.AbortMode) error { return nil }
func (h *fakePanelHarness) Pause(context.Context) error                            { return nil }
func (h *fakePanelHarness) Resume(context.Context, *harnessprotocol.HarnessInput) error {
	return nil
}
func (h *fakePanelHarness) ExportCheckpoint(context.Context) (*harnessprotocol.HarnessCheckpoint, error) {
	return nil, nil
}
func (h *fakePanelHarness) SetModel(context.Context, harnessprotocol.ModelSelection) error {
	return nil
}
func (h *fakePanelHarness) ListModels(context.Context) ([]harnessprotocol.ModelOption, error) {
	options := []harnessprotocol.ModelOption{}
	for modelID, efforts := range h.reported {
		options = append(options, harnessprotocol.ModelOption{ModelID: modelID, Efforts: efforts})
	}
	return options, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPanelPipelineSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestPanelPipelineSuite(t *testing.T) {
	suite.Run(t, new(PanelPipelineSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestConfigSaveProbeFlattenAssemble 保存→探测→展平→装配全链：
// 开关组装写链 → 探测编排（fake provider 经注册缝上报模型清单）→ 报告落盘 →
// 四键展平 → 装配预处理 in-place 修正 + 自定义 agent 补 team-mcp 命令 →
// enrich 挂 transport。
func (s *PanelPipelineSuite) TestConfigSaveProbeFlattenAssemble() {
	harnessproviders.RegisterBuiltinProvider("panel-custom",
		func() (harnessprotocol.HarnessProvider, error) {
			return &fakePanelProvider{reported: map[string][]string{"m1": {"low"}}}, nil
		})
	defer harnessproviders.UnregisterHarnessProvider("panel-custom")

	// ① 保存写链：开关参数组装 entries（探测门 fake ok）
	raw := map[string]any{}
	params := map[string]any{
		"external_cli_agent_panel-custom_enabled":     true,
		"external_cli_agent_panel-custom_use_builtin": true,
	}
	entries, err := swarmcommon.ExternalCliAgentsFromSwitches(raw, params,
		[]string{"claude", "codex", "panel-custom"}, map[string]string{},
		func(cliAgent, requestedPath string, useBuiltin bool) map[string]any {
			return map[string]any{"status": "ok", "path": "/fake/cli"}
		})
	s.Require().NoError(err)
	s.Require().Len(entries, 1)
	s.Equal("panel-custom", entries[0]["cli_agent"])

	// ② 探测编排：新启用 kind 无目录 → 播种目录回写回调收到 CLI 上报清单
	var seeded map[string][]map[string]any
	entryList := make([]any, 0, len(entries))
	for _, e := range entries {
		entryList = append(entryList, e)
	}
	warnings := external.RefreshExternalCliBuiltinModels(context.Background(),
		entryList, []string{"panel-custom"}, func(c map[string][]map[string]any) { seeded = c })
	s.Empty(warnings)
	s.Require().NotNil(seeded)
	s.Equal("m1", seeded["panel-custom"][0]["name"])
	reports := external.LoadReportedModels()
	s.Equal(map[string][]string{"m1": {"low"}}, reports["panel-custom"], "报告落盘")

	// ③ 四键展平（读面）——kind 未配置 enabled 时全 false，配置后 true
	flat := swarmcommon.FlattenExternalCliAgentsForConfigPanel(
		map[string]any{"modes": map[string]any{"team": map[string]any{"jiuwen_team": map[string]any{
			"external_cli_agents": []any{map[string]any{"cli_agent": "panel-custom"}},
		}}}},
		[]string{"claude", "codex", "panel-custom"}, nil)
	s.Equal("true", flat["external_cli_agent_panel-custom_enabled"])
	s.Equal("true", flat["external_cli_agent_panel-custom_use_builtin"])

	// ④ 装配预处理：声明目录被报告 in-place 修正（gone 剔除）
	specDict := map[string]any{"external_cli_agents": []any{
		map[string]any{"cli_agent": "panel-custom", "builtin_models": []any{
			map[string]any{"name": "gone"}, map[string]any{"name": "m1"},
		}},
	}}
	team.PrepareExternalCliAgents(specDict)
	corrected := specDict["external_cli_agents"].([]any)[0].(map[string]any)["builtin_models"].([]any)
	s.Require().Len(corrected, 1)
	s.Equal("m1", corrected[0].(map[string]any)["name"])

	// ⑤ enrich 挂 transport（assembly.py:425 对齐）
	spec := &schema.TeamAgentSpec{TeamName: "panel-team"}
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(spec, swarm.EnrichSeedParams{
		SessionID: "sess-panel", Mode: "team", ChannelID: "web",
	}))
	s.Require().NotNil(spec.ExternalTransport)
	s.Equal("hybrid", spec.ExternalTransport.Type)
	s.Equal("panel-team", spec.ExternalTransport.Params["team_name"])
	s.NotEmpty(spec.ExternalTransport.Params["external_publish_url"])
}

// TestDsh能力声明决定探测跳过 dsh 未声明 MODEL_DISCOVERY（两侧对齐的上游
// 声明）：启用 dsh 的保存触发探测 → dsh 的 ListModels 默认体报不支持 →
// 探测失败保旧报告、播种目录不产生、保存不被阻塞（"not using built-in model
// selection pays nothing"）。
func (s *PanelPipelineSuite) TestDsh能力声明决定探测跳过() {
	entries := []any{map[string]any{"cli_agent": "dsh"}}
	var catalogs map[string][]map[string]any
	warnings := external.RefreshExternalCliBuiltinModels(context.Background(),
		entries, []string{"dsh"}, func(c map[string][]map[string]any) { catalogs = c })
	// 探测失败不阻塞：只警告（失败详情进日志）
	s.Empty(warnings)
	s.Nil(catalogs, "未声明 MODEL_DISCOVERY 的 kind 不产生播种目录")
	s.NotContains(external.LoadReportedModels(), "dsh", "dsh 上报不落盘（探测失败保旧报告）")
}

// TestSpawnModelChoiceChain spawn 模型选择链：装配修正后的目录 →
// PlanExternalModel 校验解析 → ExternalCliModelConfig → ModelChoice 形状。
func (s *PanelPipelineSuite) TestSpawnModelChoiceChain() {
	// builtin 命中：无端点仅 effort
	entry := &schema.ExternalCliAgentSpec{
		CLIAgent:      "panel-custom",
		BuiltinModels: []schema.ExternalCliBuiltinModel{{Name: "m1", Efforts: []string{"low"}}},
	}
	plan, err := external.PlanExternalModel(external.PlanArgs{
		CLIAgent: "panel-custom", BuiltinHint: "m1", EffortHint: "low",
	}, entry, nil)
	s.Require().NoError(err)
	s.Require().NotNil(plan.Primary)
	s.Equal("m1", plan.Primary.Model)
	s.Equal("low", plan.Primary.Effort)

	choice := external.ModelChoice(plan.Primary)
	s.Require().NotNil(choice)
	s.Require().NotNil(choice.Model)
	s.Equal("m1", *choice.Model)
	s.Nil(choice.Endpoint, "builtin 挑选无端点（CLI 自身登录态）")

	// 目录外模型 → PlanError 双受众文案（非静默透传）
	_, err = external.PlanExternalModel(external.PlanArgs{
		CLIAgent: "panel-custom", BuiltinHint: "ghost",
	}, entry, nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "this cli_agent offers: m1")
}

// TestTransportRelayWired transport 回传链两端接通：装配挂载的
// ExternalTransport 与 15.16b 网关中继的 URL 形状一致，且 schema 往返不丢。
func (s *PanelPipelineSuite) TestTransportRelayWired() {
	spec := &schema.TeamAgentSpec{TeamName: "relay-team"}
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(spec, swarm.EnrichSeedParams{
		SessionID: "sess-relay", Mode: "team", ChannelID: "web",
	}))
	s.Require().NotNil(spec.ExternalTransport)
	url, _ := spec.ExternalTransport.Params["external_publish_url"].(string)
	s.Equal("ws://127.0.0.1:19000/ws", url, "web 渠道默认端口与网关 team.mq.publish 中继一致")

	// schema 往返：ExternalTransport 跨 JSON 边界不丢（spawned teammate 场景）
	raw, err := json.Marshal(spec)
	s.Require().NoError(err)
	var received schema.TeamAgentSpec
	s.Require().NoError(json.Unmarshal(raw, &received))
	s.Require().NotNil(received.ExternalTransport, "external_transport 必须跨序列化边界保留")
	s.Equal("hybrid", received.ExternalTransport.Type)
	s.Equal("relay-team", received.ExternalTransport.Params["team_name"])

	// tui 渠道：装配按渠道派生中继 URL
	specTui := &schema.TeamAgentSpec{TeamName: "relay-tui"}
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(specTui, swarm.EnrichSeedParams{
		SessionID: "sess-tui", Mode: "team", ChannelID: "tui",
	}))
	s.Require().NotNil(specTui.ExternalTransport)
	s.Equal("ws://127.0.0.1:19001/tui", specTui.ExternalTransport.Params["external_publish_url"])
}
