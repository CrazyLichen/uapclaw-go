package trajectory

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枌举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TrajectoryExtractor 轨迹提取器接口。
//
// 从 Agent 执行 Session 中提取完整 Trajectory。
// TracerTrajectoryExtractor 是默认实现（后续章节实现）。
//
// 对应 Python: openjiuwen/agent_evolving/trajectory/extractor.py TrajectoryExtractor
//
//	class TrajectoryExtractor:
//	    def extract(self, session: Any, case_id: Optional[str] = None) -> Trajectory: ...
type TrajectoryExtractor interface {
	// Extract 从 Session 提取 Trajectory。
	//
	// 对应 Python: TrajectoryExtractor.extract(session, case_id)
	Extract(sess *session.Session, caseID string) *Trajectory
}

// ──────────────────────────── 非导出函数 ────────────────────────────
