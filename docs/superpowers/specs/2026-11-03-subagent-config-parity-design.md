# Subagent 配置链路 parity 修复设计

- 日期：2026-11-03
- 状态：已评审，待实现
- 范围：仅后端（Go `internal/swarm/server`），不含 Web 前端
- Python 参考：`jiuwenswarm`（`/home/opensource/jiuwenswarm-develop`）

## 1. 背景

Go 已经支持通过 `config.yaml` 的 `react.subagents.<name>.enabled` 开启 subagent，机制与 Python 一致。本次不新增总开关，只修复已确认的装配与配置写回偏差。

已确认的偏差：

| # | 偏差 | Go 位置 | Python 参考 |
|---|---|---|---|
| 1 | code 模式未装配 `browser_agent` | `internal/swarm/server/adapter/code_adapter.go:866-871`（TODO） | `interface_code.py:731-760` |
| 2 | code 模式 `code_agent.max_iterations` 硬编码 15 | `internal/swarm/server/adapter/code_adapter.go:838` | `interface_code.py:709-722` |
| 3 | agents 写操作后未同步重载 | `internal/swarm/server/handle_agents.go:270`、`:396`、`:488` | `agent_ws_server.py:3914`、`:3971`、`:4015`、`:4066` |
| 4 | `agents.update` 响应缺 `applied`/`reload_error` | `internal/swarm/server/handle_agents.go:288-355` | `agent_ws_server.py:3968-3980` |
| 5 | `AgentManager.ReloadAgentsConfig` 吞掉单 agent 重载错误 | `internal/swarm/server/runtime/agent_manager.go:396-401` | `agent_manager.py:308-340` |

有意保留的 Go 额外能力：code 模式 `research_agent`。Python `interface_code.py:659-757` 的 `_build_configured_subagents` 只装配 explore/plan/code/browser，未装配 research；Go 的 `code_adapter.go:866-873` 保留该分支，本次加注释与测试锁定，避免后续 parity 检查误删。

## 2. 目标与非目标

### 2.1 目标

1. code 模式在浏览器 runtime 开启时装配 `browser_agent`，参数回退规则与 Deep 模式一致。
2. code 模式 `code_agent` 读取 `code_agent.max_iterations`，缺省回退 `react.max_iterations`，再回退 15。
3. `agents.create/update/delete/enable/disable` 五个写操作（对应四个处理函数）在配置写成功后同步重载 Agent，失败时返回真实的 `applied`/`reload_error`。
4. `AgentManager.ReloadAgentsConfig` 遇到单个 agent 重载失败时立即返回错误，与 Python 的异常传播一致。
5. 用单元测试覆盖新增分支和五个 handler 的链路。

### 2.2 非目标

- 不新增 Web Agents 管理面板。`agents.*` RPC 已存在，前端改动另开任务。
- 不修改自定义 agent 的 CRUD 协议与 Markdown frontmatter 格式。
- 不把 `reload_error` 的空值从 `""` 改成 Python 的 `null`，保持现有 wire 约定。
- 不移除 code 模式 `research_agent`。
- 不接线 MCP 配置写回后的重载（Python `agent_ws_server.py:2674-2753` 有，但不在本次 subagent 范围）。

## 3. 设计

### 3.1 browser 子代理共享构建

在 `internal/swarm/server/adapter/deep_adapter_config.go` 新增：

```go
// buildBrowserSubagentSpec 构建 browser 子代理规格。
// Python: interface_deep.py:912-948 / interface_code.py:731-760
//
// 调用方需先确认 browser runtime 已启用；本函数负责：
//   1. BROWSER_DRIVER 缺省注入 managed；
//   2. BROWSER_MANAGED_BINARY 从 browser.chrome_path 解析；
//   3. max_iterations 两级回退（browser_agent.max_iterations → react.max_iterations → 15）；
//   4. 调用 BuildBrowserAgentConfig。
func (d *DeepAdapter) buildBrowserSubagentSpec(
	model *llm.Model,
	config map[string]any,
	configBase map[string]any,
	browserAgentCfg map[string]any,
) *hschema.SubAgentConfig
```

行为与现有 DeepAdapter browser 分支逐行一致：环境变量已存在时不覆盖；`BROWSER_MANAGED_BINARY` 解析不到时保持空；`BuildBrowserAgentConfig` 返回 nil 时返回 nil。

`DeepAdapter.buildConfiguredSubagents` 的 browser 分支改为调用该 helper，`else if` 的 skip 日志保留。`CodeAdapter.buildConfiguredSubagents` 在 code_agent 之后、research_agent 之前调用同一 helper，并在未启用 runtime 且配置段非空时输出同样的 skip 日志。

### 3.2 code_agent 迭代上限

`CodeAdapter.buildConfiguredSubagents` 顶部已有：

```go
defaultMaxIterations := parseIntAny(config["max_iterations"], 15)
```

新增：

```go
codeAgentCfg, _ := subagentsCfg["code_agent"].(map[string]any)
```

`code_agent` 的 `MaxIterations` 改为：

```go
MaxIterations: parseIntAny(codeAgentCfg["max_iterations"], defaultMaxIterations),
```

对齐 Python `interface_code.py:718-722` 的 `parse_int(code_agent_cfg.get("max_iterations"), react_cfg.get("max_iterations", 15))`。

### 3.3 AgentManager 错误传播

`ReloadAgentsConfig` 的单 agent 循环由：

```go
if err := entry.agent.ReloadAgentConfig(configPayload, envAny); err != nil {
    logger.Warn(...).Msg("[AgentManager] agent reload_agent_config failed")
}
```

改为：

```go
if err := entry.agent.ReloadAgentConfig(configPayload, envAny); err != nil {
    logger.Warn(logComponent).
        Err(err).
        Str("channel_id", chKey).
        Str("cache_key", entry.cacheKey).
        Msg("[AgentManager] agent reload_agent_config failed")
    return fmt.Errorf("channel %s cache %s reload agent config failed: %w", chKey, entry.cacheKey, err)
}
```

语义：

- 已成功重载的 channel 不回滚。
- 失败 channel 的 team evolution 更新跳过。
- 后续 channel 不再重载。
- 与 `agent_manager.py:308-340` 的异常传播一致。

调用方影响：

| 调用方 | 现行为 | 变更后 |
|---|---|---|
| `handle_command.go:409`（`/model`） | 忽略 error，仅 debug | 不变（仍忽略） |
| `handle_config.go:59`（`agent.reload_config`） | error 时返回 `ok=false` | 单 agent 失败时返回 `ok=false`，与 Python 一致 |
| 新增的 agents 五个 handler | 不存在 | 失败时 `applied=false` |

### 3.4 agents handler 同步重载

在 `internal/swarm/server/handle_agents.go` 新增：

```go
// reloadAgentsConfig 同步重载所有 Agent 配置。
// Python: await self._agent_manager.reload_agents_config(get_config(), None)
func (s *AgentServer) reloadAgentsConfig(ctx context.Context) error {
	if s.agentManager == nil {
		return fmt.Errorf("agentManager 未初始化")
	}
	if s.config == nil {
		return fmt.Errorf("配置未初始化")
	}
	configData, err := s.config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	return s.agentManager.ReloadAgentsConfig(ctx, configData, nil)
}
```

调用规则：

| handler | 调用时机 | 写配置失败时 | 重载失败时 |
|---|---|---|---|
| `handleAgentsCreate` | `UpsertSubagentInConfig` 成功后 | `applied=false`，不重载 | `applied=false`，`reload_error` 为 error |
| `handleAgentsUpdate` | `UpdateAgent` 成功后 | 不适用 | 同上 |
| `handleAgentsDelete` | `RemoveSubagentFromConfig` 返回无 error 后 | `applied=false`，不重载 | 同上 |
| `handleAgentsEnable/Disable` | `UpsertSubagentInConfig` 成功后 | `applied=false`，不重载 | 同上 |

`handleAgentsUpdate` 的 payload 增加：

```go
"applied":      applied,
"reload_error": reloadError,
```

`handleAgentsEnable`、`handleAgentsDisable`、`handleAgentsDelete` 的 `_ context.Context` 改为具名 `ctx`；`handleAgentsSetEnabled` 增加 `ctx context.Context` 参数并在两个入口透传。

所有重载失败都记录一条 Warn 日志，字段包含 `agent_name` 与操作名，对齐 Python `logger.warning("... reload failed: %s", exc)`。

### 3.5 code 模式 research_agent 保留

`code_adapter.go:866-873` 的分支保留，补充注释：

```go
// 有意的 Go 额外能力：Python code 模式（interface_code.py:659-757）不注册 research_agent。
```

测试中增加一条断言，锁定 code 模式在 `research_agent.enabled: true` 时仍会产出 research spec，防止后续 parity 清理误删。

## 4. 错误语义汇总

1. 配置写失败：`applied=false`，`reload_error` 为写配置错误，重载不执行。
2. `s.config.Load()` 失败：`applied=false`，`reload_error` 为加载错误。
3. 单 agent 重载失败：`applied=false`，`reload_error` 为该错误；接口仍返回 `ok=true`。
4. `agents.update` 不写 `react.subagents`，只重载；响应字段与其余四个 handler 同形。
5. `reload_error` 成功时保持空字符串 `""`。

## 5. 测试计划

### 5.1 adapter 单元测试

文件：`internal/swarm/server/adapter/code_adapter_test.go`、`deep_adapter_subagents_test.go`

1. browser runtime 开启 + `browser_agent.max_iterations: 7` + `react.max_iterations: 20`：产出 `browser_agent`，`MaxIterations=7`。
2. browser runtime 未开启但配置了 browser 段：不产出 browser spec。
3. browser runtime 开启且 `BROWSER_DRIVER` 为空：注入 `managed`；`BROWSER_MANAGED_BINARY` 为空且 `browser.chrome_path` 有值：注入该路径。
4. `code_agent.enabled: true` + `max_iterations: 7`：`MaxIterations=7`。
5. `code_agent.enabled: true` 且无子段 `max_iterations`：回退 `react.max_iterations`。
6. code 模式 `research_agent.enabled: true`：仍产出 research spec（有意偏差锁定）。

测试需用 `t.Setenv` 控制 `PLAYWRIGHT_RUNTIME_MCP_ENABLED`、`BROWSER_DRIVER`、`BROWSER_MANAGED_BINARY`，不使用 `t.Parallel`。

### 5.2 AgentManager 单元测试

文件：`internal/swarm/server/runtime/agent_manager_test.go`

用嵌入 `adapter.AgentAdapter` 的 fake（仅实现 `ReloadAgentConfig` 返回 error）构造 `UapClaw`，写入 `am.agents`，断言：

- `ReloadAgentsConfig` 返回该 error；
- error 文本包含 channel 与 cache_key。

### 5.3 handler 单元测试

文件：`internal/swarm/server/handle_agents_test.go`

隔离方式：`t.Setenv("UAPCLAW_DATA_DIR", tmpDir)` + `pathutil.ResetCache()`，`t.Cleanup(pathutil.ResetCache)`；在临时目录创建 `config/` 后构造 server。

覆盖：

1. `agents.create` → `agents.update` → `agents.disable` → `agents.delete` 全链路，每步 `applied=true`、`reload_error=""`。
2. `agents.update` 响应包含 `applied` 与 `reload_error`。
3. `s.agentManager=nil` 时调用 enable：`applied=false`，`reload_error` 非空。
4. `s.config=nil` 时直接调用 `reloadAgentsConfig`：返回错误。

### 5.4 配置模板

`resources/config.yaml` 的 `react.subagents` 段只补注释：

- code 模式 `explore_agent`/`plan_agent` 恒挂载，`enabled` 不生效，仅 `max_iterations` 生效。
- `browser_agent` 由 `PLAYWRIGHT_RUNTIME_MCP_ENABLED`/`BROWSER_RUNTIME_MCP_ENABLED` 控制，本段只覆盖参数。

不改默认值，不新增键。

## 6. 验收标准

1. 上述测试全部通过。
2. 改动文件的行覆盖率 ≥ 85%。
3. `gofmt`、`go vet`、`staticcheck` 无新增问题。
4. code 模式在 runtime 开启时 `task_tool` 能看到 `browser_agent`；关闭时不出现。
5. `agents.*` 五个写操作在重载失败时返回 `applied=false` 与真实 `reload_error`。
