// Package agentface member_optimizer agents 子包装配产物的统一载体与能力面。
//
// 父包 member_optimizer 需要消费 agents 子包的工厂产物，而 agents 子包反向
// 引用父包的 BudgetedRsiModel 等类型——直接互相 import 即成环。本 leaf 包
// 承载桥两端的**具体替身类型**（原先以 any 传递），父包与 agents 均只引本包：
//
//	member_optimizer → agentface ← agents
//
// 依赖方向均为向下（agentcore/harness、llm/schema），无环。桥两端签名从此
// 编译期 checked（消除 any 破环，偏差 D-c1-1 的加固）。
//
// 文件目录：
//
//	agentface/
//	└── agent.go        # Agent 载体 + EvidenceFinalizer 接口
//
// 对应 Python 代码：无独立对应物（Python 鸭子类型的 agent 对象直传）。
package agentface
