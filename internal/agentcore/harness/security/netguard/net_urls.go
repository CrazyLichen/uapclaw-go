package netguard

import (
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/toolguard"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// netURLsCacheMu net_urls 缓存锁
	netURLsCacheMu sync.RWMutex
	// netURLsCache 上次加载的 map
	netURLsCache map[string]string
	// netURLsCacheKey 缓存对应的 YAML 路径
	netURLsCacheKey string
	// netURLsCacheMtime 缓存对应的 mtime
	netURLsCacheMtime float64
	// netURLsCacheLoaded 是否已加载过
	netURLsCacheLoaded bool

	// netURLsValidActions net_guard 允许的动作
	netURLsValidActions = map[string]struct{}{"allow": {}, "deny": {}}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadPackageNetURLs 返回包内 YAML 的 net_urls（hostname/URL → allow|deny）。
//
// 返回副本，调用方修改不影响缓存。mtime 变化时自动重载。
//
// Python: load_package_net_urls()
func LoadPackageNetURLs() map[string]string {
	yamlPath := resources.ResolveBuiltinRulesYAMLPath()
	if yamlPath == "" {
		parsed, err := resources.ParseBuiltinRules()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("permission.net_urls.builtin_embed_parse_failed")
			return nil
		}
		return compileNetURLs(parsed.NetUrls)
	}

	mtime := resources.GetFileModTime(yamlPath)

	netURLsCacheMu.RLock()
	if netURLsCacheLoaded && netURLsCacheKey == yamlPath && netURLsCacheMtime == mtime {
		cached := copyStringMap(netURLsCache)
		netURLsCacheMu.RUnlock()
		return cached
	}
	netURLsCacheMu.RUnlock()

	netURLsCacheMu.Lock()
	defer netURLsCacheMu.Unlock()
	// 双检
	if netURLsCacheLoaded && netURLsCacheKey == yamlPath && netURLsCacheMtime == mtime {
		return copyStringMap(netURLsCache)
	}

	parsed, err := resources.ParseBuiltinRulesFromFile(yamlPath)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", yamlPath).
			Err(err).
			Msg("permission.net_urls.yaml_read_failed")
		return nil
	}
	out := compileNetURLs(parsed.NetUrls)
	netURLsCache = out
	netURLsCacheKey = yamlPath
	netURLsCacheMtime = mtime
	netURLsCacheLoaded = true
	return copyStringMap(out)
}

// HasPackageNetURLs 判定配置的 urls 是否已含全部包内内置项。
//
// 语义（对齐 Python）：packaged 的**所有键**都在 urls 里才算「已有」；
// packaged 为空返回 false。
//
// Python: _has_package_net_urls(urls)
func HasPackageNetURLs(urls any) bool {
	m, ok := urls.(map[string]any)
	if !ok {
		return false
	}
	packaged := LoadPackageNetURLs()
	if len(packaged) == 0 {
		return false
	}
	for pattern := range packaged {
		if _, has := m[pattern]; !has {
			return false
		}
	}
	return true
}

// MergePackageNetURLs 把包内 net_urls 并入配置的 net_guard.urls。
//
// 合并语义（对齐 Python merge_package_net_urls）：
//   - overlay=allow 且 floor=deny → 用 deny（内置 floor 不可放宽）
//   - overlay 缺失 → 用 floor
//   - 其他 → 取更严（deny 优先）
//
// Python: merge_package_net_urls(permissions, *, inject=None)
func MergePackageNetURLs(permissions map[string]any, inject *bool) map[string]any {
	cfg := deepCopyPermissions(permissions)
	if !toolguard.PackageBuiltinRulesEnabled(cfg) {
		return cfg
	}
	ng, _ := cfg["net_guard"].(map[string]any)
	ngEnabled := ng != nil && truthyAny(ng["enabled"])

	var shouldInject bool
	if inject != nil {
		shouldInject = *inject
	} else {
		shouldInject = ngEnabled
	}
	if !shouldInject {
		return cfg
	}
	if ng == nil {
		ng = map[string]any{"enabled": true, "defaults": "allow", "urls": map[string]any{}}
		cfg["net_guard"] = ng
	}
	urlsMap, _ := ng["urls"].(map[string]any)
	merged := make(map[string]string, len(urlsMap)+4)
	for pattern, value := range urlsMap {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		action := parseAction(value)
		if action == "" {
			continue
		}
		merged[strings.TrimSpace(pattern)] = action
	}
	for pattern, floor := range LoadPackageNetURLs() {
		overlay, hasOverlay := merged[pattern]
		switch {
		case overlay == "allow" && floor == "deny":
			merged[pattern] = "deny"
		case !hasOverlay:
			merged[pattern] = floor
		default:
			merged[pattern] = stricterNetAction(overlay, floor)
		}
	}
	// 写回为 map[string]any（与 YAML 反序列化形态一致）
	out := make(map[string]any, len(merged))
	for k, v := range merged {
		out[k] = v
	}
	ng["urls"] = out
	return cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseNetURLAction 解析 net_urls 条目的动作（支持 dict 包装）。
//
// Python: net_urls.py 的 _parse_action
func parseNetURLAction(value any) string {
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
	if _, valid := netURLsValidActions[action]; !valid {
		return ""
	}
	return action
}

// compileNetURLs 把 YAML 的 net_urls 段编译为 pattern → action。
func compileNetURLs(raw map[string]any) map[string]string {
	out := make(map[string]string, len(raw))
	for pattern, value := range raw {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		action := parseNetURLAction(value)
		if action == "" {
			logger.Warn(logComponent).
				Str("pattern", pattern).
				Msg("permission.net_urls.skip reason=invalid_action")
			continue
		}
		out[strings.TrimSpace(pattern)] = action
	}
	return out
}

// stricterNetAction 取更严的动作（deny 优先）。
//
// Python: _stricter(left, right)
func stricterNetAction(left, right string) string {
	if left == "deny" || right == "deny" {
		return "deny"
	}
	return "allow"
}

// copyStringMap 复制字符串 map。
func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// deepCopyPermissions 深拷贝权限配置（net_guard.urls 逐项复制）。
func deepCopyPermissions(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		if k != "net_guard" {
			out[k] = v
			continue
		}
		ng, ok := v.(map[string]any)
		if !ok {
			out[k] = v
			continue
		}
		copied := make(map[string]any, len(ng))
		for nk, nv := range ng {
			if nk != "urls" {
				copied[nk] = nv
				continue
			}
			urls, ok := nv.(map[string]any)
			if !ok {
				copied[nk] = nv
				continue
			}
			urlsCopy := make(map[string]any, len(urls))
			for uk, uv := range urls {
				urlsCopy[uk] = uv
			}
			copied[nk] = urlsCopy
		}
		out[k] = copied
	}
	return out
}
