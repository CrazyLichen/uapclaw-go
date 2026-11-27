package equipment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newShowEnv 带模板包的隔离环境（复用 newCardsEnv 并加 show 素材）。
func newShowEnv(t *testing.T) string {
	newCardsEnv(t)
	pkgDir := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "tpl_show")
	writeTemplatePackage(t, pkgDir, "tpl_show", "详情说明")
	_ = os.WriteFile(filepath.Join(pkgDir, "README.md"), []byte("# README 详情"), 0o644)
	_ = os.WriteFile(filepath.Join(pkgDir, "notes.md"), []byte("api_key: abc123"), 0o644)
	_ = os.WriteFile(filepath.Join(pkgDir, "notes.json"), []byte(`{"api_key":"abc","name":"x"}`), 0o644)
	return pkgDir
}

func TestShowAgentTemplate_详情卡(t *testing.T) {
	newShowEnv(t)
	card := ShowAgentTemplate("tpl_show")
	if card == nil {
		t.Fatal("show 应返回卡片")
	}
	if card["id"] != "tpl_show" || card["source"] != "local" {
		t.Fatalf("id/source 不符: %+v", card)
	}
	if card["persona"] != "## persona" {
		t.Fatalf("persona 应取首个 md: %+v", card["persona"])
	}
	if card["details"] != "详情说明" {
		t.Fatalf("模板 details 应为 description: %+v", card["details"])
	}
	if _, ok := card["skills"].([]map[string]any); !ok {
		t.Fatalf("skills 应为数组: %+v", card["skills"])
	}
	// quick_inputs 缺省为空数组
	if qi, ok := card["quickInputs"].([]any); !ok || len(qi) != 0 {
		t.Fatalf("quickInputs 应空数组: %+v", card["quickInputs"])
	}
}

func TestShowAgentTemplate_缺失(t *testing.T) {
	newShowEnv(t)
	if got := ShowAgentTemplate("nope"); got != nil {
		t.Fatal("未知包应 nil")
	}
}

func TestListPackageFiles_过滤与预览标记(t *testing.T) {
	newShowEnv(t)
	tree, err := ListPackageFiles("agent_template", "tpl_show")
	if err != nil {
		t.Fatal(err)
	}
	// dot 文件跳过
	_ = os.WriteFile(filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "tpl_show", ".hidden"), []byte("x"), 0o644)
	found := map[string]bool{}
	var walk func(items []map[string]any)
	walk = func(items []map[string]any) {
		for _, item := range items {
			found[item["path"].(string)] = true
			if children, ok := item["children"].([]map[string]any); ok {
				walk(children)
			}
		}
	}
	walk(tree)
	for _, want := range []string{"manifest.json", "notes.md", "notes.json", "README.md", "persona/tpl_show.md"} {
		if !found[want] {
			t.Fatalf("缺 %s: %v", want, found)
		}
	}
	if found[".hidden"] {
		t.Fatal("dot 文件应跳过")
	}
}

func TestReadPackageFile_脱敏与穿越拒绝(t *testing.T) {
	newShowEnv(t)
	// markdown 读取 + 行脱敏
	got, err := ReadPackageFile("agent_template", "tpl_show", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "api_key: ******") {
		t.Fatalf("敏感行应脱敏: %q", content)
	}
	// JSON 结构化脱敏
	gotJSON, err := ReadPackageFile("agent_template", "tpl_show", "notes.json")
	if err != nil {
		t.Fatal(err)
	}
	jsonContent, _ := gotJSON["content"].(string)
	if !strings.Contains(jsonContent, "******") || strings.Contains(jsonContent, "abc") {
		t.Fatalf("JSON 键值应脱敏: %q", jsonContent)
	}
	// 穿越
	if _, err := ReadPackageFile("agent_template", "tpl_show", "../escape.md"); err == nil {
		t.Fatal("路径穿越应拒绝")
	}
	// 不可预览类型
	_ = os.WriteFile(filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "tpl_show", "blob.bin"), []byte("x"), 0o644)
	if _, err := ReadPackageFile("agent_template", "tpl_show", "blob.bin"); err == nil {
		t.Fatal("不可预览类型应拒绝")
	}
	// 不存在
	if _, err := ReadPackageFile("agent_template", "tpl_show", "missing.md"); err == nil {
		t.Fatal("缺文件应拒绝")
	}
}

func TestShowAgentGroup_详情卡(t *testing.T) {
	newCardsEnv(t)
	writeTemplatePackage(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "m1"), "m1", "成员")
	groupID := "grp_show"
	stage := filepath.Join(equipmentLocalRoot(agentGroupKind, ""), groupID)
	_ = os.MkdirAll(filepath.Join(stage, "agents", "leader"), 0o755)
	_ = os.MkdirAll(filepath.Join(stage, "agents", "analyst"), 0o755)
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
	_ = os.WriteFile(filepath.Join(stage, "README.md"), []byte("# 测试组 README"), 0o644)
	_ = os.WriteFile(filepath.Join(stage, "agents", "leader", "AGENT.md"), []byte("# leader"), 0o644)

	card := ShowAgentGroup(groupID)
	if card == nil {
		t.Fatal("分组详情应返回")
	}
	if card["persona"] != "组指令" {
		t.Fatalf("persona 应为 instruction: %+v", card["persona"])
	}
	if card["leaderId"] != "leader" {
		t.Fatalf("leaderId 应 leader: %+v", card["leaderId"])
	}
	if _, ok := card["disabledRuntimes"]; !ok {
		t.Fatal("disabledRuntimes 键应存在")
	}
	// 缺失
	if got := ShowAgentGroup("nope"); got != nil {
		t.Fatal("未知分组应 nil")
	}
}
