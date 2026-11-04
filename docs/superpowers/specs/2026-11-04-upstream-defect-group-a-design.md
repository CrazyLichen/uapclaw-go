# 上游缺陷同步 · A 组（核心 ReAct 循环与中断恢复）设计

> 来源：[`docs/sync/defect-plan-2026-11-04.md`](../../sync/defect-plan-2026-11-04.md) A 组 12 条
> 基线：agent-core `a889d9fcc` → `5a35c39f`
> 日期：2026-11-04
> 讨论方式：逐条核对上游提交并对照 Go 实现，全部结论已与用户逐条确认

---

## 0. 结论摘要

| 判定 | 条数 | 编号 |
| --- | --- | --- |
| 需在 Go 侧修复 | 9 | A-01 A-02 A-03 A-05 A-06 A-07 A-08 A-10 A-11 |
| 未迁移模块（⤵️ 归新功能） | 2 | A-04（领域二十三）、A-09（领域九 9.30） |
| 已对齐（➖） | 1 | A-12 |

核对方法：`git show <sha>` 取上游完整 diff → 在 Go 侧 grep/Read 定位等价实现 → 判定「缺实现 / 实现但无防护 / 已对齐」。
`/tmp/upstream/agent-core` 与 `/tmp/upstream/jiuwenswarm` 为带 `.git` 的分析克隆。

---

## 1. 逐条设计

### A-01 迭代预算耗尽时消费 boundary force_finish（`43d18a45`）

**判定**：Go 缺边界消费点，且兜底文案/字段与上游不一致 → 需改。

**改动**

| 文件 | 内容 |
| --- | --- |
| `single_agent/interfaces/callback.go` | 新增 `CallbackAfterReactIteration AgentCallbackEvent = "after_react_iteration"`；补 `eventMethodMap` / `BaseEventMethodMap`；`event_test.go` 补枚举断言 |
| `single_agent/agents/react_invoke.go` | ① 循环顶「轮首消费」`ConsumeForceFinish()`（对齐 Python L3100-3108，避免上一轮 hook 设的 force_finish 先白跑一次模型调用）；② 每轮「模型 + 全部工具 + ToolMessage 都写完」后 `fire(after_react_iteration)`；③ for-else 等价物：循环耗尽时先消费 force_finish，命中用其结果收尾，否则走 max_iterations 错误结果 |

**结果字段（6 个，与上游 `_finish_max_iterations` error 分支 1:1）**

```go
map[string]any{
    "output":                      "Max iterations reached without completion",   // 原中文兜底改英文
    "result_type":                 "error",
    "termination_reason":          "max_iterations",
    "iterations_used":             iteration + 1,
    "truncated_by_max_iterations": true,
    "max_iterations":              maxIter,
    "finalization_attempted":      false,
    "finalization_succeeded":      false,
}
```

**测试**：① 最后一轮边界请求 force_finish → 返回该结果而非 max_iterations 错误；② 无 ff → 断言错误文案与 6 个字段齐全；③ `after_react_iteration` 事件在每轮成功结束时恰好触发一次。

**备注**：`finalization_*` 在 Go 侧当前恒为 false（`on_max_iterations` 三策略未实现，随 H-15 补齐）；结果字段的唯一消费方是上游 RSI 评估器（`rsi/harness_rsi/*`），Go 侧暂无消费者，但结果 map 会经 session 流/历史透出。

---

### A-02 before_tool_call 请求 force_finish 后补 ToolMessage（`537ebc4c`）

**判定**：Go 已能拦截工具不执行、也能向上传播 force_finish，但**不产 ToolMessage** → 上下文里 assistant `tool_calls` 悬空 → 下一轮请求 400 → 需改。

**改动**（`single_agent/ability/ability_manager.go` `Execute()`，`wg.Wait()` 之后）

单赋值点版：先收敛、再处理结果，`finishByIndex` 全文只有一处写入。

```go
// ① 先按 tool-call 顺序一次性收敛子 ctx 上的 force-finish（一次性消费，每个 i 至多一条）
finishByIndex := make(map[int]map[string]any, len(toolCtxs))
for i := range toolCtxs {
    if ff := toolCtxs[i].ConsumeForceFinish(); ff != nil {
        finishByIndex[i] = ff.Result
    }
}
if len(finishByIndex) > 0 {
    cbc.RequestForceFinish(finishByIndex[minIndex(finishByIndex)])   // Python: min(force_finish_requests)
}

// ② 结果处理（Python ①②③④）
for i := range results {
    res := &results[i]
    // 错误兜底：ToolMsg 缺失时用 error 文案补
    if res.ToolMsg == nil && res.Result != nil {
        if err, ok := res.Result.(error); ok {
            res.ToolMsg = llmschema.NewToolMessage(toolCalls[i].ID, err.Error())
        }
    }
    // before/after 钩子请求 ff：必须补 ToolMessage
    if ffResult, hasFF := finishByIndex[i]; hasFF && res.ToolMsg == nil {
        res.Result = ffResult
        body, _ := json.Marshal(ffResult)
        res.ToolMsg = llmschema.NewToolMessage(toolCalls[i].ID, string(body))
        if inputs, ok := toolCtxs[i].Inputs().(*interfaces.ToolCallInputs); ok {   // inputs 优先
            if inputs.ToolResult != nil { res.Result = inputs.ToolResult }
            if inputs.ToolMsg != nil { res.ToolMsg = inputs.ToolMsg }
        }
    }
}
```

`minIndex(map[int]map[string]any) int` 新私有辅助（对应 Python `min(force_finish_requests)`）。

**与 Python 的唯一差异**：Python 是「边处理结果边消费、最后再收尾」，Go 是「先收尾、再处理结果」。消费只是清空标志、外部无观察者，结果等价；换来 map 单点写入。**已在代码注释中写明该顺序差异**。

**`_skip_tool` 边界**：skip 路径 `res.ToolMsg != nil` → 不进合成分支 → 由收敛循环收集 ff，与 Python（skip 返回 tuple，不匹配 dict/None 分支）一致。

**测试**：① before_tool_call 请求 ff → 工具未执行 + `results[0].ToolMsg.ToolCallID` 匹配 + `cbc` 的 ff 等于 rail 给的结果；② 并行 3 工具、中间那个 ff → 3 条 ToolMsg 齐全 + 传播的是索引 1 的结果（验证最小索引）；③ after_tool_call 同时写 `inputs.ToolResult/ToolMsg` + 请求 ff → 采用 inputs 的值。

---

### A-03 流式异常必须把流消费干净（`aeceda1c`）

**判定**：上游三部分改动中，`llm_controller` 属未迁移模块（⤵️），force-finish 结果传播归 A-02，`react_agent` 非 AssistantMessage 分支 Go 已等价（`ConsumeForceFinish #1` 接住）。Go 的等价风险在**两层流包装 goroutine 的裸发送** → 需加固。

**改动**

| 文件 | 内容 |
| --- | --- |
| `foundation/llm/model.go` | 包装 goroutine 的 `out <- chunk` 改为 `select { case out <- chunk: case <-ctx.Done(): 排空 chunkChan; return }`；加 `defer recover()`，panic 转 `StreamError` 哨兵 chunk |
| `session/tracer/decorator/decorator.go` | 同上（该层套在 model 外层，不修会两层同时挂住） |
| `single_agent/agents/react_model_call.go` | `callLLMStream` 遇 `StreamError` 返回前显式排空剩余 chunk（对齐 Python `async for _ in stream_iter`） |

**panic 兜底语义**：捕获后投递 `StreamError` 哨兵（`fmt.Errorf("stream wrapper panic: %v", rec)`），让消费方走正常错误路径，而不是拿到被截断的空响应；不得静默只记日志后关闭 channel。

**日志**

| 点 | 级别 | `event_type` | 字段 |
| --- | --- | --- | --- |
| `model.go` 排空 | Info | `llm_stream_consumer_abandoned` | `layer="model"`、`drained` |
| `tracer` 排空 | Info | `llm_stream_consumer_abandoned` | `layer="tracer"`、`drained` |
| `callLLMStream` 排空 | Debug | `llm_stream_error_drained` | `drained`、`.Err(err)` |
| 两层 panic | Error | `llm_stream_panic` | `layer`、`stack=debug.Stack()`、`.Err(...)` |

`logger.Msg` 用英文（项目约定），注释用中文。

**测试**：① 消费者提前放弃 + ctx 未取消 → 生产端在排空后退出、`resp.Body` 关闭（用 httptest 慢流构造）；② 包装层钩子 panic → 消费方收到 `StreamError` 而非拿到空消息；③ `-race` 下无泄漏（`goleak` 或 goroutine 计数断言）。

---

### A-04 普通查询恢复定位首个被中断组件（`e2f492c8`）

**判定**：落点 `openjiuwen/core/application/llm_agent/llm_controller.py`，Go 无 `internal/agentcore/application/**`，`LLMController` / `_find_interrupted_task` / `InteractiveInput.update` 恢复链全未迁移 → **⤵️ 领域二十三（LLM 协议与多模态）**。

**实现要求**（落地时按上游 HEAD）：`_create_query_resume_input(component_ids, user_query)`——空列表 → `InteractiveInput(raw_inputs=...)`；`component_ids[0] != "questioner"` → 只喂首个组件；否则全写（questioner 需广播）。

**不排期**：随 application 层实现落地，无需单独修这 15 行。

---

### A-05 内层 ReAct 未配置时视为不限（`d4e8f81e`、`980ba342`）

**判定**：Go 默认 15 + `EnableTaskLoop` 三元覆盖显式配置 → 需改。

**改动**

1. `harness/schema/config.go`：删 `DefaultMaxIterations = 15`；`NewDeepAgentConfig()` 不再填；`EffectiveMaxIterations()`（生产零调用，仅测试用）改名 `ResolveInnerReactMaxIterations()`，语义：`0` → `math.MaxInt`（未配置=不限），`<0` → Warn + 按未配置，`>0` 原样
2. `harness/deep_agent.go:1577` 与 `:1768`：两处三元删除，统一 `reactConfig.MaxIterations = cfg.ResolveInnerReactMaxIterations()`；加 Info 日志 `[DeepAgent] inner ReAct max_iterations=<N|unbounded> (configured|default)`
3. `harness/factory.go:135`：**不改**（0 现在天然表示未配置），补注释说明
4. 子代理默认值全部对齐上游 HEAD：`plan` 25→**50**、`explore` 15→**50**、`research` 15→**50**、`code` 15→**50**、`browser` 25→**100**（`DEFAULT_BROWSER_AGENT_MAX_ITERATIONS`）

**类型选择**：`MaxIterations int`，**约定 0 表示未配置**（不用 `*int`）。代价：显式配置 0 无法表达（Python 里 0 等价于不迭代，属无意义配置）。

**测试**：`0 → math.MaxInt`；显式 `7` + `EnableTaskLoop=true` → 仍为 7（上游回归用例意图）；负数 → 告警 + 不限；子代理默认值断言。

**备注**：上游 `deep_agent.py:629-630` 还会把 `on_max_iterations`、`max_iterations_hard_limit` 透传给内层 react config，Go 两字段均无 → 随 H-15 补齐。

---

### A-06 输出上限命中且无答案时判定失败（`5f017e34`）

**判定**：`_raise_for_model_response_error` 等价物在 Go 完全不存在 → 需改（全量移植两段判定）。

**改动**（`single_agent/agents/react_model_call.go`）

新增私有函数，两段判定 1:1 移植，错误文案与 Python 逐字一致：

```go
func raiseForModelResponseError(msg *llmschema.AssistantMessage) error {
    finishReason := strings.ToLower(strings.TrimSpace(msg.FinishReason))
    code := 0
    if msg.UsageMetadata != nil { code = msg.UsageMetadata.Code }
    noAnswer := len(msg.ToolCalls) == 0 && strings.TrimSpace(msg.Content.Text()) == ""

    // ① 输出上限命中且无任何答案
    if (finishReason == "length" || finishReason == "max_tokens") && noAnswer {
        return exception.NewBaseError(exception.StatusModelCallFailed,
            exception.WithMsg(fmt.Sprintf("model hit its output limit (finish_reason=%s, output_tokens=%d) before producing an answer or a tool call", finishReason, outputTokens)),
            exception.WithParams(map[string]any{"finish_reason": finishReason, "output_tokens": outputTokens}))
    }
    // ② provider 返回错误响应
    if finishReason != "error" && finishReason != "failed" && code == 0 { return nil }
    ... WithMsg(fmt.Sprintf("provider response error: code=%d, finish_reason=%s, message=%s", ...))
}
```

`railedModelCall` 收成**单一出口**（对齐 Python `ctx.inputs.response = ai_message` 后紧跟校验）：

```go
var aiMsg *llmschema.AssistantMessage
if isStreaming { aiMsg, err = a.callLLMStream(...) } else { aiMsg, err = a.callLLMInvoke(...) }
if err != nil { return nil, err }
if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok { inputs.Response = aiMsg }
if err := raiseForModelResponseError(aiMsg); err != nil { return nil, err }
return aiMsg, nil
```

**错误传播链**：`fn` 返回 error → `rail.ModelCallRail` 的 `handleException` → `fire(on_model_exception)`（`ModelBackupRail` 可换模型重试）→ 无重试则上抛 → `reactLoop` 返回 error → 流里写 `chat.error`。与 Python 同构。

**刻意不改**：`length` 但有正文 → 返回部分文本；`length` 但有 tool_calls → 照常执行工具；正常 `stop` + 有内容 → 不变。

**现实约束**：`usage.code` / `err_msg` 在 Go 与 Python 两侧都**没有生产端**（Go 全仓无客户端写这两个字段，Python 客户端构造 `UsageMetadata` 时也不填），`finish_reason="error"` 同样无产出者 → ② 是防御性判定，单测需手工构造 `UsageMetadata{Code: 123}` 覆盖。

**测试**：参数化 6 例（`length`/`max_tokens` × 空串/空白/无 tool_calls）；2 例不报错（有正文、有 tool_calls）；1 例 provider error（code=123）；1 例正常通过。

---

### A-07 空回答重试一次再失败（`ad668f63`）

**判定**：Go 把空内容当成功 answer → 需改。

**常量**（`react_invoke.go` 常量区，文案照抄上游）

```go
const (
    emptyResponseMsg          = "模型未返回有效内容（空响应），请重试或检查上下文。"          // 上游即中文
    emptyResponseMaxRetries   = 1
    emptyResponseNotice       = "[EMPTY_RESPONSE] The previous turn had no content and no tool_calls. Reply with user-visible content or a tool call."
)
```

**流程**：`reactLoop` 加循环级 `emptyResponseRetries := 0`，无 tool_calls 分支按 Python 顺序插入：
steering →（视觉降级 continue，A-08）→ 空内容重试/失败 → answer。

```go
if strings.TrimSpace(content) == "" {
    if emptyResponseRetries < emptyResponseMaxRetries && iteration+1 < maxIter {
        emptyResponseRetries++
        AddMessages(NewUserMessage(emptyResponseNotice,
            WithMetadata(map[string]any{schema.MetadataKeyMessageSourceKind: "empty_response"})))
        logger.Info(...).Msg("empty no-tool turn, retrying")
        continue
    }
    saveContexts(); iterResult = map[string]any{
        "output": emptyResponseMsg, "result_type": "error",
        "finish_reason": finishReasonOrNull(aiMsg),
        "reasoning_present": aiMsg != nil && aiMsg.ReasoningContent != "",
    }
    break
}
```

**metadata 常量搬迁**：`_openjiuwen_message_source_kind` 上游定义在 `core/foundation/llm/schema/message.py:12`，Go 现放在 `harness/prompts/attachment_types.go:119`（`MetadataKeyMessageSourceKind`，**零消费者**）。搬到 `foundation/llm/schema`（对齐上游位置），删除 harness 里的重复定义，`agents` 直接用 schema 的。

**测试**：① 空 → 重试后正常回答；② 连续两空 → `result_type=error` + 三字段；③ 纯空白算空；④ 最后一轮空 → 不重试直接 error。

**备注**：Python 该分支还有 `ctx.consume_model_continue_request()`（steering 相关），Go 无此机制 → 另属 steering 批次，不在本条。

---

### A-08 防 agent 挂死四件套（`fee4e34c`）

**判定**：四件全缺 → 全做。

#### A-08-1 无进展停止（不进 TaskCompletionRail，是它的同级兄弟）

| 文件 | 改动 |
| --- | --- |
| `harness/task_loop/stop_condition.go` | `NoProgressAnswerEvaluator{maxConsecutiveEmptyAnswers, minAnswerChars, consecutive}`；`ShouldStop`：`result_type=="answer" && len(strings.TrimSpace(output)) < minAnswerChars` → +1，否则清零，达阈值停；`Name()` 返回 `"NoProgressAnswerEvaluator"`（`stop_reason` 直接用它）；`ExportState/ImportState/Reset` 补全（Go 接口比 Python 多这两个方法） |
| `harness/schema/config.go` | `TaskLoopNoProgressGuardConfig{Enabled bool, MaxConsecutiveEmptyAnswers int, MinAnswerChars int}`，默认 `true / 3 / 20`；`DeepAgentConfig` 加 `TaskLoopNoProgressGuard` 字段 |
| `harness/deep_agent.go` | `deep_agent.go:2632` 与 `:2652` 两处重复的 `taskCompRail.BuildEvaluators()` 抽成 `buildTaskLoopEvaluators()`（原评估器 + 可选 guard 追加）；循环结束后 `stopReason == "NoProgressAnswerEvaluator"` → yield `{output:"Task loop stopped after repeated empty or near-empty no-tool answers. This indicates the model is not making observable progress.", result_type:"error", stop_reason:"NoProgressAnswerEvaluator"}` |

#### A-08-2 视觉降级（运行期自救，与 H-14 装配期模态探测不同层）

| 文件 | 改动 |
| --- | --- |
| `model_clients/openai/types.go` | `ChatCompletionChunkResponse` 加 `Error *ErrorDetail`（当前流内 `data: {"error":...}` 被静默丢弃 → `ParseStreamChunk` 返回 nil → `continue`） |
| `model_clients/openai/client.go` | `ParseStreamChunk` 前判 `chunkResp.Error != nil` → `c.FailStream(...)`；`HandleHTTPError` 把 provider 的 `code`/`type`/`status_code` 写进 `BaseError.Params`（当前 `errResp.Error.Code/Type` 解析后丢弃） |
| `agents/react_model_call.go` | `messagesContainImageInput(msgs)`（扫 `ContentPart.Type == "image_url"`）；`isImageInputUnsupportedError(err)`：仿 Python `_extract_exception_error_values` 抽全文字符串（`err.Error()` + `BaseError.Params()/Details()` + `Cause()` 链递归），规范化 `lower + "-"→"_" + " "→"_"` 后匹配 6 个 code（精确或 `_` 后缀）与 14 条 pattern（子串）；`buildImageInputUnsupportedMessage()` 双语原文照抄；`railedModelCall` 里把这类错误转成合成 `AssistantMessage` + `SetExtra("_last_image_unsupported", true)` |
| `agents/react_invoke.go` | 无 tool_calls 分支按序插：steering → `_last_image_unsupported` 清标志 + saveContexts + `continue` → 空响应（A-07）→ answer |

#### A-08-3 外层轮次硬上限

`harness/deep_agent.go` `runTaskLoop`：`maxOuterRounds = 50`，循环体第一行 `outerRound++` 后判超限 → yield `{output:"Task loop stopped after exceeding the maximum number of outer rounds (50). This usually indicates the model is not making observable progress.", result_type:"error", stop_reason:"MaxOuterRounds"}` → break。**不进评估器**（评估器为空时 `ShouldContinue()` 恒真，这正是它要兜的洞）。

#### A-08-4 技能预算

`harness/rails/skills/skill_use_rail.go` 加 `MaxSkills` / `MaxTotalChars`；`applySkillBudget()` 按与当前 query 的关键词重叠度排序，**整块丢弃**低排名技能（绝不截断到半截文本）；`harness/schema/config.go` 加 `SkillBudgetMaxSkills`/`SkillBudgetMaxTotalChars`，`factory.go` 透传。

**测试**：无进展评估器 3 例（连续短答案停 / 长答案清零 / 状态导出导入）；50 轮上限 1 例；拒图识别（6 code + 14 pattern 参数化）+ 流内 error 块 1 例 + 合成消息后循环继续 1 例；技能预算（超 `max_skills`、超 `max_total_chars`、不超不动）。

---

### A-09 只把连续的相同工具调用计为 loop（`589da12a`）

**判定**：Go 无 `ModelAnomalyDetectionRail`（917 行：瞬时失败分类 / 退避重试 / 退化重复检测 / 流超时识别 / **工具循环压缩** / bailout） → **⤵️ 领域九新增 `9.30 ModelAnomalyDetectionRail`**。

**实现要求**：工具循环压缩必须按背靠背相邻性判定——`if round.end != eligible[-1].start: break`（`model_anomaly_detection_rail.py:713`），否则"读文件 → 用户新输入 → 再读同一文件"会被误折叠并删掉用户输入。

**台账证据修正**：`1c4f5603` 在两仓均 `git cat-file -t` 失败（不存在），A-09 仅 `589da12a` 一个有效证据 → 删除该 SHA。

---

### A-10 中断后 ReAct context 持久化（`bf0a3eb2`）

**判定**：Go 取消时 `ClearContextMessages` 抹掉本轮全部消息且不落盘，异常路径完全不处理 → 需改。**直接按上游 HEAD 行为一次到位**（含 B-03 `1b28ec45`、B-04 `4ba66bf1` 的后续迭代语义），B 组核验时这两条应标 ➖。

**改动**

| 文件 | 内容 |
| --- | --- |
| `agents/react_invoke.go` | 新增 `sanitizeAbortedTurnMessages(msgs, marker) []BaseMessage`（1:1 移植清洗规则）；新增 `handleContextAbort(ctx, sess, marker) bool`（`GetMessages(0,false)` → 清洗 → `SetMessages(kept,false)` → `saveContexts` → `*session.Session` 则 `Commit`）；`invokeImpl` 的 `err != nil` 拆「取消 → `[Request cancelled by user]`」「其他错误 → `[Request interrupted by an unexpected error]`」两路调用；`abortPersisted` 传给 `needCleanup` 的 defer 抑制重复 save/commit；`innerStream` 的 `streamProcess` 同构替换 |
| `agents/react_invoke.go` | `ClearContextMessages` 保留（公开 API）但不再用于取消路径 |

**清洗规则**

- assistant 带 `tool_calls`：向后收集紧跟的 ToolMessage；**全部 tool_call_id 都有着落**才保留（assistant + 按序 tool），否则整块丢弃
- 孤立 ToolMessage：丢弃
- 其他消息：保留
- 结尾若是 UserMessage → 追加 `AssistantMessage(marker)`，避免"只有提问没有回复"的非法尾

**测试**：① 取消 → 本轮 UserMessage 保留 + 尾 marker；② 半截 tool 块被整块丢弃；③ 已完成 tool 对被保留；④ 异常路径同样持久化（marker 2）；⑤ 外部传入 session 时确实调用了 Commit。

---

### A-11 system prompt 为空仍保留 skills section（`051ae9c8`）

**改动**：`agents/react_prompt.go` 的 `updateSkillPromptBuilderSection` 删掉 `renderedSystemPrompt == ""` 条件（只由 `skillUtil.HasSkill()` 决定），`renderedSystemPrompt` 参数随之无用 → 从签名移除，唯一调用点 `react_invoke.go:422` 与注释同步。

**测试**：system prompt 为空 + 注册 1 技能 → section 存在且含技能名；无技能 → section 被移除；`skillUtil == nil` → 不 panic。

**不在本条**：Python 还给 `add_prompt_builder_section` 传 `category=ContextCategory.SKILLS`，Go 的 `PromptSection` 无 `Category` 字段 → 归 H-11（section 静态注册表）。

---

### A-12 Agent 工具 HTTP 超时与状态校验（`b8ceb049`）

**判定**：**➖ 已对齐**。`swarm/server/runtime/skill/remote_import.go:165/172`（120s 超时 + 状态码），`skill_manager.go` 六处 HTTP 调用均带显式 `Timeout`（30/60/120s）与状态码校验，`git_ops.go` clone/pull 有 `context.WithTimeout`。

**未覆盖**：`context_evolver/tool/wikipedia_tool.py` 对应工具 Go 未迁移（`grep -i wikipedia` → 0 命中），随该工具实现落地。

---

## 2. 实施顺序

依赖关系：A-07 与 A-08-2 插在同一分支（无 tool_calls），A-08-2 还需先补 client 侧的流内错误；A-01 的事件新增影响计划条目 6.4；A-10 与 B 组重叠。

| 批次 | 条目 | 说明 |
| --- | --- | --- |
| 1 | A-11 | 最小改动，先热身 |
| 2 | A-01 | 新增回调事件 + 循环边界收尾（影响面：枚举/映射/测试/计划 6.4） |
| 3 | A-06 → A-07 | 同一文件同一分支，先后插入：响应校验 → 空回答重试 |
| 4 | A-02 | `Execute()` 结果处理重构 + force_finish 传播 |
| 5 | A-03 | 两层流包装加固 + panic 兜底 |
| 6 | A-05 | 配置语义 + 5 个子代理默认值 |
| 7 | A-10 | 中断持久化（含 B-03/B-04 语义） |
| 8 | A-08 | 四件套（体量最大，含 client 层改动） |

每条完成标准：单测 ≥ 包 85% 覆盖、`go test -race` 干净、CI 四 job 全绿（Build / Lint / Unit / Integration）。

---

## 3. 台账状态更新建议

| 编号 | 现状态 | 建议 | 备注 |
| --- | --- | --- | --- |
| A-01 | 🔍 | ✅ | 同时更新计划 6.4「10 种事件」→ 11 种 |
| A-02 | 🔍 | ✅ | |
| A-03 | 🔍 | ✅ | 注明 `llm_controller` 部分 ⤵️ |
| A-04 | 🔍 | ⤵️ | 领域二十三 |
| A-05 | 🔍 | ✅ | 含 5 个子代理默认值对齐 |
| A-06 | 🔍 | ✅ | |
| A-07 | 🔍 | ✅ | |
| A-08 | 🔍 | ✅ | 4 子项 |
| A-09 | 🔍 | ⤵️ | 领域九 9.30；删除无效 SHA `1c4f5603` |
| A-10 | 🔍 | ✅ | B-03/B-04 将被一并吸收 |
| A-11 | 🔍 | ✅ | |
| A-12 | 🔍 | ➖ | wikipedia 部分随工具实现 |

---

## 4. 已知偏差与风险

1. **A-01 `finalization_attempted/succeeded` 在 Go 恒为 false**：`on_max_iterations` 三策略未实现，随 H-15 才有值。
2. **A-02 顺序差异**：Go 先收敛再处理结果（map 单点写入），Python 反向；消费无外部观察者，结果等价，代码注释已写明。
3. **A-02 ToolMessage 内容格式**：Python `str(dict)` 为单引号风格，Go 用 `json.Marshal`；该内容仅在循环继续时才会被模型看到（ff 场景循环会 break），不追求字节一致。**错误兜底分支**需注意 Python 文案带 `Ability execution error: ` 前缀，Go 若沿用裸 `err.Error()` 需在实现时补前缀（该分支仅在 `errorToExecuteResult` 未产出 ToolMsg 时生效，正常错误路径不可达）。
4. **A-05 用 `int` + 0 表示未配置**（非 `*int`）：显式配置 0 无法表达（Python 中 0 等价于不迭代，属无意义配置）。
5. **A-06 ② 段为防御性判定**：两侧都没有 `usage.code` 的生产端，真实链路走不到，靠手工构造单测覆盖。
6. **A-08-2 依赖 client 层补齐流内 `error` 事件**：不补则拒图识别在流式主路径永远不触发。
7. **A-09 无落点**：917 行 rail 属新功能，本轮不实现。
8. **未纳入本组的相关缺口**：`after_react_iteration` 的现有消费者（`agent_teams/harness/snapshot_rail.py`）Go 未迁移；`PromptSection.Category` 缺失（H-11）；`consume_model_continue_request`（steering 批次）。

---

## 5. 验收

- `go test -race ./internal/agentcore/single_agent/... ./internal/agentcore/harness/... ./internal/agentcore/foundation/llm/...`
- 覆盖率：改动行覆盖率门禁（85% / 5%）通过，纯声明搬移文件按行级豁免登记
- 集成用例：A-02（工具执行 + 上下文链）、A-10（取消持久化）跨 ≥3 个 internal 二级子系统，走 `cmd/integration-audit -verify` 两层门禁
- CI 四个 job 全绿后方可宣告完成
