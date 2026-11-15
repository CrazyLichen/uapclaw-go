// Package config 提供单 harness 自我改进的配置模型与加载入口。
//
// 顶层 AutoCoordinatingHarnessConfig 聚合 evaluator / analysis / member_optimizer /
// scheduling / promotion 五段配置，并完成跨段推导（模型引用三级回退、迭代上限继承、
// promotion 连续模式与 score_mode 联动）。加载入口负责默认模板引导与 YAML 解析。
//
// 文件目录：
//
//	config/
//	├── doc.go     # 包文档
//	├── config.go  # 配置 dataclass 等价物 + from_dict + 校验链 + 类型归一助手
//	└── loader.go  # LoadAutoCoordinatingHarnessConfig 加载入口（缺省模板引导）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/config/{config,loader}.py
package config
