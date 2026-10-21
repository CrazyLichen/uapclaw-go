// Package sdk 提供扩展 SDK 基类，对齐 Python jiuwenswarm/extensions/sdk/。
//
// 本包定义扩展开发的抽象接口：BaseExtension（基础扩展）、
// AgentServerClientExtension（AgentServer 客户端扩展）。
//
// 注意：宿主从不调用 Initialize（对齐 Python loader/manager 事实——仅
// register_extensions → set_extension_dir → shutdown 三步）。
//
// CryptoUtility 说明（10.5.10 ✅）：Go 无 Python importlib 同进程加载场景，
// sdk 不保留 CryptoUtility 接口（原 stub 已删除）；插件作者 crypto 入口为
// plugin 包 CryptoImpl，宿主侧注册接口为 extensions.CryptoUtilityExtension
// （GetCrypto 返回 common/crypto.CryptoProvider）。
//
// 文件目录：
//
//	sdk/
//	├── doc.go                 # 子包文档
//	├── base.go                # BaseExtension 接口 + BaseExtensionImpl 默认实现（10.5.4）
//	└── agent_server_client.go # AgentServerClientExtension 接口（10.5.5）
//
// 对应 Python 代码：jiuwenswarm/extensions/sdk/
package sdk
