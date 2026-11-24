package harnessobservability

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// subagent.go 给每个被派发的子 agent 自己的 agent 层 span。
// Python: harness/observability/subagent.py（全文 94 行）。
// 子 agent 在 SDK 内于 dispatch 时构建，父 agent 的 spec 够不到它：没有自己的
// rail 就不产生 agent.<type>.invoke span，其 llm/tool span 挂到**派发方**的
// span 上——子 agent 的整个运行看起来像父 agent 打的调用，而它实际运行的
// task_tool span 下面什么都没有。

// AttachSubagentObservability 给被派发的子 agent 挂自己的 agent 层观测 rail。
// Python: attach_subagent_observability（subagent.py:23-58）。构造期挂不可靠：
// 父 agent 构造一次、通常早于观测初始化，rail 守卫会返回 None；dispatch 时
// 观测已起，add_rail 仍落在子 agent 第一次 _ensure_initialized() 注册钩子之前。
// 幂等（已挂 AgentObservabilityRail 则不再加）；观测关闭或缺 rail API 时
// no-op；best-effort 绝不打断运行。
// Go 侧 AgentObservabilityRail 于 15.24a Task 3 落地后接 rail 分支（届时删占位）。
func AttachSubagentObservability(subagent any) {
	if subagent == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Debug(logComponent).Any("panic", rec).
				Msg("[AgentObservability] attach subagent rail failed")
		}
	}()
	// Task 3 接线：rail 非 nil 且子 agent 尚无 AgentObservabilityRail → AddRail。
	// Python: subagent.configured_rails() 判型 + subagent.add_rail(rail)；
	// Go 经 DeepAgentInterface 的 rails 访问缝（RegisterRail 已有），实现于
	// deep_agent.go CreateSubagent 调用点（偏差 spec §4）。
	if MaybeAgentObservabilityRail() == nil {
		return
	}
	if _, ok := subagent.(interface {
		AddObservabilityRailIfAbsent(rail any) bool
	}); ok {
		// 具体挂载由 deep_agent.go 的实现承接（防 observability→deep_agent 环）。
	}
}
