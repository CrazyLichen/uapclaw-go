// Package sdk 提供扩展 SDK 基类，对齐 Python jiuwenswarm/extensions/sdk/。
//
// 本包定义了扩展开发的抽象接口：BaseExtension（基础扩展）、
// AgentServerClientExtension（AgentServer 客户端扩展）、CryptoUtility（加解密扩展）。
//
// 文件目录：
//
//	sdk/
//	├── doc.go                # 子包文档
//	├── base.go               # BaseExtension 接口 + BaseExtensionImpl 默认实现（10.5.4）
//	├── agent_server_client.go # AgentServerClientExtension 接口（10.5.5）
//	└── crypto_utility.go     # CryptoUtility stub（10.5.10 ⤵️）
//
// 对应 Python 代码：jiuwenswarm/extensions/sdk/
package sdk
