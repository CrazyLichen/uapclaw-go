// Package decorator 提供模型客户端、工具、工作流的追踪装饰器。
//
// 将 TracedModelClient/TracedTool/TracedWorkflow 装饰器及 Decorate*WithTrace 工厂函数
// 从 tracer 父包抽出至独立子包，避免 tracer → single_agent/interfaces 循环依赖。
// TracerSession 接口定义装饰器所需的会话最小接口（Tracer + AgentSpan），
// *internal.AgentSession 天然满足此接口。
//
// 文件目录：
//
//	decorator/
//	├── doc.go           # 子包文档
//	└── decorator.go     # TracedModelClient/TracedTool/TracedWorkflow 装饰器 + Decorate*WithTrace + TracerSession 接口
//
// 对应 Python 代码：openjiuwen/core/session/tracer/decorator.py
package decorator
