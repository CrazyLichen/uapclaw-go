package evaluationresultanalyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
)

// 构造最小 harness 包目录（config.json 形态，无 manifest.json）。
func makeHarnessPkg(t *testing.T, root string, withRuntime bool) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// legacy manifest 桩（Python find_plugin_manifest 查找序：manifest.json → legacy yaml）
	if err := os.WriteFile(filepath.Join(root, "harness_config.yaml"), []byte("package_type: harness\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{
		"prompt_sections": {"sections": [{"name": "main", "file": "sections/main.md"}]},
		"skills": [{"dir": "skills/skill-a"}],
		"tools": [{"name": "bash", "file": "tools/bash.py"}],
		"rails": [{"name": "r1", "file": "rails/r1.py"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sections"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sections", "main.md"), []byte("## Main\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "skill-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "skill-a", "SKILL.md"), []byte("skill body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "bash.py"), []byte("# tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rails", "r1.py"), []byte("# rail"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "identity.md"), []byte("identity text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "soul.md"), []byte("soul text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withRuntime {
		if err := os.WriteFile(filepath.Join(root, "runtime.yaml"), []byte("max_iterations: 5\nmodel: gpt"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPrepareHarnessContext_无引用回落 对照 Python not_provided 分支。
// Python: harness_context.py:127
func TestPrepareHarnessContext_无引用回落(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()
	// eval_ref 无 harness_refs_path 且调用方也未给
	evalRef := filepath.Join(caseRoot, "eval_ref.yaml")
	if err := os.WriteFile(evalRef, []byte("eval_id: e1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ctxErr := PrepareHarnessContext(evalRef, "", runtimeDir)
	if ctxErr != nil {
		t.Fatal(ctxErr)
	}
	if got["status"] != "not_provided" {
		t.Fatalf("无引用应 not_provided: %v", got)
	}
	if roles, ok := got["roles"].([]any); !ok || len(roles) != 0 {
		t.Fatalf("roles 应空: %v", got["roles"])
	}
}

// TestPrepareHarnessContext_完整投映 对照 Python prepare_harness_context 主流程。
// Python: harness_context.py:112-217
func TestPrepareHarnessContext_完整投映(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()

	// harness 包
	pkg := filepath.Join(caseRoot, "pkg")
	makeHarnessPkg(t, pkg, true)

	// harness refs 文件：单角色指向包根目录
	refsPath := filepath.Join(caseRoot, "harness_refs.yaml")
	if err := os.WriteFile(refsPath, []byte(`roles:
  - role: planner
    member_name: planner-m
    harness_ref_path: `+pkg+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// eval_ref 声明 harness_refs_path
	evalRef := filepath.Join(caseRoot, "eval_ref.yaml")
	if err := os.WriteFile(evalRef, []byte("eval_id: e1\nharness_refs_path: "+refsPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ctxErr := PrepareHarnessContext(evalRef, "", runtimeDir)
	if ctxErr != nil {
		t.Fatal(ctxErr)
	}
	if got["status"] != "available" {
		t.Fatalf("应 available: %v", got)
	}
	roles, _ := got["roles"].([]any)
	if len(roles) != 1 {
		t.Fatalf("应 1 角色, got %d", len(roles))
	}
	row, _ := roles[0].(map[string]any)
	if row["role"] != "planner" || row["member_name"] != "planner-m" {
		t.Fatalf("角色头不符: %v", row)
	}

	// ① runtime.yaml 限制投影：仅两键，model 被剔除
	rc := row["runtime_config"].(map[string]any)
	if rc["status"] != "declared" {
		t.Fatalf("runtime 应 declared: %v", rc)
	}
	decl := rc["declaration"].(map[string]any)
	if decl["max_iterations"] != 5 {
		t.Fatalf("max_iterations 应保留: %v", decl)
	}
	if _, hasModel := decl["model"]; hasModel {
		t.Fatal("model 应被剔除（仅两键可编辑白名单）")
	}
	yamlContent, _ := os.ReadFile(filepath.Join(runtimeDir, "current_harness", "role_001", "runtime.yaml"))
	if strings.Contains(string(yamlContent), "model") {
		t.Fatal("落盘 runtime.yaml 应只含限制字段")
	}

	// ② prompt_sections：file 条目解析 + 落盘
	sections := row["prompt_sections"].([]any)
	if len(sections) != 3 { // main + identity + soul（无 manifest → normalize + identity/soul 追加）
		t.Fatalf("prompt_sections 应 3 段, got %d: %v", len(sections), sections)
	}
	first := sections[0].(map[string]any)
	if first["name"] != "main" {
		t.Fatalf("段名不符: %v", first)
	}
	promptJSON, err := os.ReadFile(filepath.Join(runtimeDir, filepath.ToSlash(first["path"].(string))))
	if err != nil || !strings.Contains(string(promptJSON), "body") {
		t.Fatalf("prompt 内容落盘不符: %v", err)
	}
	// identity/soul 追加（无 manifest.json）
	last := sections[2].(map[string]any)
	if last["name"] != "soul" {
		t.Fatalf("identity/soul 应追加, got: %v", sections)
	}

	// ③ skill 拷贝（目录发现：skill-a/SKILL.md）
	skills := row["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("应 1 技能, got %v", skills)
	}
	skill := skills[0].(map[string]any)
	if skill["name"] != "skill-a" {
		t.Fatalf("技能名不符: %v", skill)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, filepath.ToSlash(skill["path"].(string)))); err != nil {
		t.Fatal("技能文件应落盘")
	}

	// ④ tools/rails file 项拷贝
	tools := row["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["path"] == nil {
		t.Fatalf("tools 投影不符: %v", tools)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, filepath.ToSlash(tools[0]["path"].(string)))); err != nil {
		t.Fatal("工具文件应落盘")
	}
	rails := row["rails"].([]map[string]any)
	if len(rails) != 1 || rails[0]["path"] == nil {
		t.Fatalf("rails 投影不符: %v", rails)
	}

	// ⑤ index.json 落盘
	if _, err := os.Stat(filepath.Join(runtimeDir, "current_harness", "index.json")); err != nil {
		t.Fatal("index.json 应落盘")
	}
}

// TestPrepareHarnessContext_无runtime继承默认 对照 Python inherited_defaults 分支。
func TestPrepareHarnessContext_无runtime继承默认(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()
	pkg := filepath.Join(caseRoot, "pkg")
	makeHarnessPkg(t, pkg, false) // 无 runtime.yaml
	refsPath := filepath.Join(caseRoot, "harness_refs.yaml")
	if err := os.WriteFile(refsPath, []byte(`roles:
  - role: worker
    harness_ref_path: `+pkg+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	evalRef := filepath.Join(caseRoot, "eval_ref.yaml")
	if err := os.WriteFile(evalRef, []byte("harness_refs_path: "+refsPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ctxErr := PrepareHarnessContext(evalRef, "", runtimeDir)
	if ctxErr != nil {
		t.Fatal(ctxErr)
	}
	roles, _ := got["roles"].([]any)
	rc := roles[0].(map[string]any)["runtime_config"].(map[string]any)
	if rc["status"] != "inherited_defaults" {
		t.Fatalf("无 runtime.yaml 应 inherited_defaults: %v", rc)
	}
}

// TestPrepareHarnessContext_逃逸拒绝 对照 Python _local_path ValueError。
// Python: harness_context.py:22-26
func TestPrepareHarnessContext_逃逸拒绝(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()
	pkg := filepath.Join(caseRoot, "pkg")
	makeHarnessPkg(t, pkg, true)
	// 技能声明逃逸出包根
	if err := os.WriteFile(filepath.Join(pkg, "config.json"), []byte(`{
		"skills": [{"dir": "../outside"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	refsPath := filepath.Join(caseRoot, "harness_refs.yaml")
	if err := os.WriteFile(refsPath, []byte(`roles:
  - role: worker
    harness_ref_path: `+pkg+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	evalRef := filepath.Join(caseRoot, "eval_ref.yaml")
	if err := os.WriteFile(evalRef, []byte("harness_refs_path: "+refsPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareHarnessContext(evalRef, "", runtimeDir)
	if err == nil {
		t.Fatal("逃逸引用应报错（Python ValueError 等价）")
	}
	if !strings.Contains(err.Error(), "Harness evidence reference leaves its package") {
		t.Fatalf("错误文案不符: %v", err)
	}
}

// TestPrepareHarnessContext_引用缺失报错 对照 Python FileNotFoundError 文案。
// Python: harness_context.py:131-132
func TestPrepareHarnessContext_引用缺失报错(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()
	// eval_ref 声明不存在的 refs
	evalRef := filepath.Join(caseRoot, "eval_ref.yaml")
	if err := os.WriteFile(evalRef, []byte("harness_refs_path: ./missing_refs.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareHarnessContext(evalRef, "", runtimeDir)
	if err == nil {
		t.Fatal("缺失引用应报错（Python FileNotFoundError 等价）")
	}
	if !strings.Contains(err.Error(), "Evaluated Harness references not found") {
		t.Fatalf("错误文案不符: %v", err)
	}
}

// TestResolveSectionFile_回落 对照 Python _resolve_section_file 两级查找。
// Python: extension_loader.py:968-974
func TestResolveSectionFile_回落(t *testing.T) {
	root := t.TempDir()
	// 直接命中
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("direct"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := hres.ResolveSectionFile(root, "a.md"); filepath.Base(got) != "a.md" {
		t.Fatalf("直接命中不符: %s", got)
	}
	// prompt_sections/files 回落
	if err := os.MkdirAll(filepath.Join(root, "prompt_sections", "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompt_sections", "files", "b.md"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := hres.ResolveSectionFile(root, "b.md")
	if !strings.Contains(got, "prompt_sections") {
		t.Fatalf("应回落 files 目录: %s", got)
	}
}

// TestResourceNames_白名单投影 对照 Python _resource_names。
// Python: harness_context.py:69-82
func TestResourceNames_白名单投影(t *testing.T) {
	items := []any{
		"builtin-string",
		map[string]any{"name": "n1", "file": "f.py", "junk": 1},
		123, // 非 str 非 dict → 跳过
	}
	got := resourceNames(items)
	if len(got) != 2 {
		t.Fatalf("应 2 项, got %d", len(got))
	}
	if got[0]["builtin"] != "builtin-string" {
		t.Fatalf("字符串项不符: %v", got[0])
	}
	if got[1]["name"] != "n1" || got[1]["file"] != "f.py" {
		t.Fatalf("dict 项白名单不符: %v", got[1])
	}
	if _, ok := got[1]["junk"]; ok {
		t.Fatal("白名单外键应剔除")
	}
}

// TestSkillFiles_三态发现 对照 Python _skill_files 三种形态。
// Python: harness_context.py:85-109
func TestSkillFiles_三态发现(t *testing.T) {
	root := t.TempDir()
	// 形态1：直接 SKILL.md 文件
	if err := os.WriteFile(filepath.Join(root, "direct-skill.md"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 形态2：技能目录直含 SKILL.md
	if err := os.MkdirAll(filepath.Join(root, "skillA"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skillA", "SKILL.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 形态3：父目录下子目录含 SKILL.md
	if err := os.MkdirAll(filepath.Join(root, "skills-pool", "skillB"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills-pool", "skillB", "SKILL.md"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	// ① 直接文件
	got, skillErr := skillFiles(root, []any{map[string]any{"dir": "direct-skill.md"}})
	if skillErr != nil {
		t.Fatal(skillErr)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "direct-skill.md" {
		t.Fatalf("直接文件发现不符: %v", got)
	}
	// ② 目录直含
	got2, err2 := skillFiles(root, []any{map[string]any{"dir": "skillA"}})
	if err2 != nil {
		t.Fatal(err2)
	}
	if len(got2) != 1 || filepath.Base(got2[0]) != "SKILL.md" {
		t.Fatalf("目录直含发现不符: %v", got2)
	}
	// ③ 父目录扫描子目录
	got3, err3 := skillFiles(root, []any{map[string]any{"dir": "skills-pool"}})
	if err3 != nil {
		t.Fatal(err3)
	}
	if len(got3) != 1 || !strings.Contains(got3[0], "skillB") {
		t.Fatalf("父目录扫描发现不符: %v", got3)
	}
	// ④ 去重（dict.fromkeys）
	got4, err4 := skillFiles(root, []any{
		map[string]any{"dir": "direct-skill.md"},
		map[string]any{"dir": "direct-skill.md"},
	})
	if err4 != nil {
		t.Fatal(err4)
	}
	if len(got4) != 1 {
		t.Fatalf("应去重, got %d", len(got4))
	}
	// ⑤ 空值报错（Python ValueError 等价）
	if _, err := skillFiles(root, []any{map[string]any{"dir": ""}}); err == nil {
		t.Fatal("空声明应报错")
	}
}
