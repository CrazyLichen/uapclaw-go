# 13.22 读取侧数据模型（TripleBeam + TripleMemory + MultiKBRetrievalResult）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现领域 13 读取侧三件套数据容器（TripleBeam beam 路径载体 + TripleMemory 多跳事实记忆 + MultiKBRetrievalResult 多 KB 聚合出口契约），为 13.23/13.25/13.27 提供共同数据地基。

**Architecture:** common 既有包内新增 triple_beam.go / triple_memory.go 两文件 + result.go 追加 MultiKBRetrievalResult。纯数据容器零 IO/零 logger/零错误码；三关键拍板：Triples()/Memory() 浅拷贝密封（slices.Clone，防 append 写穿）、TripleFact = [3]string 定长（读取侧类型，与写入侧 Triple 分立）、raw_score float64 直装（None 边界归档不预防）。

**Tech Stack:** Go 1.26（slices.Clone）/ testify（assert.Panics 全仓先例）

**Spec:** `docs/superpowers/specs/2026-10-28-triplebeam-triplememory-multikb-13.22-design.md`（含 8 决策 + 6 偏差 §8 + 7 回填项 §9，实施中随时对照）

**Python 对齐基准：**
- 源：`/home/opensource/agent-core/openjiuwen/core/retrieval/common/triple_beam.py`（36 行）+ `triple_memory.py`（45 行）+ `retrieval_result.py`（MultiKBRetrievalResult，L33-41）
- 测试：**0 专属用例**（Python 无 test_triple_beam.py / test_triple_memory.py；test_retrieval_result.py 只测前两类）——Go 23 例全部为增强型

**关键纪律（全任务适用）：**
1. 注释全中文；文件内声明按 结构体→枚举→常量→全局变量→导出函数→非导出函数 排列（go-code-conventions）。triple_beam.go 无枚举/常量/全局变量，只有结构体+导出函数两区块；triple_memory.go 结构体（TripleMemory）→ 枚举区块（TripleFact 类型别名）→ 导出函数
2. Python 三文件零 logger → Go 零日志
3. git 提交显式 add 具体文件，**禁 -am**（工作区有 10.6.24a 未提交改动：IMPLEMENTATION_PLAN.md 修改 + 2026-10-26-swarm-tools-10.6.24a-design.md 未跟踪）；每次提交后 `git status --short` + `git show --stat HEAD` 核对零搭便车
4. 编译前 `pgrep -f 'go (build|test)'` 检查残留进程；`export GOPROXY=https://goproxy.cn,direct`
5. 测试命令统一：`cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run <TestName> -v -count=1`（包级全量 + 覆盖率在 Task 5）
6. 测试文件风格：common 包 testify 先例（result_test.go/callbacks_test.go/config_test.go/document_test.go 均用 assert）——新增测试统一用 testify；既有 triple_test.go 原生风格不动
7. **新建文件后必须显式 git add**（新文件 -am 也会漏 add——heartbeat 教训）

---

### Task 1: TripleBeam 数据容器

**Files:**
- Create: `internal/agentcore/retrieval/common/triple_beam.go`
- Test: `internal/agentcore/retrieval/common/triple_beam_test.go`

- [ ] **Step 1: 写失败测试（B1-B10 全量）**

创建 `internal/agentcore/retrieval/common/triple_beam_test.go`：

```go
package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// makeBeamNodes 构造测试用 beam 节点（3 节点典型路径）。
func makeBeamNodes() []RetrievalResult {
	return []RetrievalResult{
		{Text: "entity1 relation entity2", Score: 0.9, Metadata: map[string]any{"triple": `["entity1","relation","entity2"]`}},
		{Text: "entity2 relation2 entity3", Score: 0.8, Metadata: map[string]any{"triple": `["entity2","relation2","entity3"]`}},
		{Text: "entity3 relation3 entity4", Score: 0.7, Metadata: map[string]any{"triple": `["entity3","relation3","entity4"]`}},
	}
}

// TestNewTripleBeam 构造后 Len/Score/元素一致（B1）
func TestNewTripleBeam(t *testing.T) {
	nodes := makeBeamNodes()
	b := NewTripleBeam(nodes, 0.85)
	assert.Equal(t, 3, b.Len())
	assert.Equal(t, 0.85, b.Score())
	assert.Equal(t, nodes[0], b.Get(0))
	assert.Equal(t, nodes[2], b.Get(2))
}

// TestTripleBeam_Get正索引 正索引取节点（B2）
func TestTripleBeam_Get正索引(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.Equal(t, "entity1 relation entity2", b.Get(0).Text)
	assert.Equal(t, "entity3 relation3 entity4", b.Get(2).Text)
}

// TestTripleBeam_Get负索引 负索引从尾部数，Get(-1) 取尾（B3，生产形态对应物）
func TestTripleBeam_Get负索引(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.Equal(t, "entity3 relation3 entity4", b.Get(-1).Text)
	assert.Equal(t, "entity2 relation2 entity3", b.Get(-2).Text)
}

// TestTripleBeam_Get越界panic 越界自然 panic（B4，IndexError 对应物）
func TestTripleBeam_Get越界panic(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.Panics(t, func() { b.Get(3) })
	assert.Panics(t, func() { b.Get(-4) })
}

// TestTripleBeam_Len 长度（B5）
func TestTripleBeam_Len(t *testing.T) {
	assert.Equal(t, 0, NewTripleBeam(nil, 0.5).Len())
	assert.Equal(t, 3, NewTripleBeam(makeBeamNodes(), 0.5).Len())
}

// TestTripleBeam_Contains按text判重 同 text 异 score/metadata 视为已存在（B6，钉"按 text 非全字段"）
func TestTripleBeam_Contains按text判重(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	sameTextDiffRest := RetrievalResult{
		Text:     "entity1 relation entity2",
		Score:    0.123, // 与节点 0 同 text 异 score
		Metadata: nil,   // 异 metadata
	}
	assert.True(t, b.Contains(sameTextDiffRest))
}

// TestTripleBeam_Contains不存在 text 不同则不存在（B7）
func TestTripleBeam_Contains不存在(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.False(t, b.Contains(RetrievalResult{Text: "absent text", Score: 0.9}))
}

// TestTripleBeam_Triples密封 跨对象写穿防护（B8，C2 决策钉死）
//
// 真实写穿场景（13.23 展开形态）：NewTripleBeam(append(b1.Triples(), t), s)
// 若 Triples 返回内部引用且 backing 有空闲容量，两 beam 共享数组——
// b1 二次展开的 append 写穿 b2 的第三节点。C2 每次返回独立分配，
// 跨调用写穿不可能。注：仅"append 后查本对象视图不变"不足以判别
// C1/C2（slice len 固定，append 只写 backing[len] 越界位，视图不变）。
func TestTripleBeam_Triples密封(t *testing.T) {
	nodes := makeBeamNodes()
	// 构造 cap > len 的内部切片（append 链形态，13.23 展开的真实输入）
	base := make([]RetrievalResult, 0, 3)
	base = append(base, nodes[0], nodes[1]) // len 2, cap 3
	b1 := NewTripleBeam(base, 0.5)

	// 13.23 展开形态：append 构造扩展路径 → 新 beam
	extended := append(b1.Triples(), nodes[2])
	b2 := NewTripleBeam(extended, 0.6)
	assert.Equal(t, 3, b2.Len())

	// b1 二次展开不同节点：C1 下写穿共享 backing[2] 污染 b2；
	// C2 下各自独立分配互不影响
	sneaky := append(b1.Triples(), RetrievalResult{Text: "sneaky", Score: 1.0})
	assert.Equal(t, 3, len(sneaky))
	assert.Equal(t, "entity3 relation3 entity4", b2.Get(2).Text,
		"b1 二次展开不应写穿 b2 内部状态")
}

// TestTripleBeam_Triples元素一致 逐元素等于原构造节点（B9）
func TestTripleBeam_Triples元素一致(t *testing.T) {
	nodes := makeBeamNodes()
	got := NewTripleBeam(nodes, 0.5).Triples()
	assert.Equal(t, nodes, got)
}

// TestTripleBeam_Score 分数透传（B10）
func TestTripleBeam_Score(t *testing.T) {
	assert.Equal(t, 0.42, NewTripleBeam(nil, 0.42).Score())
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run 'TestNewTripleBeam|TestTripleBeam' -v -count=1`
Expected: 编译失败 `undefined: NewTripleBeam`（实现未写）

- [ ] **Step 3: 写实现**

创建 `internal/agentcore/retrieval/common/triple_beam.go`：

```go
package common

import "slices"

// ──────────────────────────── 结构体 ────────────────────────────

// TripleBeam beam search 路径载体（GraphRetriever 的 TripleBeamSearch 消费）。
//
// 一条 beam = 候选三元组路径 + 累积分数；构造时一次性建立 text 去重集合，
// Contains 按 text O(1) 判重（对齐 Python __contains__ 查 _exist_triples）。
// 构造后事实不可变——防环靠新建 beam 而非原地修改（对齐 Python：展开恒
// beam.triples + [triple] 新建列表，零突变路径）。
//
// Python: openjiuwen/core/retrieval/common/triple_beam.py
type TripleBeam struct {
	// beam 路径节点（RetrievalResult 列表）
	beam []RetrievalResult
	// existTriples text 去重集合（构造时建立）
	existTriples map[string]struct{}
	// score 累积分数
	score float64
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTripleBeam 创建 beam 路径。existTriples 集合在构造时一次性建立
// （对齐 Python __init__ 的 {x.text for x in nodes}）。
func NewTripleBeam(nodes []RetrievalResult, score float64) *TripleBeam {
	existTriples := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		existTriples[node.Text] = struct{}{}
	}
	return &TripleBeam{
		beam:         nodes,
		existTriples: existTriples,
		score:        score,
	}
}

// Get 按索引取节点，负索引从尾部数（对齐 Python __getitem__；生产用
// Get(-1) 取尾）。越界经 slice 下标自然 panic（IndexError 对应物，
// 偏差归档 spec §8-5）。
func (b *TripleBeam) Get(idx int) RetrievalResult {
	if idx < 0 {
		idx += len(b.beam)
	}
	return b.beam[idx]
}

// Len 返回路径长度（对齐 __len__）。
func (b *TripleBeam) Len() int {
	return len(b.beam)
}

// Contains 判断节点是否已在 beam 中——按 text 判重而非全字段比较
// （对齐 __contains__：triple.text in self._exist_triples）。
func (b *TripleBeam) Contains(triple RetrievalResult) bool {
	_, ok := b.existTriples[triple.Text]
	return ok
}

// Triples 返回路径节点副本（浅拷贝密封：元素 Metadata map 仍共享，与
// Python 引用列表语义一致；返回新 slice 使消费方 append 不写穿内部
// 状态——偏差归档 spec §8-1）。
func (b *TripleBeam) Triples() []RetrievalResult {
	return slices.Clone(b.beam)
}

// Score 返回累积分数（对齐 score property）。
func (b *TripleBeam) Score() float64 {
	return b.score
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run 'TestNewTripleBeam|TestTripleBeam' -v -count=1`
Expected: 10 例全 PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/common/triple_beam.go internal/agentcore/retrieval/common/triple_beam_test.go
git commit -m "feat(common): 13.22 TripleBeam beam search 路径载体（构造建 text 去重集合/负索引/浅拷贝密封）"
git status --short && git show --stat HEAD
```
核对：仅 2 文件，无搭便车。

---

### Task 2: TripleFact + TripleMemory 事实记忆

**Files:**
- Create: `internal/agentcore/retrieval/common/triple_memory.go`
- Test: `internal/agentcore/retrieval/common/triple_memory_test.go`

- [ ] **Step 1: 写失败测试（M1-M9 全量）**

创建 `internal/agentcore/retrieval/common/triple_memory_test.go`：

```go
package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewTripleMemory 零值状态（M1）
func TestNewTripleMemory(t *testing.T) {
	m := NewTripleMemory()
	assert.Equal(t, 0, m.Len())
	assert.Equal(t, "", m.TriplesStr())
}

// TestTripleMemory_ExtendMemory首添 首条入列（M2）
func TestTripleMemory_ExtendMemory首添(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	assert.Equal(t, 1, m.Len())
	assert.Equal(t, []TripleFact{{"Alice", "knows", "Bob"}}, m.Memory())
}

// TestTripleMemory_同串去重 相同事实二次 Extend 不重复入列（M3）
func TestTripleMemory_同串去重(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	assert.Equal(t, 1, m.Len())
}

// TestTripleMemory_大小写不同视为重复 去重键逐项 lower（M4，_tuple2str 语义钉死）
func TestTripleMemory_大小写不同视为重复(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "Knows", "BOB"})
	m.ExtendMemory(TripleFact{"alice", "knows", "bob"})
	assert.Equal(t, 1, m.Len(), "去重键小写化后应视为同一事实")
	// 保留首见原貌
	assert.Equal(t, []TripleFact{{"Alice", "Knows", "BOB"}}, m.Memory())
}

// TestTripleMemory_BatchExtendMemory含重复 批量 [x,y,x] → [x,y]（M5）
func TestTripleMemory_BatchExtendMemory含重复(t *testing.T) {
	m := NewTripleMemory()
	m.BatchExtendMemory([]TripleFact{
		{"A", "r1", "B"},
		{"B", "r2", "C"},
		{"A", "r1", "B"},
	})
	assert.Equal(t, 2, m.Len())
	assert.Equal(t, []TripleFact{{"A", "r1", "B"}, {"B", "r2", "C"}}, m.Memory())
}

// TestTripleMemory_TriplesStr格式 单条 "(s p o)"，多条换行连接（M6）
func TestTripleMemory_TriplesStr格式(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	assert.Equal(t, "(Alice knows Bob)", m.TriplesStr())

	m2 := NewTripleMemory()
	m2.BatchExtendMemory([]TripleFact{
		{"A", "r1", "B"},
		{"B", "r2", "C"},
	})
	assert.Equal(t, "(A r1 B)\n(B r2 C)", m2.TriplesStr())
}

// TestTripleMemory_TriplesStr保留原大小写 展示串不 lower（M7，与去重键两套逻辑钉死）
func TestTripleMemory_TriplesStr保留原大小写(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Foo", "Is", "Bar"})
	assert.Equal(t, "(Foo Is Bar)", m.TriplesStr())
}

// TestTripleMemory_Memory密封 跨对象写穿防护（M8，C2 同链）
// 注：内存 array 与 slice 不同——Memory() 返回新 slice，append
// 永不写穿；用与 B8 对称的跨对象形态钉死决策
func TestTripleMemory_Memory密封(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"A", "r1", "B"})
	got := m.Memory()
	got = append(got, TripleFact{"X", "y", "Z"})
	assert.Equal(t, 1, m.Len(), "对返回值 append 后内部长度不应变化")
	assert.Equal(t, []TripleFact{{"A", "r1", "B"}}, m.Memory())
}

// TestTripleMemory_Len 随去重累积计数（M9）
func TestTripleMemory_Len(t *testing.T) {
	m := NewTripleMemory()
	assert.Equal(t, 0, m.Len())
	m.BatchExtendMemory([]TripleFact{{"A", "r", "B"}, {"C", "r", "D"}})
	assert.Equal(t, 2, m.Len())
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run 'TestNewTripleMemory|TestTripleMemory' -v -count=1`
Expected: 编译失败 `undefined: TripleFact` / `undefined: NewTripleMemory`

- [ ] **Step 3: 写实现**

创建 `internal/agentcore/retrieval/common/triple_memory.go`：

```go
package common

import (
	"slices"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TripleMemory 多跳检索的事实记忆（AgenticRetriever 消费）。
//
// includedTriples 以小写化 join 字符串为键跨轮去重；memory 保序累积；
// TriplesStr 渲染为提示词注入串。单 goroutine 使用（Python 无锁，
// 13.25 消费同为单协程迭代循环），不加 mutex（spec 决策 7）。
//
// Python: openjiuwen/core/retrieval/common/triple_memory.py
type TripleMemory struct {
	// includedTriples 去重键集合（tuple2str：逐项 lower 后空格 join）
	includedTriples map[string]struct{}
	// memory 保序累积的事实列表
	memory []TripleFact
}

// ──────────────────────────── 枚举 ────────────────────────────

// TripleFact 读取侧三元组事实（定长 3 元：主语/谓语/宾语）。
//
// Python 对应物是 tuple[str, ...] 变长元组——_read LLM 输出经
// len(triple) == 3 过滤后恒为 3 元，Go 定长数组编译期收窄（偏差
// 归档 spec §8-2）。写入侧结构化模型见 Triple（13.17，带 Metadata），
// 两侧管线分立不复用。
type TripleFact = [3]string

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTripleMemory 创建空记忆（includedTriples 集合初始化；零值结构体
// 不可用，map 未初始化写入会 panic）。
func NewTripleMemory() *TripleMemory {
	return &TripleMemory{
		includedTriples: make(map[string]struct{}),
	}
}

// Len 返回已累积事实数（对齐 __len__）。
func (m *TripleMemory) Len() int {
	return len(m.memory)
}

// Memory 返回事实列表副本（浅拷贝密封，偏差归档 spec §8-1）。
func (m *TripleMemory) Memory() []TripleFact {
	return slices.Clone(m.memory)
}

// TriplesStr 渲染记忆为多行字符串，每行 "(s p o)"（空格 join 三元素 +
// 圆括号包裹 + 换行连接，对齐 triples_str property）。不做小写化——
// 与去重键 tuple2str 两套逻辑，展示串保留原大小写（Python 同款
// 不对称，测试 M7 钉死）。
func (m *TripleMemory) TriplesStr() string {
	formatted := make([]string, 0, len(m.memory))
	for _, triple := range m.memory {
		formatted = append(formatted, "("+strings.Join(triple[:], " ")+")")
	}
	return strings.Join(formatted, "\n")
}

// ExtendMemory 追加单条事实：小写化 join 键已存在则跳过（去重），
// 否则入集合并保序追加（对齐 extend_memory）。
func (m *TripleMemory) ExtendMemory(newTriple TripleFact) {
	newTripleStr := tuple2str(newTriple)
	if _, ok := m.includedTriples[newTripleStr]; !ok {
		m.includedTriples[newTripleStr] = struct{}{}
		m.memory = append(m.memory, newTriple)
	}
}

// BatchExtendMemory 批量追加（循环委托 ExtendMemory，对齐 batch_extend_memory）。
func (m *TripleMemory) BatchExtendMemory(newTriples []TripleFact) {
	for _, tmpTriple := range newTriples {
		m.ExtendMemory(tmpTriple)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tuple2str 三元组转去重键字符串：逐项 lower 后空格 join
// （对齐 _tuple2str 的 item.lower() 语义——非整体 lower）。
func tuple2str(newTriple TripleFact) string {
	lowered := make([]string, len(newTriple))
	for i, item := range newTriple {
		lowered[i] = strings.ToLower(item)
	}
	return strings.Join(lowered, " ")
}
```

注意：`strings.Join(triple[:], " ")` 中 `triple[:]` 将 `[3]string` 数组转切片传入 Join——这是数组类型使用 strings.Join 的标准写法。

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run 'TestNewTripleMemory|TestTripleMemory' -v -count=1`
Expected: 9 例全 PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/common/triple_memory.go internal/agentcore/retrieval/common/triple_memory_test.go
git commit -m "feat(common): 13.22 TripleMemory 事实记忆 + TripleFact 定长类型（lower 去重键/TriplesStr 保序渲染）"
git status --short && git show --stat HEAD
```
核对：仅 2 文件。

---

### Task 3: MultiKBRetrievalResult 出口契约

**Files:**
- Modify: `internal/agentcore/retrieval/common/result.go`（文件尾追加）
- Test: `internal/agentcore/retrieval/common/result_test.go`（文件尾追加）

- [ ] **Step 1: 写失败测试（R1-R4）**

在 `internal/agentcore/retrieval/common/result_test.go` 文件尾追加：

```go

// TestMultiKBRetrievalResult_全字段构造 六字段读写一致（R1）
func TestMultiKBRetrievalResult_全字段构造(t *testing.T) {
	r := MultiKBRetrievalResult{
		Text:           "测试文本",
		Score:          0.9,
		RawScore:       0.85,
		RawScoreScaled: 0.8,
		KbIDs:          []string{"kb-1", "kb-2"},
		Metadata:       map[string]any{"source": "web"},
	}
	assert.Equal(t, "测试文本", r.Text)
	assert.Equal(t, 0.9, r.Score)
	assert.Equal(t, 0.85, r.RawScore)
	assert.Equal(t, 0.8, r.RawScoreScaled)
	assert.Equal(t, []string{"kb-1", "kb-2"}, r.KbIDs)
	assert.Equal(t, map[string]any{"source": "web"}, r.Metadata)
}

// TestMultiKBRetrievalResult_JSON字段名 序列化含六键名（R2）
func TestMultiKBRetrievalResult_JSON字段名(t *testing.T) {
	r := MultiKBRetrievalResult{
		Text:           "text",
		Score:          0.9,
		RawScore:       0.85,
		RawScoreScaled: 0.8,
		KbIDs:          []string{"kb-1"},
		Metadata:       map[string]any{},
	}
	data, err := json.Marshal(r)
	assert.NoError(t, err)
	s := string(data)
	for _, key := range []string{`"text"`, `"score"`, `"raw_score"`, `"raw_score_scaled"`, `"kb_ids"`, `"metadata"`} {
		assert.Contains(t, s, key)
	}
}

// TestMultiKBRetrievalResult_KbIDs无omitempty nil 序列化为 null 而非省略（R3，default_factory 先例）
func TestMultiKBRetrievalResult_KbIDs无omitempty(t *testing.T) {
	r := MultiKBRetrievalResult{Text: "text", Score: 0.9}
	data, err := json.Marshal(r)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"kb_ids":null`)
}

// TestMultiKBRetrievalResult_Metadata无omitempty 同款注记（R4）
func TestMultiKBRetrievalResult_Metadata无omitempty(t *testing.T) {
	r := MultiKBRetrievalResult{Text: "text", Score: 0.9}
	data, err := json.Marshal(r)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"metadata":null`)
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run 'TestMultiKBRetrievalResult' -v -count=1`
Expected: 编译失败 `undefined: MultiKBRetrievalResult`

- [ ] **Step 3: 写实现**

在 `internal/agentcore/retrieval/common/result.go` 文件尾（RetrievalResult 结构体后）追加：

```go

// MultiKBRetrievalResult 多知识库聚合检索结果数据模型。
//
// 多 KB 并行检索按 text 聚合去重后的出口契约：Score 为跨 KB 融合
// 最大值，RawScore/RawScoreScaled 保真源库分数，KbIDs 记录命中的
// 全部知识库。
//
// Python: openjiuwen/core/retrieval/common/retrieval_result.py (MultiKBRetrievalResult)
// RawScore/RawScoreScaled 用 float64 非指针——13.27 构造点默认值
// 回落使实际数据流恒非 None；metadata 显式 None 理论路径归档 spec §8-3。
type MultiKBRetrievalResult struct {
	// Text 文本内容（聚合键）
	Text string `json:"text"`
	// Score 跨 KB 融合分数（最大值）
	Score float64 `json:"score"`
	// RawScore 源库原始分数
	RawScore float64 `json:"raw_score"`
	// RawScoreScaled 缩放到 [0,1] 的原始分数
	RawScoreScaled float64 `json:"raw_score_scaled"`
	// KbIDs 命中的知识库 ID 列表（无 omitempty——default_factory 先例，
	// nil 序列化为 null 由构造方初始化保证，同 Metadata 契约）
	KbIDs []string `json:"kb_ids"`
	// Metadata 元数据（同上无 omitempty）
	Metadata map[string]any `json:"metadata"`
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -run 'TestMultiKBRetrievalResult' -v -count=1`
Expected: 4 例全 PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/common/result.go internal/agentcore/retrieval/common/result_test.go
git commit -m "feat(common): 13.22 MultiKBRetrievalResult 多 KB 聚合出口契约（raw_score float64 直装/KbIDs 无 omitempty）"
git status --short && git show --stat HEAD
```
核对：仅 2 文件。

---

### Task 4: doc.go 包文档更新

**Files:**
- Modify: `internal/agentcore/retrieval/common/doc.go`

- [ ] **Step 1: 更新文件目录段**

`doc.go` 文件目录改为（result.go 行改描述 + 插入两新文件，按 doc.go→核心类型→辅助→配置排序惯例，triple 系文件相邻）：

```go
//	common/
//	├── doc.go           # 包文档
//	├── config.go        # 索引/知识库/检索/向量存储配置类型
//	├── document.go      # MultimodalDocument + TextChunk 文档数据模型
//	├── triple.go        # Triple 三元组数据模型（OpenIE 抽取产物，写入侧）
//	├── triple_beam.go   # TripleBeam beam search 路径载体（读取侧）
//	├── triple_memory.go # TripleMemory 事实记忆 + TripleFact 定长类型（读取侧）
//	├── callbacks.go     # DocIndexCallback 类型别名 + NoOpDocIndexCallback + LoggingDocIndexCallback
//	└── result.go        # SearchResult + RetrievalResult + MultiKBRetrievalResult 数据模型
```

- [ ] **Step 2: 更新 Python 对应段与核心类型索引**

Python 对应段追加两行（triple_memory.py 插在 triple.py 后）：

```go
//	openjiuwen/core/retrieval/common/triple_beam.py
//	openjiuwen/core/retrieval/common/triple_memory.py
```

核心类型索引在 `RetrievalResult` 行后追加：

```go
//	TripleBeam         — beam search 路径载体（读取侧）
//	TripleFact         — 读取侧三元组事实（定长 3 元）
//	TripleMemory       — 多跳检索事实记忆
//	MultiKBRetrievalResult — 多 KB 聚合检索结果
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/common/`
Expected: 无输出（编译通过；doc.go 纯注释变更）

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/common/doc.go
git commit -m "docs(common): 13.22 doc.go 增补 triple_beam/triple_memory/MultiKBRetrievalResult 条目"
git status --short && git show --stat HEAD
```
核对：仅 1 文件。

---

### Task 5: 包级全量回归 + 覆盖率 + IMPLEMENTATION_PLAN 回填

**Files:**
- Modify: `/home/opensource/uapclaw-gateway/IMPLEMENTATION_PLAN.md`（仅 13.22 一行）

- [ ] **Step 1: 包级全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -v -count=1 2>&1 | tail -15`
Expected: 全部 PASS，无 FAIL（含既有 callbacks/config/document/result/triple 测试）

- [ ] **Step 2: 覆盖率验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/ -cover -count=1`
Expected: coverage ≥ 90%（13.17 基线 91.8%，纯增量无死分支应维持或小升）

- [ ] **Step 3: 全仓编译回归（CI 同款标签）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go build -tags "sqlite_fts5 test" ./... && go vet ./internal/agentcore/retrieval/common/`
Expected: 编译零错误；vet 零告警

- [ ] **Step 4: IMPLEMENTATION_PLAN.md 13.22 行回填**

`☐` 改 `✅`，产出描述列填入：

```
| 13.22 | ✅ | 读取侧数据模型 | `TripleBeam(构造建 text 去重集合+Contains 按 text 判重+Get 负索引越界自然 panic+Triples slices.Clone 浅拷贝密封防 append 写穿) + TripleFact=[3]string 定长(读取侧类型与写入侧 Triple 分立,_read len==3 收窄) + TripleMemory(lower join 去重键跨轮去重+memory 保序+TriplesStr "(s p o)" 换行渲染不大写化与去重键两套逻辑+零 mutex 单协程) + MultiKBRetrievalResult(raw_score float64 直装 None 边界归档+KbIDs []string 无 omitempty default_factory 先例)；Python 0 专属用例 Go 23 例全增强；6 偏差归档 spec §8` | `openjiuwen/core/retrieval/common/triple_beam.py + triple_memory.py + retrieval_result.py` |
```

注意：**不触碰 13.23-13.28 已登记行**；验证点行保持 ☐；8.25 ⤴️ / 7.36 ⤵️ 联动行原样。

- [ ] **Step 5: 提交回填**

```bash
cd /home/opensource/uapclaw-gateway
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 13.22 读取侧数据模型完成回填（读取侧三件套交付）"
git status --short && git show --stat HEAD
```
核对：仅 1 文件（IMPLEMENTATION_PLAN.md 此前已有 10.6.24a 的未提交修改——**先 `git diff IMPLEMENTATION_PLAN.md` 确认本次 diff 只含 13.22 行**，若混有 10.6.24a 改动则暂缓提交并上报）。

---

## 完成核对清单（实施后逐项）

1. [ ] 五任务五提交全绿；`git log --oneline -5` 核对提交序列
2. [ ] common 包覆盖率 ≥90%（记录实际值）
3. [ ] 23 测试用例全 PASS（B×10 + M×9 + R×4）
4. [ ] 全仓 CI 同款标签编译零错误
5. [ ] IMPLEMENTATION_PLAN.md 13.22 ✅；13.23-13.28 行未动
6. [ ] spec §9 回填清单逐项核对（零错误码零日志无台账项）
7. [ ] 记忆更新：13.22 完成态（三拍板决策落地 + 13.23 消费指引）
