// Package object 提供对象存储的抽象接口定义和配置。
//
// 本包定义了所有对象存储后端必须满足的 BaseObjectStorage 接口，
// 以及通用的 ObjectStorageConfig 配置和 ListOption 列表查询选项。
// 具体后端实现由 s3 子包提供。
//
// 文件目录：
//
//	object/
//	├── doc.go     # 包文档
//	└── base.go    # BaseObjectStorage 接口 + ObjectStorageConfig + ListOption
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/object/
//
// 核心类型/接口索引：
//
//	BaseObjectStorage  — 对象存储客户端接口，定义上传/下载/删除/桶操作/列表查询
//	ObjectStorageConfig — 对象存储配置，支持结构体字段优先 + 环境变量兜底
//	ListOption         — 列表查询选项，WithMaxObjects 设置最大返回数（默认 100）
package object
