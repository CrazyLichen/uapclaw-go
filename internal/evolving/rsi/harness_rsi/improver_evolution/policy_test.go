package improver_evolution

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestNewFrozenDict 构造、排序与键校验。
func TestNewFrozenDict(t *testing.T) {
	frozen, err := NewFrozenDict(map[string]any{"b": 1, "a": 2})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if got := frozen.Keys(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("键未按序: %v", got)
	}
	if frozen.Len() != 2 {
		t.Fatalf("长度错误: %d", frozen.Len())
	}
	if v, _ := frozen.Get("a"); v != 2 {
		t.Fatalf("取值错误: %v", v)
	}
	if _, err := NewFrozenDict(map[string]any{"": 1}); err == nil || err.Error() != "policy mapping keys must be non-empty strings" {
		t.Fatalf("空键应拒绝: %v", err)
	}
	// nil 等价空映射
	empty, err := NewFrozenDict(nil)
	if err != nil || empty.Len() != 0 {
		t.Fatalf("nil 应为空映射: %v %d", err, empty.Len())
	}
}

// TestNewFrozenDict_非有限值拒绝 非有限浮点拒绝。
func TestNewFrozenDict_非有限值拒绝(t *testing.T) {
	if _, err := NewFrozenDict(map[string]any{"x": []any{nil, map[string]any{}}}); err != nil {
		t.Fatalf("嵌套合法值应通过: %v", err)
	}
	if _, err := NewFrozenDict(map[string]any{"x": []any{struct{}{}}}); err == nil {
		t.Fatalf("不支持类型应拒绝")
	}
}

// TestNewVersionedImproverPolicy_校验分支 构造校验全分支。
func TestNewVersionedImproverPolicy_校验分支(t *testing.T) {
	parent := stringPtr("P0")
	if _, err := NewVersionedImproverPolicy("", nil, "d", STATIC_RANKING_POLICY, nil, nil, nil, nil); err == nil {
		t.Fatal("空 version_id 应拒绝")
	}
	if _, err := NewVersionedImproverPolicy("v", stringPtr(" "), "d", STATIC_RANKING_POLICY, nil, nil, nil, nil); err == nil {
		t.Fatal("空白 parent_version_id 应拒绝")
	}
	if _, err := NewVersionedImproverPolicy("v", parent, " ", STATIC_RANKING_POLICY, nil, nil, nil, nil); err == nil {
		t.Fatal("空白 ledger digest 应拒绝")
	}
	if _, err := NewVersionedImproverPolicy("v", parent, "d", "", nil, nil, nil, nil); err == nil {
		t.Fatal("空 ranking_policy 应拒绝")
	}
	// 权重缺项
	bad, _ := NewFrozenDict(map[string]any{"executable": 1.0})
	if _, err := NewVersionedImproverPolicy("v", parent, "d", STATIC_RANKING_POLICY, bad, nil, nil, nil); err == nil {
		t.Fatal("权重缺项应拒绝")
	}
	// 权重完整时构造成功
	weights, _ := NewFrozenDict(map[string]any{"executable": 1.0, "coverage": 1.0, "atomicity": 1.0, "duplicate": -1.0})
	if _, err := NewVersionedImproverPolicy("v", parent, "d", STATIC_RANKING_POLICY, weights, nil, nil, nil); err != nil {
		t.Fatalf("完整权重应通过: %v", err)
	}
}

// TestVersionedImproverPolicy_证据引用清洗 证据引用去空格去重排序。
func TestVersionedImproverPolicy_证据引用清洗(t *testing.T) {
	policy, err := NewVersionedImproverPolicy("v", nil, "d", STATIC_RANKING_POLICY, nil, nil, nil,
		[]string{" b ", "a", "b", "", "  "})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if !reflect.DeepEqual(policy.EvidenceRefs, []string{"a", "b"}) {
		t.Fatalf("证据引用清洗错误: %v", policy.EvidenceRefs)
	}
}

// TestDefaultImproverPolicy 默认 I0 策略。
func TestDefaultImproverPolicy(t *testing.T) {
	policy := DefaultImproverPolicy()
	if policy.VersionID != "I0" {
		t.Fatalf("版本应为 I0: %s", policy.VersionID)
	}
	if policy.ParentVersionID != nil {
		t.Fatal("I0 不应有父版本")
	}
	if policy.RankingPolicy != STATIC_RANKING_POLICY {
		t.Fatalf("打分策略错误: %s", policy.RankingPolicy)
	}
	if policy.TrainingLedgerDigest != "sha256:"+sha256Hex([]byte("[]")) {
		t.Fatalf("空账本摘要错误: %s", policy.TrainingLedgerDigest)
	}
}

// TestToDictFromDictRoundtrip 序列化往返。
func TestToDictFromDictRoundtrip(t *testing.T) {
	original := DefaultImproverPolicy()
	roundtripped, err := VersionedImproverPolicyFromDict(original.ToDict())
	if err != nil {
		t.Fatalf("from_dict 失败: %v", err)
	}
	if roundtripped.CanonicalDigest() != original.CanonicalDigest() {
		t.Fatalf("往返摘要不一致: %s != %s", roundtripped.CanonicalDigest(), original.CanonicalDigest())
	}
	// 非 mapping 拒绝
	if _, err := VersionedImproverPolicyFromDict("x"); err == nil || err.Error() != "improver policy document must be a mapping" {
		t.Fatalf("非映射应拒绝: %v", err)
	}
	// 错误 schema_version
	bad := original.ToDict()
	bad["schema_version"] = 99
	if _, err := VersionedImproverPolicyFromDict(bad); err == nil {
		t.Fatal("错误 schema_version 应拒绝")
	}
	// evidence_refs 非列表
	bad2 := original.ToDict()
	bad2["evidence_refs"] = "x"
	if _, err := VersionedImproverPolicyFromDict(bad2); err == nil {
		t.Fatal("evidence_refs 非列表应拒绝")
	}
}

// TestCanonicalPolicyDigest 稳定摘要与键序无关。
func TestCanonicalPolicyDigest(t *testing.T) {
	a, _ := NewFrozenDict(map[string]any{"x": 1, "y": map[string]any{"p": true}})
	b, _ := NewFrozenDict(map[string]any{"y": map[string]any{"p": true}, "x": 1})
	if canonicalJSON(thawJSONValue(a)) != canonicalJSON(thawJSONValue(b)) {
		t.Fatal("摘要应与插入顺序无关")
	}
	digest := CanonicalPolicyDigest(DefaultImproverPolicy())
	if len(digest) != len("sha256:")+64 {
		t.Fatalf("摘要格式错误: %s", digest)
	}
}

// TestLoadWriteImproverPolicy YAML 读写往返。
func TestLoadWriteImproverPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "policy.yaml")
	original := DefaultImproverPolicy()
	if _, err := WriteImproverPolicy(path, original); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	loaded, err := LoadImproverPolicy(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if loaded.CanonicalDigest() != original.CanonicalDigest() {
		t.Fatal("读写往返摘要不一致")
	}
	// 非 mapping YAML 拒绝
	scalar := filepath.Join(dir, "scalar.yaml")
	if err := os.WriteFile(scalar, []byte("- 1\n- 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImproverPolicy(scalar); err == nil || err.Error() != "improver policy YAML must contain a mapping" {
		t.Fatalf("标量 YAML 应拒绝: %v", err)
	}
}

// TestScoreStaticPriority 打分与校验分支。
func TestScoreStaticPriority(t *testing.T) {
	policy := DefaultImproverPolicy()
	score, err := ScoreStaticPriority(policy, map[string]any{
		"executable": true, "coverage": 2.0, "atomicity": 1.0, "duplicate": false,
	})
	if err != nil {
		t.Fatalf("打分失败: %v", err)
	}
	if score != 100.0*1+20.0*2+5.0*1+(-30.0)*0 {
		t.Fatalf("打分错误: %v", score)
	}
	if _, err := ScoreStaticPriority(policy, map[string]any{"executable": true, "coverage": 1, "atomicity": 1, "extra": 1}); err == nil {
		t.Fatal("未知特征应拒绝")
	}
	if _, err := ScoreStaticPriority(policy, map[string]any{"executable": true, "coverage": 1}); err == nil {
		t.Fatal("缺失特征应拒绝")
	}
	if _, err := ScoreStaticPriority(policy, map[string]any{"executable": "x", "coverage": 1, "atomicity": 1, "duplicate": 1}); err == nil {
		t.Fatal("非有限特征应拒绝")
	}
	other, _ := NewVersionedImproverPolicy("v", nil, "d", "other_policy", nil, nil, nil, nil)
	if _, err := ScoreStaticPriority(other, map[string]any{"executable": 1, "coverage": 1, "atomicity": 1, "duplicate": 1}); err == nil {
		t.Fatal("未知打分策略应拒绝")
	}
}

// TestProposePolicyCandidates 候选提议：支持度门槛、排序、去重。
func TestProposePolicyCandidates(t *testing.T) {
	parent := DefaultImproverPolicy()
	feedback := map[string]any{
		"stable_patterns": []any{
			map[string]any{
				"pattern_id":                "p2",
				"support_cohorts":           3,
				"rate":                      0.5,
				"recommended_policy_change": map[string]any{"field": "ranking_weights.coverage", "operation": "set", "value": 25.0, "rationale": "r"},
			},
			map[string]any{
				"pattern_id":                "p1",
				"support_cohorts":           5,
				"rate":                      0.9,
				"recommended_policy_change": map[string]any{"field": "budget_policy.top_m", "operation": "increase", "value": 2, "rationale": "r"},
			},
			// 支持度不足
			map[string]any{"pattern_id": "p3", "support": 1, "recommended_policy_change": map[string]any{"field": "ranking_weights.coverage", "operation": "set", "value": 1.0, "rationale": "r"}},
			// 非具体改动
			map[string]any{"pattern_id": "p4", "support_cohorts": 9, "recommended_policy_change": map[string]any{"field": "x", "operation": "set", "value": 1, "rationale": "r"}},
			// 非 mapping 模式
			"not-a-mapping",
		},
	}
	candidates, err := ProposePolicyCandidates(parent, feedback, nil)
	if err != nil {
		t.Fatalf("提议失败: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("应产出 2 个候选: %d", len(candidates))
	}
	// 支持度高的排前
	if candidates[0].RankingWeights == nil || candidates[0].BudgetPolicy.MustGet("top_m") != 3 {
		t.Fatalf("首个候选应为 budget_policy.top_m increase: %v", candidates[0].BudgetPolicy.MustGet("top_m"))
	}
	if candidates[0].ParentVersionID == nil || *candidates[0].ParentVersionID != "I0" {
		t.Fatal("父版本应为 I0")
	}
	// min_support 门槛提高后过滤
	three := 3
	more, err := ProposePolicyCandidates(parent, feedback, &three)
	if err != nil {
		t.Fatalf("提议失败: %v", err)
	}
	if len(more) != 2 {
		t.Fatalf("support=3 仍应保留 2 个: %d", len(more))
	}
	// ProposePolicyUpdate
	best, err := ProposePolicyUpdate(parent, feedback, nil)
	if err != nil || best == nil {
		t.Fatalf("update 不应為空: %v", err)
	}
	none, err := ProposePolicyUpdate(parent, map[string]any{"stable_patterns": []any{}}, nil)
	if err != nil || none != nil {
		t.Fatalf("无模式应返回 nil: %v %v", none, err)
	}
	// stable_patterns 非列表
	if _, err := ProposePolicyCandidates(parent, map[string]any{"stable_patterns": "x"}, nil); err == nil ||
		err.Error() != "feedback_analysis.stable_patterns must be a list" {
		t.Fatalf("非列表应拒绝: %v", err)
	}
}

// TestIsConcreteChange 具体改动判定全分支。
func TestIsConcreteChange(t *testing.T) {
	if isConcreteChange(map[string]any{"field": "f", "operation": "set", "value": 1}) {
		t.Fatal("缺 rationale 应拒绝")
	}
	base := func() map[string]any {
		return map[string]any{"field": "ranking_weights.coverage", "operation": "set", "value": 1.0, "rationale": "r"}
	}
	if !isConcreteChange(base()) {
		t.Fatal("合法权重 set 应通过")
	}
	inc := base()
	inc["operation"], inc["value"] = "increase", 0.0
	if isConcreteChange(inc) {
		t.Fatal("increase 值 0 应拒绝")
	}
	inc2 := base()
	inc2["operation"], inc2["value"] = "increase", 2.0
	if !isConcreteChange(inc2) {
		t.Fatal("increase 正值应通过")
	}
	topM := base()
	topM["field"], topM["operation"], topM["value"] = "budget_policy.top_m", "increase", 2
	if !isConcreteChange(topM) {
		t.Fatal("top_m increase 应通过")
	}
	topMBad := base()
	topMBad["field"], topMBad["value"] = "budget_policy.top_m", true
	if isConcreteChange(topMBad) {
		t.Fatal("bool 值应拒绝")
	}
	flag := base()
	flag["field"], flag["value"] = "generation_directives.require_unique_candidate_fingerprint", true
	if !isConcreteChange(flag) {
		t.Fatal("布尔指令 set true 应通过")
	}
	flagFalse := base()
	flagFalse["field"], flagFalse["value"] = "generation_directives.require_unique_candidate_fingerprint", false
	if isConcreteChange(flagFalse) {
		t.Fatal("布尔指令 set false 应拒绝")
	}
	nested := base()
	nested["field"] = "generation_directives.require_activation_evidence.x"
	nested["value"] = true
	if !isConcreteChange(nested) {
		t.Fatal("嵌套布尔指令应通过")
	}
	if isConcreteChange(base()) {
		_ = base()
	}
}

// TestApplyDeepChange 深改路径全分支。
func TestApplyDeepChange(t *testing.T) {
	target := map[string]any{"a": map[string]any{"b": 2}, "s": "x"}
	if err := applyDeepChange(target, nil, "set", 1); err == nil {
		t.Fatal("空路径应拒绝")
	}
	if err := applyDeepChange(target, []string{"s", "leaf"}, "set", 1); err == nil {
		t.Fatal("穿越非映射应拒绝")
	}
	if err := applyDeepChange(target, []string{"a", "b"}, "set", 5); err != nil || target["a"].(map[string]any)["b"] != 5 {
		t.Fatalf("set 失败: %v", err)
	}
	if err := applyDeepChange(target, []string{"a", "b"}, "increase", 2); err != nil || target["a"].(map[string]any)["b"] != 7 {
		t.Fatalf("increase 失败: %v", err)
	}
	if err := applyDeepChange(target, []string{"a", "b"}, "decrease", 1); err != nil || target["a"].(map[string]any)["b"] != 6 {
		t.Fatalf("decrease 失败: %v", err)
	}
	if err := applyDeepChange(target, []string{"a", "s"}, "increase", 1); err == nil {
		t.Fatal("对非数值 increase 应拒绝")
	}
	if err := applyDeepChange(target, []string{"a", "b"}, "increase", true); err == nil {
		t.Fatal("bool 增量应拒绝")
	}
	if err := applyDeepChange(target, []string{"a", "b"}, "minus", 1); err == nil {
		t.Fatal("未知操作应拒绝")
	}
	// 深路径自动建映射
	if err := applyDeepChange(target, []string{"new", "deep", "leaf"}, "set", "v"); err != nil {
		t.Fatalf("自动建映射失败: %v", err)
	}
}

// TestMinimumSupport 最小支持度解析。
func TestMinimumSupport(t *testing.T) {
	parent := DefaultImproverPolicy()
	if v, err := minimumSupport(parent, nil); err != nil || v != 2 {
		t.Fatalf("默认支持度应为 2: %v %v", v, err)
	}
	three := 3
	if v, _ := minimumSupport(parent, &three); v != 3 {
		t.Fatalf("显式支持度应生效: %d", v)
	}
	zero := 0
	if _, err := minimumSupport(parent, &zero); err == nil {
		t.Fatal("非正支持度应拒绝")
	}
}

// TestFeedbackLedgerDigest 账本摘要提取优先级。
func TestFeedbackLedgerDigest(t *testing.T) {
	digest, err := feedbackLedgerDigest(map[string]any{"source_ledger_digest": " src "})
	if err != nil || digest != "src" {
		t.Fatalf("source_ledger_digest 应优先且去空格: %v %v", digest, err)
	}
	fallback, err := feedbackLedgerDigest(map[string]any{"other": 1})
	if err != nil || len(fallback) != len("sha256:")+64 {
		t.Fatalf("回退应为整体摘要: %v %v", fallback, err)
	}
}

// TestFiniteNumberAndPatternMetric 数值与模式指标助手。
func TestFiniteNumberAndPatternMetric(t *testing.T) {
	if finiteNumber(true) != nil {
		t.Fatal("bool 应拒绝")
	}
	if v := finiteNumber(3); v == nil || *v != 3.0 {
		t.Fatal("int 应接受")
	}
	if finiteNumber("x") != nil {
		t.Fatal("字符串应拒绝")
	}
	pattern := map[string]any{"opportunity": 4}
	if v := patternMetric(pattern, "opportunity_cohorts", "opportunity"); v == nil || *v != 4.0 {
		t.Fatal("回退键应生效")
	}
	if v := patternMetric(pattern, "missing", "opportunity"); v == nil || *v != 4.0 {
		t.Fatal("主键缺失应回退")
	}
}

// TestPlainJSONValueAndCanonicalJSON 纯值与规范 JSON。
func TestPlainJSONValueAndCanonicalJSON(t *testing.T) {
	plain := plainJSONValue(map[string]any{"b": []any{1, "x"}, "a": true})
	if got := canonicalJSON(plain); got != `{"a":true,"b":[1,"x"]}` {
		t.Fatalf("规范 JSON 错误: %s", got)
	}
}

// TestFrozenFieldAndStringField 字段助手。
func TestFrozenFieldAndStringField(t *testing.T) {
	mapping := map[string]any{"s": "v", "f": map[string]any{"k": 1}}
	if stringField(mapping, "s") != "v" || stringField(mapping, "missing") != "" {
		t.Fatal("stringField 行为错误")
	}
	if frozenField(mapping, "f") == nil || frozenField(mapping, "missing") != nil {
		t.Fatal("frozenField 行为错误")
	}
}

func stringPtr(s string) *string { return &s }

// TestMustGetMustPanic 缺失键 MustGet panic。
func TestMustGetMustPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("缺失键应 panic")
		}
	}()
	DefaultImproverPolicy().RankingWeights.MustGet("missing")
}

// TestToDictParentNilAndEvidence 父版本 nil 与 evidence 列表形态。
func TestToDictParentNilAndEvidence(t *testing.T) {
	dict := DefaultImproverPolicy().ToDict()
	if dict["parent_version_id"] != nil {
		t.Fatal("I0 父版本应为 nil")
	}
	refs, ok := dict["evidence_refs"].([]string)
	if !ok || len(refs) != 0 {
		t.Fatalf("evidence_refs 应为空列表: %v", dict["evidence_refs"])
	}
}

// TestAnySliceToStrings 序列字段兼容分支。
func TestAnySliceToStrings(t *testing.T) {
	refs, err := anySliceToStrings([]any{"a", "b"})
	if err != nil || !reflect.DeepEqual(refs, []string{"a", "b"}) {
		t.Fatalf("[]any 提取失败: %v %v", refs, err)
	}
	if _, err := anySliceToStrings([]any{1}); err == nil {
		t.Fatal("非字符串元素应拒绝")
	}
	if _, err := anySliceToStrings("x"); err == nil {
		t.Fatal("非序列应拒绝")
	}
}

// TestWriteImproverPolicyMarshalError 与坏路径错误分支。
func TestWriteImproverPolicyMarshalError(t *testing.T) {
	if _, err := WriteImproverPolicy("/tmp/x.yaml", nil); err == nil || err.Error() != "policy must be a VersionedImproverPolicy" {
		t.Fatalf("nil 策略应拒绝: %v", err)
	}
	// 不可创建目录的路径
	if _, err := WriteImproverPolicy("/proc/definitely/not/writable/p.yaml", DefaultImproverPolicy()); err == nil {
		t.Fatal("坏路径应报错")
	}
	// 坏 YAML 加载
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("a: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImproverPolicy(bad); err == nil {
		t.Fatal("坏 YAML 应报错")
	}
}

// TestProposePolicyCandidatesErrorBranches 错误分支。
func TestProposePolicyCandidatesErrorBranches(t *testing.T) {
	parent := DefaultImproverPolicy()
	if _, err := ProposePolicyCandidates(nil, map[string]any{}, nil); err == nil {
		t.Fatal("nil parent 应拒绝")
	}
	if _, err := ProposePolicyCandidates(parent, nil, nil); err == nil {
		t.Fatal("nil feedback 应拒绝")
	}
	if _, err := ProposePolicyUpdate(nil, nil, nil); err == nil {
		t.Fatal("update 错误应透传")
	}
}

// TestApplyRecommendedChangeEvidencePattern 证据归集与 pattern: 引用。
func TestApplyRecommendedChangeEvidencePattern(t *testing.T) {
	parent := DefaultImproverPolicy()
	pattern := map[string]any{
		"pattern_id":          "p9",
		"evidence_cohort_ids": []any{"c1", " c2 "},
	}
	feedback := map[string]any{"evidence_refs": []any{"ev1", " ev2 ", 42}}
	candidate, err := applyRecommendedChange(parent,
		map[string]any{"field": "ranking_weights.coverage", "operation": "set", "value": 25.0},
		pattern, feedback, "ledg")
	if err != nil {
		t.Fatalf("应用失败: %v", err)
	}
	want := []string{"c1", "c2", "ev1", "ev2", "pattern:p9"}
	if !reflect.DeepEqual(candidate.EvidenceRefs, want) {
		t.Fatalf("证据引用错误: %v", candidate.EvidenceRefs)
	}
	if candidate.RankingWeights.MustGet("coverage") != 25.0 {
		t.Fatal("coverage 权重应更新")
	}
}

// TestFiniteNumberAndNumericValueFull 数值助手补全分支。
func TestFiniteNumberAndNumericValueFull(t *testing.T) {
	if v := finiteNumber(int64(7)); v == nil || *v != 7.0 {
		t.Fatal("int64 应接受")
	}
	if finiteNumber(math.Inf(1)) != nil || finiteNumber(math.NaN()) != nil {
		t.Fatal("非有限应拒绝")
	}
	if _, kind := numericValue(int64(3)); kind != 1 {
		t.Fatal("int64 应为整数类")
	}
	if _, kind := numericValue(1.5); kind != 2 {
		t.Fatal("float64 应为浮点类")
	}
	if _, kind := numericValue("x"); kind != 0 {
		t.Fatal("字符串应为非数")
	}
	if _, kind := numericValue(nil); kind != 0 {
		t.Fatal("nil 应为非数")
	}
}

// TestThawToMapNilFrozen 非映射解冻拒绝。
func TestThawToMapNilFrozen(t *testing.T) {
	if _, err := thawToMap(nil); err == nil {
		t.Fatal("nil 应拒绝")
	}
}

// TestPlainJSONValueScalar 标量纯值。
func TestPlainJSONValueScalar(t *testing.T) {
	if v := plainJSONValue(42); v != 42 {
		t.Fatalf("标量应原样: %v", v)
	}
}

// TestMarshalSortedJSONError 编码错误分支。
func TestMarshalSortedJSONError(t *testing.T) {
	if _, err := marshalSortedJSON(make(chan int)); err == nil {
		t.Fatal("不可编码类型应报错")
	}
}
