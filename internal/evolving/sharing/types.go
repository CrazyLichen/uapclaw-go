package sharing

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SharingMeta 每条经验的共享侧元数据。
//
// Python: openjiuwen/agent_evolving/sharing/types.py SharingMeta
type SharingMeta struct {
	// SkillName 技能名称
	SkillName string
	// SkillVersion 技能版本
	SkillVersion string
	// UploadTrigger 上传触发方式，默认 "user_approval"
	UploadTrigger string
	// UploadAt 上传时间（UTC ISO）
	UploadAt string
	// FeedbackExcerpt 反馈摘录（可选）
	FeedbackExcerpt *string
	// SourceUserID 来源用户 ID（可选）
	SourceUserID *string
	// Confidence 置信度，默认 0.7
	Confidence float64
	// OriginBundleID 来源 bundle ID（可选）
	OriginBundleID *string
}

// SharedExperience EvolutionRecord 的共享包装。
//
// 所有共享专属元数据（keywords/summary/SharingMeta）在此类型上，
// 不侵入 EvolutionRecord 本身。
//
// Python: openjiuwen/agent_evolving/sharing/types.py SharedExperience
type SharedExperience struct {
	// Record 底层演进记录
	Record checkpointing.EvolutionRecord
	// Keywords 关键词列表
	Keywords []string
	// Summary 摘要
	Summary string
	// SharingMeta 共享元数据（可选）
	SharingMeta *SharingMeta
}

// SharedSkillBundle 后端存储最小单元：一个 skill_id 下的经验集合。
//
// Python: openjiuwen/agent_evolving/sharing/types.py SharedSkillBundle
type SharedSkillBundle struct {
	// BundleID Bundle 标识，格式: sb_{uuid10hex}
	BundleID string
	// SkillID 技能 ID
	SkillID string
	// SkillName 技能名称
	SkillName string
	// SkillVersion 技能版本
	SkillVersion string
	// KeywordsAggregate 聚合关键词
	KeywordsAggregate []string
	// SummaryAggregate 聚合摘要
	SummaryAggregate string
	// Experiences 经验列表
	Experiences []SharedExperience
	// CreatedAt 创建时间（UTC ISO）
	CreatedAt string
}

// SkillPackageMeta Hub 上技能包的元数据。
//
// Python: openjiuwen/agent_evolving/sharing/types.py SkillPackageMeta
type SkillPackageMeta struct {
	// SkillID 技能 ID
	SkillID string
	// SkillName 技能名称
	SkillName string
	// Description 描述
	Description string
	// UploadedAt 上传时间（UTC ISO）
	UploadedAt string
}

// SkillSearchResult 搜索结果行。
//
// Python: openjiuwen/agent_evolving/sharing/types.py SkillSearchResult
type SkillSearchResult struct {
	// SkillID 技能 ID
	SkillID string
	// SkillName 技能名称
	SkillName string
	// Description 描述
	Description string
	// ExperienceCount 经验数量
	ExperienceCount int
	// Keywords 关键词
	Keywords []string
	// Score 相关性分数
	Score float64
}

// QueryKeywords 下载路径检索关键词集。
//
// Python: openjiuwen/agent_evolving/sharing/types.py QueryKeywords
type QueryKeywords struct {
	// Keywords 检索关键词
	Keywords []string
	// Intent 查询意图描述
	Intent string
	// RawExcerpt 原始摘录
	RawExcerpt string
}

// UploadResult 上传结果。
//
// Python: openjiuwen/agent_evolving/sharing/types.py UploadResult
type UploadResult struct {
	// OK 是否成功
	OK bool
	// BundleID Bundle 唯一标识
	BundleID string
	// Reason 失败原因
	Reason string
	// Retryable 是否可重试
	Retryable bool
}

// DroppedRecord 被 QC 丢弃的记录及原因。
//
// Python: StagingResult.dropped_for_share 类型 List[Tuple[EvolutionRecord, str]]
type DroppedRecord struct {
	// Record 被丢弃的演进记录
	Record checkpointing.EvolutionRecord
	// Reason 丢弃原因
	Reason string
}

// StagingResult ShareStager 筛选结果。
//
// Python: openjiuwen/agent_evolving/sharing/types.py StagingResult
type StagingResult struct {
	// StagedForShare 通过 QC 的经验
	StagedForShare []SharedExperience
	// DroppedForShare 被 QC 丢弃的记录+原因
	DroppedForShare []DroppedRecord
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSharingMeta 创建 SharingMeta，设置默认值。
func NewSharingMeta(skillName string) *SharingMeta {
	return &SharingMeta{
		SkillName:     skillName,
		UploadTrigger: "user_approval",
		UploadAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Confidence:    0.7,
	}
}

// MakeSharedSkillBundle 创建 SharedSkillBundle，聚合关键词并去重。
//
// Python: SharedSkillBundle.make()
func MakeSharedSkillBundle(
	skillName string,
	experiences []SharedExperience,
	skillVersion string,
	summaryAggregate string,
) *SharedSkillBundle {
	seen := make(map[string]bool)
	var keywordsAggregate []string
	for _, exp := range experiences {
		for _, kw := range exp.Keywords {
			if kw != "" && !seen[kw] {
				seen[kw] = true
				keywordsAggregate = append(keywordsAggregate, kw)
			}
		}
	}
	if summaryAggregate == "" {
		var parts []string
		for _, exp := range experiences {
			if exp.Summary != "" {
				parts = append(parts, exp.Summary)
			}
		}
		// Python: "; ".join(...)
		for i, p := range parts {
			if i > 0 {
				summaryAggregate += "; "
			}
			summaryAggregate += p
		}
	}
	return &SharedSkillBundle{
		BundleID:          newBundleID(),
		SkillName:         skillName,
		SkillVersion:      skillVersion,
		KeywordsAggregate: keywordsAggregate,
		SummaryAggregate:  summaryAggregate,
		Experiences:       experiences,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// EmptyStagingResult 创建空的 StagingResult。
//
// Python: StagingResult.empty()
func EmptyStagingResult() *StagingResult {
	return &StagingResult{
		StagedForShare:  []SharedExperience{},
		DroppedForShare: []DroppedRecord{},
	}
}

// HasShareable StagingResult 是否有可共享的经验。
//
// Python: StagingResult.has_shareable
func (s *StagingResult) HasShareable() bool {
	return len(s.StagedForShare) > 0
}

// ToDict 将 SharingMeta 转换为字典形式。
//
// Python: SharingMeta.to_dict()
func (m *SharingMeta) ToDict() map[string]any {
	payload := map[string]any{
		"skill_name":     m.SkillName,
		"skill_version":  m.SkillVersion,
		"upload_trigger": m.UploadTrigger,
		"upload_at":      m.UploadAt,
		"confidence":     m.Confidence,
	}
	if m.FeedbackExcerpt != nil {
		payload["feedback_excerpt"] = *m.FeedbackExcerpt
	}
	if m.SourceUserID != nil {
		payload["source_user_id"] = *m.SourceUserID
	}
	if m.OriginBundleID != nil {
		payload["origin_bundle_id"] = *m.OriginBundleID
	}
	return payload
}

// FromDictSharingMeta 从字典创建 SharingMeta。
//
// Python: SharingMeta.from_dict()
func FromDictSharingMeta(data map[string]any) *SharingMeta {
	if data == nil {
		data = map[string]any{}
	}
	m := &SharingMeta{
		SkillName:     getStr(data, "skill_name", ""),
		SkillVersion:  getStr(data, "skill_version", ""),
		UploadTrigger: getStr(data, "upload_trigger", "user_approval"),
		UploadAt:      getStr(data, "upload_at", time.Now().UTC().Format(time.RFC3339Nano)),
		Confidence:    getFloat(data, "confidence", 0.7),
	}
	if v, ok := data["feedback_excerpt"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		m.FeedbackExcerpt = &s
	}
	if v, ok := data["source_user_id"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		m.SourceUserID = &s
	}
	if v, ok := data["origin_bundle_id"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		m.OriginBundleID = &s
	}
	return m
}

// ToDict 将 SharedExperience 转换为字典形式。
//
// Python: SharedExperience.to_dict()
func (e *SharedExperience) ToDict() map[string]any {
	keywords := e.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	payload := map[string]any{
		"record":   e.Record.ToDict(),
		"keywords": keywords,
		"summary":  e.Summary,
	}
	if e.SharingMeta != nil {
		payload["sharing_meta"] = e.SharingMeta.ToDict()
	} else {
		payload["sharing_meta"] = nil
	}
	return payload
}

// FromDictSharedExperience 从字典创建 SharedExperience。
//
// Python: SharedExperience.from_dict()
func FromDictSharedExperience(data map[string]any) (*SharedExperience, error) {
	if data == nil {
		data = map[string]any{}
	}
	recordData, _ := data["record"].(map[string]any)
	record, err := checkpointing.FromDictEvolutionRecord(recordData)
	if err != nil {
		return nil, fmt.Errorf("解析 SharedExperience.record 失败: %w", err)
	}
	keywords := toStringSlice(data["keywords"])
	smData := data["sharing_meta"]
	var sharingMeta *SharingMeta
	if m, ok := smData.(map[string]any); ok {
		sharingMeta = FromDictSharingMeta(m)
	}
	return &SharedExperience{
		Record:      *record,
		Keywords:    keywords,
		Summary:     getStr(data, "summary", ""),
		SharingMeta: sharingMeta,
	}, nil
}

// ToDict 将 SharedSkillBundle 转换为字典形式。
//
// Python: SharedSkillBundle.to_dict()
func (b *SharedSkillBundle) ToDict() map[string]any {
	experiences := make([]map[string]any, len(b.Experiences))
	for i, exp := range b.Experiences {
		experiences[i] = exp.ToDict()
	}
	keywordsAggregate := b.KeywordsAggregate
	if keywordsAggregate == nil {
		keywordsAggregate = []string{}
	}
	return map[string]any{
		"bundle_id":          b.BundleID,
		"skill_id":           b.SkillID,
		"skill_name":         b.SkillName,
		"skill_version":      b.SkillVersion,
		"keywords_aggregate": keywordsAggregate,
		"summary_aggregate":  b.SummaryAggregate,
		"experiences":        experiences,
		"created_at":         b.CreatedAt,
	}
}

// FromDictSharedSkillBundle 从字典创建 SharedSkillBundle。
//
// Python: SharedSkillBundle.from_dict()
func FromDictSharedSkillBundle(data map[string]any) (*SharedSkillBundle, error) {
	if data == nil {
		data = map[string]any{}
	}
	skillID := getStr(data, "skill_id", "")
	if skillID == "" {
		// Python: skill_id = data.get("skill_content_hash", "") or ""
		skillID = getStr(data, "skill_content_hash", "")
	}
	experiencesData, _ := data["experiences"].([]any)
	var experiences []SharedExperience
	for _, item := range experiencesData {
		if m, ok := item.(map[string]any); ok {
			exp, err := FromDictSharedExperience(m)
			if err != nil {
				continue
			}
			experiences = append(experiences, *exp)
		}
	}
	return &SharedSkillBundle{
		BundleID:          getStr(data, "bundle_id", newBundleID()),
		SkillID:           skillID,
		SkillName:         getStr(data, "skill_name", ""),
		SkillVersion:      getStr(data, "skill_version", ""),
		KeywordsAggregate: toStringSlice(data["keywords_aggregate"]),
		SummaryAggregate:  getStr(data, "summary_aggregate", ""),
		Experiences:       experiences,
		CreatedAt:         getStr(data, "created_at", time.Now().UTC().Format(time.RFC3339Nano)),
	}, nil
}

// ToDict 将 SkillPackageMeta 转换为字典形式。
//
// Python: SkillPackageMeta.to_dict()
func (m *SkillPackageMeta) ToDict() map[string]any {
	return map[string]any{
		"skill_id":    m.SkillID,
		"skill_name":  m.SkillName,
		"description": m.Description,
		"uploaded_at": m.UploadedAt,
	}
}

// FromDictSkillPackageMeta 从字典创建 SkillPackageMeta。
//
// Python: SkillPackageMeta.from_dict()
func FromDictSkillPackageMeta(data map[string]any) *SkillPackageMeta {
	if data == nil {
		data = map[string]any{}
	}
	return &SkillPackageMeta{
		SkillID:     getStr(data, "skill_id", ""),
		SkillName:   getStr(data, "skill_name", ""),
		Description: getStr(data, "description", ""),
		UploadedAt:  getStr(data, "uploaded_at", time.Now().UTC().Format(time.RFC3339Nano)),
	}
}

// ToDict 将 SkillSearchResult 转换为字典形式。
//
// Python: SkillSearchResult.to_dict()
func (r *SkillSearchResult) ToDict() map[string]any {
	return map[string]any{
		"skill_id":         r.SkillID,
		"skill_name":       r.SkillName,
		"description":      r.Description,
		"experience_count": r.ExperienceCount,
		"keywords":         r.Keywords,
		"score":            r.Score,
	}
}

// FromDictSkillSearchResult 从字典创建 SkillSearchResult。
//
// Python: SkillSearchResult.from_dict()
func FromDictSkillSearchResult(data map[string]any) *SkillSearchResult {
	if data == nil {
		data = map[string]any{}
	}
	return &SkillSearchResult{
		SkillID:         getStr(data, "skill_id", ""),
		SkillName:       getStr(data, "skill_name", ""),
		Description:     getStr(data, "description", ""),
		ExperienceCount: getInt(data, "experience_count", 0),
		Keywords:        toStringSlice(data["keywords"]),
		Score:           getFloat(data, "score", 0.0),
	}
}

// ToDict 将 QueryKeywords 转换为字典形式。
//
// Python: QueryKeywords.to_dict()
func (q *QueryKeywords) ToDict() map[string]any {
	return map[string]any{
		"keywords":    q.Keywords,
		"intent":      q.Intent,
		"raw_excerpt": q.RawExcerpt,
	}
}

// FromDictQueryKeywords 从字典创建 QueryKeywords。
//
// Python: QueryKeywords.from_dict()
func FromDictQueryKeywords(data map[string]any) *QueryKeywords {
	if data == nil {
		data = map[string]any{}
	}
	return &QueryKeywords{
		Keywords:   toStringSlice(data["keywords"]),
		Intent:     getStr(data, "intent", ""),
		RawExcerpt: getStr(data, "raw_excerpt", ""),
	}
}

// MarshalJSON 为 UploadResult 实现自定义 JSON 序列化。
func (r UploadResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"ok":        r.OK,
		"bundle_id": r.BundleID,
		"reason":    r.Reason,
		"retryable": r.Retryable,
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newBundleID 生成 bundle ID，格式: sb_{uuid10hex}。
// Python: _new_bundle_id()
func newBundleID() string {
	return fmt.Sprintf("sb_%010x", time.Now().UnixNano()&0x3FFFFFFFFF)
}

// getStr 从 map 安全提取 string。
func getStr(data map[string]any, key, defaultVal string) string {
	if v, ok := data[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return defaultVal
}

// getInt 从 map 安全提取 int。
func getInt(data map[string]any, key string, defaultVal int) int {
	if v, ok := data[key]; ok && v != nil {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return defaultVal
}

// getFloat 从 map 安全提取 float64。
func getFloat(data map[string]any, key string, defaultVal float64) float64 {
	if v, ok := data[key]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return defaultVal
}

// toStringSlice 从 any 类型安全提取 []string。
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	slice, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		s := fmt.Sprintf("%v", item)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}
