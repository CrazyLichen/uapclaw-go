# 7.20 Dreaming Orchestrator + Sweeper 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Dreaming 记忆整理系统（编排器 + Sweeper + 公共 API），并回填 DeepAdapter 的 ⤵️ 标记

**Architecture:** 两层分离：agentcore/memory/dreaming/ 放纯调度编排器，swarm/agents/harness/common/memory/dreaming/ 放业务 Sweeper + 公共 API。DeepAdapter 回填调用公共 API。

**Tech Stack:** Go 1.22+, time.NewTicker + goroutine 并发, llm.Model Invoke, config.New Load

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/memory/dreaming/doc.go` | 包文档 |
| `internal/agentcore/memory/dreaming/orchestrator.go` | DreamingOrchestrator 核心编排器 |
| `internal/agentcore/memory/dreaming/orchestrator_test.go` | 编排器单元测试 |
| `internal/swarm/agents/harness/common/memory/dreaming/doc.go` | 包文档 |
| `internal/swarm/agents/harness/common/memory/dreaming/sweeper.go` | Sweeper + DreamingConfig + 辅助类型 |
| `internal/swarm/agents/harness/common/memory/dreaming/prompts.go` | LLM 提示词模板（4 套） |
| `internal/swarm/agents/harness/common/memory/dreaming/api.go` | StartDreaming / StopDreaming / GetDreamingOrchestrator |
| `internal/swarm/agents/harness/common/memory/dreaming/sweeper_test.go` | Sweeper 单元测试 |
| `internal/swarm/agents/harness/common/memory/dreaming/api_test.go` | 公共 API 测试 |
| `internal/swarm/agents/harness/common/memory/dreaming/prompts_test.go` | 提示词模板测试 |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/swarm/server/adapter/deep_adapter_dreaming.go` | 回填 TryStartDreaming / TryStopDreaming 的 ⤵️ 标记 |
| `internal/swarm/server/adapter/deep_adapter_helpers_test.go` | 更新已有 dreaming 测试以验证回填 |
| `IMPLEMENTATION_PLAN.md` | 7.20 ☐→✅, 7.24 ☐→✅, 10.6.13-18 删除 dreaming ⤵️ 标记 |

---

## Task 1: DreamingOrchestrator 核心编排器

**Files:**
- Create: `internal/agentcore/memory/dreaming/doc.go`
- Create: `internal/agentcore/memory/dreaming/orchestrator.go`
- Create: `internal/agentcore/memory/dreaming/orchestrator_test.go`

Python 参考：`openjiuwen/core/memory/dreaming/orchestrator.py` + `tests/unit_tests/core/memory/dreaming/test_orchestrator.py`

- [ ] **Step 1: 创建 doc.go**

```go
// Package dreaming 提供后台记忆整理编排器。
//
// 本包实现 DreamingOrchestrator，负责定时触发记忆整理（sweep），
// 支持忙碌退避（busy backoff）和幂等启停。
//
// Dreaming 的含义是"睡觉整理记忆"——类似人睡眠期间大脑整理白天的记忆，
// 在 Agent 空闲时定期扫描历史会话，用 LLM 提取有价值的信息。
//
// 文件目录：
//
//	dreaming/
//	├── doc.go              # 包文档
//	└── orchestrator.go     # DreamingOrchestrator 后台定时编排器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/dreaming/
package dreaming
```

- [ ] **Step 2: 创建 orchestrator.go — 结构体 + 构造函数 + Health**

对齐 Python `DreamingOrchestrator.__init__` 和 `health` property。

```go
package dreaming

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingOrchestrator 空闲感知的定时记忆整理编排器。
//
// 生命周期：
//
//	Start(ctx) → 启动后台 goroutine
//	Stop(ctx)  → 取消 goroutine
//
// 调度策略：
//
//	1. Busy Backoff: busyChecker() 返回 true → 跳过本轮
//	2. 定时执行: 间隔 interval 执行 sweepFn
//
// 幂等：重复 Start()/Stop() 安全。
//
// Python: openjiuwen/core/memory/dreaming/orchestrator.py (DreamingOrchestrator)
type DreamingOrchestrator struct {
	// sweepFn 每轮执行的 sweep 函数
	sweepFn func(ctx context.Context) error
	// interval 调度间隔，最小 60s
	interval time.Duration
	// busyChecker 忙碌检查回调，返回 true 表示跳过本轮
	busyChecker func() bool
	// name 编排器名称，用于日志
	name string

	// running 是否运行中
	running bool
	// mu 保护 running 字段
	mu sync.Mutex
	// cancelFunc 用于取消后台 goroutine
	cancelFunc context.CancelFunc
	// doneCh goroutine 退出信号
	doneCh chan struct{}
}

// OrchestratorOption 编排器可选参数函数。
type OrchestratorOption func(*DreamingOrchestrator)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// minInterval 最小调度间隔，对齐 Python: self._interval = max(60.0, interval_seconds)
	minInterval = 60 * time.Second
	// initialDelay 初始延迟，对齐 Python: await asyncio.sleep(120.0)
	initialDelay = 120 * time.Second
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDreamingOrchestrator 创建编排器实例。
//
// interval 小于 60s 时钳位到 60s（对齐 Python: self._interval = max(60.0, interval_seconds)）。
// name 默认 "dreaming"（对齐 Python: name: str = "dreaming"）。
//
// Python: DreamingOrchestrator.__init__(sweep_fn, interval_seconds, busy_checker, name)
func NewDreamingOrchestrator(
	sweepFn func(ctx context.Context) error,
	interval time.Duration,
	opts ...OrchestratorOption,
) *DreamingOrchestrator {
	if interval < minInterval {
		interval = minInterval
	}
	o := &DreamingOrchestrator{
		sweepFn:     sweepFn,
		interval:    interval,
		busyChecker: nil,
		name:        "dreaming",
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithBusyChecker 设置忙碌检查回调。
func WithBusyChecker(checker func() bool) OrchestratorOption {
	return func(o *DreamingOrchestrator) { o.busyChecker = checker }
}

// WithName 设置编排器名称。
func WithName(name string) OrchestratorOption {
	return func(o *DreamingOrchestrator) { o.name = name }
}

// Health 返回编排器健康状态。
// Python: DreamingOrchestrator.health (property)
func (o *DreamingOrchestrator) Health() map[string]any {
	o.mu.Lock()
	defer o.mu.Unlock()
	return map[string]any{
		"running":          o.running,
		"interval_seconds": o.interval.Seconds(),
	}
}
```

- [ ] **Step 3: 实现 Start / Stop / loop / tick**

```go
// Start 启动后台 goroutine。幂等：已运行时重复调用无效。
// Python: DreamingOrchestrator.start()
func (o *DreamingOrchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.running {
		return nil
	}
	o.running = true
	ctx, o.cancelFunc = context.WithCancel(ctx)
	o.doneCh = make(chan struct{})
	go o.loop(ctx)
	logger.Info(logComponent).
		Str("name", o.name).
		Float64("interval_seconds", o.interval.Seconds()).
		Msg("Orchestrator started")
	return nil
}

// Stop 停止后台 goroutine。幂等：未运行时重复调用无效。
// Python: DreamingOrchestrator.stop()
func (o *DreamingOrchestrator) Stop(ctx context.Context) error {
	o.mu.Lock()
	if !o.running {
		o.mu.Unlock()
		return nil
	}
	o.running = false
	cancel := o.cancelFunc
	doneCh := o.doneCh
	o.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
	o.mu.Lock()
	o.cancelFunc = nil
	o.doneCh = nil
	o.mu.Unlock()

	logger.Info(logComponent).Str("name", o.name).Msg("Orchestrator stopped")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loop 后台主循环。
// Python: DreamingOrchestrator._loop()
func (o *DreamingOrchestrator) loop(ctx context.Context) {
	defer close(o.doneCh)

	// 初始延迟 120s（对齐 Python: await asyncio.sleep(120.0)）
	select {
	case <-time.After(initialDelay):
	case <-ctx.Done():
		logger.Error(logComponent).Str("name", o.name).Msg("loop cancelled during initial delay")
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

// tick 执行一轮 sweep。
// Python: DreamingOrchestrator._tick()
func (o *DreamingOrchestrator) tick(ctx context.Context) {
	// Busy Backoff（对齐 Python: busy_checker() 返回 True → 跳过）
	if o.busyChecker != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// 对齐 Python: except Exception → logger.warning("busy_checker raised exception, skipping check")
					logger.Warn(logComponent).
						Str("name", o.name).
						Any("panic", r).
						Msg("busy_checker raised exception, skipping check")
				}
			}()
			if o.busyChecker() {
				logger.Info(logComponent).Str("name", o.name).Msg("agent busy, delay sweep")
				return
			}
		}()
		// busyChecker panic 后继续执行 sweep（对齐 Python: 异常不阻塞）
	}

	// 执行 sweep（对齐 Python: await self._sweep_fn()）
	logger.Info(logComponent).Str("name", o.name).Msg("start sweep")
	if err := o.sweepFn(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			// 对齐 Python: CancelledError → raise
			logger.Error(logComponent).Str("name", o.name).Msg("sweep cancelled")
			return
		}
		// 对齐 Python: except Exception → logger.exception("sweep exception: %s", exc)
		logger.Error(logComponent).Err(err).Str("name", o.name).Msg("sweep exception")
	} else {
		logger.Info(logComponent).Str("name", o.name).Msg("sweep completed")
	}
}
```

- [ ] **Step 4: 创建 orchestrator_test.go — 对齐 Python test_orchestrator.py 的所有测试用例**

测试要点（对齐 Python 4 个测试类 14 个测试用例）：
- `TestNewDreamingOrchestrator_默认值`: name=dreaming, running=false, task=nil, busyChecker=nil
- `TestNewDreamingOrchestrator_间隔钳位`: interval=10s → 钳位到 60s; interval=120s → 保持 120s
- `TestNewDreamingOrchestrator_Health属性`: running=false, interval_seconds 正确
- `TestDreamingOrchestrator_Start_创建goroutine`: start 后 running=true
- `TestDreamingOrchestrator_Start_幂等`: 重复 start 返回同一实例
- `TestDreamingOrchestrator_Stop_取消并清理`: stop 后 running=false
- `TestDreamingOrchestrator_Stop_幂等`: 重复 stop 安全
- `TestDreamingOrchestrator_Stop_从未启动`: 从未 start 时 stop 安全
- `TestDreamingOrchestrator_Tick_无busyChecker时执行sweep`
- `TestDreamingOrchestrator_Tick_busy时跳过sweep`
- `TestDreamingOrchestrator_Tick_不busy时执行sweep`
- `TestDreamingOrchestrator_Tick_busyChecker异常不阻塞sweep`
- `TestDreamingOrchestrator_Tick_sweep异常记录日志`
- `TestDreamingOrchestrator_Tick_CancelledError传播`
- `TestDreamingOrchestrator_Loop_尊重cancel`
- `TestDreamingOrchestrator_Loop_初始sleep期间stop`
- `TestDreamingOrchestrator_Loop_自定义name传播`
- `TestDreamingOrchestrator_集成_完整启停`: start → 短间隔 → 确认 tick 执行 → stop

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/agentcore/memory/dreaming/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/dreaming/
git commit -m "feat(7.20): add DreamingOrchestrator core scheduler"
```

---

## Task 2: Sweeper 辅助类型 + DreamingConfig + 常量 + i18n

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/dreaming/doc.go`
- Create: `internal/swarm/agents/harness/common/memory/dreaming/sweeper.go` (先写类型+常量+Config部分)

Python 参考：`jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py` 的常量(行22-29)、DreamingConfig(行36-61)、_UI_TEXT(行67-80)

- [ ] **Step 1: 创建 doc.go**

```go
// Package dreaming 提供后台记忆整理（Dreaming）的业务实现。
//
// 本包实现 Sweeper 管线和公共 API（StartDreaming/StopDreaming/GetDreamingOrchestrator），
// 对齐 Python jiuwenswarm/agents/harness/common/memory/dreaming/。
//
// Sweeper 管线流程：Scan → Compress → LLM Extract → Promote
//
// 文件目录：
//
//	dreaming/
//	├── doc.go          # 包文档
//	├── sweeper.go      # Sweeper + DreamingConfig + 辅助类型
//	├── prompts.go      # LLM 提示词模板（4 套：code/agent × zh/en）
//	└── api.go          # StartDreaming / StopDreaming / GetDreamingOrchestrator
//
// 对应 Python 代码：
//
//	jiuwenswarm/agents/harness/common/memory/dreaming/
package dreaming
```

- [ ] **Step 2: 创建 sweeper.go — 常量 + 辅助类型 + DreamingConfig**

常量对齐 Python 行22-29，类型用于后续方法签名，DreamingConfig 对齐 Python 行36-61。

```go
package dreaming

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingConfig Dreaming 配置。
// Python: jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py (DreamingConfig)
type DreamingConfig struct {
	// Enabled 是否启用
	Enabled bool
	// IntervalSeconds 调度间隔秒数，默认 14400.0（4 小时）
	IntervalSeconds float64
}

// ScannedSession 已扫描 session 的快照信息。
type ScannedSession struct {
	// HistoryMtime history.json 的修改时间戳
	HistoryMtime float64
	// RoundCount 对话轮次计数
	RoundCount int
}

// SweeperSession 扫描结果条目。
type SweeperSession struct {
	// SessionID 会话标识
	SessionID string
	// CompressedText 压缩后的对话文本
	CompressedText string
}

// HistoryEvent history.json 中的一条事件。
type HistoryEvent struct {
	// Role 消息角色
	Role string
	// Content 消息内容
	Content string
	// EventType 事件类型（如 "chat.final"）
	EventType string
}

// Round 对话轮次（user 消息起始索引, assistant 消息索引）。
type Round struct {
	// UserIdx user 消息在 events 中的索引
	UserIdx int
	// AssistantIdx assistant 消息在 events 中的索引
	AssistantIdx int
}

// KnowledgeItem LLM 提取的知识条目。
type KnowledgeItem struct {
	// Title 标题
	Title string `json:"title"`
	// Content 内容
	Content string `json:"content"`
	// SourceSessionID 来源 session（由 RunSweep 填充）
	SourceSessionID string `json:"source_session_id,omitempty"`
}

// DreamingEntry DREAMING.md 中的条目。
type DreamingEntry struct {
	// Title 标题
	Title string
	// Source 来源信息（格式：_source: session_id | timestamp_）
	Source string
	// Content 内容
	Content string
}

// CheckpointData checkpoint 数据。
type CheckpointData struct {
	// ScannedSessions 已扫描 session 映射
	ScannedSessions map[string]ScannedSession
	// LastScanTs 上次扫描时间
	LastScanTs string
}

// UIText i18n 文本。
// Python: _UI_TEXT
type UIText struct {
	// EmptySummary 空摘要占位符
	EmptySummary string
	// DreamingHeader DREAMING.md 文件头
	DreamingHeader string
	// TruncateRounds 轮次截断提示
	TruncateRounds string
	// TruncateHard 硬截断提示
	TruncateHard string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// minSessionRounds 最少对话轮次（对齐 Python: _MIN_SESSION_ROUNDS = 4）
	minSessionRounds = 4
	// maxSessionsPerSweep 每轮最多扫描 session 数（对齐 Python: _MAX_SESSIONS_PER_SWEEP = 10）
	maxSessionsPerSweep = 10
	// maxSessionAgeDays session 最大保留天数（对齐 Python: _MAX_SESSION_AGE_DAYS = 30）
	maxSessionAgeDays = 30
	// maxCompressTokens 压缩后最大 token 估算（对齐 Python: _MAX_COMPRESS_TOKENS = 30000）
	maxCompressTokens = 30000
	// maxEntriesAgent agent 模式 DREAMING.md 最大条目数（对齐 Python: _MAX_ENTRIES_AGENT = 50）
	maxEntriesAgent = 50
	// maxPromotionsPerSession 每个 session 最多提取条目数（对齐 Python: _MAX_PROMOTIONS_PER_SESSION = 5）
	maxPromotionsPerSession = 5
	// minNewSessions 最少新 session 数才触发 sweep（对齐 Python: _MIN_NEW_SESSIONS = 3）
	minNewSessions = 3
	// minSessionAgeBypassDays 绕过最少新 session 限制的 session 天数（对齐 Python: _MIN_SESSION_AGE_BYPASS_DAYS = 7）
	minSessionAgeBypassDays = 7
	// maxCompressRecurse 压缩递归最大深度
	maxCompressRecurse = 5

	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentServer
)

// ──────────────────────────── 全局变量 ────────────────────────────

// uiText i18n 文本映射（对齐 Python: _UI_TEXT）
var uiText = map[string]UIText{
	"zh": {
		EmptySummary:   "(空)",
		DreamingHeader: "# Dreaming 记忆\n",
		TruncateRounds: "[... 前 %d 轮对话已省略 ...]\n\n",
		TruncateHard:   "[... 内容过长，已截断 ...]\n\n",
	},
	"en": {
		EmptySummary:   "(None)",
		DreamingHeader: "# Dreaming Memories\n",
		TruncateRounds: "[... First %d rounds omitted ...]\n\n",
		TruncateHard:   "[... Content too long, truncated ...]\n\n",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadDreamingConfig 从 config.yaml 和环境变量加载 Dreaming 配置。
// Python: DreamingConfig.load(mode)
func LoadDreamingConfig(mode string) DreamingConfig {
	defaultCfg := DreamingConfig{
		Enabled:         false,
		IntervalSeconds: 14400.0,
	}
	cfg, err := config.New("")
	if err != nil {
		logger.Warn(logComponent).Str("mode", mode).Err(err).Msg("dreaming configuration load failed, using default values")
		return defaultCfg
	}
	cfgData, err := cfg.Load()
	if err != nil {
		logger.Warn(logComponent).Str("mode", mode).Err(err).Msg("dreaming configuration load failed, using default values")
		return defaultCfg
	}

	// 从 config.yaml 读取 memory.dreaming.{mode} 段
	memorySection, _ := cfgData["memory"].(map[string]any)
	dreamingSection, _ := memorySection["dreaming"].(map[string]any)
	raw, _ := dreamingSection[mode].(map[string]any)
	if raw == nil {
		raw = map[string]any{}
	}

	// 环境变量优先级高于配置文件（对齐 Python: env_val = os.getenv(env_key)）
	envKey := fmt.Sprintf("DREAMING_%s_ENABLED", strings.ToUpper(mode))
	if envVal := os.Getenv(envKey); envVal != "" {
		defaultCfg.Enabled = strings.ToLower(envVal) == "true" || envVal == "1" || strings.ToLower(envVal) == "yes"
	} else {
		defaultCfg.Enabled, _ = raw["enabled"].(bool)
	}

	// 环境变量 DREAMING_INTERVAL 覆盖间隔
	if envInterval := os.Getenv("DREAMING_INTERVAL"); envInterval != "" {
		if f, err := strconv.ParseFloat(envInterval, 64); err == nil {
			defaultCfg.IntervalSeconds = f
		}
	} else {
		if v, ok := raw["interval_seconds"]; ok {
			if f, ok := v.(float64); ok {
				defaultCfg.IntervalSeconds = f
			}
		}
	}

	return defaultCfg
}

// getUIText 获取指定语言的 i18n 文本，回退到 zh。
func getUIText(language string) UIText {
	if t, ok := uiText[language]; ok {
		return t
	}
	return uiText["zh"]
}
```

注意：import 中需要 `"strconv"` 和 `"strings"`。

- [ ] **Step 3: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build -tags test ./internal/swarm/agents/harness/common/memory/dreaming/...`
Expected: 编译成功（Sweeper 结构体和方法的完整实现将在 Task 3 添加）

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/memory/dreaming/
git commit -m "feat(7.20): add Sweeper types, DreamingConfig, constants and i18n"
```

---

## Task 3: Sweeper 完整实现

**Files:**
- Modify: `internal/swarm/agents/harness/common/memory/dreaming/sweeper.go` (追加 Sweeper 结构体和全部方法)
- Create: `internal/swarm/agents/harness/common/memory/dreaming/sweeper_test.go`

Python 参考：`jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py`

- [ ] **Step 1: 在 sweeper.go 末尾追加 Sweeper 结构体 + 构造函数 + Init**

Sweeper 结构体对齐 Python `Sweeper.__init__`（行89-108），Init 对齐 Python `init()`（行110-121）。

```go
// Sweeper 统一记忆整理管线：Scan + Compression + LLM Extraction + Promotion。
// Python: jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py (Sweeper)
type Sweeper struct {
	// sessionsDir 会话目录路径
	sessionsDir string
	// outputDir 输出目录路径
	outputDir string
	// mode 模式（"agent" / "code"）
	mode string
	// language 语言（"zh" / "en"）
	language string
	// dreamsDir checkpoint 目录（outputDir + "/.dreams"）
	dreamsDir string
	// scannedSessions 已扫描 session 缓存
	scannedSessions map[string]ScannedSession
}

// NewSweeper 创建 Sweeper 实例。
// Python: Sweeper.__init__(sessions_dir, output_dir, mode, language)
func NewSweeper(sessionsDir, outputDir, mode, language string) *Sweeper {
	return &Sweeper{
		sessionsDir:     sessionsDir,
		outputDir:       outputDir,
		mode:            mode,
		language:        language,
		dreamsDir:       filepath.Join(outputDir, ".dreams"),
		scannedSessions: make(map[string]ScannedSession),
	}
}

// Init 创建目录并加载 checkpoint。
// Python: Sweeper.init()
func (s *Sweeper) Init() error {
	if err := os.MkdirAll(s.dreamsDir, 0o755); err != nil {
		return fmt.Errorf("创建 dreams 目录失败: %w", err)
	}
	if err := os.MkdirAll(s.outputDir, 0o755); err != nil {
		return fmt.Errorf("创建 output 目录失败: %w", err)
	}
	cp := s.loadCheckpoint()
	raw, _ := cp.ScannedSessions // 可能 nil
	if raw != nil {
		s.scannedSessions = raw
	} else {
		s.scannedSessions = make(map[string]ScannedSession)
	}
	return nil
}
```

- [ ] **Step 2: 实现 RunSweep — 完整管线**

对齐 Python `run_sweep()`（行123-193）。

```go
// RunSweep 执行完整管线。由 Orchestrator 调用。
// Python: Sweeper.run_sweep()
func (s *Sweeper) RunSweep(ctx context.Context) error {
	sweepStart := time.Now()

	// ── Scan + Pre-filter + Compression ──
	sessions := s.ScanNewSessions()
	if len(sessions) == 0 {
		logger.Debug(logComponent).Msg("[Sweeper] No eligible session found")
		return nil
	}

	// ── LLM Extraction ──
	existingSummary := s.LoadExistingSummary()
	var allKnowledge []KnowledgeItem
	var succeededIDs []string
	var failedIDs []string

	for _, sess := range sessions {
		items, err := s.extractViaLLM(ctx, sess.CompressedText, existingSummary)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("session_id", sess.SessionID).Msg("[Sweeper] LLM Extraction failed")
			failedIDs = append(failedIDs, sess.SessionID)
			continue
		}
		for i := range items {
			items[i].SourceSessionID = sess.SessionID
		}
		allKnowledge = append(allKnowledge, items...)
		succeededIDs = append(succeededIDs, sess.SessionID)
	}

	// ── Update checkpoint for succeeded sessions ──
	if len(succeededIDs) > 0 {
		sessionsRoot := filepath.Clean(s.sessionsDir)
		for _, sid := range succeededIDs {
			historyPath := filepath.Join(sessionsRoot, sid, "history.json")
			var historyMtime float64
			if info, err := os.Stat(historyPath); err == nil {
				historyMtime = float64(info.ModTime().Unix())
			}
			events := s.parseHistory(filepath.Join(sessionsRoot, sid))
			rounds := s.detectRounds(events)
			s.scannedSessions[sid] = ScannedSession{
				HistoryMtime: historyMtime,
				RoundCount:   len(rounds),
			}
		}
		s.saveCheckpoint()
	}

	// ── Promotion ──
	promoted := 0
	for _, k := range allKnowledge {
		result, err := s.promote(k.Title, k.Content, k.SourceSessionID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("title", k.Title).Msg("[Sweeper] Promotion failed")
			continue
		}
		if result != "" {
			promoted++
		}
	}

	duration := time.Since(sweepStart)
	logger.Info(logComponent).
		Float64("duration_seconds", duration.Seconds()).
		Int("sessions", len(sessions)).
		Int("llm_succeeded", len(succeededIDs)).
		Int("llm_failed", len(failedIDs)).
		Int("extracted", len(allKnowledge)).
		Int("promoted", promoted).
		Msg("[Sweeper] sweep completed")

	return nil
}
```

- [ ] **Step 3: 实现 ScanNewSessions + matchSessionMode + parseHistory + detectRounds + compress**

这 5 个方法对齐 Python 行199-364。

- [ ] **Step 4: 实现 extractViaLLM + getDefaultModels + loadExistingSummary + extractContentStr**

extractViaLLM 对齐 Python 行370-419。内部调用 `getDefaultModels()` 获取模型配置，和 Python 一样内部直接获取。

- [ ] **Step 5: 实现 promote + promoteAgent + promoteCode + parseDreamingEntries**

对齐 Python 行467-530。

- [ ] **Step 6: 实现 loadCheckpoint + saveCheckpoint**

对齐 Python 行556-580。

- [ ] **Step 7: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build -tags test ./internal/swarm/agents/harness/common/memory/dreaming/...`

- [ ] **Step 8: 创建 sweeper_test.go — 全部单元测试**

测试要点：
- `TestLoadDreamingConfig_配置文件enabled`: 从 mock config 读取
- `TestLoadDreamingConfig_环境变量覆盖`: DREAMING_AGENT_ENABLED=true
- `TestLoadDreamingConfig_异常回退`: config 加载失败时返回默认值
- `TestSweeper_NewSweeper`: 构造函数默认值
- `TestSweeper_Init`: 创建目录 + checkpoint 加载
- `TestParseHistory_事件过滤`: 只保留 user/chat.final/assistant
- `TestParseHistory_空文件`: 返回空
- `TestDetectRounds`: user+assistant 配对
- `TestCompress_基本压缩`: user/assistant 内容截取
- `TestCompress_递归截断`: token 超限时递归
- `TestScanNewSessions_增量扫描`: brand_new + incremental
- `TestMatchSessionMode`: metadata.json 中的 mode 匹配
- `TestParseDreamingEntries_标题来源内容`: Markdown 解析
- `TestParseDreamingEntries_空内容`: 返回空
- `TestPromoteAgent_去重`: 标题重复时返回空
- `TestPromoteAgent_淘汰`: 超 maxEntriesAgent 时淘汰
- `TestPromoteCode_hash去重`: 相同内容 hash 去重
- `TestCheckpoint_加载保存`: round-trip
- `TestCheckpoint_异常回退`: 文件不存在时返回空
- `TestLoadExistingSummary_agent模式`: 读 DREAMING.md 标题列表
- `TestLoadExistingSummary_code模式`: 读 consolidated_*.md name 字段

使用 `t.TempDir()` 创建临时目录，不依赖外部文件系统。

- [ ] **Step 9: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/swarm/agents/harness/common/memory/dreaming/... -v -count=1`

- [ ] **Step 10: 提交**

```bash
git add internal/swarm/agents/harness/common/memory/dreaming/
git commit -m "feat(7.20): add Sweeper full pipeline (scan/compress/extract/promote/checkpoint)"
```

---

## Task 4: LLM 提示词模板

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/dreaming/prompts.go`
- Create: `internal/swarm/agents/harness/common/memory/dreaming/prompts_test.go`

Python 参考：`jiuwenswarm/agents/harness/common/memory/dreaming/sweeper.py` 行587-783

- [ ] **Step 1: 创建 prompts.go**

4 套提示词模板（`promptCode` / `promptAgent` / `promptCodeEN` / `promptAgentEN`）+ 4 套系统消息（`sysMsgCode` / `sysMsgAgent` / `sysMsgCodeEN` / `sysMsgAgentEN`）+ 提示词映射表。

**遵循项目规则：提示词必须从 Python 源码直接复制，禁止自行翻译或改写。**

模板变量使用 Go 的 `fmt.Sprintf` 风格 `%s` / `%d` 替代 Python 的 `{existing_knowledge}` / `{compressed_session}` / `{max_items}`。

```go
// promptMap 提示词模板映射：key = (mode, language)。
// Python: self._prompt_map
var promptMap = map[[2]string]string{
	{"code", "zh"}: promptCode,
	{"code", "en"}: promptCodeEN,
	{"agent", "zh"}: promptAgent,
	{"agent", "en"}: promptAgentEN,
}

// sysMsgMap 系统消息映射：key = (mode, language)。
// Python: self._sys_msg_map
var sysMsgMap = map[[2]string]string{
	{"code", "zh"}: "你是技术经验提取助手。严格输出 JSON 数组。",
	{"code", "en"}: "You are a technical knowledge extractor. Output JSON array strictly.",
	{"agent", "zh"}: "你是记忆整理助手。严格输出 JSON 数组。",
	{"agent", "en"}: "You are a memory organizer. Output JSON array strictly.",
}
```

4 个提示词常量：从 Python 源码 `_PROMPT_CODE` / `_PROMPT_AGENT` / `_PROMPT_CODE_EN` / `_PROMPT_AGENT_EN` **逐字复制**，仅将 `{existing_knowledge}` 替换为 `%s`，`{compressed_session}` 替换为 `%s`，`{max_items}` 替换为 `%d`。

- [ ] **Step 2: 创建 prompts_test.go**

测试要点：
- `TestPromptMap_所有key存在`: 4 个组合都有模板
- `TestSysMsgMap_所有key存在`: 4 个组合都有系统消息
- `TestPromptCode_格式化`: 用 FormatPrompt 验证替换后内容完整
- `TestPromptAgent_格式化`: 同上

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/swarm/agents/harness/common/memory/dreaming/... -v -run TestPrompt -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/memory/dreaming/
git commit -m "feat(7.20): add LLM prompt templates (4 sets: code/agent × zh/en)"
```

---

## Task 5: 公共 API (StartDreaming / StopDreaming / GetDreamingOrchestrator)

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/dreaming/api.go`
- Create: `internal/swarm/agents/harness/common/memory/dreaming/api_test.go`

Python 参考：`jiuwenswarm/agents/harness/common/memory/dreaming/__init__.py`

- [ ] **Step 1: 创建 api.go**

对齐 Python `start_dreaming()` / `stop_dreaming()` / `get_dreaming_orchestrator()`。

```go
// orchestrators 全局编排器映射：key=mode, value=*DreamingOrchestrator。
// Python: _orchestrators: dict[str, DreamingOrchestrator] = {}
var orchestrators sync.Map

// GetDreamingOrchestrator 获取指定模式的编排器实例。
// Python: get_dreaming_orchestrator(mode)
func GetDreamingOrchestrator(mode string) *dreaming.DreamingOrchestrator {
	val, ok := orchestrators.Load(mode)
	if !ok {
		return nil
	}
	orch, ok := val.(*dreaming.DreamingOrchestrator)
	if !ok {
		return nil
	}
	return orch
}

// StartDreaming 启动 dreaming 服务。幂等：同 mode 重复调用返回已有实例。
// Python: start_dreaming(sessions_dir, output_dir, mode, busy_checker)
func StartDreaming(ctx context.Context, sessionsDir, outputDir, mode string, busyChecker func() bool) (*dreaming.DreamingOrchestrator, error) {
	// 幂等检查
	if existing := GetDreamingOrchestrator(mode); existing != nil {
		return existing, nil
	}

	cfg := LoadDreamingConfig(mode)
	if !cfg.Enabled {
		logger.Info(logComponent).Str("mode", mode).Msg("[dreaming] mode enabled=false, not started")
		return nil, nil
	}

	sweeper := NewSweeper(sessionsDir, outputDir, mode, "zh")
	if err := sweeper.Init(); err != nil {
		return nil, fmt.Errorf("sweeper init failed: %w", err)
	}

	interval := time.Duration(cfg.IntervalSeconds * float64(time.Second))
	orch := dreaming.NewDreamingOrchestrator(
		func(ctx context.Context) error {
			return sweeper.RunSweep(ctx)
		},
		interval,
		dreaming.WithBusyChecker(busyChecker),
		dreaming.WithName("dreaming-"+mode),
	)

	if err := orch.Start(ctx); err != nil {
		return nil, fmt.Errorf("orchestrator start failed: %w", err)
	}

	orchestrators.Store(mode, orch)
	return orch, nil
}

// StopDreaming 停止 dreaming 服务。幂等：同 mode 重复调用无效。mode 为空时停止所有。
// Python: stop_dreaming(mode)
func StopDreaming(ctx context.Context, mode string) error {
	if mode != "" {
		val, loaded := orchestrators.LoadAndDelete(mode)
		if loaded {
			if orch, ok := val.(*dreaming.DreamingOrchestrator); ok {
				if err := orch.Stop(ctx); err != nil {
					logger.Warn(logComponent).Str("mode", mode).Err(err).Msg("[dreaming] stop exception")
					return err
				}
			}
		}
		return nil
	}
	// mode 为空，停止所有
	orchestrators.Range(func(key, val any) bool {
		orchestrators.Delete(key)
		if orch, ok := val.(*dreaming.DreamingOrchestrator); ok {
			if err := orch.Stop(ctx); err != nil {
				m := key.(string)
				logger.Warn(logComponent).Str("mode", m).Err(err).Msg("[dreaming] stop exception")
			}
		}
		return true
	})
	return nil
}
```

注意：import 中需要 `dreaming "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/dreaming"`。

- [ ] **Step 2: 创建 api_test.go**

测试要点：
- `TestGetDreamingOrchestrator_不存在`: 返回 nil
- `TestStartDreaming_配置disabled`: enabled=false 时返回 nil, nil
- `TestStartDreaming_幂等`: 重复调用返回同一实例
- `TestStopDreaming_幂等`: 重复调用安全
- `TestStopDreaming_空mode停止所有`: 停止所有已注册编排器

使用极短 interval（60s）+ mock sweeper 验证启停行为。注意：测试中不能真正等 120s 初始延迟，需通过直接构造 orchestrator 验证。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/swarm/agents/harness/common/memory/dreaming/... -v -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/memory/dreaming/
git commit -m "feat(7.20): add StartDreaming/StopDreaming/GetDreamingOrchestrator public API"
```

---

## Task 6: DeepAdapter 回填

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_dreaming.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_helpers_test.go`

- [ ] **Step 1: 回填 TryStartDreaming**

将当前骨架实现替换为完整调用链。对齐 Python `JiuWenClawDeepAdapter.try_start_dreaming()`（行5935-5954）。

```go
func (d *DeepAdapter) TryStartDreaming(ctx context.Context, busyChecker func() bool) error {
	// 步骤 1: 已启动则跳过
	if d.dreamingStarted {
		return nil
	}

	// 步骤 2: dreaming 模式未启用则跳过
	if d.dreamingMode == "" {
		logger.Info(logComponent).Msg("dreaming mode not enabled, skipping start")
		return nil
	}

	// 步骤 3: 检查是否忙碌
	if busyChecker != nil && busyChecker() {
		logger.Warn(logComponent).Msg("agent busy, skipping dreaming start")
		return nil
	}

	// 步骤 4: 计算路径（对齐 Python: interface_deep.py:5940-5945）
	sessionsDir := workspace.AgentSessionsDir()
	outputName := "memory"
	if !strings.HasPrefix(d.dreamingMode, "agent") {
		outputName = "coding_memory"
	}
	baseDir := d.getAgentWorkspaceDir()
	outputDir := filepath.Join(baseDir, outputName)

	// 步骤 5: 调用公共 API（对齐 Python: await start_dreaming(...)）
	orch, err := dreamapi.StartDreaming(ctx, sessionsDir, outputDir, d.dreamingMode, busyChecker)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[JiuWenClawDeepAdapter] start_dreaming failed")
		return err
	}
	d.dreamingStarted = orch != nil
	return nil
}
```

- [ ] **Step 2: 回填 TryStopDreaming**

```go
func (d *DeepAdapter) TryStopDreaming(ctx context.Context) error {
	if !d.dreamingStarted {
		return nil
	}

	// 对齐 Python: await stop_dreaming(mode=mode)
	if err := dreamapi.StopDreaming(ctx, d.dreamingMode); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[JiuWenClawDeepAdapter] stop_dreaming failed")
	}
	d.dreamingStarted = false
	return nil
}
```

- [ ] **Step 3: 更新 import**

在 `deep_adapter_dreaming.go` 中添加：
```go
import (
	"context"
	"path/filepath"
	"strings"

	dreamapi "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)
```

- [ ] **Step 4: 删除 ⤵️ 标记**

移除 `// ⤵️ 10.6.13-18: 调用 swarm memory dreaming.startDreaming(...)` 和 `// ⤵️ 10.6.13-18: 调用 swarm memory dreaming.stopDreaming(...)` 注释。

- [ ] **Step 5: 更新已有测试**

在 `deep_adapter_helpers_test.go` 中，更新 TryStartDreaming / TryStopDreaming 测试，验证调用链路正确。由于 `StartDreaming` 内部读配置，测试中需 mock `DREAMING_AGENT_ENABLED` 环境变量或使用 testdata config。

- [ ] **Step 6: 编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && go build -tags test ./internal/swarm/server/adapter/... && go test -tags test ./internal/swarm/server/adapter/... -v -run TestDeepAdapter -count=1`

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/server/adapter/
git commit -m "feat(7.20): backfill DeepAdapter TryStartDreaming/TryStopDreaming with real calls"
```

---

## Task 7: IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 7.20 状态**

将 `| 7.20 | ☐ | Dreaming Orchestrator | 后台记忆整理编排器 |` 改为 `| 7.20 | ✅ | Dreaming Orchestrator + Sweeper | ✅ DreamingOrchestrator + ✅ Sweeper(Scan/Compress/Extract/Promote/Checkpoint) + ✅ 公共API(StartDreaming/StopDreaming) + ✅ DeepAdapter回填 |`

- [ ] **Step 2: 更新 7.24 状态**

将 `| 7.24 | ☐ | Memory Codec | 记忆编解码 |` 改为 `| 7.24 | ✅ | Memory Codec | AES-256-GCM 存储编解码器（AesStorageCodec + keyedProvider + passthrough 容错） |`

- [ ] **Step 3: 更新 10.6.13-18 中 dreaming ⤵️ 标记**

从描述中删除 dreaming 相关的 ⤵️ 标记（因已回填），仅保留其他未回填项。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update 7.20✅ 7.24✅ and remove dreaming ⤵️ markers in IMPLEMENTATION_PLAN"
```

---

## Task 8: doc.go 更新（memory 包目录）

**Files:**
- Modify: `internal/swarm/agents/harness/common/memory/doc.go`

- [ ] **Step 1: 在 doc.go 中添加 dreaming 子包条目**

更新文件目录，添加：
```
//	memory/
//	├── doc.go          # 包文档
//	├── forbidden.go    # 记忆禁止配置与提示词生成
//	└── dreaming/       # 后台记忆整理（Sweeper + 公共 API）
```

- [ ] **Step 2: 提交**

```bash
git add internal/swarm/agents/harness/common/memory/doc.go
git commit -m "docs: update memory doc.go with dreaming subpackage"
```
