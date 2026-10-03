# 第二批集成测试设计：Runner + Session + Tool + DeepAgent + Rail

## 概述

在第一批基础设施骨架（MockLLM + Suite 基类 + 迁移）完成后，第二批实现 5 个模块的集成测试，覆盖 Runner 生命周期、Session CRUD、工具注册执行、DeepAgent ReAct 循环、Rail 子系统。

对应 Python 源码：
- `tests/system_tests/runner/test_runner.py`（Runner 生命周期）
- `tests/system_tests/harness/test_deep_agent_e2e.py`（DeepAgent 端到端）
- `tests/system_tests/harness/rail/test_task_completion_rail.py`（TaskCompletionRail）
- `tests/system_tests/harness/rail/test_deep_agent_interrupt.py`（ConfirmInterruptRail）
- `tests/cli/e2e/test_session_persist.py`（Session 持久化）

---

## 1. 目录结构与文件规划

```
tests/integration/
├── agentcore/
│   ├── runner/
│   │   ├── doc.go                        # ✅ 已有
│   │   └── runner_test.go                # 🆕 模块1：Runner 生命周期
│   ├── session/
│   │   ├── doc.go                        # 🆕 包文档
│   │   └── session_test.go               # 🆕 模块3：Session 生命周期
│   ├── harness/
│   │   ├── doc.go                        # ✅ 已有
│   │   ├── deep_agent_test.go            # 🆕 模块2：DeepAgent ReAct 循环
│   │   └── rails/
│   │       ├── doc.go                    # ✅ 已有
│   │       ├── interrupt/
│   │       │   ├── doc.go               # ✅ 已有
│   │       │   ├── ask_user_test.go     # ✅ 已有
│   │       │   ├── confirm_rail_test.go # 🆕 模块4b：ConfirmInterruptRail
│   │       │   └── task_completion_test.go # 🆕 模块4a：TaskCompletionRail
│   │       └── tools/
│   │           ├── doc.go               # 🆕 模块5：工具注册文档
│   │           └── tool_execution_test.go # 🆕 模块5：工具注册与执行
├── suite/
│   ├── base_suite.go                    # ✅ 已有
│   ├── runner_suite.go                  # ✅ 已有
│   ├── session_suite.go                 # ✏️ 补全 NewTestSession()
│   └── agent_suite.go                   # ✏️ 补全 NewDeepAgentForTest()
```

---

## 2. Suite 基类补全

### 2.1 SessionSuite 补全

当前 `NewSession()` 返回 nil，改为提供 `NewTestSession()` 辅助方法。

**设计决策**：按需创建（非 SetupSuite 自动创建），对齐 Python 每个测试自己创建 session。

```go
// NewTestSession 创建测试用 Session 实例。
// 使用 InMemoryCheckpointer，对齐 Python: Session(session_id=..., envs=...)
func (s *SessionSuite) NewTestSession(sessionID string) *session.Session {
    return session.NewSession(
        session.WithSessionID(sessionID),
        session.WithEnvs(map[string]any{}),
    )
}
```

**要点**：
- 返回 `*session.Session`（具体类型），不返回 `SessionFacade` 接口——测试需调用 PreRun/PostRun/Commit 等具体方法
- InMemoryCheckpointer 是 NewSession 的默认行为，无需显式传入
- 删除原有的 `NewSession()` 方法（返回 nil 的占位实现）

### 2.2 AgentSuite 补全

当前只有骨架，补全 `NewDeepAgentForTest()` 辅助方法。

**设计决策**：工厂模式，直接调用 `CreateDeepAgent` 10 步流水线，对齐 Python `create_deep_agent()`。

```go
// NewDeepAgentForTest 通过工厂创建 DeepAgent 实例。
// 预填 MockLLM 的 Model 和默认 AgentCard，
// 测试只需传入 ToolInstances/Rails 等差异化参数。
func (s *AgentSuite) NewDeepAgentForTest(
    ctx context.Context,
    params hconfig.CreateDeepAgentParams,
) (*harness.DeepAgent, error) {
    if params.Model == nil {
        model, err := llm.NewModel(s.ClientConfig, s.ModelConfig)
        if err != nil {
            return nil, fmt.Errorf("创建 Mock Model 失败: %w", err)
        }
        params.Model = model
    }
    if params.Card == nil {
        params.Card = s.NewAgentCard("test_agent", "测试 Agent")
    }
    return harness.CreateDeepAgent(ctx, params)
}
```

**要点**：
- Model 从 RunnerSuite 继承的 ClientConfig/ModelConfig 创建，走 ClientRegistry 获取 MockLLM
- Card 默认生成，测试可覆盖
- 测试传入 ToolInstances、Rails、MaxIterations 等差异化参数

---

## 3. 模块 1：Runner 生命周期

对应 Python `tests/system_tests/runner/test_runner.py`

```go
// RunnerLifeCycleSuite 嵌入 RunnerSuite，测试 Runner 启停 + 资源注册
type RunnerLifeCycleSuite struct {
    suite.RunnerSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestRunner_StartStop_无报错` | `Runner.start()/stop()` | ❌ | Start 返回 nil、Stop 返回 nil |
| `TestRunner_GetResourceMgr_非空` | `Runner.resource_mgr` | ❌ | GetResourceMgr() 返回非 nil |
| `TestRunner_注册AgentCard_可获取` | `resource_mgr.add_agent()` | ✅ | AddAgent(card, provider) → GetAgent(id) 返回同名 card |
| `TestRunner_注册Tool_可获取` | `resource_mgr.add_tool()` | ❌ | AddTool(tool) → GetTool(name) 返回非空 |
| `TestRunner_注销Tool_不可获取` | `resource_mgr.remove_tool()` | ❌ | AddTool → RemoveTool → GetTool 返回空 |

**说明**：
- Python 的 Runner 测试 6 个中 4 个 skip（需 MCP/A2A 网络），Go 端只对齐可 Mock 的基础场景
- Suite 嵌入 RunnerSuite，自动获得 Runner 启停 + MockLLM 注册
- `TestRunner_注册AgentCard_可获取` 使用 AgentProvider 返回 fakeBaseAgent（mock 实现 BaseAgent 接口）

---

## 4. 模块 3：Session 生命周期

对应 Python `tests/cli/e2e/test_session_persist.py` + 各测试中 Session 的用法

```go
// SessionLifeCycleSuite 嵌入 SessionSuite，测试 Session CRUD
type SessionLifeCycleSuite struct {
    suite.SessionSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestNewSession_ID自动生成` | `Session()` 无参 | ❌ | 未指定 ID 时自动生成 UUID，非空 |
| `TestNewSession_指定ID` | `Session(session_id=...)` | ❌ | WithSessionID("test-1") → GetSessionID()=="test-1" |
| `TestPreRun_幂等` | `session.pre_run()` | ❌ | 调用两次无报错、无副作用 |
| `TestPostRun_幂等` | `session.post_run()` | ❌ | PreRun → PostRun → PostRun（第二次不报错） |
| `TestUpdateState_GetState` | `session.state` 读写 | ❌ | UpdateState({"k":"v"}) → GetState(StringKey("k"))=="v" |
| `TestDumpState_完整快照` | `session.dump_state()` | ❌ | UpdateState 后 DumpState 包含写入的键值 |
| `TestInMemoryCheckpointer_持久化` | `test_session_persist` | ❌ | PreRun → PostRun → Checkpointer 中 Session 存在 |
| `TestSession_Env读写` | `session.get_env()` | ❌ | WithEnvs({"KEY":"VAL"}) → GetEnv("KEY")=="VAL" |

**说明**：
- 全部不需要 MockLLM，纯 Session API 测试
- `TestInMemoryCheckpointer_持久化` 对齐 Python 的 `test_session_persist.py`——验证 InMemoryCheckpointer 的 Save/Recover 流程
- `TestSession_Env读写` 验证 env 通过 Config 传播的链路

---

## 5. 模块 5：工具注册与执行链路

对应 Python 各测试中的工具使用模式（`_NoteWriteTool`、`fakeTool` 等）

```go
// ToolExecutionSuite 嵌入 RunnerSuite，测试工具注册 + 通过 AbilityManager 执行
type ToolExecutionSuite struct {
    suite.RunnerSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestMapFunction_注册执行` | `_NoteWriteTool` 模式 | ❌ | 注册 MapFunction → ResourceMgr 有实例 → Invoke 返回结果 |
| `TestInvokeFunction_注册执行` | `NewTool()` 模式 | ❌ | 注册泛型工具 → AbilityManager 有 card → 可调用 |
| `TestTool_执行返回错误` | 工具失败场景 | ❌ | 工具 Invoke 返回 error → 调用方收到错误 |
| `TestTool_多工具注册` | 多工具链 | ❌ | 注册 2+ 工具 → ResourceMgr 分别可获取 |

**说明**：
- 使用 `tool.NewMapFunction(card, invokeFn, nil)` 注册弱类型工具
- 使用 `tool.NewTool(fn, WithToolName(...))` 注册强类型工具
- 不需要 DeepAgent，只测 Tool + ResourceMgr + AbilityManager 的注册/执行链路
- 为 DeepAgent 测试铺路——DeepAgent 的 ReAct 循环依赖工具执行正确

---

## 6. 模块 2：DeepAgent ReAct 循环

对应 Python `tests/system_tests/harness/test_deep_agent_e2e.py`

```go
// DeepAgentE2ESuite 嵌入 AgentSuite，测试 DeepAgent 完整执行链路
type DeepAgentE2ESuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | MockLLM 响应 | 验证点 |
|---------|------------|-------------|--------|
| `TestInvoke_单轮纯文本` | `test_deep_agent_invoke_e2e` | `[TextResponse("答案")]` | Invoke 返回结果、无 tool_call |
| `TestInvoke_单轮工具调用` | 同上 | `[ToolCall("read_file",...), TextResponse("结果")]` | 第一次 LLM 返回 tool_call → 工具执行 → 第二次 LLM 返回文本 |
| `TestInvoke_多轮工具链` | `test_deep_agent_complex_task_multi_tool_chain` | `[ToolCall("read_file",...), ToolCall("write_file",...), TextResponse("完成")]` | 连续 3 次 LLM 调用，工具按序执行 |
| `TestInvoke_工具失败错误传播` | 同上 | `[ToolCall("fail_tool",...), TextResponse("已处理错误")]` | 工具返回 error → LLM 收到错误信息 → Agent 继续执行 |
| `TestInvoke_带Session` | Agent+Session 联动 | `[TextResponse("带 Session 执行")]` | Invoke 带 Session → 执行后 Session 状态可读 |
| `TestAutoRails_默认创建` | `test_deep_agent_auto_rails_creation_e2e` | `[TextResponse("自动 Rail")]` | 不传 Rails → DeepAgent 内部自动注册默认 Rail |

**说明**：
- 所有测试使用 `s.NewDeepAgentForTest(ctx, params)` 工厂创建
- 每个测试方法开头独立调用 `s.MockLLM.SetResponses(...)`，避免 Suite 共享状态
- 工具使用 `tool.NewMapFunction()` 创建简单的 mock 工具（如 `read_file` 返回固定内容）
- `TestInvoke_带Session` 验证 Session 通过 `agentinterfaces.WithSession(sess)` 传入的完整链路
- `TestAutoRails_默认创建` 验证 `CreateDeepAgent` 的第 10 步 `addDefaultRails` 自动生效

---

## 7. 模块 4：Rail 子系统

### 7a：TaskCompletionRail

对应 Python `tests/system_tests/harness/rail/test_task_completion_rail.py`

```go
// TaskCompletionRailSuite 嵌入 AgentSuite，测试 TaskCompletionRail 停止条件
type TaskCompletionRailSuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | MockLLM 响应 | 验证点 |
|---------|------------|-------------|--------|
| `TestMaxRounds_超限自动停止` | UC-1 MaxRounds | 3 次 ToolCall 循环 | 设置 MaxRounds=2 → 第 3 轮前自动终止 |
| `TestCompletionPromise_标签触发` | UC-2 CompletionPromise | `[ToolCall, TextResponse("<promise>任务完成</promise>")]` | AfterTaskIteration 检测 `<promise>` 标签 → 通知评估器 → 循环终止 |
| `TestTaskInstruction_模板注入` | UC-3 task_instruction | `[TextResponse("响应")]` | BeforeTaskIteration 在首次迭代注入任务指令到 prompt |
| `TestTimeout_超时终止` | UC-5 Timeout | 持续 ToolCall（不终止） | 设置 Timeout=2s → 超时后循环终止 |

**说明**：
- 对齐 Python `test_task_completion_rail.py` 的 5 个 UC，跳过 UC-4（CustomPredicateEvaluator，Go 端尚未实现）
- 使用 `rails.NewTaskCompletionRail(rails.WithMaxRounds(2))` 等选项配置
- MockLLM 通过循环返回 ToolCall 模拟"不停"的场景，验证 MaxRounds/Timeout 的强制终止能力
- Rail 通过 `CreateDeepAgentParams.Rails` 传入，或通过 `agent.AddRail()` 手动注册

### 7b：ConfirmInterruptRail

对应 Python `tests/system_tests/harness/rail/test_deep_agent_interrupt.py` + `test_deep_agent_tool_permission_interrupt.py`

```go
// ConfirmInterruptRailSuite 嵌入 AgentSuite，测试工具权限中断/恢复
type ConfirmInterruptRailSuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | MockLLM 响应 | 验证点 |
|---------|------------|-------------|--------|
| `TestInterrupt_工具调用被拦截` | `test_hitl_tool_permission_interrupt_read_file_ask` | `[ToolCall("write_file",...)]` | Rail 拦截 write_file → Agent 暂停等待用户输入 |
| `TestResume_用户确认后继续` | `test_deepagent_stream_interrupt_resume` | `[ToolCall("write_file",...), TextResponse("已写入")]` | 拦截 → 用户 Approve → Agent 恢复执行 → 工具实际执行 |

**说明**：
- ConfirmInterruptRail 通过 `interrupt.NewConfirmInterruptRail("write_file")` 创建
- `TestInterrupt_工具调用被拦截` 验证 Rail 在 BeforeToolCall 钩子中拦截指定工具
- `TestResume_用户确认后继续` 验证 InteractiveInput.update() → 恢复执行 → 工具被调用 → 最终文本返回
- 此测试依赖 Session.Interact() 机制，需配合 goroutine 模拟用户输入

---

## 8. 三批交付计划

### 第一交：基础层（模块 1 + 模块 3 + Suite 补全）

| 文件 | 内容 | 预计测试数 |
|------|------|----------|
| `suite/session_suite.go` | 补全 NewTestSession() | — |
| `agentcore/runner/runner_test.go` | RunnerLifeCycleSuite | 5 |
| `agentcore/session/doc.go` | 包文档 | — |
| `agentcore/session/session_test.go` | SessionLifeCycleSuite | 8 |

### 第二交：核心层（模块 5 + 模块 2 + Suite 补全）

| 文件 | 内容 | 预计测试数 |
|------|------|----------|
| `suite/agent_suite.go` | 补全 NewDeepAgentForTest() | — |
| `agentcore/harness/rails/tools/doc.go` | 工具注册包文档 | — |
| `agentcore/harness/rails/tools/tool_execution_test.go` | ToolExecutionSuite | 4 |
| `agentcore/harness/deep_agent_test.go` | DeepAgentE2ESuite | 6 |

### 第三交：扩展层（模块 4）

| 文件 | 内容 | 预计测试数 |
|------|------|----------|
| `agentcore/harness/rails/interrupt/task_completion_test.go` | TaskCompletionRailSuite | 4 |
| `agentcore/harness/rails/interrupt/confirm_rail_test.go` | ConfirmInterruptRailSuite | 2 |

---

## 9. 设计决策汇总

| 决策 | 选择 | 理由 |
|------|------|------|
| DeepAgent 创建方式 | 工厂模式（CreateDeepAgent） | 对齐 Python `create_deep_agent()`，测到真实注册/初始化流程 |
| Session 创建方式 | 按需创建（NewTestSession） | 对齐 Python 每个测试自己创建 session，不同用例用不同 session_id |
| Suite 共享状态 | 每个测试方法独立 SetResponses | 避免 testify suite 方法间响应队列干扰 |
| CustomPredicateEvaluator | 跳过 | Go 端尚未实现 |
| 交付方式 | 三批交付 | 基础层 → 核心层 → 扩展层 |
