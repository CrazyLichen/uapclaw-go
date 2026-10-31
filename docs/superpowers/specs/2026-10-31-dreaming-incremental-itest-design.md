# Dreaming 增量语义集成测试迁移设计

- 日期：2026-10-31
- 来源：`jiuwenswarm-develop/tests/system_tests/test_dreaming_e2e.py`（Python 系统测试，markers `integration` + `system`，7 例）
- 参考单测：`jiuwenswarm-develop/tests/unit_tests/agentserver/memory/test_dreaming_sweeper.py`（`TestScanNewSessions` 2 条增量用例）
- 目标仓库：`uap-claw-go`
- 决策记录：零生产改动（不引入 `WithExtractor` 接缝）/ 复用 `testhelpers.NewAdapterTestEnv` 作为 mock provider / 真跑 code 与 agent 两种 `CreateInstance` / 双层断言（管线产物 + 接线副作用）/ 顺带撤销目录的审计豁免

## 1. 背景与目标

Python 的 dreaming 分两层：`openjiuwen/core/memory/dreaming/orchestrator.py` 只是定时器（interval 钳位 60s、`busy_checker`、tick 循环），真正的业务管线在 `jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py`（scan → compress → LLM extract → promote → checkpoint）。

Go 侧两层实现与提示词模板已一比一对齐，`internal/swarm/agents/harness/common/memory/dreaming/sweeper.go` 里 6 条增量规则全部落地。但**测试只覆盖到孤立环节**：现有 `tests/integration/.../dreaming/sweeper_e2e_test.go` 的 20 个用例都在直接调 `Promote` / `ScanNewSessions` / 手写 checkpoint，从未跑过完整 `RunSweep`。因此 Python e2e 里最有价值的四类断言——两次 sweep 的增量窗口、跨轮标题去重、LLM 报错时的落盘语义、checkpoint 驱动下一轮——**在 Go 侧没有任何锚点**。

本设计把该 Python 测试迁移到 Go 的集成层，并补上 Python e2e 未覆盖的 5 条边界，同时把 dreaming 目录的跨子系统跨度从 2 提到 4，撤销 `.integration-scope-exempt` 的 C 类豁免登记。

## 2. 现状盘点

### 2.1 Python 源测试结构与逐条判定

| # | Python 用例 | 性质 | 迁移判定 |
|---|---|---|---|
| 1 | `test_sweep_writes_dreaming_md` | 3 会话 × 5 轮 → patch 提取器 → `run_sweep` → 断言 DREAMING.md 与 checkpoint | 全迁 |
| 2 | `test_dedup_skips_existing_title` | 两次 sweep 同标题只留 1 条 | **按意图重写**（见 2.2） |
| 3 | `test_incremental_scan_reprocesses_updated_session` | 已扫会话追加 4 轮后二次 sweep 仍产出新知识 | 全迁 + 加强 |
| 4 | `test_skips_unchanged_session` | 无变更时二次 sweep 无新增 | 全迁 + 加强 |
| 5 | `test_sweep_writes_consolidated_files` | code 模式写 2 个 `consolidated_*.md` + frontmatter | 全迁 |
| 6 | `test_content_hash_dedup` | 同内容 → 1 个文件 | 全迁 |
| 7 | `test_start_stop` / `test_start_disabled_config` | 启停生命周期与禁用配置 | 全迁 |

### 2.2 Python 用例 2 的空转缺陷（迁移必须修正）

`test_dedup_skips_existing_title` 第一轮建了 `sess_a` / `sess_b` / `sess_c` 三个会话并全部扫描；第二轮只新增 `sess_002` / `sess_003` 两个目录。此时 `sweeper.py` 的 `scan_new_sessions()` 计算 `new_ids = 2`，小于 `_MIN_NEW_SESSIONS = 3`，且没有超过 7 天的旧会话可绕过门槛，因此**直接返回 `[]`，第二轮 sweep 根本没执行**。末尾 `content.count("用户偏好 Python") == 1` 恒真，实际零覆盖。

去重语义的真正锚点在单测 `TestPromoteAgent.test_dedup_title`。Go 版按"意图"实现：第二轮补足 3 个新会话让门槛放行，使第二次 sweep 真正跑起来并命中已存在标题。

### 2.3 Python 增量语义的 6 条硬规则（`sweeper.py:199-285`、`:322-341`）

| 规则 | Python 行号 | Go 对应 |
|---|---|---|
| 候选 = 新目录 ∪ (已扫 ∧ `history.json` mtime 变新) | L205-220 | `sweeper.go:396-430` |
| 候选数 < 3 且无 > 7 天旧会话 → 整轮跳过 | L222-234 | `:433-454` |
| 会话目录 > 30 天 → 标记已扫后丢弃 | L247-249 | `:470-473` |
| `metadata.mode` 前缀不匹配 → 跳过 | L253 / L287-298 | `:480` / `:722-739` |
| 增量：`new_rounds = 总轮次 - last_count`，< 4 跳过，压缩起点 `events[rounds[last_count][0]:]` | L261-269 | `:493-516` |
| 只有 LLM 成功返回的会话写入 checkpoint（mtime + round_count），写盘发生在 promote 之前 | L160-173 | `:322-341` |

> 关于"失败重试"的更正：Python `run_sweep` 的 `except` 分支（L155-157）把会话记入 `failed_ids` 从而不推进 checkpoint，但 `_extract_via_llm` 内部已用 `except json.JSONDecodeError` / `except Exception`（L414-419）吞掉全部异常并返回 `[]`，因此**外层 `except` 不可达，"失败不推进、下轮重试"并非实际语义**。Go 在 `extractViaLLM` 中同样把 LLM 错误降级为 `(nil, nil)`（`sweeper.go:860-865`，注释 S-02），对齐了 Python 的真实行为。迁移按**实际语义**建立锚点（见 §5.1 用例 7），不按注释意图建立。

### 2.4 Go 测试缺口与目录状态

| 项 | 现状 |
|---|---|
| `RunSweep` 端到端 | 无任何用例（现有用例全部绕开） |
| 增量窗口（只压新轮次） | 无 |
| LLM 失败不推进 checkpoint | 无 |
| 门槛边界（3 会话 / 7 天 / 30 天 / 轮次不足 4） | 无 |
| code 模式扫描 | 不可测：`sweeper_e2e_test.go:574` 的 `createSessionDir` 把 `metadata.mode` 硬编码为 `"agent"` |
| mtime 构造 | 无 `os.Chtimes`，Python 用 `time.sleep(0.2)` 赌精度 |
| 审计状态 | `tests/integration/swarm/agents/harness/common/memory/dreaming` 已登记 `.integration-scope-exempt`（C 类，注释记"swarm/agents 248 行 + agentcore/memory 47 行"，跨度 2） |

## 3. 关键决策

### D1：不引入生产接缝，用既有测试隔离设施做替身

Python 通过 `mock.patch.object(Sweeper, "_extract_via_llm", ...)` 替换实例方法。Go 的 `extractViaLLM`（`sweeper.go:788`）内部直接 `llm.NewModel(...)`，无替换点。三条候选路径的实际验证结果：

- 加 `WithExtractor(fn)` 变参选项：可行，但要在生产包多暴露一个测试专用接缝。
- 测试内注册 `ClientRegistry` mock provider + 自建 config：`tests/integration/testhelpers/adapter_env.go:47` 的 `NewAdapterTestEnv` 已经把这套封好了——注册唯一 provider 名（`L53`）、`UAPCLAW_DATA_DIR` 指向 `t.TempDir()`（`L56`）、`pathutil.ResetCache()`（`L57`）清掉 `path` 包的 `sync.Once` 缓存，cleanup 里 Unregister + ResourceMgr.Release + 恢复 env + 再 ResetCache。
- 第三条"真跑但断言宽松"即 real-llm 套件，另立 §7。

**决策：走第二条。** 由 `NewDreamingEnv` 写 `models.defaults` 指向 mock provider，`config.GetDefaultModels(nil)`（`sweeper.go:790`）取到 mock 条目后，`extractViaLLM` 全程真实执行——提示词模板填充、```json 围栏剥离、JSON 反序列化都被覆盖——只是模型响应由 `MockModelClient` 预设，断言保持精确。**生产代码零改动。**

### D2：`mockllm` 记录入参，替代接缝的"看 prompt"能力

`mock_client.go:145` 的 `Invoke` 目前忽略 messages 参数（签名为 `_ model_clients.MessagesParam`），`InvokeRecord` 只有 `InvokeCount`。要断言"第二轮增量只含 q6~q9、不含 q1~q5"，必须看到 prompt 原文。

**决策：** `InvokeRecord` 增加 `Messages model_clients.MessagesParam` 字段并在 `Invoke` 中记录。`mockllm` 目录已登记在审计豁免清单内，改动不影响门禁，也不触碰生产代码。

### D3：真跑 `CreateInstance`，code 与 agent 双模式都必须通过

用户明确要求两种模式都要通过；**不通过即视为生产缺陷，须定位并修复**，不得以跳过或降级用例收场。仓库已有两处先例可复用：

- code 模式：`tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go:132`（`codeAdapter.CreateInstance(ctx, cfg, "code", subMode)`）
- agent 模式：`tests/integration/real_llm/system_prompt_capture_test.go:385`（`adapter.NewDeepAdapter()` + `CreateInstance(ctx, nil, "agent.plan", "")`）

模式与 dreaming 参数的对应关系（由生产代码决定，测试须据此断言）：

| Adapter | `CreateInstance` mode | `dreamingMode` | 配置键 | 输出目录 |
|---|---|---|---|---|
| `CodeAdapter` | `"code"`（`code_adapter.go:195` 固定） | `code` | `memory.dreaming.code` / `DREAMING_CODE_ENABLED` | `<DataDir>/agent/workspace/coding_memory` |
| `DeepAdapter` | `"agent"`（生产由 `resolveModeFromRequest` 从 `agent.plan` 拆出 mode/subMode） | `agent` | `memory.dreaming.agent` / `DREAMING_AGENT_ENABLED` | `<DataDir>/agent/workspace/memory` |

agent 模式的 output 目录经 `agentWorkspaceDir → workspaceDir` 回退得到：`DeepAdapter` 不赋 `agentWorkspaceDir`，`CreateInstance` 步骤 11（`deep_adapter.go:473-485`）把 `workspaceDir` 设为 `workspace.AgentWorkspaceDir()`，恰好等于 Python `getattr(self, "_agent_workspace_dir", None) or self._workspace_dir` 的语义。

> 实施期修正（2026-10-31）：初稿假设 `CreateInstance` 会收到 `"agent.plan"`。实际两侧生产链路都先做归一化——Python `_adapter_mode_for_request` 只返回 `"code"` / `"agent"`，Go `resolveModeFromRequest`（`internal/swarm/server/runtime/agent_manager.go:780-800`）按 `"."` 切分得到 `mode="agent"` + `subMode="plan"`。因此 `dreamingMode` 恒为 `"agent"`。另注：Python 与 Go 的 `output_name` 判定都是 `mode == "agent"`，若测试直接把子模式串传给 `CreateInstance`，两侧都会落到 `coding_memory`——这是测试误用，不是生产行为。

### D4：tick 不可驱动，接线断言落在副作用上

`DreamingOrchestrator.loop` 首轮 tick 需等待一个 interval（钳位下限 60s，CI 不可接受），`tick()`（`orchestrator.go:231`）未导出、无法手动驱动。因此：

- **接线断言**：`StartDreaming` 内部会 `sweeper.Init()`，而 `Init` 会 `MkdirAll(outputDir)` 与 `MkdirAll(outputDir/.dreams)`。断言这两个目录被创建，即可证明 adapter 算出的 sessions/output 目录正确、sweeper 真被初始化。
- **产物断言**（DREAMING.md 内容、checkpoint 结构、增量窗口）：直接驱动 `Sweeper.RunSweep`，见 §5。

## 4. 测试基础设施改动

| 文件 | 改动 | 说明 |
|---|---|---|
| `tests/integration/mockllm/mock_client.go` | `InvokeRecord` 增 `Messages` 字段，`Invoke` 记录入参（约 6 行） | D2；目录已在审计豁免清单 |
| `tests/integration/testhelpers/dreaming_env.go`（新增） | `DreamingEnv`：包装 `NewAdapterTestEnv` + `WriteConfigYAML`；方法 `SessionsDir()` / `OutputDir(mode)` / `PlantSession(sid, mode, rounds)` / `AppendRounds(sid, n)` / `Touch(sid, delta)`（`os.Chtimes`）/ `ReadCheckpoint()` / `ReadDreamingMD()` / `AddJSONResponse(items)` / `UserPrompts()`（从 `InvokeRecord.Messages` 抽 user 文本） | 审计要求"每个文件必须有 suite.Run"，公共 helper 不能放测试文件；`testhelpers` 包没有 `_test.go`，不在审计 Dirs 列表（已用 `integration-audit -json` 核实） |

`AddJSONResponse` 统一以 ```` ```json\n[...]\n``` ```` 形式喂响应，命中 `extractViaLLM` 的围栏分支。

## 5. 用例清单

### 5.1 管线：`tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`（新增，12 例）

包 `dreaming`，`//go:build integration`，`SweeperIncrementalSuite` + `TestSweeperIncrementalSuite(t)` 调 `suite.Run`（满足每文件门禁）。`SetupTest` 里 `s.env = testhelpers.NewDreamingEnv(s.T(), s.MockLLM, "dreaming_itest")`，`TearDownTest` 里 `defer` 收尾。每个用例均断言 `s.MockLLM.InvokeCallCount()`，避免配置缺失时 `extractViaLLM` 静默返回 `(nil, nil)` 造成"空跑通过"。

| # | 用例 | 对齐 | 断言要点 |
|---|---|---|---|
| 1 | `TestSweep_首次扫描写出知识与检查点` | Python 1 | DREAMING.md 含两条 title 与 `_source: sess-001`；checkpoint 三 sid 的 `RoundCount==5`、`HistoryMtime>0`、`LastScanTs` 非空；`InvokeCallCount==3` |
| 2 | `TestSweep_第二次同标题不重复` | Python 2（按意图重写） | 第二轮补 3 个新会话使门槛放行；同 title 只保留一次；`count("用户偏好 Python")==1` |
| 3 | `TestSweep_增量只重扫新轮次` | Python 3 + 加强 | 5 轮 → 9 轮、`Touch(+1s)`；第二轮 prompt **含** `新问题6`~`新问题9`、**不含** `第1个问题`~`第5个问题`；DREAMING.md 两轮知识共存 |
| 4 | `TestSweep_未变更会话跳过` | Python 4 + 加强 | `InvokeCallCount==0`；DREAMING.md 内容字节不变 |
| 5 | `TestSweep_Code模式写consolidated` | Python 5 | 2 个 `consolidated_*.md`，frontmatter `name` / `source_session` / `created_at` 齐全 |
| 6 | `TestSweep_Code内容哈希去重` | Python 6 | 同 content 两条 → 1 个文件 |
| 7 | `TestSweep_LLM报错无知识且会话仍记为已扫` | 新增 | mock 返回 Err → 不产出任何知识、DREAMING.md 不存在；但 checkpoint 三 sid 仍被推进到 `RoundCount==5`（对齐 §2.3 的实际语义：`extractViaLLM` 吞异常视为成功，不会重试） |
| 8 | `TestSweep_新会话不足3且无过期会话则整轮跳过` | 新增 | 2 个新会话 → `InvokeCallCount==0` |
| 9 | `TestSweep_存在7天前会话则绕过门槛` | 新增 | 2 个新会话，其一**目录** mtime 8 天前 → 真跑（`InvokeCallCount>0`） |
| 10 | `TestSweep_超30天会话标记已扫并丢弃` | 新增 | 1 个目录 31 天前会话 + 2 个正常会话（凑够门槛并让 `saveCheckpoint` 真的落盘）→ 旧会话内容不进 DREAMING.md，checkpoint 中该 sid 为零值 `ScannedSession{}` |
| 11 | `TestSweep_增量新轮次不足4则跳过` | 新增 | 5 轮 → 7 轮（mtime 已变新）→ `InvokeCallCount==0` |
| 12 | `TestScanNewSessions_模式不匹配跳过` | 新增 | 3 个 `agent` 会话 + 1 个 `code.normal` 会话（凑门槛）→ 结果 `Len==3` 且不含 code 会话 |

### 5.2 接线：`tests/integration/swarm/agents/harness/common/memory/dreaming/start_wiring_test.go`（新增，4 例 + 1 可选）

`StartWiringSuite` + `TestStartWiringSuite(t)`。

| 用例 | 内容 |
|---|---|
| `TestStartStop_启用配置` | `DREAMING_AGENT_ENABLED=true` → `StartDreaming` → `GetDreamingOrchestrator` 返回同一实例 → `StopDreaming` 后为 nil |
| `TestStart_禁用配置` | env=false → 返回 nil 且注册表无残留 |
| `TestWiring_CodeAdapter真跑后启动Dreaming` | `NewCodeAdapter` → `CreateInstance(ctx, cfg, "code", "")` → 种 3 个 `code.normal` 会话到 `workspace.AgentSessionsDir()` → `TryStartDreaming(idle)` → 断言 `<DataDir>/agent/workspace/coding_memory/` 与 `.dreams/` 已创建 → `TryStopDreaming` → 注册表清空 |
| `TestWiring_DeepAdapter真跑后启动Dreaming` | `NewDeepAdapter` → `CreateInstance(ctx, nil, "agent.plan", "")` → 种 3 个 `agent.plan` 会话 → `TryStartDreaming(idle)` → 断言 `<DataDir>/agent/workspace/memory/` 与 `.dreams/` 已创建 → `TryStopDreaming` |

`TestWiring_BusyChecker阻断启动`（busy → 不启动、`dreamingStarted` 不置位）作为可选追加，归属同一 suite。

### 5.3 real-llm：`tests/integration/real_llm/dreaming_real_test.go`（新增，1 例）

`//go:build integration && llm`，沿用 `ltm_real_test.go` 的守卫模式（`skipUnlessRealLLMTests`）。种 3 个 4+ 轮会话 → 真 `RunSweep` → 断言：不 panic、`InvokeCallCount==3`、checkpoint 三 sid 齐全、若产出的条目非空则 frontmatter 合法（**不断言具体 title**）。

补的空白是"提示词能被真实模型消费 + 围栏解析在真模型输出上生效"。增量语义仍由 §5.1 承担——real-llm 无法提供 `count==1`、`round_count==9` 这类精确断言（提示词本身写明"绝大多数情况输出 `[]` 是常态"）。

## 6. 跨子系统跨度与豁免撤销

目录聚合的真实调用（审计按 `CallExpr` 判定）：

| 子系统 | 真实调用点 |
|---|---|
| `swarm/agents` | `NewSweeper` / `RunSweep` / `ScanNewSessions` / `StartDreaming` |
| `agentcore/memory` | `NewDreamingOrchestrator` / `GetDreamingOrchestrator` / `StopDreaming` |
| `common/workspace` | `AgentSessionsDir` / `AgentWorkspaceDir` |
| `swarm/server` | `NewCodeAdapter` / `NewDeepAdapter` / `CreateInstance` / `TryStartDreaming` / `TryStopDreaming` |

跨度 4，高于 `min-call-span=3`，不再进入灰区。收尾时从 `.integration-scope-exempt` 删除该目录条目及其注释（现记"覆盖 2 子系统"），并以 `integration-audit -root <该目录> -verify` 复核。

既有 `sweeper_e2e_test.go` 保留全部 20 个用例，仅调整 helper：`createSessionDir` 增加 mode 参数（默认 `agent`）、mtime 构造改用 `os.Chtimes`。删除用例会削弱有效测试密度，不在本次范围。

## 7. 风险与处置

| 风险 | 处置 |
|---|---|
| 双模式 `CreateInstance` 任一失败 | **视为生产缺陷**，按 D3 定位修复后重跑；不得改为只保留 code、不得 `t.Skip` 掩盖。修复若改变生产行为，需同步在本文档追加"实施期发现的偏差"小节（编号 X-n，含现象/根因/修复点/回归用例） |
| 配置缺失导致 `extractViaLLM` 返回 `(nil,nil)` 且会话仍记成功 → 空跑通过 | 每个用例强制断言 `InvokeCallCount`，为 0 即失败 |
| `MockModelClient` 响应按队列顺序消费 | 会话按固定 id 命名，`ScanNewSessions` 内部 `sortedKeys` 保证遍历顺序确定，响应按调用序号对齐；断言时按同序映射 |
| 直接把子模式串（`agent.plan`）传给 `CreateInstance` 会落 `coding_memory`，与生产路径不符 | 已核实生产链路先归一化为 `mode="agent"`（见 §3 D3 实施期修正），接线用例统一用 `CreateInstance(ctx, nil, "agent", "")` |
| 全局状态串味（orchestrator map / ClientRegistry / ResourceMgr / path 缓存） | 复用 `NewAdapterTestEnv` 的 cleanup（Unregister + `rm.Release` + 恢复 env + `pathutil.ResetCache`），用例内 `defer StopDreaming(ctx, "")` |
| mtime 精度导致增量判定抖动 | 一律 `os.Chtimes` 显式指定，禁用 sleep |
| 集成审计门禁 | 收尾跑 `go run ./cmd/integration-audit -root <该目录> -verify`，确认跨度 ≥3 且不进入覆盖交叉验证 |

## 8. 验收标准

1. `GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/` 全绿，且 `-race` 无告警。
2. 12 个管线用例全部断言 `InvokeCallCount`（无空跑用例）；4 个接线用例全部断言目录副作用与全局注册表状态。
3. `go run ./cmd/integration-audit -root tests/integration/swarm/agents/harness/common/memory/dreaming -verify` 通过；`.integration-scope-exempt` 中该目录条目已删除。
4. `go test -tags='integration llm' ./tests/integration/real_llm/ -run Dreaming` 在具备真实模型凭据时通过，无凭据时 Skip。
5. 远端 CI Build / Lint / Unit Test / Integration Test 四 job 全绿（规则 6）；real-llm 用例走 `real-llm-ci.yml`。

## 9. 文档与登记同步

- `tests/integration/swarm/agents/harness/common/memory/dreaming/doc.go`：补 `sweeper_incremental_test.go`、`start_wiring_test.go` 条目（规则 1）。
- `.integration-scope-exempt`：删除该目录条目与注释。
- `IMPLEMENTATION_PLAN.md`：仅在 7.20 备注追加集成用例计数，不新增行。
- 现有 20 个用例的 helper 调整不改变用例语义，无需在计划表登记。

## 10. 实施顺序

1. `mockllm.InvokeRecord` 增加 `Messages`；`testhelpers/dreaming_env.go` 落地。
2. `sweeper_incremental_test.go` 用例 1~6（Python 移植），跑通后再补 7~12（边界）。
3. `start_wiring_test.go`：先 code 模式（有先例），再 agent 模式；任一失败按 §7 定位修复。
4. `real_llm/dreaming_real_test.go`。
5. 现有 `sweeper_e2e_test.go` helper 调整（mode 参数 + `os.Chtimes`）。
6. `doc.go` / `.integration-scope-exempt` / `IMPLEMENTATION_PLAN.md` 同步，跑审计与远端 CI。
