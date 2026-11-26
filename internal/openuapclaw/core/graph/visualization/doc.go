// Package visualization 提供工作流图的可视化镜像与 Mermaid 导出。
//
// Python 真相源：openjiuwen/core/graph/visualization/（六文件一比一映射）。
// 环破除偏差 D-13-5：本包只依赖 common/exception；flow/loop/llm 反向 import
// 本包实现能力接口（LoopSubgraphSource/SubgraphSource/BranchRouterSource/
// DrawableBranchTargetsSource），workflow import 本包构造 Drawable 与 Draw()。
//
// 文件目录：
//
//	visualization/
//	├── doc.go                      # 包文档
//	├── drawable_node.go           # DrawableNode 数据结构（drawable_node.py）
//	├── drawable_edge.go           # DrawableEdge + DrawableBranchRouter（drawable_edge.py）
//	├── drawable_graph.go          # DrawableGraph（drawable_graph.py）
//	├── drawable_subgraph_node.go  # DrawableSubgraphNode（drawable_subgraph_node.py）
//	└── drawable.go                # Drawable 构建器 + Mermaid 拼接 + 渲染（drawable.py）
//
// 对应 Python 代码：openjiuwen/core/graph/visualization/
package visualization
