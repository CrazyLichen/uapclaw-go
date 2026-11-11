// Package endpointprofiles 提供 OpenAI 兼容端点档案与 provider 别名归一。
//
// 上游只有 4 个真正的客户端类（OpenAI / OpenAIAccount / Anthropic /
// intelli_router），其余 provider 经 LEGACY_PROVIDER_ALIASES 归一为
// client_provider="OpenAI"，靠 endpoint_profile 区分方言。归一前原名保存在
// Extra["legacy_client_provider"]，供 reasoning 层分辨厂商。
//
// 文件目录：
//
//	endpointprofiles/
//	├── doc.go       # 包文档
//	└── profiles.go  # 17 个 EndpointProfile + 14 条别名 + NormalizeModelClientConfig
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/utils/endpoint_profiles.py
package endpointprofiles
