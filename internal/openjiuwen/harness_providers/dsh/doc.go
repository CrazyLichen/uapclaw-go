// Package dsh 提供协议 v1 的 DeepSeek Harness 适配器（外部 provider）。
//
// 每个宿主 Turn 是一段串行化的 DSH 活动区间（输入接受到 whole-agent idle）；
// DSH 自身的 turn/step 记录仅为内部观测，不重定义公开 Turn 边界。
// SDK 边界通过非导出接口缝注入（Python 的 importlib 缺依赖分支在 Go 侧
// 不可达，见 spec 偏差 D-dsh-2）；真实运行时由 theoneLee/deepseek-harness-sdk-go
// 以 stdio JSON-RPC 2.0 驱动 dsh 子进程承担。
//
// 文件目录：
//
//	dsh/
//	├── doc.go           # 包文档
//	├── config.go       # DshHarnessConfig 构造与校验（含 skills 接线）
//	├── harness.go       # DshHarness：providerHooks 实现 + SDK 缝
//	├── mapping.go       # DshTurnAccumulator：SDK 通知 → 协议事件
//	├── composition.go   # mcpConfigs + writeOverlay（Cordis patch）
//	└── provider.go     # DshHarnessProvider 工厂 + ModelConfig
//
// 对应 Python 代码：openjiuwen/harness_providers/dsh/
package dsh
