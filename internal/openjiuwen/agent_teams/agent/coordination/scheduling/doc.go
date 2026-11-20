// Package scheduling 提供 scheduled dispatch 的 leader 侧决策引擎（F_62）。
//
// 职责（Python agent/scheduling/scheduler.py 模块 docstring 摘译）：leader 的
// 事件循环之外挂一个「看板对账引擎」。它不执行任务、不跑成员——只读板面、把
// 该发生的事发生掉：给空闲成员派已指派的 PENDING 任务（start），给没收到启动
// 信的在飞任务补发 handoff，把改过而承担者未读的任务正文推过去（F_129 revision
// mail），给板面清空的 task-scoped 成员发退役请求（F_118），并在任务到达终态
// 时向 leader 注入 digest（F_119）。评审半区（派发/裁决/结算/升级）随 25.2b。
//
// 两个受众、两种机制（render.py docstring）：
//   - 成员收 leader 身份的信箱消息。措辞不在这里组装——这里只构造投递载荷
//     （meta：模板键 + 行引用 + 瞬态参数），信箱行存空 content，投递时按
//     当前任务行现场渲染（prompts/message_template_expand.go，F_63）。
//   - leader 收直接输入注入（digest / escalation）。不经信箱，无 meta 通道，
//     保持单行 i18n（schema/i18n.go scheduler.* 键，i18n.py L260-308 逐字）。
//
// 文件目录：
//
//	scheduling/
//	├── doc.go        # 包文档
//	├── render.go     # 投递载荷（meta_*）与 leader 渲染（render_leader_*）
//	└── scheduler.go  # TeamScheduler：唤醒过滤 + 五遍 reconcile 扫描 + 投递原语
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/scheduling/{render.py,scheduler.py}
package scheduling
