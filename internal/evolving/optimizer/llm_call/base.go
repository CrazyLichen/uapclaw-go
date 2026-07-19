package llm_call

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMCallOptimizerBase LLM 维度优化器基类，固定 domain="llm"，
// 默认优化目标为 system_prompt 和 user_prompt。
//
// 子优化器嵌入此结构体，获得 LLM 维度的公共字段和辅助方法，
// 然后自己实现 optimizer.BaseOptimizer 接口的全部方法。
//
// 对应 Python: openjiuwen/agent_evolving/optimizer/llm_call/base.py LLMCallOptimizerBase
type LLMCallOptimizerBase struct {
	optimizer.BaseOptimizerMixin
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Domain 返回优化器域 "llm"。
//
// 对应 Python: LLMCallOptimizerBase.domain = "llm"
func (b *LLMCallOptimizerBase) Domain() string {
	return "llm"
}

// DefaultTargets 返回默认优化目标列表。
//
// 对应 Python: LLMCallOptimizerBase.default_targets() → ["system_prompt", "user_prompt"]
func (b *LLMCallOptimizerBase) DefaultTargets() []string {
	return []string{"system_prompt", "user_prompt"}
}

// RequiresForwardData 返回 true，LLM 优化器需要框架执行前向推理。
//
// 对应 Python: BaseOptimizer.requires_forward_data() → True
func (b *LLMCallOptimizerBase) RequiresForwardData() bool {
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTargetFrozen 检查 target 是否在 op.GetTunables() 中。
// 不在 tunables 中即视为冻结（operator 未暴露该参数）。
//
// 对应 Python: LLMCallOptimizerBase._is_target_frozen(op, target)
//   return target not in op.get_tunables()
func (b *LLMCallOptimizerBase) isTargetFrozen(op operator.Operator, target string) bool {
	tunables := op.GetTunables()
	_, exists := tunables[target]
	return !exists
}

// getPromptTemplate 从 op.GetState() 获取 target 内容，构建 PromptTemplate。
//
// 对齐 Python: LLMCallOptimizerBase._get_prompt_template(op, target)
//   state = op.get_state()
//   content = state.get(target, "")
//   return PromptTemplate(content=content)
//
// Python 不做类型判断，直接将 content 传给 PromptTemplate。
// 类型校验由 PromptTemplate 内部的 ToMessages()/deepCopyContent() 等方法负责。
func (b *LLMCallOptimizerBase) getPromptTemplate(op operator.Operator, target string) *prompt.PromptTemplate {
	state := op.GetState()
	if v, ok := state[target]; ok {
		return prompt.NewPromptTemplate("", v)
	}
	return prompt.NewPromptTemplate("", "")
}
