//go:build integration

package session_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 测试套件 ────────────────────────────

// SessionControllerE2ESuite SessionController E2E 集成测试套件
type SessionControllerE2ESuite struct {
	suite.Suite
	// ctx 测试上下文
	ctx context.Context
	// tmpDir 临时测试目录
	tmpDir string
}

func TestSessionControllerE2ESuite(t *testing.T) {
	suite.Run(t, new(SessionControllerE2ESuite))
}

func (s *SessionControllerE2ESuite) SetupTest() {
	s.ctx = context.Background()
	s.tmpDir = s.T().TempDir()
}

// ──────────────────────────── SessionController ────────────────────────────

// TestSessionController_创建 测试创建 SessionController
func (s *SessionControllerE2ESuite) TestSessionController_创建() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	s.NotNil(sc)
	s.Equal("agent-1", sc.AgentID)
	s.NotEmpty(sc.BasePath)
	s.Empty(sc.SessionCache, "初始缓存应为空")
	s.Empty(sc.MetaMap, "初始元数据应为空")
}

// TestSessionController_CreateIfNotExists_新建 测试新建会话
func (s *SessionControllerE2ESuite) TestSessionController_CreateIfNotExists_新建() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	isNew, sess, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)
	s.True(isNew, "首次创建应返回 isNew=true")
	s.NotNil(sess)
	s.Equal("sess-1", sess.SessionID)
	s.Equal("agent-1", sess.AgentID)
}

// TestSessionController_CreateIfNotExists_复用活跃 测试已有活跃会话时复用
func (s *SessionControllerE2ESuite) TestSessionController_CreateIfNotExists_复用活跃() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	isNew1, sess1, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)
	s.True(isNew1)

	// 同一 scope 的第二次创建应返回已有活跃会话
	isNew2, sess2, err := sc.CreateIfNotExists(scope, "sess-2")
	s.NoError(err)
	s.False(isNew2, "已有活跃会话时不应创建新会话")
	s.Equal(sess1.SessionID, sess2.SessionID, "应返回同一活跃会话")
}

// TestSessionController_GetScopeActiveSession 测试获取活跃会话
func (s *SessionControllerE2ESuite) TestSessionController_GetScopeActiveSession() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	// 未创建时返回 nil
	s.Nil(sc.GetScopeActiveSession(scope))

	// 创建后可获取
	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)
	active := sc.GetScopeActiveSession(scope)
	s.NotNil(active)
	s.Equal("sess-1", active.SessionID)
}

// TestSessionController_ActivateSession 测试激活会话
func (s *SessionControllerE2ESuite) TestSessionController_ActivateSession() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	// 激活已存在的会话
	err = sc.ActivateSession("sess-1")
	s.NoError(err)
}

// TestSessionController_ActivateSession_不存在 测试激活不存在的会话
func (s *SessionControllerE2ESuite) TestSessionController_ActivateSession_不存在() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)

	err := sc.ActivateSession("nonexist")
	s.Error(err, "激活不存在的会话应返回错误")
}

// TestSessionController_GetScopeSessions 测试获取作用域下会话列表
func (s *SessionControllerE2ESuite) TestSessionController_GetScopeSessions() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	// 未创建时返回 nil
	s.Nil(sc.GetScopeSessions(scope))

	// 创建后可获取
	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)
	sessions := sc.GetScopeSessions(scope)
	s.Len(sessions, 1)
}

// TestSessionController_ListMetas 测试列出元数据
func (s *SessionControllerE2ESuite) TestSessionController_ListMetas() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	metas := sc.ListMetas()
	s.Empty(metas, "初始元数据应为空")

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)
	metas = sc.ListMetas()
	s.NotEmpty(metas, "创建会话后元数据非空")
}

// TestSessionController_Flush 测试持久化
func (s *SessionControllerE2ESuite) TestSessionController_Flush() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	err = sc.Flush()
	s.NoError(err, "Flush 应成功")
}

// TestSessionController_Load 测试从磁盘加载
func (s *SessionControllerE2ESuite) TestSessionController_Load() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	// 创建并刷盘
	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)
	s.NoError(sc.Flush())

	// 新建控制器加载
	sc2 := controller.NewSessionController("agent-1", s.tmpDir)
	err = sc2.Load(true)
	s.NoError(err, "Load 应成功")
	active := sc2.GetScopeActiveSession(scope)
	s.NotNil(active, "加载后应能获取活跃会话")
	s.Equal("sess-1", active.SessionID)
}

// TestSessionController_RemoveSession 测试删除会话
func (s *SessionControllerE2ESuite) TestSessionController_RemoveSession() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	result := sc.RemoveSession("sess-1", nil)
	s.NotEmpty(result, "删除应返回结果")
	s.Nil(sc.GetScopeActiveSession(scope), "删除后获取活跃会话应为 nil")
}

// TestSessionController_CleanupScopeInactiveSessions 测试清理非活跃会话
func (s *SessionControllerE2ESuite) TestSessionController_CleanupScopeInactiveSessions() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	// 无非活跃会话
	results, err := sc.CleanupScopeInactiveSessions(scope)
	s.NoError(err)
	s.Empty(results[0].Sessions, "无非活跃会话应返回空列表")
}

// TestSessionController_GetScopeMeta 测试获取作用域元数据
func (s *SessionControllerE2ESuite) TestSessionController_GetScopeMeta() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	// 未创建时返回默认元数据
	meta := sc.GetScopeMeta(scope)
	s.Empty(meta.Sessions, "未创建时 Sessions 应为空")

	// 创建后获取
	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)
	meta = sc.GetScopeMeta(scope)
	s.NotEmpty(meta.Sessions, "创建后 Sessions 非空")
}

// TestSessionController_DirectSubject 测试私聊作用域
func (s *SessionControllerE2ESuite) TestSessionController_DirectSubject() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{
		Scope:   controller.MainScope{},
		Subject: controller.DirectSubject{UserID: "user-1"},
	}

	isNew, sess, err := sc.CreateIfNotExists(scope, "sess-direct")
	s.NoError(err)
	s.True(isNew)
	s.NotNil(sess)
	s.Equal("sess-direct", sess.SessionID)

	// 不同 Subject 的会话应隔离
	scope2 := controller.SessionScope{
		Scope:   controller.MainScope{},
		Subject: controller.DirectSubject{UserID: "user-2"},
	}
	isNew2, sess2, err := sc.CreateIfNotExists(scope2, "sess-direct-2")
	s.NoError(err)
	s.True(isNew2, "不同 Subject 应创建新会话")
	s.NotEqual(sess.SessionID, sess2.SessionID)
}

// TestSessionController_GroupSubject 测试群聊作用域
func (s *SessionControllerE2ESuite) TestSessionController_GroupSubject() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{
		Scope:   controller.MainScope{},
		Subject: controller.GroupSubject{GroupID: "group-1"},
	}

	isNew, sess, err := sc.CreateIfNotExists(scope, "sess-group")
	s.NoError(err)
	s.True(isNew)
	s.NotNil(sess)
}

// TestSessionController_RemoveAll 测试删除所有会话
func (s *SessionControllerE2ESuite) TestSessionController_RemoveAll() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	sc.RemoveAll()
	s.Empty(sc.SessionCache, "RemoveAll 后缓存应为空")
	s.Empty(sc.MetaMap, "RemoveAll 后元数据应为空")
}

// TestSessionController_FlushSession 测试持久化指定会话
func (s *SessionControllerE2ESuite) TestSessionController_FlushSession() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	err = sc.FlushSession("sess-1")
	s.NoError(err)

	// 不存在的会话不应报错
	err = sc.FlushSession("nonexist")
	s.NoError(err, "FlushSession 不存在的会话应返回 nil")
}

// TestSessionController_FlushScope 测试按作用域持久化
func (s *SessionControllerE2ESuite) TestSessionController_FlushScope() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, _, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	err = sc.FlushScope(scope)
	s.NoError(err)

	// 不存在的 scope 不应报错
	otherScope := controller.SessionScope{
		Scope:   controller.MainScope{},
		Subject: controller.DirectSubject{UserID: "other"},
	}
	err = sc.FlushScope(otherScope)
	s.NoError(err, "FlushScope 不存在的 scope 应返回 nil")
}

// ──────────────────────────── GlobalSessionController ────────────────────────────

// TestGlobalSessionController_获取单例 测试获取单例
func (s *SessionControllerE2ESuite) TestGlobalSessionController_获取单例() {
	gsc := controller.GetGlobalSessionController()
	s.NotNil(gsc)
	// 二次获取应返回同一实例
	gsc2 := controller.GetGlobalSessionController()
	s.Equal(gsc, gsc2, "应返回同一单例")
}

// TestGlobalSessionController_SetConfig 测试设置配置
func (s *SessionControllerE2ESuite) TestGlobalSessionController_SetConfig() {
	gsc := controller.GetGlobalSessionController()
	original := gsc.BasePath
	gsc.SetConfig(controller.GlobalSessionConfig{BasePath: "/tmp/test"})
	s.Equal("/tmp/test", gsc.BasePath)
	// 恢复
	gsc.SetConfig(controller.GlobalSessionConfig{BasePath: original})
}

// ──────────────────────────── SessionScope ────────────────────────────

// TestSessionScope_MainScope 测试主作用域字符串
func (s *SessionControllerE2ESuite) TestSessionScope_MainScope() {
	scope := controller.SessionScope{Scope: controller.MainScope{}}
	s.Equal("main", scope.String())
}

// TestSessionScope_DirectSubject 测试私聊作用域字符串
func (s *SessionControllerE2ESuite) TestSessionScope_DirectSubject() {
	scope := controller.SessionScope{
		Scope:   controller.MainScope{},
		Subject: controller.DirectSubject{UserID: "u1"},
	}
	s.Equal("main:direct:u1", scope.String())
}

// TestSessionScope_GroupSubject 测试群聊作用域字符串
func (s *SessionControllerE2ESuite) TestSessionScope_GroupSubject() {
	scope := controller.SessionScope{
		Scope:   controller.MainScope{},
		Subject: controller.GroupSubject{GroupID: "g1"},
	}
	s.Equal("main:group:g1", scope.String())
}

// TestSessionScope_GroupUserSubject 测试群内用户作用域字符串
func (s *SessionControllerE2ESuite) TestSessionScope_GroupUserSubject() {
	scope := controller.SessionScope{
		Scope:   controller.MainScope{},
		Subject: controller.GroupUserSubject{GroupID: "g1", UserID: "u1"},
	}
	s.Equal("main:group:g1:user:u1", scope.String())
}

// ──────────────────────────── ChainSession ────────────────────────────

// TestChainSession_属性 测试 ChainSession 基本属性
func (s *SessionControllerE2ESuite) TestChainSession_属性() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, sess, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	s.Equal("sess-1", sess.SessionID)
	s.Equal("agent-1", sess.AgentID)
	s.Equal(scope, sess.SessionScope)
	s.NotNil(sess.DataContainer, "DataContainer 不应为 nil")
}

// TestChainSession_Flush 测试刷盘
func (s *SessionControllerE2ESuite) TestChainSession_Flush() {
	sc := controller.NewSessionController("agent-1", s.tmpDir)
	scope := controller.SessionScope{Scope: controller.MainScope{}}

	_, sess, err := sc.CreateIfNotExists(scope, "sess-1")
	s.NoError(err)

	err = sess.Flush()
	s.NoError(err, "ChainSession Flush 应成功")
}

// ──────────────────────────── Session（业务层）────────────────────────────

// TestSession_创建AgentSession 测试创建 AgentSession
func (s *SessionControllerE2ESuite) TestSession_创建AgentSession() {
	sess := session.CreateAgentSession("sess-1", nil, nil)
	s.NotNil(sess)
	s.Equal("sess-1", sess.GetSessionID())
}

// TestSession_PreRunPostRun 测试会话生命周期
func (s *SessionControllerE2ESuite) TestSession_PreRunPostRun() {
	sess := session.CreateAgentSession("sess-lifecycle", nil, nil)

	// PreRun
	err := sess.PreRun(s.ctx)
	s.NoError(err)

	// 写入状态
	sess.UpdateState(map[string]any{"test_key": "test_value"})
	val, err := sess.GetState(state.StringKey("test_key"))
	s.NoError(err)
	s.Equal("test_value", val)

	// PostRun 幂等
	err = sess.PostRun(s.ctx)
	s.NoError(err)
	err = sess.PostRun(s.ctx)
	s.NoError(err, "PostRun 应幂等")
}

// TestSession_Tracer 测试 Tracer
func (s *SessionControllerE2ESuite) TestSession_Tracer() {
	sess := session.CreateAgentSession("sess-tracer", nil, nil)
	s.NotNil(sess.Tracer())
}

// TestSession_WriteStream 测试 WriteStream
func (s *SessionControllerE2ESuite) TestSession_WriteStream() {
	sess := session.CreateAgentSession("sess-stream", nil, nil)
	err := sess.WriteStream(s.ctx, map[string]any{"type": "text", "content": "hello"})
	s.NoError(err)
}

// TestSession_ClearSession 测试 ClearSession
func (s *SessionControllerE2ESuite) TestSession_ClearSession() {
	sess := session.CreateAgentSession("sess-clear", nil, nil)
	err := sess.PreRun(s.ctx)
	s.NoError(err)

	sess.UpdateState(map[string]any{"key": "value"})
	sess.ClearSession(s.ctx)

	// ClearSession 后状态应被清空
	val, err := sess.GetState(state.StringKey("key"))
	s.NoError(err)
	s.Nil(val, "ClearSession 后状态应为 nil")
}
