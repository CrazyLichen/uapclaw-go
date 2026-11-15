package harness_providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewSkillSource_构造校验 对齐 __post_init__（skills.py:31-40）。
func TestNewSkillSource_构造校验(t *testing.T) {
	if _, err := NewSkillSource("", "all", nil); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("空 dir 应报错: %v", err)
	}
	if _, err := NewSkillSource("/d", "merge", nil); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("非法 mode 应报错: %v", err)
	}
	s, err := NewSkillSource("/d", "auto_list", []string{"a", "b"})
	if err != nil || s.Dir != "/d" || s.Mode != "auto_list" || len(s.EnabledSkills) != 2 {
		t.Fatalf("合法构造失败: %v %+v", err, s)
	}
}

// TestNormalizeSkills_三形态 对齐 normalize_skills（skills.py:43-59）。
func TestNormalizeSkills_三形态(t *testing.T) {
	if _, err := NormalizeSkills(nil, "merge"); err == nil {
		t.Fatal("非法 conflict 应报错")
	}
	src, _ := NewSkillSource("/d", "all", nil)
	out, err := NormalizeSkills([]any{src, "/x", map[string]any{"dir": "/y"}}, "skip")
	if err != nil || len(out) != 3 || out[0].Dir != "/d" || out[1].Dir != "/x" || out[2].Dir != "/y" {
		t.Fatalf("三形态归一失败: %v %+v", err, out)
	}
	if _, err := NormalizeSkills([]any{42}, "skip"); err == nil {
		t.Fatal("非法元素应报错")
	}
}

// writeBundle 造一个最小技能包（对齐 test_skills.py bundle 助手）。
func writeBundle(t *testing.T, root string, name, text string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: " + name + "\ndescription: Test skill\n---\n" + text + "\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	// Python bundle 助手还建 scripts/run.sh（0o755）与 .hidden 二进制
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "run.sh"), []byte("echo resource"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte{0x00, 0x01, 'r', 'e', 's', 'o', 'u', 'r', 'c', 'e'}, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestSkillName_frontMatter 对齐 _name（skills.py:62-80）。
func TestSkillName_frontMatter(t *testing.T) {
	dir := writeBundle(t, filepath.Join(t.TempDir(), "pkg"), "example", "instructions")
	if got, err := parseSkillName(dir); err != nil || got != "example" {
		t.Fatalf("期望 example，实际 %s err=%v", got, err)
	}
	// 缺 front matter → 目录名兜底
	bare := filepath.Join(t.TempDir(), "bare")
	os.MkdirAll(bare, 0o755)
	if err := os.WriteFile(filepath.Join(bare, "SKILL.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := parseSkillName(bare); err != nil || got != "bare" {
		t.Fatalf("无 front matter 应取目录名: %s err=%v", got, err)
	}
	// 非法名（../escape）→ 报错含 "name"
	bad := writeBundle(t, filepath.Join(t.TempDir(), "esc"), "../escape", "x")
	if _, err := parseSkillName(bad); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("非法名应报错: %v", err)
	}
	// 未闭合 front matter → 报错含 "unterminated"
	unclosed := filepath.Join(t.TempDir(), "unclosed")
	os.MkdirAll(unclosed, 0o755)
	if err := os.WriteFile(filepath.Join(unclosed, "SKILL.md"), []byte("---\nname: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSkillName(unclosed); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("未闭合应报错: %v", err)
	}
}

// TestDiscoverBundles 对齐 _discover（skills.py:83-90）：根含 SKILL.md 即单包，否则排序递归。
func TestDiscoverBundles(t *testing.T) {
	root := t.TempDir()
	writeBundle(t, filepath.Join(root, "b"), "b", "x")
	writeBundle(t, filepath.Join(root, "a", "nested"), "a", "x")
	bundles, err := discoverBundles(root)
	if err != nil || len(bundles) != 2 {
		t.Fatalf("发现失败: %v %v", err, bundles)
	}
	// 排序序：a/nested 先于 b
	if !strings.HasSuffix(bundles[0], filepath.Join("a", "nested")) {
		t.Fatalf("排序错误: %v", bundles)
	}
}
