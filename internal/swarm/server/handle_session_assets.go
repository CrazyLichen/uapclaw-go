// 会话素材注册表（session assets）：每会话命名素材，支持 @name 引用。
// 注册表独立于其他特性：落在 <sessions>/<session_id>/session_assets.json，
// 由四个无状态 RPC 方法服务（对齐 Python jiuwenswarm/server/runtime/session_assets.py）。
package server

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionAssetsStateFile 状态文件名（对齐 _STATE_FILE）
	sessionAssetsStateFile = "session_assets.json"
	// sessionAssetsNameMax 名称最大长度（对齐 ASSET_NAME_MAX_LENGTH = 60）
	sessionAssetsNameMax = 60
	// sessionAssetsIDPrefix 素材 ID 前缀（对齐 ASSET_ID_PREFIX = "asset_"）
	sessionAssetsIDPrefix = "asset_"
	// sessionAssetsSourceUpload 来源：用户上传（ASSET_SOURCE_UPLOAD）
	sessionAssetsSourceUpload = "upload"
	// sessionAssetsSourceGenerated 来源：Agent 生成（ASSET_SOURCE_GENERATED）
	sessionAssetsSourceGenerated = "generated"
)

// sessionAssetsKinds 扩展名 → 素材类别（对齐 _KINDS；其余归 document）
var sessionAssetsKinds = map[string]map[string]bool{
	"image": {".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".bmp": true},
	"video": {".mp4": true, ".mov": true, ".webm": true, ".mkv": true, ".avi": true},
	"audio": {".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".ogg": true, ".flac": true},
}

// sessionAssetsForbiddenNameChars 名称禁止字符：@ 与换行（对齐 _FORBIDDEN_NAME_CHARS）
var sessionAssetsForbiddenNameChars = regexp.MustCompile(`[@\r\n\t]`)

// sessionAssetsSessionIDPattern 合法 session_id（对齐 _SESSION_ID）
var sessionAssetsSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionAssetError 注册表拒绝的请求；Code 机器可读（对齐 SessionAssetError）。
type SessionAssetError struct {
	// Code 机器可读错误码
	Code string
	// Message 人类可读信息
	Message string
}

// Error 实现 error 接口。
func (e *SessionAssetError) Error() string { return e.Message }

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionAssetsKindOf 按扩展名归类（对齐 _kind_of）。
func sessionAssetsKindOf(path string) string {
	suffix := strings.ToLower(filepath.Ext(path))
	for kind, suffixes := range sessionAssetsKinds {
		if suffixes[suffix] {
			return kind
		}
	}
	return "document"
}

// validateSessionAssetName 返回清洗后的名称，非法时回 SessionAssetError
// （对齐 validate_asset_name）。
func validateSessionAssetName(name string) (string, error) {
	cleaned := strings.Join(strings.Fields(name), " ")
	if cleaned == "" {
		return "", &SessionAssetError{Code: "INVALID_NAME", Message: "名称不能为空"}
	}
	if len([]rune(cleaned)) > sessionAssetsNameMax {
		return "", &SessionAssetError{Code: "INVALID_NAME",
			Message: fmt.Sprintf("名称不能超过 %d 个字符", sessionAssetsNameMax)}
	}
	if sessionAssetsForbiddenNameChars.MatchString(name) {
		return "", &SessionAssetError{Code: "INVALID_NAME", Message: "名称不能包含 @ 或换行"}
	}
	return cleaned, nil
}

// sessionAssetsBaseOverride 会话根目录测试注入点（对齐 Python 测试对
// get_agent_sessions_dir 的 monkeypatch；生产恒走 GetSessionsDir）。
var sessionAssetsBaseOverride string

// sessionAssetsSessionsBase 会话根目录。
func sessionAssetsSessionsBase() string {
	if sessionAssetsBaseOverride != "" {
		return sessionAssetsBaseOverride
	}
	return swarmsession.GetSessionsDir()
}

// sessionAssetsStateFileFor 解析并校验状态文件路径（对齐 SessionAssetManager._state_file）。
func sessionAssetsStateFileFor(sessionID string) (string, error) {
	sid := strings.TrimSpace(sessionID)
	if !sessionAssetsSessionIDPattern.MatchString(sid) {
		return "", &SessionAssetError{Code: "INVALID_PARAMS", Message: "缺少或非法的 session_id"}
	}
	base, err := filepath.Abs(sessionAssetsSessionsBase())
	if err != nil {
		return "", &SessionAssetError{Code: "INVALID_PARAMS", Message: "缺少或非法的 session_id"}
	}
	file, err := filepath.Abs(filepath.Join(base, sid, sessionAssetsStateFile))
	if err != nil || !strings.HasPrefix(file, base+string(filepath.Separator)) {
		return "", &SessionAssetError{Code: "INVALID_PARAMS", Message: "缺少或非法的 session_id"}
	}
	return file, nil
}

// loadSessionAssets 全量读入（对齐 SessionAssetManager._load）。
func loadSessionAssets(sessionID string) ([]map[string]any, error) {
	file, err := sessionAssetsStateFileFor(sessionID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Error(logger.ComponentAgentServer).Err(err).Str("file", file).
				Msg("[SessionAssets] failed to read state file, starting empty")
		}
		return []map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		logger.Error(logger.ComponentAgentServer).Err(err).Str("file", file).
			Msg("[SessionAssets] failed to parse state file, starting empty")
		return []map[string]any{}, nil
	}
	items, _ := doc["assets"].([]any)
	assets := []map[string]any{}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok && m["asset_id"] != nil {
			assets = append(assets, m)
		}
	}
	return assets, nil
}

// saveSessionAssets 原子写回（对齐 SessionAssetManager._save：tmp + replace）。
func saveSessionAssets(sessionID string, assets []map[string]any) error {
	file, err := sessionAssetsStateFileFor(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]any{"assets": assets}, "", "  ")
	if err != nil {
		return err
	}
	tmp := strings.TrimSuffix(file, ".json") + ".json.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// uniqueSessionAssetName 同名去重：追加 -N（对齐 SessionAssetManager._unique_name，
// 与 upload_storage 的 atomic_write_unique 同一 "-N" 约定，casefold 比较）。
func uniqueSessionAssetName(base string, taken map[string]bool) string {
	candidate, index := base, 1
	for taken[strings.ToLower(candidate)] {
		suffix := fmt.Sprintf("-%d", index)
		runes := []rune(base)
		cut := sessionAssetsNameMax - len(suffix)
		if cut > len(runes) {
			cut = len(runes)
		}
		candidate = string(runes[:cut]) + suffix
		index++
	}
	return candidate
}

// sessionAssetString 取 map 字符串字段（缺省空串）。
func sessionAssetString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// sessionAssetParamString params 值转字符串（缺省空串）。
func sessionAssetParamString(v any) string {
	s, _ := v.(string)
	return s
}

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleSessionAssetsList 会话素材列表（对齐 handle_session_assets_list）。
func (s *AgentServer) HandleSessionAssetsList(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	if params["session_id"] == nil || params["session_id"] == "" {
		params["session_id"] = sessionAssetParamString(request.SessionID)
	}
	assets, err := loadSessionAssets(sessionAssetParamString(params["session_id"]))
	if err != nil {
		return sessionAssetErrorResponse(request, err), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(map[string]any{"assets": assetsToAny(assets)}),
	), nil
}

// HandleSessionAssetsRegister 注册素材（对齐 handle_session_assets_register）。
func (s *AgentServer) HandleSessionAssetsRegister(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	if params["session_id"] == nil || params["session_id"] == "" {
		params["session_id"] = sessionAssetParamString(request.SessionID)
	}
	sessionID := sessionAssetParamString(params["session_id"])
	rawItems, _ := params["items"].([]any)
	if params["items"] == nil {
		return sessionAssetErrorResponse(request,
			&SessionAssetError{Code: "INVALID_PARAMS", Message: "缺少 items"}), nil
	}
	assets, err := loadSessionAssets(sessionID)
	if err != nil {
		return sessionAssetErrorResponse(request, err), nil
	}
	// 默认名必须对会话发出去过的所有名字唯一（含 name_history），
	// 重命名后的旧名不能再被无关文件占用（对齐 taken 集合构建）。
	taken := map[string]bool{}
	for _, a := range assets {
		taken[strings.ToLower(sessionAssetString(a, "name"))] = true
		if hist, ok := a["name_history"].([]any); ok {
			for _, old := range hist {
				oldStr, _ := old.(string)
				taken[strings.ToLower(oldStr)] = true
			}
		}
	}
	// 幂等索引（对齐 by_client_id / by_generated_path）：重复请求返回既有素材
	byClientID := map[string]map[string]any{}
	byGeneratedPath := map[string]map[string]any{}
	for _, a := range assets {
		if cid := sessionAssetString(a, "client_id"); cid != "" {
			byClientID[cid] = a
		}
		if sessionAssetString(a, "source") == sessionAssetsSourceGenerated {
			byGeneratedPath[sessionAssetString(a, "path")] = a
		}
	}
	created := make([]any, 0, len(rawItems))
	added := false
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			created = append(created, nil)
			continue
		}
		path := strings.TrimSpace(sessionAssetString(item, "path"))
		// 不做基于路径的去重：同一本地文件再次显式附加必须得到新的、独立命名的素材；
		// client_id（下方）才防的是意外重复到达
		if path == "" {
			created = append(created, nil)
			continue
		}
		if st, statErr := os.Stat(path); statErr != nil || st.IsDir() {
			created = append(created, nil)
			continue
		}
		source := sessionAssetString(item, "source")
		if source != sessionAssetsSourceUpload && source != sessionAssetsSourceGenerated {
			source = sessionAssetsSourceUpload
		}
		clientID := strings.TrimSpace(sessionAssetString(item, "client_id"))
		if len([]rune(clientID)) > sessionAssetsNameMax {
			clientID = string([]rune(clientID)[:sessionAssetsNameMax])
		}
		if source == sessionAssetsSourceUpload && clientID != "" {
			if existing, dup := byClientID[clientID]; dup {
				created = append(created, existing)
				continue
			}
		}
		if source == sessionAssetsSourceGenerated {
			if existing, dup := byGeneratedPath[path]; dup {
				created = append(created, existing)
				continue
			}
		}
		wanted := strings.TrimSpace(sessionAssetString(item, "name"))
		if wanted == "" {
			wanted = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		base, nameErr := validateSessionAssetName(wanted)
		if nameErr != nil {
			cleanedStem := sessionAssetsForbiddenNameChars.ReplaceAllString(
				strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "")
			cleanedStem = strings.TrimSpace(cleanedStem)
			if len([]rune(cleanedStem)) > sessionAssetsNameMax {
				cleanedStem = string([]rune(cleanedStem)[:sessionAssetsNameMax])
			}
			if cleanedStem == "" {
				cleanedStem = "asset"
			}
			base, nameErr = validateSessionAssetName(cleanedStem)
			if nameErr != nil {
				return sessionAssetErrorResponse(request, nameErr), nil
			}
		}
		name := uniqueSessionAssetName(base, taken)
		taken[strings.ToLower(name)] = true
		asset := map[string]any{
			"asset_id":   sessionAssetsIDPrefix + sessionAssetsRandomHex(),
			"name":       name,
			"kind":       sessionAssetsKindOf(path),
			"path":       path,
			"source":     source,
			"created_at": float64(time.Now().UnixNano()) / 1e9,
		}
		if clientID != "" {
			asset["client_id"] = clientID
		}
		assets = append(assets, asset)
		created = append(created, asset)
		added = true
		if clientID != "" {
			byClientID[clientID] = asset
		}
		if source == sessionAssetsSourceGenerated {
			byGeneratedPath[path] = asset
		}
	}
	if added {
		if err := saveSessionAssets(sessionAssetParamString(sessionID), assets); err != nil {
			return sessionAssetErrorResponse(request, err), nil
		}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(map[string]any{"assets": assetsToAny(assets), "created": created}),
	), nil
}

// HandleSessionAssetsRename 重命名素材（对齐 handle_session_assets_rename）。
func (s *AgentServer) HandleSessionAssetsRename(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	if params["session_id"] == nil || params["session_id"] == "" {
		params["session_id"] = sessionAssetParamString(request.SessionID)
	}
	assetID := strings.TrimSpace(sessionAssetParamString(params["asset_id"]))
	name, err := validateSessionAssetName(sessionAssetParamString(params["name"]))
	if err != nil {
		return sessionAssetErrorResponse(request, err), nil
	}
	assets, err := loadSessionAssets(sessionAssetParamString(params["session_id"]))
	if err != nil {
		return sessionAssetErrorResponse(request, err), nil
	}
	targetIdx := -1
	for i, a := range assets {
		if sessionAssetString(a, "asset_id") == assetID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return sessionAssetErrorResponse(request,
			&SessionAssetError{Code: "ASSET_NOT_FOUND", Message: fmt.Sprintf("未找到素材: %s", assetID)}), nil
	}
	for i, a := range assets {
		if i != targetIdx && strings.ToLower(sessionAssetString(a, "name")) == strings.ToLower(name) {
			return sessionAssetErrorResponse(request,
				&SessionAssetError{Code: "NAME_CONFLICT", Message: fmt.Sprintf("名称已被使用: %s", name)}), nil
		}
	}
	target := assets[targetIdx]
	oldName := sessionAssetString(target, "name")
	if oldName != "" && strings.ToLower(oldName) != strings.ToLower(name) {
		hist, _ := target["name_history"].([]any)
		target["name_history"] = append(hist, oldName)
	}
	target["name"] = name
	if err := saveSessionAssets(sessionAssetParamString(params["session_id"]), assets); err != nil {
		return sessionAssetErrorResponse(request, err), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(map[string]any{"assets": assetsToAny(assets)}),
	), nil
}

// HandleSessionAssetsRemove 删除素材条目（对齐 handle_session_assets_remove；
// 幂等，不触碰底层文件，只删注册表条目）。
func (s *AgentServer) HandleSessionAssetsRemove(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	if params["session_id"] == nil || params["session_id"] == "" {
		params["session_id"] = sessionAssetParamString(request.SessionID)
	}
	assetID := strings.TrimSpace(sessionAssetParamString(params["asset_id"]))
	assets, err := loadSessionAssets(sessionAssetParamString(params["session_id"]))
	if err != nil {
		return sessionAssetErrorResponse(request, err), nil
	}
	remaining := assets[:0:0]
	for _, a := range assets {
		if sessionAssetString(a, "asset_id") != assetID {
			remaining = append(remaining, a)
		}
	}
	if len(remaining) != len(assets) {
		if err := saveSessionAssets(sessionAssetParamString(params["session_id"]), remaining); err != nil {
			return sessionAssetErrorResponse(request, err), nil
		}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(map[string]any{"assets": assetsToAny(remaining)}),
	), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionAssetErrorResponse 失败响应（对齐 _handle_session_asset_request 的
// except 分支：payload {"error","message","code"}，code 仅在非空字符串时透出）。
func sessionAssetErrorResponse(request *schema.AgentRequest, err error) *schema.AgentResponse {
	payload := map[string]any{"error": err.Error(), "message": err.Error()}
	if se, ok := err.(*SessionAssetError); ok && strings.TrimSpace(se.Code) != "" {
		payload["code"] = strings.TrimSpace(se.Code)
	} else {
		logger.Error(logger.ComponentAgentServer).Err(err).
			Str("method", string(request.ReqMethod)).
			Msg("[SessionAssets] session.assets request failed")
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(payload),
	)
}

// sessionAssetsRandomHex 8 位随机 hex（对齐 secrets.token_hex(4)）。
func sessionAssetsRandomHex() string {
	buf := make([]byte, 4)
	if _, err := crand.Read(buf); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(buf)
}

// assetsToAny 转 []any 供 payload 承载。
func assetsToAny(assets []map[string]any) []any {
	out := make([]any, 0, len(assets))
	for _, a := range assets {
		out = append(out, a)
	}
	return out
}
