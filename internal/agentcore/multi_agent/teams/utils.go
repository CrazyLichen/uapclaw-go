package teams

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MakeTeamSession 创建团队会话。
//
// 从 message 中提取 conversation_id 作为 sessionID，若无则生成新 UUID。
// 使用 card.GetID() 作为 teamID，调用 CreateAgentTeamSession 创建会话。
//
// 对应 Python: teams/utils.py make_team_session(card, message)
func MakeTeamSession(card maschema.TeamCardInterface, message map[string]any) *session.AgentTeamSession {
	sid := extractConversationID(message)
	if sid == "" {
		sid = uuid.New().String()
	}

	teamSession := session.CreateAgentTeamSession(sid, nil, card.GetID())

	logger.Info(logComponent).
		Str("action", "make_team_session").
		Str("session_id", sid).
		Str("team_id", card.GetID()).
		Msg("创建团队会话")

	return teamSession
}

// StandaloneInvokeContext 独立调用上下文，管理会话生命周期。
//
// 若 sess 不为 nil，直接使用外部会话（调用者负责生命周期）。
// 若 sess 为 nil，创建新会话并在函数返回后自动清理（UnbindTeamSession + CleanupSession + CloseStream + Commit）。
//
// fn 是业务逻辑函数，接收 (teamSession, sessionID) 并返回结果。
//
// 对应 Python: teams/utils.py standalone_invoke_context(runtime, card, message, session)
func StandaloneInvokeContext(
	ctx context.Context,
	runtime *team_runtime.TeamRuntime,
	card maschema.TeamCardInterface,
	message map[string]any,
	sess *session.AgentTeamSession,
	fn func(*session.AgentTeamSession, string) (map[string]any, error),
) (map[string]any, error) {
	callerOwns := sess != nil
	var teamSession *session.AgentTeamSession

	if callerOwns {
		teamSession = sess
	} else {
		teamSession = MakeTeamSession(card, message)
		if err := teamSession.PreRun(ctx, message); err != nil {
			logger.Error(logComponent).Err(err).
				Str("action", "standalone_invoke_context").
				Str("session_id", teamSession.GetSessionID()).
				Str("team_id", card.GetID()).
				Msg("PreRun 失败")
			return nil, err
		}
		runtime.BindTeamSession(teamSession)
	}

	sid := teamSession.GetSessionID()

	logger.Info(logComponent).
		Str("action", "standalone_invoke_context").
		Str("session_id", sid).
		Bool("caller_owns", callerOwns).
		Msg("进入独立调用上下文")

	// 对齐 Python try/finally：即使 fn panic 也保证清理代码执行
	var result map[string]any
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("业务逻辑 panic: %v", r)
				logger.Error(logComponent).
					Str("action", "standalone_invoke_context").
					Str("session_id", sid).
					Any("panic", r).
					Msg("业务逻辑发生 panic")
			}
		}()
		result, err = fn(teamSession, sid)
	}()

	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "standalone_invoke_context").
			Str("session_id", sid).
			Msg("业务逻辑执行失败")
	}

	if !callerOwns {
		runtime.UnbindTeamSession(sid)
		if cleanupErr := runtime.CleanupSession(ctx, sid); cleanupErr != nil {
			logger.Warn(logComponent).Err(cleanupErr).
				Str("action", "standalone_invoke_context").
				Str("session_id", sid).
				Msg("CleanupSession 失败")
		}
		if closeErr := teamSession.CloseStream(); closeErr != nil {
			logger.Warn(logComponent).Err(closeErr).
				Str("action", "standalone_invoke_context").
				Str("session_id", sid).
				Msg("CloseStream 失败")
		}
		if commitErr := teamSession.Commit(ctx); commitErr != nil {
			logger.Warn(logComponent).Err(commitErr).
				Str("action", "standalone_invoke_context").
				Str("session_id", sid).
				Msg("Commit 失败")
		}

		logger.Info(logComponent).
			Str("action", "standalone_invoke_context").
			Str("session_id", sid).
			Msg("独立调用上下文已清理")
	}

	return result, err
}

// StandaloneStreamContext 独立流式上下文，管理会话生命周期并返回流通道。
//
// 若 sess 不为 nil，直接使用外部会话（调用者负责生命周期）。
// 若 sess 为 nil，创建新会话并在流结束后自动清理。
//
// runFn 是流式业务逻辑函数，接收 (teamSession, sessionID) 并返回 error。
// runFn 内部通过 teamSession.WriteStream() 写入流数据，
// 消费者通过返回的 channel 读取 teamSession.StreamIterator() 的数据。
//
// 对齐 Python: standalone_stream_context 中
//
//	async for chunk in team_session.stream_iterator(): yield chunk
//
// Go 直接返回 teamSession.StreamIterator()，runFn 在后台 goroutine 中运行，
// 数据通过 StreamWriterManager → StreamOutput() 自动流到消费者。
//
// 对应 Python: teams/utils.py standalone_stream_context(runtime, card, message, run_coro, session)
func StandaloneStreamContext(
	ctx context.Context,
	runtime *team_runtime.TeamRuntime,
	card maschema.TeamCardInterface,
	message map[string]any,
	sess *session.AgentTeamSession,
	runFn func(*session.AgentTeamSession, string) error,
) (<-chan stream.Schema, error) {
	callerOwns := sess != nil
	var teamSession *session.AgentTeamSession

	if callerOwns {
		teamSession = sess
	} else {
		teamSession = MakeTeamSession(card, message)
		if err := teamSession.PreRun(ctx, message); err != nil {
			logger.Error(logComponent).Err(err).
				Str("action", "standalone_stream_context").
				Str("session_id", teamSession.GetSessionID()).
				Str("team_id", card.GetID()).
				Msg("PreRun 失败")
			return nil, err
		}
		runtime.BindTeamSession(teamSession)
	}

	sid := teamSession.GetSessionID()

	logger.Info(logComponent).
		Str("action", "standalone_stream_context").
		Str("session_id", sid).
		Bool("caller_owns", callerOwns).
		Msg("进入独立流式上下文")

	// 对齐 Python: 返回 team_session.stream_iterator()，
	// 消费者从此 channel 读取 runFn 通过 WriteStream 写入的流数据
	streamCh := teamSession.StreamIterator()

	// 在后台 goroutine 中运行流式逻辑并管理生命周期
	go func() {
		// 对齐 Python try/finally：即使 runFn panic 也保证清理代码执行
		var runErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					runErr = fmt.Errorf("流式业务逻辑 panic: %v", r)
					logger.Error(logComponent).
						Str("action", "standalone_stream_context").
						Str("session_id", sid).
						Any("panic", r).
						Msg("流式业务逻辑发生 panic")
				}
			}()
			runErr = runFn(teamSession, sid)
		}()

		if runErr != nil {
			logger.Error(logComponent).Err(runErr).
				Str("action", "standalone_stream_context").
				Str("session_id", sid).
				Msg("流式业务逻辑执行失败")
		}

		if !callerOwns {
			runtime.UnbindTeamSession(sid)
			if cleanupErr := runtime.CleanupSession(ctx, sid); cleanupErr != nil {
				logger.Warn(logComponent).Err(cleanupErr).
					Str("action", "standalone_stream_context").
					Str("session_id", sid).
					Msg("CleanupSession 失败")
			}
			if closeErr := teamSession.CloseStream(); closeErr != nil {
				logger.Warn(logComponent).Err(closeErr).
					Str("action", "standalone_stream_context").
					Str("session_id", sid).
					Msg("CloseStream 失败")
			}
			if commitErr := teamSession.Commit(ctx); commitErr != nil {
				logger.Warn(logComponent).Err(commitErr).
					Str("action", "standalone_stream_context").
					Str("session_id", sid).
					Msg("Commit 失败")
			}

			logger.Info(logComponent).
				Str("action", "standalone_stream_context").
				Str("session_id", sid).
				Msg("独立流式上下文已清理")
		} else {
			// Runner 拥有生命周期；仅发信号关闭流
			// 对齐 Python: _bg 的 finally 中 caller_owns 时 await team_session.close_stream()
			if closeErr := teamSession.CloseStream(); closeErr != nil {
				logger.Warn(logComponent).Err(closeErr).
					Str("action", "standalone_stream_context").
					Str("session_id", sid).
					Msg("CloseStream 失败")
			}
		}
	}()

	return streamCh, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractConversationID 从 message 中提取 conversation_id。
//
// 对应 Python: message.get("conversation_id") if isinstance(message, dict) else None
func extractConversationID(message map[string]any) string {
	if message == nil {
		return ""
	}
	val, ok := message["conversation_id"]
	if !ok || val == nil {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}
