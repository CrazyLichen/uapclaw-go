package schema

import (
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
