# 9.78 对齐修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.78 审查发现的 2 处 Python-Go 对齐差异：CommitPending 签名缺 store 参数、NewEvolutionStore 使用裸 any + sysOp 构造函数注入不对齐

**Architecture:** 两处独立修改，先改 CommitPending（影响面小），再改 NewEvolutionStore（影响面大，约 50+ 测试调用点）。NewEvolutionStore 签名从 `any` 改为 `[]string`，sysOp 从构造参数改为 `SetSysOperation()` 方法注入。

**Tech Stack:** Go 1.22+, 现有测试框架

---

## 文件结构映射

| 修改文件 | 改动 |
|----------|------|
| `internal/evolving/checkpointing/manager.go` | CheckpointManager 接口 + DefaultCheckpointManager.CommitPending 签名加 store |
| `internal/evolving/checkpointing/evolution_store.go` | NewEvolutionStore 签名 `any` → `[]string`，删除 sysOp 参数，新增 SetSysOperation 方法，删除 type switch / normalizeBaseDirs(string) / parseBaseDirs |
| `internal/evolving/checkpointing/checkpointing_test.go` | ~50 处 NewEvolutionStore 调用迁移 + CommitPending 调用补 nil |
| `internal/evolving/experience/common_test.go` | 1 处 NewEvolutionStore 调用迁移 |

---

### Task 1: CommitPending 签名对齐

**Files:**
- Modify: `internal/evolving/checkpointing/manager.go:154-166`
- Modify: `internal/evolving/checkpointing/checkpointing_test.go:303`

- [ ] **Step 1: 修改 CheckpointManager 接口和 DefaultCheckpointManager 实现签名**

在 `manager.go` 中：

接口 `CheckpointManager` 添加 `CommitPending` 方法声明（当前接口中未声明 CommitPending，只有 DefaultCheckpointManager 实现上有。检查接口是否需要补充）。

先确认接口定义：

```go
type CheckpointManager interface {
    ShouldSave(epoch int, improved bool) bool
    BuildCheckpoint(agent evolving.TrainableAgent, progress evolving.CheckpointProgress, updaterState map[string]any) *EvolveCheckpoint
    Restore(agent evolving.TrainableAgent, checkpoint *EvolveCheckpoint) map[string]any
}
```

接口中**没有** CommitPending/AddPending/GetPending/DiscardPending。Python 的 CheckpointManager(Protocol) 也**没有**这些方法——它们是 DefaultCheckpointManager 的具体方法。

因此只需修改 `DefaultCheckpointManager.CommitPending` 的实现签名：

将 `manager.go:158` 的：
```go
func (m *DefaultCheckpointManager) CommitPending(operatorID string) int {
```
改为：
```go
func (m *DefaultCheckpointManager) CommitPending(operatorID string, store *EvolutionStore) int {
```

方法体不变（当前不使用 store 参数）。

同时更新注释 `manager.go:154-157`，将：
```go
// CommitPending 清空并返回 pending payload 中的 EvolutionRecord 总数。
//
// 只清空内存中的待定状态并返回记录计数，不负责写磁盘。
// 对应 Python: DefaultCheckpointManager.commit_pending(operator_id, store)
func (m *DefaultCheckpointManager) CommitPending(operatorID string) int {
```
改为：
```go
// CommitPending 清空并返回 pending payload 中的 EvolutionRecord 总数。
//
// 只清空内存中的待定状态并返回记录计数，不负责写磁盘。
// store 参数当前未使用，预留对齐 Python commit_pending(operator_id, store) 签名。
// 对应 Python: DefaultCheckpointManager.commit_pending(operator_id, store)
func (m *DefaultCheckpointManager) CommitPending(operatorID string, store *EvolutionStore) int {
```

- [ ] **Step 2: 更新测试中的 CommitPending 调用**

在 `checkpointing_test.go:303`，将：
```go
count := m.CommitPending("op_1")
```
改为：
```go
count := m.CommitPending("op_1", nil)
```

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/checkpointing/...
```

- [ ] **Step 4: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/evolving/checkpointing/... -run TestCommitPending -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/checkpointing/manager.go internal/evolving/checkpointing/checkpointing_test.go
git commit -m "fix(checkpointing): CommitPending 签名添加 store 参数对齐 Python"
```

---

### Task 2: NewEvolutionStore 消除 any + sysOp 改 Setter

**Files:**
- Modify: `internal/evolving/checkpointing/evolution_store.go:90-116`
- Modify: `internal/evolving/checkpointing/checkpointing_test.go` (约 50+ 处)
- Modify: `internal/evolving/experience/common_test.go:215`

- [ ] **Step 2.1: 修改 NewEvolutionStore 签名和实现**

在 `evolution_store.go` 中，将构造函数：

```go
// NewEvolutionStore 创建 EvolutionStore 实例。
//
// 对应 Python: EvolutionStore.__init__(skills_base_dir: Union[str, List[str]])
// skillsBaseDirs 支持 string 和 []string 两种输入，对齐 Python Union[str, List[str]]
func NewEvolutionStore(skillsBaseDirs any, sysOp sys_operation.SysOperation) *EvolutionStore {
	var dirs []string
	switch v := skillsBaseDirs.(type) {
	case string:
		dirs = normalizeBaseDirs(v)
	case []string:
		dirs = normalizeBaseDirsFromList(v)
	default:
		panic(fmt.Sprintf("skillsBaseDirs 必须为 string 或 []string，实际类型: %T", skillsBaseDirs))
	}
	if len(dirs) == 0 {
		panic("skills_base_dir 为空")
	}
	s := &EvolutionStore{
		baseDirs:     dirs,
		sysOperation: sysOp,
		skillLocks:   map[string]*sync.RWMutex{},
	}
	s.records = &StoreRecordsHelper{store: s}
	s.projection = &StoreProjectionHelper{store: s}
	s.archive = &StoreArchiveHelper{store: s}
	return s
}
```

改为：

```go
// NewEvolutionStore 创建 EvolutionStore 实例。
//
// dirs 为技能基础目录列表（至少一个），单个目录也用切片传入。
// sysOperation 通过 SetSysOperation 方法注入，对齐 Python 属性赋值模式。
//
// 对应 Python: EvolutionStore.__init__(skills_base_dir: Union[str, List[str]])
func NewEvolutionStore(dirs []string) *EvolutionStore {
	dirs = normalizeBaseDirsFromList(dirs)
	if len(dirs) == 0 {
		panic("skills_base_dir 为空")
	}
	s := &EvolutionStore{
		baseDirs:   dirs,
		skillLocks: map[string]*sync.RWMutex{},
	}
	s.records = &StoreRecordsHelper{store: s}
	s.projection = &StoreProjectionHelper{store: s}
	s.archive = &StoreArchiveHelper{store: s}
	return s
}
```

- [ ] **Step 2.2: 添加 SetSysOperation 方法**

在 `evolution_store.go` 的导出函数区域，`BaseDirs()` 方法之前添加：

```go
// SetSysOperation 注入系统操作接口。
//
// 构造后调用，对齐 Python: self.sys_operation = sys_operation 属性赋值模式。
// 缺省时 ReadFileText/WriteFileText 回退到本地 os/fs。
func (s *EvolutionStore) SetSysOperation(op sys_operation.SysOperation) {
	s.sysOperation = op
}
```

- [ ] **Step 2.3: 删除不再需要的函数**

从 `evolution_store.go` 非导出函数区域删除：

1. `normalizeBaseDirs(skillsBaseDir string) []string` — 不再需要，`NewEvolutionStore` 直接调 `normalizeBaseDirsFromList`
2. `parseBaseDirs(raw string) []string` — 只被 `normalizeBaseDirs` 调用

保留 `normalizeBaseDirsFromList`（仍被构造函数使用）。

- [ ] **Step 2.4: 更新构造函数注释中的 import**

确认 `evolution_store.go` 的 import 中 `fmt` 包是否仍被使用（`normalizeBaseDirs` 删除后可能不再需要 `fmt`——但 `panic(fmt.Sprintf(...))` 在 `NewEvolutionStore` 中仍使用 `fmt`，所以保留）。

- [ ] **Step 2.5: 编译验证（先不改测试，确认源码编译通过）**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/checkpointing/...
```

预期：编译失败（测试文件调用旧签名），这是预期的。

- [ ] **Step 2.6: 批量更新 checkpointing_test.go 中的 NewEvolutionStore 调用**

全部 `NewEvolutionStore(tmpDir, nil)` 改为 `NewEvolutionStore([]string{tmpDir})`。

全部 `NewEvolutionStore(tmpDir1+","+tmpDir2, nil)` 改为 `NewEvolutionStore([]string{tmpDir1, tmpDir2})`。

`NewEvolutionStore("", nil)` 改为 `NewEvolutionStore(nil)` 或 `NewEvolutionStore([]string{})`。

具体替换规则：
- `NewEvolutionStore(XXX, nil)` → `NewEvolutionStore([]string{XXX})`（XXX 为单目录字符串变量）
- `NewEvolutionStore(A+","+B, nil)` → `NewEvolutionStore([]string{A, B})`
- `NewEvolutionStore("", nil)` → `NewEvolutionStore([]string{})`

- [ ] **Step 2.7: 更新 experience/common_test.go 中的 NewEvolutionStore 调用**

将 `common_test.go:215`：
```go
store := checkpointing.NewEvolutionStore(skillDir, sys_operation.SysOperation(nil))
```
改为：
```go
store := checkpointing.NewEvolutionStore([]string{skillDir})
```

- [ ] **Step 2.8: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/evolving/...
```

- [ ] **Step 2.9: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/evolving/checkpointing/... -v -count=1
cd /home/opensource/uap-claw-go && go test ./internal/evolving/experience/... -v -count=1
```

- [ ] **Step 2.10: 提交**

```bash
git add internal/evolving/checkpointing/evolution_store.go internal/evolving/checkpointing/checkpointing_test.go internal/evolving/experience/common_test.go
git commit -m "fix(checkpointing): NewEvolutionStore 消除 any 参数，sysOp 改 SetSysOperation 方法注入"
```

---

### Task 3: doc.go 同步更新

**Files:**
- Modify: `internal/evolving/checkpointing/doc.go`

- [ ] **Step 3.1: 检查 doc.go 是否需要更新**

阅读 `doc.go` 确认 NewEvolutionStore 签名描述是否需要同步。如无显式签名描述则跳过。

```bash
cat internal/evolving/checkpointing/doc.go
```

- [ ] **Step 3.2: 如有需要，更新 doc.go 中的构造函数描述**

- [ ] **Step 3.3: 提交（如有修改）**

```bash
git add internal/evolving/checkpointing/doc.go
git commit -m "docs(checkpointing): 同步 doc.go 构造函数签名变更"
```
