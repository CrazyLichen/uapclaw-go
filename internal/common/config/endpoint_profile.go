package config

import "strings"

// 本文件对应上游 gateway/common/config.py 的 endpoint_profile_overrides 读取。

// ──────────────────────────── 常量 ────────────────────────────

// endpointProfileOverridesKey config.yaml 顶层键名。
// Python: config.py:318 cfg.get("endpoint_profile_overrides")
const endpointProfileOverridesKey = "endpoint_profile_overrides"

// ──────────────────────────── 导出函数 ────────────────────────────

// GetEndpointProfileOverrides 返回用户配置的 api_base host → endpoint_profile 映射。
// Python: gateway/common/config.py:308-327 get_endpoint_profile_overrides
//
// 读 config.yaml 顶层 endpoint_profile_overrides 键。用于自建网关（如
// DashScope 风格端点）——其思考控制方言（enable_thinking / thinking_budget）
// 无法从域名推断，用户在配置里声明后，保存/校验/运行各路径自动补
// endpoint_profile 透传给 core。能力档位仍按模型名 fallback。
//
// host 归一为小写；host 或 profile 为空的条目丢弃。源码不内置任何 host。
func GetEndpointProfileOverrides() map[string]string {
	cfg := readPermissionsConfigFile()
	raw, ok := cfg[endpointProfileOverridesKey].(map[string]any)
	if !ok {
		return map[string]string{}
	}
	overrides := make(map[string]string, len(raw))
	for host, profile := range raw {
		hostKey := strings.ToLower(strings.TrimSpace(toString(host)))
		profileValue := strings.TrimSpace(toString(profile))
		if hostKey != "" && profileValue != "" {
			overrides[hostKey] = profileValue
		}
	}
	return overrides
}
