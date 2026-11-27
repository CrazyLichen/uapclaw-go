package store

import "strings"

// CronSessionMatchesJob 会话目录名是否符合 cron 任务命名约定。
// Python: jiuwenswarm/common/cron_session.py cron_session_matches_job。
//
// 两种约定形态：
//   - cron_<job_id>：稳定的 proactive.tick 会话；
//   - cron_<ts>_<job_id>：每轮团队执行会话。
//
// 单 Agent 运行分配 __cron___<ts>_<rand> 会话，后缀随机永不匹配，
// 依赖持久化的 cron_id 元数据。
func CronSessionMatchesJob(sessionID, cronID string) bool {
	sid := strings.TrimSpace(sessionID)
	job := strings.TrimSpace(cronID)
	if sid == "" || job == "" {
		return false
	}
	if sid == "cron_"+job {
		return true
	}
	return strings.HasPrefix(sid, "cron_") && strings.HasSuffix(sid, "_"+job)
}
