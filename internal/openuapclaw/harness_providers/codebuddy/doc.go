// Package codebuddy 把 CodeBuddy Code CLI 适配到 harness_protocol v1。
//
// ⚠️ Python 上游无对应物（闭集 PROVIDER_NAMES = native/native_v2/claudecode/
// codex/dsh，factory.py:62），本包为 Go 侧新增第三方 provider（偏差 D-15.22-a）。
// 注册走 RegisterHarnessProvider 第三方进程内注册缝（factory.go:137），
// 不改闭集（偏差 D-15.22-b）。双工 input 帧协议按 CLI 2.181.0 实测取得
// （偏差 D-15.22-c），帧样例记录于 transport.go。失败分类仅最小四类，
// 完整 failure_classifier 归 15.7/15.8（偏差 D-15.22-d）。
//
// 文件目录：
//
//	codebuddy/
//	├── doc.go        # 包文档
//	├── config.go     # CodebuddyConfig 与 FromMapping 校验
//	├── transport.go   # Transport 缝 + 默认子进程实现 + 线帧类型
//	├── mapping.go     # CLI 帧 → harness_protocol 事件映射
//	├── harness.go     # CodebuddyHarness（providerHooks 实现）
//	└── provider.go   # CodebuddyProvider + init 注册
//
// 对应 Python 代码：无（Go 侧新增）；形态参考 harness_providers/dsh/。
package codebuddy
