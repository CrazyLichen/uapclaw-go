package permissions

import (
	"encoding/json"
	"fmt"
	"strings"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// validPermLevels 有效权限级别
// Python: _VALID_PERM_LEVEL = frozenset({"allow", "ask", "deny"})
var validPermLevels = map[string]bool{
	"allow": true,
	"ask":   true,
	"deny":  true,
}

// validRuleSeverities 有效规则严重度
// Python: _VALID_RULE_SEVERITY = frozenset({"LOW", "MEDIUM", "HIGH", "CRITICAL"})
var validRuleSeverities = map[string]bool{
	"LOW":      true,
	"MEDIUM":   true,
	"HIGH":     true,
	"CRITICAL": true,
}

// ruleMutableKeys 规则可变键集合
// Python: _RULE_MUTABLE_KEYS = frozenset({"tools", "pattern", "severity", "action", "description", "match_type"})
var ruleMutableKeys = map[string]bool{
	"tools":       true,
	"pattern":     true,
	"severity":    true,
	"action":      true,
	"description": true,
	"match_type":  true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DispatchPermissionsConfigRequest 执行一条权限配置 RPC。
// Python: dispatch_permissions_config_request(request) (permissions_config_rpc.py L57-156)
//
// 入参 params 为已解析的请求参数字典（从 json.RawMessage 解析而来）。
// 返回 (ok, payload)：ok=true 时 payload 为响应数据，ok=false 时 payload 含 error/code。
func DispatchPermissionsConfigRequest(reqMethod string, params map[string]any) (ok bool, payload map[string]any) {
	// 兜底保护：分发过程中任何地方 panic，都转成 INTERNAL_ERROR 响应返回，不让调用方直接崩掉。
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error(logComponent).
				Any("recover", rec).
				Str("req_method", reqMethod).
				Msg("[permissions_config_rpc] dispatch panic recovery")
			ok = false
			payload = map[string]any{
				"error": fmt.Sprintf("internal error: %v", rec),
				"code":  "INTERNAL_ERROR",
			}
		}
	}()

	// 按方法名分发，每个分支自己完成参数校验、执行操作、组装结果或错误。
	switch reqMethod {
	// 工具级权限的整体读写：get 直接返回当前配置，set 用新表整表覆盖。
	case "permissions.tools.get":
		return true, configutil.GetPermissionsTools()

	case "permissions.tools.set":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		tools := params["tools"]
		if err := configutil.ReplacePermissionsToolsInConfig(tools); err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, map[string]any{"ok": true}

	// 只改一个工具：工具名和 level 都必须给，返回更新后的完整工具表。
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
		result, err := configutil.UpdatePermissionsToolInConfig(toolName, params["level"])
		if err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, result

	// 删除一个工具：配置里没有这个键时返回 NOT_FOUND，删成功后回传最新的工具表。
	case "permissions.tools.delete":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		toolName := toolNameFromParams(params)
		if toolName == "" {
			return false, errPayload("tool is required", "BAD_REQUEST")
		}
		ok, err := configutil.DeletePermissionsToolInConfig(toolName)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("req_method", "permissions.tools.delete").
				Str("tool_name", toolName).
				Msg("[permissions_config_rpc] failed to delete tool")
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		if !ok {
			return false, errPayload("tool not found in permissions.tools", "NOT_FOUND")
		}
		return true, configutil.GetPermissionsTools()

	// 规则列表读取与新增：create 会补 id、检查 tools/pattern 必填项并归一化，返回落盘后的规则。
	case "permissions.rules.get":
		return true, configutil.GetPermissionsRules()

	case "permissions.rules.create":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		rule, _ := params["rule"].(map[string]any)
		if rule == nil {
			return false, errPayload("rule must be object", "BAD_REQUEST")
		}
		stored, err := configutil.CreatePermissionsRuleInConfig(rule)
		if err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, map[string]any{"rule": stored}

	// 按 id 局部更新规则：patch 只能改白名单字段，id 为空或规则不存在都会报错。
	case "permissions.rules.update":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		rid := utils.StrVal(params["id"])
		patch, _ := params["patch"].(map[string]any)
		if patch == nil {
			return false, errPayload("patch must be object", "BAD_REQUEST")
		}
		merged, err := configutil.UpdatePermissionsRuleInConfig(rid, patch)
		if err != nil {
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		return true, map[string]any{"rule": merged}

	// 按 id 删除规则：没匹配到任何一条时返回 NOT_FOUND。
	case "permissions.rules.delete":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		rid := utils.StrVal(params["id"])
		ok, err := configutil.DeletePermissionsRuleInConfig(rid)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("req_method", "permissions.rules.delete").
				Str("rule_id", rid).
				Msg("[permissions_config_rpc] failed to delete rule")
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		if !ok {
			return false, errPayload("rule not found", "NOT_FOUND")
		}
		return true, map[string]any{"ok": true}

	// 审批覆盖项：只有读取和删除，delete 按 id 定位，找不到返回 NOT_FOUND。
	case "permissions.approval_overrides.get":
		return true, configutil.GetPermissionsApprovalOverrides()

	case "permissions.approval_overrides.delete":
		if params == nil {
			return false, errPayload("params must be object", "BAD_REQUEST")
		}
		oid := utils.StrVal(params["id"])
		ok, err := configutil.DeletePermissionsApprovalOverrideInConfig(oid)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("req_method", "permissions.approval_overrides.delete").
				Str("override_id", oid).
				Msg("[permissions_config_rpc] failed to delete approval_override")
			return false, errPayload(err.Error(), "BAD_REQUEST")
		}
		if !ok {
			return false, errPayload("approval_override not found", "NOT_FOUND")
		}
		return true, map[string]any{"ok": true}

	// 方法名不在支持范围内，统一按 BAD_REQUEST 返回。
	default:
		return false, errPayload("unknown permissions req_method", "BAD_REQUEST")
	}
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

// errPayload 构造错误 payload。
// Python: _err(request, message, code=...) (permissions_config_rpc.py L38-46)
func errPayload(message, code string) map[string]any {
	return map[string]any{"error": message, "code": code}
}

// readYAMLData 读取 agent YAML 配置文件。
// Python: load_yaml_round_trip(CONFIG_YAML_PATH)
//
// 包内 permissions_persist.go / owner_scopes.go 仍需按原始字典读写配置，
// 因此保留（config 层的 11 个函数只负责 permissions 段的语义操作）。
func readYAMLData() map[string]any {
	data := harnesssecurity.ReadAgentConfigYAML("")
	if data == nil {
		return map[string]any{}
	}
	return data
}

// writeYAMLData 写回 agent YAML 配置文件。
// Python: dump_yaml_round_trip(CONFIG_YAML_PATH, data)
func writeYAMLData(data map[string]any) error {
	return harnesssecurity.WriteAgentConfigYAML("", data)
}

// toolNameFromParams 从 params 中提取 tool/name 字段。
// Python: str(params.get("tool") or params.get("name") or "").strip()
func toolNameFromParams(params map[string]any) string {
	name := utils.StrVal(params["tool"])
	if name == "" {
		name = utils.StrVal(params["name"])
	}
	return strings.TrimSpace(name)
}
