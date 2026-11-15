package config

import (
	"testing"
)

// TestCoverage_TopLevelValidateBranches 顶层 validate 其余分支的错误文案
func TestCoverage_TopLevelValidateBranches(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"max_epochs":  1,
			"data_loader": map[string]any{"batch_size": 1, "batch_balance_keys": []any{"d"}},
		}
	}
	cases := []struct {
		mutate func(map[string]any)
		want   string
	}{
		{func(m map[string]any) { m["data_loader"] = map[string]any{"batch_size": 0, "batch_balance_keys": []any{"d"}} },
			"data_loader.batch_size must be greater than or equal to 1"},
		{func(m map[string]any) { m["data_loader"] = map[string]any{"batch_size": 1, "batch_balance_keys": []any{}} },
			"data_loader.batch_balance_keys must not be empty"},
		{func(m map[string]any) {
			m["evaluation_result_analyzer"] = map[string]any{"max_issues": 0}
		}, "evaluation_result_analyzer.max_issues must be greater than or equal to 1"},
		{func(m map[string]any) {
			m["evaluation_result_analyzer"] = map[string]any{"evidence_limit_per_issue": 0}
		}, "evaluation_result_analyzer.evidence_limit_per_issue must be greater than or equal to 1"},
		{func(m map[string]any) {
			m["evaluator"] = map[string]any{"solver_backend": "other"}
		}, "evaluator.solver_backend must be one of: deep_agent, uapclawswarm"},
		{func(m map[string]any) {
			m["evaluator"] = map[string]any{"uapclawswarm_startup_timeout_sec": 0}
		}, "evaluator.uapclawswarm_startup_timeout_sec must be greater than or equal to 1"},
		{func(m map[string]any) {
			m["evaluator"] = map[string]any{"uapclawswarm_runtime_timeout_sec": 0}
		}, "evaluator.uapclawswarm_runtime_timeout_sec must be greater than or equal to 1"},
		{func(m map[string]any) {
			m["member_optimizer"] = map[string]any{"max_issue_attempts_per_batch": -1}
		}, "member_optimizer.max_issue_attempts_per_batch must be greater than or equal to 0"},
		{func(m map[string]any) {
			m["member_optimizer"] = map[string]any{"max_repair_rounds_per_batch": 0}
		}, "member_optimizer.max_repair_rounds_per_batch must be greater than or equal to 1"},
		{func(m map[string]any) {
			m["member_optimizer"] = map[string]any{"sibling_candidate_count": 0}
		}, "member_optimizer.sibling_candidate_count must be greater than or equal to 1"},
	}
	for _, testCase := range cases {
		data := base()
		testCase.mutate(data)
		_, err := AutoCoordinatingHarnessConfigFromDict(data)
		if err == nil || err.Error() != testCase.want {
			t.Fatalf("want %q, got %v", testCase.want, err)
		}
	}
}

// TestCoverage_AnalyzerConfigFromDict 诊断分析配置默认与覆盖
func TestCoverage_AnalyzerConfigFromDict(t *testing.T) {
	cfg, err := EvaluationResultAnalyzerConfigFromDict(map[string]any{
		"diagnosis_agent_max_retries":      7,
		"diagnosis_agent_max_concurrency":  3,
		"diagnosis_agent_max_iterations":   100,
		"causal_investigation_required":    false,
		"max_issues":                       5,
		"evidence_limit_per_issue":         2,
		"output_filename":                  "out.yaml",
		"model_config_ref":                 "m1",
		"diagnosis_agent_model_config_ref": "m2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DiagnosisAgentMaxRetries != 7 || cfg.DiagnosisAgentMaxConcurrency != 3 ||
		cfg.DiagnosisAgentMaxIterations != 100 || cfg.CausalInvestigationRequired ||
		cfg.MaxIssues != 5 || cfg.EvidenceLimitPerIssue != 2 ||
		cfg.OutputFilename != "out.yaml" || cfg.ModelConfigRef != "m1" ||
		cfg.DiagnosisAgentModelConfigRef != "m2" {
		t.Fatalf("analyzer override failed: %+v", cfg)
	}
	def, err := EvaluationResultAnalyzerConfigFromDict(map[string]any{})
	if err != nil || def.DiagnosisAgentMaxRetries != DefaultModelCallMaxRetries || def.OutputFilename != "issues.yaml" {
		t.Fatalf("analyzer defaults failed: %+v %v", def, err)
	}
}

// TestCoverage_ExpandTilde tilde 展开与普通路径
func TestCoverage_ExpandTilde(t *testing.T) {
	if expandTilde("/abs/path") != "/abs/path" {
		t.Fatal("absolute path unchanged")
	}
	if expandTilde("~/.config") == "~/.config" {
		t.Fatal("tilde must expand")
	}
}

// TestCoverage_ToIntHelpers toInt 数字形态
func TestCoverage_ToIntHelpers(t *testing.T) {
	if value, ok := toInt(int64(5)); !ok || value != 5 {
		t.Fatalf("int64: %v %v", value, ok)
	}
	if value, ok := toInt(2.9); !ok || value != 2 {
		t.Fatalf("float: %v %v", value, ok)
	}
	if _, ok := toInt("str"); ok {
		t.Fatal("string must not parse")
	}
	if _, ok := toInt(true); ok {
		t.Fatal("bool must not parse")
	}
}

// TestCoverage_MemberOptimizerFromDictDefaults 成员优化配置全默认
func TestCoverage_MemberOptimizerFromDictDefaults(t *testing.T) {
	cfg, err := MemberOptimizerConfigFromDict(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]any{
		"MaxRolesPerRun": cfg.MaxRolesPerRun, "MinAttributionConfidence": cfg.MinAttributionConfidence,
		"ActionPlanningMaxAttempts": cfg.ActionPlanningMaxAttempts, "MaxCasesPerIssue": cfg.MaxCasesPerIssue,
		"MaxTraceExcerptChars": cfg.MaxTraceExcerptChars, "MaxResultExcerptChars": cfg.MaxResultExcerptChars,
		"AllowEmptyHarnessRefsNoop": cfg.AllowEmptyHarnessRefsNoop, "ExecutionConcurrency": cfg.ExecutionConcurrency,
		"RoleExecutionConcurrency": cfg.RoleExecutionConcurrency, "ActionExecutionConcurrencyPerRole": cfg.ActionExecutionConcurrencyPerRole,
		"MaxActionsPerPlan": cfg.MaxActionsPerPlan, "MaxIssueAttemptsPerBatch": cfg.MaxIssueAttemptsPerBatch,
		"MaxRepairRoundsPerBatch": cfg.MaxRepairRoundsPerBatch, "SiblingCandidateCount": cfg.SiblingCandidateCount,
		"CandidateMinScoreDelta": cfg.CandidateMinScoreDelta, "AdaptFrozenTeamIssues": cfg.AdaptFrozenTeamIssues,
		"RuntimeIterationHardLimit": cfg.RuntimeIterationHardLimit,
	}
	if checks["MaxRolesPerRun"] != 2 || checks["MinAttributionConfidence"] != 0.5 ||
		checks["ActionPlanningMaxAttempts"] != 10 || checks["MaxCasesPerIssue"] != 3 ||
		checks["MaxTraceExcerptChars"] != 4000 || checks["MaxResultExcerptChars"] != 2000 ||
		checks["AllowEmptyHarnessRefsNoop"] != true || checks["ExecutionConcurrency"] != 2 ||
		checks["RoleExecutionConcurrency"] != 2 || checks["ActionExecutionConcurrencyPerRole"] != 2 ||
		checks["MaxActionsPerPlan"] != 0 || checks["MaxIssueAttemptsPerBatch"] != 0 ||
		checks["MaxRepairRoundsPerBatch"] != 3 || checks["SiblingCandidateCount"] != 1 ||
		checks["CandidateMinScoreDelta"] != 0.0 || checks["AdaptFrozenTeamIssues"] != false ||
		checks["RuntimeIterationHardLimit"] != 300 {
		t.Fatalf("member optimizer defaults mismatch: %+v", checks)
	}
	if cfg.AttributionRetryLimit != DefaultModelCallMaxRetries || cfg.StageRetryLimit != DefaultModelCallMaxRetries {
		t.Fatalf("retry defaults mismatch: %d %d", cfg.AttributionRetryLimit, cfg.StageRetryLimit)
	}
	if _, err := MemberOptimizerConfigFromDict(map[string]any{"action_planning_max_attempts": 0}); err == nil ||
		err.Error() != "member_optimizer.action_planning_max_attempts must be a positive integer" {
		t.Fatalf("attempts validation: %v", err)
	}
}

// TestCoverage_HelperBranches 助手函数剩余分支
func TestCoverage_HelperBranches(t *testing.T) {
	// stringValue 非字符串强转
	data := map[string]any{"k": 42}
	if stringValue(data, "k", "") != "42" {
		t.Fatalf("non-string must coerce: %q", stringValue(data, "k", ""))
	}
	// mappingValue 非 map 返回空映射
	if got := mappingValue(map[string]any{"k": "not a map"}, "k"); len(got) != 0 {
		t.Fatalf("non-map must be empty: %v", got)
	}
	// solverIterationLimit 浮点
	if value, err := solverIterationLimit(200.0); err != nil || value != 200 {
		t.Fatalf("float limit failed: %v %v", value, err)
	}
	// trimSpaces 多空白形态
	if trimSpaces("  \n x \t") != "x" {
		t.Fatalf("trim failed: %q", trimSpaces("  \n x \t"))
	}
	// boolValue nil → 默认
	if value, err := boolValue(nil, true); err != nil || !value {
		t.Fatalf("nil must default: %v %v", value, err)
	}
	// intValue 浮点非整也归一（对齐 Python int() 截断）
	if intValue(map[string]any{"k": 2.7}, "k", 0) != 2 {
		t.Fatal("float truncation failed")
	}
	// floatValue 整数输入
	if floatValue(map[string]any{"k": 3}, "k", 0) != 3.0 {
		t.Fatal("int to float failed")
	}
}

// TestCoverage_IntValueBoolReject bool 传入 int 字段按 Python 拒绝
func TestCoverage_IntValueBoolReject(t *testing.T) {
	if intValue(map[string]any{"k": true}, "k", 7) != 7 {
		t.Fatal("bool input must fall back to default (Python _int_value raises; Go 收敛为默认值)")
	}
}
