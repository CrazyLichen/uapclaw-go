# ModelBackupRail（6.8a）设计

- 日期：2026-10-22
- 状态：已确认（方案 A：一比一复刻）
- Python 参考：`openjiuwen/core/single_agent/rail/model_backup.py`（19 行）
- Python 测试参考：`tests/system_tests/rail/test_model_backup_rail.py`（1 个测试）

## 1. 背景与动机

ModelBackupRail 是 Python 参考项目中存在、Go 实现计划中完全遗漏的能力组件（测试对齐
A 类缺口第 1 项，成本最低）。它提供模型级故障转移（failover）：主 LLM 因网络错误、
限流、服务不可用等原因抛异常时，按序切换备用模型链逐个重试，实现 LLM 高可用。

生产代码依赖全部就绪（6.4 事件枚举 / 6.7 AgentRail+BaseRail / 6.8 RailExecutor
重试循环 / 6.10 RequestRetry / 6.11 ModelCallRail 挂载 / ReActAgent.SetLLM 含
config 同步），只缺这一个类。

Python 生产代码中无任何业务方引用——它是用户可选装配的能力组件，通过
`register_rail` 挂载。因此 Go 端同样不挂到 DeepAdapter/工厂自动注册。

## 2. 在 agent 会话中的流程位置

```
用户 invoke()
  └─ ReAct 循环（每轮）
       ├─ Think 步骤: callModel
       │    └─ rail.ModelCallRail.Execute 包裹 railedModelCall（真实 LLM 调用）
       │         ├─ 正常 → AFTER_MODEL_CALL → 返回 AssistantMessage → Act/Observe...
       │         └─ LLM 调用抛异常 → fire(ON_MODEL_EXCEPTION)  ★ ModelBackupRail 挂载点
       │              ├─ set_llm(backupModels[index])   ← 热切换到备用模型
       │              ├─ index++                          ← 前进到下一备用
       │              └─ request_retry()                   ← 请求重试
       │         ↓ RailExecutor except 分支 consume_retry_request()
       │         有请求 → 吞掉异常，attempt+1，重新调用（此时已换备用模型）
       │         无请求（备用耗尽）→ 异常正常向上抛出
       └─ ...
```

与 TaskPlanningRail 的区别：TaskPlanning 是主动按任务规划选模型（BeforeModelCall），
ModelBackup 是被动故障兜底（OnModelException）。

## 3. 文件与类型设计

新文件 `internal/agentcore/single_agent/rail/model_backup.go`——目录对齐 Python
`core/single_agent/rail/`（核心层 Rail，非 harness 层）。该包现有 doc.go +
executor.go，导入方向：`rail → interfaces + foundation/llm`（foundation 不依赖
single_agent，无循环依赖，executor.go 已有 interfaces 导入先例）。

```go
// modelSwitcher 模型切换接口（Python: hasattr(ctx.agent, "set_llm") 的 Go 等价，
// 沿用 harness/rails/task_planning.go 同名接口先例）
type modelSwitcher interface {
    SetLLM(model *llm.Model)
    GetLLM() (*llm.Model, error)
}

// ModelBackupRail 模型备份护栏，LLM 调用异常时按序切换备用模型并请求重试。
type ModelBackupRail struct {
    agentinterfaces.BaseRail   // 值嵌入 BaseRail: *agentinterfaces.NewBaseRail()
    // backupModels 备用模型链，按序依次切换
    backupModels []*llm.Model  // Python: backup_models
    // index 下一个待切换的备用模型下标
    index        int           // Python: index
}

// NewModelBackupRail 创建模型备份护栏。
func NewModelBackupRail(backupModels []*llm.Model) *ModelBackupRail
```

- 优先级：默认 50（BaseRail 默认），一比一对齐 Python `AgentRail.priority = 50`，
  不覆盖
- `index` 无锁，注释标注对齐 Python（ReAct 循环内串行触发，同 task_planning 先例）
- 构造函数仅 1 个位置参数，不加 Functional Options（Python 构造签名一致，YAGNI）

## 4. 核心逻辑（一比一复刻）

```go
// OnModelException LLM 调用异常时切换到下一个备用模型并请求重试。
func (r *ModelBackupRail) OnModelException(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    // Python L16: if hasattr(ctx.agent, "set_llm") and self.index < len(self.backup_models)
    switcher, ok := cbc.Agent().(modelSwitcher)
    if !ok || r.index >= len(r.backupModels) {
        // agent 不支持切换或备用耗尽 → 不请求重试，异常正常上抛
        return nil
    }
    switcher.SetLLM(r.backupModels[r.index])
    r.index++
    cbc.RequestRetry(0) // Python: ctx.request_retry()
    return nil
}

// GetCallbacks 声明已覆盖的钩子映射。
func (r *ModelBackupRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
    return r.BuildCallbacks(
        r.CallbackFrom(agentinterfaces.CallbackOnModelException, func(ctx context.Context, railCtx any) error {
            return r.OnModelException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
        }),
    )
}
```

重试闭环（全链路已就绪，无需改动生产代码）：`react_model_call.go` 的
`rail.ModelCallRail.Execute` → 异常 → fire OnModelException → SetLLM 切换 +
RequestRetry → executor.go except 分支 consumeRetryRequest → attempt+1 重新执行 fn →
`railedModelCall` 重新 `getLLM()`（每次 attempt 重新取，已切到备用）→ 成功。
SetLLM 会同步 config 三字段（ModelNameVal/ModelClientConfig/ModelRequestConfig）并
重置 llmOnce（react_helpers.go 已实现），下次 getLLM 直接返回新实例。

## 5. 测试策略

### 5.1 单元测试 `rail/model_backup_test.go`（同包）

复用 task_planning_test.go 的 fakeModelSwitcherAgent 模式（编译时断言
`var _ modelSwitcher = (*fakeModelSwitcherAgent)(nil)`）：

1. 构造：backupModels 注入正确、index 初始 0、Priority 为 50
2. GetCallbacks：仅含 CallbackOnModelException 一条映射，wrap 类型断言可回调
3. OnModelException 切换：agent 实现 modelSwitcher → SetLLM 收到
   backupModels[0]、index 递增、ConsumeRetryRequest 返回非 nil（且一次性消费）
4. 连续异常依次切换 0→1（两次触发，index 递进）
5. agent 不实现 modelSwitcher（fakeBaseAgent）：无切换、无 retry 请求
6. 备用耗尽（index == len）：不切换、无 retry 请求
7. 空 backupModels：不切换、无 retry 请求

### 5.2 集成测试 `tests/integration/agentcore/single_agent/model_backup_rail_test.go`

对齐 Python `test_middleware_executes_when_react_agent_invoke` 的测试意图（主模型
失败 → failover 切换 → 重试成功输出）：

- 主模型：注册 fail provider（mockllm `AddErrorResponse`）构造 `*llm.Model`
- backup 模型：注册成功 provider（`AddTextResponse`）构造 `*llm.Model`，传入
  `NewModelBackupRail([]*llm.Model{backupModel})`
- 使用 suite.AgentSuite（提供 MockLLM 与工厂设施）+ `NewDeepAgentForTest`（内部
  经 `agents.NewReActAgent` 创建 ReActAgent 并挂 rails，DeepAgent invoke 链路
  最终走 react_model_call.go 的 ModelCallRail）→ RegisterRail(ModelBackupRail)
  → Invoke
- 断言：Invoke 成功、输出含 backup 响应文本、主模型 client 的 InvokeCallCount ≥ 1
- 跨子系统 ≥3：agents + single_agent/rail + single_agent/interfaces +
  foundation/llm + model_clients（满足规则 7 两层门禁）

**有意偏差（已确认）**：Python 测试的失败源是真实 HTTP 连接失败（mock_url 无法
解析），Go 集成测试用 mockllm 错误注入（AddErrorResponse）模拟——失败点在
client.Invoke 而非网络层。语义等价（都触发 ON_MODEL_EXCEPTION），符合 mockllm
既有设计（B 类 MCP 生命周期测试同模式）。

Python 测试中 `self.model_backup_middleware.backup_models[0] = mock_llm`（构造后
替换为 MockLLMModel）在 Go 中等价为：直接以 mock provider 构造的 `*llm.Model` 传入
构造函数（mockllm 支持注册 ClientRegistry 后 llm.NewModel 关联，无需事后替换）。
主模型的注入方式对齐 planning 集成测试先例：DeepAgent 经
`CreateDeepAgentParams.Model`（或 config 的 Model 字段）注入预构建 fail Model，
backup Model 经 rail 构造函数传入。

## 6. 回填清单

1. IMPLEMENTATION_PLAN.md 领域六 6.x Rail 系统分组新增 `6.8a` 行（插在 6.8 之后）：
   状态 ✅ + 产出描述 + Python 路径 `openjiuwen/core/single_agent/rail/model_backup.py`
2. `single_agent/rail/doc.go` 文件目录补 `model_backup.go` 条目 + 核心类型索引补
   ModelBackupRail
3. 记忆 `agentcore-test-alignment-gaps.md`：A 类第 1 项标记完成
4. 集成测试完成标准：推送远端后 CI 四 job 全绿（规则 6）

## 7. 不做的事（YAGNI）

- 不加 Functional Options（Python 构造仅 1 个位置参数）
- 不做并发安全 index（Python 也无锁，ReAct 循环串行语义）
- 不挂到 DeepAdapter/工厂自动注册（Python 生产代码无任何引用，纯用户可选装配）
- 不复刻 Python 测试的 AgentCard/LocalFunction 全套夹具（Go 集成测试复用现有
  suite 设施，只保留测试意图）

## 8. 验收标准

- `go test ./internal/agentcore/single_agent/rail/...` 通过，包覆盖率 ≥ 85%
- 集成测试 `go test -tags=integration ./tests/integration/agentcore/single_agent/`
  通过（含 failover 主链路断言）
- `cmd/integration-audit -verify` 对新目录判定通过（两层门禁）
- IMPLEMENTATION_PLAN.md / doc.go / 记忆三处回填完成
- 远端 CI 全绿
