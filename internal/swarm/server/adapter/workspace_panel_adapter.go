package adapter

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleWorkspacePanel workspace 面板四方法分发。
// Python: workspace_file_adapter.py:199-238 _handle_workspace（一比一）：
// service 构造于多租户用户工作区根；单机形态根 = WorkspaceDir()
// （对齐 get_multi_tenant_user_workspace_dir 个人版分支 = get_user_workspace_dir()）。
// quota 快照层不移植（D-B2-4），usage 返回单机 du 快照。
func handleWorkspacePanel(method schema.ReqMethod, params map[string]any) (map[string]any, bool, string, error) {
	service := NewWorkspaceService(path.WorkspaceDir())
	switch method {
	case schema.ReqMethodWorkspaceTree:
		// Python :211-212 workspace.tree → service.list_tree(relative_path)
		payload, werr := service.ListTree(stringParam(params, "relative_path"))
		return workspacePanelResult(payload, werr)
	case schema.ReqMethodWorkspacePreview:
		// Python :213-216 preview_file(relative_path, max_bytes=min(int(max_bytes or 512KiB), 1MiB))
		maxBytes := any(nil)
		if v, ok := params["max_bytes"]; ok && v != nil {
			maxBytes = v
		}
		payload, werr := service.PreviewFile(stringParam(params, "relative_path"), maxBytes)
		return workspacePanelResult(payload, werr)
	case schema.ReqMethodWorkspaceEntriesDelete:
		// Python :217-223 entries_delete：relative_paths 必须是 list
		paths, isList := params["relative_paths"].([]any)
		if !isList {
			return nil, false, "BAD_REQUEST", errInvalidWorkspaceRequest
		}
		return service.DeleteEntries(paths), true, "", nil
	case schema.ReqMethodWorkspaceUsage:
		// Python :224-235 usage → resolve_effective_quota snapshot_to_dict；
		// quota 层不移植（D-B2-4），返回单机 du 快照（quota=null、三元组透传）。
		used := service.MeasureUsedBytes()
		return map[string]any{
			"used_bytes": used,
			"quota":      nil,
			"user_id":    params["user_id"],
			"group_id":   params["group_id"],
			"bot_id":     params["bot_id"],
		}, true, "", nil
	default:
		return nil, false, "BAD_REQUEST", errUnsupportedWorkspaceMethod(method)
	}
}

// workspacePanelResult 统一 WorkspaceError → (nil, false, code, error)。
// Python: :236-240 except WorkspaceError → build_error_response(request, exc.message, code=exc.code)。
func workspacePanelResult(payload map[string]any, werr *WorkspaceError) (map[string]any, bool, string, error) {
	if werr != nil {
		return nil, false, werr.Code, werr
	}
	return payload, true, "", nil
}

// stringParam 取 string 参数（缺省空串）。
func stringParam(params map[string]any, key string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return ""
}

// errInvalidWorkspaceRequest 非法 workspace 请求错误。
// Python: :240-242 except (ValueError, TypeError) → "invalid workspace request"。
var errInvalidWorkspaceRequest = fmt.Errorf("invalid workspace request")

// errUnsupportedWorkspaceMethod 未支持方法错误。
func errUnsupportedWorkspaceMethod(method schema.ReqMethod) error {
	return fmt.Errorf("unsupported method: %s", method)
}
