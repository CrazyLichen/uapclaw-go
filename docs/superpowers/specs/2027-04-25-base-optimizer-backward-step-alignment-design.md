# BaseOptimizer Backward/Step 模板方法对齐设计

## 背景

9.72e 审查发现 `ToolOptimizerBase` 和 `MemoryOptimizerBase` 的 `Backward()` / `Step()` 方法缺少 Python `BaseOptimizer.backward()` / `step()` 模板方法中的标准流程步骤（ValidateParameters → SelectSignals → _backward → ClearTrajectories）。

## Python 模板方法流程

```python
# BaseOptimizer.backward()
async def backward(self, signals):
    self._validate_parameters()                        # ① 校验参数
    self._selected_signals = self._select_signals(signals)  # ② 选择信号
    try:
        await self._backward(signals)                  # ③ 子类逻辑
    except Exception as e:
        raise build_error(TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR, ...)

# BaseOptimizer.step()
def step(self):
    self._validate_parameters()                        # ① 校验参数
    try:
        updates = self._step()                         # ② 子类逻辑
        self.clear_trajectories()                      # ③ 清轨迹
        return updates or {}                           # ④ 返回
    except Exception as e:
        self.clear_trajectories()                      # ⑤ 异常也清轨迹
        raise build_error(TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR, ...)
```

## 当前 Go 各子优化器状态

| 子优化器 | Backward() | Step() |
|---|---|---|
| InstructionOptimizer | ✅ 完整 | ✅ 完整 |
| SkillExperienceOptimizer | ✅ 完整 | ✅ 完整 |
| TeamSkillExperienceOptimizer | ✅ 完整 | ✅ 完整 |
| **ToolOptimizerBase** | ❌ 直接 return nil | ❌ 直接 return {} |
| **MemoryOptimizerBase** | ❌ 直接 return nil | ❌ 直接 return {} |

## 决策：保持现状模式，每个子类各自实现

Go 无法像 Python 那样通过继承实现模板方法自动分派到子类的 SelectSignals 覆写（Go 嵌入是静态分派）。选择保持每个子类各自实现完整流程，而非引入额外的间接层。

## 修改范围（2 个文件）

### 1. `internal/evolving/optimizer/tool_call/base.go`

**Backward()** — 补充 ValidateParameters + SelectSignals + SetSelectedSignals：

```go
// Backward 反向传播：从信号计算梯度。对齐 Python 空实现。
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

**Step()** — 补充 ValidateParameters + ClearTrajectories，拆出 step() 非导出方法：

```go
// Step 生成更新映射。对齐 Python BaseOptimizer.step() 模板方法流程。
//
// 对齐 Python:
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

### 2. `internal/evolving/optimizer/memory_call/base.go`

**Backward()** — 同 ToolOptimizerBase 模式：

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

**Step()** — 同 ToolOptimizerBase 模式：

```go
// Step 生成更新映射。对齐 Python BaseOptimizer.step() 模板方法流程。
//
// 对齐 Python:
//
//	self._validate_parameters()
//	updates = self._step()
//	self.clear_trajectories()
//	return updates or {}
func (b *MemoryOptimizerBase) Step() map[cschema.UpdateKey]any {
	b.ValidateParameters()
	updates := b.step()
	b.ClearTrajectories()
	return updates
}

// step 子类逻辑，对齐 Python _step()。
// MemoryOptimizerBase 为空实现，返回空映射。
func (b *MemoryOptimizerBase) step() map[cschema.UpdateKey]any {
	return map[cschema.UpdateKey]any{}
}
```

## 不修改的部分

- InstructionOptimizer — 已完整对齐，无需改动
- SkillExperienceOptimizer — 已完整对齐，无需改动
- TeamSkillExperienceOptimizer — 已完整对齐，无需改动
- BaseOptimizerMixin — 不增加模板方法辅助函数
- evaluator_pipeline ⤵️ 标记 — 确认为离线 CLI 工具，保留标记不回填

## error-wrap 说明

Python 的 backward/step 模板方法包含 try/except 错误包装（`TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR` / `TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR`）。Go 中三个已实现的子优化器也没有统一在 Mixin 层做 error-wrap，而是各自在 Backward 返回 error 时自行包装（如 InstructionOptimizer）。本次修改保持一致：空实现不会产生异常，不加 error-wrap。
