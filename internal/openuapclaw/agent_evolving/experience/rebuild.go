// 重建生命周期服务（prepare deterministic rebuild context，不生成重建正文）。
//
// 对应 Python 代码：openjiuwen/agent_evolving/experience/rebuild.py（138 行 1:1）。
// Python async 语义映射：Go store API 为同步形态，async 去 await 直调（既定映射）。
package experience

import (
	"context"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
)

// rebuildDefaults 构造缺省值。
// Python: prepare_rebuild_context 形参缺省（rebuild.py:27-29）。
const (
	defaultRebuildMinScore          = 0.5
	defaultRebuildMaxContextRecords = 40
	defaultRebuildMaxContextChars   = 20000
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExperienceRebuildService 准备确定性重建上下文（不生成重建正文）。
// Python: ExperienceRebuildService（rebuild.py:15-71）。
type ExperienceRebuildService struct {
	store          *checkpointing.EvolutionStore
	archiveService *EvolutionArchiveService
}

// NewExperienceRebuildService 构造重建服务（archive_service 缺省自建）。
// Python: __init__（rebuild.py:18-20）。
func NewExperienceRebuildService(store *checkpointing.EvolutionStore, archiveService *EvolutionArchiveService) *ExperienceRebuildService {
	if archiveService == nil {
		archiveService = NewEvolutionArchiveService(store)
	}
	return &ExperienceRebuildService{store: store, archiveService: archiveService}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PrepareRebuildContext 归档当前状态、过滤重建输入，返回结构化上下文。
// Python: prepare_rebuild_context()（rebuild.py:22-71，async）。
//
// 返回 map 键（Python context.update 字典）：subject/skill_name/user_intent/min_score/
// archive_path/archive_pair/archive_version/archive_error/records/overflow_index。
// archiveError 非nil 时以 error 值形态回传（Python 存 Exception 对象）。
func (s *ExperienceRebuildService) PrepareRebuildContext(
	ctx context.Context,
	subject map[string]any,
	userIntent *string,
	minScore float64,
	maxContextRecords int,
	maxContextChars int,
) (map[string]any, error) {
	// Python: normalized_subject = normalize_subject(subject)
	normalizedSubject, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return nil, err
	}
	skillName := normalizedSubject.Name
	subjectKind := normalizedSubject.Kind

	// Python: if not self._store.skill_exists(skill_name, subject_kind=...): return None
	if !s.store.SkillExistsForKind(ctx, skillName, subjectKind) {
		return nil, nil
	}

	// Python: records_log = await self._store.load_full_evolution_log(skill_name, subject_kind=...)
	recordsLog, err := s.store.LoadFullEvolutionLogForKind(ctx, skillName, subjectKind)
	if err != nil {
		return nil, err
	}

	// Python: archive_pair/archive_error try 块（:40-46）
	var archivePair *EvolutionArchivePair
	var archiveError error
	archivePair, archiveError = s.archiveService.ArchiveCurrentPair(ctx, skillName, subjectKind)
	if archiveError != nil {
		logger.Warn(logComponent).Err(archiveError).Str("skill", skillName).
			Msg("[ExperienceRebuildService] archive pair failed")
	}

	var entries []checkpointing.EvolutionRecord
	if recordsLog != nil {
		entries = recordsLog.Entries
	}
	filteredRecords := filterRebuildRecords(entries, minScore)
	context := buildRebuildContextPayload(filteredRecords, maxContextRecords, maxContextChars)

	intent := any(nil)
	if userIntent != nil {
		intent = *userIntent
	}
	var archivePath any
	var archivePairPayload any
	var archiveVersion any
	if archivePair != nil {
		archivePath = archivePair.EvolutionArchiveName()
		archivePairPayload = archivePair.ToPayload()
		archiveVersion = archivePair.Version
	}
	var archiveErrPayload any
	if archiveError != nil {
		archiveErrPayload = archiveError.Error()
	}
	context["subject"] = normalizedSubject.ToPayload()
	context["skill_name"] = skillName
	context["user_intent"] = intent
	context["min_score"] = minScore
	context["archive_path"] = archivePath
	context["archive_pair"] = archivePairPayload
	context["archive_version"] = archiveVersion
	context["archive_error"] = archiveErrPayload

	// Python: if archive_pair: await store.clear_evolutions(...); archive_service.prune(...)
	if archivePair != nil {
		if err := s.store.ClearEvolutionsForKind(ctx, skillName, subjectKind); err != nil {
			return nil, err
		}
		s.archiveService.Prune(ctx, skillName, subjectKind)
	}

	return context, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// filterRebuildRecords 按最低分与 skip_reason 过滤重建输入。
// Python: _filter_rebuild_records()（rebuild.py:74-83）。
func filterRebuildRecords(records []checkpointing.EvolutionRecord, minScore float64) []checkpointing.EvolutionRecord {
	filtered := make([]checkpointing.EvolutionRecord, 0, len(records))
	for _, record := range records {
		if record.Score < minScore {
			continue
		}
		if record.Change.SkipReason != nil && *record.Change.SkipReason != "" {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered
}

// buildRebuildContextPayload 排序/截断并构造 records + overflow_index。
// Python: _build_rebuild_context_payload()（rebuild.py:86-115）。
func buildRebuildContextPayload(records []checkpointing.EvolutionRecord, maxRecords, maxChars int) map[string]any {
	sortedRecords := append([]checkpointing.EvolutionRecord(nil), records...)
	// Python: sorted(records, key=(score, timestamp), reverse=True)
	sort.SliceStable(sortedRecords, func(i, j int) bool {
		if sortedRecords[i].Score != sortedRecords[j].Score {
			return sortedRecords[i].Score > sortedRecords[j].Score
		}
		return sortedRecords[i].Timestamp > sortedRecords[j].Timestamp
	})
	overflow := []checkpointing.EvolutionRecord(nil)
	included := sortedRecords
	if len(sortedRecords) > maxRecords {
		included = sortedRecords[:maxRecords]
		overflow = append(overflow, sortedRecords[maxRecords:]...)
	}

	items := make([]map[string]any, 0, len(included))
	usedChars := 0
	for index, record := range included {
		content := record.Change.Content
		remainingChars := maxChars - usedChars
		if remainingChars < 0 {
			remainingChars = 0
		}
		// Python: if remaining_chars == 0: overflow = included[index:] + overflow; break
		if remainingChars == 0 {
			overflow = append(append([]checkpointing.EvolutionRecord(nil), included[index:]...), overflow...)
			break
		}
		clippedContent := content
		if len(content) > remainingChars {
			clippedContent = content[:remainingChars]
		}
		usedChars += len(clippedContent)
		items = append(items, toRebuildItem(&record, clippedContent))
	}

	overflowItems := make([]map[string]any, 0, len(overflow))
	for _, record := range overflow {
		overflowItems = append(overflowItems, toIndexItem(&record))
	}
	return map[string]any{
		"records":        items,
		"overflow_index": map[string]any{"items": overflowItems},
	}
}

// toRebuildItem 重建条目（索引条目 + content）。
// Python: _to_rebuild_item()（rebuild.py:118-121）；toIndexItem 复用 query.go 同形实现。
func toRebuildItem(record *checkpointing.EvolutionRecord, content string) map[string]any {
	item := toIndexItem(record)
	item["content"] = content
	return item
}
