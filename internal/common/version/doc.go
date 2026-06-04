// Package version 提供版本号统一管理。
//
// 所有子命令和构建脚本通过此包获取当前版本号，
// 确保整个项目版本信息单一来源。
//
// 文件目录：
//
//	version/
//	├── doc.go       # 包文档
//	└── version.go   # 版本号定义和获取函数
//
// 对应 Python 代码：jiuwenswarm/common/version.py
package version
