package utils

import (
	"fmt"
	"reflect"
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
// timestamp 为 UTC 时间（对齐 Python datetime.now(timezone.utc)），
// RFC3339Nano 输出 +00:00 偏移（对齐 isoformat 的 +00:00 形态）。
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

// AddCallback 添加进度回调，返回取消函数。
// 对齐 Python: add_callback + remove_callback（Go 函数值不可 == 比较，
// 取消函数是语言特性级等价物；cancel 幂等，二次调用安全）。
func (r *ProgressReporter) AddCallback(cb ProgressCallback) func() {
	r.mu.Lock()
	r.callbacks = append(r.callbacks, cb)
	r.mu.Unlock()
	return func() { r.removeCallback(cb) }
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
	r.progress.LastUpdateTime = time.Now().UTC()

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
	r.progress.LastUpdateTime = time.Now().UTC()
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
	r.progress.LastUpdateTime = time.Now().UTC()

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
	r.progress.LastUpdateTime = time.Now().UTC()

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
// details=空 dict、timestamp=now(timezone.utc)、duration/error=None。
func NewProgressStep(stage ProgressStage, status ProgressStatus, message string, opts ...ProgressStepOption) ProgressStep {
	step := ProgressStep{
		Stage:     stage,
		Status:    status,
		Message:   message,
		Details:   make(map[string]any),
		Timestamp: time.Now().UTC(),
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
// steps=空 list、overall_progress=0.0、start/last_update_time=now(timezone.utc)、error=None。
func NewBuildProgress(sessionID, agentType string, stage ProgressStage, status ProgressStatus, message string, opts ...BuildProgressOption) BuildProgress {
	progress := BuildProgress{
		SessionID:       sessionID,
		AgentType:       agentType,
		CurrentStage:    stage,
		CurrentStatus:   status,
		CurrentMessage:  message,
		Steps:           make([]ProgressStep, 0),
		OverallProgress: 0.0,
		StartTime:       time.Now().UTC(),
		LastUpdateTime:  time.Now().UTC(),
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
	r.progress.LastUpdateTime = time.Now().UTC()
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

// cloneStageDetails 克隆细节字典（nil 转空 map，对齐 Python "details or {}"——
// 步骤 Details 恒非 nil，避免后续 mergeDetails 写 nil map panic）。
func cloneStageDetails(details map[string]any) map[string]any {
	if details == nil {
		return make(map[string]any)
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
