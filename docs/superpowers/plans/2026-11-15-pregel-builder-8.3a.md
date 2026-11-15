# 8.3a PregelBuilder 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/graph/pregel` 落地 8.3a `PregelBuilder`（`NewPregelBuilder`/`AddNode`/`AddEdge`/`AddBranch`），`Build()` 按方案 A ⤴️ 8.3c 回填；同步计划表注记与 8.8 后移标记。

**Architecture:** 一文件对一文件（Python `builder.py` → Go `builder.go`）。`PregelBuilder` 持有非导出 `nodes map[string]*PregelNode` 与 `channels []Channel`，构造期复刻 `__init__` 预注册 START/END 虚拟节点；`AddEdge` 按方案 β 用 `any` 入参运行时分派三形状；barrier 的合成路由键由 8.4 `BarrierChannel.Key()` 生成、builder 只转手。

**Tech Stack:** Go 标准库；零新增第三方依赖；测试用同包直读非导出字段。

**Spec:** `docs/superpowers/specs/2026-11-15-pregel-builder-8.3a-design.md`

**实施序提醒:** 本节完成后下一节为 8.9a（新序 `8.3a → 8.9a → 8.3b → 8.3c → 8.9a → 8.8 → 8.9b`）。

---

### Task 1: AddEdge 已知偏差 A2 登记（先落 spec，代码实施时兑现）

**Files:**
- Modify: `docs/superpowers/specs/2026-11-15-pregel-builder-8.3a-design.md`（§3.3 追加 A2）

- [ ] **Step 1: 在 spec §3.3 末尾追加偏差 A2**

在 §3.3 已知偏差 A1 段落之后追加：

```markdown
**已知偏差 A2（未注册节点的错误形态）**：Python 对 `add_edge`/`add_branch` 中未注册的源节点名在运行时抛 `KeyError`（`self.nodes[item]` 直取）；Go 侧 `AddEdge` 返回 `error`（`unknown node: <name>`），`AddBranch` 保持链式签名（spec §3.4）、对未注册 src 显式 `panic`（对齐 8.3 `NewPregelNode` 的构造期 panic 纪律）。理由：Go 无异常机制，error 是 KeyError 的等价错误通道；panic 用于签名不带 error 的链式方法，与包内既有「构造期非法输入 panic」口径一致；两者都在构图期立即失败且指名节点。回收条件：无（永久偏差，属语言层差异）。
```

- [ ] **Step 2: 提交 spec 增补**

```bash
git add docs/superpowers/specs/2026-11-15-pregel-builder-8.3a-design.md
git commit -m "docs(specs): 8.3a spec 补已知偏差 A2（未注册节点 KeyError→error）"
```

---

### Task 2: builder.go 骨架 + NewPregelBuilder + AddNode（测试先行）

**Files:**
- Create: `internal/agentcore/graph/pregel/builder_test.go`
- Create: `internal/agentcore/graph/pregel/builder.go`

- [ ] **Step 1: 写失败测试（构造期预注册 + AddNode）**

```go
package pregel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPregelBuilder_预注册起止虚拟节点 构造期立即注册 START/END 两个虚拟节点
// 并各自配 TriggerChannel。Python: builder.py:14-19
func TestNewPregelBuilder_预注册起止虚拟节点(t *testing.T) {
	b := NewPregelBuilder()

	require.Len(t, b.nodes, 2)
	sn, ok := b.nodes[Start]
	require.True(t, ok)
	assert.Equal(t, Start, sn.name)
	require.Empty(t, sn.routers)

	en, ok := b.nodes[End]
	require.True(t, ok)
	assert.Equal(t, End, en.name)
	require.Empty(t, en.routers)

	require.Len(t, b.channels, 2)
	assert.Equal(t, Start, b.channels[0].Key())
	assert.Equal(t, End, b.channels[1].Key())
}

// TestAddNode_追加节点与触发通道 add_node 注册节点并追加 TriggerChannel，
// 重复名按 Python dict 赋值语义覆盖。Python: builder.py:21-26
func TestAddNode_追加节点与触发通道(t *testing.T) {
	b := NewPregelBuilder()
	fn := func(_ context.Context, _ NodeInput) error { return nil }

	ret := b.AddNode("a", fn)
	assert.Same(t, b, ret, "AddNode 必须返回 builder 自身（链式）")

	n, ok := b.nodes["a"]
	require.True(t, ok)
	assert.Equal(t, "a", n.name)
	require.Len(t, n.routers, 0)
	require.Len(t, b.channels, 3)
	assert.Equal(t, "a", b.channels[2].Key())

	// 重复名覆盖：dict 赋值语义，channels 仍追加一条（对齐 Python）
	b.AddNode("a", fn)
	assert.Len(t, b.nodes, 3)
	require.Len(t, b.channels, 4)
}

// TestAddNode_带routers 构造期传入的 routers 原样进入节点。
func TestAddNode_带routers(t *testing.T) {
	b := NewPregelBuilder()
	r := NewStaticRouter("x")
	b.AddNode("a", NoopNodeFunc, r)

	n := b.nodes["a"]
	require.Len(t, n.routers, 1)
	assert.Same(t, r, n.routers[0])
}
```

注：测试文件头部需 `import "context"`（fn 签名用到）。

- [ ] **Step 2: 跑测试确认失败（builder.go 不存在）**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestNewPregelBuilder|TestAddNode' -v`
Expected: 编译失败 `undefined: NewPregelBuilder`

- [ ] **Step 3: 写最小实现**

```go
package pregel

import "fmt"

// PregelBuilder Pregel 图的构图期组装者：持有节点表与通道列表，
// 是 PregelNode（8.3）+ Router 三实现（8.5）+ Trigger/Barrier 通道（8.4）
// 的唯一组装点；Build()（⤴️ 8.3c 回填）将两者交付给调度引擎。
// Python: builder.py:13
type PregelBuilder struct {
	// nodes 节点名 → 节点（对齐 Python self.nodes dict；重复名后者覆盖）
	nodes map[string]*PregelNode
	// channels 构图期顺序追加的通道列表（对齐 Python self.channels list）
	channels []Channel
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPregelBuilder 创建构图器并预注册 START/END 两个虚拟节点
//（执行体 NoopNodeFunc、零 routers），各自配一条 TriggerChannel。
// Python: builder.py:14-19
func NewPregelBuilder() *PregelBuilder {
	b := &PregelBuilder{
		nodes:    make(map[string]*PregelNode),
		channels: make([]Channel, 0),
	}
	b.AddNode(Start, NoopNodeFunc)
	b.AddNode(End, NoopNodeFunc)
	return b
}

// ──────────────────────────── 导出方法 ────────────────────────────

// AddNode 注册节点并追加其触发通道；重复名按 Python dict 赋值语义覆盖。
// Python routers=None 默认值对应 Go 变参零值。返回 builder 自身（链式）。
// Python: builder.py:21-26
func (b *PregelBuilder) AddNode(name string, fn NodeFunc, routers ...Router) *PregelBuilder {
	b.nodes[name] = NewPregelNode(name, fn, routers...)
	b.channels = append(b.channels, NewTriggerChannel(name))
	return b
}
```

注：`fmt` 的 import 在 Task 3 AddEdge 落地后才会被使用，本步可暂不 import（避免 unused import 编译错），Task 3 再补。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestNewPregelBuilder|TestAddNode' -v`
Expected: PASS（3 个用例全绿）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/pregel/builder.go internal/agentcore/graph/pregel/builder_test.go
git commit -m "feat(pregel): 8.3a PregelBuilder 构造期与 AddNode（对齐 builder.py:13-26）"
```

---

### Task 3: AddEdge 三形状分派（方案 β）+ AddBranch

**Files:**
- Modify: `internal/agentcore/graph/pregel/builder.go`（追加方法）
- Modify: `internal/agentcore/graph/pregel/builder_test.go`（追加用例）

- [ ] **Step 1: 写失败测试（三形状边 + 非法组合 + AddBranch）**

```go
// TestAddEdge_单静态边 1→1 static：目标单节点。Python: builder.py:58-60
func TestAddEdge_单静态边(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("a", NoopNodeFunc)
	b.AddNode("b", NoopNodeFunc)

	require.NoError(t, b.AddEdge("a", "b"))

	routers := b.nodes["a"].routers
	require.Len(t, routers, 1)
	sr, ok := routers[0].(*StaticRouter)
	require.True(t, ok)
	assert.Equal(t, []string{"b"}, sr.targets)
}

// TestAddEdge_多静态边 1→N static。Python: builder.py:54-56
func TestAddEdge_多静态边(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("a", NoopNodeFunc)
	b.AddNode("b", NoopNodeFunc)
	b.AddNode("c", NoopNodeFunc)

	require.NoError(t, b.AddEdge("a", []string{"b", "c"}))

	sr, ok := b.nodes["a"].routers[0].(*StaticRouter)
	require.True(t, ok)
	assert.Equal(t, []string{"b", "c"}, sr.targets)
}

// TestAddEdge_屏障边 N→1 barrier：每源节点独占一组（全 AND 形态，偏差 A1）、
// 每个源节点挂 BarrierRouter 且目标为 BarrierChannel.Key()（只转手）。
// Python: builder.py:34-53
func TestAddEdge_屏障边(t *testing.T) {
	b := NewPregelBuilder()
	for _, name := range []string{"a", "b", "c"} {
		b.AddNode(name, NoopNodeFunc)
	}

	require.NoError(t, b.AddEdge([]string{"a", "b"}, "c"))

	// 通道追加：BarrierChannel 在第 5 位（Start/End/a/b/c 的 Trigger 之后）
	require.Len(t, b.channels, 6)
	barrier, ok := b.channels[5].(*BarrierChannel)
	require.True(t, ok)
	assert.Equal(t, "c", barrier.name)
	assert.Len(t, barrier.groups, 2)
	for i, want := range []string{"a", "b"} {
		group := barrier.groups[i]
		_, hit := group[want]
		assert.True(t, hit, "组 %d 应只含 %s", i, want)
	}

	// 每个源节点挂 BarrierRouter，目标 == barrier.Key()（合成路由键转手）
	for _, name := range []string{"a", "b"} {
		routers := b.nodes[name].routers
		require.Len(t, routers, 1)
		br, ok := routers[0].(*BarrierRouter)
		require.True(t, ok)
		assert.Equal(t, []string{barrier.Key()}, br.targets)
	}
}

// TestAddEdge_非法组合返回错误 非 string/[]string 组合一律报错，
// 文案含两端的 Go 类型。Python: builder.py:62-63（ValueError）
func TestAddEdge_非法组合返回错误(t *testing.T) {
	b := NewPregelBuilder()
	err := b.AddEdge([]string{"a"}, []string{"b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported edge format")

	err = b.AddEdge(1, "b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported edge format")

	err = b.AddEdge("a", 2)
	require.Error(t, err)
}

// TestAddEdge_未注册源节点报错 偏差 A2：Python KeyError → Go error。
func TestAddEdge_未注册源节点报错(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("b", NoopNodeFunc)

	err := b.AddEdge("ghost", "b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node: ghost")
}

// TestAddBranch_追加条件路由 selector 原样进入 ConditionalRouter，链式返回。
// Python: builder.py:67-69
func TestAddBranch_追加条件路由(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("a", NoopNodeFunc)
	sel := SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return []string{"b"}, nil
	})

	ret := b.AddBranch("a", sel)
	assert.Same(t, b, ret)

	cr, ok := b.nodes["a"].routers[0].(*ConditionalRouter)
	require.True(t, ok)
	assert.Same(t, sel, cr.selector)
}

// TestAddBranch_未注册源节点panic 偏差 A2：保持链式签名，未注册 src
// 显式 panic（对齐 NewPregelNode 构造期 panic 纪律）。Python KeyError。
func TestAddBranch_未注册源节点panic(t *testing.T) {
	b := NewPregelBuilder()
	assert.PanicsWithValue(t, "unknown node: ghost", func() {
		b.AddBranch("ghost", SelectorFunc(nil))
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -run 'TestAddEdge|TestAddBranch' -v`
Expected: 编译失败 `b.AddEdge undefined`

- [ ] **Step 3: 实现 AddEdge 与 AddBranch**

在 `builder.go` 的 `AddNode` 之后追加（`import` 补上 `fmt`）：

```go
// AddEdge 声明一条边，按 Python 四形状 isinstance 分派（方案 β，spec §3.3）：
//
//	[]string → string  ：N→1 barrier，每源节点独占一组（全 AND 形态，偏差 A1）
//	string   → []string：1→N static
//	string   → string  ：1→1 static
//	其余组合           ：返回 error（对齐 Python ValueError）
//
// barrier 的合成路由键由 BarrierChannel.Key() 生成，builder 只转手给
// NewBarrierRouter（8.5 spec §9 承诺）；未注册源节点返回 error（偏差 A2）。
// Python: builder.py:28-65
func (b *PregelBuilder) AddEdge(start, end any) error {
	switch s := start.(type) {
	case []string:
		if e, ok := end.(string); ok {
			return b.addBarrierEdge(s, e)
		}
	case string:
		switch e := end.(type) {
		case []string:
			node, ok := b.nodes[s]
			if !ok {
				return fmt.Errorf("unknown node: %s", s)
			}
			node.addRouter(NewStaticRouter(e...))
			return nil
		case string:
			node, ok := b.nodes[s]
			if !ok {
				return fmt.Errorf("unknown node: %s", s)
			}
			node.addRouter(NewStaticRouter(e))
			return nil
		}
	}
	return fmt.Errorf("unsupported edge format: start %T -> end %T", start, end)
}

// AddBranch 给源节点追加条件路由（selector 运行时决定目标），返回 builder 自身
// （链式）。未注册 src 显式 panic（偏差 A2，对齐构造期 panic 纪律）。
// Python: builder.py:67-69
func (b *PregelBuilder) AddBranch(src string, selector Selector) *PregelBuilder {
	node, ok := b.nodes[src]
	if !ok {
		panic(fmt.Sprintf("unknown node: %s", src))
	}
	node.addRouter(NewConditionalRouter(selector))
	return b
}

// addBarrierEdge 构造 BarrierChannel（每源节点独占一组 = 全 AND 形态，偏差 A1），
// 追加进 channels，并给 start 中每个源节点挂 BarrierRouter（目标为合成键）。
// Python: builder.py:34-53
func (b *PregelBuilder) addBarrierEdge(start []string, end string) error {
	groups := make([][]string, 0, len(start))
	for _, item := range start {
		groups = append(groups, []string{item})
	}
	barrier := NewBarrierChannel(end, groups)
	b.channels = append(b.channels, barrier)
	for _, item := range start {
		node, ok := b.nodes[item]
		if !ok {
			return fmt.Errorf("unknown node: %s", item)
		}
		node.addRouter(NewBarrierRouter(barrier.Key()))
	}
	return nil
}
```

⚠️ **签名注意**：`AddBranch` 返回 `*PregelBuilder`（链式，spec §3.4），未注册 src panic（偏差 A2）——Step 1 测试代码已按此口径书写，无需改动。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/pregel/ -v`
Expected: 包内全部 PASS（含既有 base/node/router/channels/config/constants 用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/pregel/builder.go internal/agentcore/graph/pregel/builder_test.go
git commit -m "feat(pregel): 8.3a AddEdge 三形状分派与 AddBranch（对齐 builder.py:28-69）"
```

---

### Task 4: doc.go 更新 + 包覆盖率检查

**Files:**
- Modify: `internal/agentcore/graph/pregel/doc.go`

- [ ] **Step 1: doc.go 文件目录追加 builder.go 条目**

在文件目录树 `config.go` 行之前插入（排序：doc → 核心类型 → 辅助 → 配置）：

```go
//	├── builder.go     # PregelBuilder 构图器（AddNode/AddEdge/AddBranch；Build ⤴️ 8.3c）
```

并把概述中「产出调度消息的 Router」一段之后补一句（融入既有段落风格）：

```go
// 与把节点/边/分支装配成图并交付给调度引擎的 PregelBuilder（构图期唯一组装者）。
```

同时「对应 Python 代码」行追加 `· builder.py`。

- [ ] **Step 2: 包覆盖率检查（≥85%）**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/graph/pregel/`
Expected: 全 PASS，`builder.go` 覆盖率 ≥85%

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/graph/pregel/doc.go
git commit -m "docs(pregel): doc.go 补 builder.go 条目与概述"
```

---

### Task 5: IMPLEMENTATION_PLAN.md 注记与行更新（不丢失清单）

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（L505 8.3a 行、L509 8.8 行、L551 注记区之后追加第四行）

- [ ] **Step 1: 更新 8.3a 行（L505）**

原行原文保留，仅追加内容（状态保持 ☐）：

```markdown
| 8.3a | ☐ | PregelBuilder | `AddNode`/`AddEdge`(1→N static、N→1 barrier 含 CNF 组)/`AddBranch`/`Build`；Build ⤴️ 8.3c 回填（方案 A：AddNode/AddEdge/AddBranch 本节落地） | `openjiuwen/core/graph/pregel/builder.py` |
```

- [ ] **Step 2: 更新 8.8 行（L509）**

原描述原文保留，追加 ⤵️ 后移标记：

```markdown
| 8.8 | ☐ | GraphState（节点状态片段） | 仅节点返回值聚合：`source_node_id` 以 `operator.add` 归并；⚠️ 与 8.9 检查点的 `GraphState`(ns/step/channel_values/pending_*) 同名不同义，Go 靠包隔离；⤵️ 后移至 8.9b 之后实施（2026-11-15 拍板） | `openjiuwen/core/graph/graph_state.py` |
```

- [ ] **Step 3: 注记区追加第四行（L551 之后，三条原注记逐字保留）**

```markdown
>
> **8.x 实施顺序（2026-11-15 四次调整）**：8.3a 按方案 A 落地（Build ⤴️ 8.3c 回填）；8.8 后移至 8.9b 之后实施；新实施序 8.3a → 8.9a → 8.3b → 8.3c → 8.9a → 8.8 → 8.9b（依据见 8.3a spec docs/superpowers/specs/2026-11-15-pregel-builder-8.3a-design.md §2）。原注记保留。
```

- [ ] **Step 4: 核对 diff 只含上述三处**

Run: `cd /home/opensource/uapclaw-gateway && git diff IMPLEMENTATION_PLAN.md`
Expected: 仅 8.3a 行、8.8 行、注记区追加行三处变更；L542/L544/L551 三条原注记逐字未动。

- [ ] **Step 5: 提交并推送**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 8.3a Build⤴️8.3c 标记、8.8 后移⤵️标记、四次调整注记"
git push origin main
```

---

### Task 6: 全量回归验证

- [ ] **Step 1: 全量回归（CI 同款标签）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 零 FAIL

- [ ] **Step 2: 编译与 vet**

```bash
cd /home/opensource/uapclaw-gateway && go build ./... && go vet ./...
```

Expected: 无错误。全部通过后按规则 6/11：推送即交付，不追踪远端 CI。
