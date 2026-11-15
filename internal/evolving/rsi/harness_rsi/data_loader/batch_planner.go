package dataloader

import (
	"fmt"
	"sort"
	"strings"
)

// difficultyOrder 难度排序权重（对齐 _DIFFICULTY_ORDER）
var difficultyOrder = map[string]int{"easy": 0, "medium": 1, "hard": 2}

// ──────────────────────────── 结构体 ────────────────────────────

// BatchPlanner 课程均衡批次规划（对齐 BatchPlanner；静态方法 → 包级函数）
type BatchPlanner struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Plan 返回单轮次的有序批次（对齐 BatchPlanner.plan）
func (BatchPlanner) Plan(cases []map[string]any, batchSize int) [][]map[string]any {
	ordered := curriculumBalancedCases(cases)
	batches := [][]map[string]any{}
	batch := []map[string]any{}
	for _, caseMapping := range ordered {
		batch = append(batch, caseMapping)
		if len(batch) >= batchSize {
			batches = append(batches, batch)
			batch = []map[string]any{}
		}
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

// BatchPlanItem 返回单个批次的可序列化计划条目（对齐 batch_plan_item）
func BatchPlanItem(batch []map[string]any, batchIndex int) map[string]any {
	difficulties := make([]string, 0, len(batch))
	caseEntries := make([]map[string]any, 0, len(batch))
	dimensionSet := map[string]bool{}
	for _, caseMapping := range batch {
		difficulty := CaseValue(caseMapping, "difficulty")
		difficulties = append(difficulties, difficulty)
		dimension := CaseValue(caseMapping, "dimension")
		dimensionSet[dimension] = true
		casePath, _ := caseMapping["case_path"].(string)
		caseIndex := caseMapping["case_index"]
		caseEntries = append(caseEntries, map[string]any{
			"case_id":    CaseID(caseMapping),
			"difficulty": difficulty,
			"dimension":  dimension,
			"source":     CaseValue(caseMapping, "source"),
			"task_type":  CaseValue(caseMapping, "task_type"),
			"case_path":  casePath,
			"case_index": caseIndex,
		})
	}
	dimensions := make([]string, 0, len(dimensionSet))
	for dimension := range dimensionSet {
		dimensions = append(dimensions, dimension)
	}
	sort.Strings(dimensions)
	return map[string]any{
		"batch_id": fmt.Sprintf("batch_%03d", batchIndex),
		"cases":    caseEntries,
		"metadata": map[string]any{
			"difficulty_stage": dominantDifficulty(difficulties),
			"dimensions":       dimensions,
		},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// curriculumBalancedCases 难度递进 + 维度轮转（对齐 _curriculum_balanced_cases）
func curriculumBalancedCases(cases []map[string]any) []map[string]any {
	byDifficulty := map[string][]map[string]any{}
	for _, caseMapping := range cases {
		difficulty := CaseValue(caseMapping, "difficulty")
		byDifficulty[difficulty] = append(byDifficulty[difficulty], caseMapping)
	}
	difficulties := make([]string, 0, len(byDifficulty))
	for difficulty := range byDifficulty {
		difficulties = append(difficulties, difficulty)
	}
	sort.Slice(difficulties, func(i, j int) bool {
		return difficultySortKey(difficulties[i]) < difficultySortKey(difficulties[j])
	})
	ordered := []map[string]any{}
	for _, difficulty := range difficulties {
		ordered = append(ordered, roundRobinByDimension(byDifficulty[difficulty])...)
	}
	return ordered
}

// roundRobinByDimension 维度轮转（对齐 _round_robin_by_dimension）
func roundRobinByDimension(cases []map[string]any) []map[string]any {
	grouped := map[string][]map[string]any{}
	sorted := append([]map[string]any(nil), cases...)
	sort.Slice(sorted, func(i, j int) bool {
		return stableCaseSortKey(sorted[i]) < stableCaseSortKey(sorted[j])
	})
	for _, caseMapping := range sorted {
		dimension := CaseValue(caseMapping, "dimension")
		grouped[dimension] = append(grouped[dimension], caseMapping)
	}
	dimensions := make([]string, 0, len(grouped))
	for dimension := range grouped {
		dimensions = append(dimensions, dimension)
	}
	sort.Strings(dimensions)
	ordered := []map[string]any{}
	for {
		remaining := false
		for _, dimension := range dimensions {
			if len(grouped[dimension]) > 0 {
				ordered = append(ordered, grouped[dimension][0])
				grouped[dimension] = grouped[dimension][1:]
				remaining = true
			}
		}
		if !remaining {
			break
		}
	}
	return ordered
}

// difficultySortKey 难度排序键（对齐 _difficulty_sort_key；未知难度排末尾）
func difficultySortKey(difficulty string) int {
	if weight, known := difficultyOrder[difficulty]; known {
		return weight
	}
	return len(difficultyOrder)
}

// stableCaseSortKey 稳定用例排序键（对齐 _stable_case_sort_key）
func stableCaseSortKey(caseMapping map[string]any) string {
	caseIndex := 0
	if raw, ok := caseMapping["case_index"].(int); ok {
		caseIndex = raw
	}
	return strings.Join([]string{
		CaseValue(caseMapping, "source"),
		CaseValue(caseMapping, "task_type"),
		CaseID(caseMapping),
		fmt.Sprintf("%012d", caseIndex),
	}, "\x00")
}

// dominantDifficulty 主导难度（对齐 _dominant_difficulty）
func dominantDifficulty(difficulties []string) string {
	known := []string{}
	for _, difficulty := range difficulties {
		if difficulty != UnknownValue {
			known = append(known, difficulty)
		}
	}
	if len(known) == 0 {
		return UnknownValue
	}
	best := known[0]
	for _, difficulty := range known[1:] {
		if difficultySortKey(difficulty) < difficultySortKey(best) {
			best = difficulty
		}
	}
	return best
}
