// Package remediation 团队可靠性补救层（25.1g）。
//
// 纯逻辑层：动作分类（action）+ 严重度策略（policy）+ 可逆本地纠偏（local）。
// 无 I/O、无锁（消费线程为成员 supervisor 协程序列化，spec D-g1）。
//
// 文件目录：
//
//	remediation/
//	├── doc.go      # 包文档
//	├── action.go   # RemediationAction 动作枚举
//	├── policy.go   # RemediationPolicy 严重度→动作映射
//	└── local.go   # LocalAutoRemediator 强度预算限流的自纠偏
//
// 对应 Python 代码：openjiuwen/agent_teams/reliability/remediation/
package remediation
