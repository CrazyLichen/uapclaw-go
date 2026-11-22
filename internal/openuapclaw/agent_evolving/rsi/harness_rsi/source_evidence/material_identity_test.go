// material_identity_test.go 身份计算闭包单测（自 singleharness/source_evidence_test.go 随函数下沉）。

package source_evidence

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEvidenceDigest 摘要稳定且与键序无关。
func TestEvidenceDigest(t *testing.T) {
	a := EvidenceDigest(map[string]any{"x": 1.0, "y": []any{"a"}})
	b := EvidenceDigest(map[string]any{"y": []any{"a"}, "x": 1.0})
	if a != b || len(a) != 64 {
		t.Fatalf("摘要应稳定: %s vs %s", a, b)
	}
}

// TestMaterialIdentity 材料身份：文件引用/环境展开/嵌套引用/环终止/纯文本。
func TestMaterialIdentity(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "conf.yaml")
	if err := os.WriteFile(config, []byte("name: demo\nasset: asset.bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "asset.bin"), []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity := MaterialIdentity(config, dir, nil)
	m, ok := identity.(map[string]any)
	if !ok || m["sha256"] == "" {
		t.Fatalf("文件引用应产出身份: %v", identity)
	}
	refs, ok := m["references"].(map[string]any)
	if !ok || refs["name"] != "demo" {
		t.Fatalf("嵌套引用应递归: %v", m["references"])
	}
	// 嵌套引用里的 asset.bin 也应成为文件身份
	if _, isMap := refs["asset"].(map[string]any); !isMap {
		t.Fatalf("嵌套资产应哈希: %v", refs["asset"])
	}
	// 环：自引用不再递归
	selfRef := filepath.Join(dir, "self.yaml")
	if err := os.WriteFile(selfRef, []byte("me: self.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{filepath.Clean(selfRef): true}
	selfIdentity := MaterialIdentity(selfRef, dir, seen)
	if _, hasRefs := selfIdentity.(map[string]any)["references"]; hasRefs {
		t.Fatal("已见文件不应再递归")
	}
	// 纯文本与远程路径原样返回
	if got := MaterialIdentity("plain prompt text", dir, nil); got != "plain prompt text" {
		t.Fatalf("纯文本应原样: %v", got)
	}
	if got := MaterialIdentity("container://remote/path", dir, nil); got != "container://remote/path" {
		t.Fatalf("远程路径应原样: %v", got)
	}
	// 环境变量展开（存在与缺失两分支）
	t.Setenv("EVIDENCE_TEST_VAR", "expanded")
	if got := MaterialIdentity("${EVIDENCE_TEST_VAR}", dir, nil); got != "expanded" {
		t.Fatalf("环境变量应展开: %v", got)
	}
	if got := MaterialIdentity("${EVIDENCE_TEST_MISSING}", dir, nil); got != "${EVIDENCE_TEST_MISSING}" {
		t.Fatalf("缺失变量应保留原文: %v", got)
	}
	// 列表与标量透传
	list := MaterialIdentity([]any{"x", 1, nil}, dir, nil)
	if list.([]any)[1] != 1 {
		t.Fatalf("标量应透传: %v", list)
	}
}

// TestHarnessIdentity harness 身份：目录递归清单与非目录回退。
func TestHarnessIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "harness", "__pycache__"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness", "a.yaml"), []byte("k: v\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness", "junk.pyc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness", "__pycache__", "c.pyc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity := HarnessIdentity(filepath.Join(dir, "harness")).(map[string]any)
	files, _ := identity["files"].(map[string]any)
	if _, present := files["a.yaml"]; !present {
		t.Fatalf("清单应含 a.yaml: %v", files)
	}
	if len(files) != 1 {
		t.Fatalf("应忽略编译产物: %v", files)
	}
	// 非目录 → 材料身份（字符串路径按 cwd 解析，本地不存在则原样）
	if got := HarnessIdentity("not-a-dir"); got != "not-a-dir" {
		t.Fatalf("非目录应回退材料身份: %v", got)
	}
}
