package personal_context

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRawChangeItem_构造与校验(t *testing.T) {
	content := "正文"
	item, err := NewRawChangeItem(map[string]any{
		"logical_id":   "gh:repo/readme",
		"revision_id":  "abc123",
		"operation":    "upsert",
		"content":      content,
		"original_ref": "https://github.com/x/y",
		"metadata":     map[string]any{"k": "v"},
	})
	require.NoError(t, err)
	require.Equal(t, "gh:repo/readme", item.LogicalID)
	require.Equal(t, map[string]any{"k": "v"}, item.Metadata)

	// 空白标识符拒绝
	_, err = NewRawChangeItem(map[string]any{
		"logical_id": "  ", "revision_id": "r", "operation": "upsert",
		"content": content, "original_ref": "x",
	})
	require.EqualError(t, err, "identifier must not be blank")

	// content 超 200 万字符拒绝
	long := make([]rune, 2_000_001)
	for i := range long {
		long[i] = 'a'
	}
	text := string(long)
	_, err = NewRawChangeItem(map[string]any{
		"logical_id": "a", "revision_id": "r", "operation": "upsert",
		"content": text, "original_ref": "x",
	})
	require.EqualError(t, err, "content exceeds 2,000,000 characters")

	// upsert 空内容拒绝
	empty := "   "
	_, err = NewRawChangeItem(map[string]any{
		"logical_id": "a", "revision_id": "r", "operation": "upsert",
		"content": empty, "original_ref": "x",
	})
	require.EqualError(t, err, "upsert changes require non-empty content")
}

func TestFetchBatch_缺省结果字段与校验(t *testing.T) {
	// 无结果字段 → copy_input 补齐 attempted_count/success_offsets
	item, err := NewRawChangeItem(map[string]any{
		"logical_id": "a", "revision_id": "r", "operation": "upsert",
		"content": "x", "original_ref": "x",
	})
	require.NoError(t, err)
	batch, err := NewFetchBatch(map[string]any{
		"batch_id": "batch-0",
		"items":    []any{rawItemToMap(item)},
	})
	require.NoError(t, err)
	require.Equal(t, 1, batch.AttemptedCount)
	require.Equal(t, []int{0}, batch.SuccessOffsets)

	// 非法 batch_id
	_, err = NewFetchBatch(map[string]any{"batch_id": "../escape"})
	require.EqualError(t, err, "batch_id must be a safe path segment")

	// items 与 success_offsets 数量不一致
	_, err = NewFetchBatch(map[string]any{
		"batch_id": "b", "items": []any{rawItemToMap(item)},
		"attempted_count": float64(2), "success_offsets": []any{float64(0)},
	})
	require.Error(t, err)
}

func TestFetchBatch_物化路径配对(t *testing.T) {
	pathOnly := "/tmp/candidate/x"
	_, err := NewFetchBatch(map[string]any{
		"batch_id": "b", "materialized_source_path": pathOnly,
	})
	require.EqualError(t, err, "materialized source path and revision must be provided together")

	batch, err := NewFetchBatch(map[string]any{
		"batch_id": "b", "materialized_source_path": pathOnly, "materialized_revision": "rev1",
	})
	require.NoError(t, err)
	require.NotNil(t, batch.MaterializedSourcePath)

	// 非 candidate 绝对路径拒绝
	_, err = NewFetchBatch(map[string]any{
		"batch_id": "b", "materialized_source_path": "/tmp/other/x", "materialized_revision": "rev1",
	})
	require.EqualError(t, err, "materialized_source_path must be an absolute candidate path")
}

func TestPersonalContextStatus_构造与进度校验(t *testing.T) {
	base := map[string]any{
		"configured": true, "collection_enabled": true, "agent_use_enabled": false,
		"state": "RUNNING", "pipeline_running": true, "pipeline_queue_size": float64(0),
		"fetch_service_states": map[string]any{"gh": "RUNNING"},
		"fetch_service_errors": map[string]any{},
		"fetch_run_progress":   map[string]any{},
		"context_root":         "/tmp/x",
		"context_ready":        true,
	}
	status, err := NewPersonalContextStatus(base)
	require.NoError(t, err)
	require.Equal(t, PersonalContextStateRunning, status.State)

	// succeeded 进度必须 100% 且无失败
	base["fetch_run_progress"] = map[string]any{
		"gh": map[string]any{
			"service_id": "gh", "run_state": "succeeded", "progress_percent": float64(80),
			"total_items": float64(2), "completed_items": float64(2), "failed_items": float64(0),
			"quarantined_items": float64(0), "item_errors": []any{}, "omitted_item_errors": float64(0),
			"last_error": nil,
		},
	}
	_, err = NewPersonalContextStatus(base)
	require.EqualError(t, err, "succeeded fetch run progress must be 100 percent")

	// idle 进度必须为空数值
	base["fetch_run_progress"] = map[string]any{
		"gh": map[string]any{
			"service_id": "gh", "run_state": "idle", "progress_percent": float64(10),
			"total_items": float64(0), "completed_items": float64(0), "failed_items": float64(0),
			"quarantined_items": float64(0), "item_errors": []any{}, "omitted_item_errors": float64(0),
			"last_error": nil,
		},
	}
	_, err = NewPersonalContextStatus(base)
	require.EqualError(t, err, "idle fetch run progress must be empty")

	// completed+failed > total 拒绝
	base["fetch_run_progress"] = map[string]any{
		"gh": map[string]any{
			"service_id": "gh", "run_state": "running", "progress_percent": float64(50),
			"total_items": float64(1), "completed_items": float64(1), "failed_items": float64(1),
			"quarantined_items": float64(0), "item_errors": []any{}, "omitted_item_errors": float64(0),
			"last_error": nil,
		},
	}
	_, err = NewPersonalContextStatus(base)
	require.EqualError(t, err, "fetch run progress counts are out of range")

	// copy_nested_state 补缺省字段
	base["fetch_run_progress"] = map[string]any{
		"gh": map[string]any{
			"service_id": "gh", "run_state": "idle", "progress_percent": float64(0),
			"total_items": float64(0), "completed_items": float64(0), "failed_items": float64(0),
			"last_error": nil,
		},
	}
	status, err = NewPersonalContextStatus(base)
	require.NoError(t, err)
	require.Equal(t, float64(0), status.FetchRunProgress["gh"]["quarantined_items"])
}

func TestPersonalContextStatus_LastError形状(t *testing.T) {
	base := map[string]any{
		"configured": true, "collection_enabled": true, "agent_use_enabled": false,
		"state": "FAILED", "pipeline_running": false, "pipeline_queue_size": float64(0),
		"fetch_service_states": map[string]any{}, "fetch_service_errors": map[string]any{},
		"fetch_run_progress": map[string]any{}, "context_root": "/tmp/x",
		"context_ready": false,
		"last_error":    map[string]any{"code": float64(1), "status": "s", "message": "m", "operation": "op"},
	}
	_, err := NewPersonalContextStatus(base)
	require.NoError(t, err)

	base["last_error"] = map[string]any{"code": float64(1), "status": "s"}
	_, err = NewPersonalContextStatus(base)
	require.EqualError(t, err, "last_error must contain only code, status, message, and operation")
}

// rawItemToMap RawChangeItem 还原为输入映射（测试辅助）。
func rawItemToMap(item *RawChangeItem) map[string]any {
	return map[string]any{
		"logical_id": item.LogicalID, "revision_id": item.RevisionID,
		"operation": item.Operation, "content": *item.Content,
		"original_ref": item.OriginalRef,
	}
}
