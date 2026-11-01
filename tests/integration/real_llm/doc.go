//go:build integration && llm

// Package real_llm 提供真实 LLM API 的集成测试。
//
// 与 mock 版集成测试不同，本包的测试使用真实的 LLM API 调用，
// 验证系统在真实模型推理下的行为，对齐 Python system_tests 中
// @pytest.mark.skipif(not API_KEY) 的测试模式。
//
// 环境变量：
//   - LLM_API_KEY: API 密钥（CI 中由 .github/workflows/real-llm-ci.yml 注入
//     GitHub secret LLM_API_KEY；prompt-capture 套件缺失时直接 Fatal，
//     对齐"默认 CI 就要运行，不做环境变量控制"的 fail-not-skip 决策；
//     其余套件缺失时 Skip）
//   - LLM_API_BASE: API 基础 URL（默认 https://ark.cn-beijing.volces.com/api/coding/v3）
//   - LLM_MODEL_NAME: 模型名称（默认 minimax-m3；CI 配置 deepseek-v4.1-flash）
//   - LLM_MODEL_PROVIDER: 服务商标识（默认 OpenAI）
//   - EMBED_API_KEY: embedding 密钥（缺省回落 LLM_API_KEY）
//   - EMBED_API_BASE: embedding 完整端点 URL（默认
//     https://ark.cn-beijing.volces.com/api/coding/v3/embeddings——ark coding
//     套餐 key 的 embedding 走 coding 端点，标准 /api/v3 会 404）
//   - EMBED_MODEL_NAME: embedding 模型名（默认 doubao-embedding-vision，
//     全小写——ark 模型名大小写敏感）
//   - RUN_REAL_LLM_TESTS: 真跑开关（skill/subagent/todo/ltm/code_agent 套件守卫，
//     对齐 Python os.getenv("RUN_REAL_LLM_TESTS") 检查，未设为 1 时 Skip）
//
// CI 运行：每日北京时间 02:00 由 real-llm-ci.yml 定时触发（cron "0 18 * * *" UTC）
// + workflow_dispatch 手动触发，运行本包全部 14 套件（含提示词捕获测试），
// 并设置 RUN_REAL_LLM_TESTS=1 开启守卫套件。
//
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
//
// 已修复缺陷（2026-11-01 定位，2026-11-02 修复）
//
// 流式工具调用聚合失败：真实模型的 tool_call 增量帧曾被逐个当成完整调用，
// 例（修复前的帧样本）：
//
//	{"arguments":"/","name":"","tool_call_id":""}
//	{"arguments":"Test","name":"","tool_call_id":""}
//	{"arguments":{"...":...},"name":"list_members","tool_call_id":"call_00_..."}
//
// 根因：真实 SSE 只在首个 delta 携带 tool_calls[].type，后续帧缺省（omitempty）。
// 三个客户端的流式解析把空串透传给 WithToolCallType，覆盖了 NewToolCall 的
// "function" 默认值，使 message_chunk.go 的 mergeToolCalls 同调用判定
// （要求两侧 type == "function"）失效——同一次调用的参数碎片各自成为独立
// ToolCall，被 ReAct 逐个当工具执行。表现为 leader 不建队、send_message 缺失、
// 首轮迟迟没有 chat.final；MockLLM 用 NewToolCall 构造（默认 function），
// 因此 Mock 全绿、真 LLM 全崩。
//
// 修复：schema.ToolCallDeltaOptions（type 为空时保留 "function" 默认值，
// 对齐 Python openai_model_client.py:745-751 的硬编码），openai / siliconflow /
// inference_affinity 三处流式解析改用它；deep_adapter_team 的 tool_call 分支补齐
// 与 stream_utils 一致的碎片帧守卫。回归用例见 model_clients 各包的
// TestParseStreamChunk_缺省type的增量帧可聚合。
//
// 文件目录：
//
//	real_llm/
//	├── doc.go                        # 包文档
//	├── llm_invoke_test.go            # LLM Invoke/Stream 基础连通测试
//	├── ask_user_rail_test.go         # AskUserRail + 真实 LLM
//	├── task_planning_rail_test.go    # TaskPlanningRail + 真实 LLM
//	├── deep_agent_e2e_test.go        # DeepAgent e2e 真实 LLM（问答/多轮/流式/外循环）
//	├── interrupt_stream_test.go      # ConfirmInterruptRail 中断恢复（真实写文件）
//	├── skill_real_system_test.go     # 技能注册 + 原生 sys_operation 工具链
//	├── memory_rail_test.go           # MemoryRail 9 测试（真实 embedding）
//	├── subagent_real_test.go         # 子代理委派（同步 task_tool + 异步 spawn）
//	├── code_agent_real_test.go       # CodeAgent plan 模式两轮流（plan 文件 + normal 执行）
//	├── todo_real_test.go             # Todo 工具创建/列表/修改链路
//	├── ltm_real_test.go              # LongTermMemory E2E（Chroma+SQLite+真实 LLM）
//	├── system_prompt_capture_test.go # 系统提示词实时捕获（双 skill_mode 分支，
//	    27 断言/分支；fail-not-skip，对齐 Python test_system_prompt_live_capture.py）
//	├── dreaming_real_test.go         # Dreaming Sweeper 真实模型 sweep（提示词消费 + 围栏解析）
//	└── team_continuous_dialog_real_test.go # team 模式连续对话（进程内 serve + 真实 LLM）
//
// 对应 Python 代码：tests/system_tests/harness/（test_deep_agent_ask_user.py、
// test_deepagent_interrupt_stream.py、test_deep_agent_e2e.py、
// test_deep_agent_subagent.py）、tests/system_tests/agent/skill/（test_skill_real_system.py）、
// tests/system_tests/code_agent/（test_code_agent_execute_mode_e2e.py）、
// tests/system_tests/harness/tools/test_todo_tool.py、
// tests/system_tests/memory/（test_memory_rail_e2e.py、test_long_term_memory.py）、
// tests/system_tests/test_system_prompt_live_capture.py、
// tests/system_tests/test_team_continuous_dialog.py
package real_llm
