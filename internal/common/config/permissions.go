// Package config 的 permissions 配置读写实现。
//
// 本文件对齐 Python jiuwenswarm/common/config.py 的 permissions 相关函数（L1199-1500）。
//
// # 为什么这些函数在 config 层
//
// Python 的 permissions_config_rpc.py 只是分发器：内部 `from
// jiuwenswarm.common.config import (...)` 委托 11 个函数，业务逻辑全在 config.py。
// Go 侧原先把这 11 个函数的实现放在 rails/permissions 包内，等于把 config 层职责
// 下沉到 rails 层，违反规则 12.1-1（Python 在 A 层做的事 Go 必须还在 A 层）。
//
// # 两个已修正的读取路径 bug
//
// 迁移前的 GetPermissionsTools / GetPermissionsRules 读的是顶层 cfg["tools"] /
// cfg["rules"]，漏了 ["permissions"] 这一层，导致前端「权限工具列表」读到空数据。
// 写路径（ReplacePermissionsToolsInConfig 等）用的是 data["permissions"]，读写不对称
// 正是 bug 的来源。本文件的读取路径与 Python 一致。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// permissionsSectionKey permissions 配置节键
	permissionsSectionKey = "permissions"
	// toolsSectionKey tools 子节键
	toolsSectionKey = "tools"
	// rulesSectionKey rules 子节键
	rulesSectionKey = "rules"
	// approvalOverridesSectionKey approval_overrides 子节键
	approvalOverridesSectionKey = "approval_overrides"
	// ownerScopesSectionKey owner_scopes 子节键
	ownerScopesSectionKey = "owner_scopes"
	// denyGuidanceMessageSectionKey deny_guidance_message 子节键
	denyGuidanceMessageSectionKey = "deny_guidance_message"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errEmptyToolName 工具名为空
	errEmptyToolName = errors.New("tool name must be non-empty")
	// errEmptyRuleID 规则 id 为空
	errEmptyRuleID = errors.New("rule id must be non-empty")
	// errEmptyOverrideID 覆盖项 id 为空
	errEmptyOverrideID = errors.New("override id must be non-empty")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetPermissionsTools 返回 permissions.tools（原始结构，可能含 legacy dict）。
//
// Python: get_permissions_tools (config.py L1239-1246)
func GetPermissionsTools() map[string]any {
	cfg := readPermissionsConfigFile()
	perms, _ := cfg[permissionsSectionKey].(map[string]any)
	tools, _ := perms[toolsSectionKey].(map[string]any)
	if tools == nil {
		return map[string]any{toolsSectionKey: map[string]any{}}
	}
	copied := make(map[string]any, len(tools))
	for key, value := range tools {
		copied[key] = value
	}
	return map[string]any{toolsSectionKey: copied}
}

// ReplacePermissionsToolsInConfig 整表替换 permissions.tools 并写回 YAML。
//
// Python: replace_permissions_tools_in_config (config.py L1248-1259)
func ReplacePermissionsToolsInConfig(tools any) error {
	normalized, err := ValidateToolsMap(tools)
	if err != nil {
		return err
	}
	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
		data[permissionsSectionKey] = perms
	}
	perms[toolsSectionKey] = normalized
	return writePermissionsConfigFile(data)
}

// UpdatePermissionsToolInConfig 合并单条工具级别到 permissions.tools 并写回 YAML。
//
// Python: update_permissions_tool_in_config (config.py L1261-1292)
//
// 返回 {"tools": 合并后的完整映射}，便于前端刷新。
func UpdatePermissionsToolInConfig(toolName string, level any) (map[string]any, error) {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return nil, errEmptyToolName
	}
	piece, err := ValidateToolsMap(map[string]any{name: level})
	if err != nil {
		return nil, err
	}

	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
		data[permissionsSectionKey] = perms
	}
	existing, _ := perms[toolsSectionKey].(map[string]any)
	merged := make(map[string]any, len(existing)+1)
	for key, value := range existing {
		merged[key] = value
	}
	merged[name] = piece[name]
	perms[toolsSectionKey] = merged

	if err := writePermissionsConfigFile(data); err != nil {
		return nil, err
	}
	return map[string]any{toolsSectionKey: merged}, nil
}

// DeletePermissionsToolInConfig 从 permissions.tools 中删除一个键；不存在返回 false。
//
// Python: delete_permissions_tool_in_config (config.py L1294-1342)
func DeletePermissionsToolInConfig(toolName string) (bool, error) {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return false, errEmptyToolName
	}

	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		return false, nil
	}
	tools, _ := perms[toolsSectionKey].(map[string]any)
	if tools == nil {
		return false, nil
	}

	keyToDelete := ""
	for key := range tools {
		if strings.TrimSpace(fmt.Sprint(key)) == name {
			keyToDelete = key
			break
		}
	}
	if keyToDelete == "" {
		return false, nil
	}
	delete(tools, keyToDelete)
	if err := writePermissionsConfigFile(data); err != nil {
		return false, err
	}
	return true, nil
}

// GetPermissionsRules 返回 permissions.rules 列表（仅保留 dict 项）。
//
// Python: get_permissions_rules (config.py L1344-1351)
func GetPermissionsRules() map[string]any {
	cfg := readPermissionsConfigFile()
	perms, _ := cfg[permissionsSectionKey].(map[string]any)
	rules, _ := perms[rulesSectionKey].([]any)
	filtered := make([]any, 0, len(rules))
	for _, rule := range rules {
		if _, ok := rule.(map[string]any); ok {
			filtered = append(filtered, rule)
		}
	}
	return map[string]any{rulesSectionKey: filtered}
}

// GetPermissionsApprovalOverrides 返回 permissions.approval_overrides（仅 dict 项）。
//
// Python: get_permissions_approval_overrides (config.py L1353-1360)
func GetPermissionsApprovalOverrides() map[string]any {
	cfg := readPermissionsConfigFile()
	perms, _ := cfg[permissionsSectionKey].(map[string]any)
	overrides, _ := perms[approvalOverridesSectionKey].([]any)
	filtered := make([]any, 0, len(overrides))
	for _, item := range overrides {
		if _, ok := item.(map[string]any); ok {
			filtered = append(filtered, item)
		}
	}
	return map[string]any{approvalOverridesSectionKey: filtered}
}

// CreatePermissionsRuleInConfig 创建一条 permissions.rules 规则（自动补 id）。
//
// Python: create_permissions_rule_in_config (config.py L1362-1399)
func CreatePermissionsRuleInConfig(rule map[string]any) (map[string]any, error) {
	created := make(map[string]any, len(rule)+1)
	for key, value := range rule {
		created[key] = value
	}
	if id, _ := created["id"].(string); strings.TrimSpace(id) == "" {
		created["id"] = randomHex(8)
	}

	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
		data[permissionsSectionKey] = perms
	}
	rules, _ := perms[rulesSectionKey].([]any)
	perms[rulesSectionKey] = append(rules, created)

	if err := writePermissionsConfigFile(data); err != nil {
		return nil, err
	}
	return created, nil
}

// UpdatePermissionsRuleInConfig 按 ruleID 打补丁。
//
// Python: update_permissions_rule_in_config (config.py L1401-1453)
func UpdatePermissionsRuleInConfig(ruleID string, patch map[string]any) (map[string]any, error) {
	id := strings.TrimSpace(ruleID)
	if id == "" {
		return nil, errEmptyRuleID
	}

	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		return nil, fmt.Errorf("rule not found: %s", id)
	}
	rules, _ := perms[rulesSectionKey].([]any)
	var updated map[string]any
	for index, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if existing, _ := rule["id"].(string); existing != id {
			continue
		}
		next := make(map[string]any, len(rule)+len(patch))
		for key, value := range rule {
			next[key] = value
		}
		for key, value := range patch {
			next[key] = value
		}
		next["id"] = id
		rules[index] = next
		updated = next
		break
	}
	if updated == nil {
		return nil, fmt.Errorf("rule not found: %s", id)
	}
	perms[rulesSectionKey] = rules
	if err := writePermissionsConfigFile(data); err != nil {
		return nil, err
	}
	return updated, nil
}

// DeletePermissionsRuleInConfig 按 ruleID 删除。
//
// Python: delete_permissions_rule_in_config (config.py L1455-1478)
func DeletePermissionsRuleInConfig(ruleID string) (bool, error) {
	id := strings.TrimSpace(ruleID)
	if id == "" {
		return false, errEmptyRuleID
	}

	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		return false, nil
	}
	rules, _ := perms[rulesSectionKey].([]any)
	kept := make([]any, 0, len(rules))
	deleted := false
	for _, raw := range rules {
		if rule, ok := raw.(map[string]any); ok {
			if existing, _ := rule["id"].(string); existing == id {
				deleted = true
				continue
			}
		}
		kept = append(kept, raw)
	}
	if !deleted {
		return false, nil
	}
	perms[rulesSectionKey] = kept
	if err := writePermissionsConfigFile(data); err != nil {
		return false, err
	}
	return true, nil
}

// DeletePermissionsApprovalOverrideInConfig 按 overrideID 删除。
//
// Python: delete_permissions_approval_override_in_config (config.py L1480-1500)
func DeletePermissionsApprovalOverrideInConfig(overrideID string) (bool, error) {
	id := strings.TrimSpace(overrideID)
	if id == "" {
		return false, errEmptyOverrideID
	}

	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		return false, nil
	}
	overrides, _ := perms[approvalOverridesSectionKey].([]any)
	kept := make([]any, 0, len(overrides))
	deleted := false
	for _, raw := range overrides {
		if item, ok := raw.(map[string]any); ok {
			if existing, _ := item["id"].(string); existing == id {
				deleted = true
				continue
			}
		}
		kept = append(kept, raw)
	}
	if !deleted {
		return false, nil
	}
	perms[approvalOverridesSectionKey] = kept
	if err := writePermissionsConfigFile(data); err != nil {
		return false, err
	}
	return true, nil
}

// UpdatePermissionsOwnerScopesInConfig 更新 permissions.owner_scopes（及可选 deny_guidance_message）并写回。
//
// Python: update_permissions_owner_scopes_in_config (config.py L1199-1212)
// denyGuidanceMessage 为 nil 时不改动该键（对应 Python 的 None 缺省）。
func UpdatePermissionsOwnerScopesInConfig(ownerScopes, denyGuidanceMessage any) error {
	data := readPermissionsConfigFile()
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
		data[permissionsSectionKey] = perms
	}
	perms[ownerScopesSectionKey] = ownerScopes
	if denyGuidanceMessage != nil {
		perms[denyGuidanceMessageSectionKey] = denyGuidanceMessage
	}
	return writePermissionsConfigFile(data)
}

// ValidateToolsMap 校验并归一化 tools 映射。
//
// Python: _validate_tools_map(tools) (config.py L421-437)
//
// 值仅允许 allow / ask / deny，或 legacy {"*": level}。
func ValidateToolsMap(tools any) (map[string]any, error) {
	raw, ok := tools.(map[string]any)
	if !ok {
		return nil, errors.New("tools must be object")
	}
	normalized := make(map[string]any, len(raw))
	for key, value := range raw {
		name := strings.TrimSpace(fmt.Sprint(key))
		if name == "" {
			continue
		}
		level, err := normalizeToolLevel(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, name)
		}
		normalized[name] = level
	}
	return normalized, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readPermissionsConfigFile 读取配置文件顶层 map；文件不存在或解析失败时返回空 map。
//
// Python: get_config() or {}
//
// 路径解析顺序：UAPCLAW_CONFIG_YAML 环境变量 → 默认 ConfigFile()。
func readPermissionsConfigFile() map[string]any {
	path := os.Getenv("UAPCLAW_CONFIG_YAML")
	if path == "" {
		return readPermissionsConfigFromPath("")
	}
	return readPermissionsConfigFromPath(path)
}

// readPermissionsConfigFromPath 从指定路径读配置；空串走默认路径。
func readPermissionsConfigFromPath(explicit string) map[string]any {
	path := explicit
	if path == "" {
		path = permissionsConfigFilePath()
	}
	if path == "" {
		return map[string]any{}
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return map[string]any{}
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return map[string]any{}
	}
	if data == nil {
		return map[string]any{}
	}
	return data
}

// writePermissionsConfigFile 把顶层 map 写回配置文件。
func writePermissionsConfigFile(data map[string]any) error {
	path := os.Getenv("UAPCLAW_CONFIG_YAML")
	if path == "" {
		path = permissionsConfigFilePath()
	}
	if path == "" {
		return errors.New("no config yaml path")
	}
	out, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("yaml marshal error: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("yaml write error: %w", err)
	}
	return nil
}

// permissionsConfigFilePath 返回默认配置文件路径。
func permissionsConfigFilePath() string {
	return pathutil.ConfigFile()
}

// normalizeToolLevel 归一化单个工具的级别。
//
// Python: legacy {"*": level} 也接受（_validate_tools_map 内部处理）。
func normalizeToolLevel(value any) (any, error) {
	if mapping, ok := value.(map[string]any); ok {
		star, _ := mapping["*"].(string)
		return normalizeToolLevel(star)
	}
	level := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	switch level {
	case "allow", "ask", "deny":
		return level, nil
	default:
		return nil, errors.New("invalid tool level")
	}
}

// randomHex 生成 n 字节的随机十六进制串。
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
