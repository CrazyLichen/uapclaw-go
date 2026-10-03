# 7.20 Dreaming Orchestrator + Sweeper 实现设计

## 1. 概述

实现 Dreaming 记忆整理系统，包含两层：
- **核心编排器** `DreamingOrchestrator`（agentcore 层）：纯调度框架，定时执行 sweep
- **业务 Sweeper** + **公共 API**（swarm 层）：扫描/压缩/LLM 提取/Promotion

同时回填 `DeepAdapter.TryStartDreaming` / `TryStopDreaming` 中的 `⤵️ 10.6.13-18` 标记，使调用链完整对齐 Python。

### 在 Agent 会话中的流程位置

Dreaming 位于 Agent 生命周期的**后台守护层**，独立于任何单次请求链路：

```
CreateInstance()
  ├─ 初始化 SDK Agent / SessionManager
  └─ TryStartDreaming(busyChecker)  ← dreaming 在此启动
       │
       ▼
  [后台 goroutine] DreamingOrchestrator.loop()
    ├─ 初始延迟 120s
    └─ 循环:
        ├─ time.Sleep(interval)   ← 默认 14400s (4h)
        ├─ tick()
        │   ├─ busyChecker()? → 跳过本轮
        │   └─ Sweeper.RunSweep()
        │       ├─ Scan: 扫描新 session 目录
        │       ├─ Compress: 压缩对话历史
        │       ├─ Extract: LLM 提取经验/记忆
        │       └─ Promote: 写入 DREAMING.md / consolidated_*.md
        └─ 直到 stop

ReloadAgentConfig()
  ├─ TryStopDreaming()     ← 先停
  ├─ 重载配置
  └─ TryStartDreaming()    ← 再启
```

### 作用

**Dreaming = "睡觉整理记忆"**，定期扫描历史会话对话，用 LLM 提取有价值的信息，持久化为结构化文件：

| 功能 | 说明 |
|------|------|
| Busy Backoff | 有活跃 session 任务时跳过，避免争抢资源 |
| 增量扫描 | 通过 checkpoint 只处理新增/变更的 session |
| 对话压缩 | 截断过长对话，保留近期轮次 |
| LLM 提取 | 分 code/agent 模式，用不同提示词提取经验/记忆 |
| Promotion | agent 模式写入 DREAMING.md，code 模式写入 consolidated_*.md |
| 去重淘汰 | agent 模式标题去重 + 超 50 条淘汰旧条目；code 模式内容 hash 去重 |

## 2. 包结构

对齐 Python 的两层分离：

```
agentcore/memory/dreaming/          ← 核心编排器（对齐 openjiuwen/core/memory/dreaming/）
  ├── doc.go
  └── orchestrator.go               ← DreamingOrchestrator

swarm/agents/harness/common/memory/dreaming/   ← 业务层（对齐 jiuwenswarm/agents/harness/common/memory/dreaming/）
  ├── doc.go
  ├── sweeper.go                    ← Sweeper + DreamingConfig
  ├── prompts.go                    ← LLM 提示词模板（4 套：code/agent × zh/en）
  └── api.go                        ← StartDreaming / StopDreaming / GetDreamingOrchestrator
```

## 3. 核心编排器：DreamingOrchestrator

### 文件：`agentcore/memory/dreaming/orchestrator.go`

```go
type DreamingOrchestrator struct {
    sweepFn      func(ctx context.Context) error  // 对齐 Python sweep_fn: Callable[[], Awaitable[None]]
    interval     time.Duration                     // 对齐 Python interval_seconds，最小 60s
    busyChecker  func() bool                       // 对齐 Python busy_checker: Callable[[], bool] | None
    name         string                            // 对齐 Python name，默认 "dreaming"
    running      bool
    cancelFunc   context.CancelFunc
    doneCh       chan struct{}                      // goroutine 退出信号
}
```

### 方法

| 方法 | Python 对应 | 说明 |
|------|------------|------|
| `NewDreamingOrchestrator(sweepFn, interval, opts...)` | `__init__` | 构造，interval 小于 60s 钳位到 60s |
| `Start(ctx) error` | `async start()` | 启动后台 goroutine，幂等 |
| `Stop(ctx) error` | `async stop()` | 停止 goroutine，幂等 |
| `Health() map[string]any` | `health` property | 返回 `{running, interval_seconds}` |

### 并发模型：time.NewTicker + goroutine

```go
func (o *DreamingOrchestrator) loop(ctx context.Context) {
    // 初始延迟 120s（对齐 Python: await asyncio.sleep(120.0)）
    select {
    case <-time.After(120 * time.Second):
    case <-ctx.Done():
        return
    }

    ticker := time.NewTicker(o.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            o.tick(ctx)
        case <-ctx.Done():
            return
        }
    }
}
```

### tick 逻辑

```go
func (o *DreamingOrchestrator) tick(ctx context.Context) {
    // Busy Backoff（对齐 Python _tick 的 busy_checker 逻辑）
    if o.busyChecker != nil {
        if o.busyChecker() {
            logger.Info(...).Msg("agent busy, delay sweep")
            return
        }
    }
    // 执行 sweep（对齐 Python: await self._sweep_fn()）
    if err := o.sweepFn(ctx); err != nil {
        if errors.Is(err, context.Canceled) {
            return  // 对齐 Python: CancelledError → raise
        }
        logger.Error(...).Err(err).Msg("sweep exception")
    }
}
```

## 4. 业务层：Sweeper

### 文件：`swarm/agents/harness/common/memory/dreaming/sweeper.go`

#### DreamingConfig

```go
type DreamingConfig struct {
    Enabled         bool
    IntervalSeconds float64  // 默认 14400.0
}
```

加载逻辑对齐 Python `DreamingConfig.load(mode)`：
- 从 `config.yaml` 的 `memory.dreaming.{mode}` 段读取
- 环境变量 `DREAMING_{MODE}_ENABLED` 优先级高于配置文件
- 环境变量 `DREAMING_INTERVAL` 覆盖间隔
- 异常时返回默认值（enabled=false）

#### Sweeper

```go
type Sweeper struct {
    sessionsDir    string
    outputDir      string
    mode           string   // "agent" / "code"
    language       string   // "zh" / "en"
    dreamsDir      string   // outputDir + "/.dreams"
    scannedSessions map[string]ScannedSession  // checkpoint 缓存
}
```

**配置获取方式**：和 Python 一致，Sweeper 内部直接调用配置获取函数，不需要外部注入回调：
- `_extractViaLLM` 中调用 `getDefaultModels()` 获取默认模型列表（对齐 Python `from jiuwenswarm.common.config import get_default_models`）
- `DreamingConfig.Load` 中调用 `get_config()` 读取配置段（对齐 Python `from jiuwenswarm.common.config import get_config`）

Go 等价方式：`config.New("")` + `Load()` 获取全局配置，从中提取 `models.defaults`（已有先例：`web/config_apply.go` 的 `GetDefaultModels()`）。

#### Sweeper 方法清单

| 方法 | Python 对应 | 说明 |
|------|------------|------|
| `NewSweeper(sessionsDir, outputDir, mode, language)` | `__init__` | 构造 |
| `Init() error` | `init()` | 创建目录 + 加载 checkpoint |
| `RunSweep(ctx) error` | `async run_sweep()` | 完整管线（Scan→Compress→Extract→Promote） |
| `ScanNewSessions() []ScannedSession` | `scan_new_sessions()` | 增量扫描 |
| `matchSessionMode(sessionID) bool` | `_match_session_mode()` | 按 metadata.json 过滤 mode |
| `parseHistory(sessionDir) []HistoryEvent` | `_parse_history()` | 解析 history.json |
| `detectRounds(events) []Round` | `_detect_rounds()` | 检测对话轮次 |
| `compress(events, depth) string` | `_compress()` | 压缩对话，递归截断 |
| `extractViaLLM(ctx, text, summary) []KnowledgeItem` | `_extract_via_llm()` | LLM 提取 |
| `loadExistingSummary() string` | `load_existing_summary()` | 加载已有 DREAMING.md 摘要 |
| `promote(title, content, sessionID) string` | `_promote()` | 路由到 agent/code 模式 |
| `promoteAgent(title, content, sessionID) string` | `promote_agent()` | 写入 DREAMING.md |
| `promoteCode(title, content, sessionID) string` | `promote_code()` | 写入 consolidated_*.md |
| `parseDreamingEntries(text) []DreamingEntry` | `_parse_dreaming_entries()` | 解析 DREAMING.md 条目 |
| `loadCheckpoint() CheckpointData` | `_load_checkpoint()` | 加载 .dreams/ingestion-checkpoint.json |
| `saveCheckpoint()` | `save_checkpoint()` | 保存 checkpoint |

#### 常量（对齐 Python）

```go
const (
    minSessionRounds     = 4
    maxSessionsPerSweep  = 10
    maxSessionAgeDays    = 30
    maxCompressTokens    = 30000
    maxEntriesAgent      = 50
    maxPromotionsPerSession = 5
    minNewSessions       = 3
    minSessionAgeBypassDays = 7
)
```

## 5. 公共 API

### 文件：`swarm/agents/harness/common/memory/dreaming/api.go`

```go
var orchestrators sync.Map  // key: mode(string), value: *DreamingOrchestrator

func StartDreaming(ctx context.Context, sessionsDir, outputDir, mode string, busyChecker func() bool) (*DreamingOrchestrator, error)
func StopDreaming(ctx context.Context, mode string) error
func GetDreamingOrchestrator(mode string) *DreamingOrchestrator
```

对齐 Python：
- `StartDreaming` 对齐 `start_dreaming()`：幂等，同 mode 重复调用返回已有实例
- `StopDreaming` 对齐 `stop_dreaming()`：幂等，mode 为空时停止所有
- `GetDreamingOrchestrator` 对齐 `get_dreaming_orchestrator()`

## 6. LLM 调用方式（完全对齐 Python）

Python `_extract_via_llm` 完整调用链：

```python
from jiuwenswarm.common.config import get_default_models
from openjiuwen.core.foundation.llm import Model, ModelClientConfig, ModelRequestConfig, UserMessage, SystemMessage

entries = get_default_models()
entry = entries[0]
mcc = entry.get("model_client_config", {})
model_name = mcc.get("model_name", "")
mcc_fields = {k: v for k, v in mcc.items() if k != "model_name"}
model = Model(
    model_client_config=ModelClientConfig(**mcc_fields),
    model_config=ModelRequestConfig(model=model_name, temperature=0.3),
)
response = await model.invoke([SystemMessage(...), UserMessage(...)])
```

Go 等价实现——Sweeper 内部直接获取配置（不通过外部注入回调）：

```go
// getDefaultModels 内部获取默认模型列表。
// 对齐 Python: from jiuwenswarm.common.config import get_default_models
func getDefaultModels() []map[string]any {
    cfg, err := config.New("")
    if err != nil { return nil }
    cfgData, err := cfg.Load()
    if err != nil { return nil }
    modelsSection, _ := cfgData["models"].(map[string]any)
    // 优先级：models.defaults（列表） > models.default（单对象）
    // ... 同 adapter/deep_adapter.go 中的 getDefaultModels 逻辑
}

// _extractViaLLM 内部调用
entries := getDefaultModels()   // 对齐 Python: get_default_models()
entry := entries[0]
mcc, _ := entry["model_client_config"].(map[string]any)
modelName, _ := mcc["model_name"].(string)
mccFields := filterKey(mcc, "model_name")  // 排除 model_name

clientConfig := llmschema.NewModelClientConfigFromMap(mccFields)
modelConfig := &llmschema.ModelRequestConfig{Model: modelName, Temperature: floatPtr(0.3)}
model, err := llm.NewModel(clientConfig, modelConfig)
// ...
response, err := model.Invoke(ctx, messages)
```

Python 使用 `SystemMessage` / `UserMessage`，Go 等价为 `llmschema.SystemMessage` / `llmschema.UserMessage`。

## 7. Sessions Dir 路径获取

Python：
```python
from jiuwenswarm.common.utils import get_agent_sessions_dir
sessions_dir = str(get_agent_sessions_dir() or "")
# get_agent_sessions_dir() = get_user_workspace_dir() / "agent" / "sessions"
# = ~/.jiuwenswarm/agent/sessions
```

Go 对应：
```go
workspace.AgentSessionsDir()  // = WorkspaceDir()/agent/sessions
```

Output dir 计算（Python `try_start_dreaming` 中）：
```python
output_name = "memory" if mode == "agent" else "coding_memory"
base_dir = getattr(self, "_agent_workspace_dir", None) or self._workspace_dir
output_dir = os.path.join(base_dir, output_name)
```

Go 对应：
```go
outputName := "memory"
if !strings.HasPrefix(d.dreamingMode, "agent") { outputName = "coding_memory" }
baseDir := d.getAgentWorkspaceDir()  // 已有方法
outputDir := filepath.Join(baseDir, outputName)
```

## 8. DeepAdapter 回填

### `TryStartDreaming` 回填后：

```go
func (d *DeepAdapter) TryStartDreaming(ctx context.Context, busyChecker func() bool) error {
    if d.dreamingStarted { return nil }
    if d.dreamingMode == "" { return nil }
    if busyChecker != nil && busyChecker() { return nil }

    sessionsDir := workspace.AgentSessionsDir()
    outputName := "memory"
    if !strings.HasPrefix(d.dreamingMode, "agent") { outputName = "coding_memory" }
    baseDir := d.getAgentWorkspaceDir()
    outputDir := filepath.Join(baseDir, outputName)

    orch, err := dreaming.StartDreaming(ctx, sessionsDir, outputDir, d.dreamingMode, busyChecker)
    if err != nil {
        logger.Warn(logComponent).Err(err).Msg("start_dreaming failed")
        return err
    }
    d.dreamingStarted = orch != nil
    return nil
}
```

### `TryStopDreaming` 回填后：

```go
func (d *DeepAdapter) TryStopDreaming(ctx context.Context) error {
    if !d.dreamingStarted { return nil }
    if err := dreaming.StopDreaming(ctx, d.dreamingMode); err != nil {
        logger.Warn(logComponent).Err(err).Msg("stop_dreaming failed")
    }
    d.dreamingStarted = false
    return nil
}
```

## 9. 提示词模板

### 文件：`swarm/agents/harness/common/memory/dreaming/prompts.go`

4 套模板（对齐 Python `_PROMPT_CODE` / `_PROMPT_AGENT` / `_PROMPT_CODE_EN` / `_PROMPT_AGENT_EN`），以及 4 套系统消息。

**遵循项目规则：提示词必须从 Python 源码直接复制，禁止自行翻译或改写。**

模板变量：`{existing_knowledge}` / `{compressed_session}` / `{max_items}`

## 10. i18n 文本

对齐 Python `_UI_TEXT`：

```go
var uiText = map[string]UIText{
    "zh": {EmptySummary: "(空)", DreamingHeader: "# Dreaming 记忆\n", ...},
    "en": {EmptySummary: "(None)", DreamingHeader: "# Dreaming Memories\n", ...},
}
```

## 11. 依赖方向

```
agentcore/memory/dreaming/          ← 不依赖 swarm
  ↑
  │
swarm/agents/harness/common/memory/dreaming/
  ↑
  │
swarm/server/adapter/               ← DeepAdapter 回填时 import dreaming
```

所有依赖方向正确，无循环依赖。

## 12. IMPLEMENTATION_PLAN.md 更新

| 项目 | 变更 |
|------|------|
| 7.20 | `☐` → `✅`，描述更新为 "DreamingOrchestrator + Sweeper + 公共API + DeepAdapter 回填" |
| 7.24 | `☐` → `✅`（已由 codec 实现，标记修正） |
| 10.6.13-18 | ⤵️ 标记中的 dreaming 部分删除（已回填） |

## 13. 测试覆盖

### Orchestrator 测试（对齐 Python `test_orchestrator.py`）

- 构造 & health：默认值、interval 钳位、health 属性
- 生命周期：Start 创建 goroutine、幂等、Stop 取消并清理、幂等、从未启动时 Stop
- tick：无 busyChecker 时执行 sweep、busy 时跳过、不 busy 时执行、busyChecker 异常不阻塞、sweep 异常记录日志、context.Canceled 传播
- loop：尊重 cancel、running=false 退出、意外异常记录
- 集成：初始 sleep 期间 stop、自定义 name 传播

### Sweeper 测试

- DreamingConfig.Load：配置文件 enabled、interval、环境变量覆盖
- ScanNewSessions：增量扫描、新 session 过滤、rounds 过滤、mode 匹配
- Compress：递归截断、token 限制
- ParseHistory / DetectRounds：事件过滤、轮次检测
- ExtractViaLLM：mock LLM 调用、JSON 解析、异常处理
- LoadExistingSummary：agent 模式读 DREAMING.md、code 模式读 consolidated_*.md
- PromoteAgent：去重、淘汰、写入
- PromoteCode：hash 去重、YAML frontmatter
- ParseDreamingEntries：标题/来源/内容解析
- Checkpoint：加载/保存/异常回退

### 公共 API 测试

- StartDreaming：幂等、配置 disabled 时返回 nil
- StopDreaming：幂等、空 mode 停止所有
- GetDreamingOrchestrator：获取/不存在

### DeepAdapter 回填测试

- TryStartDreaming：已有实例幂等、mode 为空跳过、busy 跳过、正常启动
- TryStopDreaming：未启动幂等、正常停止
