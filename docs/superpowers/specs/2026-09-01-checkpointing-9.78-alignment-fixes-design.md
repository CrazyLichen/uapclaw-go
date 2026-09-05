# 9.78 对齐修复设计：CommitPending 签名 + NewEvolutionStore 消除 any

## 背景

9.78 EvolveCheckpoint 章节审查发现 5 处 Python-Go 对齐差异，经逐项讨论，确定修复其中 2 处：

1. `DefaultCheckpointManager.CommitPending` 缺少 `store` 参数
2. `NewEvolutionStore(skillsBaseDirs any, sysOp)` 使用裸 `any` + 构造函数注入 sysOp 不对齐 Python

其余 3 处保持现状：`GetPending` 返回引用（当前调用方只读）、`toJSONCompatible` 缺 struct 分支（当前场景不需要）、`CheckpointProgress` 接口替代 Any（正向改进）。

## 修复 1：CommitPending 签名对齐

### 现状

```go
// Go
func (m *DefaultCheckpointManager) CommitPending(operatorID string) int

// Python
def commit_pending(self, operator_id: str, store: "EvolutionStore") -> int
```

### 目标

```go
// CheckpointManager 接口
type CheckpointManager interface {
    // ...
    CommitPending(operatorID string, store *EvolutionStore) int
}

// DefaultCheckpointManager 实现
func (m *DefaultCheckpointManager) CommitPending(operatorID string, store *EvolutionStore) int
```

### 规则

- `store` 参数当前不使用，仅预留对齐 Python 签名（Python docstring: "reserved for future async commit path"）
- 调用方传 `nil` 即可

### 影响范围

- `internal/evolving/checkpointing/manager.go` — 接口 + 实现签名
- `internal/evolving/checkpointing/checkpointing_test.go` — 测试调用补 `nil`
- 搜索所有 `CommitPending` 调用方补参数

## 修复 2：NewEvolutionStore 消除 any + sysOp 改 Setter

### 现状

```go
// Go — any + 构造函数注入 sysOp
func NewEvolutionStore(skillsBaseDirs any, sysOp sys_operation.SysOperation) *EvolutionStore
// 内部 type switch: string / []string
// sysOp 直接赋值到 struct 字段

// Python — Union 类型 + 属性后注入
def __init__(self, skills_base_dir: Union[str, List[str]]) -> None:
    self.sys_operation: Optional[SysOperation] = None
# 外部: self._evolution_store.sys_operation = sys_operation
```

### 目标

```go
// 构造函数只接受 []string，消除 any
func NewEvolutionStore(dirs []string) *EvolutionStore

// sysOp 通过 Setter 方法注入，对齐 Python 的属性赋值模式
func (s *EvolutionStore) SetSysOperation(op sys_operation.SysOperation)
```

### 变更细节

1. **`NewEvolutionStore(dirs []string)`**
   - 去掉 `any` + type switch，直接使用 `dirs` 参数
   - 内部 `normalizeBaseDirsFromList(dirs)` 已有，直接复用
   - 空切片 panic 保持不变

2. **`EvolutionStore` 结构体**
   - `sysOperation` 字段保留为非导出 `sysOperation sys_operation.SysOperation`
   - 新增 `SetSysOperation(op sys_operation.SysOperation)` 方法

3. **调用方迁移**
   - `NewEvolutionStore(dir, nil)` → `NewEvolutionStore([]string{dir})`
   - `NewEvolutionStore("dir1,dir2", nil)` → `NewEvolutionStore([]string{"dir1", "dir2"})` 或 `NewEvolutionStore(strings.Split("dir1,dir2", ","))`
   - 需要 sysOp 的调用方在构造后调用 `store.SetSysOperation(op)`

### 影响范围

- `internal/evolving/checkpointing/evolution_store.go` — 构造函数签名 + 新增 Setter
- `internal/evolving/checkpointing/checkpointing_test.go` — 约 50+ 处调用更新
- `internal/evolving/experience/common_test.go` — 1 处调用更新
- 搜索所有 `NewEvolutionStore` 调用方

## 不修改项

| 差异 | 原因 |
|------|------|
| `GetPending` 返回 slice 引用 | 当前调用方只读不修改，无实际风险 |
| `toJSONCompatible` 缺 struct 反射 | EvolveCheckpoint 字段全是 map/slice，当前不需要 |
| `CheckpointProgress` 接口替代 Any | 正向改进，编译时安全 + 解决循环依赖 |

## 测试要求

- 现有测试全部通过
- `CommitPending` 新签名在测试中传 `nil` store 验证行为不变
- `NewEvolutionStore([]string{...})` 单目录和多目录场景均覆盖
- `SetSysOperation` 注入后 ReadFileText/WriteFileText 路由到 sysOp 的路径有测试覆盖（已有）
