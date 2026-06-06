// Package logger 提供基于 zerolog 的结构化分级日志系统。
//
// 支持以下能力：
//   - 多级别日志输出（debug/info/warn/error/fatal）
//   - 分组件文件输出（common.log/gateway.log/channel.log/agent_server.log/permissions.log/agent_core.log）
//   - 全量汇总文件输出（full.log）
//   - 控制台彩色输出
//   - 日志文件轮转（lumberjack，默认 20MB/20 备份）
//   - 敏感数据过滤（4 层正则 + 7 种模式，写入前自动脱敏）
//   - 与 Config 集成（各通道独立级别配置，环境变量覆盖）
//   - 并发安全（mutexWriter 保护共享文件写入）
//
// 架构：每个组件（Common/Gateway/Channel/AgentServer/Permissions/AgentCore）创建独立的 zerolog.Logger 实例，
// 通过组件级日志函数（Info/Warn/Error/Debug/Fatal）传入 Component 参数使用。
// 每个 Logger 实例的 writer 同时写入组件日志文件、full.log 和控制台。
// Permissions 组件额外写入 agent_server.log。
//
// 使用方式：
//
//	包级定义组件常量：
//	  const logComponent = logger.ComponentCommon
//
//	调用日志函数：
//	  logger.Info(logComponent).Str("key", "val").Msg("消息")
//	  logger.Error(logComponent).Err(err).Msg("失败")
//
// 文件目录：
//
//	logger/
//	├── doc.go           # 包文档
//	├── logger.go        # 核心结构体、Option 模式、Setup/组件级日志函数/Close
//	├── levels.go        # LogLevel 枚举、LoggingLevels 结构体、级别解析
//	├── sanitizer.go     # 敏感数据脱敏（4 层正则 + 7 种模式 + SanitizerWriter）
//	├── rotation.go      # RotationConfig + MutexWriter（包装 lumberjack）
//	├── component.go     # Component 枚举
//	└── *_test.go        # 单元测试
//
// 对应 Python 代码：jiuwenswarm/common/utils.py (logging setup)
package logger
