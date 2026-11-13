// recommendation 包的推荐同意状态只读探测。
// 本文件是上游 topic_controls.py 的只读切片（spec 10.7.2 D-1）：只读
// recommendation.db 的 recommendation_consent_state 表（同意边界 + 删除 epoch），
// 不建表、不写入；写入侧（RecommendationConsentStore.save / freeze_consent_boundary /
// 两个 migrate 函数）⤵️ 10.3.31d 建 topic_controls.go 时收编本文件。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_controls.py:44-92
package recommendation

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// consentStateRow 同意边界与删除 epoch 的只读快照。
// Python: topic_controls.py:67 _consent_state 返回 {"history_after": float|None, "deleted_at": float|None}
type consentStateRow struct {
	// historyAfter 同意边界（nil = 未记录）
	historyAfter *float64
	// deletedAt 删除 epoch（nil = 未记录；10.7.2 的 consent_epoch 只消费本字段）
	deletedAt *float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ConsentDBName 同意状态与推荐数据共用的库文件（导出：server 侧 promptStore 复用同一路径）。
	// Python: topic_controls.py:41 _CONSENT_DB_NAME
	ConsentDBName = "recommendation.db"
	// consentTableName 同意状态表名
	// Python: topic_controls.py:42 _CONSENT_TABLE
	consentTableName = "recommendation_consent_state"
	// consentMissingTable sqlite3 对先于建表的库报的原文，按此静默降级
	// Python: topic_controls.py:48 _MISSING_TABLE
	consentMissingTable = "no such table"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// readConsentState 只读探测同意状态；库不存在、表不存在、被锁或损坏一律降级
// "未记录"，绝不建库建表，也绝不向上抛错。
// Python: topic_controls.py:67-92 _consent_state（缺表不 Warn，其余 Warn）
func readConsentState() consentStateRow {
	path := filepath.Join(workspace.AgentWorkspaceDir(), ConsentDBName)
	if _, err := os.Stat(path); err != nil {
		return consentStateRow{}
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("path", path).Msg("recommendation consent state unreadable")
		return consentStateRow{}
	}
	defer func() { _ = db.Close() }()

	row := db.QueryRow("SELECT history_after, deleted_at FROM " + consentTableName + " WHERE id = 1")
	var history, deleted sql.NullFloat64
	if err := row.Scan(&history, &deleted); err != nil {
		if !strings.Contains(err.Error(), consentMissingTable) {
			logger.Warn(logComponent).Err(err).Str("path", path).Msg("recommendation consent state unreadable")
		}
		return consentStateRow{}
	}
	return consentStateRow{
		historyAfter: nullFloatToPtr(history),
		deletedAt:    nullFloatToPtr(deleted),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nullFloatToPtr SQL NULL → nil 指针。
func nullFloatToPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}
