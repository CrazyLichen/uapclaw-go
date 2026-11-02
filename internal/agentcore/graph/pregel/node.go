package pregel

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// NodeInput 节点函数入参：把 Python 的「按形参名条件注入」换成显式结构体传递。
//
// 纪律：唯一构造点是 8.3b 的 NodeTask；包内别处不应构造。
// Python: pregel/task.py:137-149
type NodeInput struct {
	// Config 本次执行的内部配置，Ns/ParentNs 由 8.3b 改写为 parent:node:version
	Config InnerPregelConfig
	// State 恒 nil，对齐 task.py:149 的字面量 None。
	//
	// 只能是 any：Python 注解的 graph_state.GraphState 全仓零实例化、零读取，
	// 且该类型属 graph 包，而 graph 要 import 本包，收窄即成编译期环（8.3 spec §3.2）。
	State any
}

// NodeFunc 节点执行体。
//
// 纪律：只返回普通错误。图级中断是 panic（*GraphInterrupt），由 8.3b 用 recover
// 转成返回值；本类型内不得 recover 或吞掉它（对齐 task.py:172-174 的 except 顺序）。
// 出边完全由 PregelNode 的 routers 决定，返回值不参与路由（Python 同）。
type NodeFunc func(ctx context.Context, in NodeInput) error
