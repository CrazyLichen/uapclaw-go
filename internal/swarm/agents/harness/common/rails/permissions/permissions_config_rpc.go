package permissions

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// validPermLevels 有效权限级别
// 对齐 Python: _VALID_PERM_LEVEL = frozenset({"allow", "ask", "deny"})
var validPermLevels = map[string]bool{
	"allow": true,
	"ask":   true,
	"deny":  true,
}

// validRuleSeverities 有效规则严重度
// 对齐 Python: _VALID_RULE_SEVERITY = frozenset({"LOW", "MEDIUM", "HIGH", "CRITICAL"})
var validRuleSeverities = map[string]bool{
	"LOW":      true,
	"MEDIUM":   true,
	"HIGH":     true,
	"CRITICAL": true,
}

// ruleMutableKeys 规则可变键集合
// 对齐 Python: _RULE_MUTABLE_KEYS = frozenset({"tools", "pattern", "severity", "action", "description", "match_type"})
var ruleMutableKeys = map[string]bool{
	"tools":        true,
	"pattern":      true,
	"severity":     true,
	"action":       true,
	"description":  true,
	"match_type":   true,
}

var permRpcLogComponent = logger.ComponentChannel

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DispatchPermissionsConfigRequest 执行一条权限配置 RPC。
//
// 对齐 Python: dispatch_permissions_config_request(request) (permissions_config_rpc.py L57-156)
//
// 入参 params 为已解析的请求参数字典（从 json.RawMessage 解析而来）。
// 返回 (ok, payload)：ok=true 时 payload 为响应数据，ok=false 时 payload 含 error/code。
func DispatchPermissionsConfigRequest(reqMethod string, params map[string]any) (bool, map[string]any) {
	switch reqMethod {
	case "permissions.tools.get":
		return true, GetPermissionsTools()

	case "permissions.tools.set":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		tools := params["tools"]
		if err := ReplacePermissionsToolsInConfig(tools); err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, map[string]any{"ok": true}

	case "permissions.tools.update":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		toolName := toolNameFromParams(params)
		if toolName == "" {
			return false, errPayload("tool is required", "BAD_REQUEST")
		}
		if _, hasLevel := params["level"]; !hasLevel {
			return false, errPayload("level is required", "BAD_REQUEST")
		}
		result, err := UpdatePermissionsToolInConfig(toolName, params["level"])
		if err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, result

	case "permissions.tools.delete":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		toolName := toolNameFromParams(params)
		if toolName == "" {
			return false, errPayload("tool is required", "BAD_REQUEST")
		}
		ok := DeletePermissionsToolInConfig(toolName)
		if !ok {
			return false, errPayload("tool not found in permissions.tools", "NOT_FOUND")
		}
		return true, GetPermissionsTools()

	case "permissions.rules.get":
		return true, GetPermissionsRules()

	case "permissions.rules.create":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		rule, _ := params["rule"].(map[string]any)
		if rule == nil {
			return false, errPayload("rule must be object", "BAD_REQUEST")
		}
		stored, err := CreatePermissionsRuleInConfig(rule)
		if err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, map[string]any{"rule": stored}

	case "permissions.rules.update":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		rid := strVal(params["id"])
		patch, _ := params["patch"].(map[string]any)
		if patch == nil {
			return false, errPayload("patch must be object", "BAD_REQUEST")
		}
		merged, err := UpdatePermissionsRuleInConfig(rid, patch)
		if err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, map[string]any{"rule": merged}

	case "permissions.rules.delete":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		rid := strVal(params["id"])
		ok := DeletePermissionsRuleInConfig(rid)
		if !ok {
			return false, errPayload("rule not found", "NOT_FOUND")
		}
		return true, map[string]any{"ok": true}

	case "permissions.approval_overrides.get":
		return true, GetPermissionsApprovalOverrides()

	case "permissions.approval_overrides.delete":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		oid := strVal(params["id"])
		ok := DeletePermissionsApprovalOverrideInConfig(oid)
		if !ok {
			return false, errPayload("approval_override not found", "NOT_FOUND")
		}
		return true, map[string]any{"ok": true}

	default:
		return false, errPayload("unknown permissions req_method", "BAD_REQUEST")
	}
}

// GetPermissionsTools 返回 permissions.tools 配置。
//
// 对齐 Python: get_permissions_tools() (config.py L351-358)
func GetPermissionsTools() map[string]any {
	cfg := loadLiveConfig()
	tools, _ := cfg["tools"].(map[string]any)
	if tools == nil {
		return map[string]any{"tools": map[string]any{}}
	}
	return map[string]any{"tools": tools}
}

// ReplacePermissionsToolsInConfig 整表替换 permissions.tools 并写回 YAML。
//
// 对齐 Python: replace_permissions_tools_in_config(tools) (config.py L360-368)
func ReplacePermissionsToolsInConfig(tools any) error {
	normalized, err := validateToolsMap(tools)
	if err != nil {
		return err
	}
	data := readYAMLData()
	if data["permissions"] == nil {
		data["permissions"] = map[string]any{}
	}
	perm, _ := data["permissions"].(map[string]any)
	perm["tools"] = normalized
	return writeYAMLData(data)
}

// UpdatePermissionsToolInConfig 合并单条工具级别到 permissions.tools 并写回 YAML。
//
// 对齐 Python: update_permissions_tool_in_config(tool_name, level) (config.py L370-395)
func UpdatePermissionsToolInConfig(toolName string, level any) (map[string]any, error) {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return nil, fmt.Errorf("tool name must be non-empty")
	}
	piece, err := validateToolsMap(map[string]any{name: level})
	if err != nil {
		return nil, err
	}
	data := readYAMLData()
	if data["permissions"] == nil {
		data["permissions"] = map[string]any{}
	}
	perm, _ := data["permissions"].(map[string]any)
	existing, _ := perm["tools"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	existing[name] = piece[name]
	perm["tools"] = existing
	if err := writeYAMLData(data); err != nil {
		return nil, err
	}
	return map[string]any{"tools": existing}, nil
}

// DeletePermissionsToolInConfig 从 permissions.tools 中删除一个键。
//
// 对齐 Python: delete_permissions_tool_in_config(tool_name) (config.py L397-439)
func DeletePermissionsToolInConfig(toolName string) bool {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return false
	}
	data := readYAMLData()
	perm, _ := data["permissions"].(map[string]any)
	if perm == nil {
		return false
	}
	tools, _ := perm["tools"].(map[string]any)
	if tools == nil {
		return false
	}
	// 查找键（TrimSpace 后比较）
	keyToDelete := ""
	for k := range tools {
		if strings.TrimSpace(fmt.Sprintf("%v", k)) == name {
			keyToDelete = k
			break
		}
	}
	if keyToDelete == "" {
		return false
	}
	delete(tools, keyToDelete)
	_ = writeYAMLData(data)
	return true
}

// GetPermissionsRules 返回 permissions.rules 列表。
//
// 对齐 Python: get_permissions_rules() (config.py L441-448)
func GetPermissionsRules() map[string]any {
	cfg := loadLiveConfig()
	rules, _ := cfg["rules"].([]any)
	if rules == nil {
		return map[string]any{"rules": []any{}}
	}
	// 仅保留 dict 项
	filtered := make([]any, 0, len(rules))
	for _, r := range rules {
		if _, ok := r.(map[string]any); ok {
			filtered = append(filtered, r)
		}
	}
	return map[string]any{"rules": filtered}
}

// GetPermissionsApprovalOverrides 返回 permissions.approval_overrides 列表。
//
// 对齐 Python: get_permissions_approval_overrides() (config.py L450-457)
func GetPermissionsApprovalOverrides() map[string]any {
	cfg := loadLiveConfig()
	raw, _ := cfg["approval_overrides"].([]any)
	if raw == nil {
		return map[string]any{"approval_overrides": []any{}}
	}
	filtered := make([]any, 0, len(raw))
	for _, x := range raw {
		if _, ok := x.(map[string]any); ok {
			filtered = append(filtered, x)
		}
	}
	return map[string]any{"approval_overrides": filtered}
}

// CreatePermissionsRuleInConfig 追加一条 permissions.rules 项。
//
// 对齐 Python: create_permissions_rule_in_config(rule) (config.py L459-490)
func CreatePermissionsRuleInConfig(rule map[string]any) (map[string]any, error) {
	rid := strings.TrimSpace(strVal(rule["id"]))
	if rid == "" {
		rid = "ui_rule_" + randomHex(12)
	}

	stored := map[string]any{"id": rid}
	for key := range ruleMutableKeys {
		if v, ok := rule[key]; ok && v != nil {
			stored[key] = v
		}
	}
	// 必填检查
	if _, hasTools := stored["tools"]; !hasTools {
		return nil, fmt.Errorf("tools and pattern are required")
	}
	if _, hasPattern := stored["pattern"]; !hasPattern {
		return nil, fmt.Errorf("tools and pattern are required")
	}
	// 归一化
	normalized, err := normalizeRuleTools(stored["tools"])
	if err != nil {
		return nil, err
	}
	stored["tools"] = normalized
	stored["pattern"] = strings.TrimSpace(strVal(stored["pattern"]))
	if len(normalized) == 0 {
		return nil, fmt.Errorf("tools must be a non-empty list")
	}
	if strVal(stored["pattern"]) == "" {
		return nil, fmt.Errorf("pattern must be non-empty")
	}
	normalizeRuleSeverityAction(stored)

	data := readYAMLData()
	if data["permissions"] == nil {
		data["permissions"] = map[string]any{}
	}
	perm, _ := data["permissions"].(map[string]any)
	rules, _ := perm["rules"].([]any)
	if rules == nil {
		rules = []any{}
	}
	// 重复 ID 检查
	for _, r := range rules {
		if m, ok := r.(map[string]any); ok {
			if strings.TrimSpace(strVal(m["id"])) == rid {
				return nil, fmt.Errorf("rule id already exists: %s", rid)
			}
		}
	}
	rules = append(rules, stored)
	perm["rules"] = rules
	if err := writeYAMLData(data); err != nil {
		return nil, err
	}
	return stored, nil
}

// UpdatePermissionsRuleInConfig 按 ID 合并更新一条 rule。
//
// 对齐 Python: update_permissions_rule_in_config(rule_id, patch) (config.py L492-538)
func UpdatePermissionsRuleInConfig(ruleID string, patch map[string]any) (map[string]any, error) {
	rid := strings.TrimSpace(ruleID)
	if rid == "" {
		return nil, fmt.Errorf("id is required")
	}

	data := readYAMLData()
	if data["permissions"] == nil {
		data["permissions"] = map[string]any{}
	}
	perm, _ := data["permissions"].(map[string]any)
	rules, _ := perm["rules"].([]any)
	if rules == nil {
		rules = []any{}
	}

	idx := -1
	for i, r := range rules {
		if m, ok := r.(map[string]any); ok {
			if strings.TrimSpace(strVal(m["id"])) == rid {
				idx = i
				break
			}
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("rule not found: %s", rid)
	}

	merged, _ := rules[idx].(map[string]any)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range patch {
		if k == "id" {
			continue
		}
		if !ruleMutableKeys[k] {
			continue
		}
		if v == nil {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	merged["id"] = rid

	// 归一化
	if _, hasTools := merged["tools"]; hasTools {
		normalized, err := normalizeRuleTools(merged["tools"])
		if err != nil {
			return nil, err
		}
		merged["tools"] = normalized
	}
	if _, hasPattern := merged["pattern"]; hasPattern {
		merged["pattern"] = strings.TrimSpace(strVal(merged["pattern"]))
	}
	// 必填检查
	toolsList, _ := merged["tools"].([]string)
	if len(toolsList) == 0 {
		return nil, fmt.Errorf("tools must be a non-empty list")
	}
	if strVal(merged["pattern"]) == "" {
		return nil, fmt.Errorf("pattern must be non-empty")
	}
	normalizeRuleSeverityAction(merged)

	rules[idx] = merged
	perm["rules"] = rules
	if err := writeYAMLData(data); err != nil {
		return nil, err
	}
	return merged, nil
}

// DeletePermissionsRuleInConfig 删除 permissions.rules 中指定 ID 的规则。
//
// 对齐 Python: delete_permissions_rule_in_config(rule_id) (config.py L540-557)
func DeletePermissionsRuleInConfig(ruleID string) bool {
	rid := strings.TrimSpace(ruleID)
	if rid == "" {
		return false
	}
	data := readYAMLData()
	perm, _ := data["permissions"].(map[string]any)
	if perm == nil {
		return false
	}
	rules, _ := perm["rules"].([]any)
	if rules == nil {
		return false
	}
	newRules := make([]any, 0, len(rules))
	found := false
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if ok && strings.TrimSpace(strVal(m["id"])) == rid {
			found = true
			continue
		}
		newRules = append(newRules, r)
	}
	if !found {
		return false
	}
	perm["rules"] = newRules
	_ = writeYAMLData(data)
	return true
}

// DeletePermissionsApprovalOverrideInConfig 按 ID 删除 approval_overrides 中一项。
//
// 对齐 Python: delete_permissions_approval_override_in_config(override_id) (config.py L559-575)
func DeletePermissionsApprovalOverrideInConfig(overrideID string) bool {
	oid := strings.TrimSpace(overrideID)
	if oid == "" {
		return false
	}
	data := readYAMLData()
	perm, _ := data["permissions"].(map[string]any)
	if perm == nil {
		return false
	}
	ov, _ := perm["approval_overrides"].([]any)
	if ov == nil {
		return false
	}
	newOv := make([]any, 0, len(ov))
	found := false
	for _, x := range ov {
		m, ok := x.(map[string]any)
		if ok && strings.TrimSpace(strVal(m["id"])) == oid {
			found = true
			continue
		}
		newOv = append(newOv, x)
	}
	if !found {
		return false
	}
	perm["approval_overrides"] = newOv
	_ = writeYAMLData(data)
	return true
}

// ParseParams 从 json.RawMessage 解析请求参数。
func ParseParams(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	return params
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadLiveConfig 从 YAML 配置文件读取当前 permissions 配置段。
// 对齐 Python: get_config().get("permissions", {})
func loadLiveConfig() map[string]any {
	data := harnesssecurity.ReadAgentConfigYAML("")
	perm, _ := data["permissions"].(map[string]any)
	if perm == nil {
		return map[string]any{}
	}
	return perm
}

// readYAMLData 读取 agent YAML 配置文件。
// 对齐 Python: load_yaml_round_trip(CONFIG_YAML_PATH)
func readYAMLData() map[string]any {
	data := harnesssecurity.ReadAgentConfigYAML("")
	if data == nil {
		return map[string]any{}
	}
	return data
}

// writeYAMLData 写回 agent YAML 配置文件。
// 对齐 Python: dump_yaml_round_trip(CONFIG_YAML_PATH, data)
func writeYAMLData(data map[string]any) error {
	return harnesssecurity.WriteAgentConfigYAML("", data)
}

// errPayload 构造错误 payload。
// 对齐 Python: _err(request, message, code=...)
func errPayload(message, code string) map[string]any {
	return map[string]any{"error": message, "code": code}
}

// toolNameFromParams 从 params 中提取 tool/name 字段。
// 对齐 Python: str(params.get("tool") or params.get("name") or "").strip()
func toolNameFromParams(params map[string]any) string {
	name := strVal(params["tool"])
	if name == "" {
		name = strVal(params["name"])
	}
	return strings.TrimSpace(name)
}

// strVal 安全获取 string 值
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// validateToolsMap 校验 tools 映射值。
// 对齐 Python: _validate_tools_map(tools) (config.py L421-437)
func validateToolsMap(tools any) (map[string]any, error) {
	m, ok := tools.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tools must be an object")
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		name := strings.TrimSpace(fmt.Sprintf("%v", k))
		if name == "" {
			return nil, fmt.Errorf("tool name must be non-empty")
		}
		// 支持 {"*": "level"} 格式或直接字符串
		if subMap, ok := v.(map[string]any); ok {
			if star, ok := subMap["*"].(string); ok {
				level := strings.TrimSpace(strings.ToLower(star))
				if !validPermLevels[level] {
					return nil, fmt.Errorf("tools[%q]: invalid level %q", name, level)
				}
				out[name] = level
			} else {
				return nil, fmt.Errorf("tools[%q]: value must be allow|ask|deny or object {*: level}", name)
			}
		} else if s, ok := v.(string); ok {
			level := strings.TrimSpace(strings.ToLower(s))
			if !validPermLevels[level] {
				return nil, fmt.Errorf("tools[%q]: invalid level %q", name, level)
			}
			out[name] = level
		} else {
			return nil, fmt.Errorf("tools[%q]: value must be allow|ask|deny or object {*: level}", name)
		}
	}
	return out, nil
}

// normalizeRuleTools 归一化规则 tools 字段。
// 对齐 Python: _normalize_rule_tools(raw) (config.py L578-584)
func normalizeRuleTools(raw any) ([]string, error) {
	switch v := raw.(type) {
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					result = append(result, s)
				}
			}
		}
		return result, nil
	case []string:
		return v, nil
	default:
		return nil, fmt.Errorf("tools must be a string or array of strings")
	}
}

// normalizeRuleSeverityAction 归一化规则 severity 和 action 字段。
// 对齐 Python: _normalize_rule_severity_action(rule) (config.py L587-595)
func normalizeRuleSeverityAction(rule map[string]any) {
	if sev, ok := rule["severity"].(string); ok {
		upper := strings.TrimSpace(strings.ToUpper(sev))
		if validRuleSeverities[upper] {
			rule["severity"] = upper
		}
	}
	if act, ok := rule["action"].(string); ok {
		lower := strings.TrimSpace(strings.ToLower(act))
		if validPermLevels[lower] {
			rule["action"] = lower
		}
	}
}

// randomHex 生成 n 字节的随机十六进制字符串。
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)[:n*2]
}
