package graph

import (
	"context"

	pregel "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
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
// ctx 形参（2026-11-16 用户拍板，D-6 改写）：Python 经 asyncio 协程天然携带取消，
// Go 以显式 ctx 传播——五方法对称加 ctx，I/O TypeVar 本无类型（executable.py:9-10），
// ctx 非类型面扩张、属取消语义的语言映射。
//
// Python 的方法默认实现（invoke 拆信封体 / stream 等 pass 体）在 Go 接口上无承载点，
// 由下方 DefaultGraphXxx 包级泛型函数提供唯一参考实现（默认体单点定义不被抄错）；
// 实现者覆写时写真实现，沿用默认时一行转手 DefaultGraphXxx 即可——
// 外层调用方一律走接口方法 g.Invoke/g.Stream，覆写经接口派发天然可达。
type ExecutableGraph[I, O any] interface {
	Executable[I, O]
	// Invoke 拆信封执行（Python invoke，base.py:13-14）；
	// 默认实现见 DefaultGraphInvoke
	Invoke(ctx context.Context, inputs map[string]any, sess session.BaseSession) (O, error)
	// Stream 流式执行（Python stream，base.py:16-17 默认 pass）；默认实现见 DefaultGraphStream
	// 帧元素类型 any（2026-11-21 用户拍板，D-8 延伸）：Python AsyncIterator 无元素类型
	// 约束，组件产出 OutputSchema/dict 等异构帧；绑 O 会迫使组件装箱信封（D-17-1）
	// 并在边界出口反复拆封（8.14b 嵌套流式五层断裂链实证）
	Stream(ctx context.Context, inputs map[string]any, sess session.BaseSession) (<-chan StreamChunk[any], error)
	// Collect 聚合执行（Python collect，base.py:19-20 默认 pass）；默认实现见 DefaultGraphCollect
	Collect(ctx context.Context, inputs <-chan I, sess session.BaseSession) (O, error)
	// Transform 流式变换（Python transform，base.py:22-23 默认 pass）；默认实现见 DefaultGraphTransform
	Transform(ctx context.Context, inputs <-chan I, sess session.BaseSession) (<-chan StreamChunk[any], error)
	// Interrupt 中断图执行（Python interrupt，base.py:25-26 默认 pass）；默认实现见 DefaultGraphInterrupt
	Interrupt(ctx context.Context, message map[string]any) error
	// Invoke_ 拆信封后的抽象执行入口（Python 抽象方法 _invoke，base.py:28-30；
	// Go 接口方法须导出，下划线后缀保留 Python 名字痕迹，偏差 D4）。
	// config 收窄为 *PregelConfig（2026-11-16 用户拍板，D-8：Python 形参注解 Any
	// 但全仓唯一合法类型即 PregelConfig/None——None→nil，信封断言失败即 nil 宽容）
	Invoke_(ctx context.Context, inputs I, sess session.BaseSession, config *pregel.PregelConfig) (O, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultGraphInvoke Python invoke 的默认实现（base.py:13-14）：拆信封 {inputs:..., config:...}
// → 转发 Invoke_。对齐 .get 宽容取值——config 缺失传 nil 不报错
// （业务输入缺失=硬错误是消费方 loop_comp.py:171 的语义，留 8.20 复刻，本节不做）。
// 实现者实现 Invoke 时一行转手本函数即沿用默认行为。
func DefaultGraphInvoke[I, O any](g ExecutableGraph[I, O], ctx context.Context, envelope map[string]any, sess session.BaseSession) (O, error) {
	// Python: inputs.get(INPUTS_KEY) / inputs.get(CONFIG_KEY)——缺失即 nil，宽容
	var inputs I
	if raw, ok := envelope[InputsKey].(I); ok {
		inputs = raw
	}
	// config 判定对齐 Python is_main = config is None：信封 config 可为
	// *PregelConfig（顶层 Invoke 直调）或 *InnerPregelConfig（子图嵌套——父图
	// task 层创建的节点级 config 经 SubWorkflowComponent 信封透传，Python
	// workflow.py:600 CONFIG_KEY 透传使子图 isMain=False 跳过 pre_workflow_execute，
	// 由引擎自 graph_store 自动恢复快照；原 D-8 单形态断言使嵌套恢复被
	// checkpointer 拒绝，实测 Python 官方嵌套恢复测试后修正）。
	var cfg *pregel.PregelConfig
	switch c := envelope[ConfigKey].(type) {
	case *pregel.PregelConfig:
		cfg = c
	case *pregel.InnerPregelConfig:
		cfg = &c.PregelConfig
	case pregel.InnerPregelConfig:
		// 值形态：task 层 NodeInput.Config 以值注入（task.go:294），经信封原样透传
		cfg = &c.PregelConfig
	}
	return g.Invoke_(ctx, inputs, sess, cfg)
}

// DefaultGraphStream Python stream 的默认实现（base.py:16-17）：
// 返回已关闭的空 channel + nil error（偏差 D5）。
// 实现者实现 Stream 时一行转手本函数即沿用默认 pass。
func DefaultGraphStream[I, O any](_ ExecutableGraph[I, O], _ context.Context, _ map[string]any, _ session.BaseSession) (<-chan StreamChunk[any], error) {
	ch := make(chan StreamChunk[any])
	close(ch)
	return ch, nil
}

// DefaultGraphCollect Python collect 的默认实现（base.py:19-20）：零值 + nil error（偏差 D5）。
func DefaultGraphCollect[I, O any](_ ExecutableGraph[I, O], _ context.Context, _ <-chan I, _ session.BaseSession) (O, error) {
	var zero O
	return zero, nil
}

// DefaultGraphTransform Python transform 的默认实现（base.py:22-23）：
// 返回已关闭的空 channel + nil error（偏差 D5）。
func DefaultGraphTransform[I, O any](_ ExecutableGraph[I, O], _ context.Context, _ <-chan I, _ session.BaseSession) (<-chan StreamChunk[any], error) {
	ch := make(chan StreamChunk[any])
	close(ch)
	return ch, nil
}

// DefaultGraphInterrupt Python interrupt 的默认实现（base.py:25-26）：nil error（偏差 D5）。
func DefaultGraphInterrupt[I, O any](_ ExecutableGraph[I, O], _ context.Context, _ map[string]any) error {
	return nil
}
