// Package loop 提供循环组件全链：LoopGroup 循环体子工作流、LoopComponent 装配厂、
// AdvancedLoopComponent 循环执行体（自建 Pregel 小图：START→condition→body→post_body→condition
// 回边）、LoopBreak/LoopSetVariable 组件与 PostLoopBody 计数件。
//
// 运行链：外层图 Vertex（is_subgraph 装信封）→ LoopComponent.invoke 拆信封 → 现场造
// ConditionInSession + callbacks + AdvancedLoopComponent → 小图跑环（条件节点路由走
// 8.10 AtomicInvoke 原子链）→ OutputCallback 收割 → 结果作为节点输出写回外层 state。
//
// 对应 Python 代码：openjiuwen/core/workflow/components/flow/loop/loop_comp.py
//
// 文件目录：
//
//	loop/
//	├── doc.go        # 包文档
//	└── loop_comp.go  # 全部循环组件（Python loop_comp.py 489 行一比一单文件）
package loop
