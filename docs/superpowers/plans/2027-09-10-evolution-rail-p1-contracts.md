# EvolutionRail P1 契约层实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.24 EvolutionRail 的 P1 契约层，包括共享契约类型、审批事件构建函数和审批运行时。

**Architecture:** 新建 `internal/agentcore/harness/rails/evolution/` 包，包含 3 个源码文件 + 1 个包文档 + 3 个测试文件。依赖 `internal/evolving/` 子包（已实现）和 `internal/agentcore/session/stream`。无循环依赖。

**Tech Stack:** Go 1.23, 标准库 + 项目内部包

---

### Task 1: 创建包目录和 doc.go

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/doc.go`

- [ ] **Step 1: 创建目录**

```bash
mkdir -p internal/agentcore/harness/rails/evolution
```

- [ ] **Step 2: 编写 doc.go**

```go
// Package evolution 提供技能演化轨道（Evolution Rail）的共享契约类型、
// 审批事件构建函数和审批运行时。
//
// 本包是 9.24 EvolutionRail 实现的 P1 契约层，为 P2（EvolutionRail 基类 + TrajectoryRail）、
// P3（SkillEvolutionRail）、P4（TeamSkillEvolutionRail）提供基础类型依赖。
//
// 核心功能：
//   - 契约类型：EvolutionHostEventMeta / EvolutionSnapshot / EvolutionRequestResult / SimplifyRequestResult
//   - 审批接口：ApprovalManager 窄接口（ExperienceManager 隐式满足）
//   - 审批事件：BuildSkillApprovalEvent / BuildSimplifyApprovalEvent / BuildTeamSkillApprovalEventFromRecords
//   - 审批运行时：EvolutionApprovalRuntime（查找/批准/拒绝/路由）
//
// 文件目录：
//
//	evolution/
//	├── doc.go                # 包文档
//	├── contracts.go          # 契约类型 + ApprovalManager 接口
//	├── approval_events.go    # 审批事件构建函数（7 导出 + 1 非导出）
//	└── approval_runtime.go   # EvolutionApprovalRuntime 结构体 + 4 方法
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/
package evolution
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/doc.go
git commit -m "feat(evolution): 新增 P1 契约层包文档 doc.go"
```

---

### Task 2: 实现 contracts.go — EvolutionEventKind + EvolutionHostEventMeta

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/contracts.go`
- Create: `internal/agentcore/harness/rails/evolution/contracts_test.go`

- [ ] **Step 1: 编写 EvolutionHostEventMeta.ToPayload 测试**

```go
package evolution

import (
	"testing"
)

func TestEvolutionHostEventMeta_ToPayload_仅必选字段(t *testing.T) {
	meta := EvolutionHostEventMeta{EventKind: EvolutionEventKindApproval}
	payload := meta.ToPayload()
	if len(payload) != 1 {
		t.Fatalf("payload 应有 1 个键，实际 %d", len(payload))
	}
	if payload["event_kind"] != "approval" {
		t.Errorf("event_kind = %q, want %q", payload["event_kind"], "approval")
	}
}

func TestEvolutionHostEventMeta_ToPayload_全字段(t *testing.T) {
	railKind := "skill"
	stage := "generating"
	skillName := "my_skill"
	requestID := "req_123"
	signalType := "execution_failure"
	source := "online"
	status := "completed"

	meta := EvolutionHostEventMeta{
		EventKind:  EvolutionEventKindProgress,
		RailKind:   &railKind,
		Stage:      &stage,
		SkillName:  &skillName,
		RequestID:  &requestID,
		SignalType: &signalType,
		Source:     &source,
		Status:     &status,
	}
	payload := meta.ToPayload()

	want := map[string]string{
		"event_kind":  "progress",
		"rail_kind":   "skill",
		"stage":       "generating",
		"skill_name":  "my_skill",
		"request_id":  "req_123",
		"signal_type": "execution_failure",
		"source":      "online",
		"status":      "completed",
	}
	if len(payload) != len(want) {
		t.Fatalf("payload 有 %d 个键, want %d", len(payload), len(want))
	}
	for k, v := range want {
		if payload[k] != v {
			t.Errorf("payload[%q] = %q, want %q", k, payload[k], v)
		}
	}
}

func TestEvolutionHostEventMeta_ToPayload_部分可选字段(t *testing.T) {
	railKind := "team-skill"
	skillName := "team_skill"

	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindOutcome,
		RailKind:  &railKind,
		SkillName: &skillName,
	}
	payload := meta.ToPayload()

	if len(payload) != 3 {
		t.Fatalf("payload 应有 3 个键，实际 %d", len(payload))
	}
	if _, ok := payload["stage"]; ok {
		t.Error("stage 不应出现在 payload 中")
	}
	if payload["rail_kind"] != "team-skill" {
		t.Errorf("rail_kind = %q, want %q", payload["rail_kind"], "team-skill")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestEvolutionHostEventMeta" -v
```

Expected: 编译失败，EvolutionHostEventMeta 未定义

- [ ] **Step 3: 实现 EvolutionEventKind 常量 + EvolutionHostEventMeta struct + ToPayload**

```go
package evolution

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionHostEventMeta 演化事件元数据，携带于 OutputSchema.Payload["_evolution_meta"] 中。
// 对齐 Python: EvolutionHostEventMeta (frozen dataclass)
type EvolutionHostEventMeta struct {
	// EventKind 事件类型
	EventKind EvolutionEventKind
	// RailKind 轨道类型（可选）
	RailKind *string
	// Stage 阶段（可选）
	Stage *string
	// SkillName 技能名称（可选）
	SkillName *string
	// RequestID 请求标识（可选）
	RequestID *string
	// SignalType 信号类型（可选）
	SignalType *string
	// Source 来源（可选）
	Source *string
	// Status 状态（可选）
	Status *string
}

// EvolutionSnapshot 异步演化快照，在回调上下文仍活跃时捕获。
// 对齐 Python: EvolutionSnapshot (frozen dataclass)
type EvolutionSnapshot struct {
	// Trajectory 对话轨迹
	Trajectory *trajectory.Trajectory
	// Messages 消息列表
	Messages []map[string]any
	// SkillName 技能名称（可选）
	SkillName *string
}

// EvolutionRequestResult 主动用户触发的演化 API 返回的结构化结果。
// 对齐 Python: EvolutionRequestResult (frozen dataclass)
type EvolutionRequestResult struct {
	// SkillName 技能名称
	SkillName string
	// RequestID 请求标识（可选）
	RequestID *string
	// ApprovalEvent 审批事件（可选）
	ApprovalEvent *stream.OutputSchema
	// Records 演进记录列表
	Records []checkpointing.EvolutionRecord
	// AutoApproved 是否自动审批
	AutoApproved bool
}

// SimplifyRequestResult 主动精简请求 API 返回的结构化结果。
// 对齐 Python: SimplifyRequestResult (frozen dataclass)
type SimplifyRequestResult struct {
	// SkillName 技能名称
	SkillName string
	// RequestID 请求标识（可选）
	RequestID *string
	// ApprovalEvent 审批事件（可选）
	ApprovalEvent *stream.OutputSchema
	// Actions 精简操作列表
	Actions []map[string]any
}

// TeamSkillQuestion 团队技能审批问题。
type TeamSkillQuestion struct {
	// Section 章节
	Section string
	// Content 内容
	Content string
}

// ApprovalManager 审批管理器窄接口。
// 对齐 Python: ApprovalManagerProtocol (Protocol)
//
// ExperienceManager 隐式满足此接口：
//   - ApproveRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) ✅
//   - RejectRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) ✅
type ApprovalManager interface {
	// ApproveRequest 持久化或应用一个暂存的审批请求。
	// 对齐 Python: ApprovalManagerProtocol.approve_request
	ApproveRequest(ctx context.Context, requestID string) (experience.ExperienceApplyResult, error)
	// RejectRequest 拒绝一个暂存的审批请求。
	// 对齐 Python: ApprovalManagerProtocol.reject_request
	RejectRequest(ctx context.Context, requestID string) (experience.ExperienceApplyResult, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// EvolutionEventKind 演化事件类型。
// 对齐 Python: EvolutionEventKind = Literal["approval", "progress", "outcome"]
type EvolutionEventKind = string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EvolutionEventKindApproval 审批事件
	EvolutionEventKindApproval EvolutionEventKind = "approval"
	// EvolutionEventKindProgress 进度事件
	EvolutionEventKindProgress EvolutionEventKind = "progress"
	// EvolutionEventKindOutcome 结果事件
	EvolutionEventKindOutcome EvolutionEventKind = "outcome"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// PendingApprovalSnapshotStore 暂存审批快照映射。
// 对齐 Python: PendingApprovalSnapshotStore = MutableMapping[str, PendingChange]
type PendingApprovalSnapshotStore = map[string]*experience.PendingChange

// ──────────────────────────── 导出函数 ────────────────────────────

// HasChanges 是否有变更。
// 对齐 Python: EvolutionRequestResult.has_changes
func (r EvolutionRequestResult) HasChanges() bool {
	return len(r.Records) > 0 || r.ApprovalEvent != nil
}

// HasChanges 是否有变更。
// 对齐 Python: SimplifyRequestResult.has_changes
func (r SimplifyRequestResult) HasChanges() bool {
	return len(r.Actions) > 0 || r.ApprovalEvent != nil
}

// ToPayload 返回 JSON payload 形态，跳过空字段。
// 对齐 Python: EvolutionHostEventMeta.to_payload()
func (m EvolutionHostEventMeta) ToPayload() map[string]string {
	payload := map[string]string{"event_kind": m.EventKind}
	if m.RailKind != nil {
		payload["rail_kind"] = *m.RailKind
	}
	if m.Stage != nil {
		payload["stage"] = *m.Stage
	}
	if m.SkillName != nil {
		payload["skill_name"] = *m.SkillName
	}
	if m.RequestID != nil {
		payload["request_id"] = *m.RequestID
	}
	if m.SignalType != nil {
		payload["signal_type"] = *m.SignalType
	}
	if m.Source != nil {
		payload["source"] = *m.Source
	}
	if m.Status != nil {
		payload["status"] = *m.Status
	}
	return payload
}

// ToLegacyDict 返回供轨道钩子和测试使用的 dict 形态。
// 对齐 Python: EvolutionSnapshot.to_legacy_dict()
func (s EvolutionSnapshot) ToLegacyDict() map[string]any {
	snapshot := map[string]any{
		"trajectory": s.Trajectory,
		"messages":   s.Messages,
	}
	if s.SkillName != nil {
		snapshot["skill_name"] = *s.SkillName
	}
	return snapshot
}

// FromLegacyDict 从 dict 恢复 EvolutionSnapshot。
// 对齐 Python: EvolutionSnapshot.from_legacy_dict()
func FromLegacyDict(snapshot map[string]any) EvolutionSnapshot {
	var traj *trajectory.Trajectory
	if t, ok := snapshot["trajectory"]; ok {
		if typed, ok := t.(*trajectory.Trajectory); ok {
			traj = typed
		}
	}

	messages := []map[string]any{}
	if m, ok := snapshot["messages"]; ok {
		if typed, ok := m.([]map[string]any); ok {
			messages = typed
		}
	}

	var skillName *string
	if sn, ok := snapshot["skill_name"]; ok {
		if typed, ok := sn.(string); ok {
			skillName = &typed
		}
	}

	return EvolutionSnapshot{
		Trajectory: traj,
		Messages:   messages,
		SkillName:  skillName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：需要在文件头加 `import "context"`，完整 import 列表：

```go
import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestEvolutionHostEventMeta" -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/contracts.go internal/agentcore/harness/rails/evolution/contracts_test.go
git commit -m "feat(evolution): 实现 EvolutionEventKind + EvolutionHostEventMeta + ToPayload"
```

---

### Task 3: 补充 contracts_test.go — EvolutionSnapshot + EvolutionRequestResult + SimplifyRequestResult

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/contracts_test.go`

- [ ] **Step 1: 编写 EvolutionSnapshot / EvolutionRequestResult / SimplifyRequestResult 测试**

在 `contracts_test.go` 末尾追加：

```go
func TestEvolutionSnapshot_ToLegacyDict_全字段(t *testing.T) {
	skill := "my_skill"
	snap := EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{ExecutionID: "exec_1"},
		Messages:   []map[string]any{{"role": "user", "content": "hello"}},
		SkillName:  &skill,
	}
	dict := snap.ToLegacyDict()
	if dict["trajectory"] == nil {
		t.Error("trajectory 不应为 nil")
	}
	if len(dict["messages"].([]map[string]any)) != 1 {
		t.Error("messages 应有 1 条")
	}
	if dict["skill_name"] != "my_skill" {
		t.Errorf("skill_name = %v, want %q", dict["skill_name"], "my_skill")
	}
}

func TestEvolutionSnapshot_ToLegacyDict_无SkillName(t *testing.T) {
	snap := EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{ExecutionID: "exec_1"},
		Messages:   []map[string]any{},
	}
	dict := snap.ToLegacyDict()
	if _, ok := dict["skill_name"]; ok {
		t.Error("skill_name 不应出现")
	}
}

func TestEvolutionSnapshot_FromLegacyDict_全字段(t *testing.T) {
	skill := "my_skill"
	dict := map[string]any{
		"trajectory": &trajectory.Trajectory{ExecutionID: "exec_1"},
		"messages":   []map[string]any{{"role": "user"}},
		"skill_name": skill,
	}
	snap := FromLegacyDict(dict)
	if snap.Trajectory == nil || snap.Trajectory.ExecutionID != "exec_1" {
		t.Error("Trajectory 未正确恢复")
	}
	if len(snap.Messages) != 1 {
		t.Error("Messages 未正确恢复")
	}
	if snap.SkillName == nil || *snap.SkillName != "my_skill" {
		t.Error("SkillName 未正确恢复")
	}
}

func TestEvolutionSnapshot_FromLegacyDict_缺失字段(t *testing.T) {
	dict := map[string]any{
		"trajectory": &trajectory.Trajectory{},
	}
	snap := FromLegacyDict(dict)
	if len(snap.Messages) != 0 {
		t.Errorf("Messages 应为空切片，实际 %d", len(snap.Messages))
	}
	if snap.SkillName != nil {
		t.Error("SkillName 应为 nil")
	}
}

func TestEvolutionRequestResult_HasChanges_有Records(t *testing.T) {
	r := EvolutionRequestResult{Records: []checkpointing.EvolutionRecord{{ID: "ev_1"}}}
	if !r.HasChanges() {
		t.Error("有 Records 时 HasChanges 应返回 true")
	}
}

func TestEvolutionRequestResult_HasChanges_有ApprovalEvent(t *testing.T) {
	r := EvolutionRequestResult{ApprovalEvent: &stream.OutputSchema{}}
	if !r.HasChanges() {
		t.Error("有 ApprovalEvent 时 HasChanges 应返回 true")
	}
}

func TestEvolutionRequestResult_HasChanges_两者均空(t *testing.T) {
	r := EvolutionRequestResult{}
	if r.HasChanges() {
		t.Error("两者均空时 HasChanges 应返回 false")
	}
}

func TestSimplifyRequestResult_HasChanges_有Actions(t *testing.T) {
	r := SimplifyRequestResult{Actions: []map[string]any{{"action": "remove"}}}
	if !r.HasChanges() {
		t.Error("有 Actions 时 HasChanges 应返回 true")
	}
}

func TestSimplifyRequestResult_HasChanges_两者均空(t *testing.T) {
	r := SimplifyRequestResult{}
	if r.HasChanges() {
		t.Error("两者均空时 HasChanges 应返回 false")
	}
}
```

需在 test 文件头加 import：

```go
import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)
```

- [ ] **Step 2: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -v
```

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/contracts.go internal/agentcore/harness/rails/evolution/contracts_test.go
git commit -m "feat(evolution): 实现 EvolutionSnapshot + EvolutionRequestResult + SimplifyRequestResult + 测试"
```

---

### Task 4: 实现 approval_events.go

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/approval_events.go`
- Create: `internal/agentcore/harness/rails/evolution/approval_events_test.go`

- [ ] **Step 1: 编写 approval_events.go**

```go
package evolution

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// progressEventConfig 演化进度事件的可选参数配置。
type progressEventConfig struct {
	skillName *string
	requestID *string
	prefix    *string
}

// ProgressEventOption 演化进度事件的可选参数。
type ProgressEventOption func(*progressEventConfig)

// WithSkillName 设置技能名称。
func WithSkillName(skillName string) ProgressEventOption {
	return func(c *progressEventConfig) { c.skillName = &skillName }
}

// WithRequestID 设置请求标识。
func WithRequestID(requestID string) ProgressEventOption {
	return func(c *progressEventConfig) { c.requestID = &requestID }
}

// WithPrefix 设置显示前缀。
func WithPrefix(prefix string) ProgressEventOption {
	return func(c *progressEventConfig) { c.prefix = &prefix }
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildProgressEvent 构建 llm_reasoning 进度事件。
// 对齐 Python: build_progress_event
func BuildProgressEvent(prefix, message string) *stream.OutputSchema {
	return &stream.OutputSchema{
		Type: "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content": fmt.Sprintf("%s %s\n", prefix, message),
		},
	}
}

// BuildEvolutionProgressEvent 构建规范化的演化进度事件。
// 对齐 Python: build_evolution_progress_event
func BuildEvolutionProgressEvent(railKind, stage, message string, opts ...ProgressEventOption) *stream.OutputSchema {
	cfg := &progressEventConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	displayPrefix := "[Evolution]"
	if cfg.prefix != nil {
		displayPrefix = *cfg.prefix
	}

	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindProgress,
		RailKind:  &railKind,
		Stage:     &stage,
	}.ToPayload()

	if cfg.skillName != nil {
		meta["skill_name"] = *cfg.skillName
	}
	if cfg.requestID != nil {
		meta["request_id"] = *cfg.requestID
	}

	return &stream.OutputSchema{
		Type:  "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content":         fmt.Sprintf("%s %s\n", displayPrefix, message),
			"_evolution_meta": meta,
		},
	}
}

// AttachEvolutionMeta 向审批事件的 payload 附加规范化的演化元数据。
// 对齐 Python: attach_evolution_meta
func AttachEvolutionMeta(event *stream.OutputSchema, signalType, signalSource *string) *stream.OutputSchema {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		payload = make(map[string]any)
		event.Payload = payload
	}

	evolutionMeta, ok := payload["_evolution_meta"].(map[string]string)
	if !ok {
		evolutionMeta = map[string]string{}
		payload["_evolution_meta"] = evolutionMeta
	}

	if _, exists := evolutionMeta["event_kind"]; !exists {
		evolutionMeta["event_kind"] = EvolutionEventKindApproval
	}
	if signalType != nil {
		evolutionMeta["signal_type"] = *signalType
	}
	if signalSource != nil {
		evolutionMeta["source"] = *signalSource
	}

	return event
}

// BuildSkillApprovalEvent 构建技能经验审批事件。
// 对齐 Python: build_skill_approval_event
func BuildSkillApprovalEvent(
	skillName, requestID string,
	records []checkpointing.EvolutionRecord,
	language string,
	isSharedRecords bool,
) *stream.OutputSchema {
	en := isEn(language)
	questions := make([]map[string]any, 0, len(records))

	var header string
	if isSharedRecords {
		if en {
			header = "Shared Experience Approval"
		} else {
			header = "在线共享经验审批"
		}
	} else {
		if en {
			header = "Skill Evolution Approval"
		} else {
			header = "技能演进审批"
		}
	}

	for _, record := range records {
		content := record.Change.Content
		if len(content) > 1000 {
			content = content[:1000]
		}

		var question string
		if en {
			question = fmt.Sprintf(
				"**Skill '%s' generated a new experience:**\n\n- **Target**: %s\n- **Section**: %s\n\n%s",
				skillName, string(record.Change.Target), record.Change.Section, content,
			)
		} else {
			question = fmt.Sprintf(
				"**Skill '%s' 演进生成了新经验：**\n\n- **目标**: %s\n- **章节**: %s\n\n%s",
				skillName, string(record.Change.Target), record.Change.Section, content,
			)
		}

		var options []map[string]string
		if en {
			options = []map[string]string{
				{"label": "Accept", "description": "Keep this evolution experience"},
				{"label": "Reject", "description": "Discard this evolution experience"},
			}
		} else {
			options = []map[string]string{
				{"label": "接收", "description": "保留此演进经验"},
				{"label": "拒绝", "description": "丢弃此演进经验"},
			}
		}

		questions = append(questions, map[string]any{
			"question":     question,
			"header":       header,
			"options":      options,
			"multi_select": false,
		})
	}

	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindApproval,
		SkillName: &skillName,
		RequestID: &requestID,
	}.ToPayload()

	if isSharedRecords {
		if src, ok := meta["source"]; !ok || src == "" {
			meta["source"] = "experience_sharing"
		}
		meta["is_shared_records"] = "true"
	}

	return &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id":      requestID,
			"_evolution_meta": meta,
			"questions":       questions,
		},
	}
}

// BuildSimplifyApprovalEvent 构建精简审批事件。
// 对齐 Python: build_simplify_approval_event
func BuildSimplifyApprovalEvent(
	skillName, requestID string,
	actions []map[string]any,
	language string,
) *stream.OutputSchema {
	en := isEn(language)

	previewParts := make([]string, 0, len(actions))
	limit := len(actions)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		action := actions[i]
		act := "?"
		if a, ok := action["action"]; ok {
			if s, ok := a.(string); ok {
				act = s
			}
		}
		recordID := "?"
		if r, ok := action["record_id"]; ok {
			if s, ok := r.(string); ok {
				recordID = s
			}
		}
		reason := ""
		if r, ok := action["reason"]; ok {
			if s, ok := r.(string); ok {
				reason = s
			}
		}
		previewParts = append(previewParts, fmt.Sprintf("- **%s** `%s`: %s", act, recordID, reason))
	}
	preview := ""
	for i, p := range previewParts {
		if i > 0 {
			preview += "\n"
		}
		preview += p
	}

	var question string
	if en {
		question = fmt.Sprintf(
			"**Simplify evolution experiences for Skill '%s'**\n\n%d action(s):\n%s\n\nDo you want to execute them?",
			skillName, len(actions), preview,
		)
	} else {
		question = fmt.Sprintf(
			"**精简 Skill '%s' 的演进经验**\n\n共 %d 项操作：\n%s\n\n是否执行？",
			skillName, len(actions), preview,
		)
	}

	var header string
	if en {
		header = "Skill Simplify Approval"
	} else {
		header = "Skill 精简审批"
	}

	var options []map[string]string
	if en {
		options = []map[string]string{
			{"label": "Execute", "description": "Run the simplify actions"},
			{"label": "Cancel", "description": "Discard this simplify request"},
		}
	} else {
		options = []map[string]string{
			{"label": "执行", "description": "执行精简操作"},
			{"label": "取消", "description": "放弃本次精简"},
		}
	}

	return &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id": requestID,
			"questions": []map[string]any{
				{
					"question":     question,
					"header":       header,
					"options":      options,
					"multi_select": false,
				},
			},
		},
	}
}

// BuildTeamSkillApprovalEventFromRecords 从暂存记录构建团队技能审批事件。
// 对齐 Python: build_team_skill_approval_event_from_records
func BuildTeamSkillApprovalEventFromRecords(
	skillName, requestID, language string,
	records []checkpointing.EvolutionRecord,
) *stream.OutputSchema {
	questions := make([]TeamSkillQuestion, 0, len(records))
	for _, record := range records {
		questions = append(questions, TeamSkillQuestion{
			Section: record.Change.Section,
			Content: record.Change.Content,
		})
	}
	return buildTeamSkillExperienceQuestionEvent(skillName, requestID, language, questions)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isEn 判断语言是否为英文。
// 对齐 Python: _is_en(language: str) -> bool
func isEn(language string) bool {
	return language == "en"
}

// buildTeamSkillExperienceQuestionEvent 从标准化问题输入构建团队技能经验审批事件。
// 对齐 Python: _build_team_skill_experience_question_event
func buildTeamSkillExperienceQuestionEvent(
	skillName, requestID, language string,
	questions []TeamSkillQuestion,
) *stream.OutputSchema {
	en := isEn(language)
	questionPayload := make([]map[string]any, 0, len(questions))

	for _, q := range questions {
		content := q.Content
		if len(content) > 1000 {
			content = content[:1000]
		}

		var question string
		if en {
			question = fmt.Sprintf(
				"**Team Skill '%s' evolution:**\n\n- **Section**: %s\n\n%s",
				skillName, q.Section, content,
			)
		} else {
			question = fmt.Sprintf(
				"**团队技能 '%s' 生成了演进经验：**\n\n- **章节**: %s\n\n%s",
				skillName, q.Section, content,
			)
		}

		var header string
		if en {
			header = "Team Skill Evolution Approval"
		} else {
			header = "团队技能演进审批"
		}

		var options []map[string]string
		if en {
			options = []map[string]string{
				{"label": "Accept", "description": "Keep this evolution"},
				{"label": "Reject", "description": "Discard this evolution"},
			}
		} else {
			options = []map[string]string{
				{"label": "接收", "description": "保留此演进经验"},
				{"label": "拒绝", "description": "丢弃此演进经验"},
			}
		}

		questionPayload = append(questionPayload, map[string]any{
			"question":     question,
			"header":       header,
			"options":      options,
			"multi_select": false,
		})
	}

	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindApproval,
		SkillName: &skillName,
		RequestID: &requestID,
	}.ToPayload()

	return &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id":      requestID,
			"_evolution_meta": meta,
			"questions":       questionPayload,
		},
	}
}
```

- [ ] **Step 2: 编写 approval_events_test.go**

```go
package evolution

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

func TestBuildProgressEvent(t *testing.T) {
	event := BuildProgressEvent("[Test]", "something happened")
	payload := event.Payload.(map[string]any)
	content := payload["content"].(string)
	if !strings.Contains(content, "[Test] something happened") {
		t.Errorf("content = %q, 应包含 [Test] something happened", content)
	}
	if event.Type != "llm_reasoning" {
		t.Errorf("Type = %q, want %q", event.Type, "llm_reasoning")
	}
}

func TestBuildEvolutionProgressEvent_默认前缀(t *testing.T) {
	event := BuildEvolutionProgressEvent("skill", "generating", "started")
	payload := event.Payload.(map[string]any)
	content := payload["content"].(string)
	if !strings.HasPrefix(content, "[Evolution]") {
		t.Errorf("content 应以 [Evolution] 开头，实际 %q", content)
	}
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["event_kind"] != "progress" {
		t.Errorf("event_kind = %q, want %q", meta["event_kind"], "progress")
	}
	if meta["rail_kind"] != "skill" {
		t.Errorf("rail_kind = %q, want %q", meta["rail_kind"], "skill")
	}
	if meta["stage"] != "generating" {
		t.Errorf("stage = %q, want %q", meta["stage"], "generating")
	}
}

func TestBuildEvolutionProgressEvent_可选参数(t *testing.T) {
	event := BuildEvolutionProgressEvent("skill", "generating", "started",
		WithSkillName("my_skill"),
		WithRequestID("req_1"),
		WithPrefix("[Custom]"),
	)
	payload := event.Payload.(map[string]any)
	content := payload["content"].(string)
	if !strings.HasPrefix(content, "[Custom]") {
		t.Errorf("content 应以 [Custom] 开头，实际 %q", content)
	}
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["skill_name"] != "my_skill" {
		t.Errorf("skill_name = %q, want %q", meta["skill_name"], "my_skill")
	}
	if meta["request_id"] != "req_1" {
		t.Errorf("request_id = %q, want %q", meta["request_id"], "req_1")
	}
}

func TestAttachEvolutionMeta_默认event_kind(t *testing.T) {
	event := &stream.OutputSchema{
		Type: "chat.ask_user_question",
		Payload: map[string]any{},
	}
	result := AttachEvolutionMeta(event, nil, nil)
	payload := result.Payload.(map[string]any)
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["event_kind"] != "approval" {
		t.Errorf("event_kind = %q, want %q", meta["event_kind"], "approval")
	}
}

func TestAttachEvolutionMeta_注入signal字段(t *testing.T) {
	sigType := "execution_failure"
	sigSource := "online"
	event := &stream.OutputSchema{
		Type: "chat.ask_user_question",
		Payload: map[string]any{},
	}
	result := AttachEvolutionMeta(event, &sigType, &sigSource)
	payload := result.Payload.(map[string]any)
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["signal_type"] != "execution_failure" {
		t.Errorf("signal_type = %q, want %q", meta["signal_type"], "execution_failure")
	}
	if meta["source"] != "online" {
		t.Errorf("source = %q, want %q", meta["source"], "online")
	}
}

func makeTestRecords() []checkpointing.EvolutionRecord {
	return []checkpointing.EvolutionRecord{
		{
			ID: "ev_001",
			Change: checkpointing.EvolutionPatch{
				Section: "Troubleshooting",
				Content: "If the service fails to start, check the port configuration.",
				Target:  signal.EvolutionTargetBody,
			},
		},
	}
}

func TestBuildSkillApprovalEvent_CN(t *testing.T) {
	event := BuildSkillApprovalEvent("my_skill", "req_1", makeTestRecords(), "cn", false)
	if event.Type != "chat.ask_user_question" {
		t.Errorf("Type = %q, want %q", event.Type, "chat.ask_user_question")
	}
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	if len(questions) != 1 {
		t.Fatalf("questions 应有 1 条，实际 %d", len(questions))
	}
	q := questions[0]["question"].(string)
	if !strings.Contains(q, "演进生成了新经验") {
		t.Errorf("中文模板应包含 '演进生成了新经验'，实际 %q", q)
	}
	if questions[0]["header"] != "技能演进审批" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "技能演进审批")
	}
	opts := questions[0]["options"].([]map[string]string)
	if opts[0]["label"] != "接收" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "接收")
	}
}

func TestBuildSkillApprovalEvent_EN(t *testing.T) {
	event := BuildSkillApprovalEvent("my_skill", "req_1", makeTestRecords(), "en", false)
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	if !strings.Contains(q, "generated a new experience") {
		t.Errorf("英文模板应包含 'generated a new experience'，实际 %q", q)
	}
	if questions[0]["header"] != "Skill Evolution Approval" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "Skill Evolution Approval")
	}
}

func TestBuildSkillApprovalEvent_共享记录(t *testing.T) {
	event := BuildSkillApprovalEvent("my_skill", "req_1", makeTestRecords(), "cn", true)
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	if questions[0]["header"] != "在线共享经验审批" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "在线共享经验审批")
	}
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["is_shared_records"] != "true" {
		t.Errorf("is_shared_records = %q, want %q", meta["is_shared_records"], "true")
	}
}

func TestBuildSimplifyApprovalEvent_CN(t *testing.T) {
	actions := []map[string]any{
		{"action": "remove", "record_id": "ev_001", "reason": "outdated"},
	}
	event := BuildSimplifyApprovalEvent("my_skill", "req_1", actions, "cn")
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	if !strings.Contains(q, "精简") {
		t.Errorf("中文模板应包含 '精简'，实际 %q", q)
	}
	if !strings.Contains(q, "1 项操作") {
		t.Errorf("中文模板应包含 '1 项操作'，实际 %q", q)
	}
	opts := questions[0]["options"].([]map[string]string)
	if opts[0]["label"] != "执行" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "执行")
	}
}

func TestBuildSimplifyApprovalEvent_EN(t *testing.T) {
	actions := []map[string]any{
		{"action": "remove", "record_id": "ev_001", "reason": "outdated"},
	}
	event := BuildSimplifyApprovalEvent("my_skill", "req_1", actions, "en")
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	if !strings.Contains(q, "Simplify") {
		t.Errorf("英文模板应包含 'Simplify'，实际 %q", q)
	}
	opts := questions[0]["options"].([]map[string]string)
	if opts[0]["label"] != "Execute" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "Execute")
	}
}

func TestBuildTeamSkillApprovalEventFromRecords_CN(t *testing.T) {
	event := BuildTeamSkillApprovalEventFromRecords("team_skill", "req_1", "cn", makeTestRecords())
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	if !strings.Contains(q, "团队技能") {
		t.Errorf("中文模板应包含 '团队技能'，实际 %q", q)
	}
	if questions[0]["header"] != "团队技能演进审批" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "团队技能演进审批")
	}
}

func TestBuildTeamSkillApprovalEventFromRecords_EN(t *testing.T) {
	event := BuildTeamSkillApprovalEventFromRecords("team_skill", "req_1", "en", makeTestRecords())
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	if !strings.Contains(q, "Team Skill") {
		t.Errorf("英文模板应包含 'Team Skill'，实际 %q", q)
	}
	if questions[0]["header"] != "Team Skill Evolution Approval" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "Team Skill Evolution Approval")
	}
}
```

需在 test 文件头加 import：

```go
import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -v
```

Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/approval_events.go internal/agentcore/harness/rails/evolution/approval_events_test.go
git commit -m "feat(evolution): 实现审批事件构建函数（7 导出 + 1 非导出）+ 双语模板 + 测试"
```

---

### Task 5: 实现 approval_runtime.go

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/approval_runtime.go`
- Create: `internal/agentcore/harness/rails/evolution/approval_runtime_test.go`

- [ ] **Step 1: 编写 approval_runtime.go**

```go
package evolution

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionApprovalRuntime 共享审批生命周期辅助，绑定到一个轨道实例。
// 对齐 Python: EvolutionApprovalRuntime
type EvolutionApprovalRuntime struct {
	// manager 审批管理器
	manager ApprovalManager
	// pendingApprovalSnapshots 暂存审批快照映射
	pendingApprovalSnapshots PendingApprovalSnapshotStore
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionApprovalRuntime 创建审批运行时。
// 对齐 Python: EvolutionApprovalRuntime.__init__
func NewEvolutionApprovalRuntime(manager ApprovalManager, pendingApprovalSnapshots PendingApprovalSnapshotStore) *EvolutionApprovalRuntime {
	return &EvolutionApprovalRuntime{
		manager:                  manager,
		pendingApprovalSnapshots: pendingApprovalSnapshots,
	}
}

// LookupPendingApprovalSnapshot 解析一个具体轨道拥有的暂存审批快照。
// 对齐 Python: EvolutionApprovalRuntime.lookup_pending_approval_snapshot
func (r *EvolutionApprovalRuntime) LookupPendingApprovalSnapshot(requestID, railName, actionName string) *experience.PendingChange {
	pending := r.pendingApprovalSnapshots[requestID]
	if pending == nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("rail_name", railName).
			Str("action_name", actionName).
			Str("request_id", requestID).
			Msg("unknown request_id")
	}
	return pending
}

// ApprovePendingRequest 通过共享管理器生命周期批准一个暂存请求。
// 对齐 Python: EvolutionApprovalRuntime.approve_pending_request
func (r *EvolutionApprovalRuntime) ApprovePendingRequest(
	ctx context.Context,
	requestID, railName, actionName string,
) (*experience.PendingChange, *experience.ExperienceApplyResult, error) {
	pending := r.LookupPendingApprovalSnapshot(requestID, railName, actionName)
	if pending == nil {
		return nil, nil, nil
	}

	result, err := r.manager.ApproveRequest(ctx, requestID)
	if err != nil {
		return pending, nil, err
	}

	if result.PendingCount > 0 {
		logger.Warn(logger.ComponentAgentCore).
			Str("rail_name", railName).
			Str("action_name", actionName).
			Int("applied_count", result.AppliedCount).
			Int("pending_count", result.PendingCount).
			Str("skill_name", pending.SkillName).
			Str("request_id", requestID).
			Msg("partial failure: some records not written, retry to complete")
	}

	return pending, &result, nil
}

// RejectPendingRequest 通过共享管理器生命周期拒绝一个暂存请求。
// 对齐 Python: EvolutionApprovalRuntime.reject_pending_request
func (r *EvolutionApprovalRuntime) RejectPendingRequest(
	ctx context.Context,
	requestID, railName, actionName string,
) (*experience.PendingChange, *experience.ExperienceApplyResult, error) {
	pending := r.LookupPendingApprovalSnapshot(requestID, railName, actionName)
	if pending == nil {
		return nil, nil, nil
	}

	result, err := r.manager.RejectRequest(ctx, requestID)
	if err != nil {
		return pending, nil, err
	}

	return pending, &result, nil
}

// FinalizeStagedEvolutionRequest 将暂存请求路由到审批缓冲或自动审批副作用。
// 对齐 Python: EvolutionApprovalRuntime.finalize_staged_evolution_request
//
// Python 中使用 inspect.isawaitable 判断回调是否需要 await，
// Go 中回调统一为 func(any) error，调用方在闭包内自行处理异步。
func (r *EvolutionApprovalRuntime) FinalizeStagedEvolutionRequest(
	request any,
	requiresApproval bool,
	emitApprovalRequest func(any) error,
	onAutoApproved func(any) error,
) error {
	if request == nil {
		return nil
	}

	if requiresApproval {
		if emitApprovalRequest != nil {
			return emitApprovalRequest(request)
		}
		return nil
	}

	if onAutoApproved != nil {
		return onAutoApproved(request)
	}
	return nil
}
```

- [ ] **Step 2: 编写 approval_runtime_test.go**

```go
package evolution

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
)

// fakeApprovalManager 用于测试的模拟审批管理器。
type fakeApprovalManager struct {
	approveResult experience.ExperienceApplyResult
	approveErr    error
	rejectResult  experience.ExperienceApplyResult
	rejectErr     error
}

func (f *fakeApprovalManager) ApproveRequest(_ context.Context, _ string) (experience.ExperienceApplyResult, error) {
	return f.approveResult, f.approveErr
}

func (f *fakeApprovalManager) RejectRequest(_ context.Context, _ string) (experience.ExperienceApplyResult, error) {
	return f.rejectResult, f.rejectErr
}

func TestLookupPendingApprovalSnapshot_Found(t *testing.T) {
	snapshots := PendingApprovalSnapshotStore{
		"req_1": &experience.PendingChange{SkillName: "my_skill"},
	}
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, snapshots)
	pending := rt.LookupPendingApprovalSnapshot("req_1", "skill", "approve")
	if pending == nil {
		t.Fatal("应找到 pending snapshot")
	}
	if pending.SkillName != "my_skill" {
		t.Errorf("SkillName = %q, want %q", pending.SkillName, "my_skill")
	}
}

func TestLookupPendingApprovalSnapshot_NotFound(t *testing.T) {
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	pending := rt.LookupPendingApprovalSnapshot("nonexistent", "skill", "approve")
	if pending != nil {
		t.Error("未找到时应返回 nil")
	}
}

func TestApprovePendingRequest_Success(t *testing.T) {
	pendingChange := &experience.PendingChange{SkillName: "my_skill"}
	snapshots := PendingApprovalSnapshotStore{"req_1": pendingChange}
	mgr := &fakeApprovalManager{
		approveResult: experience.ExperienceApplyResult{AppliedCount: 3, PendingCount: 0},
	}
	rt := NewEvolutionApprovalRuntime(mgr, snapshots)

	pending, result, err := rt.ApprovePendingRequest(context.Background(), "req_1", "skill", "approve")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != pendingChange {
		t.Error("pending 不匹配")
	}
	if result.AppliedCount != 3 {
		t.Errorf("AppliedCount = %d, want 3", result.AppliedCount)
	}
}

func TestApprovePendingRequest_PartialFailure(t *testing.T) {
	pendingChange := &experience.PendingChange{SkillName: "my_skill"}
	snapshots := PendingApprovalSnapshotStore{"req_1": pendingChange}
	mgr := &fakeApprovalManager{
		approveResult: experience.ExperienceApplyResult{AppliedCount: 2, PendingCount: 1},
	}
	rt := NewEvolutionApprovalRuntime(mgr, snapshots)

	pending, result, err := rt.ApprovePendingRequest(context.Background(), "req_1", "skill", "approve")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending == nil {
		t.Fatal("pending 不应为 nil")
	}
	if result.PendingCount != 1 {
		t.Errorf("PendingCount = %d, want 1", result.PendingCount)
	}
}

func TestApprovePendingRequest_NotFound(t *testing.T) {
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	pending, result, err := rt.ApprovePendingRequest(context.Background(), "nonexistent", "skill", "approve")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != nil {
		t.Error("pending 应为 nil")
	}
	if result != nil {
		t.Error("result 应为 nil")
	}
}

func TestRejectPendingRequest_Success(t *testing.T) {
	pendingChange := &experience.PendingChange{SkillName: "my_skill"}
	snapshots := PendingApprovalSnapshotStore{"req_1": pendingChange}
	mgr := &fakeApprovalManager{
		rejectResult: experience.ExperienceApplyResult{RejectedCount: 3},
	}
	rt := NewEvolutionApprovalRuntime(mgr, snapshots)

	pending, result, err := rt.RejectPendingRequest(context.Background(), "req_1", "skill", "reject")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != pendingChange {
		t.Error("pending 不匹配")
	}
	if result.RejectedCount != 3 {
		t.Errorf("RejectedCount = %d, want 3", result.RejectedCount)
	}
}

func TestRejectPendingRequest_NotFound(t *testing.T) {
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	pending, result, err := rt.RejectPendingRequest(context.Background(), "nonexistent", "skill", "reject")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != nil {
		t.Error("pending 应为 nil")
	}
	if result != nil {
		t.Error("result 应为 nil")
	}
}

func TestFinalizeStagedEvolutionRequest_RequiresApproval(t *testing.T) {
	var called bool
	emitFn := func(_ any) error {
		called = true
		return nil
	}
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest("some_request", true, emitFn, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !called {
		t.Error("emitApprovalRequest 应被调用")
	}
}

func TestFinalizeStagedEvolutionRequest_AutoApproved(t *testing.T) {
	var called bool
	autoFn := func(_ any) error {
		called = true
		return nil
	}
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest("some_request", false, nil, autoFn)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !called {
		t.Error("onAutoApproved 应被调用")
	}
}

func TestFinalizeStagedEvolutionRequest_NilRequest(t *testing.T) {
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest(nil, true, nil, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

func TestFinalizeStagedEvolutionRequest_NoAutoApprovedCallback(t *testing.T) {
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest("some_request", false, nil, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -v
```

Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/approval_runtime.go internal/agentcore/harness/rails/evolution/approval_runtime_test.go
git commit -m "feat(evolution): 实现 EvolutionApprovalRuntime + 4 方法 + 测试"
```

---

### Task 6: 验证 ExperienceManager 隐式满足 ApprovalManager 接口

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/interface_compat_test.go`

- [ ] **Step 1: 编写接口兼容性测试**

```go
package evolution

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
)

// TestApprovalManager_ExperienceManager满足接口 验证 ExperienceManager 隐式满足 ApprovalManager。
func TestApprovalManager_ExperienceManager满足接口(t *testing.T) {
	// 编译期检查：如果 ExperienceManager 不满足 ApprovalManager，此行会编译失败
	var _ ApprovalManager = (*experience.ExperienceManager)(nil)
}
```

- [ ] **Step 2: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestApprovalManager_ExperienceManager" -v
```

Expected: PASS（编译通过即证明接口满足）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/interface_compat_test.go
git commit -m "test(evolution): 验证 ExperienceManager 隐式满足 ApprovalManager 接口"
```

---

### Task 7: 全量测试 + 覆盖率检查

**Files:**
- 无新增文件

- [ ] **Step 1: 运行全部测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -v
```

Expected: 全部 PASS

- [ ] **Step 2: 检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/...
```

Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 如果覆盖率不达标，补充测试后重新运行 Step 2**

---

### Task 8: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/doc.go`

- [ ] **Step 1: 更新 doc.go 中的文件目录，加入所有新文件**

将 doc.go 中文件目录更新为：

```
// 文件目录：
//
//	evolution/
//	├── doc.go                   # 包文档
//	├── contracts.go             # 契约类型 + ApprovalManager 接口
//	├── approval_events.go       # 审批事件构建函数（7 导出 + 1 非导出）
//	└── approval_runtime.go      # EvolutionApprovalRuntime 结构体 + 4 方法
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/doc.go
git commit -m "docs(evolution): 更新 doc.go 文件目录"
```

---

## Self-Review Checklist

**1. Spec 覆盖检查：**
- ✅ EvolutionEventKind → Task 2
- ✅ EvolutionHostEventMeta + ToPayload → Task 2
- ✅ EvolutionSnapshot + ToLegacyDict + FromLegacyDict → Task 3
- ✅ EvolutionRequestResult + HasChanges → Task 3
- ✅ SimplifyRequestResult + HasChanges → Task 3
- ✅ ApprovalManager interface → Task 2
- ✅ PendingApprovalSnapshotStore → Task 2
- ✅ BuildProgressEvent → Task 4
- ✅ BuildEvolutionProgressEvent + ProgressEventOption → Task 4
- ✅ AttachEvolutionMeta → Task 4
- ✅ BuildSkillApprovalEvent → Task 4
- ✅ BuildSimplifyApprovalEvent → Task 4
- ✅ BuildTeamSkillApprovalEventFromRecords → Task 4
- ✅ buildTeamSkillExperienceQuestionEvent → Task 4
- ✅ TeamSkillQuestion → Task 4 (定义在 approval_events.go)
- ✅ EvolutionApprovalRuntime → Task 5
- ✅ LookupPendingApprovalSnapshot → Task 5
- ✅ ApprovePendingRequest → Task 5
- ✅ RejectPendingRequest → Task 5
- ✅ FinalizeStagedEvolutionRequest → Task 5
- ✅ ExperienceManager 隐式满足验证 → Task 6

**2. Placeholder scan:** 无 TBD/TODO/待实现 ✅

**3. 类型一致性检查：**
- ApprovalManager 接口签名与 Task 5 使用一致 ✅
- EvolutionHostEventMeta.ToPayload() 返回 map[string]string 与 Task 4 使用一致 ✅
- ExperienceApplyResult 字段名（AppliedCount/PendingCount/RejectedCount）与 Task 5 测试一致 ✅
- PendingChange.SkillName 与 Task 5 测试一致 ✅
