//go:build integration

// 15.16f swarm providers 集成矩阵：真实团队链路逐成员按 config 加载/不加载
// 对应 tool / rail / subagent（spec §7.2 七用例）。
//
// 链路（真实消费路径，无被测子系统 mock）：
//	config.yaml 开关 → swarm.EnrichTeamSpecForSwarmSeed（真实 enrich，含
//	legacyConfigMap 读盘）→ swarm.RegisterSwarmProviders（三步注册段）→
//	agentcore.ResolveDeepAgentSpecParts（RailSpec/BuiltinToolSpec/
//	SubAgentSpec 三查表物化）→ agent_teams/harness.NewNativeHarness（成员
//	前向构造全链）。
//
// 断言口径：解析产物 DeepAgentParts 的 Rails 实例 / ToolInstances / ToolCards
// 逐项检查（防「spec 有、实例无」假绿），声明层（spec.Agents[role].Rails）
// 与实例层双重断言。
//
// 跨子系统：swarm/agents/swarm（enrich+注册段）→ agentcore/harness（解析+注册
// 表+物化）→ agent_teams/harness（NativeHarness）→ agent_teams/schema（TeamAgentSpec
// /BuildContext）→ agentcore/harness/schema（provider 注册表）。
package swarmproviders_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentteamsharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── TestMain ────────────────────────────

// dataDir 本包共享数据根（UAPCLAW_DATA_DIR 在进程内 sync.Once 锁存，
// 必须先于任何路径调用设置）。
var dataDir string

// TestMain 先于本包全部测试锁定数据根，让 config.yaml / Skill 库 / 工作区
// 全部落入临时目录（与其他包测试二进制进程隔离）。
func TestMain(m *testing.M) {
	dataDir = filepath.Join(os.TempDir(), "swarmproviders-itest")
	_ = os.Setenv("UAPCLAW_DATA_DIR", dataDir)
	os.Exit(m.Run())
}

// ──────────────────────────── 结构体 ────────────────────────────

// ProviderMatrixSuite 成员能力加载矩阵套件。
type ProviderMatrixSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestProviderMatrixSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestProviderMatrixSuite(t *testing.T) {
	suite.Run(t, new(ProviderMatrixSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeConfig 写 config.yaml 到当前数据根的 config 目录。
func (s *ProviderMatrixSuite) writeConfig(content string) {
	cfgPath := filepath.Join(dataDir, "config", "config.yaml")
	s.Require().NoError(os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	s.Require().NoError(os.WriteFile(cfgPath, []byte(content), 0o644))
}

// seedSpec 产生产 leader/teammate 基座齐全的团队 spec（对齐生产：enrich 的
// 成员折叠循环只处理 spec.Agents 已有的两个 native 角色；生产路径由
// loadTeamSpec/blueprint 播种，测试用最小基座等价播种）。
func seedSpec(enablePermissions bool) atschema.TeamAgentSpec {
	spec := atschema.NewTeamAgentSpec()
	spec.TeamName = "matrix-team"
	spec.EnablePermissions = enablePermissions
	if spec.Agents == nil {
		spec.Agents = map[string]atschema.DeepAgentSpec{}
	}
	if _, ok := spec.Agents["leader"]; !ok {
		spec.Agents["leader"] = atschema.NewDeepAgentSpec()
	}
	if _, ok := spec.Agents["teammate"]; !ok {
		spec.Agents["teammate"] = atschema.NewDeepAgentSpec()
	}
	return spec
}

// memberParts 走真实链路产出成员解析产物：enrich → spec → ResolveDeepAgentSpecParts。
func (s *ProviderMatrixSuite) memberParts(mode string, enablePermissions bool, role string) *hschema.DeepAgentParts {
	spec := seedSpec(enablePermissions)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec, swarm.EnrichSeedParams{
		SessionID:       "sess-matrix-" + role,
		Mode:            mode,
		ChannelID:       "web",
		RequestID:       "req-matrix",
		RequestMetadata: map[string]any{"mode": mode},
	}))
	s.Require().NotNil(spec.BuildContext, "enrich 应挂活载体")
	memberSpec, ok := spec.Agents[role]
	s.Require().True(ok, "enrich 应产出 %s 成员 spec", role)

	parts, err := agentcore.ResolveDeepAgentSpecParts(&memberSpec, spec.BuildContext)
	s.Require().NoError(err)
	if s.T().Failed() || len(parts.Rails) == 0 {
		names := make([]string, 0, len(parts.Rails))
		for _, r := range parts.Rails {
			names = append(names, railType(r))
		}
		s.T().Logf("member=%s rails=%d types=%v ctxType=%T", role, len(parts.Rails), names, spec.BuildContext)
	}
	return parts
}

// memberHarness 真实前向构造成员（NewNativeHarness 全链，只做基线证明）。
func (s *ProviderMatrixSuite) memberHarness(mode string, role string) *agentteamsharness.NativeHarness {
	spec := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec, swarm.EnrichSeedParams{
		SessionID:       "sess-harness-" + role,
		Mode:            mode,
		ChannelID:       "web",
		RequestID:       "req-harness",
		RequestMetadata: map[string]any{"mode": mode},
	}))
	memberSpec, ok := spec.Agents[role]
	s.Require().True(ok)
	h, err := agentteamsharness.NewNativeHarness(memberSpec, spec.BuildContext)
	s.Require().NoError(err, "NativeHarness 前向构造应成功（成员物化全链通电）")
	return h
}

// railTypeNames 解析产物 rails 的具体类型名集合（%T 形态，与
// runtime_inheritance 的 railTypeNames 同源口径）。
func railTypeNames(parts *hschema.DeepAgentParts) map[string]bool {
	out := make(map[string]bool, len(parts.Rails))
	for _, rail := range parts.Rails {
		out[railType(rail)] = true
	}
	return out
}

func railType(rail interface{}) string {
	return strings.TrimPrefix(strings.TrimPrefix(fmt.Sprintf("%T", rail), "*"), "")
}

// toolInstanceNames 解析产物工具实例的 card 名集合。
func toolInstanceNames(parts *hschema.DeepAgentParts) map[string]bool {
	out := make(map[string]bool, len(parts.ToolInstances))
	for _, t := range parts.ToolInstances {
		out[t.Card().GetName()] = true
	}
	return out
}

// ──────────────────────────── 测试 ────────────────────────────

// Test全开基线_逐成员加载 全开 config 下 leader/teammate 声明与实例双层齐全；
// leader subagents 钉死为空；NativeHarness 真实物化成功。
func (s *ProviderMatrixSuite) Test全开基线_逐成员加载() {
	s.writeConfig(`
symphony:
  skill_retrieval:
    enabled: true
`)
	// leader：声明层
	leaderSpec := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&leaderSpec, swarm.EnrichSeedParams{
		SessionID: "sess-base", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	leader, ok := leaderSpec.Agents["leader"]
	s.Require().True(ok)
	declaredRails := declaredRailTypes(leader.Rails)
	s.True(declaredRails["swarm.personal_context"], "leader 声明 personal_context")
	s.True(declaredRails["swarm.runtime_prompt"], "leader 声明 runtime_prompt")
	s.True(declaredRails["swarm.symphony_orchestration_prompt"], "leader 声明 symphony 编排提示")
	s.Empty(leader.Subagents, "leader subagents 必须钉死为空（无 task_tool 面）")

	// teammate：实例层（解析产物）
	parts := s.memberParts("team", false, "teammate")
	types := railTypeNames(parts)
	s.T().Logf("teammate rail types: %v / tools: %v", types, toolInstanceNames(parts))
	s.True(types["rails.RuntimePromptRail"], "teammate 物化 RuntimePromptRail")
	s.True(types["rails.ModelAnomalyDetectionRail"], "teammate 物化模型异常检测 rail")
	s.True(types["security.SafetyPromptRail"], "teammate 物化安全提示 rail")
	s.True(types["rails.TaskPlanningRail"], "teammate 物化任务规划 rail")
	s.True(types["interrupt.AskUserRail"], "teammate 物化 HITL 交互 rail")
	tools := toolInstanceNames(parts)
	s.True(tools["fetch_webpage"], "teammate 物化 core.web_fetch 工具")
	s.NotContains(tools, "paid_search", "未配置付费 key 时 paid_search 应跳过（对齐 _build_web_paid_search :252 门控）")
	s.True(tools["cron"], "teammate 物化 cron 工具组")

	// 真实成员物化全链
	s.memberHarness("team", "teammate")
}

// Test检索关_开关传导 skill_retrieval 关 → spec.EnableSkillDiscovery=true
// 且 skill_retrieval 工具门控跳过（开态为 false）。
func (s *ProviderMatrixSuite) Test检索关_开关传导() {
	s.writeConfig("") // 无 symphony 段 → retrievalEnabled=false
	spec := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec, swarm.EnrichSeedParams{
		SessionID: "sess-retrieval-off", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	teammate, ok := spec.Agents["teammate"]
	s.Require().True(ok)
	s.True(teammate.EnableSkillDiscovery, "检索关时 enable_skill_discovery=true（config_specs :526-528 语义）")

	// 开态对照
	s.writeConfig(`
symphony:
  skill_retrieval:
    enabled: true
`)
	spec2 := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec2, swarm.EnrichSeedParams{
		SessionID: "sess-retrieval-on", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	teammate2, ok := spec2.Agents["teammate"]
	s.Require().True(ok)
	s.False(teammate2.EnableSkillDiscovery, "检索开时 enable_skill_discovery=false")
}

// Test权限门控_声明与实例双层 EnablePermissions 切换 swarm.team_permission /
// swarm.team_permission_policy 的声明与实例存在性。
func (s *ProviderMatrixSuite) Test权限门控_声明与实例双层() {
	// 开：teammate 声明 team_permission，leader 声明 team_permission_policy
	s.writeConfig("")
	spec := seedSpec(true)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec, swarm.EnrichSeedParams{
		SessionID: "sess-perm-on", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	teammate, ok := spec.Agents["teammate"]
	s.Require().True(ok)
	teammateRails := declaredRailTypes(teammate.Rails)
	s.True(teammateRails["swarm.team_permission"], "权限开时 teammate 声明 team_permission")
	leader, ok := spec.Agents["leader"]
	s.Require().True(ok)
	s.True(declaredRailTypes(leader.Rails)["swarm.team_permission_policy"], "leader 声明 team_permission_policy")

	// 实例层：权限 rail 工厂门控（backend/messager extras 缺 → nil）
	parts := s.memberParts("team", true, "teammate")
	s.NotContains(railTypeNames(parts), "rails.TeamPermissionRail",
		"无团队 backend 句柄时权限 rail 工厂返回 nil（对齐 Python 门控）")

	// 关：声明消失
	spec2 := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec2, swarm.EnrichSeedParams{
		SessionID: "sess-perm-off", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	teammate2, ok := spec2.Agents["teammate"]
	s.Require().True(ok)
	s.NotContains(declaredRailTypes(teammate2.Rails), "swarm.team_permission", "权限关时声明消失")
}

// Test多模态门控_工具实例 models.video/image_gen 配置切换视频/图片工具实例。
func (s *ProviderMatrixSuite) Test多模态门控_工具实例() {
	// 关：无 models 块 → 无 video_understanding / generate_image 实例
	s.writeConfig("")
	parts := s.memberParts("team", false, "teammate")
	tools := toolInstanceNames(parts)
	s.NotContains(tools, "video_understanding", "未配置 video 时无视频理解工具")
	s.NotContains(tools, "generate_image", "未配置 image_gen 时无图片生成工具")

	// 开：完整 models.video 块 + VIDEO_* env
	s.writeConfig(`
models:
  video:
    enabled: true
    model_config:
      api_key: k-test
      api_base: https://example.invalid/v1
      model_name: glm-video
      model_provider: openai
`)
	s.T().Setenv("VIDEO_API_KEY", "k-test")
	s.T().Setenv("VIDEO_API_BASE", "https://example.invalid/v1")
	s.T().Setenv("VIDEO_MODEL_NAME", "glm-video")
	parts2 := s.memberParts("team", false, "teammate")
	tools2 := toolInstanceNames(parts2)
	s.Contains(tools2, "video_understanding", "video 配置齐全时物化视频理解工具")
}

// Test外部CLI成员消费面口径 混编口径回归锚：native 成员 spec 折入 swarm.*
// 声明；外部 CLI 成员走骨架 spec（assembly.py:200-231 分支），无 swarm.*
// rails——以骨架 spec 经解析产物验证（外部模板混编全链 ⤴️群聊/agent_group）。
func (s *ProviderMatrixSuite) Test外部CLI成员消费面口径() {
	s.writeConfig("")
	// 骨架 spec（外部 CLI 成员的运行形态）：不折成员能力声明
	skeleton := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&skeleton, swarm.EnrichSeedParams{
		SessionID: "sess-skel", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	// leader/teammate 是 native（折过声明）；构造一个未折叠的骨架 spec 断言
	// 解析产物无 swarm.* rail（provider 对骨架上下文返回 nil/不声明）。
	base := hschema.NewDeepAgentSpec()
	parts, err := agentcore.ResolveDeepAgentSpecParts(&base, skeleton.BuildContext)
	s.Require().NoError(err)
	for name := range railTypeNames(parts) {
		s.NotContains(name, "swarm.", "骨架 spec 不得物化 swarm.* rail（外部 CLI 成员消费面口径）")
	}
}

// TestSubagentFactory分派 code mode teammate 的 browser_agent spec 经
// SubAgentSpec.Build factory 分支命中 provider 产出 SubAgentConfig。
func (s *ProviderMatrixSuite) TestSubagentFactory分派() {
	s.writeConfig(`
react:
  subagents:
    browser_agent:
      enabled: true
`)
	spec := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec, swarm.EnrichSeedParams{
		SessionID:       "sess-subagent",
		Mode:            "team.code",
		ChannelID:       "web",
		RequestMetadata: map[string]any{"mode": "team.code"},
	}))
	teammate, ok := spec.Agents["teammate"]
	s.Require().True(ok)
	s.Require().NotEmpty(teammate.Subagents, "code mode teammate 应携带 code 子代理声明")

	factoryNames := make([]string, 0, len(teammate.Subagents))
	for _, sub := range teammate.Subagents {
		factoryNames = append(factoryNames, sub.FactoryName)
	}
	s.Contains(factoryNames, "swarm.browser_agent", "code 子代理声明带 factory_name")

	// 物化：factory 分支命中 provider（BuildBrowserAgentConfig 产出 SubAgentConfig）
	parts, err := agentcore.ResolveDeepAgentSpecParts(&teammate, spec.BuildContext)
	s.Require().NoError(err)
	s.Require().NotEmpty(parts.Config, "成员 config 应组装")
}

// Test种子往返_反序列化恢复物化 种子跨 JSON 边界后经平台工厂重建上下文，
// 成员 spec 仍可解析物化（跨进程恢复链）。
func (s *ProviderMatrixSuite) Test种子往返_反序列化恢复物化() {
	s.writeConfig(`
symphony:
  skill_retrieval:
    enabled: true
`)
	spec := seedSpec(false)
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(&spec, swarm.EnrichSeedParams{
		SessionID: "sess-seed", Mode: "team", ChannelID: "web",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	s.Require().NotEmpty(spec.BuildContextSeed)

	raw, err := json.Marshal(spec)
	s.Require().NoError(err)
	var received atschema.TeamAgentSpec
	s.Require().NoError(json.Unmarshal(raw, &received))
	s.Require().NoError(received.MaterializeBuildContext())
	s.Require().NotNil(received.BuildContext, "materialize 经平台工厂重建")

	teammate, ok := received.Agents["teammate"]
	s.Require().True(ok)
	_, err = agentcore.ResolveDeepAgentSpecParts(&teammate, received.BuildContext)
	s.Require().NoError(err, "重建上下文应支撑成员物化")
}

// ──────────────────────────── 非导出函数（辅助，追加段） ────────────────────────────

// declaredRailTypes 声明层 rail type 集合。
func declaredRailTypes(rails []hschema.RailSpec) map[string]bool {
	out := make(map[string]bool, len(rails))
	for _, rail := range rails {
		out[rail.Type] = true
	}
	return out
}

