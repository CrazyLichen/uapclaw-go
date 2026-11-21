// 本文件是 provider 子包的生产接线：blank import 触发各子包 init() 的
// RegisterBuiltinProvider 编译期自注册。没有本文件，生产二进制的
// ResolveProvider 注册表恒空（子包只有测试 import），路 A（manifest 单专家
// harness）与路 B（cli_agent 成员后端）在运行时全部解析失败。
//
// 先例：core/foundation/llm/model_clients_register.go（同款 blank import
// 注册模式）。放置于本包的理由：ResolveProvider 的全部生产消费点
// （manifest.go/catalog.go/spawn.go/backends.go）都在本包；子包不 import
// 本包，无环。
package external

import (
	_ "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/codebuddy"
	_ "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/dsh"
	_ "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/native"
)
