package rsi

// RsiEventConsumer：引擎事件单协程消费者（内部 v3 §4.3）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/event_consumer.py（264 行）。
//
// 分发：
//   - progress.metric → 节流合并 → 投影 metric 快照 → P2
//   - progress.usage → UsageRecorder 记账 + 实时 P2 用量推送
//   - node.created → 采纳时快照 + 投影 → P3
//   - node.stage → 投影描述更新 → P3
//
// 推送回调由 AgentServer 注入，本层不直接依赖 WebSocket；无回调时静默跳过
// （纯拉取模式仍可用）。

import (
	"log"
)

// PushCallback 推送回调签名（对齐 Python PushCallback）。
type PushCallback func(taskID string, payload map[string]any) error

// UsageRecorderSink 消费方对 usage 记账的最小依赖面
// （真身 UsageRecorder 见 usage_recorder.go；接口仅承载消费分派）。
type UsageRecorderSink interface {
	// RecordCumulative 累计用量（对齐 Python record_cumulative）。
	RecordCumulative(taskID string, usage map[string]any, iteration *int)
	// RecordEngineEvent 引擎用量事件（对齐 Python record_engine_event）。
	RecordEngineEvent(taskID string, payload map[string]any, callSequence *int)
	// UsageSummary 用量汇总视图（对齐 Python usage_summary）。
	UsageSummary(taskID string) map[string]any
}

// ArtifactSink 消费方对产物快照的最小依赖面（真身见 services.go ArtifactService）。
type ArtifactSink interface {
	// MakeSnapshot 采纳节点产物快照（对齐 Python make_snapshot）。
	MakeSnapshot(taskID, ref, nodeID string, artifacts []RsiArtifactPath) string
	// Locate 快照定位（对齐 Python locate）。
	Locate(taskID, artifactID string) (string, bool)
}

// ──────────────────────────── 结构体 ────────────────────────────

// EventConsumer 单协程消费引擎事件；绑定任务上下文（task_id / push 回调）。
type EventConsumer struct {
	taskID          string
	usageRecorder   UsageRecorderSink
	projector       *Projector
	artifactService ArtifactSink
	eventJournal    *EventJournal
	// onProgress P2 推送回调。
	onProgress PushCallback
	// onTreeDelta P3 推送回调。
	onTreeDelta PushCallback
	// lastPushedNodeIDs 节流记录：P3 只推新节点（内部 v3 §4.3）。
	lastPushedNodeIDs map[string]bool
}

// NewEventConsumer 构造消费者（eventJournal 为 nil 时按 tasksRoot 自建，
// 对齐 Python 构造期缺省）。
func NewEventConsumer(taskID string, usageRecorder UsageRecorderSink,
	projector *Projector, artifactService ArtifactSink, eventJournal *EventJournal) *EventConsumer {
	c := &EventConsumer{
		taskID:            taskID,
		usageRecorder:     usageRecorder,
		projector:         projector,
		artifactService:   artifactService,
		lastPushedNodeIDs: map[string]bool{},
	}
	if eventJournal != nil {
		c.eventJournal = eventJournal
	} else {
		c.eventJournal = NewEventJournal(projector.tasksRoot)
	}
	return c
}

// BindPush 注入推送回调（对齐 Python bind_push）。
func (c *EventConsumer) BindPush(onProgress, onTreeDelta PushCallback) {
	c.onProgress = onProgress
	c.onTreeDelta = onTreeDelta
}

// ──────────────────────────── 导出函数 ────────────────────────────

// OnEngineEvent 事件入口（兼容内部 dict 信封和 provider 形态事件）。
func (c *EventConsumer) OnEngineEvent(event map[string]any) {
	// observability 不得阻断训练（对齐 Python journal append try/except）。
	if c.eventJournal != nil {
		if _, err := c.eventJournal.Append(c.taskID, event); err != nil {
			log.Printf("[RSI] 引擎事件日志写入失败: task=%s err=%v", c.taskID, err)
		}
	}
	providerEventType := valueToString(event["event_type"])
	switch providerEventType {
	case "status":
		// TaskStore 是状态权威；Provider 状态事件仅供诊断，此处更新 store
		// 会与 worker 状态机竞争并重复 P1。
		return
	case "progress":
		var usage map[string]any
		if u, ok := event["usage"].(map[string]any); ok {
			usage = u
		}
		iteration := safeInt(event["iteration"])
		c.projector.OnProgressMetric(c.taskID, map[string]any{
			"iteration":        iteration,
			"total_iterations": safeInt(event["total_iterations"]),
			"score":            event["score"],
			"baseline":         event["baseline"],
		})
		var iterPtr *int
		if event["iteration"] != nil {
			iterPtr = &iteration
		}
		c.usageRecorder.RecordCumulative(c.taskID, usage, iterPtr)
		c.pushProgress()
		return
	case "node.stage":
		stage, _ := event["stage"].(map[string]any)
		if stage == nil {
			stage = map[string]any{}
		}
		payload := map[string]any{
			"node_ref": valueToString(event["node_ref"]),
			"stage":    stage,
		}
		if event["note"] != nil {
			payload["note"] = valueToString(event["note"])
		}
		node := c.projector.OnNodeStage(c.taskID, payload)
		if node != nil && c.onTreeDelta != nil {
			_ = c.onTreeDelta(c.taskID, map[string]any{"nodes": []map[string]any{node.ToDict()}})
		}
		return
	case "node":
		rawNode, _ := event["node"].(map[string]any)
		// Provider 完整节点投影（保留原 ID）——真身挂在 Projector.OnProviderNode。
		if rawNode == nil {
			return
		}
		node := c.projector.OnProviderNode(c.taskID, rawNode)
		if node == nil {
			return
		}
		artifacts := []RsiArtifactPath{}
		if items, ok := event["artifacts"].([]any); ok {
			for _, item := range items {
				m, ok := item.(map[string]any)
				if !ok || valueToString(m["path"]) == "" {
					continue
				}
				artifacts = append(artifacts, RsiArtifactPath{
					Role: valueToString(m["role"]), Path: valueToString(m["path"]), Format: valueToString(m["format"]),
				})
			}
		}
		if node.Adopted && len(artifacts) > 0 {
			artifactID := c.artifactService.MakeSnapshot(c.taskID, valueToString(rawNode["node_id"]), node.NodeID, artifacts)
			if artifactID != "" {
				node.SnapshotArtifactID = strPtr(artifactID)
				if path, ok := c.artifactService.Locate(c.taskID, artifactID); ok {
					extra := orMap(node.Extra)
					extra["artifact_path"] = path
					node.Extra = extra
				}
				c.projector.PersistNodeUpdate(c.taskID, *node)
			}
		}
		c.lastPushedNodeIDs[node.NodeID] = true
		if c.onTreeDelta != nil {
			_ = c.onTreeDelta(c.taskID, map[string]any{"nodes": []map[string]any{node.ToDict()}})
		}
		return
	case "progress.usage":
		var callSequence *int
		if event["event_id"] != nil {
			id := safeInt(event["event_id"])
			callSequence = &id
		}
		c.usageRecorder.RecordEngineEvent(c.taskID, map[string]any{
			"node_ref":   event["node_ref"],
			"model_call": event["model_call"],
		}, callSequence)
		if c.onProgress != nil {
			c.pushProgress()
		}
		return
	}
	// 内部规范化事件（family/kind 信封）。
	if e := ParseEngineEvent(event, c.taskID); e != nil {
		c.onNormalizedEvent(*e)
		return
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// onNormalizedEvent 规范化事件分派（对齐 Python is_* 四分支）。
func (c *EventConsumer) onNormalizedEvent(e EngineEvent) {
	switch {
	case e.IsProgressMetric():
		c.projector.OnProgressMetric(c.taskID, e.Payload)
		c.pushProgress()
	case e.IsProgressUsage():
		c.usageRecorder.RecordEngineEvent(c.taskID, e.Payload, e.EventID)
		if c.onProgress != nil {
			c.pushProgress()
		}
	case e.IsNodeCreated():
		payload := e.Payload
		nodeMeta, _ := payload["node"].(map[string]any)
		if nodeMeta == nil {
			nodeMeta = payload
		}
		nodeRef := valueToString(nodeMeta["ref"])
		adopted := boolValue(nodeMeta["accepted"])
		if adopted {
			artifacts := []RsiArtifactPath{}
			if items, ok := payload["artifacts"].([]any); ok {
				for _, item := range items {
					if m, ok := item.(map[string]any); ok {
						artifacts = append(artifacts, RsiArtifactPath{
							Role: valueToString(m["role"]), Path: valueToString(m["path"]), Format: valueToString(m["format"]),
						})
					}
				}
			}
			node := c.projector.OnNodeCreated(c.taskID, payload)
			if node != nil && len(artifacts) > 0 {
				artifactID := c.artifactService.MakeSnapshot(c.taskID, nodeRef, node.NodeID, artifacts)
				if artifactID != "" {
					node.SnapshotArtifactID = strPtr(artifactID)
					c.projector.PersistNodeUpdate(c.taskID, *node)
				}
			}
		} else {
			c.projector.OnNodeCreated(c.taskID, payload)
		}
		delta := c.projector.DeriveTreeDelta(c.taskID, c.lastPushedNodeIDs)
		nodes, _ := delta["nodes"].([]map[string]any)
		newIDs := map[string]bool{}
		for _, n := range nodes {
			if id := valueToString(n["node_id"]); id != "" {
				newIDs[id] = true
				c.lastPushedNodeIDs[id] = true
			}
		}
		if c.onTreeDelta != nil && len(nodes) > 0 {
			_ = c.onTreeDelta(c.taskID, delta)
		}
	case e.IsNodeStage():
		node := c.projector.OnNodeStage(c.taskID, e.Payload)
		if node != nil && c.onTreeDelta != nil {
			_ = c.onTreeDelta(c.taskID, map[string]any{"nodes": []map[string]any{node.ToDict()}})
		}
	}
}

// pushProgress 组装 v0.3 嵌套载荷并保留旧平铺别名（对齐 _progress_push_payload）。
func (c *EventConsumer) pushProgress() {
	if c.onProgress == nil {
		return
	}
	progress, err := c.projector.DeriveProgress(c.taskID)
	if err != nil {
		progress = map[string]any{}
	}
	var usage map[string]any
	if c.usageRecorder != nil {
		usage = c.usageRecorder.UsageSummary(c.taskID)
	}
	payload := map[string]any{"progress": progress}
	for k, v := range progress {
		payload[k] = v
	}
	if usage != nil {
		payload["usage"] = usage
	}
	_ = c.onProgress(c.taskID, payload)
}

// ConsumeQueue 单协程消费有界事件队列（内部 v3 §4.2 事件链路）。
// nil 哨兵 = 结束信号（终态后由 worker 排空再发哨兵）。
// 单事件失败不终止消费者（对齐 Python try/except finally task_done）。
func ConsumeQueue(queue <-chan map[string]any, consumer *EventConsumer, done chan<- struct{}) {
	for event := range queue {
		if event == nil {
			close(done)
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					logConsumeFailure(event, r)
				}
			}()
			consumer.OnEngineEvent(event)
		}()
	}
	close(done)
}

// log 事件消费失败日志（对齐 Python logger.exception 分支——单事件失败不终止消费者）。
func logConsumeFailure(event map[string]any, err any) {
	log.Printf("[RSI] 事件消费失败: %v err=%v", valueToString(event["event_type"]), err)
}
