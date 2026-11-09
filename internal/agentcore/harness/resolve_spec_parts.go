package harness

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveDeepAgentSpecParts 组装 spec 的 DeepAgent 装配产物，不创建实例。
//
// 这是 spec 侧的解析入口，与 ResolveDeepAgentParts **共用同一份实现**：
// 先做 spec 特有的 7 步解析，再把结果摊成 CreateDeepAgentParams 转调
// ResolveDeepAgentParts。默认 rails 的计算、config 组装、工具规范化
// 全走同一处，两条路径不会有行为分叉。
//
// 上游是 DeepAgentSpec.resolve_parts 方法里的
// `from openjiuwen.harness.factory import resolve_deep_agent_parts` +
// `return resolve_deep_agent_parts(...)`（deep_agent_spec.py:539, :598-634）。
// Go 无「函数内延迟导入」且 schema 不能 import harness（成环），因此
// 改为 harness 包的包级函数（方法归属偏差，见 spec 登记）。
//
// Python: DeepAgentSpec.resolve_parts (deep_agent_spec.py:524-634)
func ResolveDeepAgentSpecParts(spec *hschema.DeepAgentSpec, ctx hschema.BuildContextInterface) (*hschema.DeepAgentParts, error) {
	if spec == nil {
		return nil, fmt.Errorf("resolve spec parts: spec is nil")
	}

	// 步骤 1：逐项解析
	// Python: llm_model = self.model.build() if self.model else None（:542）
	llmModel := buildModelFromSpec(spec.Model)
	// Python: language = resolve_language(self.language)（:543）
	language := prompts.ResolveLanguage(spec.Language)

	// Python: :545-548
	var workspaceObj *workspace.Workspace
	if spec.Workspace != nil {
		workspaceObj = &workspace.Workspace{
			RootPath: spec.Workspace.RootPath,
			Language: language,
		}
	}
	memberCardID := ""
	if spec.Card != nil {
		memberCardID = spec.Card.GetID()
	}

	// 步骤 2：衍生 build_ctx（不污染调用方、重绑 extras）
	// Python: :550-565
	buildCtx := deriveBuildContext(ctx, language, workspaceObj, memberCardID)

	// 步骤 3：父模型进 extras（键必须存在，值可为 nil）
	// Python: build_ctx.extras["_parent_model"] = llm_model（:569）
	buildCtx.GetExtras()[parentModelExtrasKey] = llmModel

	// 步骤 4：sys_operation（先于 rails / subagents）
	// Python: :578
	sysOperation, err := resolveSysOperationSpec(spec.SysOperation)
	if err != nil {
		return nil, fmt.Errorf("resolve spec parts: sys_operation: %w", err)
	}
	buildCtx.GetExtras()[hschema.ParentSysOperationExtrasKey] = sysOperation

	// 步骤 5：rails 经 provider 解析
	// Python: :581-587
	resolvedRails, err := resolveRailsFromSpecs(spec.Rails, language, workspaceObj, buildCtx)
	if err != nil {
		return nil, err
	}

	// 步骤 6：subagents
	// ⤵️ 领域十五：SubAgentSpec provider 未落地，本版只做字段搬运。
	subagents := convertToConfigSubagents(spec.Subagents)

	// 步骤 7：摊成 CreateDeepAgentParams，转调共用的 ResolveDeepAgentParts。
	// 上游 :598-634 就是把 self 的字段逐项当关键字参数传进去。
	params := specToParams(spec, llmModel, language, workspaceObj, sysOperation,
		resolvedRails, subagents, buildCtx)

	parts, err := ResolveDeepAgentParts(params)
	if err != nil {
		return nil, fmt.Errorf("resolve spec parts: %w", err)
	}
	return parts, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// specToParams 把 DeepAgentSpec 摊成 CreateDeepAgentParams。
//
// 逐字段对齐上游 deep_agent_spec.py:598-634 的关键字参数列表。
// 默认 rails 相关开关（EnableModelAnomalyDetectionRail / ModelSelection /
// TodoModifyFactory / RestrictToWorkDir 等）在这里一并搬运，使 spec 路径
// 与 create_deep_agent 路径行为完全一致。
func specToParams(
	spec *hschema.DeepAgentSpec,
	llmModel *llm.Model,
	language string,
	ws *workspace.Workspace,
	sysOperation sysop.SysOperation,
	rails []agentinterfaces.AgentRail,
	subagents []hschema.SubagentSpec,
	buildCtx hschema.BuildContextInterface,
) *harness_config.CreateDeepAgentParams {
	params := &harness_config.CreateDeepAgentParams{
		Model:                  llmModel,
		Card:                   spec.Card,
		SystemPrompt:           spec.SystemPrompt,
		ToolCards:              spec.ToolCards,
		Mcps:                   spec.Mcps,
		Rails:                  rails,
		EnableTaskLoop:         spec.EnableTaskLoop,
		EnableAsyncSubagent:    spec.EnableAsyncSubagent,
		AddGeneralPurposeAgent: spec.AddGeneralPurposeAgent,
		MaxIterations:          spec.MaxIterations,
		Workspace:              ws,
		Skills:                 spec.Skills,
		EnableSkillDiscovery:   spec.EnableSkillDiscovery,
		Language:               language,
		EnableTaskPlanning:     spec.EnableTaskPlanning,
		AutoCreateWorkspace:    spec.AutoCreateWorkspace,
		CompletionTimeout:      spec.CompletionTimeout,
		Subagents:              subagents,
	}
	params.SysOperation = sysOperation
	if spec.RestrictToSandbox {
		v := false
		params.RestrictToWorkDir = &v
	}
	if spec.PromptMode != "" {
		if pm, err := hschema.ParsePromptMode(spec.PromptMode); err == nil {
			params.PromptMode = pm
		} else {
			logger.Warn(logComponent).Err(err).
				Str("prompt_mode", spec.PromptMode).
				Msg("resolve spec parts: invalid prompt_mode, using default")
		}
	}
	if spec.VisionModel != nil {
		params.VisionModelConfig = &hschema.VisionModelConfig{
			APIKey: spec.VisionModel.APIKey, BaseURL: spec.VisionModel.BaseURL,
			Model: spec.VisionModel.Model, MaxRetries: spec.VisionModel.MaxRetries,
		}
	}
	if spec.AudioModel != nil {
		params.AudioModelConfig = &hschema.AudioModelConfig{
			APIKey: spec.AudioModel.APIKey, BaseURL: spec.AudioModel.BaseURL,
			TranscriptionModel: spec.AudioModel.TranscriptionModel,
			QAModel:            spec.AudioModel.QAModel, MaxRetries: spec.AudioModel.MaxRetries,
		}
	}
	// buildCtx 里的平台句柄已由 provider 在步骤 5 读取；此处无需再传
	_ = buildCtx
	return params
}
