// Package sessioninput 领域二十五会话补充输入（steer）接线链路级集成测试。
//
// 跨子系统：runtime/session 协调器 + lifecycle 守卫 + server/adapter 投递链
// 分类器 + harness schema 输入条目（满足审计 ≥3 子系统）。
// 对应 spec：docs/superpowers/specs/2026-11-26-domain25.6-steer-wiring-design.md §8.2。
//
// 文件目录：
//
//	sessioninput/
//	└── steer_wiring_test.go   # 七用例套件（steer 插话/锚定失配/idle 兜底/挂起拒收/guard 边界/超时 FAILED/代次防误判）
package sessioninput
