package kbadmin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStubServices_未实现 测试骨架方法返回 ErrNotImplemented（30.5 已摘 Doc.Upload 桩）
func TestStubServices_未实现(t *testing.T) {
	st := newTestStore(t)
	require.ErrorIs(t, NewChunkService(st).Update(), ErrNotImplemented)
	require.ErrorIs(t, NewSearchService(st).Test(), ErrNotImplemented)
}
