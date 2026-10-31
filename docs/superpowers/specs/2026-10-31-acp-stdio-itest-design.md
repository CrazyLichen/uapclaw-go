# ACP stdio 集成测试迁移设计（出站 acp_chat 工具链 + 入站 acp 二进制黑盒）

- 对应 Python：`jiuwenswarm-develop/tests/system_tests/test_acp_stdio_client_st.py`（270 行）+ `tests/system_tests/test_acp_cli_stdio.py`（146 行）+ `tests/integration/test_acp_cli_stdio_integration.py`（105 行）+ `tests/unit_tests/acp/`（89 行）
- 被测 Go 代码：`internal/swarm/acp/`（963+153 行）、`internal/swarm/agents/harness/common/tools/acp_chat/`、`internal/swarm/gateway/channel_manager/protocol/acp/`（1827 行）、`cmd/uapclaw/acp_cmd.go`（200 行）、`cmd/uapclaw/acp_chat_cmd.go`（317 行）
- 日期：2026-10-31

## 1. 背景与定位

### 1.1 Python 侧实际位置（勘误）

任务最初指向 `jiuwendevloper` / `openjiuwen` 两个目录，实际核对结果：

| 路径 | 是否存在 | ACP 命中 |
|---|---|---|
| `/home/opensource/jiuwendevloper` | 不存在 | — |
| `/home/opensource/openjiuwen` | 不存在（内容在 `agent-core/openjiuwen`） | 2 处，均为无关子串 |
| `/home/opensource/jiuwenswarm-develop` | 存在 | 46 个 py 文件，**ACP stdio 全部在此** |

因此本设计的 Python 基线只取 `jiuwenswarm-develop`。

### 1.2 Python 的 ACP stdio 测试是四层，不是一层

| 层级 | 文件（行数） | 被测对象 | 手法 |
|---|---|---|---|
| 纯函数单测 | `tests/unit_tests/acp/test_stdio_client_helpers.py`（40）、`test_subprocess_env.py`（49） | `_consume_one_json`、`_extract_session_update_text`、`build_acp_subprocess_env` | 直接调用 |
| **系统测试（真子进程）** | `tests/system_tests/test_acp_stdio_client_st.py`（270，7 例） | `AcpStdioClient` + `cli._run_once/_get_spec` | inline 写 fake agent `.py` 到 `tmp_path`，四 mode：`echo`/`error`/`permission`/`fs_read` |
| 系统+集成（Popen 打桩） | `tests/system_tests/test_acp_cli_stdio.py`（146，2 例） | `channels.acp.app_acp.run_acp` | `monkeypatch subprocess.Popen` → `FakeGatewayProcess`，断言“忽略 E2A chunk、取 final result / final jsonrpc error” |
| 集成（Popen 打桩） | `tests/integration/test_acp_cli_stdio_integration.py`（105，1 例） | 同上 | 与上者重复 |

关键结论：**Python 名为“集成”的两层其实是打进桩**，唯一真跨边界的是第 2 层（真子进程 fake agent）。它真正的设计价值是那个“一个脚本扮四种对端行为”的 fake agent，而不是 pytest marker 的分层。

### 1.3 本次目标

Go 侧功能已基本对齐，缺的是 `tests/integration/` 下的 ACP stdio 链路。本设计补两条方向、都是**真子进程**：

- **出站**（client）：`acp_chat` 工具 → `AcpStdioClient` → 假 ACP 对端子进程
- **入站**（channel）：真 `uapclaw` 二进制子进程（`AcpChannel` stdio）→ 真 WS → 父进程 GatewayServer → AgentServer → MockLLM

## 2. 拍板决策

| # | 决策项 | 结论 | 理由 |
|---|---|---|---|
| D1 | 迁移范围 | 出站 + 入站**两条都补**，均用真子进程 | 用户选择；也覆盖 Python 四层里唯一有真跨边界价值的部分 |
| D2 | 出站入口 | 经 `acp_chat` **工具**（`GetTools()[0].Invoke`），不经 CLI 函数 | 跨 `swarm/acp` + `swarm/agents` + `agentcore/foundation` 三个二级子系统，天然过 `integration-audit` 门禁；顺带验证 profile 校验与 `[ERROR]` 兜底语义 |
| D3 | 入站进程边界 | **黑盒二进制**：测试 spawn 真 `uapclaw acp` 子进程，stdin/stdout 收发 ACP JSON-RPC | 用户选择；`cmd/uapclaw/acp_cmd.go` 现仅 71 行单测（只测 URL 回落与旗标注册），channel 侧装配零端到端覆盖 |
| D4 | 入站模式 | 用 **mode-2**（`--gateway-url` 指回父进程），不用 mode-1 子进程自举 | mode-1 需在子进程内装配 AgentServer + 完整 agents 配置 + 假 OpenAI SSE 服务，fixture 成本高且脆弱；mode-2 保留“真子进程 + 真 WS”的全部价值，LLM 仍由父进程 MockLLM 打桩 |
| D5 | 假对端载体 | **Go 独立小程序** `testdata/fakeacp/main.go`，`TestMain` 里 `go build` | `encoding/json` 稳、四 mode 与 Python fake agent 一一对应、跨平台（不再需要 Windows skip） |
| D6 | 用例范围 | **1:1 映射 + Go 增强** | 用户选择；出站 11 例、入站 5 例 |
| D7 | 目录归属 | 出站 `tests/integration/swarm/acp/`（新，**不登记豁免**）；入站 `tests/integration/cmd/`（追加，**复用既有豁免**） | 见 §5.5 |
| D8 | 生产代码变更 | **零**（不新增导出 API）。唯一例外：若入站用例 2 探测出 Go 与 Python 语义不一致（R1），则按 Python 语义补实现并单独提交 | acpchat 的 `resetClientsForTest` 非导出，残留子进程靠父进程退出 Close stdin 自然回收，与 Python 进程级缓存同语义 |

## 3. Python 事实基线（逐项核对）

### 3.1 fake agent 四 mode 的真实行为

`test_acp_stdio_client_st.py` L22-159 的 inline 脚本：

| mode | 对端行为 | 期望客户端结果 |
|---|---|---|
| `echo` | 发 `session/update`（`agent_message_chunk` + `{"type":"text","text":"echo:<text>"}`）→ `result{stopReason:end_turn}` | `chat()` 返回 `echo:hello st` |
| `error` | 直接回 `error{code:-32603,message:"agent failed"}`，不发 update | 抛 `RuntimeError`，消息匹配 `ACP JSON-RPC error:.*agent failed` |
| `permission` | 发 `session/request_permission`（`toolCallId=tc-1`、`options=[{optionId:"allow-once",kind:"allow"}]`）→ 等响应 → 文本 `after-perm:<text>` | `ACP_AUTO_APPROVE_PERMISSIONS=true` 时自动批 `allow-once`，chat 正常完成 |
| `fs_read` | 发 `fs/read_text_file{path:"st_file.txt"}` → 等响应 → 文本 = 响应 `result.content` | cwd 内文件内容（无尾换行） |

固定回 `initialize → {protocolVersion:1}`、`session/new → {"sessionId":"st-fake-session"}`，未知方法回 `-32601`。

### 3.2 CLI 冒烟（`cli.py` L19-62）

- `_get_spec`：`config["acp_agents"]` 非映射 → `None`；profile 非映射 → `None`
- `_run_once`：spec 缺失返回 `2`；`command` 空返回 `2`；connect/chat 异常返回 `1`；成功返回 `0`
- 关闭走 `ACP_CLI_CLOSE_TIMEOUT_S`（默认 30）限时 `shield(client.close())`

### 3.3 入站（`app_acp.py` L70-152）

`run_acp` 起 gateway 子进程，逐行读 stdout：`response_kind == E2A_RESPONSE_KIND_E2A_CHUNK` 跳过；`is_final` 时转 ACP JSON-RPC 输出并 break；无 final 回 `error{code:-32603,"no final response from gateway"}` 且退出码 1。

## 4. Go 现状与缺口

### 4.1 已有覆盖

| Go 位置 | 行数/例数 | 对应 Python 层 | 手法 |
|---|---|---|---|
| `internal/swarm/acp/stdio_client_parse_test.go` | 128 / 9 例 | 第 1 层 | 纯函数 |
| `internal/swarm/acp/subprocess_env_test.go` | 54 / 4 例 | 第 1 层 | env 注入 |
| `internal/swarm/acp/stdio_client_test.go` + `stdio_client_helpers_test.go` | 175+668 / 44 例 | 第 2 层 | **真 shell 子进程**（`testdata/acp_peer.sh`、`acp_peer_fs.sh`） |
| `cmd/uapclaw/acp_chat_cmd_test.go` | 527 / 17 例 | 第 2/3 层 | 假对端 e2e + 退出码矩阵 + 130 中断 |
| `internal/swarm/gateway/channel_manager/protocol/acp/acp_channel_test.go` | 1343 / 31 例 | 第 4 层 | 纯函数/进程内 |
| `tests/integration/swarm/gateway/e2e_acp_bridge_test.go` | 593 / 3 例 | 第 4 层（e2e） | `io.Pipe` + WS 回环 + MockLLM |

### 4.2 缺口

| 缺口 | 现状 |
|---|---|
| G1 | 出站方向在 `tests/integration/` 下**完全不存在**（`find tests -iname "*acp*"` 无目录） |
| G2 | permission 握手与 fs 读写只有“纯函数单测 + 独立 peer 脚本”，无“真子进程 + 上层工具消费者”的组合链路 |
| G3 | 入站 `AcpChannel` 只被 `io.Pipe` 驱动，`uapclaw acp` 二进制本体（stdout→stderr 重定向、`acpStdout` 早捕获、WS 自连）零端到端覆盖 |
| G4 | `cmd/uapclaw/acp_cmd_test.go` 仅 71 行（`TestResolveAcpGatewayURL` + `TestNewAcpCmd_旗标注册`） |

### 4.3 门禁口径核对

`cmd/integration-audit/audit.go` 的 `subsystemOf`（L537-540）：`internal/<一级>/<二级>` → `<一级>/<二级>`，`internal/common/*` 返回空串（不计入）。第一层要求每个 `tests/integration/` 目录**真实调用**（`pkg.Fn(...)`）跨度 ≥3，否则落灰区交第二层覆盖验证。

出站 suite 的真实调用可满足：

| 调用 | 子系统 |
|---|---|
| `acpchat.NewAcpChatTool(cfg)` | `swarm/agents` |
| `acp.NewAcpStdioClient(...)`、`acp.WithChatTimeout(...)` | `swarm/acp` |
| `tool.NewToolCallOptions(...)`、`tool.ValidateToolCard(card)` | `agentcore/foundation` |

入站黑盒 import 极少，无法过门禁 → 放已豁免目录（见 §5.5）。

## 5. 实现方案

### 5.1 文件清单

```
tests/integration/swarm/acp/            ← 新增目录（出站）
├── doc.go                              # 包文档 + 对应 Python 路径 + 缓存残留说明
├── acp_chat_e2e_test.go                # //go:build integration, package acp_test, suite.Run
└── testdata/fakeacp/main.go            # 假 ACP 对端小程序（Go，四 mode）

tests/integration/cmd/                  ← 追加（入站）
└── acp_stdio_test.go                   # //go:build integration, package cmd_test, suite.Run

.integration-scope-exempt               ← 不新增条目（tests/integration/cmd 已在册）
```

### 5.2 假对端小程序（`testdata/fakeacp/main.go`）

- `os.Args[1]` 选 mode：`echo` / `error` / `permission` / `fsread`
- 读侧：`bufio.Scanner` 逐行 + `encoding/json`（Go 客户端恒写单行 compact JSON，无需 Python 的 `raw_decode` 缓冲）
- 写侧：`json.Marshal` + `\n` + `Flush`
- `initialize → {protocolVersion:1}`；`session/new → {"sessionId":"fake-<mode>-sess"}`（session id 带 mode，便于断言 profile→进程→会话不串台）
- `session/prompt` 按 mode 分支：

| mode | 行为 |
|---|---|
| `echo` | `session/update(agent_message_chunk,"echo:<text>")` → `result{stopReason:end_turn}` |
| `error` | `error{code:-32603,message:"agent failed"}`（不发 update） |
| `permission` | 发 `session/request_permission`（id=900，两个 option：`allow-once`/`reject-once`）→ 等响应 → 文本 `after-perm:<text>:<outcome>:<optionId>`（**Go 增强：把 outcome 回显**，才能区分批准/取消两条分支） |
| `fsread` | 发 `fs/read_text_file{path:"in_root.txt",line,limit}` → 等响应 → 发 `fs/write_text_file{path:"written.txt",content:"hello-acp"}` → 文本 `<content>\|<after-fs:<text>>`（**Go 增强：同时覆盖 write 分支**） |

未知方法回 `-32601`；stdin EOF 退出 0。

### 5.3 出站 suite 装配

`TestMain` 预编译（照抄仓库先例 `internal/swarm/extensions/plugin/e2e_test.go` L15-38）：

```go
cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakeacp/")
// 失败 → 路径置空（用例 t.Skip），不误红
```

每个用例用**唯一 profile 名**（`it_<mode>_<nanotime>`），规避 `acp_chat` 的进程级客户端缓存（`client_cache.go` 全局 `map`）串台：

```go
cfg := map[string]any{"acp_agents": map[string]any{
    profile: map[string]any{"command": bin, "args": []string{mode}, "cwd": root},
}}
tools := acpchat.NewAcpChatTool(cfg).GetTools()
out, err := tools[0].Invoke(ctx, map[string]any{"agent": profile, "message": "hello"},
    tool.NewToolCallOptions(tool.WithTimeout(30)))
```

`ACP_AUTO_APPROVE_PERMISSIONS` 由 `autoApprovePermissions()`（`stdio_client.go` L893-897）**调用时读 env**，故 `t.Setenv` 可按用例切换。用例**不使用** `t.Parallel`。

### 5.4 入站 suite 装配

1. 父进程装配 7 件套，照抄 `e2e_acp_bridge_test.go` 的 `SetupSuite`：`ChannelTransport` → `AgentServer` → `AgentClient` → `MessageHandler` → `ChannelManager` → `InboundGatewayServer` → `GatewayServer`（`BuildACPRouteBinding("/acp","acp")`）+ `NewAcpGatewayBridge` + `SetAcpBridge` + `InstallDefaultAcpHooks` + `RegisterExternalChannel("acp", gwServer)` + `OnMessage`；MockLLM provider 注册进 `ClientRegistry`；`UAPCLAW_DATA_DIR` 三件套（`config/config.yaml` 写 `models.defaults[].model_client_config.client_provider=<mock provider>`）
2. 随机端口 `pickFreePort()` 绑 `GatewayServer`
3. `go build -o <tmp>/uapclaw ./cmd/uapclaw`
4. 子进程：`<tmp>/uapclaw acp --gateway-url ws://127.0.0.1:<port>/acp`，env 带**独立**的 `UAPCLAW_DATA_DIR`（只放最小 config，不与父进程共享，见 R3）；stdin/stdout 为管道，stderr 收进 buffer 供失败诊断
5. 测试写入 `{"jsonrpc":"2.0","id":N,"method":...}` 行，按帧轮询读取 stdout

mode-2 路径只跑 `bootstrapGatewayEnv` + `AcpChannel`，跳过 `bootstrapInprocServers`，因此子进程无需模型/agents 配置——这是 D4 选 mode-2 的核心收益。

### 5.5 门禁与豁免策略

| 目录 | 跨子系统 | 处理 |
|---|---|---|
| `tests/integration/swarm/acp/`（出站） | 3（`swarm/acp`、`swarm/agents`、`agentcore/foundation`） | 过第一层，**不登记豁免**，保留门禁保护 |
| `tests/integration/cmd/`（入站） | 黑盒 import 极少 | 该目录已在 `.integration-scope-exempt`（“真跨进程：CLI 二进制构建 + exec.Command 子进程执行，进程边界即集成边界”），追加用例复用同一豁免理由 |

不把入站用例塞进 `tests/integration/swarm/acp/`：目录级豁免会把出站的白白豁免掉；也不塞 `tests/integration/swarm/gateway/`：黑盒用例 import 少会拉低该目录的调用跨度，可能拖红现有门禁。

## 6. 用例清单

### 6.1 出站（`tests/integration/swarm/acp/acp_chat_e2e_test.go`，11 例）

| # | 用例 | 来源 | 断言要点 |
|---|---|---|---|
| 1 | `TestAcpChatTool_Echo文本聚合` | Python case 1 | `result` 文本 = `echo:hello` |
| 2 | `TestAcpChatTool_JSONRPC错误转文本` | Python case 2 | `err == nil`；文本含 `[ERROR] acp_chat failed` 与 `-32603 agent failed`（工具不抛错语义） |
| 3 | `TestAcpChatTool_权限自动批准` | Python case 3 | `ACP_AUTO_APPROVE_PERMISSIONS=true` → 文本含 `:selected:allow-once` |
| 4 | `TestAcpChatTool_权限取消` | Go 增强 | env=`false` → 文本含 `:cancelled:` |
| 5 | `TestAcpChatTool_fs读写穿透` | Python case 4 | root 内 `line/limit` 切片生效；`written.txt` 落盘内容 = `hello-acp` |
| 6 | `TestAcpChatTool_Profile缺失` | Python case 7 | `[ERROR]` 文本，`err == nil` |
| 7 | `TestAcpChatTool_Profile无command` | Python `_run_once` 分支 | `[ERROR]` 文本 |
| 8 | `TestAcpChatTool_args非列表` | Python 单测 `_load_profile` | 真链路同样转 `[ERROR]` 文本 |
| 9 | `TestAcpChatTool_会话缓存复用` | Go 增强 | 对端文本回显 `os.Getpid()`，同 profile 两次调用 pid 相同 |
| 10 | `TestAcpChatTool_newSession重启` | Go 增强 | `new_session=true` → pid 变化 |
| 11 | `TestAcpStdioClient_直连基线` | Python case 1 逐字版 | `NewAcpStdioClient` + `WithChatTimeout` + `Connect/Chat/Close`；同时为门禁提供 `swarm/acp` 真实调用 |

映射关系：Python 7 例中 5 例直接映射（case 1/2/3/4/7）；case 5/6 的 CLI 冒烟成功/失败语义由工具入口的 #1/#2/#7 等价覆盖；Go 增强 4 例（#4/#9/#10 与 #8 的真链路化）。

### 6.2 入站（`tests/integration/cmd/acp_stdio_test.go`，5 例）

| # | 用例 | 来源 | 断言要点 |
|---|---|---|---|
| 1 | `TestAcpStdioBinary_PromptRoundTrip` | Python 入站 case 1 | `initialize→session/new→session/prompt`；stdout 出现 `session/update` chunk 与 `result{stopReason:end_turn}`；chunk 文本 = MockLLM 固定回复 |
| 2 | `TestAcpStdioBinary_错误终帧` | Python 入站 case 2 | 上游失败 → stdout 出现 `error` 帧。**实施第一步先探测 Go 真实可观测行为**，用例以语义命名；与 Python 不一致时按 Python 语义补实现后再断言 |
| 3 | `TestAcpStdioBinary_stdinEOF优雅退出` | Go 增强 | 关 stdin → 5s 宽限内退出码 0 |
| 4 | `TestAcpStdioBinary_stdout纯协议帧` | Go 增强 | 子进程往 stderr 打日志时 stdout 每行仍为合法 JSON（验 `os.Stdout = os.Stderr` + `acpStdout` 早捕获） |
| 5 | `TestAcpStdioBinary_远端不可达` | Go 增强 | `--gateway-url ws://127.0.0.1:1/acp` → 非零退出且 stderr 有可诊断信息 |

## 7. 错误处理与稳定性

| 项 | 措施 |
|---|---|
| 子进程回收 | `t.Cleanup` 顺序：`Close()` → 仍存活 `SIGTERM` → `SIGKILL`；每条路径都 `Wait()` 收尸（防僵尸） |
| 超时 | 握手 10s、prompt 30s、单用例 `context.WithTimeout`；聚合读取用轮询 + deadline，不用固定 `Sleep` |
| 并发 | 不使用 `t.Parallel`（`t.Setenv` 与 acpchat 全局缓存互斥） |
| 跨平台 | 假对端由 shell 改为 Go 程序后，不再需要 `runtime.GOOS == "windows"` skip |
| 唯一接受的残留 | acpchat 无导出的缓存清理 API，出站用例产生的 peer 子进程活到测试二进制退出；父进程退出时 stdin 关闭 → peer EOF 自然退出。与 Python 进程级 `_clients` 缓存同语义，写进 `doc.go` |
| 编译失败降级 | `TestMain` 编译 fakeacp 失败 → 路径置空 → 用例 `t.Skip`（无 go 工具链环境不误红） |

## 8. 验收标准

1. `go test -tags=integration ./tests/integration/swarm/acp/... ./tests/integration/cmd/...` 全绿
2. `go run ./cmd/integration-audit -verify` 通过（出站目录调用跨度 = 3，不落灰区、不需豁免）
3. `gofmt -l` 干净、`go vet -tags=integration` 干净、staticcheck 无新增告警
4. 新包 `doc.go` 同步（规则 1）；默认无生产代码变更（D8），若 R1 触发则补齐后重跑前三项
5. 推送远端后 Build / Lint / Unit Test / Integration Test 四 job 全绿（规则 6）

## 9. 风险与注意事项

| # | 风险 | 处置 |
|---|---|---|
| R1 | 入站 case 2 的 Go 真实行为未知（上游错误可能不产生 `error` 帧，而是 result+stopReason） | 实施第一步先写探测用例打印真实帧，再定断言；若需补实现，单独提交并在本设计回填 |
| R2 | 父进程 7 件套与子进程 WS 的连接时序（子进程可能在 GatewayServer 就绪前连） | 父进程先 `Start` 再 spawn 子进程；子进程失败重试由用例轮询 stdin 写入后的首帧超时兜底 |
| R3 | `UAPCLAW_DATA_DIR` 与父进程共享同一临时目录会造成 config 竞争 | 父子进程各用独立临时目录（父进程的 `models.defaults` 只在父进程内存注册，子进程只需最小 config） |
| R4 | 端口占用导致 GatewayServer 启动失败 | `pickFreePort()` 后立即 `Close` 再绑定，失败重试 3 次 |
| R5 | 出站用例受 acpchat 全局锁表影响，profile 名冲突会串台 | 唯一 profile 名（含纳秒时间戳） |
| R6 | CI 机器无网络时 `go build` 仍可用（依赖已缓存） | 与既有 `TestMain` 预编译先例一致，失败即 Skip |
