package utils

import (
	"sync"
	"time"
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
		"session_id":       p.SessionID,
		"agent_type":       p.AgentType,
		"current_stage":    string(p.CurrentStage),
		"current_status":   string(p.CurrentStatus),
		"current_message":  p.CurrentMessage,
		"steps":            steps,
		"overall_progress": p.OverallProgress,
		"start_time":       p.StartTime.Format(time.RFC3339Nano),
		"last_update_time": p.LastUpdateTime.Format(time.RFC3339Nano),
		"error":            errMsg,
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

// startStageConfig StartStage 可选参数聚合。
// 对齐 Python: details=None, progress=None。
type startStageConfig struct {
	details  map[string]any
	progress *float64
}

// updateStageConfig UpdateStage 可选参数聚合。
// 对齐 Python: message=None, details=None, progress=None。
type updateStageConfig struct {
	message  *string
	details  map[string]any
	progress *float64
}

// completeStageConfig CompleteStage 可选参数聚合。
// 对齐 Python: message=None, details=None。
type completeStageConfig struct {
	message *string
	details map[string]any
}

// failStageConfig FailStage 可选参数聚合。
// 对齐 Python: message=None, details=None。
type failStageConfig struct {
	message *string
	details map[string]any
}

// warnStageConfig WarnStage 可选参数聚合。
// 对齐 Python: message=None, details=None。
type warnStageConfig struct {
	message *string
	details map[string]any
}

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
	// llmAgentProgressMap LLM Agent 阶段进度映射。
	// 对齐 Python: llm_agent_progress（L370-377）。
	llmAgentProgressMap = map[ProgressStage]float64{
		ProgressStageInitializing:       0,
		ProgressStageClarifying:         20,
		ProgressStageResourceRetrieving: 40,
		ProgressStageGeneratingConfig:   60,
		ProgressStageTransformingDSL:    80,
		ProgressStageCompleted:          100,
	}

	// workflowProgressMap Workflow Agent 阶段进度映射。
	// 对齐 Python: workflow_progress（L380-390）。
	workflowProgressMap = map[ProgressStage]float64{
		ProgressStageInitializing:             0,
		ProgressStageDetectingIntention:       10,
		ProgressStageGeneratingWorkflowDesign: 25,
		ProgressStageGeneratingDL:             45,
		ProgressStageValidatingDL:             60,
		ProgressStageRefiningDL:               70,
		ProgressStageTransformingMermaid:      85,
		ProgressStageTransformingWorkflowDSL:  95,
		ProgressStageCompleted:                100,
	}

	// globalProgressManager 全局进度管理器单例。
	// 对齐 Python: progress_manager = ProgressManager()（模块级 L512）。
	globalProgressManager *ProgressManager

	// globalProgressManagerOnce 确保单例只初始化一次。
	globalProgressManagerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProgressStep 创建构建步骤快照。
// 对齐 Python dataclass ProgressStep 必填三参 + 默认值：
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
