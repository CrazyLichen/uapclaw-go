package equipment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hresources "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// cardPtr 字符串指针助手。
func cardPtr(s string) *string { return &s }

// TestAgentTemplateRuntime_卡片形态校验 卡片三形态分支。
// Python: agent_template_runtime :258-263 卡片形态 + :271-276 闭集门卫。
func TestAgentTemplateRuntime_卡片形态校验(t *testing.T) {
	// native runtime → nil（无外部 harness）
	card := hresources.AgentTemplateCard{Runtime: "native"}
	got, err := AgentTemplateRuntime(card)
	if err != nil {
		t.Fatalf("native 应无错: %v", err)
	}
	if got != nil {
		t.Fatalf("native 应返回 nil, got %v", got)
	}

	// 外部卡片：provider 闭集内 → 通过
	card = hresources.AgentTemplateCard{
		Runtime:         "external",
		RuntimeProvider: cardPtr("codebuddy"),
		RuntimeConfig:   map[string]any{"cli_path": "/usr/bin/cbc"},
	}
	got, err = AgentTemplateRuntime(card)
	if err != nil {
		t.Fatalf("外部卡片应通过: %v", err)
	}
	if got == nil || got.ProviderName != "codebuddy" {
		t.Fatalf("provider_name 应为 codebuddy, got %+v", got)
	}
	if got.Config["cli_path"] != "/usr/bin/cbc" {
		t.Fatalf("config 应透传, got %+v", got.Config)
	}

	// 外部卡片：provider 为空 → ValueError（model_validate 非空校验）
	card = hresources.AgentTemplateCard{Runtime: "external", RuntimeProvider: cardPtr("")}
	if _, err = AgentTemplateRuntime(card); err == nil ||
		!strings.Contains(err.Error(), "provider_name must be a non-empty string") {
		t.Fatalf("空 provider_name 应报错, got %v", err)
	}

	// 外部卡片：provider 不在闭集 → 报错含允许集
	card = hresources.AgentTemplateCard{Runtime: "external", RuntimeProvider: cardPtr("foo")}
	if _, err = AgentTemplateRuntime(card); err == nil ||
		!strings.Contains(err.Error(), "runtime.provider_name must be one of") {
		t.Fatalf("未知 provider 应报错, got %v", err)
	}
}

// TestAgentTemplateRuntime_规格形态校验 已加载 spec 形态直取 runtime 字段。
// Python: agent_template_runtime :255-257 spec 形态。
func TestAgentTemplateRuntime_规格形态校验(t *testing.T) {
	// spec 无 runtime → nil
	spec := hschema.AgentTemplateSpec{}
	got, err := AgentTemplateRuntimeFromSpec(&spec)
	if err != nil || got != nil {
		t.Fatalf("无 runtime 应返回 nil, got %v, %v", got, err)
	}

	// spec 带 runtime 但 provider 不在闭集 → 报错
	spec = hschema.AgentTemplateSpec{Runtime: &hschema.AgentRuntimeSpec{ProviderName: "foo"}}
	if _, err = AgentTemplateRuntimeFromSpec(&spec); err == nil ||
		!strings.Contains(err.Error(), "runtime.provider_name must be one of") {
		t.Fatalf("spec 未知 provider 应报错, got %v", err)
	}

	// spec 带 runtime 且在闭集 → 通过
	spec = hschema.AgentTemplateSpec{Runtime: &hschema.AgentRuntimeSpec{ProviderName: "dsh"}}
	if got, err = AgentTemplateRuntimeFromSpec(&spec); err != nil || got == nil {
		t.Fatalf("dsh 应通过, got %v, %v", got, err)
	}
}

// TestRejectPackageName 包名拒绝五场景。Python: _reject_package_name :111-133。
func TestRejectPackageName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr string
	}{
		{"", "invalid agent_template name: empty"},
		{".", "invalid agent_template name: ."},
		{"..", "invalid agent_template name: .."},
		{".hidden", "invalid agent_template name (hidden): .hidden"},
		{"a/b", "invalid agent_template name (path separator): a/b"},
		{`a\b`, "invalid agent_template name (path separator): a\\b"},
	}
	for _, tc := range cases {
		if _, err := rejectPackageName(tc.name, "agent_template"); err == nil ||
			!strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%q: got %v, want %q", tc.name, err, tc.wantErr)
		}
	}
	// 合法名：strip 后通过
	if _, err := rejectPackageName(" my-expert ", "agent_template"); err != nil {
		t.Fatalf("合法名应通过: %v", err)
	}
}

// writeManifest 写 manifest.json 助手（自动建父目录）。
func writeManifest(t *testing.T, dir string, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveAgentTemplateDir 本地/内置目录解析全场景。
// Python: _resolve_package_dir :1285-1300 + resolve_agent_template_dir :1302-1305。
func TestResolveAgentTemplateDir(t *testing.T) {
	// 用 t.TempDir() 当 agent workspace：plugins/agent_templates/{local,built_in}
	root := t.TempDir()
	tplRoot := filepath.Join(root, "plugins", "agent_templates")
	localRoot := filepath.Join(tplRoot, "local")
	builtRoot := filepath.Join(tplRoot, "built_in")
	for _, dir := range []string{localRoot, builtRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifestBody := `{"name":"expert","package_type":"agent_template"}`

	// local 目录命中
	if err := os.MkdirAll(filepath.Join(localRoot, "expert"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(localRoot, "expert"), manifestBody)
	got, err := ResolveAgentTemplateDirIn("expert", root)
	if err != nil || filepath.Base(got) != "expert" || !strings.Contains(got, "local") {
		t.Fatalf("local 应命中: %v, %v", got, err)
	}

	// built_in 命中（local 缺失时）
	if err := os.RemoveAll(filepath.Join(localRoot, "expert")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(builtRoot, "expert"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(builtRoot, "expert"), manifestBody)
	got, err = ResolveAgentTemplateDirIn("expert", root)
	if err != nil || !strings.Contains(got, "built_in") {
		t.Fatalf("built_in 应命中: %v, %v", got, err)
	}

	// 双处冲突
	writeManifest(t, filepath.Join(localRoot, "expert"), manifestBody)
	if _, err = ResolveAgentTemplateDirIn("expert", root); err == nil ||
		!strings.Contains(err.Error(), "exists in both local and built_in") {
		t.Fatalf("双处应报冲突, got %v", err)
	}
	if err := os.RemoveAll(filepath.Join(localRoot, "expert")); err != nil {
		t.Fatal(err)
	}

	// 都缺失
	if _, err = ResolveAgentTemplateDirIn("missing", root); err == nil ||
		!strings.Contains(err.Error(), "agent_template package not found: missing") {
		t.Fatalf("缺失应报 not found, got %v", err)
	}

	// manifest 损坏 → missing/corrupt
	if err := os.WriteFile(filepath.Join(builtRoot, "expert", "manifest.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = ResolveAgentTemplateDirIn("expert", root); err == nil ||
		!strings.Contains(err.Error(), "missing/corrupt manifest.json") {
		t.Fatalf("损坏 manifest 应报错, got %v", err)
	}

	// package_type 不符
	writeManifest(t, filepath.Join(builtRoot, "expert"), `{"name":"expert","package_type":"plugin"}`)
	if _, err = ResolveAgentTemplateDirIn("expert", root); err == nil ||
		!strings.Contains(err.Error(), "wrong package_type") {
		t.Fatalf("package_type 不符应报错, got %v", err)
	}

	// 无 manifest 的空目录：目录存在即命中候选 → missing/corrupt（对齐 Python
	// _package_dir_if_present is_dir 判定 + _validate_package_manifest 报错序）
	writeManifest(t, filepath.Join(builtRoot, "expert"), manifestBody)
	if err := os.Remove(filepath.Join(builtRoot, "expert", "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = ResolveAgentTemplateDirIn("expert", root); err == nil ||
		!strings.Contains(err.Error(), "missing/corrupt") {
		t.Fatalf("空目录应报 missing/corrupt, got %v", err)
	}
}

// TestResolveEquipmentRuntimeID 查表 miss 原样返回。
// Python: resolve_equipment_runtime_id :3269-3274（D-18-a-2：hub 安装状态存储未移植）。
func TestResolveEquipmentRuntimeID(t *testing.T) {
	got, err := ResolveEquipmentRuntimeID("agent_templates", "abc-123")
	if err != nil || got != "abc-123" {
		t.Fatalf("miss 应原样返回, got %q, %v", got, err)
	}
	// 非法 id 走 rejectPackageName 拒绝（_lifecycle_package_id :2034-2038；
	// Python :111-133 顺序：../ 起始归 hidden 分支，故用 pkg/../ 触发 separator 分支）
	if _, err = ResolveEquipmentRuntimeID("agent_templates", "pkg/../escape"); err == nil ||
		!strings.Contains(err.Error(), "path separator") {
		t.Fatalf("路径分隔符 id 应拒绝, got %v", err)
	}
}
