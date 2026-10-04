//go:build llm

// Package real_llm 提供真实 LLM API 的集成测试。
//
// 与 mock 版集成测试不同，本包的测试使用真实的 LLM API 调用，
// 验证系统在真实模型推理下的行为，对齐 Python system_tests 中
// @pytest.mark.skipif(not API_KEY) 的测试模式。
//
// 环境变量：
//   - LLM_API_KEY: API 密钥（必填，为空则 Skip）
//   - LLM_API_BASE: API 基础 URL
//   - LLM_MODEL_NAME: 模型名称
//   - LLM_MODEL_PROVIDER: 服务商标识
//
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
//
// 文件目录：
//
//	real_llm/
//	├── doc.go                  # 包文档
//	├── llm_invoke_test.go      # LLM Invoke/Stream 基础连通测试
//	├── ask_user_rail_test.go   # AskUserRail + 真实 LLM
//	├── task_planning_rail_test.go # TaskPlanningRail + 真实 LLM
//	└── deep_agent_e2e_test.go  # DeepAgent e2e 真实 LLM
//
// 对应 Python 代码：tests/system_tests/harness/rail/test_deep_agent_ask_user.py 等
package real_llm