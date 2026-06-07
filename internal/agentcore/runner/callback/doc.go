// Package callback 提供统一的回调框架，支持事件注册与触发。
//
// 本包是所有领域（LLM、Tool、Agent、Workflow 等）共享的回调基础设施，
// 与 Python 中 openjiuwen/core/runner/callback/ 对应。
//
// 2.14 节仅实现 LLM 相关事件的回调框架最小子集。
// 后续领域三（Tool）、领域六（Agent）等扩展时，在同一框架中新增事件类型。
// 完整能力（过滤器/熔断器/链式执行/装饰器/transform_io）在 6.24 节实现。
//
// 事件体系：
//
//	LLMCallEventType  — LLM 调用生命周期事件（9 种）
//	ToolCallEventType — Tool 调用生命周期事件（11 种）
//
// 文件目录：
//
//	callback/
//	├── doc.go                # 包文档
//	├── events.go             # 事件类型定义（LLM + Tool）
//	├── framework.go          # CallbackFramework 核心
//	├── logging.go            # 默认日志回调
//
// 对应 Python 代码：openjiuwen/core/runner/callback/
package callback
