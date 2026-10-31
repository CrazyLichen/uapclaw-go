# 2026-09-28 审查报告复核与修复设计（73 项核验 / 22 项实施）

- **来源**：`docs/review/2026-09-28-48h-logic-review.md`（29S / 28M / 16T，共 73 项）
- **复核基线**：`origin/main` @ `b0ab3529`（已 fetch，无待拉取提交）
- **Python 基准**：`/home/opensource/agent-core/openjiuwen`（`core/memory`、`agent_teams`、`harness`）
- **决策方式**：brainstorming 逐项确认；每项均给出 Python 侧证据后再定方案
- **核心结论**：该报告出具于 2026-09-28，此后一个月内 36 项已被修复；复核另发现 12 项为误报（含报告自身取证错误）；实际待处置 24 项，其中 22 项进入本轮修复、3 项归档（M-12 由"误报"改判为修复）

---

## 一、核验总览

| 处置 | 数量 |
|---|---|
| 已修复（本轮无需动作） | 36 |
| 本轮修复 | 22 |
| 归档不改 | 3 |
| 误报 / 不适用 | 12 |
| **合计** | **73** |

### 1.1 已修复（36 项）

| 章节 | 条目 |
|---|---|
| 9.61 | S-07 RecoverFromSession、S-08 FromSpawnPayload、S-09 RecoverForExistingSession、S-10 DestroyTeam、S-11 ShutdownSelf、S-12 updateExecution、S-13 PersistAllocatorState、S-14 ctx 传播（`SpawnManager.baseCtx`） |
| 7.14 | S-03 SyncTurn 静默、S-04 ProviderOptions（TopK/Rerank/APIKey）、M-03 精简 payload、T-03 中文 Msg |
| 7.15 | M-24 truncStr 已改 rune、M-26 roundTo3 已用 math.Round、T-04 中文 Msg |
| 9.67 | S-15 span 用传入 ctx、S-17 AttachToTeamAgent、S-27 map 加 RWMutex、M-13 otlp_http、T-07 HandleEvent 传 ctx |
| 9.68 | S-18 缓存刷新用 ctx、S-19 resolveInterrupt 传 ctx、S-20 `interrupt.IsTruthyValue`、S-21 AddRail 全部取消注释、S-22/S-23 resource_mgr 注册与清理、M-16 specializePlanAgent、M-17 plan_mode 守卫 |
| 9.69 | S-25 ApplyTeamPlanAgentPrompt、S-26 plan 状态接入、M-21 RolePolicy 用 `atschema.TeamRole`、M-22 Invalidate 重置缓存、T-10 14 个 team tool Invoke |
| 9.61 | M-06 AutoStartMember/AutoStartAll、M-07 UpdateModelPool 守卫、M-09 LookupHumanAgentRuntime 守卫 |

### 1.2 归档不改（3 项）

| 条目 | 归档理由 |
|---|---|
| T-06 | `json.Marshal → Unmarshal` 是 Python `model_dump(mode="json")` 的精确等价物；`TeamAgentSpec` 20 个嵌套字段手写 `ToMap` 会引入 tag/exclude 双份事实源 |
| T-16 | `InitObservability` 已有 `if provider != nil { Warn; return }` 守卫（`setup.go:97-101`），与 Python `if _provider is not None: return` 一致；`SetTracerProvider` 只可能在首次 init 或 Shutdown 后执行，不存在覆盖活跃 provider 的场景 |
| M-18 | 排序已在 `TeamBackend.HumanAgentNames()` 内完成（`team_backend.go:1051 sort.Strings`），且 rail 字段是具体类型而非接口，输出确定 |

### 1.3 误报 / 不适用（12 项）

| 条目 | 证据 |
|---|---|
| S-05 | Python `initialize` 同样静默降级（`client=None` 后 return），Go 行为一致 |
| S-16 | Python `_open_task_span` 也是无 parent 的 root span（`monitor_handler.py:196`），Go 现状与基准一致 |
| S-24 | `PromptTemplate.Content()` 有 `if t == nil { return "" }`（`loader.go:90-94`），`LoadTemplate(...).Content()` 不会 panic |
| M-14 | Python `AgentRail.init` 是 `pass`（`core/single_agent/rail/base.py:471-472`），`super().init(agent)` 无副作用 |
| M-15 | Python `TeamPlanModeRail.init` 不调用 `super().init`（`team_plan_mode_rail.py:39-43`），Go 现状一致 |
| M-20 | Python `TeamPlanModeRail.priority = 84`（`team_plan_mode_rail.py:31`）；报告引用的 85 是 `MODE_INSTRUCTIONS` section 的 priority，非 rail priority |
| M-23 | `fmt.Sprintf("%x", []byte)` 每字节固定两位十六进制，输出与 `hashlib.sha256(...).hexdigest()[:16]` 逐字符一致 |
| M-27 | Python 用 `str(value)`（dict 输出 `{'k': 1}`），改 `json.Marshal` 反而偏离基准 |
| T-08 | Python observability 未注册 `on_agent_call_error`（全目录 grep 无命中），无对应实现可移植 |
| T-13 | 已确认模板与 Python 逐字节一致（cn 9 + en 9 = 18 个，报告原文"19 个"为计数笔误） |
| T-14 / T-15 | 报告正文未定义这两项（仅出现在汇总表），且当前代码已无 `workflowTemplates` 重复变量、`workspace_meta` 已由 `team_workspace.NewWorkspaceMetaTool` 创建 |

> 报告自身还有一处取证错误已在本轮发现并记录：S-29 描述「`client.close()` panic 时 `client = nil` 可能不执行」不成立——`recover` 包在独立 IIFE 内，`client = nil` 在 IIFE 之后照常执行。该项的真实问题只有 `initialized` 语义偏差。

---

## 二、修复清单（22 项）

### 2.1 7.25 DistributedLock（6 项）

| # | 条目 | 方案 |
|---|---|---|
| 1 | S-01 TOCTOU | `BaseKVStore` 新增 `CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)`；`Release` 改用该原子操作 |
| 2 | S-02 无日志 | `CompareAndDelete` 返回 `(false, nil)`（key 已消失或被他人持有）时打 Warn，字段 `lock_key` / `lock_value` / `event_type` |
| 3 | T-01 缺字段 | Error 日志补 `lock_key` / `lock_value`（Msg 已是英文，无需改） |
| 4 | T-02 build tag | 移除 `distributed_lock_test.go` 的 `//go:build test` |
| 5 | M-01 共享实例 | `Acquire` 增加重入守卫：实例已持锁时返回明确 error；结构体注释与 doc 标注「一实例一持有者，不可跨 goroutine 共享」 |
| 6 | M-02 吞 error | 保持返回 nil（Python `try/except` 静默），补注释说明「返回 nil 不代表锁已释放」 |

**方案选择依据**：方案 A（直接扩展接口）的代价比初判更大——除 4 个后端外还有 8 个测试 fake 必须实现。为把成本压到最低，在 `kv` 包提供 `CompareAndDeleteFallback` 内嵌助手（默认 Get+Delete 实现），测试 fake 各加一行内嵌即可。

**后端实现要点**：

| 后端 | 实现 |
|---|---|
| `InMemoryKVStore` | 复用现有 `sync.RWMutex`，一次 `Lock` 内比较后 `delete`（含过期判断） |
| `RedisStore` | `Eval` Lua：`if redis.call('get',KEYS[1]) == ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end` |
| `FileKVStore` | `db.Update`（bbolt 写事务）内解包 `fileEntry`（Base64）→ 比较 → `b.Delete` |
| `DbBasedKVStore` | GORM `Transaction` 内解包 `exclusiveValue`（Base64）→ 比较 → `Delete` |

**基准说明**：Python `release()` 同样是 `get → compare → delete` 两步（`core/memory/common/distributed_lock.py:28-38`），且 Redis 路径下 `get` 返回 `bytes`、`lock_value` 是 `str`，比较恒为 False → Python 在 Redis 上**从不删锁**（只能等 TTL）。Go 当前 `string(val)` 转换让删除路径真正生效，因此比 Python 更容易踩到误删。本项属 Go 侧有意加固，需在注释中记录该差异。

### 2.2 7.15 OpenVikingProvider（3 项）

| # | 条目 | 方案 |
|---|---|---|
| 7 | S-29 IsInitialized | 改为 `return p.client != nil`，删除 `initialized` 字段（Initialize 健康检查失败时不再置 true，Shutdown 不再需要清两个状态） |
| 8 | S-06 / M-05 top_k | `handleVikingSearch` 优先读 `args["top_k"]`，`limit` 作回退；**不注入默认值**（两参数都缺时 payload 与 Python 逐字节一致） |
| 9 | M-04 两消息非原子 | 原子性归档（无批量/回滚接口契约）；两条失败日志补 `role=user` / `role=assistant`，并清理现有 Msg 的双空格 |

### 2.3 9.61 TeamAgent / Recovery（4 项）

| # | 条目 | 方案 |
|---|---|---|
| 10 | M-08 排队无日志 | `DeliverInput` 的 in-flight 分支补 Info 日志（`member_name` / `content_preview`，rune 截断 60）；同时新增 `StreamController.QueuePendingInput(content)` 加锁方法，替换现有的跨包裸写 `pendingInputs`（**本项为复核时新发现：该字段消费侧持 `sc.mu`，写入侧无锁，`-race` 下必报**） |
| 11 | M-10 payload 未拷贝 | `WriteTeamNamespace` 用 `copyMap(payload)`；`MergeTeamNamespace` 内层 bucket 也做 `copyMap`（**报告只列了前者，后者同样原地修改会话状态**），对齐 Python `dict(payload)` / `dict(teams.get(...) or {})` |
| 12 | M-11 忽略返回值 | `recover_team` 的 `UpdateMemberStatus` 结果补 `if !updated { Warn }`，不改控制流（Python 同样忽略，但本文件另两处已检查，属内部一致性） |
| 13 | T-05 日志缺 session_id | 清空前快照 `sessionID`，日志补 `session_id` / `member_name`（Python 无此日志，属 Go-only 观测增强） |

### 2.4 9.67 Observability（4 项）

| # | 条目 | 方案 |
|---|---|---|
| 14 | S-28 缺 Priority | 新增 `observabilityRailPriority = 10`，构造函数 `r.WithPriority(...)`，测试补断言（Python `priority: int = 10` "observability runs last"；Go 与 Python 均为「数值大的先执行」） |
| 15 | M-25 字节截断 | `truncate` 改 `[]rune` 截断，`...<truncated N chars>` 的 N 同步改为 rune 差值；补中文用例 |
| 16 | M-28 未关闭 span | 新增 `OtelTeamMonitorHandler.CloseAllSpans()`：加锁遍历两张 map → `SetStatus(codes.Error, "closed at shutdown")` → `End()` → 清空；`ShutdownObservability` 在 `tp.Shutdown()` **之前**调用 |
| 17 | M-12 span 基址 | `closeLlmSpan` 增加 `ctx` 参数，用 `trace.ContextWithSpan(ctx, state.Span)`（父 span 不变，纯按决策统一） |

**M-28 说明**：Python `shutdown_observability` 不做任何 span 收尾（`setup.py:114-136` 只是 `_monitor_handler = None`），未 End 的 span 被 SDK 静默丢弃。本项是 Go 侧加固，需在注释中标注「Python 无对应实现」。

### 2.5 9.68 / 9.69 Team Rails 与 Prompts（5 项）

| # | 条目 | 方案 |
|---|---|---|
| 18 | M-19 条件创建 | 保留构造条件（`db != nil && messager != nil`），构造时对缺失项打 Warn；`resolveInterrupt` 遇 `messageManager == nil` 打 Error 并 `Reject("Failed to send approval request to leader")`（**现状是跳过发送后仍然挂起等待，等于静默死等**） |
| 19 | T-09 默认值 | `NewTeamToolRail` 末尾归一化：`teamName == "" → "default"`、`memberName == "" → "unknown"`（对齐 Python `team_name or "default"` / `member_name or "unknown"`） |
| 20 | T-11 LoadTemplate | 签名改为 `(*PromptTemplate, error)`，读失败返回 error；`RolePolicy` / `BuildSystemPrompt` / `BuildTeamRoleSection` / `ApplyTeamPlanAgentPrompt` / `BuildTeamPlanModePrompt` 同步返回 error |
| 21 | T-12 中文回退 | 删除 `sections.go` 的 `"（策略模板加载失败）"` 回退（错误显式返回后不再需要，且该文案在 Python 中不存在，不应注入 LLM prompt） |
| 22 | S-24/T-11 连带 | `DeepAgent.ensureInitialized` 中 rail `Init` 失败由「Warn + continue」改为返回 error（Python `deep_agent.py:843-850` 无 try/except，异常直接冒泡） |

**error 传播链（#20）**：

```
prompts.LoadTemplate                 → (*PromptTemplate, error)
  ├─ RolePolicy / BuildSystemPrompt
  ├─ BuildTeamRoleSection
  ├─ ApplyTeamPlanAgentPrompt
  └─ BuildTeamPlanModePrompt
        ↓
rails.NewTeamPolicyRail / NewTeamPlanModeRail   → (*Rail, error)
        ↓
AgentConfigurator.SetupAgent                    → (*TeamHarness, error)
        ↓
TeamAgent.Configure                             → (ctx, error) 或 (*TeamAgent, error)
        ↓
生产调用点：runtime/manager.go:1220、team_agent.go:517/997/1051、spawn_manager.go:541
```

**为什么不把错误留到 `Init` 返回**：`DeepAgent.ensureInitialized` 会 `Warn + continue` 地跳过该 rail，结果是「日志一条 + 团队 rail 整体不生效」，比空 policy 更糟。Python 在 `TeamPolicyRail.__init__` 里就 `load_template`（`team_policy_rail.py:73-105`），异常直接冒泡，因此构造期返回 error 才是 1:1。

---

## 三、接口与语义变更一览

| 变更 | 影响面 |
|---|---|
| `kv.BaseKVStore` 新增 `CompareAndDelete` | 4 个后端实现 + 8 个测试 fake（内嵌 `CompareAndDeleteFallback`）+ `kv/base_test.go` 契约测试 |
| `prompts.LoadTemplate` / `LoadSharedTemplate` 返回 error | 12 个生产调用点 + 5 个 builder 签名 |
| `NewTeamPolicyRail` / `NewTeamPlanModeRail` 返回 error | `agent_configurator.go` 3 处 |
| `SetupAgent` / `Configure` 增加 error 返回 | 5 个生产调用点 + 约 29 处测试 |
| `DeepAgent.ensureInitialized` 传播 rail Init 错误 | 全部 rail 的懒初始化语义（任一失败即中断） |
| `OpenVikingProvider` 删除 `initialized` 字段 | 结构体 + Initialize/Shutdown/IsInitialized + 1 处测试 |

---

## 四、测试计划

| 项 | 测试 |
|---|---|
| S-01 | 4 个后端的 `CompareAndDelete` 表驱动用例（值匹配删除 / 值不匹配保留 / key 不存在 / 过期 key）；Release 竞态路径用"改值后 Release"模拟 |
| S-02 / T-01 / M-02 | 日志字段断言（capture logger 或行为断言） |
| M-01 | 同一实例二次 `Acquire` 返回 error |
| S-29 | 健康检查失败后 `IsInitialized() == false` |
| S-06 | `top_k` 优先、`limit` 回退、均缺时不带 top_k |
| M-04 | 两条失败分支的 `role` 字段 |
| M-08 | 并发 `DeliverInput` + `-race`；日志 `content_preview` 的 CJK 截断 |
| M-10 | 写入后修改 payload 不影响已存储 bucket；merge 不污染原 bucket |
| S-28 | `Priority() == 10` |
| M-25 | 中文串按 rune 截断且不产生非法 UTF-8，计数为 rune 差 |
| M-28 | 开 span → `ShutdownObservability` → recorder 能收到已 End 的 span 且 status=Error |
| M-19 | `messageManager == nil` 时返回 Reject 而非 Interrupt |
| T-09 | 空 teamName/memberName → `default` / `unknown` |
| T-11/T-12 | 缺失模板 → 构造返回 error；正常模板 → 无 error |
| #22 | 注入一个 Init 失败的 rail → `ensureInitialized` 返回 error |

验证命令：

```bash
gofmt -l ./internal ./cmd ./tests
go build ./... && go vet ./...
go test -race ./internal/agentcore/memory/... ./internal/agentcore/foundation/store/kv/... ./internal/agent_teams/...
go test ./internal/agent_teams/prompts/...
go run ./cmd/integration-audit -verify        # 集成测试合规性（如有改动）
```

提交后必须跑远端 CI，Build / Lint / Unit / Integration 四 job 全绿（项目规则 6）。

---

## 五、提交拆分

| 提交 | 内容 |
|---|---|
| 1 | `feat(kv): BaseKVStore 增加原子 CompareAndDelete（4 后端 + 测试 fake）` |
| 2 | `fix(memory): DistributedLock 原子释放 + 重入守卫 + 日志字段（S-01/S-02/T-01/T-02/M-01/M-02）` |
| 3 | `fix(memory): OpenViking 初始化语义与 top_k 参数（S-29/S-06/M-04）` |
| 4 | `fix(agent_teams): 排队日志与队列加锁、metadata 拷贝、恢复日志（M-08/M-10/M-11/T-05）` |
| 5 | `fix(observability): rail 优先级、rune 截断、退出收口 span（S-28/M-25/M-28/M-12）` |
| 6 | `fix(agent_teams): 审批快速失败、成员名归一化（M-19/T-09）` |
| 7 | `feat(prompts): 模板缺失显式报错并全链路传播（T-11/T-12 + ensureInitialized）` |
| 8 | `docs(review): 删除 2026-09-28 审查文档（73 项全处置）` |

---

## 六、风险与回滚

| 风险 | 缓解 |
|---|---|
| `BaseKVStore` 接口变更破坏外部实现者 | 仓库内实现已全覆盖；`CompareAndDeleteFallback` 提供默认实现，外部实现可内嵌 |
| File/DB 的 JSON 信封比较写错导致锁永不释放 | 每个后端独立表驱动用例，且 `Release` 在 `!released` 时打 Warn，线上可观测 |
| `Configure` 签名变更波及 29 处测试 | 纯机械改动，逐包编译验证；先改签名再批量修调用点 |
| `ensureInitialized` 传播 rail 错误可能让存量 rail 的 Init 失败暴露为新错误 | 现有 rail 的 `Init` 均返回 nil；CI 全量回归可捕获意外 |
| 删除中文回退文案后模板缺失变"空 policy" | 已被 #20 替代：缺失会在构造期报错，不会走到空 policy |
