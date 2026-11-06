package harness

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestControlCmd_每条命令都带应答通道 验证 6 条命令均可 ack（C1/C16）。
func TestControlCmd_每条命令都带应答通道(t *testing.T) {
	cmds := []controlCmd{
		&cmdSend{msg: InboxMessage{Content: "hi", Immediate: true}, reply: make(chan error, 1)},
		&cmdAbort{immediate: true, reply: make(chan error, 1)},
		&cmdPause{reply: make(chan error, 1)},
		&cmdResume{reply: make(chan error, 1)},
		&cmdRoundFinished{roundID: 1, reply: make(chan error, 1)},
		&cmdStop{reply: make(chan error, 1)},
	}
	for _, c := range cmds {
		require.NotNil(t, c.ack())
	}
}

// TestAck_非阻塞回错误 验证 ack 在通道满时不阻塞（supervisor 崩溃兜底路径）。
func TestAck_非阻塞回错误(t *testing.T) {
	ch := make(chan error, 1)
	ch <- nil // 故意占满

	done := make(chan struct{})
	go func() { ack(ch, errors.New("boom")); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ack 阻塞了")
	}
	assert.Nil(t, <-ch) // 原值未被覆盖，证明非阻塞丢弃
}
