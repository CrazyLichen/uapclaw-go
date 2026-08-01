// Package fsm 提供状态机（FSM）转换表和校验函数。
//
// 本包是 agent_teams 子系统的共享 FSM 契约层，使用 string 类型定义所有状态常量和转换表，
// 使 schema、database 等包都可 import 而不产生循环依赖。
// schema 包通过类型别名（MemberStatus = string）提供类型化包装，
// database 包直接使用 string 版本。
// 对齐 Python: is_valid_transition() + MEMBER_TRANSITIONS/EXECUTION_TRANSITIONS/TASK_TRANSITIONS。
//
// 文件目录：
//
//	fsm/
//	├── doc.go           # 包文档
//	├── transitions.go   # 状态常量 + 转换表 + 校验函数
//	└── transitions_test.go # 转换表和校验函数测试
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/status.py
package fsm
