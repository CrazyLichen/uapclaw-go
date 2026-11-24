// Package configpanel 配置面板写入层与 zen 免费模型缓存。
//
// 对齐上游「写/校验 RPC 下沉共享层」的形状：Gateway 只转发，写入、模型校验、
// 变更记账、agent 重载通知全部在本包（Runtime 侧）执行；真相源是磁盘
// config.yaml 与 .env。zen 缓存条目仅驻内存、不落盘。
//
// 文件目录：
//
//	configpanel/
//	├── doc.go                  # 包文档
//	├── adapter.go              # ConfigAdapter 方法分派表（对齐 config_adapter.py）
//	├── config_set_handlers.go  # 写链：ChangeSet/env+yaml 写盘/重载通知
//	├── models_handlers.go      # 模型读写：validate/list+zen 追加/replace_all
//	└── opencode_zen.go        # Zen 免费模型内存缓存
//
// 对应 Python 代码：jiuwenswarm/common/config_panel/{config_set_handlers.py,
// models_handlers.py} · jiuwenswarm/server/runtime/{gateway_adapter/config_adapter.py,
// opencode_zen.py}
package configpanel
