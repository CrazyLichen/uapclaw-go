//go:build integration

package server_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/equipment"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EquipmentCatalogSuite 设备目录管理全链：E2A 信封分发 → 控制面分派 →
// equipment 落盘 → unload-live 联动。
//
// 跨子系统：swarm/server（信封分发）+ swarm/server/control（分派口径）+
// swarm/server/runtime/equipment（本地半区）+ swarm/server/adapter（unload-live）
// + swarm/server/session（技能目录）。
type EquipmentCatalogSuite struct {
	isuite.BaseIntegrationSuite
	env     *testhelpers.ServerEnv
	dataDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEquipmentCatalogSuite 套件入口。
func TestEquipmentCatalogSuite(t *testing.T) {
	suite.Run(t, new(EquipmentCatalogSuite))
}

// SetupTest 隔离数据目录。
func (s *EquipmentCatalogSuite) SetupTest() {
	s.dataDir = s.T().TempDir()
	s.T().Setenv(pathutil.EnvDataDir, s.dataDir)
	pathutil.ResetCache()
	swarmsession.ClearAllSessionMetadataCache()
}

// TearDownTest 刷盘还原。
func (s *EquipmentCatalogSuite) TearDownTest() {
	swarmsession.FlushMetadataQueue(5.0)
	swarmsession.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// SetupSuite 启动进程内 AgentServer。
func (s *EquipmentCatalogSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// call 投递设备方法并断言成功，返回载荷。
func (s *EquipmentCatalogSuite) call(method schema.ReqMethod, params map[string]any) map[string]any {
	rid := s.env.Send(string(method), params, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.Require().True(resp.OK, "%s 应成功: %+v", method, resp.Payload)
	return resp.Payload
}

// callFail 投递设备方法并断言失败及错误码。
func (s *EquipmentCatalogSuite) callFail(method schema.ReqMethod, params map[string]any, wantCode string) {
	rid := s.env.Send(string(method), params, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.Require().False(resp.OK, "%s 应失败: %+v", method, resp)
	s.Require().Equal(wantCode, resp.Payload["code"])
}

// localRoot 模板 local 目录。
func (s *EquipmentCatalogSuite) localRoot(kind string) string {
	if kind == "agent_groups" {
		return filepath.Join(s.dataDir, ".agent_teams", kind, "local")
	}
	return filepath.Join(s.dataDir, "agent", "workspace", "plugins", kind, "local")
}

// writeSkill 落一个工作区技能。
func (s *EquipmentCatalogSuite) writeSkill(name string) {
	skillsRoot := filepath.Join(s.dataDir, "agent", "workspace", "skills")
	s.Require().NoError(os.MkdirAll(filepath.Join(skillsRoot, name), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillsRoot, name, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: 演示技能\n---\nbody"), 0o644))
}

// createTemplateViaRPC 经 RPC 创建模板包。
func (s *EquipmentCatalogSuite) createTemplateViaRPC(id string) {
	s.writeSkill("demo_skill")
	s.call(schema.ReqMethodAgentTemplatesCreate, map[string]any{
		"id": id, "name": id + "-name", "description": "说明",
		"persona": "## 人设", "skills": []any{"demo_skill"},
	})
}

// TestEquipment_Create_List_Show_Delete 创建 → 列表 → 详情 → 文件树 → 删除全链。
func (s *EquipmentCatalogSuite) TestEquipment_Create_List_Show_Delete() {
	s.createTemplateViaRPC("it_tpl")
	// list
	list := s.call(schema.ReqMethodAgentTemplatesList, map[string]any{})
	templates := list["templates"].([]any)
	s.Require().NotEmpty(templates)
	found := false
	for _, item := range templates {
		m := item.(map[string]any)
		if m["id"] == "it_tpl" {
			found = true
			s.Assert().Equal("local", m["source"])
			s.Assert().Equal(false, m["installed"])
		}
	}
	s.Require().True(found, "新模板应在列表")
	// show
	show := s.call(schema.ReqMethodAgentTemplatesShow, map[string]any{"id": "it_tpl"})
	tmpl := show["template"].(map[string]any)
	s.Assert().Equal("## 人设", tmpl["persona"])
	// file.read（诊断：直连 equipment 与 RPC 各一次）
	if direct, dErr := equipment.ReadPackageFile("agent_template", "it_tpl", "persona/it_tpl.md"); dErr != nil {
		s.T().Logf("直接调用失败: %v", dErr)
	} else {
		s.T().Logf("直接调用成功: %+v", direct)
	}
	read := s.call(schema.ReqMethodAgentTemplatesFileRead, map[string]any{
		"id": "it_tpl", "path": "persona/it_tpl.md"})
	content, _ := read["content"].(string)
	s.Assert().Equal("## 人设", content)
	// delete
	s.call(schema.ReqMethodAgentTemplatesDelete, map[string]any{"id": "it_tpl"})
	if _, err := os.Stat(filepath.Join(s.localRoot("agent_templates"), "it_tpl")); err == nil {
		s.T().Fatal("删除后目录应消失")
	}
	// show 缺失
	s.callFail(schema.ReqMethodAgentTemplatesShow, map[string]any{"id": "it_tpl"}, "BAD_REQUEST")
}

// TestEquipment_Group_Create_Show_Uninstall 分组创建 → 详情 → 卸载。
func (s *EquipmentCatalogSuite) TestEquipment_Group_Create_Show_Uninstall() {
	s.createTemplateViaRPC("grp_member")
	s.writeSkill("grp_skill")
	s.call(schema.ReqMethodAgentGroupsCreate, map[string]any{
		"id": "it_grp", "name": "集成组", "description": "组说明",
		"persona": "组指令", "leaderId": "grp_member", "memberIds": []any{},
		"skills": []any{"grp_skill"},
	})
	show := s.call(schema.ReqMethodAgentGroupsShow, map[string]any{"id": "it_grp"})
	group := show["group"].(map[string]any)
	s.Assert().Equal("组指令", group["persona"])
	s.Assert().Equal("leader", group["leaderId"])
	// uninstall（组织路径）
	s.call(schema.ReqMethodAgentGroupsUninstall, map[string]any{"id": "it_grp"})
	if _, err := os.Stat(filepath.Join(s.localRoot("agent_groups"), "it_grp")); err == nil {
		s.T().Fatal("组卸载后目录应消失")
	}
}

// TestEquipment_Install_MarketUnavailable 三处 install 均返回 MARKET_UNAVAILABLE。
func (s *EquipmentCatalogSuite) TestEquipment_Install_MarketUnavailable() {
	for _, method := range []schema.ReqMethod{
		schema.ReqMethodAgentTemplatesInstall,
		schema.ReqMethodAgentGroupsInstall,
		schema.ReqMethodPluginPackagesInstall,
	} {
		s.callFail(method, map[string]any{"id": "any"}, "MARKET_UNAVAILABLE")
	}
}

// TestEquipment_TemplateUpdate_Persona 重写生效（unload-live 联动由分派层触发，
// 无运行会话绑定时为 no-op）。
func (s *EquipmentCatalogSuite) TestEquipment_TemplateUpdate_Persona() {
	s.createTemplateViaRPC("upd_tpl")
	s.call(schema.ReqMethodAgentTemplatesUpdate, map[string]any{
		"id": "upd_tpl", "name": "upd_tpl-name", "description": "新说明",
		"persona": "## 新人设", "skills": []any{"demo_skill"},
	})
	read := s.call(schema.ReqMethodAgentTemplatesFileRead, map[string]any{
		"id": "upd_tpl", "path": "persona/upd_tpl.md"})
	content, _ := read["content"].(string)
	s.Assert().Equal("## 新人设", content)
	// manifest JSON 落盘形状（缩进 + 尾换行）
	data, err := os.ReadFile(filepath.Join(s.localRoot("agent_templates"), "upd_tpl", "manifest.json"))
	s.Require().NoError(err)
	var doc map[string]any
	s.Require().NoError(json.Unmarshal(data, &doc))
	s.Assert().Equal("upd_tpl-name", doc["name"])
}

// 编译期守卫。
var _ = schema.ReqMethodAgentTemplatesList
