package dataset

import (
	"math/rand/v2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CaseLoader Case 容器，支持拆分和打乱。
//
// 对应 Python: openjiuwen/agent_evolving/dataset/case_loader.py CaseLoader
type CaseLoader struct {
	// cases 内部样本列表
	cases []Case
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCaseLoader 创建 CaseLoader 实例。
func NewCaseLoader(cases []Case) *CaseLoader {
	if cases == nil {
		cases = []Case{}
	}
	return &CaseLoader{cases: cases}
}

// Cases 返回内部样本列表的副本。
func (cl *CaseLoader) Cases() []Case {
	result := make([]Case, len(cl.cases))
	copy(result, cl.cases)
	return result
}

// Len 返回样本数量。
func (cl *CaseLoader) Len() int {
	return len(cl.cases)
}

// Split 按比例拆分训练集和验证集。
//
// ratio 为训练集占比（0.0-1.0），返回 (trainLoader, valLoader)。
// 对应 Python: CaseLoader.split(ratio)
func (cl *CaseLoader) Split(ratio float64) (*CaseLoader, *CaseLoader) {
	if len(cl.cases) == 0 {
		return NewCaseLoader(nil), NewCaseLoader(nil)
	}

	splitIdx := int(float64(len(cl.cases)) * ratio)
	if splitIdx < 0 {
		splitIdx = 0
	}
	if splitIdx > len(cl.cases) {
		splitIdx = len(cl.cases)
	}

	trainCases := make([]Case, splitIdx)
	copy(trainCases, cl.cases[:splitIdx])

	valCases := make([]Case, len(cl.cases)-splitIdx)
	copy(valCases, cl.cases[splitIdx:])

	return NewCaseLoader(trainCases), NewCaseLoader(valCases)
}

// ShuffleCases 随机打乱内部样本顺序。
//
// 对应 Python: shuffle_cases(cases)
func (cl *CaseLoader) ShuffleCases() {
	rand.Shuffle(len(cl.cases), func(i, j int) {
		cl.cases[i], cl.cases[j] = cl.cases[j], cl.cases[i]
	})
}
