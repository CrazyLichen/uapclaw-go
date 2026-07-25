# 48小时代码逻辑审查报告

> 审查日期：2026-07-21
> 审查范围：48小时内提交的功能实现
> 对比基准：Python 参考项目源码

## 审查章节概览

| 章节 | 内容 | 状态 |
|------|------|------|
| 9.72b | ToolOptimizer 工具描述优化器 | ✅ 已完成 |
| 9.59b | Interaction 层（payload/router/inbox/runtime） | ✅ 已完成 |
| 9.27-9.30 | CodeAgent/PlanAgent/VerificationAgent/ExploreAgent | ✅ 已完成 |
| 9.72a | InstructionOptimizer 指令优化器 | ✅ 已完成 |
| 10.3.12 | AgentManager 多实例管理 | ✅ 已完成 |
| 9.73 | SignalDetector 信号检测 + 审计优化 | ✅ 已完成 |

## 问题统计

| 严重程度 | 数量 |
|---------|------|
| 严重 | 15 |
| 一般 | 16 |
| 提示 | 10 |

---

## 一、9.72b ToolOptimizer（6 严重 / 3 一般 / 2 提示）

### S-01：[严重] SimpleAPIWrapperFromCallable.Call 返回值格式与 Python 不一致

**Python 参考代码** (`customized_api.py:115-148`):
```python
class SimpleAPIWrapperFromCallable:
    def __call__(self, tool, tool_input):
        fn = self.functions.get(self.fn_call_name)
        if fn is None:
            return json.dumps({"error": f"request invalid, no function '{tool_name}' found", "response": ""}), 12
        try:
            output = fn(params)
            return json.dumps({'response': output}, ensure_ascii=False), 0
        except Exception as e:
            return json.dumps({"error": f"request invalid, error: {str(e)}", "response": ""}), 12
```

**Go 问题代码** (`api_wrapper.go:82-88`):
```go
output, err := w.callable(tool, toolInput)
if err != 0 {
    return output, err
}
return output, 0
```

**问题分析**：
1. Python 的 `tool_callable` 只接受 `params`（dict），Go 的 `APIWrapperFunc` 多传了 `tool` 参数
2. Python 中 callable 返回原始数据，由 wrapper 统一做 JSON 序列化；Go 的 callable 直接返回 `(string, int)`，已自行序列化
3. Go 的 Call 方法没有将结果包装为 `{'response': output}` 格式，下游解析可能出错

**修复方案**：让 `APIWrapperFunc` 返回原始数据（`any`），由 wrapper 统一包装为 `{"response": output}` 或 `{"error": ..., "response": ""}` 格式。确保所有 callable 实现都遵循 Python 的包装协议。

---

### S-02：[严重] SimpleAPIWrapperFromCallable.Call 缺少 panic 恢复，无法对齐 Python try/except

**Python 参考代码** (`customized_api.py:140-148`):
```python
try:
    output = fn(params)
    return json.dumps({'response': output}, ensure_ascii=False), 0
except Exception as e:
    logger.error(f"request invalid, error: {str(e)}")
    return json.dumps({"error": f"request invalid, error: {str(e)}", "response": ""}), 12
```

**Go 问题代码**：同 S-01，`Call` 方法中没有 `defer recover()` 机制。

**问题分析**：Python 的 `try/except` 保护了整个 callable 调用，确保任何异常都返回 `status_code=12` 的 JSON。Go 中如果 callable 函数 panic，整个程序会崩溃。

**修复方案**：
```go
func (w *SimpleAPIWrapperFromCallable) Call(tool map[string]any, toolInput map[string]any) (string, int) {
    defer func() {
        if r := recover(); r != nil {
            // 对齐 Python except: 返回 error JSON + status_code=12
        }
    }()
    // 正常逻辑...
}
```

---

### S-03：[严重] resultExamples 用 extend 而非 append，嵌套结构与 Python 不同

**Python 参考代码** (`base.py:63-69`):
```python
result_example = customized_pipeline("example", tool, ...)
result_examples.append(result_example)  # 追加整个列表，形成嵌套结构
```

**Go 问题代码** (`base.go:174`):
```go
resultExamples = append(resultExamples, resultExample...)  // 展开追加，扁平结构
```

**问题分析**：Python 的 `result_examples` 是 `[][][]any`（嵌套），Go 的 `resultExamples` 是 `[][]any`（扁平）。相当于 Python 的 `append` vs Go 的 `extend`。如果后续需要按轮次访问 example 结果，Go 的扁平结构会导致无法区分不同轮次。

**修复方案**：
```go
// 改为保持嵌套结构
resultExamples = append(resultExamples, resultExample)  // 不展开
// 同时将 resultExamples 类型改为 [][][]any
```

---

### S-04：[严重] APICallToExampleMethod.Step 拒绝采样循环后 fnCall 可能为 nil

**Python 参考代码** (`toolcall_example_method.py:48-73`):
```python
for _ in range(self.config['num_init_loop']):
    fn_call = self.generate_api_call_from_description(tool_for_opt, num_gen=1, prev_output=prev_outputs)
    tool_res, status_code = self.run_tool_with_api_call(tool_for_opt, fn_call)
    if api_analysis['err_code'] == -1:
        continue
    break
```

**Go 问题代码** (`example_method.go:110-164`):
```go
var fnCall map[string]any
var toolRes string
for i := 0; i < numInitLoop; i++ {
    fnCall, err = m.GenerateAPICallFromDescription(ctx, toolForOpt, nil, 1, prevOutputsCopy)
    if err != nil {
        continue  // fnCall 保持 nil
    }
    // ...
}
// 后续直接使用 fnCall，可能为 nil → panic
```

**问题分析**：如果所有重试都失败，`fnCall` 为 `nil`，后续 `GenerateInstructionFromAPICall(ctx, toolForOpt, fnCall, ...)` 会导致 nil map 访问 panic。Python 中 `tenacity` 重试耗尽后会 reraise 异常，不会继续执行。

**修复方案**：在循环结束后检查 `fnCall` 是否为 nil：
```go
if fnCall == nil {
    return nil, "", 0, fmt.Errorf("generate_api_call failed after %d attempts", numInitLoop)
}
```

---

### S-05：[严重] OptimizeTool 中 ori_tool 传参与 Python 变量名语义不一致

**Python 参考代码** (`base.py:63-95`):
```python
original_desc = tool["description"]  # 保存原始
for i in range(self.max_turns):
    if i > 0:
        tool["description"] = latest_description  # 修改了 tool["description"]
    # ...
# 最终审查
processed = processor.process(data=output_desc, ori_tool=tool["description"])
# ⚠️ 此时 tool["description"] 是最后一轮的描述，不是 original_desc
```

**Go 问题代码** (`base.go:221-222`):
```go
oriToolDesc, _ := tool["description"].(string)
processed, err := processor.Process(ctx, dataForProcess, oriToolDesc, ...)
```

**问题分析**：Go 实现与 Python **行为一致**（都取 `tool["description"]`，不是 `originalDesc`），但 Python 变量名叫 `ori_tool` 暗示应用原始描述。这可能是 Python 本身的 bug——循环结束后 `tool["description"]` 已被修改为最后一轮的结果。

**修复方案**：当前 Go 行为与 Python 一致，暂无需修改。建议添加注释标注 Python 此处可能为 bug，如果 Python 后续修正确保同步更新。

---

### S-06：[严重] OptimizeTool 中 original_desc 不是 JSON 时 ExtractSchemaFromJSON 返回空 map

**Python 参考代码** (`base.py:89`):
```python
schema = extract_schema(original_desc)  # 如果不是 JSON，json.loads 失败，返回 {}
```

**Go 问题代码** (`base.go:211`):
```go
schema := ExtractSchemaFromJSON(originalDesc)  # 同样返回空 map
```

**问题分析**：`tool["description"]` 可能是纯文本描述，不是 JSON。Python 和 Go 都在 JSON 解析失败时返回空 dict/map。后续 `processor.Format(schema, ...)` 会将空 `{}` 作为 JSON 模板传给 LLM，导致 LLM 无法理解目标格式。行为与 Python 一致，但此逻辑本身可能不合理。

**修复方案**：暂无需修改（与 Python 行为一致）。建议作为优化项：当 `original_desc` 不是 JSON 时，从 `tool["parameters"]` 构造 schema 作为替代。

---

### N-01：[一般] GenerateInstructionFromAPICall 中 prevOutput 类型断言可能失败

**Python 参考代码** (`toolcall_example_method.py:371-376`):
```python
for i, (inst, score) in enumerate(
    zip(prev_output["instructions"], prev_output["scores"]), 1,
):
    formatted_lines.append(f'{i}. instruction="{inst}" score={score}')
```

**Go 问题代码** (`example_method.go:607-614`):
```go
if instructions, ok := prevOutput["instructions"].([]string); ok {
    if scoreVals, ok := prevOutput["scores"].([]float64); ok {
```

**问题分析**：JSON 反序列化后 `prevOutput["instructions"]` 实际类型可能是 `[]any`，不是 `[]string`。`[]any` 无法直接断言为 `[]string`，导致 `ok == false`，格式化代码完全跳过。

**修复方案**：先尝试 `[]string`，失败后尝试 `[]any` 再逐一转换：
```go
var instructions []string
switch v := prevOutput["instructions"].(type) {
case []string:
    instructions = v
case []any:
    for _, item := range v {
        if s, ok := item.(string); ok {
            instructions = append(instructions, s)
        }
    }
}
```

---

### N-02：[一般] BeamSearch 正常路径过滤 depth=0 节点，Python 不过滤

**Python 参考代码** (`beam_search.py:93-94`):
```python
nodes_sorted = sorted(best_nodes, reverse=True, key=lambda x: x.score)[:self.top_k]
return [node.history for node in nodes_sorted]  # 可能包含根节点
```

**Go 代码** (`beam_search.go:247-252`):
```go
filtered := make([]*TreeNode, 0, len(bestNodes))
for _, node := range bestNodes {
    if node.GetDepth() > 0 {
        filtered = append(filtered, node)
    }
}
```

**问题分析**：Go 的正常路径过滤了 depth=0 的根节点，Python 不过滤。Go 行为更安全（避免返回不含 `description` 键的根节点导致 KeyError），但与 Python 行为不一致。

**修复方案**：Go 当前行为更安全，保持不变。Python 的实现可能有 bug（根节点 score 最高时会包含无用数据），建议标注为有意改进。

---

### N-03：[一般] copyMap 浅拷贝与 deepCopyMap 不一致

**Go 问题代码** (`eval.go:631-639`):
```go
func copyMap(m map[string]any) map[string]any {
    result := make(map[string]any, len(m))
    for k, v := range m {
        result[k] = v  // 浅拷贝，嵌套 map 共享引用
    }
    return result
}
```

**问题分析**：项目中 `example_method.go:94` 使用了 `deepCopyMap`（真正的深拷贝），但 `base.go` 中 `NewToolOptimizerBase` 使用 `copyMap(DefaultConfigEg)`。如果 config 中包含嵌套 map，浅拷贝会导致共享引用问题。

**修复方案**：统一使用 `deepCopyMap`，或将 `copyMap` 改为深拷贝实现。

---

### T-01：[提示] isMostlyEnglish 按字节计算而非字符数

**Python 参考代码** (`customized_reviewer.py:116-127`):
```python
english_ratio = english_chars / len(text_no_space)  # len() 返回字符数
```

**Go 问题代码** (`reviewer.go:348-365`):
```go
englishRatio := float64(englishChars) / float64(len(textNoSpace))  // len() 返回字节数
```

**问题分析**：Go 的 `len()` 返回字节数，中文字符占 3 字节（UTF-8），导致 `englishRatio` 被低估，更多文本被误判为"非英文"从而跳过翻译。

**修复方案**：使用 `utf8.RuneCountInString(textNoSpace)` 替代 `len(textNoSpace)`。

---

### T-02：[提示] descDefaultPolicy 和 descDefaultPolicy15 完全相同

**Go 问题代码** (`description_method.go:681-698`):
```go
func descDefaultPolicy() llm_resilience.LLMInvokePolicy {
    return llm_resilience.LLMInvokePolicy{MaxAttempts: 15, TotalBudgetSecs: 300, ...}
}
func descDefaultPolicy15() llm_resilience.LLMInvokePolicy {
    return llm_resilience.LLMInvokePolicy{MaxAttempts: 15, TotalBudgetSecs: 300, ...}  // 完全相同
}
```

**修复方案**：合并为一个 `descDefaultPolicy` 函数，删除 `descDefaultPolicy15`。

---

## 二、9.59b Interaction 层（4 严重 / 4 一般 / 3 提示）

### S-07：[严重] InteractGate.ConsumeDone 可能重复 close channel 导致 panic

**Python 参考代码** (`gate.py`):
```python
async def consume_done(self, ticket: AdmissionTicket) -> None:
    async with self._lock:
        self._inflight -= 1
        if self._inflight == 0:
            self._drained.set()   # asyncio.Event.set() 是幂等的，重复调用安全
```

**Go 问题代码** (`gate.go:142-144`):
```go
func (g *InteractGate) ConsumeDone(ticket *AdmissionTicket) {
    g.inflight--
    if g.inflight == 0 {
        close(g.drained)   // ← 如果 drained 已关闭会 panic!
    }
}
```

**问题分析**：Python 的 `asyncio.Event.set()` 是幂等的，Go 的 `close(chan)` 不是。如果 `ConsumeDone` 被重复调用，或 `CloseAndDrain` 已关闭 `drained` 通道后再调用 `ConsumeDone`，就会 panic。

**修复方案**：
```go
if g.inflight == 0 {
    select {
    case <-g.drained:
        // 已关闭，无需再次 close
    default:
        close(g.drained)
    }
}
```

---

### S-08：[严重] HumanAgentInbox.resolveSender 硬编码 names，屏蔽了错误路径

**Python 参考代码** (`human_agent_inbox.py`):
```python
def _resolve_sender(self, sender: Optional[str]) -> str:
    names = self._team.human_agent_names()
    if not names:
        raise HumanAgentNotEnabledError(...)
```

**Go 问题代码** (`human_agent_inbox.go:164-165`):
```go
// ⤵️ 待 9.55 回填: names := h.team.HumanAgentNames()
names := []string{agentteams.HumanAgentMemberName}
```

**问题分析**：`names` 硬编码为 `["human_agent"]`，导致：
1. `HumanAgentNotEnabledError` 永远不会触发（`len(names) == 1`）
2. 如果团队没有注册 human-agent 成员，`resolveSender` 仍返回 `"human_agent"`
3. `UnknownHumanAgentError` 也无法触发

**修复方案**：9.55 回填时必须改为调用 `h.team.(TeamBackend).HumanAgentNames()`。当前 stub 应返回空切片 `[]string{}` 而非硬编码值，让错误路径能被测试到。

---

### S-09：[严重] TeamRuntimeManager.dispatchPayload 缺少 auto_start 调用

**Python 参考代码** (`manager.py`):
```python
if isinstance(payload, OperatorMessage):
    inbox = UserInbox(backend.message_manager)
    if payload.target is None:
        await agent.auto_start_all()       # ← 先启动所有成员
        result = await inbox.broadcast(payload.body)
        return result
    await agent.auto_start_member(payload.target)  # ← 先启动目标成员
    result = await inbox.direct(payload.target, payload.body)
    return result
```

**Go 问题代码** (`manager.go:259-277`):
```go
case *interaction.OperatorMessage:
    inbox := interaction.NewUserInbox(nil) // ⤵️ 待 9.55 回填
    if p.Target() == nil {
        // ⤵️ 待 9.55 回填: agent.AutoStartAll()
        return inbox.Broadcast(p.Body())
    }
    // ⤵️ 待 9.55 回填: agent.AutoStartMember(*p.Target())
    return inbox.Direct(*p.Target(), p.Body())
```

**问题分析**：Python 中 `auto_start_all()` 和 `auto_start_member()` 在消息分发之前调用，确保目标成员已订阅事件总线。Go 的 stub 跳过了这些步骤，消息可能发到未订阅的成员。

**修复方案**：回填 9.55 时必须按 Python 顺序实现：先 auto_start，再发送消息。

---

### S-10：[严重] GodViewMessage/OperatorMessage/HumanAgentMessage 字段未导出

**Python 参考代码**：
```python
@dataclass(frozen=True, slots=True)
class GodViewMessage:
    body: str          # 可直接访问
class OperatorMessage:
    body: str          # 可直接访问
    target: Optional[str] = None  # 可直接访问
```

**Go 问题代码** (`payload.go:33-62`):
```go
type GodViewMessage struct {
    body string       // 小写未导出
}
type OperatorMessage struct {
    body string       // 小写未导出
    target *string    // 小写未导出
}
```

**问题分析**：所有字段小写未导出，外部包只能通过 getter（Body()/Target()/Sender()）访问。但 `DeliverResult` 和 `HumanAgentInboundEvent` 的字段是大写导出的，风格不一致。

**修复方案**：统一风格——将 payload 结构体字段也改为导出（`Body`/`Target`/`Sender`），同时保留 getter 方法做兼容。

---

### N-04：[一般] UserInbox 日志消息未一比一复刻 Python 字符串

**Python 参考代码**：
```python
team_logger.debug("UserInbox: delivering input to leader DeepAgent")
```

**Go 问题代码**：
```go
logger.Debug(inboxLogComponent).Msg("DeliverToLeader")
```

**修复方案**：按项目规则 3（日志同步规则），改为 `Msg("UserInbox: delivering input to leader DeepAgent")`。

---

### N-05：[一般] HumanAgentInbox.Send 广播 stub 不模拟 broadcast_failed

**Python 参考代码**：
```python
msg_id = await self._mm.broadcast_message(content=body, from_member_name=resolved_sender)
if msg_id is None:
    return DeliverResult.failure("broadcast_failed")
```

**Go 问题代码**：
```go
// ⤵️ 待 9.55 回填: msgID := h.messageManager.BroadcastMessage(body, resolvedSender)
msgID := "stub-ha-broadcast-msg-id"  // 直接返回成功
```

**修复方案**：回填时需对齐 Python 的 `msg_id is None → failure("broadcast_failed")` 逻辑。

---

### N-06：[一般] HumanAgentInbox.driveAgent stub 跳过 agent.deliver_input(body)

**Python 参考代码**：
```python
async def _drive_agent(self, body: str, *, sender: str) -> DeliverResult:
    agent = self._agent_lookup(sender)
    await agent.deliver_input(body)        # ← 关键步骤
    return DeliverResult.success(None)
```

**Go 问题代码**：
```go
agent := h.agentLookup(sender)
// ⤵️ 待 9.55 回填: agent.(*TeamAgent).DeliverInput(ctx, body)
return NewDeliverResultSuccess(nil), nil  // 跳过 deliver_input
```

**修复方案**：回填 9.55 时必须实现 `agent.(*TeamAgent).DeliverInput(ctx, body)` 并处理 error。

---

### N-07：[一般] HumanAgentInbox 错误路径无法被测试

**问题分析**：因 `resolveSender` 硬编码了 `names`，`HumanAgentNotEnabledError` 和 `UnknownHumanAgentError` 永远无法触发，测试无法覆盖这些错误路径。

**修复方案**：随 S-08 修复后，测试中 mock team 的 `HumanAgentNames()` 返回值即可覆盖。

---

### T-03：[提示] 缺少 `#hashtag` 边界测试用例

**Python 注释明确说明**：
```python
# `#hashtag` and `$variable` are content, not channel markers, because they lack
# the trailing space the grammar requires.
```

**修复方案**：在 `router_test.go` 添加 `#hashtag` 和 `$variable` 的边界测试。

---

### T-04：[提示] 缺少 `$name@member body` 格式的测试

**修复方案**：在 `router_test.go` 添加 `TestParseInteractStr_美元前缀at无空格` 测试用例。

---

### T-05：[提示] TeamRuntimeActivation/RunAction 等类型未定义

**问题分析**：缺失的类型待 9.62 回填，属于预期缺失。

---

## 三、9.27-9.30 子 Agent 工厂函数（2 严重 / 2 一般 / 3 提示）

### S-11：[严重] 缺少 CreateVerificationAgent 工厂函数

**Python 参考代码** (`verification_agent.py:318-363`):
```python
def create_verification_agent(*, ..., rails=None, ...) -> DeepAgent:
    """VerificationAgent 的完整实例化入口"""
```

**Go 问题**：没有 `verification_agent_factory.go` 文件。`BuildVerificationAgentConfig` 只构建配置，但没有对应的实例化路径，VerificationAgent 无法通过工厂函数创建 DeepAgent 实例。

**修复方案**：新建 `internal/agentcore/harness/subagents/verification_agent_factory.go`，实现 `CreateVerificationAgent` 函数，对齐 Python `create_verification_agent()` 逻辑：
- 默认 Rails: `params.Rails == nil` 时注入 `[SysOperationRail(), VerificationRail()]`
- MaxIterations 默认 40
- RestrictToWorkDir 默认 false
- 不设 FactoryName

---

### S-12：[严重] VerificationRail.BeforeModelCall 缺少条件判断

**Python 参考代码** (`verification_rail.py:125-163`):
```python
async def before_model_call(self, ctx):
    if self.system_prompt_builder is None:
        return
    deep_config = getattr(self._agent, "_deep_config", None)
    if deep_config is None or not deep_config.enable_task_loop:
        return                              # ← 非任务循环时跳过
    if ctx.session is not None:
        state = self._agent.load_state(ctx.session)
        if getattr(state.plan_mode, "mode", None) == "plan":
            return                          # ← plan 模式下跳过
```

**Go 问题代码** (`verification_rail.go:167-192`):
```go
func (r *VerificationRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    if r.promptBuilder == nil {
        return nil
    }
    // 直接注入，没有检查 enable_task_loop 和 plan_mode
    section := saprompt.PromptSection{...}
    r.promptBuilder.AddSection(section)
    return nil
}
```

**问题分析**：在非 task loop 场景和 plan 模式下，也会注入约束提醒，产生噪声提示词，干扰代理行为。Python 明确跳过这两种场景。

**修复方案**：在 `BeforeModelCall` 中增加条件判断：
1. 检查 agent 的 `enable_task_loop` 配置，未启用时跳过
2. 检查当前是否处于 plan 模式，plan 模式下跳过

---

### N-08：[一般] BuildPlanAgentConfig 和 BuildExploreAgentConfig 缺少默认 Rails 注入

**Python 参考代码** (`plan_agent.py:119`):
```python
rails=rails if rails is not None else [SysOperationRail()]
```

**Python 参考代码** (`explore_agent.py:192`):
```python
rails=rails if rails is not None else [SysOperationRail(read_only=True)]
```

**Go 问题代码**：`BuildPlanAgentConfig` 和 `BuildExploreAgentConfig` 在 `params.Rails == nil` 时**不注入默认 Rails**，与 Python 不一致。

**修复方案**：
- `BuildPlanAgentConfig`: `params.Rails == nil` 时注入 `[SysOperationRail()]`
- `BuildExploreAgentConfig`: `params.Rails == nil` 时注入 `[SysOperationRail(WithReadOnly(true))]`

---

### N-09：[一般] ExploreAgent 和 PlanAgent 的 SysOperationRail read_only 与 Python 不一致

**问题分析**：Python `create_explore_agent` 中 `SysOperationRail()` 无 read_only，Go 统一用 `WithReadOnly(true)`。Python `create_plan_agent` 也是无 read_only，Go 用了 `WithReadOnly(true)`。这是功能增强（双重只读保障），因为 PlanAgent 的提示词明确要求"只读模式，禁止任何文件修改"。

**修复方案**：保持 Go 当前行为（更安全），加注释说明 Python build/create 的不一致。

---

### T-06：[提示] ExploreAgent 缺少 search_via_bash 参数

**Python 参考代码** (`explore_agent.py:174, L222`):
```python
def build_explore_agent_config(*, ..., search_via_bash: bool = False) -> SubAgentConfig:
def create_explore_agent(*, ..., search_via_bash: bool = False, ...) -> DeepAgent:
```

**修复方案**：在 `SubagentCreateParams` 中添加 `SearchViaBash` 字段，并在工厂函数中处理。

---

### T-07：[提示] CodeAgent EN 描述字符串换行/缩进差异

**Python**:
```python
DEFAULT_CODE_AGENT_DESCRIPTION_EN = """You are a senior software engineer and coding agent,
    excel at translating tasks into runnable code and verifiable results."""
```

**Go**:
```go
"en": "You are a senior software engineer and coding agent, " +
    "excel at translating tasks into runnable code and verifiable results.",
```

**修复方案**：根据项目反馈记忆"提示词一比一复刻 Python 原文"，Go 应包含 `\n    excel at...`。

---

### T-08：[提示] VerificationAgent 描述回退语言应为 "en" 非 "cn"

**Python 参考代码** (`verification_agent.py:298`):
```python
description=VERIFICATION_AGENT_DESC.get(resolved_language, VERIFICATION_AGENT_DESC["en"]),
```

**Go 问题代码** (`verification_agent.go:237-238`):
```go
desc := defaultVerificationAgentDescription[language]
if desc == "" {
    desc = defaultVerificationAgentDescription["cn"]  // 应为 "en"
}
```

**修复方案**：将回退语言改为 `"en"` 以对齐 Python。

---

## 四、9.72a InstructionOptimizer（1 严重 / 3 一般 / 1 提示）

### S-13：[严重] restorePlaceholders 失败后仍返回不完整结果，应回退为空字符串

**Python 参考代码** (`instruction_optimizer.py:159-170`):
```python
sys_prompt = await self._restore_placeholders(...) if sys_prompt else None
# None 在 _step() 中通过 if sys_val: 判断会被跳过（不写入 updates）
```

**Go 问题代码** (`instruction_optimizer.go:408-426`):
```go
if sysPrompt != "" {
    sysPrompt, err = o.restorePlaceholders(ctx, ...)
    if err != nil {
        logger.Warn(logComponent)...Msg("[optimizer] 恢复 system_prompt 占位符失败")
        // sysPrompt 仍为可能不完整的值，会被写入 updates
    }
}
```

**问题分析**：Go 中 `restorePlaceholders` 失败时，`sysPrompt` 可能包含不完整的占位符，但仍会被当作优化结果使用。Python 中 `None` 值在 `_step()` 中 `if sys_val:` 判断为 falsy，不会被写入 updates。

**修复方案**：
```go
if sysPrompt != "" {
    restored, err := o.restorePlaceholders(ctx, ...)
    if err != nil {
        logger.Warn(logComponent)...Msg("[optimizer] 恢复 system_prompt 占位符失败")
        sysPrompt = ""  // 对齐 Python: 失败则 None → 不写入 updates
    } else {
        sysPrompt = restored
    }
}
```

---

### N-10：[一般] Step() 缺少异常捕获 + 清轨迹保障

**Python 参考代码** (`base.py:129-140`):
```python
def step(self) -> Dict[tuple[str, str], Any]:
    try:
        updates = self._step()
        self.clear_trajectories()
        return updates or {}
    except Exception as e:
        self.clear_trajectories()  # ← 异常时也清轨迹
        raise build_error(StatusCode.TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR, ...) from e
```

**Go 问题代码** (`instruction_optimizer.go:158-163`):
```go
func (o *InstructionOptimizer) Step() map[schema.UpdateKey]any {
    o.ValidateParameters()
    updates := o.step()
    o.ClearTrajectories()
    return updates
}
```

**修复方案**：用 `defer` + `recover` 对齐 Python 的两层保障。

---

### N-11：[一般] backward 中错误后 continue vs Python 直接抛出

**Python 参考代码**：`_backward` 中任何 LLM 调用失败直接异常传播，导致整个 backward 终止。

**Go 问题代码** (`instruction_optimizer.go:201-209`):
```go
gradient, err := o.generateTextualGradient(ctx, op)
if err != nil {
    logger.Error(logComponent)...Msg("[optimizer] 生成文本梯度失败")
    continue  // 跳过此 operator，继续处理下一个
}
```

**问题分析**：Go 选择 `continue` 是比 Python 更宽容的行为。如果要严格对齐 Python，应 `return err`。

**修复方案**：加注释说明这是有意偏离 Python 的行为（更宽容的错误处理）。

---

### N-12：[一般] formatBadCases 非字符串 content 被跳过

**Python 参考代码** (`instruction_optimizer.py:206-211`):
```python
content = formatted.content
if isinstance(content, str):
    parts.append(content)
elif content:
    parts.append(str(content))  # ← 非 string 也处理
```

**Go 问题代码** (`instruction_optimizer.go:534-536`):
```go
if s, ok := formatted.Content.(string); ok {
    parts = append(parts, s)
}
// 非 string content 直接跳过
```

**修复方案**：
```go
if s, ok := formatted.Content.(string); ok {
    parts = append(parts, s)
} else if formatted.Content != nil {
    parts = append(parts, fmt.Sprintf("%v", formatted.Content))
}
```

---

### T-09：[提示] extractTag 每次编译正则

**修复方案**：可预编译常见 tag 的正则，或使用 `sync.Once` 缓存。非热路径，优先级低。

---

## 五、10.3.12 AgentManager（2 严重 / 2 一般 / 2 提示）

### S-14：[严重] CancelAllInflightWork 缺少 reason 参数透传

**Python 参考代码** (`agent_manager.py:184-191`):
```python
async def cancel_all_inflight_work(self, reason: str = "[gateway ws disconnect] ") -> None:
    for modes in list(self.agents.values()):
        for agent in list(modes.values()):
            await agent.cancel_inflight_work(reason)  # ← 透传 reason
```

**Go 问题代码** (`agent_manager.go:481-496`):
```go
func (am *AgentManager) CancelAllInflightWork(ctx context.Context) error {
    for _, agent := range agentsCopy {
        _ = agent.CancelInflightWork()  // 无 reason 参数
    }
    return nil
}
```

**问题分析**：
1. `CancelAllInflightWork` 没有 `reason` 参数
2. `UapClaw.CancelInflightWork()` 内部硬编码 `"[gateway disconnect]"`，Python 默认是 `"[gateway ws disconnect] "`

**修复方案**：
1. `CancelAllInflightWork` 增加 `reason string` 参数
2. `UapClaw.CancelInflightWork` 增加 `reason string` 参数
3. 默认 reason 改为 `"[gateway ws disconnect] "`，对齐 Python

---

### S-15：[严重] GetAgentNoWait 字段遍历中 normalize 逻辑导致空参数时行为不一致

**Python 参考代码** (`agent_manager.py:283-306`):
```python
requested_mode = _normalize_mode(mode) if mode is not None else ""
# mode=None → requested_mode="" → if requested_mode and ... 为 False（跳过 mode 过滤）
```

**Go 问题代码** (`agent_manager.go:186-224`):
```go
requestedMode := normalizeMode(mode)  // mode="" → normalizeMode("")="agent"
// if requestedMode != "" && ... 为 True（会过滤 mode=="agent"）
```

**问题分析**：Python 中 `mode=None` 时跳过 mode 过滤；Go 中 `mode=""` 时 `normalizeMode("")` 返回 `"agent"`，反而会过滤 mode=="agent" 的条目。语义不一致。

**修复方案**：区分"参数未提供"和"参数为空"：
```go
requestedMode := ""
if mode != "" {
    requestedMode = normalizeMode(mode)
}
requestedSubMode := ""
if subMode != "" {
    requestedSubMode = normalizeSubMode(subMode)
}
requestedProjectDir := ""
if projectDir != "" {
    requestedProjectDir = normalizeProjectDir(projectDir)
}
```

---

### N-13：[一般] GetClientCapabilities channelKey 规范化与写入时不一致

**Python 参考代码** (`agent_manager.py:193-196`):
```python
channel_key = str(channel_id or "").strip()
```

**Go 问题**：`Initialize` 写入时 key 是 `normalizeChannelID(channelID)`（空→"default"），`GetClientCapabilities` 读取时 key 是 `strings.TrimSpace(channelID)`（空→空）。两者不匹配，可能导致找不到已写入的 capabilities。

**修复方案**：`GetClientCapabilities` 也使用 `normalizeChannelID` 保持一致。

---

### N-14：[一般] envOverrides 中空字符串处理与 Python 不一致

**Python 参考代码** (`agent_manager.py:308-316`):
```python
if env_value is None:
    os.environ.pop(key, None)
else:
    os.environ[key] = str(env_value)  # 空字符串也 Setenv
```

**Go 问题代码** (`agent_manager.go:350-360`):
```go
if val == nil || s == "" {
    _ = os.Unsetenv(key)  // 空字符串也 Unsetenv，与 Python 不一致
}
```

**修复方案**：只对 `val == nil` 执行 Unsetenv，空字符串仍然 Setenv。

---

### T-10：[提示] ACP 相关功能完全占位

**问题分析**：所有 ACP 相关逻辑都用 `⤵️ ACP` 占位符标注，确认是计划延后，非遗漏。

---

## 六、9.73 SignalDetector（3 严重 / 2 一般 / 3 提示）

### S-16：[严重] Detect 方法不支持 Trajectory 输入

**Python 参考代码** (`signal.py`):
```python
def detect(self, trajectory_or_messages: Union[Trajectory, List[dict]]) -> List[EvolutionSignal]:
    if isinstance(trajectory_or_messages, Trajectory):
        messages = self.convert_trajectory_to_messages(trajectory_or_messages)
        signals.extend(self._detect_from_messages(messages))
        signals.extend(self._detect_collaboration_signals(trajectory_or_messages))
    else:
        signals.extend(self._detect_from_messages(trajectory_or_messages))
    return self._deduplicate(signals)
```

**Go 问题代码** (`from_conv.go:228`):
```go
func (d *ConversationSignalDetector) Detect(msgs []map[string]any) []*EvolutionSignal {
    signals := d.detectFromMessages(msgs)
    return d.deduplicate(signals)
}
```

**问题分析**：Python 的 `detect()` 接受 `Union[Trajectory, List[dict]]`，能处理 Trajectory 输入并额外检测协作信号。Go 版本只接受 `[]map[string]any`，完全丢失了 Trajectory 路径和协作信号检测。

**修复方案**：增加 `DetectFromTrajectory(traj *trajectory.Trajectory)` 方法，或在 Detect 中用 option 区分输入类型。同时实现 `convertTrajectoryToMessages` 和 `_detectCollaborationSignals`。

---

### S-17：[严重] DetectUserIntent 中 is_feedback 判断逻辑不一致

**Python 参考代码**：
```python
if not parsed.get("is_feedback"):
    return []  # key 不存在时返回空
```

**Go 问题代码** (`from_conv.go:341-353`):
```go
if _, ok := parsed["is_feedback"]; !ok {
    return d.fallbackUserFeedbackSignals(userMessages, skillName), nil  // key 不存在时走 fallback
}
```

**问题分析**：Python 对 `is_feedback` key 不存在时返回空列表；Go 走了 fallback 路径（返回正则匹配结果）。这可能导致 Go 产生 Python 不会产生的假阳性信号。

**修复方案**：当 `is_feedback` key 不存在时，返回 `nil, nil`（空列表），而非走 fallback 路径。fallback 只应在 LLM 调用失败时使用。

---

### S-18：[严重] matchFailureKeyword 误杀含 "error = None" 的内容

**Python 参考代码**：
```python
_FAILURE_KEYWORDS = re.compile(
    r"error(?!\s*=\s*None)|exception|traceback|failed|failure|timeout|..."
)
```

Python 用负向前瞻 `error(?!\s*=\s*None)` 精确排除 "error = None"。

**Go 问题代码** (`from_conv.go:438-439`):
```go
func matchFailureKeyword(content string) bool {
    return failureKeywords.MatchString(content) && !errorEqualsNonePattern.MatchString(content)
}
```

**问题分析**：Go 的实现是"内容包含任何失败关键词 AND 内容包含 error = None → false"。但这会误杀包含 "error = None" 但同时也包含 "timeout" 或 "failed" 的内容。例如 `"task failed, error = None"` 在 Python 中会匹配 `failed`，但在 Go 中因同时包含 "error = None" 而返回 false。

**修复方案**：`matchFailureKeyword` 应复用 `findFailureKeywordIndex` 的逻辑：
```go
func matchFailureKeyword(content string) bool {
    return findFailureKeywordIndex(content) != nil
}
```

---

### N-15：[一般] FromEvaluatedCase 绕过 MakeEvolutionSignal 工厂函数

**Python 参考代码**：
```python
return make_evolution_signal(signal_type=..., source="offline_evaluation", context={...})
```

**Go 问题代码** (`from_eval.go:40-47`):
```go
return &EvolutionSignal{
    SignalType: signalType,
    Context:    map[string]any{"source": "offline_evaluation", ...},
}
```

**问题分析**：Go 直接构造 `EvolutionSignal`，将 "source" 硬编码在 context 里，跳过了 `MakeEvolutionSignal` 函数。如果 `MakeEvolutionSignal` 的合并逻辑未来变更，这里不会同步。

**修复方案**：使用 `MakeEvolutionSignal` 构造信号，通过 `WithSource("offline_evaluation")` 和 `WithContext(...)` 传入。

---

### N-16：[一般] NewTeamSignalDetector 用 panic 而非 error

**Python 参考代码**：
```python
if policy is None:
    raise ValueError("TeamSignalDetector requires at least one LLM policy")
```

**Go 问题代码** (`team.go:164-166`):
```go
if policy.MaxAttempts == 0 && policy.TotalBudgetSecs == 0 {
    panic("TeamSignalDetector requires at least one LLM policy")
}
```

**修复方案**：改为返回 `(*TeamSignalDetector, error)`，在 policy 无效时返回 error。

---

### T-11：[提示] Team DetectUserIntent 消息截取语义不一致

**Python 参考代码**：
```python
user_msgs = []
for m in messages[-10:]:   # 先截取最后 10 条消息（不限 role）
    if role == "user":
        user_msgs.append(...)
```

**Go 代码**：
```go
for _, m := range messages {   # 先过滤全部 user 消息
    if role == "user" { ... }
}
if len(userMsgs) > 10 {       # 再截取最后 10 条
    userMsgs = userMsgs[len(userMsgs)-10:]
}
```

**问题分析**：Python 先截取最后 10 条消息再过滤 user；Go 先过滤再截取。如果最近 10 条消息中有 3 条 user 消息，Python 取 3 条；Go 可能在 100 条消息中取出 10 条 user 消息。

**修复方案**：先截取 `messages` 的最后 10 条，再过滤 user role。

---

### T-12：[提示] TrajectoryIssue 结构体未被使用

**修复方案**：考虑 `DetectTrajectoryIssues` 返回 `[]TrajectoryIssue` 而非 `[]map[string]string`。

---

### T-13：[提示] DetectUserMessageFeedback 不支持 Trajectory 输入

**问题分析**：同 S-16，Python 接受 Trajectory，Go 只接受 `[]map[string]any`。

---

## 严重问题汇总与修复优先级

| # | 章节 | 问题 | 优先级 |
|---|------|------|--------|
| S-01 | 9.72b | SimpleAPIWrapperFromCallable.Call 返回值格式不一致 | P0 |
| S-02 | 9.72b | Call 方法缺少 panic 恢复 | P0 |
| S-04 | 9.72b | 拒绝采样循环后 fnCall 可能为 nil → panic | P0 |
| S-07 | 9.59b | InteractGate.ConsumeDone 重复 close channel → panic | P0 |
| S-11 | 9.27-30 | 缺少 CreateVerificationAgent 工厂函数 | P0 |
| S-14 | 10.3.12 | CancelAllInflightWork 缺少 reason 透传 | P1 |
| S-15 | 10.3.12 | GetAgentNoWait normalize 逻辑导致空参数行为不一致 | P1 |
| S-16 | 9.73 | Detect 不支持 Trajectory 输入 | P1 |
| S-17 | 9.73 | is_feedback key 不存在时走 fallback 而非返回空 | P1 |
| S-18 | 9.73 | matchFailureKeyword 误杀含 "error = None" 的内容 | P1 |
| S-03 | 9.72b | resultExamples extend vs append 嵌套结构不同 | P1 |
| S-08 | 9.59b | resolveSender 硬编码 names 屏蔽错误路径 | P2 |
| S-09 | 9.59b | dispatchPayload 缺少 auto_start 调用（⤵️ 待回填） | P2 |
| S-10 | 9.59b | Payload 结构体字段未导出 | P2 |
| S-12 | 9.27-30 | VerificationRail.BeforeModelCall 缺少条件判断 | P2 |
| S-13 | 9.72a | restorePlaceholders 失败后仍返回不完整结果 | P2 |
