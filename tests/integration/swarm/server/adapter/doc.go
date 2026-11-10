//go:build integration

// Package adapter 提供 swarm 装配层（CodeAdapter）集成测试的包骨架。
//
// 实际测试代码在外部测试包 adapter_test，验证 CodeAdapter 把 agentcore 组件
// 装配到一起后的端到端行为：CreateInstance（配置加载→rails 装配→DeepAgent 创建）
// → ProcessMessageImpl（回调触发→工具执行→落盘验证）。
//
// 两个套件的来源：
//   - coding_memory：对齐 Python tests/unit_tests/agentserver/test_code_adapter_acp_chat_tool.py
//   - project_memory：对齐 Python tests/system_tests/test_project_memory.py
//     （类 3 装配 / 类 4 explore 开关 / 类 6 模式切换真实化）
//
// session_usage_isolation 套件锁定跨会话边界：usage_summary 的 token 数取自
// llm_usage 的嵌套 usage_metadata（对齐 Python interface_deep）且每轮独立，
// 新会话的 prompt 不得夹带上个会话的历史，同会话第二轮必须仍带首轮历史。
//
// 文件目录：
//
//	adapter/
//	├── doc.go                                  # 包文档（本文件）
//	├── code_adapter_coding_memory_test.go      # CodingMemoryRail 装配级集成测试
//	├── code_adapter_project_memory_test.go     # ProjectMemoryRail + explore/plan 子代理装配级集成测试
//	├── deep_adapter_evolution_test.go          # 演进进度事件推送
//	├── deep_adapter_reload_test.go             # 热重载后 DeepAgent 装配完整性（workspace/sys_operation/fs 工具）
//	├── goal_control_plane_test.go              # goal 控制面五 action 状态往返
//	├── goal_interaction_test.go                # goal 交互链路对照基线（无目标不产生分气泡 final）
//	├── model_token_config_test.go              # max_input/output_tokens 配置到运行时装配
//	└── session_usage_isolation_test.go         # 跨会话 usage_summary 数值与 prompt 历史隔离（A→B→A 三轮）
//
// 隔离环境（UAPCLAW_DATA_DIR / MockLLM provider / ResourceMgr 收尾）由
// tests/integration/testhelpers.NewAdapterTestEnv 提供
//
// 对应 Python 代码：
//
//	jiuwenswarm/tests/unit_tests/agentserver/test_code_adapter_acp_chat_tool.py
//	jiuwenswarm/tests/system_tests/test_project_memory.py
//
// 运行方式：go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/...
package adapter
