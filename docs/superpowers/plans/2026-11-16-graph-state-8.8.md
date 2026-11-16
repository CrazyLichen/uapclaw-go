# 8.8 GraphState 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 一比一复刻 Python `openjiuwen/core/graph/graph_state.py` 的 `GraphState`（零实例化死类型），建立 `internal/agentcore/graph/` 包，Pregel 子域收口。

**Architecture:** 新建 Go 包 `internal/agentcore/graph/`，单实现文件承载一个 struct（Python TypedDict → Go struct），无方法无构造函数（Python 侧就没有）；`operator.add` reducer 语义以注释承载（spec 偏差 D1，不发明归并方法）。测试文件保证包覆盖率 ≥85%。

**Tech Stack:** Go 1.x（无第三方依赖）；测试用标准库 `testing`。

**Spec:** `docs/superpowers/specs/2026-11-16-graph-state-8.8-design.md`

---

### Task 1: 建立 graph 包 + GraphState（TDD）

**Files:**
- Create: `internal/agentcore/graph/graph_state.go`
- Create: `internal/agentcore/graph/doc.go`
- Test: `internal/agentcore/graph/graph_state_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/graph/graph_state_test.go`：

```go
package graph

import (
	"slices"
	"testing"
)

// TestGraphState_零值 验证零值构造
func TestGraphState_零值(t *testing.T) {
	var s GraphState
	if s.SourceNodeID != nil {
		t.Fatalf("期望零值 nil，实际 %v", s.SourceNodeID)
	}
}

// TestGraphState_SourceNodeID追加语义 验证 operator.add（列表拼接）语义的字段级锚点
func TestGraphState_SourceNodeID追加语义(t *testing.T) {
	s := GraphState{}
	// 模拟两个节点执行后各自写入的追加（Python: {"source_node_id": ["n1"]} + {"source_node_id": ["n2"]}）
	s.SourceNodeID = append(s.SourceNodeID, "n1")
	s.SourceNodeID = append(s.SourceNodeID, "n2")
	if !slices.Equal(s.SourceNodeID, []string{"n1", "n2"}) {
		t.Fatalf("期望 [n1 n2]，实际 %v", s.SourceNodeID)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/ -v`
Expected: FAIL（`undefined: GraphState`，目录尚无实现文件；若目录不存在先创建目录，测试文件本身会建立包）

- [ ] **Step 3: 写实现**

创建 `internal/agentcore/graph/graph_state.go`：

```go
package graph

// ──────────────────────────── 结构体 ────────────────────────────

// GraphState 节点输出聚合 schema，对齐 openjiuwen/core/graph/graph_state.py 的 TypedDict。
// Python 声明为 source_node_id: Annotated[list, operator.add]——语义为
// "执行过的来源节点 ID 以列表拼接（operator.add）追加式归并，不覆盖"。
// 全仓零实例化死类型：唯一 Python 消费者是 vertex.py:364 对 __call__ state 形参的类型标注，
// 节点返回值在 task.py:152 被丢弃，reducer 在 Python 全仓无归并执行点（spec 偏差 D1：
// Go 类型系统无 Annotated reducer 注解对应物，语义以本注释承载，不实现归并方法）。
// 与 pregel/state.go 的检查点 GraphState 同名不同义，靠包隔离区分。
type GraphState struct {
	// SourceNodeID 执行过的来源节点 ID 列表（operator.add 追加语义）
	SourceNodeID []string
}
```

创建 `internal/agentcore/graph/doc.go`：

```go
// Package graph 提供图引擎顶层的共享类型定义，对齐 openjiuwen/core/graph 包根。
//
// 当前仅含 GraphState（节点输出聚合 schema）：Python 侧为零运行时效果的 TypedDict
// 类型标注，唯一消费者是 vertex.py:364 对 Vertex.__call__ state 形参的类型标注；
// 节点返回值在 pregel/task.py:152 被丢弃，Annotated[list, operator.add] reducer
// 全仓无归并执行点，故 Go 侧仅复刻类型不实现归并（spec 偏差 D1）。
// 与子包 pregel 的检查点 GraphState（internal/agentcore/graph/pregel/state.go）同名不同义。
//
// 文件目录：
//
//	graph/
//	├── doc.go            # 包文档
//	└── graph_state.go   # GraphState 节点输出聚合 schema
//
// 对应 Python 代码：openjiuwen/core/graph/graph_state.py
package graph
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/graph/ -v -cover`
Expected: 全 PASS，覆盖率 100%（单 struct 无复杂语句）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/graph_state.go internal/agentcore/graph/doc.go internal/agentcore/graph/graph_state_test.go
git commit -m "feat(8.8): GraphState 节点输出聚合 schema——一比一复刻 graph_state.py TypedDict，零实例化死类型+D1偏差留档"
```

### Task 2: 全量回归 + 门槛验证

**Files:** 无新增（只跑验证）

- [ ] **Step 1: 编译与 vet**

```bash
export GOPROXY=https://goproxy.cn,direct
go build ./... && go vet ./...
```
Expected: 无错误

- [ ] **Step 2: 全量回归（CI 同款标签）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 零 FAIL（新包不引入任何依赖变更，不应影响既有包）

- [ ] **Step 3: 覆盖率核对**

Run: `go test -cover ./internal/agentcore/graph/`
Expected: ≥ 85%（预期 100%）

### Task 3: 计划表回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（8.8 行，约 513 行；顺序注记区，约 559 行后）

- [ ] **Step 1: 8.8 行打 ✅**

将 8.8 行（保持表格单行）改为：

```
| 8.8 | ✅ | GraphState（节点状态片段） | ✅ Go 事实（2026-11-16）：新包 `internal/agentcore/graph/`（包根首次建立，doc.go+graph_state.go+测试三文件）；`GraphState{SourceNodeID []string}` 一比一复刻 TypedDict，无方法无构造（Python 侧就没有）；偏差 D1 归档 spec §5（`Annotated[list, operator.add]` reducer 注解 Go 类型系统无对应物，且该 reducer 在 Python 全仓无归并执行点——task.py:152 丢弃节点返回值，语义以注释承载、不发明归并方法，回收条件挂 8.11 Vertex 真实消费时）；⚠️ 与 pregel/state.go 检查点 GraphState 同名不同义，靠包隔离（原拍板保留）；✅ 原 ⤵️ 后移拍板已提前兑现；设计：docs/superpowers/specs/2026-11-16-graph-state-8.8-design.md | `openjiuwen/core/graph/graph_state.py` |
```

- [ ] **Step 2: 顺序链追加九次记录**

在「六次记录」注记之后追加：

```
> **8.x 实施顺序（2026-11-16 九次记录）**：8.8 提前落地（2026-11-15 后移拍板随 8.9b 悬挂注销自然解除，spec docs/superpowers/specs/2026-11-16-graph-state-8.8-design.md §2）；Pregel 子域（8.3/8.3a/8.3b/8.3c/8.3d/8.4/8.5/8.6/8.7/8.9）全部收口；领域八剩余 8.1/8.2/8.10-8.26。原注记保留。
```

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(8.8): 计划表回填——8.8 打✅+D1偏差归档+顺序链九次记录，Pregel子域全收口"
```

- [ ] **Step 4: 推送**

```bash
git push
```
Expected: 推送成功即交付（规则 11：本地门槛通过 + 已推送，不追踪远端 CI）。
