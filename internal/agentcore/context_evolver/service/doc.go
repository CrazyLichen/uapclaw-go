// Package service 提供 Context Evolver 的服务层函数。
//
// 包含轨迹格式化、试验评估、MaTTS 试验运行等独立函数，
// 供 ContextEvolutionRail 和其他上层调用方使用。
//
// 文件目录：
//
//	service/
//	├── doc.go                     # 包文档
//	└── trajectory_generator.go    # 轨迹生成和 MaTTS 试验函数
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/service/trajectory_generator.py
package service
