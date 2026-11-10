package harness_protocol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHarnessError_层次关系 验证 4 个子类都可被 errors.Is 归到基类。
// Python: errors.py:9-26（HarnessStateError/UnsupportedHarnessCapabilityError/
// HarnessProtocolError 继承 HarnessError；CheckpointConflictError 继承 HarnessProtocolError）
func TestHarnessError_层次关系(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		parent error
	}{
		{"HarnessStateError→HarnessError", &HarnessStateError{Msg: "x"}, ErrHarness},
		{"UnsupportedHarnessCapabilityError→HarnessError", &UnsupportedHarnessCapabilityError{Msg: "x"}, ErrHarness},
		{"HarnessProtocolError→HarnessError", &HarnessProtocolError{Msg: "x"}, ErrHarness},
		{"CheckpointConflictError→HarnessProtocolError", &CheckpointConflictError{Msg: "x"}, ErrHarnessProtocol},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.True(t, errors.Is(c.err, c.parent), "%T 应能 Is 到 %T", c.err, c.parent)
		})
	}
}

// TestHarnessError_CheckpointConflict间接归到基类 验证三层传递。
func TestHarnessError_CheckpointConflict间接归到基类(t *testing.T) {
	err := &CheckpointConflictError{Msg: "stale"}
	assert.True(t, errors.Is(err, ErrHarnessProtocol))
	assert.True(t, errors.Is(err, ErrHarness))
}

// TestHarnessStateError_错误文案 验证 Error() 返回 msg 原文。
// Python: 各异常类不覆盖 __str__，文案即入参。
func TestHarnessStateError_错误文案(t *testing.T) {
	assert.Equal(t, "非法状态", (&HarnessStateError{Msg: "非法状态"}).Error())
	assert.Equal(t, "不支持", (&UnsupportedHarnessCapabilityError{Msg: "不支持"}).Error())
}
