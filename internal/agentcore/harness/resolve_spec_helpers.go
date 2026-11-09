package harness

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
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
// 构造失败记 warn 并返回 nil（与上游"模型缺失时 agent 仍可构造"一致，
// 只是后续 run 会失败）。
func buildModelFromSpec(spec *hschema.TeamModelConfig) *llm.Model {
	if spec == nil {
		return nil
	}
	cfg := spec.ModelClientConfig
	m, err := llm.NewModel(&cfg, spec.ModelRequestConfig)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).Err(err).
			Str("client_provider", cfg.ClientProvider).
			Msg("resolve parts: model build failed, agent will have no model")
		return nil
	}
	return m
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
