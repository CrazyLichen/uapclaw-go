// Package requestext 请求级扩展字段透传（Web 握手 query / header → Message.metadata["ext"]）。
//
// 将一个 map 从 Channel 入口写入 Message.metadata["ext"]，由 AgentServer 入口
// 抬升到请求级存储，供 Agent rail 通过 GetExt 读取。
//
// 要透传哪些字段由环境变量声明（逗号分隔），两套名字等价，先读 Swarm 名：
//   - JIUWENSWARM_REQUEST_EXT_FORWARD_HEADERS
//   - JIUWENCLAW_REQUEST_EXT_FORWARD_HEADERS（企业 kub 兼容）
//
// 已知偏差：Python 用 contextvars.ContextVar 携带请求级 ext（单线程协程语义）；
// Go 无 contextvars token，改用 context.Context 值载体（WithRequestExt /
// RequestExtFromContext），登记于 spec D-B2-10；回收条件=Go 侧出现对齐
// contextvars 语义的统一载体。
//
// 文件目录：
//
//	requestext/
//	├── doc.go               # 包文档
//	└── requestext.go       # 白名单/构建/编码/解码/载体
//
// 对应 Python 代码：gateway/common/request_ext.py
package requestext
