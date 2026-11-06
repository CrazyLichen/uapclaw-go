package context_engineer

import (
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// railCfgAgent 在既有 railAgent 桩上覆写 Config()，供需要注入 ReActAgentConfig 的用例使用。
type railCfgAgent struct {
	*railAgent
	// cfg 注入的 agent 配置
	cfg sainterfaces.AgentConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Config 返回注入的 ReActAgentConfig。
func (a *railCfgAgent) Config() sainterfaces.AgentConfig { return a.cfg }
