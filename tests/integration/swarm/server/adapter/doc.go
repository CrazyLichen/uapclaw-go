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
// 文件目录：
//
//	adapter/
//	├── doc.go                                  # 包文档（本文件）
//	├── code_adapter_coding_memory_test.go      # CodingMemoryRail 装配级集成测试
//	└── code_adapter_project_memory_test.go     # ProjectMemoryRail + explore/plan 子代理装配级集成测试
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
