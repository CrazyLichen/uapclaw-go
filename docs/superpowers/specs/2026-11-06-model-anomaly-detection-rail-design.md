# 领域九 9.86 · ModelAnomalyDetectionRail 设计（含前置 2.17 流帧超时）

> 来源：`IMPLEMENTATION_PLAN.md` 9.86；缺陷台账 `docs/sync/defect-plan-2026-11-04.md` A-09（`589da12a`）
> 上游基线：agent-core `5a35c39f`（`openjiuwen/harness/rails/model_anomaly_detection_rail.py` 917 行）；jiuwenswarm 0.2.5.beta1
> 日期：2026-11-06
> 讨论方式：brainstorming 逐条确认（范围 / D1 检查器通道 / D2 计数器归属 / D3 超时落点 / D4 配置三态），结论见 §4

---

## 0. 结论摘要

本条交付三个彼此依赖的层，缺一层就有不可达分支：

| 层 | 产出 | 为什么必须一起 |
| --- | --- | --- |
| A. 地基 | 流式分片检查器通道 + 可见输出标志 | 没有它，"退化重复检测"永远不触发 |
| B. LLM（新行 2.17） | `Model.Stream` 首帧/空闲超时 | 没有它，"流超时识别"永远不触发 |
| C. rail（9.86） | `ModelAnomalyDetectionRail` + 三处接线 | 本体 |

范围已确认为**全接线**：agent-core factory 开关 + 单 agent adapter（`execution_guard`） + 团队成员 rail + `resources/config.yaml`。

关键事实：**该 rail 在产品里默认开启**（上游 `resources/config.yaml:1028-1048`：`enabled: true`、`tool_loop_compact.enabled: true`、阈值 4/3、`single_char_repeat_count: 90`），不是可选插件。

---

## 1. 上游实现清单

### 1.1 rail 本体

`openjiuwen/harness/rails/model_anomaly_detection_rail.py`

| 行段 | 内容 |
| --- | --- |
| 31-53 | 常量：inspector key、state key、repeat marker、stream timeout markers、backoff 默认 `(0.5,1,2)`、`transient_base=2.0`、`MAX_RETRY_AFTER=60`、retryable 状态码 `{408,409,429}`、7 条不可重试文本 marker |
| 56-242 | 模块函数：`_exception_chain`、`_coerce_status_code`、`_header_value`、`_headers_of`、`_status_code_of`、`_parse_retry_after_seconds`、`_client_retry_decision`、`_classify_transient_failure` |
| 244-278 | 双语警告模板 + bailout 文案 + 模板表 |
| 281-315 | `ToolLoopCompactConfig`（enabled=False / consecutive_threshold=4 ge=2 / bailout_threshold=3 ge=0）+ `_ToolRound` |
| 317-392 | `ModelAnomalyDetectionRail(priority=70)`：10 条构造校验（`:345-372`）、三类计数器、`_tool_loop_compact_counts`（按 session） |
| 408-482 | `backoff_delay`、`before_invoke`、`after_invoke`、`before_model_call`、`inspect_stream_chunk`、`on_model_exception` |
| 483-646 | 重复检测与重试记账、session key、计数读写 |
| 648-725 | `_maybe_compact_or_bailout_tool_loop` + 语言解析 |
| 728-911 | `_find_tool_loop_compact_range`、`_collect_tool_rounds`、`_collect_call_and_result_sets`、`_build_tool_loop_warning_content`、`_parse_tool_arguments`、`_canonical_arguments_text`、`_dumps_json_list`、`_normalize_language` |

### 1.2 依赖的地基（Go 缺失）

| 上游 | 行号 | 作用 |
| --- | --- | --- |
| `core/single_agent/agents/react_agent.py` | 1877-1905 | 流循环里遍历 `ctx.extra["_stream_chunk_inspectors"]` 逐个调用；写 reasoning/content 时置 `MODEL_VISIBLE_OUTPUT_EMITTED_KEY` |
| `core/single_agent/rail/base.py` | 70 | `MODEL_VISIBLE_OUTPUT_EMITTED_KEY = "_model_visible_output_emitted"` |
| `core/foundation/llm/model.py` | 228-330 | 首帧/空闲超时（`asyncio.wait_for` + `aclose` + `MODEL_CALL_FAILED`），`waited_since_progress` 只累计 `__anext__` 内耗时 |
| `core/foundation/llm/schema/config.py` | 152-166 | `stream_first_chunk_timeout=300.0`、`stream_idle_timeout=60.0`，`None` 禁用 |
| `core/foundation/llm/schema/message_chunk.py` | 242-256 | `carries_output_token()`：content/reasoning 非空，或 tool_call 的 name/arguments 非空 |

### 1.3 装配三条路径

| 路径 | 位置 |
| --- | --- |
| agent-core factory 默认 | `harness/factory.py:398-400`，参数 `enable_model_anomaly_detection_rail=True`（`:221`、`:490`） |
| 单 agent（产品） | `server/runtime/agent_adapter/interface_deep.py:11036-11078` 构造（读 `execution_guard.model_anomaly_detection_rail`）、`:11532` 挂载、`:13239` 传 `enable_...=<同一 enabled>` |
| 团队成员 | `agents/swarm/providers/member_rails.py:613` + `agents/swarm/config_specs.py:113`（`SECURITY → MODEL_ANOMALY_DETECTION → HEARTBEAT`） |

`RAIL_WHITELIST`（`team_runtime_inheritance.py:90-107`）**不含**该 rail → Go 的 `railWhitelist` 也不加。

### 1.4 上游测试

- `tests/unit_tests/harness/test_model_anomaly_detection_rail.py`：24 例（重复检测 3、重试记账 4、agent 流式闭环 4、瞬时分类 7、Retry-After 3、可见输出闸门 3）
- `tests/unit_tests/harness/test_tool_loop_compact.py`：11 例
- `tests/unit_tests/harness/test_factory_model_anomaly_detection_rail.py`：工厂默认注册/显式提供去重

---

## 2. Go 现状

### 2.1 可直接复用的

| 能力 | 位置 |
| --- | --- |
| 整调用重试语义 | `single_agent/rail/executor.go:116-200`（before/fn/on_exception → `ConsumeRetryRequest` → 重跑；等待期间处理 `ctx.Done()`） |
| `RequestRetry` / `ConsumeRetryRequest` | `single_agent/interfaces/callback.go:614-633` |
| `AbortError`（含 cause 透传） | `runner/callback/errors.go` + `feedback` 路径 `framework.go:1616-1645` |
| 跨 rail 传值 | `cbc.Extra/SetExtra`（`callback.go:115-120`，`extraMuPtr` 共享锁） |
| 内外层事件桥接 | `harness/deep_agent.go:176-201`（`BeforeModelCall/AfterModelCall/OnModelException` 桥到内层 ReAct） |
| ModelContext 读写 | `cbc.ModelContext()` + `GetMessages/SetMessages`（现有 rail 先例：`fix_incomplete_tool_context.go:43`） |
| 语言 | `cbc.Agent().SystemPromptBuilder().Language()`（先例 `mcp_rail.go:73`） |

### 2.2 缺口

| # | 缺口 | 位置 | 后果 |
| --- | --- | --- | --- |
| 1 | rail 不存在 | — | — |
| 2 | 无流分片检查器通道；`callLLMStream` 未接 `cbc` | `react_model_call.go:189,320-431` | 重复检测不可达 |
| 3 | `_model_visible_output_emitted` 全仓 0 命中 | — | 已吐字仍会做瞬时重试 → 重复输出 |
| 4 | `ModelClientConfig` 无 `stream_first_chunk_timeout/stream_idle_timeout`；`Model.Stream` 无帧超时 | `schema/config.go`、`model.go` | 流超时分支不可达 |
| 5 | `HandleHTTPError` 丢弃 `retry-after` / `x-should-retry`；非标准 body 分支连 params 都没有 | `model_clients/openai/client.go:552-580` | 两条分类能力缺失 |
| 6 | 无 `execution_guard` 配置段 | `resources/config.yaml`（408 行精简版） | 产品无法开关/调阈值 |
| 7 | factory 无 `EnableModelAnomalyDetectionRail` | `harness/harness_config/params.go`、`factory.go:551` | 只能靠调用方显式给 rail |
| 8 | adapter 无 builder / 无挂载点 | `swarm/server/adapter/deep_adapter_rails.go:50-190` | 产品路径不生效 |
| 9 | 团队成员无 builder | `swarm/agents/harness/team/runtime_inheritance.go` | 成员无护栏 |

---

## 3. 流程位置（供实现与审查对照）

```
DeepAgent.invoke / Stream
└─ FireLifecycle(BEFORE_INVOKE, AFTER_INVOKE)                 ← 外层 cbc
   ├─ rail.BeforeInvoke        清 3 类计数器 + 压缩计数（D2-a：delete）
   ├─ task loop（每轮）
   │  └─ 内层 ReActAgent.invoke（bridgeEvents 已把 model 事件桥到内层）
   │     └─ ReAct 迭代 think
   │        ├─ rail.BeforeModelCall     压缩/bailout → 清尾窗 → 装检查器
   │        ├─ ModelCallRail.Execute(fn = _railed_model_call)
   │        │   ├─ llm.Stream 逐 chunk → inspectStreamChunk → 命中即 error
   │        │   └─ 异常 → rail.OnModelException → 分类 → RequestRetry → 重跑 fn
   │        └─ rail.AfterModelCall
   └─ rail.AfterInvoke          delete 计数器条目
```

priority = 70（`sys_operation 100 > mcp 95 > progressive/task_planning 90 > agent_mode/heartbeat 85 > anomaly 70 > lsp 60 > moa/task_completion 10`）。

---

## 4. 决策记录（已与用户逐条确认）

| 编号 | 决策 | 理由 |
| --- | --- | --- |
| 范围 | A + C 同批：分层交付 + 流帧超时作为新行 2.17 先落 | 9.86 若等 2.17，就留一个 ⤴️ 不可达分支；先落 2.17 可让 9.86 一次到位 |
| 范围 | 全接线（factory + adapter + 团队 + config.yaml） | 配置与行为必须成对，否则"关不掉/开不了" |
| D1 | `[]StreamChunkInspectorFunc`（裸函数值）+ 代码指针去重 + rail 包内私有 `funcPtrEqual` | 先例：`agentbuilder/utils/progress.go:737`、`sys_operation/registry.go:73-80`；上游去重也在 rail 自己里 |
| D2 | 三类计数器按 session id 分桶 + `RWMutex`；只存正计数，归零即删 | 产品中 agent 按 `mode:subMode:projectDir` 缓存（`agent_manager.go:722`），一会话一 rail 不成立 → rail 必然跨会话共享 |
| D3 | 超时落在 `Model.Stream` 包装 goroutine：select 接收超时 → cancel 派生 ctx → 排空 → 投递 `StreamError` 哨兵 | 与上游同层（Model 层）；客户端 5 家零改动；消费方已有 StreamError 分支 |
| D4 | `StreamFirstChunkTimeout/StreamIdleTimeout *float64`（nil=禁用），默认 300/60 由 `NewModelClientConfig` 给 | 三态无损；0 仍按上游 `gt=0` 视为非法 |

### 4.1 已知偏差（Go 语言层，需在评审时对照）

1. **检查器去重靠代码指针**：同一 rail 的两个实例互相比对会判"相等"（上游 `__self__` 可区分），双实例时只保留后装的一条。可接受：双实例本身已被 factory `alreadyProvidedByType` 判为 bug（`TaskPlanningRail` 2026-11-03 修过一例）。
2. **计数器按会话分桶**（上游实例属性）：正常"一会话一轮"语义等价；并发会话时不再互相偷预算。
3. **超时以 `StreamError` 哨兵承载**（Go channel 不能抛异常），消费方 `callLLMStream` 已等价处理。
4. **默认值只在 `NewModelClientConfig` 生效**：字面量构造的 `ModelClientConfig`（如 `react_helpers.go:110`）拿不到 300/60，等于关闭；上游 pydantic 处处补默认。记录但不在本批扩散修改。
5. **`_stream_chunks_emitted` 不移植**：上游仅供 `browser_move` 运行时统计（`react_agent.py:1125`），随 9.38-49 回填。

---

## 5. 实现清单

### 5.1 新增

| 文件 | 内容 |
| --- | --- |
| `internal/agentcore/harness/rails/model_anomaly.go` | 1:1 移植 §1.1 全部符号：类型 4、导出构造 1 + Option 5、方法 23（4 钩子 + `backoffDelay` + 检测/记账/压缩助手）、包级函数 16（上游同名）+ `funcPtrEqual`（Go 独有）、常量为 `const` 组 |
| `internal/agentcore/harness/rails/model_anomaly_test.go` | §7.1 A~D |
| `tests/integration/agentcore/model_anomaly/` | §7.2 三例（`//go:build integration` + `suite.Run`） |

### 5.2 修改

| 文件 | 改动 |
| --- | --- |
| `single_agent/interfaces/callback.go` | `StreamChunkInspectorFunc` 类型；extra key 常量 `_stream_chunk_inspectors`、`_model_visible_output_emitted`（字符串与上游一致）；`StreamChunkInspectors/SetStreamChunkInspectors/SetModelVisibleOutputEmitted/ModelVisibleOutputEmitted` |
| `single_agent/agents/react_model_call.go` | `callLLMStream` 增 `cbc` 形参；开头置 visible=false；逐 chunk 先跑检查器（error → 排空 → 返回）；写 `llm_reasoning`/`llm_output` 前置 visible=true |
| `foundation/llm/schema/config.go` | 两个 `*float64` 字段 + `WithStreamFirstChunkTimeout`/`WithStreamIdleTimeout`；`NewModelClientConfig` 默认 300/60 |
| `foundation/llm/schema/message_chunk.go` | `(*AssistantMessageChunk).CarriesOutputToken() bool` |
| `foundation/llm/model.go` | `Stream` 派生 cancellable ctx；包装 goroutine 用 `select` + timer 接收；超时 → cancel → 排空 → `NewAssistantMessageChunk("", WithChunkStreamError(err))`；错误文案逐字对齐 `LLM stream timeout: stream frame timeout: stage=..., timeout=..., chunk_count=..., token_chunk_count=..., first_token_elapsed|idle_elapsed=..., total_elapsed=..., model=...` |
| `model_clients/openai/client.go` | `HandleHTTPError` 两个分支都带 `http_status/provider_code/provider_type`，并补 `retry_after`/`retry_after_ms`/`x_should_retry` |
| `harness/harness_config/params.go` + `harness/factory.go` | `EnableModelAnomalyDetectionRail *bool`（nil→true）；`addDefaultRails` 顺序：`SafetyPromptRail` → anomaly → `TaskPlanningRail` |
| `swarm/server/adapter/deep_adapter_rails.go` | `buildModelAnomalyDetectionRail(configBase)`；插入为步骤 8（security 后、heartbeat 前），原 8~12 顺延 |
| `swarm/server/adapter/deep_adapter.go` | `modelAnomalyRail` 字段；`CreateDeepAgentParams.EnableModelAnomalyDetectionRail = &enabled` |
| `swarm/agents/harness/team/runtime_inheritance.go` | `BuildModelAnomalyDetectionRail(config)`；成员组装顺序插在 heartbeat 之前；`railWhitelist` 不加 |
| `resources/config.yaml` | `execution_guard` 段（键与默认值逐条对齐上游 1028-1048） |

---

## 6. 语义细节（实现必须逐条对上）

### 6.1 `BeforeModelCall`

顺序固定：① `maybeCompactOrBailoutToolLoop` ② 清空 `state{reasoning_content,content}` ③ 摘旧检查器 → 追加自身（`funcPtrEqual`）。

### 6.2 重复检测

- 两个字段各自维护 1024 字符尾窗；`tail = (旧+新)[-window:]`
- 先单字符（末字符非空白、连续 ≥ `single_char_repeat_count`），再多字符（`unitLen ∈ [min,max]`、`unit` 非全空白/非全同字符、`requiredCount = max(min_count, ceil(min_total/unitLen))`、按整块从尾回数）
- 错误：`BaseError(StatusModelCallFailed)`，文案 `LLM repeated stream output detected: field=<f>, unit=<pyRepr>, repeat_count=<n>`；`unit=` 用 `pyReprString`（先 `\r`/`\n` → 两字符转义，再按 Python repr 二次转义，单引号）
- 命中后**必须先排空 chunk channel 再返回 error**

### 6.3 `OnModelException` 判定顺序

`repeat`（marker `LLM repeated stream output detected`）→ `stream_timeout`（marker `LLM stream timeout` / `stream frame timeout`）→ `visible` 闸门 → `classifyTransientFailure`；非重试但 `retryAfter > 60s` 时打 warning。

### 6.4 瞬时分类优先级

`details.should_retry`（bool） > 7 条不可重试文本 > `x-should-retry:false` > `retry-after>60` > `x-should-retry:true` > 无状态码即重试 > `{408,409,429}`/`>=500` > 其余不重试。

状态码来源（Go）：`Details()["status_code"/"provider_code"]`、`Params()["http_status"]`。`Retry-After` 三种格式：`retry-after-ms`、纯数字秒、HTTP-date。

### 6.5 计数与退避

| 类别 | 上限 | 延误 |
| --- | --- | --- |
| repeat | `max_retries`（2） | `backoff[i]`，越界取末项（0.5/1/2） |
| stream_timeout | `max_retries`（2） | 同上 |
| transient | `transient_max_retries`（3） | `retry_after` 优先，否则 `2s × 2^n` |

用尽即删条目；`BeforeInvoke`/`AfterInvoke` 都 delete。

### 6.6 工具循环压缩

`enabled=false` → 返回；`ModelContext` nil → 返回；`GetMessages(0,false)` → `findToolLoopCompactRange`；命中则 `next=count+1`：

- `bailout_threshold>0 && next>=bailout` → 清计数 + warn + `cb.NewAbortError(bailoutMsg, exception.BuildError(StatusContextExecutionError, bailoutMsg))`
- 否则：取最后一轮区间序列化（`SetEscapeHTML(false)` + 缩进 2；空 `[]`）→ 双语警告 `UserMessage` → `compacted = msgs[:foldStart] + warning + msgs[foldEnd:]` → `SetMessages(compacted,false)` → 计数 = next → info 日志

`collectToolRounds`：`AssistantMessage` 有带 id 的 tool_calls → 收集 `pending_ids`；只吃紧邻匹配 `ToolMessage`；未清空视为不完整跳过；`fingerprint = sort((name, canonicalArgs))`（返回值不参与）。
`findToolLoopCompactRange`：`threshold<2` / 轮数不足 → nil；反向收集时**两个 break 条件**：指纹不同、`round.end != eligible[-1].start`（背靠背，A-09 核心）。

### 6.7 文案与语言

双语模板逐字复刻（含 `---` 与 `**请跳出重复工具调用**`）；语言：`Language()` → `zh*`/`cn`→cn、`en`→en、其余 cn、nil builder→cn；5 条日志改结构化字段但保持英文。

---

## 7. 测试策略

### 7.1 单元

- **A 重复检测**：短重复不触发、多字符触发、单字符触发（100/90 各一例）、尾窗滚动仍检出、空白后缀不触发、两字段独立
- **B `OnModelException`**：8 条优先级参数化、`retry-after` 三格式、`>60s` 不重试、visible 闸门（transient 挡/repeat 不挡）、耗尽归零
- **C 工具循环压缩**：11 例逐条搬 + 不完整轮次 + 会话隔离；含 `roundsSeparatedByUserInputDoNotCompact`、`onlyTheBackToBackTailIsFolded`、`bailoutOnThirdTrigger`
- **D 构造校验**：10 条约束参数化（`:345-372`）→ error
- **E 地基**：interfaces 检查器与 visible；agents 流循环（逐 chunk 调用、报错排空、visible 复位）；`foundation/llm` 首帧/idle 超时、framing 不重置、nil 禁用、超时后内层 ctx 取消；`openai.HandleHTTPError` 两分支 params

### 7.2 集成（`tests/integration/agentcore/model_anomaly/`）

`mockllm` 增量扩展（仅新增，不改现有行为）：`CreateRepeatStreamResponse(unit, times)`、`WithStreamStall(d)`。

1. 重复输出重试闭环：`StreamCallCount()==2` + 最终正常
2. 流帧超时重试闭环：`StreamIdleTimeout=0.05` + stall 0.2s → 错误含 `stream frame timeout` → 重试成功
3. 工具循环压缩端到端：5 轮相同 tool_calls → 警告 `UserMessage` 存在、消息条数下降、第 3 次 bailout 冒泡

跨度：`harness/rails` + `harness/deep_agent` + `single_agent/agents` + `single_agent/interfaces` + `foundation/llm` + `session`（≥3 子系统，过规则 7 两层门禁）。

### 7.3 验收

- `go test -race ./internal/agentcore/...` 全绿；改动行覆盖率 ≥85%
- `go vet` / staticcheck / gofmt 无新增告警（gofmt 用完整路径，注意 CJK）
- CI 四 job 全绿（规则 6：本地绿不算完）

---

## 8. 回填与台账（不丢历史）

| 位置 | 动作 |
| --- | --- |
| `IMPLEMENTATION_PLAN.md` 9.86 | 改 ✅ + 分项（重复检测 / 瞬时分类 / 流超时←2.17 / 工具循环压缩·背靠背 / bailout / 三处接线），保留 `589da12a` 原文 |
| 新增 `2.17` | `Model 门面流帧超时` → ✅，写 ⤴️ 已回填 9.86 与 2.14 |
| `9.3 DeepAgent Factory` | 追加 ⤴️ 9.86 |
| adapter 行（10.6.x） | 追加 ⤴️ 9.86 `execution_guard` |
| `25.x 团队成员` | 追加 ⤴️ 9.86 |
| `defect-plan-2026-11-04.md` A-09 | 改 ✅ + 判定实现位置 |
| `2026-11-04-upstream-defect-group-a.md:1860` | 勘误：实际落位 9.86（9.30 已是 ExploreAgent） |

---

## 9. 提交切分

1. `feat(agentcore): 流式分片检查器与可见输出标志`
2. `feat(llm): Model.Stream 流帧超时（首帧/空闲）`
3. `feat(harness): ModelAnomalyDetectionRail`
4. `feat(swarm): execution_guard 接线与集成测试`（含文档回填）

---

## 10. 本批不做

- `ToolCallResilienceRail`（上游默认 True，Go 全缺且计划未登记）→ 建议单开一行 9.90
- `CircuitBreakerRail`（`execution_guard.circuit_breaker`，默认关）→ 同上
- `_stream_chunks_emitted` 计数 → 随 9.38-49 BrowserMove

---

## 11. 风险

| 风险 | 处置 |
| --- | --- |
| 重复检测误判正常文本 | 全部用上游默认阈值；产品配置 90 保持；集成用例覆盖短重复不触发 |
| 重试导致界面重复吐字 | 与上游同：repeat/stream-timeout 优先于 visible 闸门，transient 受闸门保护；不改上游顺序 |
| 折叠删掉用户输入 | 背靠背判定 + 两条回归用例（`roundsSeparatedByUserInputDoNotCompact`、`onlyTheBackToBackTailIsFolded`） |
| rail 双实例 | factory `alreadyProvidedByType` + adapter 用同一 `enabled` 决定构建与传参；单测覆盖"显式提供则不重复追加" |
| 超时与父 ctx 取消混淆 | 超时判定优先 `ctx.Err()`：父 ctx 已取消则不构造超时错误 |
| 检查器在非流式路径被误用 | 检查器只在 `callLLMStream` 内消费；invoke 路径不遍历 |
