// Package workspace_test 覆盖工作区初始化链路（jiuwenswarm-init）的集成测试。
//
// 对齐 Python jiuwenswarm/tests/system_tests/test_init_workspace.py：
// 语言解析、交互式语言选择、首次初始化目录结构、force 重建、取消语义，
// 以及「初始化产物能让服务真正跑起来」的端到端链路。
//
// 交互分支用 testhelpers.OpenPTY 造真终端——Go 无 monkeypatch，
// os.Pipe 造出的 stdin 被判为 ModeNamedPipe，进不到交互分支。
//
// 文件目录：
//
//	workspace/
//	├── doc.go           # 包文档
//	└── init_flow_test.go # InitFlowSuite：语言解析 / 结构 / PTY 交互 / force / 链路
//
// 对应 Python 代码：jiuwenswarm/tests/system_tests/test_init_workspace.py
package workspace_test
