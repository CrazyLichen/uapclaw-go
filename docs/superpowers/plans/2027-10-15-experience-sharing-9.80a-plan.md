# 9.80a ExperienceSharing 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现跨 Agent 经验共享（ExperienceSharing），包括数据类型、SharingBackend 接口、LocalFileBackend、KeywordExtractor、ShareStager、ExperienceSharer、ExperienceHubClient。

**Architecture:** 1:1 复刻 Python `openjiuwen/agent_evolving/sharing/`，自底向上实现：types → backend/interface → backend/local_file → keyword_extractor → experience_sharer → share_stager → hub_client。每个组件独立测试，覆盖率 ≥ 85%。

**Tech Stack:** Go 1.22+, sync.RWMutex, encoding/json, regexp, os/path/filepath, 依赖已有包 checkpointing/signal/llm_resilience/llm

---

## 文件清单

| 文件 | 职责 |
|---|---|
| `internal/evolving/sharing/doc.go` | 包文档 |
| `internal/evolving/sharing/types.go` | 7 个数据类型 + DroppedRecord + ToDict/FromDict |
| `internal/evolving/sharing/types_test.go` | 类型序列化/反序列化测试 |
| `internal/evolving/sharing/keyword_extractor.go` | KeywordExtractor + 常量 + 提示词 |
| `internal/evolving/sharing/keyword_extractor_test.go` | ParseFromOptimizerOutput + ExtractQueryKeywords mock 测试 |
| `internal/evolving/sharing/share_stager.go` | ShareStager + QC + messagesHasSuccessfulTool |
| `internal/evolving/sharing/share_stager_test.go` | QC 门控 + ScreenAndStage 测试 |
| `internal/evolving/sharing/experience_sharer.go` | ExperienceSharer + SkillSharingContextProvider |
| `internal/evolving/sharing/experience_sharer_test.go` | Stage/Flush/Download/Search 测试 |
| `internal/evolving/sharing/hub_client.go` | ExperienceHubClient |
| `internal/evolving/sharing/hub_client_test.go` | SearchSkills + InstallSkill 测试 |
| `internal/evolving/sharing/backend/doc.go` | 子包文档 |
| `internal/evolving/sharing/backend/interface.go` | SharingBackend 接口 |
| `internal/evolving/sharing/backend/local_file.go` | LocalFileBackend + jaccard |
| `internal/evolving/sharing/backend/local_file_test.go` | upload/download/去重/不可变/索引 测试 |
| `internal/evolving/signal/from_conv.go` | **修改**: 新增 `MatchFailureKeywords` 导出函数 |

---

## 前置变更：signal 包导出 `MatchFailureKeywords`

**背景**：Python 的 `share_stager.py` 导入 `from openjiuwen.agent_evolving.signal.from_conv import _FAILURE_KEYWORDS` 并在 `_messages_has_successful_tool` 中使用。Go 中 `failureKeywords` 是非导出变量，sharing 包无法访问。需要在 signal 包中新增一个导出函数。

### Task 0: signal 包新增 `MatchFailureKeywords` 导出函数

**Files:**
- Modify: `internal/evolving/signal/from_conv.go`（在导出函数区末尾添加）
- Modify: `internal/evolving/signal/from_conv_test.go`（添加测试）

- [ ] **Step 1: 在 from_conv.go 导出函数区末尾添加 `MatchFailureKeywords`**

在 `// ──────────────────────────── 导出函数 ────────────────────────────` 区末尾添加：

```go
// MatchFailureKeywords 检查 content 是否包含失败关键词。
//
// 导出此函数供 sharing 包的 ShareStager 使用，
// 因为 Go 无法跨包访问非导出变量 failureKeywords。
//
// Python: openjiuwen/agent_evolving/signal/from_conv._FAILURE_KEYWORDS.search(content)
func MatchFailureKeywords(content string) bool {
	return failureKeywords.MatchString(content)
}
```

- [ ] **Step 2: 在 from_conv_test.go 添加测试**

```go
func TestMatchFailureKeywords_匹配(t *testing.T) {
	cases := []string{"Error: something", "Exception thrown", "failed", "超时", "错误"}
	for _, tc := range cases {
		if !signal.MatchFailureKeywords(tc) {
			t.Errorf("MatchFailureKeywords should match %q", tc)
		}
	}
}

func TestMatchFailureKeywords_不匹配(t *testing.T) {
	cases := []string{"success", "completed", "正常完成", "ok"}
	for _, tc := range cases {
		if signal.MatchFailureKeywords(tc) {
			t.Errorf("MatchFailureKeywords should not match %q", tc)
		}
	}
}
```

注意：测试文件中 signal 包的引用方式——因为测试文件在同包内，需要直接调用 `MatchFailureKeywords` 而非 `signal.MatchFailureKeywords`。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -run "TestMatchFailureKeywords" -v`

- [ ] **Step 4: Commit**

```
feat(signal): 导出 MatchFailureKeywords 供 sharing 包使用
```

---

## Task 1: sharing 包 doc.go

**Files:**
- Create: `internal/evolving/sharing/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package sharing 提供跨 Agent 经验共享功能。
//
// 本包实现了经验的上传、下载、搜索和质量筛选，
// 让不同用户的技能演进经验可以共享和复用。
//
// 两条数据路径：
//   - 上传路径：本地已审批的 EvolutionRecord → ShareStager QC 筛选 →
//     KeywordExtractor 关键词提取 → 打包为 SharedSkillBundle → 上传到 SharingBackend
//   - 下载路径：KeywordExtractor 从对话摘录提取检索关键词 →
//     ExperienceHubClient 搜索/下载 → EvolutionStore 安装到本地
//
// 文件目录：
//
//	sharing/
//	├── doc.go                  # 包文档
//	├── types.go                # 共享数据类型（SharingMeta/SharedExperience/SharedSkillBundle 等）
//	├── keyword_extractor.go    # 关键词提取器（上传解析 + 下载 LLM 提取）
//	├── share_stager.go         # QC 筛选 + 入队
//	├── experience_sharer.go    # 上传/下载门面 + 待上传队列
//	├── hub_client.go           # 搜索 + 安装高层封装
//	└── backend/                # 后端接口和实现
//	    ├── doc.go              # 子包文档
//	    ├── interface.go        # SharingBackend 抽象接口
//	    └── local_file.go       # LocalFileBackend 本地文件实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/sharing/
package sharing
```

- [ ] **Step 2: Commit**

```
feat(sharing): 添加包文档 doc.go
```

---

## Task 2: sharing/backend 包 doc.go

**Files:**
- Create: `internal/evolving/sharing/backend/doc.go`

- [ ] **Step 1: 创建 backend/doc.go**

```go
// Package backend 提供经验共享的后端存储接口和实现。
//
// SharingBackend 定义了上传/下载/搜索的抽象契约，
// LocalFileBackend 是基于本地文件系统的参考实现。
//
// 文件目录：
//
//	backend/
//	├── doc.go          # 子包文档
//	├── interface.go    # SharingBackend 抽象接口
//	└── local_file.go   # LocalFileBackend 本地文件实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/sharing/backends/
package backend
```

- [ ] **Step 2: Commit**

```
feat(sharing/backend): 添加子包文档 doc.go
```

---

## Task 3: types.go — 共享数据类型

**Files:**
- Create: `internal/evolving/sharing/types.go`
- Create: `internal/evolving/sharing/types_test.go`

### types.go

```go
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
	// BundleID Bundle ID
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
		StagedForShare:   []SharedExperience{},
		DroppedForShare:  []DroppedRecord{},
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
	payload := map[string]any{
		"record":   e.Record.ToDict(),
		"keywords": e.Keywords,
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
	return map[string]any{
		"bundle_id":          b.BundleID,
		"skill_id":           b.SkillID,
		"skill_name":         b.SkillName,
		"skill_version":      b.SkillVersion,
		"keywords_aggregate": b.KeywordsAggregate,
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
func (m *SkillPackageMeta) ToDict() map[string]any {
	return map[string]any{
		"skill_id":    m.SkillID,
		"skill_name":  m.SkillName,
		"description": m.Description,
		"uploaded_at": m.UploadedAt,
	}
}

// FromDictSkillPackageMeta 从字典创建 SkillPackageMeta。
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
func (q *QueryKeywords) ToDict() map[string]any {
	return map[string]any{
		"keywords":    q.Keywords,
		"intent":      q.Intent,
		"raw_excerpt": q.RawExcerpt,
	}
}

// FromDictQueryKeywords 从字典创建 QueryKeywords。
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
```

### types_test.go

- [ ] **Step 1: 写 types_test.go 测试**

测试要点：
- `NewSharingMeta` 默认值
- `SharingMeta.ToDict` / `FromDictSharingMeta` 往返
- `SharedExperience.ToDict` / `FromDictSharedExperience` 往返
- `MakeSharedSkillBundle` 关键词去重和 summary 拼接
- `SharedSkillBundle.ToDict` / `FromDictSharedSkillBundle` 往返（含 `skill_content_hash` 回退）
- `SkillPackageMeta.ToDict` / `FromDictSkillPackageMeta` 往返
- `SkillSearchResult.ToDict` / `FromDictSkillSearchResult` 往返
- `QueryKeywords.ToDict` / `FromDictQueryKeywords` 往返
- `EmptyStagingResult` / `HasShareable`
- `UploadResult.MarshalJSON`

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/sharing/ -run "Test" -v`

- [ ] **Step 3: Commit**

```
feat(sharing): 实现共享数据类型 types.go
```

---

## Task 4: backend/interface.go — SharingBackend 接口

**Files:**
- Create: `internal/evolving/sharing/backend/interface.go`

- [ ] **Step 1: 写 interface.go**

```go
package backend

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/evolving/sharing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// SharingBackend 经验共享后端接口。
//
// Hub 按 skill_id 分区存储，每个技能保持一个不可变的技能包，
// 经验 bundle 随时间追加到同一 skill_id 下。
//
// Python: openjiuwen/agent_evolving/sharing/backends/base.py SharingBackend
type SharingBackend interface {
	// UploadBundle 上传经验 bundle，返回上传结果。
	UploadBundle(ctx context.Context, bundle sharing.SharedSkillBundle) sharing.UploadResult

	// DownloadBundles 按 skill_id 和关键词检索，返回最多 topK 个 bundle。
	DownloadBundles(ctx context.Context, skillID string, query sharing.QueryKeywords, topK int) []sharing.SharedSkillBundle

	// HasSkillPackage Hub 是否已有该技能包。
	HasSkillPackage(ctx context.Context, skillID string) bool

	// UploadSkillPackage 上传初始技能包（不可变，重复上传为 no-op）。
	UploadSkillPackage(ctx context.Context, skillID string, packageBytes []byte, meta sharing.SkillPackageMeta) error

	// DownloadSkillPackage 下载技能包字节。
	DownloadSkillPackage(ctx context.Context, skillID string) ([]byte, error)

	// GetSkillPackageMeta 获取技能包元数据。
	GetSkillPackageMeta(ctx context.Context, skillID string) (*sharing.SkillPackageMeta, error)

	// SearchSkills 全局关键词搜索技能。
	SearchSkills(ctx context.Context, query sharing.QueryKeywords, topK int) []sharing.SkillSearchResult
}
```

- [ ] **Step 2: Commit**

```
feat(sharing/backend): 实现 SharingBackend 抽象接口
```

---

## Task 5: backend/local_file.go — LocalFileBackend

**Files:**
- Create: `internal/evolving/sharing/backend/local_file.go`
- Create: `internal/evolving/sharing/backend/local_file_test.go`

这是最大的一个组件（~480 行 Python），对照 Python `backends/local_file.py` 1:1 实现。

### 关键实现点

1. **Hub 目录布局**：packages/bundles/index/global.jsonl/.outbox
2. **jaccard** 私有函数（含子串降级）
3. **去重**：upload_bundle 时检查 jaccard ≥ 0.85
4. **不可变技能包**：upload_skill_package 已存在则跳过
5. **outbox 暂存**：写入失败时 spool
6. **全局索引**：_upsertGlobalIndex / _ensureGlobalIndexEntry / _readGlobalIndex / _writeGlobalIndex
7. **sync.RWMutex**：写操作加写锁，读不加锁
8. **_readIndex**：读取 per-skill .jsonl 索引
9. **_loadBundle**：加载单个 bundle JSON 文件

### 测试要点（对齐 Python test_sharing_module.py）

- `TestLocalFileBackend_UploadAndDownload`：上传 bundle + 下载
- `TestLocalFileBackend_DifferentSkillIDsNoCollide`：不同 skill_id 不冲突
- `TestLocalFileBackend_SkillPackageImmutable`：重复上传技能包保持第一个
- `TestLocalFileBackend_RejectsDuplicateOnUpload`：高 jaccard 被拒绝
- `TestLocalFileBackend_SearchSkills`：全局搜索
- `TestLocalFileBackend_DownloadSkillPackage`：下载技能包字节
- `TestLocalFileBackend_GetSkillPackageMeta`：获取元数据
- `TestLocalFileBackend_HasSkillPackage`：存在性检查

- [ ] **Step 1: 写 local_file.go 完整实现**

- [ ] **Step 2: 写 local_file_test.go 测试**

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/sharing/backend/ -v`

- [ ] **Step 4: Commit**

```
feat(sharing/backend): 实现 LocalFileBackend 本地文件后端
```

---

## Task 6: keyword_extractor.go — 关键词提取器

**Files:**
- Create: `internal/evolving/sharing/keyword_extractor.go`
- Create: `internal/evolving/sharing/keyword_extractor_test.go`

### 关键实现点

1. **ParseFromOptimizerOutput**：静态方法，从 EvolutionPatch 或 map[string]any 提取 keywords + summary
2. **ExtractQueryKeywords**：LLM 调用提取，失败返回空（不阻塞）
3. **提示词**：一比一复刻 Python 的中/英文版本
4. **QUERY_KEYWORDS_LLM_POLICY**：常量
5. **extractQueryJSON**：私有辅助，先 json.Unmarshal，失败则正则 `{[\s\S]*}`
6. **UpdateLLM**：后置绑定

### 测试要点

- `TestParseFromOptimizerOutput_FromPatch`：从 EvolutionPatch 提取
- `TestParseFromOptimizerOutput_FromDict`：从 map[string]any 提取
- `TestExtractQueryKeywords_NoLLM`：LLM 为 nil 返回空关键词
- `TestExtractQueryKeywords_WithMockLLM`：mock LLM 返回有效 JSON
- `TestExtractQueryKeywords_LLMFailure`：LLM 失败时返回空关键词
- `TestExtractQueryJSON`：JSON 解析 + 正则回退

- [ ] **Step 1: 写 keyword_extractor.go 完整实现**

- [ ] **Step 2: 写 keyword_extractor_test.go 测试**

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/sharing/ -run "TestKeywordExtractor|TestParseFromOptimizer|TestExtractQuery|TestExtractQueryJSON" -v`

- [ ] **Step 4: Commit**

```
feat(sharing): 实现 KeywordExtractor 关键词提取器
```

---

## Task 7: experience_sharer.go — 上传/下载门面

**Files:**
- Create: `internal/evolving/sharing/experience_sharer.go`
- Create: `internal/evolving/sharing/experience_sharer_test.go`

### 关键实现点

1. **SkillSharingContextProvider** 函数类型
2. **内存队列**：pendingUploads + pendingKeys（dedupKey）
3. **sync.RWMutex** 保护队列
4. **StageForUpload**：入队 + (skill_name, record_id) 去重
5. **DiscardPendingUploads**：丢弃队列
6. **FlushPendingUploads**：打包 → syncSkillPackage → 重试上传
7. **syncSkillPackage**：通过 contextProvider 获取 skill_id + package，上传技能包（首次）
8. **DownloadRelevant**：下载 + mirrorBundle
9. **SearchSkills / DownloadSkillPackage / GetSkillPackageMeta / ListCachedBundles**
10. **mirrorBundle**：写入 local_cache_dir/{uploaded|downloaded}/{skill_id}/{bundle_id}.json

### 测试要点

- `TestExperienceSharer_StageForUpload_Dedup`：去重
- `TestExperienceSharer_StageForUpload_EmptySkillName`：空名称不入场
- `TestExperienceSharer_DiscardPendingUploads`：丢弃队列
- `TestExperienceSharer_FlushPendingUploads_UploadsInitialPackage`：首次上传技能包
- `TestExperienceSharer_FlushPendingUploads_SkillIDUnavailable`：skill_id 为空
- `TestExperienceSharer_FlushPendingUploads_RetryOnRetryable`：可重试失败
- `TestExperienceSharer_FlushPendingUploads_NoRetryOnNonRetryable`：不可重试立即返回
- `TestExperienceSharer_DownloadRelevant`：下载 + 镜像
- `TestExperienceSharer_ListCachedBundles`：列出缓存
- `TestExperienceSharer_ResolveSkillID`：通过 provider 获取

- [ ] **Step 1: 写 experience_sharer.go 完整实现**

- [ ] **Step 2: 写 experience_sharer_test.go 测试**

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/sharing/ -run "TestExperienceSharer" -v`

- [ ] **Step 4: Commit**

```
feat(sharing): 实现 ExperienceSharer 上传/下载门面
```

---

## Task 8: share_stager.go — QC 筛选 + 入队

**Files:**
- Create: `internal/evolving/sharing/share_stager.go`
- Create: `internal/evolving/sharing/share_stager_test.go`

### 关键实现点

1. **messagesHasSuccessfulTool**：检查消息列表中是否有非失败的工具结果，使用 `signal.MatchFailureKeywords`
2. **QC 门控**：execution_failure 无成功后续 + 分数低于阈值
3. **ScreenAndStage**：逐条 QC → wrap → sharer.StageForUpload
4. **wrap**：创建 SharingMeta + SharedExperience（EvolutionRecord 值拷贝，Keywords 显式 copy）

### 测试要点

- `TestMessagesHasSuccessfulTool_有成功工具`：工具内容无失败关键词 → true
- `TestMessagesHasSuccessfulTool_全是失败`：工具内容含失败关键词 → false
- `TestMessagesHasSuccessfulTool_无消息`：nil → false
- `TestShareStager_ScreenAndStage_正常记录通过`：source="user_correction" + score=0.8
- `TestShareStager_ScreenAndStage_ExecutionFailure无成功工具`：丢弃
- `TestShareStager_ScreenAndStage_ScoreBelowThreshold`：score=0.3 < 0.6 → 丢弃
- `TestShareStager_ScreenAndStage_空记录`：返回空 StagingResult

- [ ] **Step 1: 写 share_stager.go 完整实现**

- [ ] **Step 2: 写 share_stager_test.go 测试**

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/sharing/ -run "TestShareStager|TestMessagesHasSuccessfulTool" -v`

- [ ] **Step 4: Commit**

```
feat(sharing): 实现 ShareStager QC 筛选和入队
```

---

## Task 9: hub_client.go — 搜索 + 安装

**Files:**
- Create: `internal/evolving/sharing/hub_client.go`
- Create: `internal/evolving/sharing/hub_client_test.go`

### 关键实现点

1. **SearchSkills**：委托 sharer.SearchSkills
2. **InstallSkill**：下载技能包 → meta → store.InstallSkillPackage

### 测试要点

- `TestExperienceHubClient_SearchSkills`：搜索返回结果
- `TestExperienceHubClient_InstallSkill`：下载 + 安装
- `TestExperienceHubClient_InstallSkill_EmptyPackage`：skill_id 为空返回 error

- [ ] **Step 1: 写 hub_client.go 完整实现**

- [ ] **Step 2: 写 hub_client_test.go 测试**

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/sharing/ -run "TestExperienceHubClient" -v`

- [ ] **Step 4: Commit**

```
feat(sharing): 实现 ExperienceHubClient 搜索和安装
```

---

## Task 10: 全量编译 + 覆盖率检查

- [ ] **Step 1: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

- [ ] **Step 2: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/evolving/sharing/... ./internal/evolving/sharing/backend/...`

目标：≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 9.80a 的 `☐` 改为 `✅`

---

## 自查清单

| 检查项 | 状态 |
|---|---|
| Spec 中每个类型都有 Task 实现吗？ | ✅ Task 3 |
| Spec 中 SharingBackend 7 个方法都有吗？ | ✅ Task 4+5 |
| Spec 中 LocalFileBackend 所有功能都有吗？ | ✅ Task 5 |
| Spec 中 KeywordExtractor 两条路径都有吗？ | ✅ Task 6 |
| Spec 中 ShareStager QC + ScreenAndStage 都有吗？ | ✅ Task 8 |
| Spec 中 ExperienceSharer 所有方法都有吗？ | ✅ Task 7 |
| Spec 中 HubClient SearchSkills + InstallSkill 都有吗？ | ✅ Task 9 |
| signal 包 failureKeywords 访问问题解决了吗？ | ✅ Task 0 |
| 无 TBD/TODO 占位符？ | ✅ |
| 方法名/类型名前后一致？ | ✅ |
