// Package executable 可执行节点契约叶包。
//
// 从 graph 根包 executable.go 抽出的破环迁移（偏差 D-17-8）：workflow/components
// 需实现该契约但不引 graph 根（components→graph→session→stream_actor→ability 链
// 与 graph→components 会成环），故将 StreamChunk/BaseExecutable/Executable/
// GeneralExecutor 本体沉至本叶包；graph 根 executable.go 经泛型类型别名
// （Go 1.24+）保持 graph.Executable 等锚点不变。
//
// 文件目录：
//
//	executable/
//	├── doc.go           # 包文档
//	└── executable.go    # StreamChunk/BaseExecutable/Executable 契约
//
// 对应 Python 代码：openjiuwen/core/graph/executable.py
package executable
