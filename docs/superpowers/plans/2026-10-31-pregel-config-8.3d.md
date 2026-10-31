# 8.3d PregelConfig / 常量 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/graph/pregel` 落地图引擎的配置与常量层（11 颗常量 + 4 个配置 API），并把已实现代码里重复的中断字面量与错号回填标记一次性收敛。

**Architecture:** 单包（`pregel`）承载 `constants.go` + `config.go`，与 Python `openjiuwen/core/graph/pregel/{constants,config}.py` 同构。配置用值类型结构体 + 普通 string/int 表达 `TypedDict(total=False)` 的可选字段（零值即未设置），`CreateInnerConfig` 复刻 falsy 判定，`DefaultPregelConfig()` 用函数返回独立副本以避免导出可变全局。

**Tech Stack:** Go 1.26，标准库 `testing`；测试命令带项目 CI 同款标签 `-tags "sqlite_fts5 test"`；静态检查 `golangci-lint`（govet / errcheck / staticcheck）。

**Spec:** `docs/superpowers/specs/2026-10-31-pregel-config-8.3d-design.md`

---

## 文件结构

| 文件 | 动作 | 职责 |
|---|---|---|
| `internal/agentcore/graph/pregel/doc.go` | 新建 | 包文档：职责、目录树、Python 对应路径、常量索引 |
| `internal/agentcore/graph/pregel/constants.go` | 新建 | 11 颗常量（5 语义 + 4 键名 + 2 零消费） |
| `internal/agentcore/graph/pregel/constants_test.go` | 新建 | 逐值断言，防字符串漂移 |
| `internal/agentcore/graph/pregel/config.go` | 新建 | `PregelConfig`/`InnerPregelConfig`/`CreateInnerConfig`/`DefaultPregelConfig` |
| `internal/agentcore/graph/pregel/config_test.go` | 新建 | 8 个用例：falsy 四格、入参不变、透传、默认值独立 |
| `internal/agentcore/session/checkpointer/inmemory.go` | 修改 | import pregel + `isWorkflowInterrupted` 去字面量 + 注释改写 + 7 处回填改号 |
| `internal/agentcore/session/checkpointer/persistence.go` | 修改 | 5 处回填改号 |
| `internal/agentcore/session/interfaces/interfaces.go` | 修改 | 1 处回填改号 |
| `internal/agentcore/session/checkpointer/inmemory_test.go` | 修改 | 2 处测试注释改号 |
| `internal/agentcore/session/checkpointer/persistence_test.go` | 修改 | 1 处测试注释改号 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 8.3d 行 ☐ → ✅ 并补记 |

父包 `internal/agentcore/graph/doc.go` **本计划不建**（8.1 落 `base.go`/`graph.go` 时创建，避免空包）。

---

## Task 1: 建包与 doc.go

**Files:**
- Create: `internal/agentcore/graph/pregel/doc.go`

- [ ] **Step 1: 写 doc.go**

```go
// Package pregel 提供 Pregel 图引擎的配置与常量层。
//
// 本包是领域八的编译期最底层：构图（8.1）、节点与通道抽象（8.3）、
// 调度引擎（8.3b/8.3c）、通道管理（8.4）、顶点（8.11）、工作流（8.14）
// 均依赖本包提供的保留节点名、终止态键、递归预算与配置归一化能力。
//
// 配置以值类型结构体表达：字段零值等价于 Python TypedDict(total=False)
// 的「键缺失/None」，因此不存在指针字段，调用方也无须判空。
//
// 文件目录：
//
//	pregel/
//	├── doc.go         # 包文档
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
//
// 对应 Python 代码：openjiuwen/core/graph/pregel/config.py · constants.py
package pregel
```

- [ ] **Step 2: 编译验证（此时包内只有 doc.go）**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/graph/pregel/`
Expected: 无输出，退出码 0

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/graph/pregel/doc.go
git commit -m "feat(pregel 8.3d): 新建 graph/pregel 包与包文档"
```

---

## Task 2: constants.go（TDD）

**Files:**
- Create: `internal/agentcore/graph/pregel/constants_test.go`
- Create: `internal/agentcore/graph/pregel/constants.go`

- [ ] **Step 1: 先写失败测试**

```go
package pregel

import "testing"

// TestConstants_值与Python一致 逐值断言 11 颗常量。
// 任何字符串被误改（多/少下划线、大小写变动）都会在此失败。
func TestConstants_值与Python一致(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Start", Start, "__start__"},
		{"End", End, "__end__"},
		{"TaskStatusInterrupt", TaskStatusInterrupt, "__interrupt__"},
		{"TaskStatusError", TaskStatusError, "__error__"},
		{"NsSeparator", NsSeparator, ":"},
		{"NsReplaceChar", NsReplaceChar, "#"},
		{"NsKey", NsKey, "ns"},
		{"ParentNsKey", ParentNsKey, "parent_ns"},
		{"SessionIDKey", SessionIDKey, "session_id"},
		{"RecursionLimitKey", RecursionLimitKey, "recursion_limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q，期望 %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestConstants_递归上限为10000 断言 MaxRecursiveLimit 的数值。
func TestConstants_递归上限为10000(t *testing.T) {
	if MaxRecursiveLimit != 10000 {
		t.Errorf("MaxRecursiveLimit = %d，期望 10000", MaxRecursiveLimit)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/graph/pregel/ -run TestConstants -v`
Expected: 编译失败，输出含 `undefined: Start`（常量尚未定义）

- [ ] **Step 3: 写 constants.go**

```go
package pregel

// ──────────────────────────── 常量 ────────────────────────────

// 常量顺序与 Python constants.py 逐行一致，便于逐行比对。
const (
	// Start 图引擎的起始虚拟节点名，构图器自动创建。Python: START
	Start = "__start__"
	// End 图引擎的结束虚拟节点名，构图器自动创建，通道就绪判定时跳过。Python: END
	End = "__end__"
	// MaxRecursiveLimit 单次执行的最大步数默认值（递归上限）。Python: MAX_RECURSIVE_LIMIT
	MaxRecursiveLimit = 10000
	// TaskStatusInterrupt 顶层图中断的返回键，同时作为中断节点的失败状态。Python: TASK_STATUS_INTERRUPT
	TaskStatusInterrupt = "__interrupt__"
	// TaskStatusError 节点执行失败的失败标记，写入 PendingNode.Status。Python: TASK_STATUS_ERROR
	TaskStatusError = "__error__"
	// NsSeparator 命名空间分隔符。Python: NS_SEPARATOR
	//
	// 对齐留档：Python 侧零消费。禁止用于命名空间拼接——Python 就是字面量 ":"
	//（见 task.py:144），据此实现转义逻辑即偏离。
	NsSeparator = ":"
	// NsReplaceChar 命名空间替换字符。Python: NS_REPLACE_CHAR
	//
	// 对齐留档：Python 侧零消费，禁止据此实现任何转义逻辑。
	NsReplaceChar = "#"
	// NsKey config 中命名空间字段的字典键名。Python: NS
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	NsKey = "ns"
	// ParentNsKey config 中父命名空间字段的字典键名。Python: PARENT_NS
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	ParentNsKey = "parent_ns"
	// SessionIDKey config 中会话 ID 字段的字典键名。Python: SESSION_ID
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	SessionIDKey = "session_id"
	// RecursionLimitKey config 中递归上限字段的字典键名。Python: RECURSION_LIMIT
	//
	// 对齐留档：Go 结构体化后无消费者，仅作与 Python 键名的对照。
	RecursionLimitKey = "recursion_limit"
)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/graph/pregel/ -run TestConstants -v`
Expected: `--- PASS: TestConstants_值与Python一致`（**10 个子用例**：10 颗 string 常量各自一条）、`--- PASS: TestConstants_递归上限为10000`（int 常量单独断言，合计覆盖 11 颗）、`ok`

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/graph/pregel/constants.go internal/agentcore/graph/pregel/constants_test.go
git commit -m "feat(pregel 8.3d): 新增 11 颗常量与逐值断言"
```

---

## Task 3: config.go（TDD）

**Files:**
- Create: `internal/agentcore/graph/pregel/config_test.go`
- Create: `internal/agentcore/graph/pregel/config.go`

- [ ] **Step 1: 先写失败测试**

```go
package pregel

import "testing"

// TestCreateInnerConfig_零值补默认上限 零值 RecursionLimit 视同未设置，补为 MaxRecursiveLimit。
func TestCreateInnerConfig_零值补默认上限(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{})
	if inner.RecursionLimit != MaxRecursiveLimit {
		t.Errorf("RecursionLimit = %d，期望 %d", inner.RecursionLimit, MaxRecursiveLimit)
	}
}

// TestCreateInnerConfig_显式上限保留 显式正数原样保留。
func TestCreateInnerConfig_显式上限保留(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{RecursionLimit: 500})
	if inner.RecursionLimit != 500 {
		t.Errorf("RecursionLimit = %d，期望 500", inner.RecursionLimit)
	}
}

// TestCreateInnerConfig_负上限保留 负数在 Python 为真值，不补默认值。
func TestCreateInnerConfig_负上限保留(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{RecursionLimit: -1})
	if inner.RecursionLimit != -1 {
		t.Errorf("RecursionLimit = %d，期望 -1", inner.RecursionLimit)
	}
}

// TestCreateInnerConfig_不修改入参 对齐 Python create_inner_config 的 config.copy()。
func TestCreateInnerConfig_不修改入参(t *testing.T) {
	cfg := PregelConfig{RecursionLimit: 0}
	_ = CreateInnerConfig(cfg)
	if cfg.RecursionLimit != 0 {
		t.Errorf("入参被修改：RecursionLimit = %d，期望 0", cfg.RecursionLimit)
	}
}

// TestCreateInnerConfig_字段透传 SessionID/Ns 原样带出，ParentNs 保持空。
func TestCreateInnerConfig_字段透传(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{SessionID: "sess-1", Ns: "wf-1", RecursionLimit: 10})
	if inner.SessionID != "sess-1" {
		t.Errorf("SessionID = %q，期望 \"sess-1\"", inner.SessionID)
	}
	if inner.Ns != "wf-1" {
		t.Errorf("Ns = %q，期望 \"wf-1\"", inner.Ns)
	}
	if inner.ParentNs != "" {
		t.Errorf("ParentNs = %q，期望空串", inner.ParentNs)
	}
}

// TestDefaultPregelConfig_字段值 默认配置仅递归上限非零。
func TestDefaultPregelConfig_字段值(t *testing.T) {
	cfg := DefaultPregelConfig()
	if cfg.RecursionLimit != MaxRecursiveLimit {
		t.Errorf("RecursionLimit = %d，期望 %d", cfg.RecursionLimit, MaxRecursiveLimit)
	}
	if cfg.SessionID != "" {
		t.Errorf("SessionID = %q，期望空串", cfg.SessionID)
	}
	if cfg.Ns != "" {
		t.Errorf("Ns = %q，期望空串", cfg.Ns)
	}
}

// TestDefaultPregelConfig_每次返回独立值 修改一次返回值不得影响下一次调用。
func TestDefaultPregelConfig_每次返回独立值(t *testing.T) {
	first := DefaultPregelConfig()
	first.RecursionLimit = 1
	first.SessionID = "mutated"
	second := DefaultPregelConfig()
	if second.RecursionLimit != MaxRecursiveLimit {
		t.Errorf("第二次 RecursionLimit = %d，期望 %d", second.RecursionLimit, MaxRecursiveLimit)
	}
	if second.SessionID != "" {
		t.Errorf("第二次 SessionID = %q，期望空串", second.SessionID)
	}
}

// TestInnerPregelConfig_嵌入字段提升 嵌入字段可直接访问，且可回转为基础配置。
func TestInnerPregelConfig_嵌入字段提升(t *testing.T) {
	inner := InnerPregelConfig{
		PregelConfig: PregelConfig{SessionID: "sess-2", Ns: "wf-2", RecursionLimit: 7},
		ParentNs:     "wf-2:nodeA:0",
	}
	if inner.SessionID != "sess-2" {
		t.Errorf("提升字段 SessionID = %q，期望 \"sess-2\"", inner.SessionID)
	}
	if inner.ParentNs != "wf-2:nodeA:0" {
		t.Errorf("ParentNs = %q，期望 \"wf-2:nodeA:0\"", inner.ParentNs)
	}
	base := inner.PregelConfig
	if base.Ns != "wf-2" {
		t.Errorf("回转后 Ns = %q，期望 \"wf-2\"", base.Ns)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/graph/pregel/ -run TestCreateInnerConfig -v`
Expected: 编译失败，输出含 `undefined: CreateInnerConfig` 与 `undefined: PregelConfig`

- [ ] **Step 3: 写 config.go**

```go
package pregel

// ──────────────────────────── 结构体 ────────────────────────────

// PregelConfig 用户传入的外部配置。
//
// 字段零值等价于 Python TypedDict(total=False) 的「键缺失/None」：空串与 0 即未设置。
// Python: PregelConfig
type PregelConfig struct {
	// SessionID 会话 ID，与 Ns 同时非空且存在 state saver 时才做断点恢复
	SessionID string
	// RecursionLimit 递归上限（最大步数），0 表示未设置
	RecursionLimit int
	// Ns 命名空间（执行上下文标识，顶层为工作流 ID，子图为子图路径）
	Ns string
}

// InnerPregelConfig PregelLoop 内部使用的配置，比外部配置多父命名空间。
//
// 任意节点执行时都可能被派生子图，ParentNs 非空表示当前处于嵌套子图（非顶层执行）。
// Python: InnerPregelConfig(PregelConfig)
type InnerPregelConfig struct {
	// PregelConfig 嵌入外部配置，对应 Python TypedDict 继承
	PregelConfig
	// ParentNs 父命名空间
	ParentNs string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateInnerConfig 将外部配置归一化为内部配置：递归上限为 0 时补 MaxRecursiveLimit。
//
// 入参按值传递，调用方配置不会被修改（对齐 Python create_inner_config 的 config.copy()）。
// 判定必须是「等于 0」而非「非正」：Python 用 falsy 判定，负数在 Python 为真值，
// 因此负数原样保留。
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
// 每次返回独立值，杜绝导出可变全局被任意调用点污染；对齐 Python
// DEFAULT_PREGEL_CONFIG，后者因 create_inner_config 先行 copy 而从不被改坏。
// Python: DEFAULT_PREGEL_CONFIG
func DefaultPregelConfig() PregelConfig {
	return PregelConfig{RecursionLimit: MaxRecursiveLimit}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/graph/pregel/ -v`
Expected: 全部 10 个顶层用例（含 constants 的 2 个）`--- PASS`，末行 `ok  github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel`

- [ ] **Step 5: 查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/graph/pregel/`
Expected: `coverage: 100.0% of statements`（`constants.go` 无语句不参与分母）

- [ ] **Step 6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/graph/pregel/config.go internal/agentcore/graph/pregel/config_test.go
git commit -m "feat(pregel 8.3d): 新增 PregelConfig/CreateInnerConfig/DefaultPregelConfig 与 8 例测试"
```

---

## Task 4: R1 — checkpointer 中断字面量单一样本化

**Files:**
- Modify: `internal/agentcore/session/checkpointer/inmemory.go`（import 块 + `isWorkflowInterrupted`）

- [ ] **Step 1: 加 import**

`inmemory.go` 现有 import 块为：

```go
import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

在 `session/constants` 之前插入一行（`graph` < `session`，保持 gofmt 排序）：

```go
	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
```

（若上游 `logger` 行文本与本示例不同，保持原样不动，只插入 pregel 一行。实施期订正：本示例原误写为 `github.com/uapclaw/common/logger`，实际是 `internal/common/logger`。）

- [ ] **Step 2: 替换函数实现与注释**

把 `inmemory.go` 中这段：

```go
// isWorkflowInterrupted 检查工作流结果是否为中断状态。
// Python: result.get(TASK_STATUS_INTERRUPT) is None
// Go 版本简化处理：检查 result 中的 interrupt 标记。
func isWorkflowInterrupted(result any) bool {
	if result == nil {
		return false
	}
	if m, ok := result.(map[string]any); ok {
		// Python: TASK_STATUS_INTERRUPT 键
		if interruptVal, exists := m["__interrupt__"]; exists && interruptVal != nil {
			return true
		}
	}
	return false
}
```

替换为：

```go
// isWorkflowInterrupted 检查工作流结果是否为中断状态。
//
// 判据与 Python 完全一致：result.get(TASK_STATUS_INTERRUPT) is None 为「未中断」，
// 即键存在且值非 nil 才算中断。
// Python: PersistenceCheckpointer.post_workflow_execute / InMemoryCheckpointer.post_workflow_execute
func isWorkflowInterrupted(result any) bool {
	if result == nil {
		return false
	}
	if m, ok := result.(map[string]any); ok {
		interruptVal, exists := m[pregel.TaskStatusInterrupt]
		return exists && interruptVal != nil
	}
	return false
}
```

- [ ] **Step 3: 运行 checkpointer 测试**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/session/checkpointer/ -count=1`
Expected: `ok  github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer`（行为不变，测试仍用字面量做输入数据）

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/session/checkpointer/inmemory.go
git commit -m "refactor(checkpointer): isWorkflowInterrupted 改用 pregel.TaskStatusInterrupt 常量"
```

---

## Task 5: R2 — 回填标记 8.7 → 8.9 改号

13 处生产代码 + 3 处测试注释的「⤵️ 8.7 回填」内容全是 Graph Store，正确编号是 8.9（计划表 8.9 行即「⤴️ 回填 5.8 checkpointer 的 graphStore」）。**纯注释改动，独立 commit**。

**Files:**
- Modify: `internal/agentcore/session/checkpointer/inmemory.go`（7 处）
- Modify: `internal/agentcore/session/checkpointer/persistence.go`（5 处）
- Modify: `internal/agentcore/session/interfaces/interfaces.go`（1 处）
- Modify: `internal/agentcore/session/checkpointer/inmemory_test.go`（2 处）
- Modify: `internal/agentcore/session/checkpointer/persistence_test.go`（1 处）

- [ ] **Step 1: 逐文件把「8.7 回填」替换为「8.9 回填」（保留 ⤵️ 与后文全部内容）**

五个文件各做一次全文替换（`replace_all` 语义），结果示例：

```
// ⤵️ 8.7 回填：Graph Store 实现后替换为 Store 实例
```
→
```
// ⤵️ 8.9 回填：Graph Store 实现后替换为 Store 实例
```

- [ ] **Step 2: 验证无残留**

Run: `cd /home/opensource/uapclaw-gateway && grep -rn "8\.7 回填" internal/ | wc -l`
Expected: `0`

- [ ] **Step 3: 验证 GraphInterrupt 的 8.7 标记未被误改**

Run: `cd /home/opensource/uapclaw-gateway && grep -rn "8\.7" internal/agentcore/session/interaction/base.go`
Expected: 仍有 2 行命中（`// 暂放 interaction 包，8.7 实现 graph/pregel 包时可迁移。` 及邻近说明），**这两处属于 GraphInterrupt，编号正确，保持不动**

- [ ] **Step 4: 编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/session/... -count=1`
Expected: 全部 `ok`，无 FAIL

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/session/checkpointer/inmemory.go internal/agentcore/session/checkpointer/persistence.go internal/agentcore/session/interfaces/interfaces.go internal/agentcore/session/checkpointer/inmemory_test.go internal/agentcore/session/checkpointer/persistence_test.go
git commit -m "docs(checkpointer): 回填标记 8.7 更正为 8.9（Graph Store 归属节）"
```

---

## Task 6: R3 — 计划表更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（8.3d 行）

- [ ] **Step 1: 替换 8.3d 行**

把这一行：

```
| 8.3d | ☐ | PregelConfig / 常量 | `PregelConfig`/`InnerPregelConfig`/`CreateInnerConfig` + `START`/`END`/`MAX_RECURSIVE_LIMIT=10000`/`__interrupt__`/`__error__`/NS 键名 | `openjiuwen/core/graph/pregel/config.py` · `constants.py` |
```

替换为：

```
| 8.3d | ✅ | PregelConfig / 常量 | ✅ `PregelConfig`(SessionID/RecursionLimit/Ns)/`InnerPregelConfig`(+ParentNs 嵌入)/`CreateInnerConfig`（falsy 补 10000、负数保留、入参不被修改）/`DefaultPregelConfig()`（导出函数而非全局变量，杜绝可变全局）；✅ 11 常量：`Start`/`End`/`MaxRecursiveLimit=10000`/`TaskStatusInterrupt`/`TaskStatusError`/`NsSeparator`/`NsReplaceChar`（后两颗 Python 侧零消费，禁止用于 ns 拼接）/`NsKey`/`ParentNsKey`/`SessionIDKey`/`RecursionLimitKey`（四颗结构体化后零消费，对齐留档）；✅ 回填：checkpointer `isWorkflowInterrupted` 去 `"__interrupt__"` 字面量、13 处「⤵️ 8.7 回填」更正 8.9；包 `internal/agentcore/graph/pregel`（config.go 覆盖率 100%） | `openjiuwen/core/graph/pregel/config.py` · `constants.py` |
```

- [ ] **Step 2: 验证只有目标行变动**

Run: `cd /home/opensource/uapclaw-gateway && git diff IMPLEMENTATION_PLAN.md | grep -E "^[-+]\\| 8\\.3d"`
Expected: 一行 `-| 8.3d ...` 与一行 `+| 8.3d ...`

- [ ] **Step 3: 核对该文件其余未提交改动的归属**

Run: `cd /home/opensource/uapclaw-gateway && git diff --stat IMPLEMENTATION_PLAN.md && git diff IMPLEMENTATION_PLAN.md | grep -E "^[-+]\\| (8\\.1|8\\.2|8\\.3[abcd]?|10\\.6\\.24|11\\.25) "`

- 若其余改动仅是本领域 8.1/8.2/8.3/8.3a/8.3b/8.3c 的**行描述重写**（无代码影响，属同域计划细化），可整文件提交，commit body 逐行列明附带的行。
- 若出现与本任务无关的节（如 10.6.24、11.25 的状态复核记录），**不要**整文件提交：只把 8.3d 行改动保留在工作区，跳过本 Task 的提交，在 Task 7 完成后由人工确认归属再提交。

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(8.3d): 计划表标记完成并补记常量集与回填"
```

---

## Task 7: 全量回归与验收

**Files:** 无改动（只验证）

- [ ] **Step 1: 包级测试 + 覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/graph/pregel/ -count=1`
Expected: `coverage: 100.0% of statements`，`ok`

- [ ] **Step 2: 受影响的兄弟包回归**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/session/... ./internal/agentcore/graph/... -count=1`
Expected: 全部 `ok`，无 FAIL

- [ ] **Step 3: 全仓测试（单进程，先清残留编译进程）**

Run: `pgrep -f 'go (build|test)' || echo "no stale go process"`
Expected: `no stale go process`（若有输出，先 `pkill -f 'go test'` 再继续）

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -c "^FAIL"`
Expected: `0`（`grep -c` 零匹配时退出码为 1，属预期）

- [ ] **Step 4: 静态检查**

Run: `cd /home/opensource/uapclaw-gateway && golangci-lint run ./internal/agentcore/graph/... ./internal/agentcore/session/...`
Expected: `0 issues.`

- [ ] **Step 5: 常量单一来源终检**

Run: `cd /home/opensource/uapclaw-gateway && grep -rn '"__interrupt__"' --include="*.go" internal/ | grep -v "_test.go"`
Expected: 仅 `internal/agentcore/graph/pregel/constants.go` 一行命中（生产代码不再有第二处来源）

- [ ] **Step 6: spec 实施期修订回写**

若实施中出现与 spec 不一致的落地（偏差只允许记录、不允许静默），在 spec 末尾追加 `## 11. 实施期修订（YYYY-MM-DD）` 小节逐条列明；无偏差则跳过。

---

## 自检结果（写计划时已核对）

- **Spec 覆盖**：§2 文件结构 → Task 1/2/3；§3 常量表 → Task 2；§4 配置类型 → Task 3；§5 falsy 四格 → Task 3 的 4 个用例；§6 R1/R2/R3 → Task 4/5/6；§6 R4 消费清单 → 已写入 spec，属后续节对账依据，无本轮代码；§8 测试设计 → Task 2/3；§9 doc.go 与计划表 → Task 1/6；§10 实施顺序 → Task 1-7 顺序一致。
- **类型一致性**：`PregelConfig{SessionID, RecursionLimit, Ns}`、`InnerPregelConfig{PregelConfig, ParentNs}`、`CreateInnerConfig(PregelConfig) InnerPregelConfig`、`DefaultPregelConfig() PregelConfig`、`pregel.TaskStatusInterrupt` 在各 Task 中引用一致。
- **无占位符**：所有代码块与命令均为可直接复制的完整内容；唯一需人工判断处（Task 6 Step 3 的 `IMPLEMENTATION_PLAN.md` 混合未提交改动）已给出两条明确处置路径，不依赖交互式 `git add -p`。
