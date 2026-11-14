// Package native in-process DeepAgent 的 harness protocol 实现。
//
// 把 DeepAgent 的会话内交互循环适配到协议 v1：一次外部 Turn 对应一次
// attach_output 流消费，输入经 send_input 派发，DeepAgent 自己的任务循环
// 延续保持在 Turn 内部；中断保持 Turn 打开，经宿主交互解决后恢复
// （request_id = 中断 id）。
//
// 文件目录：
//
//	native/
//	├── doc.go        # 包文档
//	├── harness.go    # DeepAgentHarness 适配器 + observationRail + turnState + 助手
//	└── provider.go  # NativeHarnessProvider 工厂（可序列化配置 → 未启动 harness）
//
// 对应 Python 代码：openjiuwen/harness_providers/native/{__init__,harness,provider}.py
package native
