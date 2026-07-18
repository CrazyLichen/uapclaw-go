package browser_move

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SelectorCacheRecord 选择器缓存记录。
//
// 对齐 Python: BrowserSelectorCache._load() 返回的 records 中每一条记录
type SelectorCacheRecord struct {
	// Domain 域名
	Domain string `json:"domain"`
	// RouteSignature 路由签名
	RouteSignature string `json:"route_signature"`
	// Kind 记录类型
	Kind string `json:"kind"`
	// Selectors 选择器映射
	Selectors map[string][]string `json:"selectors"`
	// SuccessCount 成功次数
	SuccessCount int `json:"success_count"`
	// FailureCount 失败次数
	FailureCount int `json:"failure_count"`
	// LastSuccessAt 最近成功时间
	LastSuccessAt float64 `json:"last_success_at"`
	// QualityScore 质量评分
	QualityScore float64 `json:"quality_score"`
	// SampleCardCount 样本卡片数量
	SampleCardCount int `json:"sample_card_count"`
}

// BrowserSelectorCache 小型 JSON 选择器缓存，用于重复的浏览器探测发现结果。
//
// 对齐 Python: BrowserSelectorCache (site_profiles.py L237-435)
type BrowserSelectorCache struct {
	// path 缓存文件路径
	path string
	// mu 读写锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// selectorCacheVersion 缓存文件版本
	selectorCacheVersion = 1

	// selectorCacheMaxRecords 缓存最大记录数
	selectorCacheMaxRecords = 200

	// selectorCacheExportMaxRecords 导出最大记录数
	selectorCacheExportMaxRecords = 100

	// selectorUniqueLimit 选择器去重上限
	selectorUniqueLimit = 20

	// generalizeSelectorLimit 泛化选择器上限
	generalizeSelectorLimit = 4
)

// ──────────────────────────── 全局变量 ────────────────────────────

// builtinSiteProfiles 内置浏览器站点配置文件。
//
// 对齐 Python: BUILTIN_SITE_PROFILES (site_profiles.py L16-57)
var builtinSiteProfiles = []map[string]any{
	{
		"id":      "books_to_scrape",
		"domains": []string{"books.toscrape.com"},
		"route_patterns": []string{
			"^/$",
			"^/catalogue/",
		},
		"card_container_selectors": []string{
			"article.product_pod",
			"ol.row > li > article.product_pod",
		},
		"title_selectors": []string{
			"h3 a[title]",
			"h3 a",
			"a[title]",
			"img[alt]",
		},
		"price_selectors": []string{
			".price_color",
			"[class*='price' i]",
		},
		"rating_selectors": []string{
			"p.star-rating",
			"[class*='star-rating' i]",
			"[class*='rating' i]",
		},
		"availability_selectors": []string{
			".availability",
			"[class*='availability' i]",
			"[class*='stock' i]",
		},
		"primary_link_selectors": []string{
			"h3 a[href]",
			"a[href][title]",
			"a[href]",
		},
		"button_selectors": []string{
			"form button",
			"button",
			"[role='button']",
			"input[type='submit']",
		},
	},
}

// chromeSelectorFragments 页面 chrome 选择器片段。
// 对齐 Python: _CHROME_SELECTOR_FRAGMENTS (site_profiles.py L60-69)
var chromeSelectorFragments = []string{
	"#nav",
	"nav-",
	"navbar",
	"breadcrumb",
	"header",
	"footer",
	"menu",
	"sidebar",
}

// chromeTitles 页面 chrome 标题集合。
// 对齐 Python: _CHROME_TITLES (site_profiles.py L71-81)
var chromeTitles = map[string]bool{
	"fresh & fast":     true,
	"sell":             true,
	"best sellers":     true,
	"customer service": true,
	"today's deals":    true,
	"new releases":     true,
	"help":             true,
	"login":            true,
	"sign in":          true,
}

// selectorCacheSingleton 全局选择器缓存单例
// 对齐 Python: _SELECTOR_CACHE (site_profiles.py L438)
var selectorCacheSingleton *BrowserSelectorCache

// selectorCacheOnce 单例初始化控制
var selectorCacheOnce sync.Once

// nthOfTypRe 匹配 :nth-of-type(N) 的正则表达式
var nthOfTypRe = regexp.MustCompile(`:nth-of-type\(\d+\)`)

// digitRe 匹配数字的正则表达式
var digitRe = regexp.MustCompile(`\d+`)

// multiSlashRe 匹配多个连续斜杠的正则表达式
var multiSlashRe = regexp.MustCompile(`/+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuiltinSiteProfiles 返回内置浏览器站点配置文件的深拷贝。
//
// 对齐 Python: builtin_site_profiles() (site_profiles.py L84-86)
func BuiltinSiteProfiles() []map[string]any {
	return deepCopyProfiles(builtinSiteProfiles)
}

// NormalizeRouteSignature 返回用于选择器缓存键的粗粒度路由签名。
//
// 对齐 Python: normalize_route_signature() (site_profiles.py L89-101)
func NormalizeRouteSignature(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		parsed = &url.URL{}
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	// 避免对特定项目 ID 过窄地缓存选择器
	path = digitRe.ReplaceAllString(path, "*")
	path = multiSlashRe.ReplaceAllString(path, "/")

	if path != "/" && strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}

	if path == "" {
		path = "/"
	}
	return path
}

// DomainFromURL 从 URL 中提取域名。
//
// 对齐 Python: domain_from_url() (site_profiles.py L104-106)
func DomainFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return ""
	}
	return parsed.Hostname()
}

// GetSelectorCache 返回全局选择器缓存单例。
//
// 对齐 Python: get_selector_cache() (site_profiles.py L441-445)
func GetSelectorCache() *BrowserSelectorCache {
	selectorCacheOnce.Do(func() {
		selectorCacheSingleton = NewBrowserSelectorCache("")
	})
	return selectorCacheSingleton
}

// NewBrowserSelectorCache 创建新的选择器缓存实例。
//
// 对齐 Python: BrowserSelectorCache.__init__
func NewBrowserSelectorCache(path string) *BrowserSelectorCache {
	if path == "" {
		path = defaultCachePath()
	}
	return &BrowserSelectorCache{path: path}
}

// ExportForProbe 导出可用于嵌入探测 JS 的紧凑缓存记录。
//
// 对齐 Python: BrowserSelectorCache.export_for_probe (site_profiles.py L272-290)
func (c *BrowserSelectorCache) ExportForProbe(maxRecords ...int) []map[string]any {
	limit := selectorCacheExportMaxRecords
	if len(maxRecords) > 0 && maxRecords[0] > 0 {
		limit = maxRecords[0]
	}

	data := c.load()
	records, ok := data["records"].([]any)
	if !ok {
		return nil
	}

	// 排序：quality_score 降序 → success_count 降序 → failure_count 升序 → last_success_at 降序
	sort.SliceStable(records, func(i, j int) bool {
		ri, _ := records[i].(map[string]any)
		rj, _ := records[j].(map[string]any)
		qi := toFloat64Or(ri["quality_score"], 0)
		qj := toFloat64Or(rj["quality_score"], 0)
		if qi != qj {
			return qi > qj
		}
		si := toIntOrZero(ri["success_count"])
		sj := toIntOrZero(rj["success_count"])
		if si != sj {
			return si > sj
		}
		fi := toIntOrZero(ri["failure_count"])
		fj := toIntOrZero(rj["failure_count"])
		if fi != fj {
			return fi < fj
		}
		li := toFloat64Or(ri["last_success_at"], 0)
		lj := toFloat64Or(rj["last_success_at"], 0)
		return li > lj
	})

	if limit > len(records) {
		limit = len(records)
	}
	result := make([]map[string]any, 0, limit)
	for i := 0; i < limit; i++ {
		if m, ok := records[i].(map[string]any); ok {
			result = append(result, deepCopyMap(m))
		}
	}
	return result
}

// RecordCardProbeResult 从成功的卡片探测结果中记录可复用的选择器。
//
// 对齐 Python: BrowserSelectorCache.record_card_probe_result (site_profiles.py L292-435)
func (c *BrowserSelectorCache) RecordCardProbeResult(result map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if result == nil || !isTruthy(result["ok"]) {
		return
	}

	rawURL := strings.TrimSpace(fmt.Sprintf("%v", result["url"]))
	domain := DomainFromURL(rawURL)
	if domain == "" {
		return
	}

	routeSignature := NormalizeRouteSignature(rawURL)
	cards, _ := result["cards"].([]any)
	if len(cards) == 0 {
		return
	}

	// 过滤可缓存的卡片
	var cacheableCards []map[string]any
	for _, card := range cards {
		if m, ok := card.(map[string]any); ok && isCacheableCard(m) {
			cacheableCards = append(cacheableCards, m)
		}
	}
	if len(cacheableCards) < 2 {
		return
	}
	cards = nil // 仅使用 cacheableCards

	selectors := map[string][]string{
		"card_container_selectors": {},
		"title_selectors":          {},
		"price_selectors":          {},
		"rating_selectors":         {},
		"availability_selectors":   {},
		"primary_link_selectors":   {},
		"button_selectors":         {},
	}

	limit := len(cacheableCards)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		card := cacheableCards[i]
		selectors["card_container_selectors"] = append(
			selectors["card_container_selectors"],
			generalizeSelector(fmt.Sprintf("%v", card["selector_hint"]))...,
		)
		selectors["title_selectors"] = append(
			selectors["title_selectors"],
			generalizeSelector(fmt.Sprintf("%v", card["title_selector_hint"]))...,
		)
		selectors["price_selectors"] = append(
			selectors["price_selectors"],
			generalizeSelector(fmt.Sprintf("%v", card["price_selector_hint"]))...,
		)
		selectors["rating_selectors"] = append(
			selectors["rating_selectors"],
			generalizeSelector(fmt.Sprintf("%v", card["rating_selector_hint"]))...,
		)
		selectors["availability_selectors"] = append(
			selectors["availability_selectors"],
			generalizeSelector(fmt.Sprintf("%v", card["availability_selector_hint"]))...,
		)
		selectors["primary_link_selectors"] = append(
			selectors["primary_link_selectors"],
			generalizeSelector(fmt.Sprintf("%v", card["primary_link_selector_hint"]))...,
		)

		buttons, _ := card["buttons"].([]any)
		btnLimit := len(buttons)
		if btnLimit > 4 {
			btnLimit = 4
		}
		for j := 0; j < btnLimit; j++ {
			if btn, ok := buttons[j].(map[string]any); ok {
				selectors["button_selectors"] = append(
					selectors["button_selectors"],
					generalizeSelector(fmt.Sprintf("%v", btn["selector_hint"]))...,
				)
			}
		}
	}

	// 去重并过滤空值
	filteredSelectors := make(map[string][]string)
	for key, values := range selectors {
		uniqueValues := unique(values, selectorUniqueLimit)
		if len(uniqueValues) > 0 {
			filteredSelectors[key] = uniqueValues
		}
	}
	selectors = filteredSelectors

	if len(selectors) == 0 {
		return
	}

	// 计算质量评分
	var totalScore int
	for _, card := range cacheableCards {
		totalScore += cardQualityScore(card)
	}
	qualityScore := math.Min(1.0, float64(totalScore)/math.Max(1, float64(len(cacheableCards)))/100.0)

	// 加载并更新缓存
	data := c.load()
	records, ok := data["records"].([]any)
	if !ok {
		records = []any{}
	}

	// 查找已有记录
	var existing map[string]any
	existingIdx := -1
	for i, rec := range records {
		if m, ok := rec.(map[string]any); ok {
			if fmt.Sprintf("%v", m["domain"]) == domain &&
				fmt.Sprintf("%v", m["route_signature"]) == routeSignature &&
				fmt.Sprintf("%v", m["kind"]) == "card_probe" {
				existing = m
				existingIdx = i
				break
			}
		}
	}

	now := float64(time.Now().Unix())

	if existing == nil {
		newRecord := map[string]any{
			"domain":            domain,
			"route_signature":   routeSignature,
			"kind":              "card_probe",
			"selectors":         selectors,
			"success_count":     1,
			"failure_count":     0,
			"last_success_at":   now,
			"quality_score":     qualityScore,
			"sample_card_count": len(cacheableCards),
		}
		records = append(records, newRecord)
	} else {
		// 合并选择器
		oldSelectors, _ := existing["selectors"].(map[string]any)
		if oldSelectors == nil {
			oldSelectors = map[string]any{}
		}
		for name, values := range selectors {
			var oldValues []string
			if ov, ok := oldSelectors[name]; ok {
				switch v := ov.(type) {
				case []string:
					oldValues = v
				case []any:
					for _, item := range v {
						oldValues = append(oldValues, fmt.Sprintf("%v", item))
					}
				}
			}
			merged := append(oldValues, values...)
			oldSelectors[name] = unique(merged, selectorUniqueLimit)
		}
		existing["selectors"] = oldSelectors

		oldQuality := toFloat64Or(existing["quality_score"], 0)
		existing["success_count"] = toIntOrZero(existing["success_count"]) + 1
		existing["last_success_at"] = now
		existing["quality_score"] = math.Max(oldQuality, qualityScore)
		existing["sample_card_count"] = max(toIntOrZero(existing["sample_card_count"]), len(cacheableCards))

		if existingIdx >= 0 {
			records[existingIdx] = existing
		}
	}

	// 保持文件精简，最多保留 200 条记录
	if len(records) > selectorCacheMaxRecords {
		records = records[len(records)-selectorCacheMaxRecords:]
	}
	data["records"] = records
	c.save(data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCachePath 返回默认缓存文件路径。
// 对齐 Python: _default_cache_path (site_profiles.py L109-114)
func defaultCachePath() string {
	raw := strings.TrimSpace(os.Getenv("OPENJIUWEN_BROWSER_SELECTOR_CACHE"))
	if raw != "" {
		expanded := os.ExpandEnv(raw)
		return expanded
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".openjiuwen", "browser_selector_cache.json")
}

// unique 字符串列表去重，限制数量。
// 对齐 Python: _unique (site_profiles.py L117-128)
func unique(items []string, limit int) []string {
	var result []string
	seen := make(map[string]bool)
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// selectorIsTooBroad 检查选择器是否过于宽泛。
// 对齐 Python: _selector_is_too_broad (site_profiles.py L131-142)
func selectorIsTooBroad(selector string) bool {
	value := strings.TrimSpace(strings.ToLower(selector))
	if value == "" {
		return true
	}
	tooBroad := map[string]bool{
		"div": true, "li": true, "section": true, "article": true,
		"a": true, "span": true, "button": true,
	}
	if tooBroad[value] {
		return true
	}
	for _, fragment := range chromeSelectorFragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

// looksLikePageChrome 检测导航/chrome 卡片。
// 对齐 Python: _looks_like_page_chrome (site_profiles.py L145-156)
func looksLikePageChrome(card map[string]any) bool {
	selector := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", card["selector_hint"])))
	title := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", card["title"])))
	preview := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", card["text_preview"])))

	for _, fragment := range chromeSelectorFragments {
		if strings.Contains(selector, fragment) {
			return true
		}
	}

	if chromeTitles[title] || chromeTitles[preview] {
		return true
	}

	return false
}

// cardQualityScore 计算卡片的质量评分。
// 对齐 Python: _card_quality_score (site_profiles.py L159-188)
func cardQualityScore(card map[string]any) int {
	if looksLikePageChrome(card) {
		return 0
	}

	score := 0

	title := strings.TrimSpace(fmt.Sprintf("%v", card["title"]))
	preview := strings.TrimSpace(fmt.Sprintf("%v", card["text_preview"]))
	buttons, _ := card["buttons"].([]any)

	if len(title) >= 8 {
		score += 20
	}
	if len(preview) >= 60 {
		score += 15
	}
	if isTruthy(card["primary_link"]) {
		score += 12
	}
	if isTruthy(card["price"]) {
		score += 18
	}
	if isTruthy(card["rating"]) {
		score += 14
	}
	if isTruthy(card["review_count"]) {
		score += 10
	}
	if isTruthy(card["availability"]) {
		score += 8
	}
	if isTruthy(card["has_image"]) {
		score += 12
	}
	if len(buttons) > 0 {
		score += 8
	}

	return score
}

// isCacheableCard 判断卡片是否值得缓存。
// 对齐 Python: _is_cacheable_card (site_profiles.py L191-203)
func isCacheableCard(card map[string]any) bool {
	score := cardQualityScore(card)
	if score >= 42 {
		return true
	}

	preview := strings.TrimSpace(fmt.Sprintf("%v", card["text_preview"]))
	buttons, _ := card["buttons"].([]any)

	return score >= 30 &&
		len(preview) >= 80 &&
		(isTruthy(card["primary_link"]) || len(buttons) > 0)
}

// generalizeSelector 从 selector_hint 创建可复用的选择器变体。
// 对齐 Python: _generalize_selector (site_profiles.py L206-234)
func generalizeSelector(selector string) []string {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil
	}

	noNth := nthOfTypRe.ReplaceAllString(selector, "")

	// 按 > 分割
	parts := strings.Split(noNth, ">")
	var trimmedParts []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			trimmedParts = append(trimmedParts, p)
		}
	}

	var candidates []string

	if len(trimmedParts) > 0 {
		candidates = append(candidates, trimmedParts[len(trimmedParts)-1])
	}
	if len(trimmedParts) >= 2 {
		candidates = append(candidates, strings.Join(trimmedParts[len(trimmedParts)-2:], " > "))
	}
	if len(trimmedParts) >= 3 {
		candidates = append(candidates, strings.Join(trimmedParts[len(trimmedParts)-3:], " > "))
	}

	// 保留完整 non-nth 选择器作为回退，但不是首选
	if noNth != "" {
		found := false
		for _, c := range candidates {
			if c == noNth {
				found = true
				break
			}
		}
		if !found {
			candidates = append(candidates, noNth)
		}
	}

	uniqueCandidates := unique(candidates, generalizeSelectorLimit)
	var result []string
	for _, item := range uniqueCandidates {
		if !selectorIsTooBroad(item) {
			result = append(result, item)
		}
	}
	return result
}

// load 加载缓存数据。
// 对齐 Python: BrowserSelectorCache._load (site_profiles.py L243-261)
func (c *BrowserSelectorCache) load() map[string]any {
	data := map[string]any{
		"version": selectorCacheVersion,
		"records": []any{},
	}

	raw, err := os.ReadFile(c.path)
	if err != nil {
		return data
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return data
	}

	if _, ok := parsed["version"]; !ok {
		parsed["version"] = selectorCacheVersion
	}
	if _, ok := parsed["records"]; !ok {
		parsed["records"] = []any{}
	}
	if _, ok := parsed["records"].([]any); !ok {
		parsed["records"] = []any{}
	}

	return parsed
}

// save 保存缓存数据。
// 对齐 Python: BrowserSelectorCache._save (site_profiles.py L263-270)
func (c *BrowserSelectorCache) save(data map[string]any) {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn(logComponentBR).
			Str("event_type", "selector_cache_save_error").
			Str("path", c.path).
			Err(err).
			Msg("创建缓存目录失败")
		return
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		logger.Warn(logComponentBR).
			Str("event_type", "selector_cache_save_error").
			Err(err).
			Msg("序列化缓存数据失败")
		return
	}

	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o644); err != nil {
		logger.Warn(logComponentBR).
			Str("event_type", "selector_cache_save_error").
			Str("path", tmpPath).
			Err(err).
			Msg("写入临时缓存文件失败")
		return
	}

	if err := os.Rename(tmpPath, c.path); err != nil {
		logger.Warn(logComponentBR).
			Str("event_type", "selector_cache_save_error").
			Err(err).
			Msg("重命名缓存文件失败")
	}
}

// toFloat64Or 将 any 转为 float64，失败返回默认值。
func toFloat64Or(v any, defaultVal float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s == "" || s == "<nil>" {
			return defaultVal
		}
		if f, err := parseFloat(s); err == nil {
			return f
		}
		return defaultVal
	}
}

// toIntOrZero 将 any 转为 int，失败返回 0。
func toIntOrZero(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		s := strings.TrimSpace(n)
		if s == "" || s == "<nil>" {
			return 0
		}
		if i, err := strconv.Atoi(s); err == nil {
			return i
		}
		if f, err := parseFloat(s); err == nil {
			return int(f)
		}
		return 0
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s == "" || s == "<nil>" {
			return 0
		}
		if i, err := strconv.Atoi(s); err == nil {
			return i
		}
		if f, err := parseFloat(s); err == nil {
			return int(f)
		}
		return 0
	}
}

// parseFloat 解析浮点数字符串。
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// deepCopyProfiles 深拷贝站点配置文件。
func deepCopyProfiles(profiles []map[string]any) []map[string]any {
	data, _ := json.Marshal(profiles)
	var result []map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}

// deepCopyMap 深拷贝 map。
func deepCopyMap(m map[string]any) map[string]any {
	data, _ := json.Marshal(m)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}
