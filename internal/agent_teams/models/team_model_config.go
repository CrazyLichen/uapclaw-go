package models

import (
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 类型别名 ────────────────────────────

// TeamModelConfig 可序列化的团队模型配置（core schema 的类型别名）。
//
// 类型本体已下沉到 internal/agentcore/harness/schema/team_model_config.go，
// 对齐上游归属（openjiuwen/harness/schema/deep_agent_spec.py:127）。
// 保留别名让既有 models.TeamModelConfig 引用继续工作，且与 core 是同一类型。
type TeamModelConfig = hschema.TeamModelConfig

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamModelConfig 创建默认 TeamModelConfig（转发 core 实现）。
func NewTeamModelConfig() TeamModelConfig { return hschema.NewTeamModelConfig() }
