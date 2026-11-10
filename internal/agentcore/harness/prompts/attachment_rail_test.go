package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

)

// fakeRailProvider 提供固定管理器的 RailAttachmentProvider 实现。
type fakeRailProvider struct {
	// m 被返回的管理器（可为 nil）
	m *Manager
}

// PromptAttachments 返回管理器。
func (f *fakeRailProvider) PromptAttachments() *Manager { return f.m }

// TestManagerFor_非Provider返回nil 验证类型断言失败时安全返回。
func TestManagerFor_非Provider返回nil(t *testing.T) {
	require.Nil(t, ManagerFor(nil))
	require.Nil(t, ManagerFor(struct{}{}))
}
