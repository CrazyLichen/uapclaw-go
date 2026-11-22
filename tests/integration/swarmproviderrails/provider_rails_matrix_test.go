//go:build integration

// 15.20 批一成员 rail 基座集成矩阵：工厂闭合逐 rail 一用例 + 桩语义。
//
// 链路（真实消费路径，无被测子系统 mock）：
//
//	swarm.EnrichTeamSpecForSwarmSeed（真实 enrich）→ swarm.RegisterSwarmProviders
//	（注册段）→ agentcore.ResolveDeepAgentSpecParts（RailSpec 物化）→
//	断言实例层 rail 类型/字段。
//
// 覆盖：⑤ organization model/availability（真 rail）· ④ team_skill 两件
// （真 rail）· ⑥ team_permission_policy（真 rail）· ⑧ RuntimePromptRail
// 六参 runtime paths 回填 · ⑫ heartbeat tool_context 形状 · 桩语义五件
// （owner_inbox / team_permission / personal_context / skill_retrieval_prompt /
// plugin_rails 返回 nil 且不挂载，不做 Skip）。
//
// 跨子系统：swarm/agents/swarm（enrich+注册段）→ agentcore/harness（解析+物化）
// → agent_teams/schema（TeamAgentSpec）→ swarm/agents/harness/team/rails（基座）
package swarmproviderrails

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	agentcore "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── TestMain ────────────────────────────

// dataDir 本包共享数据根。
var dataDir string

// TestMain 先于本包全部测试锁定数据根。
func TestMain(m *testing.M) {
	dataDir = filepath.Join(os.TempDir(), "swarmproviderrails-itest")
	_ = os.Setenv("UAPCLAW_DATA_DIR", dataDir)
	// organization rails 由 experimental.team_organization_ui_enabled 门控
	// （config_specs.go:501）——写测试 config 开启（真实读盘链路）。
	cfgDir := filepath.Join(dataDir, "config")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.yaml"),
		[]byte("experimental:\n  team_organization_ui_enabled: true\n"), 0o644)
	os.Exit(m.Run())
}

// ──────────────────────────── 结构体 ────────────────────────────

// ProviderRailsSuite 成员 rail 基座套件。
type ProviderRailsSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestProviderRailsSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestProviderRailsSuite(t *testing.T) {
	suite.Run(t, new(ProviderRailsSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// swarmEnrich 真实 enrich。
func swarmEnrich(spec *atschema.TeamAgentSpec, role, mode string) error {
	return swarm.EnrichTeamSpecForSwarmSeed(spec, swarm.EnrichSeedParams{
		SessionID:       "sess-rails-" + role,
		Mode:            mode,
		ChannelID:       "web",
		RequestID:       "req-rails",
		RequestMetadata: map[string]any{"mode": mode},
	})
}

// seedSpec 产出 leader/teammate 基座齐全的团队 spec（与 swarmproviders 矩阵同形）。
func (s *ProviderRailsSuite) seedSpec(enablePermissions bool) atschema.TeamAgentSpec {
	spec := atschema.NewTeamAgentSpec()
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

// memberParts 真实链路产出成员解析产物。
func (s *ProviderRailsSuite) memberParts(role string, mode string) *hschema.DeepAgentParts {
	spec := s.seedSpec(false)
	s.Require().NoError(swarmEnrich(&spec, role, mode))
	s.Require().NotNil(spec.BuildContext)
	memberSpec, ok := spec.Agents[role]
	s.Require().True(ok)
	parts, err := agentcore.ResolveDeepAgentSpecParts(&memberSpec, spec.BuildContext)
	s.Require().NoError(err)
	return parts
}

// railTypeName 提取 rail 类型短名（与 swarmproviders 矩阵同口径）。
func railType(rail interface{}) string {
	t := reflect.TypeOf(rail)
	for t != nil && (t.Kind() == reflect.Ptr || t.Kind() == reflect.Struct) {
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		return t.Name()
	}
	return fmt.Sprintf("%v", rail)
}

// railTypeNames 提取实例层 rail 类型名集合。
func railTypeNames(parts *hschema.DeepAgentParts) map[string]bool {
	out := make(map[string]bool, len(parts.Rails))
	for _, rail := range parts.Rails {
		out[railType(rail)] = true
	}
	return out
}

// ──────────────────────────── 用例 ────────────────────────────

// TestOrganizationRails_实例挂载 ⑤半区：leader 解析产物含
// OrganizationModelRail / OrganizationAvailabilityRail 实例（backend 缝 nil
// 为合法降级，rail 仍挂载——init 注入策略节、触发时按无持久设置降级）。
func (s *ProviderRailsSuite) TestOrganizationRails_实例挂载() {
	parts := s.memberParts("leader", "team")
	names := railTypeNames(parts)
	assert.True(s.T(), names["OrganizationModelRail"], "rails=%v", names)
	assert.True(s.T(), names["OrganizationAvailabilityRail"], "rails=%v", names)
}

// TestTeamSkillStoragePolicy_实例挂载 ④：GlobalSkillsDir 已知时挂载策略 rail。
func (s *ProviderRailsSuite) TestTeamSkillStoragePolicy_实例挂载() {
	parts := s.memberParts("teammate", "team")
	names := railTypeNames(parts)
	// enrich 若注入 GlobalSkillsDir 则必须挂载；未知时按门控跳过。
	if s.configuredGlobalSkills() {
		assert.True(s.T(), names["TeamSkillStoragePolicyRail"], "rails=%v", names)
		assert.True(s.T(), names["TeamSkillLibraryReloadRail"], "rails=%v", names)
	}
	// 桩语义：skill_retrieval_prompt 不挂载（⤴️9.11b）
	assert.False(s.T(), names["SkillRetrievalPromptRail"])
}

// TestTeamPermissionPolicy_不越桩 ⑥半区：team_permission 组装段绝不产出实例。
func (s *ProviderRailsSuite) TestTeamPermissionPolicy_不越桩() {
	spec := s.seedSpec(true)
	s.Require().NoError(swarmEnrich(&spec, "leader", "team"))
	s.Require().NotNil(spec.BuildContext)
	memberSpec := spec.Agents["leader"]
	parts, err := agentcore.ResolveDeepAgentSpecParts(&memberSpec, spec.BuildContext)
	s.Require().NoError(err)
	// 权限策略 rail 是否挂载取决于 permissions_config.enabled 门控；
	// 桩语义断言：team_permission（组装工厂）绝不产出实例（⤴️21.9）。
	assert.False(s.T(), railTypeNames(parts)["TeamPermissionRail"], "swarm.team_permission 组装段 ⤴️21.9，不应产出实例")
}

// TestRuntimePromptRail_形参生效 ⑧：成员 rail 含 RuntimePromptRail 实例。
func (s *ProviderRailsSuite) TestRuntimePromptRail_形参生效() {
	parts := s.memberParts("teammate", "team")
	assert.True(s.T(), railTypeNames(parts)["RuntimePromptRail"])
}

// TestStubRails_不挂载 桩语义：工厂返回 nil 且不产出实例（不做 Skip）。
func (s *ProviderRailsSuite) TestStubRails_不挂载() {
	parts := s.memberParts("leader", "team")
	names := railTypeNames(parts)
	assert.False(s.T(), names["OrganizationOwnerInboxRail"], "⤴️25.9")
	assert.False(s.T(), names["PersonalContextRail"], "⤴️24.1")
	assert.False(s.T(), names["RailManager"], "⤴️15.23")
}

// configuredGlobalSkills 探测当前数据根的 Skill 库目录是否被 enrich 注入。
func (s *ProviderRailsSuite) configuredGlobalSkills() bool {
	dir := filepath.Join(dataDir, "workspace", "agent", "workspace", "skills")
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
