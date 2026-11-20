package schema

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RailSpec 约束规则的声明式引用，经 provider 注册表解析。
//
// Type 是注册名（如 "core.team.policy"）而非类名；Params 原样透传给 provider。
// 这样 spec 可完整序列化（checkpoint 持久化），构造方式由 provider 决定。
//
// Python: RailSpec (openjiuwen/harness/schema/deep_agent_spec.py:248-293)
type RailSpec struct {
	// Type 规则类型（provider 注册名）
	Type string `json:"type"`
	// Params 规则参数（透传给 provider）
	Params map[string]any `json:"params,omitempty"`
}

// BuiltinToolSpec 内置工具的声明式引用，经 tool provider 注册表解析。
//
// ⚠️ tool provider 注册表尚未落地（⤵️ 领域十五），本类型的 Build 目前
// 恒返回 (nil, nil)。签名先定形，注册表就绪后只换函数体。
//
// Python: BuiltinToolSpec (openjiuwen/harness/schema/deep_agent_spec.py:296-337)
type BuiltinToolSpec struct {
	// Type 工具类型（provider 注册名）
	Type string `json:"type"`
	// Params 工具参数（透传给 provider）
	Params map[string]any `json:"params,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Build 通过已注册的 provider 构造 rail 实例（或列表、或 nil）。
//
// 步骤严格对齐上游（:278-293）：
//  1. 先确保内置元素已注册（上游 ensure_builtin_elements_registered）
//  2. 查表；查不到 → warn + 返回 (nil, nil)
//  3. ctx 为 nil → 自建最小上下文（含 language + workspace）
//  4. 调 provider(params, ctx)
//
// 未知 type 返回 (nil, nil) 而非 error：老版本持久化的 spec 可能引用
// 已删除的 rail，不能因此中断整个构建——这是上游的明确语义。
//
// Python: RailSpec.build (deep_agent_spec.py:254-293)
func (s RailSpec) Build(
	language string, ws *workspace.Workspace, ctx BuildContextInterface,
) (any, error) {
	// 步骤 1：确保内置元素已注册
	// Python: ensure_builtin_elements_registered()（:280）
	//
	// 本仓无 manifest catalog，内置 rail 仍由 CreateDeepAgent 的默认集合补齐；
	// 此处的意图是"保证注册前必有一次同步"，实际注册由各包显式完成。
	ensureBuiltinElementsRegisteredHook()

	// 步骤 2：查表
	provider, ok := LookupRailProvider(s.Type)
	if !ok {
		// Python: logger.warning("Unknown rail type ...; skipping it.")（:283-289）
		logRailTypeUnknown(s.Type)
		return nil, nil
	}

	// 步骤 3：ctx 为 nil 时自建最小上下文
	// Python: if context is None: context = BuildContext(...)（:291-292）
	if ctx == nil {
		ctx = newMinimalBuildContext(language, ws)
	}

	// 步骤 4：调 provider
	// Python: return factory(dict(self.params), context)（:293）
	return provider(s.Params, ctx)
}

// Build 通过已注册的 tool provider 构造工具实例（或列表）。
//
// 步骤对齐上游（deep_agent_spec.py:326-340）：
//  1. 确保内置元素已注册（ensure_builtin_elements_registered）
//  2. 查表；查不到 → **报错**（与 RailSpec 的 warn+skip 不同：上游这里是
//     raise ValueError，因为缺工具会让成员能力静默残缺，属构造错误）
//  3. ctx 为 nil → 自建最小上下文（仅 language，对齐 :337-338）
//  4. 调 provider(dict(params), ctx)
//
// 返回 any：provider 可能返回单个工具、工具列表（调用方 _as_built_list 摊平）。
//
// Python: BuiltinToolSpec.build (deep_agent_spec.py:307-340)
func (s BuiltinToolSpec) Build(
	language string, toolID string, ctx BuildContextInterface,
) (any, error) {
	// 步骤 1：确保内置元素已注册
	// Python: ensure_builtin_elements_registered()（:328）
	ensureBuiltinElementsRegisteredHook()

	// 步骤 2：查表
	// Python: factory = _TOOL_PROVIDER_REGISTRY.get(self.type)（:329）
	provider, ok := LookupToolProvider(s.Type)
	if !ok {
		// Python: raise ValueError(f"Unknown tool type '{self.type}'. "
		//   f"Registered types: {list(_TOOL_PROVIDER_REGISTRY)}")（:330-334）
		return nil, fmt.Errorf("Unknown tool type '%s'. Registered types: %v", s.Type, sortedToolTypeNames())
	}

	// 步骤 3：ctx 为 nil 时自建最小上下文（仅 language，对齐 :337-338）
	if ctx == nil {
		ctx = &BuildContext{Language: language}
	}

	// 步骤 4：调 provider
	// Python: return factory(dict(self.params), context)（:339-340）
	//
	// toolID 形参：上游 build 签名无该参数（调用方 :402 组装
	// tid = f"{sa_prefix}.{item.type}" 但不传给 factory）；Go 保留形参
	// 是为签约定形（对齐既有注释说明），与上游一致不透传 provider。
	_ = toolID
	return provider(s.Params, ctx)
}

// ValidateRailSpecs 校验一组 rail 声明（类型名非空）。
//
// 供 ResolveParts 早期失败用：空 type 是构造错误，不该静默跳过
// （上游 pydantic 的 required 字段保证了这一点）。
func ValidateRailSpecs(specs []RailSpec) error {
	for i, s := range specs {
		if s.Type == "" {
			return fmt.Errorf("rail spec[%d]: type is required", i)
		}
	}
	return nil
}
