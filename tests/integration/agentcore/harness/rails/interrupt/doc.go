//go:build integration

// Package interrupt 提供中断（HITL）相关 rail 的集成测试。
//
// 覆盖 AskUserRail 的问询中断恢复、ConfirmRail 的工具确认拦截、三层中断链路
// 以及并发多工具混合确认等场景，验证「中断 → 用户输入 → 恢复执行」的完整链路。
//
// 文件目录：
//
//	interrupt/
//	├── doc.go                             # 包文档
//	├── ask_user_test.go                   # AskUserRail 问询中断恢复
//	├── confirm_rail_test.go               # ConfirmRail 工具确认拦截
//	├── hitl_chain_test.go                 # 三层中断链路
//	├── three_layer_interrupt_test.go      # 三层中断细粒度矩阵
//	├── react_agent_interrupt_test.go      # ReactAgent 中断恢复
//	├── react_interrupt_stream_test.go     # ReactAgent 流式中断
//	├── react_interrupt_exception_test.go  # ReactAgent 中断异常路径
//	├── react_auto_confirm_test.go         # ReactAgent 自动确认
//	├── fine_grained_auto_confirm_test.go  # 细粒度自动确认开关
//	├── concurrent_mixed_interrupt_test.go # 并发多工具混合确认
//	├── task_completion_test.go            # 任务完成事件
//	└── doc_test.go                        # 包引用占位套件
//
// 对应 Python 代码：openjiuwen/harness/rails/interrupt/ 与
// tests/unit_tests/harness/rails/test_deep_agent_ask_user.py
package interrupt
