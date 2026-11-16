// 诊断 agent 运行时：有界的只读 DeepAgent 构建与调用（17.4b2 Task 10）。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/agent_runtime.py（151 行）
//
// 本模块拥有模型构建、只读工具策略与瞬时调用重试；证据准备与病历编译
// 刻意留在运行时之外，诊断 agent 不因此成为工作流控制器。
package evaluationresultanalyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	hsconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	harnessconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"

	"github.com/google/uuid"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluator"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DiagnosisAgentExecutionError agent 返回执行失败而非诊断答案。
// Python: agent_runtime.py:34-39 DiagnosisAgentExecutionError(RuntimeError)
type DiagnosisAgentExecutionError struct {
	// message 错误消息
	message string
	// terminationReason 终止原因
	terminationReason string
}

// DiagnosisBudgetRail 在既有预算的最后一轮为答案保留，而不是继续调查。
// Python: agent_runtime.py:41-64 _DiagnosisBudgetRail(AgentRail)
type DiagnosisBudgetRail struct {
	interfaces.BaseRail
	// maxIterations 最大迭代次数
	maxIterations int
}

// DiagnosisAgentRuntime 创建只读 DeepAgent，但不拥有诊断策略。
// Python: agent_runtime.py:66-115 DiagnosisAgentRuntime
type DiagnosisAgentRuntime struct {
	// config 评估结果分析器配置
	config *config.EvaluationResultAnalyzerConfig
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// diagnosisModelTurnExtraKey 预算轨轮次计数键（Python _diagnosis_model_turn）
	diagnosisModelTurnExtraKey = "_diagnosis_model_turn"
)

// diagnosisBashDenyPatterns 诊断 agent bash 拒绝模式（Python _DIAGNOSIS_BASH_DENY_PATTERNS，逐字保留）
var diagnosisBashDenyPatterns = []string{
	`(?:^|[;&|]\s*)(?:find|du|ls|tree)\s+/(?:\s|$)`,
	`(?:^|\s)\.\.(?:[/\\]|\s|$)`,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDiagnosisAgentExecutionError 构造执行失败错误。
// Python: __init__(message, *, termination_reason="")
func NewDiagnosisAgentExecutionError(message string, terminationReason string) *DiagnosisAgentExecutionError {
	return &DiagnosisAgentExecutionError{message: message, terminationReason: terminationReason}
}

// Error 实现 error 接口。
func (e *DiagnosisAgentExecutionError) Error() string {
	return e.message
}

// TerminationReason 返回终止原因。
func (e *DiagnosisAgentExecutionError) TerminationReason() string {
	return e.terminationReason
}

// NewDiagnosisBudgetRail 创建预算轨。
// Python: __init__(max_iterations)
func NewDiagnosisBudgetRail(maxIterations int) *DiagnosisBudgetRail {
	return &DiagnosisBudgetRail{maxIterations: maxIterations}
}

// BeforeModelCall 模型调用前：计轮次、注入进度提示，末轮收走工具并要求出答案。
// Python: agent_runtime.py:44-64 before_model_call
func (r *DiagnosisBudgetRail) BeforeModelCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	// Python: turn = int(ctx.extra.get("_diagnosis_model_turn", 0)) + 1
	turn := 0
	if raw, ok := cbc.GetExtra(diagnosisModelTurnExtraKey); ok {
		if v, isInt := raw.(int); isInt {
			turn = v
		}
	}
	turn++
	cbc.SetExtra(diagnosisModelTurnExtraKey, turn)
	note := fmt.Sprintf(
		"Investigation turn %d/%d. Return the diagnosis as soon as the local cause is supported.",
		turn, r.maxIterations,
	)
	if turn >= r.maxIterations {
		// Python: ctx.inputs.tools = None——收走全部工具
		if modelInputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
			modelInputs.Tools = nil
			cbc.SetInputs(modelInputs)
		}
		note = "This is the final turn of the existing investigation budget. " +
			"Return the per-case diagnosis JSON now using only the task and " +
			"observations already present. No more tools are available. " +
			"Preserve uncertainty explicitly instead of inventing evidence."
	}
	// Python: # The model window is rebuilt after rails; modifying its preview loses the note.
	//         await ctx.context.add_messages(UserMessage(content=note))
	// Go 等价：ModelContext 取当前消息列表追加 UserMessage 后整体回写
	modelCtx := cbc.ModelContext()
	if modelCtx != nil {
		messages, err := modelCtx.GetMessages(0, true)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).Err(err).Msg("DiagnosisBudgetRail read messages failed")
			return err
		}
		messages = append(messages, llmschema.NewUserMessage(note))
		modelCtx.SetMessages(messages, true)
	}
	return nil
}

// GetCallbacks 声明已覆盖的钩子事件映射。
func (r *DiagnosisBudgetRail) GetCallbacks() map[interfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(interfaces.CallbackBeforeModelCall, r.wrapBeforeModelCall),
	)
}

// NewDiagnosisAgentRuntime 创建诊断 agent 运行时。
// Python: __init__(config)
func NewDiagnosisAgentRuntime(config *config.EvaluationResultAnalyzerConfig) *DiagnosisAgentRuntime {
	return &DiagnosisAgentRuntime{config: config}
}

// BuildAgent 为一个隔离的证据工作区构建有界 DeepAgent。
// Python: agent_runtime.py:80-115 build_agent
func (rt *DiagnosisAgentRuntime) BuildAgent(ctx context.Context, workspacePath string, systemPrompt string, invocationLogDir string) (interfaces.BaseAgent, error) {
	// Python: ref_path = self._config.diagnosis_agent_model_config_ref or self._config.model_config_ref
	refPath := rt.config.DiagnosisAgentModelConfigRef
	if refPath == "" {
		refPath = rt.config.ModelConfigRef
	}
	if refPath == "" {
		return nil, fmt.Errorf("model_config_ref must be set")
	}

	// Python: ref_data = load_model_config_ref(ref_path)
	refData, err := memberoptimizer.LoadModelConfigRef(refPath)
	if err != nil {
		return nil, err
	}
	// Python: model_data = ref_data.get("model", ref_data)
	modelData := refData
	if modelRaw, exists := refData["model"]; exists {
		if m, isMap := modelRaw.(map[string]any); isMap {
			modelData = m
		}
	}
	// Python: model_config = TeamModelConfig.model_validate(without_inner_sdk_retries(model_data))
	modelConfig, err := memberoptimizer.ValidateTeamModelConfig(memberoptimizer.WithoutInnerSDKRetries(modelData))
	if err != nil {
		return nil, err
	}
	// Python: return create_deep_agent(model=model_config.build(), card=..., ...)
	return hsconfig.CreateDeepAgent(ctx, harnessconfig.CreateDeepAgentParams{
		Model: modelConfig.Build(),
		Card: agentschema.NewAgentCard(
			agentschema.WithAgentName("diagnosis_agent"),
			agentschema.WithAgentDescription("Evaluation result diagnosis agent"),
		),
		SystemPrompt:        systemPrompt,
		Workspace:           workspace.NewWorkspace(workspacePath, "en"),
		InvocationLogDir:    invocationLogDir,
		RestrictToWorkDir:   boolPtr(true),
		MaxIterations:       rt.config.DiagnosisAgentMaxIterations,
		AutoCreateWorkspace: false,
		Rails: []interfaces.AgentRail{
			// Python: RSISysOperationRail(read_only=True, bash_pipefail=True,
			//                            bash_deny_patterns=_DIAGNOSIS_BASH_DENY_PATTERNS)
			evaluator.NewRSISysOperationRail(false, true, true, harnessrails.WithReadOnly(true), harnessrails.WithDenyPatterns(diagnosisBashDenyPatterns)),
			NewDiagnosisBudgetRail(rt.config.DiagnosisAgentMaxIterations),
		},
	})
}

// RunDeepAgentText 以每次重试独立会话运行一次 DeepAgent 请求。
// Python: agent_runtime.py:118-148 run_deep_agent_text
//
// 复用失败会话可能重放损坏或不完整的 agent 状态，因此模型重试策略在保持
// DeepAgent 配置不变的同时，用全新会话重试相同证据与提示词。
func RunDeepAgentText(ctx context.Context, agent interfaces.BaseAgent, prompt string, maxRetries int, operationName string) (string, error) {
	callOnce := func() (string, error) {
		// Python: result = await Runner.run_agent(agent=agent, inputs={"query": prompt},
		//                                    session=f"diagnosis_{uuid.uuid4().hex}")
		sessionID := "diagnosis_" + hexUUID()
		result, err := runner.RunAgent(ctx, runner.ByAgent(agent), map[string]any{"query": prompt}, runner.BySessionID(sessionID), nil, nil)
		if err != nil {
			return "", err
		}
		// Python: if isinstance(result, dict): fallback = json.dumps(...); output = str(result.get("output", result.get("answer", fallback)))
		output := ""
		if raw, ok := result["output"]; ok {
			output = toStringValue(raw)
		} else if raw, ok := result["answer"]; ok {
			output = toStringValue(raw)
		} else {
			fallback, _ := json.Marshal(result)
			output = string(fallback)
		}
		// Python: if result.get("result_type") == "error": raise DiagnosisAgentExecutionError(output, termination_reason=...)
		if resultType, _ := result["result_type"].(string); resultType == "error" {
			terminationReason, _ := result["termination_reason"].(string)
			return "", NewDiagnosisAgentExecutionError(output, terminationReason)
		}
		return output, nil
	}
	// Python: return await run_model_call_with_retries(call_once, operation_name=..., max_retries=max(0, int(max_retries or 0)))
	return harnessrsi.RunModelCallWithRetries(ctx, callOnce, operationName, maxRetries, 0, 0)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// wrapBeforeModelCall 适配通用回调签名，转调 BeforeModelCall。
func (r *DiagnosisBudgetRail) wrapBeforeModelCall(ctx context.Context, cbcAny any) error {
	cbc, ok := cbcAny.(*interfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeModelCall(ctx, cbc)
}

// toStringValue 值转字符串（Python str(...)）
func toStringValue(raw any) string {
	if s, isStr := raw.(string); isStr {
		return s
	}
	b, _ := json.Marshal(raw)
	return string(b)
}

// boolPtr 布尔指针构造
func boolPtr(v bool) *bool {
	return &v
}

// hexUUID 无连字符 UUID（Python uuid.uuid4().hex 等价）
func hexUUID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// 编译期检查：诊断 bash 拒绝模式必须是合法正则（Python 模块加载即校验）
var _ = regexp.MustCompile(diagnosisBashDenyPatterns[0])
