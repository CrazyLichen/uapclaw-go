# 2026-11-14 48 小时提交审查报告

- 审查范围：2026-11-12 04:00 ~ 2026-11-14 04:00（最近 48 小时）
- 提交规模：163 个提交（含多个合并提交）
- 审查方式：逐领域抽核心文件与 Python 上游逐函数对拍（`agent-core` / `jiuwenswarm-develop` 本地源码）；PyPI intelli-router wheel 本地无解包留存，23.4a 只做结构性与自洽性审查
- 结论：**严重 1 / 一般 5 / 提示 6**

## 一、这 48 小时做了什么

| 块 | 内容 | 提交量 |
|---|---|---|
| 15.5 harness/cli | provider 基座 + factory 全量装配 + repl 主循环 + 渲染/存储/prompts/rails + `cmd/uapclaw-debug` + 集成测试，15.5 打 ✅ | 约 35 个 |
| 23.4a intellirouter | PyPI wheel 移植 24 文件（core/router/strategy/provider/health/observability），裁剪项登记 D-1~D-4 | 约 20 个 |
| 20.4 duplex | duplex_input/protocol/control 三件 + machine 接线 + `--run-jsonl` 端到端集成 + race 三缺陷修复 | 约 15 个 |
| 10.3.31b/31c | topic 流水线（model/audit/terminal/url_safety/sources）+ 31c 零依赖任务并入 | 约 15 个 |
| 23.2 收尾 | config 级 reasoning/context_window 并入 request_extra、两轮历史回放集成测试 | 3 个 |
| 20.3 P-7 回填 | `uapclaw-process` 真二进制端到端集成 8 例 | 3 个 |
| 解环 + 散修 | TERMINAL_ERROR_EVENT_TYPES 移植、runtime↔server/runtime 断环、SessionManager 搬回 server/session、DeprecateMode 六调用点、mode_matrix/reasoning_config/injector | 约 10 个 |
| CI / 风格 | race、gofmt、注释中文化、plan_status 陈旧快照修复 | 约 8 个 |

整体评价：**对齐质量持续在线，注释行号抽查基本属实**（`duplex_protocol.py`、`duplex_input.py`、`duplex_control.py`、`topic_url_safety.py`、`topic_model.py`、`topic_audit.py`、`factory.py`、`runner.py` 的行号引用都能对上）。上一轮记忆里的「Go embedding 虚方法派发陷阱」这次在 23.4a 里被主动规避了（`reliable_router.go` 构造期注入 `hookAvailableDeploymentsForModel`，注释明确写 py:206-212 review 16），说明教训已经内化。桩管理也规范：`/auto-harness` 桩挂 ⏵ 领域十四、`currentBackgroundGenerationAttemptID` 桂 ⤴️ 31b（topic_pool 尚未实施，属合法待回填）、D-JM/D-LP 偏差均登记。

问题集中在：**一个校验正则没锚定导致防线失效**（S-1）、**一个构造错误被吞掉会变 panic**（M-1）、**Ark 失败归因在生产客户端下永远命不中**（M-3），以及几处注释与事实脱节 / 消警报写法。

---

## 二、严重

### S-1 topic_audit：failure_reason 的机器可读码校验完全失效（正则没锚定）

**属于哪条流程**：外部调用审计小票（`topic_audit.py` 模块职责就是"manifest-safe counters"——保证进入审计账本的小票字段是干净、机器可读的）。调用链：各 topic 阶段调用 `ExternalInvocationAudit.RecordFailure(operation, ..., failureReason, ...)` → `record()` 校验 → 写入 `TopicStarterStore.RecordEvent` 的 payload。

**缺陷**：Python（`topic_audit.py:35`）是

```python
_FAILURE_REASON_PATTERN = re.compile(r"[a-z0-9][a-z0-9_.-]{0,127}\Z")
...
_FAILURE_REASON_PATTERN.fullmatch(failure_reason)  # 整串必须匹配，且限 128 字符
```

Go（`topic_audit.go:34,294`）是

```go
failureReasonPattern = regexp.MustCompile(`[a-z0-9][a-z0-9_.-]{0,127}`)
...
if status == "failure" && !failureReasonPattern.MatchString(failureReason) {
```

两个问题叠加：①Go RE2 没有 `\Z`，也没写 `^...$`，`MatchString` 是**子串匹配**——`"!!! Bad request with spaces!!!"` 里含子串 `Bad`…（首个 `[a-z0-9]` 命中 `ad`…实际上任意含一个连续小写字母数字段的串都过）；②长度上限 `{0,127}` 只约束那一段子串，**整串可以任意长**。等价于校验形同虚设。

**影响**：任意脏字符串（带空格、中文、超长）都能作为 `failure_reason` 进入审计快照并持久化，破坏该模块"脱敏、机器可读"的核心契约；下游若按此字段做归因/聚合会把口径污染。修法：改 `^` + `$` 锚定（RE2 支持 `^(?:...)$`）或 `regexp.MatchString` 换成手写全串校验，并补一条"含空格/超长拒绝"的用例。

---

## 三、一般

### M-1 topic_model：`BuildArkStageInvoker` 吞掉 clientFactory 错误 → 后续 Invoke 直接 panic

**属于哪条流程**：后台推荐模型的 Ark 适配器构造（Python `topic_model.py:210` `client = client_factory(...)`，构造失败异常直接上抛，调用方根本拿不到 invoker）。

**缺陷**：`topic_model.go:310` 是 `client, _ := clientFactory(config.APIKey, config.BaseURL)` —— 工厂失败时 client 为 nil，`BuildArkStageInvoker` 仍返回对象；等到 `Invoke` 里调 `inv.client.CreateChatCompletion(...)` 才炸，且是 nil 接口方法调用的 panic 而不是可归因错误。Python 语义是构造期 fail-fast。修法：构造函数返回 `(*ArkStageInvoker, error)`，工厂失败即上抛（并按 Python 语义这属于装配失败，不产生小票）。

### M-2 topic_model：默认 HTTP 客户端不带厂商错误码，Ark 专属失败归因在生产路径永远命不中

**属于哪条流程**：`provider_failure_reason`（Python `topic_model.py:168-186`）——把 provider 异常归因为 `account_overdue` / `model_unavailable`（含「Ark 关闭端点回 400」与「模型拒绝 response_format 回 400」两个 Ark 专属分支）/ `invalid_key` 等，供拒绝类熔断（`PROVIDER_REFUSALS`）使用。

**缺陷**：Python 的 `exc` 来自 openai SDK，`exc.code` / `exc.param` / `exc.message` 都从响应体解析。Go 默认客户端 `defaultArkChatClient.CreateChatCompletion`（`topic_model.go:762-764`）失败时只填 `HTTPStatus`，`CodeStr`/`ParamName` 恒空、`Msg` 是固定串——于是：

- `Code() == "AccountOverdueError"` / `"InvalidEndpoint.ClosedEndpoint"` 两个分支**永远不命中**；
- `400 + response_format` 分支也**永远不命中**（生产路径下 Ark 关闭端点回 400 时，Python 判 `model_unavailable`，Go 判 `provider_request_failed`——前者属于拒绝类熔断，后者不属于，熔断行为分叉）。

修法：默认客户端解析响应体 JSON 的 `error.code` / `error.param` 填进 `ProviderFailureError`（非 200 时 body 就是错误结构），并用 httptest 补一条 400+closes-endpoint 响应的归因用例。

### M-3 runner.go 桩注释与事实相反（15.5 已落地 create_agent，注释仍说"等 15.6-15.9 回填"）

**属于哪条流程**：非交互运行模式 `runner.RunOnce`（Python `runner.py`）。这是审查点「依赖已就绪但没正确回填」的直接命中——虽然这次是反过来的：**代码其实已经接上真身了，但登记的桩状态没更新**。

- `runner.go:44-45`：「⏵ backend 的 run_streaming 依赖 create_agent 回填（15.6-15.9）；当前骨架后端返回空流」——但同批 `agent/factory.go` 已交付 `CreateAgent`/`LocalBackend` 真身，`RunOnce` 实际走的 `CreateBackend→Start→RunStreaming` 是完整链路；
- `ui/runner/doc.go:8` 同样的过期表述。

**影响**：下个会话或实施者读计划/注释会误判 15.6-15.9 还欠一笔，可能做出重复实现或错误裁剪。修法：删掉这两处过期桩注释（规则 8：失效注释同改动清理）。

### M-4 factory.go 注释乱码（8 处 UTF-8 双重编码损坏）

`internal/agentcore/harness/cli/agent/factory.go` 行 167/179/191/212/226/283/289/296 的中文注释是 mojibake：`ç¼ç  Agent`、`æµè§å¨ Agent`、`è®°å¿ rail`、`å兜åºï¼å¤ç¨ä¸»æ¨¡ååæ®` 等（大概率是某次批量替换/工具写入时编码二次损坏）。违反「注释使用中文」规范的初衷（中文注释读不了），且这几处恰好是 `BuildSubagents`/`LoadVisionConfig`/`CreateAgent` 的关键分节注释。逐处重写即可。

### M-5 plan_status 修复是兜底治标，随机取 agent 的根因仍在

**属于哪条流程**：网关 `plan_status` RPC（`handle_session_plan.go`）→ `agentManager.GetAgentNoWait(channelID, "", "", "")` 取 agent → `PlanModeSnapshot` 读 live 快照 → `combineSessionInPlan` 裁决。

2026-11-14 的 439bfbfe2 修复方向正确（flush 元数据队列 + `HasSessionRuntime` 门控 + live/metadata 矛盾时以 metadata 为准），集成用例偶发 `in_plan=true` 的现象应该消失。但根因——**全空参 `GetAgentNoWait` 在多 agent 渠道按 Go map 遍历"随机"返回一个 agent（Python dict 是插入序）**——没有修：新加的 `live==plan && metadata==normal && !active → false` 三条件裁决只覆盖「live 说 plan、metadata 说 normal」一种矛盾；若随机命中的 agent 恰好持有该会话运行时但其 plan 快照已陈旧（退出 plan 的收尾事件未送达），仍可能翻转。建议后续把「按 sessionID 找承接 agent」做成一等查询（`GetAgentForSession`），而不是在裁决函数里逐条打补丁。

---

## 四、提示

### T-1 duplex_protocol：JSON 大整数精度丢失

Python `_copy_json` 对 `int` 精确保真；Go 侧 JSON 解码全落 `float64`（`duplex_protocol.go:251`），`answers`/`tool_result.result` 里超过 2^53 的整数会丢精度并可能以科学计数法回写。当前消费面无大整数场景，登记为已知平台差异即可（与 D-6 整值 float 决策同族）。

### T-2 topic_url_safety 两处边角

① `parsePort` 拒绝端口 0，Python `urlparse("http://h:0").port` 返回 0 并交给 getaddrinfo（行为差异极小）；② `isGlobalAddr` IPv6 分支只排除 fc00::/7，Python `is_global` 还排除 2001:db8::/32（文档段）等保留段——内网若配置了文档段地址不会被拒。SSRF 防线建议对齐补齐。

### T-3 duplex_protocol `ToDict` 吞掉拷贝错误

`duplex_protocol.go:172,180` `copied, _ := copyJSON(...)`——Python `to_dict` 内部再跑一遍 `_copy_answers`，非法数据会抛；Go 静默吞掉。解码路径的数据已校验过所以当前无害，但这是「静默吞错」模式的种子，建议至少带上错误返回或注释说明为何可忽略。

### T-4 duplex_control 尾部消警报写法

`duplex_control.go:887-889` `var _ = fmt.Sprintf` / `var _ = strings.TrimSpace` / `var _ = time.Second` 三连；`topic_model.go:661` `var _ = structuredOutputMode` + 永不被调用的 `structuredOutputMode`。规则 8.2 明令禁止 `_ =` 消警——正确做法是删掉 import / 删掉未用助手函数。

### T-5 /auto-harness 桩的默认分支行为与 Python 不同

Python `_cmd_auto_harness` 对未知子命令执行 `_subcmd_run(["--goal", args_str])`（自然语言目标直接跑优化流程）；Go `cmdAutoHarness`（repl.go:719）对 default 分支打印 help。桩本身已登记 ⏵ 领域十四，合法；但回填时注意 default 分支语义别丢——建议在桩注释里记一笔「Python default 分支 = run --goal」。

### T-6 duplex_input UTF-8 读错误路径缺失

Python `read_line` 捕获 `UnicodeError` 给专用消息「line must be valid UTF-8」；Go 在 `readAvailable` 阶段不做 UTF-8 校验（分帧后在 `takeLineLocked` 校验），错误消息与 Python 有差——协议层诊断文案属前端可依赖字面量的风险面（参照 v0.1.1 白名单失配教训），建议核对消费方是否匹配该消息。

---

## 五、复核通过的抽查点（白名单）

| 抽查对象 | 结论 |
|---|---|
| `duplex_protocol.go` ↔ `duplex_protocol.py` | 字段白名单/必填/exactly-one/防御拷贝/to_dict 逐段对应，`hasKey` 对齐 Python `in` 语义 |
| `duplex_input.go` ↔ `duplex_input.py` | 三态 readAvailable、悬置 CRLF、`max+2` 缓冲上限、单活跃读互斥逐条对齐；裸 syscall+EINTR 是合理平台实现 |
| `duplex_control.go` ↔ `duplex_control.py` | consume 主循环步骤序、完成判定三形态+suspended 排除、unattended 自动 reject、stop_input 排空语义对齐；队列 32/pending 16 常量一致 |
| `topic_url_safety.go` ↔ `topic_url_safety.py` | 主链路（metadata 主机/内网后缀/单标签/链路本地/非全球）对齐；`isInvalidPortFailure` 补齐 Go url.Parse 提前报错的语义差 |
| `topic_audit.go` 主体 | attempt/record/snapshot/load 的校验序、序列号、上限淘汰对齐（除 S-1 正则） |
| `topic_model.go` 状态机 | started→received→refused/truncated/metadata_failed→json_failed→lineage_failed→parsed 与 Python 逐段一致；validation_receipts 指针身份是合理的 id() 等价 |
| 15.5 `factory.go`/`repl.go` 结构 | create_agent 装配序、LocalBackend 热加载、REPL slash 分派/技能展开/中断恢复循环对齐；D-JM/D-LP 偏差登记清晰 |
| 23.4a `reliable_router.go` 构造 | 冷却默认 60s/重试 3/timeout 30、deployment 初始 COOLDOWN 同步 state、tpm/rpm 配额接线、覆写钩子构造期注入（虚方法陷阱已规避） |
| 23.2 `9d6fbb764` | model_dump 余量补 context_window/reasoning + kwargs 覆盖顺序，与 openai_model_client.py:1585-1589 一致 |
| TERMINAL_ERROR_EVENT_TYPES | 五成员集合一致，且真消费者接线（心跳/会话执行器）在上轮已完成 |
| 桩回填扫描 | 48h 触及包内 ⤴️/⏵ 标记的依赖目标（31b topic_pool、领域十四 auto_harness、15.6/15.15）均未实施，无「依赖已就绪未回填」项（仅 M-3 的反向脱节） |
