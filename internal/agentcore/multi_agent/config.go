package multi_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamConfig 团队运行时配置，类型别名指向 schema 包。
//
// 可变参数，描述团队"怎么运行"。所有配置方法支持链式调用。
//
// 对应 Python: openjiuwen/core/multi_agent/config.py (TeamConfig)
// Python 字段: max_agents=10, max_concurrent_messages=100, message_timeout=30.0
type TeamConfig = schema.TeamConfig

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamConfig 创建 TeamConfig 实例，设置默认值。
//
// 对应 Python: TeamConfig(max_agents=10, max_concurrent_messages=100, message_timeout=30.0)
func NewTeamConfig() *TeamConfig {
	return schema.NewTeamConfig()
}

// ──────────────────────────── 非导出函数 ────────────────────────────
