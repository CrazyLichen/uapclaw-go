package dataloader

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BatchPlanStore 数据集画像与批次计划产物落盘（对齐 BatchPlanStore；静态方法 → 包级函数）
type BatchPlanStore struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// WriteDatasetProfile 写 dataset_profile.yaml 并返回绝对路径（对齐 write_dataset_profile）
func (BatchPlanStore) WriteDatasetProfile(root string, profile map[string]any) (string, error) {
	profilePath := filepath.Join(root, "dataset_profile.yaml")
	raw, err := yaml.Marshal(profile)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(profilePath, raw, 0o644); err != nil {
		return "", err
	}
	return profilePath, nil
}

// WriteBatchPlan 写 batch_plan.yaml 并返回绝对路径（对齐 write_batch_plan）
func (BatchPlanStore) WriteBatchPlan(root string, epoch, batchSize int, balanceKeys []string,
	profile map[string]any, batches [][]map[string]any) (string, error) {
	planPath := filepath.Join(root, "batch_plan.yaml")
	planBatches := make([]any, 0, len(batches))
	for index, batch := range batches {
		planBatches = append(planBatches, BatchPlanItem(batch, index+1))
	}
	summary, _ := profile["summary"].(map[string]any)
	warnings, _ := profile["warnings"].([]any)
	metadata, _ := profile["metadata"].(map[string]any)
	payload := map[string]any{
		"plan_id":         fmt.Sprintf("batch_plan_epoch_%03d", epoch),
		"dataset_dir":     root,
		"strategy":        "curriculum_balanced",
		"epoch":           epoch,
		"seed":            fmt.Sprintf("%s:epoch_%03d", filepath.Base(root), epoch),
		"batch_size":      batchSize,
		"balance_keys":    append([]string(nil), balanceKeys...),
		"profile_summary": summary,
		"batches":         planBatches,
		"warnings":        warnings,
		"metadata":        metadata,
	}
	raw, err := yaml.Marshal(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(planPath, raw, 0o644); err != nil {
		return "", err
	}
	return planPath, nil
}
