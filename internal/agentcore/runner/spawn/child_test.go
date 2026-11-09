package spawn

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// failingWriter 写入即失败的 Writer，用于覆盖写响应的错误分支。
type failingWriter struct{}

// Write 始终返回错误。
func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

// failingReader 读取即返回非 EOF 错误的 Reader。
type failingReader struct{}

// Read 始终返回错误。
func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

// fakeChildRunner 记录调用并返回预设结果的 ChildRunner 替身。
type fakeChildRunner struct {
	// setConfigErr SetConfig 返回的错误
	setConfigErr error
	// startErr Start 返回的错误
	startErr error
	// runResult RunAgent 返回的结果
	runResult map[string]any
	// runErr RunAgent 返回的错误
	runErr error
	// streamCh RunAgentStreaming 返回的通道
	streamCh <-chan stream.Schema
	// streamErr RunAgentStreaming 返回的错误
	streamErr error
	// runCalled 是否调用过 RunAgent
	runCalled bool
	// streamCalled 是否调用过 RunAgentStreaming
	streamCalled bool
	// started 是否调用过 Start
	started bool
}

// SetConfig 记录配置错误。
func (f *fakeChildRunner) SetConfig(_ map[string]any) error { return f.setConfigErr }

// Start 记录启动调用。
func (f *fakeChildRunner) Start(_ context.Context) error {
	f.started = true
	return f.startErr
}

// Stop 停止 Runner。
func (f *fakeChildRunner) Stop(_ context.Context) error { return nil }

// RunAgent 记录非流式调用。
func (f *fakeChildRunner) RunAgent(_ context.Context, _ interfaces.BaseAgent, _ map[string]any, _ string) (map[string]any, error) {
	f.runCalled = true
	return f.runResult, f.runErr
}

// RunAgentStreaming 记录流式调用。
func (f *fakeChildRunner) RunAgentStreaming(_ context.Context, _ interfaces.BaseAgent, _ map[string]any, _ string, _ any) (<-chan stream.Schema, error) {
	f.streamCalled = true
	return f.streamCh, f.streamErr
}

// fakeAgentCreator 返回预设 Agent 的 AgentCreator 替身。
type fakeAgentCreator struct {
	// agent 返回的 Agent 实例
	agent interfaces.BaseAgent
	// err 创建返回的错误
	err error
	// agentType 最近一次调用的 agent_type
	agentType string
}

// CreateByType 记录 agent_type 并返回预设结果。
func (f *fakeAgentCreator) CreateByType(_ context.Context, agentType string, _ map[string]any, _ map[string]any) (interfaces.BaseAgent, error) {
	f.agentType = agentType
	return f.agent, f.err
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// encodeMessages 把消息序列化成子进程 stdin 形态。
func encodeMessages(t *testing.T, msgs ...Message) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	for _, msg := range msgs {
		require.NoError(t, WriteMessage(&buf, msg))
	}
	return &buf
}

// decodeMessages 解析子进程 stdout 中写出的全部消息。
func decodeMessages(t *testing.T, out *bytes.Buffer) []Message {
	t.Helper()
	var msgs []Message
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var msg Message
		require.NoError(t, json.Unmarshal([]byte(line), &msg), "stdout 行必须是合法消息: %s", line)
		msgs = append(msgs, msg)
	}
	return msgs
}

// findMessage 返回第一条指定类型的消息。
func findMessage(msgs []Message, msgType MessageType) (Message, bool) {
	for _, msg := range msgs {
		if msg.Type == msgType {
			return msg, true
		}
	}
	return Message{}, false
}

// startMessageLoop 启动消息循环，返回 stdin 写入端、stdout 扫描器与退出通道。
func startMessageLoop(t *testing.T) (*io.PipeWriter, *bufio.Scanner, chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errCh := make(chan error, 1)
	go func() { errCh <- ProcessMessageLoop(context.Background(), inR, outW, nil, map[string]any{}, nil, nil) }()
	return inW, bufio.NewScanner(outR), errCh
}

// stopMessageLoop 关闭 stdin 并等循环退出。
func stopMessageLoop(t *testing.T, inW *io.PipeWriter, errCh chan error) error {
	t.Helper()
	_ = inW.Close()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("消息循环未在超时内退出")
		return nil
	}
}

// recvMessage 从 stdout 读一条消息。
func recvMessage(t *testing.T, scanner *bufio.Scanner) Message {
	t.Helper()
	require.True(t, scanner.Scan(), "期望一条输出消息")
	var msg Message
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &msg))
	return msg
}

// runInputUntilTerminal 写入一条输入消息，读到 DONE/ERROR 后关闭 stdin 并等循环退出。
// 用管道而非 bytes.Reader：stdin 立刻 EOF 会在 agent goroutine 写出 DONE 前结束循环。
func runInputUntilTerminal(t *testing.T, input Message) []Message {
	t.Helper()
	inW, scanner, errCh := startMessageLoop(t)
	require.NoError(t, WriteMessage(inW, input))

	var msgs []Message
	for scanner.Scan() {
		var msg Message
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &msg))
		msgs = append(msgs, msg)
		if msg.Type == MessageTypeDone || msg.Type == MessageTypeError {
			break
		}
	}
	require.NoError(t, stopMessageLoop(t, inW, errCh))
	return msgs
}

// ──────────────────────────── 测试用例 ────────────────────────────

// TestHandleHealthCheck_成功 健康检查写出 healthy 响应。
func TestHandleHealthCheck_成功(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, HandleHealthCheck(context.Background(), NewMessage(MessageTypeHealthCheck, nil), &out))

	msgs := decodeMessages(t, &out)
	require.Len(t, msgs, 1)
	assert.Equal(t, MessageTypeHealthCheckResponse, msgs[0].Type)
	payload, ok := msgs[0].Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "healthy", payload["status"])
}

// TestHandleHealthCheck_写入失败 写响应失败时返回错误。
func TestHandleHealthCheck_写入失败(t *testing.T) {
	err := HandleHealthCheck(context.Background(), NewMessage(MessageTypeHealthCheck, nil), failingWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "写入健康检查响应失败")
}

// TestHandleShutdown_成功 关闭请求写出 ack 响应。
func TestHandleShutdown_成功(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, HandleShutdown(context.Background(), NewMessage(MessageTypeShutdown, nil), &out))

	msgs := decodeMessages(t, &out)
	require.Len(t, msgs, 1)
	assert.Equal(t, MessageTypeShutdownAck, msgs[0].Type)
	payload, ok := msgs[0].Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "acknowledged", payload["status"])
}

// TestHandleShutdown_写入失败 写 ack 失败时返回错误。
func TestHandleShutdown_写入失败(t *testing.T) {
	err := HandleShutdown(context.Background(), NewMessage(MessageTypeShutdown, nil), failingWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "写入关闭确认失败")
}

// TestProcessMessageLoop_健康检查后关闭 循环依次处理健康检查与关闭请求。
func TestProcessMessageLoop_健康检查后关闭(t *testing.T) {
	inW, scanner, errCh := startMessageLoop(t)

	require.NoError(t, WriteMessage(inW, NewMessage(MessageTypeHealthCheck, nil)))
	assert.Equal(t, MessageTypeHealthCheckResponse, recvMessage(t, scanner).Type)

	require.NoError(t, WriteMessage(inW, NewMessage(MessageTypeShutdown, nil)))
	assert.Equal(t, MessageTypeShutdownAck, recvMessage(t, scanner).Type)

	require.NoError(t, stopMessageLoop(t, inW, errCh))
}

// TestProcessMessageLoop_未知消息类型被跳过 未知类型只记日志，循环继续到关闭。
func TestProcessMessageLoop_未知消息类型被跳过(t *testing.T) {
	inW, scanner, errCh := startMessageLoop(t)

	require.NoError(t, WriteMessage(inW, NewMessage(MessageTypeOutput, map[string]any{"x": 1})))
	require.NoError(t, WriteMessage(inW, NewMessage(MessageTypeShutdown, nil)))
	assert.Equal(t, MessageTypeShutdownAck, recvMessage(t, scanner).Type)

	require.NoError(t, stopMessageLoop(t, inW, errCh))
}

// TestProcessMessageLoop_stdin关闭 读到 EOF 时正常退出。
func TestProcessMessageLoop_stdin关闭(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, ProcessMessageLoop(context.Background(), strings.NewReader(""), &out, nil, map[string]any{}, nil, nil))
	assert.Empty(t, out.String())
}

// TestProcessMessageLoop_stdin读取错误 非 EOF 读取错误向上传播。
func TestProcessMessageLoop_stdin读取错误(t *testing.T) {
	var out bytes.Buffer
	err := ProcessMessageLoop(context.Background(), failingReader{}, &out, nil, map[string]any{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin 读取错误")
}

// TestProcessMessageLoop_上下文取消 上下文取消时返回取消原因。
func TestProcessMessageLoop_上下文取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	go func() {
		// 等循环进入 select 后取消，避免与 EOF 路径竞争
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	var out bytes.Buffer
	err := ProcessMessageLoop(ctx, pr, &out, nil, map[string]any{}, nil, nil)
	require.ErrorIs(t, err, context.Canceled)
}

// TestProcessMessageLoop_输入缺少agent配置 无 agent_config 时回错误消息并退出。
func TestProcessMessageLoop_输入缺少agent配置(t *testing.T) {
	stdin := encodeMessages(t, NewMessage(MessageTypeInput, map[string]any{
		"inputs": map[string]any{"q": "hi"},
	}))
	var out bytes.Buffer

	err := ProcessMessageLoop(context.Background(), stdin, &out, nil, map[string]any{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少 agent_config")

	msgs := decodeMessages(t, &out)
	errMsg, ok := findMessage(msgs, MessageTypeError)
	require.True(t, ok, "必须回 ERROR 消息")
	payload, ok := errMsg.Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ValueError", payload["error_type"])
}

// TestProcessMessageLoop_输入载荷非map 载荷类型不符时跳过该消息。
func TestProcessMessageLoop_输入载荷非map(t *testing.T) {
	inW, scanner, errCh := startMessageLoop(t)

	require.NoError(t, WriteMessage(inW, NewMessage(MessageTypeInput, "not-a-map")))
	require.NoError(t, WriteMessage(inW, NewMessage(MessageTypeShutdown, nil)))
	assert.Equal(t, MessageTypeShutdownAck, recvMessage(t, scanner).Type)

	require.NoError(t, stopMessageLoop(t, inW, errCh))
}

// TestProcessMessageLoop_团队Agent执行完成 注入团队函数后跑通输入→DONE 链路。
func TestProcessMessageLoop_团队Agent执行完成(t *testing.T) {
	restoreTeamFuncs(t)
	SetTeamRunnerFunc(func(_ context.Context, _ any, inputs map[string]any, sess any) (map[string]any, error) {
		assert.Equal(t, "team-1", sess)
		return map[string]any{"echo": inputs["q"]}, nil
	})
	TeamAgentFromPayloadFunc = func(_ context.Context, payload map[string]any) (any, error) {
		return "team-agent", nil
	}

	msgs := runInputUntilTerminal(t, NewMessage(MessageTypeInput, map[string]any{
		"agent_config": map[string]any{
			"agent_kind": "team_agent",
			"session_id": "team-1",
			"payload":    map[string]any{"team_id": "t1"},
		},
		"inputs": map[string]any{"q": "hello"},
	}))

	doneMsg, ok := findMessage(msgs, MessageTypeDone)
	require.True(t, ok, "必须发 DONE 消息")
	payload, ok := doneMsg.Payload.(map[string]any)
	require.True(t, ok)
	result, ok := payload["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello", result["echo"])
}

// TestProcessMessageLoop_团队Agent流式执行 流式模式逐块写 STREAM_CHUNK 后发 DONE。
func TestProcessMessageLoop_团队Agent流式执行(t *testing.T) {
	restoreTeamFuncs(t)
	ch := make(chan stream.Schema, 1)
	ch <- stream.OutputSchema{Type: "answer", Payload: map[string]any{"content": "hi"}}
	close(ch)
	TeamRunnerStreamingFunc = func(_ context.Context, _ any, _ map[string]any, _ any) (<-chan stream.Schema, error) {
		return ch, nil
	}
	TeamAgentFromPayloadFunc = func(_ context.Context, _ map[string]any) (any, error) {
		return "team-agent", nil
	}

	msgs := runInputUntilTerminal(t, NewMessage(MessageTypeInput, map[string]any{
		"agent_config": map[string]any{
			"agent_kind": "team_agent",
			"session_id": "team-1",
			"payload":    map[string]any{"team_id": "t1"},
		},
		"inputs":    map[string]any{"q": "hello"},
		"streaming": true,
	}))

	_, hasChunk := findMessage(msgs, MessageTypeStreamChunk)
	_, hasDone := findMessage(msgs, MessageTypeDone)
	assert.True(t, hasChunk, "流式路径必须写 STREAM_CHUNK")
	assert.True(t, hasDone, "流式路径结束后必须写 DONE")
}

// TestExecuteAgent_不支持的类型 未知 agent_kind 返回错误。
func TestExecuteAgent_不支持的类型(t *testing.T) {
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{AgentKind: "unknown"}, nil, &bytes.Buffer{}, false, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支持的 Agent 启动方式")
}

// TestExecuteAgent_类Agent缺少AgentCard 缺卡片时拒绝创建实例。
func TestExecuteAgent_类Agent缺少AgentCard(t *testing.T) {
	creator := &fakeAgentCreator{}
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{AgentKind: SpawnAgentKindClassAgent}, nil, &bytes.Buffer{}, false, nil, nil, creator)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少 agent_card")
	assert.Empty(t, creator.agentType, "缺卡片时不得调用创建器")
}

// TestExecuteAgent_类Agent未注入创建器 未注入 AgentCreator 时返回错误。
func TestExecuteAgent_类Agent未注入创建器(t *testing.T) {
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{AgentKind: SpawnAgentKindClassAgent}, nil, &bytes.Buffer{}, false, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未注入 AgentCreator")
}

// TestExecuteAgent_团队Agent未注入构造器 未注入 FromSpawnPayload 时返回错误。
func TestExecuteAgent_团队Agent未注入构造器(t *testing.T) {
	restoreTeamFuncs(t)
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{
		AgentKind: SpawnAgentKindTeamAgent,
		Payload:   map[string]any{"team_id": "t1"},
	}, nil, &bytes.Buffer{}, false, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TeamAgentFromPayloadFunc 尚未注入")
}

// TestExecuteAgent_团队Agent构造器类型不符 注入值类型不对时返回错误。
func TestExecuteAgent_团队Agent构造器类型不符(t *testing.T) {
	restoreTeamFuncs(t)
	TeamAgentFromPayloadFunc = "not-a-func"
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{
		AgentKind: SpawnAgentKindTeamAgent,
		Payload:   map[string]any{"team_id": "t1"},
	}, nil, &bytes.Buffer{}, false, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "函数类型不匹配")
}

// TestExecuteAgent_团队Agent载荷为空 Payload 为空时返回错误。
func TestExecuteAgent_团队Agent载荷为空(t *testing.T) {
	restoreTeamFuncs(t)
	TeamAgentFromPayloadFunc = func(_ context.Context, _ map[string]any) (any, error) { return "a", nil }
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{AgentKind: SpawnAgentKindTeamAgent}, nil, &bytes.Buffer{}, false, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Payload 为空")
}

// TestExecuteAgent_团队Agent构造失败 构造器报错时原样传播。
func TestExecuteAgent_团队Agent构造失败(t *testing.T) {
	restoreTeamFuncs(t)
	TeamAgentFromPayloadFunc = func(_ context.Context, _ map[string]any) (any, error) {
		return nil, errors.New("boom")
	}
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{
		AgentKind: SpawnAgentKindTeamAgent,
		Payload:   map[string]any{"team_id": "t1"},
	}, nil, &bytes.Buffer{}, false, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FromSpawnPayload 失败")
}

// TestExecuteAgent_团队Agent流式未注入 流式路径缺函数注入时报错。
func TestExecuteAgent_团队Agent流式未注入(t *testing.T) {
	restoreTeamFuncs(t)
	TeamAgentFromPayloadFunc = func(_ context.Context, _ map[string]any) (any, error) { return "a", nil }
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{
		AgentKind: SpawnAgentKindTeamAgent,
		Payload:   map[string]any{"team_id": "t1"},
	}, nil, &bytes.Buffer{}, true, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TeamRunnerStreamingFunc 尚未注入")
}

// TestExecuteAgent_团队Agent流式函数类型不符 注入值类型不对时报错。
func TestExecuteAgent_团队Agent流式函数类型不符(t *testing.T) {
	restoreTeamFuncs(t)
	TeamAgentFromPayloadFunc = func(_ context.Context, _ map[string]any) (any, error) { return "a", nil }
	TeamRunnerStreamingFunc = 123
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{
		AgentKind: SpawnAgentKindTeamAgent,
		Payload:   map[string]any{"team_id": "t1"},
	}, nil, &bytes.Buffer{}, true, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "函数类型不匹配")
}

// TestExecuteAgent_团队Agent非流式未注入 非流式路径缺函数注入时报错。
func TestExecuteAgent_团队Agent非流式未注入(t *testing.T) {
	restoreTeamFuncs(t)
	TeamAgentFromPayloadFunc = func(_ context.Context, _ map[string]any) (any, error) { return "a", nil }
	_, err := ExecuteAgent(context.Background(), SpawnAgentConfig{
		AgentKind: SpawnAgentKindTeamAgent,
		Payload:   map[string]any{"team_id": "t1"},
	}, nil, &bytes.Buffer{}, false, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TeamRunner 尚未注入")
}

// TestRunAgentTask_成功写DONE 执行成功写出 DONE 并通知完成。
func TestRunAgentTask_成功写DONE(t *testing.T) {
	restoreTeamFuncs(t)
	TeamAgentFromPayloadFunc = func(_ context.Context, _ map[string]any) (any, error) { return "a", nil }
	TeamRunnerFunc = func(_ context.Context, _ any, _ map[string]any, _ any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}

	var out bytes.Buffer
	doneCh := make(chan struct{}, 1)
	runAgentTask(context.Background(), SpawnAgentConfig{
		AgentKind: SpawnAgentKindTeamAgent,
		Payload:   map[string]any{"team_id": "t1"},
	}, map[string]any{}, &out, "msg-1", false, nil, doneCh, nil, nil)

	select {
	case <-doneCh:
	default:
		t.Fatal("必须通知完成")
	}
	msgs := decodeMessages(t, &out)
	_, hasDone := findMessage(msgs, MessageTypeDone)
	assert.True(t, hasDone)
}

// TestRunAgentTask_失败写ERROR 执行失败写出 ERROR 并通知完成。
func TestRunAgentTask_失败写ERROR(t *testing.T) {
	restoreTeamFuncs(t)
	var out bytes.Buffer
	doneCh := make(chan struct{}, 1)
	runAgentTask(context.Background(), SpawnAgentConfig{AgentKind: "unknown"}, map[string]any{}, &out, "msg-1", false, nil, doneCh, nil, nil)

	select {
	case <-doneCh:
	default:
		t.Fatal("必须通知完成")
	}
	msgs := decodeMessages(t, &out)
	errMsg, hasErr := findMessage(msgs, MessageTypeError)
	require.True(t, hasErr)
	payload, ok := errMsg.Payload.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, payload["error"], "不支持的 Agent 启动方式")
}

// TestPrepareSpawnAgentConfig_空配置 入参为空返回 nil。
func TestPrepareSpawnAgentConfig_空配置(t *testing.T) {
	assert.Nil(t, prepareSpawnAgentConfig(nil))
}

// TestPrepareSpawnAgentConfig_应用日志配置 解析成功并应用 logging_config。
func TestPrepareSpawnAgentConfig_应用日志配置(t *testing.T) {
	cfg := prepareSpawnAgentConfig(map[string]any{
		"agent_kind":     "team_agent",
		"logging_config": map[string]any{"level": "debug"},
	})
	require.NotNil(t, cfg)
	assert.Equal(t, SpawnAgentKindTeamAgent, cfg.AgentKind)
}

// TestResolveLogLevelFromConfig_四分支 覆盖命中/缺失/非字符串/空串。
func TestResolveLogLevelFromConfig_四分支(t *testing.T) {
	assert.Equal(t, "debug", resolveLogLevelFromConfig(map[string]any{"level": "debug"}))
	assert.Equal(t, "", resolveLogLevelFromConfig(map[string]any{}))
	assert.Equal(t, "", resolveLogLevelFromConfig(map[string]any{"level": 1}))
	assert.Equal(t, "", resolveLogLevelFromConfig(map[string]any{"level": ""}))
}

// TestApplyLoggingConfigMap_空配置不报错 nil 配置直接返回。
func TestApplyLoggingConfigMap_空配置不报错(t *testing.T) {
	applyLoggingConfigMap(nil)
}

// TestApplyLoggingConfigFromEnv_三种输入 覆盖未设置/非法 JSON/合法 JSON。
func TestApplyLoggingConfigFromEnv_三种输入(t *testing.T) {
	t.Setenv(EnvSpawnLoggingConfig, "")
	applyLoggingConfigFromEnv()

	t.Setenv(EnvSpawnLoggingConfig, "{not-json")
	applyLoggingConfigFromEnv()

	t.Setenv(EnvSpawnLoggingConfig, `{"level":"warn"}`)
	applyLoggingConfigFromEnv()
}

// TestSetTeamRunnerFunc_写入全局变量 设置函数后全局变量同步更新。
func TestSetTeamRunnerFunc_写入全局变量(t *testing.T) {
	restoreTeamFuncs(t)
	fn := func(_ context.Context, _ any, _ map[string]any, _ any) (map[string]any, error) { return nil, nil }
	SetTeamRunnerFunc(fn)
	assert.NotNil(t, TeamRunnerFunc)
}

// restoreTeamFuncs 清空注入的团队函数，避免用例间串味。
func restoreTeamFuncs(t *testing.T) {
	t.Helper()
	TeamRunnerFunc = nil
	TeamRunnerStreamingFunc = nil
	TeamAgentFromPayloadFunc = nil
	t.Cleanup(func() {
		TeamRunnerFunc = nil
		TeamRunnerStreamingFunc = nil
		TeamAgentFromPayloadFunc = nil
	})
}
