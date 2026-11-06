# LLM 客户端 C 组对齐设计（2026-11-06）

> 依据：`docs/sync/defect-plan-2026-11-04.md` §3.C（15 行）。
> 上游基线：agent-core HEAD `5a35c39f`（本地源码 `/home/opensource/agent-core`，`.git` 已清理，按 HEAD 源码逐函数比对）。
> 范围：C-01~C-14 共 14 行（C-15 已标 ➖）；核验结论为 **6 存在 + 4 部分存在 + 4 已对齐**。

## 0. 核验结论与处置一览

| 编号 | 结论 | 处置 | 批次 |
| --- | --- | --- | --- |
| C-01 | 部分存在：帧超时已实现（model.go），但 `http.Client.Timeout` 是总时长语义，且默认 60 < 首帧 300 违反上游不变量 | 修 | C-A |
| C-02 | 已对齐：阻塞 channel + `select ctx.Done()`，无丢弃分支 | ➖ | — |
| C-03 | 部分存在：非流式 `finish_reason` 两个方向都偏离 | 修 | C-F |
| C-04 | 存在：usage 只认 1 条路径 1 个字段，且逻辑复制 4 份 | 修（含 C-06） | C-B |
| C-05 | 存在：抽取只认 `reasoning_content`；deepseek 包是上游已删除的幽灵包 | 修 + 删包 | C-C |
| C-06 | 并入 C-04（Responses API 未迁移，`reasoning_tokens` 字段随 usage 层补齐） | 并入 | C-B |
| C-07 | 存在：无 `auth_mode`，api_key/ssl_cert 恒必填，拒掉合法部署 | 修 + 删兼容壳 | C-D |
| C-08 | 部分存在（方向与文档描述不同）：不注入 `/v1`，但也不剥已带的 `/chat/completions` → 双段 404 | 修 | C-E |
| C-09 | 存在：foundation 层默认 0.95/0.1 恒下发；显式 0 被吞后反弹 0.95 | 修 | C-D |
| C-10 | 存在（触发面窄）：编码失败包成 MODEL_CALL_FAILED 被误重试 | 修（A+ 严格版） | C-E |
| C-11 | 已对齐：状态码语义在 rail 层同构（model_anomaly.go:860-1010） | ➖ | — |
| C-12 | 已对齐：瞬时重试 9.86 已落地（model_anomaly.go:552/584/313） | ➖ | — |
| C-13 | 部分存在：配置换键会重建，但旧实例不回收（无 Close、无驱逐） | 修（含偏差登记） | C-G |
| C-14 | 存在：4 处每请求新建 transport | 修 | C-A |

## 1. 批次 C-A：超时语义与连接池（C-01 + C-14，联动实施）

### 1.1 缺陷

`buildHTTPClient` 每请求新建 `http.Transport` + `http.Client{Timeout: 总时长}`：

- 总时长语义覆盖**响应体读取全程**：持续吐字的健康流在第 `timeout` 秒被硬切断（11-03 `c6cd13aa` 修复了配置不生效，把 60s 换成了 1800s，刀还是那把刀）；
- 默认 `timeout=60`（`schema/config.go:263`）< 首帧 300s，违反上游不变量 `timeout >= stream_first_chunk_timeout`（`schema/config.py:148-150` 注释明文约定）；
- 每请求重新 TCP+TLS 握手，连接不可复用。

上游形态（`openai_model_client.py:1414-1490`）：进程级共享 `AsyncOpenAI` 客户端（`_client_cache` 按 connection_key），httpx `read` 超时=分片间空闲；`Limits(max_connections=100, max_keepalive_connections=20, keepalive_expiry=60.0)`；teardown 走 `aclose()`。

### 1.2 设计

**共享 Transport 池**（落 `model_clients` 包新文件 `transport_pool.go`）：

- 分桶键 `(verify_ssl, ssl_cert)`。Go 的 `Transport` host 无关（URL/鉴权头 per-request），比上游按 connection_key 分桶复用率更高——**登记偏差 D-1**；
- 参数照抄上游 Limits：`MaxIdleConns=100`、`MaxIdleConnsPerHost=20`、`IdleConnTimeout=60s`（`keepalive_expiry=60.0` 对应物，同时防「复用已被服务端关掉的死连接」）；
- `DialContext` 超时 30s；
- Transport 级 `ResponseHeaderTimeout = 360s`（统一值，兜「连上但永不给响应头」；不能 per-request——见 D-2）；
- 提供 `CloseIdleTransports()` 入口（对齐 `aclose()` 语义），进程 teardown / 测试 TearDown 调用。

**超时接线**（`openai/request_builder.go` 重写）：

| 路径 | 机制 |
| --- | --- |
| Invoke（非流式） | ctx deadline：`BuildHTTPRequest` 内 `context.WithTimeout(ctx, timeout)`，`Client.Timeout` 恒 0 |
| Stream（流式） | **无总超时**。等头由 Transport 级 `ResponseHeaderTimeout` 兜；读 body 由 model.go 帧超时（首帧 300s / 空闲 60s）+ `cancelStream` 兜 |

**默认值修正**：

- `schema/config.go:263` 默认 timeout 60 → **360**（对齐 `schema/config.py:145`，恢复 `timeout ≥ first_chunk=300` 不变量）；
- `openai/request_builder.go:150` 与 `inference_affinity/client.go:741` 的硬编码 60 兜底改为引用同一默认常量；
- `dashscope/api_client.go:201` 的 120 保留（原生多模态独立路径）。

**4 处 transport 构造收敛**：`openai/request_builder.go:124`、`dashscope/api_client.go:171`、`inference_affinity/client.go:716`、`intellirouter/router.go:766`（health check）全部改从池取。

### 1.3 已知偏差（本节）

- **D-1 分桶粒度**：上游 `_client_cache` 按 connection_key（含 api_base）分桶；Go 按安全形态 `(verify_ssl, ssl_cert)`。理由：Go Transport 跨 host 复用是语言层能力，按 host 拆桶无收益。回收条件：上游拆桶语义出现新依赖时重评。
- **D-2 ResponseHeaderTimeout 为 Transport 级统一值**：上游 httpx read timeout per-request（等头阶段也被 read 覆盖）；Go 共享 Transport 上 per-request 做不到（须包装 net.Conn，成本不成比例）。取默认 360s 统一值，任何配置下等头上限一致。

## 2. 批次 C-B：usage 收敛与字段补齐（C-04 + C-06）

### 2.1 缺陷

`UsageMetadata` 只有 `CacheTokens`，仅认 `prompt_tokens_details.cached_tokens` 一条路径（`openai/parse_response.go:166-169`）；构建逻辑在 openai/siliconflow/inference_affinity/intellirouter 复制 4 份。`context_engine/usage/session_aggregator.go:17` 已把缺口写成注释。

### 2.2 设计（对齐 `base_model_client.py:86/152/253/293` 四个静态方法）

`model_clients` 包新文件 `usage_extract.go`，包级函数一比一映射：

- `ExtractCacheTokens(obj)` ← `_extract_cache_tokens`：7 条嵌套路径 + 11 个平铺字段；
- `CacheUsageMetadata(obj)` ← `_cache_usage_metadata`：read/miss/write 归一 + `cache_status`/`cache_source`/`cache_authoritative`（legacy `cache_tokens` 不可信语义保留）；
- `ExtractCacheCreationTokens(obj)` ← `_extract_cache_creation_tokens`：3 条嵌套路径 + 4 个平铺字段；
- `ExtractReasoningTokens(obj)` ← `_extract_reasoning_tokens`：`completion_tokens_details.reasoning_tokens` 等（对齐其路径清单）。

统一入口 `BuildUsageMetadata(usage, modelName)`（Python: openai 客户端组装点 `openai_model_client.py:2817-2829` / `2940-2946`），openai/siliconflow/inference_affinity 4 处调用点全部替换，删除各自副本。

`UsageMetadata` 补字段（`schema/usage_metadata.go`）：`CacheReadTokens`/`CacheMissTokens`/`CacheWriteTokens`（`*int`，区分「未上报」与 0，对齐 Python `int | None`）、`CacheCreationTokens`、`ReasoningTokens`、`CacheStatus`、`CacheSource`、`CacheAuthoritative`。JSON 键名对齐 Python 字段名。

`session_aggregator.go` 改用新字段：`CacheReadTokens != nil` 判「已上报」，`HitRate`/`Miss` 直接取值；全部为 nil 时回落现有 `CacheTokens` 旧算法（注释同步更新，删「C 组缺陷」TODO 口径）。

## 3. 批次 C-C：reasoning 抽取与 DeepSeek 并包（C-05）

### 3.1 缺陷

- 抽取只认 `reasoning_content`；上游 `_extract_reasoning_content`（`openai_model_client.py:2751-2763`）四级回退：`reasoning_details[0].text` → `reasoning_content` → `reasoning`（OpenRouter 走此）→ `reasoning_token_text`；
- `deepseek` 包（3 文件）对齐的上游文件 `deepseek_model_client.py` **在 HEAD 已删除**：DeepSeek 合并进 OpenAI 客户端，补 `reasoning_content` 由 `model_requires_reasoning_content(model)`（模型名含 deepseek，`utils/endpoint_profiles.py:33-35`）+ `_deepseek_reasoning_content`（`endpoint_profiles.py:37-41`）在消息转换层完成。Go 用「客户端类型」当门卫，聚合网关 `provider=OpenAI + model=deepseek-*` 时不补 → 400。

### 3.2 设计

- 四级回退实现为 `model_clients` 包级函数 `ExtractReasoningContent(msgOrDelta)`，`openai/parse_stream_chunk.go`、`parse_response.go`、siliconflow、inference_affinity 共用（各客户端的 `ChunkDelta`/`ResponseMessage` 补 `Reasoning`、`ReasoningDetails` 等可选 JSON 字段）；
- 补 `reasoning_content` 挪进 `BaseClientEmbed.BuildRequestParams` 消息转换后段，门卫改「模型名含 deepseek」（`strings.Contains(strings.ToLower(model), "deepseek")`，对齐 `endpoint_profiles.py:34`）；
- **删除 `model_clients/deepseek` 包**（client.go/doc.go/client_test.go，3 文件）：注册表 `DeepSeek` 键改指向 openai 工厂（`model_clients/openai/client.go` init 段）；`model_clients_register.go` 删空导入。规则 8 验收：`grep -r "model_clients/deepseek" internal/ cmd/ tests/` 0 命中。

## 4. 批次 C-D：凭据校验与采样参数（C-07 + C-09）

### 4.1 C-07 设计（对齐 `schema/config.py:40-44/111-131/225-256` + `base_model_client.py:428-445`）

- `ModelClientConfig` 增 `AuthMode` 枚举（`api_key` 默认 / `none` / `custom_headers` / `openai_account_oauth` 四成员全量映射；后者分支暂不生效，`openai_account_model_client.py` 未迁移 ⤵️）；
- 增 `EndpointProfile` 字段 + ollama/lmstudio 默认 base 回填（`_DEFAULT_API_BASE_BY_ENDPOINT_PROFILE`）；
- 白名单矩阵（Go 现有 7 provider 对照 `_TOP_LEVEL_API_KEY_PROVIDERS`）：OpenAI/OpenRouter/SiliconFlow/DashScope/DeepSeek/InferenceAffinity → key+base 必填；`intelli_router` → 两者都不要求；
- 校验两层：provider 级（`schema/config.go` Validate）+ client 级（`base_client.go` ValidateConfig，对齐 `base_model_client.py:428-445`：`auth_mode != api_key` 不要求 key；api_base 恒必填**但** Go 无法子类覆写，改为「provider 不在 base 白名单时跳过」——**登记偏差 D-3**（效果等价上游 IntelliRouter 覆写为空）；
- `verify_ssl=true` 不再强制 `ssl_cert`（公共 CA 用系统信任库，对齐 `ssl_utils.py:55-63`；此项与 L-07 重叠，L-07 处置时引用本节）；
- 鉴权头按 mode：`none` → 省略 `Authorization`；`custom_headers` 且 key 空 → 省略；key 缺省占位 `"EMPTY"`（`_resolved_api_key_for_config`，`openai_model_client.py:326-330`）；
- **删除 `WithSkipValidate`/`skipValidate`**（生产仅 `intellirouter/client.go:81` 一处，改走白名单；测试同步改写）。

### 4.2 C-09 设计（对齐 `schema/config.py:262-266` + `base_model_client.py:587-605`）

- `ModelRequestConfig.Temperature` 改 `*float64`；`NewModelRequestConfig` 删 `Temperature: 0.95`、`TopP: &0.1` 默认；
- `BuildRequestParams`（`base_client.go:336-370`）改「params > model_config，nil 不下发」，删 `!= 0` 判空；
- 应用层默认保留不动：`deep_adapter.go:2185`（0.95）、`default_models.go:189`、`team/runtime_inheritance.go:281`——主链路行为不变；
- 传播改动：`WithTemperature(float64)` 签名改指针形（新增 `WithTemperaturePtr(*float64)`，`wiki_tools.go:631-633` 等调用点适配）；
- Anthropic 互斥丢 top_p 半条：无 Anthropic 客户端，⤵️ 登记随迁移。

## 5. 批次 C-E：URL 归一与编码错误（C-08 + C-10）

### 5.1 C-08 设计（对齐 `openai_model_client.py:307-324`）

`openai` 包导出 `NormalizeAPIBase(apiBase)`：strip 空白 → 去尾 `/` → 以 `/chat/completions` 结尾则剥掉（不推断 `/v1`）。接线点：

- `BuildHTTPRequest`（request_builder.go:72）；
- `inference_affinity/client.go:342`（release_kvcache 拼接）；
- Web 保存侧 `app_web_handlers.go:1292` 的 `TrimSuffix` 局部实现改为调用同一函数（删重复）；
- `ResolveDashScopeBaseURL`（`dashscope/api_client.go:46`）入口先过同一段清理再按 `compatible-mode` 切分——**超出上游一步（上游原生路径原样透传 `openai_model_client.py:2278`），登记偏差 D-4**。

### 5.2 C-10 设计（A+ 严格版，对齐 `utils/request_encoding.py:27-35` + `openai_model_client.py:2011/2228`）

- `encodeJSON`（request_builder.go:162）失败抛 181004 `MODEL_INVOKE_PARAM_ERROR` + `details.stage=request_encoding`；
- 编码后、`http.NewRequestWithContext` 前 `utf8.Valid(body)` 校验，非法字节同上抛（覆盖 NaN/Inf/不可序列化类型/非 UTF-8 四类）；
- `Invoke`/`Stream` 的 `WrapError` 前加放行判断（对齐 `if isinstance(e, ValidationError) and details == {"stage": "request_encoding"}: raise`）；
- 编码点收敛 `openai` 包一处（4 客户端共用）。

## 6. 批次 C-F：finish_reason 对齐（C-03）

`openai/parse_response.go:122-126` 改对齐 `openai_model_client.py:2877-2879`：provider 有值原样保留（**删 `stop→tool_calls` 改写**）；空值兜底 `tool_calls`（有工具调用）否则 `stop`。流式侧已同构（`message_chunk.go:171` = `message_chunk.py:203`），不动。已核验：agentcore 无生产代码以 `finish_reason=="tool_calls"` 分支，行为影响限于展示/轨迹。

## 7. 批次 C-G：router 生命周期（C-13，对齐 `intelli_router_model_client.py:474-501/670-700`）

- 补 `ReliableRouter.Close()`：`StopHealthChecker()` + `CloseIdleConnections()`（对齐 `await entry.router.close()`）；
- `GetOrCreateRouter`（router.go:543）增驱逐路径：`ResetRouterCache(key)` 显式触发（配置换绑调用点），锁外 best-effort `Close()` + 失败告警（对齐 L687-698）；
- web dashboard 跟随驱逐（L689 `_stop_web_server`）：Go 未迁移 dashboard，跳过；
- `_RouterCacheEntry.loop` 归属 + `_ensure_router_for_current_loop` 逐请求核对：**不实现，登记偏差 D-5**——Go 无事件循环，net 池进程级共享，goroutine 退出≠资源退役，该机制防护的失败模式在 Go 结构性不存在；其两个防护对象已由 C-14（IdleConnTimeout）与本批次（显式驱逐）覆盖。

C-11/C-12 标 ➖：rail 层 `model_anomaly.go:860-1010`（状态码分类）/`:552/584/313`（瞬时重试+退避）已同构，defect-plan 表内写明 Go 位置。

## 8. 删除清单（规则 8 验收：grep 0 命中）

| 对象 | 位置 |
| --- | --- |
| `deepseek` 包整包（client.go / doc.go / client_test.go） | `model_clients/deepseek/` |
| `WithSkipValidate` / `skipValidate` 字段 | `model_clients/base_client.go:128` + 测试 |
| 4 份 usage 构建副本 | openai `parse_response.go:159-194`、siliconflow `client.go:474-501`、inference_affinity `client.go:568-590` |
| `buildHTTPClient`（openai/dashscope/affinity 三份独立实现） | 收敛进 `transport_pool.go` |
| Web 保存侧 `TrimSuffix("/chat/completions")` 重复实现 | `app_web_handlers.go:1292` |
| `deepseek` 注册空导入 | `model_clients_register.go:17` |

## 9. 测试计划

每批次独立提交（对齐 B 组 C1~C6 模式）：

- C-A：流式 client `Timeout==0` + Transport 池参数断言；默认 360 回归；同 `(verify_ssl, ssl_cert)` 复用同一 Transport 实例（指针相等）；Invoke ctx deadline 生效（httptest 慢响应被切）；帧超时用例回归（首帧/空闲）；长流（> 总超时）不被切断的用例（httptest 持续吐 SSE 超过 timeout 秒）——注意 CI 时长，用 1~2s 量级的自定义 timeout 构造；
- C-B：7+11 字段矩阵逐一 httptest 假响应；read/miss/write 归一（含 legacy 不可信分支）；`session_aggregator` 新旧字段切换；4 客户端 usage 一致性；
- C-C：四级回退（每级一个用例）；模型名门卫（deepseek 模型补字段、非 deepseek 不补、provider=OpenAI+deepseek 模型也补）；注册表 `DeepSeek` → openai 工厂；
- C-D：白名单矩阵每格一用例；`auth_mode=none/custom_headers` 鉴权头省略；`ssl_cert` 不强制；`Temperature=0` 下发 0；未配置不下发（payload 断言无 temperature 键）；
- C-E：`/chat/completions` 结尾剥除、尾斜杠、空白；dashscope 原生侧完整 URL；NaN/Inf/非 UTF-8/不可序列化 → 181004 + stage；Invoke/Stream 放行不包 MODEL_CALL_FAILED；
- C-F：finish_reason 有值保留（含 stop+tool_calls 不改写）/空值两分支兜底；
- C-G：`ResetRouterCache` 停旧 health checker（轮询 goroutine 退出）；旧项 Close；不存在键 no-op。

覆盖率：改动文件保持 ≥85%（行级豁免沿用既有机制）；集成面：C-A + C-B 落一条「SSE 真流 + usage 汇报」跨子系统用例（llm 客户端 + harness + context_engine/usage）。

## 10. 验收

1. `go build ./... && go vet ./...` + golangci-lint 零新增；
2. 全量 `go test ./internal/agentcore/foundation/llm/... -race` 绿；
3. 远端 CI 四 job 全绿（规则 6）；
4. `defect-plan-2026-11-04.md` C 组 15 行状态回填：✅（修复行）+ ➖（C-02/C-11/C-12，写明 Go 位置）+ 修正 C-08 描述方向（「硬编码补 /v1」→「不剥已带 /chat/completions」）；
5. `IMPLEMENTATION_PLAN.md` 登记本设计（领域编号沿用 B 组先例的 24-A 风格，登记为 C-A~C-G 七批次）。
