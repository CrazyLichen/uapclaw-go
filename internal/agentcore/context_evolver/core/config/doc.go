// Package config 提供 context_evolver 的配置管理。
//
// 对齐 Python openjiuwen/extensions/context_evolver/core/config.py，
// 实现懒加载全局单例，支持 .env + config.yaml + 环境变量三级数据源。
// 任何包可随时调用 config.Get(key, default) 读取配置。
//
// 数据源优先级（对齐 Python）：
//  1. .env 文件（最高优先级，敏感凭据如 API_KEY）
//  2. config.yaml（不覆盖 .env 已有 key）
//  3. os.Getenv 环境变量（兜底）
//
// 文件目录：
//
//	config/
//	├── doc.go           # 包文档
//	├── config.go        # 懒加载全局单例 + Get/Load/Set
//	└── config_test.go   # 单元测试
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/config.py
package config
