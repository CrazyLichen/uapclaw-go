package version

// ──────────────────────────── 结构体 ────────────────────────────

// ReleaseAsset 发布资产（可下载文件）
type ReleaseAsset struct {
	// Name 文件名
	Name string
	// DownloadURL 下载地址
	DownloadURL string
	// Size 文件大小（字节）
	Size int64
}

// ReleaseInfo 发布版本信息
type ReleaseInfo struct {
	// Version 版本号（已去除 v 前缀）
	Version string
	// ReleaseNotes 发布说明
	ReleaseNotes string
	// PublishedAt 发布时间（ISO 8601 格式）
	PublishedAt string
	// Assets 可下载的资产列表
	Assets []ReleaseAsset
	// SourceType 版本来源类型（如 "github"）
	SourceType string
	// Prerelease 是否为预发布版本
	Prerelease bool
}
