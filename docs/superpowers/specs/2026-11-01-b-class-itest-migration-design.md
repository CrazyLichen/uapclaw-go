# B 类集成测试迁移设计（jiuwenswarm 存量 system_tests）

- 日期：2026-11-01
- 来源：`jiuwenswarm-develop/tests/system_tests/test_init_workspace.py`（19 用例，4 条 Python 侧 `skip`）、`test_team_continuous_dialog.py`（1 用例，Python 侧 `skip`）
- 目标仓库：`uap-claw-go`
- 决策记录：范围只做「生产已就绪、仅缺测试」的 B 类；交互用例用真 PTY 而非改生产代码；init 测试做链路级；team 用例先探针后定断言档位；额外补一条真 LLM 变体

## 1. 背景与目标

### 1.1 为什么只剩 16 条

对 `openjiuwen`（`/home/opensource/agent-core`）89 个 `system_tests` 文件与 `jiuwenswarm` 13 个 `system_tests` 文件逐文件核对「被测生产模块在 Go 侧是否真实就绪」后，剩余未迁移测试分两类：

| 类 | 判据 | 模块 | Python 侧规模 |
|---|---|---|---|
| A | Go 生产代码是桩或整文件未移植 | RL 子系统 `agent_rl/` | 7 文件 |
| A | 同上 | `workflow_agent` | 2 文件 |
| A | 同上 | `dev_tools/agent_builder`（计划 14.4-14.9 ☐） | 22 文件 |
| A | 同上 | `auto_harness`（计划 10.6.11 ☐） | 2 文件 |
| A | 同上 | `runner` RemoteAgent/A2A（`IsRemoteAgent` 恒 false） | 2 文件 |
| A | 同上 | `tune`（计划 14.14 ⤵️） | 1 文件 |
| A | 同上 | `ReActAgentEvolve`（Go 无该类型） | 1 文件 |
| A | 同上 | `remote_member_bootstrap`（2598 行，`⤵️#10.6.21`） | 10 用例 / 2 文件 |
| A | 同上 | `deep_adapter_a2x`（5 方法全 `pending backfill` 桩）+ `a2x_registry_runtime`（548 行） | 3 用例 / 1 文件 |
| **B** | **生产已就绪，仅缺测试** | `common/workspace.Init/Prepare` | **15 用例 / 1 文件** |
| **B** | 同上 | team 模式进程内 serve（`deep_adapter_team` 820 行） | **1 用例 / 1 文件** |

A 类的测试无法先写——测试的对象不存在，写了也只能断言桩的日志字符串。本批只做 B 类 16 条，A 类留给对应生产移植任务按既有 `⤴️/⤵️` 机制回填。

### 1.2 目标

1. 把 2 个 Python 文件的 16 条有效用例迁到 Go，落 `tests/integration/`，受 `cmd/integration-audit` 双层门禁约束。
2. 建立两块可复用基础设施：Linux PTY 终端模拟 helper、进程内 serve 装配范式。
3. 补一条真 LLM 变体，把 Python 侧因不稳定而 `skip` 的 `test_team_continuous_dialog` 真正跑起来（夜间 `real-llm-ci` 收口）。

### 1.3 非目标

- 不移植 `remote_member_bootstrap.py` / `a2x_registry_runtime.py` / `deep_adapter_a2x` 的任何生产逻辑。
- 不实现 `uapclaw init` / `agentserver` / `gateway` / `web` 子命令（当前均为打印「尚未实现」的桩）。
- 不实现 `transport.WebSocketTransport`，不做 subprocess 双进程真系统测试。
- 不修改任何非测试生产代码。

## 2. 现状盘点

### 2.1 Python 源测试结构

`test_init_workspace.py`（315 行，6 个测试类）：

| 类 | 用例数 | 性质 | Python 侧状态 |
|---|---|---|---|
| `TestResolvePreferredLanguage` | 2 | 语言解析优先级 | 跑 |
| `TestPromptPreferredLanguage` | 8 | 交互 prompt（monkeypatch `_is_interactive` + `builtins.input`） | 跑 |
| `TestInitUserWorkspace` | 2 | 首次初始化 / 取消语言选择 | 跑 |
| `TestInitWorkspaceMain` | 3 | 成功 / 取消 / force | 跑 |
| `TestInitCLI` | 1 | CLI 命令 | **skip**（需完整安装） |
| `TestInitIntegration` | 3 | full_initialization_flow / config 内容 / 模板复制 | **skip**（需手工隔离环境） |

`test_team_continuous_dialog.py`（377 行，1 用例）：`subprocess` 起 `app_agentserver` + `app_gateway` → `websockets.connect("/ws")` → 连发两条 `chat.send`（mode=team，第二条在首条流未结束时发出）→ 断言收到 ≥1 事件、出现 `team.member`/`team.message`、第二条确实已发。整用例 `@pytest.mark.skip("temporarily skipped until team continuous dialog is stable")`。

### 2.2 Go 侧生产就绪度

| 生产对象 | 位置 | 状态 |
|---|---|---|
| `workspace.Init(InitOption) (*InitResult, error)` | `internal/common/workspace/init.go:76` | 完整（含 overwrite 确认 / 语言选择 / 命名实例） |
| `workspace.Prepare` | `init.go:204` | 完整（config.yaml/.env/目录/多语言模板/写回 preferred_language） |
| `isInteractive` / `promptYesNo` / `promptPreferredLanguage` | `init.go:332/341/353` | 包级私有函数，读 `os.Stdin` |
| `config.New(path)` | `internal/common/config/config.go:62` | 完整 |
| `transport.NewChannelTransport` | `internal/swarm/transport/channel_transport.go` | 完整 |
| `server.NewAgentServer` / `SetAgentFactoryForTest` | `internal/swarm/server/agent_server.go` | 完整 |
| `gateway.NewAppGatewayServer` + `/ws` 路由 | `internal/swarm/gateway/app_gateway.go:689` | 完整 |
| team 模式路由 | `internal/swarm/server/adapter/deep_adapter_team.go`（820 行） | 完整 |

现成装配参考：`cmd/uapclaw/acp_cmd.go:148 bootstrapInprocServers`（生产自举）、`tests/integration/swarm/gateway/tui_cli_route_test.go:88-150`（7 件套测试装配）。

### 2.3 两个卡点（已核实）

**卡点 1：7 条交互 prompt 用例在 Go 侧无法直接触发。**
`isInteractive()` 判 `os.Stdin.Stat().Mode()&os.ModeCharDevice != 0`；`os.Pipe()` 人为造出的 stdin 是 `ModeNamedPipe`，永远走非交互分支——现有单测 `internal/common/workspace/init_test.go:537` 正是利用这一点强制非交互。Go 无 `monkeypatch`，跑不到交互分支。
决策：测试侧造真 PTY（用户拍板），不改生产代码。

**卡点 2：`integration-audit` 不认 `internal/common/*`。**
`cmd/integration-audit/audit_test.go:435` 断言 `subsystemOf(".../internal/common/logger") == ""`。因此 `workspace.Init` + `config.New` 对调用跨度贡献为 0；纯库层 init 测试跨度不足，会落入灰区并触发「≥3 子系统 × ≥30 行」覆盖交叉验证，大概率不通过。
决策：init 测试做链路级，由测试文件自己调用 `swarm/transport` + `swarm/server` + `swarm/gateway` 三个子系统构造器取得真实跨度。

## 3. 文件规划

```
tests/integration/
├── testhelpers/
│   ├── doc.go                              ✏️ 补 pty_linux.go / pty_stub.go 目录项
│   ├── pty_linux.go                        🆕 Linux PTY 终端模拟（//go:build linux）
│   ├── pty_stub.go                         🆕 非 Linux 占位（//go:build !linux）
│   └── pty_linux_test.go                   🆕 helper 自测
├── swarm/
│   ├── workspace/                          🆕 目录
│   │   ├── doc.go                          🆕 包文档
│   │   └── init_flow_test.go               🆕 init 15 条用例
│   ├── gateway/
│   │   ├── doc.go                          ✏️ 补新文件目录项
│   │   └── team_continuous_dialog_test.go  🆕 MockLLM 版 team 用例
└── real_llm/
    ├── doc.go                              ✏️ 补新文件目录项
    └── team_continuous_dialog_real_test.go 🆕 真 LLM 版 team 用例
```

新增目录 `tests/integration/swarm/workspace/` 的理由：Python 侧该测试属于 jiuwenswarm（Go 的 `internal/swarm`）系统测试，测的是 swarm 侧工作区初始化；`internal/common/workspace` 虽为被测主体，但 common 不参与跨度统计，把测试放在 swarm 树下语义与门禁都更顺。

## 4. 基础设施设计

### 4.1 PTY helper（`testhelpers`）

```go
// OpenPTY 打开一对伪终端，返回 master 与 slave。
// slave 的 Stat().Mode() 含 ModeCharDevice，等价于真实终端。
func OpenPTY() (master *os.File, slave *os.File, err error)

// WithPTYStdin 把 slave 装为 os.Stdin 并在 t.Cleanup 复原。
func WithPTYStdin(t *testing.T, slave *os.File)
```

实现步骤（Linux）：
1. `os.OpenFile("/dev/ptmx", os.O_RDWR, 0)`
2. `unix.IoctlGetInt(fd, unix.TIOCGPTN)` 取从端编号 N（常量 `0x80045430`，`golang.org/x/sys v0.49.0` 已提供）
3. `os.OpenFile("/dev/pts/N", os.O_RDWR, 0)`
4. `unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0)` 解锁从端（`0x40045431`）

依赖：`golang.org/x/sys` 当前为 indirect（`go.mod:270`），需提升为 direct；模块缓存已有 v0.49.0，无需联网下载。

跨平台：`pty_stub.go` 提供 `//go:build !linux` 的同签名实现，返回 `ErrPTYUnsupported`；用例遇此错误 `t.Skip`。CI 为 ubuntu-latest，不触发 stub。

**风险预案**：若 CI 容器 `/dev/ptmx` 不可用（helper 自测失败），则 7 条交互用例转 `t.Skip` 并在迁移台账记录偏差，不阻塞其余用例。

### 4.2 进程内 serve 装配：不抽 suite 基类

两个测试文件各自 inline 约 25 行装配，顺序与 `acp_cmd.go:bootstrapInprocServers` 同构：

```
transport.NewChannelTransport()
routing.NewAgentClient(transport)
server.NewAgentServer(cfg, transport)      // 可 SetAgentFactoryForTest
gateway.NewAppGatewayServer(cfg, agentClient)
Start: agentServer → agentClient.Connect → msgHandler.StartForwarding
       → channelMgr.StartDispatch → inboundServer.Start → gwServer.Start
Stop : 逆序
```

**为什么不抽到 `tests/integration/suite/`**：`integration-audit` 的调用跨度统计范围是「测试目录内的文件」。若把三个构造器调用藏进 `suite` 包，测试文件自身的跨度归零，目录直接掉进灰区，再被覆盖交叉验证打回。装配留在测试文件里，跨度才是真实的。

### 4.3 MockLLM 用法

沿用 `mockllm.MockModelClient`：`SetResponses` / `AddTextResponse` / `AddToolCallResponse` / `InvokeCallCount`。注意 `getNextResponseLocked` 队列耗尽时回落 `defaultMockResponse` 固定文本（**不报错**），表现是流程提前收敛而非失败——断言必须锁定具体事件序列，不能以「跑完即通过」判定。

## 5. init 用例映射

落点：`tests/integration/swarm/workspace/init_flow_test.go`，嵌入 `suite.BaseIntegrationSuite`。

| # | Python 用例 | Go 用例 | 断言要点 | 手法 | 计入跨度 |
|---|---|---|---|---|---|
| 1 | `test_resolve_explicit_language` | `TestInit_显式语言` | `Init(Language:"en")` → `config/config.yaml` 中 `preferred_language: en`，且 `config.New` 读回一致 | 链路 | 无（common 不计） |
| 2 | `test_resolve_default_to_zh` | `TestInit_缺省回落zh` | 非交互（管道 stdin）+ 空语言 → zh | 链路 | 无（common 不计） |
| 3 | `test_prompt_select_chinese` | `TestPrompt_选择编号1` | PTY 喂 `1\n` → zh | PTY | 无 |
| 4 | `test_prompt_select_english` | `TestPrompt_选择编号2` | PTY 喂 `2\n` → en | PTY | 无 |
| 5 | `test_prompt_select_zh_alias` | `TestPrompt_别名zh` | PTY 喂 `zh\n` → zh | PTY | 无 |
| 6 | `test_prompt_select_en_alias` | `TestPrompt_别名en` | PTY 喂 `en\n` → en | PTY | 无 |
| 7 | `test_prompt_cancel_with_no` | `TestPrompt_取消no` | PTY 喂 `no\n` → `Cancelled:true` | PTY | 无 |
| 8 | `test_prompt_cancel_with_n` | `TestPrompt_取消n` | PTY 喂 `n\n` → `Cancelled:true` | PTY | 无 |
| 9 | `test_prompt_cancel_with_q` | `TestPrompt_取消q` | PTY 喂 `q\n` → `Cancelled:true` | PTY | 无 |
| 10 | `test_prompt_invalid_input_returns_none` | `TestPrompt_非法输入` | PTY 喂 `xyz\n` → `Cancelled:true`，且未创建工作区 | PTY | 无 |
| 11 | `test_init_user_workspace_first_time` | `TestInit_首次初始化_结构` | `config/`、`agent/home`、`agent/skills`、`agent/memory`、`agent/sessions`、`agent/workspace`、`agent/.checkpoint`、`agent/.logs` 8 项 + `config.yaml` + `.env` + `PRINCIPLE.md`/`TONE.md`/`HEARTBEAT.md` 模板 | 链路 | 无（common 不计） |
| 12 | `test_init_user_workspace_cancel_language_selection` | `TestInit_取消不落盘` | PTY 喂 `q\n` → 取消后目标目录不存在 | PTY | 无 |
| 13 | `test_main_successful_init`（+ 变体 `test_full_initialization_flow`） | `TestInit_链路_服务可启动`★ | Init → `config.New` 读回语言 → 构造 transport/server/gateway → Start → WS dial 收 `connection.ack` → 逆序停机 | 链路★ | **transport + server + gateway（唯一贡献行）** |
| 14 | `test_main_cancelled_init` | `TestInit_覆盖前拒绝` | fence 已有工作区，PTY 喂 `no\n` → `Cancelled:true` 且原文件仍在 | PTY | 无 |
| 15 | `test_main_force_init` | `TestInit_force重建` | 工作区预置脏文件 → `Overwrite:true` → 脏文件被清除、结构重建 | 链路 | 无（common 不计） |
| — | `test_config_file_content`（Python skip） | `TestInit_config内容` | config.yaml 的语言/端口/模型字段可被 `config.New` 正确解析 | 链路 | 无（common 不计） |
| — | `test_agent_templates_copied`（Python skip） | 并入 #11 | 模板三件套由 #11 断言 | — | — |
| — | `test_cli_init_command`（Python skip） | **不迁，记偏差** | `uapclaw init` 是打印「尚未实现」的桩；改由 #13 的库层链路覆盖 | — | — |

合计 16 个 Go 测试方法：15 条对齐 Python 有效用例（#13 合并 Python 另一条 skip 的 `test_full_initialization_flow` 语义），另加 1 条覆盖 Python skip 的 config 内容断言。

★ 第 13 条是本目录过门禁的关键：测试文件内直接调用 `transport.NewChannelTransport()`、`server.NewAgentServer()`、`gateway.NewAppGatewayServer()`，三者分属 `swarm/transport`、`swarm/server`、`swarm/gateway`，真实调用跨度 = 3。

## 6. team 用例映射

### 6.1 MockLLM 版（主 CI 常跑）

落点：`tests/integration/swarm/gateway/team_continuous_dialog_test.go`（与 `tui_cli_route_test.go` 同目录，该目录并集跨度已充分）。

流程：进程内装配 → WS dial `/ws` → 发 `chat.send`（`mode:"team"`，session S）→ 收到首个事件后**在流未结束时**发第二条 `chat.send`（同 session S，不同 request id）→ 收事件至收敛或超时。

断言分两档，**由 T0 探针结果决定**：

| 档位 | 触发条件 | 断言 |
|---|---|---|
| 强（对齐 Python） | 探针证明 MockLLM 能驱动出 team 事件 | ① 收到 ≥1 事件 ② 出现 `team.member` 或 `team.message` ③ 第二条在首条流未结束时已被发出并受理（两条 request id 均有对应响应）|
| 弱（降级） | 探针不能稳定产出 team 事件 | ① 同 session 两条并发 `chat.send` 均被受理（无互斥拒绝） ② 两条 request id 都有响应 ③ 首条流未在第二条到达前提前终止 |

降级时必须在本文件顶部注释写明降级原因与强语义由 `real_llm` 版本承担。

### 6.2 真 LLM 版

落点：`tests/integration/real_llm/team_continuous_dialog_real_test.go`，`//go:build integration && llm`，嵌入 `suite.RealLLMSuite`（`LLM_API_KEY` 缺失时 Skip）。

断言完全对齐 Python 原用例：创建成员并让成员回报自身工具清单 → 收到 `team.member` / `team.message` → 第二条插话在流未结束时被受理。

接线：`.github/workflows/real-llm-ci.yml` 已运行 `go test ... ./tests/integration/real_llm/...`，新增文件自动纳入；仅需把文件头注释里的「12 套件」更新为 13。

## 7. 探针（T0，先行）

**问题**：`mode=team` 经 `deep_adapter_team.go` 的 team 路由时，能否被 MockLLM 脚本驱动出 `team.member` 事件。

**方法**：最小化搭一次进程内 serve，MockLLM 预置 `CreateToolCallResponse("team.spawn_member", ...)` + 若干文本响应，跑一次 `chat.send`，打印收到的事件序列（`t.Log`）。

**判定**：
- 出现 `team.member` 或 `team.message` → 6.1 用强断言，探针代码转为正式用例的前半段
- 未出现 → 6.1 用弱断言 + 注释记录，删掉探针中的脚本化 tool_call 预设

探针只观测、不上断言，约 30 分钟；结论写入实施计划的完成记录。

## 8. 门禁与验收

1. **本地单跑**：`go test -tags "sqlite_fts5 test integration" -v ./tests/integration/swarm/workspace/... ./tests/integration/swarm/gateway/...`
2. **审计自检**：`go run ./cmd/integration-audit -verify`，`swarm/workspace` 与 `swarm/gateway` 均不得落灰区
3. **CI 四绿**：Build / Lint / Unit Test / Integration Test 全 SUCCESS（规则 6，本地通过不算完成）
4. **真 LLM 版**：本地持 `LLM_API_KEY` 手跑一次；随后观察夜间 `real-llm-ci` 首次运行结果
5. **文档同步**：`IMPLEMENTATION_PLAN.md` 若有对应条目需同步状态；`doc.go` 目录表随新增文件更新

## 9. 风险

| 风险 | 影响 | 应对 |
|---|---|---|
| CI 容器 `/dev/ptmx` 不可用 | 7 条交互用例无法执行 | helper 自测先跑；不可用则该 7 条 `t.Skip` + 台账记偏差，其余 9 条照常交付 |
| MockLLM 驱动不出 team 事件 | 6.1 断言降级 | 已设计弱断言档位；强语义由 6.2 真 LLM 版承担 |
| `swarm/workspace` 仍被判灰区 | 覆盖交叉验证打回 | 首选补 `agentcore/session` 会话落盘断言拉跨度；次选登记豁免并在 `.integration-scope-exempt` 写明理由 |
| MockLLM 队列耗尽静默收敛 | 假通过 | 所有断言锁定具体事件序列与计数，不以「跑完」为通过条件 |
| PTY 读端阻塞 | 用例挂死 | 所有 PTY 读操作带 deadline；`t.Cleanup` 关闭 master/slave |

## 10. 明确不做

- 不写任何 A 类测试（RL / workflow_agent / agent_builder / auto_harness / RemoteAgent / tune / ReActAgentEvolve / remote_member_bootstrap / a2x）。
- 不改 `internal/common/workspace`、`internal/swarm/*` 的任何生产代码。
- 不动 `.integration-scope-exempt`（除非风险条目 3 触发）。
- 不实现 `uapclaw init` 子命令。
