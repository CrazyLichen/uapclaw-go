package checkpointing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StoreRecordsHelper 演进记录 CRUD 和持久化辅助。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/store_records.py StoreRecordsHelper
type StoreRecordsHelper struct {
	// store 所属的 EvolutionStore 实例
	store *EvolutionStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// langToExt 语言扩展名映射。
// 对应 Python: _LANG_TO_EXT
var langToExt = map[string]string{
	"python":     "py",
	"javascript": "js",
	"typescript": "ts",
	"shell":      "sh",
	"bash":       "sh",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PersistScript 将脚本源代码写入独立文件，替换 content 为引用。
// 对应 Python: StoreRecordsHelper.persist_script(skill_dir, record)
func (h *StoreRecordsHelper) PersistScript(ctx context.Context, skillDir string, record *EvolutionRecord) error {
	scriptsDir := filepath.Join(skillDir, "evolution", "scripts")
	_ = os.MkdirAll(scriptsDir, 0755)

	lang := "py"
	if record.Change.ScriptLanguage != nil {
		l := *record.Change.ScriptLanguage
		if ext, ok := langToExt[l]; ok {
			lang = ext
		} else {
			lang = l
		}
	}
	filename := fmt.Sprintf("%s_script.%s", record.ID, lang)
	if record.Change.ScriptFilename != nil && *record.Change.ScriptFilename != "" {
		filename = *record.Change.ScriptFilename
	}
	scriptPath := filepath.Join(scriptsDir, filename)

	if err := h.store.WriteFileText(ctx, scriptPath, record.Change.Content); err != nil {
		logger.Warn(logComponent).
			Str("filename", filename).
			Str("record_id", record.ID).
			Err(err).
			Msg("[EvolutionStore] 持久化脚本写入失败")
		return err
	}
	logger.Info(logComponent).
		Str("filename", filename).
		Str("record_id", record.ID).
		Msg("[EvolutionStore] 持久化脚本")

	record.Change.ScriptFilename = &filename
	scriptLang := "unknown"
	if record.Change.ScriptLanguage != nil {
		scriptLang = *record.Change.ScriptLanguage
	}
	scriptPurpose := ""
	if record.Change.ScriptPurpose != nil {
		scriptPurpose = *record.Change.ScriptPurpose
	}
	record.Change.Content = fmt.Sprintf("Script: %s\nLanguage: %s\nPurpose: %s", filename, scriptLang, scriptPurpose)
	return nil
}

// LoadFullEvolutionLog 加载完整演进日志。
// 对应 Python: StoreRecordsHelper.load_full_evolution_log(name)
//
// 对齐 Python 异常语义：错误场景返回 (nil, error)，首次无记录返回 (EmptyEvolutionLog, nil)
func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error) {
	skillDir := h.store.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return nil, fmt.Errorf("skill %s not found in evolution store", name)
	}
	evoPath := filepath.Join(skillDir, evolutionFilename)
	if !isFile(evoPath) {
		// 首次无演进记录是正常情况，不是错误
		return EmptyEvolutionLog(name), nil
	}
	fileContent, err := h.store.ReadFileText(ctx, evoPath)
	if err != nil {
		return nil, fmt.Errorf("read evolution log for skill %s: %w", name, err)
	}
	if fileContent == "" {
		return EmptyEvolutionLog(name), nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fileContent), &data); err != nil {
		return nil, fmt.Errorf("parse evolution log for skill %s: %w", name, err)
	}
	evoLog, err := FromDictEvolutionLog(data)
	if err != nil {
		return nil, fmt.Errorf("decode evolution log for skill %s: %w", name, err)
	}
	return evoLog, nil
}

// SaveEvolutionLog 持久化演进日志。
// 对应 Python: StoreRecordsHelper.save_evolution_log(name, evo_log, skill_dir)
func (h *StoreRecordsHelper) SaveEvolutionLog(ctx context.Context, name string, evoLog *EvolutionLog, skillDir string) error {
	targetDir := skillDir
	if targetDir == "" {
		targetDir = h.store.ResolveSkillDir(ctx, name, true)
	}
	if targetDir == "" {
		return nil
	}
	_ = os.MkdirAll(targetDir, 0755)
	evoPath := filepath.Join(targetDir, evolutionFilename)
	data, err := json.Marshal(evoLog.ToDict())
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("[EvolutionStore] 序列化演进日志失败")
		return err
	}
	return h.store.WriteFileText(ctx, evoPath, string(data))
}

// UpdateRecordScores 更新记录分数。
// 对应 Python: StoreRecordsHelper.update_record_scores(name, updates)
func (h *StoreRecordsHelper) UpdateRecordScores(ctx context.Context, name string, updates map[string]map[string]any) (int, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	evoLog, err := h.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("load evolution log for update_record_scores: %w", err)
	}
	updatedCount := 0

	for i := range evoLog.Entries {
		if updateData, ok := updates[evoLog.Entries[i].ID]; ok {
			if v, ok := updateData["score"]; ok {
				evoLog.Entries[i].Score = getFloatFromAny(v, evoLog.Entries[i].Score)
			}
			if v, ok := updateData["usage_stats"]; ok {
				if statsMap, ok := v.(map[string]any); ok {
					evoLog.Entries[i].UsageStats = FromDictUsageStats(statsMap)
				}
			}
			updatedCount++
		}
	}

	if updatedCount > 0 {
		evoLog.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := h.SaveEvolutionLog(ctx, name, evoLog, ""); err != nil {
			return updatedCount, err
		}
		logger.Info(logComponent).
			Int("updated_count", updatedCount).
			Str("skill", name).
			Msg("[EvolutionStore] 更新记录分数")
	}
	return updatedCount, nil
}

// GetRecordsByScore 按分数获取记录。
// 对应 Python: StoreRecordsHelper.get_records_by_score(name, min_score)
func (h *StoreRecordsHelper) GetRecordsByScore(ctx context.Context, name string, minScore *float64) []EvolutionRecord {
	evoLog, err := h.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		logger.Warn(logComponent).Str("skill", name).Err(err).Msg("[EvolutionStore] get_records_by_score: 加载失败")
		return nil
	}
	records := evoLog.Entries
	if minScore != nil {
		filtered := make([]EvolutionRecord, 0)
		for _, r := range records {
			if r.Score >= *minScore {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}
	// 对齐 Python: sorted(records, key=lambda r: r.score, reverse=True)
	sortRecordsByScore(records)
	return records
}

// DeleteRecords 删除记录。
// 对应 Python: StoreRecordsHelper.delete_records(name, record_ids)
func (h *StoreRecordsHelper) DeleteRecords(ctx context.Context, name string, recordIDs []string) (int, error) {
	if len(recordIDs) == 0 {
		return 0, nil
	}
	evoLog, err := h.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("load evolution log for delete_records: %w", err)
	}
	idsSet := make(map[string]bool, len(recordIDs))
	for _, id := range recordIDs {
		idsSet[id] = true
	}
	originalCount := len(evoLog.Entries)
	filtered := make([]EvolutionRecord, 0)
	for _, r := range evoLog.Entries {
		if !idsSet[r.ID] {
			filtered = append(filtered, r)
		}
	}
	evoLog.Entries = filtered
	deletedCount := originalCount - len(evoLog.Entries)

	if deletedCount > 0 {
		evoLog.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := h.SaveEvolutionLog(ctx, name, evoLog, ""); err != nil {
			return deletedCount, err
		}
		_ = h.store.RenderEvolutionMarkdown(ctx, name)
		logger.Info(logComponent).
			Int("deleted_count", deletedCount).
			Str("skill", name).
			Msg("[EvolutionStore] 删除记录")
	}
	return deletedCount, nil
}

// MarkRecordsApplied 标记记录已应用。
// 对应 Python: StoreRecordsHelper.mark_records_applied(name, record_ids)
func (h *StoreRecordsHelper) MarkRecordsApplied(ctx context.Context, name string, recordIDs []string) (int, error) {
	if len(recordIDs) == 0 {
		return 0, nil
	}
	evoLog, err := h.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("load evolution log for mark_records_applied: %w", err)
	}
	idsSet := make(map[string]bool, len(recordIDs))
	for _, id := range recordIDs {
		idsSet[id] = true
	}
	updatedCount := 0
	for i := range evoLog.Entries {
		if idsSet[evoLog.Entries[i].ID] && !evoLog.Entries[i].Applied {
			evoLog.Entries[i].Applied = true
			updatedCount++
		}
	}
	if updatedCount > 0 {
		evoLog.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := h.SaveEvolutionLog(ctx, name, evoLog, ""); err != nil {
			return updatedCount, err
		}
		_ = h.store.RenderEvolutionMarkdown(ctx, name)
		logger.Info(logComponent).
			Int("updated_count", updatedCount).
			Str("skill", name).
			Msg("[EvolutionStore] 标记记录已应用")
	}
	return updatedCount, nil
}

// MergeRecords 合并记录。
// 对应 Python: StoreRecordsHelper.merge_records(name, primary_id, remove_ids, new_content, new_score)
func (h *StoreRecordsHelper) MergeRecords(ctx context.Context, name string, primaryID string, removeIDs []string, newContent string, newScore *float64) (*EvolutionRecord, error) {
	evoLog, err := h.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("load evolution log for merge_records: %w", err)
	}
	var primaryRecord *EvolutionRecord
	var recordsToRemove []*EvolutionRecord
	var allScores []float64

	// 对齐 Python: 查找 primary 和 remove 记录
	for i := range evoLog.Entries {
		if evoLog.Entries[i].ID == primaryID {
			primaryRecord = &evoLog.Entries[i]
		}
	}
	removeSet := make(map[string]bool, len(removeIDs))
	for _, id := range removeIDs {
		removeSet[id] = true
	}
	for i := range evoLog.Entries {
		if removeSet[evoLog.Entries[i].ID] {
			recordsToRemove = append(recordsToRemove, &evoLog.Entries[i])
			allScores = append(allScores, evoLog.Entries[i].Score)
		}
	}

	if primaryRecord == nil {
		logger.Warn(logComponent).
			Str("primary_id", primaryID).
			Msg("[EvolutionStore] merge_records: 主记录未找到")
		return nil, nil
	}

	allScores = append(allScores, primaryRecord.Score)
	finalScore := primaryRecord.Score
	if newScore != nil {
		finalScore = *newScore
	} else if len(allScores) > 0 {
		finalScore = maxFloat(allScores)
	}

	primaryRecord.Change.Content = newContent
	primaryRecord.Summary = nil
	primaryRecord.Score = finalScore
	primaryRecord.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)

	// 对齐 Python: 从 entries 中移除 remove 记录
	filtered := make([]EvolutionRecord, 0)
	for _, entry := range evoLog.Entries {
		if !removeSet[entry.ID] {
			filtered = append(filtered, entry)
		}
	}
	evoLog.Entries = filtered

	evoLog.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.SaveEvolutionLog(ctx, name, evoLog, ""); err != nil {
		return primaryRecord, err
	}
	_ = h.store.RenderEvolutionMarkdown(ctx, name)

	logger.Info(logComponent).
		Int("removed_count", len(recordsToRemove)).
		Str("primary_id", primaryID).
		Str("skill", name).
		Msg("[EvolutionStore] 合并记录")
	return primaryRecord, nil
}

// UpdateRecordContent 更新记录内容。
// 对应 Python: StoreRecordsHelper.update_record_content(name, record_id, new_content, new_score)
func (h *StoreRecordsHelper) UpdateRecordContent(ctx context.Context, name string, recordID string, newContent string, newScore *float64) (*EvolutionRecord, error) {
	evoLog, err := h.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("load evolution log for update_record_content: %w", err)
	}
	var targetRecord *EvolutionRecord

	for i := range evoLog.Entries {
		if evoLog.Entries[i].ID == recordID {
			targetRecord = &evoLog.Entries[i]
			break
		}
	}

	if targetRecord == nil {
		logger.Warn(logComponent).
			Str("record_id", recordID).
			Msg("[EvolutionStore] update_record_content: 记录未找到")
		return nil, nil
	}

	targetRecord.Change.Content = newContent
	targetRecord.Summary = nil
	if newScore != nil {
		targetRecord.Score = *newScore
	}
	targetRecord.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)

	evoLog.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.SaveEvolutionLog(ctx, name, evoLog, ""); err != nil {
		return targetRecord, err
	}
	_ = h.store.RenderEvolutionMarkdown(ctx, name)

	logger.Info(logComponent).
		Str("record_id", recordID).
		Str("skill", name).
		Msg("[EvolutionStore] 更新记录内容")
	return targetRecord, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortRecordsByScore 按分数降序排序记录。
func sortRecordsByScore(records []EvolutionRecord) {
	for i := 0; i < len(records)-1; i++ {
		for j := i + 1; j < len(records); j++ {
			if records[j].Score > records[i].Score {
				records[i], records[j] = records[j], records[i]
			}
		}
	}
}

// maxFloat 返回 float64 列表中的最大值。
func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}
