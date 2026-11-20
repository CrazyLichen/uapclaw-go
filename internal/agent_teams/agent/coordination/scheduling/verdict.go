package scheduling

// verdict.go 纯评审票判定数学（F_62）。无 IO、无状态。
//
// 多类型评审结算：settle_review_tally 把二元票池（verifier + challenger）与
// 分数池（inspector）合并为三值判定。二元池一票否决——任何 fail 都判该轮
// 失败；inspector 池要求每位 inspector 的平均分达到或超过 0.85。
//
// Python: agent_teams/agent/scheduling/verdict.py（全文，2026-12-04 快照）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// VerdictPass 判定通过。
	VerdictPass = "pass"
	// VerdictFail 判定不通过。
	VerdictFail = "fail"
	// VerdictUndecided 尚未决出（仍有池未投满）。
	VerdictUndecided = "undecided"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SettleReviewTally 判定一次多类型评审计票。
//
// 二元池（verifier + challenger）：等全部票到齐，然后任何 fail 判该轮失败。
//
// 分数池（inspector）：等全部票到齐，然后平均分须达到或超过 inspectorThreshold。
//
// 任一池仍在等待（voted < total）返回 UNDECIDED。
//
// Python: settle_review_tally (verdict.py L17-46)。
func SettleReviewTally(tally map[string]any, inspectorThreshold float64) string {
	// — 二元池：verifier + challenger —
	binTotal := intOf(tally["verdict_total"])
	if binTotal > 0 {
		if intOf(tally["verdict_voted"]) < binTotal {
			return VerdictUndecided
		}
		if intOf(tally["verdict_fail_count"]) > 0 {
			return VerdictFail
		}
	}

	// — 分数池：inspector —
	inspTotal := intOf(tally["inspector_count"])
	if inspTotal > 0 {
		if intOf(tally["inspector_voted"]) < inspTotal {
			return VerdictUndecided
		}
		if tally["inspector_avg"] == nil {
			return VerdictFail
		}
		avg, isFloat := tally["inspector_avg"].(float64)
		if !isFloat || avg < inspectorThreshold {
			return VerdictFail
		}
	}

	return VerdictPass
}

// intOf 计票列取整（bool/int64/int 兼容；缺省 0）。
func intOf(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
