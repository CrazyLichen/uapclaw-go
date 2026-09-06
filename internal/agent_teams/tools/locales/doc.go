// Package locales 提供团队工具的多语言支持。
//
// 基于 Translator 闭包 + Markdown 描述文件 + STRINGS 映射表三层体系：
//   - Translator 闭包：绑定到特定语言，提供 tool 级别的 i18n 查询
//   - Markdown 文件：长文本工具描述（_desc），嵌入二进制
//   - STRINGS 映射表：短文本参数描述，定义在 schema/i18n.go
//
// 文件目录：
//
//	locales/
//	├── doc.go              # 包文档
//	├── translator.go       # Translator 类型 + MakeTranslator + loadToolDesc
//	└── descs/
//	    ├── cn/
//	    │   └── workspace_meta.md  # workspace_meta 工具中文描述
//	    └── en/
//	        └── workspace_meta.md  # workspace_meta 工具英文描述
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/locales/
package locales
