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
