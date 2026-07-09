# 配置热重载链路补齐设计（已完成 ✅）

## 背景

当前 `GatewayServer.OnConfigSaved()` 回调已实现（对齐 Python `_on_config_saved` 5 步逻辑），但缺少调用者。需要打通两条触发路径：
1. API 触发：WebHandler `config.set` / `config.save_all` → `_notify_config_saved_once` → `onConfigSaved`
2. fsnotify 触发：reloader → `onConfigSaved`

同时需要删除 PushInitialConfig（对齐 Python 启动时不发 `agent.reload_config`）。

## 改动清单

### Step 1: 删除 PushInitialConfig 机制

**对齐 Python**：Python 启动时 `set_or_update_server_config` 是空操作，不发 E2A 请求。Go 单体模式下 AgentServer 直接通过 `s.config` 使用配置，不需要 Gateway 主动推送。

**文件**：
- `internal/swarm/gateway/app_gateway.go`：删除 `Start()` 中 PushInitialConfig 调用（第 148-158 行）
- `internal/swarm/gateway/config_push.go`：删除 `PushInitialConfig()` 和 `pushConfigToAgentServer()` 方法

**保留**：`onConfigSavedImpl()` — 热重载核心回调，由 Step 3-4 的触发路径调用

### Step 2: NewAppRPCHandlers 接收 onConfigSaved 回调

**对齐 Python**：`WebHandlersBindParams(on_config_saved=_on_config_saved)` 将回调传入 WebHandler。

**文件**：`internal/swarm/gateway/channel_manager/web/web_handlers.go`

1. `NewAppRPCHandlers` 签名新增参数：
```go
func NewAppRPCHandlers(
    sendEvent EventSender,
    onMessage func(*schema.Message),
    onConfigSaved gateway.OnConfigSavedFunc,  // 新增
) *RPCDispatcher
```

2. `handleConfigSet` 接收 `onConfigSaved`，在写完 .env 后调用回调替换 stub
3. 新增 `handleConfigSaveAll` 替换 stubHandler

**文件**：`internal/swarm/gateway/channel_manager/web/web_connect.go`

4. `WebChannel` 创建 dispatcher 时传入 `onConfigSaved`

**文件**：`internal/swarm/gateway/app_gateway.go`

5. `NewGatewayServer` 中将 `s.OnConfigSaved()` 传给 WebChannel

### Step 3: handleConfigSet 完整实现

**对齐 Python**：`_config_set` → `_apply_config_payload` → `_notify_config_saved_once`

**文件**：`internal/swarm/gateway/channel_manager/web/web_handlers.go`

完整逻辑：
1. 参数映射到环境变量（已有，`configEnvMap` 遍历）
2. provider 校验（新增，对齐 Python `available_model_providers` 校验）
3. YAML 键写入（新增，对齐 Python `_CONFIG_YAML_KEYS` 5 个键）
4. agents/team 写入 config.yaml（新增，对齐 Python `replace_teams_in_config`）
5. os.Setenv + 持久化 .env（已有）
6. 回包给前端（已有）
7. **调用 onConfigSaved 回调**（新增，替换 stub）

### Step 4: handleConfigSaveAll 完整实现

**对齐 Python**：`_config_save_all` 支持 config + models + agents/team 三种子载荷

**文件**：`internal/swarm/gateway/channel_manager/web/web_handlers.go`

完整逻辑：
1. 解析 params：config、models、agents、team 子载荷
2. config 子载荷走 `_apply_config_payload` 等价逻辑
3. models 子载荷走 `_build_models_defaults_from_frontend` + `update_default_models_in_config`
4. 回包给前端
5. **调用 onConfigSaved 回调**（force=True）

### Step 5: _apply_config_payload 等价实现

**对齐 Python**：`_apply_config_payload` (app_web_handlers.py L701-761)

**文件**：`internal/swarm/gateway/channel_manager/web/config_apply.go`（新文件）

包含：
1. `configYAMLKeys` 常量：5 个 YAML 键（context_engine_enabled, kv_cache_affinity_enabled, permissions_enabled, memory_forbidden_enabled, memory_forbidden_description）
2. `applyConfigPayload(params) (envUpdates, yamlUpdated)` 函数
3. `updateYAMLKeyInConfig(key, value)` 辅助函数
4. `replaceTeamsInConfig(params)` 辅助函数
5. `updateDefaultModelsInConfig(models)` 辅助函数

### Step 6: _notify_config_saved_once 等价实现

**对齐 Python**：`_notify_config_saved_once` (app_web_handlers.py L763-782)

**文件**：`internal/swarm/gateway/channel_manager/web/config_apply.go`（新文件）

```go
// notifyConfigSavedOnce 在所有文件写入完成后触发一次热重载。
// 对齐 Python: _notify_config_saved_once (app_web_handlers.py L763-782)
func notifyConfigSavedOnce(
    onConfigSaved gateway.OnConfigSavedFunc,
    envUpdates map[string]any,
    yamlUpdated []string,
    force bool,
) {
    if !force && len(envUpdates) == 0 && len(yamlUpdated) == 0 {
        return
    }
    if onConfigSaved == nil {
        return
    }
    // 获取最新配置快照
    configPayload, _ := config.GetConfigRaw()
    // 合并变更键
    updatedKeys := make([]string, 0, len(envUpdates)+len(yamlUpdated))
    for k := range envUpdates {
        updatedKeys = append(updatedKeys, k)
    }
    updatedKeys = append(updatedKeys, yamlUpdated...)
    // 调用回调
    _ = onConfigSaved(updatedKeys, envUpdates, configPayload)
}
```

### Step 7: Gateway 层接入 fsnotify reloader

**补充触发**：Python 没有，但 Go 单体场景用户可能直接编辑 config.yaml，fsnotify 作为补充。

**文件**：`internal/swarm/gateway/app_gateway.go`

1. `GatewayServer` 结构体新增 `reloader` 字段
2. `NewGatewayServer` 创建 reloader
3. `Start()` 中注册 `OnReload` 回调 → 调用 `onConfigSavedImpl`
4. `Stop()` 中停止 reloader

回调逻辑：
```go
if s.reloader != nil {
    s.reloader.OnReload(func(data map[string]any) {
        // fsnotify 检测到 config.yaml 变更
        // 用最新配置快照调用 onConfigSaved
        configData, _ := s.config.Raw()
        _ = s.onConfigSavedImpl(nil, BuildEnvMap(), configData)
    })
    _ = s.reloader.Start()
}
```

### Step 8: cmd 层删除 reloader 相关代码

**文件**：`cmd/uapclaw/cmd.go`

reloader 已在 Step 7 移到 Gateway 层，cmd 层不再涉及。确认 cmd.go 中没有残留 reloader 代码（已在前次 commit 移除，此处仅验证）。

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `gateway/app_gateway.go` | 修改 | -PushInitialConfig 调用、+reloader 字段/创建/注册/停止、+onConfigSaved 传给 WebChannel |
| `gateway/config_push.go` | 修改 | -PushInitialConfig/-pushConfigToAgentServer、保留 onConfigSavedImpl |
| `gateway/config_push_test.go` | 修改 | 适配删除的方法 |
| `gateway/doc.go` | 修改 | 更新文件目录 |
| `channel_manager/web/web_handlers.go` | 修改 | +onConfigSaved 参数、handleConfigSet 升级、+handleConfigSaveAll |
| `channel_manager/web/web_connect.go` | 修改 | 传入 onConfigSaved 给 NewAppRPCHandlers |
| `channel_manager/web/config_apply.go` | 新增 | applyConfigPayload、notifyConfigSavedOnce、YAML 键写入 |
| `channel_manager/web/config_apply_test.go` | 新增 | 测试 |

## 对齐 Python 对照表

| Python | Go |
|--------|-----|
| 启动时不发 agent.reload_config（set_or_update_server_config 空操作） | 删除 PushInitialConfig |
| AgentServer 自己读 config.yaml + .env | AgentServer 通过 s.config 直接使用（单体共享） |
| `_config_set` → `_apply_config_payload` | `handleConfigSet` → `applyConfigPayload` |
| `_config_set` → `_notify_config_saved_once` | `handleConfigSet` → `notifyConfigSavedOnce` |
| `_config_save_all` → config + models + team | `handleConfigSaveAll` → config + models + team |
| `_notify_config_saved_once` → `on_config_saved` | `notifyConfigSavedOnce` → `onConfigSavedImpl` |
| `_on_config_saved` → `client.send_request(reload_env)` | `onConfigSavedImpl` → `s.agentClient.SendRequest(envelope)` |
| `_on_config_saved` → `browser_runtime_keys & updated_env_keys` | `ShouldBrowserRestart(updatedKeys)` |
| `_CONFIG_YAML_KEYS` (5 键) | `configYAMLKeys` |
| `_apply_config_payload` 加密 | TODO(⤵️): 加密待补充 |
| `_schedule_restart` 异常兜底 | TODO(⤵️): 降级策略待讨论 |
| （Python 无 fsnotify） | reloader 在 Gateway 层接入（Go 补充） |
