// SendFileToolkit 发送文件到用户工具（10.6.24b + 2026-11-16 全量对齐批）。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/send_file_to_user.py（861 行）：
// 每请求绑定路由三元组（request_id/session_id/channel_id）+ 运行时上下文
// （user_id/project_dir/team_workspace_root/require_execution_authorization/asset_owner），
// 经 UpdateRuntimeContext 刷新（不重建工具实例）；
// push/history/授权函数经包级注入缝（Python 为 lazy import，Go 编译期成环改启动接线，D-x3）。

package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	verifieddownloadassets "github.com/uapclaw/uapclaw-go/internal/swarm/common/verified_download_assets"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SendFileRuntimeContext 构造/刷新期运行时上下文
// （Python __init__ L161-172 / update_runtime_context L206-217 的 kwargs 集合）。
type SendFileRuntimeContext struct {
	// Metadata 请求元数据（E2A channel_context 映射结果，send_push 透传）
	Metadata map[string]any
	// UserID Web 下载 URL 的用户路由（Python user_id）
	UserID string
	// ProjectDir 当前用户项目目录（团队交付物物化目标，Python project_dir）
	ProjectDir string
	// TeamWorkspaceRoot 团队工作区根（Python team_workspace_root）
	TeamWorkspaceRoot string
	// RequireExecutionAuthorization 要求宿主能力授权（Python require_execution_authorization；
	// UpdateRuntimeContext 期 nil=不改，对齐 Python L228-231 的 None 语义）
	RequireExecutionAuthorization *bool
	// AssetOwner 受管资产宿主（Python asset_owner kwarg；nil → 授权路径回退共享单例）
	AssetOwner *verifieddownloadassets.VerifiedDownloadAssetOwner
}

// sendFileRuntimeEnvelope 运行时信封：可变宿主状态在第一次可挂起点前的不可变快照。
// 后半段执行全部读信封，不碰 self.*——防止挂起期间 update_runtime_context 换请求导致错投
// （Python _SendFileRuntimeEnvelope L91-103 + _snapshot_runtime_envelope L421-450）。
type sendFileRuntimeEnvelope struct {
	routingRequestID              string
	sessionID                     string
	channelID                     string
	userID                        string
	metadata                      map[string]any // 深拷贝（D-x2：拷贝语义替代 MappingProxyType）
	projectDir                    string
	teamWorkspaceRoot             string
	requireExecutionAuthorization bool
	assetOwner                    *verifieddownloadassets.VerifiedDownloadAssetOwner
}

// SendFileToolkit 发送文件到用户的工具包。
// 绑定请求级路由三元组 + 运行时上下文；每请求经 UpdateRuntimeContext 刷新（不重建工具实例）。
// Python: SendFileToolkit (send_file_to_user.py L158-197)
type SendFileToolkit struct {
	// requestID 请求标识（消息路由）
	requestID string
	// sessionID 会话标识
	sessionID string
	// channelID 渠道标识
	channelID string
	// requestMetadata 请求元数据（send_push 透传）
	requestMetadata map[string]any
	// userID Web 下载 URL 用户路由
	userID string
	// projectDir 当前用户项目目录（团队交付物物化）
	projectDir string
	// teamWorkspaceRoot 团队工作区根
	teamWorkspaceRoot string
	// requireExecutionAuthorization 要求执行授权
	requireExecutionAuthorization bool
	// assetOwner 受管资产宿主（nil → 授权路径回退共享单例）
	assetOwner *verifieddownloadassets.VerifiedDownloadAssetOwner
	// mu 保护并发 UpdateRuntimeContext 与 SendFile 读取
	mu sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sendFileSilentMessage 静默模式提示串（Python L388-390 逐字复制，协议串不本地化）
	sendFileSilentMessage = "静默模式禁止 send_file_to_user；" +
		"请直接用文件写入工具生成 Skill 目录，不要投递文件给用户。"

	// sendFileVerifiedAssetTTLSeconds 受管资产 TTL（Python _VERIFIED_ASSET_TTL_SECONDS L45）
	sendFileVerifiedAssetTTLSeconds = 600
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sendFileSentFilePathsMu 会话级去重锁（Python 单线程共享 dict，Go 加锁——asyncio→Go 并发偏差沿用）
	sendFileSentFilePathsMu sync.Mutex

	// sentFilePathsBySession 会话级去重注册表。
	// Python: _SENT_FILE_PATHS_BY_SESSION (send_file_to_user.py L109)；
	// key 为 session id（tenant 维度恒空，偏差 D-x1）。
	// 压缩可能丢弃历史工具结果导致 Agent 重复调用同一路径，请求级去重挡不住跨轮重复。
	sentFilePathsBySession = map[string]map[string]struct{}{}

	// sendFilePushFunc E2A 推送函数（agent_server 启动注入）。
	// Python send_runtime_push 返回 bool（False=无活跃 push 宿主）；Go 注入函数返回 error 语义等价。
	sendFilePushFunc func(ctx context.Context, msg map[string]any) error

	// sendFileHistoryFunc 历史记录函数（agent_server 启动注入，
	// 签名与 session.AppendHistoryRecord 一致）。
	sendFileHistoryFunc func(sessionID, requestID, channelID, role, content string,
		timestamp float64, eventType string, extra map[string]any,
		channelMetadata map[string]any, mode string)

	// sendFileProjectDirResolver session metadata 回退解析 project_dir
	// （Python _resolve_project_dir L246-249 lazy import session_metadata；Go 成环改注入缝，D-x3）。
	sendFileProjectDirResolver func(sessionID string) string

	// sendFileGrantConsumer 执行授权消费缝（D-x3：permissions 包导入 tools 成环，
	// 由 permissions 侧启动接线注入；返回授权项 resolved_path 列表）。
	sendFileGrantConsumer func(ctx context.Context, requestedPaths, targetChannels []string) ([]string, error)

	// downloadInfoBuilder 下载信息构造（测试注入点，默认 BuildFileDownloadInfo）
	downloadInfoBuilder = defaultDownloadInfoBuilder
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSendFilePushFunc 注入 E2A 推送函数（agent_server 启动调用）。
func SetSendFilePushFunc(fn func(ctx context.Context, msg map[string]any) error) {
	sendFilePushFunc = fn
}

// SetSendFileHistoryFunc 注入历史记录函数（agent_server 启动调用）。
func SetSendFileHistoryFunc(fn func(sessionID, requestID, channelID, role, content string,
	timestamp float64, eventType string, extra map[string]any,
	channelMetadata map[string]any, mode string)) {
	sendFileHistoryFunc = fn
}

// SetSendFileProjectDirResolver 注入 session metadata project_dir 回退解析（D-x3 注入缝）。
func SetSendFileProjectDirResolver(fn func(sessionID string) string) {
	sendFileProjectDirResolver = fn
}

// SetSendFileGrantConsumer 注入执行授权消费函数（permissions 侧 init 接线，D-x3）。
func SetSendFileGrantConsumer(fn func(ctx context.Context, requestedPaths, targetChannels []string) ([]string, error)) {
	sendFileGrantConsumer = fn
}

// NewSendFileToolkit 构造工具包。
// Python: SendFileToolkit.__init__ (send_file_to_user.py L161-197)
func NewSendFileToolkit(requestID, sessionID, channelID string, ctx SendFileRuntimeContext) *SendFileToolkit {
	md := copySendFileMetadata(ctx.Metadata)
	logger.Debug(logComponent).
		Str("request_id", requestID).
		Str("session_id", sessionID).
		Str("channel_id", channelID).
		Bool("has_metadata", md != nil).
		Msg("[SendFileToolkit] init")
	return &SendFileToolkit{
		requestID:                     requestID,
		sessionID:                     sessionID,
		channelID:                     channelID,
		requestMetadata:               md,
		userID:                        ctx.UserID,
		projectDir:                    resolveSendFileAbsPath(ctx.ProjectDir),
		teamWorkspaceRoot:             resolveSendFileAbsPath(ctx.TeamWorkspaceRoot),
		requireExecutionAuthorization: ctx.RequireExecutionAuthorization != nil && *ctx.RequireExecutionAuthorization,
		assetOwner:                    ctx.AssetOwner,
	}
}

// UpdateRuntimeContext 刷新请求级上下文（不重建工具）。
// RequireExecutionAuthorization 为 nil 时不改（Python L228-231）。
// Python: update_runtime_context (send_file_to_user.py L206-238)
func (tk *SendFileToolkit) UpdateRuntimeContext(requestID, sessionID, channelID string, ctx SendFileRuntimeContext) {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	tk.requestID = requestID
	tk.sessionID = sessionID
	tk.channelID = channelID
	tk.requestMetadata = copySendFileMetadata(ctx.Metadata)
	tk.userID = ctx.UserID
	tk.projectDir = resolveSendFileAbsPath(ctx.ProjectDir)
	tk.teamWorkspaceRoot = resolveSendFileAbsPath(ctx.TeamWorkspaceRoot)
	if ctx.RequireExecutionAuthorization != nil {
		tk.requireExecutionAuthorization = *ctx.RequireExecutionAuthorization
	}
	logger.Debug(logComponent).
		Str("request_id", requestID).
		Str("session_id", sessionID).
		Str("channel_id", channelID).
		Bool("has_metadata", tk.requestMetadata != nil).
		Msg("[SendFileToolkit] update_runtime_context")
}

// SendFile 发送文件到用户（核心执行）。
// targetChannels 为可选投递目标（Python target_channels：channel id 或 team 人类席位名；
// 省略时 Gateway 自动路由）。
// Python: send_file (send_file_to_user.py L358-419)
// 返回给 LLM 的中文结果串；error 恒为 nil（Python 语义：异常兜底返回描述串）。
func (tk *SendFileToolkit) SendFile(ctx context.Context, absFilePathList string, targetChannels ...string) (string, error) {
	// 0. skills.rebuild / 知识转 Skill 静默 Agent：禁止 push / chat.file，避免保存卡片污染 UI
	// （Python L379-390）
	tk.mu.Lock()
	md := tk.requestMetadata
	tk.mu.Unlock()
	if md != nil {
		if silent, _ := md["skills_rebuild_silent"].(bool); silent {
			logger.Info(logComponent).
				Str("session_id", tk.sessionID).
				Msg("[SendFileToolkit] silent mode skips send_file")
			return sendFileSilentMessage, nil
		}
		if silent, _ := md["skills_create_from_knowledge_silent"].(bool); silent {
			logger.Info(logComponent).
				Str("session_id", tk.sessionID).
				Msg("[SendFileToolkit] silent mode skips send_file")
			return sendFileSilentMessage, nil
		}
	}

	// 1. 冻结可变宿主状态（Python L395 _snapshot_runtime_envelope）
	envelope := tk.snapshotRuntimeEnvelope()

	// 2. 核心执行 + 异常兜底（Python L396-412）
	result, err := tk.sendFileWithEnvelope(ctx, envelope, absFilePathList, targetChannels)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", envelope.sessionID).
			Msg("[SendFileToolkit] send_file failed")
		return fmt.Sprintf("提交文件失败: %s", err.Error()), nil
	}

	// 3. finally 清授权（Python L413-419）——偏差 D-x4：Go ctx 不可变，
	// clear_send_file_execution_grant 语义由 ctx 生命周期（任务结束即丢弃）承担，此处无需操作。
	return result, nil
}

// ClearSentFilesForSession 会话 adapter 清理时丢弃去重状态（包级导出供清理点调用）。
// Python: clear_sent_files_for_session (send_file_to_user.py L152-155)
func ClearSentFilesForSession(sessionID string) {
	sendFileSentFilePathsMu.Lock()
	defer sendFileSentFilePathsMu.Unlock()
	delete(sentFilePathsBySession, sentFilesKey(sessionID))
}

// GetTools 返回注册用工具列表（单工具 send_file_to_user）。
// description 含 target_channels 文案（Python L800-861 逐字复制）。
func (tk *SendFileToolkit) GetTools() []tool.Tool {
	card := tool.NewToolCard(
		"send_file_to_user",
		"【文件发送工具】当需要将生成的文件、导出的数据、创建的文档等发送给用户时使用此工具。"+
			"使用场景包括：用户请求导出/下载文件、任务完成后需要交付文件、生成报告/文档后发送给用户。"+
			"参数格式：abs_file_path_list 接受单个路径字符串或路径数组，路径必须是绝对路径。"+
			"示例：'/tmp/report.pdf' 或 ['/tmp/file1.csv', '/tmp/file2.xlsx']。"+
			"target_channels 可选：指定文件投递目标，每项可以是 channel id（如 'web'）"+
			"或 team 人类席位名（如 'human-player-1'）。"+
			"省略时默认投给最近发起请求的人类成员（按 session 记录的发起者）；web 发起或无人类成员时投 web。"+
			"多 app 场景定向到指定 feishu 用户时，传入该用户的 member_name（不会误投其它 app）；"+
			"跨端投递（如把文件发给飞书用户、或发给 web）时传入对应 member_name 或 'web'。",
		[]*schema.Param{
			{
				Name: "abs_file_path_list",
				Description: "要发送的文件绝对路径。" +
					"可以是单个路径字符串如 '/path/to/file.pdf'，" +
					"或 JSON 数组字符串如 '[\"/path/file1.csv\", \"/path/file2.xlsx\"]'。" +
					"支持任意文件类型（pdf、xlsx、docx、png、zip等）。",
				Type:     schema.ParamTypeString,
				Required: true,
			},
			{
				Name: "target_channels",
				Description: "可选：文件投递目标列表。每项可为 channel id（如 'web'）" +
					"或 team 人类席位名（如 'human-player-1'）。" +
					"省略时默认投给最近发起请求的人类成员；web 发起或无人类成员时投 web。" +
					"定向到指定 feishu 用户传其 member_name；跨端投递传对应 member_name 或 'web'。",
				Type: schema.ParamTypeArray,
				Items: &schema.Param{
					Name: "target_channel",
					Type: schema.ParamTypeString,
				},
			},
		},
		nil,
	)

	invokeFn := func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		raw := ""
		if v, ok := inputs["abs_file_path_list"]; ok {
			raw = fmt.Sprint(v)
		}
		// target_channels 允许 JSON 数组串或单个字符串（Python list|str|None）
		var targetChannels []string
		if v, ok := inputs["target_channels"]; ok && v != nil {
			targetChannels = parseSendFileTargetChannelsInput(v)
		}
		result, err := tk.SendFile(ctx, raw, targetChannels...)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}

	fn, err := tool.NewMapFunction(card, invokeFn, nil)
	if err != nil {
		// 卡片为字面量常量，校验结构性不可能失败，防御性返回空
		logger.Warn(logComponent).Err(err).
			Msg("[SendFileToolkit] build map function failed")
		return nil
	}
	return []tool.Tool{fn}
}

// LooksLikeSkillPackage 轻量判断交付文件是否为 Skill 包（不解压全量内容）。
// - .skill/.skill.zip：按扩展名视为技能包
// - .zip：中央目录含根级或一层目录下的 SKILL.md 则视为技能包
// - 探测失败：返回 false（保守当普通产物）
// Python: looks_like_skill_package (send_file_to_user.py L48-88)
func LooksLikeSkillPackage(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(name, ".skill.zip") || strings.HasSuffix(name, ".skill") {
		return true
	}
	if !strings.HasSuffix(name, ".zip") {
		return false
	}
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		// Python L81-87: 探测失败 warn + 保守 False
		logger.Warn(logComponent).Str("path", path).
			Msg("[SendFileToolkit] skill package probe failed")
		return false
	}
	defer func() { _ = zr.Close() }()
	for _, entry := range zr.File {
		parts := splitZipEntryParts(entry.Name)
		if len(parts) == 0 || strings.ToLower(parts[len(parts)-1]) != "skill.md" {
			continue
		}
		// 根级 SKILL.md 或 skill_name/SKILL.md
		if len(parts) <= 2 {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// snapshotRuntimeEnvelope 冻结可变宿主状态。
// Python: _snapshot_runtime_envelope (send_file_to_user.py L421-450)
// enterprise 分支不移植（偏差 D-x1：asset_owner 无 enterprise 剥离分支）。
func (tk *SendFileToolkit) snapshotRuntimeEnvelope() *sendFileRuntimeEnvelope {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	projectDir := tk.projectDir
	if projectDir == "" && tk.sessionID != "" {
		// Python L240-264: _resolve_project_dir session metadata 回退（副作用仅一次）
		projectDir = resolveSendFileProjectDir(tk.sessionID)
		if projectDir != "" {
			tk.projectDir = resolveSendFileAbsPath(projectDir)
		}
	}
	return &sendFileRuntimeEnvelope{
		routingRequestID:              tk.requestID,
		sessionID:                     tk.sessionID,
		channelID:                     tk.channelID,
		userID:                        tk.userID,
		metadata:                      deepCopySendFileMetadata(tk.requestMetadata),
		projectDir:                    projectDir,
		teamWorkspaceRoot:             tk.teamWorkspaceRoot,
		requireExecutionAuthorization: tk.requireExecutionAuthorization,
		assetOwner:                    tk.assetOwner,
	}
}

// sendFileWithEnvelope 信封期核心执行。
// Python: _send_file_with_envelope (send_file_to_user.py L452-657) 步骤一比一。
func (tk *SendFileToolkit) sendFileWithEnvelope(
	ctx context.Context,
	envelope *sendFileRuntimeEnvelope,
	absFilePathList string,
	targetChannels []string,
) (string, error) {
	// ① target_channels 归一（Python L459-467）
	targetChannelList := normalizeSendFileTargetChannels(targetChannels)
	if len(targetChannelList) > 0 {
		logger.Info(logComponent).
			Str("target_channels", strings.Join(targetChannelList, ",")).
			Str("session_id", envelope.sessionID).
			Msg("[SendFileToolkit] send_file target_channels")
	}

	// ② 请求路径归一（Python L468 _normalize_requested_paths → permissions.normalize_send_file_paths）
	requestedPaths := normalizeSendFilePaths(absFilePathList)

	// ③ 授权分支 / 普通分支分流（Python L469-490）
	var validFiles, missingFiles []string
	if envelope.requireExecutionAuthorization {
		authorizationPaths, authErr := consumeSendFileExecutionGrantPaths(ctx, requestedPaths, targetChannels)
		if authErr != nil {
			return "", authErr
		}
		validFiles = authorizationPaths
		missingFiles = nil
	} else {
		for _, fp := range requestedPaths {
			if isSendFileRegular(fp) {
				validFiles = append(validFiles, fp)
			} else {
				missingFiles = append(missingFiles, fp)
				logger.Warn(logComponent).Str("file", fp).
					Msg("[SendFileToolkit] file not found")
			}
		}
	}

	// ④ 团队交付物物化（Python L492-516）
	sourceFiles := append([]string(nil), validFiles...)
	materializedFiles := make([]string, 0, len(validFiles))
	for _, fp := range validFiles {
		dst, err := materializeTeamDeliverableFromRoots(fp, envelope.projectDir, envelope.teamWorkspaceRoot)
		if err != nil {
			// Python L503-511: OSError → error 串（非异常上抛）
			logger.Error(logComponent).Err(err).Str("file", fp).
				Msg("[SendFileToolkit] 团队交付文件复制到项目目录失败")
			return fmt.Sprintf("发送文件失败：无法将团队交付文件写入当前项目目录\n  - %s: %s", fp, err.Error()), nil
		}
		materializedFiles = append(materializedFiles, dst)
	}
	validFiles = materializedFiles
	copiedToProject := false
	for i := range sourceFiles {
		if filepath.Clean(sourceFiles[i]) != filepath.Clean(validFiles[i]) {
			copiedToProject = true
			break
		}
	}
	// 授权模式物化路径碰撞检测（Python L517-523）
	if envelope.requireExecutionAuthorization {
		authorizedDelivered := make(map[string]struct{}, len(validFiles))
		for _, delivered := range validFiles {
			normalized := normalizeSentFilePath(delivered)
			if _, dup := authorizedDelivered[normalized]; dup {
				return "", fmt.Errorf("send_file_materialized_path_collision")
			}
			authorizedDelivered[normalized] = struct{}{}
		}
	}

	// ⑤ 全缺失早退（Python L525-529）
	if len(validFiles) == 0 {
		msgParts := []string{"发送文件失败：所有文件均不存在"}
		for _, mf := range missingFiles {
			msgParts = append(msgParts, "  - "+mf)
		}
		return strings.Join(msgParts, "\n"), nil
	}

	// ⑥ 会话级去重分流（Python L531-534，授权与否均执行——Python 无授权跳过分支）
	var skippedFiles []string
	validFiles, skippedFiles = partitionSentFiles(envelope.sessionID, validFiles)
	if len(validFiles) == 0 {
		// 全部重复（Python L535-553）
		msgParts := []string{}
		if len(skippedFiles) > 0 {
			msgParts = append(msgParts, "文件已在本次会话发送过，跳过重复投递：")
			for _, sf := range skippedFiles {
				msgParts = append(msgParts, "  - "+sf)
			}
		}
		if len(missingFiles) > 0 {
			msgParts = append(msgParts, "以下文件不存在，未发送：")
			for _, mf := range missingFiles {
				msgParts = append(msgParts, "  - "+mf)
			}
		}
		if len(msgParts) == 0 {
			msgParts = append(msgParts, "没有可发送的文件")
		}
		logger.Info(logComponent).
			Str("session_id", envelope.sessionID).
			Int("skipped", len(skippedFiles)).
			Int("missing", len(missingFiles)).
			Msg("[SendFileToolkit] skip duplicate send")
		return strings.Join(msgParts, "\n"), nil
	}

	logger.Info(logComponent).
		Str("session_id", envelope.sessionID).
		Int("valid", len(validFiles)).
		Int("missing", len(missingFiles)).
		Int("skipped", len(skippedFiles)).
		Msg("[SendFileToolkit] send_file start")

	// ⑦ 授权资产 stage（Python L563-585；enterprise 分支不移植，D-x1）
	ownedAssets := make([]*verifieddownloadassets.VerifiedDownloadAsset, 0)
	assetsByPath := make(map[string]*verifieddownloadassets.VerifiedDownloadAsset)
	exposureStarted := false
	defer func() {
		// finally：未暴露的 staged 资产 revoke（Python L650-657）
		if len(ownedAssets) > 0 && !exposureStarted && envelope.assetOwner != nil {
			for _, asset := range ownedAssets {
				envelope.assetOwner.Revoke(asset)
			}
		}
	}()
	if envelope.requireExecutionAuthorization {
		if envelope.assetOwner == nil {
			return "", fmt.Errorf("send_file_asset_owner_missing")
		}
		expiresAt := float64(time.Now().Unix() + sendFileVerifiedAssetTTLSeconds)
		for _, fp := range validFiles {
			asset, err := envelope.assetOwner.Stage(fp, verifieddownloadassets.StageOptions{
				FileName:  filepath.Base(fp),
				ExpiresAt: expiresAt,
			})
			if err != nil {
				return "", err
			}
			ownedAssets = append(ownedAssets, asset)
			assetsByPath[normalizeSentFilePath(fp)] = asset
		}
	}

	// ⑧ payload 构造（Python L586-590 → _build_files_payload L683-774）
	filesPayload := buildFilesPayload(envelope, validFiles, assetsByPath)

	// ⑨ push 消息构造（Python L591-595 → _build_push_message L777-798）
	msg := buildPushMessage(envelope, filesPayload, targetChannelList)

	// ⑩ push（commit 点，Python L597-605）
	// 注：sendFileHistoryFunc/sendFilePushFunc 为 nil 时 Warn 跳过（既有偏差沿用，
	// Python send_runtime_push False → RuntimeError；Go 测试与单机路径存在无宿主场景）
	exposureStarted = true
	if sendFilePushFunc != nil {
		if err := sendFilePushFunc(ctx, msg); err != nil {
			exposureStarted = false
			return "", err
		}
	} else {
		logger.Warn(logComponent).
			Msg("[SendFileToolkit] push func not injected, skip")
	}
	// push 成功 → commit 资产（Python L606-615）
	if envelope.assetOwner != nil {
		for _, asset := range ownedAssets {
			if err := envelope.assetOwner.Commit(asset); err != nil {
				// Python L610-615: commit 失败仅记日志（staged TTL 归属保留）
				logger.Error(logComponent).Err(err).
					Str("asset_id", asset.AssetID).
					Msg("[SendFileToolkit] asset commit failed; staged TTL ownership retained")
			}
		}
	}
	// push 成功 → 标记已发送（Python _mark_files_sent L616，授权与否均执行）
	markFilesSent(envelope.sessionID, validFiles)

	// ⑪ history（Python L617-635；注入函数为 nil 时 Warn 跳过；失败不阻断结果串）
	if sendFileHistoryFunc != nil {
		sendFileHistoryFunc(
			envelope.sessionID, envelope.routingRequestID, envelope.channelID,
			"assistant", "", currentSendFileTimestamp(),
			"chat.file", map[string]any{"files": filesPayload},
			nil, "",
		)
	} else {
		logger.Warn(logComponent).
			Msg("[SendFileToolkit] history func not injected, skip")
	}

	// ⑫ 结果串（Python L636-649）
	resultParts := []string{fmt.Sprintf("成功发送 %d 个文件", len(validFiles))}
	if copiedToProject {
		resultParts = append(resultParts, "最终交付文件已位于当前项目目录：")
		for _, delivered := range validFiles {
			resultParts = append(resultParts, "  - "+delivered)
		}
	}
	if len(skippedFiles) > 0 {
		resultParts = append(resultParts, "以下文件已在本次会话发送过，已跳过：")
		for _, sf := range skippedFiles {
			resultParts = append(resultParts, "  - "+sf)
		}
	}
	if len(missingFiles) > 0 {
		resultParts = append(resultParts, "以下文件不存在，未发送：")
		for _, mf := range missingFiles {
			resultParts = append(resultParts, "  - "+mf)
		}
	}
	return strings.Join(resultParts, "\n"), nil
}

// buildFilesPayload 构造下载信息载荷。
// Python: _build_files_payload (send_file_to_user.py L683-774)
// enterprise obs_file_delivery 分支不移植（偏差 D-x1）。
func buildFilesPayload(
	envelope *sendFileRuntimeEnvelope,
	validFiles []string,
	assetsByPath map[string]*verifieddownloadassets.VerifiedDownloadAsset,
) []map[string]any {
	filesPayload := make([]map[string]any, 0, len(validFiles))
	// 授权分支（Python L706-735）：path=sealed_path，probe 缺源时用 sealed_path
	if envelope.requireExecutionAuthorization {
		for _, fp := range validFiles {
			asset := assetsByPath[normalizeSentFilePath(fp)]
			baseName := filepath.Base(fp)
			info := BuildVerifiedAssetDownloadInfo(asset, baseName, envelope.sessionID, envelope.userID)
			probePath := fp
			if !isSendFileRegular(fp) {
				probePath = asset.SealedPath
			}
			filesPayload = append(filesPayload, map[string]any{
				"path":             filepath.ToSlash(asset.SealedPath),
				"name":             baseName,
				"size":             info["size"],
				"mime_type":        info["mime_type"],
				"download_url":     info["download_url"],
				"download_token":   info["download_token"],
				"is_skill_package": LooksLikeSkillPackage(probePath),
			})
		}
		return filesPayload
	}

	// 普通分支（Python L737-760）：整块失败回退基础模式
	filesPayload, downloadErr := buildSendFilePayload(validFiles, envelope.sessionID, envelope.userID)
	if downloadErr != nil {
		logger.Warn(logComponent).Err(downloadErr).
			Msg("[SendFileToolkit] 生成下载信息失败，回退到基础模式")
		// Python L766-773：基础模式含 is_skill_package
		payload := make([]map[string]any, 0, len(validFiles))
		for _, fp := range validFiles {
			payload = append(payload, map[string]any{
				"path":             fp,
				"name":             filepath.Base(fp),
				"is_skill_package": LooksLikeSkillPackage(fp),
			})
		}
		return payload
	}
	return filesPayload
}

// buildPushMessage 手工构造 push 消息（Python _build_push_message L777-798）。
func buildPushMessage(
	envelope *sendFileRuntimeEnvelope,
	filesPayload []map[string]any,
	targetChannels []string,
) map[string]any {
	msg := map[string]any{
		"request_id": envelope.routingRequestID,
		"channel_id": envelope.channelID,
		"session_id": envelope.sessionID,
		"payload": map[string]any{
			"event_type": "chat.file",
			"files":      filesPayload,
		},
		"is_complete": false,
	}
	mergedMeta := deepCopySendFileMetadata(envelope.metadata)
	if mergedMeta == nil && len(targetChannels) > 0 {
		mergedMeta = map[string]any{}
	}
	if len(targetChannels) > 0 {
		targets := make([]string, len(targetChannels))
		copy(targets, targetChannels)
		mergedMeta["send_file_targets"] = targets
	}
	if mergedMeta != nil {
		msg["metadata"] = mergedMeta
	}
	return msg
}

// parseSendFileInput 宽容解析入参（Python L88-101 语义由 permissions.normalize_send_file_paths
// 承担；此处为未注入授权缝时的本地等价实现）。
// JSON 数组→展开；JSON 单字符串→包裹；解析失败/其他类型→包裹原文。
func parseSendFileInput(raw string) []string {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		switch v := parsed.(type) {
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				out = append(out, fmt.Sprint(item))
			}
			return out
		case string:
			return []string{v}
		default:
			// Python: 非 list 非 str → [原文]
			return []string{raw}
		}
	}
	// 解析失败 → [原文]
	return []string{raw}
}

// normalizeSendFilePaths 请求路径归一（Python _normalize_requested_paths L659-665）。
// 默认本地等价实现；授权缝注入后由 permissions.NormalizeSendFilePaths 承担。
func normalizeSendFilePaths(raw string) []string {
	return parseSendFileInput(raw)
}

// normalizeSendFileTargetChannels target_channels 归一（Python _normalize_target_channels
// L348-356 → permissions.normalize_send_file_target_channels）。默认本地等价实现。
func normalizeSendFileTargetChannels(value []string) []string {
	seen := make(map[string]struct{}, len(value))
	out := make([]string, 0, len(value))
	for _, item := range value {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// consumeSendFileExecutionGrantPaths 消费执行授权返回 resolved 路径
// （Python _consume_execution_authorization L667-680 → consume_send_file_execution_grant）。
// 注入缝（D-x3）：permissions 包导入 tools（成环），由 permissions 侧启动接线注入。
// 未注入时返回错误（授权模式必须宿主支持）。
func consumeSendFileExecutionGrantPaths(ctx context.Context, requestedPaths []string, targetChannels []string) ([]string, error) {
	if sendFileGrantConsumer == nil {
		return nil, fmt.Errorf("send_file_execution_grant_host_unavailable")
	}
	return sendFileGrantConsumer(ctx, requestedPaths, targetChannels)
}

// parseSendFileTargetChannelsInput 工具入参 target_channels 解析（list|str）。
func parseSendFileTargetChannelsInput(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			if arr, ok := parsed.([]any); ok {
				out := make([]string, 0, len(arr))
				for _, item := range arr {
					out = append(out, fmt.Sprint(item))
				}
				return out
			}
		}
		return []string{trimmed}
	default:
		return nil
	}
}

// resolveSendFileProjectDir project_dir 回退解析（Python _resolve_project_dir L240-264）。
// resolver 未注入时返回空（D-x3 缝；异常路径 Python warn 吞错 → Go 返回空等价）。
func resolveSendFileProjectDir(sessionID string) string {
	if sendFileProjectDirResolver == nil {
		return ""
	}
	projectDir := strings.TrimSpace(sendFileProjectDirResolver(sessionID))
	if projectDir == "" {
		return ""
	}
	return resolveSendFileAbsPath(projectDir)
}

// resolveSendFileAbsPath 路径绝对化（Python str(Path(x).resolve())；空值原样）。
func resolveSendFileAbsPath(p string) string {
	if p == "" {
		return ""
	}
	return cleanSendFilePath(p)
}

// cleanSendFilePath 解析真实路径（存在段 EvalSymlinks，不存在段字面保留）。
func cleanSendFilePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	if parent := filepath.Dir(abs); parent != abs {
		if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(resolvedParent, filepath.Base(abs))
		}
	}
	return abs
}

// materializeTeamDeliverableFromRoots 把 team-workspace 交付物复制进当前项目。
// Python: _materialize_team_deliverable_from_roots (send_file_to_user.py L285-346)：
// 无 project_dir 原样返回；team root 取显式配置或路径推断（L267-275）；
// 相对 team root 的路径保留复制到项目下；逃逸守卫（L320-326）；
// 同内容已存在直接用目标（L331-336）；不同内容拒绝覆盖（L337-339）。
func materializeTeamDeliverableFromRoots(filePath, projectDir, teamWorkspaceRoot string) (string, error) {
	if projectDir == "" {
		return filePath, nil
	}
	source := cleanSendFilePath(filePath)
	teamRoot := teamWorkspaceRoot
	if teamRoot == "" {
		teamRoot = inferTeamWorkspaceRoot(source)
	}
	if teamRoot == "" {
		return filePath, nil
	}
	rel, err := filepath.Rel(teamRoot, source)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filePath, nil
	}
	destination := cleanSendFilePath(filepath.Join(projectDir, rel))
	// 逃逸守卫：目标必须仍在项目目录内（Python L320-326）
	if !isPathWithinRoot(destination, projectDir) {
		return "", fmt.Errorf("project delivery path escapes the project root: %s", destination)
	}
	if filepath.Clean(destination) == filepath.Clean(source) {
		return source, nil
	}
	if fi, err := os.Lstat(destination); err == nil {
		if fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to overwrite an existing project file: %s", destination)
		}
		if fi.Mode().IsRegular() {
			srcData, err1 := os.ReadFile(source)
			dstData, err2 := os.ReadFile(destination)
			if err1 == nil && err2 == nil && string(srcData) == string(dstData) {
				return destination, nil
			}
		}
		return "", fmt.Errorf("refusing to overwrite an existing project file: %s", destination)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	if err := copySendFileCopy2(source, destination); err != nil {
		return "", err
	}
	logger.Info(logComponent).
		Str("source", source).
		Str("destination", destination).
		Msg("[SendFileToolkit] materialized team deliverable")
	return destination, nil
}

// inferTeamWorkspaceRoot 识别 .agent_teams/*/team-workspace 路径。
// Python: _infer_team_workspace_root (send_file_to_user.py L266-275)
func inferTeamWorkspaceRoot(source string) string {
	candidate := source
	for {
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return ""
		}
		if filepath.Base(candidate) == "team-workspace" &&
			filepath.Base(filepath.Dir(parent)) == ".agent_teams" {
			return candidate
		}
		candidate = parent
	}
}

// isPathWithinRoot 判定 path 位于 root 内。
func isPathWithinRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// splitZipEntryParts zip 条目路径分段（过滤空与 "."，Python L69-73）。
func splitZipEntryParts(name string) []string {
	normalized := strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(normalized, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		out = append(out, part)
	}
	return out
}

// buildSendFilePayload 构造下载信息载荷（Python L742-760 循环；含 user_id 维度）。
func buildSendFilePayload(validFiles []string, sessionID, userID string) ([]map[string]any, error) {
	payload := make([]map[string]any, 0, len(validFiles))
	for _, fp := range validFiles {
		baseName := filepath.Base(fp)
		info, err := downloadInfoBuilder(fp, baseName, sessionID, userID)
		if err != nil {
			return nil, err
		}
		payload = append(payload, map[string]any{
			"path":             fp,
			"name":             baseName,
			"size":             info["size"],
			"mime_type":        info["mime_type"],
			"download_url":     info["download_url"],
			"download_token":   info["download_token"],
			"is_skill_package": LooksLikeSkillPackage(fp),
		})
	}
	return payload, nil
}

// defaultDownloadInfoBuilder 默认下载信息构造（持久令牌：expiresIn=nil，对齐 Python
// send_file_to_user.py build_file_download_info 无 expires_in 调用；userID 由信封传入）。
func defaultDownloadInfoBuilder(filePath, baseName, sessionID, userID string) (map[string]any, error) {
	info := BuildFileDownloadInfo(filePath, baseName, sessionID, nil, userID)
	return info, nil
}

// isSendFileRegular 判定常规文件（对齐 os.path.isfile）。
func isSendFileRegular(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// currentSendFileTimestamp 当前秒级时间戳（毫秒精度，对齐 time.time()）。
func currentSendFileTimestamp() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}

// copySendFileMetadata 浅拷贝 metadata（nil 保留 nil）。
func copySendFileMetadata(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// deepCopySendFileMetadata 深拷贝 metadata（Python copy.deepcopy，D-x2 信封只读语义）。
func deepCopySendFileMetadata(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		// 序列化失败回退浅拷贝（含不可序列化值时）
		return copySendFileMetadata(m)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return copySendFileMetadata(m)
	}
	return out
}

// sentFilesKey 会话去重键（Python _sent_files_key L112-118；
// enterprise tenant 维度恒空，偏差 D-x1；空 session 落 "default"）。
func sentFilesKey(sessionID string) string {
	if sid := strings.TrimSpace(sessionID); sid != "" {
		return sid
	}
	return "default"
}

// normalizeSentFilePath 去重路径归一：abspath + 反斜杠转正斜杠 + 小写。
// Python: _normalize_sent_file_path (send_file_to_user.py L121-122)
// 注：ToSlash 在 Unix 分隔符下为 no-op，反斜杠替换须显式 strings.ReplaceAll
// （Windows 来源路径在 Linux 服务端落地的场景）。
func normalizeSentFilePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return strings.ToLower(strings.ReplaceAll(abs, "\\", "/"))
}

// partitionSentFiles 分流 (new, already_sent)，不改注册表。
// Python: _partition_sent_files (send_file_to_user.py L125-142)
func partitionSentFiles(sessionID string, paths []string) (newPaths, skipped []string) {
	sendFileSentFilePathsMu.Lock()
	sent := sentFilePathsBySession[sentFilesKey(sessionID)]
	sendFileSentFilePathsMu.Unlock()
	seen := make(map[string]struct{}, len(sent))
	for k := range sent {
		seen[k] = struct{}{}
	}
	newPaths = make([]string, 0, len(paths))
	for _, path := range paths {
		key := normalizeSentFilePath(path)
		if _, ok := seen[key]; ok {
			skipped = append(skipped, path)
		} else {
			newPaths = append(newPaths, path)
			seen[key] = struct{}{}
		}
	}
	return newPaths, skipped
}

// markFilesSent push 成功后登记已发送路径。
// Python: _mark_files_sent (send_file_to_user.py L145-149)
func markFilesSent(sessionID string, paths []string) {
	sendFileSentFilePathsMu.Lock()
	defer sendFileSentFilePathsMu.Unlock()
	key := sentFilesKey(sessionID)
	sent := sentFilePathsBySession[key]
	if sent == nil {
		sent = make(map[string]struct{}, len(paths))
		sentFilePathsBySession[key] = sent
	}
	for _, path := range paths {
		sent[normalizeSentFilePath(path)] = struct{}{}
	}
}

// copySendFileCopy2 复制文件（Python shutil.copy2：拷贝数据 + mtime；权限走 0644）。
func copySendFileCopy2(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	if fi, err := os.Stat(src); err == nil {
		_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime()) // mtime 保留尽力而为
	}
	return nil
}
