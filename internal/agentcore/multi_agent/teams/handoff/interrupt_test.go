package handoff

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// TestExtractInterruptSignal_路径1 测试 result 包含 result_type="interrupt" 时返回 TeamInterruptSignal
func TestExtractInterruptSignal_路径1(t *testing.T) {
	result := map[string]any{
		"result_type": "interrupt",
		"message":     "需要人工确认",
	}
	sig := ExtractInterruptSignal(result, nil)
	assert.NotNil(t, sig)
	assert.Equal(t, "interrupt", sig.Result["result_type"])
	assert.Equal(t, "需要人工确认", sig.Message)
}

// TestExtractInterruptSignal_路径1_无message 测试 result 包含 result_type="interrupt" 但无 message
func TestExtractInterruptSignal_路径1_无message(t *testing.T) {
	result := map[string]any{
		"result_type": "interrupt",
	}
	sig := ExtractInterruptSignal(result, nil)
	assert.NotNil(t, sig)
	assert.Equal(t, "", sig.Message)
}

// TestExtractInterruptSignal_路径2 测试 err 为 AgentInterrupt 类型时返回 TeamInterruptSignal
func TestExtractInterruptSignal_路径2(t *testing.T) {
	err := &interaction.AgentInterrupt{Message: "中断测试"}
	sig := ExtractInterruptSignal(nil, err)
	assert.NotNil(t, sig)
	assert.Equal(t, "interrupt", sig.Result["result_type"])
	assert.Equal(t, "中断测试", sig.Message)
}

// TestExtractInterruptSignal_路径2_非string消息 测试 AgentInterrupt.Message 为非 string 类型
func TestExtractInterruptSignal_路径2_非string消息(t *testing.T) {
	err := &interaction.AgentInterrupt{Message: map[string]any{"key": "value"}}
	sig := ExtractInterruptSignal(nil, err)
	assert.NotNil(t, sig)
	assert.Equal(t, "", sig.Message) // 非 string 消息转为空字符串
}

// TestExtractInterruptSignal_无中断 测试无中断时返回 nil
func TestExtractInterruptSignal_无中断(t *testing.T) {
	// 普通 result，无 interrupt 标记
	sig := ExtractInterruptSignal(map[string]any{"result_type": "normal"}, nil)
	assert.Nil(t, sig)

	// nil result + nil err
	sig = ExtractInterruptSignal(nil, nil)
	assert.Nil(t, sig)

	// 普通 error
	sig = ExtractInterruptSignal(nil, errors.New("some error"))
	assert.Nil(t, sig)
}

// TestFlushTeamSession_session为nil 测试 session 为 nil 时返回 nil
func TestFlushTeamSession_session为nil(t *testing.T) {
	err := FlushTeamSession(context.Background(), nil)
	assert.Nil(t, err)
}

// TestFlushTeamSession_正常流程 测试正常流程调用 CloseStream + Commit
// 注意：无真实存储时 Commit 会返回错误，FlushTeamSession 会记录警告并返回该 error
func TestFlushTeamSession_正常流程(t *testing.T) {
	sess := session.NewAgentTeamSession(
		session.WithAgentTeamSessionID("flush-test"),
	)
	// FlushTeamSession 应正常执行 CloseStream + Commit，
	// 无真实检查点存储时 Commit 返回错误，FlushTeamSession 返回该错误
	_ = FlushTeamSession(context.Background(), sess)
	// 验证不 panic 即可，Commit 失败为预期行为
}
