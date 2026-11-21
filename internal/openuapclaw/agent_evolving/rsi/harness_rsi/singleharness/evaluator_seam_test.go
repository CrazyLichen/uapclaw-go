package singleharness

import (
	"context"
	"errors"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
)

// fakeEvaluator 测试用评估替身：按预置序列返回 eval_ref 路径或错误。
type fakeEvaluator struct {
	// evalRefs 每次 Evaluate/EvaluateBatch 依次弹出的返回值
	evalRefs []string
	// errs 每次调用依次弹出的错误
	errs []error
	// calls 累计调用次数
	calls int
	// lastBatch 最后一次 EvaluateBatch 的入参快照
	lastBatch struct {
		cases           []map[string]any
		harnessRefsPath string
		outputDir       string
		caseConcurrency int
	}
}

// Evaluate 实现 EvaluatorInvoker。
func (f *fakeEvaluator) Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
	teamSkillRefPath, harnessRefsPath, outputDir string) (string, error) {
	return f.EvaluateBatch(ctx, nil, teamSkillRefPath, harnessRefsPath, outputDir, &dataset, nil, 1, nil)
}

// EvaluateBatch 实现 EvaluatorInvoker（17.6c 收窄签名：EvaluationStageEvent）。
func (f *fakeEvaluator) EvaluateBatch(ctx context.Context, cases []map[string]any,
	teamSkillRefPath, harnessRefsPath, outputDir string,
	dataset *harnessrsi.DatasetArtifact,
	onCaseStage func(evaluator.EvaluationStageEvent) error,
	caseConcurrency int,
	baselineContexts map[string]*evaluator.BaselineJudgeContext) (string, error) {
	idx := f.calls
	f.calls++
	f.lastBatch.cases = cases
	f.lastBatch.harnessRefsPath = harnessRefsPath
	f.lastBatch.outputDir = outputDir
	f.lastBatch.caseConcurrency = caseConcurrency
	ref := ""
	if idx < len(f.evalRefs) {
		ref = f.evalRefs[idx]
	}
	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	return ref, err
}

// TestRealEvaluatorSatisfiesSeam 真身满足注入面（17.6c，桩哨兵已删，回填件①）。
func TestRealEvaluatorSatisfiesSeam(t *testing.T) {
	var invoker EvaluatorInvoker = &evaluator.TeamEvaluator{}
	if invoker == nil {
		t.Fatal("注入面兜底失败")
	}
}

// TestFakeEvaluatorSequence fake 序列驱动。
func TestFakeEvaluatorSequence(t *testing.T) {
	fake := &fakeEvaluator{
		evalRefs: []string{"eval1.yaml", "eval2.yaml"},
		errs:     []error{nil, errors.New("boom")},
	}
	ref1, err1 := fake.EvaluateBatch(context.Background(), []map[string]any{{"case_id": "c1"}},
		"", "refs.yaml", "out1", nil, nil, 2, nil)
	if err1 != nil || ref1 != "eval1.yaml" {
		t.Fatalf("第一次调用错误: %v %v", ref1, err1)
	}
	ref2, err2 := fake.EvaluateBatch(context.Background(), nil, "", "refs.yaml", "out2", nil, nil, 1, nil)
	if err2 == nil || err2.Error() != "boom" || ref2 != "eval2.yaml" {
		t.Fatalf("第二次调用错误: %v %v", ref2, err2)
	}
	if fake.calls != 2 || fake.lastBatch.outputDir != "out2" || fake.lastBatch.caseConcurrency != 1 {
		t.Fatalf("调用快照错误: %+v", fake.lastBatch)
	}
	if len(fake.lastBatch.cases) != 0 {
		t.Fatal("第二次调用 cases 应为空")
	}
}
