// Package llm 提供统一 LLM 调用入口，封装模型客户端的创建、注册与调用。
//
// 本包采用门面模式（Facade），对外暴露 Model 统一调用接口和 InitModel 工厂函数，
// 内部通过 model_clients 子包适配不同 LLM 供应商。model_clients_register.go 集中
// 管理 blank import 以触发各子包 init() 注册。
//
// 文件目录：
//
//	llm/
//	├── doc.go                    # 包文档
//	├── init_model.go             # InitModel 工厂函数及选项
//	├── model.go                  # Model 统一 LLM 调用入口（门面）
//	└── model_clients_register.go # 集中 blank import 注册所有内置 provider
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/
package llm
