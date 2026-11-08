package toolguard

import (
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// validCommandRuleActions 命令规则允许的 action 取值
	validCommandRuleActions = map[string]struct{}{"ask": {}, "deny": {}, "allow": {}}

	// falseStrings 字符串型假值
	//
	// Python: _FALSE_STRINGS
	falseStrings = map[string]struct{}{"false": {}, "0": {}, "no": {}, "off": {}}

	// trueStrings 字符串型真值
	//
	// Python: _TRUE_STRINGS
	trueStrings = map[string]struct{}{"true": {}, "1": {}, "yes": {}, "on": {}}

	// commandRulesCacheMu 内置规则缓存锁
	commandRulesCacheMu sync.RWMutex

	// commandRulesCache 上次读取的内置规则（只读快照，调用方不得修改）
	commandRulesCache []map[string]any

	// commandRulesCachePath 缓存对应的 YAML 路径
	commandRulesCachePath string

	// commandRulesCacheMtime 缓存对应的文件 mtime
	commandRulesCacheMtime float64

	// commandRulesCacheLoaded 是否已加载过
	commandRulesCacheLoaded bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// PackageBuiltinRulesEnabled 宿主是否启用包内 YAML 的内置规则。
//
// 该开关同时控制命令规则、sensitive_paths、net_urls 三者。
// 键缺失或值无法识别时默认开启。
//
// Python: package_builtin_rules_enabled(permissions) (builtin_rules.py L30-45)
func PackageBuiltinRulesEnabled(permissions map[string]any) bool {
	value, ok := permissions["package_builtin_rules"]
	if !ok || value == nil {
		return true
	}
	if b, isBool := value.(bool); isBool {
		return b
	}
	if s, isStr := value.(string); isStr {
		lowered := strings.ToLower(strings.TrimSpace(s))
		if _, isFalse := falseStrings[lowered]; isFalse {
			return false
		}
		if _, isTrue := trueStrings[lowered]; isTrue {
			return true
		}
	}
	return true
}

// LoadPackageCommandRules 返回包内 YAML 的原始命令规则。
//
// 返回只读快照，调用方不得修改。mtime 变化时自动重载。
//
// Python: load_package_command_rules() (builtin_rules.py L89-108)
func LoadPackageCommandRules() []map[string]any {
	yamlPath := resources.ResolveBuiltinRulesYAMLPath()
	if yamlPath == "" {
		parsed, err := resources.ParseBuiltinRules()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("permission.command_rules.builtin_embed_parse_failed")
			return nil
		}
		return convertSecurityRules(parsed.Rules)
	}

	mtime := resources.GetFileModTime(yamlPath)

	commandRulesCacheMu.RLock()
	if commandRulesCacheLoaded && commandRulesCachePath == yamlPath && commandRulesCacheMtime == mtime {
		cached := commandRulesCache
		commandRulesCacheMu.RUnlock()
		return cached
	}
	commandRulesCacheMu.RUnlock()

	commandRulesCacheMu.Lock()
	defer commandRulesCacheMu.Unlock()
	// 双检：可能在等锁期间被其他 goroutine 填充
	if commandRulesCacheLoaded && commandRulesCachePath == yamlPath && commandRulesCacheMtime == mtime {
		return commandRulesCache
	}

	parsed, err := resources.ParseBuiltinRulesFromFile(yamlPath)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", yamlPath).
			Err(err).
			Msg("permission.command_rules.yaml_read_failed")
		return nil
	}
	rules := convertSecurityRules(parsed.Rules)
	commandRulesCache = rules
	commandRulesCachePath = yamlPath
	commandRulesCacheMtime = mtime
	commandRulesCacheLoaded = true
	return rules
}

// InlinePackageCommandRules 把内置规则注入配置的 rules 段。
//
// 内置规则带 layer=builtin 标记并排在用户规则之前；调用方配置里已有的
// builtin 层会被先剔除，避免重复调用时叠加。
//
// Python: inline_package_command_rules(permissions) (builtin_rules.py L120-143)
func InlinePackageCommandRules(permissions map[string]any) map[string]any {
	cfg := deepCopyPermissions(permissions)
	if !PackageBuiltinRulesEnabled(cfg) {
		return cfg
	}

	existing := listOfMaps(cfg["rules"])
	userRules := make([]map[string]any, 0, len(existing))
	for _, r := range existing {
		if layer, _ := r["layer"].(string); layer != "builtin" {
			userRules = append(userRules, r)
		}
	}

	packageRules := make([]map[string]any, 0, 17)
	for _, raw := range LoadPackageCommandRules() {
		action := yamlCommandRuleAction(raw)
		if action == "" {
			continue
		}
		item := shallowCopyMap(raw)
		item["layer"] = "builtin"
		item["action"] = action
		packageRules = append(packageRules, item)
	}

	merged := make([]any, 0, len(packageRules)+len(userRules))
	for _, r := range packageRules {
		merged = append(merged, r)
	}
	for _, r := range userRules {
		merged = append(merged, r)
	}
	cfg["rules"] = merged
	return cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// yamlCommandRuleAction 读取并校验规则的 action 字段。
//
// 缺失/非法/allow 都返回空串并打 WARN（allow 不允许出现在内置规则里）。
//
// Python: _yaml_action(item) (builtin_rules.py L70-87)
func yamlCommandRuleAction(item map[string]any) string {
	raw, ok := item["action"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Msg("permission.command_rules.missing_action")
		return ""
	}
	action := strings.ToLower(strings.TrimSpace(raw))
	if _, valid := validCommandRuleActions[action]; !valid {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Str("action", action).
			Msg("permission.command_rules.invalid_action")
		return ""
	}
	if action == "allow" {
		logger.Warn(logComponent).
			Interface("id", item["id"]).
			Msg("permission.command_rules.skip")
		return ""
	}
	return action
}

// convertSecurityRules 把 resources 的结构体规则转为 map 形式。
func convertSecurityRules(rules []resources.SecurityRule) []map[string]any {
	out := make([]map[string]any, 0, len(rules))
	for _, r := range rules {
		item := map[string]any{
			"id":          r.ID,
			"description": r.Description,
			"severity":    r.Severity,
			"match_type":  r.MatchType,
			"pattern":     r.Pattern,
			"tools":       toAnySlice(r.TargetTools),
		}
		if r.Action != "" {
			item["action"] = r.Action
		}
		out = append(out, item)
	}
	return out
}

// deepCopyPermissions 深拷贝权限配置（rules 段逐项复制）。
func deepCopyPermissions(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		if k == "rules" {
			list := listOfMaps(v)
			copied := make([]any, 0, len(list))
			for _, item := range list {
				copied = append(copied, shallowCopyMap(item))
			}
			out[k] = copied
			continue
		}
		out[k] = v
	}
	return out
}

// listOfMaps 把 any 转为 []map[string]any，非列表返回 nil。
func listOfMaps(v any) []map[string]any {
	switch typed := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if m, isMap := item.(map[string]any); isMap {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return typed
	}
	return nil
}

// shallowCopyMap 浅拷贝一个 map。
func shallowCopyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// toAnySlice 把 []string 转为 []any（YAML 序列化友好）。
func toAnySlice(items []string) []any {
	out := make([]any, 0, len(items))
	for _, s := range items {
		out = append(out, s)
	}
	return out
}
