package configpanel

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LocaleSetConf 写入界面偏好语言（Python: config_adapter.py:212-236 LOCALE_SET_CONF
// 分支 + update_preferred_language_in_config：仅接受 zh/en，写 preferred_language）。
func LocaleSetConf(params map[string]any) error {
	rawLang := strings.TrimSpace(fmt.Sprintf("%v", orEmptyAny(params["preferred_language"])))
	if rawLang != "zh" && rawLang != "en" {
		return &ConfigPanelBadRequest{Message: "preferred_language must be zh or en"}
	}
	writeYAMLPath([]string{"preferred_language"}, rawLang)
	return nil
}

// MemoryForbiddenSet 更新 memory.forbidden_memory_definition 并写回
// （Python: config.py:3395 update_memory_forbidden_in_config：description
// 为 dict 时与既有 description 合并，其余键直接覆盖）。
func MemoryForbiddenSet(params map[string]any) {
	mutateYAMLFile(func(root map[string]any) {
		memory, _ := root["memory"].(map[string]any)
		if memory == nil {
			memory = map[string]any{}
			root["memory"] = memory
		}
		section, _ := memory["forbidden_memory_definition"].(map[string]any)
		if section == nil {
			section = map[string]any{}
			memory["forbidden_memory_definition"] = section
		}
		for k, v := range params {
			if k == "description" {
				if descMap, isMap := v.(map[string]any); isMap {
					oldDesc, _ := section["description"].(map[string]any)
					if oldDesc == nil {
						oldDesc = map[string]any{}
					}
					merged := map[string]any{}
					for ok, ov := range oldDesc {
						merged[ok] = ov
					}
					for nk, nv := range descMap {
						merged[nk] = nv
					}
					section["description"] = merged
					continue
				}
			}
			section[k] = v
		}
	})
}

// GetPermissionsOwnerScopes 读取 permissions.owner_scopes 及 deny_guidance_message
// （Python: config.py:1188 get_permissions_owner_scopes：None 归一空表）。
func GetPermissionsOwnerScopes() map[string]any {
	raw := ReadRawConfig()
	perm, _ := raw["permissions"].(map[string]any)
	ownerScopes := any(map[string]any{})
	if perm != nil {
		if os, ok := perm["owner_scopes"].(map[string]any); ok {
			ownerScopes = os
		}
	}
	denyMsg := ""
	if perm != nil {
		if s, ok := perm["deny_guidance_message"].(string); ok {
			denyMsg = s
		}
	}
	return map[string]any{
		"owner_scopes":          ownerScopes,
		"deny_guidance_message": denyMsg,
	}
}

// UpdatePermissionsOwnerScopes 更新 permissions.owner_scopes（及可选
// deny_guidance_message）并写回（Python: config.py:1199 update_permissions_owner_scopes_in_config）。
func UpdatePermissionsOwnerScopes(ownerScopes any, denyGuidanceMessage any) {
	mutateYAMLFile(func(root map[string]any) {
		perm, _ := root["permissions"].(map[string]any)
		if perm == nil {
			perm = map[string]any{}
			root["permissions"] = perm
		}
		perm["owner_scopes"] = ownerScopes
		if denyGuidanceMessage != nil {
			perm["deny_guidance_message"] = denyGuidanceMessage
		}
	})
}

// PathSet 写浏览器路径配置（Python: config_adapter.py:333-388 _handle_browser_config
// PATH_SET 分支：chrome_path 必须字符串、headless 非 bool 回 true；decision 族字段
// 依赖 browser_config.browser_decision_updates——⤵️ 待回填（缺口随 browser 域），
// 命中时保留旧值不写）。返回 (chromePath, headless, error)。
func PathSet(params map[string]any) (string, bool, error) {
	chromePathRaw, ok := params["chrome_path"]
	if !ok {
		return "", false, &ConfigPanelBadRequest{Message: "chrome_path must be string"}
	}
	chromePath, isStr := chromePathRaw.(string)
	if !isStr {
		return "", false, &ConfigPanelBadRequest{Message: "chrome_path must be string"}
	}
	chromePath = strings.TrimSpace(chromePath)
	headless := true
	if h, isBool := params["headless"].(bool); isBool {
		headless = h
	}
	mutateYAMLFile(func(root map[string]any) {
		browser, _ := root["browser"].(map[string]any)
		if browser == nil {
			browser = map[string]any{}
			root["browser"] = browser
		}
		browser["chrome_path"] = chromePath
		browser["headless"] = headless
	})
	return chromePath, headless, nil
}
