// Package codebuddy 验证 codebuddy provider 的真子进程 stdio 双工链路。
//
// Python 无对应物（偏差 D-15.22-a）：Python harness_providers 的进程级契约
// 走真 LLM 系统测试（tests/system_tests/harness_providers/_contract.py），
// 本目录以 fakecodebuddy 假 CLI 进程承担等价的进程边界验证（spec §4）。
//
// 文件目录：
//
//	codebuddy/
//	├── doc.go                         # 包文档
//	├── fakecodebuddy/main.go          # 假 CLI helper（script/crash/auth-fail/hang）
//	└── provider_roundtrip_suite_test.go  # 真子进程 stdio 双工整轮
package codebuddy
