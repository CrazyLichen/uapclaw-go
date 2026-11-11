package skill

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// buildZipBytes 生成一个 ZIP 字节流，files 为「相对路径 → 内容」。
func buildZipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("创建 zip 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写入 zip 内容失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes()
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestCopyFile 验证文件复制与源文件缺失报错。
func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("复制失败: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("内容 = %q, 期望 hello", string(data))
	}

	if err := copyFile(filepath.Join(dir, "missing.txt"), dst); err == nil {
		t.Fatal("源文件不存在应报错")
	}
}

// TestParseYAMLFrontmatterFallback 验证简易 frontmatter 解析。
func TestParseYAMLFrontmatterFallback(t *testing.T) {
	t.Run("解析键值并去引号", func(t *testing.T) {
		got := parseYAMLFrontmatterFallback("name: \"my-skill\"\ndescription: 'desc'\nversion: 1")
		if got["name"] != "my-skill" {
			t.Fatalf("name = %v, 期望 my-skill", got["name"])
		}
		if got["description"] != "desc" {
			t.Fatalf("description = %v, 期望 desc", got["description"])
		}
		if got["version"] != "1" {
			t.Fatalf("version = %v, 期望 1", got["version"])
		}
	})

	t.Run("跳过空行与注释", func(t *testing.T) {
		got := parseYAMLFrontmatterFallback("# comment\n\nname: x")
		if len(got) != 1 || got["name"] != "x" {
			t.Fatalf("应只解析出 name，实际 %v", got)
		}
	})

	t.Run("无冒号行被跳过", func(t *testing.T) {
		got := parseYAMLFrontmatterFallback("no-colon-line")
		if len(got) != 0 {
			t.Fatalf("应跳过无冒号行，实际 %v", got)
		}
	})

	t.Run("空输入返回空 map", func(t *testing.T) {
		if got := parseYAMLFrontmatterFallback("   "); len(got) != 0 {
			t.Fatalf("空输入应返回空 map，实际 %v", got)
		}
	})
}

// TestMapSliceToAny 验证 []map 到 []any 的转换保序。
func TestMapSliceToAny(t *testing.T) {
	items := []map[string]any{{"a": 1}, {"b": 2}}
	got := mapSliceToAny(items)
	if len(got) != 2 {
		t.Fatalf("长度 = %d, 期望 2", len(got))
	}
	first, ok := got[0].(map[string]any)
	if !ok || first["a"] != 1 {
		t.Fatalf("首个元素 = %v", got[0])
	}
	if empty := mapSliceToAny(nil); len(empty) != 0 {
		t.Fatalf("nil 应得空切片，实际 %v", empty)
	}
}

// TestTruncateDetailRunes 验证按 rune 截断（CJK 不被切断）。
func TestTruncateDetailRunes(t *testing.T) {
	t.Run("maxRunes<=0 时原样返回", func(t *testing.T) {
		if got := truncateDetailRunes("abc", 0); got != "abc" {
			t.Fatalf("应原样返回，实际 %q", got)
		}
	})

	t.Run("未超限原样返回", func(t *testing.T) {
		if got := truncateDetailRunes("abc", 5); got != "abc" {
			t.Fatalf("应原样返回，实际 %q", got)
		}
	})

	t.Run("超限按 rune 截断", func(t *testing.T) {
		got := truncateDetailRunes("中文内容超长", 2)
		if got != "中文" {
			t.Fatalf("应截断为「中文」，实际 %q", got)
		}
		if len([]rune(got)) != 2 {
			t.Fatalf("rune 数 = %d, 期望 2", len([]rune(got)))
		}
	})
}

// TestInstallFailedResult 验证安装失败响应结构。
func TestInstallFailedResult(t *testing.T) {
	got := installFailedResult("boom")
	if got["success"] != false {
		t.Fatalf("success 应为 false，实际 %v", got["success"])
	}
	if got["detail"] != "boom" {
		t.Fatalf("detail = %v, 期望 boom", got["detail"])
	}
	if got["detail_key"] != teamSkillsHubInstallFailedKey {
		t.Fatalf("detail_key = %v, 期望 %v", got["detail_key"], teamSkillsHubInstallFailedKey)
	}
}

// TestSafeExtractZIPBytesToDir 验证 ZIP 解压与 Zip Slip 防护。
func TestSafeExtractZIPBytesToDir(t *testing.T) {
	t.Run("正常解压", func(t *testing.T) {
		dir := t.TempDir()
		data := buildZipBytes(t, map[string]string{"a/b.txt": "hello"})
		if err := safeExtractZIPBytesToDir(data, dir); err != nil {
			t.Fatalf("解压失败: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "a", "b.txt"))
		if err != nil {
			t.Fatalf("读取解压文件失败: %v", err)
		}
		if string(got) != "hello" {
			t.Fatalf("内容 = %q", string(got))
		}
	})

	t.Run("Zip Slip 被拒绝", func(t *testing.T) {
		dir := t.TempDir()
		data := buildZipBytes(t, map[string]string{"../escaped.txt": "bad"})
		err := safeExtractZIPBytesToDir(data, dir)
		if err == nil || !strings.Contains(err.Error(), "zip slip") {
			t.Fatalf("应检测到 zip slip，实际 %v", err)
		}
	})

	t.Run("非法 ZIP 报读取失败", func(t *testing.T) {
		err := safeExtractZIPBytesToDir([]byte("not-a-zip"), t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "ZIP 读取失败") {
			t.Fatalf("应报 ZIP 读取失败，实际 %v", err)
		}
	})
}

// ──────────────────────────── 导出函数补充测试 ────────────────────────────

// TestIsBuiltinSkill 验证内置技能判定。
// Python: SkillManager._uninstall_skill_body 内置保护段（skill_manager.py:4855-4874）
//
// 判据是「内置源中能定位到同名技能」且「skillsDir/<name> 与它 resolve() 后为同一物理路径」。
// 由于内置技能安装走**复制**（copyDir）而非软链，两侧是不同实体，resolve 后不等，
// 故个人版下已安装的内置技能判定为**非内置、可卸载**——与 Python 注释
// 「内置：仅企业版不可卸载（个人版与 develop 一致）」一致。
func TestIsBuiltinSkill(t *testing.T) {
	t.Run("空名称返回 false", func(t *testing.T) {
		sm := NewSkillManager(t.TempDir())
		ok, dir := sm.isBuiltinSkill("", t.TempDir())
		if ok || dir != "" {
			t.Fatalf("应返回 (false, \"\")，实际 (%v, %q)", ok, dir)
		}
	})

	t.Run("内置目录为空返回 false", func(t *testing.T) {
		sm := NewSkillManager(t.TempDir())
		ok, dir := sm.isBuiltinSkill("any", "")
		if ok || dir != "" {
			t.Fatalf("应返回 (false, \"\")，实际 (%v, %q)", ok, dir)
		}
	})

	t.Run("内置目录不存在返回 false", func(t *testing.T) {
		sm := NewSkillManager(t.TempDir())
		ok, dir := sm.isBuiltinSkill("any", filepath.Join(t.TempDir(), "missing"))
		if ok || dir != "" {
			t.Fatalf("应返回 (false, \"\")，实际 (%v, %q)", ok, dir)
		}
	})

	t.Run("复制安装的内置技能不算内置", func(t *testing.T) {
		// 内置源：<builtinRoot>/<name>（单层，与真实内置技能结构一致）
		builtinRoot := t.TempDir()
		builtinSkill := filepath.Join(builtinRoot, "builtin-one")
		if err := os.MkdirAll(builtinSkill, 0o755); err != nil {
			t.Fatalf("创建内置技能目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(builtinSkill, "SKILL.md"),
			[]byte("---\nname: builtin-one\ndescription: d\n---\nbody"), 0o644); err != nil {
			t.Fatalf("写入 SKILL.md 失败: %v", err)
		}

		// 用户目录：复制一份（不同实体）
		sm := NewSkillManager(t.TempDir())
		userSkill := filepath.Join(sm.skillsDir, "builtin-one")
		if err := os.MkdirAll(userSkill, 0o755); err != nil {
			t.Fatalf("创建用户技能目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"),
			[]byte("---\nname: builtin-one\ndescription: d\n---\nbody"), 0o644); err != nil {
			t.Fatalf("写入 SKILL.md 失败: %v", err)
		}

		ok, dir := sm.isBuiltinSkill("builtin-one", builtinRoot)
		if ok || dir != "" {
			t.Fatalf("复制安装的内置技能两侧实体不同，不应判为内置，实际 (%v, %q)", ok, dir)
		}
	})

	t.Run("内置源无同名技能返回 false", func(t *testing.T) {
		builtinRoot := t.TempDir()
		if err := os.MkdirAll(filepath.Join(builtinRoot, "other-skill"), 0o755); err != nil {
			t.Fatalf("创建内置技能目录失败: %v", err)
		}
		sm := NewSkillManager(t.TempDir())
		if err := os.MkdirAll(filepath.Join(sm.skillsDir, "my-skill"), 0o755); err != nil {
			t.Fatalf("创建用户技能目录失败: %v", err)
		}

		ok, dir := sm.isBuiltinSkill("my-skill", builtinRoot)
		if ok || dir != "" {
			t.Fatalf("内置源无此技能不应判为内置，实际 (%v, %q)", ok, dir)
		}
	})

	t.Run("目录名与SKILL.md的name不一致时按name命中", func(t *testing.T) {
		// 内置源目录名是 dir-slug，但 SKILL.md 里 name 是 logical-name
		builtinRoot := t.TempDir()
		dirSlug := filepath.Join(builtinRoot, "dir-slug")
		if err := os.MkdirAll(dirSlug, 0o755); err != nil {
			t.Fatalf("创建内置技能目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dirSlug, "SKILL.md"),
			[]byte("---\nname: logical-name\ndescription: d\n---\nbody"), 0o644); err != nil {
			t.Fatalf("写入 SKILL.md 失败: %v", err)
		}

		// 第二段查找应能按 SKILL.md 的 name 定位到 dir-slug
		sm := NewSkillManager(t.TempDir())
		if got := sm.resolveBuiltinSkillPath("logical-name", builtinRoot); got != dirSlug {
			t.Fatalf("resolveBuiltinSkillPath = %q, 期望 %q", got, dirSlug)
		}
	})

	t.Run("下划线开头的内置目录被跳过", func(t *testing.T) {
		builtinRoot := t.TempDir()
		hidden := filepath.Join(builtinRoot, "_hidden")
		if err := os.MkdirAll(hidden, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(hidden, "SKILL.md"),
			[]byte("---\nname: hidden-name\ndescription: d\n---\n"), 0o644); err != nil {
			t.Fatalf("写入 SKILL.md 失败: %v", err)
		}
		sm := NewSkillManager(t.TempDir())
		if got := sm.resolveBuiltinSkillPath("hidden-name", builtinRoot); got != "" {
			t.Fatalf("下划线目录应被跳过，实际返回 %q", got)
		}
	})
}
