package multi_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamConfig 创建 TeamConfig 实例，设置默认值。
//
// 对应 Python: TeamConfig(max_agents=10, max_concurrent_messages=100, message_timeout=30.0)
func NewTeamConfig() *schema.TeamConfig {
	return schema.NewTeamConfig()
}
