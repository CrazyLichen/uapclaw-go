# 上下文引擎 B 组缺陷对齐设计（2026-11-06）

> 依据：`docs/sync/defect-plan-2026-11-04.md` §3.B（上下文引擎与压缩，16 行缺陷）+ 2026-11-06 逐条核验与逐条讨论结论。
> 上游基线：agent-core `5a35c39f`（本地 `~/agent-core` 已更新至该提交）、jiuwenswarm `a2abceccf`。
> 执行规则：项目规则 8（重构不得留兼容壳）、规则 9（对齐 Python 结构忠实）。

---

## 0. 核验结论总表

| 编号 | 文档描述 | 核验结论 | 本轮处置 |
| --- | --- | --- | --- |
| B-01 | 工具结果窗口化未接入 | **未迁移**：`ToolResultWindowProcessor` 上游只在 `browser_agent.py:529` 装配；Go 的 `browser_agent.go`（181 行）是纯配置构造器，无浏览器上下文处理器栈 | ⤵️ 24.16 |
| B-02 | 压缩预算缺 guard | **存在**：Go `round_level_compressor.go:191/325/388` 仍绝对阈值 + `>`；无 `budget_guard` 等价物；卸载后占位预览不截断 | 本轮修（§2） |
| B-03 | 取消时未保存消息历史 | **主体已对齐**（`handleContextAbort` + `sanitizeAbortedTurnMessages`），残留：落盘用已取消 ctx | 本轮修（§3） |
| B-04 | 取消时保存清洗后上下文 | 同 B-03（同一函数） | 本轮修（§3） |
| B-05 | base64 图片计入 token | **方向相反的差异**：Go 图像按 0 token 计（`contentToString` 只取 text 分片），且无 `content_sanitize` | 本轮修（§4） |
| B-06 | usage 基线失效 | **整体缺失**：无 `context_engine/usage/`、无消息级 `usage_metadata` 基线 | 本轮修（§9） |
| B-07 | 溢出恢复 | **存在**：无引擎级 `recover_from_model_context_overflow`，溢出即整轮失败 | 本轮修（§5） |
| B-08 | 连续同工具触发异常 | **已对齐 ➖**：上游 HEAD 汇总为「工具名+入参」单规则（`model_anomaly_detection_rail.py:816`），Go `findToolLoopCompactRange` 同构 | 不改 |
| B-09 | 百炼溢出误判 | **存在**：Go 关键词表缺 `context limit`、`range of input length` | 并入 §5 |
| B-10 | 手动 compact usage | **存在**：无 `ContextUsageAnalyzer`，无 `post_compact` 快照 | 本轮修（§9） |
| B-11 | 规则压缩排除已读源文件 | **未迁移**：`processor/forked/offloader/rule_compression/` | ⤵️ 24.15 |
| B-12 | 3 字符 ≈ 1 token | **存在**：6 处 `/4` | 本轮修（§6） |
| B-13 | 摘要卸载冗余参数 | **存在**：4 个纯兼容字段 + `Validate` 误报 + 缺 assistant/tool_calls 守卫 | 本轮修（§7） |
| B-14 | 上下文窗口取值 | **部分存在**：算法已同构，缺 override/热切换，内置表陈旧 | 并入 §2 |
| B-15 | 压缩召回 query 结构化抽取 | **未迁移**：`forked/compressor/recall/{archive,retriever}.py` + `harness/tools/compression_recall.py` | ⤵️ 24.15 |
| B-16 | 压缩状态上报 | **存在**：缺 `modified_messages`；Go 前端 `compaction.ts:100` 已在读 | 本轮修（§8） |
| B-17 | 附件投递层 | **已落地 ✅**（S1，24.10） | 不改 |

---

## 1. 结构性前提：上游产品路径用的是 forked 压缩栈

| 层 | 阈值形态 |
| --- | --- |
| 产品配置 `react.context_engine_config.*_compressor_config` | `dialogue` / `current_round` / `round_level` 均为 `trigger_context_ratio`(0.8) + `min_target_context_ratio`(0.1)；`full_compact` 不在产品配置内 |
| `processor/forked/compressor/*`（rail 真正使用） | `trigger_context_ratio`(0.8) + `min_target_context_ratio`(0.1) |
| `processor/compressor/*`（**Go 移植的这套**） | `dialogue`/`current_round` 为 `tokens_threshold`；`round_level` 为 `trigger_context_ratio`(0.9)+`target_total_tokens`；`full_compact` 为 `trigger_total_tokens` |

Go 现状是用 base 类 + rail 层自研的 `applyCompressionRatios()`（`context_processor_rail.go:630`，注释自述「Python 侧无对应实现」）把绝对阈值收成 `min(绝对, ratio×window)` 来模拟上游行为。

**本轮决定**：先消灭这处自研分支（规则 8），把 base 压缩器的阈值形态对齐到**上游产品契约**（比例化）；forked 栈整体迁移另立 24.15，届时 base 版随之退场。

**若只改 round-level 而保留其余绝对阈值，则必须保留收缩逻辑**——否则 `round_level=230000`、`full_compact=180000` 对 128k 窗口模型永远达不到，等于压缩失效。因此本条与 B-02 必须同批。

---

## 2. B-02 + B-14：窗口来源与压缩预算（合并实施）

### 2.1 删除 Go 自创项（规则 8）

| 删除对象 | 位置 |
| --- | --- |
| `model_client_config.max_input_tokens`、`max_output_tokens` | `resources/config.yaml:174/177`、`deep_adapter.go:2019-2110/2194`、`web/app_web_handlers.go:966-970/2932-2967`、config parity 测试 |
| `ContextEngineConfig.CompressionTriggerRatio` / `CompressionTargetRatio` | `schema/config.go:36-37`、`deep_adapter.go:2047-2052`、`config.yaml:227-228` |
| `applyCompressionRatios()` / `shrinkThreshold()` | `context_processor_rail.go:160/630-670` |

### 2.2 新增（对齐上游）

1. `llm/schema.ModelRequestConfig.ContextWindow *int`——模型条目级窗口元数据；**组装厂商请求参数时必须排除**（对应上游 `base_model_client.py` 的 `exclude={"context_window"}`）。Go 侧请求参数由 `base_client.go:315-390` 以 map 形式拼装（基础参数 → `ModelConfig.Extra` → `params.Extra`），新增字段不进 `Extra` 即天然不外发；实现时需补一条单测断言该键不出现在最终请求体。
2. `ContextEngineConfig.ModelContextWindowTokensOverride int`、`EnableOpenRouterModelContextWindowTokens bool`、`OpenRouterRequestTimeout float64`。
3. `ContextUtils.BuildModelContextWindowTokens(...)`：`enable_openrouter…` 为真时拉取 `https://openrouter.ai/api/v1/models`（进程级 1h TTL 缓存、3s 超时、失败沿用旧缓存），随后用用户显式 map 覆盖。
4. `ContextEngine.UpdateModelContext(modelName string, contextWindowTokens int)` + `SessionModelContext.UpdateModelContext(...)`：推给引擎与池内**全部**已建 context（不重建）；语义：全局 `ContextWindowTokens` 仍最高优先，`override` 次之，无效值（≤0）清空 override 使自动解析恢复。
5. `ReActAgent` 装配两步（对齐 `_with_context_engine_model_name` / `_with_context_engine_model_window`）：`context_engine_config.model_name` 为空时补模型名；`ModelRequestConfig.ContextWindow > 0` 且与现有 override 不同时写入 override。热切换入口 `ReActAgent.UpdateModelContext`。

解析优先级不变（已同构）：`全局 context_window_tokens` > `model_context_window_tokens_override` > `model_context_window_tokens[model_name]` > 内置表 > `200000`。

### 2.3 内置窗口表按上游重写

`context_utils.go` 的 `ModelDefaultContextWindowTokens` 对齐上游 `MODEL_DEFAULT_CONTEXT_WINDOW_TOKENS`：GLM（`glm-5.1/5/5-turbo/4.7*`=200000、`glm-4-long`=1000000）、OpenAI（`gpt-5.5/5.4`=1050000、`gpt-5.4-mini/nano`=400000、`gpt-5*`=400000、`gpt-4.1*`=1047576）、DeepSeek（`deepseek-v4-*`=1000000）、Claude（`claude-opus-4-7/4-6`、`claude-sonnet-4-6`=1000000、`claude-haiku-4-5`=200000）、Gemini（`gemini-3-*-preview`/`2.5-*`=1048576）、Llama、Qwen（`qwen3-max`=262144、`qwen3.5-*`=1000000、`qwen-max`=262144、`qwen-plus/flash`=1000000、`qwen-turbo`=8192、`qwen-long`=1000000）、Kimi（`kimi-k2.5`=262144）、MiniMax（`M2.7/M2.5*`=204800）、Grok（`grok-4.3*`=1000000）。

### 2.4 压缩阈值形态

| 处理器 | 变更 |
| --- | --- |
| `DialogueCompressorConfig` | `TokensThreshold` → `TriggerContextRatio`(0.8) + `MinTargetContextRatio`(0.1) |
| `CurrentRoundCompressorConfig` | 同上 |
| `RoundLevelCompressorConfig` | `TriggerTotalTokens` → `TriggerContextRatio`（默认 0.9，产品配置 0.8）+ `MinTargetContextRatio`(0.1)；`TargetTotalTokens` 保留（上游同为绝对值） |
| `FullCompactProcessorConfig` | 不变（上游无 forked 版，仍是 `trigger_total_tokens`） |

配置文件键：`*_compressor_config.trigger_context_ratio` / `min_target_context_ratio`（对齐 jiuwenswarm `resources/config.yaml`）。

### 2.5 `processor/budget_guard.go`（新增）

对齐 `processor/budget_guard.py`：

- `EffectiveContextBudget(mc, modelConfig, callBudget) int`：候选 = 全局/override 窗口、`callBudget`、`ResolveContextMax(...)` 中取最小正值；全无效时返回解析值。
- `CountMessagesTokens(mc, messages) int`：tokenizer 优先，异常降级 `len//3` 累加。
- `TruncateMessageContentToFixedHeadTailIfOverTokenLimit(content, trigger, countFn, preservedSuffix) string`：超限时保留 2000 字符 head + 2000 字符 tail + 标记行，保留 `preservedSuffix`。
- 常量：`TruncatedByBudgetMarker`、`TruncatedSideMaxChars = 2000`。

### 2.6 RoundLevelCompressor 接入 guard

1. 触发：`threshold = max(int(EffectiveContextBudget × TriggerContextRatio), 1)`，比较改 `>=`（`TriggerAddMessages` 与 `TriggerGetContextWindow` 两处）。
2. 压缩调用预算：`IsUnderCompressionCallBudget` 改用 `EffectiveContextBudget(callBudget=CompressionCallMaxTokens)`。
3. 替换消息预算：新增 `IsReplacementUnderBudget(replacementMessages)`，在普通替换与合并替换两处，超预算即跳过该替换。
4. 压缩提示词中的 `compression_call_budget_limit` 改用 effective budget。

### 2.7 MessageSummaryOffloader 占位预览截断

卸载产出占位消息后，若其 token 数超过 `min(LargeMessageThreshold, EffectiveContextBudget)`，则做 head/tail 截断并保留 `[[OFFLOAD:...]]` 后缀（对齐 `_truncate_offload_placeholder_preview_if_large`）。

---

## 3. B-03 / B-04：取消态落盘

现状：`handleContextAbort` 与 `finally` 都直接用调用方的 ctx；`SaveContexts → checkpointer.Save(ctx) → kvStore.Pipeline(ctx).Execute(ctx)`，ctx 已取消时 SQL/KV 写入立即失败并被 `saveContexts` 的 warn 吞掉 → 清洗后的上下文未落盘，下一轮从旧 checkpoint 重建。

方案：新增 `saveContextsDetached(ctx, sess)`，内部 `context.WithoutCancel(ctx)`（等价 `asyncio.shield`），`handleContextAbort` 与 `finally` 共用；不设超时（写库耗时由驱动 busy timeout 兜底，保持与上游同构）。

测试：以**已取消的 ctx** 调 `handleContextAbort`，断言内存 state 与持久化 checkpointer 双路均写入成功、且 marker 消息存在。

---

## 4. B-05：图像 token 计费与文本扁平化净化

1. 新增 `context_engine/content_sanitize.go`：`SanitizeValueForText` / `SanitizeContentForText`；识别 `data:image/...;base64,`、`{data|data_url|url: "data:image/…"}`、`{source:{type:base64,data}}`、`{image_url:{url}}`，替换为 `[image:data-url-omitted]` / `[image:base64-omitted]`。
2. `llm/schema.ContentPart` 增加 `Width int`、`Height int`、`Dimensions string`（`"WxH"` 或 `[W,H]` 均接受），用于生产方标注缩图后真实尺寸。
3. `TiktokenCounter`：
   - `CountMessages` 对块列表内容分块计费：图像块 → 尺寸公式；文本块 → tokenizer；未知块 → sanitize 后 compact JSON；纯字符串 content 先 sanitize 再计数。
   - 图像定价：`TIKTOKEN_IMAGE_PLACEHOLDER_TOKENS` → `detail=="low"` 给 85 → 尺寸（显式字段 > PNG IHDR / GIF 逻辑屏幕 / JPEG SOF / WebP VP8X）→ `DEFAULT_IMAGE_PLACEHOLDER_TOKENS = 1445`；公式取 OpenAI tile（85 + 170/512px 块）与 Claude patch（28×28，上限 1568）的较大者。
4. `processor.EstimateContentTokens` / `EstimateMessageTokens` 走 sanitize（防 data URL 从 JSON 兜底分支进入）。
5. `harness/tools/filesystem/read_file.go`：
   - 工具级预算 `estimateImageTokens` 由 `base64长度/8` 改用同一尺寸公式（消除两套口径）；
   - 在缩放/压缩**之后**把实际宽高写入 multimodal 块的 `Width/Height`（上游只把原始尺寸写进 content 文本，本项为生产者侧领先，见 §10 偏差 D-3）。

---

## 5. B-07 + B-09：上下文溢出识别与恢复

1. `ContextEngine.IsContextOverflowError(err) bool`：短语 `context length` / `context window` / `maximum context` / `context limit` / `prompt is too long` / `prompt too long` / `input is too long` / `input too long` / `range of input length`；扫描异常字段（`message`/`body`/`details`/`error`/`errors`/`response`/`code`/`type`/`param`/`status_code`/`status`/`text`/`content`/`cause` 及错误链）。
2. `ContextEngine.RecoverFromModelContextOverflow(mc, streaming bool, streamChunksEmitted int) bool`：`compressContext()` 返回 `"compressed"` 才算恢复；`streaming && emitted > 0` 直接返回 false（避免重发已发前缀）。
3. `react_model_call`：模型调用失败且可重试耗尽时，若 `IsContextOverflowError` → `RecoverFromModelContextOverflow` → 仅重试一次本轮；`DeepAgent` 侧同入口。
4. `message_summary_offloader.isContextOverflowError` 删除私有关键词表，改为复用引擎层判定。

---

## 6. B-12：token 降级口径 4 → 3

| # | 位置 | 改法 |
| --- | --- | --- |
| 1 | `token/tiktoken_counter.go:226` `fallbackCount` | `len/3` |
| 2 | `context/session_model_context.go:1216` 单消息降级 | `len/3` |
| 3 | `context/session_model_context.go:1241` 工具降级 | `len/3` |
| 4 | `context/processor_state_recorder.go:296` | `ceil(chars/3)` |
| 5 | `interface/types.go:462` | `len/3` |
| 6 | `interface/types.go:485` | `len/3` |

同步把「对齐 Python len//4」这类过时注释改为 `len//3`。

---

## 7. B-13：摘要卸载冗余参数

1. 删 `MessageSummaryOffloaderConfig` 的 `MessagesThreshold` / `TokensThreshold` / `MessagesToKeep` / `KeepLastRound` 及常量 `msoDefaultTokensThreshold`。
2. `Validate()` 改为仅 `applyMSODefaults()`，不再校验 messages 组合（对齐上游 `_validate_config` 直接 return）。
3. `shouldOffloadMessage` 增加守卫：`AssistantMessage` 且 `len(ToolCalls) > 0` → 不卸载。
4. `resources/config.yaml` 删除 `message_summary_offloader_config` 下 4 个键。
5. 父类 `message_offloader.go` 的同名字段**保留**（上游保留）。

---

## 8. B-16：压缩状态 `modified_messages`

1. `schema/context_state.go` 新增 `ContextCompressionModifiedMessage{MessageID, Role, ToolCallID, OffloadHandle, OffloadType}`，`ContextCompressionState` 增 `ModifiedMessages []ContextCompressionModifiedMessage`（JSON `modified_messages`）。
2. `processor_state_recorder.go` 新增 `buildModifiedMessages(before, after)`：
   - 以 `context_message_id` 建 before 索引（重复 id 取首个）；
   - 遍历 after：id 缺失、`original == message`、不在 before → 跳过；
   - 新卸载判定：after 实现 `schema.Offloadable` 且（before 非卸载 或 handle 不同）；
   - 非新卸载且内容未变 → 跳过；
   - 其余记一条（`OffloadHandle/OffloadType` 仅在新卸载时填）。
3. `stateToMap()` 增加 `modified_messages` 数组（snake_case 键：`message_id`/`role`/`tool_call_id`/`offload_handle`/`offload_type`）。
4. 消费方已就绪：`frontend/src/features/trajectory/trajectory/compaction.ts:100`。

---

## 9. B-06 + B-10：上下文用量分析子系统（`context_engine/usage/`）

### 9.1 现状与影响

Go 侧完全缺失该子系统；`swarm/agents/harness/common/rails/stream_event_emit.go` 自行拼了 `context.usage` 事件，载荷只有 `{rate, context_max, tokens_used}`。
而 Go 的 web 前端（迁自上游）要求 `event_type="context.usage"` + `schema_version="context-usage.v1"` + `phase ∈ {post_call, post_compact}` + `context_window{limit_tokens,input_tokens,occupancy_rate}` + `parts{category→{category,tokens,percentage_of_window}}` + `session_kv_cache_hit_rate` 等（`features/contextUsage/contextUsageModel.ts:36-80`），否则整条快照被丢弃 —— **上下文用量指示器当前是失效状态**。

### 9.2 迁移内容（文件级 1:1）

| Python | Go |
| --- | --- |
| `usage/models.py`（195） | `usage/models.go`：`ContextCategory`(system_prompt/tools/skills/messages/memory)、`ContextPartUsage`、`ContextWindowUsage`、`RequestKVCacheUsage`、`SessionKVCacheUsage`、`ContextUsageMeasurement`、`ContextUsageSnapshot`（`schema_version=context-usage.v1`，含 team/member/depth/delegation 等归属字段） |
| `usage/provider_usage.py`（141） | `usage/provider_usage.go`：从 `UsageMetadata` 提取输入/缓存读/写/未命中，产出 `RequestKVCacheUsage` 与 `SessionKVCacheUsage` 聚合 |
| `usage/analyzer.py`（639） | `usage/analyzer.go`：`ContextUsageAnalyzer`（parts 拆分、provider 权威总量、差额按比例归因、`pre_call`/`post_call`/`post_compact` 快照） |
| `usage/session_aggregator.py`（184） | `usage/session_aggregator.go`：会话级 KV cache 聚合 |
| `usage/__init__.py`（34） | `usage/doc.go` + 导出 |

### 9.3 消息级 usage 基线（B-06）

1. 消息 schema 增加 `UsageMetadata` 承载（累积输入 token 基线）；`ContextUtils.InvalidateUsageMetadata` / `HasValidUsageMetadata` 等价物。
2. `SessionModelContext` 五处失效点（对齐 `context.py:260/354/368/524/540`）：`clear_messages`、`add_messages` 两条快路径、`set_messages`、重载/回滚路径。
3. 纯 append 且保留前缀未被改写时**不失效**（`_invalidate_usage_if_append_changed_retained_prefix` 等价：比较 append 前后保留前缀是否仍是同一前缀）。
4. 统计口径：优先取最后一条带合法 `usage_metadata` 的 AssistantMessage 的累积输入 token，否则回退 tokenizer 估算。

### 9.4 接入与载荷切换（B-10）

1. `react_model_call`：模型调用前 `pre_call` 快照、调用后 `post_call` 快照；手动 `CompressContext` 完成后 `post_compact` 快照。
2. `stream_event_emit.go` 删除自拼载荷，改发 `ContextUsageSnapshot`（含 `phase/request_id/product_session_id/session_id/agent_id/team_id/member_name/depth/agent_path` 等归属字段）。
3. `session.WriteStream` 载荷与回调 payload 保持一致；轨迹/历史恢复链路（`historyRestore.ts`）能回放最新快照。

---

## 10. 已知偏差登记

| 编号 | 偏差 | 理由 | 回收条件 |
| --- | --- | --- | --- |
| D-1 | base 压缩器（dialogue/current_round/round_level）采用**上游产品契约**的比例阈值，而上游 base 类仍是绝对阈值 | 产品配置面已全比例化；不比例化则小窗口模型永不触发压缩 | forked 压缩栈（24.15）迁移完成后删 base 版 |
| D-2 | `context_window` 元数据链路在 `ReActAgent.Configure` 装配，而非上游 `_with_context_engine_model_*` 两个类方法 | Go 无类方法继承体系，等价逻辑内联到 Configure | 无（等价实现） |
| D-3 | `read_file` 把**缩放后真实尺寸**写入 multimodal 块，上游仅写原始尺寸文本 | 上游字段本就是为缩图生产者预留，Go 缩图在 read_file 内 | 上游出现标注生产者时对齐其字段名 |
| D-4 | `saveContextsDetached` 不加超时 | 保持与 `asyncio.shield` 同构；写库耗时由驱动 busy timeout 兜底 | 若出现取消路径长时间阻塞再补超时 |

---

## 11. 测试策略

| 组 | 单测 | 集成 |
| --- | --- | --- |
| §2 | budget_guard 全分支；round-level 触发/预算/replacement；resolve 优先级 + override 热切换；内置表抽样；OpenRouter 拉取（httptest + TTL + 失败降级） | 上下文引擎 + harness rail + config 三系统联动（窗口来源 → 阈值 → 触发） |
| §3 | 取消态 ctx 落盘（内存 + 持久化双路） | — |
| §4 | sanitize 各形态；图像尺寸解析（PNG/GIF/JPEG/WebP/坏数据/显式字段/detail low/env）；read_file 缩放后标注 | read_file → 上下文 → 计数（跨工具/上下文两系统，另加 LLM schema 消费） |
| §5 | 9 短语 + 字段/因果链；streaming 守卫；只重试一次 | ReAct 模型调用失败 → 压缩 → 重试链路 |
| §6 | 6 处降级值断言 | — |
| §7 | 配置字段删除后校验；assistant+tool_calls 守卫 | — |
| §8 | 5 类（内容改写/新卸载/删除/新增/无 id）+ stateToMap 键名 | 压缩 → 状态事件 → 前端契约快照 |
| §9 | analyzer parts/归因/三时点；provider usage 解析；会话聚合；五处失效点 | 模型调用 → 快照事件 → web 事件契约（含 schema_version/phase/parts） |

覆盖率：改动行 ≥85%（门禁口径）；新增 `usage/` 包整包 ≥85%。

---

## 12. 分批与提交顺序

1. **P0 数据**：§3（取消态落盘）——最小、独立、可先合。
2. **窗口与预算**：§2 全量（含删除项、web 面板、config.yaml、parity 测试）——改动面最大，单独一批，先绿再动其他。
3. **计数与净化**：§6（6 处）→ §4（sanitize + 图像计费 + read_file）。
4. **恢复链**：§5。
5. **零散对齐**：§7 → §8。
6. **用量子系统**：§9（新增包 + 接入 + 载荷切换）单独一批，最后做（依赖前面窗口解析与预算口径稳定）。

每批完成后：`go build ./...`、`go vet ./...`、包级 `-race` 单测通过；改动涉及集成断言时跑 `go run ./cmd/integration-audit -verify`。

---

## 13. 明确不做

| 项 | 去向 |
| --- | --- |
| B-01 工具结果窗口化 | ⤵️ 24.16（browser 子代理上下文栈） |
| B-11 规则压缩排除已读源文件 | ⤵️ 24.15（forked 压缩栈迁移） |
| B-15 压缩召回 query 结构化抽取 | ⤵️ 24.15 |
| B-08 | 已对齐，不改 |
| deployment 级 `deploy/*.yaml` 配置样例同步 | 随发布批次处理 |
