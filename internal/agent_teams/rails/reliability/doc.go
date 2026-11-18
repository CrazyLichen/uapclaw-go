// Package reliability 提供团队可靠性监测子系统（25.1g）。
//
// 成员 DeepAgent 上挂 ReliabilityRail（priority=5，仅采集 + 非破坏性 steer），
// 生命周期钩子转成 Signal 喂 ReliabilityMonitor；成员内 detectors 判定异常，
// RemediationPolicy 按严重度把异常路由为三类出口：
//
//	① 本地 steer（LocalAutoRemediator，轮内自愈，强度预算限流）
//	② 跨进程上报 leader（EventAnomalyReporter → messager TEAM topic）
//	③ leader 自检（LocalAnomalyReporter → 进程内 sink，绕过 self-filter）
//
// leader 进程的 ReliabilityHandler / ExternalRuntimeHandler（coordination/handlers）
// 消费上报并经 deliver_input 注入 leader 轮次；最终决策权在 leader LLM 或用户。
//
// 文件目录：
//
//	reliability/
//	├── doc.go              # 包文档
//	├── anomaly.go          # Anomaly / AnomalyKind / Severity（异常模型）
//	├── signals.go          # Signal / SignalKind（统一信号流）
//	├── window.go           # SlidingWindowCounter + 稳定哈希
//	├── config.go           # ReliabilityConfig 及各 detector 阈值配置
//	├── monitor.go          # ReliabilityMonitor（扇出 + 隔离 + 策略路由）
//	├── rail.go             # ReliabilityRail（7 钩子采集 + 本地纠偏）
//	├── reporter.go         # AnomalyReporter 接口 + Event/Local 两实现
//	├── factory.go          # 组件装配（detectors / rail / handler 依赖）
//	├── detectors/          # 六个成员内 detector + 团队级 pingpong
//	└── remediation/       # 动作分类 / 严重度策略 / 本地自动纠偏
//
// 对应 Python 代码：openjiuwen/agent_teams/reliability/
package reliability