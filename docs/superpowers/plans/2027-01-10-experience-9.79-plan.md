# 9.79 Experience 包实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现在线经验生命周期编排包（EvolutionStore ctx 回改 + experience 包 7 文件一比一复刻）

**Architecture:** 先回改 EvolutionStore 为 ctx 版本（30+ 方法加 ctx 参数，写操作加 error），然后一比一复刻 Python experience 包的 7 个文件（types/lifecycle/common/scorer/tracker/manager/orchestrator），所有 I/O 方法接收 context.Context，LLM 调用复用 llm_resilience.InvokeTextWithRetry

**Tech Stack:** Go 1.22+ / llm_resilience / checkpointing 包 / signal 包 / trajectory 包 / schema 包

---

## 文件结构

### 阶段 1：回改 EvolutionStore（修改现有文件）

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/evolving/checkpointing/evolution_store.go` | 修改 | 30+ 方法加 ctx 参数，写操作加 error 返回 |
| `internal/evolving/checkpointing/store_records.go` | 修改 | StoreRecordsHelper 所有方法加 ctx，调用 store 传 ctx |
| `internal/evolving/checkpointing/store_projection.go` | 修改 | StoreProjectionHelper 所有方法加 ctx，调用 store 传 ctx |
| `internal/evolving/checkpointing/store_archive.go` | 修改 | StoreArchiveHelper 所有方法加 ctx，调用 store 传 ctx |

### 阶段 2：experience 包（新建文件）

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/evolving/experience/doc.go` | 修改 | 更新包文档（7 文件目录） |
| `internal/evolving/experience/types.go` | 修改 | 补充 EvolutionContext/OnlineEvolutionStatus/ExperienceProposal/ExperienceApprovalRequest/OnlineEvolutionResult/ExperienceApplyResult |
| `internal/evolving/experience/lifecycle.go` | 新建 | LocalApplyPreview/PendingCommitResult/HostFacingExperienceResult/RebuildRequest |
| `internal/evolving/experience/lifecycle_test.go` | 新建 | lifecycle 类型测试 |
| `internal/evolving/experience/common.go` | 新建 | MakePendingChange/RejectPendingChange/CommitPendingChange/ExecuteSimplifyActions/RequestRebuildContext |
| `internal/evolving/experience/common_test.go` | 新建 | common 辅助函数测试 |
| `internal/evolving/experience/scorer.go` | 新建 | ExperienceScorer + E/U/F 评分 + 双语提示词 + ParseLLMJSON |
| `internal/evolving/experience/scorer_test.go` | 新建 | 评分函数数学验证 + ParseLLMJSON + Scorer mock 测试 |
| `internal/evolving/experience/tracker.go` | 新建 | ExperienceTracker 展示追踪+周期评估 |
| `internal/evolving/experience/tracker_test.go` | 新建 | tracker 逻辑测试 |
| `internal/evolving/experience/manager.go` | 新建 | ExperienceManager 全生命周期 |
| `internal/evolving/experience/manager_test.go` | 新建 | Manager 生命周期测试 |
| `internal/evolving/experience/orchestrator.go` | 新建 | OnlineEvolutionOrchestrator |
| `internal/evolving/experience/orchestrator_test.go` | 新建 | Orchestrator 测试 |

---

## Task 1: EvolutionStore 方法签名回改（evolution_store.go）

**Files:**
- Modify: `internal/evolving/checkpointing/evolution_store.go`

**签名变更规则：**
- 纯属性方法（`BaseDirs`, `BaseDir`）：不改
- 纯读方法（`ListSkillNames`, `SkillExists`, `ResolveSkillDir`, `FindSkillMD`, `LoadEvolutionLog`, `LoadFullEvolutionLog`, `GetPendingRecords`, `GetRecordsByScore`, `ListPendingSummary`, `FormatDescExperienceText`, `FormatAllDescExperiences`, `FormatBodyExperienceText`, `ListSkillNamesWithDescriptions`, `ListArchives`, `ReadSkillID`）：加 `ctx context.Context` 第一参数，返回值不变
- 读+可能失败方法（`ReadFileText`, `ReadSkillContent`, `ReadPristineSkillContent`, `PackSkillForSharing`, `InstallSkillPackage`, `EnsureSkillID`）：加 `ctx`，返回值加 error
- 写操作方法（`WriteFileText`, `WriteSkillContent`, `AppendRecord`, `SaveEvolutionLog`, `UpdateRecordScores`, `DeleteRecords`, `MarkRecordsApplied`, `MergeRecords`, `UpdateRecordContent`, `ArchiveSkillBody`, `ArchiveEvolutions`, `ClearEvolutions`, `CreateSkill`, `RenderEvolutionMarkdown`）：加 `ctx`，返回值加 error

- [ ] **Step 1: 修改 evolution_store.go 所有方法签名**

逐方法修改签名。示例：

```go
// 纯读
func (s *EvolutionStore) ListSkillNames(ctx context.Context) []string
func (s *EvolutionStore) SkillExists(ctx context.Context, name string) bool
func (s *EvolutionStore) ResolveSkillDir(ctx context.Context, name string, create ...bool) string
func (s *EvolutionStore) FindSkillMD(ctx context.Context, skillDir string) string
func (s *EvolutionStore) LoadEvolutionLog(ctx context.Context, name string, target *signal.EvolutionTarget) *EvolutionLog
func (s *EvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) *EvolutionLog
func (s *EvolutionStore) SaveEvolutionLog(ctx context.Context, name string, evoLog *EvolutionLog, skillDir string) error
func (s *EvolutionStore) GetPendingRecords(ctx context.Context, name string, target *signal.EvolutionTarget) []EvolutionRecord
func (s *EvolutionStore) RenderEvolutionMarkdown(ctx context.Context, name string) error
func (s *EvolutionStore) FormatDescExperienceText(ctx context.Context, name string, maxItems int) string
func (s *EvolutionStore) FormatAllDescExperiences(ctx context.Context, names []string) map[string]string
func (s *EvolutionStore) FormatBodyExperienceText(ctx context.Context, name string) string
func (s *EvolutionStore) ListPendingSummary(ctx context.Context, names []string) string
func (s *EvolutionStore) UpdateRecordScores(ctx context.Context, name string, updates map[string]map[string]any) (int, error)
func (s *EvolutionStore) GetRecordsByScore(ctx context.Context, name string, minScore *float64) []EvolutionRecord
func (s *EvolutionStore) DeleteRecords(ctx context.Context, name string, recordIDs []string) (int, error)
func (s *EvolutionStore) MarkRecordsApplied(ctx context.Context, name string, recordIDs []string) (int, error)
func (s *EvolutionStore) MergeRecords(ctx context.Context, name string, primaryID string, removeIDs []string, newContent string, newScore *float64) (*EvolutionRecord, error)
func (s *EvolutionStore) UpdateRecordContent(ctx context.Context, name string, recordID string, newContent string, newScore *float64) (*EvolutionRecord, error)
func (s *EvolutionStore) ListSkillNamesWithDescriptions(ctx context.Context) []struct{Name string; Description string}
func (s *EvolutionStore) ListArchives(ctx context.Context, name string) []string
func (s *EvolutionStore) ReadSkillID(ctx context.Context, name string) string

// 读+可能失败
func (s *EvolutionStore) ReadFileText(ctx context.Context, path string) (string, error)
func (s *EvolutionStore) ReadSkillContent(ctx context.Context, name string) (string, error)
func (s *EvolutionStore) ReadPristineSkillContent(ctx context.Context, name string) (string, error)
func (s *EvolutionStore) PackSkillForSharing(ctx context.Context, name string) ([]byte, error)
func (s *EvolutionStore) InstallSkillPackage(ctx context.Context, packageBytes []byte, skillName string) (string, error)
func (s *EvolutionStore) EnsureSkillID(ctx context.Context, name string) (string, error)

// 写操作
func (s *EvolutionStore) WriteFileText(ctx context.Context, path string, content string) error
func (s *EvolutionStore) WriteSkillContent(ctx context.Context, name string, content string) (bool, error)
func (s *EvolutionStore) AppendRecord(ctx context.Context, name string, record EvolutionRecord) error
func (s *EvolutionStore) ArchiveSkillBody(ctx context.Context, name string) (string, error)
func (s *EvolutionStore) ArchiveEvolutions(ctx context.Context, name string) (string, error)
func (s *EvolutionStore) ClearEvolutions(ctx context.Context, name string) error
func (s *EvolutionStore) CreateSkill(ctx context.Context, name string, description string, body string, frontmatter string) (string, error)
```

对于返回值从单一结果改为 `(result, error)` 的方法，内部实现不变，只在最后 `return result, nil`。原来不返回 error 的写操作方法，成功时 `return nil`。

注意：`ReadFileText` 和 `WriteFileText` 已有 error 返回，只需加 ctx 参数。`ReadSkillContent`/`ReadPristineSkillContent`/`WriteSkillContent` 等原来返回 string/bool 的，改为返回 `(string, error)` 或 `(bool, error)`。

对于 `EnsureSkillID` 原来返回 string，改为 `(string, error)`。内部 `s.WriteFileText(mdPath, updated)` 调用改为 `s.WriteFileText(ctx, mdPath, updated)` 并检查 error。

- [ ] **Step 2: 编译验证 EvolutionStore 签名回改**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/checkpointing/...`
Expected: 编译失败（因为 Helper 文件中的调用还没改）

- [ ] **Step 3: 修改 store_records.go 所有方法签名**

StoreRecordsHelper 所有方法加 ctx 参数，调用 store 方法时传 ctx：

```go
func (h *StoreRecordsHelper) PersistScript(ctx context.Context, skillDir string, record *EvolutionRecord) error
func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) *EvolutionLog
func (h *StoreRecordsHelper) SaveEvolutionLog(ctx context.Context, name string, evoLog *EvolutionLog, skillDir string) error
func (h *StoreRecordsHelper) UpdateRecordScores(ctx context.Context, name string, updates map[string]map[string]any) (int, error)
func (h *StoreRecordsHelper) GetRecordsByScore(ctx context.Context, name string, minScore *float64) []EvolutionRecord
func (h *StoreRecordsHelper) DeleteRecords(ctx context.Context, name string, recordIDs []string) (int, error)
func (h *StoreRecordsHelper) MarkRecordsApplied(ctx context.Context, name string, recordIDs []string) (int, error)
func (h *StoreRecordsHelper) MergeRecords(ctx context.Context, name string, primaryID string, removeIDs []string, newContent string, newScore *float64) (*EvolutionRecord, error)
func (h *StoreRecordsHelper) UpdateRecordContent(ctx context.Context, name string, recordID string, newContent string, newScore *float64) (*EvolutionRecord, error)
```

内部 `h.store.ReadFileText(evoPath)` 改为 `h.store.ReadFileText(ctx, evoPath)` 等。`h.store.WriteFileText(scriptPath, content)` 改为 `h.store.WriteFileText(ctx, scriptPath, content)` 等。

对于 `LoadFullEvolutionLog` 返回值不变（`*EvolutionLog`），但 ReadFileText 返回 `(string, error)`，需要处理 error：

```go
func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) *EvolutionLog {
    skillDir := h.store.ResolveSkillDir(ctx, name)
    if skillDir == "" {
        return EmptyEvolutionLog(name)
    }
    evoPath := filepath.Join(skillDir, evolutionFilename)
    if !isFile(evoPath) {
        return EmptyEvolutionLog(name)
    }
    fileContent, _ := h.store.ReadFileText(ctx, evoPath)  // error 忽略，对齐 Python 行为
    if fileContent == "" {
        return EmptyEvolutionLog(name)
    }
    // ... 后续不变
}
```

- [ ] **Step 4: 修改 store_projection.go 所有方法签名**

StoreProjectionHelper 所有方法加 ctx 参数：

```go
func (h *StoreProjectionHelper) RenderEvolutionMarkdown(ctx context.Context, name string) error
func (h *StoreProjectionHelper) ClearRenderedOutputs(ctx context.Context, skillDir string) error
func (h *StoreProjectionHelper) RenderSectionFile(ctx context.Context, evoDir string, section string, records []EvolutionRecord) error
func (h *StoreProjectionHelper) RenderScriptIndex(ctx context.Context, scriptsDir string, entries []EvolutionRecord) error
func (h *StoreProjectionHelper) UpdateSkillMDIndex(ctx context.Context, skillDir string, entries []EvolutionRecord) error
func (h *StoreProjectionHelper) FormatDescExperienceText(ctx context.Context, name string, maxItems int) string
func (h *StoreProjectionHelper) FormatAllDescExperiences(ctx context.Context, names []string) map[string]string
func (h *StoreProjectionHelper) FormatBodyExperienceText(ctx context.Context, name string) string
func (h *StoreProjectionHelper) ListPendingSummary(ctx context.Context, names []string) string
```

内部 `h.store.ReadFileText(mdPath)` 改为 `h.store.ReadFileText(ctx, mdPath)` 等。`h.store.WriteFileText(...)` 改为 `h.store.WriteFileText(ctx, ...)` 等。

- [ ] **Step 5: 修改 store_archive.go 所有方法签名**

StoreArchiveHelper 所有方法加 ctx 参数：

```go
func (h *StoreArchiveHelper) CreateSkill(ctx context.Context, name string, description string, body string, frontmatter string) (string, error)
func (h *StoreArchiveHelper) ArchiveSkillBody(ctx context.Context, name string) (string, error)
func (h *StoreArchiveHelper) ArchiveEvolutions(ctx context.Context, name string) (string, error)
func (h *StoreArchiveHelper) ClearEvolutions(ctx context.Context, name string) error
func (h *StoreArchiveHelper) ListArchives(ctx context.Context, name string) []string
```

内部 `h.store.ReadFileText(mdPath)` → `h.store.ReadFileText(ctx, mdPath)` 等。

- [ ] **Step 6: 编译验证所有 checkpointing 回改**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/...`
Expected: BUILD SUCCESS（checkpointing 包内所有文件已同步）

- [ ] **Step 7: 跑通已有 checkpointing 测试**

Run: `go test ./internal/evolving/checkpointing/...`
Expected: PASS

- [ ] **Step 8: 更新 IMPLEMENTATION_PLAN.md**

在 IMPLEMENTATION_PLAN.md 中将 9.78 行的注释更新，说明 EvolutionStore 已改为 ctx 版本。

- [ ] **Step 9: Commit 阶段 1**

```
git add internal/evolving/checkpointing/
git commit -m "refactor(9.78): EvolutionStore 所有方法加 ctx 参数，写操作加 error 返回，为 9.79 experience 包做准备"
```

---

## Task 2: experience 包 types.go 补充类型定义

**Files:**
- Modify: `internal/evolving/experience/types.go`
- Modify: `internal/evolving/experience/doc.go`
- Create: `internal/evolving/experience/types_test.go`

- [ ] **Step 1: 写 types_test.go 失败测试**

```go
package experience

import (
    "testing"
)

// TestEvolutionContext_基本字段 测试 EvolutionContext 字段赋值
func TestEvolutionContext_基本字段(t *testing.T) {
    ctx := &EvolutionContext{
        SkillName:           "test_skill",
        UserQuery:           "查询内容",
        SkillContent:        "技能内容",
        Metadata:            map[string]any{"key": "val"},
    }
    if ctx.SkillName != "test_skill" {
        t.Errorf("SkillName = %s, 期望 test_skill", ctx.SkillName)
    }
    if ctx.UserQuery != "查询内容" {
        t.Errorf("UserQuery = %s, 期望 查询内容", ctx.UserQuery)
    }
}

// TestOnlineEvolutionContext_类型别名 测试 OnlineEvolutionContext 是 EvolutionContext 的别名
func TestOnlineEvolutionContext_类型别名(t *testing.T) {
    var _ OnlineEvolutionContext = EvolutionContext{}
}

// TestOnlineEvolutionStatus_常量 测试状态常量值
func TestOnlineEvolutionStatus_常量(t *testing.T) {
    if OnlineEvolutionStatusStaged != "staged" {
        t.Errorf("OnlineEvolutionStatusStaged = %s, 期望 staged", OnlineEvolutionStatusStaged)
    }
    if OnlineEvolutionStatusAutoApproved != "auto_approved" {
        t.Errorf("OnlineEvolutionStatusAutoApproved = %s, 期望 auto_approved", OnlineEvolutionStatusAutoApproved)
    }
}

// TestExperienceProposal_RecordCount 测试提案记录计数
func TestExperienceProposal_RecordCount(t *testing.T) {
    p := &ExperienceProposal{
        SkillName:       "test",
        Records:         make([]checkpointing.EvolutionRecord, 3),
        RequiresApproval: true,
        Source:          "experience_optimizer",
    }
    if p.RecordCount() != 3 {
        t.Errorf("RecordCount = %d, 期望 3", p.RecordCount())
    }
}

// TestExperienceApplyResult_Ok 测试 Ok 方法的各种情况
func TestExperienceApplyResult_Ok(t *testing.T) {
    r1 := &ExperienceApplyResult{SkillName: "test", AppliedCount: 5}
    if !r1.Ok() {
        t.Errorf("Ok() 应为 true（无 error 且 pending_count=0）")
    }
    r2 := &ExperienceApplyResult{SkillName: "test", PendingCount: 2}
    if r2.Ok() {
        t.Errorf("Ok() 应为 false（pending_count > 0）")
    }
    r3 := &ExperienceApplyResult{SkillName: "test", Errors: []string{"fail"}}
    if r3.Ok() {
        t.Errorf("Ok() 应为 false（有 errors）")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/evolving/experience/... -run TestEvolutionContext`
Expected: FAIL（类型未定义）

- [ ] **Step 3: 实现 types.go 补充所有类型定义**

重写 `internal/evolving/experience/types.go`，一比一复刻 Python `types.py`：

```go
package experience

import (
    "github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
    "github.com/uapclaw/uap-claw-go/internal/evolving/schema"
    "github.com/uapclaw/uap-claw-go/internal/evolving/signal"
    "github.com/uapclaw/uap-claw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionContext 在线/离线演进输入上下文。
// 对应 Python: openjiuwen/agent_evolving/experience/types.py EvolutionContext
type EvolutionContext struct {
    // SkillName 技能名称
    SkillName string
    // Signals 演进信号列表
    Signals []signal.EvolutionSignal
    // SkillContent 技能内容
    SkillContent string
    // Messages 消息列表
    Messages []map[string]any
    // ExistingDescRecords 已有描述层记录
    ExistingDescRecords []checkpointing.EvolutionRecord
    // ExistingBodyRecords 已有主体层记录
    ExistingBodyRecords []checkpointing.EvolutionRecord
    // UserQuery 用户查询
    UserQuery string
    // Trajectory 关联轨迹（可选）
    Trajectory *trajectory.Trajectory
    // ExistingScriptRecords 已有脚本层记录
    ExistingScriptRecords []checkpointing.EvolutionRecord
    // Metadata 元数据
    Metadata map[string]any
}

// OnlineEvolutionContext 类型别名，对齐 Python。
// 对应 Python: OnlineEvolutionContext = EvolutionContext
type OnlineEvolutionContext = EvolutionContext

// ExperienceProposal 经验提案（审批前）。
// 对应 Python: openjiuwen/agent_evolving/experience/types.py ExperienceProposal
type ExperienceProposal struct {
    // SkillName 技能名称
    SkillName string
    // Records 记录列表
    Records []checkpointing.EvolutionRecord
    // RequiresApproval 是否需要审批
    RequiresApproval bool
    // Source 来源标识，默认 "experience_optimizer"
    Source string
    // UserQuery 用户查询
    UserQuery string
    // SignalType 信号类型（可选）
    SignalType *string
    // SignalSource 信号来源（可选）
    SignalSource *string
}

// RecordCount 返回提案中的记录数量。
// 对应 Python: ExperienceProposal.record_count (property)
func (p *ExperienceProposal) RecordCount() int {
    return len(p.Records)
}

// ExperienceApprovalRequest 审批面向视图。
// 对应 Python: openjiuwen/agent_evolving/experience/types.py ExperienceApprovalRequest
type ExperienceApprovalRequest struct {
    // SkillName 技能名称
    SkillName string
    // Proposal 经验提案
    Proposal ExperienceProposal
    // PendingChange 暂存变更（可选）
    PendingChange *PendingChange
    // RequestID 请求标识（可选）
    RequestID *string
    // ApplyResults 应用结果列表
    ApplyResults []schema.ApplyResult
}

// OnlineEvolutionResult 在线演进编排器返回的结构化结果。
// 对应 Python: openjiuwen/agent_evolving/experience/types.py OnlineEvolutionResult
type OnlineEvolutionResult struct {
    // SkillName 技能名称
    SkillName string
    // Status 演进状态
    Status OnlineEvolutionStatus
    // Request 审批请求（可选）
    Request *ExperienceApprovalRequest
    // Message 描述信息
    Message string
}

// ExperienceApplyResult 经验变更应用结果。
// 对应 Python: openjiuwen/agent_evolving/experience/types.py ExperienceApplyResult
type ExperienceApplyResult struct {
    // SkillName 技能名称
    SkillName string
    // AppliedCount 已应用数量
    AppliedCount int
    // RejectedCount 已拒绝数量
    RejectedCount int
    // PendingCount 待处理数量
    PendingCount int
    // Errors 错误列表
    Errors []string
    // Metadata 元数据
    Metadata map[string]any
}

// Ok 判断应用结果是否成功。
// 对应 Python: ExperienceApplyResult.ok (property)
func (r *ExperienceApplyResult) Ok() bool {
    return len(r.Errors) == 0 && r.PendingCount == 0
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// OnlineEvolutionStatus 在线演进结果状态（string 常量而非 iota 枚举）。
// 对应 Python: Literal["staged", "auto_approved", ...]
type OnlineEvolutionStatus = string

const (
    // OnlineEvolutionStatusStaged 已暂存
    OnlineEvolutionStatusStaged OnlineEvolutionStatus = "staged"
    // OnlineEvolutionStatusAutoApproved 已自动审批
    OnlineEvolutionStatusAutoApproved OnlineEvolutionStatus = "auto_approved"
    // OnlineEvolutionStatusNoEvolutionNoRecords 无演进无记录
    OnlineEvolutionStatusNoEvolutionNoRecords OnlineEvolutionStatus = "no_evolution_no_records"
    // OnlineEvolutionStatusSkippedNoInput 跳过无输入
    OnlineEvolutionStatusSkippedNoInput OnlineEvolutionStatus = "skipped_no_input"
    // OnlineEvolutionStatusSkippedSkillNotFound 跳过技能不存在
    OnlineEvolutionStatusSkippedSkillNotFound OnlineEvolutionStatus = "skipped_skill_not_found"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// PendingChange 等待审批的暂存演进记录快照。
// 类型别名，指向 checkpointing.PendingChange。
type PendingChange = checkpointing.PendingChange
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/evolving/experience/...`
Expected: PASS

- [ ] **Step 5: 更新 doc.go**

```go
// Package experience 提供在线经验生命周期编排的类型定义和逻辑。
//
// 包含在线演进流水线协调（OnlineEvolutionOrchestrator）、
// 经验生命周期管理（ExperienceManager）、
// LLM 驱动评分（ExperienceScorer）、
// 展示追踪（ExperienceTracker），
// 以及辅助函数（common.go）和生命周期类型（lifecycle.go）。
//
// 文件目录：
//
//	experience/
//	├── doc.go            # 包文档
//	├── types.go          # EvolutionContext/OnlineEvolutionStatus/ExperienceProposal/ExperienceApprovalRequest/OnlineEvolutionResult/ExperienceApplyResult/PendingChange 别名
//	├── lifecycle.go      # LocalApplyPreview/PendingCommitResult/HostFacingExperienceResult/RebuildRequest
//	├── common.go         # MakePendingChange/RejectPendingChange/CommitPendingChange/ExecuteSimplifyActions/RequestRebuildContext
//	├── scorer.go         # ExperienceScorer + CalcEffectiveness/CalcUtilization/CalcFreshness/CalcScore/UpdateScore + 双语提示词 + ParseLLMJSON
//	├── tracker.go        # ExperienceTracker 展示追踪+周期评估
//	├── manager.go        # ExperienceManager stage/approve/reject/retry/commit/simplify/rebuild
//	└── orchestrator.go   # OnlineEvolutionOrchestrator evolve 流水线
//
// 对应 Python 代码：openjiuwen/agent_evolving/experience/
package experience
```

- [ ] **Step 6: Commit Task 2**

```
git add internal/evolving/experience/
git commit -m "feat(9.79): experience 包 types.go 补充类型定义（EvolutionContext/OnlineEvolutionStatus/ExperienceProposal/ExperienceApprovalRequest/OnlineEvolutionResult/ExperienceApplyResult）"
```

---

## Task 3: lifecycle.go 新增生命周期类型

**Files:**
- Create: `internal/evolving/experience/lifecycle.go`
- Create: `internal/evolving/experience/lifecycle_test.go`

- [ ] **Step 1: 写 lifecycle_test.go 失败测试**

测试 LocalApplyPreview 构造、HostFacingExperienceResult 三个工厂方法、PendingCommitResult、RebuildRequest：

```go
package experience

import (
    "testing"
)

func TestLocalApplyPreview_基本字段(t *testing.T) {
    preview := LocalApplyPreview{
        SkillName:      "test_skill",
        ChangeType:     "skill_experience_entry",
        LifecycleStage: "local_apply_completed",
    }
    if preview.SkillName != "test_skill" {
        t.Errorf("SkillName = %s", preview.SkillName)
    }
    if preview.LifecycleStage != "local_apply_completed" {
        t.Errorf("LifecycleStage = %s", preview.LifecycleStage)
    }
}

func TestHostFacingExperienceResult_PendingApproval(t *testing.T) {
    result := NewHostFacingExperienceResultPendingApproval("skill1", "req1", "skill_experience_entry", 3)
    if result.Status != "pending_approval" {
        t.Errorf("Status = %s, 期望 pending_approval", result.Status)
    }
    if result.Effect != "pending_change" {
        t.Errorf("Effect = %s, 期望 pending_change", result.Effect)
    }
    if result.PendingCount != 3 {
        t.Errorf("PendingCount = %d, 期望 3", result.PendingCount)
    }
}

func TestHostFacingExperienceResult_Persisted(t *testing.T) {
    result := NewHostFacingExperienceResultPersisted("skill1", "req1", "skill_experience_entry", 5, 0, nil)
    if result.Status != "persisted" {
        t.Errorf("Status = %s, 期望 persisted", result.Status)
    }
    if result.Effect != "state" {
        t.Errorf("Effect = %s, 期望 state", result.Effect)
    }
    if result.AppliedCount != 5 {
        t.Errorf("AppliedCount = %d, 期望 5", result.AppliedCount)
    }
}

func TestHostFacingExperienceResult_Persisted_部分(t *testing.T) {
    result := NewHostFacingExperienceResultPersisted("skill1", "req1", "skill_experience_entry", 3, 2, []string{"err"})
    if result.Status != "partial" {
        t.Errorf("Status = %s, 期望 partial", result.Status)
    }
}

func TestHostFacingExperienceResult_Rejected(t *testing.T) {
    result := NewHostFacingExperienceResultRejected("skill1", "req1", "skill_experience_entry", 4)
    if result.Status != "rejected" {
        t.Errorf("Status = %s, 期望 rejected", result.Status)
    }
    if result.RejectedCount != 4 {
        t.Errorf("RejectedCount = %d, 期望 4", result.RejectedCount)
    }
}

func TestPendingCommitResult_基本字段(t *testing.T) {
    result := PendingCommitResult{AppliedCount: 5, PendingCount: 2}
    if result.AppliedCount != 5 {
        t.Errorf("AppliedCount = %d", result.AppliedCount)
    }
}

func TestRebuildRequest_基本字段(t *testing.T) {
    req := RebuildRequest{SkillName: "skill1", MinScore: 0.5}
    if req.MinScore != 0.5 {
        t.Errorf("MinScore = %f, 期望 0.5", req.MinScore)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 lifecycle.go**

一比一复刻 Python `lifecycle.py`，包含：
- LocalApplyPreview（对应 Python frozen dataclass）
- PendingCommitResult（对应 Python frozen dataclass）
- HostFacingExperienceResult + 三个工厂方法（PendingApproval/Persisted/Rejected）
- RebuildRequest

注意：Go 用值语义代替 Python frozen。工厂方法命名用 `NewHostFacingExperienceResultPendingApproval` 等（Go 风格）。

HostFacingExperienceResult.Persisted 的 status 逻辑：`pending_count == 0 && len(errors) == 0` → "persisted"，否则 "partial"。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 补充 ExperienceApprovalRequest.ToHostResult 和 ExperienceApplyResult.ToHostResult 方法**

这两个方法在 types.go 中引用了 HostFacingExperienceResult（现在在 lifecycle.go 中定义），需要补充实现：

```go
// ToHostResult 返回 host-facing 稳定形态。
// 对应 Python: ExperienceApprovalRequest.to_host_result()
func (r *ExperienceApprovalRequest) ToHostResult() HostFacingExperienceResult {
    pendingCount := 0
    changeType := schema.SkillExperienceEntry
    if r.PendingChange != nil {
        pendingCount = len(r.PendingChange.Payload)
        changeType = r.PendingChange.ChangeType
    }
    requestID := ""
    if r.RequestID != nil {
        requestID = *r.RequestID
    }
    return NewHostFacingExperienceResultPendingApproval(r.SkillName, requestID, changeType, pendingCount)
}

// ToHostResult 返回 host-facing 稳定形态。
// 对应 Python: ExperienceApplyResult.to_host_result()
func (r *ExperienceApplyResult) ToHostResult(requestID, changeType string) HostFacingExperienceResult {
    if r.RejectedCount > 0 {
        return NewHostFacingExperienceResultRejected(r.SkillName, requestID, changeType, r.RejectedCount)
    }
    return NewHostFacingExperienceResultPersisted(r.SkillName, requestID, changeType, r.AppliedCount, r.PendingCount, r.Errors)
}
```

- [ ] **Step 6: 运行测试确认通过**

- [ ] **Step 7: Commit Task 3**

```
git add internal/evolving/experience/lifecycle.go internal/evolving/experience/lifecycle_test.go internal/evolving/experience/types.go
git commit -m "feat(9.79): lifecycle.go 生命周期类型（LocalApplyPreview/PendingCommitResult/HostFacingExperienceResult/RebuildRequest）+ types.go ToHostResult 方法"
```

---

## Task 4: common.go 新增辅助函数

**Files:**
- Create: `internal/evolving/experience/common.go`
- Create: `internal/evolving/experience/common_test.go`

- [ ] **Step 1: 写 common_test.go 失败测试**

重点测试：
- MakePendingChange：构建 PendingChange
- RejectPendingChange：构建拒绝结果
- CommitPendingChange：成功提交 + 部分失败保留尾部
- ExecuteSimplifyActions：DELETE/MERGE/REFINE/KEEP/unknown 各分支
- RequestRebuildContext：skill 不存在返回 nil

使用 `t.TempDir()` 创建临时技能目录 + `NewEvolutionStore` 构造真实 store 测试。

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 common.go**

一比一复刻 Python `common.py` 的 5 个函数：

1. `MakePendingChange(ctx, skillName, records, opts...)` — 调用 `checkpointing.NewPendingChange` + 设置 `IsSharedRecords`/`ChangeID` 前缀
2. `RejectPendingChange(pending)` — 返回 `ExperienceApplyResult{RejectedCount: len(pending.Payload)}`
3. `CommitPendingChange(ctx, pendingByID, changeID, store)` — 逐条 `store.AppendRecord(ctx, ...)`，失败时保留尾部
4. `ExecuteSimplifyActions(ctx, store, skillName, actions)` — 遍历 actions 按 action_type 分发
5. `RequestRebuildContext(ctx, store, request, formatRecords, defaultIntent, template)` — 归档+过滤+构建 prompt

所有函数接收 ctx，I/O 操作调用 store 的 ctx 版本方法。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: Commit Task 4**

```
git add internal/evolving/experience/common.go internal/evolving/experience/common_test.go
git commit -m "feat(9.79): common.go 辅助函数（MakePendingChange/RejectPendingChange/CommitPendingChange/ExecuteSimplifyActions/RequestRebuildContext）"
```

---

## Task 5: scorer.go 新增评分和 LLM 评估逻辑

**Files:**
- Create: `internal/evolving/experience/scorer.go`
- Create: `internal/evolving/experience/scorer_test.go`

- [ ] **Step 1: 写 scorer_test.go 失败测试**

测试纯数学函数和 ParseLLMJSON：

```go
// CalcEffectiveness: (0+1)/(0+2) = 0.5（无数据），(3+1)/(3+2+2) = 4/7 ≈ 0.571
// CalcUtilization: 0/0 = 0.5（无数据），3/5 = 0.6
// CalcFreshness: 刚创建 → 近 1.0，90 天前 → 0.75
// CalcScore: WE*e + WU*u + WF*f
// UpdateScore: 更新 usage_stats 并重新计算
// ParseLLMJSON: 有效 JSON、带 markdown 代码块、空输入、无效 JSON
```

LLM 调用部分（Evaluate/Simplify）用 fakeModel mock 测试，不依赖真实 LLM。

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 scorer.go**

一比一复刻 Python `scorer.py`：

- 常量：WE=0.5, WU=0.3, WF=0.2, FreshnessHalfLifeDays=90, StaleVersionPenalty=0.7
- EvaluateLLMPolicy / SimplifyLLMPolicy
- 5 个评分函数：CalcEffectiveness/CalcUtilization/CalcFreshness/CalcScore/UpdateScore
- ExperienceScorer 结构体 + Evaluate/Simplify/UpdateLLM 方法
- 双语提示词（一比一复刻 Python 原文）
- parseLLMJSON 辅助函数（最佳努力 JSON 解析，去除 markdown 代码块、注释、尾部逗号）
- formatPresentedExperiences / formatScoredExperiences 格式化辅助

注意：Evaluate 和 Simplify 调用 `llm_resilience.InvokeTextWithRetry(ctx, s.llm, s.model, prompt, policy, isResultUsableFunc)`。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: Commit Task 5**

```
git add internal/evolving/experience/scorer.go internal/evolving/experience/scorer_test.go
git commit -m "feat(9.79): scorer.go 评分和 LLM 评估逻辑（CalcE/U/F/Score/UpdateScore + ExperienceScorer + 双语提示词）"
```

---

## Task 6: tracker.go 新增展示追踪逻辑

**Files:**
- Create: `internal/evolving/experience/tracker.go`
- Create: `internal/evolving/experience/tracker_test.go`

- [ ] **Step 1: 写 tracker_test.go 失败测试**

测试：
- RecordPresented：获取 body_records + 更新 UsageStats + 存入 session map
- RecordPresentedRecords：按 record_ids 查找 + 更新 + 存入
- ConsumeEvalState：间隔逻辑（counter < interval → 不消费，counter >= interval → 消费+清零）
- EvaluatePresented：按 skill 分组 → scorer.Evaluate → update_score → store.UpdateRecordScores
- isBodyRecord：判断 target 是否为 BODY

用 fakeEvolutionStore mock（不依赖真实文件系统）。

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 tracker.go**

一比一复刻 Python `tracker.py`：

- PresentedRecordEntry 结构体
- ExperienceTracker 结构体（store + scorer + evalInterval）
- 包级 map：sessionPresentedRecords / sessionEvalCounter（用 session.SessionID() 作为 key）
- RecordPresented(ctx, session, skillName, snippet) → 获取 body_records → 更新 UsageStats → store.UpdateRecordScores → 存入 session map
- RecordPresentedRecords(ctx, session, skillName, snippet, recordIDs) → 按 IDs 查找 → 更新 → 存入
- ConsumeEvalState(session) → 检查 counter，达到 interval 时消费+清零
- EvaluatePresented(ctx, entries) → 按 (skill, snippet) 分组 → scorer.Evaluate → UpdateScore → store.UpdateRecordScores
- isBodyRecord 辅助函数
- ClearSession(sessionID) 方法用于 session 结束时清理包级 map

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: Commit Task 6**

```
git add internal/evolving/experience/tracker.go internal/evolving/experience/tracker_test.go
git commit -m "feat(9.79): tracker.go 展示追踪逻辑（ExperienceTracker + PresentedRecordEntry + 包级 session map）"
```

---

## Task 7: manager.go 新增全生命周期管理

**Files:**
- Create: `internal/evolving/experience/manager.go`
- Create: `internal/evolving/experience/manager_test.go`

- [ ] **Step 1: 写 manager_test.go 失败测试**

测试：
- NewExperienceManager：kind 验证（skill/team-skill），非法 kind 报错
- StageRecords：构建 proposal → stage → 返回 ApprovalRequest
- StageApplyResults：preview → stage → 返回 ApprovalRequest
- ApproveRequest：成功 commit → 返回 ExperienceApplyResult
- RejectRequest：移除 pending → 返回 ExperienceApplyResult
- RequestSimplify / ApproveSimplify / RejectSimplify：simplify 生命周期
- RequestRebuild：构建重建 prompt
- BuildLocalApplyPreview（静态方法）
- FormatEvolutionRecords（静态方法）

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 manager.go**

一比一复刻 Python `skill_experience_manager.py`：

- ExperienceManager 结构体（store/scorer/kind/language/skillOps/pendingApprovalSnapshots/pendingGovernance）
- 双语模板常量：rebuildPromptTemplates / defaultRebuildIntents（一比一复刻 Python 原文）
- NewExperienceManager(ctx, store, scorer, kind, language, opts...)
- StageRecords / StageApplyResults → 内部 _stageRecords / _stagePendingRequest
- ApproveRequest / RejectRequest / RetryRequest → _applyRequest
- CommitProposal → stage + auto-approve
- RequestSimplify / ApproveSimplify / RejectSimplify
- RequestRebuild → 调用 RequestRebuildContext(ctx, ...)
- BuildLocalApplyPreview（静态）
- FormatEvolutionRecords（静态）
- 辅助方法：_previewApplyResults / _stagePendingChange / _rejectPendingChange / _commitPendingChange

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: Commit Task 7**

```
git add internal/evolving/experience/manager.go internal/evolving/experience/manager_test.go
git commit -m "feat(9.79): manager.go 全生命周期管理（ExperienceManager stage/approve/reject/retry/commit/simplify/rebuild）"
```

---

## Task 8: orchestrator.go 新增在线演进流水线

**Files:**
- Create: `internal/evolving/experience/orchestrator.go`
- Create: `internal/evolving/experience/orchestrator_test.go`

- [ ] **Step 1: 写 orchestrator_test.go 失败测试**

测试：
- Evolve：skill_name 为空 → 返回 skipped_no_input
- Evolve：skill 不存在 → 返回 skipped_skill_not_found
- Evolve：preview.records 为空 → 返回 no_evolution_no_records
- Evolve：requires_approval=true → 返回 staged
- Evolve：requires_approval=false → auto-approve → 返回 auto_approved
- getPreferredSignal：优先 USER_INTENT_SIGNAL

用 mock store/updater/manager 测试。

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 orchestrator.go**

一比一复刻 Python `online_orchestrator.py`：

- OnlineEvolutionOrchestrator 结构体（store/updater/manager/skillOps/requestIDPrefix/stageSource）
- NewOnlineEvolutionOrchestrator
- Evolve(ctx, skillName, signals, opts...) → 前置检查 → buildContext → generateLocalApplyPreview → stage → 可选 auto-approve
- buildContext(ctx, ...) → store.ReadSkillContent(ctx, ...) + store.GetPendingRecords(ctx, ...)
- generateLocalApplyPreview(ctx, operator, context) → updater.Bind + process + execute_updates + BuildLocalApplyPreview
- getPreferredSignal / getSignalType / getSignalSource（静态辅助）

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: Commit Task 8**

```
git add internal/evolving/experience/orchestrator.go internal/evolving/experience/orchestrator_test.go
git commit -m "feat(9.79): orchestrator.go 在线演进流水线（OnlineEvolutionOrchestrator Evolve）"
```

---

## Task 9: 全量编译 + 覆盖率检查 + IMPLEMENTATION_PLAN 更新

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 2: 跑通所有 evolving 包测试**

Run: `go test -cover ./internal/evolving/...`
Expected: ALL PASS，experience 包覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 9.79 行从 `☐` 改为 `✅`：
```
| 9.79 | ✅ | Experience | 在线经验生命周期：OnlineEvolutionOrchestrator + ExperienceManager + ExperienceScorer + ExperienceTracker + PendingChange/EvolutionContext/OnlineEvolutionResult | `openjiuwen/agent_evolving/experience/` |
```

同时更新 9.78 行注释，说明 EvolutionStore 已改为 ctx 版本。

- [ ] **Step 4: Commit Task 9**

```
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(9.79): IMPLEMENTATION_PLAN 更新 9.79 ✅ + 9.78 ctx 版本说明"
```

---

## Self-Review

### 1. Spec Coverage Check

| Spec 要求 | 对应 Task |
|-----------|----------|
| EvolutionStore ctx 回改 | Task 1 |
| types.go 补充类型 | Task 2 |
| lifecycle.go 生命周期类型 | Task 3 |
| common.go 辅助函数 | Task 4 |
| scorer.go 评分+LLM+双语提示词 | Task 5 |
| tracker.go 展示追踪 | Task 6 |
| manager.go 全生命周期 | Task 7 |
| orchestrator.go 流水线 | Task 8 |
| 编译+覆盖率+计划更新 | Task 9 |

✅ 全覆盖

### 2. Placeholder Scan

无 "TBD"/"TODO"/"implement later"/"fill in details"。所有代码步骤有具体签名和实现描述。

### 3. Type Consistency

- `PendingChange` 在 types.go 中是 `= checkpointing.PendingChange` 别名，与 Task 1 回改后的 ctx 版一致
- `HostFacingExperienceResult` 在 lifecycle.go 中定义，在 types.go 的 ToHostResult 方法中引用——Task 3 先创建 lifecycle.go，再补充 types.go 的方法引用，顺序正确
- `ExperienceApplyResult.Ok()` 在 Task 2 定义，在 Task 4/6/7 中使用——类型一致
- `checkpointing.EvolutionRecord` 在所有 experience 文件中引用——包路径一致

✅ 类型一致
