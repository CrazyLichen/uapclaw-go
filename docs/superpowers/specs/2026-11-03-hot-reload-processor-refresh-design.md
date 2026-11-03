# 热重载后老会话未跟随新模型/新阈值 修复设计

- 日期：2026-11-03
- 前置事实：集成测试 `tests/integration/swarm/gateway/hot_reload/` 已复现全部观测（含现状锁定用例）
- 关联：`2026-11-03-model-token-config`（`max_input_tokens` / `max_output_tokens` 接线）与本轮压缩链路分析

## 1. 问题与实测证据

界面保存（`config.save_all`）同时切换模型与 token 上限后，**主循环换了、压缩链路没换**。

| 观测 | 保存前 | 保存后（同 session） | 保存后（新 session） |
|---|---|---|---|
| 主循环实际模型 | `model-a` | **`model-b`** | `model-b` |
| ContextProcessorRail `Init` 收到的模型 | `model-a` | **`model-b`** | `model-b` |
| `reactCfg.ContextProcessors` 里的压缩阈值 | 680 / 440 | **510 / 330** | 510 / 330 |
| 压缩实际打到的模型 | `model-a` | **`model-a`**（未跟随） | `model-b` |
| 老会话 `mc.ContextMaxTokens()` | 800 | **800**（未跟随） | 600 |

即：rail 重建成功、specs 已更新、阈值已重算，但**已存在的会话仍用旧处理器实例与旧窗口值**。

## 2. 根因

### 2.1 上下文池命中分支丢弃新处理器规格

- `ContextEngine.contextPool` 以 `sessionID_contextID` 为键缓存 `ModelContext`（`engine.go:34-35`）；
- 命中时直接返回旧对象，`opt.Processors` 从头到尾没被读取（`engine.go:101-114`）；
- 只有 miss 才用 specs 实例化（`engine.go:119-127`）。

`RoundLevelCompressor` 等处理器在**构造时**把配置抄成字段（`round_level_compressor.go:279`、`:295-303`），因此实例一旦建好，其模型指针与阈值就冻结。

### 2.2 上下文级配置在构造时被逐字段拷贝，且 `ce.config` 从不更新

`NewSessionModelContext` 把引擎配置拆成字段（`session_model_context.go:146-151`：`modelName` / `contextWindowTokens` / `modelContextWindowTokens` / `enableReload` / `defaultWindowSize` / `defaultDialogueRound`）。`mc.ContextMaxTokens()` 用这些字段现算（`:1217` → `ResolveContextMax`）。

`ContextEngine` 只在 `initialConfigure` 时用 `*cfg.ContextEngineConfig` 构造一次（`deep_agent.go:1839-1847`）；`hotReconfigure` 只更新 react config 里的 `ContextEngineConfig`，不重建 engine，更不碰池内 mc。

### 2.3 附带缺陷：默认模型选择依赖 map 随机序（已修）

`createModel` 原本 `range nameToKeys`（Go map 迭代随机）选默认模型；Python `_create_model` 遍历 `_model_name_to_keys`（dict 插入序 = 列表第一个）。多模型并存时 Go 的默认模型会随进程漂移。已改为按 `models.defaults` 出现顺序（`deep_adapter.go`），并补单测 `TestCreateModel_多模型默认按配置顺序`。

## 3. 修复方案

采用**推模式 + 内容指纹 + 轮次级惰性落地**：

### 3.1 引擎新增期望配置暂存区

```go
// engine.go
type ContextEngine struct {
    ...
    pendingConfig      ceschema.ContextEngineConfig
    pendingSpecs       []iface.ProcessorSpec
    pendingFingerprint string
    appliedFingerprint string
}

// ApplyRuntimeConfig 推送新配置与处理器规格（不立即改池子）
func (ce *ContextEngine) ApplyRuntimeConfig(cfg ceschema.ContextEngineConfig, specs []iface.ProcessorSpec)
```

指纹 = `sha256(json.Marshal(struct{ Cfg ContextEngineConfig; Specs []ProcessorSpec }{...}))`。`encoding/json` 对 struct 按字段序、对 map 按 key 排序，结果稳定，无需自定义序列化。

`ApplyRuntimeConfig` 只在具体类型 `*contextEngine` 上实现，并通过包内可选接口 `runtimeConfigApplier` 暴露，**不进入 `iface.ContextEngine`**——避免给所有已有 mock/实现加方法。rail 侧同样用本地可选接口断言。

首次推送时 `appliedFingerprint` 为空，会在下一次 `CreateContext` 应用一次；生产顺序是 rail Init 先推、会话后建上下文，此时池为空，无实际重建。

### 3.2 `CreateContext` 入口统一落地

```
CreateContext(...)
 ├─ pending != applied → applyPendingLocked()
 │    ① 用 ce.createProcessor 逐个实例化 pendingSpecs（任一失败 → 整批放弃）
 │    ② 对池内所有 mc 调 mc.ApplyRuntimeConfig(pendingConfig, 新实例)
 │    ③ ce.config = pendingConfig；appliedFingerprint = pendingFingerprint
 └─ 原命中 / miss 逻辑不变（miss 建出的新 mc 自然使用新 ce.config）
```

失败时：不替换任何 mc、不更新 `ce.config` 与 `appliedFingerprint`，打 `WARN` 并在下一次 `CreateContext` 自动重试（指纹仍不一致）。

### 3.3 `ModelContext` 唯一写入口

```go
// session_model_context.go
func (mc *SessionModelContext) ApplyRuntimeConfig(cfg ceschema.ContextEngineConfig, processors []iface.ContextProcessor) {
    mc.modelName = cfg.ModelName
    mc.contextWindowTokens = cfg.ContextWindowTokens
    mc.modelContextWindowTokens = cfg.ModelContextWindowTokens
    mc.enableReload = cfg.EnableReload
    mc.defaultWindowSize = cfg.DefaultWindowMessageNum
    mc.defaultDialogueRound = cfg.DefaultWindowRoundNum
    mc.messageBuffer.SetMaxSize(cfg.MaxContextMessageNum) // 新增：只改上限，缩小时交给 ifNeedResize
    mc.processors = processors                            // 换实例，消息/句柄/元数据不动
}
```

新增只读 `Processors()` 供测试断言与诊断。

### 3.4 推送点

`ContextProcessorRail.Init` 末尾（`config.ContextProcessors = allProcessors` 之后）取 `DeepAgent → ReactAgent → ContextEngine()` 并调用 `ApplyRuntimeConfig(config.ContextEngineConfig, allProcessors)`；取不到引擎时跳过并打 Debug。Init 只在首次装配与每次热重载重建 rail 时执行，因此推送时机与重载天然对齐。

### 3.5 不迁移处理器内部状态

核对结论：全部处理器 `SaveState()` 返回空 map、`LoadState()` 空操作；`processor/` 下无会话级缓存字段；`BaseProcessor.compressionUsage` 每轮入口都 `ResetCompressionUsage()`。会话级产物（offload 句柄、`compress_level`、记忆块文本）都在消息里，重建实例不动消息，因此不迁移。

## 4. 与 Python 的差异

| 项 | Python | 本实现 |
|---|---|---|
| 老会话是否跟随热重载 | **否**（`context_engine.py:96-101` 同样忽略 `processors`） | **是**（本设计新增） |
| 默认模型选择 | 插入序（`_create_model` 遍历 dict） | 修前 map 随机序 → 修后按 `models.defaults` 顺序（对齐 Python） |
| 配置开关 | 无 | 无（无新配置项、无新事件类型） |

本设计属 **Go 侧增强**，不回改 Python；后续如需同步，需要在 Python `create_context` 命中分支补同样的惰性重建。

## 5. 约束与风险

- **同 session 并发轮次不受支持**：按"同 session 串行"假设，替换 `processors` 与遍历它之间不加锁；`ModelContext` 本身按 `(session, context)` 隔离，跨会话不存在共享。若后续需要，可加无锁并发探测告警，不改数据结构。
- **单轮内不重建**：重建只发生在 `CreateContext`（`invokeImpl → initContext`，每轮一次），一轮内的 ReAct 迭代与工具调用不受影响。
- **旧实例的 llm.Model 无 Release 通道**：与 Python 一致，依赖 GC；如需精细管理再给处理器加 `Release` 钩子。
- **指纹比较成本**：每轮一次字符串比较；只有配置真变才重建。

## 6. 测试矩阵

| 层级 | 用例 | 断言 |
|---|---|---|
| 单测 `engine_runtime_config_test.go` | 指纹相同 | 池内 mc 的 `Processors()` 指针不变 |
| 单测 | 指纹变化 | 指针变化 + `mc.ContextMaxTokens()` 刷新 |
| 单测 | 处理器创建失败 | 保留旧实例、`AppliedFingerprint()` 不前进、下轮重试 |
| 单测 | 多 session | 池内全部刷新 |
| 单测 `session_model_context_runtime_config_test.go` | `ApplyRuntimeConfig` | 六个字段 + `messageBuffer` 上限 |
| 集成 `hot_reload` | 老会话跟随 | 同 session 第二轮压缩打 `model-b`、阈值 510/330 |
| 集成 `hot_reload` | 新会话跟随（保留） | 压缩打 `model-b`、阈值 510/330 |
| 集成 `hot_reload` | 主循环跟随（保留） | `Stream` 打 `model-b` |
| 回归 | `ModelTokenConfigSuite` / `DeepAdapterReloadSuite` / `CompressionTriggerSuite` + adapter/web/context_engine 单测 | 全绿 |

## 7. 回滚

无开关；回滚方式为 revert 本次提交。
