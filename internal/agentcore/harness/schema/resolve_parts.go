package schema

import (
	"fmt"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
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

// ResolveParts 组装本 spec 的 DeepAgent 装配产物，不创建实例。
//
// 这是 build 的"provider 解析那一半"：解析模型、rails（经 provider +
// 每成员 BuildContext）、子代理、工具与 sys_operation，然后委托工厂侧
// 完成 config 组装。
//
// 七步严格对齐上游（openjiuwen/harness/schema/deep_agent_spec.py:542-634）：
//  1. 模型 / 语言 / 视觉 / 音频 / workspace / card id 逐个解析
//  2. 衍生 build_ctx（回填 workspace + card id + moa=nil），ctx 为 nil 则自建
//  3. build_ctx.extras["_parent_model"] = 模型（子代理工厂要读）
//  4. sys_operation 解析 —— **必须在 rails / subagents 之前**
//  5. rails：逐个 RailSpec.Build(language, workspace, build_ctx) 并展平
//  6. subagents：逐个构造（⤵️ 领域十五，本版直接透传 spec 列表）
//  7. 委托工厂侧完成 config 组装
//
// Python: DeepAgentSpec.resolve_parts (deep_agent_spec.py:524-634)
func (s *DeepAgentSpec) ResolveParts(ctx BuildContextInterface) (*DeepAgentParts, error) {
	// 步骤 1：逐项解析
	// Python: llm_model = self.model.build() if self.model else None（:542）
	llmModel := buildModelFromSpec(s.Model)

	// Python: language = resolve_language(self.language)（:543）
	language := resolveLanguage(s.Language)

	// Python: :545-548
	visionConfig := s.VisionModel
	audioConfig := s.AudioModel
	var workspaceObj *workspace.Workspace
	if s.Workspace != nil {
		workspaceObj = &workspace.Workspace{
			RootPath: s.Workspace.RootPath,
			Language: language,
		}
	}
	memberCardID := ""
	if s.Card != nil {
		memberCardID = s.Card.GetID()
	}

	// 步骤 2：衍生 build_ctx
	// Python: build_ctx = context.derive(workspace=..., member_card_id=..., moa=None)
	//         build_ctx.extras = dict(build_ctx.extras)（:550-565）
	//
	// extras 必须重新绑定一层：derive 是浅拷贝，不重绑会让成员之间
	// 共享同一个 extras map（上游注释："rebind extras because derive() is a shallow copy"）。
	buildCtx := deriveBuildContext(ctx, language, workspaceObj, memberCardID)

	// 步骤 3：父模型进 extras
	// Python: build_ctx.extras["_parent_model"] = llm_model（:569）
	// 无模型时值可为 nil，但**键必须存在**。
	buildCtx.GetExtras()[parentModelExtrasKey] = llmModel

	// 步骤 4：sys_operation（先于 rails / subagents）
	//
	// Python: sys_operation = self.sys_operation.resolve() if ... else None（:578）
	// 上游注释解释了顺序理由：不带 sys_operation 的子代理会从 create_deep_agent
	// 拿到一个全新的 LOCAL 实例，既逃出本 agent 的沙箱，又把 restrict_to_sandbox
	// 对着自己更窄的 workspace 打开。
	sysOperation, err := resolveSysOperationSpec(s.SysOperation)
	if err != nil {
		return nil, fmt.Errorf("resolve parts: sys_operation: %w", err)
	}
	buildCtx.GetExtras()[ParentSysOperationExtrasKey] = sysOperation

	// 步骤 5：rails 经 provider 解析
	// Python: :581-587
	resolvedRails, err := resolveRailsFromSpecs(s.Rails, language, workspaceObj, buildCtx)
	if err != nil {
		return nil, err
	}

	// 步骤 6：subagents
	//
	// ⤵️ 领域十五：SubAgentSpec 的 provider 注册表未落地，本版直接透传
	// spec 列表（Registry 侧再决定如何物化）。上游此处逐个
	// subagent_spec.build(parent_model=..., language=..., context=build_ctx)。
	subagents := s.Subagents

	// 步骤 7：委托工厂侧完成 config 组装
	return resolveDeepAgentPartsFromSpec(s, llmModel, language, workspaceObj,
		visionConfig, audioConfig, sysOperation, resolvedRails, subagents)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildModelFromSpec 从 TeamModelConfig 构造模型实例。
//
// 构造失败记 warn 并返回 nil（与上游"模型缺失时 agent 仍可构造"一致，
// 只是后续 run 会失败）。
func buildModelFromSpec(spec *TeamModelConfig) *llm.Model {
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
	ctx BuildContextInterface, language string, ws *workspace.Workspace, memberCardID string,
) BuildContextInterface {
	if ctx == nil {
		return &BuildContext{
			Language:     language,
			Workspace:    ws,
			MemberCardID: memberCardID,
		}
	}
	derived := ctx.Derive(BuildContextOverrides{
		Language:     StringPtr(language),
		HasWorkspace: true,
		Workspace:    ws,
		MemberCardID: StringPtr(memberCardID),
	})
	// 重新绑定 extras：derive 是浅拷贝，不重绑会让成员之间共享句柄字典。
	// Python: build_ctx.extras = dict(build_ctx.extras)（:558）
	rebound := make(map[string]any, len(derived.GetExtras()))
	for k, v := range derived.GetExtras() {
		rebound[k] = v
	}
	// 用具体类型承载，便于后续 GetExtras 直接写入
	if concrete, ok := derived.(*BuildContext); ok {
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
	specs []RailSpec, language string, ws *workspace.Workspace, ctx BuildContextInterface,
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

// ──────────────────────────── 语言解析 ────────────────────────────

// supportedLanguages 支持的语言列表（与 prompts 包保持一致）。
//
// 在 schema 包重复一份而非 import prompts：prompts → schema 是既有方向，
// 反向引用会成环。这里只复制两个纯数据常量与一段无依赖的判定逻辑。
var supportedLanguages = []string{"cn", "en"}

// supportedLanguageEnv 环境变量覆盖语言的键名。
// Python: AGENT_PROMPT_LANGUAGE
const supportedLanguageEnv = "AGENT_PROMPT_LANGUAGE"

// resolveLanguage 解析语言配置，返回合法语言码或默认值。
//
// 与 prompts.ResolveLanguage 行为一致（同一套判定与同一环境变量）；
// 在 schema 包内实现是为了打破 schema → prompts → schema 的环。
//
// Python: resolve_language()
func resolveLanguage(configLanguage string) string {
	if configLanguage != "" && isSupportedLanguage(configLanguage) {
		return configLanguage
	}
	if envLang := os.Getenv(supportedLanguageEnv); isSupportedLanguage(envLang) {
		return envLang
	}
	return DefaultLanguage
}

// isSupportedLanguage 检查语言是否在支持列表中。
func isSupportedLanguage(lang string) bool {
	for _, supported := range supportedLanguages {
		if lang == supported {
			return true
		}
	}
	return false
}
