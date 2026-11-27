package equipment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/experts"
)

// pathutilEnvKey / resetPathCacheForCards 测试环境助手（对齐 session_lifecycle 口径）。
const pathutilEnvKey = pathutil.EnvDataDir

func resetPathCacheForCards() { pathutil.ResetCache() }

// newCardsEnv 隔离数据根 + 完整包骨架（模板 ×1 + 分组 ×1）。
func newCardsEnv(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(pathutilEnvKey, root)
	resetPathCacheForCards()
	t.Cleanup(resetPathCacheForCards)
	t.Chdir(root)
	_ = os.MkdirAll(filepath.Join(root, "resources", "agent", "workspace", "plugins", agentTemplateKind), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "resources", "agent", "workspace", "plugins", agentGroupKind), 0o755)
}

// writeTemplatePackage 落一个最小可加载的 agent_template 包。
func writeTemplatePackage(t *testing.T, dir, pkgName, description string) {
	t.Helper()
	manifest := `{
		"package_type": "agent_template",
		"name": "` + pkgName + `",
		"description": "` + description + `",
		"display_name": {"zh": "模板A", "en": "TemplateA"},
		"display_description": {"zh": "描述", "en": "Desc"},
		"persona": {"dir": "./persona"},
		"skills": []
	}`
	_ = os.MkdirAll(filepath.Join(dir, "persona"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "persona", pkgName+".md"), []byte("## persona"), 0o644)
}

func TestBuildAgentTemplateListCard_字段投影(t *testing.T) {
	newCardsEnv(t)
	pkgDir := filepath.Join(resourcesKindRoot(agentTemplateKind), "tpl_a")
	writeTemplatePackage(t, pkgDir, "tpl_a", "说明")
	card, err := BuildAgentTemplateListCard(pkgDir, "builtin", map[string]any{"installed": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if card["id"] != "tpl_a" {
		t.Fatalf("id 应为目录名: %+v", card["id"])
	}
	dn := card["displayName"].(map[string]any)
	if dn["zh"] != "模板A" || dn["en"] != "TemplateA" {
		t.Fatalf("displayName i18n 不符: %+v", dn)
	}
	if card["source"] != "builtin" || card["installed"] != true {
		t.Fatalf("source/installed 不符: %+v", card)
	}
	cs, _ := card["connection_state"].(string)
	if cs != "connected" {
		t.Fatalf("installed 无 connectors 应 connected: %q", cs)
	}
	// 无 marketplace → installed=false → disconnected
	card2, err := BuildAgentTemplateListCard(pkgDir, "local", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	cs2, _ := card2["connection_state"].(string)
	if cs2 != "disconnected" {
		t.Fatalf("未安装应 disconnected: %q", cs2)
	}
}

func TestListAgentTemplates_扫描与filter(t *testing.T) {
	newCardsEnv(t)
	writeTemplatePackage(t, filepath.Join(resourcesKindRoot(agentTemplateKind), "tpl_b"), "tpl_b", "内置")
	writeTemplatePackage(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "tpl_c"), "tpl_c", "本地")
	got, err := ListAgentTemplates(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, card := range got {
		ids[card["id"].(string)] = true
	}
	if !ids["tpl_b"] || !ids["tpl_c"] {
		t.Fatalf("两个模板都应出现: %v", ids)
	}
	// filter=builtin 只留内置
	got2, _ := ListAgentTemplates(map[string]any{"filter": "builtin"})
	for _, card := range got2 {
		if card["id"] == "tpl_c" {
			t.Fatal("filter=builtin 不应含 local")
		}
	}
	// filter=mine 只留 local
	got3, _ := ListAgentTemplates(map[string]any{"filter": "mine"})
	if len(got3) != 1 || got3[0]["id"] != "tpl_c" {
		t.Fatalf("filter=mine 应只含本地: %+v", got3)
	}
}

func TestListAgentGroups_冲突校验与卡片(t *testing.T) {
	newCardsEnv(t)
	// 组包落 local（成员引用本地模板）
	writeTemplatePackage(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "m1"), "m1", "成员")
	groupID := "grp_x"
	stage := filepath.Join(equipmentLocalRoot(agentGroupKind, ""), groupID)
	_ = os.MkdirAll(filepath.Join(stage, "agents", "leader"), 0o755)
	_ = os.MkdirAll(filepath.Join(stage, "agents", "analyst"), 0o755)
	// 成员=模板包拷贝
	src := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "m1")
	_ = copyTreeHelper(src, filepath.Join(stage, "agents", "leader"))
	_ = copyTreeHelper(src, filepath.Join(stage, "agents", "analyst"))
	groupManifest := `{
		"name": "` + groupID + `",
		"package_type": "agent_group",
		"description": "测试组",
		"display_name": {"zh": "测试组", "en": "GroupX"},
		"display_description": {"zh": "组描述", "en": "GroupDesc"},
		"instruction": "组指令",
		"agents": ["leader", "analyst"],
		"member_templates": {"leader": "m1", "analyst": "m1"},
		"skills": []
	}`
	_ = os.WriteFile(filepath.Join(stage, "manifest.json"), []byte(groupManifest), 0o644)
	_ = os.WriteFile(filepath.Join(stage, "agents", "leader", "AGENT.md"), []byte("# leader 规则"), 0o644)
	_ = os.WriteFile(filepath.Join(stage, "README.md"), []byte("# 测试组"), 0o644)

	if _, loadErr := experts.LoadAgentGroupPackageBundle(stage); loadErr != nil {
		t.Fatalf("组包装载失败（诊断）: %v", loadErr)
	}
	got, err := ListAgentGroups(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var groupCard map[string]any
	for _, card := range got {
		if card["id"] == groupID {
			groupCard = card
		}
	}
	if groupCard == nil {
		t.Fatalf("分组卡应出现: %+v", got)
	}
	if groupCard["memberCount"] != 2 {
		t.Fatalf("memberCount 应 2: %+v", groupCard["memberCount"])
	}
	members := groupCard["members"].([]map[string]any)
	var leader map[string]any
	for _, m := range members {
		if m["role"] == "leader" {
			leader = m
		}
	}
	if leader == nil || leader["agentTemplateId"] != "m1" {
		t.Fatalf("leader 应绑定 m1: %+v", leader)
	}
}

func TestListAgentGroups_冲突报错(t *testing.T) {
	newCardsEnv(t)
	// resources 与 local 同名
	writeTemplatePackage(t, filepath.Join(resourcesKindRoot(agentGroupKind), "dup"), "dup", "x")
	stage := filepath.Join(equipmentLocalRoot(agentGroupKind, ""), "dup")
	_ = os.MkdirAll(filepath.Join(stage, "agents", "leader"), 0o755)
	_ = os.WriteFile(filepath.Join(stage, "manifest.json"), []byte(`{"name":"dup","package_type":"agent_group"}`), 0o644)
	_, err := ListAgentGroups(map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("应报冲突: %v", err)
	}
}
