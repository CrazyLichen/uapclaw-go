package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestControllerOutputPayload_正常 测试正常构造
func TestControllerOutputPayload_正常(t *testing.T) {
	payload := &ControllerOutputPayload{
		Type: TaskProcessing,
		Data: []DataFrame{&TextDataFrame{Text: "hello"}},
	}
	assert.Equal(t, TaskProcessing, payload.Type)
	assert.Len(t, payload.Data, 1)
}

// TestControllerOutputPayload_空Data 测试空 Data 列表
func TestControllerOutputPayload_空Data(t *testing.T) {
	payload := &ControllerOutputPayload{
		Type: AllTasksProcessed,
		Data: []DataFrame{},
	}
	assert.Equal(t, AllTasksProcessed, payload.Type)
	assert.Empty(t, payload.Data)
}

// TestControllerOutput_正常 测试批量输出正常构造
func TestControllerOutput_正常(t *testing.T) {
	output := &ControllerOutput{
		Type:         TaskProcessing,
		Data:         []*ControllerOutputPayload{{Type: TaskProcessing}},
		InputEventID: "evt-1",
	}
	assert.Equal(t, TaskProcessing, output.Type)
	assert.Len(t, output.Data, 1)
	assert.Equal(t, "evt-1", output.InputEventID)
}
