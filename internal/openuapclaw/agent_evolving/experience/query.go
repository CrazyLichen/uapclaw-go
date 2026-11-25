package experience

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExperienceQueryService 只读经验查询服务（演进工具与宿主命令共用）。
// Python: ExperienceQueryService (experience/query.py:77-159)。
type ExperienceQueryService struct {
	// store 演进存储（checkpointing.EvolutionStore）。
	store *checkpointing.EvolutionStore
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SplitExperienceIndexQueryTerms 切分字面 OR 查询表达式供经验索引匹配。
// Python: split_experience_index_query_terms (query.py:13-23)——
// 按 | 分隔、小写、去重、剔除空白项。
func SplitExperienceIndexQueryTerms(query string) []string {
	terms := make([]string, 0)
	seen := make(map[string]bool)
	for _, rawTerm := range strings.Split(strings.ToLower(query), "|") {
		term := strings.TrimSpace(rawTerm)
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

// FilterExperienceIndexRecords 为有界的 agent 侧经验索引过滤记录。
// Python: filter_experience_index_records (query.py:26-60)——
// target/section 精确匹配、query 词袋命中 haystack；
// score_desc 主键 score 次键 timestamp 倒序，updated_desc 仅 timestamp 倒序，
// 其余取值报错。
func FilterExperienceIndexRecords(
	records []checkpointing.EvolutionRecord,
	target string,
	section string,
	query string,
	sortOrder string,
) ([]checkpointing.EvolutionRecord, error) {
	filtered := make([]checkpointing.EvolutionRecord, 0)
	queryTerms := SplitExperienceIndexQueryTerms(query)
	for _, record := range records {
		changeTarget := string(record.Change.Target)
		if target != "" && changeTarget != target {
			continue
		}
		if section != "" && record.Change.Section != section {
			continue
		}
		if len(queryTerms) > 0 {
			haystack := strings.ToLower(strings.Join([]string{
				derefString(record.Summary),
				record.ID,
				changeTarget,
				record.Change.Section,
			}, " "))
			matched := false
			for _, term := range queryTerms {
				if strings.Contains(haystack, term) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		filtered = append(filtered, record)
	}

	if sortOrder == "updated_desc" {
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Timestamp > filtered[j].Timestamp
		})
		return filtered, nil
	}
	if sortOrder != "score_desc" {
		return nil, fmt.Errorf("sort must be score_desc or updated_desc")
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Score != filtered[j].Score {
			return filtered[i].Score > filtered[j].Score
		}
		return filtered[i].Timestamp > filtered[j].Timestamp
	})
	return filtered, nil
}

// NewExperienceQueryService 创建只读查询服务。
// Python: ExperienceQueryService.__init__ (query.py:80-81)。
func NewExperienceQueryService(store *checkpointing.EvolutionStore) *ExperienceQueryService {
	return &ExperienceQueryService{store: store}
}

// ListExperiences 返回有界的结构化经验元数据（游标分页）。
// Python: list_experiences (query.py:83-125)。
func (s *ExperienceQueryService) ListExperiences(
	ctx context.Context,
	subject map[string]any,
	minScore *float64,
	limit int,
	cursor string,
	target string,
	section string,
	query string,
	sortOrder string,
) (map[string]any, error) {
	normalizedSubject, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return nil, err
	}
	skillName := normalizedSubject.Name
	// Python list_experiences: skill_exists(skill_name, subject_kind=normalized_subject.kind)
	if !s.store.SkillExistsForKind(ctx, skillName, normalizedSubject.Kind) {
		return nil, fmt.Errorf("skill not found: %s", skillName)
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("limit must be between 1 and 100")
	}

	records := s.store.GetRecordsByScoreForKind(ctx, skillName, minScore, normalizedSubject.Kind)
	filtered, err := FilterExperienceIndexRecords(records, target, section, query, sortOrder)
	if err != nil {
		return nil, err
	}
	page, nextCursor := paginateExperienceIndex(filtered, cursor, limit)
	items := make([]map[string]any, 0, len(page))
	for i := range page {
		items = append(items, toIndexItem(&page[i]))
	}
	result := map[string]any{
		"success":     true,
		"operation":   "list",
		"subject":     normalizedSubject.ToPayload(),
		"total_count": len(filtered),
		"has_more":    nextCursor != "",
		"items":       items,
	}
	if nextCursor != "" {
		result["next_cursor"] = nextCursor
	} else {
		result["next_cursor"] = nil
	}
	return result, nil
}

// ReadExperiences 读取选定的完整经验记录。
// Python: read_experiences (query.py:127-159)。
func (s *ExperienceQueryService) ReadExperiences(
	ctx context.Context,
	subject map[string]any,
	recordIDs []string,
	maxContentChars int,
) (map[string]any, error) {
	normalizedSubject, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return nil, err
	}
	skillName := normalizedSubject.Name
	if !s.store.SkillExistsForKind(ctx, skillName, normalizedSubject.Kind) {
		return nil, fmt.Errorf("skill not found: %s", skillName)
	}
	if len(recordIDs) == 0 {
		return nil, fmt.Errorf("record_ids must be a non-empty list")
	}
	if maxContentChars < 1 || maxContentChars > 20000 {
		return nil, fmt.Errorf("max_content_chars must be between 1 and 20000")
	}

	records := s.store.LoadRecordsByIdsForKind(ctx, skillName, recordIDs, normalizedSubject.Kind)
	foundIDs := make(map[string]bool, len(records))
	for i := range records {
		foundIDs[records[i].ID] = true
	}
	missingIDs := make([]string, 0)
	for _, recordID := range recordIDs {
		if !foundIDs[recordID] {
			missingIDs = append(missingIDs, recordID)
		}
	}
	if len(missingIDs) > 0 {
		return nil, fmt.Errorf("unknown record_ids: %s", strings.Join(missingIDs, ", "))
	}
	items := make([]map[string]any, 0, len(records))
	for i := range records {
		items = append(items, toReadItem(&records[i], maxContentChars))
	}
	return map[string]any{
		"success":   true,
		"operation": "read",
		"subject":   normalizedSubject.ToPayload(),
		"items":     items,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// paginateExperienceIndex 返回经验索引的一页游标结果。
// Python: _paginate_experience_index (query.py:63-74)——零基字符串游标。
func paginateExperienceIndex(
	records []checkpointing.EvolutionRecord,
	cursor string,
	limit int,
) ([]checkpointing.EvolutionRecord, string) {
	start := 0
	if cursor != "" {
		if _, err := fmt.Sscanf(cursor, "%d", &start); err != nil {
			start = 0
		}
	}
	if start < 0 {
		start = 0
	}
	end := start + limit
	pageEnd := end
	if pageEnd > len(records) {
		pageEnd = len(records)
	}
	var page []checkpointing.EvolutionRecord
	if start < len(records) {
		page = records[start:pageEnd]
	} else {
		page = []checkpointing.EvolutionRecord{}
	}
	if end < len(records) {
		return page, fmt.Sprintf("%d", end)
	}
	return page, ""
}

// toIndexItem 构造索引条目。
// Python: _to_index_item (query.py:162-171)。
func toIndexItem(record *checkpointing.EvolutionRecord) map[string]any {
	return map[string]any{
		"record_id":  record.ID,
		"summary":    derefString(record.Summary),
		"target":     string(record.Change.Target),
		"section":    record.Change.Section,
		"score":      record.Score,
		"updated_at": record.Timestamp,
	}
}

// toReadItem 构造读取条目（含截断正文）。
// Python: _to_read_item (query.py:174-183)。
func toReadItem(record *checkpointing.EvolutionRecord, maxContentChars int) map[string]any {
	item := toIndexItem(record)
	content := record.Change.Content
	truncated := len(content) > maxContentChars
	if truncated {
		content = content[:maxContentChars]
	}
	item["content"] = content
	item["content_truncated"] = truncated
	return item
}

// derefString 解引用可选字符串（nil → 空串）。
// Python: record.summary or "" 的等价形态（Summary 为 *string）。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
