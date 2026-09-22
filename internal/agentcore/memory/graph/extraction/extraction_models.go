package extraction

// ──────────────────────────── 结构体 ────────────────────────────

// Datetime 日期时间（未使用）
//
// Python: Datetime (MultilingualBaseModel)
type Datetime struct {
	// Year 年
	Year int `json:"year" jsonschema:"description={{[year]}}"`
	// Month 月
	Month int `json:"month" jsonschema:"description={{[month]}}"`
	// Day 日
	Day int `json:"day" jsonschema:"description={{[day]}}"`
	// Hour 小时
	Hour int `json:"hour" jsonschema:"description={{[hour]}}"`
	// Minute 分钟
	Minute int `json:"minute" jsonschema:"description={{[minute]}}"`
	// Second 秒
	Second int `json:"second" jsonschema:"description={{[second]}}"`
}

// EntityDeclaration 实体声明
//
// Python: EntityDeclaration (MultilingualBaseModel)
type EntityDeclaration struct {
	// Name 新提取实体的名字
	Name string `json:"name" jsonschema:"description={{[ent_def_name]}}"`
	// EntityTypeID 新提取实体的类型id
	EntityTypeID int `json:"entity_type_id" jsonschema:"description={{[ent_def_type]}}"`
}

// Duplication 实体去重
//
// Python: Duplication (MultilingualBaseModel)
type Duplication struct {
	// Name 现有实体的名字
	Name string `json:"name" jsonschema:"description={{[ent_dupe_name]}}"`
	// ID 现有实体的ID
	ID int `json:"id" jsonschema:"description={{[ent_dupe_id]}}"`
	// DuplicateIDs 重复实体ID列表
	DuplicateIDs []int `json:"duplicate_ids" jsonschema:"description={{[ent_dupe_id_list]}}"`
}

// Fact 事实关系
//
// Python: Fact (MultilingualBaseModel)
type Fact struct {
	// Name 关系名称
	Name string `json:"name" jsonschema:"description={{[rel_name]}}"`
	// Fact 事实内容
	Fact string `json:"fact" jsonschema:"description={{[rel_fact]}}"`
	// ValidSince 生效日期
	ValidSince string `json:"valid_since" jsonschema:"description={{[rel_valid_since]}}"`
	// ValidUntil 中止日期
	ValidUntil string `json:"valid_until" jsonschema:"description={{[rel_valid_until]}}"`
	// SourceID 主体实体ID
	SourceID int `json:"source_id" jsonschema:"description={{[rel_source_id]}}"`
	// TargetID 客体实体ID
	TargetID int `json:"target_id" jsonschema:"description={{[rel_target_id]}}"`
}

// PossibleTimezone 可能的时区
//
// Python: PossibleTimezone (MultilingualBaseModel)
type PossibleTimezone struct {
	// Name 时区名称
	Name string `json:"name" jsonschema:"description={{[tz_name]}}"`
	// OffsetFromUTC UTC 偏移量
	OffsetFromUTC string `json:"offset_from_utc" jsonschema:"description={{[tz_offset]}}"`
	// Reasoning 推理原因
	Reasoning string `json:"reasoning" jsonschema:"description={{[tz_reason]}}"`
}

// EntityExtraction 实体声明抽取输出模型
//
// Python: EntityExtraction (MultilingualBaseModel)
type EntityExtraction struct {
	// ExtractedEntities 新提取的实体列表
	ExtractedEntities []EntityDeclaration `json:"extracted_entities" jsonschema:"description={{[ent_ext_list]}}"`
}

// EntitySummary 实体摘要与属性输出模型
//
// Python: EntitySummary (MultilingualBaseModel)
type EntitySummary struct {
	// Summary 实体摘要
	Summary string `json:"summary" jsonschema:"description={{[ent_summary]}}"`
	// Attributes 实体属性
	Attributes map[string]any `json:"attributes" jsonschema:"description={{[ent_attributes]}}"`
}

// EntityDuplication 实体去重输出模型
//
// Python: EntityDuplication (MultilingualBaseModel)
type EntityDuplication struct {
	// DuplicatedEntities 重复实体列表
	DuplicatedEntities []Duplication `json:"duplicated_entities" jsonschema:"description={{[ent_dupe_list]}}"`
}

// RelationExtraction 关系抽取输出模型
//
// Python: RelationExtraction (MultilingualBaseModel)
type RelationExtraction struct {
	// ExtractedRelations 新提取的关系列表
	ExtractedRelations []Fact `json:"extracted_relations" jsonschema:"description={{[rel_ext_list]}}"`
}

// RelevantFacts 关系过滤输出模型
//
// Python: RelevantFacts (MultilingualBaseModel)
type RelevantFacts struct {
	// BriefReasoning 过滤推理
	BriefReasoning string `json:"brief_reasoning" jsonschema:"description={{[rel_filter_reasoning]}}"`
	// RelevantRelations 相关关系ID列表
	RelevantRelations []int `json:"relevant_relations" jsonschema:"description={{[rel_filter_list]}}"`
}

// TimezonePredictions 时区预测输出模型
//
// Python: TimezonePredictions (MultilingualBaseModel)
type TimezonePredictions struct {
	// ExtractedRelations 可能的时区列表
	ExtractedRelations []PossibleTimezone `json:"extracted_relations" jsonschema:"description={{[tz_list]}}"`
}

// MergeRelations 关系合并输出模型
//
// Python: MergeRelations (MultilingualBaseModel)
type MergeRelations struct {
	// NeedMerging 是否需要合并
	NeedMerging bool `json:"need_merging" jsonschema:"description={{[rel_dupe_need_merge]}}"`
	// ShortReasoning 合并推理
	ShortReasoning string `json:"short_reasoning" jsonschema:"description={{[rel_dupe_reasoning]}}"`
	// CombinedContent 合并后内容
	CombinedContent string `json:"combined_content" jsonschema:"description={{[rel_dupe_content]}}"`
	// DuplicateIDs 需合并的关系ID列表
	DuplicateIDs []int `json:"duplicate_ids" jsonschema:"description={{[rel_dupe_id_list]}}"`
	// ValidSince 生效日期
	ValidSince string `json:"valid_since" jsonschema:"description={{[rel_valid_since]}}"`
	// ValidUntil 中止日期
	ValidUntil string `json:"valid_until" jsonschema:"description={{[rel_valid_until]}}"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
