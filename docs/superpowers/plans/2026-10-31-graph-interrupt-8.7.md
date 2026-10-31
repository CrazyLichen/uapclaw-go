# 8.7 GraphInterrupt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `GraphInterrupt` 从 `session/interaction` 迁入 `graph/pregel`，接通 tracer 的中断分支，并把中断值信封回填到 `stream.OutputSchema`。

**Architecture:** `graph/pregel` 是叶子包（只依赖标准库），成为 `GraphInterrupt` 的唯一来源；`session/interaction` 只保留交互语义（构造中断值并 panic），`session/tracer` 直接 import `graph/pregel` 做具体类型断言。中断值从专用结构体 `InteractionOutputSchema` 回落到 `stream.OutputSchema`（`Payload` 装 `InteractionOutput`），与 Python「一个信封复用」完全同构。

**Tech Stack:** Go 1.x、标准库 `testing`、`go test -cover`；无新增第三方依赖。

**设计文档：** `docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md`

---

## 文件结构

| 文件 | 责任 | 本计划动作 |
|---|---|---|
| `internal/agentcore/graph/pregel/base.go` | Pregel 超步调度信号 + 中断值 + 中断跳转 | 追加 `GraphInterrupt`、`PanicGraphInterrupt`、`(*GraphInterrupt).Error()` |
| `internal/agentcore/graph/pregel/base_test.go` | 上述类型的单测 | 追加 5 条用例 |
| `internal/agentcore/graph/pregel/doc.go` | 包文档 | 目录树、概述、分层纪律 |
| `internal/agentcore/session/interaction/base.go` | 交互基类 + Agent 中断 | 删 `GraphInterrupt` / `PanicGraphInterrupt` |
| `internal/agentcore/session/interaction/interaction.go` | 三种交互实现 + `InteractionOutput` | 两处 panic 改造、删 `InteractionOutputSchema` |
| `internal/agentcore/session/interaction/base_test.go` | 交互基类单测 | 删 `TestPanicGraphInterrupt` |
| `internal/agentcore/session/interaction/interaction_test.go` | 交互实现单测 | 断言改类型 + 新增中断值形状断言 |
| `internal/agentcore/session/interaction/doc.go` | 包文档 | 两处注释 + 目录树 |
| `internal/agentcore/session/tracer/handler.go` | 追踪处理器 | 删 `graphInterrupter`，改直连 |
| `internal/agentcore/session/tracer/handler_test.go` | 追踪处理器单测 | 删假类型，用例改用真实类型 |
| `internal/agentcore/session/doc.go` | session 包文档 | 目录树 + 类型索引 |
| `internal/agentcore/session/node_test.go` | NodeSession 门面单测 | 断言包名 |
| `IMPLEMENTATION_PLAN.md` | 计划表 | 8.7 行 → ✅ |

**执行纪律（项目规则 5）：** 每步跑 `go` 前先 `pgrep -f 'go (build|test)'`，有残留则先 `pkill`；不要 `go clean -cache`。全量回归必须带构建标签。

---

## Task 1: pregel 新增 GraphInterrupt 本体

**Files:**
- Modify: `internal/agentcore/graph/pregel/base.go`
- Test: `internal/agentcore/graph/pregel/base_test.go`

- [ ] **Step 1: 预检编译进程**

Run: `pgrep -f 'go (build|test)' || echo "no go process"`
Expected: `no go process`（或有残留，则 `pkill -f 'go test'`）

- [ ] **Step 2: 写失败测试**

在 `internal/agentcore/graph/pregel/base_test.go` **文件末尾追加**（该文件当前只有 `import "testing"`，无需新增 import）：

```go
// TestGraphInterrupt_Error 错误文案对齐 Python str(value)。
// Python: pregel/base.py:20 super().__init__(str(value))
func TestGraphInterrupt_Error(t *testing.T) {
	cases := []struct {
		name       string
		interrupts []Interrupt
		want       string
	}{
		{"零元素", nil, "[]"},
		{"单元素", []Interrupt{{Value: "test_value"}}, "[{test_value false }]"},
		{"双元素", []Interrupt{{Value: "a"}, {Value: 1, Resumable: true, NS: "n1"}}, "[{a false } {1 true n1}]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gi := &GraphInterrupt{Interrupts: c.interrupts}
			if got := gi.Error(); got != c.want {
				t.Errorf("Error() = %q，期望 %q", got, c.want)
			}
		})
	}
}

// TestPanicGraphInterrupt panic 值恒为 *GraphInterrupt，中断值原样落地。
// 用例迁移自 session/interaction/base_test.go（8.7 本体迁入）。
func TestPanicGraphInterrupt(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
		if gi.Interrupts[0].Value != "test_value" {
			t.Errorf("Interrupts[0].Value = %v，期望 \"test_value\"", gi.Interrupts[0].Value)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "test_value"})
}

// TestPanicGraphInterrupt_多中断 变参展开为切片，顺序与实参一致。
func TestPanicGraphInterrupt_多中断(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 2 {
			t.Fatalf("Interrupts 长度应为 2，实际=%d", len(gi.Interrupts))
		}
		if gi.Interrupts[0].Value != "first" || gi.Interrupts[1].Value != "second" {
			t.Errorf("中断值顺序错乱：%v", gi.Interrupts)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "first"}, Interrupt{Value: "second"})
}

// TestPanicGraphInterrupt_零参数 零参调用不崩，Error() 输出空切片。
// Python 同样允许 GraphInterrupt()（无参），越界防护由消费方 8.14 自行决定。
func TestPanicGraphInterrupt_零参数(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 0 {
			t.Errorf("Interrupts 长度应为 0，实际=%d", len(gi.Interrupts))
		}
		if got := gi.Error(); got != "[]" {
			t.Errorf("Error() = %q，期望 \"[]\"", got)
		}
	}()
	PanicGraphInterrupt()
}

// TestGraphInterrupt_Resumable与NS不丢 守住 8.6 §R1 的书面承诺。
func TestGraphInterrupt_Resumable与NS不丢(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if !gi.Interrupts[0].Resumable {
			t.Error("Resumable 应为 true（8.6 §R1：迁 GraphInterrupt 时不得删）")
		}
		if gi.Interrupts[0].NS != "node_x" {
			t.Errorf("NS = %q，期望 \"node_x\"", gi.Interrupts[0].NS)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "v", Resumable: true, NS: "node_x"})
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/graph/pregel/ -run 'TestGraphInterrupt|TestPanicGraphInterrupt' -v`
Expected: 编译失败，`undefined: GraphInterrupt` / `undefined: PanicGraphInterrupt`

- [ ] **Step 4: 写最小实现**

`internal/agentcore/graph/pregel/base.go` 做三处改动。

(a) 文件头加 import（该文件当前无 import 块，`package pregel` 之后紧跟第一个区块注释）：

```go
package pregel

import "fmt"
```

(b) 在 `Interrupt` 结构体之后、`// ──────────────────────────── 枚举 ────────────────────────────` 之前追加：

```go
// GraphInterrupt 图级中断，控制流跳转手段（Python 侧是 Exception）。
//
// 生命周期与 Interrupt 不同：Interrupt 是随检查点持久化的值对象，
// 本类型是一次执行内的控制流跳转——由交互层 panic 抛出，被节点任务恢复成
// 返回值，最终在引擎顶层转成 {"__interrupt__": Interrupts} 交给上层。
// Python: pregel/base.py:17-20
type GraphInterrupt struct {
	// Interrupts 中断信号列表。Python 类型注解为 Interrupt | tuple[Interrupt, ...]，
	// 但唯二生产点（interaction.py:49/61）均传一元 tuple、消费点按 [0] 取用，
	// 故 Go 用切片表达；空切片语义与 Python 空 tuple 一致（8.14 取 [0] 时须自防越界）。
	Interrupts []Interrupt
}
```

(c) 在文件末尾 `Kind()` 方法之后追加：

```go
// PanicGraphInterrupt 触发图级中断 panic。
// panic 值恒为 *GraphInterrupt，消费者按指针断言。
// Python: raise GraphInterrupt((Interrupt(...),))
func PanicGraphInterrupt(interrupts ...Interrupt) {
	panic(&GraphInterrupt{Interrupts: interrupts})
}

// Error 实现 error 接口，对齐 Python Exception.__init__(str(value))（base.py:20）。
//
// 纪律：本方法**仅供日志与格式化**。任何层禁止用 r.(error) 判定中断——
// Python 的 except 顺序（GraphInterrupt 先于 BaseError/Exception）在 Go 侧
// 只能靠具体类型断言还原，一旦按 error 兜底就会把中断误判成普通错误并吞掉。
func (e *GraphInterrupt) Error() string {
	return fmt.Sprintf("%v", e.Interrupts)
}
```

- [ ] **Step 5: 跑测试确认通过 + 覆盖率**

Run: `go test ./internal/agentcore/graph/pregel/ -cover`
Expected: `ok ... coverage: 100.0% of statements`

Run: `go test ./internal/agentcore/graph/pregel/ -race`
Expected: `ok`（无 DATA RACE）

Run: `$(go env GOROOT)/bin/gofmt -l internal/agentcore/graph/pregel/`
Expected: 无输出

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/graph/pregel/base.go internal/agentcore/graph/pregel/base_test.go
git commit -m "feat(8.7): pregel 新增 GraphInterrupt 中断跳转与 Error 文案"
```

---

## Task 2: interaction 迁移本体并合并中断值信封

**Files:**
- Modify: `internal/agentcore/session/interaction/base.go`
- Modify: `internal/agentcore/session/interaction/interaction.go`
- Test: `internal/agentcore/session/interaction/base_test.go`
- Test: `internal/agentcore/session/interaction/interaction_test.go`

- [ ] **Step 1: 改测试（先失败）**

`internal/agentcore/session/interaction/interaction_test.go`：

(a) 在 import 块加 `pregel`（与 `session/stream` 同一组，按字母序放在 `session/interfaces` 之前）：

```go
import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)
```

(b) `TestWorkflowInteraction_WaitUserInputs_队列空时触发GraphInterrupt`（约 106 行）的断言改为 `pregel.GraphInterrupt`，并**追加中断值形状断言**：

```go
		gi, ok := r.(*pregel.GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *pregel.GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
		val, ok := gi.Interrupts[0].Value.(stream.OutputSchema)
		if !ok {
			t.Fatalf("Interrupts[0].Value 期望 stream.OutputSchema，得到 %T", gi.Interrupts[0].Value)
		}
		if val.Type != InteractionType {
			t.Errorf("Value.Type = %q，期望 %q", val.Type, InteractionType)
		}
		if val.Index != 0 {
			t.Errorf("Value.Index = %d，期望 0", val.Index)
		}
		payload, ok := val.Payload.(InteractionOutput)
		if !ok {
			t.Fatalf("Value.Payload 期望 InteractionOutput，得到 %T", val.Payload)
		}
		if payload.Value != "question" {
			t.Errorf("Payload.Value = %v，期望 \"question\"", payload.Value)
		}
```

(c) `TestWorkflowInteraction_UserLatestInput_有缓存` 中（约 142 行）的断言改为：

```go
		if _, ok := r.(*pregel.GraphInterrupt); !ok {
			t.Fatalf("期望 *pregel.GraphInterrupt，得到 %T", r)
		}
```

(d) `TestWorkflowInteraction_UserLatestInput_无缓存触发GraphInterrupt`（约 160-169 行）改为：

```go
		gi, ok := r.(*pregel.GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *pregel.GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
		if !gi.Interrupts[0].Resumable {
			t.Error("UserLatestInput 的 GraphInterrupt 应为 Resumable")
		}
		if gi.Interrupts[0].NS != "test.exec.id" {
			t.Errorf("NS 期望 'test.exec.id'，实际=%s", gi.Interrupts[0].NS)
		}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/session/interaction/ -run 'TestWorkflowInteraction' 2>&1 | head -20`
Expected: 编译失败（`unknown field`/`invalid operation`：`*GraphInterrupt` 仍是 interaction 本地类型）

- [ ] **Step 3: 改 interaction.go**

(a) import 块加 `stream`：

```go
import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

(b) 删除 `InteractionOutputSchema` 结构体（含其注释块，当前 24-36 行）：

```go
// InteractionOutputSchema 交互中断信号的结构化输出。
// ……（整段删除）
type InteractionOutputSchema struct {
	Type string `json:"type"`
	Index int `json:"index"`
	Payload InteractionOutput `json:"payload"`
}
```

删除后在 `InteractionOutput` 结构体注释中**补一句去向说明**（保留 `InteractionOutput` 本身）：

```go
// InteractionOutput 交互输出数据，标识一次交互的节点和值。
// 中断值经 stream.OutputSchema{Type: "__interaction__", Index, Payload: 本类型} 承载
// （Python 复用同一个 OutputSchema，Go 侧 8.7 起同构）。
// Python: openjiuwen/core/session/interaction/interaction.py (InteractionOutput)
type InteractionOutput struct {
```

(c) `WaitUserInputs` 中的 panic 改为（约 138-144 行）：

```go
	pregel.PanicGraphInterrupt(pregel.Interrupt{
		Value: stream.OutputSchema{
			Type:    InteractionType,
			Index:   w.idx,
			Payload: payload,
		},
	})
```

(d) `UserLatestInput` 中的 panic 改为（约 170-178 行，`Resumable`/`NS` 原样保留）：

```go
	pregel.PanicGraphInterrupt(pregel.Interrupt{
		Value: stream.OutputSchema{
			Type:    InteractionType,
			Index:   w.idx,
			Payload: payload,
		},
		Resumable: true,
		NS:        nodeID,
	})
```

- [ ] **Step 4: 改 interaction/base.go**

(a) 删除 `GraphInterrupt` 结构体及其注释（14-20 行）：

```go
// GraphInterrupt 图级中断异常，通过 panic 传播。
// Interrupt 类型来自 graph/pregel（8.6 起单一来源），本类型本体 8.7 迁入该包。
// Python: openjiuwen/core/graph/pregel/base.py (GraphInterrupt)
type GraphInterrupt struct {
	// Interrupts 中断信号列表
	Interrupts []pregel.Interrupt
}
```

(b) 删除 `PanicGraphInterrupt`（80-84 行）：

```go
// PanicGraphInterrupt 触发图级中断 panic。
// Python: raise GraphInterrupt(...)
func PanicGraphInterrupt(interrupts ...pregel.Interrupt) {
	panic(&GraphInterrupt{Interrupts: interrupts})
}
```

(c) 删除 import 块中的 `graph/pregel`（本文件此后不再引用）：

```go
import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

(d) 文件顶部结构体区在 `AgentInterrupt` 之前补一行来源说明（便于审查时定位去向）：

```go
// 图级中断 GraphInterrupt 已于 8.7 迁入 graph/pregel（单一来源），本包只保留
// Agent 级中断 AgentInterrupt。
```

- [ ] **Step 5: 改 interaction/base_test.go**

删除 `TestPanicGraphInterrupt` 整个函数（当前 137-159 行，含 `// ──────── GraphInterrupt 测试 ────────` 分隔注释），并删除 import 块中的 `graph/pregel`（该文件唯一的 pregel 用法就在被删函数里）。

Run: `grep -n "pregel" internal/agentcore/session/interaction/base_test.go`
Expected: 无输出

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/agentcore/session/interaction/ -cover`
Expected: `ok ... coverage: >= 85%`

Run: `go test ./internal/agentcore/session/interaction/ -race`
Expected: `ok`

Run: `$(go env GOROOT)/bin/gofmt -l internal/agentcore/session/interaction/`
Expected: 无输出

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/session/interaction/
git commit -m "refactor(8.7): interaction 迁移 GraphInterrupt 至 pregel，中断值回填 stream.OutputSchema"
```

---

## Task 3: tracer 改为直连 pregel 类型

**Files:**
- Modify: `internal/agentcore/session/tracer/handler.go`
- Test: `internal/agentcore/session/tracer/handler_test.go`
- Test: `internal/agentcore/session/node_test.go`

- [ ] **Step 1: 改 handler_test.go（先失败）**

(a) import 块加 `graph/pregel`：

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)
```

(b) 删除假类型（24-27 行）：

```go
// testGraphInterrupt 测试用的图中断信号，满足 graphInterrupter 接口
type testGraphInterrupt struct{}

func (*testGraphInterrupt) isGraphInterrupt() {}
```

(c) 表驱动 `TestTraceWorkflowHandler_OnInvoke_异常` 的用例改为真实类型：

```go
		{
			name:       "GraphInterrupt",
			exc:        &pregel.GraphInterrupt{Interrupts: []pregel.Interrupt{{Value: "interrupted"}}},
			wantCode:   0, // 不设置 Error
			wantStatus: NodeStatusInterrupted,
		},
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/session/tracer/ -run TestTraceWorkflowHandler_OnInvoke_异常 2>&1 | head -20`
Expected: 编译失败 `undefined: testGraphInterrupt`

- [ ] **Step 3: 改 handler.go**

(a) import 块加 `graph/pregel`：

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

(b) 删除接口（17-21 行）：

```go
// graphInterrupter 图中断信号接口，避免 tracer → interaction 循环依赖。
// interaction.GraphInterrupt 隐式满足此接口。
type graphInterrupter interface {
	isGraphInterrupt()
}
```

(c) `OnInvoke` 的类型 switch 分支（约 300 行）改为：

```go
		case *pregel.GraphInterrupt:
			span.Status = string(NodeStatusInterrupted)
```

(d) 更新 `OnInvoke` 的文档注释（约 288 行）：

```go
// OnInvoke 组件调用中（运行时数据/错误）
// exc 参数支持 *exception.BaseError、*pregel.GraphInterrupt 和普通 error 三种类型。
// 中断必须用具体类型断言放行（禁止 r.(error) 兜底，见 8.7 spec §7 D3）。
```

- [ ] **Step 4: 改 node_test.go**

(a) import 块：加 `graph/pregel`，**删 `session/interaction`**（该文件对 interaction 的引用只有被改的这两行，删后不再需要）：

```go
import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
)
```

(b) `TestNodeSessionFacade_Interact_非流式模式触发GraphInterrupt` 的断言（388-389 行）改为：

```go
		if _, ok := r.(*pregel.GraphInterrupt); !ok {
			t.Fatalf("期望 *pregel.GraphInterrupt，得到 %T", r)
		}
```

- [ ] **Step 4b: 确认 interaction import 已无残留**

Run: `grep -n "interaction\." internal/agentcore/session/node_test.go`
Expected: 无输出（若仍有输出，说明还有别处引用 interaction，保留 import 并从 §Step 4(a) 的 import 块中恢复该行）

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/agentcore/session/tracer/ -cover`
Expected: `ok ... coverage: >= 85%`

Run: `go test ./internal/agentcore/session/ -cover`
Expected: `ok`

Run: `go test ./internal/agentcore/session/... -race`
Expected: 全部 `ok`，无 DATA RACE

Run: `$(go env GOROOT)/bin/gofmt -l internal/agentcore/session/`
Expected: 无输出

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/tracer/ internal/agentcore/session/node_test.go
git commit -m "refactor(8.7): tracer 直连 pregel.GraphInterrupt，删除 graphInterrupter 死分支"
```

---

## Task 4: 文档回填

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go`
- Modify: `internal/agentcore/graph/pregel/base.go:27`
- Modify: `internal/agentcore/session/interaction/doc.go`
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 改 pregel/base.go 的 Interrupt 注释**

把 `// 与 GraphInterrupt（控制流跳转手段，8.7 迁入本包）职责不同：本类型只承载中断值。`
改为：

```go
// 与 GraphInterrupt（控制流跳转手段，定义于本文件）职责不同：本类型只承载中断值。
```

- [ ] **Step 2: 改 pregel/doc.go**

(a) 首行包概述改为：

```go
// Package pregel 提供 Pregel 图引擎的配置、常量、超步调度信号、中断值与中断控制流。
```

(b) 第二段中 `以及超步之间传递调度消息的 Message 与承载中断值的 Interrupt。` 改为：

```go
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt 与承载中断跳转的
// GraphInterrupt。
```

(c) 分层纪律段落**追加两条**（放在现有三条之后）：

```go
//   - 本包含 panic 类 GraphInterrupt（Python 侧是 Exception），但仍是叶子包：
//     不 recover、不打日志、不读取 Interrupts 的内容。
//   - recover 点必须用 `case *pregel.GraphInterrupt` 判别；禁止 `r.(error)` 兜底——
//     GraphInterrupt 实现了 Error()（仅供日志），按 error 兜底会把中断当普通错误吞掉。
```

(d) 文件目录 `base.go` 行改为：

```go
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
```

- [ ] **Step 3: 改 interaction/doc.go**

(a) `//   - WorkflowInteraction：工作流节点交互，通过 GraphInterrupt 暂停图执行`
→ `//   - WorkflowInteraction：工作流节点交互，通过 pregel.GraphInterrupt 暂停图执行`

(b) `//   - WorkflowInteraction 通过 panic(GraphInterrupt) 暂停工作流图执行`
→ `//   - WorkflowInteraction 通过 panic(pregel.GraphInterrupt) 暂停工作流图执行（本体定义在 graph/pregel）`

(c) 目录树 base.go 行去掉 `GraphInterrupt`：

```go
//	├── base.go               # ExecutableIDProvider 类型别名（→ interfaces 包）+ BaseInteraction + AgentInterrupt + 常量 + 依赖接口
```

(d) 目录树 interaction.go 行补 `InteractionOutput` 说明已存在，追加中断值去向：

```go
//	├── interaction.go        # WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput（中断值装进 stream.OutputSchema）
```

- [ ] **Step 4: 改 session/doc.go**

(a) 目录树 `base.go` 行（53 行）去掉 `GraphInterrupt`：

```go
//	│   ├── base.go                          # ExecutableIDProvider（可执行ID提供者） 类型别名 + BaseInteraction（基础交互） + AgentInterrupt（Agent中断） + 常量
```

(b) 类型索引（118、122 行）改为：

```go
//	WorkflowInteraction   — 工作流交互，通过 pregel.GraphInterrupt 暂停图执行
//	GraphInterrupt        — 图级中断（定义于 graph/pregel，本层只做 panic 传播）
```

- [ ] **Step 5: 验证文档与代码一致**

Run: `go build ./... && $(go env GOROOT)/bin/gofmt -l internal/agentcore/graph/pregel internal/agentcore/session`
Expected: 构建成功、无格式输出

Run: `grep -rn "interaction.GraphInterrupt\|GraphInterrupt（Interrupt 引用 graph/pregel）\|InteractionOutputSchema" --include="*.go" internal/ | grep -v _test`
Expected: 无输出（旧描述已清空）

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/graph/pregel/doc.go internal/agentcore/graph/pregel/base.go internal/agentcore/session/interaction/doc.go internal/agentcore/session/doc.go
git commit -m "docs(8.7): 包文档回填 GraphInterrupt 新归属与中断值信封去向"
```

---

## Task 5: 计划表 8.7 标记完成

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:507`

- [ ] **Step 1: 替换 8.7 行**

把该行整行替换为（**保留**原两个 ⤴️ 注记的语义，改写为已回填；不得删除 8.6 交棒信息）：

```markdown
| 8.7 | ✅ | GraphInterrupt | ✅ `pregel.GraphInterrupt`（单字段 `Interrupts []Interrupt` + 指针 `Error()` 对齐 Python `str(value)` + `PanicGraphInterrupt`；`Resumable`/`NS` 按 8.6 §R1 不删）；⤴️ 已回填：本体从 `session/interaction/base.go` 迁入（原注释「8.7 实现 graph/pregel 时迁移，避免 interaction→graph 反向依赖」已兑现）；⤴️ 已回填：8.6 前移的 `Interrupt` 与 `GraphInterrupt.Interrupts` 类型指向收口完成；⚠️ 8.6 交棒的「补 `isGraphInterrupt()` 标记方法」**不可行**（未导出方法名包限定，跨包不构成同一方法），改为 `session/tracer/handler.go` 直连 `case *pregel.GraphInterrupt`（偏差 D1）；同批回填 `interaction.InteractionOutputSchema` → `stream.OutputSchema`（D2）；`Error()` 仅供日志、禁止 `r.(error)` 判中断（D3）。包 `internal/agentcore/graph/pregel`（base.go 覆盖率 100%）。设计：docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md | `openjiuwen/core/graph/pregel/base.py` |
```

- [ ] **Step 2: 校验表格未破坏**

Run: `grep -c "^| 8\." IMPLEMENTATION_PLAN.md`
Expected: 与改动前一致（36 行 8.x）

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(8.7): 计划表标记完成并归档 D1-D3 偏差"
```

---

## Task 6: 全量回归

- [ ] **Step 1: 预检并全量跑（带 CI 同款构建标签）**

Run: `pgrep -f 'go (build|test)' || echo "clean" && export GOPROXY=https://goproxy.cn,direct && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -v "^ok\|no test files" | head -40`
Expected: 无输出（零 FAIL）

- [ ] **Step 2: 关键包 race 复跑**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/graph/... ./internal/agentcore/session/...`
Expected: 全部 `ok`，无 DATA RACE

- [ ] **Step 3: 覆盖率核对**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/graph/pregel/ ./internal/agentcore/session/interaction/ ./internal/agentcore/session/tracer/`
Expected: 三个包覆盖率均 ≥ 85%（pregel 期望 100%）

- [ ] **Step 4: 若有失败则修复并追加提交**

失败处理顺序：判明是「本节引入」还是「既有失败」（`git stash` 后复跑基线）。本节引入的必须修；既有失败记录在提交正文，不夹带无关修复。

---

## 交付判定

- `GraphInterrupt` 在 `graph/pregel` 唯一定义，`session/interaction` 不再有同名类型
- `session/tracer/handler.go` 无 `graphInterrupter` 接口，`case *pregel.GraphInterrupt` 为活分支
- 全仓无 `InteractionOutputSchema` 引用，中断值为 `stream.OutputSchema`
- `IMPLEMENTATION_PLAN.md` 8.7 ✅ 且 D1/D2/D3 可追溯
- 全量回归零 FAIL（带 `-tags "sqlite_fts5 test"` + `CGO_ENABLED=1`）
