package executable

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamChunk 流式元素：Val 为正常产出，Err 非 nil 表示流中错误（此后流关闭）。
// 对齐仓内流式惯例（AssistantMessageChunk 内嵌 error 字段先例）；
// Python AsyncIterator 迭代中途 raise ≙ 消费方收到 Err 元素后停止迭代（spec 偏差 D3）。
type StreamChunk[O any] struct {
	// Val 正常产出块
	Val O
	// Err 流中错误；非 nil 时为流的最后一个元素，送达后生产方关闭 channel
	Err error
}

// BaseExecutable Executable 嵌入基座：提供四钩子默认值与四入口默认未实现错误。
// 对齐 Python Executable 基类默认实现；组件嵌入后按需覆写
// （Python 继承 → Go 嵌入，spec 偏差 D1）。
type BaseExecutable[I, O any] struct{}

// ──────────────────────────── 接口 ────────────────────────────

// Executable 可执行节点契约，对齐 Python Executable[Input, Output] 泛型基类
// （executable.py:13-61）。四执行入口构成"单/流 × 单/流"矩阵，
// 四策略钩子由 8.11 Vertex 在节点执行时读取。
type Executable[I, O any] interface {
	// OnInvoke 单进单出（Python on_invoke，executable.py:14）
	OnInvoke(ctx context.Context, inputs I, sess session.BaseSession, kwargs map[string]any) (O, error)
	// OnStream 单进流出（Python on_stream，executable.py:23）；
	// 第二返回值承载启动错误，流中错误经 StreamChunk.Err 作为末元素送达
	OnStream(ctx context.Context, inputs I, sess session.BaseSession, kwargs map[string]any) (<-chan StreamChunk[O], error)
	// OnCollect 流进单出（Python on_collect，executable.py:32）：聚合上游多块输入为一份输出
	OnCollect(ctx context.Context, inputs <-chan I, sess session.BaseSession, kwargs map[string]any) (O, error)
	// OnTransform 流进流出（Python on_transform，executable.py:41）：逐块变换转发
	OnTransform(ctx context.Context, inputs <-chan I, sess session.BaseSession, kwargs map[string]any) (<-chan StreamChunk[O], error)
	// SkipTrace 是否跳过 tracer 埋点（默认 false；Python executable.py:50）
	SkipTrace() bool
	// GraphInvoker 是否子图递归入口（默认 false；Python executable.py:53）
	GraphInvoker() bool
	// PostCommit 执行完是否提交 WorkflowState（默认 true，与其他钩子不对称；Python executable.py:56）
	PostCommit() bool
	// ComponentType 组件类型标签，写进 span（默认空串；Python executable.py:59）
	ComponentType() string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── BaseExecutable 方法 ────────────────────────────

// SkipTrace 默认不跳过埋点（Python executable.py:50-51）
func (BaseExecutable[I, O]) SkipTrace() bool { return false }

// GraphInvoker 默认非子图入口（Python executable.py:53-54）
func (BaseExecutable[I, O]) GraphInvoker() bool { return false }

// PostCommit 默认提交状态（Python executable.py:56-57，默认值与其他钩子不对称）
func (BaseExecutable[I, O]) PostCommit() bool { return true }

// ComponentType 默认空标签（Python executable.py:59-60）
func (BaseExecutable[I, O]) ComponentType() string { return "" }

// OnInvoke 默认未实现（Python executable.py:14-21 文案语义复刻；偏差 D2 不带类名）
func (BaseExecutable[I, O]) OnInvoke(_ context.Context, _ I, _ session.BaseSession, _ map[string]any) (O, error) {
	var zero O
	return zero, errors.New("OnInvoke 未实现：请在 Executable 子类覆写本方法以提供推理逻辑（对齐 Python on_invoke 抽象语义）")
}

// OnStream 默认未实现（Python executable.py:23-30；偏差 D2）
func (BaseExecutable[I, O]) OnStream(_ context.Context, _ I, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[O], error) {
	return nil, errors.New("OnStream 未实现：请在 Executable 子类覆写本方法以提供流式逻辑（对齐 Python on_stream 抽象语义）")
}

// OnCollect 默认未实现（Python executable.py:32-39；偏差 D2）
func (BaseExecutable[I, O]) OnCollect(_ context.Context, _ <-chan I, _ session.BaseSession, _ map[string]any) (O, error) {
	var zero O
	return zero, errors.New("OnCollect 未实现：请在 Executable 子类覆写本方法以提供聚合逻辑（对齐 Python on_collect 抽象语义）")
}

// OnTransform 默认未实现（Python executable.py:41-48；偏差 D2）
func (BaseExecutable[I, O]) OnTransform(_ context.Context, _ <-chan I, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[O], error) {
	return nil, errors.New("OnTransform 未实现：请在 Executable 子类覆写本方法以提供变换逻辑（对齐 Python on_transform 抽象语义）")
}

// ──────────────────────────── 导出类型别名 ────────────────────────────

// GeneralExecutor 最一般的执行器：map 载荷进、map 载荷出。
// 对齐 Python executable.py:63 同名别名（dict[str, Any] → map[string]any）；
// Python 全仓零消费死别名，纯复刻不发明调用方（spec 偏差 D6）。
type GeneralExecutor = Executable[map[string]any, map[string]any]
