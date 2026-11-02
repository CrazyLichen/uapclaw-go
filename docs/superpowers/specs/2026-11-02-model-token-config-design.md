# 模型 Token 配置可配化（max_input_tokens / max_output_tokens）设计

- 日期：2026-11-02
- 状态：待评审
- 关联问题：模型配置界面只有基础信息，无输入/输出 token 配置；上下文窗口固定显示 200K；后端值硬编码

## 1. 背景

### 1.1 现状事实（已核实）

**模型条目可配字段白名单**只有 9 个：`model_name` / `api_base` / `api_key` / `model_provider` / `temperature` / `is_default` / `timeout` / `verify_ssl` / `alias`。

- 前端表单字段元组：`ConfigPanel/index.tsx:1098`、`:1145`
- 写侧白名单：`config_apply.go:615-629`（`buildModelsDefaultsFromFrontend`）
- Python 同构：`app_web_handlers.py:828-843`（`_build_models_defaults_from_frontend`）

**窗口值完全由代码决定**，链路为 `ResolveContextMax` 四级优先级（`context_engine/context/context_utils.go:112`，Python `context_utils.py:103`）：

1. `ContextEngineConfig.ContextWindowTokens`（全局单值，含输入输出）
2. `ContextEngineConfig.ModelContextWindowTokens[model_name]`（自定义映射）
3. 内置表 `ModelDefaultContextWindowTokens`（23 个裸模型名，`context_utils.go:31-55`）
4. `DefaultContextMaxTokens = 200000`

三个断链导致 1、2 实际永不可达：

| 断链 | 位置 | 事实 |
|---|---|---|
| 无调用点 | `single_agent/config/agent_config.go:258` | `WithContextEngine()` 全仓库只有定义，无调用 |
| 无配置键 | `resources/config.yaml:177-179` | `react.context_engine_config` 仅有 `enable_kv_cache_release` |
| 无 UI 通道 | `web_handlers.go:722-724` | `config.get` 只透传 `enabled` 与 `enable_kv_cache_release` |

**压缩阈值硬编码**，且与窗口值零耦合：

| 阈值 | 值 | 位置 |
|---|---|---|
| `MessageSummaryOffloader.TokensThreshold` | 10000（preset 默认；Python 模板覆盖为 60000） | `context_processor_rail.go:313-320` |
| `DialogueCompressor.TokensThreshold` | 100000 | `context_processor_rail.go:325` |
| `CurrentRoundCompressor.TokensThreshold` | 100000 | `context_processor_rail.go:341` |
| `RoundLevelCompressor.TriggerTotalTokens` / `TargetTotalTokens` | 230000 / 160000 | `context_processor_rail.go:355-356` |
| `FullCompactProcessorConfig.TriggerTotalTokens` | 180000 | `full_compact_processor.go:248` |

后果：32K 窗口的模型永远达不到 230000，压缩永不触发。

**界面「上下文窗口」的口径**（`ToolPanel/index.tsx:245-257`）：

- 分子 `tokens_used` = 最后一次模型响应 `usage_metadata.total_tokens`（输入+输出，`stream_event_emit.go:206-210`）
- 分母 `context_max` = `ResolveContextMax` 结果（当前 = 200000 兜底）

**`max_tokens` 现状**：schema 有字段（Python `config.py:86`、Go `schema/config.go:361`），但无任何配置入口；请求侧语义为「请求级优先、模型配置兜底」（`model_clients/base_client.go:320-321`、`:341-342`）。今天实际值全部是代码硬编码的请求级参数：`common/hooks/executor.go:552`=1024、`server/runtime/agent_config_llm.go:103`=2000、`adapter/deep_adapter_evolution.go:551`=300、`video_understanding.go:42`=2048；主对话 ReAct 循环不传（服务端默认）。

**Go 主 Agent 不读 processor 配置**（Python 会读）：

| 路径 | 是否读 `react.context_engine_config.*_config` | 位置 |
|---|---|---|
| Python 主 Agent | 是 | `interface_deep.py:355-374` `_build_context_processor_rail(self._config_cache)` |
| Go 团队路径 | 是 | `team/runtime_inheritance.go:358-386` |
| Go 主 Agent | 否 | `deep_adapter_rails.go:789` 零参 `NewContextProcessorRail()` |

覆盖机制在 Go 里是完整的（`merge_config.go:93-163`：`ConfigOverrides` 非空走字段级 `MergeConfigWithOverrides`），缺的只是「adapter 从不把 yaml 传进去」。

## 2. 目标

1. 前端模型配置可填输入上限与输出上限，保存后回显。
2. `config.yaml` 手改同样生效。
3. 值真实影响运行时：界面百分比、压缩触发时机、请求 `max_tokens`。
4. 压缩阈值随输入上限联动，但**只下调不放大**（大窗口模型行为不变）。
5. 与 Python 保持同构（同一份 yaml 语义、同一套优先级与公式）。

## 3. 决策

| # | 决策 | 取值 | 理由 / 被否决项 |
|---|---|---|---|
| D1 | 字段位置 | `models.defaults[].model_client_config` | 用户指定。技术上安全：Go 未知键只进 `ConfigExtra`（仅参与配置 JSON 序列化，`schema/config.go:294-302`），Python `ModelClientConfig` 是 `extra: allow`，均不进请求体 |
| D2 | 键名 | `max_input_tokens` / `max_output_tokens` | 用户指定，字面直白 |
| D3 | 语义 | `max_input_tokens` = 输入侧可用额度（作分母）；`max_output_tokens` = 输出上限 | 两个上限各自独立，不互相推导；直接对应厂商文档的两个数字 |
| D4 | 运行时落点 | 不新增结构体字段：`ContextEngineConfig.ModelContextWindowTokens[model_name]`、`ModelRequestConfig.MaxTokens` | 既有字段语义正好匹配，无需扩展 `ContextEngineConfig` |
| D5 | 让值「已知」 | `buildModelFromEntry` 的 `knownKeys` 加两键 + 两条消费分支 | 只加白名单＝键被彻底丢弃（既不入 Extra 也无分支消费），必须配消费分支 |
| D6 | 百分比口径 | 分子 = `usage_metadata.input_tokens`，无 usage 时回退本地估算；分母 = `max_input_tokens` | 分子分母同量纲；本地估算本身是输入侧估算，两条回退路径一致。否决「分子保持 total_tokens」（长输出虚高，两条路径量纲不一致） |
| D7 | 阈值规则 | `实际阈值 = min(yaml 绝对阈值, ratio × max_input_tokens)` | 只下调不放大。否决「严格按比例」（1M 窗口会从 230K 触发变 900K，属行为变更） |
| D8 | 比例键 | 全局 2 键：`react.context_engine_config.compression_trigger_ratio`（0.85）、`compression_target_ratio`（0.55） | 压缩策略是 agent 级行为，不是模型属性；放 `mcc` 会随模型条目重复。否决 per-processor ratio（同一件事配 4~5 遍） |
| D9 | processor 通道 | 修复 Go 主 Agent 四段读取（parity）+ 新增 `full_compact_processor_config` 段（Go 与 Python 同时新增） | FullCompact 当前无任何 yaml 通道，与「不硬编码」冲突 |
| D10 | 多条目冲突 | 同 `model_name` 多条目时：`is_default=true` 优先，否则取首个非零值，值不一致打 `Warn` | 多 key 轮换场景下窗口应一致，不一致属配置错误但不应阻断启动 |
| D11 | 全局覆盖键 | `ContextEngineConfig.ContextWindowTokens`（优先级 1）不使用，保持 0 | 用户明确不动 `react` 段的窗口键；该字段保留给未来运行时显式覆盖 |
| D12 | 兜底链 | 保留内置表（三级）与 200000（四级） | 未配置窗口的老用户行为不变；避免「未填就没值」导致分母为 0 |
| D13 | 模板基线 | `resources/config.yaml` 的 `react.context_engine_config` 整段对齐 Python 模板（含 `enabled: true` / `enable_reload: true` / 四段），再追加 2 个 ratio 键 | 见 §4.4：Go 模板缺失该段导致「压缩默认关闭 + processor yaml 未生效」双重偏差，属 parity 修复；不修则本次阈值改动在默认配置下休眠 |

## 4. 配置契约

### 4.1 改前

```yaml
models:
  defaults:
    - model_client_config:
        api_base: ${API_BASE}
        api_key: ${API_KEY}
        model_name: ${MODEL_NAME}
        client_provider: ${MODEL_PROVIDER}
        timeout: 1800
        verify_ssl: false
        custom_headers: {}
      model_config_obj:
        temperature: 0.95
      is_default: true
react:
  context_engine_config:
    enable_kv_cache_release: false
```

### 4.2 改后

```yaml
models:
  defaults:
    - model_client_config:
        api_base: ${API_BASE}
        api_key: ${API_KEY}
        model_name: ${MODEL_NAME}
        client_provider: ${MODEL_PROVIDER}
        timeout: 1800
        verify_ssl: false
        custom_headers: {}
        # 输入上限（token）：该模型单次请求可接受的输入量，用作上下文窗口分母。
        # 0 / 缺省 = 未知，回退内置表或 200000。
        max_input_tokens: 0
        # 输出上限（token）：作为请求 max_tokens 的默认值；请求级显式传值优先。
        # 0 / 缺省 = 不限制，交由服务端默认。
        max_output_tokens: 0
      model_config_obj:
        temperature: 0.95
      is_default: true
react:
  context_engine_config:
    # 全段照抄 Python 模板 (jiuwenswarm/resources/config.yaml:269-303)，仅追加两个 ratio 键。
    # 关键项如下，其余字段见 §4.4：
    enabled: true
    enable_reload: true
    enable_kv_cache_release: false
    # 新增：压缩水位比例（相对 max_input_tokens）；缺省/0 = 不按比例，仅用绝对阈值
    compression_trigger_ratio: 0.85
    compression_target_ratio: 0.55
    # 各 processor 绝对阈值（天花板），值取自 Python 模板
    message_summary_offloader_config:
      tokens_threshold: 60000
      large_message_threshold: 60000
    dialogue_compressor_config:
      tokens_threshold: 100000
    current_round_compressor_config:
      tokens_threshold: 100000
    round_level_compressor_config:
      trigger_total_tokens: 230000
      target_total_tokens: 160000
    # 新增段（session_memory 路径预设），Python 侧当前无此段
    full_compact_processor_config:
      trigger_total_tokens: 180000
```

### 4.4 模板基线与 Python 对齐（本设计的前置修复）

Go 的 `resources/config.yaml:177-179` 只有 `context_engine_config.enable_kv_cache_release` 一个键，而 Python 的 `jiuwenswarm/resources/config.yaml:269-303` 有完整一段：`enabled: true`、`enable_reload: true` 和四段 processor 配置（`message_summary_offloader_config` / `dialogue_compressor_config` / `current_round_compressor_config` / `round_level_compressor_config`）。

这造成两个实际偏差：

1. **默认开关相反**：Go 模板缺 `enabled` → `config.get` 缺省 `"false"` → `ContextProcessorRail` 不注册 → 压缩默认不跑；Python 模板 `enabled: true` → 默认跑。
2. **preset 阈值被 yaml 覆盖的程度不同**：Python 部署里 yaml 覆盖了 preset（如 offloader `large_message_threshold` 10000 → 60000、`tokens_threshold` 60000），Go 主 Agent 不读 yaml → 一直用 preset 硬编码值。

因此 T1 不是"加两个键"，而是**把 Python 模板的这一整段搬过来**（值逐字段照抄，仅追加 2 个 ratio 键），配合 A2 接通读取。前提是 `enabled: true` 也一并搬——否则本次所有阈值改动在默认配置下都是休眠的。

### 4.3 比例取值的来源与影响

比例取 `0.85` / `0.55`（用户指定），不是由现值反推。与现值的关系：

| 现值 | ÷256000 | 采用比例 | 256K 窗口下的实际结果 |
|---|---|---|---|
| 230000 | 0.898 | `trigger: 0.85` | `min(230000, 217600) = 217600`（比今天提前 5.4%） |
| 160000 | 0.625 | `target: 0.55` | `min(160000, 140800) = 140800`（比今天压得更深 12%） |
| 180000 | 0.703 | 说明见 §6.4（FullCompact 复用 trigger 比例） | `min(180000, 217600) = 180000`（不变） |

**行为分界点**：trigger 回到绝对值需 `window ≥ 230000/0.85 ≈ 270588`；target 需 `window ≥ 160000/0.55 ≈ 290909`。因此 256K 窗口的模型也会被收紧（不再“完全一致”），290909 以上才与今天完全一致。这是选择 0.85/0.55 的必然结果，属预期行为变更。

## 5. 数据流与改动清单

```
① UI 表单 ─② 后端解析 ─③ 写 YAML ─④ 读回填 ─⑤ 装配搬运 ─⑥ 运行时消费
                                                       ├─ context.usage 百分比
                                                       ├─ 压缩阈值（5 个 processor）
                                                       └─ 请求 max_tokens
```

### 5.1 Go 后端（8 处）

| # | 文件:位置 | 改动 |
|---|---|---|
| B1 | `swarm/gateway/channel_manager/web/config_apply.go:615-629` | `buildModelsDefaultsFromFrontend` 解析 `max_input_tokens` / `max_output_tokens` 写入 `entry["model_client_config"]`；非法值返回 `ConfigBadRequest` |
| B2 | `web/web_handlers.go:960-1001` | `handleModelsList` 展平 map 补两字段（回填界面） |
| B3 | `adapter/deep_adapter.go:2189` | `knownKeys` 加 `"max_input_tokens"` / `"max_output_tokens"` |
| B4 | `adapter/deep_adapter.go:2128-2137` | `mcc["max_output_tokens"] > 0` → `llmschema.WithMaxTokens(n)` |
| B5 | `adapter/deep_adapter.go:2028` `deepAgentContextEngineConfig` | 签名加 `configBase` 入参；遍历 `config.GetDefaultModels(configBase)` 收集 `model_client_config.max_input_tokens` → `result.ModelContextWindowTokens[model_name]`；`result.ModelName` = 当前生效模型名 |
| B6 | `common/rails/stream_event_emit.go:186-221` | 分母改从 agent 的 `ContextEngineConfig` 取（`ResolveContextMax(ceConfig.ModelName, ceConfig.ContextWindowTokens, ceConfig.ModelContextWindowTokens)`）；分子按 §6.5 的回退顺序取 `InputTokens` |
| B7 | `context_engine/context/session_model_context.go:1058-1068` + `:1192` | `statMessages` 按 §6.5 的回退顺序取 `InputTokens`；`resolveContextMax` 已走 `ResolveContextMax`，无需改 |
| B8 | `harness/rails/context_engineer/context_processor_rail.go:87-128`（`Init`） | **在 `MergeProcessors` 之后**遍历 `allProcessors`，对五个 processor 的阈值统一做 `min(绝对, ratio×window)` 收口（顺序要求见 §6.6） |

### 5.2 Go 多装配点（3 处）

| # | 文件:位置 | 改动 |
|---|---|---|
| A1 | `adapter/deep_adapter.go:595`（CreateInstance）、`:2830`（makeDeepAgentConfig） | 调用 B5 时传 `configBase`；两处均已有该变量 |
| A2 | `server/adapter/deep_adapter_rails.go:789` | `buildContextProcessorRail` 读 `react.context_engine_config` 四段 + 新增 `full_compact_processor_config` → `WithUserProcessors`（修复 parity 断链） |
| A3 | `team/config_loader.go:267` `buildDefaultModelDict` / `team/runtime_inheritance.go:358` | 团队成员复用同一窗口收集 helper；`agent_teams/models/pool.go:353` 已有 `metadata.request.max_tokens`，需补窗口 |

### 5.3 前端（3 处）

| # | 文件:位置 | 改动 |
|---|---|---|
| F1 | `types/index.ts:32` `ModelEntry` | 加 `max_input_tokens?: number` / `max_output_tokens?: number` |
| F2 | `components/ConfigPanel/index.tsx:1098`、`:1145`、`:236-249` | 字段元组加两项；数字输入（`inputMode="numeric"`）；校验分支：非负整数 |
| F3 | `i18n/locales/{zh,en}.json` | 两个标签 + 非法值提示 |

前端无单测框架（`package.json` 无 test script），验证方式为 `tsc` + `vite build` + 手工冒烟。

### 5.4 配置模板（1 处）

| # | 文件:位置 | 改动 |
|---|---|---|
| T1 | `resources/config.yaml:126-179` | 按 §4.2/§4.4 把 `react.context_engine_config` 整段对齐 Python 模板（`enabled: true` + `enable_reload: true` + 四段），并追加 2 个 ratio 键；models 段加两个 token 键 |

### 5.5 Python 同构改动（6 处）

| # | 文件:位置 | 改动 |
|---|---|---|
| P1 | `jiuwenswarm/gateway/channel_manager/web/app_web_handlers.py:784-857` | `_build_models_defaults_from_frontend` 白名单加两键 |
| P2 | 同文件 `_models_list`（L1023-1055） | 回填两字段 |
| P3 | `jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:309-320` | `_deep_agent_context_engine_config` 收 `max_input_tokens` → `model_context_window_tokens` |
| P4 | 同文件 `:1482-1490` `_build_model_from_entry` | `mcc.max_output_tokens` → `ModelRequestConfig(max_tokens=...)` |
| P5 | 同文件 `:4927-4966`、`_emit_context_usage` | 分子改 `input_tokens`；分母取 DeepAgent 返回值 |
| P6 | `openjiuwen/core/context_engine/processor/context_processor_rail.py` 等 | 五段 yaml 通道 + `min(绝对, ratio×window)` 推导 |

Python 侧需同一批次提交，否则两端行为分叉（同一份 config.yaml 在两个实现下表现不同）。

## 6. 阈值推导规则

### 6.1 公式

```
window = ResolveContextMax(model_name, ceConfig.ContextWindowTokens, ceConfig.ModelContextWindowTokens)
trigger_effective = min(绝对 trigger, round(compression_trigger_ratio × window))
target_effective  = min(绝对 target,  round(compression_target_ratio  × window))
```

**参与收口的字段**（都与「整个上下文总量」同量纲）：

| processor | 字段 |
|---|---|
| `MessageSummaryOffloader` | `tokens_threshold` |
| `DialogueCompressor` | `tokens_threshold` |
| `CurrentRoundCompressor` | `tokens_threshold` |
| `RoundLevelCompressor` | `trigger_total_tokens` → trigger；`target_total_tokens` → target |
| `FullCompactProcessor` | `trigger_total_tokens` → trigger |

**不参与收口的字段**（量纲不同，缩放会破坏语义）：

- `MessageSummaryOffloader.large_message_threshold`（单条消息阈值）
- `DialogueCompressor.compression_target_tokens`、`CurrentRoundCompressor.compression_target_tokens`（单次压缩输出长度）
- `current_round_compressor_config.min_selected_tokens_for_compression`（单轮选择下限）
- 各种 `messages_to_keep` / `keep_recent_messages` / `*_pass_target_tokens`（消息条数或单阶段预算）

### 6.2 三层回退

| 条件 | 结果 |
|---|---|
| 有 window 且对应 ratio > 0 | `min(绝对, ratio × window)` |
| 有 window、ratio 缺省/0 | 绝对阈值（今天行为） |
| 无 window（回退到 200000 也算有） | 绝对阈值 |

### 6.3 验算

| 窗口 | Dialogue（绝对 100000） | RoundLevel（绝对 230000 / 160000） | 对比今天 |
|---|---|---|---|
| 32K | 27200 | 27200 / 17600 | 以前永不触发 → 现在按窗口触发 |
| 128K | 100000（ratio 值 108800 > 绝对，min 命中绝对） | 108800 / 70400 | 收紧 |
| 256K | 100000 | 217600 / 140800 | 收紧（触发早 5.4%，目标深 12%） |
| 512K | 100000 | 230000 / 160000 | **完全一致** |
| 1M | 100000 | 230000 / 160000 | **完全一致** |

### 6.4 FullCompact 的处理

`FullCompactProcessor` 今天 `TriggerTotalTokens=180000`（约 0.7×256K），与其他 processor 不同比例。为保持单套 ratio 的设计，取 `min(180000, 0.85×window)`：

| 窗口 | 结果 | 对比今天 |
|---|---|---|
| 32K | 27200 | 收紧 |
| 200K | 170000 | 收紧 |
| 256K | 180000（ratio 值 217600 被 min 掉） | 一致 |
| ≥212K | 180000 | 一致 |

### 6.5 百分比分子的回退顺序

```
input_tokens = UsageMetadata.InputTokens            // 有真实 usage 时（首选）
             → UsageMetadata.TotalTokens            // 有 usage 但 InputTokens 为 0（部分兼容端点只回 total）
             → 本地 token 计数（输入侧估算）          // 无 usage_metadata
分母固定为 window（§6.2 解析结果），不再用 200000 常量。
```

### 6.6 推导与 yaml 覆盖的顺序要求

阈值收口**必须在 `MergeProcessors` 之后**执行，原因：yaml 里的绝对阈值是「天花板」，需要参与 `min` 计算；若在 merge 之前推导，yaml 的原始值会在 merge 阶段直接覆盖已收口的结果，绕过 `min`（例：32K 窗口下用户写 `trigger_total_tokens: 50000`，正确结果是 `min(50000, 27200)=27200`，错误顺序会得到 50000）。

因此 `Init` 的执行顺序为：`buildPresetProcessors`（默认值）→ `MergeProcessors`（合入 yaml 绝对阈值）→ 阈值收口（`min(绝对, ratio×window)`）。

### 6.7 校验

- `0 < compression_target_ratio < compression_trigger_ratio ≤ 1`，否则配置加载报错
- 绝对阈值必须 > 0（沿用现有 `Validate`）
- 推导后若 `target_effective >= trigger_effective`（极窄窗口），`target_effective = round(trigger_effective × 0.7)` 兜底并打 `Warn`

## 7. 错误处理与边界

| 场景 | 处理 |
|---|---|
| 写侧两字段非整数 / 负数 | 返回 `ConfigBadRequest`（静默回默认会让人误以为保存成功） |
| 读侧两字段非法（手改 yaml） | 忽略该值 + `Warn`，走兜底链 |
| `max_input_tokens` 未配置 | 走内置表 → 200000（D12） |
| `max_output_tokens` 未配置 | 不设 `MaxTokens`，维持请求级/服务端默认 |
| 同 `model_name` 多条目窗口不一致 | `is_default` 优先，其次首个非零；不一致打 `Warn`（D10） |
| 窗口小于 1 层系统提示词 | 不特判，压缩逻辑本就有 `minimalTokens` 保护（`round_level_compressor.go:1441`） |
| `react.context_engine_config.enabled=false` | 整个 `ContextProcessorRail` 不注册（`deep_adapter_rails.go:935-957`），所有阈值与比例均不生效；窗口值仍影响 `context.usage` 显示 |

## 8. 兼容性

- **老配置零改动**：不填两字段 → `ModelContextWindowTokens` 为空 → 走内置表/200000，与今天完全一致。
- **不填 ratio**：绝对阈值生效，与今天完全一致。
- **填了 ratio（模板默认会写入）**：窗口 < 270588 的模型触发会提前、< 290909 的模型压缩目标会更深——这是 0.85/0.55 的预期结果，不是兼容性缺陷（见 §4.3 分界点）。
- **压缩总开关**：本次把 `enabled: true` 写进模板（对齐 Python），压缩默认开启——这是**行为变更**（Go 现在默认不跑压缩），但属 parity 修复。用户仍可在界面关闭；关闭时本次阈值改动全部休眠。
- **两端口径**：Python 已默认开启压缩且已有四段 yaml，Go 本次只是追平，不改 Python 的默认值。
- **请求侧**：`max_output_tokens` 只是「模型配置兜底」，所有显式 `WithInvokeMaxTokens(...)` 调用优先级更高，不受影响。
- **`react.context_engine_config.context_window_tokens`**（优先级 1）保持 0，语义不变。

## 9. 测试计划

### 9.1 单元测试（Go）

| 对象 | 用例 |
|---|---|
| `buildModelsDefaultsFromFrontend` | 两字段正常写入 mcc；非法值 → `ConfigBadRequest`；未填 → 不产生键 |
| `handleModelsList` | 两字段回填；配置缺失时不出现在结果里（前端显示空） |
| `deepAgentContextEngineConfig` | 多条目收集成 map；同模型冲突按 D10；无模型段时不 panic；`configBase=nil` 回退 |
| `buildModelFromEntry` | `max_output_tokens` → `MaxTokens`；`0` → nil；进 `Extra` 的键集合不含这两项 |
| `ContextProcessorRail.Init` | window=32K/128K/256K/512K 四档 × 五个 processor 的阈值（对照 §6.3/§6.4）；ratio 缺省时等于绝对值；`target>=trigger` 兜底；yaml 绝对值覆盖后仍被 min 收口（§6.6） |
| `ResolveContextMax` | 既有用例保持（4 级优先级不变） |
| 百分比 | `input_tokens` 优先、回退 `total_tokens`、再回退本地估算 |

### 9.2 集成测试

放 `tests/integration/`，须真实跨 ≥3 个 internal 二级子系统以满足 `cmd/integration-audit` 两层门禁（记忆：审计按调用跨度 + 覆盖交叉验证）：

- 链路：`web`（`models.replace_all` 写入 config.yaml）→ `swarm/server/adapter`（`deepAgentContextEngineConfig` 装配）→ `agentcore/context_engine`（`ResolveContextMax` + 中间件阈值）→ `agentcore/foundation/llm`（请求体 `max_tokens`）
- 断言：写入 32K/8K → `context.usage` 事件 `context_max=32000`；prompt capture 里请求体 `max_tokens=8192`；`chat.usage_summary` 的 `context_window_tokens=32000`
- 阈值断言用例的 fixture **必须显式写 `react.context_engine_config.enabled: true`**，否则 rail 不注册、阈值无从观测（`deep_adapter_rails.go:935`）
- mock 只在边界（LLM HTTP 端点用 httptest），不 mock adapter / context engine

### 9.3 门禁

- 覆盖率 ≥85%，「改动行」门禁 85%/5%（记忆中的既有口径）
- 远端 CI 四个 job 全绿（Build / Lint / Unit / Integration）

## 10. 不做（YAGNI）

- 不做 `react.context_engine_config.context_window_tokens` 全局覆盖键（D11）
- 不做 per-processor 独立 ratio（D8）
- 不在 UI 暴露压缩阈值/比例（只在 yaml；界面仍只读展示百分比）
- 不改 token 计数算法、不改 `ResolveContextMax` 的四级优先级

## 11. 风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| 小窗口模型压缩行为变化 | 用户可感知：更早压缩、早期上下文更早被摘要 | 属预期目标；yaml 里可按需把绝对阈值调大或把 ratio 设 0 关闭联动 |
| 0.85/0.55 让 256K 档也收紧 | 触发早 5.4%、目标深 12%，不是纯粹「小窗口才变」 | 已在 §4.3 与 §6.3 明确写出分界点（270588 / 290909），避免误判为回归 |
| 压缩由默认关变默认开 | 用户会突然开始看到压缩行为（更省 token，但早期上下文更早被摘要） | 属 parity 修复（Python 一直默认开）；在 CHANGELOG 与发布说明中显式写出，并提示可用界面开关关闭 |
| Python 未同步 | 同一份 config.yaml 两种行为 | Python 改动与 Go 同批提交（§5.5） |
| 团队/子代理未覆盖 | 主 Agent 显示 32K、成员仍 200K | A3 列为同批交付；若拆分，需在文档与状态里标注「仅主 Agent 生效」 |
| `mcc` 承载语义不属于它的字段 | 未来若把 `mcc` 整体透传请求，窗口值会被当请求参数发出 | B3 白名单 + 注释；`mcc` 当前无整体透传路径 |
| 百分比口径变更 | 数值整体变小（不再含输出） | 属修正；CHANGELOG 说明 |

## 12. 实施顺序（供计划阶段参考）

1. 后端读写闭环（B1/B2/B3/B4 + T1）——不涉及行为，先跑通「配得上、读得出、回填得对」
2. 装配层（B5 + A1）——窗口值进入 `ModelContextWindowTokens`，`context.usage` 分母随之正确
3. 百分比口径（B6/B7）
4. 阈值联动（B8 + A2 + D9 的 FullCompact 段）
5. 多装配点补齐（A3）
6. 前端渲染（F1/F2/F3）
7. Python 同构（P1-P6）
8. 测试补齐与 CI 全绿
