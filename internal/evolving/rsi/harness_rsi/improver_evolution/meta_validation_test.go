package improver_evolution

import (
	"math"
	"testing"
)

func TestDefaultMetaValidationThresholds_七默认值(t *testing.T) {
	d := DefaultMetaValidationThresholds()
	if d.MinUnseenCheckpoints() != 3 {
		t.Fatalf("min_unseen_checkpoints 默认应为 3")
	}
	if d.TopMGainNonDegradationTolerance() != 0.0 ||
		d.MinSelectionRegretImprovement() != 1e-12 ||
		d.BestOfKGainNonDegradationTolerance() != 0.0 ||
		d.FinalHarnessGainPerBudgetNonDegradationTolerance() != 0.0 ||
		d.MaxRegressionFailureRateIncrease() != 0.0 ||
		d.MaxInfrastructureFailureRateIncrease() != 0.0 {
		t.Fatal("容差默认值不符")
	}
}

func TestNewMetaValidationThresholds_校验链逐字(t *testing.T) {
	_, err := NewMetaValidationThresholds(0, 0, 0, 0, 0, 0, 0)
	assertErrMsg(t, err, "min_unseen_checkpoints must be an integer of at least 1")

	_, err = NewMetaValidationThresholds(-1, 0, 0, 0, 0, 0, 0)
	assertErrMsg(t, err, "min_unseen_checkpoints must be an integer of at least 1")

	for name, values := range map[string][6]float64{
		"top_m_gain_non_degradation_tolerance":                    {-1, 0, 0, 0, 0, 0},
		"min_selection_regret_improvement":                        {0, -1, 0, 0, 0, 0},
		"best_of_k_gain_non_degradation_tolerance":                {0, 0, -1, 0, 0, 0},
		"final_harness_gain_per_budget_non_degradation_tolerance": {0, 0, 0, -1, 0, 0},
		"max_regression_failure_rate_increase":                    {0, 0, 0, 0, -1, 0},
		"max_infrastructure_failure_rate_increase":                {0, 0, 0, 0, 0, -1},
	} {
		_, err = NewMetaValidationThresholds(3,
			values[0], values[1], values[2], values[3], values[4], values[5])
		assertErrMsg(t, err, name+" must be a finite non-negative number")
	}

	_, err = NewMetaValidationThresholds(3, math.Inf(1), 0, 0, 0, 0, 0)
	assertErrMsg(t, err, "top_m_gain_non_degradation_tolerance must be a finite non-negative number")
}

func TestMetaValidationThresholds_AsDict_键名snake_case(t *testing.T) {
	d := DefaultMetaValidationThresholds()
	dict := d.AsDict()
	want := []string{
		"min_unseen_checkpoints",
		"top_m_gain_non_degradation_tolerance",
		"min_selection_regret_improvement",
		"best_of_k_gain_non_degradation_tolerance",
		"final_harness_gain_per_budget_non_degradation_tolerance",
		"max_regression_failure_rate_increase",
		"max_infrastructure_failure_rate_increase",
	}
	if len(dict) != 7 {
		t.Fatalf("expected 7 keys, got %d", len(dict))
	}
	for _, key := range want {
		if _, ok := dict[key]; !ok {
			t.Fatalf("缺键: %s", key)
		}
	}
	if dict["min_unseen_checkpoints"] != 3 {
		t.Fatalf("unexpected: %v", dict["min_unseen_checkpoints"])
	}
}
