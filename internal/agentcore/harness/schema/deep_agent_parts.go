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
// ⚠️ 与上游的一处阶段归属差异（偏差 D-9 的一部分）：
// 上游 resolve 阶段就把默认 rails（Security/TaskPlanning/SkillUse/Subagent…）
// 算进 parts；Go 侧默认 rails 在 apply 阶段补齐（addDefaultRails 在 harness 包，
// schema 不能引用它，否则 schema→rails→prompts→schema 成环）。
// **最终 agent 上的 rail 集合完全一致**，只是 parts.Rails 不含默认项。
//
// Python: DeepAgentParts (harness/factory.py)
type DeepAgentParts struct {
	// Config 组装好的 DeepAgent 配置
	Config *DeepAgentConfig
	// Rails 待挂载的 rails（仅 spec 声明的，不含默认补齐项）
	Rails []agentinterfaces.AgentRail
	// ToolCards 规范化后的工具卡片
	ToolCards []*tool.ToolCard
	// ToolInstances 待注册到资源管理器的工具实例
	ToolInstances []tool.Tool
}
