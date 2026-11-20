package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestControllerConfig_默认值对齐Python 验证每个字段默认值与 Python 完全一致。
func TestControllerConfig_默认值对齐Python(t *testing.T) {
	cfg := DefaultControllerConfig()

	// 任务调度
	assert.Equal(t, 5, cfg.MaxConcurrentTasks, "max_concurrent_tasks 默认 5")
	assert.Equal(t, 1.0, cfg.ScheduleInterval, "schedule_interval 默认 1.0")
	assert.Nil(t, cfg.TaskTimeout, "task_timeout 默认 nil")

	// 任务管理
	assert.Equal(t, 1, cfg.DefaultTaskPriority, "default_task_priority 默认 1")
	assert.False(t, cfg.EnableTaskPersistence, "enable_task_persistence 默认 false")

	// 事件队列
	assert.Equal(t, 10000, cfg.EventQueueSize, "event_queue_size 默认 10000")
	assert.Equal(t, 300.0, cfg.EventTimeout, "event_timeout 默认 300")

	// 意图识别
	assert.False(t, cfg.EnableIntentRecognition, "enable_intent_recognition 默认 false")
	assert.Equal(t, "", cfg.IntentLLMID, "intent_llm_id 默认空")
	assert.Equal(t, 0.7, cfg.IntentConfidenceThreshold, "intent_confidence_threshold 默认 0.7")
	assert.Equal(t, []string{"create_task", "pause_task", "resume_task", "cancel_task", "unknown_task"}, cfg.IntentTypeList)

	// 默认响应
	assert.Equal(t, "text", cfg.DefaultResponse.Type, "default_response.type 默认 text")

	// 完成信号
	assert.False(t, cfg.SuppressCompletionSignal, "suppress_completion_signal 默认 false")

	// 流
	assert.Equal(t, 30.0, cfg.StreamFirstFrameTimeout, "stream_first_frame_timeout 默认 30.0")
}
