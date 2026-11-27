package equipment

import (
	"os"
	"path/filepath"
	"testing"
)

// newLocalMgmtEnv 隔离环境 + 一个可被分组引用的模板 + 一个工作区技能。
func newLocalMgmtEnv(t *testing.T) string {
	newCardsEnv(t)
	skillsRoot := agentSkillsDirForTest()
	if err := os.MkdirAll(filepath.Join(skillsRoot, "demo_skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsRoot, "demo_skill", "SKILL.md"), []byte("---\nname: demo\ndescription: 演示\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "m1")
	writeTemplatePackage(t, pkgDir, "m1", "成员")
	return pkgDir
}

func TestCreateAgentTemplate_骨架与重名(t *testing.T) {
	newLocalMgmtEnv(t)
	params := map[string]any{
		"id": "myagent", "name": "我的智能体", "description": "说明",
		"persona": "## 人设", "skills": []any{"demo_skill"},
		"quickInputs": []any{"帮我看下"}, "tags": []any{map[string]any{"zh": "标签", "en": "tag"}},
	}
	if err := CreateAgentTemplate(params); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "myagent")
	manifest := readPackageManifestForShow(pkgDir)
	if manifest == nil || manifest["package_type"] != "agent_template" {
		t.Fatalf("manifest 落盘失败: %+v", manifest)
	}
	// persona 落盘
	if _, err := os.Stat(filepath.Join(pkgDir, "persona", "myagent.md")); err != nil {
		t.Fatal("persona md 应落盘")
	}
	// skills 拷贝
	if _, err := os.Stat(filepath.Join(pkgDir, "skills", "demo_skill", "SKILL.md")); err != nil {
		t.Fatal("skills 应拷贝")
	}
	// marketplace 条目 installed=false source=local
	entries := ReadAgentTemplateMarketplaceEntries()
	if len(entries) != 1 || entries[0]["installed"] != false || entries[0]["source"] != "local" {
		t.Fatalf("marketplace 条目不符: %+v", entries)
	}
	// 重名
	if err := CreateAgentTemplate(params); err == nil {
		t.Fatal("重名应报错")
	}
	// 技能不存在
	bad := map[string]any{"id": "other", "name": "n", "description": "d", "persona": "p",
		"skills": []any{"missing_skill"}}
	if err := CreateAgentTemplate(bad); err == nil {
		t.Fatal("缺失技能应报错")
	}
}

func TestUpdateAgentTemplate_本地限定(t *testing.T) {
	newLocalMgmtEnv(t)
	if err := CreateAgentTemplate(map[string]any{
		"id": "upd", "name": "旧名", "description": "旧", "persona": "旧人设", "skills": []any{"demo_skill"},
	}); err != nil {
		t.Fatal(err)
	}
	// 更新
	if err := UpdateAgentTemplate(map[string]any{
		"id": "upd", "name": "新名", "description": "新", "persona": "新人设", "skills": []any{"demo_skill"},
	}, noopUnloadLive); err != nil {
		t.Fatal(err)
	}
	manifest := readPackageManifestForShow(filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "upd"))
	if manifest["description"] != "新" {
		t.Fatalf("更新未生效: %+v", manifest["description"])
	}
	// 不存在的包
	if err := UpdateAgentTemplate(map[string]any{
		"id": "ghost", "name": "n", "description": "d", "persona": "p", "skills": []any{"demo_skill"},
	}, noopUnloadLive); err == nil {
		t.Fatal("未知包应报错")
	}
}

func TestDeleteAgentTemplate_本地限定(t *testing.T) {
	newLocalMgmtEnv(t)
	if err := CreateAgentTemplate(map[string]any{
		"id": "del", "name": "n", "description": "d", "persona": "p", "skills": []any{"demo_skill"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAgentTemplate(map[string]any{"id": "del"}, noopUnloadLive); err != nil {
		t.Fatal(err)
	}
	if isDir(filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "del")) {
		t.Fatal("目录应删除")
	}
	if len(ReadAgentTemplateMarketplaceEntries()) != 0 {
		t.Fatal("marketplace 条目应移除")
	}
	// 未知包
	if err := DeleteAgentTemplate(map[string]any{"id": "del"}, noopUnloadLive); err == nil {
		t.Fatal("再删应报 not found")
	}
}

func TestCreateAgentGroup_骨架校验与拷贝(t *testing.T) {
	newLocalMgmtEnv(t)
	params := map[string]any{
		"id": "mygrp", "name": "我的小组", "description": "组说明",
		"persona": "组人设", "leaderId": "m1", "memberIds": []any{"m1", "m1_copy"},
		"skills": []any{"demo_skill"}, "category": "办公",
	}
	// memberIds 不能含 leaderId（m1 为 leader）→ 先调整：leaderId=m1_copy 需存在
	// Python 语义：leaderId 与 members 均为专家 ID；这里 leader=m1，成员给 m1 会冲突，
	// 按真实前端形态 leader 选一个模板、成员选其余模板。为测试先建第二个模板。
	writeTemplatePackage(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "m2"), "m2", "成员二")
	params["leaderId"] = "m1"
	params["memberIds"] = []any{"m2"}
	if _, err := CreateAgentGroup(params); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(equipmentLocalRoot(agentGroupKind, ""), "mygrp")
	manifest := readPackageManifestForShow(dest)
	if manifest["package_type"] != "agent_group" {
		t.Fatalf("组 manifest 落盘失败: %+v", manifest)
	}
	// 成员目录拷贝
	if _, err := os.Stat(filepath.Join(dest, "agents", "leader", "manifest.json")); err != nil {
		t.Fatal("leader 包应拷贝")
	}
	if _, err := os.Stat(filepath.Join(dest, "agents", "m2", "manifest.json")); err != nil {
		t.Fatal("成员包应拷贝")
	}
	// leader AGENT.md 自动生成
	if _, err := os.Stat(filepath.Join(dest, "agents", "leader", "AGENT.md")); err != nil {
		t.Fatal("leader AGENT.md 应生成")
	}
	// skills 拷贝
	if _, err := os.Stat(filepath.Join(dest, "skills", "demo_skill", "SKILL.md")); err != nil {
		t.Fatal("组共享 skills 应拷贝")
	}
	// 组卡
	card, err := BuildAgentGroupCard(dest, "local", nil, loadGroupBundle, false)
	if err != nil {
		t.Fatal(err)
	}
	if card["memberCount"] != 2 {
		t.Fatalf("memberCount 应 2: %+v", card["memberCount"])
	}
	// 重名（id）
	if _, err := CreateAgentGroup(params); err == nil {
		t.Fatal("同 id 重建应报错")
	}
}

func TestCreateAgentGroup_成员校验(t *testing.T) {
	newLocalMgmtEnv(t)
	base := map[string]any{
		"id": "g1", "name": "g", "description": "d", "persona": "p",
		"skills": []any{"demo_skill"},
	}
	// 缺 leaderId
	if _, err := CreateAgentGroup(base); err == nil {
		t.Fatal("缺 leaderId 应报错")
	}
	// 未知 leader
	if _, err := CreateAgentGroup(map[string]any{
		"id": "g1", "name": "g", "description": "d", "persona": "p", "skills": []any{"demo_skill"},
		"leaderId": "ghost",
	}); err == nil {
		t.Fatal("未知 leader 应报错")
	}
	// leaderId 重复出现在 members
	writeTemplatePackage(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "m1"), "m1", "成员")
	if _, err := CreateAgentGroup(map[string]any{
		"id": "g1", "name": "g", "description": "d", "persona": "p", "skills": []any{"demo_skill"},
		"leaderId": "m1", "memberIds": []any{"m1"},
	}); err == nil {
		t.Fatal("leaderId 与 memberIds 冲突应报错")
	}
	// 成员名保留字 leader
	writeTemplatePackage(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "m2"), "m2", "成员二")
	if _, err := CreateAgentGroup(map[string]any{
		"id": "g1", "name": "g", "description": "d", "persona": "p", "skills": []any{"demo_skill"},
		"leaderId": "m1", "memberIds": []any{"leader"},
	}); err == nil {
		t.Fatal("保留名应报错")
	}
}

func TestImportAgentTemplate_目录导入(t *testing.T) {
	newLocalMgmtEnv(t)
	// 源目录（带 README + persona）
	src := filepath.Join(t.TempDir(), "imported_tpl")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("# readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(src, "persona"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "persona", "p.md"), []byte("## p"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "manifest.json"), []byte(`{
		"package_type": "agent_template",
		"name": "imported_tpl",
		"description": "导入",
		"display_name": {"zh": "导入", "en": "Imported"},
		"persona": {"dir": "./persona"},
		"skills": []
	}`), 0o644)
	got, err := ImportAgentTemplate(map[string]any{"path": src})
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "imported_tpl" {
		t.Fatalf("id 不符: %+v", got)
	}
	if !isDir(filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "imported_tpl")) {
		t.Fatal("应拷入 local/")
	}
	// 同 id 再导入 → 已存在
	if _, err := ImportAgentTemplate(map[string]any{"path": src}); err == nil {
		t.Fatal("重复导入应报错")
	}
}

// agentSkillsDirForTest 工作区技能根（与 workspace.AgentSkillsDir 一致）。
func agentSkillsDirForTest() string {
	return filepath.Join(os.Getenv("UAPCLAW_DATA_DIR"), "agent", "workspace", "skills")
}
