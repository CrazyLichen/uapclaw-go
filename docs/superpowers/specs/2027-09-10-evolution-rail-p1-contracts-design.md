# 9.24 P1 契约层设计 — EvolutionRail 基础契约与审批事件

## 概述

9.24 EvolutionRail 拆分为 6 个阶段（P1-P6），本文档描述 P1 契约层的实现设计。
P1 实现所有演化轨道共享的契约类型、审批事件构建函数和审批运行时，
为 P2（EvolutionRail 基类 + TrajectoryRail）提供基础依赖。

## 包路径

`internal/agentcore/harness/rails/evolution/`

对应 Python：`openjiuwen/harness/rails/evolution/`

## 文件结构

```
evolution/
├── doc.go                   # 包文档
├── contracts.go             # 契约类型（6 类型 + 3 常量 + 方法）
├── approval_events.go       # 7 个事件构建函数 + 双语模板
├── approval_runtime.go      # EvolutionApprovalRuntime 结构体 + 4 方法
├── contracts_test.go        # contracts 单元测试
├── approval_events_test.go  # approval_events 单元测试
└── approval_runtime_test.go # approval_runtime 单元测试
```

## 依赖方向

```
evolution ──→ internal/evolving/experience     (ExperienceApplyResult, PendingChange)
          ──→ internal/evolving/checkpointing  (EvolutionRecord)
          ──→ internal/evolving/trajectory     (Trajectory)
          ──→ internal/agentcore/session/stream (OutputSchema)
          ──→ internal/common/logger           (日志)
```

**无循环依赖**。所有被依赖包均不反向导入 evolution。

---

## contracts.go

### EvolutionEventKind

对齐 Python：`EvolutionEventKind = Literal["approval", "progress", "outcome"]`

Go 使用 string 常量（不用 iota），对齐 Python Literal 语义：

```go
// EvolutionEventKind 演化事件类型。
// 对齐 Python: EvolutionEventKind = Literal["approval", "progress", "outcome"]
type EvolutionEventKind = string

const (
    // EvolutionEventKindApproval 审批事件
    EvolutionEventKindApproval EvolutionEventKind = "approval"
    // EvolutionEventKindProgress 进度事件
    EvolutionEventKindProgress EvolutionEventKind = "progress"
    // EvolutionEventKindOutcome 结果事件
    EvolutionEventKindOutcome  EvolutionEventKind = "outcome"
)
```

### EvolutionHostEventMeta

对齐 Python：`EvolutionHostEventMeta` (frozen dataclass)

```go
// EvolutionHostEventMeta 演化事件元数据，携带于 OutputSchema.Payload["_evolution_meta"] 中。
// 对齐 Python: EvolutionHostEventMeta
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

// ToPayload 返回 JSON payload 形态，跳过空字段。
// 对齐 Python: EvolutionHostEventMeta.to_payload()
func (m EvolutionHostEventMeta) ToPayload() map[string]string
```

**对齐要点**：
- Python frozen dataclass → Go 值类型 struct（非指针）
- Python `Optional[str]` → Go `*string`
- `ToPayload()` 遍历 7 个可选字段，跳过 nil 值

### EvolutionSnapshot

对齐 Python：`EvolutionSnapshot` (frozen dataclass)

```go
// EvolutionSnapshot 异步演化快照，在回调上下文仍活跃时捕获。
// 对齐 Python: EvolutionSnapshot
type EvolutionSnapshot struct {
    // Trajectory 对话轨迹
    Trajectory *trajectory.Trajectory
    // Messages 消息列表
    Messages []map[string]any
    // SkillName 技能名称（可选）
    SkillName *string
}

// ToLegacyDict 返回供轨道钩子和测试使用的 dict 形态。
// 对齐 Python: EvolutionSnapshot.to_legacy_dict()
func (s EvolutionSnapshot) ToLegacyDict() map[string]any

// FromLegacyDict 从 dict 恢复 EvolutionSnapshot。
// 对齐 Python: EvolutionSnapshot.from_legacy_dict()
func FromLegacyDict(snapshot map[string]any) EvolutionSnapshot
```

**ToLegacyDict 逻辑**：
- 始终包含 `trajectory` 和 `messages` 键
- `skill_name` 仅在非 nil 时包含

**FromLegacyDict 逻辑**：
- `messages` 缺失时默认空切片
- `skill_name` 缺失时为 nil

### EvolutionRequestResult

对齐 Python：`EvolutionRequestResult` (frozen dataclass)

```go
// EvolutionRequestResult 主动用户触发的演化 API 返回的结构化结果。
// 对齐 Python: EvolutionRequestResult
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

// HasChanges 是否有变更。
// 对齐 Python: EvolutionRequestResult.has_changes
func (r EvolutionRequestResult) HasChanges() bool
```

**HasChanges 逻辑**：`len(r.Records) > 0 || r.ApprovalEvent != nil`

### SimplifyRequestResult

对齐 Python：`SimplifyRequestResult` (frozen dataclass)

```go
// SimplifyRequestResult 主动精简请求 API 返回的结构化结果。
// 对齐 Python: SimplifyRequestResult
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

// HasChanges 是否有变更。
// 对齐 Python: SimplifyRequestResult.has_changes
func (r SimplifyRequestResult) HasChanges() bool
```

**HasChanges 逻辑**：`len(r.Actions) > 0 || r.ApprovalEvent != nil`

### ApprovalManager

对齐 Python：`ApprovalManagerProtocol` (Protocol)

```go
// ApprovalManager 审批管理器窄接口。
// 对齐 Python: ApprovalManagerProtocol
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
```

**设计决策**：
- Python 返回 `Any`，Go 返回具体类型 `ExperienceApplyResult`
- `EvolutionApprovalRuntime` 通过 `result.PendingCount` / `result.AppliedCount` 访问字段，
  Python 用 `getattr(result, "pending_count", 0)` 反射访问，Go 直接字段访问

### PendingApprovalSnapshotStore

对齐 Python：`PendingApprovalSnapshotStore = MutableMapping[str, PendingChange]`

```go
// PendingApprovalSnapshotStore 暂存审批快照映射。
// 对齐 Python: PendingApprovalSnapshotStore = MutableMapping[str, PendingChange]
type PendingApprovalSnapshotStore = map[string]*experience.PendingChange
```

---

## approval_events.go

### 辅助函数

```go
// isEn 判断语言是否为英文。
// 对齐 Python: _is_en(language: str) -> bool
func isEn(language string) bool
```

### 导出函数

| Go 函数 | Python 函数 | 签名 | 返回 |
|---------|------------|------|------|
| `BuildProgressEvent` | `build_progress_event` | `(prefix, message string) *stream.OutputSchema` | type=`llm_reasoning`, payload=`{content: prefix+message}` |
| `BuildEvolutionProgressEvent` | `build_evolution_progress_event` | `(railKind, stage, message string, opts ...ProgressEventOption) *stream.OutputSchema` | type=`llm_reasoning`, payload 含 `_evolution_meta` |
| `AttachEvolutionMeta` | `attach_evolution_meta` | `(event *stream.OutputSchema, signalType, signalSource *string) *stream.OutputSchema` | 修改 event.Payload，添加 `_evolution_meta` |
| `BuildSkillApprovalEvent` | `build_skill_approval_event` | `(skillName, requestID string, records []checkpointing.EvolutionRecord, language string, isSharedRecords bool) *stream.OutputSchema` | type=`chat.ask_user_question` |
| `BuildSimplifyApprovalEvent` | `build_simplify_approval_event` | `(skillName, requestID string, actions []map[string]any, language string) *stream.OutputSchema` | type=`chat.ask_user_question` |
| `BuildTeamSkillApprovalEventFromRecords` | `build_team_skill_approval_event_from_records` | `(skillName, requestID, language string, records []checkpointing.EvolutionRecord) *stream.OutputSchema` | type=`chat.ask_user_question` |

### 非导出函数

| Go 函数 | Python 函数 | 签名 |
|---------|------------|------|
| `buildTeamSkillExperienceQuestionEvent` | `_build_team_skill_experience_question_event` | `(skillName, requestID, language string, questions []TeamSkillQuestion) *stream.OutputSchema` |

### ProgressEventOption

`BuildEvolutionProgressEvent` 使用 functional options 模式处理可选参数
（Python 使用 keyword-only args）：

```go
// ProgressEventOption 演化进度事件的可选参数。
type ProgressEventOption func(*progressEventConfig)

// WithSkillName 设置技能名称。
func WithSkillName(skillName string) ProgressEventOption

// WithRequestID 设置请求标识。
func WithRequestID(requestID string) ProgressEventOption

// WithPrefix 设置显示前缀。
func WithPrefix(prefix string) ProgressEventOption
```

### TeamSkillQuestion

`buildTeamSkillExperienceQuestionEvent` 使用的输入结构体：

```go
// TeamSkillQuestion 团队技能审批问题。
type TeamSkillQuestion struct {
    // Section 章节
    Section string
    // Content 内容
    Content string
}
```

### 双语模板

所有 cn/en 字符串**一比一复刻 Python 原文**：

| 函数 | Python 原文 (cn) | Python 原文 (en) |
|------|-----------------|-----------------|
| `BuildSkillApprovalEvent` header | `"在线共享经验审批"` / `"技能演进审批"` | `"Shared Experience Approval"` / `"Skill Evolution Approval"` |
| `BuildSkillApprovalEvent` question | `"**Skill '{skill_name}' 演进生成了新经验：**\n\n- **目标**: {target}\n- **章节**: {section}\n\n{content[:1000]}"` | `"**Skill '{skill_name}' generated a new experience:**\n\n- **Target**: {target}\n- **Section**: {section}\n\n{content[:1000]}"` |
| `BuildSkillApprovalEvent` options | `"接收"/"保留此演进经验"` `"拒绝"/"丢弃此演进经验"` | `"Accept"/"Keep this evolution experience"` `"Reject"/"Discard this evolution experience"` |
| `BuildSimplifyApprovalEvent` question | `"**精简 Skill '{skill_name}' 的演进经验**\n\n共 {n} 项操作：\n{preview}\n\n是否执行？"` | `"**Simplify evolution experiences for Skill '{skill_name}'**\n\n{n} action(s):\n{preview}\n\nDo you want to execute them?"` |
| `BuildSimplifyApprovalEvent` options | `"执行"/"执行精简操作"` `"取消"/"放弃本次精简"` | `"Execute"/"Run the simplify actions"` `"Cancel"/"Discard this simplify request"` |
| `BuildTeamSkillApprovalEventFromRecords` question | `"**团队技能 '{skill_name}' 生成了演进经验：**\n\n- **章节**: {section}\n\n{content[:1000]}"` | `"**Team Skill '{skill_name}' evolution:**\n\n- **Section**: {section}\n\n{content[:1000]}"` |
| `BuildTeamSkillApprovalEventFromRecords` options | `"接收"/"保留此演进经验"` `"拒绝"/"丢弃此演进经验"` | `"Accept"/"Keep this evolution"` `"Reject"/"Discard this evolution"` |

---

## approval_runtime.go

### EvolutionApprovalRuntime

对齐 Python：`EvolutionApprovalRuntime`

```go
// EvolutionApprovalRuntime 共享审批生命周期辅助，绑定到一个轨道实例。
// 对齐 Python: EvolutionApprovalRuntime
type EvolutionApprovalRuntime struct {
    // manager 审批管理器
    manager ApprovalManager
    // pendingApprovalSnapshots 暂存审批快照映射
    pendingApprovalSnapshots PendingApprovalSnapshotStore
}

// NewEvolutionApprovalRuntime 创建审批运行时。
// 对齐 Python: EvolutionApprovalRuntime.__init__
func NewEvolutionApprovalRuntime(manager ApprovalManager, pendingApprovalSnapshots PendingApprovalSnapshotStore) *EvolutionApprovalRuntime
```

### 方法

| Go 方法 | Python 方法 | 签名 | 说明 |
|---------|------------|------|------|
| `LookupPendingApprovalSnapshot` | `lookup_pending_approval_snapshot` | `(requestID, railName, actionName string) *experience.PendingChange` | 未找到时记 Warn 日志并返回 nil |
| `ApprovePendingRequest` | `approve_pending_request` | `(ctx context.Context, requestID, railName, actionName string) (*experience.PendingChange, *experience.ExperienceApplyResult, error)` | pending 为 nil 时返回 nil,nil,nil；部分失败时记 Warn 日志 |
| `RejectPendingRequest` | `reject_pending_request` | `(ctx context.Context, requestID, railName, actionName string) (*experience.PendingChange, *experience.ExperienceApplyResult, error)` | pending 为 nil 时返回 nil,nil,nil |
| `FinalizeStagedEvolutionRequest` | `finalize_staged_evolution_request` | `(request any, requiresApproval bool, emitApprovalRequest func(any) error, onAutoApproved func(any) error) error` | request 为 nil 时直接返回 nil |

**关键对齐决策**：
1. Python `inspect.isawaitable` 分支 → Go 中不存在，回调统一 `func(any) error`，
   调用方在闭包内自行处理异步
2. Python `getattr(result, "pending_count", 0)` → Go 直接访问 `result.PendingCount`
3. Python `tuple[Any, Any]` 返回值 → Go 多返回值 `(pending, result, error)`
4. 日志格式对齐 Python：`[%s] %s: unknown request_id=%s` → `[EvolutionApprovalRuntime] %s: unknown request_id=%s`
5. Python `pending.skill_name` → Go `pending.SkillName`（PendingChange 的 SkillName 字段）

---

## 测试策略

### contracts_test.go

- `TestEvolutionHostEventMeta_ToPayload`：验证必选字段和可选字段的序列化/跳过逻辑
- `TestEvolutionSnapshot_ToLegacyDict`：验证全字段/部分字段的 dict 输出
- `TestEvolutionSnapshot_FromLegacyDict`：验证从 dict 恢复，包括缺失字段默认值
- `TestEvolutionRequestResult_HasChanges`：验证 Records 非空 / ApprovalEvent 非空 / 两者均空
- `TestSimplifyRequestResult_HasChanges`：验证 Actions 非空 / ApprovalEvent 非空 / 两者均空

### approval_events_test.go

- `TestBuildProgressEvent`：验证 payload content 格式
- `TestBuildEvolutionProgressEvent`：验证默认前缀 / 自定义前缀 / 可选字段（skillName/requestID）
- `TestAttachEvolutionMeta`：验证 event_kind 默认值 / signal_type / source 注入
- `TestBuildSkillApprovalEvent_CN`：中文模板对齐验证
- `TestBuildSkillApprovalEvent_EN`：英文模板对齐验证
- `TestBuildSkillApprovalEvent_SharedRecords`：共享记录标记验证
- `TestBuildSimplifyApprovalEvent_CN`：中文精简审批模板验证
- `TestBuildSimplifyApprovalEvent_EN`：英文精简审批模板验证
- `TestBuildTeamSkillApprovalEventFromRecords_CN`：中文团队审批验证
- `TestBuildTeamSkillApprovalEventFromRecords_EN`：英文团队审批验证

### approval_runtime_test.go

- 使用 `fakeApprovalManager` mock 实现 ApprovalManager 接口
- `TestLookupPendingApprovalSnapshot_Found`：找到时返回正确值
- `TestLookupPendingApprovalSnapshot_NotFound`：未找到时返回 nil + 日志
- `TestApprovePendingRequest_Success`：正常审批返回 (pending, result, nil)
- `TestApprovePendingRequest_PartialFailure`：PendingCount > 0 时记 Warn 日志
- `TestApprovePendingRequest_NotFound`：pending 为 nil 时返回 nil,nil,nil
- `TestRejectPendingRequest_Success`：正常拒绝
- `TestRejectPendingRequest_NotFound`：pending 为 nil 时返回 nil,nil,nil
- `TestFinalizeStagedEvolutionRequest_RequiresApproval`：调用 emitApprovalRequest
- `TestFinalizeStagedEvolutionRequest_AutoApproved`：调用 onAutoApproved
- `TestFinalizeStagedEvolutionRequest_NilRequest`：直接返回 nil
- `TestFinalizeStagedEvolutionRequest_NoAutoApprovedCallback`：onAutoApproved 为 nil 时跳过

---

## 预估规模

| 文件 | 行数（估计） |
|------|------------|
| doc.go | ~30 |
| contracts.go | ~160 |
| approval_events.go | ~250 |
| approval_runtime.go | ~120 |
| contracts_test.go | ~180 |
| approval_events_test.go | ~250 |
| approval_runtime_test.go | ~200 |
| **合计** | **~1190** |

---

## 后续阶段依赖

P1 完成后，P2（EvolutionRail 基类 + TrajectoryRail）将依赖：
- `EvolutionHostEventMeta.ToPayload()` — 用于 `_emit_background_outcome_event`
- `EvolutionSnapshot` — 用于 `_snapshot_for_evolution`
- `EvolutionApprovalRuntime` — 被 SkillEvolutionRail / TeamSkillEvolutionRail 组合使用
- `BuildEvolutionProgressEvent` / `AttachEvolutionMeta` — 被 SkillEvolutionRail 使用
