package archive

import (
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionRuntime 本服务依赖的 Runtime 边界。
//
// 只声明归档/删除实际用到的方法。Python 侧通过 getattr 逐步探测
// （stop_heartbeat_runs / stop_subagent_runtimes / stop_session_for_lifecycle /
// delete_session / is_session_running / session_message_service），
// Go 侧用可选实现接口表达同样的"有就用、没有就跳过"语义。
type SessionRuntime interface {
	// DeleteSession 删除一个持久会话（含目录）
	DeleteSession(ctx context.Context, channelID, sessionID string) error
}

// HeartbeatStopper 可选：停止会话的后台心跳。
// Python: session_archive.py _stop_heartbeat（runtime.stop_heartbeat_runs）
type HeartbeatStopper interface {
	// StopHeartbeatRuns 停止该会话全部心跳轮
	StopHeartbeatRuns(ctx context.Context, sessionID string) error
}

// SubagentStopper 可选：释放会话常驻子代理。
// Python: session_archive.py _stop_subagents（runtime.stop_subagent_runtimes）
type SubagentStopper interface {
	// StopSubagentRuntimes 释放该会话常驻的子代理
	StopSubagentRuntimes(ctx context.Context, sessionID, channelID, reason string) error
}

// SessionStopper 可选：按代次停止会话执行。
// Python: session_archive.py _finalize_session_body（runtime.stop_session_for_lifecycle）
type SessionStopper interface {
	// StopSessionForLifecycle 停止指定代次的会话执行
	StopSessionForLifecycle(ctx context.Context, channelID, sessionID string, generation *int, stopRunner bool) error
}

// SessionRunningProbe 可选：查询会话是否有正在跑的执行。
// Python: session_archive.py（runtime.is_session_running）
type SessionRunningProbe interface {
	// IsSessionRunning 该会话是否有执行在跑
	IsSessionRunning(sessionID string) bool
}

// Mailbox 可选：会话邮箱屏障。
// Python: session_archive.py _session_message_service（session_message_service）
type Mailbox interface {
	// BlockTargetDelete 非阻塞地标记目标删除屏障
	BlockTargetDelete(sessionID string)
	// BeginTargetDelete 阻塞等待消费者退出
	BeginTargetDelete(ctx context.Context, sessionID string) error
	// OnTargetDeleted 目标已删除（触发 FIFO 清理）
	OnTargetDeleted(ctx context.Context, sessionID string) error
}

// GenerationProbe 可选：读取会话运行时当前代次。
// Python: session_archive.py _capture_generation（_session_coordinator.snapshot_session）
type GenerationProbe interface {
	// SessionGeneration 返回会话当前代次（不存在返回 nil）
	SessionGeneration(sessionID string) *int
}

// WriterFlusher 可选：刷写待落盘队列。
// Python: session_archive.py（session_metadata.flush_pending_writes /
// session_history.flush_pending_writes）
type WriterFlusher interface {
	// FlushPendingWrites 刷写挂起写入，返回是否全部成功
	FlushPendingWrites(timeoutSeconds float64) bool
}

// ProjectRestorer 可选：把被归档隐藏的项目恢复出来。
// Python: session_archive.py _restore_hidden_project
type ProjectRestorer interface {
	// RestoreHiddenProject 取消项目隐藏
	RestoreHiddenProject(projectID string) error
}

// Service 会话归档与删除服务。
// Python: session_archive.py SessionArchiveService
type Service struct {
	// mu 保护以下字段
	mu sync.Mutex
	// runtime Runtime 删除边界
	runtime SessionRuntime
	// finalizing 会话 → 在飞收尾取消函数
	finalizing map[string]context.CancelFunc
	// finalizingOperations 会话 → 在飞收尾对应的操作标识
	finalizingOperations map[string]string
	// ownerID 本进程的操作归属标识
	ownerID string
	// finalizeSem 并行收尾名额
	finalizeSem chan struct{}
	// recoveryActive 恢复循环是否在跑
	recoveryActive bool
	// recoveryPoke 恢复循环唤醒信号
	recoveryPoke chan struct{}
	// heartbeatStopper 可选：心跳停止器
	heartbeatStopper HeartbeatStopper
	// subagentStopper 可选：子代理释放器
	subagentStopper SubagentStopper
	// sessionStopper 可选：按代次停止器
	sessionStopper SessionStopper
	// runningProbe 可选：会话运行探测
	runningProbe SessionRunningProbe
	// mailbox 可选：会话邮箱
	mailbox Mailbox
	// generationProbe 可选：代次读取
	generationProbe GenerationProbe
	// metadataFlusher 可选：元数据刷写器
	metadataFlusher WriterFlusher
	// historyFlusher 可选：历史刷写器
	historyFlusher WriterFlusher
	// projectRestorer 可选：项目恢复器
	projectRestorer ProjectRestorer
	// reindexPins 可选：置顶序号重排
	reindexPins func() error
	// removeMetadataCache 可选：清元数据缓存
	removeMetadataCache func(sessionID string)
	// sessionHasLeftProject 可选：会话离开项目后的推荐刷新
	sessionHasLeftProject func(ctx context.Context, projectID, sessionID string) error
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxCleanupAttempts 收尾最大尝试次数
	// Python: session_archive.py _MAX_CLEANUP_ATTEMPTS = 5
	MaxCleanupAttempts = 5
	// MaxParallelFinalizers 并行收尾上限
	// Python: session_archive.py _MAX_PARALLEL_FINALIZERS = 4
	MaxParallelFinalizers = 4
	// maxBusyRetries 操作占用时的快速重试预算
	// Python: session_archive.py _finalize_session 的 range(40)
	maxBusyRetries = 40
	// busyRetrySleep 占用重试间隔
	// Python: session_archive.py await asyncio.sleep(0.05)
	busyRetrySleep = 50 * time.Millisecond
	// recoveryIdleInterval 恢复循环空闲轮询间隔
	// Python: session_archive.py _recover 的 delay = 10.0
	recoveryIdleInterval = 10 * time.Second
	// flushTimeoutSeconds 队列刷写超时
	// Python: session_archive.py flush_pending_writes(10)
	flushTimeoutSeconds = 10.0
	// operationKindDelete 删除
	operationKindDelete = lifecycle.OperationKindDelete
	// operationKindArchive 归档
	operationKindArchive = lifecycle.OperationKindArchive
	// operationKindUnarchive 取消归档
	operationKindUnarchive = "unarchive"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentServer
	// cleanupBackoffSeconds 失败退避序列（秒）
	// Python: session_archive.py _CLEANUP_BACKOFF_SECONDS = (5.0, 15.0, 60.0, 300.0)
	cleanupBackoffSeconds = []float64{5.0, 15.0, 60.0, 300.0}
	// nonRetryableCodes 不值得重试的错误码
	// Python: session_archive.py _record_finalize_failure 的集合
	nonRetryableCodes = map[string]struct{}{
		"BAD_REQUEST": {}, "SESSION_ID_CONFLICT": {}, "PROJECT_NAME_CONFLICT": {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewService 创建归档服务。
// Python: SessionArchiveService.__init__
func NewService(runtime SessionRuntime, ownerID string) *Service {
	return &Service{
		runtime:              runtime,
		ownerID:              ownerID,
		finalizing:           map[string]context.CancelFunc{},
		finalizingOperations: map[string]string{},
		finalizeSem:          make(chan struct{}, MaxParallelFinalizers),
		recoveryPoke:         make(chan struct{}, 1),
	}
}

// CleanupBackoff 第 attempts 次失败后的退避秒数。
// Python: session_archive.py _cleanup_backoff
func CleanupBackoff(attempts int) float64 {
	index := attempts - 1
	if index < 0 {
		index = 0
	}
	if index >= len(cleanupBackoffSeconds) {
		index = len(cleanupBackoffSeconds) - 1
	}
	return cleanupBackoffSeconds[index]
}

// SetHeartbeatStopper 注入心跳停止器。
func (s *Service) SetHeartbeatStopper(stopper HeartbeatStopper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatStopper = stopper
}

// SetSubagentStopper 注入子代理释放器。
func (s *Service) SetSubagentStopper(stopper SubagentStopper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subagentStopper = stopper
}

// SetSessionStopper 注入按代次停止器。
func (s *Service) SetSessionStopper(stopper SessionStopper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionStopper = stopper
}

// SetRunningProbe 注入会话运行探测。
func (s *Service) SetRunningProbe(probe SessionRunningProbe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runningProbe = probe
}

// SetMailbox 注入会话邮箱。
func (s *Service) SetMailbox(mailbox Mailbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailbox = mailbox
}

// SetGenerationProbe 注入代次读取器。
func (s *Service) SetGenerationProbe(probe GenerationProbe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generationProbe = probe
}

// SetFlushers 注入元数据与历史刷写器。
func (s *Service) SetFlushers(metadata, history WriterFlusher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadataFlusher = metadata
	s.historyFlusher = history
}

// SetProjectRestorer 注入项目恢复器。
func (s *Service) SetProjectRestorer(restorer ProjectRestorer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projectRestorer = restorer
}

// SetReindexPins 注入置顶序号重排。
func (s *Service) SetReindexPins(reindex func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindexPins = reindex
}

// SetRemoveMetadataCache 注入元数据缓存清理。
func (s *Service) SetRemoveMetadataCache(remove func(sessionID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeMetadataCache = remove
}

// SetSessionHasLeftProject 注入"会话离开项目"的推荐刷新回调。
func (s *Service) SetSessionHasLeftProject(
	hook func(ctx context.Context, projectID, sessionID string) error,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionHasLeftProject = hook
}

// Close 停止在飞收尾与恢复循环。
// Python: SessionArchiveService.close
func (s *Service) Close() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.finalizing))
	for _, cancel := range s.finalizing {
		cancels = append(cancels, cancel)
	}
	s.finalizing = map[string]context.CancelFunc{}
	s.finalizingOperations = map[string]string{}
	s.recoveryActive = false
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pokeRecovery 唤醒恢复循环（非阻塞）。
// Python: session_archive.py _poke_recovery
func (s *Service) pokeRecovery() {
	select {
	case s.recoveryPoke <- struct{}{}:
	default:
	}
}

// lockWait 等待资源锁（阻塞）。
// Python: session_archive.py self.lock("session", session_id)（异步上下文管理器）
//
// Go 侧用可重入文件锁 + 进程内互斥实现；等待期间不持其他锁。
func (s *Service) lockWait(kind, resourceID string) (func(), error) {
	return lifecycle.ResourceLock(kind, resourceID)
}
