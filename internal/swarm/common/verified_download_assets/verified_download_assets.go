// TTL 拥有的不可变下载资产账本，用于授权文件投递。
// 对齐 Python jiuwenswarm/common/verified_download_assets.py（409 行）：
// 方法归属与步骤顺序一比一；Python threading.RLock → Go sync.Mutex（同族偏差沿用）。

package verified_download_assets

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const logComponent = logger.ComponentCommon

// ──────────────────────────── 常量 ────────────────────────────

const (
	// assetStateStaged sidecar 初始态（Python _ASSET_STATE_STAGED L23）
	assetStateStaged = "staged"
	// assetStateCommitted push 成功后的交付态（Python _ASSET_STATE_COMMITTED L24）
	assetStateCommitted = "committed"

	// defaultSweepIntervalSeconds 后台清扫间隔（Python L26）
	defaultSweepIntervalSeconds = 30.0
	// defaultOrphanGraceSeconds 孤儿文件宽限（Python L27）
	defaultOrphanGraceSeconds = 120.0
	// digestChunkSize 拷贝/摘要分块（Python _DIGEST_CHUNK_SIZE L28）
	digestChunkSize = 1024 * 1024
)

// ──────────────────────────── 结构体 ────────────────────────────

// VerifiedDownloadAsset 单个不可变密封文件（token 可见前 staged）。
// Python: VerifiedDownloadAsset (verified_download_assets.py L39-47 dataclass)
type VerifiedDownloadAsset struct {
	// AssetID 资产标识（32 位 hex，Python asset_id L43）
	AssetID string
	// SealedPath 密封副本路径（Python sealed_path L44）
	SealedPath string
	// ExpiresAt 过期时间戳（秒，Python expires_at L45）
	ExpiresAt float64
	// SizeBytes 字节数（Python size_bytes L46）
	SizeBytes int64
	// ContentDigest 内容摘要 "sha256:<hex>"（Python content_digest L47）
	ContentDigest string
}

// OwnerOptions 构造选项（Python __init__ kwargs L53-61）。
type OwnerOptions struct {
	// NowFn 时钟注入（Python now_fn；nil → time.time）
	NowFn func() float64
	// SweepIntervalSeconds 后台清扫间隔（Python sweep_interval_seconds；0 → 默认 30，下限 0.1）
	SweepIntervalSeconds float64
	// OrphanGraceSeconds 孤儿宽限（Python orphan_grace_seconds；0 → 默认 120，下限 1）
	OrphanGraceSeconds float64
	// StartSweeper 是否启后台清扫（Python start_sweeper）
	StartSweeper bool
}

// VerifiedDownloadAssetOwner 资产账本宿主：在 token TTL 内持有 staged/committed 密封文件。
// Python: VerifiedDownloadAssetOwner (verified_download_assets.py L50)
type VerifiedDownloadAssetOwner struct {
	// root 资产根目录（Python self.root L65）
	root string
	// nowFn 时钟（Python _now_fn L66）
	nowFn func() float64
	// sweepIntervalSeconds 清扫间隔（Python _sweep_interval_seconds L67）
	sweepIntervalSeconds float64
	// orphanGraceSeconds 孤儿宽限（Python _orphan_grace_seconds L68）
	orphanGraceSeconds float64
	// startSweeper 是否允许后台清扫（Python _start_sweeper L69）
	startSweeper bool
	// mu 互斥（Python _lock RLock L70）
	mu sync.Mutex
	// stopCh 清扫停止信号（Python _stop_event L71）
	stopCh chan struct{}
	// sweeperStarted 后台清扫已启动（Python _sweeper L72）
	sweeperStarted bool
	// sweeperDone 清扫 goroutine 退出信号（Close join 对齐）
	sweeperDone chan struct{}
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sharedOwner 进程级单例（Python _SHARED_VERIFIED_DOWNLOAD_ASSET_OWNER L403）。
	// 偏差注记：Python 在模块导入时构造，Go 首次调用时惰性构造（env 读取时机差异，
	// 行为等价：root 均来自 defaultAssetRoot）。
	sharedOwner     *VerifiedDownloadAssetOwner
	sharedOwnerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVerifiedDownloadAssetOwner 构造账本宿主；root 为 symlink 时拒绝。
// Python: VerifiedDownloadAssetOwner.__init__ (verified_download_assets.py L53-72)
func NewVerifiedDownloadAssetOwner(root string, opts OwnerOptions) (*VerifiedDownloadAssetOwner, error) {
	requested := root
	if requested == "" {
		requested = defaultAssetRoot()
	}
	if isSymlink(requested) {
		return nil, fmt.Errorf("download_asset_root_invalid")
	}
	// Python L65: absolute().resolve(strict=False)
	absRoot, err := filepath.Abs(requested)
	if err != nil {
		return nil, err
	}
	if resolved, err := resolveNonStrict(absRoot); err == nil {
		absRoot = resolved
	}
	nowFn := opts.NowFn
	if nowFn == nil {
		nowFn = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	sweepInterval := opts.SweepIntervalSeconds
	if sweepInterval <= 0 {
		sweepInterval = defaultSweepIntervalSeconds
	}
	if sweepInterval < 0.1 {
		sweepInterval = 0.1
	}
	orphanGrace := opts.OrphanGraceSeconds
	if orphanGrace <= 0 {
		orphanGrace = defaultOrphanGraceSeconds
	}
	if orphanGrace < 1.0 {
		orphanGrace = 1.0
	}
	return &VerifiedDownloadAssetOwner{
		root:                 absRoot,
		nowFn:                nowFn,
		sweepIntervalSeconds: sweepInterval,
		orphanGraceSeconds:   orphanGrace,
		startSweeper:         opts.StartSweeper,
		stopCh:               make(chan struct{}),
		sweeperDone:          make(chan struct{}),
	}, nil
}

// StageOptions Stage 选项（Python stage kwargs L76-79）。
type StageOptions struct {
	// FileName 交付文件名（决定密封后缀，Python file_name）
	FileName string
	// ExpiresAt 过期时间戳（秒，必须晚于当前时钟）
	ExpiresAt float64
}

// Stage 原子捕获一份交付时稳定快照。
// Python: VerifiedDownloadAssetOwner.stage (verified_download_assets.py L74-117)
func (o *VerifiedDownloadAssetOwner) Stage(sourcePath string, opts StageOptions) (*VerifiedDownloadAsset, error) {
	// ① 过期校验（Python L83-84）
	if opts.ExpiresAt <= o.nowFn() {
		return nil, fmt.Errorf("download_asset_expired")
	}
	// ② 源路径归一（Python L85）
	source, err := filepath.Abs(expandHome(sourcePath))
	if err != nil {
		return nil, err
	}
	// ③ 根目录就绪 + prune（Python L86-87）
	if err := o.ensureRoot(); err != nil {
		return nil, err
	}
	o.Prune(0)
	// ④ asset_id（uuid4 hex 32 位，Python L88）
	assetID, err := randomHex32()
	if err != nil {
		return nil, err
	}
	// ⑤ 密封路径保留后缀（截 32 字符，Python L89-90）
	suffix := filepath.Ext(opts.FileName)
	if len(suffix) > 32 {
		suffix = suffix[:32]
	}
	sealedPath := filepath.Join(o.root, assetID+suffix)
	sidecarPath := o.sidecarPath(assetID)
	// ⑥ 密封拷贝 + sidecar（Python L92-105）；失败清理（L106-109）
	actualSize, actualDigest, copyErr := copyVerifiedSource(source, sealedPath)
	if copyErr != nil {
		o.safeUnlink(sidecarPath)
		o.safeUnlink(sealedPath)
		return nil, copyErr
	}
	payload := map[string]any{
		"asset_id":       assetID,
		"sealed_path":    toPosix(sealedPath),
		"expires_at":     opts.ExpiresAt,
		"size_bytes":     actualSize,
		"content_digest": actualDigest,
		"state":          assetStateStaged,
	}
	if err := o.writeSidecarAtomic(sidecarPath, payload); err != nil {
		o.safeUnlink(sidecarPath)
		o.safeUnlink(sealedPath)
		return nil, err
	}
	// ⑦ 首次启动后台清扫（Python L110）
	o.ensureSweeper()
	return &VerifiedDownloadAsset{
		AssetID:       assetID,
		SealedPath:    sealedPath,
		ExpiresAt:     opts.ExpiresAt,
		SizeBytes:     actualSize,
		ContentDigest: actualDigest,
	}, nil
}

// Commit 标记 staged 资产已交付（保留同一 TTL 归属）。
// Python: VerifiedDownloadAssetOwner.commit (verified_download_assets.py L119-130)
func (o *VerifiedDownloadAssetOwner) Commit(asset *VerifiedDownloadAsset) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	sidecarPath := o.sidecarPath(asset.AssetID)
	payload, err := o.readSidecar(sidecarPath)
	if err != nil {
		return err
	}
	if !payloadMatchesAsset(payload, asset) {
		return fmt.Errorf("download_asset_registration_mismatch")
	}
	state, _ := payload["state"].(string)
	if state != assetStateStaged && state != assetStateCommitted {
		return fmt.Errorf("download_asset_state_invalid")
	}
	payload["state"] = assetStateCommitted
	return o.writeSidecarAtomic(sidecarPath, payload)
}

// Revoke 删除尚未对外可见的资产。
// Python: VerifiedDownloadAssetOwner.revoke (verified_download_assets.py L132-137)
func (o *VerifiedDownloadAssetOwner) Revoke(asset *VerifiedDownloadAsset) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.safeUnlink(o.sidecarPath(asset.AssetID))
	o.safeUnlink(asset.SealedPath)
}

// IsActiveClaim token 侧声明（Python is_active kwargs L141-147）。
type IsActiveClaim struct {
	// AssetID 资产标识
	AssetID string
	// SealedPath 密封路径
	SealedPath string
	// ExpiresAt 声明的过期时间
	ExpiresAt float64
	// SizeBytes 声明的字节数
	SizeBytes int64
	// ContentDigest 声明的摘要
	ContentDigest string
}

// IsActive 以持久 staged 归属校验 token 声明。
// Python: VerifiedDownloadAssetOwner.is_active (verified_download_assets.py L139-175)
// now 参数（Python now=None）由 owner.nowFn 承担；显式时刻走 Prune(0) 同款约定不适用，
// IsActive 无 now 入参（校验矩阵测试经时钟注入拨动）。
func (o *VerifiedDownloadAssetOwner) IsActive(claim IsActiveClaim) bool {
	// ① 过期（Python L151-153）
	if o.nowFn() > claim.ExpiresAt {
		return false
	}
	// ② asset_id 归一（Python L154-156）
	normalizedID := normalizeAssetID(claim.AssetID)
	if normalizedID == "" {
		return false
	}
	// ③ 路径必须位于 root 内且不以 "." 开头（Python L157-159）
	candidate, err := resolveNonStrict(expandHome(claim.SealedPath))
	if err != nil {
		return false
	}
	if filepath.Dir(candidate) != o.root || strings.HasPrefix(filepath.Base(candidate), ".") {
		return false
	}
	// ④ sidecar 读取（Python L160-163）
	payload, err := o.readSidecar(o.sidecarPath(normalizedID))
	if err != nil {
		return false
	}
	// ⑤ 五项一致性 + 常规文件非 symlink（Python L164-175）
	state, _ := payload["state"].(string)
	expiredAt, _ := payload["expires_at"].(float64)
	sizeBytes, _ := payload["size_bytes"].(float64)
	digest, _ := payload["content_digest"].(string)
	normalizedDigest, digestErr := normalizeDigest(claim.ContentDigest)
	if digestErr != nil {
		return false
	}
	return (state == assetStateStaged || state == assetStateCommitted) &&
		payload["asset_id"] == normalizedID &&
		payload["sealed_path"] == toPosix(candidate) &&
		expiredAt == claim.ExpiresAt &&
		int64(sizeBytes) == claim.SizeBytes &&
		digest == normalizedDigest &&
		regularFileWithoutSymlink(candidate)
}

// Prune 回收注册资产并清理过期/陈旧孤儿文件。
// Python: VerifiedDownloadAssetOwner.prune (verified_download_assets.py L177-221)
// now 参数：0 → 用 nowFn（对齐 Python now=None）。
func (o *VerifiedDownloadAssetOwner) Prune(now float64) {
	if now <= 0 {
		now = o.nowFn()
	}
	if err := o.ensureRoot(); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[VerifiedDownloadAssets] ensure root failed")
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	// ① sidecar 名册扫描（Python L183-209）
	registeredNames := make(map[string]struct{})
	sidecars, _ := filepath.Glob(filepath.Join(o.root, "*.json"))
	for _, sidecarPath := range sidecars {
		if isSymlink(sidecarPath) {
			o.safeUnlink(sidecarPath)
			continue
		}
		payload, err := o.readSidecar(sidecarPath)
		sealedPath := ""
		validLocation := false
		expired := true
		validState := false
		if err == nil {
			sp, _ := payload["sealed_path"].(string)
			sealedPath = resolveNonStrictString(sp)
			_, assetIDOK := payload["asset_id"]
			validLocation = filepath.Dir(sealedPath) == o.root &&
				assetIDOK && payload["asset_id"] == strings.TrimSuffix(filepath.Base(sidecarPath), ".json")
			expAt, _ := payload["expires_at"].(float64)
			expired = now > expAt
			state, _ := payload["state"].(string)
			validState = state == assetStateStaged || state == assetStateCommitted
		} else {
			sealedPath = filepath.Join(o.root, strings.TrimSuffix(filepath.Base(sidecarPath), ".json"))
		}
		if !validLocation || expired || !validState {
			o.safeUnlink(sidecarPath)
			if validLocation {
				o.safeUnlink(sealedPath)
			}
			continue
		}
		registeredNames[filepath.Base(sealedPath)] = struct{}{}
	}
	// ② 孤儿清理（Python L211-221）
	orphanCutoff := now - o.orphanGraceSeconds
	entries, err := os.ReadDir(o.root)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[VerifiedDownloadAssets] list root failed")
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".json") {
			continue
		}
		if _, ok := registeredNames[name]; ok || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		if float64(info.ModTime().UnixNano())/1e9 < orphanCutoff {
			o.safeUnlink(filepath.Join(o.root, name))
		}
	}
}

// Close 停止后台清扫。
// Python: VerifiedDownloadAssetOwner.close (verified_download_assets.py L223-229)
func (o *VerifiedDownloadAssetOwner) Close() {
	close(o.stopCh)
	select {
	case <-o.sweeperDone:
	case <-time.After(time.Second):
	}
}

// Root 返回资产根目录（测试与校验用）。
func (o *VerifiedDownloadAssetOwner) Root() string { return o.root }

// GetVerifiedDownloadAssetOwner 返回进程级单例（共享投递根）。
// Python: get_verified_download_asset_owner (verified_download_assets.py L406-409)
func GetVerifiedDownloadAssetOwner() *VerifiedDownloadAssetOwner {
	sharedOwnerOnce.Do(func() {
		owner, err := NewVerifiedDownloadAssetOwner("", OwnerOptions{StartSweeper: true})
		if err != nil {
			logger.Error(logComponent).Err(err).
				Msg("[VerifiedDownloadAssets] failed to create shared owner")
			// 兜底：退回临时目录独立 root（构造期已排除常见失败源，防御性）
			fallback := filepath.Join(os.TempDir(), fmt.Sprintf("uapclaw-download-assets-%d-fallback", os.Getpid()))
			owner, err = NewVerifiedDownloadAssetOwner(fallback, OwnerOptions{StartSweeper: true})
			if err != nil {
				panic(err)
			}
		}
		sharedOwner = owner
	})
	return sharedOwner
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureRoot 根目录就绪校验（0o700）。
// Python: _ensure_root (verified_download_assets.py L231-235)
func (o *VerifiedDownloadAssetOwner) ensureRoot() error {
	if err := os.MkdirAll(o.root, 0o700); err != nil {
		return err
	}
	if isSymlink(o.root) {
		return fmt.Errorf("download_asset_root_invalid")
	}
	fi, err := os.Stat(o.root)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("download_asset_root_invalid")
	}
	if err := os.Chmod(o.root, 0o700); err != nil {
		return err
	}
	return nil
}

// ensureSweeper 首次 Stage 启动后台清扫（单次）。
// Python: _ensure_sweeper (verified_download_assets.py L237-248)
func (o *VerifiedDownloadAssetOwner) ensureSweeper() {
	if !o.startSweeper {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sweeperStarted {
		return
	}
	o.sweeperStarted = true
	go o.sweepLoop()
}

// sweepLoop 后台清扫循环。
// Python: _sweep_loop (verified_download_assets.py L250-255)
func (o *VerifiedDownloadAssetOwner) sweepLoop() {
	defer close(o.sweeperDone)
	ticker := time.NewTicker(time.Duration(o.sweepIntervalSeconds * float64(time.Second)))
	defer ticker.Stop()
	for {
		select {
		case <-o.stopCh:
			return
		case <-ticker.C:
			o.Prune(0)
		}
	}
}

// copyVerifiedSource 密封拷贝源文件并返回 (size, digest)。
// Python: _copy_verified_source (verified_download_assets.py L257-295)
// 步骤：①lstat 源（非常规文件/symlink 拒绝）→ ②O_NOFOLLOW 读打开 + fstat 复查 →
// ③O_EXCL 写打开 → ④分块读写+sha256 → ⑤size 复核 → ⑥fsync+chmod 0400。
func copyVerifiedSource(source, destination string) (int64, string, error) {
	sourceStat, err := os.Lstat(source)
	if err != nil {
		return 0, "", fmt.Errorf("download_asset_source_not_file: %w", err)
	}
	if !sourceStat.Mode().IsRegular() {
		return 0, "", fmt.Errorf("download_asset_source_not_file")
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return 0, "", fmt.Errorf("download_asset_source_not_file: %w", err)
	}
	defer sourceFile.Close()
	openedStat, err := sourceFile.Stat()
	if err != nil || !openedStat.Mode().IsRegular() {
		return 0, "", fmt.Errorf("download_asset_source_not_file")
	}
	destinationFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, "", err
	}
	defer destinationFile.Close()
	digest := sha256.New()
	copied, err := ioCopyDigest(destinationFile, sourceFile, digest)
	if err != nil {
		return 0, "", err
	}
	if copied != openedStat.Size() {
		return 0, "", fmt.Errorf("download_asset_size_mismatch")
	}
	actualDigest := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if err := destinationFile.Sync(); err != nil {
		return 0, "", err
	}
	if err := os.Chmod(destination, 0o400); err != nil {
		return 0, "", err
	}
	return copied, actualDigest, nil
}

// ioCopyDigest 分块拷贝并同步计算 sha256（Python L279-288 循环，chunk 1MiB）。
func ioCopyDigest(dst io.Writer, src io.Reader, digest hash.Hash) (int64, error) {
	buf := make([]byte, digestChunkSize)
	total := int64(0)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, werr := dst.Write(chunk); werr != nil {
				return total, werr
			}
			digest.Write(chunk) // hash.Hash.Write 恒成功
			total += int64(n)
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

// writeSidecarAtomic 原子写 sidecar（tmp + rename + 目录 fsync）。
// Python: _write_sidecar_atomic (verified_download_assets.py L297-324)
func (o *VerifiedDownloadAssetOwner) writeSidecarAtomic(sidecarPath string, payload map[string]any) error {
	// Python sort_keys=True + separators=(",", ":") → 稳定字节序
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tempPath := filepath.Join(o.root, fmt.Sprintf(".%s.%s.tmp",
		strings.TrimSuffix(filepath.Base(sidecarPath), ".json"), mustRandomHex32()))
	tempFile, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := tempFile.Write(encoded)
	syncErr := tempFile.Sync()
	closeErr := tempFile.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		o.safeUnlink(tempPath)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := os.Rename(tempPath, sidecarPath); err != nil {
		o.safeUnlink(tempPath)
		return err
	}
	// Python L317-322: 非 Windows fsync 目录（Go 无目录 fd fsync 可移植语义，
	// 依赖 rename 原子性，行为等价；Linux 用 syscall.Fsync 目录 fd 尽力对齐）
	fsyncDirBestEffort(o.root)
	return nil
}

// readSidecar 读取并校验 sidecar payload。
// Python: _read_sidecar (verified_download_assets.py L326-333)
func (o *VerifiedDownloadAssetOwner) readSidecar(sidecarPath string) (map[string]any, error) {
	if !regularFileWithoutSymlink(sidecarPath) {
		return nil, fmt.Errorf("download_asset_sidecar_invalid")
	}
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("download_asset_sidecar_invalid")
	}
	return payload, nil
}

// payloadMatchesAsset 五字段一致性（Python _payload_matches_asset L335-346）。
func payloadMatchesAsset(payload map[string]any, asset *VerifiedDownloadAsset) bool {
	expiredAt, _ := payload["expires_at"].(float64)
	sizeBytes, _ := payload["size_bytes"].(float64)
	digest, _ := payload["content_digest"].(string)
	return payload["asset_id"] == asset.AssetID &&
		payload["sealed_path"] == toPosix(asset.SealedPath) &&
		expiredAt == asset.ExpiresAt &&
		int64(sizeBytes) == asset.SizeBytes &&
		digest == asset.ContentDigest
}

// sidecarPath sidecar 路径（asset_id 校验）。
// Python: _sidecar_path (verified_download_assets.py L348-352)
func (o *VerifiedDownloadAssetOwner) sidecarPath(assetID string) string {
	normalizedID := normalizeAssetID(assetID)
	if normalizedID == "" {
		// Python 抛 ValueError；Go 调用链中 assetID 均已归一，防御性返回空拼接
		return filepath.Join(o.root, ".invalid.json")
	}
	return filepath.Join(o.root, normalizedID+".json")
}

// safeUnlink 仅删 root 内路径，忽略不存在。
// Python: _safe_unlink (verified_download_assets.py L354-360)
func (o *VerifiedDownloadAssetOwner) safeUnlink(path string) {
	parent := resolveNonStrictString(filepath.Dir(path))
	if parent != o.root {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Warn(logComponent).Str("path", path).
			Msg("[VerifiedDownloadAssets] failed to remove asset path")
	}
}

// normalizeAssetId 归一资产 id：trim+lower，32 位 hex，非法返回空。
// Python: _normalize_asset_id (verified_download_assets.py L363-371)
func normalizeAssetID(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != 32 {
		return ""
	}
	if !isHex(normalized) {
		return ""
	}
	return normalized
}

// normalizeDigest 归一摘要：trim+lower，去 sha256: 前缀，须 64 位 hex，非法报错。
// Python: _normalize_digest (verified_download_assets.py L374-383，raise ValueError → Go 返回 error)
func normalizeDigest(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimPrefix(normalized, "sha256:")
	if len(normalized) != 64 || !isHex(normalized) {
		return "", fmt.Errorf("download_asset_digest_invalid")
	}
	return "sha256:" + normalized, nil
}

// regularFileWithoutSymlink 非常规文件或 symlink 返回 false。
// Python: _regular_file_without_symlink (verified_download_assets.py L386-391)
func regularFileWithoutSymlink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular() && !isSymlink(path)
}

// defaultAssetRoot 默认资产根（环境变量优先，否则临时目录按 uid 隔离）。
// Python: _default_asset_root (verified_download_assets.py L31-36)
func defaultAssetRoot() string {
	configured := strings.TrimSpace(os.Getenv("JIUWENSWARM_DOWNLOAD_ASSET_ROOT"))
	if configured != "" {
		return expandHome(configured)
	}
	ownerID := os.Getuid()
	if ownerID == -1 {
		ownerID = os.Getpid() // Windows 无 getuid（Python hasattr 兜底对齐）
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("jiuwenswarm-download-assets-%d", ownerID))
}

// isSymlink 判定路径本身是否 symlink（lstat）。
func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// resolveNonStrict 不存在也返回归一化绝对路径（Python Path.resolve(strict=False)）。
func resolveNonStrict(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// 逐段清洗，模拟 Python resolve 对 .. 的语义化消解
	return cleanPath(abs), nil
}

// resolveNonStrictString 忽略错误的字符串版。
func resolveNonStrictString(path string) string {
	resolved, err := resolveNonStrict(path)
	if err != nil {
		return path
	}
	return resolved
}

// cleanPath 逐段解析真实路径（已存在段用 EvalSymlinks，不存在段保留字面）。
func cleanPath(abs string) string {
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	parent := filepath.Dir(abs)
	if parent != abs {
		if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(resolvedParent, filepath.Base(abs))
		}
	}
	return abs
}

// expandHome 展开 ~（Python Path.expanduser）。
func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// toPosix 路径转 posix 斜杠（Python as_posix）。
func toPosix(path string) string {
	return filepath.ToSlash(path)
}

// randomHex32 生成 32 位 hex 随机 id（Python uuid.uuid4().hex）。
func randomHex32() (string, error) {
	return mustRandomHex32E()
}

// mustRandomHex32 生成 32 位 hex（writeSidecarAtomic 临时名用，失败 panic 不可能路径）。
func mustRandomHex32() string {
	id, err := mustRandomHex32E()
	if err != nil {
		// crypto/rand 失败属系统级异常，与 Python uuid4 同语义不可恢复
		panic(err)
	}
	return id
}

// mustRandomHex32E 随机 16 字节 → 32 hex 字符。
func mustRandomHex32E() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// isHex 判定 0-9a-f。
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// fsyncDirBestEffort 目录 fsync 尽力而为（仅 Linux/类 Unix）。
func fsyncDirBestEffort(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	defer f.Close()
	_ = f.Sync() // Windows 返回 EINVAL，忽略（Python os.name != "nt" 守卫等价）
}
