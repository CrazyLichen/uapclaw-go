// Package pregel 提供 Pregel 图引擎的配置与常量层。
//
// 本包是领域八的编译期最底层：构图（8.1）、节点与通道抽象（8.3）、
// 调度引擎（8.3b/8.3c）、通道管理（8.4）、顶点（8.11）、工作流（8.14）
// 均依赖本包提供的保留节点名、终止态键、递归预算与配置归一化能力。
//
// 配置以值类型结构体表达：字段零值等价于 Python TypedDict(total=False)
// 的「键缺失/None」，因此不存在指针字段，调用方也无须判空。
//
// 文件目录：
//
//	pregel/
//	├── doc.go         # 包文档
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
//
// 对应 Python 代码：openjiuwen/core/graph/pregel/config.py · constants.py
package pregel
