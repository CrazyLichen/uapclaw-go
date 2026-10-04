package testhelpers

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BlockingTool 阻塞工具，对齐 Python _BlockingTool。
//
// 进入时发出信号（Entered），阻塞等待门控释放（Release）。
// 用于测试 steer/follow_up 在工具执行期间注入的场景。
//
// Python: tests/system_tests/harness/test_steer_inner_loop.py _BlockingTool
type BlockingTool struct {
	// mu 保护并发访问
	mu sync.Mutex
	// gate 门控通道：Release() 关闭后继续执行
	gate chan struct{}
	// entered 信号通道：工具已被调用
	entered chan struct{}
	// invokeCount 调用次数
	invokeCount atomic.Int32
	// released 是否已释放
	released atomic.Bool
	// toolInstance 底层 tool.Tool 实例
	toolInstance tool.Tool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBlockingTool 创建阻塞工具。
// toolName 为工具名称，返回值同时实现 tool.Tool 接口。
func NewBlockingTool(toolName string) *BlockingTool {
	bt := &BlockingTool{
		gate:    make(chan struct{}),
		entered: make(chan struct{}, 1),
	}

	tc := tool.NewToolCardWithID(toolName, toolName, "阻塞测试工具: "+toolName, nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			bt.invokeCount.Add(1)
			// 通知测试：工具已进入
			select {
			case bt.entered <- struct{}{}:
			default:
			}
			// 阻塞等待释放
			select {
			case <-bt.gate:
				return map[string]any{"status": "blocked_tool_done"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, nil,
	)
	bt.toolInstance = t
	return bt
}

// Tool 返回底层 tool.Tool 实例，用于注册到 Agent。
func (bt *BlockingTool) Tool() tool.Tool {
	return bt.toolInstance
}

// WaitEntered 等待工具被调用（阻塞直到 Entered 信号）。
// 对齐 Python blocking_tool.entered.wait()。
func (bt *BlockingTool) WaitEntered(ctx context.Context) error {
	select {
	case <-bt.entered:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release 释放门控，允许工具继续执行。
// 对齐 Python blocking_tool.gate.set()。
func (bt *BlockingTool) Release() {
	if bt.released.CompareAndSwap(false, true) {
		close(bt.gate)
	}
}

// InvokeCount 返回工具被调用的次数。
func (bt *BlockingTool) InvokeCount() int32 {
	return bt.invokeCount.Load()
}

// ──────────────────────────── 非导出函数 ────────────────────────────
