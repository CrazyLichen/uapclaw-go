// Package cn 提供中文语言的实体提取多语言描述和格式化模板注册。
//
// 通过 init() 将中文描述、关系格式、日期时间模板等注册到 registry 包的全局映射表中，
// 供 entity_extraction 提示词构建器按语言选择模板。
//
// 文件目录：
//
//	cn/
//	├── doc.go           # 包文档
//	└── register.go      # 中文语言注册（init）
//
// 对应 Python 代码：openjiuwen/core/memory/graph/extraction/entity_extraction/cn.py
package cn
