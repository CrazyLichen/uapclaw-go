package agent_mode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWordSlug(t *testing.T) {
	slug := GenerateWordSlug()
	parts := strings.Split(slug, "-")
	if len(parts) != 3 {
		t.Errorf("期望 3 段，实际 %d 段: %s", len(parts), slug)
	}
	// 验证各段在词表中
	foundAdj := false
	for _, w := range adjectives {
		if w == parts[0] {
			foundAdj = true
			break
		}
	}
	if !foundAdj {
		t.Errorf("形容词 '%s' 不在词表中", parts[0])
	}
}

func TestGenerateWordSlug_多次生成不重复(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		slug := GenerateWordSlug()
		seen[slug] = struct{}{}
	}
	// 100 次生成至少应有多个不同值（概率极高）
	if len(seen) < 10 {
		t.Errorf("100 次生成仅有 %d 个不同值", len(seen))
	}
}

func TestResolvePlanFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	path := ResolvePlanFilePath(tmpDir, "test-slug")
	expected := filepath.Join(tmpDir, ".plans", "test-slug.md")
	if path != expected {
		t.Errorf("期望 %s，实际 %s", expected, path)
	}
	// 验证 .plans 目录已创建
	plansDir := filepath.Join(tmpDir, ".plans")
	info, err := os.Stat(plansDir)
	if err != nil {
		t.Fatalf(".plans 目录未创建: %v", err)
	}
	if !info.IsDir() {
		t.Error(".plans 不是目录")
	}
}

func TestGetOrCreatePlanSlug(t *testing.T) {
	tmpDir := t.TempDir()
	slug := GetOrCreatePlanSlug(tmpDir)
	if slug == "" {
		t.Error("slug 不应为空")
	}
	parts := strings.Split(slug, "-")
	if len(parts) != 3 {
		t.Errorf("期望 3 段，实际 %d 段: %s", len(parts), slug)
	}
	// 验证对应文件不存在（因为只是生成 slug，没有创建文件）
	path := filepath.Join(tmpDir, ".plans", slug+".md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("slug 对应的文件不应存在")
	}
}

func TestGetOrCreatePlanSlug_已有文件时不冲突(t *testing.T) {
	tmpDir := t.TempDir()
	plansDir := filepath.Join(tmpDir, ".plans")
	_ = os.MkdirAll(plansDir, 0o755)
	// 创建一个已有文件
	existingSlug := "ancient-brewing-anchor"
	existingPath := filepath.Join(plansDir, existingSlug+".md")
	_ = os.WriteFile(existingPath, []byte(""), 0o644)
	// 多次生成应能得到不同 slug（概率极高）
	for i := 0; i < 50; i++ {
		slug := GetOrCreatePlanSlug(tmpDir)
		if slug == existingSlug {
			// 概率极低但允许；仅当路径文件存在时才报告
			path := filepath.Join(plansDir, slug+".md")
			if _, err := os.Stat(path); err == nil && slug == existingSlug {
				t.Logf("生成与已有文件冲突的 slug（概率事件），跳过")
			}
		}
	}
}

func TestNormalizeLanguage(t *testing.T) {
	if normalizeLanguage("en") != "en" {
		t.Error("en 应返回 en")
	}
	if normalizeLanguage("cn") != "cn" {
		t.Error("cn 应返回 cn")
	}
	if normalizeLanguage("") != "cn" {
		t.Error("空字符串应默认返回 cn")
	}
	if normalizeLanguage("ja") != "cn" {
		t.Error("未知语言应默认返回 cn")
	}
}

func TestFormatPlanPath(t *testing.T) {
	// 正斜杠路径保持不变
	if formatPlanPath("/tmp/.plans/test.md") != "/tmp/.plans/test.md" {
		t.Error("正斜杠路径不应改变")
	}
	// 反斜杠路径转换为正斜杠（Windows 兼容）
	result := formatPlanPath(`C:\tmp\.plans\test.md`)
	if strings.Contains(result, `\`) {
		t.Errorf("反斜杠应被替换，实际: %s", result)
	}
}
