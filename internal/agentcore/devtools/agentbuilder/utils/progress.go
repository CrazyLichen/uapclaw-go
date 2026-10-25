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
