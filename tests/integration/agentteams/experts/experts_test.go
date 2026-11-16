//go:build integration

// AgentTemplate 清单集成测试：manifest.json → 展示卡片 → 成员装配全链。
//
// 每条用例真实调用 ≥3 个 internal 二级子系统的导出函数（规则 7）：
// agentcore/harness/resources（卡片加载 / prompt 渲染）+
// agentcore/harness/schema（AgentTemplateSpec / DeepAgentSpec）+
// agent_teams/experts（persona 拍平 + 能力快照 compose）。
//
// 对应 Python: tests/unit_tests/agent_teams/experts/ 的装配语义 +
// workflow/backends/expert_specs.py 的「同一模板两次 compose 产物一致」不变量。
package experts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/experts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cmschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExpertsSuite AgentTemplate → 成员装配集成套件。
type ExpertsSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExpertsSuite 套件入口。
func TestExpertsSuite(t *testing.T) {
	suite.Run(t, new(ExpertsSuite))
}

// TestTemplateToMember_卡片_装配_幂等 manifest.json 只读出卡片，模板两次 compose 产物一致。
func (s *ExpertsSuite) TestTemplateToMember_卡片_装配_幂等() {
	dir := s.T().TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	body := `{"package_type":"agent_template","name":"coder","description":"writes code",
	  "display_name":{"cn":"编程助手","en":"Coder"},
	  "runtime":{"provider_name":"codex","config":{}},
	  "skills":[{"dir":"skills/py"},{"dir":"skills/py"}]}`
	s.Require().NoError(os.WriteFile(manifest, []byte(body), 0o644))

	// 子系统① resources：卡片只读解析（不加载包体；skills 目录名去重）
	card, err := hres.LoadAgentTemplateCard(manifest)
	s.Require().NoError(err)
	s.Require().Equal("external", card.Runtime)
	s.Require().NotNil(card.RuntimeProvider)
	s.Require().Equal("codex", *card.RuntimeProvider)
	s.Require().Equal([]string{"py"}, card.Skills)
	s.Require().Equal("编程助手", card.DisplayLabel("cn"))
	s.Require().Equal("Coder", card.DisplayLabel("en"))

	// 子系统② schema：模板 spec（identity 段 + skills）
	spec := hschema.AgentTemplateSpec{
		AgentCard:      saschema.AgentCard{BaseCard: cmschema.BaseCard{ID: "coder", Name: "Coder", Description: "writes code"}},
		PromptSections: []hschema.PromptSectionSpec{{Name: "identity", Content: map[string]string{"cn": "角色 {{ language }} 于 {{ workspace }}"}}},
		Skills:         []hschema.SkillSpec{{Dir: dir + "/skills/py"}, {Dir: dir + "/skills/sql"}},
	}

	// 子系统③ experts：同一模板 + 同一 base spec ⇒ 同一 persona/spec（幂等不变量）
	base := hschema.NewDeepAgentSpec()
	base.Language = "cn"
	base.Workspace = &hschema.WorkspaceSpec{RootPath: "/team/ws"}
	m1 := experts.ComposeTemplateMember(base, spec, "")
	m2 := experts.ComposeTemplateMember(base, spec, "")
	s.Require().Equal(m1.Persona, m2.Persona)
	s.Require().Equal("角色 cn 于 /team/ws", m1.Persona)
	s.Require().NotNil(m1.Spec.AgentTemplateSpec)
	s.Require().Equal([]string{"py", "sql"}, m1.Spec.Skills)

	// 快照不含 prompt_sections（人格走 prompt lane、能力走快照的分离不变量）
	raw, err := json.Marshal(m1.Spec.AgentTemplateSpec)
	s.Require().NoError(err)
	s.Require().NotContains(string(raw), "identity")
}
