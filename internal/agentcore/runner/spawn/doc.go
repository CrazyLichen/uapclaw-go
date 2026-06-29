// Package spawn 提供 Spawn 子进程机制，通过 JSON over stdin/stdout（NDJSON 协议）
// 实现父子进程通信，在独立子进程中隔离运行 Agent。
//
// 核心组件：
//   - MessageType/Message：通信协议消息类型和结构
//   - SpawnAgentConfig/ClassAgentSpawnConfig：Agent 启动配置
//   - SpawnConfig：子进程管理配置（健康检查间隔、关闭超时等）
//   - SpawnedProcessHandle：父端子进程句柄（通信、健康检查、关闭）
//   - ChildRunner：子进程 Runner 接口（由 runner.ChildRunnerImpl 实现，避免循环依赖）
//   - AgentCreator：Agent 创建接口（由 factory.DefaultAgentCreator 实现）
//   - SpawnProcess()：创建子进程的工厂函数
//   - RunSpawnedProcess()/ProcessMessageLoop()：子端入口和消息循环
//
// 通信协议：
//
//	父子进程通过 stdin/stdout 交换 NDJSON 消息（每行一个 JSON 对象）。
//	消息类型：INPUT, OUTPUT, HEALTH_CHECK, HEALTH_CHECK_RESPONSE,
//	SHUTDOWN, SHUTDOWN_ACK, ERROR, STREAM_CHUNK, DONE。
//
// 子进程入口：
//
//	通过 uapclaw spawn-child 子命令启动，
//	环境变量 UAPCLAW_SPAWN_PROCESS=1 标识子进程身份，
//	UAPCLAW_SPAWN_LOGGING_CONFIG 传递日志配置。
//	子进程启动后自动从环境变量应用日志配置，并根据 SpawnAgentConfig.logging_config
//	支持运行时动态重配（通过 logger.Reconfigure）。
//
// 文件目录：
//
//	spawn/
//	├── doc.go              # 包文档
//	├── protocol.go         # 消息协议（MessageType 枚举 + Message 结构体 + 序列化/反序列化）
//	├── config.go           # 配置模型（SpawnAgentKind + SpawnAgentConfig + ClassAgentSpawnConfig + SpawnConfig）
//	├── handle.go           # 父端进程管理器（SpawnedProcessHandle：通信/健康检查/关闭）
//	├── process.go          # 子进程创建工厂（SpawnProcess 函数）
//	├── child.go            # 子端逻辑（ChildRunner 接口 + 消息循环 + Agent 执行 + 日志重配）
//	└── factory/            # Agent 创建工厂（DefaultAgentCreator：switch 按类型创建 Agent 实例）
//
// 对应 Python 代码：openjiuwen/core/runner/spawn/
package spawn
