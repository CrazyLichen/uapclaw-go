// Package config 提供会话配置的具体实现。
//
// 核心类型：
//   - MetadataLike         — 回调元数据结构体
//   - BuiltinConfigLoader  — 内置配置加载钩子接口
//   - defaultSessionConfig — SessionConfig 的默认实现
//
// 文件目录：
//
//	config/
//	├── doc.go             # 包文档
//	├── config.go          # MetadataLike、BuiltinConfigLoader、defaultSessionConfig
//	├── env_loader.go      # 环境变量加载（trySetEnv、loadEnvConfigs 等）
//	└── context.go         # WithEnvs context 注入函数
//
// 对应 Python 代码：openjiuwen/core/session/config/base.py
package config
