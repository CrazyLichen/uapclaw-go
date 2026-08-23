package experience

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PresentedRecordEntry 展示记录条目。
// 对应 Python: tuple[str, EvolutionRecord, str]
type PresentedRecordEntry struct {
	// SkillName 技能名称
	SkillName string
	// Record 演进记录
	Record checkpointing.EvolutionRecord
	// Snippet 展示片段文本
	Snippet string
}

// ExperienceTracker 展示经验追踪器。
// 对应 Python: ExperienceTracker
type ExperienceTracker struct {
	// store 所属的 EvolutionStore
	store *checkpointing.EvolutionStore
	// scorer 经验评分器
	scorer *ExperienceScorer
	// evalInterval 评估间隔
	evalInterval int
}

// RecordScoreUpdate 单条记录的评分更新数据。
//
// 对应 Python: update_record_scores 中内层 dict {"score": ..., "usage_stats": ...}
type RecordScoreUpdate struct {
	// Score 新评分
	Score float64
	// UsageStats 使用统计字典（传给 EvolutionStore.UpdateRecordScores）
	UsageStats map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// sessionPresentedRecords 包级 map，sessionID → 展示记录条目列表。
// 对应 Python: session._experience_tracker_presented_records
var sessionPresentedRecords = map[string][]PresentedRecordEntry{}

// sessionEvalCounter 包级 map，sessionID → 评估计数器。
// 对应 Python: session._experience_tracker_eval_counter
var sessionEvalCounter = map[string]int{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExperienceTracker 创建 ExperienceTracker。
func NewExperienceTracker(
	store *checkpointing.EvolutionStore,
	scorer *ExperienceScorer,
	evalInterval int,
) *ExperienceTracker {
	return &ExperienceTracker{
		store:        store,
		scorer:       scorer,
		evalInterval: evalInterval,
	}
}

// RecordPresented 记录展示的 BODY 经验。
// 对应 Python: ExperienceTracker.record_presented()
func (t *ExperienceTracker) RecordPresented(
	ctx context.Context,
	sessionID string,
	skillName string,
	presentationSnippet string,
) error {
	records := t.store.GetRecordsByScore(ctx, skillName, func() *float64 { f := 0.5; return &f }())
	if len(records) == 0 {
		return nil
	}

	bodyRecords := filterBodyRecords(records)
	if len(bodyRecords) == 0 {
		return nil
	}

	updates := map[string]*RecordScoreUpdate{}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	limit := 5
	if len(bodyRecords) < limit {
		limit = len(bodyRecords)
	}
	for i := 0; i < limit; i++ {
		record := bodyRecords[i]
		existingStats := record.UsageStats
		if existingStats == nil {
			existingStats = &checkpointing.UsageStats{}
		}
		newStats := checkpointing.UsageStats{
			TimesPresented:  existingStats.TimesPresented + 1,
			TimesUsed:       existingStats.TimesUsed,
			TimesPositive:   existingStats.TimesPositive,
			TimesNegative:   existingStats.TimesNegative,
			LastPresentedAt: &now,
			LastEvaluatedAt: existingStats.LastEvaluatedAt,
		}
		updates[record.ID] = &RecordScoreUpdate{
			Score:      record.Score,
			UsageStats: newStats.ToDict(),
		}
	}

	if _, err := t.store.UpdateRecordScores(ctx, skillName, updatesToMap(updates)); err != nil {
		logger.Error(logComponent).
			Str("skill", skillName).
			Err(err).
			Msg("[ExperienceTracker] update_record_scores 失败")
		return err
	}

	presentedEntries := []PresentedRecordEntry{}
	for i := 0; i < limit; i++ {
		record := bodyRecords[i]
		if update, ok := updates[record.ID]; ok {
			record.UsageStats = checkpointing.FromDictUsageStats(update.UsageStats)
			presentedEntries = append(presentedEntries, PresentedRecordEntry{
				SkillName: skillName,
				Record:    record,
				Snippet:   presentationSnippet,
			})
		}
	}

	existing := sessionPresentedRecords[sessionID]
	sessionPresentedRecords[sessionID] = append(existing, presentedEntries...)

	logger.Debug(logComponent).
		Int("presented_count", len(presentedEntries)).
		Str("skill", skillName).
		Msg("[ExperienceTracker] tracked presented records")

	return nil
}

// RecordPresentedRecords 记录显式展示的经验记录。
// 对应 Python: ExperienceTracker.record_presented_records()
func (t *ExperienceTracker) RecordPresentedRecords(
	ctx context.Context,
	sessionID string,
	skillName string,
	presentationSnippet string,
	recordIDs []string,
) error {
	if len(recordIDs) == 0 {
		return nil
	}

	evoLog := t.store.LoadFullEvolutionLog(ctx, skillName)
	requestedIDs := map[string]bool{}
	for _, id := range recordIDs {
		requestedIDs[id] = true
	}

	var bodyRecords []checkpointing.EvolutionRecord
	for _, record := range evoLog.Entries {
		if requestedIDs[record.ID] && isBodyRecord(&record) {
			bodyRecords = append(bodyRecords, record)
		}
	}
	if len(bodyRecords) == 0 {
		return nil
	}

	updates := map[string]*RecordScoreUpdate{}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, record := range bodyRecords {
		existingStats := record.UsageStats
		if existingStats == nil {
			existingStats = &checkpointing.UsageStats{}
		}
		newStats := checkpointing.UsageStats{
			TimesPresented:  existingStats.TimesPresented + 1,
			TimesUsed:       existingStats.TimesUsed,
			TimesPositive:   existingStats.TimesPositive,
			TimesNegative:   existingStats.TimesNegative,
			LastPresentedAt: &now,
			LastEvaluatedAt: existingStats.LastEvaluatedAt,
		}
		updates[record.ID] = &RecordScoreUpdate{
			Score:      record.Score,
			UsageStats: newStats.ToDict(),
		}
	}

	if _, err := t.store.UpdateRecordScores(ctx, skillName, updatesToMap(updates)); err != nil {
		logger.Error(logComponent).
			Str("skill", skillName).
			Err(err).
			Msg("[ExperienceTracker] update_record_scores 失败")
		return err
	}

	presentedEntries := []PresentedRecordEntry{}
	for _, record := range bodyRecords {
		if update, ok := updates[record.ID]; ok {
			record.UsageStats = checkpointing.FromDictUsageStats(update.UsageStats)
			presentedEntries = append(presentedEntries, PresentedRecordEntry{
				SkillName: skillName,
				Record:    record,
				Snippet:   presentationSnippet,
			})
		}
	}

	existing := sessionPresentedRecords[sessionID]
	sessionPresentedRecords[sessionID] = append(existing, presentedEntries...)

	return nil
}

// ConsumeEvalState 消费评估状态（达到评估间隔时返回记录列表）。
// 对应 Python: ExperienceTracker.consume_eval_state()
func (t *ExperienceTracker) ConsumeEvalState(sessionID string) []PresentedRecordEntry {
	counter := sessionEvalCounter[sessionID]
	counter++

	var presentedEntries []PresentedRecordEntry
	if counter >= t.evalInterval {
		presentedEntries = sessionPresentedRecords[sessionID]
		sessionPresentedRecords[sessionID] = nil
		sessionEvalCounter[sessionID] = 0
	} else {
		sessionEvalCounter[sessionID] = counter
	}
	return presentedEntries
}

// EvaluatePresented 评估展示的经验并更新评分。
// 对应 Python: ExperienceTracker.evaluate_presented()
func (t *ExperienceTracker) EvaluatePresented(
	ctx context.Context,
	presentedEntries []PresentedRecordEntry,
) error {
	if len(presentedEntries) == 0 {
		return nil
	}

	// 对齐 Python: 按 (skill_name, snippet) 分组
	bySkillSnippet := map[string]map[string][]checkpointing.EvolutionRecord{} // 技能名 → 片段 → 记录
	for _, entry := range presentedEntries {
		snippets, ok := bySkillSnippet[entry.SkillName]
		if !ok {
			snippets = map[string][]checkpointing.EvolutionRecord{}
			bySkillSnippet[entry.SkillName] = snippets
		}
		snippets[entry.Snippet] = append(snippets[entry.Snippet], entry.Record)
	}

	for skillName, snippets := range bySkillSnippet {
		for snippet, records := range snippets {
			evalResults, err := t.scorer.Evaluate(ctx, snippet, records)
			if err != nil || len(evalResults) == 0 {
				continue
			}

			updates := map[string]*RecordScoreUpdate{}
			for _, result := range evalResults {
				recordID, ok := result["record_id"].(string)
				if !ok || recordID == "" {
					continue
				}
				for _, record := range records {
					if record.ID == recordID {
						newScore := UpdateScore(&record, result, nil)
						if record.UsageStats == nil {
							record.UsageStats = &checkpointing.UsageStats{}
						}
						updates[recordID] = &RecordScoreUpdate{
							Score:      newScore,
							UsageStats: record.UsageStats.ToDict(),
						}
						break
					}
				}
			}

			if len(updates) > 0 {
				if _, err := t.store.UpdateRecordScores(ctx, skillName, updatesToMap(updates)); err != nil {
					logger.Warn(logComponent).
						Str("skill", skillName).
						Err(err).
						Msg("[ExperienceTracker] evaluate_presented: update_record_scores 失败")
					continue
				}
				logger.Info(logComponent).
					Int("updated_count", len(updates)).
					Str("skill", skillName).
					Msg("[ExperienceTracker] async evaluation updated records")
			}
		}
	}
	return nil
}

// ClearSession 清理指定 session 的追踪数据。
func (t *ExperienceTracker) ClearSession(sessionID string) {
	sessionPresentedRecords[sessionID] = nil
	sessionEvalCounter[sessionID] = 0
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ToMap 转换为 map[string]any，用于调用 EvolutionStore.UpdateRecordScores。
func (u *RecordScoreUpdate) ToMap() map[string]any {
	return map[string]any{
		"score":       u.Score,
		"usage_stats": u.UsageStats,
	}
}

// updatesToMap 将 RecordScoreUpdate map 转换为 UpdateRecordScores 所需的 map[string]map[string]any 格式。
func updatesToMap(updates map[string]*RecordScoreUpdate) map[string]map[string]any {
	result := make(map[string]map[string]any, len(updates))
	for id, update := range updates {
		result[id] = update.ToMap()
	}
	return result
}

// filterBodyRecords 从记录列表中筛选 BODY 类型记录。
func filterBodyRecords(records []checkpointing.EvolutionRecord) []checkpointing.EvolutionRecord {
	var bodyRecords []checkpointing.EvolutionRecord
	for _, record := range records {
		if isBodyRecord(&record) {
			bodyRecords = append(bodyRecords, record)
		}
	}
	return bodyRecords
}

// isBodyRecord 判断是否为 BODY 类型记录。
// 对应 Python: ExperienceTracker._is_body_record()
func isBodyRecord(record *checkpointing.EvolutionRecord) bool {
	return record.Change.Target == signal.EvolutionTargetBody
}
