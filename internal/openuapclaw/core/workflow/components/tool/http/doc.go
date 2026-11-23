// Package http 提供工作流 HTTP 请求节点组件（领域八 8.22）。
//
// 一比一对齐 Python openjiuwen/core/workflow/components/tool/http/
// http_request_component.py：HTTPRequestComponent 组合面 + HTTPRequestExecutable
// 执行体（invoke 三步链 processInputs→makeRequest→processResponse）。
// 支持 {{key}} 占位符模板、六类认证枚举（实际实现 basic/bearer/api_key）、
// 五类请求体、响应格式化、三退避重试、响应大小限制、SSL/代理安全工具。
// 已知偏差见 spec docs/superpowers/specs/2026-11-23-http-request-comp-8.22-design.md §5。
//
// 文件目录：
//
//	http/
//	├── doc.go                       # 包文档
//	└── http_request_component.go   # 枚举/配置/执行体/组件
//
// 对应 Python 代码：openjiuwen/core/workflow/components/tool/http/
package http
