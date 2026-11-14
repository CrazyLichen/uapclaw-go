// session 模型选择的安全持久化。
// 对应 Python 代码：jiuwenswarm/common/model_selection_store.py:1-60。
package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	commonids "github.com/uapclaw/uapclaw-go/internal/common"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// sharingRetryAttempts 读侧重试预算。
// Python: model_selection_store.py:17 _SHARING_RETRY_ATTEMPTS = 100。
// metadata 写方经 tmp+rename 发布并重试 Windows sharing violation 至该预算；
// 落在窗口内的读者会在健康文件上得到 Errno 13。
const sharingRetryAttempts = 100

// ──────────────────────────── 非导出函数 ────────────────────────────

// metadataPath 校验并解析 session metadata 路径。
// Python: model_selection_store.py:20-28 _metadata_path。
// 步骤：①去空白 ②is_valid_session_id ③resolve 会话根 ④resolve 目标路径
// ⑤根必须是目标的父目录（防 session_id 逃逸）。
func metadataPath(sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if !commonids.IsValidSessionID(sessionID) {
		return "", fmt.Errorf("invalid session_id")
	}
	root, err := filepath.EvalSymlinks(workspace.AgentSessionsDir())
	if err != nil {
		// Python: get_agent_sessions_dir().resolve() 不要求目录存在；
		// Go EvalSymlinks 对不存在路径报错，此处退化为 Abs+Clean 等价语义。
		root, err = filepath.Abs(workspace.AgentSessionsDir())
		if err != nil {
			return "", err
		}
		root = filepath.Clean(root)
	}
	path := filepath.Join(root, sessionID, "metadata.json")
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(resolved, filepath.Base(path))
	} else {
		path = filepath.Clean(path)
	}
	// Python: model_selection_store.py:26-27 root not in path.parents → 逃逸
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("session_id escapes session root")
	}
	return path, nil
}

// readTextRideOutSharing 读文件并在 permission 错误上重试。
// Python: model_selection_store.py:31-40 _read_text_ride_out_sharing。
// 退避 min(0.001*attempt, 0.01) 秒，与 Python 逐字一致。
func readTextRideOutSharing(path string) ([]byte, error) {
	attempt := 0
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		// Python: except PermissionError（Errno 13）
		if !os.IsPermission(err) {
			return nil, err
		}
		attempt++
		if attempt >= sharingRetryAttempts {
			return nil, err
		}
		sleep := time.Duration(float64(attempt)) * time.Millisecond
		if sleep > 10*time.Millisecond {
			sleep = 10 * time.Millisecond
		}
		time.Sleep(sleep)
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSessionModelSelection 读会话保存的模型选择。
// Python: model_selection_store.py:43-51 get_session_model_selection。
// 无文件返回 nil（Python None）；读失败/解析失败包装 MODEL_GROUP_INVALID。
func GetSessionModelSelection(sessionID string) (*ModelSelection, error) {
	path, err := metadataPath(sessionID)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
		// Python: model_selection_store.py:45-46 not path.is_file() → None
		return nil, nil
	}
	raw, readErr := readTextRideOutSharing(path)
	if readErr != nil {
		return nil, NewModelSelectionError(ModelGroupInvalid, "cannot read the session's model selection", map[string]any{"cause": readErr.Error()})
	}
	var metadata map[string]any
	if jsonErr := json.Unmarshal(raw, &metadata); jsonErr != nil {
		return nil, NewModelSelectionError(ModelGroupInvalid, "cannot read the session's model selection", map[string]any{"cause": jsonErr.Error()})
	}
	value, _ := metadata["model_selection"].(map[string]any)
	if value == nil {
		return nil, nil
	}
	// Python: model_selection_store.py:49 ModelSelection.model_validate(raw)
	selection := &ModelSelection{
		Type:    asString(value["type"]),
		ID:      asString(value["id"]),
		RouteID: asString(value["route_id"]),
	}
	if err := selection.Validate(); err != nil {
		return nil, NewModelSelectionError(ModelGroupInvalid, "cannot read the session's model selection", map[string]any{"cause": err.Error()})
	}
	return selection, nil
}

// SetSessionModelSelection 写会话模型选择。
// Python: model_selection_store.py:54-60 set_session_model_selection。
// 步骤：①_metadata_path 校验（先于进入共享写方）②provider("session_metadata")
// 的 update_session_model_selection 合并写入（Go 侧为
// swarmsession.UpdateSessionModelSelection，对齐 session_metadata.py:801-810）。
func SetSessionModelSelection(sessionID string, selection *ModelSelection) error {
	if _, err := metadataPath(sessionID); err != nil {
		return err
	}
	var payload map[string]any
	if selection != nil {
		payload = map[string]any{
			"type":     selection.Type,
			"id":       selection.ID,
			"route_id": selection.RouteID,
		}
	}
	return swarmsession.UpdateSessionModelSelection(sessionID, payload)
}
