# 8.14b SubWorkflowComponent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地子工作流嵌套组件（Python `workflow_comp.py` 一比一），打通父工作流嵌套子 Workflow 的批式/流式/缓存/中断恢复全链路。

**Architecture:** `SubWorkflowComponent` 独立结构体嵌入既有 `WorkflowComponent`（方法提升遮蔽三钩子 + 构造期 InvokeFn/StreamFn 槽赋值）；`SubWorkflowStreamState` 组件实例字段承载 cache_stream 累积。消费端三处地基（isGraphInvoker 探测/GetStreamOutput 断言/is_sub 分派）8.14 已就绪，零改动。

**Tech Stack:** Go 1.24、现有 `internal/agentcore/workflow`/`graph`/`session` 体系、exception 错误码、testify + 集成 suite。

**Spec:** `docs/superpowers/specs/2026-11-21-subworkflow-component-8.14b-design.md`（拍板 §2、偏差 §7）

**工作方式（8.14 记忆）**：主仓直接改并推送 main，不用 worktree（bash cwd 会静默重置回主仓）。

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/agentcore/workflow/components/flow/sub_workflow_comp.go` | 新增：SubWorkflowStreamState + SubWorkflowComponent（一比一映射 workflow_comp.py） |
| `internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go` | 新增：单测（合并矩阵/构造守卫/override/短路两形态/累积/四事件对） |
| `internal/agentcore/workflow/components/doc.go` | 修改：flow/ 目录职责行补 SubWorkflow |
| `docs/superpowers/specs/2026-11-21-subworkflow-component-8.14b-design.md` | 修改：追加 §12 方法对照表（回填③） |
| `tests/integration/agentcore/workflow_sub_workflow/suite_test.go` | 新增：集成套件（两层嵌套/cache 短路/callback 序列/中断恢复） |
| `IMPLEMENTATION_PLAN.md` | 修改：8.14b 打 ✅、8.14 行三处 ⤵️8.14b 回填标记清零 |

已知约束（实施时勿踩）：
- 错误码复用既有 `exception.StatusComponentSubWorkflowParamInvalid`（codes_workflow.go:135，文案 `component sub_workflow param is invalid, error='{reason}'`）
- 信封常量 `graph.InputsKey = "inputs"` / `graph.ConfigKey = "config"`（executable_graph.go）
- workflow（根包）→ components 单向 import；flow → workflow 合法不成环（components 根不 import flow）
- StreamFn 槽出参固定 `map[string]any`，流帧承载用 `map[string]any{"frame": <stream.Schema>}`（End 同款 Go 惯例，偏差随 D-17-1 延伸登记）
- InvokeFn 出参 `map[string]any`：`*WorkflowOutput` 经 JSON 往返转 map（对齐 pydantic 序列化面 {"result":…,"state":…}，D-17-1 延伸）
- 注释一律中文；声明顺序按 Go 规范（结构体→常量→导出函数→非导出函数）

---

### Task 1: SubWorkflowStreamState（状态机 + 合并语义）

**Files:**
- Create: `internal/agentcore/workflow/components/flow/sub_workflow_comp.go`（本任务只写状态机部分）
- Test: `internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go`

- [ ] **Step 1: 写失败测试（合并矩阵）**

```go
package flow

import (
	"testing"
)

// TestBuildFinalResult_空累积 返回空 map（Python workflow_comp.py:47-48）
func TestBuildFinalResult_空累积(t *testing.T) {
	s := &SubWorkflowStreamState{}
	got, err := s.BuildFinalResult()
	if err != nil {
		t.Fatalf("期望无错误, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("期望空 map, got %v", got)
	}
}

// TestBuildFinalResult_嵌套output展开 复刻 Python :50-55
func TestBuildFinalResult_嵌套output展开(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"output": map[string]any{"out": "x"}})
	got, err := s.BuildFinalResult()
	if err != nil {
		t.Fatalf("期望无错误, got %v", err)
	}
	if v, ok := got["out"]; !ok || v != "x" {
		t.Fatalf("期望 out=x, got %v", got)
	}
}

// TestBuildFinalResult_同key字符串拼接 复刻 Python merged[key]+value
func TestBuildFinalResult_同key字符串拼接(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"k": "a"})
	s.Accumulate(map[string]any{"k": "b"})
	got, _ := s.BuildFinalResult()
	if got["k"] != "ab" {
		t.Fatalf("期望 k=ab, got %v", got)
	}
}

// TestBuildFinalResult_同key数字求和 int/int、float/float、int/float
func TestBuildFinalResult_同key数字求和(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"a": 1, "b": 1.5, "c": 2})
	s.Accumulate(map[string]any{"a": 2, "b": 2.5, "c": 0.5})
	got, _ := s.BuildFinalResult()
	if got["a"] != 3 || got["b"] != 4.0 || got["c"] != 2.5 {
		t.Fatalf("期望 a=3 b=4.0 c=2.5, got %v", got)
	}
}

// TestBuildFinalResult_异类型报错 对齐 Python TypeError（dict+dict / str+int）
func TestBuildFinalResult_异类型报错(t *testing.T) {
	cases := []struct {
		name  string
		frames []map[string]any
	}{
		{"dict加dict", []map[string]any{{"k": map[string]any{"x": 1}}, {"k": map[string]any{"y": 2}}}},
		{"str加int", []map[string]any{{"k": "a"}, {"k": 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &SubWorkflowStreamState{}
			for _, f := range c.frames {
				s.Accumulate(f)
			}
			if _, err := s.BuildFinalResult(); err == nil {
				t.Fatalf("期望报错（对齐 Python TypeError）, got nil")
			}
		})
	}
}

// TestBuildFinalResult_非dict帧跳过 复刻 Python isinstance(output, dict) 过滤
func TestBuildFinalResult_非dict帧跳过(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate("not a dict")
	s.Accumulate(42)
	s.Accumulate(map[string]any{"k": "v"})
	got, _ := s.BuildFinalResult()
	if got["k"] != "v" || len(got) != 1 {
		t.Fatalf("期望仅 k=v, got %v", got)
	}
}

// TestClear 清空后重新累积
func TestClear(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"k": "v"})
	s.Clear()
	got, _ := s.BuildFinalResult()
	if len(got) != 0 {
		t.Fatalf("Clear 后期望空, got %v", got)
	}
	s.Accumulate(map[string]any{"k2": "v2"})
	got, _ = s.BuildFinalResult()
	if got["k2"] != "v2" {
		t.Fatalf("Clear 后应可重新累积, got %v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/workflow/components/flow/ -run 'TestBuildFinalResult|TestClear' -v`
Expected: FAIL（`SubWorkflowStreamState` 未定义，编译错误）

- [ ] **Step 3: 最小实现（状态机部分）**

```go
package flow

import (
	"fmt"
)

// SubWorkflowStreamState 子工作流组件的流式结果缓存状态
// （Python workflow_comp.py:22-63 SubWorkflowStreamState 一比一）。
// 并发约定：Accumulate 仅由 stream 生产方调用、BuildFinalResult 由通道耗尽后的
// Vertex postStream / invoke 短路检查调用，通道序自带 happens-before，不加锁
// （spec 决策 #3；定义复用+并发 Invoke 的缓存混淆为已知限制 D-14b-3）。
type SubWorkflowStreamState struct {
	// accumulatedOutputs 累积的流帧（Python _accumulated_outputs）
	accumulatedOutputs []any
}

// Accumulate 累积流帧（Python accumulate）
func (s *SubWorkflowStreamState) Accumulate(output any) {
	s.accumulatedOutputs = append(s.accumulatedOutputs, output)
}

// BuildFinalResult 合并累积帧为最终结果（Python build_final_result :41-63）：
// 只认 map 帧；嵌套取 output 键回退自身；同 key 相加——string 拼接、数字求和、
// 其余类型（含 map+map）对齐 Python TypeError 返回 error（spec 决策 #2）。
func (s *SubWorkflowStreamState) BuildFinalResult() (map[string]any, error) {
	if len(s.accumulatedOutputs) == 0 {
		return map[string]any{}, nil
	}
	merged := map[string]any{}
	for _, output := range s.accumulatedOutputs {
		m, ok := output.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := m["output"]
		if !ok {
			inner = m
		}
		innerMap, ok := inner.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range innerMap {
			if old, exists := merged[key]; exists {
				sum, err := addValues(old, value)
				if err != nil {
					return nil, err
				}
				merged[key] = sum
			} else {
				merged[key] = value
			}
		}
	}
	return merged, nil
}

// Clear 清空状态（Python clear）
func (s *SubWorkflowStreamState) Clear() {
	s.accumulatedOutputs = nil
}

// addValues 复刻 Python merged[key] + value 的类型分派：
// string+string 拼接、数值求和（int/float 提升）、其余报错对齐 TypeError。
func addValues(a, b any) (any, error) {
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as + bs, nil
	}
	af, aIsNum := toFloat64(a)
	bf, bIsNum := toFloat64(b)
	if aIsNum && bIsNum {
		return af + bf, nil
	}
	return nil, fmt.Errorf("unsupported operand type(s) for +: '%T' and '%T'", a, b)
}

// toFloat64 数值类型统一提升（int/int64/float64/float32；bool 不算数——对齐 Python bool 参与 + 但此处实现按需收窄，Python bool+bool=1+1 语义由 int 提升天然覆盖）
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	default:
		return 0, false
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/workflow/components/flow/ -run 'TestBuildFinalResult|TestClear' -v`
Expected: PASS 全绿

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/workflow/components/flow/sub_workflow_comp.go internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go
git commit -m "feat(8.14b): SubWorkflowStreamState 流式缓存状态机（合并语义忠实复刻+异类型 TypeError 对齐）"
```

---

### Task 2: SubWorkflowComponent 构造 + 三 override + invoke 槽（Quirk A 修复）

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/sub_workflow_comp.go`（追加组件部分）
- Test: `internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
)

// fakeWorkflow 测试替身：记录 Invoke 调用并返回固定输出
type fakeWorkflow struct {
	invokeCalled int
	invokeInputs any
	invokeKwargs map[string]any
	invokeOut    *workflow.WorkflowOutput
	invokeErr    error
}

func (f *fakeWorkflow) Invoke(ctx context.Context, inputs any,
	sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (*workflow.WorkflowOutput, error) {
	f.invokeCalled++
	f.invokeInputs = inputs
	f.invokeKwargs = kwargs
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	if f.invokeOut == nil {
		return &workflow.WorkflowOutput{Result: map[string]any{"done": true}, State: workflow.WorkflowExecutionStateCompleted}, nil
	}
	return f.invokeOut, nil
}

// TestNewSubWorkflowComponent_nil拒绝 构造参数校验（Python __init__ :70-72）
func TestNewSubWorkflowComponent_nil拒绝(t *testing.T) {
	_, err := NewSubWorkflowComponent(nil)
	if err == nil {
		t.Fatal("期望 nil sub_workflow 报错")
	}
	if !strings.Contains(err.Error(), "sub_workflow is None") {
		t.Fatalf("文案复刻 Python, got %v", err)
	}
}

// TestSubWorkflowComponent_三override 方法提升遮蔽
func TestSubWorkflowComponent_三override(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, err := NewSubWorkflowComponent(nil, WithSubWorkflowForTest(fw))
	// 注：测试用私有构造缝，见 Task 2 Step 3 的 newSubWorkflowComponentForTest
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if !comp.GraphInvoker() {
		t.Fatal("GraphInvoker 应恒 true（Python :87-88）")
	}
	if comp.ComponentType() != SubWorkflowComponentType {
		t.Fatalf("期望 %q, got %q", SubWorkflowComponentType, comp.ComponentType())
	}
	if comp.SubWorkflow() == nil {
		t.Fatal("SubWorkflow() 应返回注入实例")
	}
}

// TestSubWorkflowComponent_Invoke_空缓存短路 Quirk A 逐字复刻锁定（D-14b-1 已知行为）
func TestSubWorkflowComponent_Invoke_空缓存不短路(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, _ := newSubWorkflowComponentForTest(fw, true) // cacheStream=true
	sess := newFakeNodeSession(t)
	env := map[string]any{
		graph.InputsKey: map[string]any{"q": "hi"},
		graph.ConfigKey: nil,
	}
	out, err := comp.InvokeFn(context.Background(), env, sess)
	if err != nil {
		t.Fatalf("期望真实转调, got err %v", err)
	}
	if fw.invokeCalled != 1 {
		t.Fatalf("Quirk A 复刻：空缓存即短路, 不应转调子工作流, invokeCalled=%d", fw.invokeCalled)
	}
	if out == nil {
		t.Fatal("期望有输出")
	}
}

// TestSubWorkflowComponent_Invoke_非空短路 缓存命中不重跑
func TestSubWorkflowComponent_Invoke_非空短路(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	comp.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "cached"}})
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{"q": "hi"}, graph.ConfigKey: nil}
	out, err := comp.InvokeFn(context.Background(), env, sess)
	if err != nil {
		t.Fatalf("短路路径不应报错: %v", err)
	}
	if fw.invokeCalled != 0 {
		t.Fatalf("缓存命中不应重跑子工作流, invokeCalled=%d", fw.invokeCalled)
	}
	m, ok := out["output"].(map[string]any)
	if !ok || m["out"] != "cached" {
		t.Fatalf("期望 {'output': cached 形态}, got %v", out)
	}
}

// TestSubWorkflowComponent_Invoke_关缓存恒转调 cache_stream=false
func TestSubWorkflowComponent_Invoke_关缓存恒转调(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, _ := newSubWorkflowComponentForTest(fw, false)
	comp.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "cached"}})
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{"q": "hi"}, graph.ConfigKey: nil}
	if _, err := comp.InvokeFn(context.Background(), env, sess); err != nil {
		t.Fatalf("转调不应报错: %v", err)
	}
	if fw.invokeCalled != 1 {
		t.Fatalf("cache_stream=false 恒转调, invokeCalled=%d", fw.invokeCalled)
	}
}
```

辅助桩 `newSubWorkflowComponentForTest` / `newFakeNodeSession` 放本测试文件（fake NodeSessionFacade 零值结构体即可满足 InvokeFn 转调链；若 `Workflow.Invoke` 内部消费 sess 更多方法，则按最小实现补桩）：

```go
// newSubWorkflowComponentForTest 测试构造缝：绕过 *workflow.Workflow 具体类型，
// 注入 fake。生产构造函数只收 *workflow.Workflow（Python 形参类型 Workflow）。
func newSubWorkflowComponentForTest(fw *fakeWorkflow, cacheStream bool) (*SubWorkflowComponent, error) {
	comp, err := newSubWorkflowComponent((*workflow.Workflow)(nil), WithCacheStream(cacheStream))
	if err != nil {
		return nil, err
	}
	comp.subWorkflowInvokeFn = func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (*workflow.WorkflowOutput, error) {
		return fw.Invoke(ctx, inputs, sess, modelContext, kwargs)
	}
	return comp, nil
}
```

（实现注记：为让 fake 可注入且生产路径不泄漏测试缝，组件内对子工作流的调用收敛到一个非导出函数字段 `subWorkflowInvokeFn`/`subWorkflowStreamFn`，生产构造函数里赋值为真实 `w.Invoke`/`w.Stream` 闭包——Python 直接调方法的等价承载，不改变归属。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/workflow/components/flow/ -run 'TestNewSubWorkflowComponent|TestSubWorkflowComponent' -v`
Expected: FAIL（组件未定义，编译错误）

- [ ] **Step 3: 实现组件（构造 + override + invoke）**

```go
// ──────────────────────────── 常量 ────────────────────────────

// SubWorkflowComponentType 子工作流组件类型标签（Python SUB_WORKFLOW_COMPONENT）
const SubWorkflowComponentType = "sub_workflow"

// ──────────────────────────── 结构体 ────────────────────────────

// SubWorkflowOption 子工作流组件构造选项（Python keyword-only 参数的 Go 惯用法承载，D-14b-4）
type SubWorkflowOption func(*SubWorkflowComponent)

// WithCacheStream 是否缓存流式结果供批式取用（Python cache_stream=False）
func WithCacheStream(enabled bool) SubWorkflowOption {
	return func(c *SubWorkflowComponent) { c.cacheStream = enabled }
}

// SubWorkflowComponent 子工作流嵌套组件（Python workflow_comp.py:66-96 一比一）。
// 嵌入 WorkflowComponent：执行面经 InvokeFn/StreamFn 槽、包装序由基类 OnInvoke/OnStream
// 承担；GraphInvoker/ComponentType 经方法提升遮蔽 override（深度 1 > 深度 2）。
type SubWorkflowComponent struct {
	components.WorkflowComponent
	// subWorkflow 内嵌子工作流（Python _sub_workflow）
	subWorkflow *workflow.Workflow
	// cacheStream 是否缓存流式结果（Python _cache_stream）
	cacheStream bool
	// streamState 流式累积缓存（Python _stream_state；组件实例字段，
	// 生存期跨单次调用——中断恢复读到部分缓存即 Quirk B 的机制，D-14b-2）
	streamState SubWorkflowStreamState

	// subWorkflowInvokeFn 子工作流批式调用缝（生产=闭包转 w.Invoke；测试可注入 fake）
	subWorkflowInvokeFn func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (*workflow.WorkflowOutput, error)
	// subWorkflowStreamFn 子工作流流式调用缝（生产=闭包转 w.Stream）
	subWorkflowStreamFn func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, streamModes []stream.StreamMode,
		kwargs map[string]any) (<-chan workflow.WorkflowChunk, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSubWorkflowComponent 构造子工作流组件（Python __init__ :66-75）。
// subWorkflow 为 nil → StatusComponentSubWorkflowParamInvalid 复刻 build_error。
func NewSubWorkflowComponent(subWorkflow *workflow.Workflow, opts ...SubWorkflowOption) (*SubWorkflowComponent, error) {
	return newSubWorkflowComponent(subWorkflow, opts...)
}

// GraphInvoker 子图递归入口标识（Python :87-88，恒 true；
// _workflow.py:109 据此给 Vertex 递 InnerPregelConfig 形成 ns 嵌套）
func (c *SubWorkflowComponent) GraphInvoker() bool { return true }

// ComponentType 组件类型标签（Python :90-91）
func (c *SubWorkflowComponent) ComponentType() string { return SubWorkflowComponentType }

// SubWorkflow 暴露内嵌子工作流（Python sub_workflow property）
func (c *SubWorkflowComponent) SubWorkflow() *workflow.Workflow { return c.subWorkflow }

// GetStreamOutput 批式侧取缓存结果（Python get_stream_output :81-85；
// Vertex postStream 接口断言 vertex.go:1487 自动接通）
func (c *SubWorkflowComponent) GetStreamOutput() map[string]any {
	if !c.cacheStream {
		return nil
	}
	merged, err := c.streamState.BuildFinalResult()
	if err != nil {
		// Python build_final_result 对异类型抛 TypeError 会炸掉 postStream；
		// Go 侧按防御收敛为 nil（异常路径不顶替正常执行链），
		// 合并错误的真实暴露点在 invoke 短路检查（可传播给调用方）。
		return nil
	}
	return merged
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSubWorkflowComponent 内部构造（测试缝经 subWorkflowInvokeFn/subWorkflowStreamFn 注入）
func newSubWorkflowComponent(subWorkflow *workflow.Workflow, opts ...SubWorkflowOption) (*SubWorkflowComponent, error) {
	if subWorkflow == nil {
		return nil, exception.BuildError(exception.StatusComponentSubWorkflowParamInvalid,
			exception.WithParam("reason", "sub_workflow is None"))
	}
	c := &SubWorkflowComponent{subWorkflow: subWorkflow}
	for _, opt := range opts {
		opt(c)
	}
	// 生产调用缝（Python invoke :78-84 直接调 self._sub_workflow.invoke 的等价承载）
	c.subWorkflowInvokeFn = func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (*workflow.WorkflowOutput, error) {
		return subWorkflow.Invoke(ctx, inputs, sess, modelContext, kwargs)
	}
	c.subWorkflowStreamFn = func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, streamModes []stream.StreamMode,
		kwargs map[string]any) (<-chan workflow.WorkflowChunk, error) {
		return subWorkflow.Stream(ctx, inputs, sess, modelContext, streamModes, kwargs)
	}
	// InvokeFn 槽绑定（Python invoke :78-84，Quirk A 逐字复刻 D-14b-1 已知行为）
	c.InvokeFn = func(ctx context.Context, inputs map[string]any,
		sess *session.NodeSessionFacade) (map[string]any, error) {
		// Quirk A 修复：Python workflow_comp.py:81-84 空缓存 build_final_result 返回 {}
		// （非 None）触发 cached is not None 短路 → 批入腿永不执行子工作流，属上游缺陷；
		// Go 修复为「缓存非空才短路」，空缓存真实转调。
		if c.cacheStream {
			merged, err := c.streamState.BuildFinalResult()
			if err == nil && len(merged) > 0 {
				return map[string]any{"output": merged}, nil
			}
		}
		cfg := inputs[graph.ConfigKey]
		out, err := c.subWorkflowInvokeFn(ctx, inputs[graph.InputsKey], sess, nil,
			map[string]any{graph.ConfigKey: cfg, "is_sub": true})
		if err != nil {
			return nil, err
		}
		// *WorkflowOutput → map（D-17-1 延伸：Output TypeVar 具体化承载，
		// JSON 往返对齐 pydantic 序列化面 {"result":…,"state":…}）
		raw, _ := json.Marshal(out)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return m, nil
	}
	return c, nil
}
```

imports 补充：`encoding/json`、`strings`（测试）、`github.com/uapclaw/uapclaw-go/internal/common/exception`、`sessioninterfaces`、`stream`、`graph`。

注意：测试里的 `newSubWorkflowComponentForTest` 传 `(nil, ...)` 后立即注入 fake 缝——构造校验只拒绝 nil 且 **fake 注入发生在校验之后**，因此测试缝需绕过 nil 校验：改用 `&workflow.Workflow{}` 空实例占位再注入 fake 缝（`newSubWorkflowComponent(&workflow.Workflow{}, ...)` + 覆写 `subWorkflowInvokeFn`），避免 nil 短路。同步修正 Step 1 的桩：

```go
func newSubWorkflowComponentForTest(fw *fakeWorkflow, cacheStream bool) (*SubWorkflowComponent, error) {
	comp, err := newSubWorkflowComponent(&workflow.Workflow{}, WithCacheStream(cacheStream))
	if err != nil {
		return nil, err
	}
	comp.subWorkflowInvokeFn = fw.Invoke
	return comp, nil
}
```

- [ ] **Step 4: 编译 + 跑测试确认通过**

Run: `go build ./internal/agentcore/workflow/... && go test ./internal/agentcore/workflow/components/flow/ -run 'TestNewSubWorkflowComponent|TestSubWorkflowComponent' -v`
Expected: PASS。若 `fw.Invoke` 签名与 `subWorkflowInvokeFn` 不匹配（SessionFacade 接口差异），以编译错误为准修正 fake 签名，不改生产代码形状。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/workflow/components/flow/sub_workflow_comp.go internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go
git commit -m "feat(8.14b): SubWorkflowComponent 构造+三override+invoke 槽（Quirk A 逐字复刻 D-14b-1）"
```

---

### Task 3: stream 槽（逐帧累积 + 信封转发）+ get_stream_output 用例

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/sub_workflow_comp.go`
- Test: `internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestSubWorkflowComponent_Stream_逐帧累积并转发 cache_stream=true
func TestSubWorkflowComponent_Stream_逐帧累积并转发(t *testing.T) {
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		&stream.OutputSchema{Type: "message", Index: 0, Payload: map[string]any{"out": "a"}},
		&stream.OutputSchema{Type: "message", Index: 1, Payload: map[string]any{"out": "b"}},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{"q": "hi"}, graph.ConfigKey: nil}
	ch, err := comp.StreamFn(context.Background(), env, sess)
	if err != nil {
		t.Fatalf("流启动失败: %v", err)
	}
	var got []executable.StreamChunk[map[string]any]
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 2 || got[0].Err != nil {
		t.Fatalf("期望两帧无错误, got %v", got)
	}
	// 转发形态：{"frame": <stream.Schema>} 信封（End Go 同款）
	f0, ok := got[0].Val["frame"].(*stream.OutputSchema)
	if !ok || f0.Payload.(map[string]any)["out"] != "a" {
		t.Fatalf("期望帧信封承载原 Schema, got %v", got[0].Val)
	}
	// 累积的是原始帧值（转发不改变缓存内容形态：Schema 帧 BuildFinalResult 跳过）
	merged, _ := comp.streamState.BuildFinalResult()
	if len(merged) != 0 {
		t.Fatalf("Schema 帧应被合并过滤（对齐 Python isinstance dict）, got %v", merged)
	}
}

// TestSubWorkflowComponent_Stream_关缓存不累积
func TestSubWorkflowComponent_Stream_关缓存不累积(t *testing.T) {
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		map[string]any{"output": map[string]any{"out": "x"}},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, false)
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{}, graph.ConfigKey: nil}
	ch, _ := comp.StreamFn(context.Background(), env, sess)
	for range ch {
	}
	merged, _ := comp.streamState.BuildFinalResult()
	if len(merged) != 0 {
		t.Fatalf("cache_stream=false 不应累积, got %v", merged)
	}
}

// TestSubWorkflowComponent_Stream_dict帧累积命中短路 dict 帧累积后 invoke 短路
func TestSubWorkflowComponent_Stream_dict帧累积命中短路(t *testing.T) {
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		map[string]any{"output": map[string]any{"out": "x"}},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{}, graph.ConfigKey: nil}
	ch, _ := comp.StreamFn(context.Background(), env, sess)
	for range ch {
	}
	// 流后批式 invoke：缓存命中短路
	out, err := comp.InvokeFn(context.Background(), env, sess)
	if err != nil {
		t.Fatalf("短路路径不应报错: %v", err)
	}
	if fw.invokeCalled != 0 {
		t.Fatalf("缓存命中不应重跑, invokeCalled=%d", fw.invokeCalled)
	}
	if out["output"].(map[string]any)["out"] != "x" {
		t.Fatalf("期望缓存合并结果, got %v", out)
	}
}

// TestGetStreamOutput_两态 开=合并结果 关=nil
func TestGetStreamOutput_两态(t *testing.T) {
	fw := &fakeWorkflow{}
	compOn, _ := newSubWorkflowComponentForTest(fw, true)
	compOn.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "v"}})
	if m := compOn.GetStreamOutput(); m == nil || m["out"] != "v" {
		t.Fatalf("开启时期望合并结果, got %v", m)
	}
	compOff, _ := newSubWorkflowComponentForTest(fw, false)
	compOff.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "v"}})
	if m := compOff.GetStreamOutput(); m != nil {
		t.Fatalf("关闭时期望 nil, got %v", m)
	}
}
```

fakeWorkflow 补充流式部分：

```go
type fakeWorkflow struct {
	invokeCalled int
	invokeInputs any
	invokeKwargs map[string]any
	invokeOut    *workflow.WorkflowOutput
	invokeErr    error
	streamFrames []workflow.WorkflowChunk
}

func (f *fakeWorkflow) Stream(ctx context.Context, inputs any,
	sess sessioninterfaces.SessionFacade, modelContext any, streamModes []stream.StreamMode,
	kwargs map[string]any) (<-chan workflow.WorkflowChunk, error) {
	out := make(chan workflow.WorkflowChunk)
	go func() {
		defer close(out)
		for _, fr := range f.streamFrames {
			out <- fr
		}
	}()
	return out, nil
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/workflow/components/flow/ -run 'Stream|GetStreamOutput' -v`
Expected: FAIL（StreamFn 未绑定 / fakeWorkflow 无 Stream 方法）

- [ ] **Step 3: 实现（newSubWorkflowComponent 内补 StreamFn 绑定）**

```go
	// StreamFn 槽绑定（Python stream :76-81：转调子工作流流式 + cache_stream 逐帧累积；
	// 帧信封 {"frame": <stream.Schema>} 为 Go map 槽承载惯例，End 同款，D-17-1 延伸）
	c.StreamFn = func(ctx context.Context, inputs map[string]any,
		sess *session.NodeSessionFacade) (<-chan executable.StreamChunk[map[string]any], error) {
		cfg := inputs[graph.ConfigKey]
		subCh, err := c.subWorkflowStreamFn(ctx, inputs[graph.InputsKey], sess, nil,
			nil, map[string]any{graph.ConfigKey: cfg, "is_sub": true})
		if err != nil {
			return nil, err
		}
		out := make(chan executable.StreamChunk[map[string]any])
		go func() {
			defer close(out)
			for frame := range subCh {
				if c.cacheStream {
					c.streamState.Accumulate(frame)
				}
				out <- executable.StreamChunk[map[string]any]{Val: map[string]any{"frame": frame}}
			}
		}()
		return out, nil
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/workflow/components/flow/ -v`
Expected: 全 PASS（含 Task 1/2 用例）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/workflow/components/flow/sub_workflow_comp.go internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go
git commit -m "feat(8.14b): stream 槽逐帧累积+信封转发、get_stream_output 两态（Quirk B 机制锚点）"
```

---

### Task 4: 回填①——元类装饰四事件对断言（8.14 遗留）

**Files:**
- Test: `internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go`（追加）

- [ ] **Step 1: 写失败测试（四事件成对触发，经本组件真实 OnInvoke/OnStream 链）**

用既有 `callback` 测试工具（8.17 组件事件常量 ComponentBatchInput/ComponentBatchOutput/ComponentStreamOutput）记录 Trigger 序列：

```go
// TestSubWorkflowComponent_元类装饰四事件对断言 8.14 回填①：
// __init_subclass__ 装饰序（D-17-5 等价承载）在本组件经 OnInvoke/OnStream 时
// ComponentBatchInput/ComponentBatchOutput 成对、ComponentStreamOutput 逐帧触发。
func TestSubWorkflowComponent_元类装饰四事件对断言(t *testing.T) {
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		map[string]any{"output": map[string]any{"out": "1"}},
		map[string]any{"output": map[string]any{"out": "2"}},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{}, graph.ConfigKey: nil}

	// 注册事件收集器（8.17 既有回调注册面，参照 end_comp_test.go 同款工具）
	events := collectWorkflowEvents(t)
	defer events.close()

	_, _ = comp.OnInvoke(context.Background(), env, sess.Facade)
	ch, _ := comp.OnStream(context.Background(), env, sess.Facade)
	for range ch {
	}

	seq := events.sequence()
	// 批入轨：Input → Output 成对
	if !events.assertPair(seq, callback.ComponentBatchInput, callback.ComponentBatchOutput, "invoke") {
		t.Fatalf("invoke 轨四事件未成对: %v", seq)
	}
	// 流轨：Input → 逐帧 StreamOutput(×2) → Output
	if !events.assertStreamSequence(seq, callback.ComponentBatchInput, 2, callback.ComponentBatchOutput) {
		t.Fatalf("stream 轨序列不符: %v", seq)
	}
}
```

（`collectWorkflowEvents` 若 8.17 测试工具已有同款则直接复用；没有则按 `callback.GetCallbackFramework().RegisterWorkflow(...)` 写一个记录 Event 序列的收集器，用完注销。）

- [ ] **Step 2: 跑测试确认通过（此断言验证既有基类行为，预期直接 PASS；若 FAIL 说明包装序有洞，修基类调用序而非本组件）**

Run: `go test ./internal/agentcore/workflow/components/flow/ -run 'TestSubWorkflowComponent_元类装饰四事件对断言' -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/workflow/components/flow/sub_workflow_comp_test.go
git commit -m "test(8.14b): 回填① 元类装饰四事件对断言（8.14 遗留 ⤵️8.14b 兑现）"
```

---

### Task 5: doc.go 更新 + IMPLEMENTATION_PLAN 状态

**Files:**
- Modify: `internal/agentcore/workflow/components/doc.go:14`
- Modify: `IMPLEMENTATION_PLAN.md`（8.14b 行 + 8.14 行回填标记）

- [ ] **Step 1: components/doc.go flow 目录行补 SubWorkflow**

```
//	└── flow/           # 流程组件子目录（Start/End/SubWorkflow，对应 Python components/flow/）
```

- [ ] **Step 2: IMPLEMENTATION_PLAN.md 8.14b 行改为 🔄（实施中，完成后 Task 8 打 ✅ 并填产出）**

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/workflow/components/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(8.14b): doc.go flow 目录职责补 SubWorkflow + 计划表状态同步"
```

---

### Task 6: 集成——两层真实嵌套（批式/流式）+ callback 序列（回填②）

**Files:**
- Create: `tests/integration/agentcore/workflow_sub_workflow/suite_test.go`

参照既有 `tests/integration/agentcore/workflow_run/workflow_run_test.go` 的 suite 骨架（`suite.Run(` + `//go:build integration` 标签，审计两层门禁要求真实跨 ≥3 子系统）。

- [ ] **Step 1: 写集成套件（批式+流式）**

```go
//go:build integration

package workflow_sub_workflow

import (
	"context"
	"testing"
	// import 面：workflow + graph + session（真实跨 ≥3 子系统）
)

func TestSubWorkflowSuite(t *testing.T) {
	suite.Run(t, new(SubWorkflowSuite))
}

type SubWorkflowSuite struct {
	suite.Suite
}

// buildChild 组子工作流：Start → End("{{ inputs.answer }}")
func (s *SubWorkflowSuite) buildChild() *workflow.Workflow { /* 复刻 workflow_run_suite buildHelloWorkflow 双层结构 */ }

// buildParent 组父工作流：Start → sub(NewSubWorkflowComponent(child)) → End
func (s *SubWorkflowSuite) buildParent(child *workflow.Workflow) *workflow.Workflow { /* AddConnection 普通边拓扑 */ }

func (s *SubWorkflowSuite) TestInvoke_两层真实嵌套输入输出贯通() {
	child := s.buildChild()
	parent := s.buildParent(child)
	out, err := parent.Invoke(ctx, map[string]any{"answer": "42"}, sess, nil, nil)
	s.Require().NoError(err)
	// 真实输入输出断言：子工作流收到 {"answer": "42"} → End 模板渲染 → 父 End 输出含 "42"
	s.Require().NotNil(out)
	s.Contains(fmt.Sprint(out.Result), "42")
}

func (s *SubWorkflowSuite) TestStream_两层帧序列贯通() {
	// 父 stream；断言子工作流帧经 sub_workflow_stream / 转发层逐帧到达父侧，
	// 逐帧核对 Index 序与 Payload 内容（真实输出）
}

func (s *SubWorkflowSuite) TestCallback_触发序列断言() {
	// 回填②：真实链路断言 ComponentBatchInput →（流轨逐帧 ComponentStreamOutput）→
	// ComponentBatchOutput 顺序（用 Task 4 收集器同款，注册到真实 CallbackFramework）
}
```

（骨架中 `/* ... */` 处按 workflow_run 既有 suite 的真实装配代码逐行复刻——装配面与 workflow_run 同源：NewWorkflowCard/NewWorkflowConfig/Start/NewEnd/NewSubWorkflowComponent/AddConnection/Compile/RegisterWorkflowConfig 走 Init。实施时先读 workflow_run_test.go 全文再填。）

- [ ] **Step 2: 本地跑集成 + 审计自检**

Run: `go test -tags=integration ./tests/integration/agentcore/workflow_sub_workflow/ -v && go run ./cmd/integration-audit -verify`
Expected: PASS + 审计两层门禁通过

- [ ] **Step 3: Commit**

```bash
git add tests/integration/agentcore/workflow_sub_workflow/suite_test.go
git commit -m "test(8.14b): 集成两层真实嵌套批式/流式 + callback 触发序列（回填②）"
```

---

### Task 7: 集成——cache_stream 短路 + 中断恢复（真实输入输出，用户明确要求）

**Files:**
- Modify: `tests/integration/agentcore/workflow_sub_workflow/suite_test.go`（追加）

- [ ] **Step 1: 写 cache 短路集成用例**

```go
func (s *SubWorkflowSuite) TestCacheStream_流后批式短路不重跑() {
	// cache_stream=true 的组件：先 stream 跑完（缓存积累）
	// → 再 invoke 同父工作流（批入腿）→ 断言子工作流未重跑
	//（子侧用可计数的真实标记：子工作流 End 输出值带运行序号/子 Invoke 侧
	//  checkpointer 快照计数，或父侧 span/日志计数——以 workflow_run 既有观测面为准）
}
```

- [ ] **Step 2: 写中断恢复用例（真实输入输出）**

```go
func (s *SubWorkflowSuite) TestInterruptResume_真实输入输出() {
	// ① 组父 Workflow：Start → sub（cache_stream=true，子工作流内含中断点：
	//    复用 workflow_run/HITL 既有中断构造形态——子工作流 End 前置交互组件
	//    或 InteractiveInput 恢复基座，参照 tests/integration 已有中断用例装配）
	// ② 第一次运行（真实输入 map[string]any{"q": ...}）：
	//    - 断言返回 TaskStatusInterrupt 形态（{"__interrupt__": value}）
	//    - 断言中断前已下发的流帧内容（真实帧逐帧核对）
	// ③ resume（真实 InteractiveInput 提交用户答案）：
	//    - 断言恢复后 invoke 腿命中部分缓存（Quirk B 复刻锁定 D-14b-2：
	//      子工作流未整体重跑、返回中断前已累积帧的合并结果）
	//    - 断言最终输出完整（用户输入贯通、End 渲染含提交值）
	//    - 断言 checkpointer 快照重建正确（ns 嵌套键存在、pending 现场消费）
}
```

（中断点构造形态实施前先盘点 `tests/integration/agentcore/` 既有中断/恢复用例——workflow_run 或 checkpointer 相关目录——复用其真实装配，不发明新中断机制。）

- [ ] **Step 3: 本地跑集成 + 审计**

Run: `go test -tags=integration ./tests/integration/agentcore/workflow_sub_workflow/ -v -race && go run ./cmd/integration-audit -verify`
Expected: PASS（含 -race）

- [ ] **Step 4: Commit**

```bash
git add tests/integration/agentcore/workflow_sub_workflow/suite_test.go
git commit -m "test(8.14b): 集成 cache 短路 + 中断恢复真实输入输出（Quirk B D-14b-2 锁定）"
```

---

### Task 8: 方法对照表（回填③）+ 全量回归 + 计划表收口 + 推送

**Files:**
- Modify: `docs/superpowers/specs/2026-11-21-subworkflow-component-8.14b-design.md`（追加 §12）
- Modify: `IMPLEMENTATION_PLAN.md`（8.14b ✅ + 产出 + 8.14 行三处 ⤵️8.14b 标记清零）

- [ ] **Step 1: spec 追加 §12 方法对照表（12.4 回填）**

逐符号对照（来源 §3 表，实施后补 Go 行号）：

| Python workflow_comp.py | Go sub_workflow_comp.go | 一致性 |
|---|---|---|
| SUB_WORKFLOW_COMPONENT (:19) | SubWorkflowComponentType (:xx) | ✅ |
| SubWorkflowStreamState.__init__ (:23-25) | 结构体零值 (:xx) | ✅ |
| .accumulate (:27-29) | Accumulate (:xx) | ✅ |
| .build_final_result (:31-60) | BuildFinalResult (:xx) | ✅（Quirk A 修复联动） |
| .clear (:62-65) | Clear (:xx) | ✅ |
| SubWorkflowComponent.__init__ (:66-75) | NewSubWorkflowComponent (:xx) | ✅（D-14b-1 已知行为登记） |
| .invoke (:78-84) | InvokeFn 槽 (:xx) | ✅ |
| .stream (:76-81) | StreamFn 槽 (:xx) | ✅（D-17-1 延伸信封） |
| .get_stream_output (:83-85) | GetStreamOutput (:xx) | ✅ |
| .graph_invoker (:87-88) | GraphInvoker (:xx) | ✅ |
| .component_type (:90-91) | ComponentType (:xx) | ✅ |
| .sub_workflow property (:93-94) | SubWorkflow 访问器 (:xx) | ✅ |

- [ ] **Step 2: 清理 8.14 遗留 ⤵️8.14b 标记**

`IMPLEMENTATION_PLAN.md` 8.14 行三处回填标记（元类四事件断言/callback 序列集成断言/方法对照表）改为「✅ 已回填（8.14b）」；8.14b 行打 ✅ 并填产出摘要（对照 8.14/8.17 行格式：Go 事实 + 偏差 + 覆盖率 + 设计/计划链接）。

- [ ] **Step 3: 全量回归（CI 同款标签）**

Run: `go build ./... && go vet ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 零 FAIL（环境性 flaky 包单跑复核，参照 8.14 先例）

- [ ] **Step 4: 覆盖率核对**

Run: `go test -cover ./internal/agentcore/workflow/components/flow/ ./internal/agentcore/workflow/`
Expected: ≥85%

- [ ] **Step 5: Commit + Push**

```bash
git add docs/superpowers/specs/2026-11-21-subworkflow-component-8.14b-design.md IMPLEMENTATION_PLAN.md
git commit -m "docs(8.14b): 12.4 方法对照表回填（③）+ 计划表 8.14b 收口、8.14 遗留标记清零"
git push origin main
```

推送前 `git status --porcelain` 盘点工作区（防 -am 搭车，feedback 记忆）；推送即交付（规则 11，不追踪 CI）。
