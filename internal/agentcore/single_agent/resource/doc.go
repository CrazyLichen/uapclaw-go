// Package resource 提供能力实例获取的资源管理器接口。
//
// ResourceManager 定义 Tool/Workflow/Agent 实例获取接口，
// 由领域六/九提供具体实现，3.13 阶段使用 NoopResourceManager。
//
// 文件目录：
//
//	resource/
//	├── doc.go               # 包文档
//	└── resource_manager.go  # ResourceManager 接口 + NoopResourceManager + ResourceOptions
//
// 对应 Python 代码：openjiuwen/core/single_agent/ability_manager.py (ResourceManager 部分)
package resource
