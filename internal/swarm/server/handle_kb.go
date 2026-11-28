package server

// AgentServer kb 分发接线：kb.* 前缀统一走 kbadmin 域（领域三十 30.1；
// Python 无对应物，独立编排域；分发层 = server/kbadmin 包）。
// 对齐 handle_rsi.go 前缀分支约定：本文件自己构造响应写回。

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/kbadmin"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// kbDomainInstance kb_admin 域句柄（懒装配，每进程一次）
var (
	kbDomainInstance *kbadmin.Domain
	kbWiringMu       sync.Mutex
	kbWiringErr      error
	// kbPushWired 推送桥进程级一次性接线标记
	kbPushWired   bool
	kbPushWiredMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleKBRequest 处理任意 kb.* method（30.1 分发全族，未实现方法返
// KB_NOT_IMPLEMENTED，见 kbadmin.HandleKBRequest）。
// 域装配失败时返回统一错误响应（不 panic、不影响 WS 主链）。
func (s *AgentServer) HandleKBRequest(_ context.Context, request *schema.AgentRequest) {
	domain := getKBDomain()
	if domain == nil {
		s.writeResponse(request.RequestID, request.ChannelID, kbadmin.BootstrapErrorResponse(request))
		return
	}
	s.wireKBPushOnce(domain)
	s.writeResponse(request.RequestID, request.ChannelID,
		kbadmin.HandleKBRequest(domain, request))
}

// wireKBPushOnce 30.2 kb.task 推送桥（进程一次）：kbadmin 事件 → AgentServer
// writeResponse 主动推送（响应帧 payload 携带 kb_task_event；requestID 走新生成
// UUID——推送无对应请求上下文，前端按 payload 语义消费，30.8 接入）
func (s *AgentServer) wireKBPushOnce(domain *kbadmin.Domain) {
	kbPushWiredMu.Lock()
	defer kbPushWiredMu.Unlock()
	if kbPushWired {
		return
	}
	kbPushWired = true
	domain.SetPushFunc(func(channelID string, event kbadmin.TaskEvent) {
		resp := schema.NewAgentResponse(uuid.NewString(), channelID,
			schema.WithPayload(map[string]any{"kb_task_event": event}))
		s.writeResponse("", channelID, resp)
	})
}

// getKBDomain 懒装配 kb_admin 域（真实 SQLite + 真实 Chroma 本地向量库；
// 数据目录 = workspace/kbadmin）。装配失败时返回 nil Domain，分发层
// nil-service 调用会 panic —— 防线上挂死改为装配失败也走错误响应。
func getKBDomain() *kbadmin.Domain {
	kbWiringMu.Lock()
	defer kbWiringMu.Unlock()
	if kbDomainInstance == nil && kbWiringErr == nil {
		store, err := kbadmin.OpenStore(workspace.WorkspaceDir() + "/kbadmin/kbadmin.db")
		if err != nil {
			logger.Error(logger.ComponentAgentServer).Err(err).
				Msg("kbadmin store bootstrap failed")
			kbWiringErr = err
			return nil
		}
		domain, err := kbadmin.NewDomain(store,
			kbadmin.NewRetrievalKBFactory(workspace.WorkspaceDir()+"/kbadmin/collections"))
		if err != nil {
			logger.Error(logger.ComponentAgentServer).Err(err).
				Msg("kbadmin domain bootstrap failed")
			kbWiringErr = err
			return nil
		}
		kbDomainInstance = domain
	}
	return kbDomainInstance
}

// ResetKBDomainForTest 测试用：重置懒装配状态（仅 _test.go 可达语义）
func ResetKBDomainForTest() {
	kbWiringMu.Lock()
	defer kbWiringMu.Unlock()
	kbDomainInstance = nil
	kbWiringErr = nil
}
