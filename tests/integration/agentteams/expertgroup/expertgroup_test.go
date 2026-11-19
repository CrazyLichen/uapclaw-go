//go:build integration

// Package expertgroup_test 专家浏览/孵化/binding 全链集成测试（15.16e-T8）。
//
// 每条用例真实调用 ≥3 个 internal 二级子系统的导出函数（规则 7）：
// agent_teams/experts（AgentGroup 包加载 / ExpertRef / BoundAgentGroup）+
// agent_teams/tools（CreateTeamTools 装配 view_experts/view_expert_groups/
// spawn_expert 三件 + TeamBackend.SpawnMember/SpawnExternalCLI 注册行 +
// database.GetMemberAgentTemplate）+
// swarm/server/runtime（team_binding_store 持久化）+
// agentcore/harness/resources（manifest 卡片/模板加载）。
// mock 只在文件系统边界（catalog 的安装根目录）。
//
// 对应 Python: openjiuwen/agent_teams/tools/tool_factory.py:479-529（三件一起）
// + tool_expert.py + tool_member.py + jiuwenswarm/server/runtime/team_binding_store.py。
package expertgroup_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/experts"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExpertGroupSuite 专家浏览/孵化/binding 全链集成套件。
type ExpertGroupSuite struct {
	isuite.AgentSuite
	// cachedBackend fixtureTools 装配的 backend（行断言用）。
	cachedBackend *tools.TeamBackend
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExpertGroupSuite 套件入口。
func TestExpertGroupSuite(t *testing.T) {
	suite.Run(t, new(ExpertGroupSuite))
}

// ──────────────────────────── 夹具 ────────────────────────────

// fixtureCatalog 构造临时安装根的专家 catalog：local 根下一个 agent_template
// 包（writer）+ 一个 agent_group 包（story 含 leader/editor）。
func (s *ExpertGroupSuite) fixtureCatalog() experts.ExpertCatalog {
	dir := s.T().TempDir()
	localRoot := filepath.Join(dir, "local")
	write := func(rel, content string) {
		p := filepath.Join(localRoot, rel)
		s.Require().NoError(os.MkdirAll(filepath.Dir(p), 0o755))
		s.Require().NoError(os.WriteFile(p, []byte(content), 0o644))
	}
	write("agent_template/marketplace.json",
		`{"plugins":[{"id":"writer","installed":true,"source":"local"},{"id":"cli-expert","installed":true,"source":"local"}]}`)
	write("agent_group/marketplace.json",
		`{"plugins":[{"id":"story","installed":true,"source":"local"}]}`)
	write("agent_template/local/writer/manifest.json",
		`{"package_type":"agent_template","name":"Writer","description":"writes stories"}`)
	write("agent_template/local/cli-expert/manifest.json",
		`{"package_type":"agent_template","name":"CliExpert","description":"external expert",
		  "runtime":{"provider_name":"codex","config":{}}}`)
	write("agent_group/local/story/manifest.json",
		`{"package_type":"agent_group","name":"story","instruction":"tell stories","agents":["leader","editor"]}`)
	write("agent_group/local/story/agents/leader/manifest.json",
		`{"package_type":"agent_template","name":"Leader","description":"lead","persona":{"dir":"."}}`)
	write("agent_group/local/story/agents/leader/AGENT.md", "# rules")
	write("agent_group/local/story/agents/leader/persona.md", "lead persona")
	write("agent_group/local/story/agents/editor/manifest.json",
		`{"package_type":"agent_template","name":"Editor","description":"edits","persona":{"dir":"."}}`)
	write("agent_group/local/story/agents/editor/persona.md", "editor persona")
	return &rootCatalog{root: localRoot}
}

// rootCatalog catalog 协议的文件系统边界实现：目录判定复用真实 marketplace
// 语义（installed + local 目录存在），根目录钉到临时区。
type rootCatalog struct{ root string }

func (c *rootCatalog) ListExpertPackages() []experts.PackageLocation {
	return listInstalledAt(c.root, "agent_template")
}
func (c *rootCatalog) LocateExpert(name string) (experts.PackageLocation, error) {
	return locateInstalledAt(c.root, "agent_template", name)
}
func (c *rootCatalog) ListGroupPackages() []experts.PackageLocation {
	return listInstalledAt(c.root, "agent_group")
}
func (c *rootCatalog) LocateGroup(name string) (experts.PackageLocation, error) {
	return locateInstalledAt(c.root, "agent_group", name)
}

func listInstalledAt(root, packageType string) []experts.PackageLocation {
	data, err := os.ReadFile(filepath.Join(root, packageType, "marketplace.json"))
	if err != nil {
		return nil
	}
	var payload struct {
		Plugins []struct {
			ID        string `json:"id"`
			Installed bool   `json:"installed"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil
	}
	locs := []experts.PackageLocation{}
	for _, p := range payload.Plugins {
		if !p.Installed {
			continue
		}
		path := filepath.Join(root, packageType, "local", p.ID)
		if _, statErr := os.Stat(path); statErr == nil {
			locs = append(locs, experts.PackageLocation{Name: p.ID, Path: path, Source: "local"})
		}
	}
	return locs
}

func locateInstalledAt(root, packageType, name string) (experts.PackageLocation, error) {
	for _, loc := range listInstalledAt(root, packageType) {
		if loc.Name == name {
			return loc, nil
		}
	}
	return experts.PackageLocation{}, &runtime.TeamBindingStoreError{
		Msg: packageType + " package not installed: " + name, Code: "BAD_REQUEST",
	}
}

// fixtureTools 经 CreateTeamTools（真实工厂）装配 leader 工具集，注入
// catalog——与 Python create_team_tools(expert_catalog=...) 同一入口。
// backend 缓存到套件供行断言。
func (s *ExpertGroupSuite) fixtureTools(catalog experts.ExpertCatalog) map[string]tool.Tool {
	db := database.NewInMemoryTeamDatabase()
	backend := tools.NewTeamBackend("test-team", "leader", true, db,
		messager.NewInProcessMessager(atschema.NewMessagerTransportConfig()),
		tools.WithTeammateMode("build_mode"))
	s.cachedBackend = backend
	toolList := tools.CreateTeamTools(
		backend,
		tools.TeamToolOptions{
			Role:          "leader",
			Language:      "cn",
			DispatchMode:  "autonomous",
			ExpertCatalog: catalog,
			ExpertBoundGroup: &experts.BoundAgentGroup{
				Name:    "story",
				Members: []experts.BoundGroupMember{},
			},
		},
		nil, nil)
	byName := map[string]tool.Tool{}
	for _, t := range toolList {
		byName[t.Card().Name] = t
	}
	return byName
}

// outputData 取 toolSuccess 包装的 data 段（顶层键为 success/data/error）。
func outputData(output map[string]any) map[string]any {
	if data, ok := output["data"].(map[string]any); ok {
		return data
	}
	return output
}

// invokeExpertTool 按名调用工具。
func invokeExpertTool(byName map[string]tool.Tool, name string, inputs map[string]any) map[string]any {
	t, ok := byName[name]
	if !ok {
		return map[string]any{"success": false, "error": "tool not wired: " + name}
	}
	output, err := t.Invoke(context.Background(), inputs)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	return output
}

// ──────────────────────────── 用例 ────────────────────────────

// TestViewExperts_浏览_检索_明细 单专家/组浏览链：catalog → 三件装配 → list/get。
func (s *ExpertGroupSuite) TestViewExperts_浏览_检索_明细() {
	byName := s.fixtureTools(s.fixtureCatalog())

	// 三件一起注册（Python tool_factory.py:495-497 不缺角）
	s.Require().Contains(byName, "view_experts")
	s.Require().Contains(byName, "view_expert_groups")
	s.Require().Contains(byName, "spawn_expert")

	// 单专家 list
	output := invokeExpertTool(byName, "view_experts", map[string]any{"action": "list"})
	s.Require().Equal(true, output["success"])
	items, ok := outputData(output)["experts"].([]map[string]any)
	s.Require().True(ok)
	s.Require().Len(items, 2) // writer + cli-expert

	// 单专家 get（resources 加载 manifest 卡片 → 明细字段）
	output = invokeExpertTool(byName, "view_experts", map[string]any{"action": "get", "name": "writer"})
	s.Require().Equal(true, output["success"])
	detail := outputData(output)["expert"].(map[string]any)
	s.Require().Equal("Writer", detail["display_name"])
	s.Require().Equal("native", detail["runtime"])
	s.Require().Equal("local", detail["source"])

	// 组 get：花名册含 leader+editor
	output = invokeExpertTool(byName, "view_expert_groups", map[string]any{"action": "get", "name": "story"})
	s.Require().Equal(true, output["success"])
	group := outputData(output)["group"].(map[string]any)
	members, _ := group["members"].([]map[string]any)
	s.Require().Len(members, 2) // 花名册含 leader（Python :661 全员）
}

// TestSpawnExpert_native_binding_store spawn_expert native 注册链 +
// team_binding_store 团队实体持久化。
func (s *ExpertGroupSuite) TestSpawnExpert_native_binding_store() {
	byName := s.fixtureTools(s.fixtureCatalog())

	// swarm/server/runtime 子系统：团队实体绑定（真实文件持久化）
	store := runtime.NewTeamBindingStore(filepath.Join(s.T().TempDir(), "bindings.json"))
	_, err := store.Create("test-team", "tpl-story", nil, false)
	s.Require().NoError(err)
	_, err = store.BindSession("test-team", "sess-1")
	s.Require().NoError(err)
	binding := store.Get("test-team")
	s.Require().NotNil(binding)
	s.Require().Equal([]string{"sess-1"}, binding.SessionIDs)

	// spawn_expert：native 注册（expert → resolver → RegisterTeammate）
	output := invokeExpertTool(byName, "spawn_expert", map[string]any{
		"expert": "writer", "member_name": "dev-writer", "display_name": "Dev Writer",
		"desc": "from spawn", "prompt": "extra prompt",
	})
	s.Require().Equal(true, output["success"], "spawn_expert output: %v", output)

	// 成员行断言：options.agent_template 记录 + prompt 追加语义
	member, getErr := s.cachedBackend.GetMember(context.Background(), "dev-writer")
	s.Require().NoError(getErr)
	s.Require().NotNil(member)
	record := database.GetMemberAgentTemplate(member)
	s.Require().NotNil(record)
	s.Require().Equal("writer", record.Ref["name"])
	s.Require().Contains(member.Prompt, "extra prompt")
	// 模型选择持久化（同一 binding store 面的 selection 半区）
	_, err = store.SetTeamSelection("test-team", map[string]any{"mode": "model", "model_name": "m1"})
	s.Require().NoError(err)
	s.Require().Equal("m1", store.GetTeamSelection("test-team")["model_name"])
}

// TestSpawnExpert_external_kind未声明拒绝 外部专家孵化：能力天花板
// （TeamAgentSpec.external_cli_agents 闭集校验，Python team.py:3260-3267）。
func (s *ExpertGroupSuite) TestSpawnExpert_external_kind未声明拒绝() {
	byName := s.fixtureTools(s.fixtureCatalog())
	// backend 未声明任何 external_cli_agents → codex kind 注册被拒
	output := invokeExpertTool(byName, "spawn_expert", map[string]any{
		"expert": "cli-expert", "member_name": "dev-cli",
	})
	errText, _ := output["error"].(string)
	s.Require().Contains(errText, "not declared in TeamAgentSpec.external_cli_agents")
}
