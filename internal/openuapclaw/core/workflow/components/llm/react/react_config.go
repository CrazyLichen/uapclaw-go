// ReActAgentCompConfig ReAct 工作流组件配置。
// Python: react_config.py ReActAgentCompConfig(ReActAgentConfig)——纯继承零新字段
// （上游注释 "May add workflow-specific configurations later"，原样保留）。
package react

import (
	saconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/config"
)

// ReActAgentCompConfig ReAct 工作流组件配置（Python ReActAgentCompConfig）。
type ReActAgentCompConfig struct {
	// ReActAgentConfig 嵌入领域 6 的 ReActAgent 配置（6.3 已迁 sa/config）
	*saconfig.ReActAgentConfig
}
