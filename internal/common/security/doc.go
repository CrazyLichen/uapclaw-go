// Package security 提供安全相关的工具函数，包括 SSL/TLS 配置、证书安全加载
// 与全局代理 URL 解析。本包对齐 Python openjiuwen/core/common/security/ 的能力，
// 为工具认证（ToolAuth）、LLM 请求代理和其他需要安全配置的组件提供基础设施。
// 文件目录：
//
//	security/
//	├── doc.go               # 包文档
//	├── ssl_utils.go         # SSL/TLS 配置工具函数
//	├── ssl_utils_unix.go    # Unix 平台安全文件打开（O_NOFOLLOW）
//	├── ssl_utils_windows.go # Windows 平台安全文件打开
//	└── url_utils.go         # 全局代理 URL 与 NO_PROXY 匹配
//
// 对应 Python 代码：openjiuwen/core/common/security/ssl_utils.py + url_utils.py
package security
