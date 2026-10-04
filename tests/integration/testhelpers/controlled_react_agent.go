package testhelpers

import (
	"context"
	"sync"
	"time"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InvokeCallRecord 记录一次 Invoke 调用的详细信息。
type InvokeCallRecord struct {
	// CallNo 调用序号（从 1 开始递增）
	CallNo int
	// Inputs 调用输入
	Inputs map[string]any
	// Opts 调用选项
	Opts []agentinterfaces.AgentOption
}

// ControlledReactAgent 可控内层 Agent，对齐 Python ControlledReactAgent。
//
// 用于测试外部循环（task loop）的行为：测试可以阻塞指定调用序号，
// 在阻塞期间注入 steer/follow_up，然后释放继续执行。
//
// Python: tests/system_tests/harness/test_deep_agent_outer_loop_system.py
//
//	ControlledReactAgent
type ControlledReactAgent struct {
	// mu 保护并发访问
	mu sync.Mutex
	// invokeCalls 所有 Invoke 调用记录
	invokeCalls []InvokeCallRecord
	// blockedCalls 需要阻塞的调用序号集合
	blockedCalls map[int]struct{}
	// gates 阻塞门控，每个调用序号对应一个
	gates map[int]chan struct{}
	// callStarted 调用启动信号通道
	callStarted chan int
	// callNoCounter 下一次调用的序号
	callNoCounter int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewControlledReactAgent 创建可控内层 Agent。
// blockedCalls 为需要阻塞的调用序号集合（从 1 开始）。
func NewControlledReactAgent(blockedCalls ...int) *ControlledReactAgent {
	blocked := make(map[int]struct{}, len(blockedCalls))
	for _, callNo := range blockedCalls {
		blocked[callNo] = struct{}{}
	}
	return &ControlledReactAgent{
		blockedCalls:  blocked,
		gates:         make(map[int]chan struct{}),
		callStarted:   make(chan int, 64),
		callNoCounter: 0,
	}
}

// Invoke 实现 DeepAgent innerInvokeOverride 签名。
// 记录调用，阻塞指定序号，返回可控结果。
func (c *ControlledReactAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	c.mu.Lock()
	c.callNoCounter++
	callNo := c.callNoCounter
	c.invokeCalls = append(c.invokeCalls, InvokeCallRecord{
		CallNo: callNo,
		Inputs: inputs,
		Opts:   opts,
	})
	isBlocked := false
	if _, ok := c.blockedCalls[callNo]; ok {
		isBlocked = true
		gate := make(chan struct{})
		c.gates[callNo] = gate
	}
	c.mu.Unlock()

	// 通知测试：此调用已启动
	select {
	case c.callStarted <- callNo:
	default:
	}

	// 如果需要阻塞，等待释放
	if isBlocked {
		select {
		case <-c.getGate(callNo):
			// 已释放
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// 提取 query 用于输出
	query := ""
	if q, ok := inputs["query"]; ok {
		if qs, ok := q.(string); ok {
			query = qs
		}
	}

	return map[string]any{
		"output":      "ok:" + query,
		"result_type": "answer",
		"call_no":     callNo,
	}, nil
}

// Stream 实现 DeepAgent innerStreamOverride 签名。
// 委托给 Invoke 并包装为 stream channel。
func (c *ControlledReactAgent) Stream(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	result, err := c.Invoke(ctx, inputs, opts...)
	if err != nil {
		return nil, err
	}
	ch := make(chan stream.Schema, 1)
	ch <- stream.OutputSchema{
		Type:    "answer",
		Index:   0,
		Payload: result,
	}
	close(ch)
	return ch, nil
}

// WaitCallStarted 等待指定调用序号启动，超时返回错误。
// 对齐 Python ControlledReactAgent.wait_call_started(call_no, timeout)。
func (c *ControlledReactAgent) WaitCallStarted(callNo int, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case got := <-c.callStarted:
			if got == callNo {
				return nil
			}
			// 不是目标调用，继续等待
		case <-deadline:
			return context.DeadlineExceeded
		}
	}
}

// ReleaseCall 释放指定调用序号的阻塞门控。
// 对齐 Python ControlledReactAgent.release_call(call_no)。
func (c *ControlledReactAgent) ReleaseCall(callNo int) {
	c.mu.Lock()
	gate, ok := c.gates[callNo]
	c.mu.Unlock()
	if ok && gate != nil {
		close(gate)
	}
}

// InvokeCallCount 返回总 Invoke 调用次数。
func (c *ControlledReactAgent) InvokeCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.invokeCalls)
}

// GetInvokeCall 返回第 i 次（0-indexed）调用记录。
func (c *ControlledReactAgent) GetInvokeCall(i int) *InvokeCallRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < 0 || i >= len(c.invokeCalls) {
		return nil
	}
	return &c.invokeCalls[i]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getGate 获取指定调用序号的门控 channel。
func (c *ControlledReactAgent) getGate(callNo int) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gates[callNo]
}
