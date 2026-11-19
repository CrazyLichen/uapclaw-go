// Python 源：agent_group.py / manifest.py / bound_group.py 用例 1:1 转 Go（T4）
//
// 场景对应（讨论点 → 用例）：
//   - manifest 校验（package_type/name==目录/roster 含 leader/名正则/去重）
//     → TestLoadAgentGroupPackageBundle
//   - instruction 段追加 + 共享 skills 并集 + 成员身份=目录名 → 同上
//   - 非 leader 成员禁 AGENT.md → 同上
//   - manifest 翻译（provider 白名单/版本/sdk_paths/未知 config 键/
//     unsupported 报错/skills append）→ TestExternalCliAgentSpecFromTemplate
//   - merge（基座声明 + 模板非默认键 + skills 去重追加 + kind 不匹配）
//     → TestMergeExternalCliConfig
//   - bound_group 提取 → TestBoundAgentGroupOf

package experts_test

import (
	"encoding/json"
	experts "github.com/uapclaw/uapclaw-go/internal/agent_teams/experts"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	external "github.com/uapclaw/uapclaw-go/internal/agent_teams/external"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// fixtureGroup 建一个最小合法 AgentGroup 包（t.TempDir）。
func fixtureGroup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := filepath.Base(dir)
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write("manifest.json", `{"package_type":"agent_group","name":"`+name+`","instruction":"be nice","agents":["leader","writer"]}`)
	write("agents/leader/manifest.json", `{"package_type":"agent_template","name":"Leader Display","description":"leader","persona":{"dir":"."}}`)
	write("agents/leader/AGENT.md", "# leader rules")
	write("agents/leader/persona.md", "leader persona")
	write("agents/writer/manifest.json", `{"package_type":"agent_template","name":"Writer Display","description":"writer","persona":{"dir":"."}}`)
	write("agents/writer/persona.md", "writer persona")
	return dir
}

func TestLoadAgentGroupPackageBundle(t *testing.T) {
	t.Run("加载校验改写全链", func(t *testing.T) {
		dir := fixtureGroup(t)
		pkg, err := experts.LoadAgentGroupPackageBundle(dir)
		require.NoError(t, err)
		require.Len(t, pkg.Templates, 2)
		// 成员身份 = 目录名（agent_card.id 改写），manifest name 保持展示名
		assert.Equal(t, "leader", pkg.Templates["leader"].AgentCard.BaseCard.ID)
		assert.Equal(t, "Leader Display", pkg.Templates["leader"].AgentCard.BaseCard.Name)
		// instruction 追加 agent_group_instruction 段
		found := false
		for _, section := range pkg.Templates["writer"].PromptSections {
			if section.Name == "agent_group_instruction" {
				found = true
				assert.Equal(t, 20, section.Priority)
			}
		}
		assert.True(t, found)
		assert.Equal(t, "be nice", pkg.Instruction())
	})
	t.Run("package_type错误", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
			[]byte(`{"package_type":"agent_template","agents":["leader"]}`), 0o644))
		_, err := experts.LoadAgentGroupPackageBundle(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "package_type")
	})
	t.Run("name与目录不匹配", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
			[]byte(`{"package_type":"agent_group","name":"other","agents":["leader"]}`), 0o644))
		_, err := experts.LoadAgentGroupPackageBundle(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must match directory")
	})
	t.Run("花名册缺leader", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
			[]byte(`{"package_type":"agent_group","name":"`+filepath.Base(dir)+`","agents":["writer"]}`), 0o644))
		_, err := experts.LoadAgentGroupPackageBundle(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must contain 'leader'")
	})
	t.Run("花名册名非法", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
			[]byte(`{"package_type":"agent_group","name":"`+filepath.Base(dir)+`","agents":["Leader"]}`), 0o644))
		_, err := experts.LoadAgentGroupPackageBundle(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid agent name")
	})
	t.Run("非leader成员禁AGENT_md", func(t *testing.T) {
		dir := fixtureGroup(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "agents/writer/AGENT.md"), []byte("x"), 0o644))
		_, err := experts.LoadAgentGroupPackageBundle(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not contain AGENT.md")
	})
	t.Run("leader不支持外部runtime", func(t *testing.T) {
		dir := fixtureGroup(t)
		mPath := filepath.Join(dir, "agents/leader/manifest.json")
		data, _ := os.ReadFile(mPath)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(data, &payload))
		payload["runtime"] = map[string]any{"provider_name": "codex"}
		updated, _ := json.Marshal(payload)
		require.NoError(t, os.WriteFile(mPath, updated, 0o644))
		_, err := experts.LoadAgentGroupPackageBundle(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support external runtime")
	})
}

func TestReadAgentGroupCard(t *testing.T) {
	dir := fixtureGroup(t)
	card, err := experts.ReadAgentGroupCard(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(dir), card.Name)
	assert.Equal(t, "be nice", card.Instruction)
	require.Len(t, card.Members, 2)
	assert.True(t, card.Members[0].IsLeader())
	assert.Equal(t, filepath.Base(dir)+"/writer", card.Members[1].Ref().Label())
}

func TestGroupCapabilities(t *testing.T) {
	assert.Empty(t, experts.GroupCapabilities(map[string]any{}))
	assert.Equal(t, []string{"a", "b"}, experts.GroupCapabilities(map[string]any{
		"capabilities": []any{"a", map[string]any{"id": "b"}, "a", ""},
	}))
	assert.Equal(t, []string{"zh-tag"}, experts.GroupCapabilities(map[string]any{
		"tags": []any{map[string]any{"zh": "zh-tag"}},
	}))
}

func TestExternalCliAgentSpecFromTemplate(t *testing.T) {
	t.Run("runtime缺失得nil", func(t *testing.T) {
		spec, err := external.ExternalCliAgentSpecFromTemplate(&hschema.AgentTemplateSpec{})
		require.NoError(t, err)
		assert.Nil(t, spec)
	})
	t.Run("provider白名单", func(t *testing.T) {
		tpl := &hschema.AgentTemplateSpec{Runtime: &hschema.AgentRuntimeSpec{ProviderName: "dsh"}}
		_, err := external.ExternalCliAgentSpecFromTemplate(tpl)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be 'codex' or 'claudecode'")
	})
	t.Run("未知config键拒绝", func(t *testing.T) {
		tpl := &hschema.AgentTemplateSpec{Runtime: &hschema.AgentRuntimeSpec{
			ProviderName: "codex",
			Config:       map[string]any{"bogus_key": "x"},
		}}
		_, err := external.ExternalCliAgentSpecFromTemplate(tpl)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "runtime.config cannot set: bogus_key")
	})
	t.Run("unsupported能力面拒绝", func(t *testing.T) {
		tpl := &hschema.AgentTemplateSpec{
			Runtime: &hschema.AgentRuntimeSpec{ProviderName: "codex"},
			Tools:   []hschema.BuiltinToolSpec{{Type: "x"}},
		}
		_, err := external.ExternalCliAgentSpecFromTemplate(tpl)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support: tools")
	})
	t.Run("合法翻译_append与skills", func(t *testing.T) {
		tpl := &hschema.AgentTemplateSpec{
			Runtime: &hschema.AgentRuntimeSpec{ProviderName: "codex"},
			Skills:  []hschema.SkillSpec{{Dir: "/s/a", Mode: "all"}},
		}
		spec, err := external.ExternalCliAgentSpecFromTemplate(tpl)
		require.NoError(t, err)
		assert.Equal(t, "codex", spec.CLIAgent)
		assert.Equal(t, "append", spec.SkillConflict)
		assert.Len(t, spec.Skills, 1)
	})
}

func TestMergeExternalCliConfig(t *testing.T) {
	declared := &atschema.ExternalCliAgentSpec{CLIAgent: "codex", SkillConflict: "skip",
		Skills: []any{map[string]any{"dir": "/declared"}}}
	tpl := &atschema.ExternalCliAgentSpec{CLIAgent: "codex", SkillConflict: "append",
		Skills: []any{map[string]any{"dir": "/declared"}, map[string]any{"dir": "/template"}}}
	t.Run("kind不匹配报错", func(t *testing.T) {
		_, err := external.MergeExternalCliConfig(declared, &atschema.ExternalCliAgentSpec{CLIAgent: "claude"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "declared config is for")
	})
	t.Run("基座声明加模板非默认键", func(t *testing.T) {
		merged, err := external.MergeExternalCliConfig(declared, tpl)
		require.NoError(t, err)
		assert.Equal(t, "append", merged.SkillConflict)
		assert.Len(t, merged.Skills, 2) // 去重追加
	})
}

func TestBoundAgentGroupOf(t *testing.T) {
	t.Run("未绑定得nil", func(t *testing.T) {
		assert.Nil(t, experts.BoundAgentGroupOf(&atschema.TeamAgentSpec{}))
	})
	t.Run("外部成员与快照成员分取", func(t *testing.T) {
		spec := &atschema.TeamAgentSpec{
			AgentGroupName: "g1",
			PredefinedMembers: []atschema.TeamMemberSpec{
				{MemberName: "cli1", DisplayName: "CLI", RoleType: atschema.TeamRoleTeammate,
					Prompt: "p1", ExternalCLI: &atschema.ExternalCliAgentSpec{CLIAgent: "codex"}},
				{MemberName: "nat1", DisplayName: "Nat", RoleType: atschema.TeamRoleTeammate, Prompt: "p2"},
				{MemberName: "human1", RoleType: atschema.TeamRoleHumanAgent},
			},
			Agents: map[string]atschema.DeepAgentSpec{
				"nat1": {AgentTemplateSpec: &hschema.AgentTemplateSpec{}},
			},
		}
		group := experts.BoundAgentGroupOf(spec)
		require.NotNil(t, group)
		assert.Equal(t, "g1", group.Name)
		require.Len(t, group.Members, 2) // human_agent 无快照被排除
		assert.NotNil(t, group.Member("cli1").ExternalCLI)
		assert.NotNil(t, group.Member("nat1").Snapshot)
		assert.Nil(t, group.Member("missing"))
		assert.Equal(t, []string{"cli1", "nat1"}, group.MemberNames())
	})
}
