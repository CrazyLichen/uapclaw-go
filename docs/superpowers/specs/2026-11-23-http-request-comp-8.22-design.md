# 8.22 HTTPRequestComponent HTTP 请求组件设计 spec

- 日期：2026-11-23
- 章节：领域八 8.22（HTTPRequestComponent）
- Python 真相源：`openjiuwen/core/workflow/components/tool/http/http_request_component.py`（512 行）+ 同目录 `__init__.py`（12 导出符号）
- Go 落点：`internal/openuapclaw/core/workflow/components/tool/http/`（新包）
- 流程位置：工作流 DAG 原子节点（swarmflow 图的 HTTP 请求节点类型），不属 Agent 交互式 ReAct 循环。运行时链路：`BaseWorkflow.Compile` → 节点装配（8.14 双基座）→ WorkflowComponent 槽位 ToExecutableFn 首调构造（8.17/8.18 D-18-4 惯例）→ `HTTPRequestExecutable.invoke` 三步链（processInputs → makeRequest → processResponse）。编排顺序上紧跟 8.21 ToolComp，后接 8.23 IntentDetectionComp / 8.24 QuestionerComp（后者均在 Python llm 包）。Python 侧经 `workflow/__init__.py` 对外导出，全仓 grep 证实除导出外零消费方（能力面完备性组件，无真实跑通行为可对照）。

## 1. 背景

8.21 已收口（ToolComp，一次收口含 8.18 D-18-5 连带废弃），8.22-8.26 按编号推进（2026-11-23 二十次注记）。用户拍板方案 A：按编号 8.22 → 8.23 → 8.24，不做 llm 家族换序。

前置件盘点（规则 13.4，全部已就绪，无需换章节）：

- ✅ `common/security.SslUtils.GetSSLConfig / CreateStrictTLSConfig`（ssl_utils.go:24/:50，≙ Python SslUtils.get_ssl_config / create_strict_ssl_context）
- ✅ `common/security.UrlUtils.GetProxyURL(targetURL, configuredProxy)`（url_utils.go:32，≙ UrlUtils.get_proxy_url）
- ✅ 错误码 102000 COMPONENT_TOOL_EXECUTION_ERROR 已注册（`StatusComponentToolExecutionError`，codes_workflow.go:264；勘误：Python http_request_component.py:299/:348 用的是 codes.py:166 的 COMPONENT_TOOL_EXECUTION_ERROR=102000，非 tool_comp.py 的 TOOL_EXECUTION_ERROR=182012，初稿误记已订正）
- ✅ 8.17 WorkflowComponent 槽位基座 + 8.20 收口后"槽优先"模式（构造期 ToExecutableFn=注入 self，勿再 override 两方法——memory rsi-8-20-complete 铁律）
- ✅ `foundation/llm.Model` 与本节无关（HTTP 组件不涉 LLM）
- **回填情况**：全计划 grep 证实无任何 ⤴️ 指向 8.22 的回填义务；仅有的邻域钩子（6.25 交互中断恢复 ⤵️ 8.14-8.24、8.25 跨域联动）均不涉及 8.22。本节无旧 Go 实现需删除（规则 8 不适用）。

Python 侧逐文件对照：

| Python 文件 | 行数 | Go 对应 | 状态 |
|---|---|---|---|
| `tool/http/http_request_component.py` | 512 | `http/http_request_component.go`（一文件对一文件，规则 9.1） | 完全缺失 |
| `tool/http/__init__.py` | 31 | `http/doc.go` 导出说明（Go 包导出天然对外） | 完全缺失 |
| `workflow/__init__.py` 导出行 | — | 不涉及（Go 无同款聚合导出） | 不移植 |

缺失项清单（规则 13.2）：无——Python 侧能力 Go 全部承接（含死字段照搬）。

## 2. 拍板记录

| # | 议题 | 拍板 |
|---|------|------|
| 1 | 实施顺序 | **方案 A 按编号**：8.22 → 8.23 → 8.24；8.23/8.24 虽同属 Python llm 包且可复用 8.21 热上下文，但 8.22 独立无风险，不打乱编号注记 |
| 2 | 二进制响应 body 类型 | **方案 A `[]byte` 直装**：非文本 content-type 时结果 dict 的 body 放 `[]byte`，忠实 Python bytes 形态；下游再 JSON 序列化时 Go encoding/json 自动 base64 化（Python json.dumps 遇 bytes 报 TypeError），登记已知偏差 D-22-1 |
| 3 | 重试传输层结构 | **方案 A 每轮新建**：每次重试尝试新建 `http.Client` + 独立 `http.Transport`（SSL 策略按 env 每轮重求），用完即弃不复用连接池——与 Python"每轮新建 connector（随 session 关闭）"逐字对应；重试属异常路径，TLS 重握手成本不敏感 |
| 4 | FORM 请求体怪癖 | **方案 B 修成一致**：Python `aiohttp.FormData(dict)` 实际编出 multipart 载荷但声明头写 `application/x-www-form-urlencoded`（载荷-头不一致，上游死路 bug，零消费方从未暴露）；Go 用 `net/url.Values.Encode()` 出 urlencoded 载荷 + urlencoded 头，作为**有意偏差 D-22-2** 登记。回收条件：上游若修正该行为（载荷改 urlencoded 或头改 multipart），Go 同步复核 |
| 5 | 设计总表（7 条默认决策） | ① 枚举全量映射（HttpAuthType 含 DIGEST/AWS、HttpContentType 含 AUTO、HttpResponseFormat 含 BUFFER），DIGEST/AWS 在 applyAuthentication 中不实现（复刻 Python 注释语义）；② 死字段照搬不消费（见 §4）；③ processInputs 原地写回怪癖复刻（见偏差 D-22-3）；④ 错误码 = `COMPONENT_TOOL_EXECUTION_ERROR` **102000**（codes.py:166；Go `StatusComponentToolExecutionError`，codes_workflow.go:264）响应超限与请求失败两处同码，非 182012（182012 是 tool_comp.py 的 TOOL_EXECUTION_ERROR，本组件不用）；⑤ timeout 字段（秒）→ Client.Timeout，advanced_options.timeout(ms) 死字段不消费；⑥ 一文件对一文件 + doc.go；⑦ 测试走 httptest 真链（规则 3.3 禁 build tag 逃避） |

## 3. Python 一比一对照表

| Python 符号（http_request_component.py） | Go 符号（http_request_component.go） | 说明 |
|---|---|---|
| `HttpAuthType(str, Enum)` 六成员 | `HttpAuthType` 枚举（iota + String()） | none/basic/bearer/api_key/digest/aws 全量，成员不裁剪（规则 9.2.4） |
| `HttpContentType(str, Enum)` 六成员 | `HttpContentType` 枚举 | json/form/multipart_form/binary/text/auto 全量 |
| `HttpResponseFormat(str, Enum)` 五成员 | `HttpResponseFormat` 枚举 | autodetect/json/text/binary/buffer 全量 |
| `HttpAuthConfig(BaseModel)` | `HttpAuthConfig` 结构体 | type/username/password/token/api_key/in_location/name/access_key/secret_key/region 十字段，默认值对齐（type 默认 NONE、name 默认 "Authorization"、in_location 默认 header） |
| `HttpRequestBodyConfig` | `HttpRequestBodyConfig` 结构体 | content_type/json_data(`any`：dict\|str\|nil)/form_data/multipart_form/binary_data/text_data |
| `HttpResponseHandlingConfig` | `HttpResponseHandlingConfig` 结构体 | response_format 默认 AUTODETECT、success_codes 默认 [200,201,202,204]、failure_codes 默认空、response_mode 默认 full、response_data_property、max_redirects(死)、throw_on_http_error(死) |
| `HttpAdvancedOptionsConfig` | `HttpAdvancedOptionsConfig` 结构体 | follow_redirect/ignore_ssl_issues/proxy/timeout(ms,死)/disable_compression(死)/disable_follow_track_redirect(死)/max_body_length(死)/use_stream(死)/proxy_header(死)——仅 ignore_ssl_issues + proxy 两字段被消费 |
| `HttpRetryConfig` | `HttpRetryConfig` 结构体 | enabled/max_retries/retry_on_status_codes 默认 [429,500,502,503,504]/retry_delay(ms)/backoff_type |
| `HttpRateLimitConfig` | `HttpRateLimitConfig` 结构体 | **零消费死配置**，照搬不实现 |
| `HttpRequestParamConfig` | `HttpRequestParamConfig` 结构体 | url/method(默认 GET)/headers(`any`：dict\|str)/query_parameters/body/authentication/response_handling/advanced_options/retry_config/rate_limit_config/timeout(秒，1.0-300.0)/max_response_byte_size(默认 10MB) |
| `HttpComponentConfig` | `HttpComponentConfig` 结构体 | request_params + metadata(嵌 WorkflowComponentMetadata) |
| `HTTPRequestExecutable.__init__` | `NewHTTPRequestExecutable(config)` | 持 config + request_params 引用 |
| `invoke` | `Invoke(modelContext any)` | 三步链：processInputs → makeRequest → processResponse |
| `stream` / `collect` / `transform` | `StreamFn` / `CollectFn` / `TransformFn` 槽位 | 全部委托 invoke 单帧（Python 同款 pass 复刻）；挂 8.17 WorkflowComponent 槽位基座，构造期 ToExecutableFn 注入（8.20 槽优先铁律，勿 override 基座两方法） |
| `_resolve_placeholders` | `resolvePlaceholders`（包级非导出） | `{{key}}` 字面替换，str(value) |
| `_resolve_template_to_dict` | `resolveTemplateToDict`（包级非导出） | 占位符解析后 ast.literal_eval dict 化，失败回 `{}`；Go 用宽解析（yaml/json 容错）替代 ast，登记偏差 D-22-4 |
| `process_inputs` | `processInputs` | url 占位符 → method 大写（inputs 'method' 可覆盖）→ headers（str 模板 dict 化 / dict 拷贝，inputs.headers 覆盖）→ query（str 值逐个占位符解析，inputs.query_parameters 覆盖）→ body（config 有 body：json_data str 时解析**原地写回**；无 body：取 inputs.body 默认 `{}`）→ authentication（config 有则用 config，否则取 inputs.authentication 默认 `{}`） |
| `make_request` | `makeRequest` | 认证注入 → body 编码（prepareRequestBody）+ Content-Type 头 → 代理（UrlUtils.GetProxyURL）→ 重试循环（每轮新 transport + SSL env 每轮重求：ssl_verify 且非 ignore_ssl_issues → strict config，否则 InsecureSkipVerify）→ 8192 分块读 + 超限 102000 → 文本/二进制分型（TEXT_CONTENT_TYPES 白名单，类常量）→ 结果 dict{status_code,headers,content,url,reason} → 按状态码判定重试（retry_on_status_codes 命中且未到上限 → calculateRetryDelay 后重试）→ 异常路径同判定后 102000（COMPONENT_TOOL_EXECUTION_ERROR）包装上抛 |
| `process_response` | `processResponse` | success/failure 码判定（`is_success = in success_codes or (无 failure_codes 且 2xx)`；`is_failure = in failure_codes or ≥400`）→ autodetect 按响应 content-type 二次判型（json/text/其余 BINARY）→ 按 format 解析（JSON 失败原文透传 / TEXT decode / BINARY pass）→ response_data_property 抽取（仅顶层 key，miss 回原值）→ 结果 dict{statusCode,headers,body,url,ok} → response_mode 三分支（on-success 非 success 清空 / on-error success 清空 / full 保留） |
| `apply_authentication` | `applyAuthentication` | NONE/空 → 原头返回；BASIC（username+password 均非空 → `Basic base64(user:pass)`）；BEARER（token → `Bearer {token}`）；API_KEY（仅 header 位置生效；query/body 位置注释级不实现，Python 同款）；DIGEST/AWS 不实现（Python 注释明说） |
| `prepare_request_body` | `prepareRequestBody` | JSON（json_data 非空 → `json.dumps` + 'application/json'）/ **FORM（urlencoded 载荷 + urlencoded 头，D-22-2 偏差）** / MULTIPART_FORM（逐字段 add → multipart writer + 'multipart/form-data'）/ TEXT（text_data + 'text/plain'）/ BINARY（binary_data base64 解码 + 'application/octet-stream'）/ 无匹配 → (nil, nil) |
| `calculate_retry_delay` | `calculateRetryDelay` | base_delay = retry_delay/1000 秒；fixed → base；linear → base×(n+1)；exponential → base×2ⁿ |
| `HTTPRequestComponent` | `HTTPRequestComponent` | 组合面：config 持有 + `executable` 属性懒构造缓存（to_executable 单例）+ `ToExecutable` 方法；`__config` 私有属性 G.CLS.11 怪癖（property getter）→ Go 非导出字段 + `Config()` 访问器 |

TEXT_CONTENT_TYPES 类常量：`['text/', 'application/json', 'application/xml', 'application/javascript', 'application/xhtml+xml']`——make_request 的文本分型与 process_response 的 autodetect 判型**不同源**（后者是 `'application/json' in ct` / `'text/' in ct` 两分支 + 其余 BINARY），两处各自照搬不合并（规则 9.3 步骤边界）。

死导入不移植：`oauthlib.common.urlencode`、`response_parser.ParserRegistry`（Python 侧 import 未使用）。

## 4. 死字段清单（照搬不消费，登记备查）

Python 定义但 `make_request`/`process_response` 零消费：`max_redirects=21`、`follow_redirect=True`、`disable_follow_track_redirect`、`disable_compression`、`max_body_length=1MB`、`use_stream`、`proxy_header`、`HttpRateLimitConfig` 整体、`throw_on_http_error`、`advanced_options.timeout`(ms)。

事实语义：请求**恒 `allow_redirects=False`**（aiohttp 显式禁用，`follow_redirect=True` 默认值不生效）；Go 用 `http.Client` 不设 CheckRedirect 或 CheckRedirect 返回 ErrUseLastResponse 等价实现（跟随细节由实施时定，忠实"从不跟随"语义）。

## 5. 已知偏差登记

| # | 性质 | Python 事实 | Go 实现 | 回收条件 |
|---|---|---|---|---|
| D-22-1 | 平台差异 | 二进制 body 为 bytes，json.dumps 遇 bytes 抛 TypeError（断链从未触发，零消费方） | `[]byte` 直装 map，下游 encoding/json 自动 base64 | 若上游补真实消费方并定义 bytes 出口形态，同步对齐 |
| D-22-2 | 有意修正 | FORM：multipart 载荷 + urlencoded 声明头（不一致 bug） | urlencoded 载荷 + urlencoded 头 | 上游修正该行为时同步复核 |
| D-22-3 | 忠实复刻 | `process_inputs` 对 `body_config.json_data` str→dict **原地写回**（组件级状态跨 invoke 持续，重入可见） | 同样改 config 指针字段，不拷贝 | 无（对齐项非偏差，登记防"顺手修掉"） |
| D-22-4 | 语言差异 | `ast.literal_eval` 解析 str headers 模板 | Go 无 ast：yaml/json 宽解析 + 失败回 `{}`（Python ValueError/SyntaxError 同款吞掉） | 如遇真实模板语法差异案例再收敛 |
| D-22-5 | 语言差异 | asyncio.sleep 重试等待 | `time.Sleep`（同步）；aiohttp 异步取消语义由 ctx 取消承载 | 无 |

## 6. 测试设计（httptest 真链，规则 3.3）

单测覆盖面（全部 httptest 本地服务，无 build tag）：

1. GET 基本链 + 占位符 URL/headers(dict)/query 三处解析
2. POST json：body_config.json_data dict 与 str 模板（含原地写回断言：二次 invoke 不再解析）
3. FORM 修正后 urlencoded 载荷 + Content-Type 头断言（D-22-2 契约钉住）
4. MULTIPART_FORM：multipart writer 字段断言
5. BINARY body：base64 解码 + 'application/octet-stream' 头
6. 响应文本分型：text/json content-type → str body；二进制 content-type → `[]byte` body（D-22-1 契约钉住）
7. processResponse：success_codes/failure_codes 矩阵、autodetect 三分支、JSON 解析失败原文透传、response_data_property 命中/miss、response_mode 三分支（on-success 失败清空 / on-error 成功清空）
8. 认证四类：basic(b64 断言)/bearer/api_key header/none 原头；API_KEY 非 header 位置不注入
9. 重试：retry_on_status_codes 命中 → 三退避延迟断言（short delay）→ 耗尽后最终响应；网络错误 → 102000（COMPONENT_TOOL_EXECUTION_ERROR）
10. 响应超限：小 max_response_byte_size + httptest 大响应 → 102000（COMPONENT_TOOL_EXECUTION_ERROR）
11. SSL：ignore_ssl_issues=true → InsecureSkipVerify；env 开关走 GetSSLConfig 真实读（子进程或 env 注入）
12. 代理：GetProxyURL 调用透传（URL 断言）
13. stream/collect/transform 单帧委托等价 invoke
14. Component 组合面：executable 懒构造缓存单例、Config 访问器

集成测试：`ExecuteSingleComponent` 真链一例（对齐 8.18 llm 集成套件惯例），httptest 端点 + 占位符 URL 走 inputs 注入。

## 7. 验收口径

- `go build ./...` / `go vet ./...` 通过；`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全量回归零 FAIL（规则 6/11：本地门槛 + 推送，不追远端 CI）
- http 包覆盖率 ≥ 85%（规则 3）
- `doc.go` 含包概述/文件树/对应 Python 路径/核心类型索引（规则 1）
- IMPLEMENTATION_PLAN.md 8.22 行 ☐→✅ 并按 8.21 行格式回填 Go 事实（偏差编号 D-22-1..5、覆盖率、spec/plan 路径）
- 实施顺序注记新增"二十一次记录"行，保留全部历史注记（回填内容不丢失）
