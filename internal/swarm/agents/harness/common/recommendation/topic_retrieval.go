// Package recommendation 提供 topic 推荐的检索契约层。
//
// 本文件一比一对齐 topic_retrieval.py（1681 行）：
// PA-Bridge 与 VerbatimRAG 适配的话题推荐检索契约。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_retrieval.py
package recommendation

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TerminalRetrievalContractError PA 检索契约违规（对齐 topic_retrieval.py:70）。
type TerminalRetrievalContractError struct {
	// Reason 违规原因码
	Reason string
	// Detail 细节（可空）
	Detail string
	// ComponentReceipt 关联组件小票（可空）
	ComponentReceipt *LearnedComponentReceipt
}

// Error 实现 error（对齐 :83 f"{reason}{suffix}"）。
func (e *TerminalRetrievalContractError) Error() string {
	if e.Detail != "" {
		return e.Reason + ": " + e.Detail
	}
	return e.Reason
}

// EvidenceContractError 冻结文档/抽取器/准入契约 fail-closed（对齐 :86）。
type EvidenceContractError struct {
	// Reason 违规原因码
	Reason string
	// Detail 细节（可空）
	Detail string
	// ComponentReceipt 关联组件小票（可空）
	ComponentReceipt *LearnedComponentReceipt
}

// Error 实现 error（对齐 :99）。
func (e *EvidenceContractError) Error() string {
	if e.Detail != "" {
		return e.Reason + ": " + e.Detail
	}
	return e.Reason
}

// PATrainingExample 一条主动或被动 PA 训练元组，不含复制的私有文本（对齐 :128）。
type PATrainingExample struct {
	// UserStateSnapshotID 用户状态快照 ID
	UserStateSnapshotID string
	// QueryTextLocator 查询文本定位器（active 用，可空）
	QueryTextLocator *string
	// QuerySourceStarterID 查询来源起始 ID（passive 用，可空）
	QuerySourceStarterID *string
	// Domain 领域（active/passive）
	Domain string
	// PositiveLabel 正样本标记
	PositiveLabel bool
	// EventTime 事件时间戳
	EventTime float64
	// ContextScope 上下文范围
	ContextScope string
	// SourceArtifactID 来源产物 ID
	SourceArtifactID string
	// SchemaVersion 契约版本
	SchemaVersion string
}

// PassiveStarterExample 被动起始曝光样本（对齐 :201）。
type PassiveStarterExample struct {
	// UserStateSnapshotID 用户状态快照 ID
	UserStateSnapshotID string
	// ContextScope 上下文范围
	ContextScope string
	// CorpusVersion 语料版本
	CorpusVersion string
	// StarterID 起始 ID
	StarterID string
	// ExposureID 曝光 ID
	ExposureID string
	// ManifestID 清单 ID
	ManifestID string
	// CardRevisionID 卡片修订 ID
	CardRevisionID string
	// ExposedAt 曝光时间
	ExposedAt float64
	// ClickedAt 点击时间（可空）
	ClickedAt *float64
	// NotInterestedAt 不感兴趣时间（可空）
	NotInterestedAt *float64
	// FeatureVersionRefs 特征版本引用
	FeatureVersionRefs map[string]string
	// SchemaVersion 契约版本
	SchemaVersion string
}

// ActiveExpressionObservation 主动表达的产品结果，保留在主动训练排除闸之前（对齐 :304）。
type ActiveExpressionObservation struct {
	// UserStateSnapshotID 用户状态快照 ID
	UserStateSnapshotID string
	// SourceLocator 来源定位器
	SourceLocator string
	// ObservedAt 观察时间
	ObservedAt float64
	// InputOrigin 输入来源
	InputOrigin string
	// AssistanceOrigin 协助来源
	AssistanceOrigin string
	// ContextScope 上下文范围
	ContextScope string
	// SchemaVersion 契约版本
	SchemaVersion string
}

// ActiveExpressionExample 主动表达训练样本（对齐 :368）。
type ActiveExpressionExample struct {
	// UserStateSnapshotID 用户状态快照 ID
	UserStateSnapshotID string
	// SourceLocator 来源定位器
	SourceLocator string
	// ObservedAt 观察时间
	ObservedAt float64
	// InputOrigin 输入来源
	InputOrigin string
	// AssistanceOrigin 协助来源
	AssistanceOrigin string
	// ContextScope 上下文范围
	ContextScope string
	// SchemaVersion 契约版本
	SchemaVersion string
}

// ActiveExpressionAdmission 主动表达准入结果（对齐 :440）。
type ActiveExpressionAdmission struct {
	// Observation 原始观察
	Observation *ActiveExpressionObservation
	// Example 准入样本（被排除时为 nil）
	Example *ActiveExpressionExample
	// ExclusionReason 排除原因（未排除为空）
	ExclusionReason string
}

// PublicationDateProvenance 一个归一化发布时间戳的精确来源（对齐 :525）。
type PublicationDateProvenance struct {
	// SourceType 来源类型
	SourceType string
	// SourceField 来源字段
	SourceField string
	// RawValue 原始值
	RawValue string
	// SearchProviderID 搜索厂商 ID（可空）
	SearchProviderID string
	// SearchReceiptID 搜索回执 ID（可空）
	SearchReceiptID string
	// SearchResultURL 搜索结果 URL（可空）
	SearchResultURL string
	// LinkedPage 链接日期页（可空）
	LinkedPage map[string]any
	// ListedAt 搜索列表给同一 URL 的日期（page_explicit_metadata 专用，可空）
	ListedAt string
	// PagePublishedAt 页面自身的原始日期（structured_search_result 专用，可空）
	PagePublishedAt string
	// APIURL 发布方详情 API 的 URL（page_explicit_metadata 专用，可空）
	APIURL string
}

// FrozenDocumentRevision 一次生成快照内冻结一次的抓取页内容（对齐 :596）。
type FrozenDocumentRevision struct {
	// DocumentRevisionID 文档修订 ID
	DocumentRevisionID string
	// SourceURL 来源 URL
	SourceURL string
	// Publisher 发布方
	Publisher string
	// SourceKind 来源类别
	SourceKind string
	// PublishedAt 发布时间（ISO 串，空串对齐 Python None）
	PublishedAt string
	// FetchedAt 抓取时间
	FetchedAt float64
	// ImmutableContent 不可变正文（Python 字符索引语义 → Go 按 rune 处理）
	ImmutableContent string
	// SnapshotDate 快照日期
	SnapshotDate string
	// SnapshotVersion 快照版本
	SnapshotVersion string
	// PublicationDateProvenance 发布时间溯源（可空）
	PublicationDateProvenance *PublicationDateProvenance
}

// FrozenDocumentSnapshot 一次生成快照的冻结文档集合（对齐 :663）。
type FrozenDocumentSnapshot struct {
	// SnapshotDate 快照日期
	SnapshotDate string
	// SnapshotVersion 快照版本
	SnapshotVersion string
	// Documents 冻结文档
	Documents []FrozenDocumentRevision
}

// EvidenceCandidate 证据候选（对齐 :691）。
type EvidenceCandidate struct {
	// CandidateID 候选 ID
	CandidateID string
	// Title 标题
	Title string
	// PrimaryQuery 主查询
	PrimaryQuery string
	// FreshnessClass 新鲜度类别
	FreshnessClass string
	// ContextScope 上下文范围
	ContextScope string
	// Category 类别
	Category string
}

// ExactEvidenceSpan 精确证据片段（对齐 :732）。
type ExactEvidenceSpan struct {
	// CandidateID 候选 ID
	CandidateID string
	// DocumentRevisionID 文档修订 ID
	DocumentRevisionID string
	// SourceURL 来源 URL
	SourceURL string
	// Publisher 发布方
	Publisher string
	// SourceKind 来源类别
	SourceKind string
	// PublishedAt 发布时间（空串对齐 None）
	PublishedAt string
	// FetchedAt 抓取时间
	FetchedAt float64
	// SnapshotDate 快照日期
	SnapshotDate string
	// SnapshotVersion 快照版本
	SnapshotVersion string
	// Start 起始偏移（Python 字符索引）
	Start int
	// End 结束偏移（Python 字符索引）
	End int
	// ExactText 精确文本
	ExactText string
	// RetrievedAt 检索时间
	RetrievedAt float64
	// PublicationDateProvenance 发布时间溯源（可空）
	PublicationDateProvenance *PublicationDateProvenance
}

// ExtractedCandidateEvidence 一个候选在一份冻结快照内的抽取结果（对齐 :772）。
type ExtractedCandidateEvidence struct {
	// Candidate 冻结候选
	Candidate *EvidenceCandidate
	// SnapshotDate 快照日期
	SnapshotDate string
	// SnapshotVersion 快照版本
	SnapshotVersion string
	// Spans 证据片段
	Spans []ExactEvidenceSpan
	// ComponentReceipt 组件小票
	ComponentReceipt *LearnedComponentReceipt
	// ContractVersion 契约版本
	ContractVersion string
}

// CandidateID 候选 ID（对齐 :781 property candidate_id）。
func (e *ExtractedCandidateEvidence) CandidateID() string { return e.Candidate.CandidateID }

// EvidenceBundle 一个候选的完整证据束（对齐 :786）。
type EvidenceBundle struct {
	// CandidateID 候选 ID
	CandidateID string
	// ContextScope 上下文范围
	ContextScope string
	// Category 类别
	Category string
	// FreshnessClass 新鲜度类别
	FreshnessClass string
	// SnapshotDate 快照日期
	SnapshotDate string
	// SnapshotVersion 快照版本
	SnapshotVersion string
	// Spans 选定片段
	Spans []ExactEvidenceSpan
	// SourceGateMode 来源闸模式
	SourceGateMode string
	// ComponentReceipt 组件小票
	ComponentReceipt *LearnedComponentReceipt
	// ContractVersion 契约版本
	ContractVersion string
}

// EvidenceOffset 留出集人工核对偏移（对齐 :814）。
type EvidenceOffset struct {
	// DocumentRevisionID 文档修订 ID
	DocumentRevisionID string
	// Start 起始偏移（字符索引）
	Start int
	// End 结束偏移（字符索引）
	End int
}

// EvidenceHeldOutCase 一条人工精确核对的中文证据评估用例（对齐 :834）。
type EvidenceHeldOutCase struct {
	// CaseID 用例 ID
	CaseID string
	// FixtureVersion 夹具版本
	FixtureVersion string
	// Candidate 候选
	Candidate *EvidenceCandidate
	// Snapshot 冻结快照
	Snapshot *FrozenDocumentSnapshot
	// GoldOffsets 人工核对偏移
	GoldOffsets []EvidenceOffset
	// GoldShouldAdmit 金标准入
	GoldShouldAdmit bool
	// GoldReviewStatus 金标复核状态
	GoldReviewStatus string
}

// EvidenceHeldOutPrediction 一次留出集预测（对齐 :870）。
type EvidenceHeldOutPrediction struct {
	// CaseID 用例 ID
	CaseID string
	// PredictedOffsets 预测偏移
	PredictedOffsets []EvidenceOffset
	// Admitted 是否准入
	Admitted bool
	// LatencyMS 时延（毫秒）
	LatencyMS float64
	// ProviderCost 厂商成本
	ProviderCost float64
}

// EvidenceHeldOutMetrics 留出集聚合指标（对齐 :901，纯数据类无校验）。
type EvidenceHeldOutMetrics struct {
	// FixtureVersion 夹具版本
	FixtureVersion string
	// CaseCount 用例数
	CaseCount int
	// ExactOffsetValidity 精确偏移有效率
	ExactOffsetValidity float64
	// SpanPrecision 片段精确率
	SpanPrecision float64
	// SpanRecall 片段召回率
	SpanRecall float64
	// SpanF1 片段 F1
	SpanF1 float64
	// NoSupportRejection 无支撑拒绝率
	NoSupportRejection float64
	// AdmissionPrecision 准入精确率
	AdmissionPrecision float64
	// AdmissionCoverage 准入覆盖率
	AdmissionCoverage float64
	// MeanLatencyMS 平均时延
	MeanLatencyMS float64
	// TotalProviderCost 厂商总成本
	TotalProviderCost float64
}

// FixedStarterRecord 预置起始语料中一条不可变版本化条目（对齐 :1340）。
type FixedStarterRecord struct {
	// StarterID 起始 ID
	StarterID string
	// Title 标题
	Title string
	// Text 正文
	Text string
	// Topic 话题
	Topic string
	// HookType 钩子类型
	HookType string
	// FreshnessClass 新鲜度类别
	FreshnessClass string
	// Origin 来源
	Origin string
	// Category 类别
	Category string
	// ContextScopes 适用上下文范围
	ContextScopes []string
	// CorpusVersion 语料版本
	CorpusVersion string
}

// FrozenStarterCorpus 同时服务排序与物化的不可变语料版本（对齐 :1392）。
type FrozenStarterCorpus struct {
	// CorpusVersion 语料版本
	CorpusVersion string
	// Starters 语料条目
	Starters []FixedStarterRecord
}

// RankedStarterInterest 排序后的兴趣（对齐 :1415）。
type RankedStarterInterest struct {
	// InterestSummaryID 兴趣摘要 ID
	InterestSummaryID string
	// InterestSummary 兴趣摘要文本
	InterestSummary string
	// Rank 名次（从 1 起）
	Rank int
}

// FixedStarterHit 厂商排序 ID；语料文本有意缺失（对齐 :1434）。
type FixedStarterHit struct {
	// StarterID 起始 ID
	StarterID string
	// InterestSummaryID 兴趣摘要 ID（可空）
	InterestSummaryID string
	// Rank 名次
	Rank int
	// ComponentReceipt 组件小票
	ComponentReceipt *LearnedComponentReceipt
}

// FixedStarterRetrievalBatch 一次检索批次（对齐 :1444）。
type FixedStarterRetrievalBatch struct {
	// UserStateSnapshotID 用户状态快照 ID
	UserStateSnapshotID string
	// ContextScope 上下文范围
	ContextScope string
	// CorpusVersion 语料版本
	CorpusVersion string
	// Hits 排序命中
	Hits []FixedStarterHit
	// ComponentReceipt 组件小票
	ComponentReceipt *LearnedComponentReceipt
	// IgnoredProviderFields 被忽略的厂商字段
	IgnoredProviderFields []string
	// ContractVersion 契约版本
	ContractVersion string
}

// MaterializedFixedStarter 已取回文本的物化固定起始（对齐 :1455）。
type MaterializedFixedStarter struct {
	// StarterID 起始 ID
	StarterID string
	// InterestSummaryID 兴趣摘要 ID（可空）
	InterestSummaryID string
	// Rank 名次
	Rank int
	// Title 标题
	Title string
	// Text 正文
	Text string
	// Topic 话题
	Topic string
	// HookType 钩子类型
	HookType string
	// FreshnessClass 新鲜度类别
	FreshnessClass string
	// Origin 来源
	Origin string
	// Category 类别
	Category string
	// ContextScope 上下文范围
	ContextScope string
	// CorpusVersion 语料版本
	CorpusVersion string
	// ComponentReceipt 组件小票
	ComponentReceipt *LearnedComponentReceipt
}

// FixedStarterMaterializationBatch 一次物化批次（对齐 :1472）。
type FixedStarterMaterializationBatch struct {
	// UserStateSnapshotID 用户状态快照 ID
	UserStateSnapshotID string
	// ContextScope 上下文范围
	ContextScope string
	// CorpusVersion 语料版本
	CorpusVersion string
	// Candidates 物化候选
	Candidates []MaterializedFixedStarter
	// ComponentReceipt 组件小票
	ComponentReceipt *LearnedComponentReceipt
	// MaterializedStage 物化阶段
	MaterializedStage string
}

// I4FixedStarterCandidate I4 阶段固定起始候选（对齐 :1482）。
type I4FixedStarterCandidate struct {
	// StarterID 起始 ID
	StarterID string
	// InterestSummaryID 兴趣摘要 ID（可空）
	InterestSummaryID string
	// Rank 名次
	Rank int
	// Title 标题
	Title string
	// Text 正文
	Text string
	// Topic 话题
	Topic string
	// HookType 钩子类型
	HookType string
	// FreshnessClass 新鲜度类别
	FreshnessClass string
	// Origin 来源
	Origin string
	// Category 类别
	Category string
	// ContextScope 上下文范围
	ContextScope string
	// CorpusVersion 语料版本
	CorpusVersion string
	// PrimaryQuery 主查询
	PrimaryQuery string
	// PersonaContext 人设上下文
	PersonaContext string
	// Provenance 血缘
	Provenance map[string]any
	// ComponentReceipt 组件小票
	ComponentReceipt *LearnedComponentReceipt
}

// ──────────────────────────── 枚举 ────────────────────────────

// retrievalFreshnessClasses 合法新鲜度类别集合（对齐 :712-718 集合字面量）。
var retrievalFreshnessClasses = map[string]struct{}{
	"current_event":      {},
	"recent_development": {},
	"evergreen_corpus":   {},
	NewsFreshnessClass:   {},
}

// ──────────────────────────── 常量 ────────────────────────────

// 组件与契约版本常量（对齐 :23-44 逐条）。
const (
	// PARetrieverComponentID PA 固定检索器组件 ID（:23）
	PARetrieverComponentID = "pa_bridge_fixed_retriever"
	// PAAPIFallbackProviderID API 排序降级厂商 ID（:24）
	PAAPIFallbackProviderID = "api-ranker-fallback"
	// PARetrievalContractVersion PA 检索契约版本（:25）
	PARetrievalContractVersion = "topic-starter-v6-pa-retrieval-v2"
	// PATrainingExampleSchemaVersion PA 训练样本契约版本（:26）
	PATrainingExampleSchemaVersion = "topic-starter-v6-pa-training-example-v1"
	// PassiveStarterExampleSchemaVersion 被动起始样本契约版本（:27-29）
	PassiveStarterExampleSchemaVersion = "topic-starter-v6-passive-starter-example-v1"
	// ActiveExpressionObservationSchemaVersion 主动观察契约版本（:30-32）
	ActiveExpressionObservationSchemaVersion = "topic-starter-v6-active-expression-observation-v1"
	// ActiveExpressionExampleSchemaVersion 主动样本契约版本（:33-35）
	ActiveExpressionExampleSchemaVersion = "topic-starter-v6-active-expression-example-v1"
	// EvidenceComponentID 证据组件 ID（:36）
	EvidenceComponentID = "verbatim_web_evidence"
	// EvidenceAPIProviderID 证据厂商 ID（:37）
	EvidenceAPIProviderID = "ark-zero-shot-adapted"
	// EvidenceContractVersionStr 证据契约版本（:38，避开与 EvidenceContractError 命名冲突）
	EvidenceContractVersionStr = "topic-starter-v6-verbatim-evidence-v2"
	// NewsSourceKind 新闻来源类别（:39）
	NewsSourceKind = "news_report"
	// NewsFreshnessClass 新闻新鲜度类别（:40）
	NewsFreshnessClass = "news_event"
	// NewsMaxWindowHours 新闻最大窗口小时数（:41）
	NewsMaxWindowHours = 72.0
	// EvidenceExtractorVersion 证据抽取器版本（:42）
	EvidenceExtractorVersion = "topic-starter-v6-verbatim-web-v5"
	// LegacyPrefixCharacterCount 遗留前缀字符数（:43）
	LegacyPrefixCharacterCount = 2000
	// OffsetCatalogMaxChars 偏移目录最大字符数（:44）
	OffsetCatalogMaxChars = 600
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalFixedCategories 全局固定类别集合（对齐 :45-47 frozenset）
	globalFixedCategories = map[string]struct{}{
		"adjacent_explanation":    {},
		"broad_timely_practical":  {},
	}
	// ignoredProviderTextFields 被忽略的厂商文本字段（对齐 :48）
	ignoredProviderTextFields = map[string]struct{}{
		"title":  {},
		"text":   {},
		"prompt": {},
	}
	// arxivVersionedRe arXiv 带版本路径（对齐 :496 fullmatch）
	arxivVersionedRe = regexp.MustCompile(`^(?:/abs|/html|/pdf)/(\d{4}\.\d{4,5})v(\d+)(?:\.pdf)?/?$`)
	// arxivUnversionedRe arXiv 无版本路径（对齐 :499）
	arxivUnversionedRe = regexp.MustCompile(`^/abs/(\d{4}\.\d{4,5})/?$`)
	// arxivCitedRe 正文中的 arXiv:XXXX.vN 引用（对齐 :501 search）
	arxivCitedRe = regexp.MustCompile(`(?i)arXiv:(\d{4}\.\d{4,5})v(\d+)\b`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTerminalRetrievalContractError 构造（对齐 :73-83 __init__）。
func NewTerminalRetrievalContractError(reason, detail string, receipt *LearnedComponentReceipt) *TerminalRetrievalContractError {
	return &TerminalRetrievalContractError{Reason: reason, Detail: detail, ComponentReceipt: receipt}
}

// NewEvidenceContractError 构造（对齐 :89-99 __init__）。
func NewEvidenceContractError(reason, detail string, receipt *LearnedComponentReceipt) *EvidenceContractError {
	return &EvidenceContractError{Reason: reason, Detail: detail, ComponentReceipt: receipt}
}

// NewPATrainingExample 构造并校验（对齐 :141-164 __post_init__）。
func NewPATrainingExample(userStateSnapshotID string, queryTextLocator, querySourceStarterID *string,
	domain string, positiveLabel bool, eventTime float64, contextScope, sourceArtifactID string) (*PATrainingExample, error) {
	e := &PATrainingExample{
		UserStateSnapshotID: userStateSnapshotID, QueryTextLocator: queryTextLocator,
		QuerySourceStarterID: querySourceStarterID, Domain: domain,
		PositiveLabel: positiveLabel, EventTime: eventTime,
		ContextScope: contextScope, SourceArtifactID: sourceArtifactID,
		SchemaVersion: PATrainingExampleSchemaVersion,
	}
	// 对齐 :142-154 _rejected3
	if !retrievalNonempty(e.UserStateSnapshotID) ||
		(e.Domain != "active" && e.Domain != "passive") ||
		!retrievalFiniteTimestamp(e.EventTime) ||
		!retrievalValidScope(e.ContextScope) ||
		!retrievalNonempty(e.SourceArtifactID) ||
		e.SchemaVersion != PATrainingExampleSchemaVersion {
		return nil, NewTerminalRetrievalContractError("invalid_pa_training_example", "", nil)
	}
	// 对齐 :155-164 active/passive 分支
	if e.Domain == "active" {
		if e.QueryTextLocator == nil || retrievalNonempty(*e.QueryTextLocator) == false || e.QuerySourceStarterID != nil {
			return nil, NewTerminalRetrievalContractError("invalid_pa_training_example", "", nil)
		}
	} else {
		if e.QuerySourceStarterID == nil || !retrievalNonempty(*e.QuerySourceStarterID) || e.QueryTextLocator != nil {
			return nil, NewTerminalRetrievalContractError("invalid_pa_training_example", "", nil)
		}
	}
	return e, nil
}

// ArtifactID 产物 ID（对齐 :180-181）。
func (e *PATrainingExample) ArtifactID() string {
	return fmt.Sprintf("pa-training:%s:%s", e.Domain, e.SourceArtifactID)
}

// AsPayload 序列化（对齐 :166-177）。
func (e *PATrainingExample) AsPayload() map[string]any {
	return map[string]any{
		"user_state_snapshot_id": e.UserStateSnapshotID,
		"query_text_locator":     optionalString(e.QueryTextLocator),
		"query_source_starter_id": optionalString(e.QuerySourceStarterID),
		"domain":                 e.Domain,
		"positive_label":         e.PositiveLabel,
		"event_time":             e.EventTime,
		"context_scope":          e.ContextScope,
		"source_artifact_id":     e.SourceArtifactID,
		"schema_version":         e.SchemaVersion,
	}
}

// PATrainingExampleFromPayload 反序列化（对齐 :184-197）。
func PATrainingExampleFromPayload(payload any) (*PATrainingExample, error) {
	m, keys, ok := payloadMap(payload)
	if !ok || !stringSetEqual(keys, []string{
		"user_state_snapshot_id", "query_text_locator", "query_source_starter_id",
		"domain", "positive_label", "event_time", "context_scope",
		"source_artifact_id", "schema_version",
	}) {
		return nil, NewTerminalRetrievalContractError("invalid_pa_training_example", "", nil)
	}
	// Python cls(**payload) 传参后走 __post_init__ 校验，Go 同构：取值后进构造器
	return NewPATrainingExample(
		str(m["user_state_snapshot_id"]),
		optionalStringFromAny(m["query_text_locator"]),
		optionalStringFromAny(m["query_source_starter_id"]),
		str(m["domain"]),
		m["positive_label"] == true,
		num(m["event_time"]),
		str(m["context_scope"]),
		str(m["source_artifact_id"]),
	)
}

// NewPassiveStarterExample 构造并校验（对齐 :215-244 __post_init__）。
func NewPassiveStarterExample(userStateSnapshotID, contextScope, corpusVersion, starterID, exposureID,
	manifestID, cardRevisionID string, exposedAt float64, clickedAt, notInterestedAt *float64,
	featureVersionRefs map[string]string) (*PassiveStarterExample, error) {
	e := &PassiveStarterExample{
		UserStateSnapshotID: userStateSnapshotID, ContextScope: contextScope,
		CorpusVersion: corpusVersion, StarterID: starterID, ExposureID: exposureID,
		ManifestID: manifestID, CardRevisionID: cardRevisionID,
		ExposedAt: exposedAt, ClickedAt: clickedAt, NotInterestedAt: notInterestedAt,
		FeatureVersionRefs: featureVersionRefs, SchemaVersion: PassiveStarterExampleSchemaVersion,
	}
	// 对齐 :216-230 六个非空串 + scope + schema
	for _, v := range []string{e.UserStateSnapshotID, e.CorpusVersion, e.StarterID,
		e.ExposureID, e.ManifestID, e.CardRevisionID} {
		if !retrievalNonempty(v) {
			return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
		}
	}
	if !retrievalValidScope(e.ContextScope) || e.SchemaVersion != PassiveStarterExampleSchemaVersion {
		return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
	}
	// 对齐 :231-235 时间戳
	if !retrievalFiniteTimestamp(e.ExposedAt) {
		return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
	}
	for _, v := range []*float64{e.ClickedAt, e.NotInterestedAt} {
		if v != nil && !retrievalFiniteTimestamp(*v) {
			return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
		}
	}
	// 对齐 :236-244 feature_version_refs
	if e.FeatureVersionRefs == nil || len(e.FeatureVersionRefs) == 0 {
		return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
	}
	for k, v := range e.FeatureVersionRefs {
		if !retrievalNonempty(k) || !retrievalNonempty(v) {
			return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
		}
	}
	return e, nil
}

// ToTrainingExample 投影为被动训练样本（对齐 :246-261）。
func (e *PassiveStarterExample) ToTrainingExample(positiveLabel bool, eventTime float64) (*PATrainingExample, error) {
	return NewPATrainingExample(
		e.UserStateSnapshotID,
		nil,
		stringPtr(e.StarterID),
		"passive",
		positiveLabel,
		eventTime,
		e.ContextScope,
		e.ArtifactID(),
	)
}

// ArtifactID 产物 ID（对齐 :264-265）。
func (e *PassiveStarterExample) ArtifactID() string {
	return fmt.Sprintf("passive-starter:%s:%s", e.ExposureID, e.CardRevisionID)
}

// AsPayload 序列化（对齐 :267-281）。
func (e *PassiveStarterExample) AsPayload() map[string]any {
	return map[string]any{
		"user_state_snapshot_id": e.UserStateSnapshotID,
		"context_scope":          e.ContextScope,
		"corpus_version":         e.CorpusVersion,
		"starter_id":             e.StarterID,
		"exposure_id":            e.ExposureID,
		"manifest_id":            e.ManifestID,
		"card_revision_id":       e.CardRevisionID,
		"exposed_at":             e.ExposedAt,
		"clicked_at":             optionalFloat(e.ClickedAt),
		"not_interested_at":      optionalFloat(e.NotInterestedAt),
		"feature_version_refs":   copyStringMap(e.FeatureVersionRefs),
		"schema_version":         e.SchemaVersion,
	}
}

// PassiveStarterExampleFromPayload 反序列化（对齐 :284-300）。
func PassiveStarterExampleFromPayload(payload any) (*PassiveStarterExample, error) {
	m, keys, ok := payloadMap(payload)
	if !ok || !stringSetEqual(keys, []string{
		"user_state_snapshot_id", "context_scope", "corpus_version", "starter_id",
		"exposure_id", "manifest_id", "card_revision_id", "exposed_at",
		"clicked_at", "not_interested_at", "feature_version_refs", "schema_version",
	}) {
		return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
	}
	// 对齐 :298 feature_version_refs 必须是 dict（兼容 map[string]any 与 map[string]string 双形态）
	refs := map[string]string{}
	switch refsValue := m["feature_version_refs"].(type) {
	case map[string]any:
		for k, v := range refsValue {
			refs[k] = str(v)
		}
	case map[string]string:
		for k, v := range refsValue {
			refs[k] = v
		}
	default:
		if m["feature_version_refs"] != nil {
			return nil, NewTerminalRetrievalContractError("invalid_passive_starter_example", "", nil)
		}
	}
	return NewPassiveStarterExample(
		str(m["user_state_snapshot_id"]), str(m["context_scope"]), str(m["corpus_version"]),
		str(m["starter_id"]), str(m["exposure_id"]), str(m["manifest_id"]),
		str(m["card_revision_id"]), num(m["exposed_at"]),
		optionalFloatFromAny(m["clicked_at"]), optionalFloatFromAny(m["not_interested_at"]),
		refs,
	)
}

// NewActiveExpressionObservation 构造并校验（对齐 :315-330 __post_init__）。
func NewActiveExpressionObservation(userStateSnapshotID, sourceLocator string, observedAt float64,
	inputOrigin, assistanceOrigin, contextScope string) (*ActiveExpressionObservation, error) {
	o := &ActiveExpressionObservation{
		UserStateSnapshotID: userStateSnapshotID, SourceLocator: sourceLocator,
		ObservedAt: observedAt, InputOrigin: inputOrigin, AssistanceOrigin: assistanceOrigin,
		ContextScope: contextScope, SchemaVersion: ActiveExpressionObservationSchemaVersion,
	}
	for _, v := range []string{o.UserStateSnapshotID, o.SourceLocator, o.InputOrigin, o.AssistanceOrigin} {
		if !retrievalNonempty(v) {
			return nil, NewTerminalRetrievalContractError("invalid_active_expression_observation", "", nil)
		}
	}
	if !retrievalValidScope(o.ContextScope) || o.SchemaVersion != ActiveExpressionObservationSchemaVersion {
		return nil, NewTerminalRetrievalContractError("invalid_active_expression_observation", "", nil)
	}
	if !retrievalFiniteTimestamp(o.ObservedAt) {
		return nil, NewTerminalRetrievalContractError("invalid_active_expression_observation", "", nil)
	}
	return o, nil
}

// ArtifactID 产物 ID（对齐 :333-337）。
func (o *ActiveExpressionObservation) ArtifactID() string {
	return fmt.Sprintf("active-observation:%s:%s", o.UserStateSnapshotID, o.SourceLocator)
}

// AsPayload 序列化（对齐 :339-348）。
func (o *ActiveExpressionObservation) AsPayload() map[string]any {
	return map[string]any{
		"user_state_snapshot_id": o.UserStateSnapshotID,
		"source_locator":         o.SourceLocator,
		"observed_at":            o.ObservedAt,
		"input_origin":           o.InputOrigin,
		"assistance_origin":      o.AssistanceOrigin,
		"context_scope":          o.ContextScope,
		"schema_version":         o.SchemaVersion,
	}
}

// ActiveExpressionObservationFromPayload 反序列化（对齐 :351-364）。
func ActiveExpressionObservationFromPayload(payload any) (*ActiveExpressionObservation, error) {
	m, keys, ok := payloadMap(payload)
	if !ok || !stringSetEqual(keys, []string{
		"user_state_snapshot_id", "source_locator", "observed_at",
		"input_origin", "assistance_origin", "context_scope", "schema_version",
	}) {
		return nil, NewTerminalRetrievalContractError("invalid_active_expression_observation", "", nil)
	}
	return NewActiveExpressionObservation(
		str(m["user_state_snapshot_id"]), str(m["source_locator"]), num(m["observed_at"]),
		str(m["input_origin"]), str(m["assistance_origin"]), str(m["context_scope"]),
	)
}

// NewActiveExpressionExample 构造并校验（对齐 :377-390 __post_init__）。
func NewActiveExpressionExample(userStateSnapshotID, sourceLocator string, observedAt float64,
	inputOrigin, assistanceOrigin, contextScope string) (*ActiveExpressionExample, error) {
	e := &ActiveExpressionExample{
		UserStateSnapshotID: userStateSnapshotID, SourceLocator: sourceLocator,
		ObservedAt: observedAt, InputOrigin: inputOrigin, AssistanceOrigin: assistanceOrigin,
		ContextScope: contextScope, SchemaVersion: ActiveExpressionExampleSchemaVersion,
	}
	// 对齐 :378-379 来源组合闸
	if e.InputOrigin != "manual_free_text" || e.AssistanceOrigin != "none" {
		return nil, NewTerminalRetrievalContractError("invalid_active_expression_example", "", nil)
	}
	if !retrievalNonempty(e.UserStateSnapshotID) || !retrievalNonempty(e.SourceLocator) ||
		!retrievalFiniteTimestamp(e.ObservedAt) ||
		!retrievalValidScope(e.ContextScope) ||
		e.SchemaVersion != ActiveExpressionExampleSchemaVersion {
		return nil, NewTerminalRetrievalContractError("invalid_active_expression_example", "", nil)
	}
	return e, nil
}

// ToTrainingExample 投影为主动训练样本（对齐 :392-407）。
func (e *ActiveExpressionExample) ToTrainingExample(positiveLabel bool, eventTime float64) (*PATrainingExample, error) {
	return NewPATrainingExample(
		e.UserStateSnapshotID,
		stringPtr(e.SourceLocator),
		nil,
		"active",
		positiveLabel,
		eventTime,
		e.ContextScope,
		e.ArtifactID(),
	)
}

// ArtifactID 产物 ID（对齐 :410-411）。
func (e *ActiveExpressionExample) ArtifactID() string {
	return fmt.Sprintf("active-example:%s:%s", e.UserStateSnapshotID, e.SourceLocator)
}

// AsPayload 序列化（对齐 :413-422）。
func (e *ActiveExpressionExample) AsPayload() map[string]any {
	return map[string]any{
		"user_state_snapshot_id": e.UserStateSnapshotID,
		"source_locator":         e.SourceLocator,
		"observed_at":            e.ObservedAt,
		"input_origin":           e.InputOrigin,
		"assistance_origin":      e.AssistanceOrigin,
		"context_scope":          e.ContextScope,
		"schema_version":         e.SchemaVersion,
	}
}

// ActiveExpressionExampleFromPayload 反序列化（对齐 :425-436）。
func ActiveExpressionExampleFromPayload(payload any) (*ActiveExpressionExample, error) {
	m, keys, ok := payloadMap(payload)
	if !ok || !stringSetEqual(keys, []string{
		"user_state_snapshot_id", "source_locator", "observed_at",
		"input_origin", "assistance_origin", "context_scope", "schema_version",
	}) {
		return nil, NewTerminalRetrievalContractError("invalid_active_expression_example", "", nil)
	}
	return NewActiveExpressionExample(
		str(m["user_state_snapshot_id"]), str(m["source_locator"]), num(m["observed_at"]),
		str(m["input_origin"]), str(m["assistance_origin"]), str(m["context_scope"]),
	)
}

// AdmitActiveExpression 保留结果但只准入自然无协助的手输文本（对齐 :446-474）。
func AdmitActiveExpression(observation *ActiveExpressionObservation) (*ActiveExpressionAdmission, error) {
	// 对齐 :451-456
	if observation.InputOrigin != "manual_free_text" {
		return &ActiveExpressionAdmission{
			Observation:     observation,
			Example:         nil,
			ExclusionReason: "ineligible_input_origin",
		}, nil
	}
	// 对齐 :457-462
	if observation.AssistanceOrigin != "none" {
		return &ActiveExpressionAdmission{
			Observation:     observation,
			Example:         nil,
			ExclusionReason: "assisted_input_excluded",
		}, nil
	}
	// 对齐 :463-474
	example, err := NewActiveExpressionExample(
		observation.UserStateSnapshotID, observation.SourceLocator, observation.ObservedAt,
		observation.InputOrigin, observation.AssistanceOrigin, observation.ContextScope,
	)
	if err != nil {
		return nil, err
	}
	return &ActiveExpressionAdmission{Observation: observation, Example: example, ExclusionReason: ""}, nil
}

// VersionedDocumentIdentity 识别显式 arXiv 版本；无版本 URL 不能绑定日期（对齐 :491-504）。
// 返回 (documentID, version, ok)。
func VersionedDocumentIdentity(rawURL, content string) (string, string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() != "arxiv.org" && parsed.Hostname() != "www.arxiv.org" {
		return "", "", false
	}
	if m := arxivVersionedRe.FindStringSubmatch(parsed.Path); m != nil {
		return "arxiv:" + m[1], "v" + m[2], true
	}
	if m := arxivUnversionedRe.FindStringSubmatch(parsed.Path); m != nil {
		// 对齐 :501 arXiv:<id>v<n>（id 已由正则锚定，逐字拼接等价 re.escape）
		cited := arxivCitedRe.FindStringSubmatch(content)
		if cited != nil && cited[1] == m[1] {
			return "arxiv:" + m[1], "v" + cited[2], true
		}
	}
	return "", "", false
}

// ValidLinkedDatePage 校验链接日期页结构（对齐 :507-521）。
func ValidLinkedDatePage(value any, bodyURL string) bool {
	m, keys, ok := payloadMap(value)
	if !ok || !stringSetEqual(keys, []string{
		"date_page_url", "body_url", "document_id", "version", "date_page_excerpt",
		"operation_id", "request_id", "http_status_code",
	}) {
		return false
	}
	bodyDoc, bodyVersion, identity := VersionedDocumentIdentity(bodyURL, "")
	linkedDoc, linkedVersion, linkedOK := VersionedDocumentIdentity(str(m["date_page_url"]), str(m["date_page_excerpt"]))
	if !identity ||
		str(m["body_url"]) != bodyURL ||
		!linkedOK || (identityTuple{linkedDoc, linkedVersion} != (identityTuple{bodyDoc, bodyVersion})) ||
		str(m["date_page_url"]) == bodyURL ||
		str(m["document_id"]) != bodyDoc || str(m["version"]) != bodyVersion {
		return false
	}
	for _, k := range []string{"date_page_excerpt", "operation_id", "request_id"} {
		if !retrievalNonempty(str(m[k])) {
			return false
		}
	}
	if !plainInt(m["http_status_code"]) {
		return false
	}
	code := int(num(m["http_status_code"]))
	return code >= 200 && code < 300
}

// NewPublicationDateProvenance 构造并校验（对齐 :545-578 __post_init__）。
func NewPublicationDateProvenance(sourceType, sourceField, rawValue string, searchProviderID,
	searchReceiptID, searchResultURL string, linkedPage map[string]any,
	listedAt, pagePublishedAt, apiURL string) (*PublicationDateProvenance, error) {
	p := &PublicationDateProvenance{
		SourceType: sourceType, SourceField: sourceField, RawValue: rawValue,
		SearchProviderID: searchProviderID, SearchReceiptID: searchReceiptID,
		SearchResultURL: searchResultURL, LinkedPage: linkedPage,
		ListedAt: listedAt, PagePublishedAt: pagePublishedAt, APIURL: apiURL,
	}
	// 对齐 :546-559 _rejected5
	if !retrievalNonempty(p.SourceType) || !retrievalNonempty(p.SourceField) || !retrievalNonempty(p.RawValue) {
		return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
	}
	if p.ListedAt != "" && (p.SourceType != "page_explicit_metadata" || !retrievalNonempty(p.ListedAt)) {
		return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
	}
	if p.PagePublishedAt != "" && (p.SourceType != "structured_search_result" || !retrievalNonempty(p.PagePublishedAt)) {
		return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
	}
	if p.APIURL != "" && (p.SourceType != "page_explicit_metadata" || !retrievalNonempty(p.APIURL)) {
		return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
	}
	// 对齐 :560-578 按 source_type 分支
	searchValues := []string{p.SearchProviderID, p.SearchReceiptID, p.SearchResultURL}
	anySearch := func() bool {
		for _, v := range searchValues {
			if v != "" {
				return true
			}
		}
		return false
	}
	switch p.SourceType {
	case "structured_search_result":
		for _, v := range searchValues {
			if !retrievalNonempty(v) {
				return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
			}
		}
	case "linked_document_metadata":
		if anySearch() || !ValidLinkedDatePage(p.LinkedPage, str(mapValueOrEmpty(p.LinkedPage, "body_url"))) {
			return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
		}
	case "page_explicit_metadata":
		if anySearch() {
			return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
		}
	default:
		return nil, NewEvidenceContractError("invalid_publication_date_provenance", "", nil)
	}
	return p, nil
}

// AsPayload 序列化（对齐 :580-592，None 字段不落键）。
func (p *PublicationDateProvenance) AsPayload() map[string]any {
	payload := map[string]any{
		"source_type":   p.SourceType,
		"source_field":  p.SourceField,
		"raw_value":     p.RawValue,
		"search_provider_id": optionalStringFromEmpty(p.SearchProviderID),
		"search_receipt_id":  optionalStringFromEmpty(p.SearchReceiptID),
		"search_result_url":  optionalStringFromEmpty(p.SearchResultURL),
	}
	if p.LinkedPage != nil {
		payload["linked_page"] = copyAnyMap(p.LinkedPage)
	}
	if p.ListedAt != "" {
		payload["listed_at"] = p.ListedAt
	}
	if p.PagePublishedAt != "" {
		payload["page_published_at"] = p.PagePublishedAt
	}
	if p.APIURL != "" {
		payload["api_url"] = p.APIURL
	}
	return payload
}

// NewFrozenDocumentRevision 构造并校验（对齐 :610-639 __post_init__）。
func NewFrozenDocumentRevision(documentRevisionID, sourceURL, publisher, sourceKind, publishedAt string,
	fetchedAt float64, immutableContent, snapshotDate, snapshotVersion string,
	provenance *PublicationDateProvenance) (*FrozenDocumentRevision, error) {
	d := &FrozenDocumentRevision{
		DocumentRevisionID: documentRevisionID, SourceURL: sourceURL, Publisher: publisher,
		SourceKind: sourceKind, PublishedAt: publishedAt, FetchedAt: fetchedAt,
		ImmutableContent: immutableContent, SnapshotDate: snapshotDate,
		SnapshotVersion: snapshotVersion, PublicationDateProvenance: provenance,
	}
	for _, v := range []string{d.DocumentRevisionID, d.SourceURL, d.Publisher, d.SourceKind,
		d.ImmutableContent, d.SnapshotDate, d.SnapshotVersion} {
		if !retrievalNonempty(v) {
			return nil, NewEvidenceContractError("invalid_frozen_document", "", nil)
		}
	}
	if !retrievalFiniteTimestamp(d.FetchedAt) {
		return nil, NewEvidenceContractError("invalid_frozen_document", "", nil)
	}
	// 对齐 :625-628 published_at 可解析
	if d.PublishedAt != "" {
		if _, ok := ParseISOTimestamp(d.PublishedAt); !ok {
			return nil, NewEvidenceContractError("invalid_frozen_document", "", nil)
		}
	}
	// 对齐 :629-633 无 published_at 不得带溯源
	if d.PublishedAt == "" && d.PublicationDateProvenance != nil {
		return nil, NewEvidenceContractError("invalid_frozen_document", "", nil)
	}
	// 对齐 :634-639 溯源 URL 与文档 URL 一致
	if d.PublicationDateProvenance != nil &&
		d.PublicationDateProvenance.SearchResultURL != "" &&
		d.PublicationDateProvenance.SearchResultURL != d.SourceURL {
		return nil, NewEvidenceContractError("invalid_frozen_document", "", nil)
	}
	return d, nil
}

// AsPayload 序列化（对齐 :641-659）。
func (d *FrozenDocumentRevision) AsPayload(includeContent bool) map[string]any {
	payload := map[string]any{
		"document_revision_id": d.DocumentRevisionID,
		"source_url":           d.SourceURL,
		"publisher":            d.Publisher,
		"source_kind":          d.SourceKind,
		"published_at":         optionalStringFromEmpty(d.PublishedAt),
		"publication_date_provenance": func() any {
			if d.PublicationDateProvenance == nil {
				return nil
			}
			return d.PublicationDateProvenance.AsPayload()
		}(),
		"fetched_at":       d.FetchedAt,
		"snapshot_date":    d.SnapshotDate,
		"snapshot_version": d.SnapshotVersion,
	}
	if includeContent {
		payload["immutable_content"] = d.ImmutableContent
	}
	return payload
}

// NewFrozenDocumentSnapshot 构造并校验（对齐 :668-683 __post_init__）。
func NewFrozenDocumentSnapshot(snapshotDate, snapshotVersion string, documents []FrozenDocumentRevision) (*FrozenDocumentSnapshot, error) {
	s := &FrozenDocumentSnapshot{SnapshotDate: snapshotDate, SnapshotVersion: snapshotVersion, Documents: documents}
	if !retrievalNonempty(s.SnapshotDate) || !retrievalNonempty(s.SnapshotVersion) || len(s.Documents) == 0 {
		return nil, NewEvidenceContractError("invalid_document_snapshot", "", nil)
	}
	ids := make([]string, 0, len(s.Documents))
	for _, doc := range s.Documents {
		ids = append(ids, doc.DocumentRevisionID)
	}
	if hasStringDuplicates(ids) {
		return nil, NewEvidenceContractError("duplicate_document_revision", "", nil)
	}
	for _, doc := range s.Documents {
		if doc.SnapshotDate != s.SnapshotDate || doc.SnapshotVersion != s.SnapshotVersion {
			return nil, NewEvidenceContractError("mixed_document_snapshot", "", nil)
		}
	}
	return s, nil
}

// ByID 按修订 ID 索引（对齐 :686-687 property by_id）。
func (s *FrozenDocumentSnapshot) ByID() map[string]*FrozenDocumentRevision {
	byID := make(map[string]*FrozenDocumentRevision, len(s.Documents))
	for i := range s.Documents {
		byID[s.Documents[i].DocumentRevisionID] = &s.Documents[i]
	}
	return byID
}

// NewEvidenceCandidate 构造并校验（对齐 :699-718 __post_init__）。
func NewEvidenceCandidate(candidateID, title, primaryQuery, freshnessClass, contextScope, category string) (*EvidenceCandidate, error) {
	c := &EvidenceCandidate{
		CandidateID: candidateID, Title: title, PrimaryQuery: primaryQuery,
		FreshnessClass: freshnessClass, ContextScope: contextScope, Category: category,
	}
	for _, v := range []string{c.CandidateID, c.Title, c.PrimaryQuery, c.FreshnessClass, c.Category} {
		if !retrievalNonempty(v) {
			return nil, NewEvidenceContractError("invalid_evidence_candidate", "", nil)
		}
	}
	if !retrievalValidScope(c.ContextScope) {
		return nil, NewEvidenceContractError("invalid_evidence_candidate", "", nil)
	}
	if _, ok := retrievalFreshnessClasses[c.FreshnessClass]; !ok {
		return nil, NewEvidenceContractError("invalid_freshness_class", "", nil)
	}
	return c, nil
}

// AsPayload 序列化（对齐 :720-728）。
func (c *EvidenceCandidate) AsPayload() map[string]any {
	return map[string]any{
		"candidate_id":    c.CandidateID,
		"title":           c.Title,
		"primary_query":   c.PrimaryQuery,
		"freshness_class": c.FreshnessClass,
		"context_scope":   c.ContextScope,
		"category":        c.Category,
	}
}

// AsPayload 片段序列化（对齐 :748-768）。
func (s *ExactEvidenceSpan) AsPayload() map[string]any {
	return map[string]any{
		"candidate_id":         s.CandidateID,
		"document_revision_id": s.DocumentRevisionID,
		"source_url":           s.SourceURL,
		"publisher":            s.Publisher,
		"source_kind":          s.SourceKind,
		"published_at":         optionalStringFromEmpty(s.PublishedAt),
		"publication_date_provenance": func() any {
			if s.PublicationDateProvenance == nil {
				return nil
			}
			return s.PublicationDateProvenance.AsPayload()
		}(),
		"fetched_at":       s.FetchedAt,
		"snapshot_date":    s.SnapshotDate,
		"snapshot_version": s.SnapshotVersion,
		"start":            s.Start,
		"end":              s.End,
		"exact_text":       s.ExactText,
		"retrieved_at":     s.RetrievedAt,
	}
}

// AsPayload 证据束序列化（对齐 :798-810）。
func (b *EvidenceBundle) AsPayload() map[string]any {
	spans := make([]map[string]any, 0, len(b.Spans))
	for i := range b.Spans {
		spans = append(spans, b.Spans[i].AsPayload())
	}
	return map[string]any{
		"candidate_id":     b.CandidateID,
		"context_scope":    b.ContextScope,
		"category":         b.Category,
		"freshness_class":  b.FreshnessClass,
		"snapshot_date":    b.SnapshotDate,
		"snapshot_version": b.SnapshotVersion,
		"spans":            spans,
		"source_gate_mode": b.SourceGateMode,
		"component_receipt": func() any {
			if b.ComponentReceipt == nil {
				return nil
			}
			return b.ComponentReceipt.AsPayload()
		}(),
		"contract_version": b.ContractVersion,
	}
}

// NewEvidenceOffset 构造并校验（对齐 :819-830 __post_init__）。
func NewEvidenceOffset(documentRevisionID string, start, end int) (*EvidenceOffset, error) {
	o := &EvidenceOffset{DocumentRevisionID: documentRevisionID, Start: start, End: end}
	if !retrievalNonempty(o.DocumentRevisionID) || start < 0 || end <= start {
		return nil, NewEvidenceContractError("invalid_heldout_offset", "", nil)
	}
	return o, nil
}

// NewEvidenceHeldOutCase 构造并校验（对齐 :845-866 __post_init__）。
func NewEvidenceHeldOutCase(caseID, fixtureVersion string, candidate *EvidenceCandidate,
	snapshot *FrozenDocumentSnapshot, goldOffsets []EvidenceOffset, goldShouldAdmit bool) (*EvidenceHeldOutCase, error) {
	c := &EvidenceHeldOutCase{
		CaseID: caseID, FixtureVersion: fixtureVersion, Candidate: candidate,
		Snapshot: snapshot, GoldOffsets: goldOffsets, GoldShouldAdmit: goldShouldAdmit,
		GoldReviewStatus: "human_exact_verified",
	}
	if !retrievalNonempty(c.CaseID) || !retrievalNonempty(c.FixtureVersion) {
		return nil, NewEvidenceContractError("invalid_heldout_case", "", nil)
	}
	// 对齐 :848-849 金标必须人工精确核对
	if c.GoldReviewStatus != "human_exact_verified" {
		return nil, NewEvidenceContractError("heldout_gold_not_human_verified", "", nil)
	}
	// 对齐 :852-857 偏移三元组去重
	type offsetKey struct {
		doc   string
		start int
		end   int
	}
	seen := map[offsetKey]struct{}{}
	for _, o := range c.GoldOffsets {
		k := offsetKey{o.DocumentRevisionID, o.Start, o.End}
		if _, dup := seen[k]; dup {
			return nil, NewEvidenceContractError("invalid_heldout_case", "", nil)
		}
		seen[k] = struct{}{}
	}
	// 对齐 :858-866 偏移必须在文档内且非空（字符语义 → rune）
	documents := c.Snapshot.ByID()
	for _, o := range c.GoldOffsets {
		document := documents[o.DocumentRevisionID]
		if document == nil {
			return nil, NewEvidenceContractError("invalid_heldout_gold_offset", "", nil)
		}
		runes := []rune(document.ImmutableContent)
		if o.End > len(runes) || len(runes[o.Start:o.End]) == 0 {
			return nil, NewEvidenceContractError("invalid_heldout_gold_offset", "", nil)
		}
	}
	return c, nil
}

// NewEvidenceHeldOutPrediction 构造并校验（对齐 :877-897 __post_init__）。
func NewEvidenceHeldOutPrediction(caseID string, predictedOffsets []EvidenceOffset,
	admitted bool, latencyMS, providerCost float64) (*EvidenceHeldOutPrediction, error) {
	p := &EvidenceHeldOutPrediction{
		CaseID: caseID, PredictedOffsets: predictedOffsets, Admitted: admitted,
		LatencyMS: latencyMS, ProviderCost: providerCost,
	}
	if !retrievalNonempty(p.CaseID) ||
		math.IsNaN(p.LatencyMS) || math.IsInf(p.LatencyMS, 0) || p.LatencyMS < 0 ||
		math.IsNaN(p.ProviderCost) || math.IsInf(p.ProviderCost, 0) || p.ProviderCost < 0 {
		return nil, NewEvidenceContractError("invalid_heldout_prediction", "", nil)
	}
	type offsetKey struct {
		doc   string
		start int
		end   int
	}
	seen := map[offsetKey]struct{}{}
	for _, o := range p.PredictedOffsets {
		k := offsetKey{o.DocumentRevisionID, o.Start, o.End}
		if _, dup := seen[k]; dup {
			return nil, NewEvidenceContractError("invalid_heldout_prediction", "", nil)
		}
		seen[k] = struct{}{}
	}
	return p, nil
}

// Ratio 安全除法（对齐 :915-916）。
func Ratio(numerator, denominator int, empty float64) float64 {
	if denominator == 0 {
		return empty
	}
	return float64(numerator) / float64(denominator)
}

// EligibleDocumentRevisionIds 在花抽取器调用之前施加来源/新鲜度闸（对齐 :974-1026）。
func EligibleDocumentRevisionIds(candidate *EvidenceCandidate, documents []FrozenDocumentRevision, now float64) ([]string, error) {
	if !retrievalFiniteTimestamp(now) {
		return nil, NewEvidenceContractError("evidence_admission_input_failed", "", nil)
	}
	// 对齐 :985-990 evergreen：来源三类别直通
	if candidate.FreshnessClass == "evergreen_corpus" {
		ids := []string{}
		for i := range documents {
			kind := documents[i].SourceKind
			if kind == "authoritative" || kind == "original" || kind == "stable" {
				ids = append(ids, documents[i].DocumentRevisionID)
			}
		}
		return ids, nil
	}
	// 对齐 :991-1000 新鲜过滤
	fresh := []FrozenDocumentRevision{}
	for i := range documents {
		if PublishedAtIsFresh(documents[i].PublishedAt, candidate.FreshnessClass, now) {
			fresh = append(fresh, documents[i])
		}
	}
	// 对齐 :1001-1006 news：单一新闻来源
	if candidate.FreshnessClass == NewsFreshnessClass {
		ids := []string{}
		for i := range fresh {
			if fresh[i].SourceKind == NewsSourceKind {
				ids = append(ids, fresh[i].DocumentRevisionID)
			}
		}
		return ids, nil
	}
	// 对齐 :1007-1013 主来源优先
	primary := []FrozenDocumentRevision{}
	for i := range fresh {
		if fresh[i].SourceKind == "authoritative" || fresh[i].SourceKind == "original" {
			primary = append(primary, fresh[i])
		}
	}
	if len(primary) > 0 {
		ids := make([]string, 0, len(primary))
		for i := range primary {
			ids = append(ids, primary[i].DocumentRevisionID)
		}
		return ids, nil
	}
	// 对齐 :1014-1026 双独立报道：最多 2 家不同发布方
	reports := []string{}
	publishers := map[string]struct{}{}
	for i := range fresh {
		if fresh[i].SourceKind != "reliable_report" {
			continue
		}
		if _, dup := publishers[fresh[i].Publisher]; dup {
			continue
		}
		reports = append(reports, fresh[i].DocumentRevisionID)
		publishers[fresh[i].Publisher] = struct{}{}
		if len(reports) == 2 {
			return reports, nil
		}
	}
	return []string{}, nil
}

// ExtractCandidateEvidence 在一份冻结快照内为一个候选选定精确偏移（对齐 :1045-1237）。
// 所有厂商失败 fail-closed 并附 failed 小票。
func ExtractCandidateEvidence(
	frozenCandidate *EvidenceCandidate,
	frozenDocuments *FrozenDocumentSnapshot,
	allowedDocumentRevisionIDs []string,
	inputSnapshotID, requestID string,
	providerBinding *LearnedComponentBinding,
	invokeExtractor EvidenceExtractor,
	retrievedAt float64,
) (*ExtractedCandidateEvidence, error) {
	// 对齐 :1058-1067 _rejected 输入契约
	if !retrievalNonempty(inputSnapshotID) || !retrievalNonempty(requestID) ||
		!retrievalFiniteTimestamp(retrievedAt) ||
		providerBinding.ComponentID != EvidenceComponentID {
		return nil, NewEvidenceContractError("evidence_input_contract_failed", "", nil)
	}
	// 对齐 :1068-1069 厂商契约
	if providerBinding.ProviderID != EvidenceAPIProviderID {
		return nil, NewEvidenceContractError("evidence_provider_contract_failed", "", nil)
	}
	snapshotDocumentsByID := frozenDocuments.ByID()
	ids := allowedDocumentRevisionIDs
	if ids == nil {
		for id := range snapshotDocumentsByID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	if len(ids) == 0 || hasStringDuplicates(ids) {
		return nil, NewEvidenceContractError("evidence_input_contract_failed", "", nil)
	}
	for _, id := range ids {
		if _, known := snapshotDocumentsByID[id]; !known {
			return nil, NewEvidenceContractError("evidence_input_contract_failed", "", nil)
		}
	}
	allowedDocuments := make([]*FrozenDocumentRevision, 0, len(ids))
	for _, id := range ids {
		allowedDocuments = append(allowedDocuments, snapshotDocumentsByID[id])
	}

	invokedFailure := func(reason string) *EvidenceContractError {
		return NewEvidenceContractError(reason, "", failedComponentReceipt(
			providerBinding, inputSnapshotID, requestID, reason,
		))
	}

	// 对齐 :1098-1103 偏移目录
	offsetCatalogByID := map[string][]map[string]any{}
	for _, document := range allowedDocuments {
		offsetCatalogByID[document.DocumentRevisionID] = hostExactOffsetCatalog(document.ImmutableContent)
	}
	// 对齐 :1104-1130 请求 payload
	documents := make([]map[string]any, 0, len(allowedDocuments))
	for _, document := range allowedDocuments {
		entry := document.AsPayload(true)
		entry["offset_catalog"] = offsetCatalogByID[document.DocumentRevisionID]
		documents = append(documents, entry)
	}
	snapshotRevisionIDs := make([]string, 0, len(frozenDocuments.Documents))
	for i := range frozenDocuments.Documents {
		snapshotRevisionIDs = append(snapshotRevisionIDs, frozenDocuments.Documents[i].DocumentRevisionID)
	}
	requestPayload := map[string]any{
		"contract_version":     EvidenceContractVersionStr,
		"request_id":           requestID,
		"input_snapshot_id":    inputSnapshotID,
		"candidate_id":         frozenCandidate.CandidateID,
		"candidate":            frozenCandidate.AsPayload(),
		"snapshot_date":        frozenDocuments.SnapshotDate,
		"snapshot_version":     frozenDocuments.SnapshotVersion,
		"snapshot_document_revision_ids": snapshotRevisionIDs,
		"document_revision_ids":          ids,
		"required_source_gate_mode": RequiredSourceGateMode(frozenCandidate, allowedDocuments),
		"documents":                 documents,
		"provider_binding":          providerBinding.AsProvenance(),
	}
	// 对齐 :1131-1141 调用与失败包装
	response, err := invokeExtractor(context.Background(), requestPayload)
	if err != nil {
		reason := "evidence_provider_invocation_failed"
		if ecErr, ok := err.(*EvidenceContractError); ok && ecErr.Reason == "evidence_output_contract_failed" {
			reason = "evidence_output_contract_failed"
		}
		return nil, invokedFailure(reason)
	}
	// 对齐 :1142-1154 顶层结构契约
	responseMap, responseKeys, ok := payloadMap(response)
	if !ok {
		return nil, invokedFailure("evidence_output_contract_failed")
	}
	topLevelExtras := stringSetDifference(responseKeys, []string{"candidate_id", "spans"})
	for _, extra := range topLevelExtras {
		if extra == "snippet" || extra == "title" || extra == "text" || extra == "span_text" {
			return nil, invokedFailure("snippet_or_title_not_evidence")
		}
	}
	spansRaw, isList := responseMap["spans"].([]any)
	if !stringSetEqual(responseKeys, []string{"candidate_id", "spans"}) || !isList {
		return nil, invokedFailure("evidence_output_contract_failed")
	}
	if str(responseMap["candidate_id"]) != frozenCandidate.CandidateID {
		return nil, invokedFailure("candidate_mismatch")
	}
	if len(spansRaw) == 0 {
		return nil, invokedFailure("no_candidate_support")
	}

	// 对齐 :1156-1221 逐片段校验
	spans := []ExactEvidenceSpan{}
	type seenKey struct {
		doc   string
		start int
		end   int
	}
	seenOffsets := map[seenKey]struct{}{}
	for _, raw := range spansRaw {
		rawMap, rawKeys, rawOK := payloadMap(raw)
		if !rawOK {
			return nil, invokedFailure("evidence_output_contract_failed")
		}
		extras := stringSetDifference(rawKeys, []string{"document_revision_id", "start", "end"})
		for _, extra := range extras {
			if extra == "snippet" || extra == "title" || extra == "text" ||
				extra == "span_text" || extra == "source" {
				return nil, invokedFailure("snippet_or_title_not_evidence")
			}
		}
		if !stringSetEqual(rawKeys, []string{"document_revision_id", "start", "end"}) {
			return nil, invokedFailure("evidence_output_contract_failed")
		}
		documentID := str(rawMap["document_revision_id"])
		if !retrievalNonempty(documentID) {
			return nil, invokedFailure("unknown_document_revision")
		}
		document, known := snapshotDocumentsByID[documentID]
		if !known {
			return nil, invokedFailure("unknown_document_revision")
		}
		if !plainInt(rawMap["start"]) || !plainInt(rawMap["end"]) {
			return nil, invokedFailure("invalid_evidence_offsets")
		}
		start, end := int(num(rawMap["start"])), int(num(rawMap["end"]))
		if start == end {
			return nil, invokedFailure("empty_evidence_span")
		}
		contentRunes := []rune(document.ImmutableContent)
		if start < 0 || end < 0 || start > end || end > len(contentRunes) {
			return nil, invokedFailure("invalid_evidence_offsets")
		}
		// 对齐 :1188-1189 遗留前缀整段拒绝
		if start == 0 && end == LegacyPrefixCharacterCount {
			return nil, invokedFailure("legacy_prefix_not_evidence")
		}
		// 对齐 :1190-1194 必须命中宿主偏移目录
		inCatalog := false
		for _, entry := range offsetCatalogByID[documentID] {
			if int(num(entry["start"])) == start && int(num(entry["end"])) == end {
				inCatalog = true
				break
			}
		}
		if !inCatalog {
			return nil, invokedFailure("invalid_evidence_offsets")
		}
		if _, dup := seenOffsets[seenKey{documentID, start, end}]; dup {
			return nil, invokedFailure("duplicate_evidence_span")
		}
		seenOffsets[seenKey{documentID, start, end}] = struct{}{}
		exactText := document.ImmutableContent
		if start > 0 || end < len(contentRunes) {
			exactText = string(contentRunes[start:end])
		}
		if exactText == "" {
			return nil, invokedFailure("empty_evidence_span")
		}
		spans = append(spans, ExactEvidenceSpan{
			CandidateID:         frozenCandidate.CandidateID,
			DocumentRevisionID:  document.DocumentRevisionID,
			SourceURL:           document.SourceURL,
			Publisher:           document.Publisher,
			SourceKind:          document.SourceKind,
			PublishedAt:         document.PublishedAt,
			FetchedAt:           document.FetchedAt,
			SnapshotDate:        document.SnapshotDate,
			SnapshotVersion:     document.SnapshotVersion,
			Start:               start,
			End:                 end,
			ExactText:           exactText,
			RetrievedAt:         retrievedAt,
			PublicationDateProvenance: document.PublicationDateProvenance,
		})
	}

	// 对齐 :1223-1230 成功小票
	receipt, err := NewLearnedComponentReceipt(
		providerBinding, inputSnapshotID, requestID,
		"uncalibrated_api_proxy", "success", "",
	)
	if err != nil {
		return nil, err
	}
	return &ExtractedCandidateEvidence{
		Candidate:        frozenCandidate,
		SnapshotDate:     frozenDocuments.SnapshotDate,
		SnapshotVersion:  frozenDocuments.SnapshotVersion,
		Spans:            spans,
		ComponentReceipt: receipt,
		ContractVersion:  EvidenceContractVersionStr,
	}, nil
}

// AdmitCandidateEvidence 在精确片段校验之后独立施加日期与来源闸（对齐 :1253-1336）。
func AdmitCandidateEvidence(extracted *ExtractedCandidateEvidence, now float64) (*EvidenceBundle, error) {
	if !retrievalFiniteTimestamp(now) {
		return nil, NewEvidenceContractError("evidence_admission_input_failed", "", nil)
	}
	candidate := extracted.Candidate
	// 对齐 :1263-1274 新鲜过滤
	freshSpans := []ExactEvidenceSpan{}
	for i := range extracted.Spans {
		if PublishedAtIsFresh(extracted.Spans[i].PublishedAt, candidate.FreshnessClass, now) {
			freshSpans = append(freshSpans, extracted.Spans[i])
		}
	}
	if len(freshSpans) == 0 {
		return nil, NewEvidenceContractError("freshness_gate_failed", "", nil)
	}

	// 对齐 :1276-1324 来源闸分支
	var selected []ExactEvidenceSpan
	sourceGateMode := ""
	if candidate.FreshnessClass == "evergreen_corpus" {
		stable := []ExactEvidenceSpan{}
		for _, span := range freshSpans {
			if span.SourceKind == "authoritative" || span.SourceKind == "original" || span.SourceKind == "stable" {
				stable = append(stable, span)
			}
		}
		if len(stable) == 0 {
			return nil, NewEvidenceContractError("source_gate_failed", "", nil)
		}
		selected = []ExactEvidenceSpan{stable[0]}
		sourceGateMode = "single_stable_source"
	} else if candidate.FreshnessClass == NewsFreshnessClass {
		news := []ExactEvidenceSpan{}
		for _, span := range freshSpans {
			if span.SourceKind == NewsSourceKind {
				news = append(news, span)
			}
		}
		if len(news) == 0 {
			return nil, NewEvidenceContractError("source_gate_failed", "", nil)
		}
		selected = []ExactEvidenceSpan{news[0]}
		sourceGateMode = "single_news_report"
	} else {
		primary := []ExactEvidenceSpan{}
		for _, span := range freshSpans {
			if span.SourceKind == "authoritative" || span.SourceKind == "original" {
				primary = append(primary, span)
			}
		}
		if len(primary) > 0 {
			selected = []ExactEvidenceSpan{primary[0]}
			sourceGateMode = "single_primary_source"
		} else {
			reports := []ExactEvidenceSpan{}
			publishers := map[string]struct{}{}
			documents := map[string]struct{}{}
			for _, span := range freshSpans {
				if span.SourceKind != "reliable_report" {
					continue
				}
				if _, dup := publishers[span.Publisher]; dup {
					continue
				}
				if _, dup := documents[span.DocumentRevisionID]; dup {
					continue
				}
				reports = append(reports, span)
				publishers[span.Publisher] = struct{}{}
				documents[span.DocumentRevisionID] = struct{}{}
				if len(reports) == 2 {
					break
				}
			}
			if len(reports) != 2 {
				return nil, NewEvidenceContractError("source_gate_failed", "", nil)
			}
			selected = reports
			sourceGateMode = "two_independent_reports"
		}
	}

	return &EvidenceBundle{
		CandidateID:      candidate.CandidateID,
		ContextScope:     candidate.ContextScope,
		Category:         candidate.Category,
		FreshnessClass:   candidate.FreshnessClass,
		SnapshotDate:     extracted.SnapshotDate,
		SnapshotVersion:  extracted.SnapshotVersion,
		Spans:            selected,
		SourceGateMode:   sourceGateMode,
		ComponentReceipt: extracted.ComponentReceipt,
		ContractVersion:  EvidenceContractVersionStr,
	}, nil
}

// NewFixedStarterRecord 构造并校验（对齐 :1354-1388 __post_init__）。
func NewFixedStarterRecord(starterID, title, text, topic, hookType, freshnessClass,
	origin, category string, contextScopes []string, corpusVersion string) (*FixedStarterRecord, error) {
	r := &FixedStarterRecord{
		StarterID: starterID, Title: title, Text: text, Topic: topic,
		HookType: hookType, FreshnessClass: freshnessClass, Origin: origin,
		Category: category, ContextScopes: contextScopes, CorpusVersion: corpusVersion,
	}
	// 对齐 :1366-1372 九个非空串
	for _, v := range []string{r.StarterID, r.Title, r.Text, r.Topic, r.HookType,
		r.FreshnessClass, r.Origin, r.Category, r.CorpusVersion} {
		if !retrievalNonempty(v) {
			return nil, NewTerminalRetrievalContractError("invalid_corpus_record", "record fields must be non-empty", nil)
		}
	}
	// 对齐 :1373-1380 scope 非空、唯一、合法
	if len(r.ContextScopes) == 0 || hasStringDuplicates(r.ContextScopes) {
		return nil, NewTerminalRetrievalContractError("invalid_corpus_record", "context scopes must be unique and valid", nil)
	}
	for _, scope := range r.ContextScopes {
		if !retrievalValidScope(scope) {
			return nil, NewTerminalRetrievalContractError("invalid_corpus_record", "context scopes must be unique and valid", nil)
		}
	}
	// 对齐 :1381-1388 global 类别闸
	for _, scope := range r.ContextScopes {
		if scope == "global_new_chat" {
			if _, ok := globalFixedCategories[r.Category]; !ok {
				return nil, NewTerminalRetrievalContractError(
					"wrong_category",
					"global fixed starters require one of the two v6 product categories", nil)
			}
		}
	}
	return r, nil
}

// NewFrozenStarterCorpus 构造并校验（对齐 :1398-1407 __post_init__）。
func NewFrozenStarterCorpus(corpusVersion string, starters []FixedStarterRecord) (*FrozenStarterCorpus, error) {
	c := &FrozenStarterCorpus{CorpusVersion: corpusVersion, Starters: starters}
	if !retrievalNonempty(c.CorpusVersion) || len(c.Starters) == 0 {
		return nil, NewTerminalRetrievalContractError("invalid_corpus", "", nil)
	}
	ids := make([]string, 0, len(c.Starters))
	versions := map[string]struct{}{}
	for _, starter := range c.Starters {
		ids = append(ids, starter.StarterID)
		versions[starter.CorpusVersion] = struct{}{}
	}
	if hasStringDuplicates(ids) {
		return nil, NewTerminalRetrievalContractError("duplicate_corpus_id", "", nil)
	}
	if _, onlySelf := versions[c.CorpusVersion]; len(versions) != 1 || !onlySelf {
		return nil, NewTerminalRetrievalContractError("cross_version_corpus", "", nil)
	}
	return c, nil
}

// ByID 按起始 ID 索引（对齐 :1410-1411）。
func (c *FrozenStarterCorpus) ByID() map[string]FixedStarterRecord {
	byID := make(map[string]FixedStarterRecord, len(c.Starters))
	for _, starter := range c.Starters {
		byID[starter.StarterID] = starter
	}
	return byID
}

// NewRankedStarterInterest 构造并校验（对齐 :1420-1430 __post_init__）。
func NewRankedStarterInterest(interestSummaryID, interestSummary string, rank int) (*RankedStarterInterest, error) {
	i := &RankedStarterInterest{InterestSummaryID: interestSummaryID, InterestSummary: interestSummary, Rank: rank}
	if !retrievalNonempty(i.InterestSummaryID) || !retrievalNonempty(i.InterestSummary) || rank < 1 {
		return nil, NewTerminalRetrievalContractError("invalid_ranked_interest", "", nil)
	}
	return i, nil
}

// RetrieveFixedStarters 在文本主权保留于宿主的前提下排序语料 ID（对齐 :1517-1679）。
func RetrieveFixedStarters(
	userStateSnapshotID, contextScope string,
	rankedInterests []*RankedStarterInterest,
	corpus *FrozenStarterCorpus,
	corpusVersion string,
	providerBinding *LearnedComponentBinding,
	invokeRetriever FixedStarterRetriever,
	requestID string,
	topK int,
	fallbackReason string,
) (*FixedStarterRetrievalBatch, error) {
	// 对齐 :1532-1543 _rejected2 输入契约
	if !retrievalNonempty(userStateSnapshotID) || !retrievalValidScope(contextScope) ||
		!retrievalNonempty(requestID) || topK < 1 ||
		corpus.CorpusVersion != corpusVersion {
		return nil, NewTerminalRetrievalContractError("retrieval_input_contract_failed", "", nil)
	}
	interests, err := validateInterests(rankedInterests)
	if err != nil {
		return nil, err
	}
	// 对齐 :1545-1548 组件与厂商闸
	if providerBinding.ComponentID != PARetrieverComponentID {
		return nil, NewTerminalRetrievalContractError("wrong_provider_component", "", nil)
	}
	if providerBinding.ProviderID != PAAPIFallbackProviderID {
		return nil, NewTerminalRetrievalContractError("wrong_api_fallback_provider", "", nil)
	}

	invokedFailure := func(reason string) *TerminalRetrievalContractError {
		return NewTerminalRetrievalContractError(reason, "", failedComponentReceipt(
			providerBinding, userStateSnapshotID, requestID, reason,
		))
	}

	// 对齐 :1561-1565 scope 内可用语料
	eligible := []FixedStarterRecord{}
	for _, starter := range corpus.Starters {
		for _, scope := range starter.ContextScopes {
			if scope == contextScope {
				eligible = append(eligible, starter)
				break
			}
		}
	}
	if len(eligible) == 0 {
		return nil, NewTerminalRetrievalContractError("no_scope_eligible_starters", "", nil)
	}

	// 对齐 :1567-1596 请求 payload
	interestRows := make([]map[string]any, 0, len(interests))
	for _, interest := range interests {
		interestRows = append(interestRows, map[string]any{
			"interest_summary_id": interest.InterestSummaryID,
			"interest_summary":    interest.InterestSummary,
			"rank":                interest.Rank,
		})
	}
	corpusRows := make([]map[string]any, 0, len(eligible))
	for _, starter := range eligible {
		corpusRows = append(corpusRows, map[string]any{
			"starter_id":      starter.StarterID,
			"title":           starter.Title,
			"text":            starter.Text,
			"topic":           starter.Topic,
			"hook_type":       starter.HookType,
			"freshness_class": starter.FreshnessClass,
			"category":        starter.Category,
		})
	}
	requestPayload := map[string]any{
		"contract_version":     PARetrievalContractVersion,
		"request_id":           requestID,
		"input_snapshot_id":    userStateSnapshotID,
		"user_state_snapshot_id": userStateSnapshotID,
		"context_scope":        contextScope,
		"corpus_version":       corpusVersion,
		"provider_binding":     providerBinding.AsProvenance(),
		"top_k":                topK,
		"ranked_interests":     interestRows,
		"corpus":               corpusRows,
	}
	response, err := invokeRetriever(context.Background(), requestPayload)
	if err != nil {
		// 对齐 :1599-1600 fail-closed
		return nil, invokedFailure("provider_invocation_failed")
	}

	// 对齐 :1601-1640 provider_validation 包裹的业务校验；batch 为校验闭包的产出载体
	var batch *FixedStarterRetrievalBatch
	validationErr := func() error {
		responseMap, responseKeys, ok := payloadMap(response)
		var hitsRaw []any
		var isList bool
		if ok {
			hitsRaw, isList = responseMap["hits"].([]any)
		}
		if !ok || !stringSetEqual(responseKeys, []string{"hits"}) || !isList {
			return invokedFailure("retrieval_output_contract_failed")
		}
		if len(hitsRaw) > minInt(topK, len(eligible)) {
			return invokedFailure("retrieval_output_exceeds_top_k")
		}
		eligibleByID := map[string]FixedStarterRecord{}
		for _, starter := range eligible {
			eligibleByID[starter.StarterID] = starter
		}
		interestIDs := map[string]struct{}{}
		for _, interest := range interests {
			interestIDs[interest.InterestSummaryID] = struct{}{}
		}
		seen := map[string]struct{}{}
		type hitRow struct {
			starterID  string
			interestID *string
			rank       int
		}
		hitRows := []hitRow{}
		ignoredFields := []string{}
		for rank, raw := range hitsRaw {
			rawMap, rawKeys, rawOK := payloadMap(raw)
			if !rawOK {
				return invokedFailure("retrieval_output_contract_failed")
			}
			// 对齐 :1617-1622 required 子集 + extras 落入忽略白名单
			required := []string{"starter_id", "interest_summary_id"}
			if !stringSetContainsAll(rawKeys, required) {
				return invokedFailure("retrieval_output_contract_failed")
			}
			extras := stringSetDifference(rawKeys, required)
			for _, extra := range extras {
				if _, ignored := ignoredProviderTextFields[extra]; !ignored {
					return invokedFailure("retrieval_output_contract_failed")
				}
			}
			starterID := str(rawMap["starter_id"])
			interestID := optionalStringFromAny(rawMap["interest_summary_id"])
			if !retrievalNonempty(starterID) {
				return invokedFailure("unknown_or_wrong_scope_starter_id")
			}
			if _, known := eligibleByID[starterID]; !known {
				return invokedFailure("unknown_or_wrong_scope_starter_id")
			}
			if _, dup := seen[starterID]; dup {
				return invokedFailure("duplicate_starter_id")
			}
			if len(interests) > 0 {
				if interestID == nil {
					return invokedFailure("unknown_interest_summary_id")
				}
				if _, known := interestIDs[*interestID]; !known {
					return invokedFailure("unknown_interest_summary_id")
				}
			} else if interestID != nil {
				return invokedFailure("cold_hit_must_not_claim_interest")
			}
			seen[starterID] = struct{}{}
			// 对齐 :1635-1637 排序后的忽略字段名
			sortedExtras := append([]string{}, extras...)
			sort.Strings(sortedExtras)
			for _, field := range sortedExtras {
				ignoredFields = append(ignoredFields, fmt.Sprintf("hits[%d].%s", rank, field))
			}
			hitRows = append(hitRows, hitRow{starterID: starterID, interestID: interestID, rank: rank + 1})
		}

		// 对齐 :1642-1662 fallback_reason 合成与小票
		fallbackReasons := []string{}
		if fallbackReason != "" {
			fallbackReasons = append(fallbackReasons, fallbackReason)
		}
		if len(ignoredFields) > 0 {
			fallbackReasons = append(fallbackReasons, "provider_output_fields_ignored:"+strings.Join(ignoredFields, ","))
		}
		effectiveFallbackReason := strings.Join(fallbackReasons, ";")
		outputStatus := "success"
		scoreSemantics := "uncalibrated_api_proxy"
		if effectiveFallbackReason != "" {
			outputStatus = "fallback"
			scoreSemantics = "not_scored"
		}
		receipt, err := NewLearnedComponentReceipt(
			providerBinding, userStateSnapshotID, requestID,
			scoreSemantics, outputStatus, effectiveFallbackReason,
		)
		if err != nil {
			return err
		}
		hits := make([]FixedStarterHit, 0, len(hitRows))
		for _, row := range hitRows {
			hits = append(hits, FixedStarterHit{
				StarterID:         row.starterID,
				InterestSummaryID: derefStringOrEmpty(row.interestID),
				Rank:              row.rank,
				ComponentReceipt:  receipt,
			})
		}
		batch = &FixedStarterRetrievalBatch{
			UserStateSnapshotID:   userStateSnapshotID,
			ContextScope:          contextScope,
			CorpusVersion:         corpusVersion,
			Hits:                  hits,
			ComponentReceipt:      receipt,
			IgnoredProviderFields: ignoredFields,
			ContractVersion:       PARetrievalContractVersion,
		}
		return nil
	}()

	// 对齐 :1601 provider_validation 语义：校验失败记 business_validation_failed
	if validationErr != nil {
		recordProviderValidationOn(invokeRetriever, response, "business_validation_failed")
		return nil, validationErr
	}
	recordProviderValidationOn(invokeRetriever, response, "business_validated")
	return batch, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// identityTuple 文档身份二元组（对齐 :518 (document_id, version) == identity）。
type identityTuple struct {
	doc     string
	version string
}

// str any → string（非字符串一律空串，等价 Python 强转前的 isinstance 守卫）。
func str(value any) string {
	s, _ := value.(string)
	return s
}

// num any → float64（JSON 数字统一 float64；其余 0）。
func num(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

// payloadMap any → map[string]any（Python isinstance(payload, dict) 守卫）。
func payloadMap(value any) (map[string]any, []string, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, nil, false
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return m, keys, true
}

// stringSetEqual 键集合与期望集合是否完全一致（对齐 Python set(payload) != {...}）。
func stringSetEqual(keys []string, expected []string) bool {
	if len(keys) != len(expected) {
		return false
	}
	set := map[string]struct{}{}
	for _, k := range keys {
		set[k] = struct{}{}
	}
	for _, e := range expected {
		if _, ok := set[e]; !ok {
			return false
		}
	}
	return true
}

// stringSetDifference keys 中不在 expected 内的部分（对齐 Python set 差集）。
func stringSetDifference(keys []string, expected []string) []string {
	expectedSet := map[string]struct{}{}
	for _, e := range expected {
		expectedSet[e] = struct{}{}
	}
	diff := []string{}
	for _, k := range keys {
		if _, ok := expectedSet[k]; !ok {
			diff = append(diff, k)
		}
	}
	return diff
}

// stringSetContainsAll keys 是否包含 required 全部成员（对齐 Python required.issubset）。
func stringSetContainsAll(keys []string, required []string) bool {
	set := map[string]struct{}{}
	for _, k := range keys {
		set[k] = struct{}{}
	}
	for _, r := range required {
		if _, ok := set[r]; !ok {
			return false
		}
	}
	return true
}

// hasStringDuplicates 字符串切片去重判定。
func hasStringDuplicates(values []string) bool {
	set := map[string]struct{}{}
	for _, v := range values {
		if _, dup := set[v]; dup {
			return true
		}
		set[v] = struct{}{}
	}
	return false
}

// minInt 两数取小。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// minIntSlice 切片取小（调用方保证非空）。
func minIntSlice(values []int) int {
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

// stringPtr 字符串指针（对齐 Python 非 None 标量字段）。
func stringPtr(value string) *string { return &value }

// derefStringOrEmpty 空指针回空串（Python None → "" 的字段收敛）。
func derefStringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// optionalString *string → any（nil 对齐 Python None）。
func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// optionalStringFromEmpty 空串对齐 Python None。
func optionalStringFromEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// optionalStringFromAny any → *string（nil/非串返回 nil，对齐 str|None）。
func optionalStringFromAny(value any) *string {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	return &s
}

// optionalFloat *float64 → any（nil 对齐 None）。
func optionalFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

// optionalFloatFromAny any → *float64（nil/非数字返回 nil）。
func optionalFloatFromAny(value any) *float64 {
	switch v := value.(type) {
	case float64:
		return &v
	case int:
		f := float64(v)
		return &f
	default:
		return nil
	}
}

// copyStringMap 字符串 map 浅拷贝（对齐 dict(self.feature_version_refs)）。
func copyStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

// copyAnyMap any map 浅拷贝（对齐 dict(self.linked_page)）。
func copyAnyMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

// mapValueOrEmpty 取键值（缺键空串，对齐 (self.linked_page or {}).get("body_url", "")）。
func mapValueOrEmpty(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return str(values[key])
}

// retrievalNonempty 非空白字符串（对齐 :56-57 _nonempty，复用 terminalNonempty 实现）。
func retrievalNonempty(value string) bool { return terminalNonempty(value) }

// retrievalValidScope 合法上下文范围（对齐 :60-67 _valid_scope）。
func retrievalValidScope(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	if s == "global_new_chat" {
		return true
	}
	if !strings.HasPrefix(s, "project:") {
		return false
	}
	rest := strings.TrimPrefix(s, "project:")
	return terminalNonempty(rest) && rest == strings.TrimSpace(rest)
}

// retrievalFiniteTimestamp 有限非负时间戳（对齐 :119-124 _finite_timestamp）。
func retrievalFiniteTimestamp(value any) bool {
	switch v := value.(type) {
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
	case int:
		return v >= 0
	case int64:
		return v >= 0
	default:
		return false
	}
}

// ParseISOTimestamp 解析 ISO 时间戳（对齐 :477-488 _parse_iso_timestamp）。
// 返回 (unix 秒, ok)；None/空白/解析失败均返回 ok=false。
func ParseISOTimestamp(value string) (float64, bool) {
	if value == "" {
		return 0, false
	}
	if !terminalNonempty(value) {
		return 0, false
	}
	// 对齐 :483 Z → +00:00
	normalized := strings.Replace(value, "Z", "+00:00", -1)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			return float64(parsed.Unix()), true
		}
	}
	return 0, false
}

// PublishedAtIsFresh 发布时间新鲜度判定（对齐 :956-971 _published_at_is_fresh）。
// publishedAt 空串对齐 Python None。
func PublishedAtIsFresh(publishedAt, freshnessClass string, now float64) bool {
	if freshnessClass == "evergreen_corpus" {
		return true
	}
	publishedTimestamp, ok := ParseISOTimestamp(publishedAt)
	if !ok {
		return false
	}
	ageSeconds := now - publishedTimestamp
	if freshnessClass == NewsFreshnessClass {
		return ageSeconds >= 0 && ageSeconds <= NewsMaxWindowHours*60*60
	}
	maxDays := 90.0
	if freshnessClass == "current_event" {
		maxDays = 7
	}
	return ageSeconds >= 0 && ageSeconds <= maxDays*24*60*60
}

// RequiredSourceGateMode 来源闸模式（对齐 :1029-1042）。
func RequiredSourceGateMode(candidate *EvidenceCandidate, documents []*FrozenDocumentRevision) string {
	if candidate.FreshnessClass == "evergreen_corpus" {
		return "single_stable_source"
	}
	if candidate.FreshnessClass == NewsFreshnessClass {
		return "single_news_report"
	}
	for _, document := range documents {
		if document.SourceKind == "authoritative" || document.SourceKind == "original" {
			return "single_primary_source"
		}
	}
	return "two_independent_reports"
}

// FreshForCandidate 片段新鲜判定（对齐 :1240-1250 _fresh_for_candidate）。
func FreshForCandidate(span *ExactEvidenceSpan, freshnessClass string, now float64) bool {
	return PublishedAtIsFresh(span.PublishedAt, freshnessClass, now)
}

// EvidenceExtractor 证据抽取器调用形态（对齐 :920 EvidenceExtractor Callable）。
type EvidenceExtractor = func(ctx context.Context, payload map[string]any) (any, error)

// FixedStarterRetriever 固定起始检索器调用形态（对齐 :1501）。
type FixedStarterRetriever = func(ctx context.Context, payload map[string]any) (any, error)

// hostExactOffsetCatalog 宿主计算的可复制边界目录，不选择证据（对齐 :923-953）。
// Python 字符索引语义 → Go 全程 []rune。
func hostExactOffsetCatalog(content string) []map[string]any {
	catalog := []map[string]any{}
	runes := []rune(content)
	markers := []string{"\n\n", "\n", "。", "！", "？", ". ", "! ", "? "}
	cursor := 0
	for cursor < len(runes) {
		for cursor < len(runes) && isSpaceRune(runes[cursor]) {
			cursor++
		}
		if cursor >= len(runes) {
			break
		}
		hardEnd := cursor + OffsetCatalogMaxChars
		if hardEnd > len(runes) {
			hardEnd = len(runes)
		}
		window := string(runes[cursor:hardEnd])
		boundaries := []int{}
		for _, marker := range markers {
			// strings.Index 返回字节偏移，需换算为 rune 偏移以对齐 Python 字符索引
			if byteIdx := strings.Index(window, marker); byteIdx >= 0 {
				runeIdx := len([]rune(window[:byteIdx]))
				boundaries = append(boundaries, runeIdx+len([]rune(marker)))
			}
		}
		end := hardEnd
		if len(boundaries) > 0 {
			end = cursor + minIntSlice(boundaries)
		}
		for end > cursor && isSpaceRune(runes[end-1]) {
			end--
		}
		if end <= cursor {
			end = hardEnd
		}
		catalog = append(catalog, map[string]any{
			"start":      cursor,
			"end":        end,
			"exact_text": string(runes[cursor:end]),
		})
		cursor = end
	}
	return catalog
}

// validateInterests 兴趣排序一致性校验（对齐 :1504-1514 _validate_interests）。
func validateInterests(interests []*RankedStarterInterest) ([]*RankedStarterInterest, error) {
	ids := make([]string, 0, len(interests))
	ranks := make([]int, 0, len(interests))
	for _, interest := range interests {
		ids = append(ids, interest.InterestSummaryID)
		ranks = append(ranks, interest.Rank)
	}
	if hasStringDuplicates(ids) || !isConsecutiveRanks(ranks) {
		return nil, NewTerminalRetrievalContractError("invalid_ranked_interests", "", nil)
	}
	return interests, nil
}

// failedComponentReceipt 失败小票（对齐 :102-116 _failed_component_receipt）。
func failedComponentReceipt(binding *LearnedComponentBinding, inputSnapshotID, requestID, reason string) *LearnedComponentReceipt {
	receipt, err := NewLearnedComponentReceipt(
		binding, inputSnapshotID, requestID, "not_scored", "failed", reason,
	)
	if err != nil {
		// 契约层失败小票构造不可能失败（全部常量合法），触发即实现缺陷
		panic(fmt.Sprintf("failed_component_receipt: %v", err))
	}
	return receipt
}

// recordProviderValidationOn 鸭子类型 record_validation 的等价记录（对齐 topic_model.py:429-436）。
// invoker 未实现 RecordValidation（如测试用假检索器）时为 no-op。
func recordProviderValidationOn(invoker any, response any, outcome string) {
	recorder, ok := any(invoker).(interface {
		RecordValidation(response any, outcome string)
	})
	if ok {
		recorder.RecordValidation(response, outcome)
	}
}

// isSpaceRune Python str.isspace 的等价判定（对齐 :929/:941 isspace）。
func isSpaceRune(r rune) bool { return strings.ContainsRune(" \t\n\r\v\f\x1c\x1d\x1e\x1f\x85", r) || r == 0x20 }

// isConsecutiveRanks 名次必须是 1..n 的排列（对齐 :1510-1512 sorted(ranks) == range(1,n+1)）。
func isConsecutiveRanks(ranks []int) bool {
	sorted := append([]int{}, ranks...)
	sort.Ints(sorted)
	for i, r := range sorted {
		if r != i+1 {
			return false
		}
	}
	return true
}
