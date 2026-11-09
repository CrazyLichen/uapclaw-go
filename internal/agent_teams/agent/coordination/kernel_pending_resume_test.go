package coordination

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResumePausedRound_Warm 验证 warm 分支直接续跑且不带 query。
func TestResumePausedRound_Warm(t *testing.T) {
	harnessAcc := &fakeHarnessAccessor{pausedQuery: "任务", paused: true}
	sc := &fakeStreamControllerAccessor{}
	host := &fakeKernelHost{harnessAccessor: harnessAcc, streamControllerAccessor: sc}
	k := NewCoordinationKernel(host)

	k.resumePausedRound(context.Background())

	assert.True(t, sc.resumeCalled)
	assert.Nil(t, sc.resumeQuery, "warm resume 不带 query")
}

// TestResumePausedRound_未暂停时noop 验证没有挂起轮时不续跑。
func TestResumePausedRound_未暂停时noop(t *testing.T) {
	harnessAcc := &fakeHarnessAccessor{paused: false}
	sc := &fakeStreamControllerAccessor{}
	host := &fakeKernelHost{harnessAccessor: harnessAcc, streamControllerAccessor: sc}
	k := NewCoordinationKernel(host)

	k.resumePausedRound(context.Background())

	assert.False(t, sc.resumeCalled)
}
