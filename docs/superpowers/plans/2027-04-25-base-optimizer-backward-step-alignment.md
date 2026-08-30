# BaseOptimizer Backward/Step 模板方法对齐 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 ToolOptimizerBase 和 MemoryOptimizerBase 的 Backward()/Step() 方法与 Python BaseOptimizer.backward()/step() 模板方法流程

**Architecture:** 修改 2 个子优化器文件，补充 ValidateParameters + SelectSignals + SetSelectedSignals（Backward）和 ValidateParameters + ClearTrajectories + step() 拆分（Step），保持每个子类各自实现的模式

**Tech Stack:** Go 1.x, testify/assert

---

### Task 1: 修改 ToolOptimizerBase 的 Backward 和 Step

**Files:**
- Modify: `internal/evolving/optimizer/tool_call/base.go:253-265`
- Modify: `internal/evolving/optimizer/tool_call/base_method_test.go`

- [ ] **Step 1: 修改 ToolOptimizerBase.Backward() 补充模板方法流程**

将 `internal/evolving/optimizer/tool_call/base.go` 第 253-258 行从：

```go
// Backward 反向传播：从信号计算梯度。对齐 Python 空实现。
//
// 对齐 Python: async def _backward(self, signals): pass
func (b *ToolOptimizerBase) Backward(_ context.Context, _ []*signal.EvolutionSignal) error {
	return nil
}
```

改为：

```go
// Backward 反向传播：从信号计算梯度。
//
// 对齐 Python: BaseOptimizer.backward() 模板方法流程：
//
//	self._validate_parameters()
//	self._selected_signals = self._select_signals(signals)
//	await self._backward(signals)  # pass
//
// 对应 Python: async def _backward(self, signals): pass
func (b *ToolOptimizerBase) Backward(_ context.Context, signals []*signal.EvolutionSignal) error {
	b.ValidateParameters()
	selected := b.SelectSignals(signals)
	b.SetSelectedSignals(selected)
	// ToolOptimizer 是黑盒优化器，_backward 为 pass
	return nil
}
```

- [ ] **Step 2: 修改 ToolOptimizerBase.Step() 补充模板方法流程并拆出 step()**

将 `internal/evolving/optimizer/tool_call/base.go` 第 260-265 行从：

```go
// Step 生成更新映射。对齐 Python 空实现。
//
// 对齐 Python: def _step(self): updates = {}; for operator in self.operators.items(): return
func (b *ToolOptimizerBase) Step() map[cschema.UpdateKey]any {
	return map[cschema.UpdateKey]any{}
}
```

改为：

```go
// Step 生成更新映射。
//
// 对齐 Python: BaseOptimizer.step() 模板方法流程：
//
//	self._validate_parameters()
//	updates = self._step()
//	self.clear_trajectories()
//	return updates or {}
//
// 对应 Python: BaseOptimizer.step() → _step() → return
func (b *ToolOptimizerBase) Step() map[cschema.UpdateKey]any {
	b.ValidateParameters()
	updates := b.step()
	b.ClearTrajectories()
	return updates
}

// step 子类逻辑，对齐 Python _step()。
// ToolOptimizer 为空实现，返回空映射。
//
// 对应 Python: def _step(self): updates = {}; return
func (b *ToolOptimizerBase) step() map[cschema.UpdateKey]any {
	return map[cschema.UpdateKey]any{}
}
```

- [ ] **Step 3: 更新 ToolOptimizerBase 的 Backward 和 Step 测试**

在 `internal/evolving/optimizer/tool_call/base_method_test.go` 中，找到现有测试：

```go
// TestToolOptimizerBase_Backward 测试 Backward 空实现
func TestToolOptimizerBase_Backward(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	err := base.Backward(context.Background(), nil)
	if err != nil {
		t.Errorf("期望 Backward 返回 nil, 实际=%v", err)
	}
}
```

改为：

```go
// TestToolOptimizerBase_Backward 测试 Backward 模板方法流程
func TestToolOptimizerBase_Backward(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	err := base.Backward(context.Background(), nil)
	if err != nil {
		t.Errorf("期望 Backward 返回 nil, 实际=%v", err)
	}
	// 验证 SelectedSignals 已设置（空信号列表）
	selected := base.SelectedSignals()
	if len(selected) != 0 {
		t.Errorf("期望 SelectedSignals 长度 0, 实际=%d", len(selected))
	}
}

// TestToolOptimizerBase_Backward_未绑定时panic 测试 Backward 未绑定参数时 panic
func TestToolOptimizerBase_Backward_未绑定时panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 Backward 未绑定时 panic, 实际未 panic")
		}
	}()
	base := NewToolOptimizerBase(nil)
	base.Backward(context.Background(), nil)
}

// TestToolOptimizerBase_Backward_信号选择 测试 Backward 正确选择信号
func TestToolOptimizerBase_Backward_信号选择(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	sig1 := signal.MakeEvolutionSignal("execution_failure", "Test", "excerpt1")
	sig2 := signal.MakeEvolutionSignal("low_score", "Test", "excerpt2")
	err := base.Backward(context.Background(), []*signal.EvolutionSignal{sig1, sig2})
	if err != nil {
		t.Errorf("期望 Backward 返回 nil, 实际=%v", err)
	}
	selected := base.SelectedSignals()
	if len(selected) != 2 {
		t.Errorf("期望 SelectedSignals 长度 2, 实际=%d", len(selected))
	}
}
```

找到现有 Step 测试：

```go
// TestToolOptimizerBase_Step 测试 Step 空实现
func TestToolOptimizerBase_Step(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	updates := base.Step()
	if len(updates) != 0 {
		t.Errorf("期望 Step 返回空 map, 实际=%d 项", len(updates))
	}
}
```

改为：

```go
// TestToolOptimizerBase_Step 测试 Step 模板方法流程
func TestToolOptimizerBase_Step(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	updates := base.Step()
	if len(updates) != 0 {
		t.Errorf("期望 Step 返回空 map, 实际=%d 项", len(updates))
	}
}

// TestToolOptimizerBase_Step_未绑定时panic 测试 Step 未绑定参数时 panic
func TestToolOptimizerBase_Step_未绑定时panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 Step 未绑定时 panic, 实际未 panic")
		}
	}()
	base := NewToolOptimizerBase(nil)
	base.Step()
}

// TestToolOptimizerBase_Step_清空轨迹 测试 Step 后轨迹被清空
func TestToolOptimizerBase_Step_清空轨迹(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	base.AddTrajectory(&trajectory.Trajectory{ExecutionID: "test"})
	if len(base.GetTrajectories()) != 1 {
		t.Errorf("添加轨迹后期望 1 条, 实际=%d", len(base.GetTrajectories()))
	}

	base.Step()
	if len(base.GetTrajectories()) != 0 {
		t.Errorf("Step 后期望轨迹清空, 实际=%d 条", len(base.GetTrajectories()))
	}
}
```

注意：需在测试文件 import 中确认已有 `"github.com/uapclaw/uap-claw-go/internal/evolving/signal"` 和 `"github.com/uapclaw/uap-claw-go/internal/evolving/trajectory"` 的导入（检查已有的 import）。

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/evolving/optimizer/tool_call/... -run "TestToolOptimizerBase_Backward|TestToolOptimizerBase_Step" -v`

Expected: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/optimizer/tool_call/base.go internal/evolving/optimizer/tool_call/base_method_test.go
git commit -m "fix(optimizer): ToolOptimizerBase Backward/Step 对齐 Python 模板方法流程"
```

---

### Task 2: 修改 MemoryOptimizerBase 的 Backward 和 Step

**Files:**
- Modify: `internal/evolving/optimizer/memory_call/base.go:98-122`
- Modify: `internal/evolving/optimizer/memory_call/base_test.go:82-95`

- [ ] **Step 1: 修改 MemoryOptimizerBase.Backward() 补充模板方法流程**

将 `internal/evolving/optimizer/memory_call/base.go` 第 98-109 行从：

```go
// Backward 反向传播：从信号计算梯度。
//
// 对齐 Python:
//
//		async def _backward(self, signals: List["EvolutionSignal"]) -> None:
//	   子类实现记忆特定反向逻辑
//		    pass
//
// 对应 Python: MemoryOptimizerBase._backward(signals) → pass
func (b *MemoryOptimizerBase) Backward(_ context.Context, _ []*signal.EvolutionSignal) error {
	return nil
}
```

改为：

```go
// Backward 反向传播：从信号计算梯度。
//
// 对齐 Python: BaseOptimizer.backward() 模板方法流程：
//
//	self._validate_parameters()
//	self._selected_signals = self._select_signals(signals)
//	await self._backward(signals)  # pass
//
// 对应 Python: MemoryOptimizerBase._backward(signals) → pass
func (b *MemoryOptimizerBase) Backward(_ context.Context, signals []*signal.EvolutionSignal) error {
	b.ValidateParameters()
	selected := b.SelectSignals(signals)
	b.SetSelectedSignals(selected)
	// MemoryOptimizer _backward 为 pass
	return nil
}
```

- [ ] **Step 2: 修改 MemoryOptimizerBase.Step() 补充模板方法流程并拆出 step()**

将 `internal/evolving/optimizer/memory_call/base.go` 第 111-122 行从：

```go
// Step 生成更新映射。空梯度自然产生空映射。
//
// 对齐 Python:
//
//	MemoryOptimizerBase._backward() 是 pass，不写梯度；
//	_step() 是 BaseOptimizer 的抽象方法，子类必须实现。
//	此处返回空 map，与 ToolOptimizerBase.Step() 模式一致。
//
// 对应 Python: BaseOptimizer._step() → 抽象（子类实现）
func (b *MemoryOptimizerBase) Step() map[cschema.UpdateKey]any {
	return map[cschema.UpdateKey]any{}
}
```

改为：

```go
// Step 生成更新映射。
//
// 对齐 Python: BaseOptimizer.step() 模板方法流程：
//
//	self._validate_parameters()
//	updates = self._step()
//	self.clear_trajectories()
//	return updates or {}
//
// 对应 Python: BaseOptimizer.step() → _step()
func (b *MemoryOptimizerBase) Step() map[cschema.UpdateKey]any {
	b.ValidateParameters()
	updates := b.step()
	b.ClearTrajectories()
	return updates
}

// step 子类逻辑，对齐 Python _step()。
// MemoryOptimizerBase 为空实现，返回空映射。
//
// 对应 Python: BaseOptimizer._step() → 抽象（子类实现）
func (b *MemoryOptimizerBase) step() map[cschema.UpdateKey]any {
	return map[cschema.UpdateKey]any{}
}
```

- [ ] **Step 3: 更新 MemoryOptimizerBase 的 Backward 和 Step 测试**

在 `internal/evolving/optimizer/memory_call/base_test.go` 中，找到现有测试（第 82-95 行）：

```go
// TestMemoryOptimizerBase_Backward 测试 Backward 返回 nil（空实现）。
func TestMemoryOptimizerBase_Backward(t *testing.T) {
	base := &MemoryOptimizerBase{}
	err := base.Backward(context.Background(), nil)
	assert.NoError(t, err)
}

// TestMemoryOptimizerBase_Step 测试 Step 返回空 map。
func TestMemoryOptimizerBase_Step(t *testing.T) {
	base := &MemoryOptimizerBase{}
	updates := base.Step()
	assert.NotNil(t, updates)
	assert.Equal(t, 0, len(updates))
}
```

改为：

```go
// TestMemoryOptimizerBase_Backward 测试 Backward 模板方法流程。
func TestMemoryOptimizerBase_Backward(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	err := base.Backward(context.Background(), nil)
	assert.NoError(t, err)
	// 验证 SelectedSignals 已设置（空信号列表）
	selected := base.SelectedSignals()
	assert.Equal(t, 0, len(selected))
}

// TestMemoryOptimizerBase_Backward_未绑定参数时panic 测试 Backward 未绑定参数时 panic。
func TestMemoryOptimizerBase_Backward_未绑定参数时panic(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "期望 Backward 未绑定时 panic")
	}()
	base := &MemoryOptimizerBase{}
	base.Backward(context.Background(), nil)
}

// TestMemoryOptimizerBase_Backward_信号选择 测试 Backward 正确选择信号。
func TestMemoryOptimizerBase_Backward_信号选择(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	sig1 := signal.MakeEvolutionSignal("execution_failure", "Test", "excerpt1")
	sig2 := signal.MakeEvolutionSignal("low_score", "Test", "excerpt2")
	err := base.Backward(context.Background(), []*signal.EvolutionSignal{sig1, sig2})
	assert.NoError(t, err)
	selected := base.SelectedSignals()
	assert.Equal(t, 2, len(selected))
}

// TestMemoryOptimizerBase_Step 测试 Step 模板方法流程。
func TestMemoryOptimizerBase_Step(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	updates := base.Step()
	assert.NotNil(t, updates)
	assert.Equal(t, 0, len(updates))
}

// TestMemoryOptimizerBase_Step_未绑定参数时panic 测试 Step 未绑定参数时 panic。
func TestMemoryOptimizerBase_Step_未绑定参数时panic(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "期望 Step 未绑定时 panic")
	}()
	base := &MemoryOptimizerBase{}
	base.Step()
}

// TestMemoryOptimizerBase_Step_清空轨迹 测试 Step 后轨迹被清空。
func TestMemoryOptimizerBase_Step_清空轨迹(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	base.AddTrajectory(&trajectory.Trajectory{ExecutionID: "test"})
	assert.Equal(t, 1, len(base.GetTrajectories()))

	base.Step()
	assert.Equal(t, 0, len(base.GetTrajectories()))
}
```

同时更新该文件中第 154-171 行的 `TestMemoryOptimizerBase_Step_绑定后无梯度` 测试——因为它也调用了 Backward 和 Step，但未先 Bind，会导致新增的 ValidateParameters panic。改为：

```go
// TestMemoryOptimizerBase_Step_绑定后无梯度 测试绑定后 Step 返回空 map（_backward 是 pass）。
func TestMemoryOptimizerBase_Step_绑定后无梯度(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()

	operators := map[string]operator.Operator{
		"memory_call": memOp,
	}
	base.Bind(operators, nil, nil)

	// Backward 是 pass，不写梯度
	err := base.Backward(context.Background(), nil)
	assert.NoError(t, err)

	// Step 应返回空 map
	updates := base.Step()
	assert.Equal(t, 0, len(updates))
}
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/evolving/optimizer/memory_call/... -v`

Expected: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/optimizer/memory_call/base.go internal/evolving/optimizer/memory_call/base_test.go
git commit -m "fix(optimizer): MemoryOptimizerBase Backward/Step 对齐 Python 模板方法流程"
```

---

### Task 3: 全量回归测试

**Files:** 无修改

- [ ] **Step 1: 运行 optimizer 全包测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/evolving/optimizer/... -v`

Expected: 所有测试 PASS

- [ ] **Step 2: 运行 evolving 全包测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/evolving/... -v`

Expected: 所有测试 PASS

- [ ] **Step 3: 提交设计文档**

```bash
git add docs/superpowers/specs/2027-04-25-base-optimizer-backward-step-alignment-design.md
git commit -m "docs: 添加 BaseOptimizer Backward/Step 模板方法对齐设计文档"
```
