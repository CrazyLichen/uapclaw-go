# 10.3.23 server/hooks 覆盖率提升设计

## 背景

`internal/swarm/server/hooks` 包当前覆盖率 **79.4%**，低于项目要求的 ≥85%。
主要缺口集中在 LLM 依赖路径（queryLLM 13.6%、runPromptHook 52.5%），
以及若干边缘分支未覆盖。

## 设计决策

### D1: 接受 LLM 依赖路径由集成测试覆盖

`queryLLM` 和 `runPromptHook` 内部直接创建 `llm.NewModel` 并调用 `model.Invoke()`，
无法在不修改生产代码结构的前提下 mock。按项目编码规范 §3.2，
"无法 mock 的真实外部服务"应使用 `//go:build llm` 标签隔离，不纳入覆盖率基线。

现有 `executor_llm_test.go` 已有 `//go:build llm` 集成测试覆盖这两个方法。

**在 `executor.go` 中补充标注**：`queryLLM` 和 `runPromptHook` 方法注释中
添加"集成测试覆盖"说明，与 `executor_llm_test.go` 的 build tag 对应。

排除 LLM 依赖路径后，其余代码的覆盖率目标为 **≥90%**。

### D2: 不修改 HookExecutor 生产代码结构

不引入接口注入、函数字段等 mock 机制，保持与 Python `HookExecutor` 的结构一致性。
LLM 调用路径的真实正确性由集成测试保证。

## 补充测试用例

### T1: getSessionID 带 mock Session

**目标**：覆盖 `getSessionID` 中 `cbc.Session() != nil` 分支（当前 66.7%）

**测试位置**：`user_hook_rail_test.go`

**方案**：在现有 `TestUserHookRail_BeforeToolCall_附加上下文` 或类似测试中，
构造一个实现了 `GetSessionID() string` 的 mock Session，传入 `NewAgentCallbackContext`，
验证 `hookInput["session_id"]` 不为空。

```go
type mockSession struct{ id string }
func (m *mockSession) GetSessionID() string { return m.id }
// ... 其他 Session 接口方法返回零值
```

### T2: runCommandHook JSON 序列化失败

**目标**：覆盖 `runCommandHook` 中 `json.Marshal(hookInput)` 返回 error 的分支

**测试位置**：`executor_test.go`

**方案**：构造含不可 JSON 序列化值的 hookInput（如 `map[string]any{"ch": make(chan int)}`），
验证返回 `HookResult{Outcome: NON_BLOCKING_ERROR, Error: "serialize hook input: ..."}`

### T3: runCommandHook float64 timeout

**目标**：覆盖 `runCommandHook` 中 timeout 从 config 读取 float64 类型的分支

**测试位置**：`executor_test.go`

**方案**：hookConfigs 中 `"timeout": 5.0`（float64 而非 int），
验证超时值正确转换为 5 秒，命令正常执行。

同理补充 shell 从 config 读取的默认值分支。

### T4: runCommandHook ProcessState==nil

**目标**：覆盖 `runCommandHook` 中 `cmd.ProcessState == nil` 时返回
`"hook process killed"` 的分支

**测试位置**：`executor_test.go`

**方案**：用 `context.WithCancel` 在命令启动后立即 cancel，
模拟进程被外部 kill（非 timeout）的场景。
验证返回 `HookResult{Outcome: NON_BLOCKING_ERROR, Error: "hook process killed"}`

### T5: BeforeToolCall _tool_name 修改

**目标**：覆盖 `BeforeToolCall` 中 `modifiedInput` 含 `_tool_name` 且为非空 string 时
修改 `toolInputs.ToolName` 的分支

**测试位置**：`user_hook_rail_test.go`

**方案**：command hook echo 出含 `_tool_name` 的 JSON：
```json
{"decision": "allow", "modifiedInput": {"_tool_name": "safe_tool", "path": "/safe"}}
```
验证 `toolInputs.ToolName` 从原来的 `"test_tool"` 变为 `"safe_tool"`。

### T6: BeforeToolCall additionalContext 拼接

**目标**：覆盖 `BeforeToolCall` 中 `existing != ""` 时换行拼接 additionalContext 的分支

**测试位置**：`user_hook_rail_test.go`

**方案**：配置两个 hook 都返回 additionalContext：
- hook1 返回 `"context1"`
- hook2 返回 `"context2"`
验证 `cbc.Extra()["_hook_additional_context"]` 为 `"context1\ncontext2"`

### T7: AfterToolCall 非 string ToolResult

**目标**：覆盖 `AfterToolCall` 中 `ToolResult` 为非 string 类型时 JSON 序列化保底的分支

**测试位置**：`user_hook_rail_test.go`

**方案**：设置 `ToolResult` 为 `map[string]any{"key": "value"}`（非 string），
配置 hook 返回 additionalContext，
验证 ToolResult 被正确拼接为 `{"key":"value"}\n[Hook 发现]: extra info`

### T8: AfterInvoke reason 截断

**目标**：覆盖 `AfterInvoke` 中 `len(reason) > 200` 时截断到 200 字符的分支

**测试位置**：`user_hook_rail_test.go`

**方案**：command hook echo 出超长 reason（>200 字符）+ exit 2，
验证 `cbc.Extra()["_stop_hook_feedback"]` 长度 ≤ 200

## 覆盖率预估

| 方法 | 改前 | 改后预估 | 说明 |
|------|------|----------|------|
| NewHookExecutor | 100% | 100% | |
| RunAll | 93.1% | 93% | 跳过 panic recovery |
| ParseCommandOutput | 100% | 100% | |
| ExtractJSONFromResponse | 100% | 100% | |
| runCommandHook | 87.9% | ~97% | T2+T3+T4 |
| runPromptHook | 52.5% | ~52% | LLM 依赖，集成测试覆盖 |
| queryLLM | 13.6% | ~14% | LLM 依赖，集成测试覆盖 |
| NewUserHookRail | 100% | 100% | |
| BeforeToolCall | 85.2% | ~97% | T5+T6 |
| AfterToolCall | 85.7% | ~97% | T7 |
| OnToolException | 100% | 100% | |
| AfterInvoke | 94.1% | 100% | T8 |
| getSessionID | 66.7% | 100% | T1 |

**整体预估**：~83%（LLM 路径纳入分母），非 LLM 路径 ~95%

## 文件变更清单

| 文件 | 变更 |
|------|------|
| `internal/swarm/server/hooks/executor.go` | queryLLM/runPromptHook 注释补充"集成测试覆盖" |
| `internal/swarm/server/hooks/executor_test.go` | 新增 T2、T3、T4 |
| `internal/swarm/server/hooks/user_hook_rail_test.go` | 新增 T1、T5、T6、T7、T8 |

无生产代码逻辑变更，仅补充测试和注释。
