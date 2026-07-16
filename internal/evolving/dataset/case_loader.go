package dataset

import (
	"fmt"
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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

// Split 按比例拆分训练集和验证集（先打乱再拆分，保证可复现）。
//
// ratio 为训练集占比（0.0-1.0），seed 为随机种子（默认 0），
// 返回 (trainLoader, valLoader)。
// 对齐 Python: CaseLoader.split(ratio, seed=0) — 内部先 shuffle_cases 再分割。
func (cl *CaseLoader) Split(ratio float64, seed ...int64) (*CaseLoader, *CaseLoader, error) {
	if ratio < 0.0 || ratio > 1.0 {
		return nil, nil, fmt.Errorf("ratio must be in [0.0, 1.0], got %f", ratio)
	}
	if len(cl.cases) == 0 {
		return NewCaseLoader(nil), NewCaseLoader(nil), nil
	}

	// 对齐 Python: shuffle_cases(self._cases, seed) — 先打乱再分割
	s := int64(0)
	if len(seed) > 0 {
		s = seed[0]
	}
	shuffled := shuffleCases(cl.cases, s)

	cut := int(float64(len(shuffled)) * ratio)
	trainCases := shuffled[:cut]
	valCases := shuffled[cut:]

	return NewCaseLoader(trainCases), NewCaseLoader(valCases), nil
}

// ShuffleCases 随机打乱内部样本顺序。
//
// 对应 Python: shuffle_cases(cases)
func (cl *CaseLoader) ShuffleCases() {
	rand.Shuffle(len(cl.cases), func(i, j int) {
		cl.cases[i], cl.cases[j] = cl.cases[j], cl.cases[i]
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// shuffleCases 返回打乱后的副本（不修改原列表），使用指定种子保证可复现。
//
// 对齐 Python: shuffle_cases(cases, seed=0) — random.Random(seed).shuffle(copy)
func shuffleCases(cases []Case, seed int64) []Case {
	shuffled := make([]Case, len(cases))
	copy(shuffled, cases)
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled
}
