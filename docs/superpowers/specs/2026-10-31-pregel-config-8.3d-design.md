# 8.3d PregelConfig / 常量 设计文档

> 对应实现计划：领域八 8.3d（IMPLEMENTATION_PLAN.md L503）
> Python 源码：`openjiuwen/core/graph/pregel/config.py`（28 行）+ `openjiuwen/core/graph/pregel/constants.py`（19 行）
> 前置依赖：无（领域八第一个落地节，8.1/8.3/8.3a/8.3b/8.3c/8.11/8.14 全部 import 本节）
> 后续消费方：8.1（`graph.py` 构图）、8.3a（builder 建 START/END 虚拟节点）、8.3b（`NodeTask` 反射注入 config、`PendingNode.Status`）、8.3c（`Pregel.Run`/`PregelLoop`）、8.4（channels 跳过 END）、8.11（Vertex 子图 config）、8.14（workflow 构造 config）、5.8 回填（checkpointer 判中断）

## 1. 流程位置与作用

Python 侧完整调用链（行号已逐条核实）：

```
渠道 / Gateway ──E2A──▶ AgentServer ──▶ AgentSession（session 层）
  └─ Workflow.invoke / stream（8.14）              workflow.py:514  self._internal.compile(session, context)
       └─ CompiledGraph._invoke(inputs, session, config)              graph.py:283-307
            │   config 为 None 时：
            │   PregelConfig(session_id=…, ns=workflow_id,
            │               recursion_limit=MAX_RECURSIVE_LIMIT)       graph.py:295  ← 本节常量
            └─ Pregel.run(config)                                     engine.py:222-229
                 ├─ create_inner_config(config or DEFAULT_PREGEL_CONFIG)  config.py:21-25  ← 本节核心函数
                 ├─ is_top_level = not inner_config.get(PARENT_NS)         engine.py:225  ← 本节键名
                 ├─ PregelLoop.init()
                 │    ├─ max_step = config[RECURSION_LIMIT]                engine.py:39   ← 本节键名
                 │    ├─ if config.get(SESSION_ID) and config.get(NS) and saver:
                 │    │     state = await saver.get(config[SESSION_ID], config[NS])   engine.py:41-42
                 │    └─ resume: max_step = state.step + config[RECURSION_LIMIT]      engine.py:50
                 ├─ run_step() 循环 ─▶ TaskExecutorPool ─▶ NodeTask.run()
                 │    ├─ if 'config' in sig.parameters:
                 │    │     inner_config = create_inner_config(self.config)           task.py:140
                 │    │     if current_parent_ns:
                 │    │       inner_config[NS] = f"{parent_ns}:{node}:{version}"       task.py:144（":" 是字面量）
                 │    └─ 节点失败：PendingNode(status=TASK_STATUS_INTERRUPT/ERROR)      task.py:36
                 ├─ channels 就绪判定跳过 END                              channels.py:79
                 └─ 顶层 GraphInterrupt ─▶ return {TASK_STATUS_INTERRUPT: e.value}    engine.py:275
                       └─ Checkpointer.PostWorkflowExecute:
                          result.get(TASK_STATUS_INTERRUPT) is None ? 清 : 留
                          ← persistence.py:876 / inmemory.py:91 / redis:452
                          ← Go 侧 5.8 已实现，当前为硬编码字面量（见 §6 R1）
构图侧：PregelBuilder.__init__ 自动 add_node(START/END)               builder.py:18-19
子图侧：Vertex(8.11) → 子工作流 → InnerPregelConfig(parent_ns=component_id,
                        recursion_limit=100)                          _workflow.py:104-116
```

**作用（5 条）**：

1. **保留节点名词表**：`START`/`END` 是构图器自动创建的虚拟节点，`ChannelManager` 遇 `END` 不入就绪集；用户节点名与之冲突即构成保留字。
2. **终止态键协议**：`__interrupt__` 是顶层中断的**返回形状**（`Pregel.run` 返回值），并作为 checkpointer「清/留 checkpoint」的唯一判据；`__error__` 是节点失败降级标记，写入 `PendingNode.status`。
3. **递归预算**：`MAX_RECURSIVE_LIMIT=10000` 是 `max_step` 默认值，防图死循环；恢复执行时预算按 `state.step + limit` 续算，保证「断点续跑不吃掉已用步数」。
4. **命名空间协议 + 归一化**：`ns`/`parent_ns`/`session_id`/`recursion_limit` 四键构成 config 词表；`create_inner_config` 承担「缺省补 10000」，`DEFAULT_PREGEL_CONFIG` 承担「顶层无配置兜底」。
5. **域内编译期前置**：本节是领域八唯一零依赖节，其余所有节在 import 层面依赖它，故必须最先落地。

## 2. 文件结构

```
internal/agentcore/graph/pregel/
├── doc.go             # 包文档：职责 + 目录树 + 对应 Python 路径 + 常量索引
├── constants.go       # 11 颗常量
├── constants_test.go  # 逐值断言，防字符串漂移
├── config.go          # PregelConfig / InnerPregelConfig / CreateInnerConfig / DefaultPregelConfig
└── config_test.go
```

- 父包 `internal/agentcore/graph/doc.go` **本轮不建**（避免出现只有 doc.go 的空包），8.1 落 `base.go`/`graph.go` 时一并创建。
- 单包方案（拍板 A）：与 Python「同包两文件」同构；已核对 `pregel` 包的依赖闭包不含 `session`，`session/checkpointer → graph/pregel` 不成环（真正依赖 session 的是 `graph/vertex`(8.11) 与 `graph/stream_actor`(8.12)，均在 `pregel` 包之外）。若 8.9/8.12 后续引入环，逃生口：把 constants 拆成叶子包 `graph/pregel/constants`（本仓 `schema/events`、`sessionctx` 有拆包先例）。

## 3. 常量表（11 颗，顺序与 constants.py 逐行一致）

```go
const (
	// Start 图引擎的起始虚拟节点名，构图器自动创建。Python: START
	Start = "__start__"
	// End 图引擎的结束虚拟节点名，构图器自动创建，通道就绪判定时跳过。Python: END
	End = "__end__"
	// MaxRecursiveLimit 单次执行的最大步数默认值（递归上限）。Python: MAX_RECURSIVE_LIMIT
	MaxRecursiveLimit = 10000
	// TaskStatusInterrupt 顶层图中断的返回键/失败节点状态：中断占位。Python: TASK_STATUS_INTERRUPT
	TaskStatusInterrupt = "__interrupt__"
	// TaskStatusError 节点执行失败的失败标记，写入 PendingNode.Status。Python: TASK_STATUS_ERROR
	TaskStatusError = "__error__"
	// NsSeparator 命名空间分隔符。Python: NS_SEPARATOR
	// 对齐留档：Python 侧零消费；禁止用于命名空间拼接（Python 就是字面量 ":"，见 task.py:144）。
	NsSeparator = ":"
	// NsReplaceChar 命名空间替换字符。Python: NS_REPLACE_CHAR
	// 对齐留档：Python 侧零消费，禁止据此实现任何转义逻辑。
	NsReplaceChar = "#"
	// NsKey config 中命名空间字段的字典键名。Python: NS
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	NsKey = "ns"
	// ParentNsKey config 中父命名空间字段的字典键名。Python: PARENT_NS
	// 对齐留档：同上。
	ParentNsKey = "parent_ns"
	// SessionIDKey config 中会话 ID 字段的字典键名。Python: SESSION_ID
	// 对齐留档：同上。
	SessionIDKey = "session_id"
	// RecursionLimitKey config 中递归上限字段的字典键名。Python: RECURSION_LIMIT
	// 对齐留档：同上。
	RecursionLimitKey = "recursion_limit"
)
```

消费方清单：

| Go 名 | 值 | 后续消费方 |
|---|---|---|
| `Start` | `"__start__"` | 8.1 起边、8.3a 建虚拟节点、8.3c `Pregel.initial` 默认、8.4 就绪判定 |
| `End` | `"__end__"` | 8.1、8.3a、8.3c 跳过 END、8.4 `Snapshot`/`Restore` 跳过 END |
| `MaxRecursiveLimit` | `10000` | 本节 `CreateInnerConfig`/`DefaultPregelConfig`；8.1 `CompiledGraph` 构造 config |
| `TaskStatusInterrupt` | `"__interrupt__"` | 8.3b `PendingNode.Status`、8.3c 顶层返回、5.8 回填（§6 R1） |
| `TaskStatusError` | `"__error__"` | 8.3b `PendingNode.Status` |
| `NsKey`/`ParentNsKey`/`SessionIDKey`/`RecursionLimitKey` | `"ns"`/`"parent_ns"`/`"session_id"`/`"recursion_limit"` | 零消费（对齐留档） |
| `NsSeparator`/`NsReplaceChar` | `":"`/`"#"` | 零消费（Python 亦零消费） |

全部为无类型常量（untyped），便于与 `string` / `int` 直接比较与赋值。

## 4. 配置类型设计

```go
// PregelConfig 用户传入的外部配置。
//
// 零值等价于 Python TypedDict(total=False) 的「键缺失/None」：空串与 0 即未设置。
// Python: PregelConfig (TypedDict, total=False)
type PregelConfig struct {
	// SessionID 会话 ID，与 Ns 同时非空且存在 saver 时才做断点恢复
	SessionID string
	// RecursionLimit 递归上限（最大步数），0 表示未设置
	RecursionLimit int
	// Ns 命名空间（顶层为 workflow_id，子图为子图路径）
	Ns string
}

// InnerPregelConfig PregelLoop 内部使用的配置，比外部配置多父命名空间。
// Python: InnerPregelConfig(PregelConfig)
type InnerPregelConfig struct {
	// PregelConfig 嵌入外部配置（对应 Python TypedDict 继承）
	PregelConfig
	// ParentNs 父命名空间，非空即表示当前为嵌套子图（非顶层执行）
	ParentNs string
}

// CreateInnerConfig 将外部配置归一化为内部配置：递归上限为 0 时补 MaxRecursiveLimit。
//
// 入参按值传递，调用方配置不会被修改（对齐 Python create_inner_config 的 config.copy()）。
// Python: create_inner_config(config)
func CreateInnerConfig(cfg PregelConfig) InnerPregelConfig {
	inner := InnerPregelConfig{PregelConfig: cfg}
	if inner.RecursionLimit == 0 {
		inner.RecursionLimit = MaxRecursiveLimit
	}
	return inner
}

// DefaultPregelConfig 返回默认外部配置（递归上限为 MaxRecursiveLimit，其余为空）。
//
// 每次返回独立值，杜绝可变全局；对齐 Python DEFAULT_PREGEL_CONFIG，
// 后者因 create_inner_config 先行 copy 而从不被改坏。
// Python: DEFAULT_PREGEL_CONFIG
func DefaultPregelConfig() PregelConfig {
	return PregelConfig{RecursionLimit: MaxRecursiveLimit}
}
```

字段顺序与 `config.py` 的键顺序一致（session_id → recursion_limit → ns），便于逐行比对。

## 5. CreateInnerConfig 语义对齐（本节最易写错处）

Python 判定是 **falsy**：`if not inner_config.get(RECURSION_LIMIT)`，不是 `is None`。

| 输入 `RecursionLimit` | Python `not get(...)` | Go 判定 | 结果 |
|---|---|---|---|
| 缺键 / `None` → Go `0` | falsy | `== 0` | `10000` |
| 显式 `0` | falsy | `== 0` | `10000` |
| `500` | truthy | `!= 0` | `500` |
| `-1` | truthy（负数在 Python 为真值） | `!= 0` | `-1` |

其余语义点：

- **不污染入参**：Python 先 `.copy()` 再改；Go 值拷贝天然满足，测试须断言入参未被修改。
- **`ParentNs` 原样透传**：外部配置不存在该字段，归一化不触碰它（恒为 `""`）；`ParentNs` 只由 8.3c/8.3b 在嵌套执行时写入。
- **顶层判定**：`is_top_level = not inner_config.get(PARENT_NS)` → Go `inner.ParentNs == ""`（8.3c 实现，本节只提供字段）。

8.3c 的接线写法（写入本节 spec 供后续对照，本轮不实现）：

```go
cfg := pregel.DefaultPregelConfig()
if config != nil {
	cfg = *config // Python: config or DEFAULT_PREGEL_CONFIG
}
inner := pregel.CreateInnerConfig(cfg)
```

## 6. 回填与接线清单

### R1 单一样本化 checkpointer 的中断字面量（本次实施）

`internal/agentcore/session/checkpointer/inmemory.go:1089` 现为：

```go
if interruptVal, exists := m["__interrupt__"]; exists && interruptVal != nil {
```

改为 `m[pregel.TaskStatusInterrupt]`，并 import `github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel`；同时把函数头注释「Go 版本简化处理」改写为「对齐 Python `result.get(TASK_STATUS_INTERRUPT) is None`」，保留 `isWorkflowInterrupted` 的共享（`persistence.go:781`、`inmemory.go:224` 两处调用点不变）。

调用链影响：无行为变化（常量值相同），仅消除双源。

### R2 回填标记编号修正（本次实施，独立 commit）

生产代码共 **13 处**注释写作「⤵️ 8.7 回填」，内容全部是 Graph Store，计划表 8.9 行也写的是「⤴️ 回填 5.8 checkpointer 的 graphStore any 与 4 处 Delete」，编号应为 **8.9**：

| 文件 | 处数 |
|---|---|
| `session/checkpointer/inmemory.go` | 7 |
| `session/checkpointer/persistence.go` | 5 |
| `session/interfaces/interfaces.go` | 1 |

测试注释另有 3 处（`inmemory_test.go` 2、`persistence_test.go` 1）同步改号。

**不动的**：`session/interaction/base.go:24-25` 的「⤵️ 8.7」指的是 GraphInterrupt 迁移，编号正确，保持原样。

### R3 计划表更新（本次实施）

`IMPLEMENTATION_PLAN.md` 8.3d 行：状态 `☐` → `✅`，并在内容列补记计划行原本遗漏的三项：`NsSeparator`/`NsReplaceChar` 死常量保留、`DefaultPregelConfig` 由 dict 常量改为函数、4 颗键名常量保留为零消费对齐留档。

### R4 后续消费清单（写入 spec，防后续节漂移）

| 节 | 应使用的本节符号 | 禁止 |
|---|---|---|
| 8.1 | `Start`/`End`/`MaxRecursiveLimit`/`PregelConfig` | 硬编码 `"__start__"` |
| 8.3a | `Start`/`End` | 同上 |
| 8.3b | `CreateInnerConfig`/`PregelConfig`/`InnerPregelConfig`/`TaskStatusInterrupt`/`TaskStatusError`/`NsKey`(仅注释用) | 复刻一份 config 归一化逻辑 |
| 8.3c | `CreateInnerConfig`/`DefaultPregelConfig`/`TaskStatusInterrupt`/`End`/`Start` | 在 engine 内重算递归上限 |
| 8.4 | `End` | — |
| 8.11/8.14 | `PregelConfig`/`InnerPregelConfig` | 保留 `recursion_limit=100` 魔法数（Python 即字面量，见 D6） |

## 7. 偏差归档

| # | 偏差 | 理由 |
|---|---|---|
| D1 | 4 颗键名常量 + 2 颗分隔符常量保留但 Go 侧零消费 | 遵循「零调用死代码仍保留」惯例；注释与 §3 表格双重标注禁止误用（尤其 `NsSeparator` 不得替换 `task.py:144` 的字面量 `":"`） |
| D2 | `DEFAULT_PREGEL_CONFIG` 由模块级 dict 常量改为函数 `DefaultPregelConfig()` | 结构体为值类型，导出可变全局会被任意调用点污染；Python 侧靠 `create_inner_config` 的 `.copy()` 偶然获得不可变性，改为函数把该不变量固化 |
| D3 | TypedDict `total=False` 的「键缺失」用零值表达，不再区分 `None` 与 `""`/`0` | 已核对：Python 全部消费点均为 truthiness/falsy 判定，无一处 `if X in config` 键存在性判断，故零值表达逐条等价 |
| D4 | `create_inner_config` 的 `config.copy()` 用结构体值拷贝表达 | 浅拷贝 vs 值拷贝对 4 个标量字段等价 |
| D5 | `sys.intern` 无对应实现 | Go 常量天然常驻且唯一，无需驻留 |
| D6 | `_workflow.py:104-116` 的 `recursion_limit=100` 不抽入常量集 | Python 即调用点字面量；若抽成常量会诱导后续节「统一」成 10000，反而偏离 |
| D7 | 测试中保留 `"__interrupt__"` 字面量作为输入数据 | 测试输入与实现常量解耦，可反向验证常量值不漂移；非偏差，仅记录 |

## 8. 测试设计

`constants_test.go`（`TestConstants_值与Python一致`）：11 颗逐值断言（`Start=="_start__"` 之类），任何字符串被误改即失败。

`config_test.go`：

| 用例 | 断言 |
|---|---|
| `TestCreateInnerConfig_零值补默认上限` | `RecursionLimit==0` → `10000` |
| `TestCreateInnerConfig_显式上限保留` | `500` → `500` |
| `TestCreateInnerConfig_负上限保留` | `-1` → `-1`（Python 真值语义） |
| `TestCreateInnerConfig_不修改入参` | 传入 `PregelConfig{RecursionLimit: 0}` 后入参仍为 0 |
| `TestCreateInnerConfig_字段透传` | `SessionID`/`Ns` 原样带出，`ParentNs==""` |
| `TestDefaultPregelConfig_字段值` | `RecursionLimit==10000`、`SessionID==""`、`Ns==""` |
| `TestInnerPregelConfig_嵌入字段提升` | `inner.SessionID` 可直取；`inner.PregelConfig` 可回转为 `PregelConfig` |
| `TestDefaultPregelConfig_每次返回独立值` | 修改一次返回值不影响下一次调用 |

覆盖率目标：`config.go` 100%（无分支遗漏），`constants.go` 无语句不参与分母；包级 `go test -cover` ≥ 85%。

## 9. doc.go 与计划表更新

- 新建 `internal/agentcore/graph/pregel/doc.go`：包职责（图引擎配置与常量层）、文件目录树（4 文件，不含 `_test.go`）、对应 Python 路径（`openjiuwen/core/graph/pregel/config.py` · `constants.py`）、核心常量/类型索引。
- `IMPLEMENTATION_PLAN.md` 8.3d 行按 §6 R3 更新。

## 10. 实施顺序建议（供 writing-plans 展开）

1. 建包 + `doc.go`（目录树先立，后续文件逐个补条目）
2. `constants.go` + `constants_test.go`
3. `config.go` + `config_test.go`
4. R1：checkpointer 引入常量、改注释
5. R2：13 处生产注释 + 3 处测试注释改号（独立 commit，纯注释）
6. R3：计划表更新
7. 回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/graph/... ./internal/agentcore/session/...` + `golangci-lint run`

## 11. 实施期记录（2026-10-31）

1. **工具替代**：本机未安装 `golangci-lint` / `staticcheck`，以 `go vet ./internal/agentcore/graph/... ./internal/agentcore/session/...`（通过）+ `go fmt`（零差异）+ 全仓 `go test`（0 FAIL）作为等价验证，待 CI 上补跑 lint。
2. **计划表提交未搭便车**：开工时 `IMPLEMENTATION_PLAN.md` 已存在他处未提交改动（8.1-8.3c 行重写 + 10.6.24/11.25 行补充），实施期间被并发提交 `5822ca50`（08:44）；因此本节对计划表的提交（`85a63bfc`）只含 8.3d 单行改动（1 insertion / 1 deletion）。
3. **计划文本订正**：实现计划 Task 4 Step 1 的 import 示例把 logger 路径写作 `common/logger`，实际文件是 `internal/common/logger`；按「保持原样、只插入 pregel 一行」执行，无代码影响。
4. **结果**：`pregel` 包 10 个用例全通过（含 11 颗常量逐值断言），`config.go` 覆盖率 100%；全仓 `go test -tags "sqlite_fts5 test" ./...` 293 包 ok、0 FAIL；生产代码中 `"__interrupt__"` 只剩 `constants.go` 一处来源。
5. **零实施偏差**：D1-D7 全部按 spec 落地，未出现需要新增偏差项的情况。

## 12. 实施后审查修订（2026-10-31）

审查（实现 vs 计划、循环依赖、any 使用）后做两处修订：

1. **计划文本订正**（`b98b7599`）：Task 2 Step 4 的期望值由「11 个子用例」改为「10 个子用例 + 1 个独立断言函数」；Task 4 Step 1 的 import 示例中 logger 路径由 `github.com/uapclaw/common/logger` 改为实际的 `github.com/uapclaw/uapclaw-go/internal/common/logger`。代码实现不变。
2. **`isWorkflowInterrupted` 入参收窄**（`f88258bb`）：`any` → `map[string]any`，删除函数内的类型断言与 `result == nil` 分支；「非 map 结果视为未中断」上移到两处调用点（`resultMap, isMap := result.(map[string]any); isInterrupted := isMap && isWorkflowInterrupted(resultMap)`）。**接口 `PostWorkflowExecute(ctx, session, result any, exception error)` 保持 `any` 不变**，因为 Python 侧签名是 `result: Any`，收窄接口将引入新偏差且波及 10 个 mock 实现。覆盖率 83.5% → 83.6%（基线用 `git worktree` 单独测量，无回归）。

**循环依赖结论（审查项）**：`pregel` 包依赖闭包仅标准库（零业务依赖），`session/checkpointer → graph/pregel` 为单向边，反向不可达，`go list ./...` 无环。**遗留约束**：8.3c 落地后 `pregel` 将 import `graph/store`，届时若 8.9 的 `Store` 反向 import `session/interfaces`，即形成 `checkpointer → pregel → store → session/interfaces` 环；8.9 必须保持 `store` 层 session-free（Python `store/base.py` 只 import logging，可作依据）。
