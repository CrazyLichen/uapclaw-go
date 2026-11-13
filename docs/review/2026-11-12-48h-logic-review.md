# 2026-11-12 48小时提交审查报告

- 审查范围：2026-11-10 04:00 ~ 2026-11-12 04:00（近 48 小时）
- 提交规模：337 个提交（约 319 个非合并提交，约 15 万行净新增）
- 审查方式：按业务域拆 6 组，逐提交对照 Python 上游源码（agent-core 5a35c39f / jiuwenswarm-develop 0.2.5.beta1）核实
- 结论：**严重 12 / 一般 26 / 提示 43**

## 一、总体评价

这 48 小时主要交付了 6 大块：23.1 Anthropic 客户端、9.90e 权限护栏与中断交互、10.7.1 话题起始 store、20.2 一次性执行协议、15.4 检查点与序列化、25.1d 异步工具与 24.14 用量归属，外加一批 CI/风格修复。

整体对齐质量不错：多数提交注释里写了 Python 行号且抽查属实；17 处权限桩回填真实完成、数量与 Python 逐文件一致；旧符号删除基本干净；10.7.1 的 SQLite 层没发现注入、事务漏用；CI 修复（wg.Wait 复用 panic、agentRuntime 原子指针、owner_scopes 回归）本身都改对了。

但每块都还有「看起来做完了、关键路径其实断着」的问题，尤其是三条横切性根因值得优先处理：

1. **删包/迁移时能力没跟着搬全**：删掉 inference_affinity 包后，KV 亲和协议在 Go 侧被硬编码成「不支持」，配置了 affinity 的部署整个静默失效（严重）。
2. **「回调式流 + channel」没有等价实现 Python 的超时/取消传播**：20.2 的超时只做事后补判不真打断，Ctrl-C 被归成错误退出码 1 而非 130（两条严重）。
3. **同一根因只修被打红的那条路**：JSON 深拷贝把 RunContext 降级的问题这次只修了 GetTask 读取路径，GetState/GetChildTask 快照路径没修，goal 硬刹车跨检查点恢复会再坏一次。

---

## 二、按域明细

### 域 A：23.1 Anthropic 客户端（严重 5 / 一般 6 / 提示 10）

**严重**

- **A-S1 tool_choice 转换丢了两个分支，输出形状还是错的**。OpenAI 形状的 `function.name` 该从嵌套里读，Go 读顶层拿不到 → 强制工具选择被静默丢掉；且命中时输出的 `type:"function"` Anthropic API 不认（应为 `tool`）。convert_blocks.go:97-117 vs anthropic_model_client.py:221-236。
- **A-S2 流中途读错误全被当成「正常结束」**。网络中断、ctx 取消、服务端半途断连统统折叠成 done，failStream 成死代码，LLM_CALL_ERROR 不发。用户长回答断网时拿到半截内容且无任何错误信号。event_chunk.go:194-217。
- **A-S3 连接池的 key 和驱逐的 key 算法不一致，热重载驱逐大概率打不中**。入池时 baseURL 被归一化（剥 /v1），驱逐时用原始字符串。最常见的 api_base 写法下旧凭证连接永不关闭。transport_pool.go:159-165。
- **A-S4 非法图片输入直接 panic**。Python 是 raise 后被上层接住变成模型调用错误，Go 一路 panic 上抛无人 recover，用户传坏图片 data URL 会崩调用链。convert_blocks.go:205-222。
- **A-S5 SSL 三处偏差**：丢系统信任库（只装用户 CA）、没有证书路径约束（安全项）、证书读失败静默降级。transport_pool.go:72-92 vs ssl_utils.py。

**一般**：BuildAnthropicParams 漏了 spec 明确列出的 reasoning_request_controls 步骤（openai 侧已接，anthropic 侧漏）；dict 消息场景 replay 回填与 transient 判定整体失效；Invoke 解码失败无错误日志无回调；LastInputIsTransient 生产没接线且有两份逻辑；连接池只有 idle 上限没有总连接上限（Python 是活跃总数 100）；ConvertToolSchemas 字段优先级与 Python 相反。

**提示**（10 条）：Stream 结尾多补发一次响应 tracer；parse 失败无警告日志；usage chunk 的模型名来源不对；非 200 也先发 LLM_RESPONSE_STARTED；错误回调从不设 ErrorMessage；两处日志文案不一致；死代码 anyArg/mustJSONArgs；config 包注释还在宣传已删除的 fsnotify 热重载；tool arguments 为 JSON 数组时落 _raw_arguments；model_name 非字符串时跳过条目与 Python 行为不同。

**已核实无问题**：S0-1/S0-2/S0-3/S0-5/S0-6（删除干净）、13 种事件、provider_error 12 函数、三断点缓存、spec 登记偏差未越界。

### 域 B：9.90e 权限护栏 + 8a/8b 中断交互（严重 4 / 一般 4 / 提示 6）

**严重**

- **B-S1 缺整个 `permissions.tools.update_user` RPC 分支**。前端调它更新用户层工具级别时 Go 直接回 BAD_REQUEST。讽刺的是 `UpdateUserToolLevel` 早已实现，就是没接到路由上。spec §4.8 和计划 Task 4 白纸黑字列了这个分支。
- **B-S2 `permissions.tools.get` 读错了数据源**。Python 返回 global⊕user⊕session 三层合成后的生效权限，Go 返回 config.yaml 静态段。用户层/会话层临时授权过的工具，前端权限页看不到。
- **B-S3 `permissions.tools.set` 写错了层**。Python 拆 deny/ask/allow 写用户层 overlay，Go 整表替换配置文件。deny_guidance 语义、session 叠加行为全不对。这三条都属 spec 自己列了但代码没照做，且违反规则 13.2（缺失项应提出来讨论，不能默默换实现）。
- **B-S4 `project_interruption_snapshots` 开关整条丢失**。Python 里「中断结束后发普通恢复提示」默认是关的（opt-in），Go 无条件发。8b-8 收口时把这个门吞了，spec §4.3 伪代码里明明有。

**一般**：runtime_bridge 头部偏差注释已过时且说反（说没做的其实做了）；观测钩子吞 panic 不打日志（注释声称对齐了 Python 的 logger.exception，实际一字未打）；审批校验失败丢错误链无日志；AutoPermissionRail 共享字段零锁，配置热更新与工具审批并发时可能读撕裂。

**提示**：merged_match 边界取值与 Python 的 get 短路行为不同；readValueField 只认 string 会丢列表键；config 层每次 RPC 重读 YAML 无缓存；interrupt_helpers.py 一个文件被拆成 7 个 Go 文件（规则 9.1 打破，未见偏差登记）；计划文档 Task 4 全没勾但 IMPLEMENTATION_PLAN 已标 ✅，勾选状态掩盖了缺口；审计 JSON 非 ensure_ascii（已有决策备忘）。

**已核实无问题**：17 处桩回填数量逐文件与 Python 一致；删除重复通道旧符号 0 命中；隔离双写加锁正确；中断判据逐行对齐。

### 域 C：10.7.1 话题起始 store（一般 4 / 提示 5，无严重）

**一般**：常量块注释里的 Python 行号系统性漂移 3~7 行（机器核对，8 处），按注释去找会对错地方；文件头类行号范围写短了；pythonTruthy/pyStr 引的行号指到不相关函数；LoadLatestSnapshot 遇非 dict JSON 时 Go 继续扫旧行可能返回旧快照（Python 立即返 None），行为差异未登记偏差。

**提示**：card_revision_ids 对字符串的计数语义不同；payload_json 坏行时 Go 整个 ListEvents 报错（Python 返回 list）；prune 遇坏 payload 行 Go 静默归类可能误删；created_at 数字字符串解析差异；spec/plan 文档日期倒挂（写着 2026-11-19/11-26，早于实际提交时间）。

**已核实无问题**：InterestDrift 三信号逻辑与行号一致；16 项 TTL 机器核对两侧一致；pyStr 浮点对拍正确（1.0/-0.0/1e16 指数格式全对）；SQL 全参数化无注入；10.7.2 无虚勾。

### 域 D：20.2 一次性执行协议（严重 2 / 一般 4 / 提示 7）

**严重**

- **D-S1 timeout_seconds 执行期间完全不生效**。Go 只算 deadline 时间戳，从没挂到 ctx 上，流跑多久都不被打断，整轮跑完才在步骤 25 事后看一眼表。设 10 秒超时、流跑 10 分钟，token 照烧，最后才报 TIMEOUT。Python 是 asyncio.timeout 真打断。
- **D-S2 Ctrl-C 终态错位**。信号/ctx 取消后生产协程直接 close 不发错误帧，Stream 正常结束返回 nil，最终被打成 INCOMPLETE_RUN → 退出码 1，而 Python 契约是 CANCELLED/130。

**一般**：handler 停流后生产协程永久阻塞（Python 有 aclose 真终结生成器，Go 只 break）；cleanupStep 的 5 秒窗口对不可取消的操作无效（传 Background 或函数不收 ctx）；RunQuery 丢 CANCELLED 分支且收了 ctx 从头到尾没用；TimeoutError 的「非 deadline 爆的错保留原码」区分丢失。

**提示**：cleanup 少 session_writes 屏障步骤；session_id 一处 trim 一处不 trim 可能恢复错会话；mcp.validate 响应缺 references 键；executeDocument 缺 panic 兜底；asyncio 关停哨兵无对应物；NaN 字符串字面量误报；会话开通委托未对齐上游 lifecycle_lock 全程持锁。

**已核实无问题**：25 步编排、七步 cleanup、双屏障、OutputError 哨兵在单测/golden 层都对得上。

### 域 E：15.4 检查点/序列化 + 23.3（严重 1 / 一般 5 / 提示 6）

**严重**

- **E-S1 KV 亲和协议在 Go 全链路静默失效**。删 inference_affinity 包后，`SupportsKVCacheAffinity()` 硬编码 return false，agent_hint 注入全仓 0 命中。Python 里 affinity 是 openai 客户端的真实能力。配置了 `kv_cache.mode=affinity` 的部署整个协议变 no-op，且 spec 只登记了 Release 形态、没登记 invoke 侧缺失。

**一般**：sanitizeToolCalls 没接进请求构建链路（SiliconFlow 严格校验会拒非标 tool_calls，删包后防线断链）；DropOrphanToolCalls 是死代码（函数+单测有了，生产没接线，测试绿但行为没生效）；disabled-thinking 判据文本收集不完整，code/type 在 params 里不在 Error() 文本里可能漏判重试；集成测试 checkpointFromDict 少还原 provider_session_id/revision 两个字段（往返有损，注释自称对应 member_runtime.py:170-190 实际缺）；HarnessEventToDict 吞错降级产出必解码失败的死信（Python 直接抛错）。

**提示**：未知事件分支比上游多一次 Freeze 可能破坏 NaN 透传；解码端把显式 null 当缺省（Python 抛错）；解码错误文案缺统一前缀（进 payload.error 会破坏前端白名单，参照 v0.1.1 教训）；删包后残留以旧客户端为主语的注释 4 处；测试名与实际行为不符；测试 sink 回执 revision 格式与 Python 不一致未声明。

**已核实无问题**：jsonsafe 逐分支对齐；13 个枚举无丢值；七对值对象编解码无丢字段；集成 9 例断言真实非恒真；并发面（加锁、深拷贝、sink mutex）无共享可变状态问题。

### 域 F：25.1d 异步工具 / 25.1e / 24.14 / 10.3.27 / CI 修复（一般 3 / 提示 9，无严重）

**一般**

- **F-M1（最重要）GetState/GetChildTask 仍是 JSON 深拷贝**。52ccdd0fc/6813b283b 只修了 GetTask 读取路径，快照保存→恢复路径（GetState→LoadState）没修——凡任务 metadata 带 run_context 的任务经历一次会话恢复/检查点，run_context 降级成 map，goal 硬刹车再次失效。这是本次「同根因只修一条路」的典型。
- **F-M2 RenderResultText 缺 `_to_jsonable` 等价层**。Python 序列化失败时递归 model_dump、set 排序、深度 50 截断；Go 直接 fmt.Sprint，struct 会输出 `&{...}` 指针垃圾文本注入 leader 上下文。
- **F-M3 快照 + UpdateTask 全量替换 = 并发丢更新**。GetTask 快照与 UpdateTask 之间调度器的 Status/ErrorMessage 写入会被旧快照覆盖。Python 同构但单线程无此窗口，Go 并发模型下是活引用→快照迁移的未闭环处。

**提示**：Cancel 对已终态任务返回值语义与 Python 相反；run() 的 err==nil&&ctx.Err()!=nil 是 Python 无对应的新增逻辑未登记；缺 BudgetExhausted 专属告警分支；Cancel 与结算有竞态窗口（取消后可能落成 completed）；归属字段多覆盖 agent_id 键；ctx.extra 三键写入条件不一致（Python 缺省也写空串）；StreamController.Stop 少 cancel_team_idle 与 served_model_tasks 收尾（既有缺口非本次引入）。

**已核实无问题**：Cancel「tasks 完成即删/events 独立保留」语义通过；GetTask 浅拷贝本身竞态安全（写锁/读锁隔离 + 只读契约）；nil inputs 兜底移除后调用方全覆盖；SwitchMode 落 live instance 理由成立；四级用量归属回退链逐行一致；四项 CI 修复（done 快照、atomic.Pointer、owner_scopes、doc.go）本身全部改对。

---

## 三、共性问题（大白话总结）

1. **删东西的时候能力要跟着搬**。删包不只是删代码，包里生效的能力（affinity 注入、SiliconFlow 清洗、孤儿裁剪）必须先确认在原地有承接，否则就是「测试绿但功能死了」。
2. **Python 的 asyncio 超时/取消不是摆设**。Go 用 channel 流时，超时要变成 ctx deadline 贯穿整条链，取消要有错误帧，否则超时变摆设、Ctrl-C 退出码都对不上。
3. **修 bug 要修一族，不是修一个**。JSON 深拷贝降级、池 key 不一致这类问题，打红一条修一条，相邻同构路径不查就留着下次再爆。
4. **注释行号是契约**。多份文件里 Python 行号漂移或指向错误函数，等于「可核对」承诺失效。
5. **spec 列了的东西没做要提出来讨论**（规则 13.2），update_user 分支、中断快照开关、affinity invoke 缺失都属于「默默换实现/默默缺着」，不是合规做法。

## 四、建议优先处置

| 优先级 | 事项 |
|---|---|
| 1 | B-S1~S3 权限 tools.get/set/update_user 三条 RPC（同一收口内修，影响前端权限页正确性） |
| 2 | E-S1 KV affinity 失效 + M-1 sanitizeToolCalls 接线（删包遗留，补登记或补实现） |
| 3 | D-S1/D-S2 20.2 超时与取消终态（改对外契约，20.4 回填前修） |
| 4 | A-S1~S4 Anthropic 四条（tool_choice/流错误折叠/池 key/panic） |
| 5 | F-M1 GetState/GetChildTask 深拷贝降级（goal 恢复场景会再爆） |
