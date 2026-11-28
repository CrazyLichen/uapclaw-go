package kbadmin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKBModelTableName 测试五张表表名注册正确
func TestKBModelTableName(t *testing.T) {
	require.Equal(t, "kb_info", (&KBInfo{}).TableName())
	require.Equal(t, "kb_doc", (&KBDoc{}).TableName())
	require.Equal(t, "kb_chunk_meta", (&KBChunkMeta{}).TableName())
	require.Equal(t, "kb_chunk_revision", (&KBChunkRevision{}).TableName())
	require.Equal(t, "kb_task", (&KBTask{}).TableName())
}
