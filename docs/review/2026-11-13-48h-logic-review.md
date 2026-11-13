# 2026-11-13 48 小时提交审查报告

- 审查范围：2026-11-11 04:00 ~ 2026-11-13 04:00（最近 48 小时）
- 提交规模：234 个提交（含 8 个合并提交）
- 审查方式：逐提交读 diff + 与 Python 上游（`agent-core` / `jiuwenswarm-develop`）逐函数对拍；对疑似缺陷均写临时探针程序实测复现（探针已全部删除，工作区未改动）
- 结论：**严重 3 / 一般 8 / 提示 12**

## 一、这 48 小时做了什么

| 块 | 内容 | 提交量 |
|---|---|---|
| 20.3 process_cli | REPL/命令/渲染/诊断 全套（17 个 Python 文件 → 20 个 Go 文件），`cmd/uapclaw-process` 二进制 | 约 25 个 |
| 23.2 Responses API | `responses` 包（transport + utils）、openai client 分流、reasoning 配置化 | 约 10 个 |
| 23.1 Anthropic | 集成测试 6 例 + 4 处审查修复（SSE reader 共享等） | 3 个 |
| 9.90e 权限护栏 | 17 处桩回填、permissions 配置读写、审计旁路 | 8 个 |
| 10.7.1 / 10.3.31a / 10.7.2 | 话题起始 store、推荐引擎、建议账本闭环 | 约 20 个 |
| 20.2 一次性执行协议 | protocol 包、machine 编排、持久化双屏障 | 约 20 个 |
| 15.2/15.3/15.4/15.5 | 事件、交互、检查点、provider 基座（15.5 只出了计划） | 约 15 个 |
| 解环 + 编排修复 | runtime↔server/runtime 断环、on_agent_ready、TERMINAL_ERROR_EVENT_TYPES、P-7 | 6 个 |
| CI / 风格 | race 修复、异步写队列排空、空 const 块清理、gofmt/lint 清零 | 约 10 个 |

整体评价：**对齐质量仍然在线**。抽查的注释 Python 行号基本属实（`mode_matrix.py:305-330`、`reasoning_config.py:120-126`、`responses_utils.py:349-386`、`url_utils.py:82-160` 等都对得上），规则 8 的删除也做得比较干净（删 `ExecuteOption`、删 `facadeUsesSessionRuntime`、删 `dashscope/siliconflow/inference_affinity` 三包后锚点归零）。CI 那轮修复（race + 队列排空）诊断得很准，顺带修掉的真缺陷（flush 哨兵阻塞 worker）也确实存在，实测验证通过。

问题集中在三个地方，都是「看起来做完了、但那条路其实走不通」：

1. **23.2 的 Responses 调用链根本接不上**——客户端没注册、OAuth 没实现、`verify` 传错类型，配置成 responses 模式直接报"不支持的 client_provider"。
2. **P-7 只修了一半**——manager 现在自建了，`uapclaw-process "任务"` 能跑到模型检查了，但计划里 P-7 的一半描述（走 clientFactory 注入）没做，回填时形成自相矛盾。
3. **Python 里"必须查表"的地方 Go 是"随便给个值"**——失败的判定改成看 event_type 了，但只接到一个测试里，真正的消费者（心跳/会话执行器/cron）一个没改；agent 缓存 key 的 plan 子模式坍缩、mode 归一里 5 个前置分支也都丢了。

---

## 二、严重

### S-1 23.2 Responses API：整条链路当前走不通（三个断点叠在一起）

**属于哪条流程**：`CreateModelClient`（`registry.go:183`）→ 归一 provider 别名 → `ImplementationProvider`（`registry.go:174`）→ `GetClientRegistry().GetClient(...)` → openai client 的 `Invoke`/`Stream` → `usesResponsesAPI()`（`responses_api.go:28`）→ `makeResponsesTransport`（`responses_api.go:137`）。

三个断点：

1. **OpenAIAccount 客户端没有任何工厂注册**。provider 别名表（`endpointprofiles/profiles.go:84-88`）把 `OpenAIAccount` 映射成 `APIMode=responses` + `AuthMode=openai_account_oauth`，`ImplementationProvider` 于是返回 `"OpenAIAccount"`；但 `openai/client.go:700-720` 的 `init()` 只 `Register` 了 `OpenAI`/`OpenRouter`/`DeepSeek` 三个名字。实测：

   ```
   CreateModelClient(provider=OpenAI, auth=openai_account_oauth) →
     [181000] 不支持的 client_provider: 'OpenAIAccount'，支持的类型: []
   ```

   Python 侧同一路径是 `model_clients/__init__.py:43-45` 显式 `from ... openai_account_model_client import OpenAIAccountModelClient` 后实例化。

2. **responses 的鉴权凭据永远是空串**。`invokeResponsesAPI` / `streamResponsesAPI` 都把 `c.ClientConfig.APIKey` 当 Bearer token 传（`responses_api.go:186`、`:239`），而 `shouldOmitAuthorization` 在 `openai_account_oauth` 下返回 true（`request_builder.go:378`）——说明这个模式下本来就不该有静态 api_key，真凭据来自 OAuth token 刷新。OAuth 取 token 这一层 Go 侧没有（`schema/config.go:196` 注释自己写着"未迁移，占位"）。就算第 1 条修好，请求也会带着空 Bearer 发出去。

3. **传输层 SSL 语义和上游相反，而且把 `api_base` 当证书路径**。`makeResponsesTransport` 写的是：

   ```go
   var verify any = false
   if c.ClientConfig.VerifySSL {
       verify = c.ClientConfig.SSLCert       // ← 把"cert"当成了"ca 包路径"
   }
   ```

   然后 `Transport.getHTTPClient`（`transport.go:257-273`）拿到字符串就 `os.ReadFile`。可 `SSLCert` 在语义上是**客户端证书**，绝大多数配置为空 → `verify=""` 落到 default 分支 → `tlsConfig` 是空结构 → 走系统根证书（这一步碰巧对）。一旦用户真的配了客户端证书（含私钥），`AppendCertsFromPEM` 会失败，函数直接返回 error，所有 responses 请求都建不出 client。反过来 `VerifySSL=false` 时 `verify=false` → `InsecureSkipVerify=true`，这个对。**问题在于「verify_ssl 打开时到底要不要系统信任库 + 用户 CA 追加」这条根本没按 `ssl_utils.py` 实现**——这正是 11-12 报告里 A-S5 的同一个坑，anthropic 侧修了一部分，responses 侧新写了一遍同样的错。

**影响**：`api_mode=responses` 是 OpenAI 账号（OAuth）模式的唯一入口，现在配了就跑不起来，报的错还是"不支持的 client_provider"，用户完全无法从错误里判断是缺实现。

**另外一处行为反转**：`emitResponsesError`（`responses_api.go:288-295`）的注释写"error_message 仅在错误自身无消息时填充（Python :1827-1829）"，代码却是：

```go
errorMessage := ""
if strings.TrimSpace(callErr.Error()) == "" {
    errorMessage = callErr.Error()      // 空串再赋空串
}
```

条件写反了（应为 `!= ""`）。当前后果只是错误回调里 `ErrorMessage` 永远是空——但上一轮报告里正是同一类"回调从不带错误文本"被当作提示记录过，这次是在新代码里又写了一遍。

---

### S-2 P-7 回填口径与实际代码对不上，计划文档自相矛盾

**属于哪条流程**：`cmd/uapclaw-process` → `processcli.RunMain` → `dispatchBranch` → `Run`（`processcli/app.go:80`）→ `NewInProcessRuntimeClient(nil)`（`client.go:59`）→ `NewAgentRuntime(AgentRuntimeDeps{})`（`runtime/service.go:205-220`）→ `manager`。

代码侧确实修好了核心：`NewAgentRuntime` 现在在 `Manager == nil` 时自建 `srvruntime.NewAgentManager()`（对齐 `plane.py:155`），实测 `uapclaw-process "hello"` 已经从 `agent runtime manager is nil` 变成 `模型未正确配置，请先配置模型信息`，退出码 1——装配链通了，这一步值得肯定。

问题在文档（`IMPLEMENTATION_PLAN.md:1129`，提交 `09a266933`）：

- 行首写「**P-7 已回填（2026-11-12）**」，同一行后半仍然保留「⤴️ **必须一并回填 20.3 遗留的 P-7**」，并且原文要求「须由 `cmd/uapclaw-process` 装配完整 Runtime 后经 clientFactory 注入 `Run`」——**这条要求的实现形态根本没做**：`Run` 里仍是硬编码 `NewInProcessRuntimeClient(nil)`，没有任何 `clientFactory` 形参。
- 状态从 `☐` 改成 `🔄`，但「剩余」里列的仍是原本 20.10 自己的验收项，读者无法看出 P-7 到底是"完成"还是"没有"。

**为什么算严重**：这是本仓最典型的失败模式——文档说做完、代码只做了一半，下一轮 review/接手的人要么重做要么误以为已完成。按规则 13.2，缺口要么做、要么登记，不能两头都写。实际留下的技术债是：`Run` 无法注入 Runtime，**所有 P-7 的端到端测试只能测到"未配模型"这一条环境确定的路径**（见 `tests/integration/swarm/processcli/cli_e2e_test.go` 的 8 个用例，全都在模型检查之前就返回），真正的执行面（chat 成功 / REPL fork / 会话操作）一条都没被真实覆盖。

---

### S-3 会话异步写队列：跨用例"我删了目录，你替我重建"的循环又回来了

**属于哪条流程**：集成用例 `t.Cleanup` 顺序 → `testhelpers.NewAdapterTestEnv` 的 cleanup（`adapter_env.go:70`）→ `session.ResetDrainedQueues()` → `FlushHistoryQueue` / `FlushMetadataQueue` → 哨兵条目 → `metadataWorkerHandle` / `historyWorker` 非哨兵分支 → `WriteSessionMetadata` → `GetSessionsDir()` → `os.MkdirAll`。

`6ab8efbe0` 的思路是对的：先 Flush 排空、再 Reset 关队列，且把 `t.Cleanup` 放在 TempDir 清理之前。但它有一个前提没成立——**队列的目录归属是"上一次写入时"的**：

1. 用例 A 只写元数据、从不 Flush（大量既有用例如此），worker 里留着 A 的目录（上一个 `UAPCLAW_DATA_DIR`）。
2. 用例 B 的 `ResetDrainedQueues` 先 `FlushMetadataQueue` → `metadataWorkerQueue()` 会 `ensureMetadataWorker()` 建新队列，但如果旧队列与旧 worker 还在，`sync.Once` 没被重置，拿到的还是旧队列。
3. B 的 `t.Setenv` 只改环境变量，A 的 TempDir 已经删了；哨兵被旧 worker 消费，`metadataWorkerHandle` 走非哨兵分支 → `WriteSessionMetadata(GetSessionsDir(), ...)` → `MkdirAll` 在 A 的已删除目录下重建。

实测复现（临时探针，已删除）：

```
用例A: TempDir(A) → Flush → Enqueue → RemoveAll(A)
用例B: ResetDrainedQueues()
→ BUG 复现：Reset 的哨兵让 worker 在已删除的用例 A 目录下重建了目录
```

同一探针在把 Reset 前置成"先 Close 再 Flush"之后变成 `OK：未重建`，说明修法很轻（cleanup 里先用一个不排空的 close+置空，让后续 Reset 看到 nil 直接返回）。

**影响**：这正是 `6ab8efbe0` 想灭掉的 `TempDir RemoveAll cleanup: directory not empty`。它被压下去了（因为新增的 flush 让多数用例恰好排空），但触发条件（用户例不 flush + 后例 Reset）没有消失，属于偶发红会被再次复现的问题。附带的真实后果不止测试：`ResetDrainedQueues` 是导出函数且注释写"仅供测试使用"，但没有任何编译期/运行期约束，生产代码误调会把队列清空。

---

## 三、一般

### M-1 `TerminalErrorEventTypes` 移植了，但消费者一个没接

**属于哪条流程**：runtime 事件流 → 各路消费者判定"这一轮到底成没成"。

`3b0451a78` 的理由陈述完全正确（`events.py:115-133`：`ok` 默认 True，consumer 必须同时看 event_type）。但 `IsTerminalErrorEvent` / `TerminalErrorEventTypes` 在全仓生产代码里 **0 个调用方**（只有 `events.go` 自己的定义和 3 个单测）——唯一改动是 `tests/integration/swarm/processcli` 里把断言从 `ok==false` 换成查集合。

Python 侧的消费者有 4 处：`agent_ws_server.py:667/4174/4211/4486`（心跳与会话消息执行器）、`cron/scheduler.py:2942/3135`、`runtime/service.py:3663`、`process_cli/live_input.py:220`。Go 侧对应位置：

- `internal/swarm/gateway/cron/scheduler.go:601` 仍只 `if resp.OK { succeeded }`；同文件里 `team.error` 0 命中，即 Python 那段「team.error 直接判失败并提前停止，避免等到超时」的逻辑不存在。
- 心跳/会话执行器的 outcome 判定在 Go 里没有对应聚合逻辑。

**为什么列一般而非严重**：这条路径当前不会崩溃，只是"失败被判成功"的语义缺陷又靠一层兜底掩盖了（processcli 用 `renderer.Failed()` 兜了一次）。但 `3b0451a78` 的提交信息把范围说成了"移植并连锁修正"，实际是"加了一张表和一组测试"，与描述不符；且提交信息里写的"各消费方共用本集合"目前是空的。

### M-2 `_resolve_request_owner_mode` 复刻时丢了 5 个前置分支，注释误引上游

**属于哪条流程**：`AgentManager` 收到请求 → `resolveRequestOwnerMode`（`server/runtime/agent_manager.go:997`）→ 拿 (mode, subMode) 去找/建 agent 实例。

`cac5fea8a` 为断环，在本层复刻了一份自包含模式解析。上游 `agent_manager.py:101-127` 的真实次序是：

1. `mode is not None` → 直接用（**这条是上游的核心短路，Go 完全没实现**：这是"调用方已解析过，别再解析一遍"）
2. `resolve_new_canonical_mode`
3. `compose_web_mode`（work_mode）
4. **`mode_text in {"agent","code","team"}` → 返回 `mode_text, sub_mode`（透传入参 sub_mode）**
5. `deprecate_mode` 再查
6. 兜底 `mode_text.split(".", 1)[0], sub_mode`

Go 的顺序是 1→2→3→(自造的 ④ 点集切分)→(自造的 ⑤ work_mode 重折算)，等于**把上游第 4 条换成了一个自造的、更复杂的推导**，而注释写的 `Python: request.py:305-330` 指的是另一个函数（`gateway/common/mode_matrix.py:resolve_request_mode` 的 legacy 分支），文档与代码对不上。

实测差异（探针）：

| 请求 | 上游预期 | Go 实际 |
|---|---|---|
| `{"mode":"agent"}` + `work_mode=code` | 走第 4 条 → `("agent", sub_mode原样)`＝`agent,""` | `code,"normal"` |
| `{"mode":"plan"}` | 第 6 条 → `"plan"` | `"plan"`（碰巧一致，因为兜底一样） |

即：**Web 端"agent 模式 + 代码工作目录"的请求，会被判成 code 模式**，从而命中 `code:*` 缓存键、拿到 code adapter 实例（工具集、提示词 profile 都跟着换）。这正是上游第 4 条要防的情况——它的语义是"裸 `agent` 就是 agent，work_mode 只影响运行时 profile"。

### M-3 agent 缓存 key 的 plan 子模式坍缩没移植

**属于哪条流程**：同一个 `agent_manager.go` → `_make_agent_cache_key(mode, sub_mode, project_dir)`。

上游 `agent_manager.py:140-155` 有一对必须成对使用的函数：

```python
_PLAN_SUB_MODE_ALIASES = {"agent": "", "code": "normal"}
def collapse_plan_sub_mode(mode, sub_mode): ...   # plan → 普通子模式
def _make_agent_cache_key(mode, sub_mode, project_dir):
    sub_mode_key = collapse_plan_sub_mode(mode_key, sub_mode)   # 关键一步
```

注释里写得很清楚（原文）：单 agent 的 plan 是**会话运行期状态**，不是另一种 agent 装配，"plan 与非 plan 用同一套 rails 和工具"；不并轨就会"换实例等于换掉 context_engine，整段对话历史凭空消失"。

Go 侧对应的缓存键函数没有这一步。后果：用户在会话中切 Plan 开关 → sub_mode 从 `""`/`normal` 变 `plan` → 缓存键变化 → 新建一个 adapter 实例 → context_engine 重来，对话历史丢失。这是上游专门写了注释防的 bug。

### M-4 从 Python 一次搬过来的 `reasoning_config.py` 少了 3 个函数，且归一函数对布尔值失效

**属于哪条流程**：模型保存/校验路径（Web/TUI 的 models 面板、process CLI `--model-*` 临时连接）→ `reasoning_level` 归一 → 校验 → 落到 `model_config_obj` → 请求期 `InjectReasoningParams` 读它。

(a) **缺函数**：Python `gateway/common/reasoning_config.py` 有 9 个公开函数，Go `schema/reasoning_config.go` 只有 6 个。缺的 3 个里，`validate_reasoning_level_for_model` 是**保存路径的必经校验**（Python 生产调用方 `config_panel/tui_models_handlers.py:336/513`、`1188/1365`），Go 是全仓 0 命中。同样缺 `reasoning_level_options`、`validate` 里"能力查询异常转 ValueError（让保存返回 BAD_REQUEST 而不是 500）"这条。

(b) **归一函数类型不对**：`NormalizeReasoningLevel`（`reasoning_config.go:93`）第一行 `raw.(string)`，非字符串一律返回空。实测：

```
nil → ""      false → ""      true → ""      0.0 → ""     "high" → "high"
```

Python 侧是 `str(raw).strip().lower()`，并且上游专门写了一整段注释解释为什么不能写 `raw_level or ""`——**YAML 1.1 会把裸 `off` 读成布尔 False，`on` 读成 True**。Go 这里把 `False`/`True` 直接吞成空（= 未设置），于是用户 yaml 里写 `reasoning_level: off`，会被静默当成"用厂商默认值"，思考反而开着。这是上游明令要防的场景，且属于"配置写了但不生效"这类最难排查的问题。

补一句：这条当前会不会真的炸取决于 YAML 解出来是 bool 还是 string（本项目 yaml 解析走的是 map 泛型解，`off` 通常解成 string），所以定"一般"；但归一函数本身与上游不等价，任何把该字段当 bool 的来源（JSON 配置、模型目录回填、前端传参）都会踩到。

### M-5 `live_input.go` 只搬了外层的壳，控制器主体整块缺失，且一处判定与上游相反

**属于哪条流程**：REPL 一轮执行中用户继续输入（steer）→ 父进程把文本写到 worker stdin → worker 回执 → 父进程展示。

Python `channels/process_cli/live_input.py`（404 行）的核心是 `LiveSessionInputController`（`live_input.py:230-404`，约 175 行）：`start`/`observe`/`pause`/`resume_after_event`/`_run`/`_deliver`/`_read_loop`/`_cancel_read`，以及它依赖的 `execution_id_from_event`、`build_steer_request`、`_copy_steer_context`、`_message_marks_target_changed`。

Go `live_input.go`（约 200 行）里：

- `LiveSessionInputController`、`ExecutionIDFromEvent`、`BuildSteerRequest` 全仓 0 命中；
- `ClassifySteerError` 是自造签名 `(message string) string`，上游是 `classify_delivery_error(code, message) -> LiveInputReceipt`（返回结构体而不是字符串），调用点因此无法区分 `rejected`/`unknown` 两种状态；
- `SupportsLiveSessionInput` 比上游多了一个 `executionID` 参数位置、少了 tty 双条件里的第二个条件（上游 `_is_tty(stdin) and _is_tty(stdout)`）。

偏差 E-9（go-prompt 无 alternate screen）已在计划里登记，但**登记的是"不做终端重绘"，不是"不做控制器"**。控制器里的准入判定（`_input_allowed` / `_target_ready` 两个 Event）决定"什么时候允许 steer、什么时候必须让位给交互问答"，是功能性逻辑，不是渲染。

### M-6 `permissions.tools.update_user` 仍然缺失（上一轮 B-S1 未修）

`permissions_config_rpc.go:66-120` 的分支只有 get / set / update / delete，Python `permissions_config_rpc.py:139-155` 有第 5 个分支 `PERMISSIONS_TOOLS_UPDATE_USER`，写用户层 overlay（`update_user_tool_level`）。Go 侧 `UpdateUserToolLevel` 早就实现好了（`permissions_layers_test.go:417` 还在测它），就是没接到分发器上。前端 PATCH `/permissions/tools/{name}` 带 user 语义时直接回 BAD_REQUEST。

这轮没有新增修复提交，属于**上轮已列严重项、本轮延续未处理**，这里降级登记为一般（因为本轮无相关改动，不是新引入）。

### M-7 `verify_ssl` / 证书路径这一类 SSL 实现，现在有 3 份各自不同的写法

同一个语义在三处被各写了一遍：

| 位置 | 取法 | 结果 |
|---|---|---|
| `anthropic/transport_pool` | 走 `security` 包 SSL context | 上次已按上游修过一轮 |
| `responses/transport.go:257` | `switch verify.(type)`，string 当 CA 文件 | 见 S-1 第 3 点 |
| `openai/request_builder`（chat 路径） | 各自处理 | — |

`responses` 这份是**新写的**，没有复用已有实现，也没有和 `ssl_utils.py` 对齐（缺"系统信任库 + 用户 CA 追加"、缺证书路径存在性校验、读失败静默降级）。规则 8/12 的隐含要求是"同一语义同一实现"，这里等于把一个已修过的坑重新挖了一遍。

### M-8 `reasoning_level` 的 `Pop` 顺序与上游不一致（`docs` 与代码方向相反）

`responses_api.go:97-110`：

```go
reasoning := extra["reasoning"]
delete(extra, "reasoning")
extraBody, _ := extra["extra_body"].(map[string]any)
delete(extra, "extra_body")
...
if reasoning != nil { requestExtra["reasoning"] = reasoning }
```

上游 `openai_model_client.py:1585-1590` 的顺序是：先 `model_dump` 得 `request_extra`，再 `pop` 出 `reasoning`/`extra_body`/`tool_choice`… 然后 `request_extra.update(kwargs)`（kwargs 覆盖 config 值）。Go 先 pop 再 merge，`extraBody` 与 `modelDumpExtra` 的覆盖次序又是"extra_body 覆盖余量"（`:112-118`），两处覆盖方向都依赖当前巧合成立。这一条 09d6fbb76 修过一次（config 级 reasoning），但 kwargs 与 extra_body 的相对优先没有对拍用例守着，属于随时会翻的灰区。

---

## 四、提示

1. **`docs/go` 头注释里的行号开始漂移**：`agent_manager.go:1013-1015` 引 `request.py:305-330`（应该是 `agent_manager.py:101-127` 或 `mode_matrix.py:413-489`）；`reasoning_config.go:93` 引 `:92-118`（实际 `normalize_reasoning_level` 在 `:79-102`）。方向没错但行号差 13 行，按注释去找会看到不相关的函数。

2. **`reasoning_config.go` 缺 `__all__` 对应的导出面**：Python 有 9 个导出符号，Go 6 个，且没有任何"已知偏差"说明为什么少了 3 个。按规则 9.2，枚举/成员集合不得因"Go 侧用不到"裁掉，函数同理，至少要登记。

3. **`responses` 包的 doc.go 说"一 Python 文件对一 Go 文件"，实际是两个 Python 文件并进 `utils.go`**：`responses_utils.py` 784 行 + `responses_transport.py` 175 行 → `utils.go` 1170 行 + `transport.go` 320 行，比例大致对，但 `ParseResponse` / `MessageFromStreamChunk` 在 Python 里是模块级函数、在 Go 里也是包级函数，这条对得上；真正的问题是 `StreamParser` 的方法集拆成了 6 个 `(p *StreamParser)` 方法 + 5 个包级 `xxxChunk(event)` 助手，与 Python 里 `ResponsesStreamParser` 的 12 个私有方法不一一对应（规则 9.2 方法归属）。影响可读性，不影响行为。

4. **processcli 包测试基线太薄，覆盖率靠新增用例堆**：`20.3` 计划写覆盖率 50.1%，本轮补了 ~110 例到 61.9%，但 `Run`/`consume` 的成功分支、四个会话操作成功路径、`runWorker` 全部未覆盖（提交信息自己承认了）。按规则 3 的 85% 目标差距仍大，建议在 20.10 里明确"哪些是可测的、哪些必须等 clientFactory"。

5. **`session_history.go` / `session_metadata.go` 里"仅供测试使用"的导出函数已有 4 个**（`ResetHistoryWorker`/`ResetMetadataWorker`/`ResetDrainedQueues`/`ClearAllSessionMetadataCache`），无任何保护。建议加 `_test.go` 同名内部版本 + 导出包装，或在 doc 注释里写明"生产不得调用"（现在只有前者做到了）。

6. **`live_layout.go` 的"偏差 E-9 退化形态"注释过多、实质代码少**：242 行里约 90 行是注释解释为什么不做（无 alternate screen）。注释有价值，但同样内容在 spec、计划、文件头重复了三次，维护时三处会漂。

7. **`ui.go` 自建东亚宽度判定（偏差 E-3）**：`ui.go` 里手写了 east-asian wide 区段表。这类表一旦不全（emoji、变体选择符、组合字符）就会出现中文对齐错位，且没有对拍用例。建议补一组与 Python `wcwidth` 的对拍表。

8. **`6ab8efbe0` 的失败路径没有覆盖**：超时分支现在"不再等执行体收敛"，但如果执行体在超时后仍然往 `consume` 里写（`renderer.Render` 仍被调用），会和主 goroutine 的 `renderer.Finish` 抢同一个 writer。按 commit 的推理（`asyncio.timeout` 立即抛出），Python 侧靠 task 取消兜；Go 这边 `execute` 的 goroutine 没有任何取消信号，只能等 `client.Stream` 自然返回。实测当前 `Stream` 在无模型时立即返回所以看不出来，接上真模型后这段会变成"超时后仍在打日志/写 stdout"。

9. **`--list-sessions` 与 `--latest-session` 退出码不对称**（0 vs 1）在 20.3 spec 里写了是"对齐上游"，但 `session_entry.go` 的两条路径用了不同的错误码常量（`exitArgError` vs 直接 1），建议统一到一个常量，避免后续改动时漂移。

10. **`git show 61ecfca21`（分段注释）质量不错**，但 `root_permission_queue.ReserveAnswer` 的八步编号与代码里的空行分组不是严格 1:1（第 4/5 步之间没有空行）。注释要求"便于定位每一步失败点"，弱一致性会让定位失效。

11. **`response_input_tool_call` 的 `randomCallID()`（`utils.go:506`）在 Go 里没有对拍**：Python `_response_input_tool_call` 在缺 `call_id` 时生成 `call_<随机>`，Go 用 `crypto/rand` 还是 `math/rand` 未确认；这类"随机串形状"会影响前端按前缀分流，建议加一条形状断言。

12. **`20.9` 状态仍是 `☐`，但描述里写"`diagnostics.py` 已整体移入 20.3"**——即该节实际上已经没有任何剩余工作，应标 ✅ 或明确"并入 20.3，本节关闭"。同类问题：`IMPLEMENTATION_PLAN.md` 里 `20.7`（权限与策略）也仍是 `☐`，而 `machine_policy.go` 已存在并有测试。

---

## 五、已核实无问题的部分

- **20.2 的 25 步编排 / 七步 cleanup / 双屏障**：与 `machine.py` 逐段对得上，两个屏障各自独立超时（`persistence.go:33-52`）与上游 `asyncio.gather` 语义一致。
- **`mode_matrix.go` 的 8 张表 + 20 个函数**：与 `gateway/common/mode_matrix.py` 逐表比对一致，`ResolveRequestMode` 的两个分支（web 组合 / legacy）与上游 `:413-489` 顺序一致。
- **`url_utils.go` 的 NO_PROXY 匹配**：`get_proxy_url` / `should_bypass_proxy` / `_extract_hostname` / `_get_no_proxy_list` / `_hostname_matches_no_proxy` / `_is_ip_match` 六函数逐条与 `url_utils.py:40-160` 对齐，含 `"*"`、后缀、IP/CIDR 四个分支与顺序。
- **`reasoning_injector.go` 的六步归一**：agentos `max_tokens→context_window` 迁移、内部键清理（含 `_source`）、`context_window` 非正数丢弃、`model` 回填，与 `reasoning_injector.py:91-138` 一致；偏差 E-5（不做 `core_has_context_window_field()` 探测）已在文件头登记。
- **Anthropic `readStreamEvent` 的 bufio 共享修复（`f7b8aafc7`）**：诊断准确，`bufio.Reader` 预读吞流是真实缺陷，修法（reader 提到循环外）正确。
- **`3b0451a78` 里 `NewAgentRuntime` 自建 manager**：与 `plane.py:155` 的 `SingleTenantStrategy(agent_manager or AgentManager())` 语义一致，`TestAgentManager_未注入时自建` / `注入时用注入值` 两条用例有效。
- **`57a86d6f3` 的空 const 块清理 + doc.go 补条目**：9 处 `const ()` 全部删净，6 个 doc.go 条目补齐的文件都存在，golangci-lint 从 12→0 可信。
- **`6ab8efbe0` 里 flush 哨兵改非阻塞投递**：这是真实缺陷（等待方超时后 worker 永久阻塞，此后所有会话写入失效），修法正确，`flushDone` 缓冲 1 也解释了偶发超时。
- **本地验证**：`go build ./...` 通过；`go vet`（受影响包）通过；`internal/swarm/...` + `internal/common/...` + `llm/...` 单测全绿（`memory` 包需 `-tags sqlite_fts5`，未带标签时的 FAIL 是既有约束，非本次引入）。
