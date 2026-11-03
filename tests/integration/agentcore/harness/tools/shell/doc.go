//go:build integration

// Package shell 提供 bash/powershell 工具的集成测试。
//
// 测试覆盖：
//   - bash 工具未传 timeout 时回落默认 300 秒（回归：曾把缺失 key 的 0 钳成 1 秒）
//   - bash 工具显式传 timeout=1 时命令按 1 秒超时
//   - powershell 工具未传 timeout 时不产生「执行超时 1 秒」
//
// 文件目录：
//
//	shell/
//	├── doc.go              # 包文档
//	└── shell_tools_test.go # bash/powershell 工具集成测试
//
// 对应 Python 代码：tests/system_tests/harness/tools（bash/powershell 工具）。
package shell
