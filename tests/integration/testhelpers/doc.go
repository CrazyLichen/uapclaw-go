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
//	└── model_call_observer.go    # 模型调用观测器
package testhelpers
