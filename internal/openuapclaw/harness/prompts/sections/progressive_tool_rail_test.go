package sections

import (
	"strings"
	"testing"
)

// TestCompactToolDescription_三档 验证摘要压缩的三种情况。
func TestCompactToolDescription_三档(t *testing.T) {
	if got := compactToolDescription("  a   b  "); got != "a b" {
		t.Fatalf("应折叠空白，got %q", got)
	}
	long := strings.Repeat("字", 200)
	got := compactToolDescription(long)
	if len([]rune(got)) != maxDeferredToolSummaryChars {
		t.Fatalf("超长应截断到 %d，got %d", maxDeferredToolSummaryChars, len([]rune(got)))
	}
	if !strings.HasSuffix(got, deferredToolSummaryEllipsis) {
		t.Fatalf("超长应加省略号，got %q", got)
	}
	if got := compactToolDescription("读取表格。" + strings.Repeat("字", 200)); got != "读取表格。" {
		t.Fatalf("句末标点应优先，got %q", got)
	}
}

// TestBuildProgressiveToolRulesSection_含初始目录 验证规则正文与目录排序。
func TestBuildProgressiveToolRulesSection_含初始目录(t *testing.T) {
	section := BuildProgressiveToolRulesSection(map[string]string{
		"xlsx_read": "读取 Excel",
		"pdf_read":  "提取 PDF",
	})
	if section.Name != SectionProgressiveToolRules {
		t.Fatalf("节名不符，got %s", section.Name)
	}
	if section.Priority != 75 {
		t.Fatalf("priority 应为 75，got %d", section.Priority)
	}
	content := section.Content["cn"]
	if !strings.Contains(content, "# 渐进式工具使用规则") {
		t.Fatalf("缺少规则标题，got %s", content)
	}
	if !strings.Contains(content, "- **xlsx_read**: 读取 Excel") {
		t.Fatalf("初始目录缺失，got %s", content)
	}
	if strings.Index(content, "pdf_read") > strings.Index(content, "xlsx_read") {
		t.Fatal("目录应按名字排序")
	}
	if !strings.Contains(section.Content["en"], "Progressive Tool Usage Rules") {
		t.Fatal("英文节内容缺失")
	}
}

// TestBuildProgressiveToolRulesSection_空目录 验证空目录文案。
func TestBuildProgressiveToolRulesSection_空目录(t *testing.T) {
	section := BuildProgressiveToolRulesSection(nil)
	if !strings.Contains(section.Content["cn"], "当前没有可通过 tool_search 搜索的 deferred 工具") {
		t.Fatalf("空目录文案不符，got %s", section.Content["cn"])
	}
	if !strings.Contains(section.Content["en"], "No deferred tools are currently available") {
		t.Fatalf("英文空目录文案不符，got %s", section.Content["en"])
	}
}

// TestRenderDeferredToolCatalogSnapshot 验证快照文案与版本号。
func TestRenderDeferredToolCatalogSnapshot(t *testing.T) {
	content := RenderDeferredToolCatalogSnapshot(map[string]string{"a": "工具 A"}, "cn", 3)
	for _, want := range []string{"目录版本：3", "- **a**: 工具 A"} {
		if !strings.Contains(content, want) {
			t.Fatalf("快照缺 %s：%s", want, content)
		}
	}
	english := RenderDeferredToolCatalogSnapshot(nil, "en", 1)
	if !strings.Contains(english, "Catalog version: 1") {
		t.Fatalf("英文快照版本缺失：%s", english)
	}
}

// TestRenderDeferredToolCatalogDelta_三段 验证增量三段的完整渲染。
func TestRenderDeferredToolCatalogDelta_三段(t *testing.T) {
	content := RenderDeferredToolCatalogDelta(
		map[string]string{"new_tool": "新工具"},
		map[string]string{"upd_tool": "改过的工具"},
		[]string{"old_tool"},
		"cn", 3,
	)
	for _, want := range []string{"目录版本：3", "新增", "修改", "删除", "new_tool", "upd_tool", "old_tool"} {
		if !strings.Contains(content, want) {
			t.Fatalf("delta 缺 %s：%s", want, content)
		}
	}
}

// TestRenderDeferredToolCatalogDelta_无变化 验证无变化文案。
func TestRenderDeferredToolCatalogDelta_无变化(t *testing.T) {
	content := RenderDeferredToolCatalogDelta(nil, nil, nil, "cn", 2)
	if !strings.Contains(content, "本次没有变化。") {
		t.Fatalf("无变化文案不符：%s", content)
	}
	english := RenderDeferredToolCatalogDelta(nil, nil, nil, "en", 2)
	if !strings.Contains(english, "No changes.") {
		t.Fatalf("英文无变化文案不符：%s", english)
	}
}
