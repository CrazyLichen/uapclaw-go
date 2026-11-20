// Package securitytest 是 security 包的外部集成测试包（仅含 _test.go）。
//
// 放在独立包而非 security 包内，是因为 A/B/C 三段管线测试需要 import
// fileguard / netguard 触发它们的 init（注册判定器工厂），而 security 包
// 自身 import 子包会形成循环。Go 允许「包外测试」import 被测包与子包。
//
// 对应 Python 代码：security 包管线的集成级测试
package securitytest
