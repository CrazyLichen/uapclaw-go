# 9.78 EvolveCheckpoint 实现设计

> 本文档定义 9.78 EvolveCheckpoint 的 Go 实现方案，对应 Python `openjiuwen/agent_evolving/checkpointing/` 模块。

## 1. 流程位置与作用

**在自演化流程中的位置：**

```
训练循环流程（Trainer 主循环）：

  1. SignalDetector（9.73 ✅） — 检测信号，决定是否触发演进
  2. Optimizer（9.72 ✅） — 生成 EvolutionPatch
  3. UpdateExecution（9.80 ✅） — 将 patch 应用到技能
  4. Trajectory（9.77 ✅） — 记录完整演进轨迹
  5. ⭐ EvolveCheckpoint（9.78 ☐） — 中途保存/恢复训练状态 ← 当前
  6. Experience（9.79 ☐） — 在线经验审批/评分/追踪
```

**核心作用：**

- **断点续训**：CheckpointManager 在关键节点保存 EvolveCheckpoint，下次启动时 ResumeIfNeeded 恢复状态（epoch、best_score、operators_state），避免从头训练
- **演进数据持久化**：EvolutionStore 是技能文件系统的 IO 门面，管理演进记录的 CRUD、Markdown 投影渲染、归档治理——Optimizer 生成的 EvolutionPatch 的最终归宿
- **待定变更缓冲**：DefaultCheckpointManager._pending 在内存中暂存变更，等 Experience（9.79）审批后 commit 到 EvolutionStore

## 2. 设计决策汇总

| 决策点 | 选择 | 理由 |
|--------|------|------|
| PendingChange 处理 | 先在 experience/types.go 定义，9.79 由用户自行扩展 | 与 Python 文件组织一致，唯一未实现的外部依赖 |
| 包结构 | 扁平单包，按 Python 文件名拆分 Go 文件 | 一一对应方便对照查找 |
| 并发模型 | 同步方法 + sync.RWMutex per skill_name | 读操作远多于写，RWMutex 允许读并发；与 Trainer 同步模型一致 |
| SysOperation 路由 | 可选注入 SysOperation，缺省用本地 os/fs | 与 Python 对齐，沙箱和本地双模式支持 |
| 实现范围 | 完整一次性实现 + Trainer 5处回填 | 所有依赖已就绪，不留中间态 |
| TrainableAgent 归属 | 从 trainer 包迁移到 evolving 根包的 trainable_agent.go | 解决 checkpointing↔trainer 循环依赖 |
| 9.79 后续 | 由用户自行处理 | — |

## 3. 外部依赖状态

| 类型 | Go 包 | 状态 |
|------|--------|------|
| SysOperation | `internal/agentcore/sys_operation` | ✅ 已完整实现 |
| Operator | `internal/agentcore/operator` | ✅ 已完整实现 |
| EvolutionTarget | `internal/evolving/signal` | ✅ 已完整实现 |
| VALID_PATCH_ACTIONS / VALID_SECTIONS | `internal/evolving/schema/protocol.go` | ✅ 已完整实现 |
| UpdateEffectPendingChange | `internal/evolving/schema/update.go` | ✅ 常量已定义 |
| PendingChange | `internal/evolving/experience/types.go` | ❌ 需提前定义 |

## 4. 文件结构

### 4.1 新增文件

```
internal/evolving/checkpointing/        # 新建包（与 Python checkpointing/ 一一对应）
├── doc.go               # 包文档（含文件目录树 + Python 对应路径）
├── state.go             # EvolveCheckpoint
├── types.go             # UsageStats / EvolutionPatch / EvolutionRecord / EvolutionLog + 序列化
├── manager.go           # CheckpointManager interface + DefaultCheckpointManager
├── store_file.go        # FileCheckpointStore（本地 JSON 检查点存储）
├── evolution_store.go   # EvolutionStore 门面（组合三个 Helper + SysOperation 路由 + RWMutex）
├── store_records.go     # StoreRecordsHelper（记录 CRUD 持久化）
├── store_projection.go  # StoreProjectionHelper（Markdown 投影渲染）
├── store_archive.go     # StoreArchiveHelper（归档/清空/创建技能）
├── skill_package.go     # 打包/解包/skill_id 纯函数

internal/evolving/experience/           # 新建包（提前定义 PendingChange）
├── doc.go               # 包文档
├── types.go             # PendingChange（9.79 的其余类型由用户自行补充）

internal/evolving/trainable_agent.go    # TrainableAgent 从 trainer 迁移至此
```

### 4.2 修改文件

```
internal/evolving/trainer/trainer.go    # 5处回填 + TrainableAgent 引用改为 evolving.TrainableAgent
internal/evolving/doc.go                # 更新子包列表（新增 checkpointing、experience）
internal/evolving/trainer/doc.go        # 更新文件目录（TrainableAgent 迁移说明）
IMPLEMENTATION_PLAN.md                  # 9.78 状态 ☐ → ✅（完成后）
```

## 5. 核心类型设计

### 5.1 前置定义：PendingChange

```go
// internal/evolving/experience/types.go

// PendingChange 等待审批的暂存演进记录快照。
// 对应 Python: openjiuwen/agent_evolving/experience/types.py PendingChange
type PendingChange struct {
    OperatorID    string                    // Operator 标识符
    SkillName     string                    // 技能名称
    ChangeType    string                    // 变更类型
    Payload       []checkpointing.EvolutionRecord // 暂存的演进记录列表
    CreatedAt     string                    // 创建时间（UTC ISO 格式）
    ChangeID      string                    // 变更标识，格式: skill_evolve_{uuid8}
    IsSharedRecords bool                    // 是否为共享记录
    Trajectory    any                       // 关联轨迹（可选）
    Messages      []map[string]any          // 关联消息列表（可选）
}
```

注意：`Payload` 引用 `checkpointing.EvolutionRecord`，形成 experience → checkpointing 的跨包引用。两个包都在 `internal/evolving/` 下，Go 允许。Python 中也是 PendingChange payload 引用 checkpointing.types.EvolutionRecord。

### 5.2 EvolveCheckpoint

```go
// internal/evolving/checkpointing/state.go

// EvolveCheckpoint 训练检查点，用于断点续训。
// 对应 Python: openjiuwen/agent_evolving/checkpointing/state.py EvolveCheckpoint
type EvolveCheckpoint struct {
    Version         string                        // 检查点版本标签
    RunID           string                        // 运行标识
    Step            map[string]int                // {"epoch": int, "batch": int}
    Best            map[string]any                // {"best_score": float}
    Seed            *int                          // 随机种子
    OperatorsState  map[string]map[string]any     // operator_id → state dict
    UpdaterState    map[string]any                // 更新器状态
    SearcherState   map[string]any                // 搜索器状态
    LastMetrics     map[string]any                // {"current_epoch_score": float}
}
```

### 5.3 types.go — UsageStats / EvolutionPatch / EvolutionRecord / EvolutionLog

```go
// UsageStats 演进经验的使用统计。
// 对应 Python: checkpointing/types.py UsageStats
type UsageStats struct {
    TimesPresented   int
    TimesUsed        int
    TimesPositive    int
    TimesNegative    int
    LastPresentedAt  *string     // UTC ISO
    LastEvaluatedAt  *string     // UTC ISO
}

// EvolutionPatch 单次生成的演进变更。
// 对应 Python: checkpointing/types.py EvolutionPatch
type EvolutionPatch struct {
    Section          string                      // 目标 section (Instructions/Examples/Troubleshooting/Scripts)
    Action           string                      // append/merge/replace/skip
    Content          string                      // 变更内容
    Target           signal.EvolutionTarget      // 引用 signal 包枚举
    SkipReason       *string
    MergeTarget      *string                     // merge 时替换的目标 record ID
    ScriptFilename   *string
    ScriptLanguage   *string
    ScriptPurpose    *string
    Keywords         []string
    Summary          *string
}

// EvolutionRecord 单条持久化的演进记录。
// 对应 Python: checkpointing/types.py EvolutionRecord
type EvolutionRecord struct {
    ID             string
    Source         string
    Timestamp      string                       // UTC ISO
    Context        string
    Change         EvolutionPatch
    Applied        bool                         // 默认 false
    Score          float64                      // 默认 0.6
    UsageStats     *UsageStats
    SkillVersion   *string
    Summary        *string
}

// EvolutionLog 单个技能的所有演进记录容器。
// 对应 Python: checkpointing/types.py EvolutionLog
type EvolutionLog struct {
    SkillID   string                            // 默认由 Empty() 工厂设置
    Version   string                            // 默认 "1.0.0"
    UpdatedAt string                            // UTC ISO，默认当前时间
    Entries   []EvolutionRecord
}
```

每个类型有 `ToDict()` / `FromDict()` 序列化方法。EvolutionRecord 有 `Make()` 工厂方法（验证 action/section/target）。EvolutionLog 有 `Empty()` 工厂方法和 `PendingEntries()` 方法。EvolutionPatch 在构造时验证 action ∈ VALID_PATCH_ACTIONS、section ∈ VALID_SECTIONS。

### 5.4 CheckpointManager interface + DefaultCheckpointManager

```go
// internal/evolving/checkpointing/manager.go

// CheckpointManager 检查点管理器接口。
// 对应 Python: checkpointing/manager.py CheckpointManager(Protocol)
type CheckpointManager interface {
    ShouldSave(epoch int, improved bool) bool
    BuildCheckpoint(agent evolving.TrainableAgent, progress any, updaterState map[string]any) *EvolveCheckpoint
    Restore(agent evolving.TrainableAgent, checkpoint *EvolveCheckpoint) map[string]any
}

// DefaultCheckpointManager 默认检查点管理器实现。
type DefaultCheckpointManager struct {
    runID             string
    ckptVersion       string                    // 默认 "v1"
    saveEveryNEpochs  int                       // 默认 1，最小 1
    saveOnImprove     bool                      // 默认 true
    pending           map[string][]*experience.PendingChange
}
```

注意：BuildCheckpoint 的 `progress` 参数使用 `any` 类型，因为 Progress 定义在 trainer 包中（会造成循环依赖）。Trainer 调用时传入 *Progress，DefaultCheckpointManager 内部通过类型断言提取 epoch/batch/bestScore/seed 等字段。

Pending 变更管理：AddPending / GetPending / CommitPending / DiscardPending。CommitPending 只清空+计数，不负责写磁盘。

### 5.5 FileCheckpointStore

```go
// internal/evolving/checkpointing/store_file.go

// FileCheckpointStore 基于 JSON 文件的检查点存储。
type FileCheckpointStore struct {
    baseDir string
}

// NewFileCheckpointStore(baseDir) — 构造并确保目录存在
// SaveCheckpoint(ckpt, filename) (*string, error) — JSON 序列化写入
// LoadCheckpoint(path) (*EvolveCheckpoint, error) — JSON 反序列化读取
// LoadStateDict(path) (map[string]map[string]any, error) — 仅读 operators_state
// toJSONCompatible(obj) any — 递归处理嵌套结构体/map/list/primitive
```

### 5.6 EvolutionStore（核心门面）

```go
// internal/evolving/checkpointing/evolution_store.go

// EvolutionStore 技能演进数据的文件系统 IO 门面。
type EvolutionStore struct {
    baseDirs    []string                        // 配置的技能基础目录列表（resolve 后）
    sysOperation *sys_operation.SysOperation    // 可选注入，缺省用本地 os/fs
    skillLocks  map[string]*sync.RWMutex        // 技能级读写锁（惰性创建）
    records     *StoreRecordsHelper             // 记录持久化辅助
    projection  *StoreProjectionHelper          // Markdown 投影辅助
    archive     *StoreArchiveHelper             // 归档/创建辅助
}
```

**文件读写路由**：ReadFileText / WriteFileText 判断 sysOperation 是否注入，有则走 SysOperation.Fs()，否则走本地 os.ReadFile / os.WriteFile。

**锁策略**：
- 读操作（listSkillNames / loadEvolutionLog / readSkillContent 等）→ RLock
- RMW 写操作（AppendRecord / UpdateRecordScores / DeleteRecords / MarkRecordsApplied / MergeRecords / UpdateRecordContent）→ Lock

**目录解析**：_normalizeBaseDirs 支持分号/逗号分隔多路径，去重 resolve。

完整方法清单对齐 Python EvolutionStore（见设计节4b）。

### 5.7 StoreRecordsHelper

```go
// internal/evolving/checkpointing/store_records.go

// StoreRecordsHelper 演进记录 CRUD 和持久化辅助。
type StoreRecordsHelper struct {
    store *EvolutionStore
}

// persistScript — 脚本持久化到 evolution/scripts/{filename}
// loadFullEvolutionLog / saveEvolutionLog — evolutions.json 读写
// updateRecordScores / getRecordsByScore / deleteRecords / markRecordsApplied
// mergeRecords / updateRecordContent — RMW 操作（由 EvolutionStore 在 Lock 保护下调用）

// _langToExt 语言扩展名映射：python→py, javascript→js, typescript→ts, shell→sh, bash→sh
```

### 5.8 StoreProjectionHelper

```go
// internal/evolving/checkpointing/store_projection.go

// StoreProjectionHelper Markdown 投影和 pending 记录格式化辅助。
type StoreProjectionHelper struct {
    store *EvolutionStore
}

// RenderEvolutionMarkdown(name) — 核心渲染：过滤→按 section 分组→写 md→更新 SKILL.md index 块
// ClearRenderedOutputs / RenderSectionFile / RenderScriptIndex / UpdateSkillMDIndex

// FormatDescExperienceText / FormatAllDescExperiences / FormatBodyExperienceText / ListPendingSummary

// SKILL.md evolution-index 块格式：
// <!-- evolution-index-start -->
// | Section | Action | Score | Summary |
// <!-- evolution-index-end -->

// 静态辅助：sectionFilename / recordSummary / normalizeSummaryText / formatExperienceIndexTable / formatScriptAssetsTable / extractDescriptionFromSkillMD
```

### 5.9 StoreArchiveHelper

```go
// internal/evolving/checkpointing/store_archive.go

// StoreArchiveHelper 归档、清空和创建技能辅助。
type StoreArchiveHelper struct {
    store *EvolutionStore
}

// CreateSkill(name, description, body, frontmatter) — 名称校验 + mkdir + 写 SKILL.md + 空 EvolutionLog
// ArchiveSkillBody — SKILL.md → archive/SKILL.v{timestamp}.md
// ArchiveEvolutions — evolutions.json → archive/evolutions.v{timestamp}.json
// ClearEvolutions — 写空 EvolutionLog + 重新渲染
// ListArchives — archive/ 下所有文件名倒序

// tsSuffix() — UTC 时间戳 "%Y%m%dT%H%M%S"
// archiveDir(skillDir) — skill_dir/archive 子目录
```

### 5.10 skill_package.go（纯函数）

```go
// internal/evolving/checkpointing/skill_package.go

// NewSkillID() string — "sk_{uuid12}"
// ReadSkillIDFromContent(content) string — 从 frontmatter 读 skill_id
// EnsureSkillIDInContent(content) (string, string) — 返回 (updatedContent, skillID)
// PackSkillDirectory(skillDir, skillMDRelpath, skillMDContent) ([]byte, error) — tar.gz 打包
// UnpackSkillPackage(packageBytes, destDir) error — 安全解压（防路径遍历，使用 data_filter 等价）
// ListPackableFiles(skillDir) ([]string, error) — 列出可打包文件

// 排除规则（包级常量）：
// _excludeDirNames = {"evolution", "archive", "__pycache__", ".git"}
// _excludeFileNames = {"evolutions.json"}
// 隐藏文件（.xxx）也排除
```

排除是业务逻辑：打包技能是为了"分享"，分享时只携带技能本身，不携带演进历史和本地治理数据。

## 6. TrainableAgent 迁移

**原位置**：`internal/evolving/trainer/trainer.go` 中的 `TrainableAgent` 接口定义

**新位置**：`internal/evolving/trainable_agent.go`

**原因**：解决 checkpointing ↔ trainer 循环依赖。checkpointing 的 CheckpointManager.BuildCheckpoint / Restore 需要引用 TrainableAgent，但 trainer 也引用 checkpointing。

**迁移步骤**：
1. 在 `internal/evolving/trainable_agent.go` 定义 TrainableAgent（完整接口，包含 Invoke/Card/GetOperators）
2. trainer.go 中删除 TrainableAgent 定义，改为引用 `evolving.TrainableAgent`
3. trainer_test.go 同步更新引用

## 7. Trainer 回填

trainer.go 中 5 处 `⤵️ 待 9.78 回填` 的回填内容：

| 占位 | 回填后类型/逻辑 |
|------|----------------|
| `checkpointStore any` | `checkpointStore *checkpointing.FileCheckpointStore` |
| `checkpointManager any` | `checkpointManager checkpointing.CheckpointManager` |
| `WithCheckpointManager(cm any) TrainerOption` | `WithCheckpointManager(cm checkpointing.CheckpointManager) TrainerOption` |
| `ResumeIfNeeded(ctx, agent)` 空实现 | 加载检查点 → Restore 恢复 Operator 状态 → 返回 progress 信息 |
| `SaveCheckpointIfNeeded(...)` 空实现 | ShouldSave 判断 → BuildCheckpoint → FileCheckpointStore.SaveCheckpoint |

## 8. 测试策略

- 每个 Go 文件配备 `_test.go`，测试覆盖率 ≥ 85%
- EvolutionStore 的文件系统操作使用 `t.TempDir()` 创建临时目录 mock
- SysOperation 可选注入：测试中用 nil（走本地 os）或 mock SysOperation
- 序列化/反序列化：EvolveCheckpoint / EvolutionLog / EvolutionRecord 的 ToDict/FromDict 往返测试
- skill_package 打包/解包：用临时目录创建技能结构，打包后解包验证内容一致性
- DefaultCheckpointManager：ShouldSave 逻辑、BuildCheckpoint/Restore、Pending 变更管理
- RWMutex 并发：AppendRecord 等写操作的并发安全性测试
