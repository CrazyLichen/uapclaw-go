package model

import (
	"context"
	"errors"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageAddRequest 添加消息请求。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/message_manager.py (MessageAddRequest)
type MessageAddRequest struct {
	// UserID 用户 ID（必填）
	UserID string
	// ScopeID 作用域 ID（必填）
	ScopeID string
	// Content 消息内容（必填）
	Content string
	// Role 消息角色
	Role string
	// SessionID 会话 ID
	SessionID string
	// Timestamp 时间戳（零值时自动生成当前时间）
	Timestamp time.Time
}

// MessageManager 消息管理器，BaseMessageStore 的上层封装。
//
// 提供验证和简化的消息操作接口，由 LongTermMemory 使用。
// 对应 Python: openjiuwen/core/memory/manage/mem_model/message_manager.py (MessageManager)
type MessageManager struct {
	// store 消息存储接口
	store db.BaseMessageStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageManager 创建 MessageManager 实例。
func NewMessageManager(store db.BaseMessageStore) *MessageManager {
	return &MessageManager{store: store}
}

// Add 验证必填字段后添加消息。
// 必填字段：UserID、ScopeID、Content。
//
// 对应 Python: MessageManager.add(req)
func (m *MessageManager) Add(ctx context.Context, req *MessageAddRequest) (string, error) {
	if req.UserID == "" {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", "must provide user_id for add message"),
		)
	}
	if req.ScopeID == "" {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", "must provide scope_id for add message"),
		)
	}
	if req.Content == "" {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", "must provide content for add message"),
		)
	}

	// 解析 role
	role := roleTypeFromString(req.Role)
	message := schema.NewDefaultMessage(role, req.Content)

	timestamp := req.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	messageAdd := &db.MessageAdd{
		Message:   message,
		UserID:    req.UserID,
		ScopeID:   req.ScopeID,
		SessionID: req.SessionID,
		Timestamp: timestamp,
	}

	return m.store.AddMessage(ctx, messageAdd)
}

// Get 获取消息，返回 (消息, 时间戳) 列表。
// 倒序获取后反转，使最旧的消息排在前面。
//
// 对应 Python: MessageManager.get(user_id, scope_id, session_id, message_len)
func (m *MessageManager) Get(ctx context.Context, userID string, scopeID string, sessionID string, messageLen int) ([]*db.MessageAndMeta, error) {
	if messageLen <= 0 {
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", "message length must be bigger than zero for get message"),
		)
	}

	filter := &db.MessageFilter{
		UserID:    userID,
		ScopeID:   scopeID,
		SessionID: &sessionID,
	}

	// 倒序获取
	messages, err := m.store.GetMessages(ctx, filter, messageLen, "timestamp", "desc")
	if err != nil {
		return nil, err
	}

	// 反转，使最旧的在前
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetByID 按 ID 获取消息，不存在时返回 nil。
// 修正：正确传播 error，只在"未找到"时返回 (nil, nil)。
//
// 对应 Python: MessageManager.get_by_id(msg_id)
func (m *MessageManager) GetByID(ctx context.Context, msgID string) (*db.MessageAndMeta, error) {
	msg, meta, err := m.store.GetMessageByID(ctx, msgID)
	if err != nil {
		// 消息不存在返回 nil, nil；其他错误正常传播
		var baseErr *exception.BaseError
		if errors.As(err, &baseErr) && baseErr.Status() == exception.StatusStoreMessageNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &db.MessageAndMeta{Message: msg, Metadata: meta}, nil
}

// DeleteByUserAndScope 删除指定用户+作用域的所有消息。
//
// 对应 Python: MessageManager.delete_by_user_and_scope(user_id, scope_id)
func (m *MessageManager) DeleteByUserAndScope(ctx context.Context, userID string, scopeID string) (int64, error) {
	filter := &db.MessageFilter{
		UserID:  userID,
		ScopeID: scopeID,
	}
	return m.store.DeleteMessages(ctx, filter)
}
