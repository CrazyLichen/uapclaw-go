package netguard

import (
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NetGuardChecker fetch 工具的网络 deny/allow 判定。
//
// ALLOW 不抬升 Pipeline A（返回 nil）；只有 deny 才产生结果。
//
// Python: class NetGuardChecker
type NetGuardChecker struct {
	// enabled 是否启用
	enabled bool
	// defaults 无规则命中时的默认动作（"allow" 或 "deny"）
	defaults string
	// urls 编译后的 pattern → action
	urls map[string]string
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fetchTools 受网络防护管辖的工具
	//
	// Python: _FETCH_TOOLS
	fetchTools = map[string]struct{}{
		"mcp_fetch_webpage": {}, "fetch_webpage": {}, "web_fetch_webpage": {},
	}

	// urlArgKeys URL 参数键，顺序即查找优先级
	//
	// Python: _URL_ARG_KEYS
	urlArgKeys = []string{"url", "uri", "href", "webpage", "page_url", "target_url"}

	// validNetActions net_guard 允许的动作（没有 ask）
	//
	// Python: _VALID_ACTIONS
	validNetActions = map[string]struct{}{"allow": {}, "deny": {}}

	// urlWildcardChars URL glob 的字符类。URL 可能含查询串/片段里的字符，
	// 命令通配符的那套字符类会拒绝它们。
	//
	// Python: _URL_WILDCARD_CHARS
	urlWildcardChars = `[-a-zA-Z0-9._/:?#&=%+~@,;\[\]']`

	// urlGlobEscapeChars URL glob 里需要转义的字符
	urlGlobEscapeChars = ".+^${}()|[]\\"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractFetchURL 从工具参数里取抓取 URL（按 urlArgKeys 顺序取第一个非空）。
//
// 反斜杠归一化为 /。无可用值时返回空串。
//
// Python: extract_fetch_url(tool_args)
func ExtractFetchURL(toolArgs map[string]any) string {
	if toolArgs == nil {
		return ""
	}
	for _, key := range urlArgKeys {
		raw, ok := toolArgs[key].(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		return strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
	}
	return ""
}

// MatchNetPattern 判定 URL 是否命中 net_guard 的一条 pattern。
//
// 两种形态（对齐 Python match_net_pattern）：
//  1. pattern 含 "://" → URL 形态。含通配符走 glob；否则要求前缀匹配且 netloc 相同
//  2. 否则视为 hostname → 用命令通配符匹配 host
//
// Python: match_net_pattern(pattern, url)
func MatchNetPattern(pattern, url string) bool {
	pat := strings.ReplaceAll(strings.TrimSpace(pattern), `\`, "/")
	normalized := strings.ReplaceAll(strings.TrimSpace(url), `\`, "/")
	if pat == "" || normalized == "" {
		return false
	}
	if strings.Contains(pat, "://") {
		if strings.Contains(pat, "*") || strings.Contains(pat, "?") {
			return matchURLGlob(normalized, pat)
		}
		if !strings.HasPrefix(normalized, pat) {
			return false
		}
		return urlNetloc(normalized) == urlNetloc(pat)
	}
	host := urlHostname(normalized)
	if host == "" {
		return false
	}
	return security.MatchWildcard(strings.ToLower(pat), host)
}

// BuildNetGuardChecker 编译 net_guard 段；未启用或缺失时返回 nil。
//
// Python: build_net_guard_checker(permissions)
func BuildNetGuardChecker(permissions map[string]any) *NetGuardChecker {
	perms := permissions
	if perms == nil {
		perms = map[string]any{}
	}
	section, ok := perms["net_guard"].(map[string]any)
	if !ok {
		return nil
	}
	if !truthyAny(section["enabled"]) {
		return nil
	}
	return &NetGuardChecker{
		enabled:  true,
		defaults: parseAction(section["defaults"]),
		urls:     compileURLRules(section["urls"]),
	}
}

// Enabled 是否启用（实现 security.NetGuardEvaluator 之外的便捷访问器）。
//
// Python: NetGuardChecker.enabled（property）
func (c *NetGuardChecker) Enabled() bool { return c.enabled }

// Evaluate 评估网络层；只有 deny 才返回结果，allow 与无命中都返回 nil。
//
// Python: NetGuardChecker.evaluate(tool_name, tool_args)
func (c *NetGuardChecker) Evaluate(toolName string, toolArgs map[string]any) *security.PermissionResult {
	if !c.enabled {
		return nil
	}
	if _, hit := fetchTools[strings.TrimSpace(toolName)]; !hit {
		return nil
	}
	url := ExtractFetchURL(toolArgs)

	type urlHit struct {
		pattern string
		action  string
	}
	hits := make([]urlHit, 0, 4)
	if url != "" {
		for pattern, action := range c.urls {
			if MatchNetPattern(pattern, url) {
				hits = append(hits, urlHit{pattern: pattern, action: action})
			}
		}
	}
	if len(hits) > 0 {
		for _, h := range hits {
			if h.action == "deny" {
				return &security.PermissionResult{
					Permission:  security.PermissionLevelDeny,
					MatchedRule: "net_guard:url:" + h.pattern,
					Reason:      "net_guard denied: " + h.pattern,
				}
			}
		}
		// 只有 allow 命中 → 不抬升
		return nil
	}
	if c.defaults == "deny" {
		return &security.PermissionResult{
			Permission:  security.PermissionLevelDeny,
			MatchedRule: "net_guard:defaults",
			Reason:      "net_guard denied: defaults",
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseAction 解析动作值，支持 dict 包装（{"action":...} 或 {"fetch":...}）。
//
// 只在 allow/deny 范围内返回动作，其余返回空串。
//
// Python: _parse_action(value)
func parseAction(value any) string {
	raw := value
	if m, ok := value.(map[string]any); ok {
		if v, has := m["action"]; has && v != nil {
			raw = v
		} else {
			raw = m["fetch"]
		}
	}
	s, ok := raw.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return ""
	}
	action := strings.ToLower(strings.TrimSpace(s))
	if _, valid := validNetActions[action]; !valid {
		return ""
	}
	return action
}

// compileURLRules 把 urls 段的原始 map 编译为 pattern → action。
func compileURLRules(raw any) map[string]string {
	urls, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(urls))
	for pattern, value := range urls {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		action := parseAction(value)
		if action == "" {
			continue
		}
		out[strings.TrimSpace(pattern)] = action
	}
	return out
}

// matchURLGlob URL 形态的 glob 匹配。
//
// 与命令通配符的差别：? 与 * 用 urlWildcardChars（含 URL 合法字符），
// 以支持查询串与片段。
//
// Python: _match_url_glob(value, pattern)
func matchURLGlob(value, pattern string) bool {
	if value == "" || pattern == "" {
		return false
	}
	val := strings.ReplaceAll(value, `\`, "/")
	pat := strings.ReplaceAll(pattern, `\`, "/")

	var escaped strings.Builder
	escaped.Grow(len(pat) * 2)
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		if strings.ContainsRune(urlGlobEscapeChars, rune(c)) {
			escaped.WriteByte('\\')
		}
		escaped.WriteByte(c)
	}
	expr := escaped.String()
	// 顺序敏感：先 ? 后 *（与 Python 一致）
	expr = strings.ReplaceAll(expr, "?", urlWildcardChars)
	expr = strings.ReplaceAll(expr, "*", urlWildcardChars+"*")

	re, err := regexp.Compile("^(?:" + expr + ")$")
	if err != nil {
		return false
	}
	return re.MatchString(val)
}

// urlHostname 从 URL 取 hostname（小写、去尾部点）。失败返回空串。
//
// 无 scheme 时按 http:// 处理。
//
// Python: _hostname(url)
func urlHostname(url string) string {
	text := strings.TrimSpace(url)
	if text == "" {
		return ""
	}
	candidate := text
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	rest := candidate[strings.Index(candidate, "://")+3:]
	// 取 authority 段（到第一个 / ? # 为止）
	if idx := strings.IndexAny(rest, "/?#"); idx >= 0 {
		rest = rest[:idx]
	}
	// 去掉 userinfo
	if idx := strings.LastIndex(rest, "@"); idx >= 0 {
		rest = rest[idx+1:]
	}
	// 去掉端口（IPv6 的 [::1]:80 需特殊处理）
	host := rest
	if strings.HasPrefix(rest, "[") {
		if end := strings.Index(rest, "]"); end >= 0 {
			host = rest[1:end]
		}
	} else if idx := strings.LastIndex(rest, ":"); idx >= 0 {
		host = rest[:idx]
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host
}

// urlNetloc 取 URL 的 netloc（authority 段，含端口）。
//
// Python: urlparse(normalized).netloc
func urlNetloc(url string) string {
	text := strings.TrimSpace(url)
	idx := strings.Index(text, "://")
	if idx < 0 {
		return ""
	}
	rest := text[idx+3:]
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		rest = rest[:cut]
	}
	return rest
}

// truthyAny Python 的 bool() 语义。
func truthyAny(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// init 注册网络防护工厂，让 security 包能构造本包判定器。
//
// 同时注册 net_urls 注入与存在性判定（供 security.PreparePermissionsForEngine）。
func init() {
	security.RegisterNetGuardFactory(func(permissions map[string]any) security.NetGuardEvaluator {
		checker := BuildNetGuardChecker(permissions)
		if checker == nil {
			return nil
		}
		return checker
	})
	security.RegisterPermissionBridges(nil, MergePackageNetURLs, HasPackageNetURLs)
}

// 编译期断言：*NetGuardChecker 满足 security.NetGuardEvaluator
var _ security.NetGuardEvaluator = (*NetGuardChecker)(nil)
