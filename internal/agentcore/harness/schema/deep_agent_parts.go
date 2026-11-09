package schema

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentParts DeepAgent 的装配产物：配置 + 待挂 rails + 待注册工具。
//
// 分成 resolve / apply 两半是对齐上游的结构：spec 可以先"解析"
// （不创建实例，便于检查与复用），再"物化"到目标 agent。
//
// 字段与上游 DeepAgentParts 完全一致（4 个）：config / rails /
// tool_cards / tool_instances。默认 rails 在 resolve 阶段就算好并
// 构造实例放进 Rails（对齐 factory.py:394-415），apply 阶段只做挂载。
//
// Python: DeepAgentParts (harness/factory.py)
type DeepAgentParts struct {
	// Config 组装好的 DeepAgent 配置
	Config *DeepAgentConfig
	// Rails 待挂载的 rails（用户声明的在前，默认补齐的在后，顺序即挂载顺序）
	Rails []agentinterfaces.AgentRail
	// ToolCards 规范化后的工具卡片
	ToolCards []*tool.ToolCard
	// ToolInstances 待注册到资源管理器的工具实例
	ToolInstances []tool.Tool
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultRailsResolverMu 保护 defaultRailsResolver
	defaultRailsResolverMu sync.RWMutex
	// defaultRailsResolver 默认 rails 计算器（由 harness 包在 init 注入）
	//
	// 存在的原因：上游 spec.resolve_parts 委托 resolve_deep_agent_parts，
	// 后者在同一次调用里算好默认 rails 放进 parts.rails。Go 侧
	// addDefaultRails 住在 harness 包（依赖 rails/prompts 等上层），
	// schema 不能反向引用，故改为构造期注入函数指针。
	//
	// ⚠️ 与 Go embedding 虚方法不同：这是**显式函数指针**，
	// 调用处 `if fn != nil { fn(...) }` 会真正执行注入的实现，
	// 不会静态绑定到空实现（2026-11-08 的 25.0c 陷阱）。
	defaultRailsResolver func(spec *DeepAgentSpec, config *DeepAgentConfig, userRails []agentinterfaces.AgentRail) []agentinterfaces.AgentRail
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetDefaultRailsResolver 注入默认 rails 计算器（由 harness 包在 init 调用）。
//
// 对齐上游：spec.resolve_parts → resolve_deep_agent_parts 的 default_rails 循环
// (factory.py:394-415)。Go 侧因包层次限制改为注入。
func SetDefaultRailsResolver(
	fn func(spec *DeepAgentSpec, config *DeepAgentConfig, userRails []agentinterfaces.AgentRail) []agentinterfaces.AgentRail,
) {
	defaultRailsResolverMu.Lock()
	defer defaultRailsResolverMu.Unlock()
	defaultRailsResolver = fn
}

// resolveDefaultRails 调默认 rails 计算器（未注入时返回 nil）。
func resolveDefaultRails(spec *DeepAgentSpec, config *DeepAgentConfig, userRails []agentinterfaces.AgentRail) []agentinterfaces.AgentRail {
	defaultRailsResolverMu.RLock()
	fn := defaultRailsResolver
	defaultRailsResolverMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(spec, config, userRails)
}
