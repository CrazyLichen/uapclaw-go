# 9.82 P2 IO Schema 层实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Context Evolver P2 IO Schema 层的所有数据类型，为 P3-P5 算法管线提供 IO 模型基础。

**Architecture:** 新增 `internal/agentcore/context_evolver/schema/` 包，包含 3 个源文件（trajectory.go / memory.go / io_schema.go）+ 3 个测试文件。该包依赖 P1 的 `core/schema` 包（VectorNode）。为避免包名冲突，导入 `core/schema` 时使用别名 `coreschema`。

**Tech Stack:** Go 1.18+ 泛型、标准库 `encoding/json`、`time`、`crypto/md5`、`fmt`

**设计文档:** `docs/superpowers/specs/2027-12-01-context-evolver-9.82-p2-io-schema-design.md`

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 修改 | `internal/agentcore/context_evolver/doc.go` | 更新包文档文件目录，新增 schema/ 条目 |
| 创建 | `internal/agentcore/context_evolver/schema/doc.go` | schema 子包文档 |
| 创建 | `internal/agentcore/context_evolver/schema/trajectory.go` | FeedbackType + Trajectory + TrajectoryBatch |
| 创建 | `internal/agentcore/context_evolver/schema/trajectory_test.go` | Trajectory 系列测试 |
| 创建 | `internal/agentcore/context_evolver/schema/memory.go` | BaseMemory + MemoryInterface + 各 Memory 类型 + 包级构造函数 |
| 创建 | `internal/agentcore/context_evolver/schema/memory_test.go` | Memory 系列测试 |
| 创建 | `internal/agentcore/context_evolver/schema/io_schema.go` | ACE/RB/ReMe Request/Response + 泛型 Response |
| 创建 | `internal/agentcore/context_evolver/schema/io_schema_test.go` | IO Schema 系列测试 |

---

### Task 1: 创建 schema 包文档 + 更新顶层 doc.go

**Files:**
- Create: `internal/agentcore/context_evolver/schema/doc.go`
- Modify: `internal/agentcore/context_evolver/doc.go`

- [ ] **Step 1: 创建 schema/doc.go**

```go
// Package schema 提供上下文记忆演化系统的 IO 数据模式定义。
//
// 定义三条算法管线（ACE/ReasoningBank/ReMe）共用的输入输出类型，
// 包括轨迹（Trajectory）、记忆（Memory）和请求/响应（Request/Response）。
// 所有 Memory 类型实现 MemoryInterface 接口，支持与 VectorNode 双向转换。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── trajectory.go    # FeedbackType + Trajectory + TrajectoryBatch 轨迹系列
//	├── memory.go        # BaseMemory + MemoryInterface + ACE/RB/ReMe Memory 类型
//	└── io_schema.go     # ACE/RB/ReMe Request/Response + 泛型 SummarizeResponse/RetrieveResponse
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/schema/
package schema
```

- [ ] **Step 2: 更新顶层 doc.go 文件目录**

在 `internal/agentcore/context_evolver/doc.go` 的文件目录中，`core/` 条目之后新增 `schema/` 条目：

```go
// 文件目录：
//
//	context_evolver/
//	├── doc.go                                # 包文档
//	├── core/                                 # 核心框架子包
//	│   ├── context/                          # RuntimeContext + ServiceContext
//	│   ├── op/                               # BaseOp + SequentialOp + ParallelOp
//	│   ├── schema/                           # VectorNode
//	│   ├── vector_store/                     # MemoryVectorStore
//	│   ├── file_connector/                   # JSONFileConnector
//	│   └── persistence/                      # MemoryPersistenceHelper
//	└── schema/                               # IO Schema 子包
//	    ├── doc.go                            # 包文档
//	    ├── trajectory.go                     # FeedbackType + Trajectory + TrajectoryBatch
//	    ├── memory.go                         # BaseMemory + MemoryInterface + 各 Memory 类型
//	    └── io_schema.go                      # ACE/RB/ReMe Request/Response + 泛型 Response
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_evolver/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_evolver/doc.go internal/agentcore/context_evolver/schema/doc.go
git commit -m "feat(context_evolver): 添加 schema 子包文档和顶层 doc.go 更新"
```

---

### Task 2: 实现 trajectory.go — FeedbackType + Trajectory + TrajectoryBatch

**Files:**
- Create: `internal/agentcore/context_evolver/schema/trajectory.go`
- Create: `internal/agentcore/context_evolver/schema/trajectory_test.go`

- [ ] **Step 1: 编写 trajectory_test.go 失败测试**

```go
package schema

import (
	"encoding/json"
	"testing"
)

func TestFeedbackType_String(t *testing.T) {
	tests := []struct {
		ft    FeedbackType
		want  string
	}{
		{FeedbackHelpful, "helpful"},
		{FeedbackHarmful, "harmful"},
		{FeedbackNeutral, "neutral"},
	}
	for _, tt := range tests {
		if got := tt.ft.String(); got != tt.want {
			t.Errorf("FeedbackType(%d).String() = %q, want %q", tt.ft, got, tt.want)
		}
	}
}

func TestFeedbackType_MarshalJSON(t *testing.T) {
	ft := FeedbackHelpful
	data, err := json.Marshal(ft)
	if err != nil {
		t.Fatalf("MarshalJSON 失败: %v", err)
	}
	if string(data) != `"helpful"` {
		t.Errorf("MarshalJSON = %s, want \"helpful\"", data)
	}
}

func TestFeedbackType_UnmarshalJSON(t *testing.T) {
	var ft FeedbackType
	err := json.Unmarshal([]byte(`"harmful"`), &ft)
	if err != nil {
		t.Fatalf("UnmarshalJSON 失败: %v", err)
	}
	if ft != FeedbackHarmful {
		t.Errorf("UnmarshalJSON = %d, want %d", ft, FeedbackHarmful)
	}
}

func TestFeedbackType_UnmarshalJSON_无效值(t *testing.T) {
	var ft FeedbackType
	err := json.Unmarshal([]byte(`"invalid"`), &ft)
	if err == nil {
		t.Error("期望返回 error，得到 nil")
	}
}

func TestTrajectory_IsSuccess(t *testing.T) {
	traj := Trajectory{Feedback: FeedbackHelpful}
	if !traj.IsSuccess() {
		t.Error("IsSuccess() 应返回 true")
	}
	if traj.IsFailure() {
		t.Error("IsFailure() 应返回 false")
	}
}

func TestTrajectory_IsFailure(t *testing.T) {
	traj := Trajectory{Feedback: FeedbackHarmful}
	if traj.IsSuccess() {
		t.Error("IsSuccess() 应返回 false")
	}
	if !traj.IsFailure() {
		t.Error("IsFailure() 应返回 true")
	}
}

func TestTrajectory_IsNeutral(t *testing.T) {
	traj := Trajectory{Feedback: FeedbackNeutral}
	if traj.IsSuccess() {
		t.Error("中性反馈 IsSuccess() 应返回 false")
	}
	if traj.IsFailure() {
		t.Error("中性反馈 IsFailure() 应返回 false")
	}
}

func TestTrajectory_ToDict(t *testing.T) {
	traj := Trajectory{
		Query:    "test query",
		Response: "test response",
		Feedback: FeedbackHelpful,
		Context:  map[string]any{"key": "value"},
	}
	dict := traj.ToDict()
	if dict["query"] != "test query" {
		t.Errorf("ToDict query = %v, want test query", dict["query"])
	}
	if dict["feedback"] != "helpful" {
		t.Errorf("ToDict feedback = %v, want helpful", dict["feedback"])
	}
}

func TestTrajectory_String(t *testing.T) {
	short := Trajectory{Query: "short", Feedback: FeedbackHelpful}
	if short.String() != "Trajectory(query='short', feedback=helpful)" {
		t.Errorf("String() = %q", short.String())
	}

	long := Trajectory{Query: "this is a very long query that exceeds fifty characters limit for display", Feedback: FeedbackHarmful}
	s := long.String()
	if len(s) > 80 {
		t.Errorf("String() 过长: %q", s)
	}
	// 验证截断包含 "..."
	found := false
	for i := 0; i <= len(s)-3; i++ {
		if s[i:i+3] == "..." {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("String() 缺少截断标记: %q", s)
	}
}

func TestTrajectory_JSON往返(t *testing.T) {
	original := Trajectory{
		Query:    "如何实现缓存",
		Response: "使用 lru_cache",
		Feedback: FeedbackHelpful,
		Context:  map[string]any{"env": "python"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored Trajectory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.Feedback != original.Feedback {
		t.Errorf("Feedback = %d, want %d", restored.Feedback, original.Feedback)
	}
}

func TestTrajectoryBatch_GetSuccessTrajectories(t *testing.T) {
	batch := TrajectoryBatch{
		Trajectories: []Trajectory{
			{Query: "q1", Feedback: FeedbackHelpful},
			{Query: "q2", Feedback: FeedbackHarmful},
			{Query: "q3", Feedback: FeedbackHelpful},
			{Query: "q4", Feedback: FeedbackNeutral},
		},
		UserID: "user1",
	}
	success := batch.GetSuccessTrajectories()
	if len(success) != 2 {
		t.Errorf("GetSuccessTrajectories 返回 %d 条, want 2", len(success))
	}
	failure := batch.GetFailureTrajectories()
	if len(failure) != 1 {
		t.Errorf("GetFailureTrajectories 返回 %d 条, want 1", len(failure))
	}
}

func TestTrajectoryBatch_CountByFeedback(t *testing.T) {
	batch := TrajectoryBatch{
		Trajectories: []Trajectory{
			{Feedback: FeedbackHelpful},
			{Feedback: FeedbackHelpful},
			{Feedback: FeedbackHarmful},
			{Feedback: FeedbackNeutral},
		},
		UserID: "user1",
	}
	counts := batch.CountByFeedback()
	if counts[FeedbackHelpful] != 2 {
		t.Errorf("Helpful = %d, want 2", counts[FeedbackHelpful])
	}
	if counts[FeedbackHarmful] != 1 {
		t.Errorf("Harmful = %d, want 1", counts[FeedbackHarmful])
	}
	if counts[FeedbackNeutral] != 1 {
		t.Errorf("Neutral = %d, want 1", counts[FeedbackNeutral])
	}
}

func TestTrajectoryBatch_String(t *testing.T) {
	batch := TrajectoryBatch{
		Trajectories: []Trajectory{
			{Feedback: FeedbackHelpful},
			{Feedback: FeedbackHarmful},
		},
		UserID: "user1",
	}
	s := batch.String()
	if s == "" {
		t.Error("String() 不应为空")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/schema/... -run TestFeedback -v 2>&1 | head -20`
Expected: 编译失败，类型未定义

- [ ] **Step 3: 实现 trajectory.go**

```go
package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 枚举 ────────────────────────────

// FeedbackType 轨迹结果反馈类型。
// 对齐 Python FeedbackType(str, Enum)。
type FeedbackType int

const (
	// FeedbackHelpful 有帮助
	FeedbackHelpful FeedbackType = iota
	// FeedbackHarmful 有害
	FeedbackHarmful
	// FeedbackNeutral 中性
	FeedbackNeutral
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// feedbackHelpfulStr JSON 序列化值
	feedbackHelpfulStr = "helpful"
	// feedbackHarmfulStr JSON 序列化值
	feedbackHarmfulStr = "harmful"
	// feedbackNeutralStr JSON 序列化值
	feedbackNeutralStr = "neutral"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Trajectory 表示一次任务执行的轨迹。
// 对齐 Python Trajectory(BaseModel)。
//
// 轨迹捕获：
//   - 执行的查询/任务
//   - 生成的响应/输出
//   - 结果反馈（有帮助/有害/中性）
type Trajectory struct {
	// Query 执行的查询或任务
	Query string `json:"query"`
	// Response 生成的响应或输出
	Response string `json:"response"`
	// Feedback 结果反馈
	Feedback FeedbackType `json:"feedback"`
	// Context 执行的附加上下文
	Context map[string]any `json:"context"`
}

// TrajectoryBatch 轨迹批量数据。
// 对齐 Python TrajectoryBatch(BaseModel)。
type TrajectoryBatch struct {
	// Trajectories 轨迹列表
	Trajectories []Trajectory `json:"trajectories"`
	// UserID 用户标识
	UserID string `json:"user_id"`
	// Metadata 批次元数据
	Metadata map[string]any `json:"metadata"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// feedbackFromString 从字符串解析 FeedbackType。
func feedbackFromString(s string) (FeedbackType, error) {
	switch s {
	case feedbackHelpfulStr:
		return FeedbackHelpful, nil
	case feedbackHarmfulStr:
		return FeedbackHarmful, nil
	case feedbackNeutralStr:
		return FeedbackNeutral, nil
	default:
		return FeedbackNeutral, fmt.Errorf("未知的 FeedbackType: %q", s)
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// String 实现 Stringer 接口。
// 对齐 Python FeedbackType.__str__()，输出 "helpful"/"harmful"/"neutral"。
func (ft FeedbackType) String() string {
	switch ft {
	case FeedbackHelpful:
		return feedbackHelpfulStr
	case FeedbackHarmful:
		return feedbackHarmfulStr
	case FeedbackNeutral:
		return feedbackNeutralStr
	default:
		return fmt.Sprintf("FeedbackType(%d)", ft)
	}
}

// MarshalJSON 实现 json.Marshaler 接口。
// 对齐 Python use_enum_values=True，序列化为字符串。
func (ft FeedbackType) MarshalJSON() ([]byte, error) {
	return json.Marshal(ft.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
func (ft *FeedbackType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := feedbackFromString(s)
	if err != nil {
		return err
	}
	*ft = parsed
	return nil
}

// IsSuccess 检查轨迹是否成功。
// 对齐 Python Trajectory.is_success()，Feedback == helpful 时返回 true。
func (t *Trajectory) IsSuccess() bool {
	return t.Feedback == FeedbackHelpful
}

// IsFailure 检查轨迹是否失败。
// 对齐 Python Trajectory.is_failure()，Feedback == harmful 时返回 true。
func (t *Trajectory) IsFailure() bool {
	return t.Feedback == FeedbackHarmful
}

// ToDict 转换为字典。
// 对齐 Python Trajectory.to_dict()，即 Pydantic model_dump()。
func (t *Trajectory) ToDict() map[string]any {
	return map[string]any{
		"query":    t.Query,
		"response": t.Response,
		"feedback": t.Feedback.String(),
		"context":  t.Context,
	}
}

// String 实现 Stringer 接口。
// 对齐 Python Trajectory.__repr__()，query 超过 50 字符时截断。
func (t *Trajectory) String() string {
	queryPreview := t.Query
	if len(queryPreview) > 50 {
		queryPreview = queryPreview[:50] + "..."
	}
	return fmt.Sprintf("Trajectory(query='%s', feedback=%s)", queryPreview, t.Feedback)
}

// GetSuccessTrajectories 获取成功的轨迹列表。
// 对齐 Python TrajectoryBatch.get_success_trajectories()。
func (b *TrajectoryBatch) GetSuccessTrajectories() []Trajectory {
	var result []Trajectory
	for i := range b.Trajectories {
		if b.Trajectories[i].IsSuccess() {
			result = append(result, b.Trajectories[i])
		}
	}
	return result
}

// GetFailureTrajectories 获取失败的轨迹列表。
// 对齐 Python TrajectoryBatch.get_failure_trajectories()。
func (b *TrajectoryBatch) GetFailureTrajectories() []Trajectory {
	var result []Trajectory
	for i := range b.Trajectories {
		if b.Trajectories[i].IsFailure() {
			result = append(result, b.Trajectories[i])
		}
	}
	return result
}

// CountByFeedback 按反馈类型统计轨迹数量。
// 对齐 Python TrajectoryBatch.count_by_feedback()。
func (b *TrajectoryBatch) CountByFeedback() map[FeedbackType]int {
	counts := map[FeedbackType]int{
		FeedbackHelpful: 0,
		FeedbackHarmful: 0,
		FeedbackNeutral: 0,
	}
	for i := range b.Trajectories {
		counts[b.Trajectories[i].Feedback]++
	}
	return counts
}

// String 实现 Stringer 接口。
// 对齐 Python TrajectoryBatch.__repr__()。
func (b *TrajectoryBatch) String() string {
	counts := b.CountByFeedback()
	return fmt.Sprintf(
		"TrajectoryBatch(user=%s, total=%d, helpful=%d, harmful=%d)",
		b.UserID,
		len(b.Trajectories),
		counts[FeedbackHelpful],
		counts[FeedbackHarmful],
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// （无额外非导出函数，所有辅助逻辑已内联到导出方法中）
```

注意：Trajectory 和 TrajectoryBatch 的 JSON tag 需要对齐 Python Pydantic 的字段名。Python `context` 字段在 Go 中也是 `context`，但 Go 的 `context` 是标准库包名。字段名用 `Context` 不会与包冲突（大写），JSON tag 用 `json:"context"` 对齐 Python。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/schema/... -v -count=1`
Expected: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/schema/trajectory.go internal/agentcore/context_evolver/schema/trajectory_test.go
git commit -m "feat(context_evolver): 实现 trajectory.go — FeedbackType + Trajectory + TrajectoryBatch"
```

---

### Task 3: 实现 memory.go — BaseMemory + MemoryInterface + 各 Memory 类型

**Files:**
- Create: `internal/agentcore/context_evolver/schema/memory.go`
- Create: `internal/agentcore/context_evolver/schema/memory_test.go`

- [ ] **Step 1: 编写 memory_test.go 失败测试**

```go
package schema

import (
	"encoding/json"
	"testing"
	"time"

	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// --- BaseMemory ---

func TestBaseMemory_GetWorkspaceID(t *testing.T) {
	bm := BaseMemory{WorkspaceID: "user1"}
	if bm.GetWorkspaceID() != "user1" {
		t.Errorf("GetWorkspaceID() = %q, want %q", bm.GetWorkspaceID(), "user1")
	}
}

func TestBaseMemory_默认值(t *testing.T) {
	bm := BaseMemory{}
	if bm.WorkspaceID != "" {
		t.Errorf("默认 WorkspaceID = %q, want 空", bm.WorkspaceID)
	}
}

// --- ACEMemory ---

func TestACEMemory_ToVectorNode(t *testing.T) {
	mem := ACEMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws1"},
		ID:         "mem_001",
		Section:    "python",
		Content:    "Use lru_cache for memoization",
		Helpful:    3,
		Harmful:    1,
		Neutral:    2,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	node := mem.ToVectorNode()
	if node.Metadata["type"] != "ace_memory" {
		t.Errorf("type = %v, want ace_memory", node.Metadata["type"])
	}
	if node.Content != "Use lru_cache for memoization" {
		t.Errorf("embedding content = %q, want 原始 content", node.Content)
	}
	if node.Metadata["section"] != "python" {
		t.Errorf("section = %v, want python", node.Metadata["section"])
	}
}

func TestACEMemory_FromVectorNode_往返(t *testing.T) {
	original := ACEMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws1"},
		ID:         "mem_002",
		Section:    "go",
		Content:    "Use sync.Pool",
		Helpful:    5,
		Harmful:    0,
		Neutral:    1,
		CreatedAt:  time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 3, 16, 10, 30, 0, 0, time.UTC),
	}
	node := original.ToVectorNode()
	restored := NewACEMemoryFromVectorNode(node)
	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
	if restored.Section != original.Section {
		t.Errorf("Section = %q, want %q", restored.Section, original.Section)
	}
	if restored.Content != original.Content {
		t.Errorf("Content = %q, want %q", restored.Content, original.Content)
	}
	if restored.Helpful != original.Helpful {
		t.Errorf("Helpful = %d, want %d", restored.Helpful, original.Helpful)
	}
	if restored.WorkspaceID != original.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", restored.WorkspaceID, original.WorkspaceID)
	}
}

func TestACEMemory_GetWorkspaceID(t *testing.T) {
	mem := ACEMemory{BaseMemory: BaseMemory{WorkspaceID: "ws_ace"}}
	if mem.GetWorkspaceID() != "ws_ace" {
		t.Errorf("GetWorkspaceID() = %q, want %q", mem.GetWorkspaceID(), "ws_ace")
	}
}

// --- ReasoningBankMemory ---

func TestReasoningBankMemory_ToVectorNode(t *testing.T) {
	label := true
	mem := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "How to implement caching?",
		Memory: []ReasoningBankMemoryItem{
			{Title: "Python Caching", Description: "Memoization technique", Content: "Use functools.lru_cache"},
		},
		Label: &label,
	}
	node := mem.ToVectorNode()
	if node.Metadata["type"] != "reasoning_bank_memory" {
		t.Errorf("type = %v, want reasoning_bank_memory", node.Metadata["type"])
	}
	if node.Content != "How to implement caching?" {
		t.Errorf("embedding content = %q, want query", node.Content)
	}
	if node.Metadata["query"] != "How to implement caching?" {
		t.Errorf("query = %v, want 原始 query", node.Metadata["query"])
	}
}

func TestReasoningBankMemory_FromVectorNode_往返(t *testing.T) {
	label := false
	original := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "error handling",
		Memory: []ReasoningBankMemoryItem{
			{Title: "Error Handling", Description: "Best practices", Content: "Use specific exception types"},
		},
		Label: &label,
	}
	node := original.ToVectorNode()
	restored := NewReasoningBankMemoryFromVectorNode(node)
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if len(restored.Memory) != 1 {
		t.Fatalf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
	if restored.Memory[0].Title != "Error Handling" {
		t.Errorf("Memory[0].Title = %q, want %q", restored.Memory[0].Title, "Error Handling")
	}
	if restored.Label == nil || *restored.Label != false {
		t.Errorf("Label = %v, want false", restored.Label)
	}
}

func TestReasoningBankMemory_无顶层Content(t *testing.T) {
	mem := ReasoningBankMemory{
		Query: "test",
		Memory: []ReasoningBankMemoryItem{
			{Title: "T", Description: "D", Content: "C"},
		},
	}
	// RBMemory 没有顶层 content 字段，content 嵌套在 Memory 列表项中
	// 此测试验证结构体设计正确
	if mem.Query != "test" {
		t.Error("Query 字段应该可用")
	}
}

// --- ReMeMemory ---

func TestReMeMemory_ToVectorNode(t *testing.T) {
	mem := ReMeMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws3"},
		WhenToUse:  "When implementing caching in Python",
		Content:    "Use functools.lru_cache decorator",
		Score:      0.8,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Metadata: ReMeMemoryMetadata{
			Tags:       []string{"python", "caching"},
			StepType:   "implementation",
			ToolsUsed:  []string{"functools"},
			Confidence: 0.95,
			Freq:       5,
			Utility:    4,
		},
	}
	node := mem.ToVectorNode()
	if node.Metadata["type"] != "reme_memory" {
		t.Errorf("type = %v, want reme_memory", node.Metadata["type"])
	}
	if node.Content != "When implementing caching in Python" {
		t.Errorf("embedding content = %q, want when_to_use", node.Content)
	}
	meta, ok := node.Metadata["metadata"].(map[string]any)
	if !ok {
		t.Fatal("metadata 字段不是 map[string]any")
	}
	if meta["step_type"] != "implementation" {
		t.Errorf("step_type = %v, want implementation", meta["step_type"])
	}
}

func TestReMeMemory_FromVectorNode_往返(t *testing.T) {
	original := ReMeMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws3"},
		WhenToUse:  "When handling errors",
		Content:    "Use specific exception types",
		Score:      0.92,
		CreatedAt:  time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC),
		Metadata: ReMeMemoryMetadata{
			Tags:       []string{"python"},
			StepType:   "error_handling",
			ToolsUsed:  []string{"try"},
			Confidence: 0.9,
			Freq:       3,
			Utility:    5,
		},
	}
	node := original.ToVectorNode()
	restored := NewReMeMemoryFromVectorNode(node)
	if restored.WhenToUse != original.WhenToUse {
		t.Errorf("WhenToUse = %q, want %q", restored.WhenToUse, original.WhenToUse)
	}
	if restored.Content != original.Content {
		t.Errorf("Content = %q, want %q", restored.Content, original.Content)
	}
	if restored.Score != original.Score {
		t.Errorf("Score = %f, want %f", restored.Score, original.Score)
	}
	if restored.Metadata.StepType != original.Metadata.StepType {
		t.Errorf("Metadata.StepType = %q, want %q", restored.Metadata.StepType, original.Metadata.StepType)
	}
	if len(restored.Metadata.Tags) != 1 || restored.Metadata.Tags[0] != "python" {
		t.Errorf("Metadata.Tags = %v, want [python]", restored.Metadata.Tags)
	}
}

// --- VectorNodeToMemory 工厂 ---

func TestVectorNodeToMemory_ACE(t *testing.T) {
	node := coreschema.NewVectorNode("ace_ws1_001", "content", nil, map[string]any{
		"type":         "ace_memory",
		"id":           "mem_001",
		"section":      "python",
		"content":      "Use lru_cache",
		"helpful":      1,
		"harmful":      0,
		"neutral":      0,
		"workspace_id": "ws1",
	})
	mem, err := VectorNodeToMemory(node)
	if err != nil {
		t.Fatalf("VectorNodeToMemory 失败: %v", err)
	}
	ace, ok := mem.(*ACEMemory)
	if !ok {
		t.Fatal("期望 *ACEMemory 类型")
	}
	if ace.ID != "mem_001" {
		t.Errorf("ID = %q, want mem_001", ace.ID)
	}
}

func TestVectorNodeToMemory_ReasoningBank(t *testing.T) {
	node := coreschema.NewVectorNode("rb_ws2_001", "query text", nil, map[string]any{
		"type":         "reasoning_bank_memory",
		"query":        "test query",
		"memory":       []any{map[string]any{"title": "T", "description": "D", "content": "C"}},
		"workspace_id": "ws2",
	})
	mem, err := VectorNodeToMemory(node)
	if err != nil {
		t.Fatalf("VectorNodeToMemory 失败: %v", err)
	}
	rb, ok := mem.(*ReasoningBankMemory)
	if !ok {
		t.Fatal("期望 *ReasoningBankMemory 类型")
	}
	if rb.Query != "test query" {
		t.Errorf("Query = %q, want test query", rb.Query)
	}
}

func TestVectorNodeToMemory_ReMe(t *testing.T) {
	node := coreschema.NewVectorNode("reme_ws3_001", "when to use", nil, map[string]any{
		"type":         "reme_memory",
		"when_to_use":  "test when",
		"content":      "test content",
		"workspace_id": "ws3",
		"metadata": map[string]any{
			"tags":        []any{"tag1"},
			"step_type":   "test",
			"tools_used":  []any{"tool1"},
			"confidence":  0.9,
			"freq":        float64(3),
			"utility":     4.0,
		},
	})
	mem, err := VectorNodeToMemory(node)
	if err != nil {
		t.Fatalf("VectorNodeToMemory 失败: %v", err)
	}
	reme, ok := mem.(*ReMeMemory)
	if !ok {
		t.Fatal("期望 *ReMeMemory 类型")
	}
	if reme.WhenToUse != "test when" {
		t.Errorf("WhenToUse = %q, want test when", reme.WhenToUse)
	}
}

func TestVectorNodeToMemory_未知类型(t *testing.T) {
	node := coreschema.NewVectorNode("unknown_001", "content", nil, map[string]any{
		"type": "unknown_memory",
	})
	_, err := VectorNodeToMemory(node)
	if err == nil {
		t.Error("期望返回 error，得到 nil")
	}
}

func TestVectorNodeToMemory_无type字段(t *testing.T) {
	node := coreschema.NewVectorNode("no_type_001", "content", nil, map[string]any{})
	_, err := VectorNodeToMemory(node)
	if err == nil {
		t.Error("期望返回 error，得到 nil")
	}
}

// --- MemoryInterface 实现 ---

func TestACEMemory_实现MemoryInterface(t *testing.T) {
	var _ MemoryInterface = (*ACEMemory)(nil)
}

func TestReasoningBankMemory_实现MemoryInterface(t *testing.T) {
	var _ MemoryInterface = (*ReasoningBankMemory)(nil)
}

func TestReMeMemory_实现MemoryInterface(t *testing.T) {
	var _ MemoryInterface = (*ReMeMemory)(nil)
}

// --- JSON 序列化 ---

func TestACEMemory_JSON往返(t *testing.T) {
	original := ACEMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws1"},
		ID:         "mem_j",
		Section:    "test",
		Content:    "test content",
		Helpful:    1,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACEMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
	if restored.WorkspaceID != original.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", restored.WorkspaceID, original.WorkspaceID)
	}
}

func TestReasoningBankMemory_JSON往返(t *testing.T) {
	label := true
	original := ReasoningBankMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws2"},
		Query:      "test query",
		Memory: []ReasoningBankMemoryItem{
			{Title: "T1", Description: "D1", Content: "C1"},
		},
		Label: &label,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
}

func TestReMeMemory_JSON往返(t *testing.T) {
	original := ReMeMemory{
		BaseMemory: BaseMemory{WorkspaceID: "ws3"},
		WhenToUse:  "test when",
		Content:    "test content",
		Score:      0.75,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Metadata: ReMeMemoryMetadata{
			Tags:      []string{"t1"},
			StepType:  "s1",
			ToolsUsed: []string{"tool1"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.WhenToUse != original.WhenToUse {
		t.Errorf("WhenToUse = %q, want %q", restored.WhenToUse, original.WhenToUse)
	}
	if restored.Score != original.Score {
		t.Errorf("Score = %f, want %f", restored.Score, original.Score)
	}
}

func TestReasoningBankMemoryItem_JSON往返(t *testing.T) {
	original := ReasoningBankMemoryItem{
		Title:       "Test Title",
		Description: "Test Desc",
		Content:     "Test Content",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankMemoryItem
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Title != original.Title {
		t.Errorf("Title = %q, want %q", restored.Title, original.Title)
	}
}

func TestReMeMemoryMetadata_JSON往返(t *testing.T) {
	original := ReMeMemoryMetadata{
		Tags:       []string{"a", "b"},
		StepType:   "impl",
		ToolsUsed:  []string{"t1", "t2"},
		Confidence: 0.85,
		Freq:       10,
		Utility:    3.5,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeMemoryMetadata
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.StepType != original.StepType {
		t.Errorf("StepType = %q, want %q", restored.StepType, original.StepType)
	}
	if restored.Freq != original.Freq {
		t.Errorf("Freq = %d, want %d", restored.Freq, original.Freq)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/schema/... -run TestBaseMemory -v 2>&1 | head -20`
Expected: 编译失败，类型未定义

- [ ] **Step 3: 实现 memory.go**

```go
package schema

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"

	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// MemoryInterface 记忆类型的通用接口。
// 所有 Memory 类型（ACEMemory/ReasoningBankMemory/ReMeMemory）均实现此接口。
//
// 注意：FromVectorNode 不在接口中，因为它是构造函数语义（Python classmethod），
// Go 中使用包级函数 NewXxxFromVectorNode 代替。
type MemoryInterface interface {
	// GetWorkspaceID 返回工作空间标识
	GetWorkspaceID() string
	// ToVectorNode 转换为 VectorNode 用于向量存储
	ToVectorNode() *coreschema.VectorNode
}

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMemory 记忆类型的公共基类。
// 对齐 Python io_schema.BaseMemory(BaseModel)，各 Memory 类型嵌入此结构体。
//
// Python 中 BaseMemory 只含 workspace_id 字段。
type BaseMemory struct {
	// WorkspaceID 工作空间/用户标识
	WorkspaceID string `json:"workspace_id"`
}

// ACEMemory ACE 算法的记忆类型。
// 对齐 Python ACEMemory(BaseModel)。
//
// ACE 记忆以 Playbook 条目形式组织，每条记忆属于一个 section，
// 包含内容文本和 helpful/harmful/neutral 反馈计数。
type ACEMemory struct {
	BaseMemory       // 嵌入基类
	// ID 记忆标识
	ID string `json:"id"`
	// Section 记忆分类
	Section string `json:"section"`
	// Content 记忆内容
	Content string `json:"content"`
	// Helpful 有帮助计数
	Helpful int `json:"helpful"`
	// Harmful 有害计数
	Harmful int `json:"harmful"`
	// Neutral 中性计数
	Neutral int `json:"neutral"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// ReasoningBankMemoryItem ReasoningBank 记忆条目。
// 对齐 Python ReasoningBankMemoryItem(BaseModel)。
//
// 非独立 Memory 类型，是 ReasoningBankMemory.Memory 列表中的元素。
type ReasoningBankMemoryItem struct {
	// Title 核心策略标识
	Title string `json:"title"`
	// Description 一句话摘要
	Description string `json:"description"`
	// Content 详细内容
	Content string `json:"content"`
}

// ReasoningBankMemory ReasoningBank 算法的记忆类型。
// 对齐 Python ReasoningBankMemory(BaseModel)。
//
// RB 记忆以 query 为索引，挂载多个 MemoryItem。
// 注意：没有顶层 content 字段，content 嵌套在 Memory 列表项中。
type ReasoningBankMemory struct {
	BaseMemory // 嵌入基类
	// Query 用作 embedding 索引的查询
	Query string `json:"query"`
	// Memory 记忆条目列表
	Memory []ReasoningBankMemoryItem `json:"memory"`
	// Label 记忆标签
	Label *bool `json:"label,omitempty"`
}

// ReMeMemoryMetadata ReMe 记忆的元数据。
// 对齐 Python ReMeMemoryMetadata(BaseModel)。
type ReMeMemoryMetadata struct {
	// Tags 记忆标签
	Tags []string `json:"tags"`
	// StepType 步骤类型
	StepType string `json:"step_type"`
	// ToolsUsed 使用的工具
	ToolsUsed []string `json:"tools_used"`
	// Confidence 置信度
	Confidence float64 `json:"confidence"`
	// Freq 使用频率
	Freq int `json:"freq"`
	// Utility 效用分数
	Utility float64 `json:"utility"`
}

// ReMeMemory ReMe 算法的记忆类型。
// 对齐 Python ReMeMemory(BaseModel)。
//
// ReMe 记忆包含使用条件（when_to_use）、内容、分数和结构化元数据。
type ReMeMemory struct {
	BaseMemory // 嵌入基类
	// WhenToUse 使用条件
	WhenToUse string `json:"when_to_use"`
	// Content 记忆内容
	Content string `json:"content"`
	// Score 记忆分数 (0-1)
	Score float64 `json:"score"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
	// Metadata 元数据
	Metadata ReMeMemoryMetadata `json:"metadata"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewACEMemoryFromVectorNode 从 VectorNode 反序列化 ACEMemory。
// 对齐 Python ACEMemory.from_vector_node(node)。
func NewACEMemoryFromVectorNode(node *coreschema.VectorNode) *ACEMemory {
	metadata := node.Metadata
	createdAt := parseTimeField(metadata, "created_at")
	updatedAt := parseTimeField(metadata, "updated_at")

	return &ACEMemory{
		BaseMemory: BaseMemory{
			WorkspaceID: getStringField(metadata, "workspace_id", "default"),
		},
		ID:        getStringField(metadata, "id", ""),
		Section:   getStringField(metadata, "section", ""),
		Content:   getStringField(metadata, "content", ""),
		Helpful:   getIntField(metadata, "helpful"),
		Harmful:   getIntField(metadata, "harmful"),
		Neutral:   getIntField(metadata, "neutral"),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

// NewReasoningBankMemoryFromVectorNode 从 VectorNode 反序列化 ReasoningBankMemory。
// 对齐 Python ReasoningBankMemory.from_vector_node(node)。
func NewReasoningBankMemoryFromVectorNode(node *coreschema.VectorNode) *ReasoningBankMemory {
	metadata := node.Metadata

	// 解析 memory 列表：可能为 []any（JSON 反序列化后）或 []ReasoningBankMemoryItem
	var memoryItems []ReasoningBankMemoryItem
	if memRaw, ok := metadata["memory"]; ok && memRaw != nil {
		memoryItems = parseMemoryItems(memRaw)
	}

	var label *bool
	if l, ok := metadata["label"]; ok && l != nil {
		if lb, ok := l.(bool); ok {
			label = &lb
		}
	}

	return &ReasoningBankMemory{
		BaseMemory: BaseMemory{
			WorkspaceID: getStringField(metadata, "workspace_id", "default"),
		},
		Query:  getStringField(metadata, "query", ""),
		Memory: memoryItems,
		Label:  label,
	}
}

// NewReMeMemoryFromVectorNode 从 VectorNode 反序列化 ReMeMemory。
// 对齐 Python ReMeMemory.from_vector_node(node)。
func NewReMeMemoryFromVectorNode(node *coreschema.VectorNode) *ReMeMemory {
	metadata := node.Metadata
	createdAt := parseTimeField(metadata, "created_at")
	updatedAt := parseTimeField(metadata, "updated_at")

	// 解析嵌套的 metadata
	var memMetadata ReMeMemoryMetadata
	if metaRaw, ok := metadata["metadata"]; ok && metaRaw != nil {
		if metaMap, ok := metaRaw.(map[string]any); ok {
			memMetadata = parseReMeMemoryMetadata(metaMap)
		}
	}

	return &ReMeMemory{
		BaseMemory: BaseMemory{
			WorkspaceID: getStringField(metadata, "workspace_id", ""),
		},
		WhenToUse: getStringField(metadata, "when_to_use", ""),
		Content:   getStringField(metadata, "content", ""),
		Score:     getFloatField(metadata, "score"),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Metadata:  memMetadata,
	}
}

// VectorNodeToMemory 按 metadata.type 分发，将 VectorNode 转换为对应的 Memory 类型。
// 对齐 Python vector_node_to_memory(node) 工厂函数。
func VectorNodeToMemory(node *coreschema.VectorNode) (MemoryInterface, error) {
	typeVal, ok := node.Metadata["type"]
	if !ok {
		return nil, fmt.Errorf("VectorNodeToMemory: 缺少 metadata.type 字段")
	}
	typeStr, ok := typeVal.(string)
	if !ok {
		return nil, fmt.Errorf("VectorNodeToMemory: metadata.type 不是字符串: %T", typeVal)
	}

	switch typeStr {
	case "ace_memory":
		return NewACEMemoryFromVectorNode(node), nil
	case "reasoning_bank_memory":
		return NewReasoningBankMemoryFromVectorNode(node), nil
	case "reme_memory":
		return NewReMeMemoryFromVectorNode(node), nil
	default:
		return nil, fmt.Errorf("VectorNodeToMemory: 未知的记忆类型: %q", typeStr)
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// GetWorkspaceID 实现 MemoryInterface 接口。
func (b *BaseMemory) GetWorkspaceID() string {
	return b.WorkspaceID
}

// ToVectorNode 实现 MemoryInterface 接口。
// 对齐 Python ACEMemory.to_vector_node()。
// metadata.type = "ace_memory"，embedding content = Content。
func (m *ACEMemory) ToVectorNode() *coreschema.VectorNode {
	nodeID := fmt.Sprintf("ace_%s_%s", m.WorkspaceID, m.ID)
	metadata := map[string]any{
		"type":         "ace_memory",
		"id":           m.ID,
		"section":      m.Section,
		"content":      m.Content,
		"helpful":      m.Helpful,
		"harmful":      m.Harmful,
		"neutral":      m.Neutral,
		"created_at":   m.CreatedAt.Format(time.RFC3339),
		"updated_at":   m.UpdatedAt.Format(time.RFC3339),
		"workspace_id": m.WorkspaceID,
	}
	return coreschema.NewVectorNode(nodeID, m.Content, nil, metadata)
}

// ToVectorNode 实现 MemoryInterface 接口。
// 对齐 Python ReasoningBankMemory.to_vector_node()。
// metadata.type = "reasoning_bank_memory"，embedding content = Query。
func (m *ReasoningBankMemory) ToVectorNode() *coreschema.VectorNode {
	combined := m.Query
	if len(m.Memory) > 0 {
		combined = fmt.Sprintf("%s|%s", m.Query, m.Memory[0].Title)
	}
	contentHash := md5Hash(combined)
	nodeID := fmt.Sprintf("reasoning_bank_%s_%s", m.WorkspaceID, contentHash)

	// 将 MemoryItem 列表转为 []map[string]any 以便 JSON 序列化
	memoryData := make([]any, len(m.Memory))
	for i, item := range m.Memory {
		memoryData[i] = map[string]any{
			"title":       item.Title,
			"description": item.Description,
			"content":     item.Content,
		}
	}

	metadata := map[string]any{
		"type":         "reasoning_bank_memory",
		"query":        m.Query,
		"memory":       memoryData,
		"label":        m.Label,
		"workspace_id": m.WorkspaceID,
	}
	return coreschema.NewVectorNode(nodeID, m.Query, nil, metadata)
}

// ToVectorNode 实现 MemoryInterface 接口。
// 对齐 Python ReMeMemory.to_vector_node()。
// metadata.type = "reme_memory"，embedding content = WhenToUse。
func (m *ReMeMemory) ToVectorNode() *coreschema.VectorNode {
	contentHash := md5Hash(m.WhenToUse)[:12]
	nodeID := fmt.Sprintf("reme_%s_%s", m.WorkspaceID, contentHash)

	metadata := map[string]any{
		"type":         "reme_memory",
		"when_to_use":  m.WhenToUse,
		"content":      m.Content,
		"score":        m.Score,
		"created_at":   m.CreatedAt.Format(time.RFC3339),
		"updated_at":   m.UpdatedAt.Format(time.RFC3339),
		"workspace_id": m.WorkspaceID,
		"metadata": map[string]any{
			"tags":        m.Metadata.Tags,
			"step_type":   m.Metadata.StepType,
			"tools_used":  m.Metadata.ToolsUsed,
			"confidence":  m.Metadata.Confidence,
			"freq":        m.Metadata.Freq,
			"utility":     m.Metadata.Utility,
		},
	}
	return coreschema.NewVectorNode(nodeID, m.WhenToUse, nil, metadata)
}

// String 实现 Stringer 接口。
// 对齐 Python ACEMemory.__repr__()。
func (m *ACEMemory) String() string {
	return fmt.Sprintf("ACEMemory(id=%s, section=%s, helpful=%d, harmful=%d)",
		m.ID, m.Section, m.Helpful, m.Harmful)
}

// String 实现 Stringer 接口。
// 对齐 Python ReasoningBankMemory.__repr__()。
func (m *ReasoningBankMemory) String() string {
	return fmt.Sprintf("ReasoningBankMemory(query=%s, items=%d, label=%v)",
		m.Query, len(m.Memory), m.Label)
}

// String 实现 Stringer 接口。
// 对齐 Python ReMeMemory 无显式 __repr__，提供格式化输出。
func (m *ReMeMemory) String() string {
	whenPreview := m.WhenToUse
	if len(whenPreview) > 30 {
		whenPreview = whenPreview[:30] + "..."
	}
	return fmt.Sprintf("ReMeMemory(when_to_use=%s, score=%.2f)", whenPreview, m.Score)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// md5Hash 计算字符串的 MD5 哈希。
func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// getStringField 从 metadata 中安全获取字符串字段。
func getStringField(metadata map[string]any, key, defaultVal string) string {
	if v, ok := metadata[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

// getIntField 从 metadata 中安全获取整数字段。
func getIntField(metadata map[string]any, key string) int {
	if v, ok := metadata[key]; ok && v != nil {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return 0
}

// getFloatField 从 metadata 中安全获取浮点数字段。
func getFloatField(metadata map[string]any, key string) float64 {
	if v, ok := metadata[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f
			}
		}
	}
	return 0
}

// parseTimeField 从 metadata 中安全解析时间字段。
// 支持 time.Time（直接赋值）和 string（RFC3339 解析）。
func parseTimeField(metadata map[string]any, key string) time.Time {
	if v, ok := metadata[key]; ok && v != nil {
		switch t := v.(type) {
		case time.Time:
			return t
		case string:
			parsed, err := time.Parse(time.RFC3339, t)
			if err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

// parseMemoryItems 解析 ReasoningBankMemory 的 memory 字段。
// 支持 []any（JSON 反序列化后）和 []map[string]any 两种格式。
func parseMemoryItems(raw any) []ReasoningBankMemoryItem {
	var items []ReasoningBankMemoryItem

	switch memList := raw.(type) {
	case []any:
		for _, item := range memList {
			if m, ok := item.(map[string]any); ok {
				items = append(items, ReasoningBankMemoryItem{
					Title:       getStringField(m, "title", ""),
					Description: getStringField(m, "description", ""),
					Content:     getStringField(m, "content", ""),
				})
			}
		}
	case []ReasoningBankMemoryItem:
		items = memList
	case []map[string]any:
		for _, m := range memList {
			items = append(items, ReasoningBankMemoryItem{
				Title:       getStringField(m, "title", ""),
				Description: getStringField(m, "description", ""),
				Content:     getStringField(m, "content", ""),
			})
		}
	}

	return items
}

// parseReMeMemoryMetadata 从 map 解析 ReMeMemoryMetadata。
func parseReMeMemoryMetadata(m map[string]any) ReMeMemoryMetadata {
	var tags []string
	if t, ok := m["tags"]; ok && t != nil {
		if arr, ok := t.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					tags = append(tags, s)
				}
			}
		}
	}

	var toolsUsed []string
	if t, ok := m["tools_used"]; ok && t != nil {
		if arr, ok := t.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					toolsUsed = append(toolsUsed, s)
				}
			}
		}
	}

	return ReMeMemoryMetadata{
		Tags:       tags,
		StepType:   getStringField(m, "step_type", ""),
		ToolsUsed:  toolsUsed,
		Confidence: getFloatField(m, "confidence"),
		Freq:       getIntField(m, "freq"),
		Utility:    getFloatField(m, "utility"),
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/schema/... -v -count=1`
Expected: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/schema/memory.go internal/agentcore/context_evolver/schema/memory_test.go
git commit -m "feat(context_evolver): 实现 memory.go — BaseMemory + MemoryInterface + ACE/RB/ReMe Memory 类型"
```

---

### Task 4: 实现 io_schema.go — ACE/RB/ReMe Request/Response + 泛型 Response

**Files:**
- Create: `internal/agentcore/context_evolver/schema/io_schema.go`
- Create: `internal/agentcore/context_evolver/schema/io_schema_test.go`

- [ ] **Step 1: 编写 io_schema_test.go 失败测试**

```go
package schema

import (
	"encoding/json"
	"testing"
)

// --- ACE Request/Response ---

func TestACESummarizeRequest_JSON往返(t *testing.T) {
	gt := "expected output"
	fb := []string{"good", "bad"}
	original := ACESummarizeRequest{
		Matts:        "parallel",
		Query:        "How to implement caching?",
		Trajectories: []string{"Traj1", "Traj2"},
		GroundTruth:  &gt,
		Feedback:     fb,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACESummarizeRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.Matts != "parallel" {
		t.Errorf("Matts = %q, want parallel", restored.Matts)
	}
	if restored.GroundTruth == nil || *restored.GroundTruth != "expected output" {
		t.Errorf("GroundTruth = %v, want expected output", restored.GroundTruth)
	}
	if len(restored.Feedback) != 2 {
		t.Errorf("Feedback 长度 = %d, want 2", len(restored.Feedback))
	}
}

func TestACESummarizeResponse_JSON往返(t *testing.T) {
	original := ACESummarizeResponse{
		Status: "success",
		Memory: []ACEMemory{
			{ID: "mem_001", Section: "python", Content: "test"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACESummarizeResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
	if len(restored.Memory) != 1 {
		t.Fatalf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
	if restored.Memory[0].ID != "mem_001" {
		t.Errorf("Memory[0].ID = %q, want mem_001", restored.Memory[0].ID)
	}
}

func TestACERetrieveRequest_JSON往返(t *testing.T) {
	uid := "alice"
	original := ACERetrieveRequest{UserID: &uid}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACERetrieveRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.UserID == nil || *restored.UserID != "alice" {
		t.Errorf("UserID = %v, want alice", restored.UserID)
	}
}

func TestACERetrieveResponse_JSON往返(t *testing.T) {
	original := ACERetrieveResponse{
		Status:       "success",
		MemoryString: "Section: python\nContent: test",
		RetrievedMemory: []ACERetrievedMemory{
			{ID: "mem_001", Section: "python", Content: "test", Helpful: 1},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACERetrieveResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != original.MemoryString {
		t.Errorf("MemoryString = %q, want %q", restored.MemoryString, original.MemoryString)
	}
	if len(restored.RetrievedMemory) != 1 {
		t.Fatalf("RetrievedMemory 长度 = %d, want 1", len(restored.RetrievedMemory))
	}
	if restored.RetrievedMemory[0].ID != "mem_001" {
		t.Errorf("RetrievedMemory[0].ID = %q, want mem_001", restored.RetrievedMemory[0].ID)
	}
}

func TestACERetrievedMemory_JSON往返(t *testing.T) {
	original := ACERetrievedMemory{
		ID:      "mem_001",
		Section: "python",
		Content: "Use lru_cache",
		Helpful: 5,
		Harmful: 1,
		Neutral: 2,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ACERetrievedMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
}

// --- ReasoningBank Request/Response ---

func TestReasoningBankSummarizeRequest_JSON往返(t *testing.T) {
	l1, l2 := true, false
	original := ReasoningBankSummarizeRequest{
		Matts:        "parallel",
		Query:        "How to handle errors?",
		Trajectories: []string{"Traj1", "Traj2"},
		Label:        []*bool{&l1, &l2},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankSummarizeRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if len(restored.Label) != 2 {
		t.Errorf("Label 长度 = %d, want 2", len(restored.Label))
	}
}

func TestReasoningBankSummarizeResponse_JSON往返(t *testing.T) {
	original := ReasoningBankSummarizeResponse{
		Status: "success",
		Memory: []ReasoningBankMemory{
			{Query: "test query"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankSummarizeResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
	if len(restored.Memory) != 1 {
		t.Errorf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
}

func TestReasoningBankRetrieveRequest_JSON往返(t *testing.T) {
	original := ReasoningBankRetrieveRequest{
		Query: "How to implement caching?",
		TopK:  3,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankRetrieveRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.TopK != 3 {
		t.Errorf("TopK = %d, want 3", restored.TopK)
	}
}

func TestReasoningBankRetrieveResponse_JSON往返(t *testing.T) {
	original := ReasoningBankRetrieveResponse{
		Status:       "success",
		MemoryString: "Title: Python Caching\nDescription: Memoization...",
		RetrievedMemory: []ReasoningBankRetrievedMemory{
			{Title: "Python Caching", Description: "Memoization", Content: "Use lru_cache"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankRetrieveResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != original.MemoryString {
		t.Errorf("MemoryString 不匹配")
	}
}

func TestReasoningBankRetrievedMemory_JSON往返(t *testing.T) {
	original := ReasoningBankRetrievedMemory{
		Title:       "Error Handling",
		Description: "Best practices",
		Content:     "Use specific exceptions",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReasoningBankRetrievedMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Title != original.Title {
		t.Errorf("Title = %q, want %q", restored.Title, original.Title)
	}
}

// --- ReMe Request/Response ---

func TestReMeSummarizeRequest_JSON往返(t *testing.T) {
	original := ReMeSummarizeRequest{
		Matts:        "parallel",
		Trajectories: []string{"Traj1", "Traj2"},
		Score:        []float64{0.85, 0.92},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeSummarizeRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Matts != "parallel" {
		t.Errorf("Matts = %q, want parallel", restored.Matts)
	}
	if len(restored.Score) != 2 {
		t.Errorf("Score 长度 = %d, want 2", len(restored.Score))
	}
}

func TestReMeSummarizeResponse_JSON往返(t *testing.T) {
	original := ReMeSummarizeResponse{
		Status: "success",
		Memory: []ReMeMemory{
			{WhenToUse: "test when", Content: "test content", Score: 0.9},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeSummarizeResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
}

func TestReMeRetrieveRequest_JSON往返(t *testing.T) {
	original := ReMeRetrieveRequest{
		Query:          "How to implement caching?",
		TopKRetrieval:  10,
		TopKRerank:     5,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeRetrieveRequest
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.TopKRetrieval != 10 {
		t.Errorf("TopKRetrieval = %d, want 10", restored.TopKRetrieval)
	}
	if restored.TopKRerank != 5 {
		t.Errorf("TopKRerank = %d, want 5", restored.TopKRerank)
	}
}

func TestReMeRetrieveResponse_JSON往返(t *testing.T) {
	original := ReMeRetrieveResponse{
		Status:       "success",
		MemoryString: "When to use: When implementing caching...",
		RetrievedMemory: []ReMeRetrievedMemory{
			{WhenToUse: "When implementing caching", Content: "Use lru_cache"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeRetrieveResponse
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != original.MemoryString {
		t.Errorf("MemoryString 不匹配")
	}
}

func TestReMeRetrievedMemory_JSON往返(t *testing.T) {
	original := ReMeRetrievedMemory{
		WhenToUse: "When implementing caching",
		Content:   "Use functools.lru_cache",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored ReMeRetrievedMemory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.WhenToUse != original.WhenToUse {
		t.Errorf("WhenToUse = %q, want %q", restored.WhenToUse, original.WhenToUse)
	}
}

// --- 泛型 Response ---

func TestSummarizeResponse_ACE(t *testing.T) {
	resp := SummarizeResponse[ACEMemory]{
		Status: "success",
		Memory: []ACEMemory{
			{ID: "mem_001", Section: "python", Content: "test"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored SummarizeResponse[ACEMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q, want success", restored.Status)
	}
	if len(restored.Memory) != 1 {
		t.Errorf("Memory 长度 = %d, want 1", len(restored.Memory))
	}
	if restored.Memory[0].ID != "mem_001" {
		t.Errorf("Memory[0].ID = %q, want mem_001", restored.Memory[0].ID)
	}
}

func TestSummarizeResponse_ReasoningBank(t *testing.T) {
	resp := SummarizeResponse[ReasoningBankMemory]{
		Status: "success",
		Memory: []ReasoningBankMemory{
			{Query: "test query"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored SummarizeResponse[ReasoningBankMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Memory[0].Query != "test query" {
		t.Errorf("Memory[0].Query = %q, want test query", restored.Memory[0].Query)
	}
}

func TestSummarizeResponse_ReMe(t *testing.T) {
	resp := SummarizeResponse[ReMeMemory]{
		Status: "success",
		Memory: []ReMeMemory{
			{WhenToUse: "test when", Content: "test content"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored SummarizeResponse[ReMeMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Memory[0].WhenToUse != "test when" {
		t.Errorf("Memory[0].WhenToUse = %q, want test when", restored.Memory[0].WhenToUse)
	}
}

func TestRetrieveResponse_ACE(t *testing.T) {
	resp := RetrieveResponse[ACERetrievedMemory]{
		Status:       "success",
		MemoryString: "test string",
		RetrievedMemory: []ACERetrievedMemory{
			{ID: "mem_001", Content: "test"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored RetrieveResponse[ACERetrievedMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.MemoryString != "test string" {
		t.Errorf("MemoryString = %q, want test string", restored.MemoryString)
	}
	if len(restored.RetrievedMemory) != 1 {
		t.Errorf("RetrievedMemory 长度 = %d, want 1", len(restored.RetrievedMemory))
	}
}

func TestRetrieveResponse_ReasoningBank(t *testing.T) {
	resp := RetrieveResponse[ReasoningBankRetrievedMemory]{
		Status:       "success",
		MemoryString: "test string",
		RetrievedMemory: []ReasoningBankRetrievedMemory{
			{Title: "T1", Content: "C1"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored RetrieveResponse[ReasoningBankRetrievedMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.RetrievedMemory[0].Title != "T1" {
		t.Errorf("RetrievedMemory[0].Title = %q, want T1", restored.RetrievedMemory[0].Title)
	}
}

func TestRetrieveResponse_ReMe(t *testing.T) {
	resp := RetrieveResponse[ReMeRetrievedMemory]{
		Status:       "success",
		MemoryString: "test string",
		RetrievedMemory: []ReMeRetrievedMemory{
			{WhenToUse: "when test", Content: "content test"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored RetrieveResponse[ReMeRetrievedMemory]
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.RetrievedMemory[0].WhenToUse != "when test" {
		t.Errorf("RetrievedMemory[0].WhenToUse = %q, want when test", restored.RetrievedMemory[0].WhenToUse)
	}
}

// --- 默认值 ---

func TestACESummarizeRequest_默认值(t *testing.T) {
	req := ACESummarizeRequest{}
	if req.Matts != "none" {
		t.Errorf("默认 Matts = %q, want none", req.Matts)
	}
}

func TestReasoningBankSummarizeRequest_默认值(t *testing.T) {
	req := ReasoningBankSummarizeRequest{}
	if req.Matts != "none" {
		t.Errorf("默认 Matts = %q, want none", req.Matts)
	}
}

func TestReMeSummarizeRequest_默认值(t *testing.T) {
	req := ReMeSummarizeRequest{}
	if req.Matts != "none" {
		t.Errorf("默认 Matts = %q, want none", req.Matts)
	}
}

func TestReasoningBankRetrieveRequest_默认值(t *testing.T) {
	req := ReasoningBankRetrieveRequest{}
	if req.TopK != 5 {
		t.Errorf("默认 TopK = %d, want 5", req.TopK)
	}
}

func TestReMeRetrieveRequest_默认值(t *testing.T) {
	req := ReMeRetrieveRequest{}
	if req.TopKRetrieval != 10 {
		t.Errorf("默认 TopKRetrieval = %d, want 10", req.TopKRetrieval)
	}
	if req.TopKRerank != 5 {
		t.Errorf("默认 TopKRerank = %d, want 5", req.TopKRerank)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/schema/... -run TestACESummarizeRequest -v 2>&1 | head -20`
Expected: 编译失败，类型未定义

- [ ] **Step 3: 实现 io_schema.go**

```go
package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ============================================================================
// ACE 系列
// ============================================================================

// ACESummarizeRequest ACE 摘要请求。
// 对齐 Python ACESummarizeRequest(BaseModel)。
type ACESummarizeRequest struct {
	// Matts MaTTS 模式：none/parallel/sequential
	Matts string `json:"matts"`
	// Query 摘要查询
	Query string `json:"query"`
	// Trajectories 轨迹字符串列表
	Trajectories []string `json:"trajectories"`
	// GroundTruth 可选的参考答案
	GroundTruth *string `json:"ground_truth,omitempty"`
	// Feedback 可选的轨迹环境反馈
	Feedback []string `json:"feedback,omitempty"`
}

// ACESummarizeResponse ACE 摘要响应。
// 对齐 Python ACESummarizeResponse(BaseModel)。
type ACESummarizeResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表
	Memory []ACEMemory `json:"memory"`
}

// ACERetrieveRequest ACE 检索请求。
// 对齐 Python ACERetrieveRequest(BaseModel)，检索所有记忆。
type ACERetrieveRequest struct {
	// UserID 可选的用户标识
	UserID *string `json:"user_id,omitempty"`
}

// ACERetrieveResponse ACE 检索响应。
// 对齐 Python ACERetrieveResponse(BaseModel)。
type ACERetrieveResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表
	RetrievedMemory []ACERetrievedMemory `json:"retrieved_memory"`
}

// ACERetrievedMemory ACE 检索到的记忆条目。
// 对齐 Python ACERetrievedMemory(BaseModel)。
type ACERetrievedMemory struct {
	// ID 记忆标识
	ID string `json:"id"`
	// Section 记忆分类
	Section string `json:"section"`
	// Content 记忆内容
	Content string `json:"content"`
	// Helpful 有帮助计数
	Helpful int `json:"helpful"`
	// Harmful 有害计数
	Harmful int `json:"harmful"`
	// Neutral 中性计数
	Neutral int `json:"neutral"`
}

// ============================================================================
// ReasoningBank 系列
// ============================================================================

// ReasoningBankSummarizeRequest ReasoningBank 摘要请求。
// 对齐 Python ReasoningBankSummarizeRequest(BaseModel)。
type ReasoningBankSummarizeRequest struct {
	// Matts MaTTS 模式：none/parallel/sequential
	Matts string `json:"matts"`
	// Query 摘要查询
	Query string `json:"query"`
	// Trajectories 轨迹字符串列表
	Trajectories []string `json:"trajectories"`
	// Label 可选的轨迹标签列表
	Label []*bool `json:"label,omitempty"`
}

// ReasoningBankSummarizeResponse ReasoningBank 摘要响应。
// 对齐 Python ReasoningBankSummarizeResponse(BaseModel)。
type ReasoningBankSummarizeResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表
	Memory []ReasoningBankMemory `json:"memory"`
}

// ReasoningBankRetrieveRequest ReasoningBank 检索请求。
// 对齐 Python ReasoningBankRetrieveRequest(BaseModel)。
type ReasoningBankRetrieveRequest struct {
	// Query 检索查询
	Query string `json:"query"`
	// TopK 检索返回数量
	TopK int `json:"topk"`
}

// ReasoningBankRetrieveResponse ReasoningBank 检索响应。
// 对齐 Python ReasoningBankRetrieveResponse(BaseModel)。
type ReasoningBankRetrieveResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表
	RetrievedMemory []ReasoningBankRetrievedMemory `json:"retrieved_memory"`
}

// ReasoningBankRetrievedMemory ReasoningBank 检索到的记忆条目。
// 对齐 Python ReasoningBankRetrievedMemory(BaseModel)。
type ReasoningBankRetrievedMemory struct {
	// Title 记忆标题
	Title string `json:"title"`
	// Description 记忆描述
	Description string `json:"description"`
	// Content 记忆内容
	Content string `json:"content"`
}

// ============================================================================
// ReMe 系列
// ============================================================================

// ReMeSummarizeRequest ReMe 摘要请求。
// 对齐 Python ReMeSummarizeRequest(BaseModel)。
type ReMeSummarizeRequest struct {
	// Matts MaTTS 模式：none/parallel/sequential
	Matts string `json:"matts"`
	// Trajectories 轨迹字符串列表
	Trajectories []string `json:"trajectories"`
	// Score 可选的轨迹分数列表 (0-1)
	Score []float64 `json:"score,omitempty"`
}

// ReMeSummarizeResponse ReMe 摘要响应。
// 对齐 Python ReMeSummarizeResponse(BaseModel)。
type ReMeSummarizeResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表
	Memory []ReMeMemory `json:"memory"`
}

// ReMeRetrieveRequest ReMe 检索请求。
// 对齐 Python ReMeRetrieveRequest(BaseModel)。
type ReMeRetrieveRequest struct {
	// Query 检索查询
	Query string `json:"query"`
	// TopKRetrieval 初始检索数量
	TopKRetrieval int `json:"topk_retrieval"`
	// TopKRerank 重排序后保留数量
	TopKRerank int `json:"topk_rerank"`
}

// ReMeRetrieveResponse ReMe 检索响应。
// 对齐 Python ReMeRetrieveResponse(BaseModel)。
type ReMeRetrieveResponse struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表
	RetrievedMemory []ReMeRetrievedMemory `json:"retrieved_memory"`
}

// ReMeRetrievedMemory ReMe 检索到的记忆条目。
// 对齐 Python ReMeRetrievedMemory(BaseModel)。
type ReMeRetrievedMemory struct {
	// WhenToUse 使用条件
	WhenToUse string `json:"when_to_use"`
	// Content 记忆内容
	Content string `json:"content"`
}

// ============================================================================
// 泛型 Response
// ============================================================================

// SummarizeResponse 通用摘要响应，支持任意算法的 Memory 类型。
// 对齐 Python SummarizeResponse(BaseModel) 的 Union 字段，Go 用泛型实现。
type SummarizeResponse[T any] struct {
	// Status 操作状态
	Status string `json:"status"`
	// Memory 创建或更新的记忆列表（算法特定类型）
	Memory []T `json:"memory"`
}

// RetrieveResponse 通用检索响应，支持任意算法的 RetrievedMemory 类型。
// 对齐 Python RetrieveResponse(BaseModel) 的 Union 字段，Go 用泛型实现。
type RetrieveResponse[T any] struct {
	// Status 操作状态
	Status string `json:"status"`
	// MemoryString 格式化的记忆字符串
	MemoryString string `json:"memory_string"`
	// RetrievedMemory 检索到的记忆列表（算法特定类型）
	RetrievedMemory []T `json:"retrieved_memory"`
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/schema/... -v -count=1`
Expected: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/schema/io_schema.go internal/agentcore/context_evolver/schema/io_schema_test.go
git commit -m "feat(context_evolver): 实现 io_schema.go — ACE/RB/ReMe Request/Response + 泛型 Response"
```

---

### Task 5: 整体验证 + 覆盖率检查 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` (9.82 P2 状态更新)

- [ ] **Step 1: 运行整体测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/... -v -count=1`
Expected: P1 + P2 所有测试 PASS

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/context_evolver/schema/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 9.82 P2 状态**

将 `P2(☐` 改为 `P2(✅`：

找到：`P2(☐ IO Schema层: Trajectory/TrajectoryBatch + BaseMemory/TaskMemory/PersonalMemory + ACE/RB/ReMe 三系 Memory+Request+Response + SummarizeResponse/RetrieveResponse)`

替换为：`P2(✅ IO Schema层: Trajectory/TrajectoryBatch + BaseMemory/MemoryInterface + ACE/RB/ReMe 三系 Memory+Request+Response + SummarizeResponse/RetrieveResponse[泛型])`

注意：移除了已跳过的死代码 TaskMemory/PersonalMemory，新增 MemoryInterface 和泛型标记。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.82 P2 状态为已完成"
```
