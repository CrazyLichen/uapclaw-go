// Package config 提供 Runner 全局配置类型及读写接口。
//
// 本包对齐 Python runner_config.py，定义消息队列类型枚举、
// 分布式配置、Pulsar 配置、Runner 全局配置等结构体，
// 并提供 set/get_runner_config 全局单例读写函数。
//
// 核心概念：
//
//	MessageQueueType   — 消息队列类型枚举（PULSAR / FAKE）
//	PulsarConfig       — Pulsar 连接配置（URL + MaxWorkers）
//	MessageQueueConfig — 消息队列配置（类型 + Pulsar 子配置）
//	DistributedConfig  — 分布式模式配置（超时、并发、topic 模板等）
//	RunnerConfig       — Runner 全局配置（分布式开关、Checkpointer、Session 控制器等）
//
// 文件目录：
//
//	config/
//	├── doc.go        # 包文档
//	├── enums.go      # MessageQueueType 枚举
//	├── models.go     # PulsarConfig / MessageQueueConfig / DistributedConfig / RunnerConfig
//	└── global.go     # DEFAULT_RUNNER_CONFIG / SetRunnerConfig / GetRunnerConfig
//
// 对应 Python 代码：openjiuwen/core/runner/runner_config.py
package config
