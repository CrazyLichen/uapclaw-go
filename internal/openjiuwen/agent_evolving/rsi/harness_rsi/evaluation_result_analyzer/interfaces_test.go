package evaluationresultanalyzer

import (
	"context"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
)

// fakeExtractor / fakeStrategy 接口编译期断言用的测试替身。
type fakeExtractor struct{}

func (f *fakeExtractor) Name() string { return "fake" }
func (f *fakeExtractor) Extract(_ EvaluationSummaryInput, _ []CaseAnalysisInput) DeterministicSignals {
	return DeterministicSignals{}
}

type fakeStrategy struct{}

func (f *fakeStrategy) Name() string { return "fake" }
func (f *fakeStrategy) Analyze(_ context.Context, _ harnessrsi.EvaluationResultAnalysisInvocation) (harnessrsi.EvaluationResultAnalysisArtifact, error) {
	return harnessrsi.EvaluationResultAnalysisArtifact{}, nil
}

// TestInterfaces_编译期契约 验证两个 Protocol 的 Go interface 形状可被实现。
// Python: interfaces.py SignalExtractor / EvaluationResultAnalysisStrategy
func TestInterfaces_编译期契约(t *testing.T) {
	var e SignalExtractor = &fakeExtractor{}
	if e.Name() != "fake" {
		t.Fatal("Name 应返回稳定名")
	}
	var s EvaluationResultAnalysisStrategy = &fakeStrategy{}
	if s.Name() != "fake" {
		t.Fatal("Name 应返回稳定名")
	}
}
