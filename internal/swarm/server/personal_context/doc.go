// Package personalcontext 提供主动上下文（PersonalContext）的宿主半区：进程内 Host API、
// WS 请求分发与错误本地化，对齐 Python jiuwenswarm/server/personal_context/ 一比一。
//
// Host 持有配置文件（<workspace>/.personal_context/personal_context.yaml）与 Core 生命周期，
// 将 Context 查询委托给 Core（Python openjiuwen/harness/personal_context/，Go 侧 24.3a/b/c 回填）。
// 本批 Core 缺席，Host 走空态运行（spec：docs/superpowers/specs/2026-12-25-24.3-personal-context-host-design.md）。
//
// 文件目录：
//
//	personal_context/
//	├── doc.go             # 包文档
//	├── host_api.go        # PersonalContextHostAPI 主体（对应 host_api.py 一比一）
//	├── ws_handler.go      # 26 method 分发与流式四事件（对应 ws_handler.py 一比一）
//	└── error_messages.go  # 错误本地化与脱敏日志（对应 error_messages.py 一比一）
//
// 对应 Python 代码：jiuwenswarm/server/personal_context/；Core：openjiuwen/harness/personal_context/
package personalcontext
