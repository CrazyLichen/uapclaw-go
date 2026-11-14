// Package common 提供 swarm 网关层的模型目录、模型选择 resolver、reasoning 请求参数注入与厂商预设表。
//
// 本包是 23.4d 的落位（总设计 spec docs/superpowers/specs/2026-11-12-domain23.4-model-group-routing-design.md
// §8，细化 spec docs/superpowers/specs/2026-11-28-domain23.4d-gateway-model-catalog-design.md）。
// 23.4a-c 已交付路由引擎（intellirouter 客户端、routing 编译器、模型池/分配器）；
// 本包把用户在配置面板选的"模型/模型组"稳定引用翻译成引擎输入 ResolvedModelGroup：
// 删除回落（Restore）、五级 Choose 回退、组级/路由级 enabled 与权限闸。
//
// 上游双副本事实：Python 侧 jiuwenswarm/common/ 与 gateway/common/ 各有一份 model_*.py，
// gateway 侧文件头自述"仅保留 gateway 实际引用的符号"的裁剪副本。Go 侧单 swarm 层同时
// 承担 gateway 与 runtime 两个角色，按裁决 D 以 jiuwenswarm/common 完整版为移植基线，
// gateway/common/ 独有的 model_selection.py / model_vendor_registry.py / reasoning_*.py
// 直接以 gateway 侧为源。
//
// 文件目录：
//
//	common/
//	├── doc.go                        # 包文档
//	├── helpers.go                   # 本包共用小工具（asString/copyMap/firstNonEmpty）
//	├── model_selection.go           # ModelSelection / ResolvedModel / ResolvedRoute / ResolvedModelGroup DTO
//	├── model_errors.go               # ModelSelectionError 与五个错误码
//	├── reasoning_config.go           # 思考档位归一 / 档位映射 / 采样覆盖（裁决 A）
//	├── reasoning_injector.go         # 请求参数注入与清理（裁决 A）
//	├── model_catalog.go              # 只读模型目录与引用扫描（cron 扫描 ⤵️ 裁决 E）
//	├── model_selection_store.go      # session 模型选择持久化
//	├── model_config_validation.go    # 模型配置校验与连通性探测（完整版基线，裁决 D）
//	├── model_vendor_registry.go      # 厂商预设注册表（裁决 B）
//	├── model_routing_registry.go     # ModelSelectionResolver 五级回退与 Resolve
//	├── model_resource_ops.go         # 模型资源 upsert/delete（config.py:2791-2825）
//	└── model_migration.go           # 旧配置迁移
//
// 对应 Python 代码：jiuwenswarm/common/{model_errors,model_catalog,model_routing_registry,
// model_config_validation,model_selection_store,model_migration}.py（完整版基线）·
// gateway/common/{model_selection,model_vendor_registry,reasoning_config,reasoning_injector}.py ·
// gateway/common/config.py:2791-2825（模型资源三函数）
package common
