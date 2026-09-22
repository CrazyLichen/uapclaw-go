// Package prompts 提供 Graph Extraction 提示词模板管理功能。
//
// 包含 .pr.md 格式解析器、线程安全的模板管理器单例和格式化辅助函数。
// .pr.md 文件使用 `#role#` 分隔消息角色（system/user/assistant/tool），
// 解析后生成 []schema.BaseMessage 消息列表，用于 LLM 调用。
//
// 文件目录：
//
//	prompts/
//	├── doc.go                # 包文档
//	├── manager.go            # TemplateManager 单例（线程安全，glob 扫描 .pr.md 加载）
//	├── pr_parser.go          # .pr.md 解析器（`#role#` → []BaseMessage）
//	├── format_helpers.go     # 格式化辅助函数
//	├── cn/                   # 中文提示词模板（11 个 .pr.md）
//	│   ├── register.go       # 中文多语言注册
//	│   └── *.pr.md           # 提示词模板文件
//	├── en/                   # 英文提示词模板（11 个 .pr.md）
//	│   ├── register.go       # 英文多语言注册
//	│   └── *.pr.md           # 提示词模板文件
//	└── entity_extraction/    # 实体抽取格式化辅助
//	    ├── doc.go            # 包文档
//	    └── format.go         # 格式化辅助函数
//
// 对应 Python 代码：openjiuwen/core/memory/graph/extraction/prompts/
package prompts
