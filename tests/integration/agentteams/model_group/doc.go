// Package model_group 23.4c 模型组模型池集成测试。
//
// 覆盖 spawn 点名持久化、非法点名中止、整组分配、记账→名册读池（D-2 闭环）
// 与编译产物物化五条链路。
//
// 文件目录：
//
//	model_group/
//	├── doc.go            # 包文档
//	└── model_group_test.go  # 五条链路集成用例
//
// 对应 Python 代码：tests/unit_tests/agent_teams/models/test_model_group_contract.py
// · tests/unit_tests/agent_teams/tools/test_spawn_teammate_model_group.py
package model_group_test
