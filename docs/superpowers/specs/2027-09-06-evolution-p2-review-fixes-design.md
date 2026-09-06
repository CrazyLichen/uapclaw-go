# 9.24 P2 实现偏差修复 + any 消除设计

## 概述

9.24 P2（EvolutionRail 基类 + TrajectoryRail）实现完成后，审查发现 8 处与设计计划不一致的偏差，以及若干 `any` 类型使用需要评估。本文档记录逐项讨论的结论和修复方案。

## 偏差修复

### 偏差 1：测试文件结构（4 个文件合并为 1 个）

**现状**：所有 44+ 测试函数合并到单个 `evolution_rail_test.go`（736 行）
**设计要求**：4 个独立文件
**决策**：拆分为 4 个文件

| 文件 | 内容 |
|------|------|
| `extension_test.go` | noOpExtension 测试、EvolutionExtension 接口测试、EvolutionTriggerPoint 测试 |
| `evolution_rail_test.go` | EvolutionRail 构造/选项/回调/轨迹收集/异步演化测试 |
| `trajectory_rail_test.go` | TrajectoryRail 构造/Priority 测试 |
| `helpers_test.go` | baseMessageToMap/toolInfoToMap/splitResponseTokenFields/normalizeSkillNames 等测试 |

共享的 helper 类型（fakeTrajectorySink、fakeSessionFacade、countingExtension）放入 `evolution_rail_test.go`，其他文件通过 `package evolution` 同包访问。

### 偏差 2：`task.Done()` 返回值类型处理

**现状**：使用 `select { case <-task.Done(): ... default: ... }` 模式
**设计文档**：`if t.Done() { delete(...) }`
**决策**：回填设计文档为正确的 select 模式。`BackgroundTask.Done()` 返回 `<-chan struct{}` 不是 `bool`，select 是唯一正确写法。

### 偏差 3：快照键函数命名

**现状**：`formatSkillName(snapshot)`
**设计文档**：`snapshotKey(snapshot)`
**决策**：保留 `formatSkillName`（更具描述性），回填设计文档。

### 偏差 4：`saveTrajectory` 非导出方法被内联

**现状**：`AfterInvoke` 中直接调用 `r.trajectoryStore.Save(traj, "")`
**设计文档**：列出非导出方法 `saveTrajectory(traj)`
**决策**：保持内联（仅一行操作无需封装），回填设计文档移除 `saveTrajectory` 的列出。

### 偏差 5：`baseMessageToMap` / `toolInfoToMap` 未在设计计划中

**现状**：实现时新增了两个必需的辅助函数
**原因**：`BaseMessage` 和 `ToolInfoInterface` 接口没有 `ToMap()` 方法，Python 用 `model_dump()` 直接序列化
**决策**：回填设计文档，在 helpers.go 规格中补充两个函数的说明。

### 偏差 6：`containsMessage` 去重方式

**现状**：`json.Marshal(msg)` 序列化后比较
**设计文档**：`fmt.Sprintf("%v", msg)` 做去重键
**决策**：保留 `json.Marshal`（`%v` 对 map 输出顺序不确定，无法可靠去重），回填设计文档。

### 偏差 7：6 个新增辅助方法

**现状**：额外添加了 6 个设计未列出的方法

| 方法 | 用途 | 调用方 |
|------|------|--------|
| `_normalizeNameSet(raw any)` | 桥接 normalizeSkillNames | P3/P4 子类预留 |
| `_isSkillDisabled(skillName)` | 检查技能是否被禁用 | P3/P4 子类预留 |
| `_collectMessagesFromTrajectory(traj)` | 桥接 collectMessagesFromTrajectory | P3/P4 子类预留 |
| `normalizeCallbackMessagesGo(messages)` | 桥接 normalizeCallbackMessages | P3/P4 子类预留 |
| `_getAgentIDStr(cbc)` | 从回调上下文获取 agent ID | AfterModelCall/AfterToolCall |
| `_isBlank(s)` | 检查空字符串 | 内部辅助 |

**决策**：保留全部 6 个，回填设计文档。

### 偏差 8：`normalizeCallbackMessages` 浅拷贝

**现状**：实现做了浅拷贝防止外部修改
**设计文档**：只说"浅拷贝返回"
**决策**：回填设计文档，补充"浅拷贝防止外部修改"说明。

## any 类型消除

### Any1：`PerAgentCallbackFunc` 签名

```go
// 现状
type PerAgentCallbackFunc func(ctx context.Context, agentCallbackContext any) error
```

**调查结果**：`agentinterfaces` 包已导入 `callback` 包，`callback` 不能反向导入 `agentinterfaces`（循环依赖）。约 40 处引用均做 `railCtx.(*AgentCallbackContext)` 类型断言。

**决策**：保持 `any` + 加 TODO 注释说明循环依赖原因和未来解决方向。

```go
// PerAgentCallbackFunc 实例级 PerAgent 回调函数类型。
// agentCallbackContext 实际类型为 *interfaces.AgentCallbackContext，回调内需类型断言。
//
// TODO: 收紧为 *AgentCallbackContext 具体类型。当前因循环依赖
// (callback → agentinterfaces → callback) 无法直接引用。
// 可能的解决方案：(A) 定义回调上下文接口于底层包 (B) 抽取共享类型包
//
// 对应 Python: AnyAgentCallback = Union[AgentCallback, SyncAgentCallback]
type PerAgentCallbackFunc func(ctx context.Context, agentCallbackContext any) error
```

### Any2：`normalizeSkillNames(raw any)` / `normalizeMemberRole(role any)`

```go
// 现状
func normalizeSkillNames(raw any) map[string]bool
func normalizeMemberRole(role any) *string
```

**决策**：
- `normalizeSkillNames` 改为只接受 `[]string`（单个 string 包裹为 `[]string{s}`）
- `normalizeMemberRole` 改为只接受 `string`（Go 中枚举类型统一用 `String()` 转换后再传入）
- `_normalizeNameSet(raw any)` 桥接方法保留 `any`（P3/P4 需要）

修改后的签名：

```go
func normalizeSkillNames(names []string) map[string]bool
func normalizeMemberRole(role string) *string
```

`WithDisabledSkills` 已经接受 `[]string`，无需修改。`SetTrajectorySink` 的 `memberRole` 参数已是 `string`，无需修改。

### Any3：`approval_runtime.go` `FinalizeStagedEvolutionRequest` 的 `any` 参数

**决策**：保持 `any`，保留现有 TODO。P2 不涉及调用此方法，P3/P4 实现时再收紧。

### Any4：`TrajectoryStep.Logprobs any`

```go
// 现状
Logprobs any
```

**决策**：改为 `[]map[string]any`。OpenAI logprobs 最常见格式是 `list[dict]`，非标准格式用 `Meta` 字段兜底。

同步影响：
- `types.go`：`Logprobs any` → `Logprobs []map[string]any`
- `helpers.go`：`splitResponseTokenFields` 返回值第 4 个从 `any` 改为 `[]map[string]any`
- `evolution_rail.go`：`AfterModelCall` 中 `logprobs` 变量类型同步调整
- `extractor.go`：`buildLLMCallDetail` 中 logprobs 赋值同步调整
- 相关测试同步调整

### Any5：`extractor.go` 的 `extractInputs()/extractOutputs()/parseLLMResponse()` any 返回值

```go
// 现状（待删除）
func parseLLMResponse(outputs any) map[string]any
func extractInputs(span *tracer.Span) any
func extractOutputs(span *tracer.Span) any

// 保留
func extractInputsAsMap(span *tracer.Span) map[string]any
func extractOutputsAsMap(span *tracer.Span) map[string]any
```

**决策**：删除 `extractInputs()`、`extractOutputs()`、`parseLLMResponse()` 三个 any 版本，只保留 `AsMap` 版本。需先确认无外部调用方。

### Any5a-5e：`SessionFacade` 接口 `any` 方法

| 方法 | 决策 | 原因 |
|------|------|------|
| `GetState(key) (any, error)` | 保持 any + 加注释 | 不同 key 返回不同类型（map/struct/slice） |
| `WriteStream(ctx, data any)` | 保持 any + 加注释 | 3 种传入类型并存，内部 normalizeOutputStream 统一处理 |
| `WriteCustomStream(ctx, data any)` | 保持 any + 加注释 | 零生产调用方，预留 Python 对齐 |
| `GetEnv(key, ...any) any` | 保持 any + 加注释 | 配置存储天然多态（float64/int/bool/string） |
| `Interact(ctx, value any)` | 保持 any + 加注释 | 对齐 Python Session.interact(value) 任意类型 |

注释格式：

```go
// GetState 获取会话状态值。
// 返回类型由 key 决定：常见 map[string]any / *DeepAgentState / []string / []any 等。
// 调用方需根据 key 语义做类型断言。
GetState(key state.StateKey) (any, error)

// WriteStream 写入流数据。
// data 接受 *stream.OutputSchema / stream.OutputSchema / map[string]any，
// 内部 normalizeOutputStream 统一转为 OutputSchema。
WriteStream(ctx context.Context, data any) error

// WriteCustomStream 写入自定义流数据。
// data 接受 stream.CustomSchema / map[string]any / 任意类型，
// 内部 normalizeCustomStream 统一转为 CustomSchema。
// 当前无生产调用方。
WriteCustomStream(ctx context.Context, data any) error

// GetEnv 获取环境变量。
// 返回类型由配置值决定：常见 float64 / int / bool / string / nil。
// 调用方需根据 key 语义做类型断言。
GetEnv(key string, defaultValue ...any) any

// Interact 交互（等待用户输入）。
// value 通常传 string；内部序列化为 InteractionOutput.Value (any)。
// 对齐 Python Session.interact(value) 的任意类型签名。
Interact(ctx context.Context, value any) error
```
