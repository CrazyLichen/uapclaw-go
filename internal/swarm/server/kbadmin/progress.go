package kbadmin

import (
	"encoding/json"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskEvent 解析进度事件（kb.task.subscribe 推送载荷，spec §5）
type TaskEvent struct {
	// TaskID 任务标识
	TaskID string `json:"task_id"`
	// DocID 文档标识
	DocID string `json:"doc_id"`
	// KBID 知识库标识
	KBID string `json:"kb_id"`
	// Stage 阶段名（parsing/chunking/embedding/triple）
	Stage string `json:"stage"`
	// Phase 事件相位：started / finished / error / status（终态）
	Phase string `json:"phase"`
	// Status 终态状态（Phase=status 时有值）
	Status string `json:"status,omitempty"`
	// Error 错误信息（Phase=error 或终态失败时有值）
	Error string `json:"error,omitempty"`
	// Timestamp 事件时间（Unix 秒）
	Timestamp int64 `json:"timestamp"`
}

// ProgressSink 观察者：stage 台账写入 + 事件发布 + 停滞扫描（spec §2）
type ProgressSink struct {
	// store kbadmin SQLite 台账
	store *Store
	// publisher 事件出口（既有事件通道接缝；nil 允许=仅落台账）
	publisher func(TaskEvent)
}

// stageRecord stages_json 单阶段记录（spec §3：各阶段独立起止时间与失败信息）
type stageRecord struct {
	// Stage 阶段名
	Stage string `json:"stage"`
	// StartedAt 开始时间（Unix 秒）
	StartedAt int64 `json:"started_at"`
	// FinishedAt 结束时间（Unix 秒）
	FinishedAt int64 `json:"finished_at"`
	// Error 失败信息
	Error string `json:"error,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProgressSink 构造（publisher 为既有事件通道接缝；nil 允许=仅落台账）
func NewProgressSink(store *Store, publisher func(TaskEvent)) *ProgressSink {
	return &ProgressSink{store: store, publisher: publisher}
}

// OnStageStart 阶段开始：stages_json 记 started_at + last_activity_at 刷新
func (s *ProgressSink) OnStageStart(job *ParseJob, stage string) {
	s.touchStage(job, stage, func(st *stageRecord) { st.StartedAt = time.Now().Unix() })
	s.emit(TaskEvent{TaskID: job.TaskID, DocID: job.DocID, KBID: job.KBID, Stage: stage, Phase: "started"})
}

// OnStageEnd 阶段成功结束：刷新 kb_task.progress 与 stage_desc
func (s *ProgressSink) OnStageEnd(job *ParseJob, stage string) {
	s.touchStage(job, stage, func(st *stageRecord) { st.FinishedAt = time.Now().Unix() })
	s.touchTaskProgress(job, stage)
	s.emit(TaskEvent{TaskID: job.TaskID, DocID: job.DocID, KBID: job.KBID, Stage: stage, Phase: "finished"})
}

// OnStageError 阶段失败：记错误信息（失败分级由阶段名承载——parse/chunk 归 parse 类，
// embedding/triple 归 index 类）
func (s *ProgressSink) OnStageError(job *ParseJob, stage string, err error) {
	s.touchStage(job, stage, func(st *stageRecord) {
		st.FinishedAt = time.Now().Unix()
		st.Error = err.Error()
	})
	s.emit(TaskEvent{TaskID: job.TaskID, DocID: job.DocID, KBID: job.KBID,
		Stage: stage, Phase: "error", Error: err.Error()})
}

// EmitStatus 终态事件（worker 收尾调用）
func (s *ProgressSink) EmitStatus(job *ParseJob, status, errMsg string) {
	s.emit(TaskEvent{TaskID: job.TaskID, DocID: job.DocID, KBID: job.KBID,
		Phase: "status", Status: status, Error: errMsg})
}

// ScanStalled 停滞扫描：last_activity_at 超阈值的 processing 文档标 suspected_stalled
// （仅标记 + cancel 入口，不自动取消——对齐 WeKnora；周期任务调用，spec §3）
func (s *ProgressSink) ScanStalled(threshold time.Duration) {
	docs, err := s.store.Doc().ListByStatus("processing")
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-threshold).Unix()
	for i := range docs {
		doc := &docs[i]
		if doc.LastActivityAt < cutoff && doc.StallState == "normal" {
			doc.StallState = "suspected_stalled"
			_ = s.store.Doc().Update(doc)
		}
	}
}

// stageProgressWeights 阶段→进度权重（kb_task.progress 0-100）
var stageProgressWeights = map[string]int{
	"parsing":   25,
	"chunking":  50,
	"embedding": 75,
	"triple":    90,
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emit 发布事件（publisher 为 nil 时跳过）
func (s *ProgressSink) emit(e TaskEvent) {
	e.Timestamp = time.Now().Unix()
	if s.publisher != nil {
		s.publisher(e)
	}
}

// touchTaskProgress 按阶段推进 kb_task.progress（阶段权重表；未知阶段不更新）
func (s *ProgressSink) touchTaskProgress(job *ParseJob, stage string) {
	weight, ok := stageProgressWeights[stage]
	if !ok {
		return
	}
	task, err := s.store.Task().Get(job.TaskID)
	if err != nil {
		return
	}
	if weight > task.Progress {
		task.Progress = weight
		task.StageDesc = stage
		_ = s.store.Task().Update(task)
	}
}

// touchStage 读改写 kb_doc.stages_json + last_activity_at（同一阶段重复上报合并为一条记录）
func (s *ProgressSink) touchStage(job *ParseJob, stage string, mutate func(*stageRecord)) {
	doc, err := s.store.Doc().Get(job.DocID)
	if err != nil {
		return
	}
	var stages []stageRecord
	if doc.StagesJSON != "" {
		_ = json.Unmarshal([]byte(doc.StagesJSON), &stages)
	}
	target := -1
	for i := range stages {
		if stages[i].Stage == stage {
			target = i
			break
		}
	}
	if target < 0 {
		stages = append(stages, stageRecord{Stage: stage})
		target = len(stages) - 1
	}
	mutate(&stages[target])
	data, _ := json.Marshal(stages)
	doc.StagesJSON = string(data)
	doc.LastActivityAt = time.Now().Unix()
	doc.UpdatedAt = doc.LastActivityAt
	_ = s.store.Doc().Update(doc)
}
