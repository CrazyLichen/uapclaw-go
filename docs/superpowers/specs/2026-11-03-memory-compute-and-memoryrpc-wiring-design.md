# memory.compute 真值实现 + memoryrpc 接线 设计文档

## 概述

界面「内存占用」恒为 0（WS 收到 `memory.compute` 响应全是 0）。根因是网关把它注册成了写死的桩，且字段名与前端读取不一致。本设计同时把已实现但零调用的 `memoryrpc` 五方法接进 `/tui` 路由，两者都严格按 Python 分层复刻。

**问题**：

1. `internal/swarm/gateway/channel_manager/web/web_handlers.go:317` 把 `memory.compute` 注册为 `stubHandler(..., {"rss": 0, "total": 0})`；前端 `ToolPanel/index.tsx:116-128` 只读 `rss_mb` / `used_percent`，三个值全部拿不到。
2. `internal/swarm/agents/harness/common/memoryrpc` 的 5 个 handler（list/edit/status/toggle/open）全仓无生产调用方，属死代码；`IMPLEMENTATION_PLAN.md` 11.15 行留有「⤴️ 需回填来源：10.6.18 memory_rpc 5 个 local handler 注册进 TUI 记忆管理面板」。

**目标**：

- `memory.compute` 返回真实进程 RSS 与系统内存数据。
- `memory.list/edit/status/toggle/open` 在 `/tui` 路由以本地 handler 形式可用，业务逻辑复用 `memoryrpc`。

**非目标**：

- 不给 web 通道注册 memory.* 五方法（Python 的 web 通道也没有；Go web 前端不调用它们）。
- 不动 `memory.forbidden.get/set`、不动 `memoryrpc` 内部业务逻辑。
- 不新增抽象包（不抽 `memoryapi`）。

---

## 一、事实基线（Python 对照）

| 关注点 | Python | Go 现状 |
|---|---|---|
| `memory.compute` | `gateway/channel_manager/web/app_web_handlers.py:1414-1427`，`channel.register_method`，psutil 采集 | web stub（`web_handlers.go:317`） |
| `memory.list/edit/status/toggle/open` | `gateway/channel_manager/tui/tui_connect.py:1840-1907`，`channel.register_local_handler` | `/tui` 只注册 5 个 chat/history handler（`tui_connect.go:261-265`） |
| 业务逻辑 | `agents/harness/common/memory_rpc.py` 的 5 个 `handle_memory_*` 纯函数 | `memoryrpc.HandleMemory*`（已实现，签名与 Python 一一对应：list/status/toggle 带 mode，edit/open 不带） |
| 错误协议 | handler 抛异常 → wrapper `except` → `send_response(ok=False, error=str(exc), code="INTERNAL_ERROR")` | handler 用 `result["error"]` 内嵌（`memoryrpc/doc.go:8-12`），注册层需转 `ok=false` |

`/tui` 的本地 handler 形态：`func(ws, reqID, params, sessionID) error` 且**自己发帧**（`tui_connect.go:276-284`）。`SendResFrame` 恒带 `payload` 键（`res_frame.go:20-22`），与 Python `payload or {}` 一致。

---

## 二、组件划分、数据流与契约

### 2.1 文件清单

| 动作 | 文件 | 内容 |
|---|---|---|
| 新增 | `internal/swarm/gateway/channel_manager/web/memory_compute.go` | `handleMemoryCompute()` + `collectMemory` 注入点 + 换算 |
| 修改 | `internal/swarm/gateway/channel_manager/web/web_handlers.go:317` | stub → `d.Register("memory.compute", handleMemoryCompute())` |
| 新增 | `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers.go` | 5 个 wrapper + workspace/mode/project_dir 解析 + 错误帧 |
| 修改 | `internal/swarm/gateway/channel_manager/tui/tui_connect.go:261-265` | `Install` 追加 5 条 `RegisterLocalHandler("/tui", ...)` |
| 修改 | `internal/swarm/gateway/channel_manager/web/doc.go` | 文件目录补 `memory_compute.go` |
| 修改 | `internal/swarm/gateway/channel_manager/tui/doc.go` | 文件目录补 `tui_memory_handlers.go` |
| 修改 | `go.mod` | `github.com/shirou/gopsutil/v3` 由 indirect 提升为直接依赖 |

### 2.2 数据流

**memory.compute（web 通道）**

```
前端 ToolPanel 每 10s webRequest('memory.compute')
  → WS res 请求帧 → WebChannel 读循环（web_connect.go:259）
  → dispatcher.Dispatch("memory.compute")
  → handleMemoryCompute() → {rss_mb, total_mb, available_mb, used_percent}
```

**memory.* 五方法（tui 通道）**

```
WS /tui req 帧 → gateway_server.handleRawMessage（gateway_server.go:719）
  → memory.* 不在 ForwardMethods / ForwardNoLocalHandlerMethods（无需改集合）
  → route.LocalHandlers[method] → wrapper
  → memoryrpc.HandleMemory*()（零改动）
  → SendResFrame(ok, payload)
```

### 2.3 契约

**memory.compute**（全部 `float64`，公式与 Python 一致 `bytes / (1024*1024)`）：

| 键 | 来源 | 说明 |
|---|---|---|
| `rss_mb` | `process.MemoryInfo().RSS` | 本进程物理内存，等价 psutil `memory_info().rss` |
| `total_mb` | `mem.VirtualMemory().Total` | 系统总内存 |
| `available_mb` | `mem.VirtualMemory().Available` | 系统可用内存 |
| `used_percent` | `mem.VirtualMemory().UsedPercent` | **有意偏差**：Python 算了 `mem.percent` 却未放进 payload，此处补为超集 |

**memory.* 五方法**：payload 结构完全沿用 `memoryrpc` 现有返回，不新增/不改名任何键。

### 2.4 wrapper 参数解析（逐项对齐 Python）

1. `workspace = get_agent_workspace_dir()` → `workspace.AgentWorkspaceDir()`（固定 agent 工作区，非会话工作区）
2. `mode = params.get("mode", "plan")` → 取 `params["mode"]`，缺省 `"plan"`
   - Go 的 mode 默认注入（`gateway_server.go:670-684`，tui→`code.normal`）**只发生在 forward 分支**，本地 handler 收不到，因此与 Python 一致。
3. `project_dir` 三级回退：`params["project_dir"]` → `params["trusted_dirs"][0]` → `params["cwd"]`；命中后拷贝 params 副本再写入，禁止就地修改调用方 map（Python 用 `{**params, ...}`）
4. 方法 → handler 映射：`memory.list`/`memory.status`/`memory.toggle` 传 mode，`memory.edit`/`memory.open` 不传

---

## 三、错误处理

| 场景 | Python | Go 设计 |
|---|---|---|
| `memory.compute` 采集失败 | `psutil.Process()` 抛 `psutil.Error`，无 try → 冒到 `register_method` 包装 → `ok=false` + `INTERNAL_ERROR` | `handleMemoryCompute` 返回 error，`web_connect.go:260-267` 已映射 `INTERNAL_ERROR` |
| memory.* 业务失败（`result["error"]` 非空） | handler 抛异常 → wrapper `except` → `send_response(ok=False, error=str(exc), code="INTERNAL_ERROR")` | wrapper 自己发失败帧：`ok=false`、`error=result["error"]`、`code="INTERNAL_ERROR"`、`payload=nil`（帧内为 `{}`） |
| wrapper 内意外 panic | `except Exception` 兜住 | wrapper 内 `defer recover()` → 同一失败帧 |
| 成功 | `send_response(ok=True, payload=result)` | `SendResFrame(..., true, result, "", "")` |

**关键约束**：wrapper 恒 `return nil`。`gateway_server.go:733-748` 对本地 handler 返回的 error 会二次包装成 `"handler error: %s"`，与 Python 文案不符；wrapper 自己发帧可避免该偏差。

---

## 四、可测性注入点

为满足改动文件 85% 行覆盖率门禁，两个分支必须有注入点（均为包内未导出细节，不外扩 API）：

1. web：`var collectMemory = defaultCollectMemory`，签名 `(rssBytes uint64, totalBytes, availBytes uint64, usedPercent float64, err error)`。测试替换为固定字节数（覆盖换算分支）或 error（覆盖错误分支）。
2. tui：wrapper 工厂接收 handler 映射（生产填充 `memoryrpc.Handle*`），测试注入「返回 `{"error": ...}` 的桩」与「直接 panic 的桩」。

---

## 五、测试方案

### 5.1 单元测试

**`web/memory_compute_test.go`**

1. 真值：`rss_mb > 0`、`total_mb > 0`、`available_mb > 0`、`0 <= used_percent <= 100`、`rss_mb <= total_mb`
2. 换算：桩给 1048576 字节 → `rss_mb == 1.0`
3. 采集错误 → 返回 error
4. 真 WS：`httptest` + `NewWebChannel` + `RegisterWebHandlers` + 真实拨号 → 发 `memory.compute` → 帧 `ok=true`，三个 MB 字段为 number 且 > 0，`used_percent` 为 number 且落在 `[0,100]`

**`tui/tui_memory_handlers_test.go`**

5. 5 个方法各一条成功用例（fakeSender 捕获帧，断言 `ok=true` 与 payload 关键键）
6. `project_dir` 三级回退 ×3（`project_dir` / `trusted_dirs[0]` / `cwd`）
7. 调用后入参 map 未被修改
8. mode 缺省时 `status.current_mode == "plan"`
9. `result["error"]` 非空 → `ok=false` + `INTERNAL_ERROR` + `payload` 为空对象
10. panic 桩 → `ok=false`（recover 生效）

### 5.2 集成测试

位置：`tests/integration/swarm/gateway/tui_cli_route_test.go`（既有 suite 内新增方法；该目录已满足规则 7 的跨度与 `suite.Run(` 要求，新增独立文件会缺 `suite.Run(` 触发审计）。

- `TestMemoryStatusFullChain`：在 `agent/workspace/memory/` 造 2 个 md、项目根造 1 个记忆文件 → `/tui` 发 `memory.status{detailed:true}` → 断言 `auto_memory.files_count`、`project_memory.files_count` 与磁盘一致，`enabled`/`storage_mode` 存在，`large_files` 为数组
- `TestMemoryListFullChain`：`memory.list` 返回刚建的文件

不断言 `index` 块：Python 同位置也是条件返回，且依赖索引管理器初始化，属非确定性内容。

`memory.compute` 不进 `tests/integration`：web 通道单方法，链路只跨 web 包 + gopsutil，不满足规则 7 的 ≥3 子系统；真 WS 断言已在 5.1 第 4 条覆盖，强行塞入属于为过审计造跨度。

### 5.3 门禁

- CI 四绿（Build / Lint / Unit / Integration）
- 改动文件行覆盖率 ≥ 85%
- `gofmt -l` 全量无输出、staticcheck 无新增
- `web/doc.go`、`tui/doc.go` 文件目录同步
- `IMPLEMENTATION_PLAN.md`：11.15 行保持 🔄（27 个纯本地 handler 仍有 22 个未补），把「⤴️ 需回填来源：10.6.18 memory_rpc 5 个 local handler 注册进 TUI」标注为已回填；10.6.13-18 行的 `⤵️ TUI 面板注册(11.15)` 同步标注

---

## 六、有意偏差登记

| 偏差 | 内容 | 理由 |
|---|---|---|
| D1 | `memory.compute` 多返回 `used_percent` | Python 计算了 `mem.percent` 但未返回；Go 前端 `ToolPanel` 读该键，返回它属超集，不影响任何现有消费者 |
| D2 | `memory.*` 只在 `/tui` 注册，未注册到 web 通道 | 与 Python 分工一致（Python web 通道只有 `memory.compute`/`memory.forbidden.*`）；Go web 前端不调用这 5 个方法 |
| D3 | `memory.compute` 无集成用例 | 单方法链路跨子系统不足，真 WS 断言放在包内测试 |

---

## 七、验收标准

1. `/web` 上 `memory.compute` 返回 `rss_mb`/`total_mb`/`available_mb`/`used_percent`；三个 MB 字段 > 0，`used_percent` ∈ [0,100]。
2. `/tui` 上 `memory.status{detailed:true}` 返回的 `auto_memory.files_count`、`project_memory.files_count` 与磁盘一致。
3. `/tui` 上 `memory.edit` 对不可创建路径返回 `ok=false` + `INTERNAL_ERROR`，而非 `ok=true`。
4. 改动文件覆盖率 ≥85%，CI 四绿。
