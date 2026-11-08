package schema

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSysOperationSpec 把 SysOperationSpec 解析成活实例。
//
// 上游是 get-or-create（同一稳定 id 复用同一实例）；该语义由资源管理器
// 与调用方保证，这里只负责按 mode 构造。
//
// WorkConfig / GatewayConfig 在 Go 侧仍是 any 占位（见 SysOperationSpec 注释），
// 因此按 mode 分支取用：Local 看 WorkConfig，Sandbox 看 GatewayConfig。
func resolveSysOperationSpec(spec *SysOperationSpec) (sysop.SysOperation, error) {
	if spec == nil {
		return nil, nil
	}

	var mode sysop.OperationMode
	switch spec.Mode {
	case "local", "":
		mode = sysop.OperationModeLocal
	case "sandbox":
		mode = sysop.OperationModeSandbox
	default:
		return nil, fmt.Errorf("unknown sys_operation mode %q", spec.Mode)
	}

	opts := []sysop.SysOperationCardOption{sysop.WithSysOpMode(mode)}
	if spec.ID != "" {
		opts = append(opts, sysop.WithSysOpCustomID(spec.ID))
	}
	// 运行配置透传：Local 用 WorkConfig，Sandbox 用 GatewayConfig
	switch mode {
	case sysop.OperationModeLocal:
		if cfg, ok := spec.WorkConfig.(*sysop.LocalWorkConfig); ok {
			opts = append(opts, sysop.WithSysOpWorkConfig(cfg))
		}
	case sysop.OperationModeSandbox:
		if cfg, ok := spec.GatewayConfig.(*sysop.SandboxGatewayConfig); ok {
			opts = append(opts, sysop.WithSysOpGatewayConfig(cfg))
		}
	}

	card := sysop.NewSysOperationCard(opts...)
	return sysop.NewSysOperation(card)
}

// resolveDeepAgentPartsFromSpec 把 spec 摊成 config + 工具，产出 parts。
//
// 对应上游 resolve_parts 末尾的 resolve_deep_agent_parts(...) 调用
// （deep_agent_spec.py:598-634）：把逐项解析结果汇总成 DeepAgentParts。
//
// ⚠️ 偏差（并入 D-9）：上游此处还会算默认 rails 并放进 parts；
// Go 侧默认 rails 在 harness 包 apply 阶段补齐（addDefaultRails 在 harness 包，
// schema 引用它会让 schema→rails→prompts→schema 成环）。
// 最终 agent 上挂的 rail 集合完全一致。
func resolveDeepAgentPartsFromSpec(
	spec *DeepAgentSpec,
	llmModel *llm.Model,
	language string,
	ws *workspace.Workspace,
	visionConfig *VisionModelSpec,
	audioConfig *AudioModelSpec,
	sysOperation sysop.SysOperation,
	rails []agentinterfaces.AgentRail,
	subagents []SubAgentSpec,
) (*DeepAgentParts, error) {
	// 组装配置
	config := NewDeepAgentConfig()
	config.Model = llmModel
	config.Card = spec.Card
	config.SystemPrompt = spec.SystemPrompt
	config.EnableTaskLoop = spec.EnableTaskLoop
	config.EnableAsyncSubagent = spec.EnableAsyncSubagent
	config.AddGeneralPurposeAgent = spec.AddGeneralPurposeAgent
	config.MaxIterations = spec.MaxIterations
	config.Workspace = ws
	config.Skills = spec.Skills
	config.EnableSkillDiscovery = spec.EnableSkillDiscovery
	config.SysOperation = sysOperation
	config.Language = language
	config.EnablePlanMode = spec.EnableTaskPlanning
	config.AutoCreateWorkspace = spec.AutoCreateWorkspace
	if spec.CompletionTimeout > 0 {
		config.CompletionTimeout = spec.CompletionTimeout
	}
	if spec.PromptMode != "" {
		if pm, err := ParsePromptMode(spec.PromptMode); err == nil {
			config.PromptMode = pm
		} else {
			logger.Warn(logger.ComponentAgentCore).Err(err).
				Str("prompt_mode", spec.PromptMode).
				Msg("resolve parts: invalid prompt_mode, using default")
		}
	}
	if visionConfig != nil {
		config.VisionModelConfig = &VisionModelConfig{
			APIKey:     visionConfig.APIKey,
			BaseURL:    visionConfig.BaseURL,
			Model:      visionConfig.Model,
			MaxRetries: visionConfig.MaxRetries,
		}
	}
	if audioConfig != nil {
		config.AudioModelConfig = &AudioModelConfig{
			APIKey:             audioConfig.APIKey,
			BaseURL:            audioConfig.BaseURL,
			TranscriptionModel: audioConfig.TranscriptionModel,
			QAModel:            audioConfig.QAModel,
			MaxRetries:         audioConfig.MaxRetries,
		}
	}
	if spec.Card == nil {
		config.Card = agentschema.NewAgentCard(
			agentschema.WithAgentName("deep_agent"),
			agentschema.WithAgentDescription("DeepAgent instance"),
		)
	}
	if len(spec.Mcps) > 0 {
		config.Mcps = spec.Mcps
	}
	if len(subagents) > 0 {
		config.Subagents = convertToConfigSubagents(subagents)
	}

	// 工具：spec 声明的 ToolCards 直接透传（Tool 实例由 BuiltinTools 解析，
	// ⤵️ 领域十五 tool provider 就绪后追加）
	toolCards := make([]*tool.ToolCard, 0, len(spec.ToolCards))
	toolCards = append(toolCards, spec.ToolCards...)
	if len(toolCards) > 0 {
		config.Tools = toolCards
	}

	return &DeepAgentParts{
		Config:    config,
		Rails:     rails,
		ToolCards: toolCards,
	}, nil
}

// convertToConfigSubagents 把 spec 形态的子代理转成配置形态。
//
// SubagentSpec 是只要求 SpecName() 的窄接口（config.go:26-29），
// 这里用 specSubagentAdapter 包一层满足它。
//
// ⤵️ 领域十五：上游此处由 SubAgentSpec.build 产出 SubAgentConfig；
// 本版只做字段搬运，provider 注册表就绪后替换为 provider 调用。
func convertToConfigSubagents(specs []SubAgentSpec) []SubagentSpec {
	out := make([]SubagentSpec, 0, len(specs))
	for i := range specs {
		out = append(out, &specSubagentAdapter{spec: &specs[i]})
	}
	return out
}

// specSubagentAdapter 把 SubAgentSpec 适配成 config 侧的 SubagentSpec 接口。
type specSubagentAdapter struct {
	// spec 被适配的子代理规格
	spec *SubAgentSpec
}

// SpecName 返回规格名称（取卡片名，无卡片时用工厂名兜底）。
func (a *specSubagentAdapter) SpecName() string {
	if a.spec == nil {
		return ""
	}
	if a.spec.AgentCard != nil && a.spec.AgentCard.GetName() != "" {
		return a.spec.AgentCard.GetName()
	}
	return a.spec.FactoryName
}
