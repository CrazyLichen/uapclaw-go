package store

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphStore 日志壳：包装 pregel.Store 并按 Python 逐条复刻日志与异常透传。
// GraphState/Store/CreateState（store/base.py:32-82）已于 2026-11-15 8.3c 实施时
// 迁入 pregel 包（spec 偏差 B5，engine 依赖破环，与 8.3b D6 同源），本文件只留日志壳。
// Python: store/base.py:80-159
type GraphStore struct {
	// saver 被包装的真实存储
	saver pregel.Store
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量（规则 3）
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGraphStore 构造日志壳
// Python: store/base.py:81-83（__init__）
func NewGraphStore(saver pregel.Store) *GraphStore {
	return &GraphStore{saver: saver}
}

// Get 读取图状态：未命中记 debug、异常 error 后原样透传。
// Python: store/base.py:85-103
func (g *GraphStore) Get(ctx context.Context, sessionID, ns string) (*pregel.GraphState, error) {
	state, err := g.saver.Get(ctx, sessionID, ns)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_STORE_GET").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed to get graph state")
		return nil, err
	}
	if state == nil {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_STORE_GET").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Msg("Not found graph state for session")
	}
	return state, nil
}

// Save 保存图状态：前后 debug（含 step）、异常 error 后原样透传。
// Python: store/base.py:105-130
func (g *GraphStore) Save(ctx context.Context, sessionID, ns string, state *pregel.GraphState) error {
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_SAVE").
		Str("session_id", sessionID).
		Int("step", state.Step).
		Str("graph_id", ns).
		Msg("Begin to save graph state of super-step")
	if err := g.saver.Save(ctx, sessionID, ns, state); err != nil {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_STORE_SAVE").
			Str("session_id", sessionID).
			Int("step", state.Step).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed to save graph state of super-step")
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_SAVE").
		Str("session_id", sessionID).
		Int("step", state.Step).
		Str("graph_id", ns).
		Msg("Succeed to save graph state of super-step")
	return nil
}

// Delete 删除图状态：ns 空串打 all、前后 debug、异常走 debug 级（Python 原文即 logger.debug + re-raise，照抄）后透传。
// Python: store/base.py:132-159
func (g *GraphStore) Delete(ctx context.Context, sessionID, ns string) error {
	target := ns
	if target == "" {
		target = "all"
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_DELETE").
		Str("session_id", sessionID).
		Str("graph_id", ns).
		Msg("Begin to delete " + target + " graph states for session")
	if err := g.saver.Delete(ctx, sessionID, ns); err != nil {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_STORE_DELETE").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed delete " + target + " graph states for session")
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_DELETE").
		Str("session_id", sessionID).
		Str("graph_id", ns).
		Msg("Succeed to delete " + target + " graph states for session")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
