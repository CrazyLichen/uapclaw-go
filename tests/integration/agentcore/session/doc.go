// Package session 提供 Session 生命周期集成测试。
//
// 覆盖 Session 创建、状态读写、PreRun/PostRun 幂等、
// InMemoryCheckpointer 持久化、环境变量传播、AgentTeamSession 生命周期、
// SessionController 控制器管理等场景。
// 对齐 Python: tests/cli/e2e/test_session_persist.py
//
// 文件目录：
//
//	session/
//	├── doc.go                            # 包文档
//	├── session_test.go                   # Session 生命周期测试
//	├── session_reuse_test.go             # Session 复用集成测试
//	├── session_controller_e2e_test.go    # SessionController + ChainSession + 全局控制器 E2E 测试（32 测试）
//	└── agent_team_session_test.go        # AgentTeamSession 生命周期测试
package session
