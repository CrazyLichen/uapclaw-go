package tool_discovery

import (
	"testing"

	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// testToolInfo 构造测试用工具记录。
func testToolInfo(name string, description string, parameters map[string]any) cschema.ToolInfoInterface {
	info := cschema.ToolInfo{}
	info.Type = "function"
	info.Name = name
	info.Description = description
	info.Parameters = parameters
	return &info
}

// names 提取工具名列表。
func names(documents []cschema.ToolInfoInterface) []string {
	out := make([]string, 0, len(documents))
	for _, document := range documents {
		out = append(out, document.GetName())
	}
	return out
}

// TestBM25ToolIndex_精确名优先 验证精确名命中排在最高分之前。
func TestBM25ToolIndex_精确名优先(t *testing.T) {
	documents := []cschema.ToolInfoInterface{
		testToolInfo("search_skill", "搜索技能目录", nil),
		testToolInfo("helper", "search skill for everything search skill", nil),
	}
	index := BuildBM25ToolIndex(documents, nil)
	got := index.Search("search_skill", 5)
	if len(got) == 0 || got[0].GetName() != "search_skill" {
		t.Fatalf("精确名应排首位，got %v", names(got))
	}
}

// TestBM25ToolIndex_名字边界 验证子串不算精确命中（只影响精确匹配判定）。
func TestBM25ToolIndex_名字边界(t *testing.T) {
	if containsExactToolName("请调用search_skill搜索", "search_skill_extra") {
		t.Fatal("search_skill_extra 不应被当作 query 的精确命中")
	}
	if !containsExactToolName("请调用search_skill搜索", "search_skill") {
		t.Fatal("中文包夹下的完整工具名应判为精确命中")
	}
	if containsExactToolName("search_skill_extra", "search_skill") {
		t.Fatal("search_skill 后接下划线时不应判为精确命中")
	}
	if !containsExactToolName("search_skill", "search_skill") {
		t.Fatal("完全一致应判为精确命中")
	}
}

// TestBM25ToolIndex_Fingerprint随参数变化 验证指纹对 schema 变化敏感、对 key 顺序不敏感。
func TestBM25ToolIndex_Fingerprint随参数变化(t *testing.T) {
	base := testToolInfo("x", "d", map[string]any{
		"type":       "object",
		"properties": map[string]any{"a": map[string]any{"type": "string"}},
	})
	changed := testToolInfo("x", "d", map[string]any{
		"type":       "object",
		"properties": map[string]any{"b": map[string]any{"type": "string"}},
	})
	if ToolFingerprint(base) == ToolFingerprint(changed) {
		t.Fatal("参数变化应改变指纹")
	}
	same := testToolInfo("x", "d", map[string]any{
		"properties": map[string]any{"a": map[string]any{"type": "string"}},
		"type":       "object",
	})
	if ToolFingerprint(base) != ToolFingerprint(same) {
		t.Fatal("参数 key 顺序不应影响指纹")
	}
}

// TestBM25ToolIndex_extraSearchText参与 验证额外检索文本生效。
func TestBM25ToolIndex_extraSearchText参与(t *testing.T) {
	documents := []cschema.ToolInfoInterface{testToolInfo("t1", "无关描述", nil)}
	index := BuildBM25ToolIndex(documents, []string{"电子表格 excel"})
	if got := index.Search("excel", 5); len(got) != 1 {
		t.Fatalf("额外检索文本应参与匹配，got %v", names(got))
	}
	if index.SourceFingerprint() == "" {
		t.Fatal("源指纹不应为空")
	}
}

// TestBM25ToolIndex_Limit与空查询 验证边界行为。
func TestBM25ToolIndex_Limit与空查询(t *testing.T) {
	documents := []cschema.ToolInfoInterface{
		testToolInfo("a", "alpha", nil),
		testToolInfo("b", "alpha", nil),
	}
	index := BuildBM25ToolIndex(documents, nil)
	if got := index.Search("alpha", 0); got != nil {
		t.Fatalf("limit<=0 应返回 nil，got %v", names(got))
	}
	if got := index.Search("", 5); got != nil {
		t.Fatalf("空查询应返回 nil，got %v", names(got))
	}
	if got := index.Search("alpha", 1); len(got) != 1 {
		t.Fatalf("limit=1 应只返回 1 条，got %v", names(got))
	}
	if index.DocumentCount() != 2 {
		t.Fatalf("文档数应为 2，got %d", index.DocumentCount())
	}
	if len(index.Documents()) != 2 {
		t.Fatal("Documents 应返回副本")
	}
}
