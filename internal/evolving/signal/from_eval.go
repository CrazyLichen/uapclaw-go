package signal

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FromEvaluatedCase 将离线评估结果转换为演化信号。
//
// 当 scoreThreshold 非空且 case.GetScore() >= threshold 时返回 nil（不产生信号）；
// 否则根据分数判断 signal_type：score==0 → "low_score"，其他 → "evaluated"。
//
// 对应 Python: openjiuwen/agent_evolving/signal/from_eval.py from_evaluated_case
func FromEvaluatedCase(case_ *dataset.EvaluatedCase, operatorID string, scoreThreshold *float64) *EvolutionSignal {
	if scoreThreshold != nil && case_.GetScore() >= *scoreThreshold {
		return nil
	}

	signalType := "evaluated"
	if case_.GetScore() == 0 {
		signalType = "low_score"
	}

	var skillName *string
	if operatorID != "" {
		skillName = &operatorID
	}

	context := map[string]any{
		"question": fmt.Sprintf("%v", case_.GetInputs()),
		"label":    fmt.Sprintf("%v", case_.GetLabel()),
		"answer":   fmt.Sprintf("%v", case_.Answer),
		"reason":   case_.Reason,
		"score":    case_.GetScore(),
		"source":   "offline_evaluation",
	}

	return &EvolutionSignal{
		SignalType: signalType,
		Section:    "Troubleshooting",
		Excerpt:    fmt.Sprintf("score=%.2f", case_.GetScore()),
		SkillName:  skillName,
		Context:    context,
	}
}

// FromEvaluatedCases 批量将 EvaluatedCase 列表转换为 EvolutionSignal 列表。
//
// 对应 Python: openjiuwen/agent_evolving/signal/from_eval.py from_evaluated_cases
func FromEvaluatedCases(cases []*dataset.EvaluatedCase, operatorID string, scoreThreshold *float64) []*EvolutionSignal {
	signals := make([]*EvolutionSignal, 0, len(cases))
	for _, case_ := range cases {
		sig := FromEvaluatedCase(case_, operatorID, scoreThreshold)
		if sig != nil {
			signals = append(signals, sig)
		}
	}
	return signals
}

// ──────────────────────────── 非导出函数 ────────────────────────────
