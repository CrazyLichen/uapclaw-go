# 9.78 EvolveCheckpoint 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整实现 9.78 EvolveCheckpoint 包（checkpointing），回填 Trainer 5处占位，并提前定义 PendingChange

**Architecture:** 扁平单包 `internal/evolving/checkpointing/`，按 Python 文件名拆分 Go 文件。EvolutionStore 组合三个 Helper（Records/Projection/Archive）作为核心 IO 门面。同步方法 + sync.RWMutex 并发控制。SysOperation 可选注入。TrainableAgent 从 trainer 迁移到 evolving 根包解决循环依赖。

**Tech Stack:** Go 1.22+, sync.RWMutex, tar/archive, encoding/json, os/fs

---

## 文件结构映射

| 新建文件 | 职责 |
|----------|------|
| `internal/evolving/experience/doc.go` | experience 包文档（最小占位） |
| `internal/evolving/experience/types.go` | PendingChange 定义 |
| `internal/evolving/trainable_agent.go` | TrainableAgent 从 trainer 迁移至此 |
| `internal/evolving/checkpointing/doc.go` | checkpointing 包文档 |
| `internal/evolving/checkpointing/state.go` | EvolveCheckpoint |
| `internal/evolving/checkpointing/types.go` | UsageStats/EvolutionPatch/EvolutionRecord/EvolutionLog |
| `internal/evolving/checkpointing/manager.go` | CheckpointManager interface + DefaultCheckpointManager |
| `internal/evolving/checkpointing/store_file.go` | FileCheckpointStore |
| `internal/evolving/checkpointing/evolution_store.go` | EvolutionStore 门面 |
| `internal/evolving/checkpointing/store_records.go` | StoreRecordsHelper |
| `internal/evolving/checkpointing/store_projection.go` | StoreProjectionHelper |
| `internal/evolving/checkpointing/store_archive.go` | StoreArchiveHelper |
| `internal/evolving/checkpointing/skill_package.go` | 打包/解包/skill_id 纯函数 |

| 修改文件 | 改动 |
|----------|------|
| `internal/evolving/trainer/trainer.go` | 删除 TrainableAgent 定义，引用 evolving.TrainableAgent，回填 5处占位 |
| `internal/evolving/trainer/progress.go` | TrainableAgent 引用改为 evolving.TrainableAgent |
| `internal/evolving/trainer/trainer_test.go` | TrainableAgent 引用改为 evolving.TrainableAgent |
| `internal/evolving/doc.go` | 更新子包列表 |
| `internal/evolving/trainer/doc.go` | 更新文件目录 |
| `IMPLEMENTATION_PLAN.md` | 9.78 ☐ → ✅ |

---

### Task 1: TrainableAgent 迁移到 evolving 根包

**Files:**
- Create: `internal/evolving/trainable_agent.go`
- Modify: `internal/evolving/trainer/trainer.go:22-38` (删除 TrainableAgent 定义)
- Modify: `internal/evolving/trainer/progress.go:79-86` (TrainableAgent 引用)
- Modify: `internal/evolving/trainer/trainer_test.go:3-12,16-36` (引用 + fakeTrainableAgent)
- Modify: `internal/evolving/trainer/doc.go` (文件目录更新)
- Modify: `internal/evolving/doc.go` (子包列表)

- [ ] **Step 1: 创建 trainable_agent.go**

```go
// internal/evolving/trainable_agent.go
package evolving

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枌举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TrainableAgent 可训练 Agent 接口。
//
// Trainer 需要通过 Agent 获取 Operator 注册表和执行推理，
// 这是 BaseAgent 的最小扩展接口。
//
// 对应 Python: BaseAgent + get_operators() 方法
type TrainableAgent interface {
	// Invoke 非流式调用 Agent。
	// 对应 Python: BaseAgent.invoke(inputs, session)
	Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
	// Card 返回 Agent 身份卡片。
	// 对应 Python: BaseAgent.card 属性
	Card() *agentschema.AgentCard
	// GetOperators 获取 Operator 注册表。
	// 对应 Python: BaseAgent.get_operators()
	GetOperators() map[string]operator.Operator
}
```

注意：此文件属于 evolving 根包，文件名 `trainable_agent.go`。

- [ ] **Step 2: 修改 trainer.go — 删除 TrainableAgent 定义，添加 import**

删除 trainer.go 中 L22-38 的 TrainableAgent 定义和 import 中的 agentinterfaces/agentschema/operator（这些现在由 evolving 根包导入）。在 trainer.go 添加：

```go
import "github.com/uapclaw/uapclaw-go/internal/evolving"
```

trainer.go 中所有使用 `TrainableAgent` 的地方改为 `evolving.TrainableAgent`。

- [ ] **Step 3: 修改 progress.go — TrainableAgent 引用**

progress.go 的 `TrainCallbackFunc` 和 `TrainEpochBeginFunc` 使用了 `TrainableAgent`。需要在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/evolving"`，将 `TrainableAgent` 改为 `evolving.TrainableAgent`。

- [ ] **Step 4: 修改 trainer_test.go — TrainableAgent 引用**

trainer_test.go 的 `fakeTrainableAgent` 和测试代码使用 `TrainableAgent`。需要在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/evolving"`，将 `TrainableAgent` 改为 `evolving.TrainableAgent`。注意 trainer_test.go 是同包测试，不需要额外 import trainer 包，但需要 import evolving 包。

- [ ] **Step 5: 更新 trainer/doc.go 文件目录**

在 trainer/doc.go 中添加说明 TrainableAgent 已迁移至 `evolving/trainable_agent.go`，文件目录树中标注。

- [ ] **Step 6: 更新 evolving/doc.go 子包列表**

在 evolving/doc.go 的子包列表中添加 `checkpointing/` 和 `experience/` 条目。

- [ ] **Step 7: 编译验证**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; go build ./internal/evolving/...
```

Expected: 编译成功，无错误

- [ ] **Step 8: 运行 trainer 测试**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/evolving/trainer/... -v -count=1
```

Expected: 所有已有测试通过

- [ ] **Step 9: 提交**

```bash
git add internal/evolving/trainable_agent.go internal/evolving/trainer/ internal/evolving/doc.go
git commit -m "feat(evolving): 迁移 TrainableAgent 到 evolving 根包，解决循环依赖"
```

---

### Task 2: 前置定义 — experience 包 + PendingChange

**Files:**
- Create: `internal/evolving/experience/doc.go`
- Create: `internal/evolving/experience/types.go`

- [ ] **Step 1: 创建 experience/doc.go**

```go
// Package experience 提供在线经验生命周期编排的类型定义。
//
// 当前仅定义 PendingChange 结构体（供 9.78 checkpointing 引用），
// 其余类型（ExperienceProposal/ExperienceApprovalRequest/OnlineEvolutionResult 等）
// 将在 9.79 完整实现时补充。
//
// 文件目录：
//
//	experience/
//	├── doc.go   # 包文档
//	├── types.go # PendingChange 及相关类型
//
// 对应 Python 代码：openjiuwen/agent_evolving/experience/types.py
package experience
```

- [ ] **Step 2: 创建 experience/types.go**

```go
package experience

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingChange 等待审批的暂存演进记录快照。
//
// 当优化器生成 EvolutionPatch 并经 Operator 预览后，
// 变更以 PendingChange 形式暂存于 DefaultCheckpointManager._pending 中，
// 等待 ExperienceManager 审批后 commit 到 EvolutionStore。
//
// 对应 Python: openjiuwen/agent_evolving/experience/types.py PendingChange
type PendingChange struct {
	// OperatorID Operator 标识符，格式: skill_experience_{skill_name}
	OperatorID string
	// SkillName 技能名称
	SkillName string
	// ChangeType 变更类型，默认 "skill_experience_entry"
	ChangeType string
	// Payload 暂存的演进记录列表
	Payload []checkpointing.EvolutionRecord
	// CreatedAt 创建时间（UTC ISO 格式）
	CreatedAt string
	// ChangeID 变更标识，格式: skill_evolve_{uuid8}
	ChangeID string
	// IsSharedRecords 是否为共享记录
	IsSharedRecords bool
	// Trajectory 关联轨迹（可选）
	Trajectory any
	// Messages 关联消息列表（可选）
	Messages []map[string]any
}

// ──────────────────────────── 枌举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPendingChange 创建 PendingChange 的工厂方法。
//
// 对应 Python: PendingChange.make(skill_name, records, *, trajectory, messages)
func NewPendingChange(
	skillName string,
	records []checkpointing.EvolutionRecord,
	trajectory any,
	messages []map[string]any,
) *PendingChange {
	return &PendingChange{
		OperatorID: fmt.Sprintf("skill_experience_%s", skillName),
		SkillName:  skillName,
		ChangeType: schema.SkillExperienceEntry,
		Payload:    records,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		ChangeID:   fmt.Sprintf("skill_evolve_%s", generateUUID8()),
		Trajectory: trajectory,
		Messages:   messages,
	}
}

// NewPendingChangeForSharedRecords 创建共享记录的 PendingChange。
//
// 对应 Python: PendingChange.make_for_shared_records(skill_name, records, *, trajectory, messages)
func NewPendingChangeForSharedRecords(
	skillName string,
	records []checkpointing.EvolutionRecord,
	trajectory any,
	messages []map[string]any,
) *PendingChange {
	pc := NewPendingChange(skillName, records, trajectory, messages)
	pc.IsSharedRecords = true
	return pc
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateUUID8 生成 8 位 UUID hex。
func generateUUID8() string {
	// 使用 crypto/rand 生成 4 字节随机数，转 hex 得 8 字符
	// 此处简化为 time+counter 模式，后续可用 crypto/rand
	return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
}
```

注意：`generateUUID8` 此处用简化实现。后续可改用 crypto/rand。Python 中用 `uuid.uuid4().hex[:8]`。

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/experience/...
```

Expected: 此时会失败，因为 experience 引用了尚未创建的 checkpointing 包。先跳过，Task 3 创建 checkpointing 后再验证。

- [ ] **Step 4: 提交（与 Task 3 合并）**

---

### Task 3: checkpointing 核心数据类型 — state.go + types.go

**Files:**
- Create: `internal/evolving/checkpointing/doc.go`
- Create: `internal/evolving/checkpointing/state.go`
- Create: `internal/evolving/checkpointing/types.go`
- Test: `internal/evolving/checkpointing/state_test.go`
- Test: `internal/evolving/checkpointing/types_test.go`

- [ ] **Step 1: 创建 checkpointing/doc.go**

```go
// Package checkpointing 提供训练检查点保存/恢复和技能演进数据文件系统 IO。
//
// 本包是自演化框架的持久化层，负责：
//   - 断点续训：CheckpointManager 在关键节点保存 EvolveCheckpoint，
//     Trainer 下次启动时 ResumeIfNeeded 恢复状态
//   - 演进数据持久化：EvolutionStore 是技能文件系统的 IO 门面，
//     管理演进记录的 CRUD、Markdown 投影渲染、归档治理
//   - 待定变更缓冲：DefaultCheckpointManager._pending 在内存中暂存变更
//
// 文件目录：
//
//	checkpointing/
//	├── doc.go               # 包文档
//	├── state.go             # EvolveCheckpoint 训练检查点数据类
//	├── types.go             # UsageStats/EvolutionPatch/EvolutionRecord/EvolutionLog 及序列化
//	├── manager.go           # CheckpointManager 接口 + DefaultCheckpointManager
//	├── store_file.go        # FileCheckpointStore 本地 JSON 检查点存储
//	├── evolution_store.go   # EvolutionStore 核心 IO 门面
//	├── store_records.go     # StoreRecordsHelper 记录 CRUD 持久化
//	├── store_projection.go  # StoreProjectionHelper Markdown 投影渲染
//	├── store_archive.go     # StoreArchiveHelper 归档/清空/创建技能
//	├── skill_package.go     # 打包/解包/skill_id 纯函数
//
// 对应 Python 代码：openjiuwen/agent_evolving/checkpointing/
package checkpointing
```

- [ ] **Step 2: 创建 checkpointing/state.go**

完整复刻 Python `state.py` 的 EvolveCheckpoint dataclass：

```go
package checkpointing

// ──────────────────────────── 结构体 ────────────────────────────

// EvolveCheckpoint 训练检查点，用于断点续训。
//
// 保存训练过程中的完整状态快照，包括运行标识、步数进度、
// 最佳指标、各 Operator 状态、更新器和搜索器状态等。
// Trainer 通过 FileCheckpointStore 持久化此数据，
// 并在 ResumeIfNeeded 时恢复。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/state.py EvolveCheckpoint
type EvolveCheckpoint struct {
	// Version 检查点版本标签，默认 "v1"
	Version string
	// RunID 运行标识
	RunID string
	// Step 当前步数 {"epoch": int, "batch": int}
	Step map[string]int
	// Best 最佳指标 {"best_score": float}
	Best map[string]any
	// Seed 随机种子
	Seed *int
	// OperatorsState 各 Operator 的状态快照 operator_id → state dict
	OperatorsState map[string]map[string]any
	// UpdaterState 更新器状态
	UpdaterState map[string]any
	// SearcherState 搜索器状态
	SearcherState map[string]any
	// LastMetrics 上一次指标 {"current_epoch_score": float}
	LastMetrics map[string]any
}
```

- [ ] **Step 3: 创建 checkpointing/types.go**

完整复刻 Python `types.py` 的 UsageStats/EvolutionPatch/EvolutionRecord/EvolutionLog，包括序列化方法和工厂方法：

```go
package checkpointing

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UsageStats 演进经验的使用统计。
//
// 记录演进记录被展示、使用、正/负反馈的次数，
// 用于经验评分和经验淘汰决策。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py UsageStats
type UsageStats struct {
	// TimesPresented 展示次数
	TimesPresented int
	// TimesUsed 使用次数
	TimesUsed int
	// TimesPositive 正反馈次数
	TimesPositive int
	// TimesNegative 负反馈次数
	TimesNegative int
	// LastPresentedAt 最后展示时间（UTC ISO）
	LastPresentedAt *string
	// LastEvaluatedAt 最后评估时间（UTC ISO）
	LastEvaluatedAt *string
}

// EvolutionPatch 单次生成的演进变更。
//
// 由优化器（Optimizer）生成，描述对 SKILL.md 的一个具体变更，
// 包括目标 section、动作类型和变更内容。
// action ∈ VALID_PATCH_ACTIONS (append/merge/replace/skip)
// section ∈ VALID_SECTIONS (Instructions/Examples/Troubleshooting/Scripts 等)
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py EvolutionPatch
type EvolutionPatch struct {
	// Section 目标 section (Instructions/Examples/Troubleshooting/Scripts)
	Section string
	// Action 动作 (append/merge/replace/skip)
	Action string
	// Content 变更内容
	Content string
	// Target 演化目标层 (description/body/script)
	Target signal.EvolutionTarget
	// SkipReason skip 时的原因（可选）
	SkipReason *string
	// MergeTarget merge 时替换的目标 record ID（可选）
	MergeTarget *string
	// ScriptFilename 脚本文件名（可选，target=script 时）
	ScriptFilename *string
	// ScriptLanguage 脚本语言（可选，target=script 时）
	ScriptLanguage *string
	// ScriptPurpose 脚本用途（可选，target=script 时）
	ScriptPurpose *string
	// Keywords 关键词（可选）
	Keywords []string
	// Summary 摘要（可选）
	Summary *string
}

// EvolutionRecord 单条持久化的演进记录。
//
// 由 EvolutionPatch 封装为完整记录，包含来源、时间戳、
// 评分和使用统计，持久化于技能目录的 evolutions.json。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py EvolutionRecord
type EvolutionRecord struct {
	// ID 记录标识，格式: ev_{uuid8}
	ID string
	// Source 来源标识
	Source string
	// Timestamp UTC ISO 时间戳
	Timestamp string
	// Context 上下文描述
	Context string
	// Change 变更内容（EvolutionPatch）
	Change EvolutionPatch
	// Applied 是否已应用，默认 false
	Applied bool
	// Score 评分，默认 0.6
	Score float64
	// UsageStats 使用统计（可选）
	UsageStats *UsageStats
	// SkillVersion 技能版本（可选）
	SkillVersion *string
	// Summary 摘要（可选）
	Summary *string
}

// EvolutionLog 单个技能的所有演进记录容器。
//
// 持久化于技能目录的 evolutions.json，包含技能标识、
// 版本号、更新时间和记录列表。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py EvolutionLog
type EvolutionLog struct {
	// SkillID 技能标识
	SkillID string
	// Version 版本号，默认 "1.0.0"
	Version string
	// UpdatedAt 更新时间（UTC ISO）
	UpdatedAt string
	// Entries 演进记录列表
	Entries []EvolutionRecord
}

// ──────────────────────────── 枌举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionPatch 创建 EvolutionPatch 并验证。
//
// 验证 action ∈ VALID_PATCH_ACTIONS, target 为合法 EvolutionTarget,
// action != "skip" 时 section ∈ VALID_SECTIONS。
// 对应 Python: EvolutionPatch.__post_init__
func NewEvolutionPatch(section, action, content string, target signal.EvolutionTarget) (*EvolutionPatch, error) {
	if !schema.ValidPatchActions[action] {
		return nil, fmt.Errorf("无效的演进补丁动作: %s", action)
	}
	if action != "skip" && !schema.ValidSections[section] {
		return nil, fmt.Errorf("无效的演进补丁区域: %s", section)
	}
	return &EvolutionPatch{
		Section: section,
		Action:  action,
		Content: content,
		Target:  target,
	}, nil
}

// MakeEvolutionRecord 创建 EvolutionRecord 的工厂方法。
//
// 自动生成 ID (ev_{uuid8}) 和 timestamp (UTC ISO)，
// 初始化 UsageStats 为零值实例。
// 对应 Python: EvolutionRecord.make(source, context, change, *, score, skill_version, summary)
func MakeEvolutionRecord(
	source, context string,
	change EvolutionPatch,
	score float64,
	skillVersion *string,
	summary *string,
) *EvolutionRecord {
	if score == 0 {
		score = 0.6
	}
	return &EvolutionRecord{
		ID:         fmt.Sprintf("ev_%08x", time.Now().UnixNano()&0xFFFFFFFF),
		Source:     source,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Context:    context,
		Change:     change,
		Applied:    false,
		Score:      score,
		UsageStats: &UsageStats{},
		SkillVersion: skillVersion,
		Summary:    summary,
	}
}

// EmptyEvolutionLog 创建空的 EvolutionLog。
//
// 对应 Python: EvolutionLog.empty(skill_id)
func EmptyEvolutionLog(skillID string) *EvolutionLog {
	return &EvolutionLog{
		SkillID:   skillID,
		Version:   "1.0.0",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Entries:   []EvolutionRecord{},
	}
}

// IsPending 判断 EvolutionRecord 是否为待定状态。
//
// 对应 Python: EvolutionRecord.is_pending (property)
func (r *EvolutionRecord) IsPending() bool {
	return !r.Applied
}

// PendingEntries 返回 EvolutionLog 中所有待定记录。
//
// 对应 Python: EvolutionLog.pending_entries (property)
func (l *EvolutionLog) PendingEntries() []EvolutionRecord {
	var result []EvolutionRecord
	for _, entry := range l.Entries {
		if entry.IsPending() {
			result = append(result, entry)
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 添加序列化方法 ToDict/FromDict**

在 types.go 中为每个类型添加 `ToDict()` 和 `FromDict()` 方法，一比一复刻 Python 的 `to_dict()` / `from_dict()`。这些方法较长，在此给出完整实现框架：

**UsageStats.ToDict / FromDict**: 对齐 Python — 非 nil 的 LastPresentedAt/LastEvaluatedAt 才写入 dict。

**EvolutionPatch.ToDict / FromDict**: section/action/content/target(转为 string value) 为必写字段，_OPTIONAL_FIELDS (skip_reason/merge_target/script_filename/script_language/script_purpose) 只在非 nil 时写入。FromDict 中 target 通过 `signal.ParseEvolutionTarget(rawTarget)` 解析。

**EvolutionRecord.ToDict / FromDict**: id/source/timestamp/context/change(调用 patch.ToDict)/applied/score 为必写字段，usage_stats/skill_version/summary 只在非 nil/非空时写入。FromDict 中 change 通过 `FromDictEvolutionPatch(data["change"])` 解析。

**EvolutionLog.ToDict / FromDict**: skill_id/version/updated_at/entries(逐条 ToDict) 为必写字段。

需要在 signal 包中添加 `ParseEvolutionTarget(value string) (EvolutionTarget, error)` 辅助函数（用于从字符串解析枚举值）。

- [ ] **Step 5: 创建 checkpointing/state_test.go**

测试 EvolveCheckpoint 的构造和基本字段访问：

```go
package checkpointing

import "testing"

func TestEvolveCheckpoint_基本构造(t *testing.T) {
	seed := 42
	ckpt := &EvolveCheckpoint{
		Version:         "v1",
		RunID:           "run-001",
		Step:            map[string]int{"epoch": 3, "batch": 0},
		Best:            map[string]any{"best_score": 0.85},
		Seed:            &seed,
		OperatorsState:  map[string]map[string]any{"op1": {"prompt": "hello"}},
		UpdaterState:    map[string]any{"key": "val"},
		SearcherState:   map[string]any{},
		LastMetrics:     map[string]any{"current_epoch_score": 0.75},
	}
	if ckpt.Version != "v1" {
		t.Errorf("期望 Version=v1, 实际=%s", ckpt.Version)
	}
	if ckpt.Step["epoch"] != 3 {
		t.Errorf("期望 epoch=3, 实际=%d", ckpt.Step["epoch"])
	}
	if ckpt.OperatorsState["op1"]["prompt"] != "hello" {
		t.Errorf("期望 op1.prompt=hello, 实际=%v", ckpt.OperatorsState["op1"]["prompt"])
	}
}

func TestEvolveCheckpoint_Seed为nil(t *testing.T) {
	ckpt := &EvolveCheckpoint{Seed: nil}
	if ckpt.Seed != nil {
		t.Error("期望 Seed 为 nil")
	}
}
```

- [ ] **Step 6: 创建 checkpointing/types_test.go**

测试所有四个类型的构造、验证、ToDict/FromDict 往返：

```go
package checkpointing

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

func TestUsageStats_ToDict_FromDict(t *testing.T) {
	presented := "2025-01-01T00:00:00Z"
	us := &UsageStats{
		TimesPresented:  5,
		TimesUsed:       3,
		TimesPositive:   2,
		TimesNegative:   1,
		LastPresentedAt: &presented,
	}
	dict := us.ToDict()
	if dict["times_presented"] != 5 {
		t.Errorf("期望 times_presented=5, 实际=%v", dict["times_presented"])
	}
	if dict["last_presented_at"] != presented {
		t.Errorf("期望 last_presented_at 写入")
	}
	// FromDict 往返
	us2 := FromDictUsageStats(dict)
	if us2.TimesPresented != 5 {
		t.Errorf("往返后 times_presented=5, 实际=%d", us2.TimesPresented)
	}
}

func TestNewEvolutionPatch_有效动作(t *testing.T) {
	patch, err := NewEvolutionPatch("Instructions", "append", "新增指引", signal.EvolutionTargetBody)
	if err != nil {
		t.Errorf("期望无错误, 实际=%v", err)
	}
	if patch.Action != "append" {
		t.Errorf("期望 action=append, 实际=%s", patch.Action)
	}
}

func TestNewEvolutionPatch_无效动作(t *testing.T) {
	_, err := NewEvolutionPatch("Instructions", "invalid", "test", signal.EvolutionTargetBody)
	if err == nil {
		t.Error("期望返回错误")
	}
}

func TestNewEvolutionPatch_skip动作不验证section(t *testing.T) {
	patch, err := NewEvolutionPatch("InvalidSection", "skip", "reason", signal.EvolutionTargetBody)
	if err != nil {
		t.Errorf("skip 动作不应验证 section, 错误=%v", err)
	}
	if patch.SkipReason != nil {
		// skip 的 SkipReason 应在后续设置
	}
}

func TestMakeEvolutionRecord(t *testing.T) {
	patch, _ := NewEvolutionPatch("Instructions", "append", "test", signal.EvolutionTargetBody)
	record := MakeEvolutionRecord("optimizer", "优化指令", *patch, 0.8, nil, nil)
	if record.Applied != false {
		t.Error("期望 Applied=false")
	}
	if record.Score != 0.8 {
		t.Errorf("期望 Score=0.8, 实际=%f", record.Score)
	}
	if !record.ID.HasPrefix("ev_") {
		t.Errorf("期望 ID 以 ev_ 开头, 实际=%s", record.ID)
	}
}

func TestEvolutionRecord_IsPending(t *testing.T) {
	r := &EvolutionRecord{Applied: false}
	if !r.IsPending() {
		t.Error("期望 IsPending=true")
	}
	r.Applied = true
	if r.IsPending() {
		t.Error("期望 IsPending=false")
	}
}

func TestEmptyEvolutionLog(t *testing.T) {
	log := EmptyEvolutionLog("sk_test")
	if log.SkillID != "sk_test" {
		t.Errorf("期望 SkillID=sk_test, 实际=%s", log.SkillID)
	}
	if log.Version != "1.0.0" {
		t.Errorf("期望 Version=1.0.0, 实际=%s", log.Version)
	}
	if len(log.Entries) != 0 {
		t.Errorf("期望 Entries 为空")
	}
}

func TestEvolutionLog_PendingEntries(t *testing.T) {
	log := &EvolutionLog{
		Entries: []EvolutionRecord{
			{ID: "ev1", Applied: false},
			{ID: "ev2", Applied: true},
			{ID: "ev3", Applied: false},
		},
	}
	pending := log.PendingEntries()
	if len(pending) != 2 {
		t.Errorf("期望 2 条 pending, 实际=%d", len(pending))
	}
}

func TestEvolutionRecord_ToDict_FromDict(t *testing.T) {
	patch, _ := NewEvolutionPatch("Instructions", "append", "test content", signal.EvolutionTargetBody)
	record := MakeEvolutionRecord("optimizer", "test context", *patch, 0.7, nil, nil)
	dict := record.ToDict()
	record2, err := FromDictEvolutionRecord(dict)
	if err != nil {
		t.Errorf("FromDict 错误: %v", err)
	}
	if record2.ID != record.ID {
		t.Errorf("往返后 ID 不一致")
	}
	if record2.Change.Section != record.Change.Section {
		t.Errorf("往返后 Change.Section 不一致")
	}
}

func TestEvolutionLog_ToDict_FromDict(t *testing.T) {
	patch, _ := NewEvolutionPatch("Examples", "replace", "new example", signal.EvolutionTargetBody)
	record := MakeEvolutionRecord("optimizer", "test", *patch, 0.6, nil, nil)
	log := &EvolutionLog{
		SkillID:   "sk_test",
		Version:   "1.0.0",
		UpdatedAt: "2025-01-01T00:00:00Z",
		Entries:   []EvolutionRecord{*record},
	}
	dict := log.ToDict()
	log2, err := FromDictEvolutionLog(dict)
	if err != nil {
		t.Errorf("FromDict 错误: %v", err)
	}
	if log2.SkillID != "sk_test" {
		t.Errorf("往返后 SkillID 不一致")
	}
	if len(log2.Entries) != 1 {
		t.Errorf("往返后 Entries 数量不一致")
	}
}
```

- [ ] **Step 7: 在 signal 包添加 ParseEvolutionTarget 辅助函数**

在 `internal/evolving/signal/signal.go` 的导出函数区块添加：

```go
// ParseEvolutionTarget 从字符串解析 EvolutionTarget 枚举值。
//
// 对应 Python: EvolutionTarget(value)
func ParseEvolutionTarget(value string) (EvolutionTarget, error) {
	switch EvolutionTarget(value) {
	case EvolutionTargetDescription, EvolutionTargetBody, EvolutionTargetScript:
		return EvolutionTarget(value), nil
	default:
		return "", fmt.Errorf("无效的演进目标: %s", value)
	}
}
```

- [ ] **Step 8: 编译验证**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/...
```

Expected: 编译成功（experience 和 checkpointing 包都能正确构建）

- [ ] **Step 9: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/evolving/checkpointing/... ./internal/evolving/signal/... -v -count=1
```

Expected: 所有测试通过

- [ ] **Step 10: 提交（包含 Task 2）**

```bash
git add internal/evolving/experience/ internal/evolving/checkpointing/doc.go internal/evolving/checkpointing/state.go internal/evolving/checkpointing/types.go internal/evolving/checkpointing/state_test.go internal/evolving/checkpointing/types_test.go internal/evolving/signal/
git commit -m "feat(evolving): 添加 checkpointing 核心数据类型 (EvolveCheckpoint/UsageStats/EvolutionPatch/EvolutionRecord/EvolutionLog) 和 experience PendingChange 前置定义"
```

---

### Task 4: CheckpointManager 接口 + DefaultCheckpointManager

**Files:**
- Create: `internal/evolving/checkpointing/manager.go`
- Test: `internal/evolving/checkpointing/manager_test.go`

- [ ] **Step 1: 创建 manager.go**

完整复刻 Python `manager.py` 的 CheckpointManager Protocol + DefaultCheckpointManager，包括 should_save/build_checkpoint/restore 和 pending 变更管理：

```go
package checkpointing

import (
	"fmt"
	"math"

	"github.com/uapclaw/uapclaw-go/internal/evolving"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枌举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// CheckpointManager 检查点管理器接口。
//
// 定义检查点保存时机判断、构建和恢复的核心协议。
// DefaultCheckpointManager 是默认实现。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/manager.py CheckpointManager(Protocol)
type CheckpointManager interface {
	// ShouldSave 判断是否应保存检查点。
	ShouldSave(epoch int, improved bool) bool
	// BuildCheckpoint 从 agent 和 progress 构建检查点数据。
	// progress 参数为 any 类型（避免与 trainer 包循环依赖），
	// Trainer 调用时传入 *Progress。
	BuildCheckpoint(agent evolving.TrainableAgent, progress any, updaterState map[string]any) *EvolveCheckpoint
	// Restore 从检查点恢复 agent 状态，返回 progress 恢复信息。
	Restore(agent evolving.TrainableAgent, checkpoint *EvolveCheckpoint) map[string]any
}

// DefaultCheckpointManager 默认检查点管理器实现。
//
// 保存时机：分数提升或每 N 个 epoch。
// 恢复内容：operators_state + progress best/epoch。
// 待定变更管理：内存中的 pending map。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/manager.py DefaultCheckpointManager
type DefaultCheckpointManager struct {
	runID            string
	ckptVersion      string
	saveEveryNEpochs int
	saveOnImprove    bool
	pending          map[string][]*experience.PendingChange
}

// NewDefaultCheckpointManager 创建默认检查点管理器。
func NewDefaultCheckpointManager(
	runID string,
	ckptVersion string,
	saveEveryNEpochs int,
	saveOnImprove bool,
) *DefaultCheckpointManager {
	if runID == "" {
		runID = generateUUID()
	}
	if saveEveryNEpochs < 1 {
		saveEveryNEpochs = 1
	}
	return &DefaultCheckpointManager{
		runID:            runID,
		ckptVersion:      ckptVersion,
		saveEveryNEpochs: saveEveryNEpochs,
		saveOnImprove:    saveOnImprove,
		pending:          make(map[string][]*experience.PendingChange),
	}
}

// RunID 返回运行标识。
func (m *DefaultCheckpointManager) RunID() string {
	return m.runID
}

// ShouldSave 判断是否应保存检查点。
//
// 逻辑：saveOnImprove && improved → true 或 epoch % saveEveryNEpochs == 0 → true
func (m *DefaultCheckpointManager) ShouldSave(epoch int, improved bool) bool {
	if m.saveOnImprove && improved {
		return true
	}
	return epoch%m.saveEveryNEpochs == 0
}

// BuildCheckpoint 从 agent 和 progress 构建检查点数据。
//
// progress 参数为 any，通过反射提取 epoch/batch/bestScore/seed/currentEpochScore。
// 对应 Python: DefaultCheckpointManager.build_checkpoint(agent, progress, updater_state)
func (m *DefaultCheckpointManager) BuildCheckpoint(
	agent evolving.TrainableAgent,
	progress any,
	updaterState map[string]any,
) *EvolveCheckpoint {
	operatorsState := snapshotOperatorsState(agent)

	// 从 progress 提取字段（any 类型，使用类型断言或反射）
	// Trainer 传入 *trainer.Progress，此处通过接口提取
	epoch := extractIntField(progress, "CurrentEpoch", 0)
	batch := extractIntField(progress, "CurrentBatchIter", 0)
	bestScore := extractFloatField(progress, "BestScore", 0.0)
	currentScore := extractFloatField(progress, "CurrentEpochScore", 0.0)
	seed := extractIntPtrField(progress, "Seed")

	return &EvolveCheckpoint{
		Version:         m.ckptVersion,
		RunID:           m.runID,
		Step:            map[string]int{"epoch": epoch, "batch": batch},
		Best:            map[string]any{"best_score": bestScore},
		Seed:            seed,
		OperatorsState:  operatorsState,
		UpdaterState:    updaterState,
		SearcherState:   map[string]any{},
		LastMetrics:     map[string]any{"current_epoch_score": currentScore},
	}
}

// Restore 从检查点恢复 agent 状态，返回 progress 恢复信息。
//
// 恢复所有 Operator 状态，返回 {"start_epoch", "best_score", "run_id"}。
func (m *DefaultCheckpointManager) Restore(
	agent evolving.TrainableAgent,
	checkpoint *EvolveCheckpoint,
) map[string]any {
	restoreOperatorsState(agent, checkpoint.OperatorsState)
	return map[string]any{
		"start_epoch": getIntFromMap(checkpoint.Step, "epoch", 0),
		"best_score":  getFloatFromMap(checkpoint.Best, "best_score", 0.0),
		"run_id":      checkpoint.RunID,
	}
}

// AddPending 添加待定变更到内存存储。
func (m *DefaultCheckpointManager) AddPending(operatorID string, change *experience.PendingChange) {
	m.pending[operatorID] = append(m.pending[operatorID], change)
}

// GetPending 获取某 Operator 的待定变更列表。
func (m *DefaultCheckpointManager) GetPending(operatorID string) []*experience.PendingChange {
	return m.pending[operatorID]
}

// CommitPending 清空并返回 pending payload 中的 EvolutionRecord 总数。
//
// 只清空内存中的待定状态并返回记录计数，不负责写磁盘。
// 调用方应在调用此方法前后通过 store.AppendRecord 持久化。
func (m *DefaultCheckpointManager) CommitPending(operatorID string) int {
	pendingList := m.pending[operatorID]
	delete(m.pending, operatorID)
	count := 0
	for _, change := range pendingList {
		count += len(change.Payload)
	}
	return count
}

// DiscardPending 按 changeID 丢弃特定的待定变更。
func (m *DefaultCheckpointManager) DiscardPending(operatorID, changeID string) {
	list := m.pending[operatorID]
	filtered := make([]*experience.PendingChange, 0, len(list))
	for _, change := range list {
		if change.ChangeID != changeID {
			filtered = append(filtered, change)
		}
	}
	m.pending[operatorID] = filtered
}

// snapshotOperatorsState 快照所有 Operator 的状态。
//
// 对应 Python: DefaultCheckpointManager._snapshot_operators_state(agent)
func snapshotOperatorsState(agent evolving.TrainableAgent) map[string]map[string]any {
	ops := agent.GetOperators()
	if ops == nil {
		return map[string]map[string]any{}
	}
	out := make(map[string]map[string]any, len(ops))
	for _, op := range ops {
		out[op.OperatorID()] = op.GetState()
	}
	return out
}

// restoreOperatorsState 恢复所有 Operator 的状态。
//
// 对应 Python: DefaultCheckpointManager._restore_operators_state(agent, operators_state)
func restoreOperatorsState(agent evolving.TrainableAgent, operatorsState map[string]map[string]any) {
	ops := agent.GetOperators()
	if ops == nil || operatorsState == nil {
		return
	}
	for operatorID, state := range operatorsState {
		op, ok := ops[operatorID]
		if ok {
			op.LoadState(state)
		}
	}
}
```

注意：`extractIntField`/`extractFloatField`/`extractIntPtrField`/`getIntFromMap`/`getFloatFromMap`/`generateUUID` 等辅助函数需要在 manager.go 的非导出函数区块实现。

- [ ] **Step 2: 创建 manager_test.go**

测试 ShouldSave 逻辑、BuildCheckpoint/Restore、Pending 变更管理。需要创建 fakeTrainableAgent（在测试包中）：

```go
package checkpointing

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// fakeTrainableAgent 用于测试
type fakeTrainableAgent struct {
	operators map[string]operator.Operator
}

func (a *fakeTrainableAgent) Invoke(_ context.Context, _ map[string]any, _ ...any) (map[string]any, error) {
	return nil, nil
}
func (a *fakeTrainableAgent) Card() *agentschema.AgentCard { return nil }
func (a *fakeTrainableAgent) GetOperators() map[string]operator.Operator { return a.operators }

// fakeOperator 用于测试
type fakeOperator struct {
	id    string
	state map[string]any
}
func (o *fakeOperator) OperatorID() string                           { return o.id }
func (o *fakeOperator) GetTunables() map[string]operator.TunableSpec { return nil }
func (o *fakeOperator) GetState() map[string]any                     { return o.state }
func (o *fakeOperator) SetParameter(_ string, _ any)                 {}
func (o *fakeOperator) ApplyUpdate(_ string, _ schema.UpdateValue) schema.ApplyResult { return schema.ApplyResult{} }
func (o *fakeOperator) LoadState(state map[string]any)               { o.state = state }

func TestShouldSave_分数提升(t *testing.T) {
	m := NewDefaultCheckpointManager("", "v1", 1, true)
	if !m.ShouldSave(3, true) {
		t.Error("saveOnImprove && improved → 应保存")
	}
}

func TestShouldSave_每N个Epoch(t *testing.T) {
	m := NewDefaultCheckpointManager("", "v1", 5, false)
	if !m.ShouldSave(5, false) {
		t.Error("epoch=5, n=5 → 应保存")
	}
	if m.ShouldSave(3, false) {
		t.Error("epoch=3, n=5 → 不应保存")
	}
}

func TestBuildCheckpoint_And_Restore(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &fakeOperator{id: "op1", state: map[string]any{"prompt": "hello"}},
	}
	agent := &fakeTrainableAgent{operators: ops}

	// 用 trainer.Progress 的字段构造 progress data
	progressData := map[string]any{
		"CurrentEpoch":       3,
		"CurrentBatchIter":   0,
		"BestScore":          0.85,
		"CurrentEpochScore":  0.75,
	}

	m := NewDefaultCheckpointManager("run-001", "v1", 1, true)
	ckpt := m.BuildCheckpoint(agent, progressData, nil)

	if ckpt.Version != "v1" { t.Errorf("期望 v1") }
	if ckpt.Step["epoch"] != 3 { t.Errorf("期望 epoch=3") }

	// Restore
	result := m.Restore(agent, ckpt)
	if result["run_id"] != "run-001" { t.Errorf("期望 run-001") }
	if result["start_epoch"] != 3 { t.Errorf("期望 start_epoch=3") }
}

func TestPendingManagement(t *testing.T) {
	m := NewDefaultCheckpointManager("", "v1", 1, true)
	patch, _ := NewEvolutionPatch("Instructions", "append", "test", signal.EvolutionTargetBody)
	record := MakeEvolutionRecord("optimizer", "test", *patch, 0.6, nil, nil)
	pc := experience.NewPendingChange("my_skill", []EvolutionRecord{*record}, nil, nil)

	m.AddPending("skill_experience_my_skill", pc)
	pending := m.GetPending("skill_experience_my_skill")
	if len(pending) != 1 { t.Errorf("期望 1 条 pending") }

	count := m.CommitPending("skill_experience_my_skill")
	if count != 1 { t.Errorf("期望 count=1") }
	pending2 := m.GetPending("skill_experience_my_skill")
	if len(pending2) != 0 { t.Errorf("期望 commit 后为空") }

	// DiscardPending
	pc2 := experience.NewPendingChange("my_skill2", []EvolutionRecord{*record}, nil, nil)
	m.AddPending("skill_experience_my_skill2", pc2)
	m.DiscardPending("skill_experience_my_skill2", pc2.ChangeID)
	pending3 := m.GetPending("skill_experience_my_skill2")
	if len(pending3) != 0 { t.Errorf("期望 discard 后为空") }
}
```

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/checkpointing/...
```

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/evolving/checkpointing/... -v -count=1
```

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/checkpointing/manager.go internal/evolving/checkpointing/manager_test.go
git commit -m "feat(evolving): 添加 CheckpointManager 接口和 DefaultCheckpointManager 实现"
```

---

### Task 5: FileCheckpointStore

**Files:**
- Create: `internal/evolving/checkpointing/store_file.go`
- Test: `internal/evolving/checkpointing/store_file_test.go`

- [ ] **Step 1: 创建 store_file.go**

完整复刻 Python `store_file.py` 的 FileCheckpointStore，包括 save_checkpoint/load_checkpoint/load_state_dict 和 toJSONCompatible 辅助函数。使用 `encoding/json` 序列化，`os` 包读写文件。

- [ ] **Step 2: 创建 store_file_test.go**

使用 `t.TempDir()` 创建临时目录，测试 SaveCheckpoint → LoadCheckpoint 往返、LoadStateDict、toJSONCompatible 递归处理。

- [ ] **Step 3: 编译 + 测试**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/checkpointing/... && go test ./internal/evolving/checkpointing/... -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/checkpointing/store_file.go internal/evolving/checkpointing/store_file_test.go
git commit -m "feat(evolving): 添加 FileCheckpointStore 本地 JSON 检查点存储"
```

---

### Task 6: skill_package.go 打包/解包/skill_id

**Files:**
- Create: `internal/evolving/checkpointing/skill_package.go`
- Test: `internal/evolving/checkpointing/skill_package_test.go`

- [ ] **Step 1: 创建 skill_package.go**

完整复刻 Python `skill_package.py` 的所有纯函数：NewSkillID/ReadSkillIDFromContent/EnsureSkillIDInContent/PackSkillDirectory/UnpackSkillPackage/ListPackableFiles。排除规则常量 `_excludeDirNames` 和 `_excludeFileNames`。使用 `archive/tar` + `compress/gzip` 打包，`tarfile.data_filter` 等价使用安全路径检查。

- [ ] **Step 2: 创建 skill_package_test.go**

使用 `t.TempDir()` 创建临时技能目录结构，测试：NewSkillID 格式、ReadSkillIDFromContent/EnsureSkillIDInContent frontmatter 解析、PackSkillDirectory 排除规则验证、UnpackSkillPackage 解压验证、ListPackableFiles 列出可打包文件。

- [ ] **Step 3: 编译 + 测试 + 提交**

---

### Task 7: StoreArchiveHelper

**Files:**
- Create: `internal/evolving/checkpointing/store_archive.go`
- Test: `internal/evolving/checkpointing/store_archive_test.go`

- [ ] **Step 1: 创建 store_archive.go**

复刻 Python `store_archive.py` 的 StoreArchiveHelper：CreateSkill（名称校验+mkdir+写SKILL.md+空EvolutionLog）、ArchiveSkillBody/ArchiveEvolutions（带时间戳后缀归档）、ClearEvolutions（写空log+重渲染）、ListArchives（archive/下倒序）、tsSuffix/archiveDir 辅助方法。

- [ ] **Step 2: 创建 store_archive_test.go**

使用 `t.TempDir()` 创建 EvolutionStore 实例（无 SysOperation），测试 CreateSkill 名称校验、ArchiveSkillBody 归档、ArchiveEvolutions 归档、ClearEvolutions 清空、ListArchives 列出。

- [ ] **Step 3: 编译 + 测试 + 提交**

注意：此 Task 需要 EvolutionStore 的基础框架先建好（Task 8），但 Helper 可以先写结构体定义和构造函数，测试中创建最小 EvolutionStore。

---

### Task 8: StoreRecordsHelper

**Files:**
- Create: `internal/evolving/checkpointing/store_records.go`
- Test: `internal/evolving/checkpointing/store_records_test.go`

- [ ] **Step 1: 创建 store_records.go**

复刻 Python `store_records.py` 的 StoreRecordsHelper：persistScript（脚本持久化到 evolution/scripts/）、loadFullEvolutionLog/saveEvolutionLog（evolutions.json 读写）、updateRecordScores/getRecordsByScore/deleteRecords/markRecordsApplied/mergeRecords/updateRecordContent 的 CRUD 方法。语言扩展名映射 `_langToExt`。

- [ ] **Step 2: 创建 store_records_test.go**

使用 `t.TempDir()` + 最小 EvolutionStore，测试：evolutions.json 写入→读取往返、persistScript 脚本持久化、updateRecordScores 分数更新、deleteRecords 删除、markRecordsApplied 标记已应用、mergeRecords 合并记录。

- [ ] **Step 3: 编译 + 测试 + 提交**

---

### Task 9: StoreProjectionHelper

**Files:**
- Create: `internal/evolving/checkpointing/store_projection.go`
- Test: `internal/evolving/checkpointing/store_projection_test.go`

- [ ] **Step 1: 创建 store_projection.go**

复刻 Python `store_projection.py` 的 StoreProjectionHelper：RenderEvolutionMarkdown（核心渲染方法：过滤→按section分组→写md→更新SKILL.md index块）、ClearRenderedOutputs/RenderSectionFile/RenderScriptIndex/UpdateSkillMDIndex、FormatDescExperienceText/FormatAllDescExperiences/FormatBodyExperienceText/ListPendingSummary、静态辅助方法（sectionFilename/recordSummary/normalizeSummaryText/formatExperienceIndexTable/formatScriptAssetsTable/extractDescriptionFromSkillMD）。SKILL.md evolution-index 块格式：`<!-- evolution-index-start -->...<!-- evolution-index-end -->`。

- [ ] **Step 2: 创建 store_projection_test.go**

使用 `t.TempDir()` + 最小 EvolutionStore + 带演进记录的技能目录，测试：RenderEvolutionMarkdown 生成 section md 文件和 SKILL.md index 块、FormatDescExperienceText 格式化、ClearRenderedOutputs 清理。

- [ ] **Step 3: 编译 + 测试 + 提交**

---

### Task 10: EvolutionStore 门面

**Files:**
- Create: `internal/evolving/checkpointing/evolution_store.go`
- Test: `internal/evolving/checkpointing/evolution_store_test.go`

- [ ] **Step 1: 创建 evolution_store.go**

这是核心门面类，组合三个 Helper + SysOperation 路由 + RWMutex。完整复刻 Python `evolution_store.py` 的所有方法。

关键设计：
- 构造函数 `NewEvolutionStore(skillsBaseDir string, sysOp *sys_operation.SysOperation)`
- `_normalizeBaseDirs` 解析分号/逗号分隔多路径
- `skillLocks map[string]*sync.RWMutex` 惰性创建，`getSkillLock` 方法
- `ReadFileText/WriteFileText` 判断 sysOperation 是否注入，有则走 Fs()，否则走 os
- 所有 CRUD 方法在调用 Helper 前获取 RLock/Lock
- 三个 Helper 在构造时初始化

方法清单（对齐 Python）：
- 目录解析：listSkillNames/skillExists/resolveSkillDir/findSkillMD
- 文件读写：ReadFileText/WriteFileText/ReadSkillContent/ReadPristineSkillContent/WriteSkillContent
- skill_id：ReadSkillID/EnsureSkillID
- 打包：PackSkillForSharing/InstallSkillPackage
- CRUD（带锁）：LoadEvolutionLog/LoadFullEvolutionLog/SaveEvolutionLog/AppendRecord/GetPendingRecords/UpdateRecordScores/GetRecordsByScore/DeleteRecords/MarkRecordsApplied/MergeRecords/UpdateRecordContent
- 投影（委托 projection）：RenderEvolutionMarkdown/FormatDescExperienceText/FormatAllDescExperiences/FormatBodyExperienceText/ListPendingSummary
- 归档（委托 archive）：CreateSkill/ListSkillNamesWithDescriptions/ExtractDescriptionFromSkillMD/ArchiveSkillBody/ArchiveEvolutions/ClearEvolutions/ListArchives

- [ ] **Step 2: 创建 evolution_store_test.go**

使用 `t.TempDir()` 创建多技能目录结构，测试：
- 多路径解析（分号/逗号分隔）
- listSkillNames 列出技能
- ReadSkillContent/WriteSkillContent 文件读写
- AppendRecord + LoadEvolutionLog 往返
- RWMutex 并发安全性（多个 goroutine 同时 AppendRecord）
- PackSkillForSharing 打包排除规则
- SysOperation 注入路由（可选：mock SysOperation 的 Fs()）

- [ ] **Step 3: 编译 + 测试 + 提交**

---

### Task 11: Trainer 回填

**Files:**
- Modify: `internal/evolving/trainer/trainer.go`

回填 5 处 `⤵️ 待 9.78 回填` 占位：

- [ ] **Step 1: 替换 checkpointStore 类型**

`checkpointStore any` → `checkpointStore *checkpointing.FileCheckpointStore`

- [ ] **Step 2: 替换 checkpointManager 类型**

`checkpointManager any` → `checkpointManager checkpointing.CheckpointManager`

- [ ] **Step 3: 替换 WithCheckpointManager 参数类型**

`WithCheckpointManager(manager any)` → `WithCheckpointManager(manager checkpointing.CheckpointManager)`

- [ ] **Step 4: 实现 ResumeIfNeeded**

替换空实现：

```go
func (t *Trainer) ResumeIfNeeded(ctx context.Context, agent evolving.TrainableAgent) error {
	if t.checkpointStore == nil || t.checkpointManager == nil || t.resumeFrom == "" {
		return nil
	}
	ckpt, err := t.checkpointStore.LoadCheckpoint(t.resumeFrom)
	if err != nil {
		return fmt.Errorf("加载检查点失败: %w", err)
	}
	if ckpt == nil {
		return nil
	}
	restored := t.checkpointManager.Restore(agent, ckpt)
	// 从 restored 恢复 Progress 状态
	// 注意：Progress 在 Train 方法中创建，此处需要将 restored 信息传递出去
	// 当前设计：ResumeIfNeeded 返回 map，Trainer 在 Train 中使用返回值更新 progress
	_ = restored
	return nil
}
```

注意：ResumeIfNeeded 的签名需要调整——原签名是 `(ctx context.Context, _ any)`，回填后需要改为 `(ctx context.Context, agent evolving.TrainableAgent)`。但 Trainer 中调用点是 `_ = t.ResumeIfNeeded(ctx, agent)`，agent 已经是 `evolving.TrainableAgent` 类型，所以签名可直接改为接收 `evolving.TrainableAgent`。

- [ ] **Step 5: 实现 SaveCheckpointIfNeeded**

替换空实现，对齐 Python：

```go
func (t *Trainer) SaveCheckpointIfNeeded(epoch int, valScore float64, operators map[string]operator.Operator, improved bool) error {
	if t.checkpointStore == nil || t.checkpointManager == nil {
		return nil
	}
	if !t.checkpointManager.ShouldSave(epoch, improved) {
		return nil
	}
	// 构建 updaterState
	var updaterState map[string]any
	if t.updater != nil {
		updaterState = t.updater.GetState()
	}
	ckpt := t.checkpointManager.BuildCheckpoint(agent, progress, updaterState)
	_, err := t.checkpointStore.SaveCheckpoint(ckpt, "latest.json")
	if err != nil {
		return fmt.Errorf("保存检查点失败: %w", err)
	}
	return nil
}
```

注意：SaveCheckpointIfNeeded 需要接收 `agent` 和 `progress` 参数（当前签名只有 epoch/valScore/operators/improved）。需要调整签名。

- [ ] **Step 6: 添加 checkpointing import**

在 trainer.go 的 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"`。

- [ ] **Step 7: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/trainer/...
```

- [ ] **Step 8: 运行 trainer 测试**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/evolving/trainer/... -v -count=1
```

- [ ] **Step 9: 提交**

```bash
git add internal/evolving/trainer/trainer.go
git commit -m "feat(evolving): 回填 Trainer checkpointStore/checkpointManager/ResumeIfNeeded/SaveCheckpointIfNeeded"
```

---

### Task 12: 更新 IMPLEMENTATION_PLAN.md + 最终验证

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.78 状态**

将 `| 9.78 | ☐ |` 改为 `| 9.78 | ✅ |`

- [ ] **Step 2: 最终全量编译**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

- [ ] **Step 3: 最终全量测试**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/evolving/... -v -count=1
```

- [ ] **Step 4: 覆盖率检查**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test -cover ./internal/evolving/checkpointing/... ./internal/evolving/experience/...
```

Expected: 覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "feat(evolving): 完成 9.78 EvolveCheckpoint 实现，更新实现计划状态"
```
