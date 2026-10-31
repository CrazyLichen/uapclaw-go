# 10.4.6 ACP CLI Smoke 入口（uapclaw acp-chat）设计

- 对应 Python：`jiuwenswarm/acp/cli.py`（90 行）+ `pyproject.toml:79`（console script `jiuwenswarm-acp-chat`）
- 测试基线：Python 侧**无** cli 单测（`tests/unit_tests/acp/` 仅 `test_stdio_client_helpers.py` / `test_subprocess_env.py`）；Go 侧自建（规范要求 ≥85%）
- 依赖现状：`internal/swarm/acp`（AcpStdioClient，10.6.24d + 10.4.3）、`acpchat`（profile 解析，10.6.24d）、`internal/common/config` 均已就绪
- 日期：2026-10-31

## 1. 背景与定位

10.4.6 是「外部 ACP 子代理」这条**出口链**的旁路冒烟入口，不进入主会话链路，不产生 E2A 消息、不写 session JSONL、不注册工具卡。

流程位置：

```
主链路（10.4.6 不在此）
User → Channel(REPL 10.4.1 / stdio 10.4.3 / HTTP 10.4.2 / IM) 
     → Gateway(11.9 / 11.25 桥) → E2A(10.2) → AgentServer(10.3.1)
     → AgentManager → Deep/Code Adapter(10.3.4-6) → LLM 循环
     → 工具调用 acp_chat(10.6.24d) ─────────────┐
                                                 │
                        AcpStdioClient(swarm/acp) │ spawn 外部 ACP 子进程
                                                 │ (Codex CLI / Gemini CLI …)
                                                 └──→ stdio JSON-RPC 2.0 ──→ 本进程边界之外

10.4.6 旁路（跳过 Gateway / AgentServer / LLM / 工具调度）
uapclaw acp-chat <profile> [message]
     → config.yaml 的 acp_agents.<profile> → AcpStdioClient
     → Connect(initialize + session/new) → Chat 一次 → 打印 → Close
```

作用三条：

1. **配置冒烟**：单独验证 `config.yaml` 的 `acp_agents.<profile>`（`command/args/cwd/env`）能否 spawn 并完成 ACP 握手与一轮 prompt。
2. **链路隔离诊断**：外部 ACP agent 升级换协议时，跳过 adapter + 权限确认桥（10.6.24d 的四态映射），只看 stdio 段裸行为。
3. **对照基线**：同一 profile 走「工具路径」与「冒烟入口」应得一致输出，可作为 `acp_chat` 工具路径的回归对照。

**PLAN 原文订正**：当前 PLAN 写作「jiuwenswarm-acp-chat 对应（cli.py 90 行 REPL）」，实际 `cli.py` 是**一次性冒烟**（`connect → chat 一次 → close`），无 REPL；10.4.3 行「剩 cli.py REPL 入口 ⤵️ 10.4.6」同源误读。两处均在回填时订正。

## 2. 拍板决策

| # | 决策 | 内容 |
|---|---|---|
| D1 | 实施范围 | **单独做 10.4.6**，不与 10.4.1（CLI REPL）/10.4.2（HTTP API）合并；一节收口并关闭 10.4.3 的悬空回填点 |
| D2 | profile 解析 | **复用 `acpchat` 严格解析**：导出 `LoadProfile` / `ClientSpec` / `BuildClientSpec` 供 cmd 层调用；配置写错时冒烟入口比 Python 更严（报错 → 码 2），归档为有意偏差 |
| D3 | 自举链 | **轻量 + logger.Setup**：不加载 extensions、不初始化 crypto、不 prepare workspace；只做 `logger.Setup(WithConfigFile)` → `dotenv(非覆盖)` → `config.New/Load` |
| D4 | 退出码 | **typed error + `exitCodeFor`**：cmd 层定义带码 error，`Execute()` 用 `exitCodeFor(err)` 决定 `os.Exit`；同时 `SilenceUsage`/`SilenceErrors` |
| D5 | 输出通道 | **响应文本直出 stdout + 诊断走 logger**：`fmt.Fprintln(os.Stdout, out)`；其余 INFO/WARN 走 `logger` 体系 |

## 3. Python 事实基线（逐行核对）

| 维度 | Python 事实（`acp/cli.py`） |
|---|---|
| 入口形态 | 独立 console script `jiuwenswarm-acp-chat = jiuwenswarm.acp.cli:main`（非 `jiuwenswarm xxx` 子命令） |
| 参数 | `agent`（位置必填）、`message`（位置，`nargs="?"`，默认 `"Hello"`）；argparse usage 错误 → 退出码 2 |
| `_get_spec` | `acp_agents` 非 dict → `"[ERROR] config has no acp_agents."` + 返回 None；`agents.get(agent.strip())` 非 dict → `"[ERROR] unknown acp_agents profile %r."` + None |
| `command` | `str(spec.get("command") or "").strip()`；空 → `"[ERROR] profile needs command."` + 码 2 |
| `args` | `isinstance(raw_args, list)` 才 `[str(x) ...]`，否则**静默 `[]`** |
| `cwd` | 仅 `isinstance(str)` 且 `strip()` 非空才用，否则 **静默 None** |
| `env` | `spec.get("env") if isinstance(dict) else None`，否则 **静默 None** |
| 客户端 | `AcpStdioClient(command, args, cwd=cwd_s, env=env)` → `connect()` → `chat(message)`（`chat` 内部默认超时 `ACP_CHAT_TIMEOUT=600`） |
| 输出 | `logging.basicConfig(level=INFO, format="%(message)s", stream=sys.stdout)`；`logger.info("%s", out or "(empty response)")` |
| 关闭 | `finally` 内 `asyncio.wait_for(asyncio.shield(client.close()), ACP_CLI_CLOSE_TIMEOUT_S=30)`，异常仅 `logger.warning("Failed to close ACP client: %s")` |
| 退出码 | 0 成功 / 2 配置错 / 1 会话异常 / 130 `KeyboardInterrupt` |
| dotenv | `JIUWENCLAW_SKIP_DOTENV.strip() != "1"` 时 `load_dotenv(get_env_file(), override=False)`；异常仅 `debug("Skipping dotenv load")` |
| 配置读取 | `get_config()` = 读 `config.yaml` + `${VAR:-default}` 展开 + `_normalize_config`；**不加载 extensions、不建 crypto provider、不 prepare workspace** |

## 4. 实现方案

### 4.1 文件清单

| 动作 | 文件 | 说明 |
|---|---|---|
| 新增 | `cmd/uapclaw/acp_chat_cmd.go` | 子命令本体（约 230 行） |
| 新增 | `cmd/uapclaw/acp_chat_cmd_test.go` | 单测 + 假对端 e2e |
| 改 | `cmd/uapclaw/cmd.go` | `newAcpChatCmd()` 注册 + import |
| 改 | `cmd/uapclaw/main.go` | `Execute()` 改用 `exitCodeFor`；包注释子命令清单补 `acp-chat` |
| 改 | `cmd/uapclaw/doc.go` | 文件目录 + 子命令说明 |
| 改 | `internal/swarm/agents/harness/common/tools/acp_chat/client_cache.go` | 导出 `LoadProfile` / `ClientSpec` / `BuildClientSpec` |
| 改 | `acpchat/tool_test.go` | 字段改名（`cs.command` → `cs.Command` 等） |
| 改 | `acpchat/doc.go` | 说明新增导出面 |
| 改 | `IMPLEMENTATION_PLAN.md` | 10.4.6 ✅ + 10.4.3 ⤵️ 回填（见 §8） |

### 4.2 命令形态

```
uapclaw acp-chat <profile> [message]
```

- `profile` 必填，`message` 缺省 `"Hello"`；无旗标。
- 参数个数在 `RunE` 入口手写校验：`len(args) == 0` 或 `len(args) > 2` → usage 打 stderr → 码 2（对齐 argparse）。
- `SilenceUsage` + `SilenceErrors`，避免 cobra 打 usage/`Error:` 前缀。

### 4.3 执行流

```
1. loadAcpChatConfig()
     logger.Setup(logger.WithConfigFile())          // 日志进体系；不加载 extensions
     if getenv("UAPCLAW_SKIP_DOTENV").strip != "1":
         dotenv.LoadNonOverride(workspace.EnvFile()) // 失败仅 Debug
     cfg := config.New("", config.WithNormalize(config.NormalizeConfig),
                           config.WithDecrypt(crypto.NewDecryptFunc()))
     cfg.Load()                                     // 缺文件返回空，不报错
     return cfg.Raw()
2. LoadProfile(raw, profile)        错 → Error 日志 + 码 2
3. BuildClientSpec(spec)            错 → Error 日志 + 码 2
4. acp.NewAcpStdioClient(...) + Connect(ctx)   错 → Error 日志 + 码 1
5. Chat(ctx, message)               错 → Error 日志 + 码 1
6. fmt.Fprintln(stdout, out | "(empty response)")      → 码 0
7. 收尾 closeAcpChatClientWithTimeout(client, ACP_CLI_CLOSE_TIMEOUT_S=30)
   签名收窄为 interface{ Close() error }（便于测试注入阻塞桩），
   超时 → Warn 日志，不改退出码
8. ctx 因信号取消（ctx.Err() != nil）→ 码 130
```

第 7 步必须在 defer 中执行（对应 Python `finally`），与退出码解耦。

### 4.4 可测性注入

```go
// acpChatDeps 冒烟入口依赖注入点（生产用 defaultAcpChatDeps，测试替换）
type acpChatDeps struct {
    loadConfig func(context.Context) (map[string]any, error)
    connect    func(context.Context, *acp.AcpStdioClient) error
    chat       func(context.Context, *acp.AcpStdioClient, string) (string, error)
    stdout     io.Writer
    getenv     func(string) string
}
```

`runAcpChatCmd`（`RunE` 目标）只做 wiring；`runAcpChat(ctx, deps, profile, message) int` 是单测主体，纯返回退出码，不上抛语义错误。

### 4.5 导出面变更（acpchat）

```go
// ClientSpec 子进程启动规格（原 clientSpec 导出，字段一并导出）
type ClientSpec struct {
    Command string
    Args    []string
    Env     map[string]any
    Cwd     string
}

// LoadProfile 从 config base 映射读取 acp_agents.<profile>
func LoadProfile(configBase map[string]any, profile string) (map[string]any, error)

// BuildClientSpec 解析并校验子进程启动规格
func BuildClientSpec(spec map[string]any) (*ClientSpec, error)
```

实现方式：**直接改名**（不保留包内小写薄包装）——`clientSpec` → `ClientSpec`（字段同步导出）、`loadProfile` → `LoadProfile`、`buildClientSpec` → `BuildClientSpec`；`getOrCreateClient` 与 `tool_test.go` 调用点同步更新。同包内调用者仅这两处，无需兼容层。**行为零变更**（10.6.24d 已收口代码仅换名与可见性）。

## 5. 退出码与输出

### 5.1 退出码

```go
// acpChatExitError 带进程退出码的语义错误
type acpChatExitError struct{ code int }

func (e *acpChatExitError) Error() string

// exitCodeFor 从 error 解析进程退出码（0/1/2/130，未知 → 1）
func exitCodeFor(err error) int

// Execute（main.go）
func Execute() {
    if err := newRootCmd().Execute(); err != nil {
        os.Exit(exitCodeFor(err))
    }
}
```

映射：成功 nil → 0；`*acpChatExitError` → 其 code；其他 → 1。130 由 `runAcpChat` 显式返回：

- `connect`/`chat` 阶段 `ctx.Err() != nil` → 130；`ctx.Err() == nil` → 1
- **close 阶段被信号打断** → 130（2026-10-31 修订）：正常路径改为显式 `close` 后再复查 `ctx`，defer 仅作提前 return 的兜底；响应文本已打印不回退

### 5.2 输出通道

- **响应文本**：`fmt.Fprintln(stdout, ...)`，无时间戳/字段前缀 → 可被管道/脚本直接消费
- **诊断日志**：`logger.Info/Warn/Error(logComponent)` → **stderr**（2026-10-31 修订：logger 控制台 writer 由 `os.Stdout` 改为 `os.Stderr`，对齐 Python `setup_logger` 的 `logging.StreamHandler()` 默认流；Python `cli.py` 的 `basicConfig(stream=sys.stdout)` 属该脚本特例，Go 侧不跟随）
- **日志落盘**：`logger.Setup` 默认目录由硬编码 `~/.uapclaw/agent/.logs` 改为 `pathutil.LogsDir()`（= `WorkspaceDir()/agent/.logs`），受 `UAPCLAW_DATA_DIR` 管辖（2026-10-31 修订）
- **空响应占位**：逐字 `(empty response)`（对齐 `cli.py`，**不是**工具侧的 `(empty response from external ACP agent)`）
- **`--dotenv`/`--name`**：`acp-chat` 挂 `makeDotenvPreRunE()`，root 持久旗标生效（Go 侧扩展，Python 无对应旗标）

## 6. 保真 / 偏差表

| 项 | Python | Go 方案 | 性质 |
|---|---|---|---|
| args/env 容忍度 | 非 list/非 dict → 静默 `[]`/None | 复用 acpchat 严格解析 → 报错 + 码 2 | **有意偏差（D2）** |
| 错误文案 | `[ERROR] …` 前缀 | 英文 Msg 无前缀（日志规范） | 有意偏差 |
| dotenv override | `override=False` | 新增 `dotenv.LoadNonOverride`（保真） | 保真 |
| extensions / crypto / workspace | 不加载 | 不加载（logger 除外，见 D3） | 保真（logger 因 Go 未 Setup 即静默而必需） |
| close 外层 30s | `wait_for(shield(close()))` | goroutine + select 30s | 保真 |
| 空响应占位 | `(empty response)` | 逐字同 | 保真 |
| Ctrl-C | `KeyboardInterrupt` → 130 | `signal.NotifyContext` 取消 → 130 | 保真 |
| 跳过 dotenv 开关 | `JIUWENCLAW_SKIP_DOTENV` | `UAPCLAW_SKIP_DOTENV` | 品牌映射偏差（`UAPCLAW_` 前缀惯例） |
| 命令名 | `jiuwenswarm-acp-chat` | `uapclaw acp-chat` | 品牌映射 |
| 关闭超时非法值 | `float()` 解析失败抛 ValueError | 回落默认 30s 并 Warn | 防御性偏差 |
| close 错误文案 | `Failed to close ACP client: …` | 英文 Msg（日志规范） | 有意偏差 |
| 日志流 | `basicConfig(stream=sys.stdout)`，全部日志进 stdout | stdout 仅响应文本，日志走 stderr（logger 全局修复） | **有意偏差（用户拍板）** |
| 日志落盘 | 不落盘 | 写 `WorkspaceDir()/agent/.logs`（受 `UAPCLAW_DATA_DIR`） | 新增行为（Go 日志体系） |
| `--dotenv`/`--name` | 无该旗标 | `PreRunE` 解析，支持多实例/指定 .env | Go 扩展 |
| close 阶段中断 | `KeyboardInterrupt` → 130 | close 后复查 ctx → 130 | 保真（时序显式化） |
| 测试 | 无 cli 单测 | 自建 ≥85% | 新增 |

## 7. 测试计划（cmd 包 ≥85%）

| 用例 | 断言 |
|---|---|
| `TestRunAcpChat_配置缺失` | `loadConfig` 返回无 `acp_agents` → 码 2 |
| `TestRunAcpChat_未知profile` | → 码 2 |
| `TestRunAcpChat_command为空` | → 码 2 |
| `TestRunAcpChat_连接失败` | `connect` 返回 err → 码 1 |
| `TestRunAcpChat_对话失败` | `chat` 返回 err → 码 1 |
| `TestRunAcpChat_成功` | 码 0，stdout 含回复文本 |
| `TestRunAcpChat_空响应` | stdout 为 `(empty response)` |
| `TestCloseAcpChatClientWithTimeout_超时` | 假客户端阻塞超过超时 → 仅 Warn，不 panic 不改码 |
| `TestRunAcpChat_信号取消` | `ctx.Err() != nil` → 码 130 |
| `TestRunAcpChat_参数个数` | 0 个 / 3 个 → 码 2 |
| `TestExitCodeFor` | 0/1/2/130/未知 全分支 |
| `TestLoadAcpChatConfig` | `UAPCLAW_DATA_DIR=t.TempDir()` + 临时 config.yaml → 读到 `acp_agents` |
| `TestAcpChatE2E_假对端` | 注入真实 raw map + `acp/testdata/acp_peer.sh`，stdout 含 `peer-reply` |

假对端路径：`cmd/uapclaw` → `../../internal/swarm/acp/testdata/acp_peer.sh`（`sh` 启动，`acp_chat` 包测试已有同类用法）。

## 8. 回填清单（实施逐项打勾）

- [x] `IMPLEMENTATION_PLAN.md` 10.4.6 行：`☐` → `✅`，描述改为「一次性冒烟（connect→chat→close，message 缺省 Hello），落 `cmd/uapclaw/acp_chat_cmd.go`；严格解析对齐 acpchat，偏差入 spec §6」
- [x] `IMPLEMENTATION_PLAN.md` 10.4.3 行：删除「剩 cli.py REPL 入口 ⤵️ 10.4.6」，改为「cli.py 一次性冒烟入口已随 10.4.6 落地」
- [x] 10.4.1 行不动（CLI REPL 属另一节）
- [x] `cmd/uapclaw/doc.go` 文件目录 + `main.go` 包注释子命令清单补 `acp-chat`
- [x] `acpchat/doc.go` 说明导出面（`LoadProfile` / `ClientSpec` / `BuildClientSpec`）
- [x] 本 spec §6 偏差表随实施核对一遍（实施后发现新偏差继续追加）

## 9. 风险与注意事项

- 工作区存在他人在途改动（35 文件 / +867-290）与并发 codebuddy 进程：**全程显式 `git add` 指定文件，禁用 `git commit -a`**。
- `acpchat` 导出面变更触及 10.6.24d 已收口代码：只改可见性与命名，不改逻辑；改完跑 `go test ./internal/swarm/agents/harness/common/tools/acp_chat/...` 确认零回归。
- 冒烟入口不写日志文件之外的状态；`logger.Setup` 会建日志目录，测试需 `UAPCLAW_DATA_DIR` 重定向到 `t.TempDir()`。
- `config.WithDecrypt(crypto.NewDecryptFunc())` 在无 provider 时降级明文（已核实），与 Python「无 extensions 即不解密」等价。
