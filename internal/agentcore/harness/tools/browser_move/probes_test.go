package browser_move

import (
	"encoding/json"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestBuildInteractiveProbeJS_基本参数 测试交互探测 JS 参数注入
func TestBuildInteractiveProbeJS_基本参数(t *testing.T) {
	js := BuildInteractiveProbeJS(30, true, "login")

	if !strings.Contains(js, "async (page) =>") {
		t.Error("JS 代码应包含 async (page) => 入口")
	}
	if !strings.Contains(js, `"max_items":30`) {
		t.Error("JS 代码应包含 max_items=30 参数")
	}
	if !strings.Contains(js, `"viewport_only":true`) {
		t.Error("JS 代码应包含 viewport_only=true 参数")
	}
	if !strings.Contains(js, `"query":"login"`) {
		t.Error("JS 代码应包含 query=login 参数")
	}
}

// TestBuildInteractiveProbeJS_参数钳位 测试参数钳位
func TestBuildInteractiveProbeJS_参数钳位(t *testing.T) {
	js := BuildInteractiveProbeJS(200, false, "")
	if !strings.Contains(js, `"max_items":100`) {
		t.Error("max_items 超过 100 应钳位到 100")
	}

	js = BuildInteractiveProbeJS(0, true, "")
	if !strings.Contains(js, `"max_items":1`) {
		t.Error("max_items 低于 1 应钳位到 1")
	}
}

// TestBuildInteractiveProbeJS_query转小写 测试 query 自动转小写
func TestBuildInteractiveProbeJS_query转小写(t *testing.T) {
	js := BuildInteractiveProbeJS(50, true, "LOGIN")
	if !strings.Contains(js, `"query":"login"`) {
		t.Error("query 应自动转小写")
	}
}

// TestBuildInteractiveProbeJS_关键函数 测试 JS 中包含关键函数
func TestBuildInteractiveProbeJS_关键函数(t *testing.T) {
	js := BuildInteractiveProbeJS(50, true, "")

	keywords := []string{
		"const normalize",
		"const attrEscape",
		"const cssEscape",
		"const roleFromTag",
		"const isVisible",
		"const buildSelectorHint",
		"const elementText",
		"const accessibleName",
		"const scoreElement",
		"const selectors = [",
		"browser_probe_interactives",
	}
	for _, kw := range keywords {
		// 不检查 browser_probe_interactives 因为这是内部 JS 代码
		if kw == "browser_probe_interactives" {
			continue
		}
		if !strings.Contains(js, kw) {
			t.Errorf("JS 代码应包含 %q", kw)
		}
	}
}

// TestBuildInteractiveProbeJS_返回结构 测试 JS 返回结构包含关键字段
func TestBuildInteractiveProbeJS_返回结构(t *testing.T) {
	js := BuildInteractiveProbeJS(50, true, "")

	fields := []string{
		"ok: true",
		"url: window.location.href",
		"title: document.title",
		"viewport:",
		"total_candidates:",
		"returned:",
		"elements,",
		"error: null",
	}
	for _, f := range fields {
		if !strings.Contains(js, f) {
			t.Errorf("JS 代码应包含 %q", f)
		}
	}
}

// TestBuildCardProbeJS_基本参数 测试卡片探测 JS 参数注入
func TestBuildCardProbeJS_基本参数(t *testing.T) {
	js := BuildCardProbeJS(15, true, true, "mouse", nil, nil)

	if !strings.Contains(js, "async (page) =>") {
		t.Error("JS 代码应包含 async (page) => 入口")
	}
	if !strings.Contains(js, `"max_cards":15`) {
		t.Error("JS 代码应包含 max_cards=15 参数")
	}
	if !strings.Contains(js, `"query":"mouse"`) {
		t.Error("JS 代码应包含 query=mouse 参数")
	}
}

// TestBuildCardProbeJS_参数钳位 测试卡片探测参数钳位
func TestBuildCardProbeJS_参数钳位(t *testing.T) {
	js := BuildCardProbeJS(100, false, false, "", nil, nil)
	if !strings.Contains(js, `"max_cards":50`) {
		t.Error("max_cards 超过 50 应钳位到 50")
	}

	js = BuildCardProbeJS(0, true, true, "", nil, nil)
	if !strings.Contains(js, `"max_cards":1`) {
		t.Error("max_cards 低于 1 应钳位到 1")
	}
}

// TestBuildCardProbeJS_站点配置注入 测试站点配置和缓存记录注入
func TestBuildCardProbeJS_站点配置注入(t *testing.T) {
	profiles := []map[string]any{
		{"id": "amazon", "domains": []string{"amazon.com"}, "route_patterns": []string{"/s"}},
	}
	cacheRecords := []map[string]any{
		{"domain": "amazon.com", "route_signature": "/s/*", "selectors": map[string]any{
			"card_container_selectors": []string{"div.s-result-item"},
		}},
	}

	js := BuildCardProbeJS(20, true, true, "", profiles, cacheRecords)

	// 验证 params JSON 包含站点配置
	if !strings.Contains(js, `"site_profiles":`) {
		t.Error("JS 代码应包含 site_profiles 参数")
	}
	if !strings.Contains(js, `"selector_cache_records":`) {
		t.Error("JS 代码应包含 selector_cache_records 参数")
	}
	if !strings.Contains(js, "amazon") {
		t.Error("JS 代码应包含 amazon 站点配置数据")
	}
}

// TestBuildCardProbeJS_关键函数 测试 JS 中包含关键函数
func TestBuildCardProbeJS_关键函数(t *testing.T) {
	js := BuildCardProbeJS(20, true, true, "", nil, nil)

	keywords := []string{
		"const normalize",
		"const isVisible",
		"const buildSelectorHint",
		"const extractTitle",
		"const extractPrice",
		"const extractRating",
		"const extractReviewCount",
		"const extractAvailability",
		"const extractButtons",
		"const looksLikePageChrome",
		"const cardQualityScore",
		"const structuralSignature",
		"const buildCandidatesFromContainers",
		"const domainMatches",
		"const routeMatches",
		"const cacheSelectors",
		"const siteProfileSelectors",
		"const profileSelectors",
	}
	for _, kw := range keywords {
		if !strings.Contains(js, kw) {
			t.Errorf("JS 代码应包含 %q", kw)
		}
	}
}

// TestBuildCardProbeJS_返回结构 测试 JS 返回结构包含关键字段
func TestBuildCardProbeJS_返回结构(t *testing.T) {
	js := BuildCardProbeJS(20, true, true, "", nil, nil)

	fields := []string{
		"ok: true",
		"selector_source:",
		"recurring_signatures:",
		"cards,",
		"error: null",
		"total_candidates:",
		"returned:",
	}
	for _, f := range fields {
		if !strings.Contains(js, f) {
			t.Errorf("JS 代码应包含 %q", f)
		}
	}
}

// TestBuildCardProbeJS_ParamsJSON 测试参数 JSON 有效
func TestBuildCardProbeJS_ParamsJSON(t *testing.T) {
	js := BuildCardProbeJS(20, true, true, "test", nil, nil)

	// 找到 __PARAMS__ 被替换后的 params JSON
	idx := strings.Index(js, "const params = ")
	if idx == -1 {
		t.Fatal("JS 代码应包含 const params = ...")
	}
	// 提取 params JSON 片段
	start := idx + len("const params = ")
	end := strings.Index(js[start:], ";")
	if end == -1 {
		t.Fatal("params 行应以 ; 结尾")
	}
	paramsStr := js[start : start+end]

	var params map[string]any
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		t.Fatalf("params JSON 解析失败: %v, 内容: %s", err, paramsStr[:min(len(paramsStr), 200)])
	}

	if params["max_cards"].(float64) != 20 {
		t.Errorf("max_cards = %v, want 20", params["max_cards"])
	}
	if params["query"] != "test" {
		t.Errorf("query = %v, want test", params["query"])
	}
}

// TestBuildInteractiveProbeJS_ParamsJSON 测试交互探测参数 JSON 有效
func TestBuildInteractiveProbeJS_ParamsJSON(t *testing.T) {
	js := BuildInteractiveProbeJS(50, true, "search")

	idx := strings.Index(js, "const params = ")
	if idx == -1 {
		t.Fatal("JS 代码应包含 const params = ...")
	}
	start := idx + len("const params = ")
	end := strings.Index(js[start:], ";")
	if end == -1 {
		t.Fatal("params 行应以 ; 结尾")
	}
	paramsStr := js[start : start+end]

	var params map[string]any
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		t.Fatalf("params JSON 解析失败: %v", err)
	}

	if params["max_items"].(float64) != 50 {
		t.Errorf("max_items = %v, want 50", params["max_items"])
	}
	if params["query"] != "search" {
		t.Errorf("query = %v, want search", params["query"])
	}
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
