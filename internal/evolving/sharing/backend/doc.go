// Package backend 提供经验共享的后端存储实现。
//
// SharingBackend 接口定义在 sharing 包的 interface.go 中，
// LocalFileBackend 是基于本地文件系统的参考实现。
//
// 文件目录：
//
//	backend/
//	├── doc.go          # 子包文档
//	└── local_file.go   # LocalFileBackend 本地文件实现（隐式实现 sharing.SharingBackend）
//
// 对应 Python 代码：openjiuwen/agent_evolving/sharing/backends/
package backend
