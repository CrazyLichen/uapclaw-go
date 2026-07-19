# 9.72a InstructionOptimizer 设计文档

## 概述

实现 InstructionOptimizer（指令优化器），这是自演化系统中 LLM 维度的核心优化器，
负责通过文本梯度优化来改写 system_prompt 和 user_prompt。

## 流程位置

InstructionOptimizer 位于 Trainer 离线演化循环的 backward/step 阶段：

```
Trainer 离线演化循环:
  ① evaluate → 产生 EvaluatedCase + EvolutionSignal
  ② signal   → SignalDetector 过滤/分类信号
  ③ backward → Optimizer 从信号计算梯度  ◄── InstructionOptimizer 在此
  ④ step     → Optimizer 生成更新映射     ◄── InstructionOptimizer 在此
  ⑤ update   → Updater 应用更新到 Operator
  ⑥ writeback → 持久化更新结果
```

## Python 参考

- `openjiuwen/agent_evolving/optimizer/llm_call/base.py` — LLMCallOptimizerBase
- `openjiuwen/agent_evolving/optimizer/llm_call/instruction_optimizer.py` — InstructionOptimizer
- `openjiuwen/agent_evolving/optimizer/llm_call/templates.py` — 5 个 PromptTemplate 常量

## 9.80 依赖确认

9.80（UpdateExecution + Types）已完整实现：
- `schema/protocol.go` — 17 个协议常量 ✅
- `schema/update.go` — UpdateValue/ApplyResult/NormalizeUpdates ✅
- `update_execution.go` — ExecuteUpdates/ApplyUpdates/SummarizeApplyResults ✅

9.72a 不缺 9.80 依赖，`Step()` 返回的 `map[schema.UpdateKey]any` 可直接被 `ExecuteUpdates` 消费。

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 辅助函数位置 | `internal/evolving/utils.go` | 与 Python `agent_evolving/utils.py` 对齐 |
| 目录文件拆分 | 3 文件：base.go + instruction_optimizer.go + templates.go + doc.go | 与 Python 一比一 |
| LLM 调用方式 | 直接用 `llm.Model.Invoke` | 与 Python 一致，简单直接 |
| 模板格式 | `PromptTemplate` 常量 | 与 Python 一致，调用路径对齐 |
| 继承方式 | LLMCallOptimizerBase 嵌入 BaseOptimizerMixin，InstructionOptimizer 嵌入 LLMCallOptimizerBase | 两层嵌入，与 Python 继承链对齐 |

## 文件结构

```
internal/evolving/
├── utils.go                              # 新建：GetContentStringFromTemplate
└── optimizer/
    ├── base.go                           # 已有：BaseOptimizer + BaseOptimizerMixin + TextualParameter
    └── llm_call/                         # 新建子包
        ├── doc.go                        # 包文档
        ├── base.go                       # LLMCallOptimizerBase 嵌入结构体
        ├── instruction_optimizer.go      # InstructionOptimizer 核心实现
        └── templates.go                  # 5 个 PromptTemplate 常量
```

## 组件详细设计

### 1. `internal/evolving/utils.go`

```go
// GetContentStringFromTemplate 将 PromptTemplate 转为多行文本。
// 对齐 Python: TuneUtils.get_content_string_from_template(template)
func GetContentStringFromTemplate(tpl *prompt.PromptTemplate) string
```

逻辑：调用 `tpl.ToMessages()` 获取消息列表，拼接所有消息的文本内容，用 `\n` 连接。

### 2. `llm_call/base.go` — LLMCallOptimizerBase

嵌入 `optimizer.BaseOptimizerMixin`，提供 LLM 维度公共逻辑：

```go
type LLMCallOptimizerBase struct {
    optimizer.BaseOptimizerMixin
}

func (b *LLMCallOptimizerBase) Domain() string           { return "llm" }
func (b *LLMCallOptimizerBase) DefaultTargets() []string  { return []string{"system_prompt", "user_prompt"} }
func (b *LLMCallOptimizerBase) RequiresForwardData() bool { return true }

// isTargetFrozen 检查 target 是否在 op.GetTunables() 中
func (b *LLMCallOptimizerBase) isTargetFrozen(op operator.Operator, target string) bool

// getPromptTemplate 从 op.GetState() 获取 target 内容构建 PromptTemplate
func (b *LLMCallOptimizerBase) getPromptTemplate(op operator.Operator, target string) *prompt.PromptTemplate
```

### 3. `llm_call/instruction_optimizer.go` — InstructionOptimizer

```go
type InstructionOptimizer struct {
    LLMCallOptimizerBase
    model *llm.Model
}

func NewInstructionOptimizer(model *llm.Model) *InstructionOptimizer
```

**BaseOptimizer 接口实现：**

| 方法 | 实现 |
|------|------|
| `Domain()` | 委托 LLMCallOptimizerBase |
| `RequiresForwardData()` | 委托 LLMCallOptimizerBase |
| `DefaultTargets()` | 委托 LLMCallOptimizerBase |
| `Bind()` | 委托 BaseOptimizerMixin.Bind() |
| `AddTrajectory/GetTrajectories/ClearTrajectories` | 委托 Mixin |
| `Parameters()` | 委托 Mixin |
| `SelectSignals(signals)` | 仅保留失败驱动信号 |
| `Backward(ctx, signals)` | ValidateParameters + SelectSignals + backward |
| `Step()` | ValidateParameters + step + ClearTrajectories |

**SelectSignals 过滤规则（对齐 Python）：**

仅保留以下类型的信号：
- `execution_failure`
- `low_score`
- `user_correction`
- `collaboration_failure`
- context.score == 0 的信号

**backward 核心逻辑（对齐 Python _backward）：**

```
遍历每个 parameter:
  1. 清空 _optimized 缓存（system_prompt_optimized, user_prompt_optimized）
  2. 如果没有选中信号 → continue
  3. 生成文本梯度 generateTextualGradient
  4. 设置 system_prompt / user_prompt 梯度（未冻结时）
  5. 根据 targets 决定优化方式:
     - system+user 都在且未冻结 → optimizeBoth
     - 仅 system → optimizeSingle("system_prompt")
     - 仅 user → optimizeSingle("user_prompt")
  6. 结果写入 param.SetGradient("xxx_optimized", val)
```

**私有方法：**

| 方法 | 功能 | Python 对应 |
|------|------|-------------|
| `backward(ctx, signals)` | 反向传播主逻辑 | `_backward` |
| `step()` | 返回预计算更新映射 | `_step` |
| `generateTextualGradient(ctx, op)` | LLM 分析 prompt 失败原因 | `_generate_textual_gradient` |
| `invokeLLM(ctx, messages)` | 调用 LLM 返回字符串 | `_invoke_llm` |
| `optimizeBoth(ctx, op, param)` | 联合优化 system+user prompt | `_optimize_both` |
| `optimizeSingle(ctx, op, param, promptType)` | 单独优化一个 prompt | `_optimize_single` |
| `formatBadCases()` | 格式化失败信号 | `_format_bad_cases` |
| `extractTag(response, tag)` | 提取 XML 标签内容 | `_extract_tag` |
| `restorePlaceholders(ctx, original, optimized)` | 确保保留原始占位符 | `_restore_placeholders` |

### 4. `llm_call/templates.go` — 5 个 PromptTemplate 常量

一比一复刻 Python 原文字符串：

| Go 常量名 | Python 对应 |
|-----------|-------------|
| `PromptInstructionOptimizeTemplate` | `PROMPT_INSTRUCTION_OPTIMIZE_TEMPLATE` |
| `PromptInstructionOptimizeBothTemplate` | `PROMPT_INSTRUCTION_OPTIMIZE_BOTH_TEMPLATE` |
| `CreatePromptTextualGradientTemplate` | `CREATE_PROMPT_TEXTUAL_GRADIENT_TEMPLATE` |
| `CreateBadCaseTemplate` | `CREATE_BAD_CASE_TEMPLATE` |
| `PlaceholderRestoreTemplate` | `PLACEHOLDER_RESTORE_TEMPLATE` |

占位符：`{{prompt_instruction}}`、`{{system_prompt}}`、`{{user_prompt}}`、`{{bad_cases}}`、
`{{reflections_on_bad_cases}}`、`{{tools_description}}`、`{{question}}`、`{{label}}`、
`{{answer}}`、`{{reason}}`、`{{original_prompt}}`、`{{revised_prompt}}`、
`{{all_placeholders}}`、`{{missing_placeholders}}`

## 数据流

```
Trainer.backward() 调用 InstructionOptimizer.Backward(ctx, signals)
  → ValidateParameters()
  → SelectSignals(signals) → 仅保留失败驱动信号 → selectedSignals
  → backward(ctx, signals):
      遍历 parameters:
        1. param.SetGradient("system_prompt_optimized", nil)
        2. param.SetGradient("user_prompt_optimized", nil)
        3. 如果 selectedSignals 为空 → continue
        4. generateTextualGradient:
             getPromptTemplate(op, "system_prompt") → 系统提示词模板
             getPromptTemplate(op, "user_prompt") → 用户提示词模板
             CreatePromptTextualGradientTemplate.Format(keywords).ToMessages()
             model.Invoke(ctx, messages) → 文本梯度
        5. 设置梯度:
             param.SetGradient("system_prompt", gradient) — 未冻结时
             param.SetGradient("user_prompt", gradient) — 未冻结时
        6. 预计算优化 prompt:
             has_sys && has_usr → optimizeBoth → 两个 LLM 调用
             has_sys only → optimizeSingle("system_prompt") → 一个 LLM 调用
             has_usr only → optimizeSingle("user_prompt") → 一个 LLM 调用
        7. 结果写入 param.SetGradient("xxx_optimized", val)

Trainer.step() 调用 InstructionOptimizer.Step()
  → ValidateParameters()
  → step():
      遍历 parameters:
        读取 system_prompt_optimized / user_prompt_optimized
        → 组装 map[UpdateKey]any 返回
  → ClearTrajectories()
```

## 日志同步

对照 Python 中 InstructionOptimizer 无显式 logger 调用（LLM 调用层日志由 llm_resilience 和 Model 层记录），
Go 实现中同样不在 InstructionOptimizer 方法内添加额外日志，仅依赖底层 Model.Invoke 的日志输出。

在 Backward/Step 的异常路径中（与 BaseOptimizer.base.go 一致），使用 `exception.NewBaseError` 包装错误。

## 测试策略

| 测试场景 | 方法 |
|----------|------|
| LLMCallOptimizerBase.Domain/DefaultTargets | 单元测试 |
| LLMCallOptimizerBase.isTargetFrozen | 单元测试（构造 fakeOperator） |
| LLMCallOptimizerBase.getPromptTemplate | 单元测试 |
| InstructionOptimizer.SelectSignals | 单元测试（构造各种 signal 类型） |
| InstructionOptimizer.Step（无优化结果时返回空） | 单元测试 |
| InstructionOptimizer.Backward（需要 LLM 调用） | `//go:build llm` 集成测试 |
| extractTag | 单元测试（纯字符串操作） |
| formatBadCases | 单元测试 |
| restorePlaceholders（需要 LLM 调用） | `//go:build llm` 集成测试 |
| GetContentStringFromTemplate | 单元测试 |
| 5 个模板常量 Format + ToMessages | 单元测试（验证占位符替换和消息生成） |

## 回填标记

无 ⤵️/⤴️ 需要回填的内容。9.71 的 evaluator_pipeline 占位不涉及 9.72a。
