# 9.79 Experience 包设计文档

> 实现章节：9.79 Experience — 在线经验生命周期编排
> Python 对应：`openjiuwen/agent_evolving/experience/`（7 文件）
> Go 包路径：`internal/evolving/experience/`

---

## 1. 背景

### 1.1 在 Agent 会话中的流程位置

Experience 包处于 **在线经验生命周期** 的核心编排层：

```
信号检测 (9.73 SignalDetector)
    ↓
轨迹提取 (9.77 Trajectory)
    ↓
上下文构建 → [9.79 Experience 包] ← 在线演进编排入口
    ↓                        ↓
Updater 生成更新       ExperienceScorer 评分/评估
    ↓                        ↓
execute_updates      ExperienceTracker 追踪展示/评估
    ↓                        ↓
[ExperienceManager] ← 审批生命周期中心
    ↓
  stage → approve/reject → commit 到 EvolutionStore
    ↓
CheckpointManager (9.78) 持久化
```

### 1.2 核心作用

1. **OnlineEvolutionOrchestrator** — 在线演进流水线协调器：组装上下文 → 调用 Updater → 生成 LocalApplyPreview → stage → 可选自动 approve
2. **ExperienceManager** — 生命周期所有者：stage/approve/reject/retry/commit + simplify（经验库整理）+ rebuild（技能重建）
3. **ExperienceScorer** — LLM 驱动的评分器：evaluate（对话片段评估经验有效性）+ simplify（经验库整理建议）+ E/U/F 三维评分公式
4. **ExperienceTracker** — 展示追踪器：record_presented → consume_eval_state → evaluate_presented（周期性评估已展示经验）
5. **类型系统** — PendingChange、EvolutionContext、ExperienceProposal、ExperienceApprovalRequest、OnlineEvolutionResult 等

### 1.3 当前 Go 状态

- `experience/doc.go` — 仅声明包文档
- `experience/types.go` — 仅有一个 `PendingChange = checkpointing.PendingChange` 类型别名
- `checkpointing/types.go` — PendingChange 已定义（9.78 ✅），Trajectory 字段为 `*trajectory.Trajectory`（不是 `any`）

---

## 2. 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 文件组织 | 一比一复刻 7 文件 | 与 Python 对照查找最方便，职责单一 |
| Scorer LLM 调用 | 复用 `llm_resilience.InvokeTextWithRetry` | 与其他 optimizer 一致，避免重复逻辑 |
| 方法异步模式 | 全 `context.Context` + `error` 返回 | Go 最佳实践，支持超时/取消 |
| EvolutionStore 接口适配 | 改造 EvolutionStore 为 ctx 版本 | 风格统一，ctx 真正控制超时，不积累 hack |
| 章节顺序 | 直接做 9.79（先回改 EvolutionStore） | 所有硬依赖已满足，RL/A2A 不阻塞 |

---

## 3. 阶段 1：回改 EvolutionStore 为 ctx 版本

### 3.1 回改策略

EvolutionStore 30+ 个方法，分三类处理：

| 类别 | 方法 | 回改方式 |
|------|------|---------|
| **纯属性（无 I/O）** | `BaseDirs`, `BaseDir` | **不改**，只返回内部字段，无 ctx 无 error |
| **纯读（含 I/O）** | `ListSkillNames`, `SkillExists`, `ResolveSkillDir`, `FindSkillMD`, `LoadEvolutionLog`, `LoadFullEvolutionLog`, `GetPendingRecords`, `GetRecordsByScore`, `ListPendingSummary`, `FormatDescExperienceText`, `FormatAllDescExperiences`, `FormatBodyExperienceText`, `ListSkillNamesWithDescriptions`, `ListArchives`, `ReadSkillID` | 加 `ctx` 参数，返回值不变。Python 中这些方法不会 raise，Go 保持不返回 error |
| **读+可能失败** | `ReadFileText`, `ReadSkillContent`, `ReadPristineSkillContent`, `PackSkillForSharing`, `InstallSkillPackage`, `EnsureSkillID` | 加 `ctx` 参数，原来不返回 error 的现在返回 `(result, error)` |
| **写操作（含 I/O）** | `WriteFileText`, `WriteSkillContent`, `AppendRecord`, `SaveEvolutionLog`, `UpdateRecordScores`, `DeleteRecords`, `MarkRecordsApplied`, `MergeRecords`, `UpdateRecordContent`, `ArchiveSkillBody`, `ArchiveEvolutions`, `ClearEvolutions`, `CreateSkill`, `RenderEvolutionMarkdown` | 加 `ctx` 参数 + 返回 `(result, error)` |

### 3.2 具体签名变更示例

```go
// Before（同步）
func (s *EvolutionStore) AppendRecord(name string, record EvolutionRecord)
func (s *EvolutionStore) LoadFullEvolutionLog(name string) *EvolutionLog
func (s *EvolutionStore) DeleteRecords(name string, recordIDs []string) int
func (s *EvolutionStore) ReadSkillContent(name string) string

// After（ctx 版本）
func (s *EvolutionStore) AppendRecord(ctx context.Context, name string, record EvolutionRecord) error
func (s *EvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) *EvolutionLog  // 纯读不改返回值
func (s *EvolutionStore) DeleteRecords(ctx context.Context, name string, recordIDs []string) (int, error)
func (s *EvolutionStore) ReadSkillContent(ctx context.Context, name string) (string, error)  // 读+可能失败
```

### 3.3 受影响的调用方

| 调用方 | 影响 |
|--------|------|
| `StoreRecordsHelper` | 所有方法需加 ctx，内部调用 store 方法时传 ctx |
| `StoreProjectionHelper` | 同上 |
| `StoreArchiveHelper` | 同上 |
| `DefaultCheckpointManager` | 不直接调用 EvolutionStore，不受影响 |
| `Trainer` | 不直接调用 EvolutionStore，不受影响 |
| `SkillExperienceOperator` | 通过 `execute_updates` 间接使用，不直接调用 store |

### 3.4 回改流程

1. EvolutionStore 自身方法签名改为 ctx 版本
2. StoreRecordsHelper / StoreProjectionHelper / StoreArchiveHelper 方法签名改为 ctx 版本
3. 补充 ctx 参数到已有测试（`state_test.go` 不涉及 store，无需改）
4. 编译验证

---

## 4. 阶段 2：一比一复刻 experience 包 7 个文件

### 4.1 文件对应关系

| Go 文件 | Python 文件 | 核心内容 |
|---------|-----------|---------|
| `types.go` | `types.py` | EvolutionContext / OnlineEvolutionContext / OnlineEvolutionStatus / ExperienceProposal / ExperienceApprovalRequest / OnlineEvolutionResult / ExperienceApplyResult。PendingChange 保持类型别名 |
| `lifecycle.go` | `lifecycle.py` | LocalApplyPreview / PendingCommitResult / HostFacingExperienceResult / RebuildRequest |
| `common.go` | `common.py` | MakePendingChange / RejectPendingChange / CommitPendingChange / ExecuteSimplifyActions / RequestRebuildContext |
| `scorer.go` | `scorer.py` | ExperienceScorer + CalcEffectiveness/CalcUtilization/CalcFreshness/CalcScore/UpdateScore + 双语提示词 + ParseLLMJSON |
| `tracker.go` | `tracker.py` | ExperienceTracker（展示追踪+周期评估） |
| `manager.go` | `skill_experience_manager.py` | ExperienceManager（全生命周期） |
| `orchestrator.go` | `online_orchestrator.py` | OnlineEvolutionOrchestrator（evolve 流水线） |

### 4.2 types.go — 补充类型定义

当前 `types.go` 只有 `PendingChange = checkpointing.PendingChange` 别名，需补充：

```go
// EvolutionContext 在线/离线演进输入上下文。
// 对应 Python: EvolutionContext / OnlineEvolutionContext
type EvolutionContext struct {
    SkillName             string
    Signals               []signal.EvolutionSignal
    SkillContent          string
    Messages              []map[string]any
    ExistingDescRecords   []checkpointing.EvolutionRecord
    ExistingBodyRecords   []checkpointing.EvolutionRecord
    UserQuery             string
    Trajectory            *trajectory.Trajectory
    ExistingScriptRecords []checkpointing.EvolutionRecord
    Metadata              map[string]any
}

// OnlineEvolutionContext 类型别名，对齐 Python。
type OnlineEvolutionContext = EvolutionContext

// OnlineEvolutionStatus 在线演进结果状态（string 常量而非 iota 枚举）。
// 对应 Python: Literal["staged", "auto_approved", ...]
type OnlineEvolutionStatus = string

const (
    OnlineEvolutionStatusStaged             OnlineEvolutionStatus = "staged"
    OnlineEvolutionStatusAutoApproved       OnlineEvolutionStatus = "auto_approved"
    OnlineEvolutionStatusNoEvolutionNoRecords OnlineEvolutionStatus = "no_evolution_no_records"
    OnlineEvolutionStatusSkippedNoInput     OnlineEvolutionStatus = "skipped_no_input"
    OnlineEvolutionStatusSkippedSkillNotFound OnlineEvolutionStatus = "skipped_skill_not_found"
)

// ExperienceProposal 经验提案（审批前）。
type ExperienceProposal struct {
    SkillName       string
    Records         []checkpointing.EvolutionRecord
    RequiresApproval bool
    Source          string
    UserQuery       string
    SignalType      *string
    SignalSource    *string
}

// RecordCount 返回提案中的记录数量。
func (p *ExperienceProposal) RecordCount() int { return len(p.Records) }

// ExperienceApprovalRequest 审批面向视图。
type ExperienceApprovalRequest struct {
    SkillName      string
    Proposal       ExperienceProposal
    PendingChange  *checkpointing.PendingChange
    RequestID      *string
    ApplyResults   []schema.ApplyResult
}

// ToHostResult 返回 host-facing 稳定形态。
func (r *ExperienceApprovalRequest) ToHostResult() HostFacingExperienceResult { ... }

// OnlineEvolutionResult 在线演进编排器返回的结构化结果。
type OnlineEvolutionResult struct {
    SkillName string
    Status    OnlineEvolutionStatus
    Request   *ExperienceApprovalRequest
    Message   string
}

// ExperienceApplyResult 经验变更应用结果。
type ExperienceApplyResult struct {
    SkillName     string
    AppliedCount  int
    RejectedCount int
    PendingCount  int
    Errors        []string
    Metadata      map[string]any
}

// Ok 判断应用结果是否成功。
func (r *ExperienceApplyResult) Ok() bool { return len(r.Errors) == 0 && r.PendingCount == 0 }

// ToHostResult 返回 host-facing 稳定形态。
func (r *ExperienceApplyResult) ToHostResult(requestID, changeType string) HostFacingExperienceResult { ... }
```

### 4.3 lifecycle.go — 新增

```go
// LocalApplyPreview 本地应用预览合约（frozen，Go 用值语义）。
type LocalApplyPreview struct {
    SkillName        string
    Records          []checkpointing.EvolutionRecord
    ApplyResults     []schema.ApplyResult
    ChangeType       string
    LifecycleStage   string  // "local_apply_completed"
}

// PendingCommitResult 暂存变更提交结果。
type PendingCommitResult struct {
    AppliedCount  int
    PendingCount  int
}

// HostFacingExperienceResult host-facing 稳定形态结果合约。
type HostFacingExperienceResult struct {
    SkillName      string
    RequestID      *string
    Effect         string
    ChangeType     string
    AppliedCount   int
    RejectedCount  int
    PendingCount   int
    Status         string  // "pending_approval" / "persisted" / "partial" / "rejected"
    Errors         []string
    Metadata       map[string]any
}

// PendingApproval 工厂方法。
func HostFacingExperienceResultPendingApproval(skillName, requestID, changeType string, pendingCount int) HostFacingExperienceResult

// Persisted 工厂方法。
func HostFacingExperienceResultPersisted(skillName, requestID, changeType string, appliedCount, pendingCount int, errors []string) HostFacingExperienceResult

// Rejected 工厂方法。
func HostFacingExperienceResultRejected(skillName, requestID, changeType string, rejectedCount int) HostFacingExperienceResult

// RebuildRequest 技能重建请求参数。
type RebuildRequest struct {
    SkillName  string
    UserIntent *string
    MinScore   float64
    Metadata   map[string]any
}
```

### 4.4 common.go — 新增

```go
// MakePendingChange 构建暂存演进快照。
func MakePendingChange(ctx context.Context, skillName string, records []checkpointing.EvolutionRecord, opts ...) (*checkpointing.PendingChange, error)

// RejectPendingChange 构建拒绝结果。
func RejectPendingChange(pending *checkpointing.PendingChange) ExperienceApplyResult

// CommitPendingChange 持久化一条暂存变更，失败时保留未写入尾部。
func CommitPendingChange(ctx context.Context, pendingByID map[string]*checkpointing.PendingChange, changeID string, store *checkpointing.EvolutionStore) (PendingCommitResult, error)

// ExecuteSimplifyActions 执行经验库整理操作。
func ExecuteSimplifyActions(ctx context.Context, store *checkpointing.EvolutionStore, skillName string, actions []map[string]any) (map[string]int, error)

// RequestRebuildContext 归档当前状态、过滤重建输入、构建重建提示词。
func RequestRebuildContext(ctx context.Context, store *checkpointing.EvolutionStore, request RebuildRequest, opts ...) (map[string]any, error)
```

**CommitPendingChange 逻辑**（对齐 Python）：
- 遍历 `pending.Payload`，逐条调用 `store.AppendRecord(ctx, skillName, record)`
- 任何一条失败时，保留剩余记录在 `pending.Payload` 中
- 全部成功时，从 `pendingByID` 中移除该 changeID

**ExecuteSimplifyActions 逻辑**（对齐 Python）：
- 遍历 actions，按 action_type 分发：DELETE → `store.DeleteRecords(ctx, ...)`，MERGE → `store.MergeRecords(ctx, ...)`，REFINE → `store.UpdateRecordContent(ctx, ...)`，KEEP → 计数
- 返回 `{"deleted": N, "merged": N, "refined": N, "kept": N, "errors": N}`

**RequestRebuildContext 逻辑**（对齐 Python）：
- `store.ArchiveSkillBody(ctx, skillName)` + `store.ArchiveEvolutions(ctx, skillName)`
- `store.LoadFullEvolutionLog(ctx, skillName)` → 过滤 score < min_score 的记录
- 构建 prompt 文本

### 4.5 scorer.go — 新增

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
    WE = 0.5   // Effectiveness 权重
    WU = 0.3   // Utilization 权重
    WF = 0.2   // Freshness 权重
    FreshnessHalfLifeDays     = 90
    StaleVersionPenalty       = 0.7
)

var (
    EvaluateLLMPolicy = llm_resilience.LLMInvokePolicy{AttemptTimeoutSecs: 60, TotalBudgetSecs: 120, MaxAttempts: 2}
    SimplifyLLMPolicy = llm_resilience.LLMInvokePolicy{AttemptTimeoutSecs: 150, TotalBudgetSecs: 300, MaxAttempts: 2}
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExperienceScorer LLM 驱动的经验评分和整理器。
type ExperienceScorer struct {
    llm             llm.Model
    model           string
    language        string  // "cn" or "en"
    evaluatePolicy  llm_resilience.LLMInvokePolicy
    simplifyPolicy  llm_resilience.LLMInvokePolicy
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CalcEffectiveness 计算 E（Effectiveness）评分，使用贝叶斯平滑 Beta(1,1)。
func CalcEffectiveness(stats *checkpointing.UsageStats) float64

// CalcUtilization 计算 U（Utilization）评分。
func CalcUtilization(stats *checkpointing.UsageStats) float64

// CalcFreshness 计算 F（Freshness）评分，指数衰减 + 版本惩罚。
func CalcFreshness(record *checkpointing.EvolutionRecord, currentSkillVersion *string) float64

// CalcScore 计算综合评分 WE*e + WU*u + WF*f。
func CalcScore(record *checkpointing.EvolutionRecord, currentSkillVersion *string) float64

// UpdateScore 更新记录的 usage_stats 和重新计算评分。
func UpdateScore(record *checkpointing.EvolutionRecord, evalResult map[string]any, currentSkillVersion *string) float64

// NewExperienceScorer 创建 ExperienceScorer。
func NewExperienceScorer(llm llm.Model, model string, language string, opts ...) *ExperienceScorer

// Evaluate 评估展示经验是否被有效使用。
func (s *ExperienceScorer) Evaluate(ctx context.Context, conversationSnippet string, presentedRecords []checkpointing.EvolutionRecord) ([]map[string]any, error)

// Simplify 生成经验库整理建议。
func (s *ExperienceScorer) Simplify(ctx context.Context, skillName string, skillSummary string, records []checkpointing.EvolutionRecord, userIntent *string) ([]map[string]any, error)

// UpdateLLM 热更新 LLM/model。
func (s *ExperienceScorer) UpdateLLM(llm llm.Model, model string)

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseLLMJSON 最佳努力解析 LLM JSON 输出。
func parseLLMJSON(raw string) ([]map[string]any, error)

// formatPresentedExperiences 格式化展示经验用于提示词。
func formatPresentedExperiences(records []checkpointing.EvolutionRecord) string

// formatScoredExperiences 格式化评分经验用于提示词。
func formatScoredExperiences(records []checkpointing.EvolutionRecord) string
```

**双语提示词**：一比一复刻 Python 原文，不做自行翻译：

```go
var ExperienceEvalPromptCN = `你是一个经验评估专家。...` // 对齐 Python EXPERIENCE_EVAL_PROMPT_CN
var ExperienceEvalPromptEN = `You are an experience evaluation expert....` // 对齐 Python EXPERIENCE_EVAL_PROMPT_EN
var ExperienceEvalPrompt = map[string]string{"cn": ExperienceEvalPromptCN, "en": ExperienceEvalPromptEN}

var SimplifyPromptCN = `你是一个经验库维护专家。...` // 对齐 Python SIMPLIFY_PROMPT_CN
var SimplifyPromptEN = `You are an experience library maintenance expert....` // 对齐 Python SIMPLIFY_PROMPT_EN
var SimplifyPrompt = map[string]string{"cn": SimplifyPromptCN, "en": SimplifyPromptEN}
```

**LLM 调用**：
- `Evaluate` 调用 `llm_resilience.InvokeTextWithRetry(ctx, s.llm, s.model, prompt, s.evaluatePolicy, ...)`
- `Simplify` 调用 `llm_resilience.InvokeTextWithRetry(ctx, s.llm, s.model, prompt, s.simplifyPolicy, ...)`

### 4.6 tracker.go — 新增

```go
// PresentedRecordEntry 展示记录条目。
type PresentedRecordEntry struct {
    SkillName string
    Record    checkpointing.EvolutionRecord
    Snippet   string
}

// ExperienceTracker 展示经验追踪器。
type ExperienceTracker struct {
    store        *checkpointing.EvolutionStore
    scorer       *ExperienceScorer
    evalInterval int
}

// RecordPresented 记录展示的 BODY 经验。
func (t *ExperienceTracker) RecordPresented(ctx context.Context, session *session.Session, skillName string, presentationSnippet string) error

// RecordPresentedRecords 记录显式展示的经验记录。
func (t *ExperienceTracker) RecordPresentedRecords(ctx context.Context, session *session.Session, skillName string, presentationSnippet string, recordIDs []string) error

// ConsumeEvalState 消费评估状态（达到评估间隔时返回记录列表）。
func (t *ExperienceTracker) ConsumeEvalState(session *session.Session) []PresentedRecordEntry

// EvaluatePresented 评估展示的经验并更新评分。
func (t *ExperienceTracker) EvaluatePresented(ctx context.Context, presentedEntries []PresentedRecordEntry) error

// ──────────────────────────── 非导出函数 ────────────────────────────

// isBodyRecord 判断是否为 BODY 类型记录。
func isBodyRecord(record *checkpointing.EvolutionRecord) bool
```

**Session 状态存取策略**：
Python 使用 `session._experience_tracker_presented_records` 和 `session._experience_tracker_eval_counter` 属性存取。
Go 中 `session.Session` 是独立的包，experience 包无法向其添加私有字段。

**方案：包级 map 存储 sessionID → entries 映射。**

```go
// tracker 包内部维护两个 map：
var (
    sessionPresentedRecords = map[string][]PresentedRecordEntry{}  // sessionID → entries
    sessionEvalCounter      = map[string]int{}                     // sessionID → counter
)
```

- `RecordPresented/RecordPresentedRecords` 接收 `session *session.Session`，用 `session.SessionID()` 作为 key 存取
- `ConsumeEvalState` 同样用 sessionID 作为 key
- 优点：不需要修改 session 包，不需要循环依赖
- 缺点：map 不随 session 生命周期自动清理（需要调用方在 session 结束时清理，或在 tracker 中提供 ClearSession 方法）

### 4.7 manager.go — 新增

```go
// ExperienceManager 经验生命周期编排器。
type ExperienceManager struct {
    store                    *checkpointing.EvolutionStore
    scorer                   *ExperienceScorer
    kind                     string  // "skill" or "team-skill"
    language                 string  // "cn" or "en"
    skillOps                 map[string]*skill_call.SkillExperienceOperator
    pendingApprovalSnapshots map[string]*checkpointing.PendingChange
    pendingGovernance        map[string]map[string]any
}

// NewExperienceManager 创建 ExperienceManager。
func NewExperienceManager(ctx context.Context, store *checkpointing.EvolutionStore, scorer *ExperienceScorer, kind string, language string, opts ...) (*ExperienceManager, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// StageRecords 暂存一批记录到审批状态。
func (m *ExperienceManager) StageRecords(ctx context.Context, skillName string, records []checkpointing.EvolutionRecord, opts ...) (*ExperienceApprovalRequest, error)

// StageApplyResults 暂存已生成的在线 apply 结果。
func (m *ExperienceManager) StageApplyResults(ctx context.Context, skillName string, applyResults []schema.ApplyResult, opts ...) (*ExperienceApprovalRequest, error)

// ApproveRequest 审批通过暂存变更。
func (m *ExperienceManager) ApproveRequest(ctx context.Context, requestID string) (*ExperienceApplyResult, error)

// RejectRequest 拒绝暂存变更。
func (m *ExperienceManager) RejectRequest(ctx context.Context, requestID string) (*ExperienceApplyResult, error)

// RetryRequest 重试部分应用的暂存变更。
func (m *ExperienceManager) RetryRequest(ctx context.Context, requestID string) (*ExperienceApplyResult, error)

// CommitProposal 通过暂存生命周期持久化提案。
func (m *ExperienceManager) CommitProposal(ctx context.Context, proposal *ExperienceProposal) (*ExperienceApplyResult, error)

// RequestSimplify 发起经验库整理请求。
func (m *ExperienceManager) RequestSimplify(ctx context.Context, skillName string, userIntent *string) (*string, error) // 返回 requestID

// ApproveSimplify 执行暂存的经验库整理操作。
func (m *ExperienceManager) ApproveSimplify(ctx context.Context, requestID string) (map[string]int, error)

// RejectSimplify 丢弃暂存的经验库整理操作。
func (m *ExperienceManager) RejectSimplify(ctx context.Context, requestID string) error

// RequestRebuild 发起技能重建请求。
func (m *ExperienceManager) RequestRebuild(ctx context.Context, skillName string, userIntent *string, minScore float64) (*string, error) // 返回 prompt

// BuildLocalApplyPreview 构建 LocalApplyPreview（静态方法，无 ctx）。
func BuildLocalApplyPreview(skillName string, applyResults []schema.ApplyResult) LocalApplyPreview

// FormatEvolutionRecords 格式化演进记录（静态方法，无 ctx）。
func FormatEvolutionRecords(records []checkpointing.EvolutionRecord, language string) string
```

**双语模板**（一比一复刻 Python）：

```go
// rebuildPromptTemplates 重建提示词模板（双语）。
// 对齐 Python: ExperienceManager._REBUILD_PROMPT_TEMPLATES
var rebuildPromptTemplates = map[string]map[string]string{
    "skill": {
        "cn": "你收到了一个技能的重建请求...",
        "en": "You received a skill rebuild request...",
    },
    "team-skill": {
        "cn": "你收到了一个团队技能的重建请求...",
        "en": "You received a team skill rebuild request...",
    },
}

// defaultRebuildIntents 默认重建意图（双语）。
// 对齐 Python: ExperienceManager._DEFAULT_REBUILD_INTENTS
var defaultRebuildIntents = map[string]map[string]string{
    "skill": {
        "cn": "根据以上演进经验，对技能进行全面优化和重建。",
        "en": "Based on the evolution records above, perform a comprehensive rebuild of the skill.",
    },
    "team-skill": {
        "cn": "根据以上演进经验，对团队技能进行全面优化和重建。",
        "en": "Based on the evolution records above, perform a comprehensive rebuild of the team skill.",
    },
}
```

### 4.8 orchestrator.go — 新增

```go
// OnlineEvolutionOrchestrator 在线演进流水线协调器。
type OnlineEvolutionOrchestrator struct {
    store           *checkpointing.EvolutionStore
    updater         *updater.SingleDimUpdater
    manager         *ExperienceManager
    skillOps        map[string]*skill_call.SkillExperienceOperator
    requestIDPrefix string
    stageSource      string
}

// NewOnlineEvolutionOrchestrator 创建编排器。
func NewOnlineEvolutionOrchestrator(ctx context.Context, store *checkpointing.EvolutionStore, updater *updater.SingleDimUpdater, manager *ExperienceManager, skillOps map[string]*skill_call.SkillExperienceOperator, opts ...) *OnlineEvolutionOrchestrator

// Evolve 执行在线演进并返回结构化结果。
func (o *OnlineEvolutionOrchestrator) Evolve(ctx context.Context, skillName string, signals []signal.EvolutionSignal, opts ...) (*OnlineEvolutionResult, error)

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildContext 构建 EvolutionContext。
func (o *OnlineEvolutionOrchestrator) buildContext(ctx context.Context, ...) (*EvolutionContext, error)

// generateLocalApplyPreview 生成 LocalApplyPreview。
func (o *OnlineEvolutionOrchestrator) generateLocalApplyPreview(ctx context.Context, operator *skill_call.SkillExperienceOperator, onlineContext *EvolutionContext) (*LocalApplyPreview, error)

// getPreferredSignal 获取优先信号。
func getPreferredSignal(onlineContext *EvolutionContext) *signal.EvolutionSignal

// getSignalType 获取信号类型。
func getSignalType(onlineContext *EvolutionContext) *string

// getSignalSource 获取信号来源。
func getSignalSource(onlineContext *EvolutionContext) *string
```

**Evolve 流程**（对齐 Python）：
1. 前置检查：skill_name 和 signals 非空，skill 存在于 store
2. 获取 SkillExperienceOperator（惰性创建）
3. `buildContext`：读取 skill_content、existing_desc/body/script_records
4. `generateLocalApplyPreview`：绑定 updater → process → execute_updates → build preview
5. 如果 preview.records 为空，返回 `no_evolution_no_records`
6. `manager.StageApplyResults(ctx, ...)`
7. 如果 requires_approval → 返回 `staged`；否则 auto-approve → 返回 `auto_approved`

---

## 5. 回填点梳理

| 回填位置 | 内容 |
|---------|------|
| `experience/types.go` | 当前只有 PendingChange 别名，需补充所有类型定义 |
| `experience/doc.go` | 更新文件目录（7 文件）和包描述 |
| `checkpointing/evolution_store.go` | 所有方法加 ctx 参数，写操作加 error 返回 |
| `checkpointing/store_records.go` | 所有方法加 ctx 参数，内部调用 store 时传 ctx |
| `checkpointing/store_projection.go` | 所有方法加 ctx 参数 |
| `checkpointing/store_archive.go` | 所有方法加 ctx 参数 |

## 6. 不需要回填的已完成内容

| 内容 | 原因 |
|------|------|
| `checkpointing/manager.go` | DefaultCheckpointManager 不直接调用 EvolutionStore |
| `trainer/trainer.go` | Trainer 不直接调用 EvolutionStore |
| `update_execution.go` | ExecuteUpdates 调用 operator.ApplyUpdate，不涉及 EvolutionStore |
| `checkpointing/types.go` PendingChange | Trajectory 字段已为 `*trajectory.Trajectory`（不是 `any`），对齐循环依赖消除决策 |

---

## 7. 测试策略

### 7.1 阶段 1 测试

- 回改 EvolutionStore 后编译验证：`go build ./internal/evolving/checkpointing/...`
- 跑通已有测试：`go test ./internal/evolving/checkpointing/...`
- 新增 ctx 参数的集成测试（使用 `t.TempDir()` 创建临时技能目录）

### 7.2 阶段 2 测试

| 文件 | 测试文件 | 核心测试点 |
|------|---------|-----------|
| `types.go` | `types_test.go` | 类型构造、RecordCount、Ok、ToHostResult |
| `lifecycle.go` | `lifecycle_test.go` | HostFacingExperienceResult 工厂方法、LocalApplyPreview 构造 |
| `common.go` | `common_test.go` | MakePendingChange、CommitPendingChange（部分失败保留尾部）、ExecuteSimplifyActions、RequestRebuildContext |
| `scorer.go` | `scorer_test.go` | CalcEffectiveness/Utilization/Freshness/Score/UpdateScore 数学验证、ParseLLMJSON 边界、LLM 调用 mock |
| `tracker.go` | `tracker_test.go` | RecordPresented/RecordPresentedRecords、ConsumeEvalState 间隔逻辑、EvaluatePresented mock scorer |
| `manager.go` | `manager_test.go` | StageRecords/StageApplyResults、Approve/Reject/Retry 生命周期、RequestSimplify/ApproveSimplify、RequestRebuild、FormatEvolutionRecords |
| `orchestrator.go` | `orchestrator_test.go` | Evolve 全流程（mock store+updater+manager）、前置检查（空输入/skill 不存在）、auto-approve 路径 |

### 7.3 LLM 调用 mock

- `ExperienceScorer.Evaluate/Simplify` 使用 `//go:build llm` 标签做真实 LLM 调用测试
- 单元测试中 mock Model 接口（定义 `fakeModel` 实现 `llm.Model`）
- 对齐项目规则：可 mock 的代码禁止使用 build tag 逃避测试

### 7.4 覆盖率目标

整体 ≥ 85%，LLM 真实调用测试用 `//go:build llm` 隔离，不计入基线。
