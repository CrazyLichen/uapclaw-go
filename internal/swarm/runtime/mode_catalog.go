package runtime

import (
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeModeDescriptor 一个受支持的单 Agent 模式及其 SDK 可见能力。
// Python: mode_catalog.py:31-47 RuntimeModeDescriptor
type RuntimeModeDescriptor struct {
	// Mode 权威模式串
	Mode string
	// WorkMode "work" 或 "code"
	WorkMode string
	// IsPlan 是否规划模式
	IsPlan bool
	// SupportsCustomAgentDefinitions 是否支持自定义 Agent 定义
	SupportsCustomAgentDefinitions bool
}

// ModeCatalogResult 确定性排序的模式集合。
// Python: mode_catalog.py:50-56 ModeCatalogResult
type ModeCatalogResult struct {
	// Modes 按产品顺序排列
	Modes []RuntimeModeDescriptor
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrModeRequired 模式缺失
	// Python: mode_catalog.py:100 raise ModeCatalogError("mode is required", code="BAD_REQUEST")
	ErrModeRequired = errors.New("mode is required")
	// ErrModeNotFound 单 Agent 模式不存在
	// Python: mode_catalog.py:106 raise ModeCatalogError("single-Agent mode not found", code="NOT_FOUND")
	ErrModeNotFound = errors.New("single-Agent mode not found")
)

// singleAgentModes 受支持的四个权威单 Agent 模式，顺序即产品顺序。
// Python: mode_catalog.py:57-84 _SINGLE_AGENT_MODES
var singleAgentModes = []RuntimeModeDescriptor{
	{
		Mode:                           schema.ModeAgentWorkNormal.String(),
		WorkMode:                       "work",
		IsPlan:                         false,
		SupportsCustomAgentDefinitions: true,
	},
	{
		Mode:                           schema.ModeAgentWorkPlan.String(),
		WorkMode:                       "work",
		IsPlan:                         true,
		SupportsCustomAgentDefinitions: true,
	},
	{
		Mode:                           schema.ModeAgentCodeNormal.String(),
		WorkMode:                       "code",
		IsPlan:                         false,
		SupportsCustomAgentDefinitions: true,
	},
	{
		Mode:                           schema.ModeAgentCodePlan.String(),
		WorkMode:                       "code",
		IsPlan:                         true,
		SupportsCustomAgentDefinitions: true,
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ListModeCapabilities 只返回权威单 Agent 模式，顺序稳定。
// Python: mode_catalog.py:87-90 list_mode_capabilities
func ListModeCapabilities() ModeCatalogResult {
	out := make([]RuntimeModeDescriptor, len(singleAgentModes))
	copy(out, singleAgentModes)
	return ModeCatalogResult{Modes: out}
}

// ResolveModeCapability 解析权威模式，并保留受支持的遗留别名。
// Python: mode_catalog.py:92-107 resolve_mode_capability
//
// 步骤逐条对应上游：
//  1. normalized = deprecate_mode(requested)  → schema.CanonicalizeMode
//     （含 modeAliases 与 DEPRECATION_MAP；空值/空白原样返回，不回落到默认模式）
//  2. target = normalized.strip() if isinstance(normalized, str) else ""
//  3. 空 → BAD_REQUEST
//  4. 在 _SINGLE_AGENT_MODES 中精确匹配
//  5. 未命中 → NOT_FOUND
func ResolveModeCapability(requested any) (RuntimeModeDescriptor, error) {
	// 步骤 1-2：deprecate_mode + 取字符串并去空白
	normalized := ""
	if raw, ok := requested.(string); ok {
		normalized = schema.DeprecateMode(raw)
	}
	target := strings.TrimSpace(normalized)

	// 步骤 3：空模式
	if target == "" {
		return RuntimeModeDescriptor{}, ErrModeRequired
	}

	// 步骤 4：精确匹配
	for _, descriptor := range singleAgentModes {
		if descriptor.Mode == target {
			return descriptor, nil
		}
	}

	// 步骤 5：未命中
	return RuntimeModeDescriptor{}, ErrModeNotFound
}

// ModeCatalogResultToDict 返回机器协议表示。
// Python: mode_catalog.py:50-56 ModeCatalogResult.to_dict
func ModeCatalogResultToDict(result ModeCatalogResult) map[string]any {
	modes := make([]map[string]any, 0, len(result.Modes))
	for _, descriptor := range result.Modes {
		modes = append(modes, RuntimeModeDescriptorToDict(descriptor))
	}
	return map[string]any{"modes": modes}
}

// RuntimeModeDescriptorToDict 返回单模式的机器协议表示。
// Python: mode_catalog.py:39-47 RuntimeModeDescriptor.to_dict
func RuntimeModeDescriptorToDict(descriptor RuntimeModeDescriptor) map[string]any {
	return map[string]any{
		"mode":                              descriptor.Mode,
		"work_mode":                         descriptor.WorkMode,
		"is_plan":                           descriptor.IsPlan,
		"supports_custom_agent_definitions": descriptor.SupportsCustomAgentDefinitions,
	}
}
