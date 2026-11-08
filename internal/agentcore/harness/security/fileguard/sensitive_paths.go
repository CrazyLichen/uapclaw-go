package fileguard

import (
	"os"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/toolguard"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// homePrefix home 目录前缀
	homePrefix = "~/"
	// layerBuiltin 内置层标记
	layerBuiltin = "builtin"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sensitivePathsCacheMu 敏感路径缓存锁
	sensitivePathsCacheMu sync.RWMutex
	// sensitivePathsCache 上次加载的条目
	sensitivePathsCache []map[string]any
	// sensitivePathsCacheKey 缓存对应的 YAML 路径
	sensitivePathsCacheKey string
	// sensitivePathsCacheMtime 缓存对应的 mtime
	sensitivePathsCacheMtime float64
	// sensitivePathsCacheLoaded 是否已加载过
	sensitivePathsCacheLoaded bool

	// validSensitiveActions 敏感路径允许的 action 取值
	validSensitiveActions = map[string]struct{}{"ask": {}, "deny": {}, "allow": {}}

	// axisRank 三轴严格度排序（数字越小越严）
	axisRank = map[string]int{"deny": 0, "ask": 1, "allow": 2}

	// sensitiveAxes 三轴名称（顺序即合并时的迭代序）
	sensitiveAxes = []string{"read", "write", "exec"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadPackageSensitivePaths 返回包内 YAML 的敏感路径条目。
//
// 每条固定补 read / write / exec 三轴（都等于 action）、layer=builtin、
// match（默认 glob）；path 里的 ~ 展开为绝对路径。返回副本，调用方修改
// 不影响缓存。mtime 变化时自动重载。
//
// Python: load_package_sensitive_paths()
func LoadPackageSensitivePaths() []map[string]any {
	yamlPath := resources.ResolveBuiltinRulesYAMLPath()
	if yamlPath == "" {
		// 嵌入资源回退
		parsed, err := resources.ParseBuiltinRules()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("permission.sensitive_paths.builtin_embed_parse_failed")
			return nil
		}
		return buildSensitiveEntries(parsed.SensitivePaths)
	}

	mtime := resources.GetFileModTime(yamlPath)
	key := yamlPath

	sensitivePathsCacheMu.RLock()
	if sensitivePathsCacheLoaded && sensitivePathsCacheKey == key && sensitivePathsCacheMtime == mtime {
		cached := copySensitiveEntries(sensitivePathsCache)
		sensitivePathsCacheMu.RUnlock()
		return cached
	}
	sensitivePathsCacheMu.RUnlock()

	sensitivePathsCacheMu.Lock()
	defer sensitivePathsCacheMu.Unlock()
	// 双检
	if sensitivePathsCacheLoaded && sensitivePathsCacheKey == key && sensitivePathsCacheMtime == mtime {
		return copySensitiveEntries(sensitivePathsCache)
	}

	parsed, err := resources.ParseBuiltinRulesFromFile(yamlPath)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", yamlPath).
			Err(err).
			Msg("permission.sensitive_paths.yaml_read_failed")
		return nil
	}
	out := buildSensitiveEntries(parsed.SensitivePaths)
	sensitivePathsCache = out
	sensitivePathsCacheKey = key
	sensitivePathsCacheMtime = mtime
	sensitivePathsCacheLoaded = true
	return copySensitiveEntries(out)
}

// MergePackageSensitivePaths 把包内敏感路径并入配置的 file_guard.paths。
//
// shouldInject 顺序（对齐 Python merge_package_sensitive_paths）：
//  1. package_builtin_rules 关闭 → 原样返回
//  2. inject 显式给出 → 用之
//  3. 否则看 file_guard.enabled
//
// Python: merge_package_sensitive_paths(permissions, *, inject=None)
func MergePackageSensitivePaths(permissions map[string]any, inject *bool) map[string]any {
	cfg := deepCopyPermissions(permissions)
	if !toolguard.PackageBuiltinRulesEnabled(cfg) {
		return cfg
	}
	fg := mapOf(cfg["file_guard"])
	fgEnabled := fg != nil && truthyAny(fg["enabled"])

	var shouldInject bool
	if inject != nil {
		shouldInject = *inject
	} else {
		shouldInject = fgEnabled
	}
	if !shouldInject {
		return cfg
	}
	if fg == nil {
		fg = map[string]any{"enabled": true, "paths": []any{}}
		cfg["file_guard"] = fg
	}
	overlayPaths := pathListOf(fg["paths"])
	fg["paths"] = toAnySliceFromMaps(mergeSensitivePaths(overlayPaths))
	return cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSensitiveEntries 把 YAML 原始条目编译为带三轴与 layer 的条目。
//
// Python: load_package_sensitive_paths 的循环体
func buildSensitiveEntries(raw []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if item == nil {
			continue
		}
		rawPath, ok := item["path"].(string)
		if !ok || strings.TrimSpace(rawPath) == "" {
			continue
		}
		action := yamlSensitiveAction(item)
		if action == "" {
			continue
		}
		entry := shallowCopyMap(item)
		entry["path"] = expandHomePath(rawPath)
		entry["match"] = stringOrDefault(item["match"], "glob")
		entry["read"] = action
		entry["write"] = action
		entry["exec"] = action
		entry["layer"] = layerBuiltin
		out = append(out, entry)
	}
	return out
}

// yamlSensitiveAction 读取并校验 action 字段。
//
// 缺失/非法/allow 都返回空串并打 WARN（内置敏感路径不允许放宽）。
//
// Python: _yaml_action(item)
func yamlSensitiveAction(item map[string]any) string {
	raw, ok := item["action"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Msg("permission.sensitive_paths.missing_action")
		return ""
	}
	action := strings.ToLower(strings.TrimSpace(raw))
	if _, valid := validSensitiveActions[action]; !valid {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Str("action", action).
			Msg("permission.sensitive_paths.invalid_action")
		return ""
	}
	if action == "allow" {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Msg("permission.sensitive_paths.skip reason=allow_not_allowed")
		return ""
	}
	return action
}

// expandHomePath 把 ~/ 或 ~ 展开为当前用户的 home 目录。
//
// Python: _expand_home_path(raw)
func expandHomePath(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return text
	}
	if !strings.HasPrefix(text, homePrefix) && text != "~" {
		return text
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return text
	}
	home = strings.TrimSuffix(filepathToSlash(home), "/")
	rest := strings.TrimLeft(text[1:], "/")
	if rest == "" {
		return home
	}
	return home + "/" + rest
}

// stricterAxis 取两轴中更严的一个，未知输入按 ask。
//
// Python: _stricter_axis(left, right)
func stricterAxis(left, right string) string {
	lv, okLeft := axisRank[strings.ToLower(strings.TrimSpace(left))]
	if !okLeft {
		lv = axisRank["ask"]
	}
	rv, okRight := axisRank[strings.ToLower(strings.TrimSpace(right))]
	if !okRight {
		rv = axisRank["ask"]
	}
	merged := lv
	if rv < merged {
		merged = rv
	}
	switch merged {
	case 0:
		return "deny"
	case 1:
		return "ask"
	}
	return "allow"
}

// mergeSensitivePaths 把内置敏感路径并入 overlay 路径列表。
//
// 同 path 时三轴逐个取严（overlay 的 allow 会被 floor 的非 allow 覆盖）；
// 内置独有项追加到末尾。
//
// Python: _merge_sensitive_paths(overlay_paths)
func mergeSensitivePaths(overlayPaths []map[string]any) []map[string]any {
	packaged := LoadPackageSensitivePaths()
	byKey := make(map[string]map[string]any, len(packaged))
	for _, item := range packaged {
		key, _ := item["path"].(string)
		key = strings.ReplaceAll(key, `\`, "/")
		if key != "" {
			byKey[key] = shallowCopyMap(item)
		}
	}

	merged := make([]map[string]any, 0, len(overlayPaths)+len(packaged))
	used := make(map[string]struct{}, len(overlayPaths))

	for _, item := range overlayPaths {
		path, _ := item["path"].(string)
		path = strings.ReplaceAll(path, `\`, "/")
		floor, hit := byKey[path]
		if !hit {
			merged = append(merged, shallowCopyMap(item))
			continue
		}
		used[path] = struct{}{}
		combined := shallowCopyMap(item)
		for _, axis := range sensitiveAxes {
			overlayVal := strings.ToLower(strings.TrimSpace(stringOrDefault(item[axis], "")))
			floorVal := strings.ToLower(strings.TrimSpace(stringOrDefault(floor[axis], "ask")))
			switch {
			case overlayVal == "allow" && floorVal != "allow":
				combined[axis] = floorVal
			case overlayVal != "":
				combined[axis] = stricterAxis(overlayVal, floorVal)
			default:
				combined[axis] = floorVal
			}
		}
		combined["layer"] = layerBuiltin
		merged = append(merged, combined)
	}

	// 内置独有项追加到末尾（按 YAML 原始顺序，遍历 packaged 保证稳定）
	for _, item := range packaged {
		key, _ := item["path"].(string)
		key = strings.ReplaceAll(key, `\`, "/")
		if _, wasUsed := used[key]; wasUsed {
			continue
		}
		merged = append(merged, shallowCopyMap(item))
	}
	return merged
}

// copySensitiveEntries 深拷贝一层（每个 map 浅拷贝）。
func copySensitiveEntries(in []map[string]any) []map[string]any {
	out := make([]map[string]any, len(in))
	for i, item := range in {
		out[i] = shallowCopyMap(item)
	}
	return out
}

// deepCopyPermissions 深拷贝权限配置（file_guard.paths 逐项复制）。
func deepCopyPermissions(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		if k != "file_guard" {
			out[k] = v
			continue
		}
		fg := mapOf(v)
		if fg == nil {
			out[k] = v
			continue
		}
		copied := make(map[string]any, len(fg))
		for fk, fv := range fg {
			if fk != "paths" {
				copied[fk] = fv
				continue
			}
			oldPaths := pathListOf(fv)
			newPaths := make([]any, 0, len(oldPaths))
			for _, p := range oldPaths {
				newPaths = append(newPaths, shallowCopyMap(p))
			}
			copied[fk] = newPaths
		}
		out[k] = copied
	}
	return out
}

// toAnySliceFromMaps 把 map 列表转为 []any。
func toAnySliceFromMaps(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, m := range items {
		out = append(out, m)
	}
	return out
}

// shallowCopyMap 浅拷贝一个 map。
func shallowCopyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// filepathToSlash 把路径分隔符统一为 /。
func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

// init 注册三类跨包能力：
//   - FileGuardFactory：让 security 包构造本包判定器
//   - SensitivePathsMerger：让 security.PreparePermissionsForEngine 注入敏感路径
func init() {
	security.RegisterFileGuardFactory(func(permissions map[string]any, workspaceRoot string, trustedDirs []string) security.FileGuardEvaluator {
		checker := BuildFileGuardChecker(permissions, workspaceRoot, trustedDirs)
		if checker == nil {
			return nil
		}
		return checker
	})
	security.RegisterPermissionBridges(MergePackageSensitivePaths, nil, nil)
}

// 编译期断言：*FileGuardChecker 满足 security.FileGuardEvaluator
var _ security.FileGuardEvaluator = (*FileGuardChecker)(nil)
