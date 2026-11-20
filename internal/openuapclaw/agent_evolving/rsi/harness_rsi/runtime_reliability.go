// Package harnessrsi 单 agent 可靠性轨装配（25.1g 回填真身）。
//
// 对应 Python：openjiuwen/rsi/harness_rsi/runtime_reliability.py（68 行）。
// 把既有 runtime detectors 接入 standalone 评估 agent：fresh state per case，
// source 与 candidate 评估完全一致。
package harnessrsi

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/remediation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// standaloneTruncatedText 超长输出截断后的转向文案（逐字对齐上游）。
//
// Python: runtime_reliability.py _StandaloneRemediator.steer_message 分支。
// ⚠️ 前置条件 anomaly.evidence.get("truncated_without_action") 由 output_length
// detector 的 evidence 承载：上游 output_length.py 不产该键，由 rsi 侧 evaluation
// 管线在 evidence 里补（本批 detectors 一比一未加键，与上游行为一致；
// 17.4d 接线时若上游补键再同步）。
const standaloneTruncatedText = "[reliability] Your last output was truncated, not a completed answer. " +
	"Do not repeat it. Use one available tool for the next bounded action, " +
	"or return a concise final answer if the task is already verified complete."

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildSingleAgentReliabilityRail 构建单 agent 可靠性轨。
//
// Python: runtime_reliability.py build_single_agent_reliability_rail。
// 每案全新状态（fresh state per case），source 与 candidate 评估一致：
// repeat_tool 阈值收紧（36/3/6/6/12），tool_error/model_error/compaction/pingpong
// 禁用，output_length 启用；策略全档 [local_steer, observe_only]；is_leader=true
// （本地 sink，无 messager）。
//
// ⤴️ 17.4a 登记回填：占位工厂（warn + 哨兵错误 ErrReliabilityNotImplemented）已按
// 规则 8 删除，返回签名收窄为 *reliability.ReliabilityRail（消 any，D-4 闭环）。
// 17.4d single_harness 每案构建时消费本工厂。
func BuildSingleAgentReliabilityRail() (*reliability.ReliabilityRail, error) {
	cfg := singleAgentReliabilityConfig()
	policy := reliability.BuildRemediationPolicy(cfg)
	components := reliability.BuildReliabilityComponents(reliability.BuildComponentsDeps{
		Config:     cfg,
		MemberName: "task_agent",
		TeamName:   "",
		SenderId:   "task_agent",
		IsLeader:   true,
	})
	// Python: components.auto_remediator = _StandaloneRemediator(...)（覆盖默认）
	components.AutoRemediator = newStandaloneRemediator(policy, cfg)
	return reliability.ReliabilityRailFromComponents(components), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newStandaloneRemediator 构造 standalone 纠偏器。
//
// Python: runtime_reliability.py _StandaloneRemediator(build_remediation_policy(cfg),
// intensity=..., period_seconds=...)。Go 无继承：以 SetSteerText 缝在基类渲染后
// 覆写——等价 super().steer_message(anomaly) 语义（非偏差）。
func newStandaloneRemediator(policy *remediation.RemediationPolicy, cfg *core.ReliabilityConfig) *remediation.LocalAutoRemediator {
	rem := remediation.NewLocalAutoRemediator(
		policy,
		cfg.RestartIntensity.Intensity,
		cfg.RestartIntensity.PeriodSeconds,
		nil,
	)
	rem.SetSteerText(func(kind core.AnomalyKind, summary string) string {
		message := remediation.DefaultSteerText(kind, summary)
		if kind == core.AnomalyKindOutputTooLong {
			return standaloneTruncatedText
		}
		return message
	})
	return rem
}

// singleAgentReliabilityConfig standalone 评估的硬编码配置。
//
// Python: runtime_reliability.py ReliabilityConfig.model_validate({...}) 逐字段。
func singleAgentReliabilityConfig() *core.ReliabilityConfig {
	cfg := core.DefaultReliabilityConfig()
	cfg.Enabled = true
	cfg.Detectors.ToolError.Enabled = false
	cfg.Detectors.RepeatTool = core.RepeatToolConfig{
		Enabled:      true,
		HistorySize:  36,
		RepeatWarn:   3,
		PingpongWarn: 6,
		LoopBlock:    6,
		GlobalStop:   12,
	}
	cfg.Detectors.ModelError.Enabled = false
	cfg.Detectors.OutputLength.Enabled = true
	cfg.Detectors.Compaction.Enabled = false
	cfg.Detectors.PingPong.Enabled = false
	// policy.severity_actions 全档 ["local_steer", "observe_only"]
	levels := []string{"low", "medium", "high", "critical"}
	cfg.Policy.SeverityActions = make(map[string][]string, len(levels))
	for _, level := range levels {
		cfg.Policy.SeverityActions[level] = []string{"local_steer", "observe_only"}
	}
	return cfg
}
