// Package entity_extraction 提供实体抽取的格式化辅助函数。
//
// 包含格式化实体/关系列表、Schema 信息拼接、数据源描述格式化、
// 语言校验等函数，对齐 Python prompts/entity_extraction/base.py。
//
// 文件目录：
//
//	entity_extraction/
//	├── doc.go       # 包文档
//	├── base.go      # 格式化辅助函数（FormatSchemaInfo/FormatSourceDescription/...）
//	└── format.go    # FormatNewEntities 格式化辅助
//
// 对应 Python 代码：openjiuwen/core/memory/graph/extraction/prompts/entity_extraction/base.py
package entity_extraction
