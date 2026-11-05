// Package main 扫描前端、本仓与上游三方的 Web WS RPC 方法注册，
// 生成 docs/sync/web-rpc-matrix.md 契约矩阵，并在 -verify 模式下作为 CI 门禁。
//
// 矩阵除覆盖状态外还给出「下游状态」：该方法在上游是网关本地实现，
// 还是薄代理到 AgentServer；若为代理，本仓是否已具备对应的 req method 常量
// 与 AgentServer 处理器。下游缺失的方法只登记、不在本层实现，清单由
// docs/sync/web-rpc-downstream-gaps.md 承载。
//
// 文件目录：
//
//	rpc-matrix/
//	├── doc.go        # 包文档
//	├── main.go       # 入口与参数
//	├── scan.go       # 三方扫描器
//	├── classify.go   # 上游 handler 形态与下游状态判定
//	├── matrix.go     # 归类与渲染
//	└── scan_test.go  # 扫描与判定单测
//
// 对应 Python 代码：无（本仓工程工具）。
package main
