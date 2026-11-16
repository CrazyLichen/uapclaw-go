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
// 在 Executable 之上追加：Invoke 信封拆分、默认 pass 的流式四方法（偏差 D5，
// 与父类抛 NotImplementedError 不同——Python 侧本类是 pass 静默空实现）、
// 抽象 _invoke 的导出对应物 Invoke_（偏差 D4，8.1 CompiledGraph 实现并消费）。
// 默认 pass 行为经包级泛型函数 GraphInvoke/GraphStream/GraphCollect/GraphTransform/
// GraphInterrupt 承载（Python 继承默认方法 → Go 包级函数，消费方调 GraphXxx(g, ...)
// 即得"默认 pass + 实现者可覆写"两态）。
type ExecutableGraph[I, O any] interface {
	Executable[I, O]
	// Invoke_ 拆信封后的抽象执行入口（Python 抽象方法 _invoke，base.py:28-30；
	// Go 接口方法须导出，下划线后缀保留 Python 名字痕迹，偏差 D4）
	Invoke_(inputs I, sess session.BaseSession, config any) (O, error)
	// Interrupt 中断图执行（Python base.py:25-26 默认 pass）
	Interrupt(message map[string]any) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GraphInvoke 拆信封执行：inputs 信封 {inputs:..., config:...} → Invoke_。
// 对齐 Python base.py:13-14：.get 宽容取值——config 缺失传 nil 不报错
// （业务输入缺失=硬错误是消费方 loop_comp.py:171 的语义，留 8.20 复刻，本节不做）。
func GraphInvoke[I, O any](g ExecutableGraph[I, O], envelope map[string]any, sess session.BaseSession) (O, error) {
	// Python: inputs.get(INPUTS_KEY) / inputs.get(CONFIG_KEY)——缺失即 nil，宽容
	var inputs I
	if raw, ok := envelope[InputsKey].(I); ok {
		inputs = raw
	}
	return g.Invoke_(inputs, sess, envelope[ConfigKey])
}

// GraphStream 默认 pass 的流式执行（Python base.py:16-17）：
// 返回已关闭的空 channel + nil error（偏差 D5）。
func GraphStream[I, O any](_ ExecutableGraph[I, O], _ map[string]any, _ session.BaseSession) (<-chan StreamChunk[O], error) {
	ch := make(chan StreamChunk[O])
	close(ch)
	return ch, nil
}

// GraphCollect 默认 pass 的聚合执行（Python base.py:19-20）：返回零值 + nil error（偏差 D5）
func GraphCollect[I, O any](_ ExecutableGraph[I, O], _ <-chan I, _ session.BaseSession) (O, error) {
	var zero O
	return zero, nil
}

// GraphTransform 默认 pass 的流式变换（Python base.py:22-23）：
// 返回已关闭的空 channel + nil error（偏差 D5）
func GraphTransform[I, O any](_ ExecutableGraph[I, O], _ <-chan I, _ session.BaseSession) (<-chan StreamChunk[O], error) {
	ch := make(chan StreamChunk[O])
	close(ch)
	return ch, nil
}

// GraphInterrupt 默认 pass 的中断（Python base.py:25-26）：nil error（偏差 D5）
func GraphInterrupt[I, O any](_ ExecutableGraph[I, O], _ map[string]any) error {
	return nil
}
