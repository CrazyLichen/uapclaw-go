package tools

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMessageManager 团队消息管理器接口。⤵️ 回填: 9.65a
type TeamMessageManager interface {
	// SendMessage 发送消息
	SendMessage(ctx context.Context, content string, to string, from string) (string, error)
	// BroadcastMessage 广播消息
	BroadcastMessage(ctx context.Context, content string, from string) (string, error)
	// GetMessages 获取指定成员的消息
	GetMessages(ctx context.Context, to string, from string, unreadOnly bool) ([]any, error)
	// GetBroadcastMessages 获取广播消息
	GetBroadcastMessages(ctx context.Context, memberName string, unreadOnly bool) ([]any, error)
	// GetTeamMessages 获取团队所有消息
	GetTeamMessages(ctx context.Context, teamName string) ([]any, error)
	// HasUnreadMessages 是否有未读消息
	HasUnreadMessages(ctx context.Context, includeBroadcast bool) bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
