// Package e2a 提供 E2A（Everything-to-Agent）统一信封协议的数据模型和编解码。
//
// E2A 协议是 Gateway 与 AgentServer 之间的核心通信协议，将 ACP、A2A 等多种外部协议消息
// 统一转换为 E2AEnvelope 请求信封和 E2AResponse 响应模型，实现协议无关的内部通信。
//
// 本包定义了 E2A 协议的完整数据模型和编解码，包括：
//   - 协议常量（来源协议、响应状态、响应类型、ACP 方法名、Wire 键名等）
//   - 子结构体（IdentityOrigin 枚举、E2AProvenance 出处追踪、E2AFileRef 文件引用、E2AAuth 身份鉴权）
//   - 请求信封（E2AEnvelope）及其序列化/反序列化，含 5 种 Legacy 兼容逻辑
//   - 响应模型（E2AResponse）及其序列化/反序列化
//   - ACP 参数补全（MergeParamsToACPPrompt）
//   - ACP/A2A 协议适配器（入站信封构造 + 出站响应投影 + 工具响应回流 Message，10.2.9+10.2.10）
//   - Gateway 规范化（Message→E2AEnvelope、AgentResponse/Chunk↔E2AResponse 格式互转、兜底信封）
//   - Wire Codec（E2A 线编解码 + legacy fallback）
//   - Agent 兼容层（E2AEnvelope→AgentRequest 转换）
//
// 文件目录：
//
//	e2a/
//	├── doc.go                 # 包文档
//	├── adapters.go            # E2A ↔ ACP/A2A 外协议形状互转（10.2.9+10.2.10）
//	├── constants.go           # 协议常量（来源协议、响应状态、响应类型、ACP 方法名、Wire 键名）
//	├── provenance.go          # IdentityOrigin 枚举 + E2AProvenance/E2AFileRef/E2AAuth 子结构体
//	├── envelope.go            # E2AEnvelope 请求信封 + 序列化/反序列化 + Legacy 兼容 + MergeParamsToACPPrompt
//	├── response.go            # E2AResponse 响应模型 + 序列化/反序列化
//	├── gateway_normalize.go   # Message/E2A/AgentResponse 格式互转（请求方向 + 响应方向）
//	├── wire_codec.go          # E2A ↔ AgentResponse/AgentResponseChunk 线编解码 + fallback 兜底
//	└── agent_compat.go        # E2AEnvelope → AgentRequest 转换
//
// 对应 Python 代码：jiuwenswarm/common/e2a/
package e2a
