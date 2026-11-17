package graph

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InputsKey 顶层调用信封中业务输入的键（Python base.py:7 INPUTS_KEY = "inputs"）。
	// 打包只发生在跨图/跨组件边界（workflow.py:527/600/633、vertex.py:137/156），
	// 节点到节点内部传递不重新打包。
	InputsKey = "inputs"
	// ConfigKey 顶层调用信封中执行配置的键（Python base.py:7 CONFIG_KEY = "config"）
	ConfigKey = "config"
)

// ──────────────────────────── 接口 ────────────────────────────

// ExecutableGraph 可执行图契约，对齐 Python base.py:12-30 ExecutableGraph(Executable[I,O])。
// 在 Executable 之上追加：Invoke 信封拆分（Python invoke，base.py:13-14）、
// 默认 pass 的 Stream/Collect/Transform/Interrupt（偏差 D5，与父类抛 NotImplementedError
// 不同——Python 侧本类是 pass 静默空实现）、抽象 _invoke 的导出对应物 Invoke_
// （偏差 D4，8.1 CompiledGraph 实现并消费）。
//
// Python 的方法默认实现（invoke 拆信封体 / stream 等 pass 体）在 Go 接口上无承载点，
// 由下方 DefaultGraphXxx 包级泛型函数提供唯一参考实现（默认体单点定义不被抄错）；
// 实现者覆写时写真实现，沿用默认时一行转手 DefaultGraphXxx 即可——
// 外层调用方一律走接口方法 g.Invoke/g.Stream，覆写经接口派发天然可达。
type ExecutableGraph[I, O any] interface {
	Executable[I, O]
	// Invoke 拆信封执行（Python invoke，base.py:13-14）；
	// 默认实现见 DefaultGraphInvoke
	Invoke(inputs map[string]any, sess session.BaseSession) (O, error)
	// Stream 流式执行（Python stream，base.py:16-17 默认 pass）；默认实现见 DefaultGraphStream
	Stream(inputs map[string]any, sess session.BaseSession) (<-chan StreamChunk[O], error)
	// Collect 聚合执行（Python collect，base.py:19-20 默认 pass）；默认实现见 DefaultGraphCollect
	Collect(inputs <-chan I, sess session.BaseSession) (O, error)
	// Transform 流式变换（Python transform，base.py:22-23 默认 pass）；默认实现见 DefaultGraphTransform
	Transform(inputs <-chan I, sess session.BaseSession) (<-chan StreamChunk[O], error)
	// Interrupt 中断图执行（Python interrupt，base.py:25-26 默认 pass）；默认实现见 DefaultGraphInterrupt
	Interrupt(message map[string]any) error
	// Invoke_ 拆信封后的抽象执行入口（Python 抽象方法 _invoke，base.py:28-30；
	// Go 接口方法须导出，下划线后缀保留 Python 名字痕迹，偏差 D4）
	Invoke_(inputs I, sess session.BaseSession, config any) (O, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultGraphInvoke Python invoke 的默认实现（base.py:13-14）：拆信封 {inputs:..., config:...}
// → 转发 Invoke_。对齐 .get 宽容取值——config 缺失传 nil 不报错
// （业务输入缺失=硬错误是消费方 loop_comp.py:171 的语义，留 8.20 复刻，本节不做）。
// 实现者实现 Invoke 时一行转手本函数即沿用默认行为。
func DefaultGraphInvoke[I, O any](g ExecutableGraph[I, O], envelope map[string]any, sess session.BaseSession) (O, error) {
	// Python: inputs.get(INPUTS_KEY) / inputs.get(CONFIG_KEY)——缺失即 nil，宽容
	var inputs I
	if raw, ok := envelope[InputsKey].(I); ok {
		inputs = raw
	}
	return g.Invoke_(inputs, sess, envelope[ConfigKey])
}

// DefaultGraphStream Python stream 的默认实现（base.py:16-17）：
// 返回已关闭的空 channel + nil error（偏差 D5）。
// 实现者实现 Stream 时一行转手本函数即沿用默认 pass。
func DefaultGraphStream[I, O any](_ ExecutableGraph[I, O], _ map[string]any, _ session.BaseSession) (<-chan StreamChunk[O], error) {
	ch := make(chan StreamChunk[O])
	close(ch)
	return ch, nil
}

// DefaultGraphCollect Python collect 的默认实现（base.py:19-20）：零值 + nil error（偏差 D5）。
func DefaultGraphCollect[I, O any](_ ExecutableGraph[I, O], _ <-chan I, _ session.BaseSession) (O, error) {
	var zero O
	return zero, nil
}

// DefaultGraphTransform Python transform 的默认实现（base.py:22-23）：
// 返回已关闭的空 channel + nil error（偏差 D5）。
func DefaultGraphTransform[I, O any](_ ExecutableGraph[I, O], _ <-chan I, _ session.BaseSession) (<-chan StreamChunk[O], error) {
	ch := make(chan StreamChunk[O])
	close(ch)
	return ch, nil
}

// DefaultGraphInterrupt Python interrupt 的默认实现（base.py:25-26）：nil error（偏差 D5）。
func DefaultGraphInterrupt[I, O any](_ ExecutableGraph[I, O], _ map[string]any) error {
	return nil
}
