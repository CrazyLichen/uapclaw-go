package browser_move

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNormalizeRouteSignature 测试路由签名规范化
func TestNormalizeRouteSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"根路径", "https://example.com/", "/"},
		{"简单路径", "https://example.com/products", "/products"},
		{"数字替换为星号", "https://example.com/items/123/details", "/items/*/details"},
		{"尾部斜杠移除", "https://example.com/catalogue/", "/catalogue"},
		{"多个数字", "https://example.com/u/42/p/7", "/u/*/p/*"},
		{"空URL", "", "/"},
		{"多斜杠合并", "https://example.com/a//b", "/a/b"},
		{"根路径带查询", "https://example.com/?q=test", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeRouteSignature(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeRouteSignature(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestDomainFromURL 测试从 URL 提取域名
func TestDomainFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"标准URL", "https://www.example.com/path", "www.example.com"},
		{"无协议", "example.com/path", ""},
		{"空字符串", "", ""},
		{"带端口", "https://localhost:8080/test", "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DomainFromURL(tt.input)
			if result != tt.expected {
				t.Errorf("DomainFromURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestBuiltinSiteProfiles 测试内置站点配置返回深拷贝
func TestBuiltinSiteProfiles(t *testing.T) {
	profiles := BuiltinSiteProfiles()
	if len(profiles) == 0 {
		t.Error("BuiltinSiteProfiles() 返回空列表")
	}

	// 验证是深拷贝，修改不影响原数据
	profiles[0]["id"] = "modified"
	original := BuiltinSiteProfiles()
	if original[0]["id"] == "modified" {
		t.Error("BuiltinSiteProfiles() 未返回深拷贝，修改影响了原数据")
	}
}

// TestUnique 测试字符串去重
func TestUnique(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		limit    int
		expected []string
	}{
		{"空列表", []string{}, 5, nil},
		{"基本去重", []string{"a", "b", "a", "c"}, 10, []string{"a", "b", "c"}},
		{"限制数量", []string{"a", "b", "c", "d", "e"}, 3, []string{"a", "b", "c"}},
		{"空字符串过滤", []string{"a", "", "b", "  "}, 10, []string{"a", "b"}},
		{"前后空格修剪", []string{"  a  ", "a", "b"}, 10, []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unique(tt.input, tt.limit)
			if len(result) != len(tt.expected) {
				t.Errorf("unique(%v, %d) = %v, want %v", tt.input, tt.limit, result, tt.expected)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("unique(%v, %d)[%d] = %q, want %q", tt.input, tt.limit, i, v, tt.expected[i])
				}
			}
		})
	}
}

// TestSelectorIsTooBroad 测试选择器宽泛判断
func TestSelectorIsTooBroad(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		expected bool
	}{
		{"空选择器", "", true},
		{"div", "div", true},
		{"li", "li", true},
		{"具体选择器", "article.product_pod", false},
		{"含nav片段", "#nav-bar", true},
		{"含header片段", "header.main", true},
		{"含footer片段", "footer.footer", true},
		{"正常选择器", "h3 a[title]", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selectorIsTooBroad(tt.selector)
			if result != tt.expected {
				t.Errorf("selectorIsTooBroad(%q) = %v, want %v", tt.selector, result, tt.expected)
			}
		})
	}
}

// TestLooksLikePageChrome 测试页面 chrome 检测
func TestLooksLikePageChrome(t *testing.T) {
	tests := []struct {
		name     string
		card     map[string]any
		expected bool
	}{
		{
			"导航元素",
			map[string]any{"selector_hint": "#nav-bar", "title": "Menu"},
			true,
		},
		{
			"chrome标题",
			map[string]any{"selector_hint": "div.item", "title": "Best Sellers"},
			true,
		},
		{
			"chrome预览文本",
			map[string]any{"selector_hint": "div.item", "text_preview": "Sign In"},
			true,
		},
		{
			"正常卡片",
			map[string]any{"selector_hint": "article.product_pod", "title": "Book Title"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikePageChrome(tt.card)
			if result != tt.expected {
				t.Errorf("looksLikePageChrome() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestCardQualityScore 测试卡片质量评分
func TestCardQualityScore(t *testing.T) {
	tests := []struct {
		name     string
		card     map[string]any
		minScore int
		maxScore int
	}{
		{
			"chrome卡片评分为0",
			map[string]any{"selector_hint": "#navbar", "title": "Login"},
			0, 0,
		},
		{
			"高质量卡片",
			map[string]any{
				"selector_hint": "article.product",
				"title":         "A Very Long Product Title",
				"text_preview":  "This is a very long preview text that exceeds sixty characters easily",
				"primary_link":  true,
				"price":         "$29.99",
				"rating":        "4.5",
				"has_image":     true,
			},
			60, 100,
		},
		{
			"空卡片",
			map[string]any{},
			0, 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := cardQualityScore(tt.card)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("cardQualityScore() = %d, want range [%d, %d]", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

// TestIsCacheableCard 测试卡片缓存判断
func TestIsCacheableCard(t *testing.T) {
	tests := []struct {
		name     string
		card     map[string]any
		expected bool
	}{
		{
			"高分卡片可缓存",
			map[string]any{
				"selector_hint": "article.product",
				"title":         "A Very Long Product Title Here",
				"primary_link":  true,
				"price":         "$29.99",
				"rating":        "4.5",
				"has_image":     true,
			},
			true,
		},
		{
			"chrome卡片不可缓存",
			map[string]any{"selector_hint": "#navbar", "title": "Login"},
			false,
		},
		{
			"空卡片不可缓存",
			map[string]any{},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCacheableCard(tt.card)
			if result != tt.expected {
				t.Errorf("isCacheableCard() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestGeneralizeSelector 测试选择器泛化
func TestGeneralizeSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		wantAny  []string // 结果中应包含的选择器
	}{
		{
			"简单选择器",
			"article.product_pod",
			[]string{"article.product_pod"},
		},
		{
			"带nth-of-type",
			"ol.row > li:nth-of-type(3) > article.product_pod",
			[]string{"article.product_pod", "li > article.product_pod"},
		},
		{
			"空选择器",
			"",
			nil,
		},
		{
			"宽泛选择器被过滤",
			"div",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generalizeSelector(tt.selector)
			if len(tt.wantAny) == 0 {
				if len(result) != 0 {
					t.Errorf("generalizeSelector(%q) = %v, want empty", tt.selector, result)
				}
				return
			}
			for _, want := range tt.wantAny {
				found := false
				for _, got := range result {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("generalizeSelector(%q) = %v, want to contain %q", tt.selector, result, want)
				}
			}
		})
	}
}

// TestBrowserSelectorCache_ExportForProbe 测试导出探测缓存
func TestBrowserSelectorCache_ExportForProbe(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")

	// 准备测试数据
	data := map[string]any{
		"version": 1,
		"records": []any{
			map[string]any{
				"domain":          "example.com",
				"route_signature": "/products",
				"kind":            "card_probe",
				"quality_score":   0.8,
				"success_count":   5,
				"failure_count":   1,
				"last_success_at": 1000.0,
				"selectors": map[string]any{
					"card_container_selectors": []any{"article.product"},
				},
			},
			map[string]any{
				"domain":          "other.com",
				"route_signature": "/items",
				"kind":            "card_probe",
				"quality_score":   0.5,
				"success_count":   2,
				"failure_count":   3,
				"last_success_at": 500.0,
				"selectors":       map[string]any{},
			},
		},
	}
	raw, _ := json.MarshalIndent(data, "", "  ")
	_ = os.WriteFile(cachePath, raw, 0o644)

	cache := NewBrowserSelectorCache(cachePath)
	records := cache.ExportForProbe(10)

	if len(records) != 2 {
		t.Fatalf("ExportForProbe() returned %d records, want 2", len(records))
	}

	// 应按 quality_score 降序排列
	domain0, _ := records[0]["domain"].(string)
	if domain0 != "example.com" {
		t.Errorf("ExportForProbe()[0].domain = %q, want %q", domain0, "example.com")
	}
}

// TestBrowserSelectorCache_ExportForProbe空文件 测试空缓存导出
func TestBrowserSelectorCache_ExportForProbe空文件(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "nonexistent.json")

	cache := NewBrowserSelectorCache(cachePath)
	records := cache.ExportForProbe()

	if len(records) != 0 {
		t.Errorf("ExportForProbe() on nonexistent file = %d records, want 0", len(records))
	}
}

// TestBrowserSelectorCache_RecordCardProbeResult 测试记录卡片探测结果
func TestBrowserSelectorCache_RecordCardProbeResult(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")

	cache := NewBrowserSelectorCache(cachePath)

	result := map[string]any{
		"ok":  true,
		"url": "https://books.toscrape.com/catalogue/category/books_1/index.html",
		"cards": []any{
			map[string]any{
				"selector_hint":       "article.product_pod",
				"title_selector_hint": "h3 a",
				"title":               "A Long Book Title Here",
				"text_preview":        "This is a very long preview text that exceeds eighty characters for caching eligibility",
				"primary_link":        true,
				"price":               "£10.99",
				"rating":              "4",
				"has_image":           true,
				"availability":        "In Stock",
				"buttons":             []any{},
			},
			map[string]any{
				"selector_hint":       "article.product_pod",
				"title_selector_hint": "h3 a",
				"title":               "Another Long Book Title Here",
				"text_preview":        "Another very long preview text that exceeds eighty characters for caching eligibility",
				"primary_link":        true,
				"price":               "£15.99",
				"rating":              "3",
				"has_image":           true,
				"availability":        "In Stock",
				"buttons":             []any{},
			},
		},
	}

	cache.RecordCardProbeResult(result)

	// 验证数据已持久化
	records := cache.ExportForProbe()
	if len(records) == 0 {
		t.Error("RecordCardProbeResult() 未持久化任何记录")
	}

	// 验证记录内容
	found := false
	for _, rec := range records {
		if rec["domain"] == "books.toscrape.com" {
			found = true
			if rec["success_count"] != float64(1) {
				t.Errorf("success_count = %v, want 1", rec["success_count"])
			}
			sels, _ := rec["selectors"].(map[string]any)
			if len(sels) == 0 {
				t.Error("selectors 为空")
			}
			break
		}
	}
	if !found {
		t.Error("未找到 books.toscrape.com 的缓存记录")
	}
}

// TestBrowserSelectorCache_RecordCardProbeResult_非成功结果 不记录非成功结果
func TestBrowserSelectorCache_RecordCardProbeResult_非成功结果(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")
	cache := NewBrowserSelectorCache(cachePath)

	// ok=false 不应记录
	cache.RecordCardProbeResult(map[string]any{"ok": false})
	records := cache.ExportForProbe()
	if len(records) != 0 {
		t.Error("ok=false 的结果不应被记录")
	}

	// 无 URL 不应记录
	cache.RecordCardProbeResult(map[string]any{"ok": true, "url": ""})
	records = cache.ExportForProbe()
	if len(records) != 0 {
		t.Error("无 URL 的结果不应被记录")
	}
}

// TestBrowserSelectorCache_RecordCardProbeResult_合并 测试多次记录合并
func TestBrowserSelectorCache_RecordCardProbeResult_合并(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")
	cache := NewBrowserSelectorCache(cachePath)

	card := map[string]any{
		"selector_hint":       "article.product_pod",
		"title_selector_hint": "h3 a",
		"title":               "A Very Long Book Title For Testing",
		"text_preview":        "This is a very long preview text that exceeds eighty characters for cache eligibility test",
		"primary_link":        true,
		"price":               "£10.99",
		"rating":              "4",
		"has_image":           true,
	}

	result := map[string]any{
		"ok":    true,
		"url":   "https://example.com/products/1",
		"cards": []any{card, card},
	}

	// 记录两次
	cache.RecordCardProbeResult(result)
	cache.RecordCardProbeResult(result)

	records := cache.ExportForProbe()
	if len(records) != 1 {
		t.Fatalf("合并后应有 1 条记录，实际 %d 条", len(records))
	}

	if records[0]["success_count"] != float64(2) {
		t.Errorf("合并后 success_count = %v, want 2", records[0]["success_count"])
	}
}

// TestBrowserSelectorCache_记录上限 测试缓存记录上限
func TestBrowserSelectorCache_记录上限(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")
	cache := NewBrowserSelectorCache(cachePath)

	card := map[string]any{
		"selector_hint":       "article.product",
		"title_selector_hint": "h3 a",
		"title":               "A Very Long Product Title For Testing Purposes",
		"text_preview":        "This is a very long preview text that exceeds eighty characters for cache eligibility test",
		"primary_link":        true,
		"price":               "$10.99",
		"rating":              "4",
		"has_image":           true,
	}

	// 插入超过上限的记录
	for i := 0; i < selectorCacheMaxRecords+10; i++ {
		result := map[string]any{
			"ok":    true,
			"url":   "https://example.com/items/" + string(rune('a'+i%26)) + "/details",
			"cards": []any{card, card},
		}
		cache.RecordCardProbeResult(result)
	}

	records := cache.ExportForProbe(1000)
	if len(records) > selectorCacheMaxRecords {
		t.Errorf("记录数 %d 超过上限 %d", len(records), selectorCacheMaxRecords)
	}
}

// TestGetSelectorCache 测试全局缓存单例
func TestGetSelectorCache(t *testing.T) {
	cache1 := GetSelectorCache()
	cache2 := GetSelectorCache()
	if cache1 != cache2 {
		t.Error("GetSelectorCache() 未返回同一实例")
	}
}

// TestToFloat64Or 测试浮点数转换
func TestToFloat64Or(t *testing.T) {
	tests := []struct {
		name       string
		input      any
		defaultVal float64
		expected   float64
	}{
		{"float64", 3.14, 0, 3.14},
		{"int", 42, 0, 42},
		{"nil", nil, 1.5, 1.5},
		{"字符串", "2.5", 0, 2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toFloat64Or(tt.input, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("toFloat64Or(%v, %v) = %v, want %v", tt.input, tt.defaultVal, result, tt.expected)
			}
		})
	}
}

// TestToIntOrZero 测试整数转换
func TestToIntOrZero(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{"int", 42, 42},
		{"float64", 3.0, 3},
		{"nil", nil, 0},
		{"字符串数字", "7", 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toIntOrZero(tt.input)
			if result != tt.expected {
				t.Errorf("toIntOrZero(%v) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

// TestDeepCopyProfiles 测试深拷贝站点配置
func TestDeepCopyProfiles(t *testing.T) {
	original := []map[string]any{
		{"id": "test", "domains": []string{"example.com"}},
	}
	copied := deepCopyProfiles(original)

	copied[0]["id"] = "modified"
	if original[0]["id"] == "modified" {
		t.Error("deepCopyProfiles 未正确深拷贝")
	}
}
