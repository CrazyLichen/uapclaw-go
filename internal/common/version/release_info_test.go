package version

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestReleaseAsset 验证 ReleaseAsset 结构体字段赋值
func TestReleaseAsset(t *testing.T) {
	asset := ReleaseAsset{
		Name:        "uapclaw-0.2.0-linux-amd64.tar.gz",
		DownloadURL: "https://github.com/example/repo/releases/download/v0.2.0/uapclaw-0.2.0-linux-amd64.tar.gz",
		Size:        1024000,
	}

	if asset.Name != "uapclaw-0.2.0-linux-amd64.tar.gz" {
		t.Errorf("Name = %q, 期望 %q", asset.Name, "uapclaw-0.2.0-linux-amd64.tar.gz")
	}
	if asset.DownloadURL == "" {
		t.Error("DownloadURL 不应为空")
	}
	if asset.Size != 1024000 {
		t.Errorf("Size = %d, 期望 %d", asset.Size, 1024000)
	}
}

// TestReleaseInfo 验证 ReleaseInfo 结构体字段赋值
func TestReleaseInfo(t *testing.T) {
	info := ReleaseInfo{
		Version:      "0.2.0",
		ReleaseNotes: "修复若干问题",
		PublishedAt:  "2025-07-12T00:00:00Z",
		Assets: []ReleaseAsset{
			{Name: "uapclaw-0.2.0-linux-amd64.tar.gz", DownloadURL: "https://example.com/1", Size: 1024},
			{Name: "uapclaw-0.2.0-darwin-arm64.tar.gz", DownloadURL: "https://example.com/2", Size: 2048},
		},
		SourceType: "github",
		Prerelease: false,
	}

	if info.Version != "0.2.0" {
		t.Errorf("Version = %q, 期望 %q", info.Version, "0.2.0")
	}
	if len(info.Assets) != 2 {
		t.Fatalf("Assets 数量 = %d, 期望 2", len(info.Assets))
	}
	if info.Assets[0].Name != "uapclaw-0.2.0-linux-amd64.tar.gz" {
		t.Errorf("Assets[0].Name = %q, 期望 %q", info.Assets[0].Name, "uapclaw-0.2.0-linux-amd64.tar.gz")
	}
	if info.SourceType != "github" {
		t.Errorf("SourceType = %q, 期望 %q", info.SourceType, "github")
	}
	if info.Prerelease {
		t.Error("Prerelease 应为 false")
	}
}

// TestReleaseInfo_Prerelease 验证预发布版本标记
func TestReleaseInfo_Prerelease(t *testing.T) {
	info := ReleaseInfo{
		Version:    "0.2.0-beta1",
		SourceType: "github",
		Prerelease: true,
	}

	if !info.Prerelease {
		t.Error("Prerelease 应为 true")
	}
}

// TestReleaseInfo_EmptyAssets 验证空资产列表
func TestReleaseInfo_EmptyAssets(t *testing.T) {
	info := ReleaseInfo{
		Version:    "0.1.0",
		SourceType: "github",
	}

	if info.Assets != nil {
		t.Errorf("Assets 应为 nil，实际为 %v", info.Assets)
	}
}
