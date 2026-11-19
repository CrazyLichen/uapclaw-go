package evaluator

import "testing"

func TestOptimizationSignalsContract_归一化(t *testing.T) {
	contract := OptimizationSignalsContract(
		0.75,
		map[string]any{
			"b_dim": map[string]any{"availability": "available", "value": 0.5, "source": "s1"},
			"a_dim": map[string]any{"availability": "unavailable", "value": 9},
			"":      map[string]any{"value": 1},
		},
		"test",
	)
	if contract["schema_version"] != OptimizationSignalsSchemaVersion {
		t.Fatalf("schema_version: %v", contract["schema_version"])
	}
	continuous := contract["continuous_score"].(map[string]any)
	if continuous["availability"] != "available" || continuous["value"].(float64) != 0.75 {
		t.Fatalf("continuous_score: %v", continuous)
	}
	if contract["promotion_authority"] != "eval_ref_case_score" {
		t.Fatalf("promotion_authority: %v", contract["promotion_authority"])
	}
	dimensions := contract["dimensions"].(map[string]any)
	if _, ok := dimensions["b_dim"]; !ok {
		t.Fatalf("可用维度应保留: %v", dimensions)
	}
	if _, ok := dimensions["a_dim"]; ok {
		t.Fatalf("不可用维度应丢弃")
	}
	if _, ok := dimensions[""]; ok {
		t.Fatalf("空名维度应丢弃")
	}
}

func TestOptimizationSignalsContract_不可用分数(t *testing.T) {
	contract := OptimizationSignalsContract(nil, nil, "")
	continuous := contract["continuous_score"].(map[string]any)
	if continuous["availability"] != "not_available" || continuous["value"] != nil {
		t.Fatalf("nil 分数应 not_available: %v", continuous)
	}
}

func TestEvaluationOptimizationSignals_显式契约(t *testing.T) {
	// 无 optimization_signals → 空契约
	empty := EvaluationOptimizationSignals(map[string]any{})
	if empty["continuous_score"].(map[string]any)["availability"] != "not_available" {
		t.Fatalf("缺省应 not_available")
	}
	// available 读取 value
	got := EvaluationOptimizationSignals(map[string]any{
		"optimization_signals": map[string]any{
			"continuous_score": map[string]any{
				"availability": "available", "value": 0.5, "source": "rubric",
			},
			"dimensions": map[string]any{
				"d1": map[string]any{"availability": "available", "value": 0.25},
			},
		},
	})
	continuous := got["continuous_score"].(map[string]any)
	if continuous["value"].(float64) != 0.5 || continuous["source"] != "rubric" {
		t.Fatalf("available 应读取 value: %v", continuous)
	}
	// unavailable → 分数不采信
	got = EvaluationOptimizationSignals(map[string]any{
		"optimization_signals": map[string]any{
			"continuous_score": map[string]any{"availability": "unavailable", "value": 0.9},
		},
	})
	if got["continuous_score"].(map[string]any)["value"] != nil {
		t.Fatalf("unavailable 不应采信 value")
	}
}

func TestFiniteFloat_形态矩阵(t *testing.T) {
	if finiteFloat(true) != nil || finiteFloat(nil) != nil {
		t.Fatalf("bool/nil 应 nil")
	}
	if v := finiteFloat(1.5); v == nil || *v != 1.5 {
		t.Fatalf("float 应提取")
	}
	if finiteFloat("abc") != nil {
		t.Fatalf("非数字应 nil")
	}
	if v := finiteFloat("2.5"); v == nil || *v != 2.5 {
		t.Fatalf("数字字符串应提取")
	}
	inf := infFloat()
	if finiteFloat(inf) != nil {
		t.Fatalf("Inf 应 nil")
	}
}

func TestRequirementResultsFromJudgeCriteria(t *testing.T) {
	got := RequirementResultsFromJudgeCriteria([]any{
		map[string]any{"criterion_id": "c1", "score": 1.0, "rationale": "ok"},
		map[string]any{"verifier_id": "c2", "score": 0.5},
		map[string]any{"criterion_id": "", "score": 0.1},
		map[string]any{"criterion_id": "c3", "passed": false},
		"not-a-dict",
	})
	items := got["items"].([]map[string]any)
	if len(items) != 4 {
		t.Fatalf("应 4 条（空 ID 按 Python or 链生成 criterion_N，非映射跳过）: %d", len(items))
	}
	// Python: enumerate(start=1)——第 3 项 index=3，生成 criterion_3
	if items[2]["requirement_id"] != "criterion_3" {
		t.Fatalf("空准则 ID 应按序号生成: %v", items[2])
	}
	if items[0]["passed"] != true {
		t.Fatalf("score>=1 应显式通过: %v", items[0])
	}
	if items[1]["requirement_id"] != "c2" {
		t.Fatalf("verifier_id 回退: %v", items[1])
	}
	if items[2]["passed"] != false {
		t.Fatalf("显式 passed 优先: %v", items[2])
	}
}

func TestEvaluationRequirementResults_通用契约优先(t *testing.T) {
	// requirement_results 存在 → 直接归一化
	got := EvaluationRequirementResults(map[string]any{
		"requirement_results": map[string]any{
			"items": []any{
				map[string]any{"requirement_id": "r1", "group": "fail_to_pass", "passed": true, "score": 1.0},
				map[string]any{"requirement_id": "r1", "group": "fail_to_pass", "passed": false, "score": 0.0},
			},
		},
	}, "case1")
	if len(got) != 1 || got[0]["passed"] != false {
		t.Fatalf("后出现者胜出: %v", got)
	}
}

func TestEvaluationRequirementResults_legacy回退(t *testing.T) {
	// atomic_checks
	got := EvaluationRequirementResults(map[string]any{
		"atomic_checks": []any{
			map[string]any{"name": "a1", "passed": true, "detail": "d"},
			map[string]any{"name": "", "passed": false},
		},
	}, "")
	if len(got) != 1 || got[0]["group"] != AtomicCheckGroup {
		t.Fatalf("atomic_checks legacy: %v", got)
	}
	if got[0]["passed"] != true {
		t.Fatalf("缺席不是失败证据（显式 true 保留）")
	}
	// instance_report（按 case_id 取）
	got = EvaluationRequirementResults(map[string]any{
		"instance_report": map[string]any{
			"case1": map[string]any{
				"tests_status": map[string]any{
					"FAIL_TO_PASS": map[string]any{"success": []any{"t1"}, "failure": []any{"t2"}},
					"PASS_TO_PASS": map[string]any{"success": []any{"t3"}},
				},
			},
		},
	}, "case1")
	if len(got) != 3 {
		t.Fatalf("SWE legacy 应 3 条: %d", len(got))
	}
	if got[0]["group"] != FailToPassGroup || got[0]["passed"] != true || got[0]["score"].(float64) != 1.0 {
		t.Fatalf("f2p success: %v", got[0])
	}
	if got[1]["passed"] != false || got[1]["status"] != "failure" {
		t.Fatalf("f2p failure: %v", got[1])
	}
	if got[2]["group"] != PassToPassGroup {
		t.Fatalf("p2p: %v", got[2])
	}
	// 单键 instance_report 免 case_id
	got = EvaluationRequirementResults(map[string]any{
		"instance_report": map[string]any{
			"only": map[string]any{"tests_status": map[string]any{}},
		},
	}, "")
	if len(got) != 0 {
		t.Fatalf("空 tests_status 应空: %v", got)
	}
}

func TestNormalizeRequirementResults_身份与排序(t *testing.T) {
	got := NormalizeRequirementResults([]any{
		map[string]any{"name": "b", "group": "atomic_check"},
		map[string]any{"id": "a"},
		map[string]any{"requirement_id": "  "},
	})
	if len(got) != 2 {
		t.Fatalf("空 ID 跳过: %d", len(got))
	}
	// Python: sorted(by_identity) 按元组排序——atomic_check 组在前
	if got[0]["group"] != AtomicCheckGroup || got[0]["requirement_id"] != "b" {
		t.Fatalf("atomic_check 组应排前: %v", got[0])
	}
	if got[1]["group"] != RequirementGroup || got[1]["requirement_id"] != "a" {
		t.Fatalf("缺省组与 id 回退: %v", got[1])
	}
}

func TestRequirementResultsContract_版本(t *testing.T) {
	contract := RequirementResultsContract(nil)
	if contract["schema_version"] != RequirementResultsSchemaVersion {
		t.Fatalf("schema_version: %v", contract["schema_version"])
	}
}

func infFloat() float64 {
	inf := 1.0
	for i := 0; i < 1024; i++ {
		inf *= 2
	}
	return inf
}
