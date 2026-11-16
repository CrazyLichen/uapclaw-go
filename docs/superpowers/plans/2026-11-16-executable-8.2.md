# 8.2 Executable / ExecutableGraph 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 8.2 能力契约层——泛型 `Executable[I,O]` 接口（四执行入口 + 四策略钩子）、`StreamChunk` 流式元素、`BaseExecutable` 嵌入基座、`ExecutableGraph` 可执行图契约与 `InputsKey/ConfigKey` 信封常量，一比一对齐 Python `executable.py` + `base.py`。

**Architecture:** 包 `internal/agentcore/graph/`（复用 8.8/8.10 已建包根），新增两个源文件各对一个 Python 文件（executable.py → executable.go；base.py 的 ExecutableGraph/Router 部分 → executable_graph.go）。流式语义用 `<-chan StreamChunk[O]`（仓内 AssistantMessageChunk 惯例），Python 继承默认行为由 `BaseExecutable` 嵌入基座承载。`Router` 不另造类型，指向 8.5 已有 `pregel.Selector`。

**Tech Stack:** Go 泛型、`internal/agentcore/session`（BaseSession）、标准库 testing；spec：`docs/superpowers/specs/2026-11-16-executable-8.2-design.md`。

**偏差对照（spec §4）：** D1 继承→嵌入基座；D2 NotImplementedError 文案不带类名；D3 输入流异常→ctx 取消+channel 关闭；D4 `_invoke` → 导出 `Invoke_`；D5 默认 pass 语义锚点（零值+nil error）；D6 GeneralExecutor 死别名纯复刻。

---

### Task 1: StreamChunk + Executable 接口 + BaseExecutable 基座（executable.go）

**Files:**
- Create: `internal/agentcore/graph/executable.go`
- Test: `internal/agentcore/graph/executable_test.go`
- Modify: `internal/agentcore/graph/doc.go`

- [ ] **Step 1: 写失败测试（executable_test.go）**

```go
package graph

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 测试工具 ────────────────────────────

// fakeSession 测试用空会话
type fakeSession struct {
	session.BaseSession
}

// ──────────────────────────── Executable 接口 ────────────────────────────

// TestBaseExecutable_四钩子默认值 断言四个策略钩子的默认值
// （重点：PostCommit 默认 true，与其他钩子不对称，spec §3.3）
func TestBaseExecutable_四钩子默认值(t *testing.T) {
	var base BaseExecutable[map[string]any, map[string]any]
	if base.SkipTrace() != false {
		t.Error("SkipTrace 默认应为 false")
	}
	if base.GraphInvoker() != false {
		t.Error("GraphInvoker 默认应为 false")
	}
	if base.PostCommit() != true {
		t.Error("PostCommit 默认应为 true（对齐 executable.py:56-57 默认值不对称）")
	}
	if base.ComponentType() != "" {
		t.Error("ComponentType 默认应为空串")
	}
}

// TestBaseExecutable_OnInvoke默认抛错 断言四个 On* 默认返回 NotImplementedError 复刻
func TestBaseExecutable_OnInvoke默认抛错(t *testing.T) {
	var base BaseExecutable[map[string]any, map[string]any]
	sess := fakeSession{}
	ctx := context.Background()

	_, err := base.OnInvoke(ctx, map[string]any{}, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnInvoke") {
		t.Errorf("OnInvoke 默认应返回未实现错误且含方法名, got %v", err)
	}

	_, err = base.OnStream(ctx, map[string]any{}, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnStream") {
		t.Errorf("OnStream 默认应返回未实现错误且含方法名, got %v", err)
	}

	_, err = base.OnCollect(ctx, nil, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnCollect") {
		t.Errorf("OnCollect 默认应返回未实现错误且含方法名, got %v", err)
	}

	_, err = base.OnTransform(ctx, nil, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnTransform") {
		t.Errorf("OnTransform 默认应返回未实现错误且含方法名, got %v", err)
	}
}

// TestBaseExecutable_OnInvoke错误文案 断言错误文案复刻 executable.py:16-21 提示语义
func TestBaseExecutable_OnInvoke错误文案(t *testing.T) {
	var base BaseExecutable[map[string]any, map[string]any]
	_, err := base.OnInvoke(context.Background(), map[string]any{}, fakeSession{}, nil)
	if err == nil {
		t.Fatal("err 不应为 nil")
	}
	// 对齐 Python 文案语义：未实现 + 请在子类覆写 + 提供推理逻辑
	for _, want := range []string{"未实现", "覆写"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误文案应含 %q, got: %s", want, err.Error())
		}
	}
	// 偏差 D2：Go 侧文案不带实现类名（Python 为 type(self).__name__）
}

// generalExecutorHolder 编译期断言 GeneralExecutor 别名可实例化（D6 死别名纯复刻）
type generalExecutorComp struct {
	BaseExecutable[map[string]any, map[string]any]
}

func (generalExecutorComp) OnInvoke(_ context.Context, inputs map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return inputs, nil
}

// TestGeneralExecutor_别名实例化 断言 map 载荷往返（spec §5 用例 5）
func TestGeneralExecutor_别名实例化(t *testing.T) {
	var exec GeneralExecutor = generalExecutorComp{}
	got, err := exec.OnInvoke(context.Background(), map[string]any{"k": "v"}, fakeSession{}, nil)
	if err != nil {
		t.Fatalf("OnInvoke 不应报错: %v", err)
	}
	if got["k"] != "v" {
		t.Errorf("map 载荷应原样返回, got %v", got)
	}
}

// ──────────────────────────── StreamChunk 契约 ────────────────────────────

// streamComp 测试用流式组件：产两块正常数据后送达错误并关闭
type streamComp struct {
	BaseExecutable[map[string]any, map[string]any]
	streamErr error
}

func (c streamComp) OnStream(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[map[string]any], error) {
	ch := make(chan StreamChunk[map[string]any], 3)
	ch <- StreamChunk[map[string]any]{Val: map[string]any{"i": 1}}
	ch <- StreamChunk[map[string]any]{Val: map[string]any{"i": 2}}
	ch <- StreamChunk[map[string]any]{Err: c.streamErr}
	close(ch)
	return ch, nil
}

// TestStreamChunk_错误末元素后关闭 断言流中错误作为末元素送达、随后 channel 关闭（spec §5 用例 4）
func TestStreamChunk_错误末元素后关闭(t *testing.T) {
	wantErr := errors.New("mid-stream failure")
	comp := streamComp{streamErr: wantErr}
	ch, err := comp.OnStream(context.Background(), map[string]any{}, fakeSession{}, nil)
	if err != nil {
		t.Fatalf("启动错误不应发生: %v", err)
	}

	var vals []int
	var gotErr error
	for chunk := range ch {
		if chunk.Err != nil {
			gotErr = chunk.Err
			break
		}
		vals = append(vals, chunk.Val["i"].(int))
	}
	if len(vals) != 2 {
		t.Errorf("错误前应收到 2 个正常块, got %d", len(vals))
	}
	if !errors.Is(gotErr, wantErr) {
		t.Errorf("末元素错误应与预期一致, got %v", gotErr)
	}
	// 错误后 channel 已关闭：再次 range 立即结束
	if _, ok := <-ch; ok {
		t.Error("错误送达后 channel 应已关闭")
	}
}

// TestStreamChunk_正常流关闭不带Err 断言正常结束的流不含 Err 元素
func TestStreamChunk_正常流关闭不带Err(t *testing.T) {
	ch := make(chan StreamChunk[map[string]any], 1)
	ch <- StreamChunk[map[string]any]{Val: map[string]any{"done": true}}
	close(ch)
	for chunk := range ch {
		if chunk.Err != nil {
			t.Errorf("正常流不应携带 Err, got %v", chunk.Err)
		}
	}
}

// 编译期断言：嵌入基座的组件满足 Executable 接口
var _ Executable[map[string]any, map[string]any] = (*streamComp)(nil)
var _ = io.Discard // 占位保持 import 稳定（实施时若未用 io 可删除此行与 import）
```

> 注意：`io` import 若未使用会导致编译失败，实施时删掉 `_ = io.Discard` 与 import 行（上面的占位仅为提示测试文件可独立编译）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/graph/ -run 'TestBaseExecutable|TestGeneralExecutor|TestStreamChunk' -v`
Expected: FAIL，`undefined: BaseExecutable` / `undefined: StreamChunk` / `undefined: Executable` / `undefined: GeneralExecutor`

- [ ] **Step 3: 最小实现（executable.go）**

```go
package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamChunk 流式元素：Val 为正常产出，Err 非 nil 表示流中错误（此后流关闭）。
// 对齐仓内流式惯例（AssistantMessageChunk 内嵌 error 字段先例）；
// Python AsyncIterator 迭代中途 raise ≙ 消费方收到 Err 元素后停止迭代（spec 偏差 D3）。
type StreamChunk[O any] struct {
	// Val 正常产出块
	Val O
	// Err 流中错误；非 nil 时为流的最后一个元素，送达后生产方关闭 channel
	Err error
}

// BaseExecutable Executable 嵌入基座：提供四钩子默认值与四入口默认未实现错误。
// 对齐 Python Executable 基类默认实现；组件嵌入后按需覆写
// （Python 继承 → Go 嵌入，spec 偏差 D1）。
type BaseExecutable[I, O any] struct{}

// ──────────────────────────── 接口 ────────────────────────────

// Executable 可执行节点契约，对齐 Python Executable[Input, Output] 泛型基类
// （executable.py:13-61）。四执行入口构成"单/流 × 单/流"矩阵，
// 四策略钩子由 8.11 Vertex 在节点执行时读取。
type Executable[I, O any] interface {
	// OnInvoke 单进单出（Python on_invoke，executable.py:14）
	OnInvoke(ctx context.Context, inputs I, sess session.BaseSession, kwargs map[string]any) (O, error)
	// OnStream 单进流出（Python on_stream，executable.py:23）；
	// 第二返回值承载启动错误，流中错误经 StreamChunk.Err 作为末元素送达
	OnStream(ctx context.Context, inputs I, sess session.BaseSession, kwargs map[string]any) (<-chan StreamChunk[O], error)
	// OnCollect 流进单出（Python on_collect，executable.py:32）：聚合上游多块输入为一份输出
	OnCollect(ctx context.Context, inputs <-chan I, sess session.BaseSession, kwargs map[string]any) (O, error)
	// OnTransform 流进流出（Python on_transform，executable.py:41）：逐块变换转发
	OnTransform(ctx context.Context, inputs <-chan I, sess session.BaseSession, kwargs map[string]any) (<-chan StreamChunk[O], error)
	// SkipTrace 是否跳过 tracer 埋点（默认 false；Python executable.py:50）
	SkipTrace() bool
	// GraphInvoker 是否子图递归入口（默认 false；Python executable.py:53）
	GraphInvoker() bool
	// PostCommit 执行完是否提交 WorkflowState（默认 true，与其他钩子不对称；Python executable.py:56）
	PostCommit() bool
	// ComponentType 组件类型标签，写进 span（默认空串；Python executable.py:59）
	ComponentType() string
}

// ──────────────────────────── 枚举 ────────────────────────────

// （本节无枚举）

// ──────────────────────────── 常量 ────────────────────────────

// （本节无常量）

// ──────────────────────────── 全局变量 ────────────────────────────

// （本节无全局变量）

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── BaseExecutable 方法 ────────────────────────────

// SkipTrace 默认不跳过埋点（Python executable.py:50-51）
func (BaseExecutable[I, O]) SkipTrace() bool { return false }

// GraphInvoker 默认非子图入口（Python executable.py:53-54）
func (BaseExecutable[I, O]) GraphInvoker() bool { return false }

// PostCommit 默认提交状态（Python executable.py:56-57，默认值与其他钩子不对称）
func (BaseExecutable[I, O]) PostCommit() bool { return true }

// ComponentType 默认空标签（Python executable.py:59-60）
func (BaseExecutable[I, O]) ComponentType() string { return "" }

// OnInvoke 默认未实现（Python executable.py:14-21 文案语义复刻；偏差 D2 不带类名）
func (BaseExecutable[I, O]) OnInvoke(_ context.Context, _ I, _ session.BaseSession, _ map[string]any) (O, error) {
	var zero O
	return zero, errors.New("OnInvoke 未实现：请在 Executable 子类覆写本方法以提供推理逻辑（对齐 Python on_invoke 抽象语义）")
}

// OnStream 默认未实现（Python executable.py:23-30；偏差 D2）
func (BaseExecutable[I, O]) OnStream(_ context.Context, _ I, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[O], error) {
	return nil, errors.New("OnStream 未实现：请在 Executable 子类覆写本方法以提供流式逻辑（对齐 Python on_stream 抽象语义）")
}

// OnCollect 默认未实现（Python executable.py:32-39；偏差 D2）
func (BaseExecutable[I, O]) OnCollect(_ context.Context, _ <-chan I, _ session.BaseSession, _ map[string]any) (O, error) {
	var zero O
	return zero, errors.New("OnCollect 未实现：请在 Executable 子类覆写本方法以提供聚合逻辑（对齐 Python on_collect 抽象语义）")
}

// OnTransform 默认未实现（Python executable.py:41-48；偏差 D2）
func (BaseExecutable[I, O]) OnTransform(_ context.Context, _ <-chan I, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[O], error) {
	return nil, errors.New("OnTransform 未实现：请在 Executable 子类覆写本方法以提供变换逻辑（对齐 Python on_transform 抽象语义）")
}

// ──────────────────────────── 导出类型别名 ────────────────────────────

// GeneralExecutor 最一般的执行器：map 载荷进、map 载荷出。
// 对齐 Python executable.py:63 同名别名（dict[str, Any] → map[string]any）；
// Python 全仓零消费死别名，纯复刻不发明调用方（spec 偏差 D6）。
type GeneralExecutor = Executable[map[string]any, map[string]any]

// 未使用占位：fmt 供后续错误文案格式化（实施时若无使用可删除）
var _ = fmt.Sprintf
```

> 注意：`fmt` 若未使用编译会失败——上面 `var _ = fmt.Sprintf` 行与 import 按需保留或删除。声明排列遵守规范 2（结构体→接口→方法→别名）；接口在结构体之后是本文件特例（StreamChunk 是 Executable 签名的依赖类型，须先声明，`ExecutableGraph` 文件同理）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/graph/ -run 'TestBaseExecutable|TestGeneralExecutor|TestStreamChunk' -v`
Expected: PASS（7 个用例全绿）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/executable.go internal/agentcore/graph/executable_test.go
git commit -m "feat(graph): 8.2 Executable 泛型接口 + StreamChunk + BaseExecutable 基座"
```

---

### Task 2: ExecutableGraph 接口 + 信封常量（executable_graph.go）

**Files:**
- Create: `internal/agentcore/graph/executable_graph.go`
- Test: `internal/agentcore/graph/executable_graph_test.go`

- [ ] **Step 1: 写失败测试（executable_graph_test.go）**

```go
package graph

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 测试工具 ────────────────────────────

// fakeGraph 满足 ExecutableGraph 的最小 fake：仅实现 Invoke_ 与四钩子（继承基座）
type fakeGraph struct {
	BaseExecutable[map[string]any, map[string]any]
	invokeArgs map[string]any
	config     any
}

func (f *fakeGraph) Invoke_(inputs map[string]any, _ session.BaseSession, config any) (map[string]any, error) {
	f.invokeArgs = inputs
	f.config = config
	return map[string]any{"ok": true}, nil
}

// 编译期断言：fakeGraph 满足 ExecutableGraph 接口
var _ ExecutableGraph[map[string]any, map[string]any] = (*fakeGraph)(nil)

// ──────────────────────────── 信封常量 ────────────────────────────

// Test信封常量 断言键名对齐 Python INPUTS_KEY/CONFIG_KEY（constant.py:16-17）
func Test信封常量(t *testing.T) {
	if InputsKey != "inputs" {
		t.Errorf("InputsKey 应为 inputs, got %q", InputsKey)
	}
	if ConfigKey != "config" {
		t.Errorf("ConfigKey 应为 config, got %q", ConfigKey)
	}
}

// ──────────────────────────── Invoke 信封拆分 ────────────────────────────

// TestExecutableGraph_Invoke拆信封 断言 Invoke 拆出 inputs/config 后转发 Invoke_
// （Python base.py:13-14：inputs.get(INPUTS_KEY), inputs.get(CONFIG_KEY)）
func TestExecutableGraph_Invoke拆信封(t *testing.T) {
	g := &fakeGraph{}
	cfg := map[string]any{"recursion_limit": 25}
	got, err := GraphInvoke[map[string]any, map[string]any](g,
		map[string]any{InputsKey: map[string]any{"q": "hi"}, ConfigKey: cfg}, nil)
	if err != nil {
		t.Fatalf("Invoke 不应报错: %v", err)
	}
	if got["ok"] != true {
		t.Errorf("应转发 Invoke_ 返回值, got %v", got)
	}
	if g.invokeArgs["q"] != "hi" {
		t.Errorf("Invoke_ 应收到拆出的业务输入, got %v", g.invokeArgs)
	}
	if g.config == nil || g.config.(map[string]any)["recursion_limit"] != 25 {
		t.Errorf("Invoke_ 应收到拆出的 config, got %v", g.config)
	}
}

// TestExecutableGraph_Invoke_config缺失宽容 断言 config 缺失传 nil 不报错
// （Python .get(CONFIG_KEY) 无默认值 → None；业务输入缺失硬错是消费方语义，留 8.20）
func TestExecutableGraph_Invoke_config缺失宽容(t *testing.T) {
	g := &fakeGraph{}
	_, err := GraphInvoke[map[string]any, map[string]any](g,
		map[string]any{InputsKey: map[string]any{}}, nil)
	if err != nil {
		t.Fatalf("config 缺失应宽容: %v", err)
	}
	if g.config != nil {
		t.Errorf("config 缺失时应传 nil, got %v", g.config)
	}
}

// TestExecutableGraph_Invoke_inputs缺失宽容 断言 inputs 键缺失传 nil（.get 宽容语义）
func TestExecutableGraph_Invoke_inputs缺失宽容(t *testing.T) {
	g := &fakeGraph{}
	_, err := GraphInvoke[map[string]any, map[string]any](g, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("inputs 键缺失应宽容: %v", err)
	}
	if g.invokeArgs != nil {
		t.Errorf("inputs 键缺失时应传 nil, got %v", g.invokeArgs)
	}
}

// TestExecutableGraph_Invoke_信封nil宽容 断言整个信封为 nil 时不 panic
func TestExecutableGraph_Invoke_信封nil宽容(t *testing.T) {
	g := &fakeGraph{}
	_, err := GraphInvoke[map[string]any, map[string]any](g, nil, nil)
	if err != nil {
		t.Fatalf("信封 nil 应宽容: %v", err)
	}
}

// ──────────────────────────── 默认 pass 语义（spec 偏差 D5） ────────────────────────────

// TestExecutableGraph_默认pass语义 断言 Stream/Collect/Transform/Interrupt 默认空实现
// （Python base.py:16-26 为 pass，与父类抛 NotImplementedError 不同；流式返回已关闭空 channel）
func TestExecutableGraph_默认pass语义(t *testing.T) {
	g := &fakeGraph{}
	ctx := context.Background()

	ch, err := GraphStream[map[string]any, map[string]any](g, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Stream 默认不应报错: %v", err)
	}
	if _, ok := <-ch; ok {
		t.Error("Stream 默认应返回已关闭的空 channel")
	}

	if _, err := GraphCollect[map[string]any, map[string]any](g, nil, nil); err != nil {
		t.Errorf("Collect 默认不应报错: %v", err)
	}

	ch2, err := GraphTransform[map[string]any, map[string]any](g, nil, nil)
	if err != nil {
		t.Fatalf("Transform 默认不应报错: %v", err)
	}
	if _, ok := <-ch2; ok {
		t.Error("Transform 默认应返回已关闭的空 channel")
	}

	if err := g.Interrupt(nil); err != nil {
		t.Errorf("Interrupt 默认不应报错: %v", err)
	}
	_ = ctx
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/graph/ -run 'TestExecutableGraph|Test信封常量' -v`
Expected: FAIL，`undefined: ExecutableGraph` / `undefined: InputsKey` / `undefined: GraphInvoke`

- [ ] **Step 3: 最小实现（executable_graph.go）**

```go
package graph

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InputsKey 顶层调用信封中业务输入的键（Python base.py:7 INPUTS_KEY = "inputs"）。
	// 打包只发生在跨图/跨组件边界（workflow.py:527/600/633、vertex.py:137/156），
	// 节点到节点内部传递不重新打包。
	InputsKey = "inputs"
	// ConfigKey 顶层调用信封中执行配置的键（Python base.py:7 CONFIG_KEY = "config"）
	ConfigKey = "config"
)

// ──────────────────────────── 接口 ────────────────────────────

// ExecutableGraph 可执行图契约，对齐 Python base.py:12-30 ExecutableGraph(Executable[I,O])。
// 在 Executable 之上追加：Invoke 信封拆分、默认 pass 的流式四方法（偏差 D5，
// 与父类抛 NotImplementedError 不同——Python 侧本类是 pass 静默空实现）、
// 抽象 _invoke 的导出对应物 Invoke_（偏差 D4，8.1 CompiledGraph 实现并消费）。
type ExecutableGraph[I, O any] interface {
	Executable[I, O]
	// Invoke_ 拆信封后的抽象执行入口（Python 抽象方法 _invoke，base.py:28-30；
	// Go 接口方法须导出，下划线后缀保留 Python 名字痕迹，偏差 D4）
	Invoke_(inputs I, sess session.BaseSession, config any) (O, error)
	// Interrupt 中断图执行（Python base.py:25-26 默认 pass）
	Interrupt(message map[string]any) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GraphInvoke 拆信封执行：inputs 信封 {inputs:..., config:...} → Invoke_。
// 对齐 Python base.py:13-14：.get 宽容取值——config 缺失传 nil 不报错
// （业务输入缺失=硬错误是消费方 loop_comp.py:171 的语义，留 8.20 复刻，本节不做）。
func GraphInvoke[I, O any](g ExecutableGraph[I, O], envelope map[string]any, sess session.BaseSession) (O, error) {
	// Python: inputs.get(INPUTS_KEY) / inputs.get(CONFIG_KEY)——缺失即 nil，宽容
	var inputs I
	if raw, ok := envelope[InputsKey].(I); ok {
		inputs = raw
	}
	return g.Invoke_(inputs, sess, envelope[ConfigKey])
}

// GraphStream 默认 pass 的流式执行（Python base.py:16-17）：
// 返回已关闭的空 channel + nil error（偏差 D5）。
func GraphStream[I, O any](_ ExecutableGraph[I, O], _ map[string]any, _ session.BaseSession) (<-chan StreamChunk[O], error) {
	ch := make(chan StreamChunk[O])
	close(ch)
	return ch, nil
}

// GraphCollect 默认 pass 的聚合执行（Python base.py:19-20）：返回零值 + nil error（偏差 D5）
func GraphCollect[I, O any](_ ExecutableGraph[I, O], _ <-chan I, _ session.BaseSession) (O, error) {
	var zero O
	return zero, nil
}

// GraphTransform 默认 pass 的流式变换（Python base.py:22-23）：
// 返回已关闭的空 channel + nil error（偏差 D5）
func GraphTransform[I, O any](_ ExecutableGraph[I, O], _ <-chan I, _ session.BaseSession) (<-chan StreamChunk[O], error) {
	ch := make(chan StreamChunk[O])
	close(ch)
	return ch, nil
}

// GraphInterrupt 默认 pass 的中断（Python base.py:25-26）：nil error（偏差 D5）
func GraphInterrupt[I, O any](_ ExecutableGraph[I, O], _ map[string]any) error {
	return nil
}
```

> 设计说明：Python 的 `invoke/stream/collect/transform/interrupt` 是子类可覆写的实例方法，Go 接口无法带默认实现且方法名与 Executable 的 On* 无冲突，但 `Invoke/Stream/...` 若直接进接口会强迫所有实现者写一遍 pass 体。故采用**接口只含抽象成员（Invoke_/Interrupt）+ 包级泛型函数承载默认 pass 行为**的形状：消费方调用 `GraphInvoke(g, ...)` 即获得"默认 pass + 可被覆写"两态（覆写方式：实现者自行包装）。此形状对应 Python 继承默认方法，登记为本计划的实施细则（与 spec §3.4"包级函数或基座方法皆可，实施时按测试友好性定"一致，选择包级函数）。fake 里 `ctx` 未消费时删除 `_ = ctx` 与 import。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/graph/ -v`
Expected: PASS（Task 1 + Task 2 全部用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/executable_graph.go internal/agentcore/graph/executable_graph_test.go
git commit -m "feat(graph): 8.2 ExecutableGraph 契约 + InputsKey/ConfigKey 信封拆分"
```

---

### Task 3: doc.go 更新 + 全量回归 + 计划表同步

**Files:**
- Modify: `internal/agentcore/graph/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件树与包注释**

包注释追加一段：

```go
// Executable（executable.go）为可执行节点契约（8.2）：泛型接口承载四执行入口
// （OnInvoke/OnStream/OnCollect/OnTransform，"单/流 × 单/流"矩阵）与四策略钩子
// （SkipTrace/GraphInvoker/PostCommit/ComponentType，由 8.11 Vertex 消费）；
// 流式语义经 StreamChunk（Err 非 nil 为末元素后关闭）承载。ExecutableGraph
//（executable_graph.go）为可执行图契约，Invoke 信封拆分（InputsKey/ConfigKey）
// 对齐 base.py:13-14，默认 pass 流式方法经包级泛型函数承载（spec 偏差 D5）。
```

文件树插入两行（按 doc.go 树形顺序，核心类型文件在前）：

```go
//	graph/
//	├── doc.go                 # 包文档
//	├── executable.go          # Executable 泛型接口 + StreamChunk + BaseExecutable 基座（8.2）
//	├── executable_graph.go    # ExecutableGraph 契约 + InputsKey/ConfigKey 信封常量（8.2）
//	├── atomic_node.go         # AtomicNode 原子提交包装（8.10）
//	└── graph_state.go        # GraphState 节点输出聚合 schema
```

对应 Python 行追加 `· executable.py · base.py`。

- [ ] **Step 2: 跑包覆盖率**

Run: `go test -cover ./internal/agentcore/graph/`
Expected: PASS，executable.go / executable_graph.go 覆盖率 ≥85%（目标 100%）

- [ ] **Step 3: 全量回归（CI 同款构建标签）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 全量零 FAIL

- [ ] **Step 4: go vet + 编译**

Run: `go build ./... && go vet ./...`
Expected: 无错误

- [ ] **Step 5: 更新 IMPLEMENTATION_PLAN.md**

1. 8.2 行：`☐` → `✅`，追加 Go 事实（两个文件、八方法接口、StreamChunk A' 签名、偏差 D1-D6、覆盖率、spec/plan 路径），补 `⤴️ ExecutableGraph/Router 自 8.1 前移（方案 B）、Invoke_ 消费留 8.1`；
2. 追加第十一次顺序调整记录（`> **8.x 实施顺序（2026-11-16 十一次调整）**：方案 B 拍板 8.2 → 8.11 → 8.1 → 8.12，8.2 前移理由=零依赖契约层先定型、8.11 六处消费四钩子、8.1 CompiledGraph 编译期依赖 8.2 类型；依据见 8.2 spec docs/superpowers/specs/2026-11-16-executable-8.2-design.md §2。原注记保留。`）；
3. 8.1 行核对：ExecutableGraph/Router 部分移至 8.2 的注记；
4. 8.11/8.12 行核对回填注记不变。

- [ ] **Step 6: 提交并推送**

```bash
git add internal/agentcore/graph/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(graph): 8.2 收尾——doc.go 同步 + 计划表打勾与第十一次顺序记录"
git push
```

> 规则 11：推送即交付，不追踪远端 CI。

---

## 自审记录

- **Spec 覆盖**：spec §3.1 文件表 → Task 1/2/3；§3.2 接口形状 → Task 1；§3.3 基座+默认值 → Task 1；§3.4 信封+pass 语义 → Task 2；§3.5 Router 不另造 → 无代码任务（spec 层注明，无 Go 产物）；§5 测试六类 → Task 1 用例 1/2/4/5 + Task 2 用例（信封拆分/默认 pass）；§6 交付物 → Task 3。无缺口。
- **占位符扫描**：无 TBD/TODO；两处"按需删除 import"是 Go import 纪律说明而非内容缺失。
- **类型一致性**：`StreamChunk[O]{Val, Err}`、`Executable[I,O]` 八方法签名、`GraphInvoke/GraphStream/GraphCollect/GraphTransform/GraphInterrupt` 包级函数、`Invoke_` 命名在 Task 1/2 与测试间一致。
