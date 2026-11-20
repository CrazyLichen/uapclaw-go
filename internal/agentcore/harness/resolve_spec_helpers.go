package harness

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// parentModelExtrasKey 父 agent 解析出的模型在 extras 里的键。
	//
	// core.subagent.* / progressive_tool 工厂靠它读取父模型，
	// 与上游 `build_ctx.extras["_parent_model"] = llm_model` 同键。
	parentModelExtrasKey = "_parent_model"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// buildModelFromSpec 从 hschema.TeamModelConfig 构造模型实例。
//
// 构造逻辑已收敛到 schema 的 TeamModelConfig.Build
// （Python deep_agent_spec.py:127-132 ModelSpec.build），此处仅保留 nil 语义并转调。
func buildModelFromSpec(spec *hschema.TeamModelConfig) *llm.Model {
	if spec == nil {
		return nil
	}
	return spec.Build()
}

// deriveBuildContext 衍生每成员构建上下文（回填 workspace / card id）。
//
// 上游 derive 后**重新绑定 extras**（浅拷贝会让多个成员共享同一 map）。
// ctx 为 nil 时自建最小载体。
//
// Python: deep_agent_spec.py:550-565
func deriveBuildContext(
	ctx hschema.BuildContextInterface, language string, ws *workspace.Workspace, memberCardID string,
) hschema.BuildContextInterface {
	if ctx == nil {
		return &hschema.BuildContext{
			Language:     language,
			Workspace:    ws,
			MemberCardID: memberCardID,
		}
	}
	derived := ctx.Derive(hschema.BuildContextOverrides{
		Language:     hschema.StringPtr(language),
		HasWorkspace: true,
		Workspace:    ws,
		MemberCardID: hschema.StringPtr(memberCardID),
	})
	// 重新绑定 extras：derive 是浅拷贝，不重绑会让成员之间共享句柄字典。
	// Python: build_ctx.extras = dict(build_ctx.extras)（:558）
	rebound := make(map[string]any, len(derived.GetExtras()))
	for k, v := range derived.GetExtras() {
		rebound[k] = v
	}
	// 用具体类型承载，便于后续 GetExtras 直接写入
	if concrete, ok := derived.(*hschema.BuildContext); ok {
		concrete.Extras = rebound
		return concrete
	}
	// 子类：通过 GetExtras 覆写内容（GetExtras 返回内部 map）
	derived.GetExtras()
	target := derived.GetExtras()
	for k := range target {
		delete(target, k)
	}
	for k, v := range rebound {
		target[k] = v
	}
	return derived
}

// resolveRailsFromSpecs 逐个解析 rail 声明并展平。
//
// Python: :581-587 的 rails.extend(_as_built_list(rail_spec.build(...)))
func resolveRailsFromSpecs(
	specs []hschema.RailSpec, language string, ws *workspace.Workspace, ctx hschema.BuildContextInterface,
) ([]agentinterfaces.AgentRail, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]agentinterfaces.AgentRail, 0, len(specs))
	for _, railSpec := range specs {
		built, err := railSpec.Build(language, ws, ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve parts: rail %q: %w", railSpec.Type, err)
		}
		out = append(out, flattenBuiltRails(built)...)
	}
	return out, nil
}

// flattenBuiltRails 把 provider 返回值展平成 rail 列表。
//
// 对应上游 _as_built_list：provider 可能返回单个 rail、rail 列表或 nil。
func flattenBuiltRails(built any) []agentinterfaces.AgentRail {
	switch v := built.(type) {
	case nil:
		return nil
	case agentinterfaces.AgentRail:
		return []agentinterfaces.AgentRail{v}
	case []agentinterfaces.AgentRail:
		return v
	case []any:
		out := make([]agentinterfaces.AgentRail, 0, len(v))
		for _, item := range v {
			if r, ok := item.(agentinterfaces.AgentRail); ok {
				out = append(out, r)
			}
		}
		return out
	default:
		logger.Warn(logger.ComponentAgentCore).
			Str("built_type", fmt.Sprintf("%T", built)).
			Msg("resolve parts: rail provider returned unexpected type, skipping")
		return nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolvedSpecTools 工具声明的解析产物：实例进 ToolInstances（注册
// resource_mgr），透传卡片进 ToolCards（AbilityManager schema 注册）。
type resolvedSpecTools struct {
	// instances 工具实例列表
	instances []tool.Tool
	// cards provider 产出的工具卡片（非实例形态）
	cards []*tool.ToolCard
}

// resolveBuiltinToolsFromSpecs 逐项解析 BuiltinToolSpec 声明。
//
// Python: DeepAgentSpec._resolve_tools (deep_agent_spec.py:506-522)：
// `tid = f"{tool_id_prefix}.{item.type}" if tool_id_prefix else None`，
// `resolved.extend(_as_built_list(item.build(language=..., tool_id=tid, ...)))`。
// 空列表返回零值（对齐 `if not self.tools: return None`）。
//
// toolIDPrefix：DeepAgentSpec 侧传 card.id（:602），SubAgentSpec 侧传
// card.id 或 card.name 兜底（agent_specs.go 已同款处理）。
func resolveBuiltinToolsFromSpecs(
	specs []hschema.BuiltinToolSpec, language string, toolIDPrefix string, ctx hschema.BuildContextInterface,
) (*resolvedSpecTools, error) {
	if len(specs) == 0 {
		return &resolvedSpecTools{}, nil
	}
	out := &resolvedSpecTools{}
	for i := range specs {
		item := &specs[i]
		tid := ""
		if toolIDPrefix != "" {
			tid = toolIDPrefix + "." + item.Type
		}
		built, err := item.Build(language, tid, ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve parts: tool %q: %w", item.Type, err)
		}
		// _as_built_list 摊平：provider 可能返回单个工具、工具列表或 nil
		switch v := built.(type) {
		case nil:
		case tool.Tool:
			out.instances = append(out.instances, v)
		case []any:
			for _, item2 := range v {
				switch t2 := item2.(type) {
				case nil:
				case tool.Tool:
					out.instances = append(out.instances, t2)
				case *tool.ToolCard:
					out.cards = append(out.cards, t2)
				default:
					return nil, fmt.Errorf("builtin tool spec %q built unexpected element type %T", item.Type, item2)
				}
			}
		case *tool.ToolCard:
			out.cards = append(out.cards, v)
		default:
			return nil, fmt.Errorf("builtin tool spec %q built unexpected type %T", item.Type, built)
		}
	}
	return out, nil
}

// buildSubagentsFromSpecs 逐项物化子代理声明。
//
// Python: :588-596 的
// `subagents.extend(_as_built_list(subagent_spec.build(parent_model=..., language=..., context=...)))`。
// factory_name 命中注册表时由 provider 直接产出（SubAgentSpec.Build 内部
// 分派），未命中走正常路径；nil 产物跳过（_as_built_list None 语义）。
func buildSubagentsFromSpecs(
	specs []hschema.SubAgentSpec, parentModel *llm.Model, language string, ctx hschema.BuildContextInterface,
) ([]hschema.SubagentSpec, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]hschema.SubagentSpec, 0, len(specs))
	for i := range specs {
		built, err := specs[i].Build(parentModel, language, ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve parts: subagent %q: %w", specs[i].FactoryName, err)
		}
		if built != nil {
			out = append(out, built)
		}
	}
	return out, nil
}
