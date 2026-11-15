package harnessrsi

import (
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// ErrReliabilityNotImplemented 占位哨兵：agent_teams reliability 子系统未落地
// （对齐偏差 D-4；⤴️ 25.1g 回填真身后删除，规则 8）
var ErrReliabilityNotImplemented = errors.New("agent_teams reliability subsystem is not implemented; see plan 25.1g")

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildSingleAgentReliabilityRail 构建单 Agent 可靠性轨（占位工厂）。
//
// 上游 openjiuwen/rsi/harness_rsi/runtime_reliability.py 的 build_single_agent_reliability_rail
// 依赖 openjiuwen/agent_teams/reliability/ 全链（ReliabilityConfig / AnomalyKind /
// build_reliability_components / LocalAutoRemediator），该子系统 Go 侧未落地
// （与 internal/agent_teams/rails/elements.go buildTeamReliabilityRail 占位同源）。
// 真实实现（含 _StandaloneRemediator.steer_message 的 OUTPUT_TOO_LONG
// truncated_without_action 转向文案分支）⤴️ 25.1g 回填；本占位仅保持调用面签名稳定，
// 消费方（17.4d single_harness）开工前 25.1g 必须先行。
func BuildSingleAgentReliabilityRail() (any, error) {
	logger.Warn(logComponent).
		Str("rail", "runtime_reliability").
		Msg("runtime reliability rail provider is not implemented; skipping")
	return nil, ErrReliabilityNotImplemented
}
