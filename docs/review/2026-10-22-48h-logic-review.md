# 2026-10-22 48h 逻辑审查

- **审查窗口**：2026-10-20 04:00 ~ 2026-10-22 04:01（62 个提交，排除 style/gofmt/docs/lint 后功能性提交约 30 个）
- **审查范围**：48h 内完成的功能章节——13.10 Parser 抽象接口、10.5.10 CryptoUtility 双路径、SubagentRail async 分支回填（任务 #77）、10.5.7+10.5.8 ExtensionLoader/Manager、10.5.3 HookContext.FromMap 写回、11.3 before_chat_request 触发点、FollowUp 竞态修复、uapclaw.go 3 处对齐修复、integration-audit 双层门禁（工具本身非移植对象，未纳入 Python 对照）
- **审查方法**：逐方法签名与步骤对照 Python 参考项目（agent-core/openjiuwen + jiuwenswarm-develop），重点核对方法签名一致性、步骤缺失、占位真实性

## 总体结论

本次 48h 移植质量整体较高：核心数据流（AesGcm 加解密、事件双源 metadata 合并、取消静默路径、stream 顺序消费）与 Python 对齐良好，此前多轮审查的高频问题模式（context.Background 泛滥、事件丢失、双条件门控）大部分已被主动规避。发现 **1 项严重、6 项一般、8 项提示**。

**严重**问题集中在扩展插件 transport 的并发 panic 窗口；**一般**问题集中在 asyncio→Go 移植的数据竞争（SessionToolkit row 指针逃逸）与插件子进程生命周期管理。

---

## 严重问题（S）

### S-01 GRPCPluginTransport.Close 与 pumpRecvLoop 存在 send-on-closed-channel panic 竞态

**Python 样例**（无此问题——asyncio 单线程无 send/close 并发）：
```python
# ChannelTransport（Python 进程内实现，transport 由 asyncio 协程顺序消费）
async def recv(self):
    while True:
        data = await self._queue.get()  # 协程挂起点，无并发 close
        yield data
```

**Go 问题**（`internal/swarm/extensions/plugin/transport_proxy.go:95-146`）：
```go
// Close：锁内置位后锁外 close(recvCh)
func (t *GRPCPluginTransport) Close() error {
    t.mu.Lock()
    ...
    t.closed = true
    recvCh := t.recvCh
    t.mu.Unlock()
    if recvCh != nil {
        close(recvCh)          // ← 锁外 close
    }
    ...
}

// pumpRecvLoop：检查 closed 后锁外 send
func (t *GRPCPluginTransport) pumpRecvLoop() {
    for {
        resp, err := stream.Recv()
        ...
        t.mu.Lock()
        closed := t.closed
        t.mu.Unlock()
        if closed { return }
        select {
        case t.recvCh <- data:  // ← Close 可在检查与发送之间插入：panic
        case <-time.After(recvStallTimeout):
            ...
        }
    }
}
```

竞态窗口：pump 检查 `closed=false` → Close 置位并 `close(recvCh)` → pump 执行 `t.recvCh <- data` → **send on closed channel panic**。`time.After` 兜底只防阻塞不防 panic；pump goroutine 无 recover，将带崩整个宿主进程。触发场景：AgentClient.Close（网关关闭/重连）与 receiverLoop 高频收帧并发。CI `-race` 检测不到 send-on-closed（这是逻辑竞态非内存竞态），单靠现有测试无法拦截。

**修复方案**：Close 不直接 `close(recvCh)`，改为关 done channel，由 pump 收口：
```go
// 结构体增加 done chan struct{}（Recv 首次调用时创建）
func (t *GRPCPluginTransport) Close() error {
    t.mu.Lock()
    if t.closed { t.mu.Unlock(); return nil }
    t.closed = true
    done := t.done
    t.mu.Unlock()
    if done != nil {
        close(done)  // 只关 done，不关 recvCh
    }
    ...
}
func (t *GRPCPluginTransport) pumpRecvLoop() {
    defer close(t.recvCh)  // pump 退出时收口 recvCh
    for {
        select {
        case <-t.done:
            return
        default:
        }
        resp, err := stream.Recv()
        ...
        select {
        case t.recvCh <- data:
        case <-t.done:
            return
        case <-time.After(recvStallTimeout):
        }
    }
}
```

---

## 一般问题（M）

### M-01 SessionToolkit.Get/ListAll 返回 row 指针逃逸锁外，读写竞态（asyncio→Go 移植遗漏）

**Python 样例**（`harness/tools/subagent/session_tools.py:74-80`，单线程无问题）：
```python
def list_all(self) -> List[SessionTaskRow]:
    return list(self._rows.values())  # asyncio 单线程，读取期间无并发写

def get(self, task_id: str) -> Optional[SessionTaskRow]:
    return self._rows.get(task_id)
```

**Go 问题**（`internal/agentcore/harness/tools/subagent/session_tools.go:133-149`）：
```go
func (t *SessionToolkit) ListAll() []*SessionTaskRow {
    t.mu.RLock()
    defer t.mu.RUnlock()
    result := make([]*SessionTaskRow, 0, len(t.rows))
    for _, row := range t.rows {
        result = append(result, row)  // ← 返回共享指针
    }
    return result  // ← 锁已释放，调用方读 row.Status 与 MarkXxx 并发写竞态
}
func (t *SessionToolkit) Get(taskID string) *SessionTaskRow {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.rows[taskID]  // ← 同上
}
```

调用方锁外读示例：
- `session_tools.go:166-171` sessions_list 工具读 `task.Status/Result/Error`（模型调用工具，与 spawn 完成事件处理器 `MarkCompleted` 完全并发）
- `deep_agent.go:2598-2601` hasPendingSessionSpawn 读 `r.Status`
- `session_tools.go:346` sessions_cancel 失败分支读 `task.Status`

Python asyncio 单线程事件循环保证读取与写入不交错；Go 真并行下 mutex 只保护 map 结构，不保护 row 字段——正是项目记忆中"Python 单线程共享状态须改快照拷贝或加锁"的已知模式，此处漏改。CI `-race` 下 sessions_list 与 spawn 完成并发时可报 DATA RACE（当前 e2e 测试未触发此窗口，属漏网）。

**修复方案**：Get/ListAll 返回值拷贝（值语义）：
```go
func (t *SessionToolkit) ListAll() []SessionTaskRow {  // 值切片
    t.mu.RLock()
    defer t.mu.RUnlock()
    result := make([]SessionTaskRow, 0, len(t.rows))
    for _, row := range t.rows {
        result = append(result, *row)  // 深拷贝
    }
    return result
}
func (t *SessionToolkit) Get(taskID string) SessionTaskRow {  // 值 + ok
    t.mu.RLock()
    defer t.mu.RUnlock()
    if row, ok := t.rows[taskID]; ok {
        return row, true
    }
    return SessionTaskRow{}, false
}
```
调用方 `task.Status` 等读法不变（值自动解引用），返回值改值类型后无指针逃逸。注意 e2e 测试中 `toolkit.ListAll()` 发现 task_id 的用法同步适配。

### M-02 多声明插件被重复 Shutdown/Kill

**Python 样例**（`jiuwenswarm/extensions/manager.py:61-67`）：
```python
async def shutdown_all_extensions(self) -> None:
    for ext in self._loaded_extensions:  # 扩展实例级，register_extensions 返回 N 个实例
        try:
            if hasattr(ext, "shutdown"):
                await ext.shutdown()  # 每实例一次
```

**Go 问题**（`internal/swarm/extensions/loader.go:254` + `manager.go:104-116`）：
```go
// loader：每个 Declaration 生成一条 LoadedExtension，但 client 共享同一 PluginClient
loaded = append(loaded, LoadedExtension{Metadata: decl.Metadata, Root: root, client: pc})

// manager：对每条记录调 Shutdown + Kill
for _, le := range m.loaded {
    if err := le.client.Shutdown(ctx); err != nil { ... }  // N 声明 = N 次 Shutdown RPC
    le.client.Kill()  // N 次 Kill
}
```

Python 侧 `_loaded_extensions` 存的是扩展实例（每个实例独立 shutdown 语义）；Go 侧一个插件进程的多个声明共享同一 `PluginClient`，N 声明会发 N 次 Shutdown RPC + N 次 Kill。Kill 幂等无害，但 `impl.Shutdown` 不保证幂等，第二次 RPC 报错产生误导性 warn 日志。

**修复方案**：ShutdownAllExtensions 按 client 指针去重后逐进程 Shutdown+Kill（`seen := map[PluginClientFace]struct{}{}`）。

### M-03 全部声明 SetExtensionDir 失败时插件子进程泄漏

**Python 样例**（`jiuwenswarm/extensions/loader.py:58-67`，root 级异常传播）：
```python
if hasattr(module, "register_extensions"):
    registered = await module.register_extensions(self.registry)  # 异常 → 整个 root 失败
```

**Go 问题**（`internal/swarm/extensions/loader.go:224-231`）：
```go
for _, decl := range decls {
    if err := pc.SetExtensionDir(ctx, root); err != nil {
        logger.Error(...).Msg("[ExtensionLoader] SetExtensionDir 失败")
        continue  // ← 声明级跳过；若全部失败，loaded 为空
    }
    ...
}
return loaded, nil  // ← 返回 (空, nil)，manager 视为成功
```

若某 root 全部声明 SetExtensionDir 失败：返回 `(空切片, nil)`，manager 记成功；`pc` 无人持有，ShutdownAllExtensions 遍历 loaded 为空——**插件子进程永不 Kill，进程泄漏**。Python 语义是 register_extensions 抛异常 → root 级失败 → manager error 日志（差异：Python 是 root 级失败而非声明级跳过，无泄漏路径）。

**修复方案**：loadAndRegister 结束时若 `len(loaded)==0 && len(decls)>0` 则 `pc.Kill()` 并返回 error，交由 manager 记日志继续。

### M-04 插件 crypto 代理硬编码 context.Background 且无超时

**Python 样例**（进程内直调，无挂起风险）：
```python
# registry.get_crypto_provider().decrypt(val) — 同进程函数调用
```

**Go 问题**（`internal/swarm/extensions/plugin/crypto_provider.go:38-45`）：
```go
func (p *GRPCPluginCryptoProvider) Encrypt(plaintext string) (string, error) {
    return p.pc.Encrypt(context.Background(), plaintext)  // ← 无超时无 ctx
}
func (p *GRPCPluginCryptoProvider) Decrypt(ciphertext string) (string, error) {
    return p.pc.Decrypt(context.Background(), ciphertext)
}
```

对比同文件先例：`host.go:143-148` Shutdown 有 2s 超时。插件子进程挂起时，config.get/config.set/models.replace_all handler 的 api_key 加解密路径将永久阻塞（gRPC 默认无 deadline）。本仓库历史审查多次将 `context.Background()` 泛滥列为 P0，此处为新引入。

**修复方案**：`ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second); defer cancel()`（或改签名透传调用方 ctx，接口 `Encrypt(ctx, ...)`——前者改动小，与 Shutdown 先例一致）。

### M-05 isSensitiveKey 缺 ToLower，同仓两处行为不一致

**Python 样例**（`app_web_handlers.py:612`）：
```python
if (("api_key" in key.lower() or "token" in key.lower())
        and ExtensionRegistry.get_instance().get_crypto_provider()):
```

**Go 问题**（`internal/swarm/gateway/channel_manager/web/web_handlers.go:717-719`）：
```go
func isSensitiveKey(key string) bool {
    return strings.Contains(key, "api_key") || strings.Contains(key, "token")  // ← 无 ToLower
}
```

而 `config_apply.go:87-88`（同一功能对）有 `strings.ToLower(key)`。当前 configEnvMap 键全小写无实际影响，但 config.set 用户传 `API_KEY` 大写键时：config_apply 加密、config.get 不解密——读写不对称产生永远解不开的密文。

**修复方案**：`isSensitiveKey` 内加 `strings.ToLower(key)`，与 config_apply.go 统一。

### M-06 extractAgentTools 步骤 2（DeepAgent 实例读 ability_manager 工具名）缺失

**Python 样例**（`harness/rails/subagent/subagent_rail.py:204-221`）：
```python
# 2. DeepAgent instance with registered tools
if not isinstance(spec, SubAgentConfig):
    ability_mgr = getattr(spec, "ability_manager", None)
    if ability_mgr is not None:
        try:
            tool_names: list[str] = []
            for c in getattr(ability_mgr, "cards", []):
                n = getattr(c, "name", None)
                if isinstance(n, str):
                    tool_names.append(n)
            if tool_names:
                return ", ".join(tool_names)
        except (AttributeError, TypeError) as e:
            logger.debug(...)
```

**Go 问题**（`internal/agentcore/harness/rails/subagent/subagent_rail.go:333`）：
```go
// 2. DeepAgent 实例的已注册工具 — Go 中暂不实现（需要 ability_manager 访问）
```

DeepAgent 实例作为 subagent spec（`spec.(*DeepAgent)` 路径，CreateSubagent L648 支持）时，可用子代理描述中的 Tools 退化为 knownAgentTools 或 "All tools"，与实际注册工具不符。影响：sessions_spawn/TaskTool 提示词中的工具描述失真（不影响执行正确性，但 Python 语义是精确列表）。

**修复方案**：`extractAgentTools` 中 `if deepAgent, ok := spec.(*hschema.DeepAgentImpl); ok` 等价路径——项目内 DeepAgent 类型可断言后走 `AbilityManager().ListCards()` 取工具名。若因接口循环依赖受阻，按项目惯例用本地等价接口（同文件 sessionToolkitSetter 先例）。

---

## 提示问题（T）

### T-01 CJK 多字节截断（历史存量，本次功能路径叠加使用）

**Python 样例**（rune 语义）：
```python
return str(output)[:500]   # 按字符截断，中文安全
```

**Go 问题**（`internal/agentcore/harness/task_loop/handler.go:638/645/666/317/456`）：
```go
if len(s) > 500 {
    s = s[:500]  // ← 字节截断，UTF-8 中文 3 字节，500 边界截断多字节字符产生非法 UTF-8
}
```

9.7 引入的历史代码（`4857b56e`），非本次新增；但本次 async spawn 的 steer 文本走中文模板（`[后台任务完成] 任务描述=...`），截断后推入 steer 队列的内容可能带非法 UTF-8 尾字节。项目已有 `truncStr` 类 rune 安全函数先例（记忆：Viking truncStr CJK）。建议统一替换为 rune 截断。

### T-02 TestDeepAgentSessionRail 场景 1-4（非 Mock）未移植

Python `tests/system_tests/harness/test_deep_agent_subagent.py:139-349` 的 `TestDeepAgentSessionRail`（auto_invoke_on_spawn_done / query2 不阻塞 / steering 可见性 / dedup 多 spawn）Go 端未移植，只移植了 CancelMock 6 场景。场景 1/2/4 可用 MockLLM 移植（Python 端就是 mock），场景 3 依赖真实 LLM 可暂缓。auto-invoke 与 dedup 路径目前只有 unit 级测试覆盖，建议补集成测试。

### T-03 插件 crypto 注入优先级与设计文档相悖（快照 vs 逐请求全局）

设计文档 §七写明 handler 内部从 `crypto.GetCryptoProvider()` 全局取、BindParams 注入仅测试用；实际 `web_handlers.go:707-712` 注入优先 + `app_gateway.go:130` 生产装配注入 → 生产走装配期快照。当前等价（cmd.go 扩展加载先于网关创建），但 models.list 走 config 层全局、config.get 走快照，两通道不一致；扩展热加载出现时 config.get 将用陈旧 provider。建议按设计文档收敛为"生产不注入"。

### T-04 registry 显式 SetCryptoProvider 与 RegisterCryptoUtility 内部双写冗余

`loader.go:245-248` 显式覆盖 + `registry.go:127-129` 内部双写。幂等无害，注释已说明为语义强调。可接受。

### T-05 manager.go ListExtensions 注释与实现不符

注释称"按非空 ID 判断"，代码无任何过滤（`manager.go:122-133`）。Python 按 `hasattr(p, "metadata")` 过滤。当前 Go 结构恒有 Metadata，注释误导。修注释或补过滤。

### T-06 serve.go GetMetadata 每次调用重复执行 impl.Register

`serve.go:257-263`：GetMetadata RPC 每次都调 `impl.Register(ctx)`，Register 有副作用时被重复触发；Python `metadata` 是 property + 缓存。当前宿主无调用点，仅测试触达。建议 Register 结果缓存（sync.Once）。

### T-07 proto EventSubscription.priority 缺 100 默认值兜底

插件忘设 priority → 0，与 Python `register(priority=100)` 默认值不一致，影响多扩展同事件排序。serve.go Register 入口对 0 归一为 100（或 proto 定义 default）。

### T-08 dispatch.go SessionID 空串序列化为 "" 而非 null

`Message.SessionID` 为 string（Python `str|None`），空串经 structpb 序列化为 `""`。扩展判断 session 是否存在时语义有差。schema 层既有差异，非本次引入，跨进程边界建议统一（本次新增 dispatch → 插件路径受影响，故记录）。

---

## 对齐确认（无问题项）

1. **13.10 Parser**：五方法接口（Parse/Process/LazyParse/Supports/ParseContent）+ ParseOptions 三选项 + BaseParser 模板适配器（self 注入动态派发）与 Python `base.py` 逐行对齐；`parse` 的 truthiness 分支、metadata 显式空 map、lazy_parse 回调式等价 yield、`_parse` 默认空，均正确。Python 端 base.py 零 logger，Go 端零日志对应。覆盖率 88.6% 声明可信（base_test.go 三 fake 结构完整）。
2. **publishTaskEvent 双源合并**（27d5df2f 修复）：对齐 Python `_publish_task_event` 的 `metadata=payload_metadata` + `event.metadata.update(task.metadata)`，task_type 传播恢复，SessionSpawn 完成分支正确路由。
3. **取消静默路径**：对齐 Python asyncio.Task.cancel 语义（await 点终止不 yield error chunk），MarkFailed/MarkCanceled 竞态规避正确。
4. **SubagentRail async 分支**：Init/Uninit/BeforeModelCall 三钩子、SetSessionToolkit 注入（本地接口解循环依赖）、三工具注册（AbilityManager+ResourceMgr 双注册）与 Python 一致；e2e 6 场景对齐 CancelMock 场景 1-6，per-test MockLLM 隔离与 task_id 发现机制合理。
5. **uapclaw.go 3 处对齐修复**（0b27c357）：params 归一化、MEMORY_AFTER_CHAT 双条件（`result.ok and content`）、session_id 四处回退 "default"，均与 Python 逐行对齐；流式消费者改 `defer close(outCh)` + `for range` 顺序消费，修复了 select 随机分支丢 chunk 的真实 bug。
6. **FollowUp 竞态修复**（83ae9da0）：StreamQueue sendMu 读写锁排除 send/close 竞态，语义对齐 Python `queue.join()`；测试同步屏障方案保留生产 fire-and-forget 语义，正确。
7. **CryptoUtility 核心数据流**：AesGcm nonce+tag+ciphertext hex 拼接、12/32/16 常量、decryptModelEntries 三条件（深拷贝/双条件/静默保留）、web 消费点失败跳过字段，均与 Python 对齐；设计偏差表 7 项全部核对成立。
8. **HookContext.FromMap 写回**：四种上下文逐字段比对无遗漏；payload 原地修改语义经 ToMap→Trigger→FromMap→重序列化闭环复刻；before_chat_request 触发点位于步骤 8/10 之间对齐 Python L2502，门控与返回值语义一致（两侧均不中断流程）。
9. **deep_agent 工厂链**：CreateSubagent 配置合并（model/card/prompt/tools/mcps/rails/max_iterations/workspace/skills/backend/sys_operation/language/prompt_mode/subagents=None/enable_async=False/add_general=False/enable_plan_mode/restrict_to_work_dir）与 Python L938-982 逐项对齐；browser/code/research 工厂分派一致；mobile_gui_agent 占位真实（⤵️ 9.31 与计划表一致）。
10. **占位真实性核查**：`load_harness_config`/`unload_harness_config`（⤵️ 9.3）、mobile_gui_agent（⤵️ 9.31）确实未实现，与 IMPLEMENTATION_PLAN 回填标记一致，无"假完成"。

## 复核状态（2026-10-23）

上一轮已修复 4 项：

- **S-01 ✅**：transport_proxy.go 重写为 recvCancel context 收口方案（`Recv()` 创建 cancel、`Close()` 取消、pump `defer close(recvCh)` 统一收口）——比本文原建议的 done channel 方案更完整，同时覆盖了 pump 阻塞在 `stream.Recv()` 的场景
- **M-01 ✅**：`ListAll() []SessionTaskRow` 值切片深拷贝 + `Get(taskID) (SessionTaskRow, bool)`，调用方值语义读法不变
- **M-03 ✅**：loader.go 全声明 SetExtensionDir 失败 → `pc.Kill()` + 返回 error
- **T-04 ✅（可接受，维持现状）**：显式覆盖 + registry 内部双写并存，注释已说明为"最后加载者胜"语义强调，与文档原结论"幂等无害，可接受"一致

尚未修复 10 项 + 部分 1 项：M-02、M-04、M-05、M-06、T-01、T-03、T-05、T-06、T-07、T-08；T-02 部分（CancelMock 6 场景与 real_llm 场景 1 简化版已有，Mock 级场景 1/2/4 集成测试未补）。

---

## 处置建议汇总

| 级别 | 数量 | 编号 |
|------|------|------|
| 严重 | 1 | S-01 |
| 一般 | 6 | M-01 ~ M-06 |
| 提示 | 8 | T-01 ~ T-08 |

优先级：S-01（宿主进程 panic 风险）> M-01（race detector 可检出的数据竞争）> M-03（进程泄漏）> M-02/M-04/M-05 > M-06 > T 系列。

### 逐项修复状态（2026-10-23 复核）

| 编号 | 级别 | 状态 | 复核证据 |
|------|------|------|----------|
| S-01 | 严重 | ✅ 已修复 | transport_proxy.go：recvCancel 收口方案落地，Close 不再直接 close(recvCh) |
| M-01 | 一般 | ✅ 已修复 | session_tools.go:137-159：值拷贝返回 + 竞态注释 |
| M-02 | 一般 | ❌ 未修复 | manager.go:103-119：ShutdownAllExtensions 仍逐条 Shutdown+Kill，无 client 指针去重 |
| M-03 | 一般 | ✅ 已修复 | loader.go:260-263：`len(loaded)==0 && len(decls)>0` → Kill + 返回 error |
| M-04 | 一般 | ❌ 未修复 | crypto_provider.go:38-45：Encrypt/Decrypt 仍 `context.Background()` 无超时 |
| M-05 | 一般 | ❌ 未修复 | web_handlers.go isSensitiveKey：仍无 `strings.ToLower`，与 config_apply.go 不一致 |
| M-06 | 一般 | ❌ 未修复 | subagent_rail.go:333：步骤 2 仍为"暂不实现"注释占位 |
| T-01 | 提示 | ❌ 未修复 | task_loop/handler.go：5 处字节截断（638/645 行 500 字节、317/456 行 100 字节、extractErrorFromEvent 300 字节），无 rune 安全截断 |
| T-02 | 提示 | ◐ 部分覆盖 | CancelMock 6 场景 + real_llm 场景 1 简化版已有；Mock 级场景 1/2/4 集成测试未补，auto-invoke/dedup 仅 unit 级覆盖 |
| T-03 | 提示 | ❌ 未修复 | app_gateway.go:131：生产装配仍 `crypto.GetCryptoProvider()` 快照注入，与设计文档"生产不注入"相悖 |
| T-04 | 提示 | ✅ 可接受 | 双写冗余维持现状（本文原结论"幂等无害，可接受"） |
| T-05 | 提示 | ❌ 未修复 | manager.go:122：注释仍称"按非空 ID 判断"，代码无过滤 |
| T-06 | 提示 | ❌ 未修复 | serve.go:257-263：GetMetadata 每次调用重复执行 impl.Register，无 sync.Once 缓存 |
| T-07 | 提示 | ❌ 未修复 | serve.go:242：Priority 直接透传，无 0→100 默认值归一化 |
| T-08 | 提示 | ❌ 未修复 | dispatch.go:119：`SessionID: &agentMsg.SessionID` 无条件取指针，空串序列化为 `""` 而非 null |
