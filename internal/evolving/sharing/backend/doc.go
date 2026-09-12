// Package backend 提供经验共享的后端存储接口和实现。
//
// SharingBackend 定义了上传/下载/搜索的抽象契约，
// LocalFileBackend 是基于本地文件系统的参考实现。
//
// 文件目录：
//
//	backend/
//	├── doc.go          # 子包文档
//	├── interface.go    # SharingBackend 抽象接口
//	└── local_file.go   # LocalFileBackend 本地文件实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/sharing/backends/
package backend
