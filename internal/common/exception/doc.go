// Package exception 提供统一的异常体系和错误码定义。
//
// 核心设计原则：
//   - StatusCode 是语义主键，ErrorCategory 表达控制语义，两者解耦通过映射表绑定
//   - 所有异常必须挂一个 StatusCode，禁止绕开体系直接返回裸 error
//   - 消息模板渲染容忍缺失占位符，错误路径上不产生新异常
//
// 模块地图：
//
//	exception/
//	├── doc.go                 # 包文档
//	├── status_code.go         # StatusCode 结构体定义 + 模板渲染
//	├── errors.go              # BaseError 结构体 + ErrorCategory + 工厂函数
//	├── status_mapping.go      # StatusCode → ErrorCategory 三级映射
//	├── codes_framework.go     # SUCCESS/ERROR + Foundation 区间 (180000-189999)
//	├── codes_workflow.go      # Workflow + Component 区间 (100000-119999)
//	├── codes_agent.go         # Agent + MultiAgent + DevTools 区间 (120000-140099)
//	├── codes_runner.go        # Runner + Resource + Callback 区间 (110000-110699)
//	├── codes_session.go       # Session + Stream + Tracer 区间 (111000-111999)
//	├── codes_graph.go         # Graph Engine 区间 (112000-112999)
//	├── codes_context.go       # Context + Retrieval + Memory 区间 (150000-159999)
//	├── codes_toolchain.go     # Toolchain + AgentRL 区间 (170000-179999)
//	├── codes_tool.go          # Tool 定义与执行区间 (182000-182999)
//	└── codes_security.go      # Security + SysOperation 区间 (190000-199999)
//
// 使用方式：
//
//	// 1. 返回错误
//	return exception.RaiseError(
//	    exception.StatusWorkflowExecutionError,
//	    exception.WithParam("reason", "timeout"),
//	    exception.WithParam("workflow", wfID),
//	)
//
//	// 2. 构建错误对象（延迟处理）
//	err := exception.BuildError(exception.StatusToolExecutionError, exception.WithCause(cause))
//
//	// 3. 强制使用特定类别
//	return exception.SystemError(exception.StatusMessageQueueInitiationError, exception.WithCause(err))
//
//	// 4. 判断控制语义
//	var baseErr *exception.BaseError
//	if errors.As(err, &baseErr) && baseErr.IsFatal() { ... }
//
// 对应 Python 代码：openjiuwen/core/common/exception/
package exception
