//go:build integration

// Package spawn 提供子代理 spawn 的集成测试。
//
// 被测边界是「真实子进程」：用例启动独立进程跑子代理，验证进程启动、输入投递与
// 结果回收，不 mock 进程本身，因此归入集成测试而非单元测试。
//
// 文件目录：
//
//	spawn/
//	├── doc.go       # 包文档
//	└── spawn_test.go # 子进程 spawn 的启动与结果回收用例
//
// 对应 Python 代码：openjiuwen/core/runner/spawn/
package spawn
