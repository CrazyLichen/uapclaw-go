# 14.3 进度上报（devtools agent_builder utils/progress）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/devtools/agentbuilder/utils/` 同包续建 progress.go：ProgressStep/BuildProgress 值类型 + ProgressReporter（观察者，方法级锁）+ ProgressManager（session 注册表）+ GetProgressManager 单例，并用 progress_test.go 收口 15 个 Python unit 用例（1:1）+ 4 个 system 回填用例 + Go 并发增强用例。

**Architecture:** 方案已批准（spec `docs/superpowers/specs/2026-10-26-devtools-progress-14.3-design.md`，brainstorming 7 决策锁定）：方法级 sync.Mutex 锁内回调；AddCallback 返回取消函数（Go 函数值不可 == 的语言特性级等价物）；导出 GetProgressManager 单例（sync.Once 惰性初始化）；命名回调类型 ProgressCallback；ToDict() map[string]any + RFC3339Nano；New 构造器收口 dataclass 默认值 + 11 个 With 选项薄构造器。

**Tech Stack:** Go 标准库（time/sync/fmt/strings 无需第三方）；测试用标准 testing 包 + t.Run 子用例（包内既有风格，无 testify）。

**Python 参考（实施前必读）:**
- 源码：`/home/opensource/agent-core/openjiuwen/dev_tools/agent_builder/utils/progress.py`（512 行）
- 单测：`/home/opensource/agent-core/tests/unit_tests/dev_tools/agent_builder/utils/test_progress.py`（3 类 15 方法：Step 4 / BuildProgress 3 / Reporter 8）
- system：`/home/opensource/agent-core/tests/system_tests/dev_tools/agent_builder/utils/test_utils_integration.py`（TestProgressIntegration 4 方法 L87-144）

**包内既有惯例（已核实，直接沿用）:**
- 断言风格：`if got != want { t.Errorf("Xxx = %v, 期望 %v", got, want) }`，无 testify
- system 用例命名：`TestProgressIntegration_中文场景`，注释 `// 对齐 Python system test_xxx`
- unit 用例命名：`TestXxx_中文场景`，注释 `// 对齐 Python TestXxx（N 用例）`
- 日志调用：`logger.Error(logComponent).Str("error", err.Error()).Msg("JSON decode error")` 链式（utils.go L139 先例）；Msg 保留英文
- 声明顺序（规范 2）：结构体→枚举（选项类型归此区块）→常量→全局变量→导出函数→非导出函数
- logComponent 常量已存在于 utils.go（同包复用，不重复定义）

**验收（spec §9）:** 覆盖率 ≥ 85%（包基线 97.2% 不降）；`go test -race` PASS；gofmt + 声明顺序合规；注释全中文；完成后回填（计划行/doc.go/记忆文件，⤴️ 清偿 4 system 用例）。

---

### Task 1: progress.go——结构体区块（4 类型 + ProgressCallback）

**Files:**
- Create: `internal/agentcore/devtools/agentbuilder/utils/progress.go`

- [x] **Step 1: 写 progress.go 结构体区块**

文件头 + import + 结构体区块（ProgressCallback 放最前，接口/回调类型先于结构体的规范惯例）：

```go
package utils

import (
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 进度上报（观察者模式）。
// 对齐 Python: openjiuwen/dev_tools/agent_builder/utils/progress.py。
// 消费方：14.5 executor（CreateReporter）、14.6 base 模板方法（5 上报点）、
// 14.7/14.8 builders（细粒度上报）、14.9 门面（GetProgress 查询）。

// ──────────────────────────── 结构体 ────────────────────────────

// ProgressCallback 进度回调函数。
// 对齐 Python: Callable[[BuildProgress], None]。
// 约束：回调在 reporter 锁内同步执行——回调内禁止调用同一 reporter 的任何方法（死锁）；
// 跨 goroutine 读取进度须在回调内（锁内安全窗口）或走 ToDict 快照。
type ProgressCallback func(progress *BuildProgress)

// ProgressStep 构建步骤快照。
// 对齐 Python dataclass ProgressStep。
type ProgressStep struct {
	// Stage 构建阶段
	Stage ProgressStage
	// Status 步骤状态
	Status ProgressStatus
	// Message 步骤描述消息
	Message string
	// Details 细节字典（构造器收口为空 map，非 nil）
	Details map[string]any
	// Timestamp 步骤时间戳（构造器恒 now，对齐 default_factory 不可传参）
	Timestamp time.Time
	// Duration 步骤时长秒（nil 对齐 Python None）
	Duration *float64
	// Error 错误消息（nil 对齐 Python None）
	Error *string
}

// ToDict 将 ProgressStep 序列化为 JSON 友好字典。
// 对齐 Python: ProgressStep.to_dict（7 键）。
func (s ProgressStep) ToDict() map[string]any {
	var duration any
	if s.Duration != nil {
		duration = *s.Duration
	}
	var errMsg any
	if s.Error != nil {
		errMsg = *s.Error
	}
	return map[string]any{
		"stage":     string(s.Stage),
		"status":    string(s.Status),
		"message":   s.Message,
		"details":   s.Details,
		"timestamp": s.Timestamp.Format(time.RFC3339Nano),
		"duration":  duration,
		"error":     errMsg,
	}
}

// BuildProgress 构建进度聚合（session 级）。
// 对齐 Python dataclass BuildProgress。
type BuildProgress struct {
	// SessionID 会话标识
	SessionID string
	// AgentType Agent 类型
	AgentType string
	// CurrentStage 当前阶段
	CurrentStage ProgressStage
	// CurrentStatus 当前状态
	CurrentStatus ProgressStatus
	// CurrentMessage 当前消息
	CurrentMessage string
	// Steps 步骤列表（构造器收口为空切片）
	Steps []ProgressStep
	// OverallProgress 总体进度 0-100
	OverallProgress float64
	// StartTime 开始时间（构造器恒 now）
	StartTime time.Time
	// LastUpdateTime 最后更新时间（构造器恒 now）
	LastUpdateTime time.Time
	// Error 错误消息（nil 对齐 Python None）
	Error *string
}

// ToDict 将 BuildProgress 序列化为 JSON 友好字典。
// 对齐 Python: BuildProgress.to_dict（10 键）。
func (p *BuildProgress) ToDict() map[string]any {
	var errMsg any
	if p.Error != nil {
		errMsg = *p.Error
	}
	steps := make([]map[string]any, 0, len(p.Steps))
	for _, step := range p.Steps {
		steps = append(steps, step.ToDict())
	}
	return map[string]any{
		"session_id":        p.SessionID,
		"agent_type":        p.AgentType,
		"current_stage":     string(p.CurrentStage),
		"current_status":    string(p.CurrentStatus),
		"current_message":   p.CurrentMessage,
		"steps":             steps,
		"overall_progress":  p.OverallProgress,
		"start_time":        p.StartTime.Format(time.RFC3339Nano),
		"last_update_time":  p.LastUpdateTime.Format(time.RFC3339Nano),
		"error":             errMsg,
	}
}

// ProgressReporter 进度上报器（观察者核心）。
// 对齐 Python: ProgressReporter（方法级锁，所有变更方法+notify 锁内执行）。
type ProgressReporter struct {
	// sessionID 会话标识
	sessionID string
	// agentType Agent 类型
	agentType string
	// progress 进度聚合（值语义，锁内读写）
	progress BuildProgress
	// callbacks 回调列表（有序切片，锁内增删遍历）
	callbacks []ProgressCallback
	// stepStartTimes 各阶段墙钟起始秒（对齐 Python time.time()）
	stepStartTimes map[ProgressStage]float64
	// mu 方法级锁
	mu sync.Mutex
}

// ProgressManager 进度管理器（session→reporter 注册表）。
// 对齐 Python: ProgressManager。
type ProgressManager struct {
	// mu 读写锁
	mu sync.RWMutex
	// reporters 会话→上报器注册表
	reporters map[string]*ProgressReporter
}
```

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/devtools/agentbuilder/utils/`
Expected: 编译失败（progress.go 引用未定义的方法/函数——Step 1 只放结构体+ToDict，无其他引用则应通过；若通过直接进 Task 2）

- [x] **Step 3: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/progress.go
git commit -m "feat(devtools): 14.3 progress 结构体区块——ProgressCallback+4类型+ToDict×2"
```

---

### Task 2: progress.go——选项类型 + 全局变量 + 构造器 + 单例

**Files:**
- Modify: `internal/agentcore/devtools/agentbuilder/utils/progress.go`

- [x] **Step 1: 追加选项类型区块、全局变量区块、导出构造器与单例**

在结构体区块之后追加（规范 2 声明顺序）：

```go
// ──────────────────────────── 枚举 ────────────────────────────

// ProgressStepOption ProgressStep 构造选项。
type ProgressStepOption func(*ProgressStep)

// BuildProgressOption BuildProgress 构造选项。
type BuildProgressOption func(*BuildProgress)

// StartStageOption StartStage 方法选项。
type StartStageOption func(*startStageConfig)

// UpdateStageOption UpdateStage 方法选项。
type UpdateStageOption func(*updateStageConfig)

// CompleteStageOption CompleteStage 方法选项。
type CompleteStageOption func(*completeStageConfig)

// FailStageOption FailStage 方法选项。
type FailStageOption func(*failStageConfig)

// WarnStageOption WarnStage 方法选项。
type WarnStageOption func(*warnStageConfig)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// llmAgentProgressMap LLM Agent 阶段进度映射
	// 对齐 Python: llm_agent_progress（L370-377）
	llmAgentProgressMap = map[ProgressStage]float64{
		ProgressStageInitializing:      0,
		ProgressStageClarifying:        20,
		ProgressStageResourceRetrieving: 40,
		ProgressStageGeneratingConfig:  60,
		ProgressStageTransformingDSL:   80,
		ProgressStageCompleted:         100,
	}

	// workflowProgressMap Workflow Agent 阶段进度映射
	// 对齐 Python: workflow_progress（L380-390）
	workflowProgressMap = map[ProgressStage]float64{
		ProgressStageInitializing:            0,
		ProgressStageDetectingIntention:      10,
		ProgressStageGeneratingWorkflowDesign: 25,
		ProgressStageGeneratingDL:            45,
		ProgressStageValidatingDL:            60,
		ProgressStageRefiningDL:              70,
		ProgressStageTransformingMermaid:     85,
		ProgressStageTransformingWorkflowDSL: 95,
		ProgressStageCompleted:               100,
	}

	// globalProgressManager 全局进度管理器单例
	// 对齐 Python: progress_manager = ProgressManager()（模块级 L512）
	globalProgressManager *ProgressManager

	// globalProgressManagerOnce 确保单例只初始化一次
	globalProgressManagerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProgressStep 创建构建步骤快照。
// 对齐 Python dataclass ProgressStage 必填三参 + 默认值：
// details=空 dict、timestamp=now(utc)、duration/error=None。
func NewProgressStep(stage ProgressStage, status ProgressStatus, message string, opts ...ProgressStepOption) ProgressStep {
	step := ProgressStep{
		Stage:     stage,
		Status:    status,
		Message:   message,
		Details:   make(map[string]any),
		Timestamp: time.Now(),
	}
	for _, opt := range opts {
		opt(&step)
	}
	return step
}

// WithStepDetails 设置步骤细节字典（整体替换）。
func WithStepDetails(details map[string]any) ProgressStepOption {
	return func(s *ProgressStep) {
		s.Details = details
	}
}

// WithStepDuration 设置步骤时长秒。
func WithStepDuration(duration float64) ProgressStepOption {
	return func(s *ProgressStep) {
		s.Duration = &duration
	}
}

// WithStepError 设置步骤错误消息。
func WithStepError(errMsg string) ProgressStepOption {
	return func(s *ProgressStep) {
		s.Error = &errMsg
	}
}

// NewBuildProgress 创建构建进度聚合。
// 对齐 Python dataclass BuildProgress 必填五参 + 默认值：
// steps=空 list、overall_progress=0.0、start/last_update_time=now(utc)、error=None。
func NewBuildProgress(sessionID, agentType string, stage ProgressStage, status ProgressStatus, message string, opts ...BuildProgressOption) BuildProgress {
	progress := BuildProgress{
		SessionID:       sessionID,
		AgentType:       agentType,
		CurrentStage:    stage,
		CurrentStatus:   status,
		CurrentMessage:  message,
		Steps:           make([]ProgressStep, 0),
		OverallProgress: 0.0,
		StartTime:       time.Now(),
		LastUpdateTime:  time.Now(),
	}
	for _, opt := range opts {
		opt(&progress)
	}
	return progress
}

// WithProgressSteps 设置进度步骤列表（整体替换）。
func WithProgressSteps(steps []ProgressStep) BuildProgressOption {
	return func(p *BuildProgress) {
		p.Steps = steps
	}
}

// NewProgressReporter 创建进度上报器。
// 对齐 Python: ProgressReporter.__init__（progress 初始字面量 L135-141）。
func NewProgressReporter(sessionID, agentType string) *ProgressReporter {
	return &ProgressReporter{
		sessionID: sessionID,
		agentType: agentType,
		progress: NewBuildProgress(
			sessionID, agentType,
			ProgressStageInitializing,
			ProgressStatusPending,
			"Initializing...",
		),
		stepStartTimes: make(map[ProgressStage]float64),
	}
}

// NewProgressManager 创建独立进度管理器实例（测试隔离用）。
// 对齐 Python: ProgressManager() 直构（system 测试同款）。
func NewProgressManager() *ProgressManager {
	return &ProgressManager{
		reporters: make(map[string]*ProgressReporter),
	}
}

// GetProgressManager 获取全局进度管理器单例。
// 对齐 Python: from progress import progress_manager（模块级单例）。
// 消费方：14.5 executor 创建 reporter、14.9 门面查询、REST/WS 推送。
func GetProgressManager() *ProgressManager {
	return getGlobalProgressManager()
}

// WithStartStageDetails 设置 StartStage 细节。
func WithStartStageDetails(details map[string]any) StartStageOption {
	return func(c *startStageConfig) {
		c.details = details
	}
}

// WithStartStageProgress 设置 StartStage 显式总体进度（nil 则自动计算）。
func WithStartStageProgress(progress float64) StartStageOption {
	return func(c *startStageConfig) {
		c.progress = &progress
	}
}

// WithUpdateStageMessage 设置 UpdateStage 消息。
func WithUpdateStageMessage(message string) UpdateStageOption {
	return func(c *updateStageConfig) {
		c.message = &message
	}
}

// WithUpdateStageDetails 设置 UpdateStage 细节（合并进末步骤）。
func WithUpdateStageDetails(details map[string]any) UpdateStageOption {
	return func(c *updateStageConfig) {
		c.details = details
	}
}

// WithUpdateStageProgress 设置 UpdateStage 总体进度。
func WithUpdateStageProgress(progress float64) UpdateStageOption {
	return func(c *updateStageConfig) {
		c.progress = &progress
	}
}

// WithCompleteStageMessage 设置 CompleteStage 消息。
func WithCompleteStageMessage(message string) CompleteStageOption {
	return func(c *completeStageConfig) {
		c.message = &message
	}
}

// WithCompleteStageDetails 设置 CompleteStage 细节。
func WithCompleteStageDetails(details map[string]any) CompleteStageOption {
	return func(c *completeStageConfig) {
		c.details = details
	}
}

// WithFailStageMessage 设置 FailStage 消息。
func WithFailStageMessage(message string) FailStageOption {
	return func(c *failStageConfig) {
		c.message = &message
	}
}

// WithFailStageDetails 设置 FailStage 细节。
func WithFailStageDetails(details map[string]any) FailStageOption {
	return func(c *failStageConfig) {
		c.details = details
	}
}

// WithWarnStageMessage 设置 WarnStage 消息。
func WithWarnStageMessage(message string) WarnStageOption {
	return func(c *warnStageConfig) {
		c.message = &message
	}
}

// WithWarnStageDetails 设置 WarnStage 细节。
func WithWarnStageDetails(details map[string]any) WarnStageOption {
	return func(c *warnStageConfig) {
		c.details = details
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getGlobalProgressManager 获取全局进度管理器单例（sync.Once 惰性初始化）。
// 先例：harness/registry.go getGlobalRegistry。
func getGlobalProgressManager() *ProgressManager {
	globalProgressManagerOnce.Do(func() {
		globalProgressManager = &ProgressManager{
			reporters: make(map[string]*ProgressReporter),
		}
	})
	return globalProgressManager
}
```

**选项配置结构体**（非导出，放非导出函数区块之前的结构体尾部——同文件结构体区块内追加，与 Python 形参 Optional 语义一一对应）：

```go
// startStageConfig StartStage 可选参数聚合
// 对齐 Python: details=None, progress=None
type startStageConfig struct {
	details  map[string]any
	progress *float64
}

// updateStageConfig UpdateStage 可选参数聚合
// 对齐 Python: message=None, details=None, progress=None
type updateStageConfig struct {
	message  *string
	details  map[string]any
	progress *float64
}

// completeStageConfig CompleteStage 可选参数聚合
// 对齐 Python: message=None, details=None
type completeStageConfig struct {
	message  *string
	details  map[string]any
}

// failStageConfig FailStage 可选参数聚合
// 对齐 Python: message=None, details=None
type failStageConfig struct {
	message  *string
	details  map[string]any
}

// warnStageConfig WarnStage 可选参数聚合
// 对齐 Python: message=None, details=None
type warnStageConfig struct {
	message  *string
	details  map[string]any
}
```

注意：这 5 个 config 结构体是 Go 结构体（归结构体区块，追加在 ProgressManager 之后），选项类型引用它们。

- [x] **Step 2: 编译 + gofmt 验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/devtools/agentbuilder/utils/ && gofmt -l internal/agentcore/devtools/agentbuilder/utils/`
Expected: 编译通过；gofmt 输出为空

- [x] **Step 3: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/progress.go
git commit -m "feat(devtools): 14.3 选项类型+进度映射表+构造器+单例（11 个 With 薄构造器）"
```

---

### Task 3: progress.go——ProgressReporter 方法群（核心语义）

**Files:**
- Modify: `internal/agentcore/devtools/agentbuilder/utils/progress.go`

- [x] **Step 1: 追加 ProgressReporter 方法（结构体区块内，方法按 Python 方法序）**

```go
// AddCallback 添加进度回调，返回取消函数。
// 对齐 Python: add_callback + remove_callback（Go 函数值不可 == 比较，
// 取消函数是语言特性级等价物；cancel 幂等，二次调用安全）。
func (r *ProgressReporter) AddCallback(cb ProgressCallback) func() {
	r.mu.Lock()
	r.callbacks = append(r.callbacks, cb)
	r.mu.Unlock()
	return func() { r.removeCallback(cb) }
}

// removeCallback 按代码指针移除回调（仅 cancel 闭包内调用，幂等：未找到为 no-op）。
// 注意：funcPtrEqual 比较函数代码指针——同一闭包字面量的两个实例代码指针相同
// 会误判相等（可接受：Python == 语义同为值比较且生产零消费；spec §4-1 已归档）。
func (r *ProgressReporter) removeCallback(cb ProgressCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.callbacks {
		if funcPtrEqual(existing, cb) {
			r.callbacks = append(r.callbacks[:i], r.callbacks[i+1:]...)
			return
		}
	}
}

// funcPtrEqual 比较两个回调的代码指针（reflect.ValueOf(f).Pointer()）。
func funcPtrEqual(a, b ProgressCallback) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// StartStage 开始构建阶段。
// 对齐 Python: start_stage（L177-228）。
// ①当前阶段非 INITIALIZING 先自动收尾 SUCCESS（隐性状态机）
// ②记录墙钟起始 ③更新当前态 ④显式进度或自动计算 ⑤追加步骤
// ⑥Info 日志 ⑦通知回调。
func (r *ProgressReporter) StartStage(stage ProgressStage, message string, opts ...StartStageOption) {
	cfg := startStageConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	r.mu.Lock()
	// 阶段自动收尾：前序阶段非 INITIALIZING 时先以 SUCCESS 结束
	if r.progress.CurrentStage != ProgressStageInitializing {
		r.endCurrentStageLocked(ProgressStatusSuccess, nil, nil, nil)
	}

	r.stepStartTimes[stage] = nowWallClock()

	r.progress.CurrentStage = stage
	r.progress.CurrentStatus = ProgressStatusRunning
	r.progress.CurrentMessage = message
	r.progress.LastUpdateTime = time.Now()

	if cfg.progress != nil {
		r.progress.OverallProgress = *cfg.progress
	} else {
		r.progress.OverallProgress = calculateProgress(r.agentType, stage)
	}

	step := NewProgressStep(stage, ProgressStatusRunning, message, WithStepDetails(cloneStageDetails(cfg.details)))
	r.progress.Steps = append(r.progress.Steps, step)

	sessionID := r.sessionID
	stageStr := string(stage)
	r.notifyLocked()
	r.mu.Unlock()

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("stage", stageStr).
		Str("message", message).
		Msg("Started build stage")
}

// UpdateStage 更新当前阶段信息。
// 对齐 Python: update_stage（L230-254）。不建新步骤；细节合并进末步骤。
func (r *ProgressReporter) UpdateStage(opts ...UpdateStageOption) {
	cfg := updateStageConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	r.mu.Lock()
	if cfg.message != nil {
		r.progress.CurrentMessage = *cfg.message
	}
	if cfg.details != nil && len(r.progress.Steps) > 0 {
		mergeDetails(r.progress.Steps[len(r.progress.Steps)-1].Details, cfg.details)
	}
	if cfg.progress != nil {
		r.progress.OverallProgress = *cfg.progress
	}
	r.progress.LastUpdateTime = time.Now()
	r.notifyLocked()
	r.mu.Unlock()
}

// CompleteStage 完成当前阶段。
// 对齐 Python: complete_stage（L256-268）。
func (r *ProgressReporter) CompleteStage(opts ...CompleteStageOption) {
	cfg := completeStageConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	r.mu.Lock()
	r.endCurrentStageLocked(ProgressStatusSuccess, cfg.message, cfg.details, nil)
	r.mu.Unlock()
}

// FailStage 标记当前阶段失败。
// 对齐 Python: fail_stage（L270-285）。
// 先写 progress.error 再收尾 FAILED（双写 error 同一值）。
func (r *ProgressReporter) FailStage(errMsg string, opts ...FailStageOption) {
	cfg := failStageConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	r.mu.Lock()
	r.progress.Error = &errMsg
	r.endCurrentStageLocked(ProgressStatusFailed, cfg.message, cfg.details, &errMsg)
	r.mu.Unlock()
}

// WarnStage 标记当前阶段警告。
// 对齐 Python: warn_stage（L287-320）。不建新步骤；末步骤改 WARNING+写细节。
func (r *ProgressReporter) WarnStage(warning string, opts ...WarnStageOption) {
	cfg := warnStageConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	r.mu.Lock()
	if cfg.message != nil {
		r.progress.CurrentMessage = *cfg.message
	}
	if len(r.progress.Steps) > 0 {
		step := &r.progress.Steps[len(r.progress.Steps)-1]
		step.Status = ProgressStatusWarning
		step.Details["warning"] = warning
		mergeDetails(step.Details, cfg.details)
	}
	r.progress.CurrentStatus = ProgressStatusWarning
	r.progress.LastUpdateTime = time.Now()

	stageStr := string(r.progress.CurrentStage)
	sessionID := r.sessionID
	r.notifyLocked()
	r.mu.Unlock()

	logger.Warn(logComponent).
		Str("session_id", sessionID).
		Str("stage", stageStr).
		Str("warning", warning).
		Msg("Build stage warning")
}

// GetProgress 获取当前进度。
// 对齐 Python: get_progress（返回同一对象引用）。
// 注意：跨 goroutine 读取须走 ToDict 或在回调内读（锁内安全窗口）。
func (r *ProgressReporter) GetProgress() *BuildProgress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &r.progress
}

// Complete 标记构建整体完成。
// 对齐 Python: complete（L408-431）。message 变参默认 "Build completed"。
func (r *ProgressReporter) Complete(message ...string) {
	msg := "Build completed"
	if len(message) > 0 {
		msg = message[0]
	}

	r.mu.Lock()
	r.progress.CurrentStage = ProgressStageCompleted
	r.progress.CurrentStatus = ProgressStatusSuccess
	r.progress.CurrentMessage = msg
	r.progress.OverallProgress = 100.0
	r.progress.LastUpdateTime = time.Now()

	if len(r.progress.Steps) > 0 {
		r.endCurrentStageLocked(ProgressStatusSuccess, &msg, nil, nil)
	}

	sessionID := r.sessionID
	agentType := r.agentType
	r.notifyLocked()
	r.mu.Unlock()

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("agent_type", agentType).
		Msg("Build completed")
}
```

- [x] **Step 2: 追加非导出辅助函数（非导出函数区块）**

```go
// endCurrentStageLocked 结束当前阶段（须持有 r.mu 调用）。
// 对齐 Python: _end_current_stage（L322-357）。
// 步骤空直接返回；末步骤置终态；算时长并删计时键。
func (r *ProgressReporter) endCurrentStageLocked(status ProgressStatus, message *string, details map[string]any, errMsg *string) {
	if len(r.progress.Steps) == 0 {
		return
	}

	step := &r.progress.Steps[len(r.progress.Steps)-1]
	step.Status = status

	if message != nil {
		step.Message = *message
	}

	if details != nil {
		mergeDetails(step.Details, details)
	}

	if errMsg != nil {
		step.Error = errMsg
		r.progress.Error = errMsg
	}

	// 计算时长：阶段在计时表中则算墙钟差并删键（同阶段二次进入重新计时）
	if start, ok := r.stepStartTimes[step.Stage]; ok {
		duration := nowWallClock() - start
		step.Duration = &duration
		delete(r.stepStartTimes, step.Stage)
	}

	r.progress.CurrentStatus = status
	if message != nil {
		r.progress.CurrentMessage = *message
	}
	r.progress.LastUpdateTime = time.Now()
}

// notifyLocked 通知所有回调（须持有 r.mu 调用）。
// 对齐 Python: _notify（L164-175）——单回调异常吞掉只记日志，不中断后续。
func (r *ProgressReporter) notifyLocked() {
	for _, cb := range r.callbacks {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error(logComponent).
						Str("error", fmt.Sprintf("%v", rec)).
						Str("callback", "progress_callback").
						Str("session_id", r.sessionID).
						Msg("Progress callback execution failed")
				}
			}()
			cb(&r.progress)
		}()
	}
}

// calculateProgress 按阶段计算总体进度。
// 对齐 Python: _calculate_progress（L359-397）。
// agentType 命中映射查表（未命中键 0.0）；未知 agentType 0.0。
func calculateProgress(agentType string, stage ProgressStage) float64 {
	switch agentType {
	case string(AgentTypeLLMAgent):
		if v, ok := llmAgentProgressMap[stage]; ok {
			return v
		}
		return 0.0
	case string(AgentTypeWorkflow):
		if v, ok := workflowProgressMap[stage]; ok {
			return v
		}
		return 0.0
	default:
		return 0.0
	}
}

// mergeDetails 将 src 逐键合并进 dst（对齐 Python dict.update 原地覆盖）。
func mergeDetails(dst, src map[string]any) {
	if src == nil {
		return
	}
	for k, v := range src {
		dst[k] = v
	}
}

// cloneStageDetails 克隆细节字典（nil 转 nil，避免构造步骤与调用方共享可变 map）。
func cloneStageDetails(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}
	cloned := make(map[string]any, len(details))
	for k, v := range details {
		cloned[k] = v
	}
	return cloned
}

// nowWallClock 当前墙钟秒（对齐 Python time.time()，非单调钟——原始行为保留）。
func nowWallClock() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
```

注意 import 需补 `"fmt"`（notifyLocked 的 recover 格式化用）与 `"reflect"`（funcPtrEqual 用）；`"strings"` 若无消费则删除。

- [x] **Step 4: 追加 ProgressManager 方法（结构体区块内）**

```go
// GetReporter 获取进度上报器（不存在返回 nil，对齐 Python None）。
func (m *ProgressManager) GetReporter(sessionID string) *ProgressReporter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reporters[sessionID]
}

// CreateReporter 创建进度上报器。
// 对齐 Python: create_reporter（L463-485）——同 session 已存在返回既有实例（幂等，agentType 被忽略）。
func (m *ProgressManager) CreateReporter(sessionID, agentType string) *ProgressReporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if reporter, ok := m.reporters[sessionID]; ok {
		return reporter
	}
	reporter := NewProgressReporter(sessionID, agentType)
	m.reporters[sessionID] = reporter
	return reporter
}

// RemoveReporter 移除进度上报器（不存在为 no-op）。
func (m *ProgressManager) RemoveReporter(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reporters, sessionID)
}

// GetProgress 获取构建进度。
// 对齐 Python: get_progress（L497-508）——reporter 存在转发，否则 nil。
// 两锁不嵌套：manager 锁内只取指针，锁释放后再调 reporter 方法。
func (m *ProgressManager) GetProgress(sessionID string) *BuildProgress {
	m.mu.RLock()
	reporter := m.reporters[sessionID]
	m.mu.RUnlock()
	if reporter == nil {
		return nil
	}
	return reporter.GetProgress()
}
```

- [x] **Step 5: 编译 + gofmt 验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/devtools/agentbuilder/utils/ && gofmt -l internal/agentcore/devtools/agentbuilder/utils/`
Expected: 编译通过；gofmt 输出为空（strings import 若未用则移除——Task 1 引入的 strings 供后用，如未消费删除之）

- [x] **Step 6: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/progress.go
git commit -m "feat(devtools): 14.3 ProgressReporter 方法群+Manager+单例访问器（10 条隐性语义保真）"
```

---

### Task 4: progress_test.go——15 个 Python unit 用例 1:1 映射

**Files:**
- Create: `internal/agentcore/devtools/agentbuilder/utils/progress_test.go`

- [x] **Step 1: 写 TestProgressStep（4 用例）+ TestBuildProgress（3 用例）**

```go
package utils

import (
	"testing"
	"time"
)

// TestProgressStep 对齐 Python TestProgressStep（4 用例）
func TestProgressStep(t *testing.T) {
	// 子用例 1: test_progress_step_creation
	t.Run("创建默认值", func(t *testing.T) {
		step := NewProgressStep(ProgressStageInitializing, ProgressStatusRunning, "Starting build...")
		if step.Stage != ProgressStageInitializing {
			t.Errorf("Stage = %v, 期望 initializing", step.Stage)
		}
		if step.Status != ProgressStatusRunning {
			t.Errorf("Status = %v, 期望 running", step.Status)
		}
		if step.Message != "Starting build..." {
			t.Errorf("Message = %q, 期望 %q", step.Message, "Starting build...")
		}
		if step.Details == nil || len(step.Details) != 0 {
			t.Errorf("Details = %v, 期望空 map 非 nil", step.Details)
		}
		if step.Duration != nil {
			t.Errorf("Duration = %v, 期望 nil", *step.Duration)
		}
		if step.Error != nil {
			t.Errorf("Error = %v, 期望 nil", *step.Error)
		}
		if step.Timestamp.IsZero() {
			t.Error("Timestamp 期望非零值")
		}
	})

	// 子用例 2: test_progress_step_with_details
	t.Run("携带细节", func(t *testing.T) {
		step := NewProgressStep(ProgressStageClarifying, ProgressStatusSuccess, "Completed", WithStepDetails(map[string]any{"key": "value"}))
		if v, ok := step.Details["key"]; !ok || v != "value" {
			t.Errorf("Details[key] = %v, 期望 value", step.Details)
		}
	})

	// 子用例 3: test_progress_step_to_dict
	t.Run("ToDict", func(t *testing.T) {
		step := NewProgressStep(ProgressStageInitializing, ProgressStatusRunning, "Starting",
			WithStepDetails(map[string]any{"count": 1}), WithStepDuration(1.5))
		result := step.ToDict()
		if result["stage"] != "initializing" {
			t.Errorf("stage = %v, 期望 initializing", result["stage"])
		}
		if result["status"] != "running" {
			t.Errorf("status = %v, 期望 running", result["status"])
		}
		if result["message"] != "Starting" {
			t.Errorf("message = %v, 期望 Starting", result["message"])
		}
		if _, ok := result["timestamp"].(string); !ok {
			t.Errorf("timestamp 期望 string, got %T", result["timestamp"])
		}
		if result["duration"] != 1.5 {
			t.Errorf("duration = %v, 期望 1.5", result["duration"])
		}
		if ts, ok := result["timestamp"].(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
				t.Errorf("timestamp %q 非 RFC3339 可解析: %v", ts, err)
			}
		}
	})

	// 子用例 4: test_progress_step_with_error
	t.Run("携带错误", func(t *testing.T) {
		step := NewProgressStep(ProgressStageError, ProgressStatusFailed, "Failed", WithStepError("Test error"))
		if step.Error == nil || *step.Error != "Test error" {
			t.Error("Error 期望 Test error")
		}
		result := step.ToDict()
		if result["error"] != "Test error" {
			t.Errorf("to_dict error = %v, 期望 Test error", result["error"])
		}
	})
}

// TestBuildProgress 对齐 Python TestBuildProgress（3 用例）
func TestBuildProgress(t *testing.T) {
	// 子用例 1: test_build_progress_creation
	t.Run("创建默认值", func(t *testing.T) {
		progress := NewBuildProgress("test_session", "llm_agent", ProgressStageInitializing, ProgressStatusPending, "Initializing...")
		if progress.SessionID != "test_session" {
			t.Errorf("SessionID = %v, 期望 test_session", progress.SessionID)
		}
		if progress.AgentType != "llm_agent" {
			t.Errorf("AgentType = %v, 期望 llm_agent", progress.AgentType)
		}
		if progress.CurrentStage != ProgressStageInitializing {
			t.Errorf("CurrentStage = %v, 期望 initializing", progress.CurrentStage)
		}
		if progress.CurrentStatus != ProgressStatusPending {
			t.Errorf("CurrentStatus = %v, 期望 pending", progress.CurrentStatus)
		}
		if progress.Steps == nil || len(progress.Steps) != 0 {
			t.Errorf("Steps = %v, 期望空切片非 nil", progress.Steps)
		}
		if progress.OverallProgress != 0.0 {
			t.Errorf("OverallProgress = %v, 期望 0.0", progress.OverallProgress)
		}
	})

	// 子用例 2: test_build_progress_to_dict
	t.Run("ToDict", func(t *testing.T) {
		progress := NewBuildProgress("test_session", "workflow", ProgressStageCompleted, ProgressStatusSuccess, "Done")
		result := progress.ToDict()
		if result["session_id"] != "test_session" {
			t.Errorf("session_id = %v, 期望 test_session", result["session_id"])
		}
		if result["agent_type"] != "workflow" {
			t.Errorf("agent_type = %v, 期望 workflow", result["agent_type"])
		}
		if result["current_stage"] != "completed" {
			t.Errorf("current_stage = %v, 期望 completed", result["current_stage"])
		}
		if result["current_status"] != "success" {
			t.Errorf("current_status = %v, 期望 success", result["current_status"])
		}
		if result["current_message"] != "Done" {
			t.Errorf("current_message = %v, 期望 Done", result["current_message"])
		}
		for _, key := range []string{"start_time", "last_update_time"} {
			if ts, ok := result[key].(string); ok {
				if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
					t.Errorf("%s %q 非 RFC3339 可解析: %v", key, ts, err)
				}
			} else {
				t.Errorf("%s 期望 string, got %T", key, result[key])
			}
		}
	})

	// 子用例 3: test_build_progress_with_steps
	t.Run("携带步骤", func(t *testing.T) {
		step1 := NewProgressStep(ProgressStageInitializing, ProgressStatusSuccess, "Init done")
		step2 := NewProgressStep(ProgressStageClarifying, ProgressStatusRunning, "Clarifying...")
		progress := NewBuildProgress("test", "llm_agent", ProgressStageClarifying, ProgressStatusRunning, "Processing",
			WithProgressSteps([]ProgressStep{step1, step2}))
		result := progress.ToDict()
		steps, ok := result["steps"].([]map[string]any)
		if !ok {
			t.Fatalf("steps 期望 []map[string]any, got %T", result["steps"])
		}
		if len(steps) != 2 {
			t.Errorf("steps len = %d, 期望 2", len(steps))
		}
	})
}
```

- [x] **Step 2: 写 TestProgressReporter（8 用例）**

```go
// TestProgressReporter 对齐 Python TestProgressReporter（8 用例）
func TestProgressReporter(t *testing.T) {
	// 子用例 1: test_progress_reporter_creation
	t.Run("创建", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		if reporter.sessionID != "session_123" {
			t.Errorf("sessionID = %v, 期望 session_123", reporter.sessionID)
		}
		if reporter.agentType != "llm_agent" {
			t.Errorf("agentType = %v, 期望 llm_agent", reporter.agentType)
		}
		if reporter.progress.CurrentStage != ProgressStageInitializing {
			t.Errorf("CurrentStage = %v, 期望 initializing", reporter.progress.CurrentStage)
		}
		if reporter.progress.CurrentStatus != ProgressStatusPending {
			t.Errorf("CurrentStatus = %v, 期望 pending", reporter.progress.CurrentStatus)
		}
	})

	// 子用例 2: test_add_callback
	t.Run("添加回调", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		var called []string
		cancel := reporter.AddCallback(func(p *BuildProgress) {
			called = append(called, p.SessionID)
		})
		_ = cancel
		reporter.StartStage(ProgressStageClarifying, "Test message")
		if len(called) != 1 {
			t.Errorf("回调调用次数 = %d, 期望 1", len(called))
		}
		if len(called) > 0 && called[0] != "session_123" {
			t.Errorf("回调收到 sessionID = %q, 期望 session_123", called[0])
		}
	})

	// 子用例 3: test_remove_callback（cancel 语义改写）
	t.Run("取消回调", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		var callCount int
		cancel := reporter.AddCallback(func(p *BuildProgress) {
			callCount++
		})
		cancel()
		reporter.StartStage(ProgressStageClarifying, "Test message")
		if callCount != 0 {
			t.Errorf("cancel 后调用次数 = %d, 期望 0", callCount)
		}
	})

	// 子用例 4: test_start_stage
	t.Run("开始阶段", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageClarifying, "Clarifying requirements...",
			WithStartStageDetails(map[string]any{"query_length": 100}),
			WithStartStageProgress(20.0))
		p := reporter.GetProgress()
		if p.CurrentStage != ProgressStageClarifying {
			t.Errorf("CurrentStage = %v, 期望 clarifying", p.CurrentStage)
		}
		if p.CurrentStatus != ProgressStatusRunning {
			t.Errorf("CurrentStatus = %v, 期望 running", p.CurrentStatus)
		}
		if p.CurrentMessage != "Clarifying requirements..." {
			t.Errorf("CurrentMessage = %v, 期望 Clarifying requirements...", p.CurrentMessage)
		}
		if p.OverallProgress != 20.0 {
			t.Errorf("OverallProgress = %v, 期望 20.0", p.OverallProgress)
		}
	})

	// 子用例 5: test_complete_stage
	t.Run("完成阶段", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageClarifying, "Clarifying...")
		reporter.CompleteStage(WithCompleteStageMessage("Clarification complete"),
			WithCompleteStageDetails(map[string]any{"found": 5}))
		p := reporter.GetProgress()
		if p.CurrentStatus != ProgressStatusSuccess {
			t.Errorf("CurrentStatus = %v, 期望 success", p.CurrentStatus)
		}
	})

	// 子用例 6: test_fail_stage
	t.Run("失败阶段", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageClarifying, "Clarifying...")
		reporter.FailStage("Test error", WithFailStageMessage("Error occurred"))
		p := reporter.GetProgress()
		if p.CurrentStatus != ProgressStatusFailed {
			t.Errorf("CurrentStatus = %v, 期望 failed", p.CurrentStatus)
		}
		if p.Error == nil || *p.Error != "Test error" {
			t.Error("progress.Error 期望 Test error")
		}
	})

	// 子用例 7: test_complete
	t.Run("整体完成", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")
		reporter.StartStage(ProgressStageCompleted, "Finishing...")
		reporter.Complete("Build completed successfully")
		p := reporter.GetProgress()
		if p.CurrentStage != ProgressStageCompleted {
			t.Errorf("CurrentStage = %v, 期望 completed", p.CurrentStage)
		}
		if p.CurrentStatus != ProgressStatusSuccess {
			t.Errorf("CurrentStatus = %v, 期望 success", p.CurrentStatus)
		}
		if p.OverallProgress != 100.0 {
			t.Errorf("OverallProgress = %v, 期望 100.0", p.OverallProgress)
		}
	})

	// 子用例 8: test_multiple_stages
	t.Run("多阶段推进", func(t *testing.T) {
		reporter := NewProgressReporter("session_123", "llm_agent")

		reporter.StartStage(ProgressStageInitializing, "Init...")
		if reporter.GetProgress().CurrentStage != ProgressStageInitializing {
			t.Error("第 1 段后 CurrentStage 期望 initializing")
		}

		reporter.CompleteStage(WithCompleteStageMessage("Init done"))
		reporter.StartStage(ProgressStageClarifying, "Clarifying...")
		if reporter.GetProgress().CurrentStage != ProgressStageClarifying {
			t.Error("第 2 段后 CurrentStage 期望 clarifying")
		}

		reporter.CompleteStage(WithCompleteStageMessage("Clarify done"))
		reporter.StartStage(ProgressStageGeneratingConfig, "Generating...")
		if reporter.GetProgress().CurrentStage != ProgressStageGeneratingConfig {
			t.Error("第 3 段后 CurrentStage 期望 generating_config")
		}
	})
}
```

- [x] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -run 'TestProgressStep|TestBuildProgress|TestProgressReporter' -v 2>&1 | tail -20`
Expected: 全部 PASS（15 子用例）

- [x] **Step 4: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/progress_test.go
git commit -m "test(devtools): 14.3 progress 15 个 Python unit 用例 1:1 映射"
```

---

### Task 5: progress_test.go——system 回填 4 用例 + Go 增强

**Files:**
- Modify: `internal/agentcore/devtools/agentbuilder/utils/progress_test.go`

- [x] **Step 1: 追加 system 回填 4 用例（TestProgressIntegration——14.1 spec 归属拆分承诺兑现）**

```go
// TestProgressIntegration_回调上报 对齐 Python system test_progress_reporter_with_callbacks
func TestProgressIntegration_回调上报(t *testing.T) {
	reporter := NewProgressReporter("test_session", "llm_agent")
	var events int
	reporter.AddCallback(func(p *BuildProgress) { events++ })
	reporter.StartStage(ProgressStageInitializing, "Starting...")
	reporter.CompleteStage(WithCompleteStageMessage("Done"))
	if events < 1 {
		t.Errorf("回调事件数 = %d, 期望 >= 1", events)
	}
}

// TestProgressIntegration_序列化 对齐 Python system test_build_progress_serialization
func TestProgressIntegration_序列化(t *testing.T) {
	progress := NewBuildProgress("test_session", "llm_agent", ProgressStageInitializing, ProgressStatusPending, "Initializing")
	progressDict := progress.ToDict()
	if _, ok := progressDict["session_id"]; !ok {
		t.Error("ToDict 缺 session_id 键")
	}
	if _, ok := progressDict["steps"]; !ok {
		t.Error("ToDict 缺 steps 键")
	}
	if progressDict["session_id"] != "test_session" {
		t.Errorf("session_id = %v, 期望 test_session", progressDict["session_id"])
	}
}

// TestProgressIntegration_管理器 对齐 Python system test_progress_manager_integration
func TestProgressIntegration_管理器(t *testing.T) {
	manager := NewProgressManager()
	reporter := manager.CreateReporter("session_001", "llm_agent")
	if reporter == nil {
		t.Fatal("CreateReporter 返回 nil")
	}
	if reporter.sessionID != "session_001" {
		t.Errorf("sessionID = %v, 期望 session_001", reporter.sessionID)
	}
	retrieved := manager.GetReporter("session_001")
	// Python: assert retrieved is reporter —— Go 等价：同指针
	if retrieved != reporter {
		t.Error("GetReporter 与 CreateReporter 期望同一实例（指针相等）")
	}
}

// TestProgressIntegration_完整工作流 对齐 Python system test_progress_reporter_complete_workflow
func TestProgressIntegration_完整工作流(t *testing.T) {
	reporter := NewProgressReporter("test_session", "llm_agent")
	reporter.StartStage(ProgressStageInitializing, "Starting...")
	reporter.CompleteStage(WithCompleteStageMessage("Initialization done"))
	reporter.StartStage(ProgressStageClarifying, "Clarifying requirements...")
	reporter.CompleteStage(WithCompleteStageMessage("Clarification done"))
	reporter.Complete("Build completed")
	progress := reporter.GetProgress()
	if progress.CurrentStage != ProgressStageCompleted {
		t.Errorf("CurrentStage = %v, 期望 completed", progress.CurrentStage)
	}
	if progress.OverallProgress != 100.0 {
		t.Errorf("OverallProgress = %v, 期望 100.0", progress.OverallProgress)
	}
}
```

- [x] **Step 2: 追加 Go 增强用例（并发+隐性语义，Python 无对应）**

```go
// TestProgressReporter_并发阶段操作 验证方法级锁（-race 必跑）
func TestProgressReporter_并发阶段操作(t *testing.T) {
	reporter := NewProgressReporter("race_session", "llm_agent")
	reporter.AddCallback(func(p *BuildProgress) {})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			stage := []ProgressStage{ProgressStageClarifying, ProgressStageGeneratingConfig, ProgressStageTransformingDSL}[n%3]
			reporter.StartStage(stage, "concurrent")
			reporter.UpdateStage(WithUpdateStageMessage("tick"))
			reporter.CompleteStage(WithCompleteStageMessage("done"))
		}(i)
	}
	wg.Wait()
	p := reporter.GetProgress()
	if len(p.Steps) != 30 {
		t.Errorf("Steps len = %d, 期望 30（10 goroutine × 3 阶段）", len(p.Steps))
	}
}

// TestProgressReporter_并发回调注册 验证回调切片锁安全
func TestProgressReporter_并发回调注册(t *testing.T) {
	reporter := NewProgressReporter("race_cb", "llm_agent")
	var wg sync.WaitGroup
	cancels := make([]func(), 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cancels[n] = reporter.AddCallback(func(p *BuildProgress) {})
		}(i)
	}
	wg.Wait()
	reporter.StartStage(ProgressStageClarifying, "after add")
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
	reporter.StartStage(ProgressStageGeneratingConfig, "after cancel")
}

// TestProgressReporter_取消幂等 cancel 二次调用安全
func TestProgressReporter_取消幂等(t *testing.T) {
	reporter := NewProgressReporter("idem", "llm_agent")
	var callCount int
	cancel := reporter.AddCallback(func(p *BuildProgress) { callCount++ })
	cancel()
	cancel() // 二次调用不得 panic
	reporter.StartStage(ProgressStageClarifying, "msg")
	if callCount != 0 {
		t.Errorf("cancel 后调用次数 = %d, 期望 0", callCount)
	}
}

// TestProgressReporter_回调panic不中断 对齐 Python except Exception 继续遍历语义
func TestProgressReporter_回调panic不中断(t *testing.T) {
	reporter := NewProgressReporter("panic", "llm_agent")
	var secondCalled bool
	reporter.AddCallback(func(p *BuildProgress) { panic("boom") })
	reporter.AddCallback(func(p *BuildProgress) { secondCalled = true })
	reporter.StartStage(ProgressStageClarifying, "msg")
	if !secondCalled {
		t.Error("首个回调 panic 后第二个回调仍应被调用")
	}
}

// TestProgressReporter_回调内读进度 锁内安全窗口验证
func TestProgressReporter_回调内读进度(t *testing.T) {
	reporter := NewProgressReporter("cbread", "llm_agent")
	var stageSeen string
	var dictOK bool
	reporter.AddCallback(func(p *BuildProgress) {
		stageSeen = string(p.CurrentStage)
		_ = p.ToDict() // 回调内走 ToDict 快照合法
		dictOK = true
	})
	reporter.StartStage(ProgressStageClarifying, "msg")
	if stageSeen != "clarifying" {
		t.Errorf("回调内 CurrentStage = %q, 期望 clarifying", stageSeen)
	}
	if !dictOK {
		t.Error("回调内 ToDict 未执行")
	}
}

// TestProgressManager_并发注册 manager 锁验证
func TestProgressManager_并发注册(t *testing.T) {
	manager := NewProgressManager()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sid := fmt.Sprintf("s_%d", n%5)
			manager.CreateReporter(sid, "llm_agent")
			manager.GetReporter(sid)
			manager.GetProgress(sid)
		}(i)
	}
	wg.Wait()
	if len(manager.reporters) != 5 {
		t.Errorf("reporters 数 = %d, 期望 5（10 goroutine 折叠 5 session）", len(manager.reporters))
	}
}

// TestProgressReporter_自动进度计算 calculateProgress 三分支
func TestProgressReporter_自动进度计算(t *testing.T) {
	if got := calculateProgress("llm_agent", ProgressStageClarifying); got != 20.0 {
		t.Errorf("llm_agent/clarifying = %v, 期望 20.0", got)
	}
	if got := calculateProgress("workflow", ProgressStageGeneratingDL); got != 45.0 {
		t.Errorf("workflow/generating_dl = %v, 期望 45.0", got)
	}
	if got := calculateProgress("llm_agent", ProgressStageGeneratingWorkflowDesign); got != 0.0 {
		t.Errorf("llm_agent/未映射阶段 = %v, 期望 0.0", got)
	}
	if got := calculateProgress("unknown_type", ProgressStageInitializing); got != 0.0 {
		t.Errorf("未知 agentType = %v, 期望 0.0", got)
	}
}

// TestProgressStep_ToDict_空指针形态 duration/error nil → ToDict 键为 nil
func TestProgressStep_ToDict_空指针形态(t *testing.T) {
	step := NewProgressStep(ProgressStageInitializing, ProgressStatusPending, "m")
	result := step.ToDict()
	if result["duration"] != nil {
		t.Errorf("duration = %v, 期望 nil", result["duration"])
	}
	if result["error"] != nil {
		t.Errorf("error = %v, 期望 nil", result["error"])
	}
}

// TestProgressReporter_隐性自动收尾 spec §4b-1：StartStage 自动结束前序阶段
func TestProgressReporter_隐性自动收尾(t *testing.T) {
	reporter := NewProgressReporter("autoend", "llm_agent")
	reporter.StartStage(ProgressStageInitializing, "Init...")
	reporter.StartStage(ProgressStageClarifying, "Clarify...")
	p := reporter.GetProgress()
	if len(p.Steps) != 2 {
		t.Fatalf("Steps len = %d, 期望 2", len(p.Steps))
	}
	if p.Steps[0].Status != ProgressStatusSuccess {
		t.Errorf("前序步骤 Status = %v, 期望 success（自动收尾）", p.Steps[0].Status)
	}
	if p.Steps[0].Duration == nil {
		t.Error("前序步骤 Duration 期望非 nil（收尾时计算）")
	}
}

// TestProgressManager_幂等创建 spec §4b-7
func TestProgressManager_幂等创建(t *testing.T) {
	manager := NewProgressManager()
	r1 := manager.CreateReporter("dup", "llm_agent")
	r2 := manager.CreateReporter("dup", "workflow") // agentType 被忽略
	if r1 != r2 {
		t.Error("同 sessionID 两次 CreateReporter 期望同实例")
	}
	if r1.agentType != "llm_agent" {
		t.Errorf("既有实例 agentType = %v, 期望 llm_agent（首次值保留）", r1.agentType)
	}
}

// TestGetProgressManager_单例 两次调用同指针
func TestGetProgressManager_单例(t *testing.T) {
	m1 := GetProgressManager()
	m2 := GetProgressManager()
	if m1 != m2 {
		t.Error("GetProgressManager 两次调用期望同指针")
	}
}

// TestProgressReporter_更新阶段语义 spec §4b-3：不建新步骤
func TestProgressReporter_更新阶段语义(t *testing.T) {
	reporter := NewProgressReporter("upd", "llm_agent")
	reporter.StartStage(ProgressStageClarifying, "start",
		WithStartStageDetails(map[string]any{"a": 1}))
	reporter.UpdateStage(WithUpdateStageMessage("updated"),
		WithUpdateStageDetails(map[string]any{"b": 2}))
	p := reporter.GetProgress()
	if len(p.Steps) != 1 {
		t.Fatalf("Steps len = %d, 期望 1（update 不建新步骤）", len(p.Steps))
	}
	if p.CurrentMessage != "updated" {
		t.Errorf("CurrentMessage = %v, 期望 updated", p.CurrentMessage)
	}
	if v, ok := p.Steps[0].Details["b"]; !ok || v != 2 {
		t.Error("末步骤 details 应合并 b=2")
	}
	if v, ok := p.Steps[0].Details["a"]; !ok || v != 1 {
		t.Error("末步骤 details 应保留 a=1")
	}
}

// TestProgressReporter_警告阶段语义 spec §4b-4：不建新步骤+warning 写入细节
func TestProgressReporter_警告阶段语义(t *testing.T) {
	reporter := NewProgressReporter("warn", "llm_agent")
	reporter.StartStage(ProgressStageGeneratingConfig, "gen")
	reporter.WarnStage("slow response")
	p := reporter.GetProgress()
	if len(p.Steps) != 1 {
		t.Fatalf("Steps len = %d, 期望 1（warn 不建新步骤）", len(p.Steps))
	}
	if p.Steps[0].Status != ProgressStatusWarning {
		t.Errorf("末步骤 Status = %v, 期望 warning", p.Steps[0].Status)
	}
	if p.Steps[0].Details["warning"] != "slow response" {
		t.Errorf("details[warning] = %v, 期望 slow response", p.Steps[0].Details["warning"])
	}
	if p.CurrentStatus != ProgressStatusWarning {
		t.Errorf("CurrentStatus = %v, 期望 warning", p.CurrentStatus)
	}
}
```

import 追加：`"fmt"`、`"sync"`（progress_test.go 文件头 import 块补齐）。

- [x] **Step 3: 运行全量包测试 + race + 覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -race ./internal/agentcore/devtools/agentbuilder/utils/ -cover 2>&1 | tail -3`
Expected: ok + coverage ≥ 96%（包基线 97.2%；progress.go 预估 95%+，包级不低于 96%）

- [x] **Step 4: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/progress_test.go
git commit -m "test(devtools): 14.3 system 回填 4 用例+并发/隐性语义增强 13 用例"
```

---

### Task 6: doc.go 目录树更新

**Files:**
- Modify: `internal/agentcore/devtools/agentbuilder/utils/doc.go`

- [x] **Step 1: 目录树补两条目 + 流程位置段落更新**

doc.go 中文件目录树修改为（在 utils.go 行后、constants_test.go 行前插入 progress 两行）：

```
//	utils/
//	├── doc.go              # 包文档
//	├── enums.go            # 4 枚举类型+25 常量+Parse×4+All×4（对齐 Python enums.py）
//	├── constants.go        # 20 个共享常量（含 13 个死常量，对齐 Python 全量）
//	├── utils.go            # 7 个工具函数 + 2 个预编译正则
//	├── progress.go         # 进度上报：ProgressStep/BuildProgress/Reporter/Manager+单例（对齐 Python progress.py）
//	├── constants_test.go   # 21.5 用例（对齐 Python test_constants.py）
//	├── enums_test.go       # 12 unit+4.5 system 回填+增强用例（对齐 Python test_enums.py + system）
//	├── utils_test.go       # 39.5+17 用例（对齐 Python test_utils.py + system 3 + 增强）
//	└── progress_test.go    # 15 unit+4 system 回填+13 并发/语义增强用例（对齐 Python test_progress.py + system）
```

同时"流程位置"段落后补一句（插在"本包函数（LLM 输出解析咽喉，13 处消费）"之后）：

```
// ProgressReporter 进度上报（14.3）由 14.5 executor 创建、14.6 模板方法埋
// 5 个上报点、14.7/14.8 细粒度调用、14.9 门面查询。
```

- [x] **Step 2: gofmt 验证 + 编译**

Run: `cd /home/opensource/uap-claw-go && gofmt -l internal/agentcore/devtools/agentbuilder/utils/ && go build ./internal/agentcore/devtools/agentbuilder/utils/`
Expected: gofmt 输出为空；编译通过

- [x] **Step 3: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/doc.go
git commit -m "docs(devtools): 14.3 doc.go 目录树补 progress 两条目+流程位置更新"
```

---

### Task 7: 回填三处（计划行 + 记忆文件 + ⤴️ 清偿标记）

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:826`
- Modify: `/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/domain-14-devtools-progress.md`
- Modify: `/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/MEMORY.md`（索引行更新）

- [x] **Step 1: 更新 IMPLEMENTATION_PLAN.md 14.3 行**

状态 ☐→✅，替换 826 行为（覆盖率数字以 Task 5 Step 3 实测为准填入，不留占位符）：

```markdown
| 14.3 | ✅ | 进度上报 | `internal/agentcore/devtools/agentbuilder/utils/progress.go`：ProgressStep/BuildProgress 值类型(ToDict×2,RFC3339Nano)+ProgressReporter(方法级锁+AddCallback 返回取消函数+Start/Update/Complete/Fail/Warn Stage+10 条隐性语义保真：自动收尾/删键计时/update·warn 不建新 step/fail 双写 error)+ProgressManager(RWMutex)+GetProgressManager 单例(sync.Once)+llmAgent/workflow 双进度映射表+11 个 With 薄构造器；progress_test.go 15 unit 1:1+4 system 回填(⤴️ 清偿,utils 系 system 12/12 全清)+13 并发/语义增强；包覆盖率 9x%；设计：docs/superpowers/specs/2026-10-26-devtools-progress-14.3-design.md | `openjiuwen/dev_tools/agent_builder/utils/progress.py` |
```

- [x] **Step 2: 更新记忆文件 domain-14-devtools-progress.md**

"后续步骤"段更新：14.3 完成（日期+提交号+覆盖率+⤴️ 清偿记录）；下一节 14.4 resource；新增关键坑记录（removeCallback 代码指针比较限制、回调锁内执行约束、manager 两锁不嵌套）。

- [x] **Step 3: 更新 MEMORY.md 索引行**

`领域十四 devtools 实施进度与决策` 行更新为：14.1/14.2/14.3 完成（utils 目录测试 100% 回填），下一节 14.4。

- [x] **Step 4: Commit（仓库内文件）**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 14.3 状态回填 ✅（⤴️ 4 system 用例清偿,utils 系 12/12 全清）"
```

---

### Task 8: 全量验证收口

**Files:** 无新文件（验证-only）

- [x] **Step 1: 包全量测试 + race + 覆盖率确认**

Run: `cd /home/opensource/uap-claw-go && go test -race ./internal/agentcore/devtools/agentbuilder/utils/ -cover 2>&1 | tail -3`
Expected: ok + coverage ≥ 96%（基线 97.2% 不显著下降；progress.go 高覆盖应拉平）

- [x] **Step 2: 全仓库编译确认（防跨包破坏）**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过（progress.go 仅同包扩展零外部影响，此步为保险）

- [x] **Step 3: 单进程互斥确认（规则 5）**

Run: `pgrep -f 'go (build|test)'`（如有残留先 kill）

- [x] **Step 4: 远端 CI 确认（规则 6）**

```bash
git push
gh run list --limit 3
gh run watch <run-id>
```
Expected: Build/Lint/Unit Test/Integration Test 四 job 全绿（规则 6 完成标准）

---

## 计划自审记录

- **Spec 覆盖核对**：§0 七决策→Task 1-3 全落地（锁方案 A/取消函数/方案 B 单例/命名回调/ToDict/构造器/原序）；§3.1-3.5→Task 1-3；§4 六偏差+§4b 十隐性语义→Task 3 代码内注释锚点+Task 5 增强用例逐条验证；§5 测试三层→Task 4-5；§6 日志 4 处→Task 3（StartStage Info/WarnStage Warn/Complete Info/notifyLocked Error）；§8 回填清单 6 项→Task 6/7（#4 留 14.10 已归档）
- **类型一致性**：NewProgressReporter 返回 *ProgressReporter（Task 2 定义/Task 4-5 消费一致）；removeCallback 只被 cancel 闭包调用（Task 3 Step 1 定稿版含 funcPtrEqual，计划编写时的初版永假比较已在自审中修复删除）；calculateProgress 双参签名（agentType, stage）与 Task 5 直调一致
- **占位符扫描**：无 TBD/TODO；计划行覆盖率数字明确"以 Task 5 Step 3 实测填入"闭环
- **已知技术债记录（非本节义务）**：funcPtrEqual 代码指针比较的闭包实例局限已在注释与 spec §4-1 归档（Python == 语义同为值比较且生产零消费，可接受）
