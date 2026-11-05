// Package testhelpers 提供集成测试公共辅助工具。
//
// 对齐 Python tests/system_tests/agent/react_agent/interrupt/test_base.py 的测试基础设施，
// 包括断言助手、工具追踪 Rail、阻塞工具、模型调用观测器等。
//
// 文件目录：
//
//	testhelpers/
//	├── doc.go                    # 包文档
//	├── helpers.go                # 断言和构造助手函数
//	├── controlled_react_agent.go # 可控内层 Agent（对齐 Python ControlledReactAgent）
//	├── tool_trace_rail.go        # 工具调用追踪 Rail
//	├── blocking_tool.go          # 阻塞工具
//	├── model_call_observer.go    # 模型调用观测器
//	├── adapter_env.go            # CodeAdapter 集成测试隔离环境（数据目录/provider/收尾）
//	├── dreaming_env.go           # Dreaming 集成测试隔离环境（会话播种/mtime/响应排队/prompt 抽取）
//	├── context.go                # MockSessionFacade 最小会话门面（上下文类集成测试复用）
//	├── plan_env.go               # PlanEnv 计划模式环境（AgentServer 入口驱动 + 计划模式/计划文件读取）
//	├── server_env.go             # ServerEnv 进程内 AgentServer 环境（按 E2A 信封投递 + 出站帧收集）
//	├── web_rpc.go                # CallWebRPC 经真实 WebSocket 链路调用 web RPC 方法
//	├── pty_linux.go              # Linux PTY 终端模拟（覆盖 isatty 交互分支）
//	└── pty_stub.go               # 非 Linux 同签名占位（OpenPTY 返回 ErrPTYUnsupported）
package testhelpers
