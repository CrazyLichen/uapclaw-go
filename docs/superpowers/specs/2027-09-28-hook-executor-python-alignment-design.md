# HookExecutor 对齐 Python 无参构造 — 设计文档

## 背景

10.3.23.5 审查发现 Go 版 `HookExecutor` / `UserHookRail` 的构造方式与 Python 不对齐：

| 组件 | Python | Go（当前） | 偏差 |
|------|--------|------------|------|
| `HookExecutor()` | 无参构造，`_query_llm` 运行时动态 `get_config()` | 需传入 `LLMConfig`，调用方预提取配置 | 调用方承担了 Python 内部的职责 |
| `UserHookRail(hooks_config)` | 只接收 `HooksConfig` | 需额外传入 `*HookExecutor` | 签名不对齐 |

`handleHooksList` 异常响应多了 `"code": "INTERNAL_ERROR"` 字段，属于增强偏差，保留不改。

## 决策

- **偏差 1**：改对齐 Python，`HookExecutor` 无参构造，内部通过全局 Config 注册点读取配置
- **偏差 2**：保留 `code` 字段，属合理增强

## 设计

### 方案：全局 Config 注册点

在 `hooks` 包内定义包级 `*config.Config` 变量，由 `AgentServer` 启动时注册。`HookExecutor.queryLLM` 调用时从全局配置读取 LLM 参数，等价 Python 的 `get_config()`。

**循环依赖验证**：`config` 包不导入 `hooks` 包，`hooks` 包新增对 `config` 的单向依赖安全。

### 变更清单

#### 1. `hooks/executor.go`

- **移除** `LLMConfig` 结构体及其字段
- **移除** `HookExecutor.llmConfig` 字段
- **改** `NewHookExecutor(llmConfig LLMConfig) → NewHookExecutor()` 无参构造
- **新增** 全局配置注册：

```go
var (
    globalCfg   *config.Config
    globalCfgMu sync.RWMutex
)

// RegisterConfig 注册全局 Config 实例，对齐 Python get_config() 的全局可用性
func RegisterConfig(cfg *config.Config) {
    globalCfgMu.Lock()
    defer globalCfgMu.Unlock()
    globalCfg = cfg
}

func getGlobalConfig() *config.Config {
    globalCfgMu.RLock()
    defer globalCfgMu.RUnlock()
    return globalCfg
}
```

- **改** `queryLLM` 内部从 `getGlobalConfig().Load()` 提取 `models.default.model_client_config`，等价 Python `_query_llm` 中 `from jiuwenswarm.common.config import get_config; config_base = get_config()` 的行为
- **新增** `getGlobalConfig()` 返回 nil 时的错误处理

#### 2. `hooks/user_hook_rail.go`

- **改** `NewUserHookRail(config hookscfg.HooksConfig, executor *HookExecutor) → NewUserHookRail(config hookscfg.HooksConfig)`
- 内部 `executor: NewHookExecutor()` 无参创建，对齐 Python `self._executor = HookExecutor()`

#### 3. `adapter/deep_adapter_rails.go`

- **移除** `extractLLMConfig` 函数
- **移除** `strFromMap` 函数
- **简化** 步骤 21 UserHookRail 注册：移除 `extractLLMConfig` + `NewHookExecutor` 调用，改为 `serverhooks.NewUserHookRail(*hooksCfg)`

#### 4. `adapter/code_adapter.go`

- **简化** UserHookRail 注册：同上移除 `extractLLMConfig` + `NewHookExecutor`，改为 `serverhooks.NewUserHookRail(*hooksCfg)`

#### 5. `server/agent_server.go`

- **新增** 启动时 `serverhooks.RegisterConfig(s.config)`

#### 6. 测试适配

- `executor_test.go`：`NewHookExecutor(LLMConfig{})` → `NewHookExecutor()`；新增 `RegisterConfig` 注册 mock Config
- `executor_llm_test.go`：同上
- `user_hook_rail_test.go`：`NewUserHookRail(cfg, exec)` → `NewUserHookRail(cfg)`；移除手动创建 `HookExecutor`

### 不变更

- `handleHooksList` 异常响应的 `"code": "INTERNAL_ERROR"` 字段保留（增强偏差）
- `HookExecutor` 的 `RunAll`、`runCommandHook`、`runPromptHook`、`ParseCommandOutput`、`ExtractJSONFromResponse` 逻辑不变
- `UserHookRail` 的四个钩子方法逻辑不变

### 对齐验证标准

修改完成后，以下对比必须成立：

| 检查点 | Python | Go（修改后） |
|--------|--------|-------------|
| HookExecutor 构造 | `HookExecutor()` | `NewHookExecutor()` ✅ |
| UserHookRail 构造 | `UserHookRail(hooks_config)` | `NewUserHookRail(hooksCfg)` ✅ |
| LLM 配置获取时机 | `_query_llm` 运行时 `get_config()` | `queryLLM` 运行时 `getGlobalConfig().Load()` ✅ |
| DeepAdapter 注册 | `UserHookRail(hooks_config)` | `NewUserHookRail(*hooksCfg)` ✅ |
| CodeAdapter 注册 | `UserHookRail(hooks_config)` | `NewUserHookRail(*hooksCfg)` ✅ |
