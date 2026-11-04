//go:build integration

// Package attachment_mode 提供 mode 解析与 plan 模式行为的集成测试。
//
// 覆盖链路：网关请求信封 → adapter 的 mode 归一与 plan 状态机 → rail 生命周期 →
// 下发给前端的事件流，验证「显式进入 plan / 退出 plan / 跨会话排队」等场景下
// 后端状态与事件是否一致。
//
// 文件目录：
//
//	attachment_mode/
//	├── doc.go                      # 包文档
//	├── mode_string_test.go         # mode 字符串新旧写法归一
//	├── plan_state_test.go          # plan 状态进入、退出与残留阻断
//	├── plan_event_test.go          # plan 进入/退出事件下发（unary + stream）
//	├── rail_lifecycle_test.go      # rail 幂等、附件化指引与静态节保留
//	├── envelope_integrity_test.go  # 请求信封字段完整性
//	├── code_todo_reminder_test.go  # code 模式 todo 提醒节奏
//	└── dbg2_test.go                # rail 装配的调试用例
//
// 对应 Python 代码：jiuwenswarm/server/adapter/（deep_adapter 的 plan 模式部分）
package attachment_mode
