// Package externalcli_test 外部 CLI 成员生命周期集成测试。
//
// 覆盖 15.13 spec §4 的四个集成用例：adapter 路全生命周期（假 CLI 探针 +
// tracker 折叠）、dsh provider 路（SDKFactory 缝注入 fake SDK + 模型端点
// 映射与 fallback 丢弃）、session_cleanup 清理链、schema 校验器红绿与
// BuildContextSeed 往返。子进程边界用 t.TempDir() 假脚本、SDK 边界用
// fake 工厂承担，绝不触达真实 CLI / 网络。
//
// 文件目录：
//
//	externalcli/
//	├── doc.go                    # 包文档
//	├── external_lifecycle_test.go # ExternalCliLifecycleSuite：adapter 全生命周期
//	├── dsh_provider_test.go      # DshProviderSuite：provider 路映射与 fake SDK
//	├── session_cleanup_test.go   # SessionCleanupSuite：清理链编排
//	└── config_roundtrip_test.go  # ConfigRoundtripSuite：校验器红绿 + seed 往返
//
// 对应 Python 代码：openjiuwen/agent_teams/external/（runtime/member_runtime/
// session_cleanup）+ spawn/external_cli_spawn.py
package externalcli
