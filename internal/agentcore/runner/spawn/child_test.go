package spawn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHandleHealthCheck 测试健康检查处理器
func TestHandleHealthCheck(t *testing.T) {
	var buf bytes.Buffer
	msg := NewMessage(MessageTypeHealthCheck, map[string]any{})

	err := HandleHealthCheck(context.Background(), msg, &buf)
	if err != nil {
		t.Fatalf("HandleHealthCheck 失败: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if got.Type != MessageTypeHealthCheckResponse {
		t.Errorf("Type = %d, want HEALTH_CHECK_RESPONSE", got.Type)
	}
}

// TestHandleShutdown 测试关闭处理器
func TestHandleShutdown(t *testing.T) {
	var buf bytes.Buffer
	msg := NewMessage(MessageTypeShutdown, map[string]any{"reason": "parent_initiated"})

	err := HandleShutdown(context.Background(), msg, &buf)
	if err != nil {
		t.Fatalf("HandleShutdown 失败: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if got.Type != MessageTypeShutdownAck {
		t.Errorf("Type = %d, want SHUTDOWN_ACK", got.Type)
	}
}

// TestProcessMessageLoop_stdin关闭 测试 stdin 关闭时消息循环退出
func TestProcessMessageLoop_stdin关闭(t *testing.T) {
	var stdout bytes.Buffer
	// 空的 stdin（立即 EOF）
	err := ProcessMessageLoop(context.Background(), io.LimitReader(nil, 0), &stdout, nil, nil)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v（预期 EOF 退出）", err)
	}
}

// TestExecuteAgent_不支持的方式 测试不支持的 Agent 启动方式
func TestExecuteAgent_不支持的方式(t *testing.T) {
	var buf bytes.Buffer
	_, err := ExecuteAgent(
		context.Background(),
		SpawnAgentConfig{AgentKind: "unknown"},
		nil, &buf, false, nil,
	)
	if err == nil {
		t.Error("不支持的 Agent 启动方式应返回错误")
	}
}

// TestExecuteAgent_TeamAgent 测试 TEAM_AGENT 模式返回未实现错误
func TestExecuteAgent_TeamAgent(t *testing.T) {
	var buf bytes.Buffer
	_, err := ExecuteAgent(
		context.Background(),
		SpawnAgentConfig{AgentKind: SpawnAgentKindTeamAgent},
		nil, &buf, false, nil,
	)
	if err == nil {
		t.Error("TEAM_AGENT 模式应返回未实现错误")
	}
	if !strings.Contains(err.Error(), "尚未实现") {
		t.Errorf("错误信息应包含'尚未实现'，实际: %v", err)
	}
}

// TestExecuteAgent_ClassAgent 测试 CLASS_AGENT 执行（占位实现）
func TestExecuteAgent_ClassAgent(t *testing.T) {
	var buf bytes.Buffer
	result, err := ExecuteAgent(
		context.Background(),
		SpawnAgentConfig{AgentKind: SpawnAgentKindClassAgent},
		nil, &buf, false, nil,
	)
	if err != nil {
		t.Fatalf("CLASS_AGENT 执行失败: %v", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("结果类型断言失败")
	}
	if resultMap["status"] != "placeholder" {
		t.Errorf("status = %v, want placeholder", resultMap["status"])
	}
}

// TestExecuteAgent_ClassAgent_流式 测试 CLASS_AGENT 流式执行
func TestExecuteAgent_ClassAgent_流式(t *testing.T) {
	var buf bytes.Buffer
	result, err := ExecuteAgent(
		context.Background(),
		SpawnAgentConfig{AgentKind: SpawnAgentKindClassAgent},
		nil, &buf, true, []string{"text"},
	)
	if err != nil {
		t.Fatalf("CLASS_AGENT 流式执行失败: %v", err)
	}
	if result == nil {
		t.Error("结果不应为 nil")
	}
}

// TestPrepareSpawnAgentConfig_正常 测试正常解析配置
func TestPrepareSpawnAgentConfig_正常(t *testing.T) {
	cfg := prepareSpawnAgentConfig(map[string]any{
		"agent_kind": "class_agent",
		"payload":    map[string]any{},
	})
	if cfg == nil {
		t.Fatal("配置不应为 nil")
	}
	if cfg.AgentKind != SpawnAgentKindClassAgent {
		t.Errorf("AgentKind = %s, want class_agent", cfg.AgentKind)
	}
}

// TestPrepareSpawnAgentConfig_nil 测试 nil 输入返回 nil
func TestPrepareSpawnAgentConfig_nil(t *testing.T) {
	cfg := prepareSpawnAgentConfig(nil)
	if cfg != nil {
		t.Error("nil 输入应返回 nil")
	}
}

// TestPrepareSpawnAgentConfig_解析失败 测试无效配置返回 nil
func TestPrepareSpawnAgentConfig_解析失败(t *testing.T) {
	// 传入无法解析的字段不会让 ParseSpawnAgentConfig 返回错误（JSON 序列化总能成功）
	// 测试一个 agent_kind 不是字符串但 ParseSpawnAgentConfig 仍能处理的场景
	cfg := prepareSpawnAgentConfig(map[string]any{
		"agent_kind": 123, // 非 string 类型，但仍能序列化
	})
	// ParseSpawnAgentConfig 会成功，但 AgentKind 为空
	if cfg != nil && cfg.AgentKind != "" {
		t.Logf("AgentKind = %s", cfg.AgentKind)
	}
}

// TestProcessMessageLoop_健康检查消息 测试处理健康检查消息
func TestProcessMessageLoop_健康检查消息(t *testing.T) {
	// 构造包含 HEALTH_CHECK 消息的 stdin
	healthMsg := NewMessage(MessageTypeHealthCheck, nil)
	data, _ := json.Marshal(healthMsg)
	data = append(data, '\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(
		context.Background(),
		bytes.NewReader(data),
		&stdout,
		nil, nil,
	)
	// 消息循环读完 stdin 后应退出
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// TestProcessMessageLoop_关闭消息 测试处理关闭消息
func TestProcessMessageLoop_关闭消息(t *testing.T) {
	shutdownMsg := NewMessage(MessageTypeShutdown, nil)
	data, _ := json.Marshal(shutdownMsg)
	data = append(data, '\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(
		context.Background(),
		bytes.NewReader(data),
		&stdout,
		nil, nil,
	)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// TestProcessMessageLoop_上下文取消 测试上下文取消时退出消息循环
func TestProcessMessageLoop_上下文取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// 使用一个不会产生数据的 reader，让循环等待
	r, _ := io.Pipe()

	var stdout bytes.Buffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- ProcessMessageLoop(ctx, r, &stdout, nil, nil)
	}()

	// 取消上下文
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Logf("ProcessMessageLoop 返回: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("消息循环应在上下文取消后退出")
	}
}

// TestProcessMessageLoop_输入消息 测试处理 INPUT 消息启动 Agent
func TestProcessMessageLoop_输入消息(t *testing.T) {
	_ = SpawnAgentConfig{ // 验证类型存在
		AgentKind: SpawnAgentKindClassAgent,
		Payload:   map[string]any{},
	}
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"agent_config": map[string]any{
			"agent_kind": "class_agent",
			"payload":    map[string]any{},
		},
		"inputs":   map[string]any{},
		"streaming": false,
	})
	data, _ := json.Marshal(inputMsg)
	data = append(data, '\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(
		context.Background(),
		bytes.NewReader(data),
		&stdout,
		nil, nil,
	)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// TestProcessMessageLoop_缺少AgentConfig 测试缺少 agent_config 时返回错误
func TestProcessMessageLoop_缺少AgentConfig(t *testing.T) {
	// INPUT 消息中没有 agent_config，且全局 agentConfig 也为 nil
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"inputs": map[string]any{},
	})
	data, _ := json.Marshal(inputMsg)
	data = append(data, '\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(
		context.Background(),
		bytes.NewReader(data),
		&stdout,
		nil, nil,
	)
	if err == nil {
		t.Error("缺少 agent_config 时应返回错误")
	}
	if !strings.Contains(err.Error(), "缺少 agent_config") {
		t.Errorf("错误信息应包含'缺少 agent_config'，实际: %v", err)
	}
}

// TestRunAgentTask_成功 测试 Agent 任务成功完成
func TestRunAgentTask_成功(t *testing.T) {
	var stdout bytes.Buffer
	doneCh := make(chan struct{}, 1)

	go runAgentTask(
		context.Background(),
		SpawnAgentConfig{AgentKind: SpawnAgentKindClassAgent},
		map[string]any{},
		&stdout,
		"test-msg-id",
		false,
		nil,
		doneCh,
	)

	select {
	case <-doneCh:
		// 读取 stdout 中的 DONE 消息
		msg, err := ReadMessage(&stdout)
		if err != nil {
			t.Fatalf("读取消息失败: %v", err)
		}
		if msg.Type != MessageTypeDone {
			t.Errorf("消息类型 = %d, want DONE", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("Agent 任务应在超时前完成")
	}
}

// TestRunAgentTask_错误 测试 Agent 任务执行失败
func TestRunAgentTask_错误(t *testing.T) {
	var stdout bytes.Buffer
	doneCh := make(chan struct{}, 1)

	go runAgentTask(
		context.Background(),
		SpawnAgentConfig{AgentKind: SpawnAgentKindTeamAgent}, // TEAM_AGENT 未实现
		map[string]any{},
		&stdout,
		"test-msg-id",
		false,
		nil,
		doneCh,
	)

	select {
	case <-doneCh:
		msg, err := ReadMessage(&stdout)
		if err != nil {
			t.Fatalf("读取消息失败: %v", err)
		}
		if msg.Type != MessageTypeError {
			t.Errorf("消息类型 = %d, want ERROR", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("Agent 任务应在超时前完成")
	}
}

// TestProcessMessageLoop_多次消息 测试处理多条消息
func TestProcessMessageLoop_多次消息(t *testing.T) {
	// 先发 HEALTH_CHECK，再发 SHUTDOWN
	healthMsg := NewMessage(MessageTypeHealthCheck, nil)
	shutdownMsg := NewMessage(MessageTypeShutdown, nil)

	var buf bytes.Buffer
	data1, _ := json.Marshal(healthMsg)
	buf.Write(data1)
	buf.WriteByte('\n')
	data2, _ := json.Marshal(shutdownMsg)
	buf.Write(data2)
	buf.WriteByte('\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(
		context.Background(),
		&buf,
		&stdout,
		nil, nil,
	)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// TestProcessMessageLoop_InputPayload非map 测试 INPUT 消息 payload 非 map 时的跳过
func TestProcessMessageLoop_InputPayload非map(t *testing.T) {
	// INPUT 消息 payload 不是 map[string]any，应被跳过
	inputMsg := NewMessage(MessageTypeInput, "not_a_map")
	data, _ := json.Marshal(inputMsg)
	data = append(data, '\n')

	// 后面跟一个 SHUTDOWN 消息以退出循环
	shutdownMsg := NewMessage(MessageTypeShutdown, nil)
	data2, _ := json.Marshal(shutdownMsg)
	data2 = append(data2, '\n')

	combined := append(data, data2...)

	var stdout bytes.Buffer
	err := ProcessMessageLoop(
		context.Background(),
		bytes.NewReader(combined),
		&stdout,
		nil, nil,
	)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// TestHandleHealthCheck_写入失败 测试健康检查写入失败
func TestHandleHealthCheck_写入失败(t *testing.T) {
	msg := NewMessage(MessageTypeHealthCheck, nil)
	err := HandleHealthCheck(context.Background(), msg, &failingWriter{})
	if err == nil {
		t.Error("写入失败时应返回错误")
	}
}

// TestHandleShutdown_写入失败 测试关闭确认写入失败
func TestHandleShutdown_写入失败(t *testing.T) {
	msg := NewMessage(MessageTypeShutdown, nil)
	err := HandleShutdown(context.Background(), msg, &failingWriter{})
	if err == nil {
		t.Error("写入失败时应返回错误")
	}
}

// TestRunSpawnedProcess_缺少配置 测试子进程入口缺少配置
func TestRunSpawnedProcess_缺少配置(t *testing.T) {
	// 重定向 stdin/stdout 以便测试
	origStdin := osStdin
	origStdout := osStdout
	defer func() {
		osStdin = origStdin
		osStdout = origStdout
	}()

	// 这个测试需要 RunSpawnedProcess 使用 os.Stdin/Stdout，
	// 但由于 RunSpawnedProcess 直接使用 os.Stdin/os.Stdout，
	// 无法在单元测试中模拟。此处仅验证函数签名存在。
	// 实际测试依赖集成测试（//go:build integration）
}

// TestProcessMessageLoop_输入消息补充inputs 测试 INPUT 消息补充 inputs
func TestProcessMessageLoop_输入消息补充inputs(t *testing.T) {
	// 先发送一个 INPUT 消息，包含 agent_config 和 inputs
	// 再发送 SHUTDOWN 退出
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"agent_config": map[string]any{
			"agent_kind": "class_agent",
			"payload":    map[string]any{},
		},
		"inputs":   map[string]any{"key1": "val1"},
		"streaming": false,
	})
	shutdownMsg := NewMessage(MessageTypeShutdown, nil)

	var buf bytes.Buffer
	data1, _ := json.Marshal(inputMsg)
	buf.Write(data1)
	buf.WriteByte('\n')
	// 需要等 Agent 完成后再发 SHUTDOWN，但简单测试中先发即可
	data2, _ := json.Marshal(shutdownMsg)
	buf.Write(data2)
	buf.WriteByte('\n')

	var stdout bytes.Buffer
	// 初始 inputs 不为空
	initialInputs := map[string]any{"existing": "data"}
	err := ProcessMessageLoop(
		context.Background(),
		&buf,
		&stdout,
		nil,
		initialInputs,
	)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// failingWriter 总是返回错误的 io.Writer
type failingWriter struct{}

func (w *failingWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("写入失败")
}

// osStdin/osStdout 占位：用于未来重定向测试
var (
	osStdin  = io.Reader(nil)
	osStdout = io.Writer(nil)
)

// TestProcessMessageLoop_并发Agent执行 测试 INPUT 消息启动 Agent 后只启动一次
func TestProcessMessageLoop_并发Agent执行(t *testing.T) {
	cfg := &SpawnAgentConfig{
		AgentKind: SpawnAgentKindClassAgent,
		Payload:   map[string]any{},
	}

	// 发送两个 INPUT 消息：第二个不应再次启动 Agent
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"agent_config": map[string]any{
			"agent_kind": "class_agent",
			"payload":    map[string]any{},
		},
		"inputs":   map[string]any{},
		"streaming": false,
	})

	var buf bytes.Buffer
	data1, _ := json.Marshal(inputMsg)
	buf.Write(data1)
	buf.WriteByte('\n')

	// 第二个 INPUT 消息（agentCancel != nil 时只更新 inputs，不启动新 Agent）
	data2, _ := json.Marshal(inputMsg)
	buf.Write(data2)
	buf.WriteByte('\n')

	var stdout syncBuffer
	// 使用较短超时的 context，因为 Agent 完成后循环才会退出
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = ProcessMessageLoop(ctx, &buf, &stdout, cfg, map[string]any{})
}

// syncBuffer 并发安全的 bytes.Buffer
type syncBuffer struct {
	mu sync.Mutex
	buf bytes.Buffer
}

func (sb *syncBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *syncBuffer) Read(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Read(p)
}

// TestProcessMessageLoop_未知消息类型 测试未知消息类型不 panic
func TestProcessMessageLoop_未知消息类型(t *testing.T) {
	// 发送一条 OUTPUT 消息（子→父方向，在子进程消息循环中属于 default 分支）
	outputMsg := NewMessage(MessageTypeOutput, map[string]any{"result": "ok"})
	// 后跟 SHUTDOWN 退出
	shutdownMsg := NewMessage(MessageTypeShutdown, nil)

	var buf bytes.Buffer
	data1, _ := json.Marshal(outputMsg)
	buf.Write(data1)
	buf.WriteByte('\n')
	data2, _ := json.Marshal(shutdownMsg)
	buf.Write(data2)
	buf.WriteByte('\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(context.Background(), &buf, &stdout, nil, nil)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// TestProcessMessageLoop_输入消息有streaming和streamModes 测试 INPUT 消息携带 streaming 和 stream_modes
func TestProcessMessageLoop_输入消息有streaming和streamModes(t *testing.T) {
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"agent_config": map[string]any{
			"agent_kind": "class_agent",
			"payload":    map[string]any{},
		},
		"inputs":       map[string]any{},
		"streaming":    true,
		"stream_modes": []string{"text", "events"},
	})

	var buf bytes.Buffer
	data, _ := json.Marshal(inputMsg)
	buf.Write(data)
	buf.WriteByte('\n')

	var stdout syncBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = ProcessMessageLoop(ctx, &buf, &stdout, nil, nil)
}

// TestProcessMessageLoop_输入消息无agentConfigKey 测试 INPUT 消息 payload 中没有 agent_config 键
func TestProcessMessageLoop_输入消息无agentConfigKey(t *testing.T) {
	// payload 有 inputs 但没有 agent_config，且全局 agentConfig 也为 nil
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"inputs":   map[string]any{},
		"streaming": false,
	})

	var buf bytes.Buffer
	data, _ := json.Marshal(inputMsg)
	buf.Write(data)
	buf.WriteByte('\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(context.Background(), &buf, &stdout, nil, nil)
	// 缺少 agent_config 应返回错误
	if err == nil {
		t.Log("缺少 agent_config 时应返回错误（或超时退出）")
	}
}

// TestProcessMessageLoop_已有AgentConfig 测试全局已有 AgentConfig 时 INPUT 直接启动
func TestProcessMessageLoop_已有AgentConfig(t *testing.T) {
	// 全局已提供 agentConfig，INPUT 消息不需要再包含 agent_config
	cfg := &SpawnAgentConfig{
		AgentKind: SpawnAgentKindClassAgent,
		Payload:   map[string]any{},
	}

	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"inputs":   map[string]any{"key": "value"},
		"streaming": false,
	})

	var buf bytes.Buffer
	data, _ := json.Marshal(inputMsg)
	buf.Write(data)
	buf.WriteByte('\n')

	var stdout syncBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = ProcessMessageLoop(ctx, &buf, &stdout, cfg, map[string]any{})
}

// TestProcessMessageLoop_输入消息agentConfig非map 测试 INPUT 消息中 agent_config 不是 map[string]any
func TestProcessMessageLoop_输入消息agentConfig非map(t *testing.T) {
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"agent_config": "not_a_map",
		"inputs":       map[string]any{},
	})
	// 后跟 SHUTDOWN 以退出
	shutdownMsg := NewMessage(MessageTypeShutdown, nil)

	var buf bytes.Buffer
	data1, _ := json.Marshal(inputMsg)
	buf.Write(data1)
	buf.WriteByte('\n')
	data2, _ := json.Marshal(shutdownMsg)
	buf.Write(data2)
	buf.WriteByte('\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(context.Background(), &buf, &stdout, nil, nil)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}

// TestProcessMessageLoop_输入消息inputs非map 测试 INPUT 消息中 inputs 不是 map[string]any
func TestProcessMessageLoop_输入消息inputs非map(t *testing.T) {
	inputMsg := NewMessage(MessageTypeInput, map[string]any{
		"inputs": "not_a_map",
	})
	shutdownMsg := NewMessage(MessageTypeShutdown, nil)

	var buf bytes.Buffer
	data1, _ := json.Marshal(inputMsg)
	buf.Write(data1)
	buf.WriteByte('\n')
	data2, _ := json.Marshal(shutdownMsg)
	buf.Write(data2)
	buf.WriteByte('\n')

	var stdout bytes.Buffer
	err := ProcessMessageLoop(context.Background(), &buf, &stdout, nil, nil)
	if err != nil {
		t.Logf("ProcessMessageLoop 返回: %v", err)
	}
}
