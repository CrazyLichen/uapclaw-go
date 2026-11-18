// Package graph 提供图引擎顶层的共享类型定义，对齐 openjiuwen/core/graph 包根。
//
// 含 GraphState（节点输出聚合 schema）与 AtomicNode（8.10）：GraphState 在 Python
// 侧为零运行时效果的 TypedDict 类型标注，唯一消费者是 vertex.py:364 对
// Vertex.__call__ state 形参的类型标注；节点返回值在 pregel/task.py:152 被丢弃，
// Annotated[list, operator.add] reducer 全仓无归并执行点，故 Go 侧仅复刻类型
// 不实现归并（spec 偏差 D1）。与子包 pregel 的检查点 GraphState
// （internal/agentcore/graph/pregel/state.go）同名不同义。
// AtomicNode（atomic_node.go）为节点级原子提交包装（Python atomic_node.py，
// 8.10），消费方为 8.11 Vertex 与 8.19/8.20 条件组件；当前无即时消费者，
// 属后续章节的地基件。
//
// Executable（executable.go）为可执行节点契约（8.2）：泛型接口承载四执行入口
// （OnInvoke/OnStream/OnCollect/OnTransform，"单/流 × 单/流"矩阵）与四策略钩子
// （SkipTrace/GraphInvoker/PostCommit/ComponentType，由 8.11 Vertex 消费）；
// 流式语义经 StreamChunk（Err 非 nil 为末元素后关闭）承载。ExecutableGraph
// （executable_graph.go）为可执行图契约：接口声明 Invoke/Stream/Collect/Transform/
// Interrupt/Invoke_ 全量方法面，Invoke 信封拆分（InputsKey/ConfigKey）对齐
// base.py:13-14；Python 方法默认实现（invoke 拆信封体 / stream 等 pass 体）由
// DefaultGraphXxx 包级泛型函数单点承载（偏差 D5），实现者一行转手沿用默认、
// 覆写写真实现，外层调用方一律走接口方法。
//
// 文件目录：
//
//	graph/
//	├── doc.go                 # 包文档
//	├── base.go               # Graph 声明式构图接口 + Router 类型别名（8.1，Python base.py）
//	├── graph.go              # Branch/PregelGraph/CompiledGraph 构图与编译门面（8.1，Python graph.py）
//	├── executable.go          # Executable 泛型接口 + StreamChunk + BaseExecutable 基座（8.2）
//	├── executable_graph.go    # ExecutableGraph 契约 + InputsKey/ConfigKey 信封常量（8.2）
//	├── atomic_node.go         # AtomicNode 原子提交包装（8.10）
//	├── graph_state.go        # GraphState 节点输出聚合 schema
//	└── vertex.go             # Vertex 图顶点执行器（8.11，双链批入/流入执行体）
//
// Vertex（vertex.go）为图顶点执行器（8.11，Python vertex.py）：双链模型——批入侧
// （INVOKE/STREAM 同步链）+ 流入侧（COLLECT/TRANSFORM，StreamActor 首帧异步链），
// 两半经当轮 streamDone channel 握手对齐；并发映射零锁，依赖引擎屏障定序不变式。
//
// PregelGraph（graph.go）为声明式构图门面（8.1，Python graph.py）：构图期登记
// 节点/边/条件边（校验对齐 graph.py:103-132 四 error 分支），Compile 期三段翻译
// 至 PregelBuilder（waits 前驱 CNF 屏障解析 graph.py:180-237 → builder.AddEdge
// CNF 组形态），CompiledGraph 承载 invoke 生命周期（pre/post_workflow_execute、
// commit_user_inputs、取消三分支保 HITL 快照 graph.py:349-394）。
// Router（base.go）= pregel.Selector 纯别名；运行时判断靠捕获 session 引用，
// state 形参恒 nil（对齐 router.py:37 字面量，8.5 D2 同源）。
//
// 对应 Python 代码：openjiuwen/core/graph/graph_state.py · atomic_node.py · executable.py · base.py · vertex.py
package graph
