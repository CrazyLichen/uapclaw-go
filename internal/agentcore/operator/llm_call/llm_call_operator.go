package llm_call

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMCallOperator LLM 提示词参数句柄。
//
// 管理 system_prompt 和 user_prompt 参数。
// 参数变更通过 onParameterUpdated 回调推送给消费者。
//
// 更新入口：
//   - SetParameter(): 演化更新（检查冻结标记）
//   - LoadState(): 检查点恢复（不检查冻结标记）
//
// 对应 Python: openjiuwen/core/operator/llm_call/base.py LLMCallOperator
type LLMCallOperator struct {
	// systemPrompt 系统 prompt 模板
	systemPrompt *prompt.PromptTemplate
	// userPrompt 用户 prompt 模板
	userPrompt *prompt.PromptTemplate
	// freezeSystemPrompt 系统 prompt 是否冻结
	freezeSystemPrompt bool
	// freezeUserPrompt 用户 prompt 是否冻结
	freezeUserPrompt bool
	// operatorID 操作器标识
	operatorID string
	// onParameterUpdated 参数变更回调
	onParameterUpdated operator.ParameterUpdatedCallback
}

// LLMCallOperatorOption LLMCallOperator 构造选项函数。
type LLMCallOperatorOption func(*LLMCallOperator)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TargetSystemPrompt 系统 prompt 目标名。
	// 对应 Python: "system_prompt"
	TargetSystemPrompt = "system_prompt"
	// TargetUserPrompt 用户 prompt 目标名。
	// 对应 Python: "user_prompt"
	TargetUserPrompt = "user_prompt"
	// defaultUserPrompt 默认用户 prompt 模板。
	// 对应 Python: DEFAULT_USER_PROMPT = "{{query}}"
	defaultUserPrompt = "{{query}}"
	// defaultOperatorID 默认操作器标识。
	defaultOperatorID = "llm_call"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLLMCallOperator 创建 LLMCallOperator 实例。
//
// 对应 Python: LLMCallOperator.__init__(system_prompt, user_prompt, ...)
func NewLLMCallOperator(systemPrompt, userPrompt string, opts ...LLMCallOperatorOption) *LLMCallOperator {
	// userPrompt 为空时使用默认模板
	if userPrompt == "" {
		userPrompt = defaultUserPrompt
	}

	op := &LLMCallOperator{
		systemPrompt:       prompt.NewPromptTemplate("", systemPrompt),
		userPrompt:         prompt.NewPromptTemplate("", userPrompt),
		freezeSystemPrompt: false,
		freezeUserPrompt:   true, // Python 默认 user_prompt 冻结
		operatorID:         defaultOperatorID,
	}

	for _, opt := range opts {
		opt(op)
	}

	return op
}

// OperatorID 返回操作器标识。
//
// 对应 Python: LLMCallOperator.operator_id (property)
func (op *LLMCallOperator) OperatorID() string {
	return op.operatorID
}

// GetTunables 获取可调参数。
// 冻结的参数不会包含在返回结果中。
//
// 对应 Python: LLMCallOperator.get_tunables()
func (op *LLMCallOperator) GetTunables() map[string]operator.TunableSpec {
	tunables := make(map[string]operator.TunableSpec)
	if !op.freezeSystemPrompt {
		tunables[TargetSystemPrompt] = operator.TunableSpec{
			Name: TargetSystemPrompt,
			Kind: operator.TunableKindPrompt,
			Path: TargetSystemPrompt,
		}
	}
	if !op.freezeUserPrompt {
		tunables[TargetUserPrompt] = operator.TunableSpec{
			Name: TargetUserPrompt,
			Kind: operator.TunableKindPrompt,
			Path: TargetUserPrompt,
		}
	}
	return tunables
}

// SetParameter 设置可调参数值（演化更新）。
// 仅更新未冻结的参数，并触发 onParameterUpdated 回调。
//
// 对应 Python: LLMCallOperator.set_parameter(target, value)
func (op *LLMCallOperator) SetParameter(target string, value any) {
	content := promptContent(value)
	if target == TargetSystemPrompt && !op.freezeSystemPrompt {
		op.systemPrompt = prompt.NewPromptTemplate("", content)
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetSystemPrompt, content)
		}
	} else if target == TargetUserPrompt && !op.freezeUserPrompt {
		op.userPrompt = prompt.NewPromptTemplate("", content)
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetUserPrompt, content)
		}
	}
}

// GetState 获取当前 prompt 状态，用于检查点。
//
// 对应 Python: LLMCallOperator.get_state()
func (op *LLMCallOperator) GetState() map[string]any {
	return map[string]any{
		TargetSystemPrompt: op.systemPrompt.Content,
		TargetUserPrompt:   op.userPrompt.Content,
	}
}

// LoadState 从检查点恢复 prompt 状态。
// 不检查冻结标记（检查点恢复必须恢复完整状态）。
// 逐字段更新并触发回调。
//
// 对应 Python: LLMCallOperator.load_state(state)
func (op *LLMCallOperator) LoadState(state map[string]any) {
	if sp, ok := state[TargetSystemPrompt]; ok {
		content := promptContent(sp)
		op.systemPrompt = prompt.NewPromptTemplate("", content)
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetSystemPrompt, content)
		}
	}
	if up, ok := state[TargetUserPrompt]; ok {
		content := promptContent(up)
		op.userPrompt = prompt.NewPromptTemplate("", content)
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetUserPrompt, content)
		}
	}
}

// ApplyUpdate 应用结构化演化更新。
// 使用 DefaultApplyUpdate 提供的默认兼容行为。
//
// 对应 Python: Operator.apply_update 默认实现
func (op *LLMCallOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return operator.DefaultApplyUpdate(op, target, update)
}

// SetFreezeSystemPrompt 设置系统 prompt 冻结状态。
//
// 对应 Python: LLMCallOperator.set_freeze_system_prompt(switch)
func (op *LLMCallOperator) SetFreezeSystemPrompt(freeze bool) {
	op.freezeSystemPrompt = freeze
}

// GetFreezeSystemPrompt 获取系统 prompt 冻结状态。
//
// 对应 Python: LLMCallOperator.get_freeze_system_prompt()
func (op *LLMCallOperator) GetFreezeSystemPrompt() bool {
	return op.freezeSystemPrompt
}

// SetFreezeUserPrompt 设置用户 prompt 冻结状态。
//
// 对应 Python: LLMCallOperator.set_freeze_user_prompt(switch)
func (op *LLMCallOperator) SetFreezeUserPrompt(freeze bool) {
	op.freezeUserPrompt = freeze
}

// GetFreezeUserPrompt 获取用户 prompt 冻结状态。
//
// 对应 Python: LLMCallOperator.get_freeze_user_prompt()
func (op *LLMCallOperator) GetFreezeUserPrompt() bool {
	return op.freezeUserPrompt
}

// WithFreezeSystemPrompt 设置系统 prompt 冻结状态选项。
func WithFreezeSystemPrompt(freeze bool) LLMCallOperatorOption {
	return func(op *LLMCallOperator) { op.freezeSystemPrompt = freeze }
}

// WithFreezeUserPrompt 设置用户 prompt 冻结状态选项。
func WithFreezeUserPrompt(freeze bool) LLMCallOperatorOption {
	return func(op *LLMCallOperator) { op.freezeUserPrompt = freeze }
}

// WithLLMCallOperatorID 设置操作器标识选项。
func WithLLMCallOperatorID(id string) LLMCallOperatorOption {
	return func(op *LLMCallOperator) { op.operatorID = id }
}

// WithLLMCallOnParameterUpdated 设置参数变更回调选项。
func WithLLMCallOnParameterUpdated(cb operator.ParameterUpdatedCallback) LLMCallOperatorOption {
	return func(op *LLMCallOperator) { op.onParameterUpdated = cb }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// promptContent 将 value 转为 prompt 内容字符串。
// 对应 Python: content = value if isinstance(value, (str, list)) else str(value)
func promptContent(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
