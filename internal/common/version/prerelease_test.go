package version

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestIsPrereleaseVersion 验证预发布版本检测
func TestIsPrereleaseVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		// 预发布版本
		{"alpha后缀", "0.2.0-alpha", true},
		{"beta后缀带数字", "0.2.0-beta1", true},
		{"beta后缀点分", "0.2.0.beta.1", true},
		{"rc后缀", "0.2.0rc2", true},
		{"dev后缀", "0.2.0.dev0", true},
		{"pre后缀", "0.2.0-pre3", true},
		{"a后缀", "0.2.0a1", true},
		{"b后缀", "0.2.0b2", true},
		{"带v前缀", "v0.2.0-beta1", true},
		{"大写V前缀", "V0.2.0-rc1", true},
		// 稳定版本
		{"纯数字版本", "0.2.0", false},
		{"带v前缀稳定版", "v0.2.0", false},
		{"三位版本号", "1.0.0", false},
		{"两位版本号", "1.0", false},
		// 边界情况
		{"空字符串", "", false},
		{"仅数字", "1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPrereleaseVersion(tt.version)
			if result != tt.expected {
				t.Errorf("IsPrereleaseVersion(%q) = %v, 期望 %v", tt.version, result, tt.expected)
			}
		})
	}
}

// TestStripPrereleaseSuffix 验证去除预发布后缀
func TestStripPrereleaseSuffix(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{"alpha后缀", "0.2.0-alpha", "0.2.0"},
		{"beta后缀带数字", "0.2.0-beta1", "0.2.0"},
		{"beta后缀点分", "0.2.0.beta.1", "0.2.0"},
		{"rc后缀", "0.2.0rc2", "0.2.0"},
		{"dev后缀", "0.2.0.dev0", "0.2.0"},
		{"pre后缀", "0.2.0-pre3", "0.2.0"},
		{"a后缀", "0.2.0a1", "0.2.0"},
		{"b后缀", "0.2.0b2", "0.2.0"},
		{"带v前缀", "v0.2.0-beta1", "0.2.0"},
		{"无预发布后缀", "0.2.0", "0.2.0"},
		{"空字符串", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripPrereleaseSuffix(tt.version)
			if result != tt.expected {
				t.Errorf("StripPrereleaseSuffix(%q) = %q, 期望 %q", tt.version, result, tt.expected)
			}
		})
	}
}

// TestIsNewerVersion 验证版本比较
func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		current   string
		expected  bool
	}{
		// 基础版本号比较
		{"更高版本", "0.3.0", "0.2.0", true},
		{"更低版本", "0.1.0", "0.2.0", false},
		{"相同版本", "0.2.0", "0.2.0", false},
		{"更高主版本", "1.0.0", "0.9.9", true},
		{"更高补丁版本", "0.2.1", "0.2.0", true},
		// 预发布 vs 稳定版
		{"稳定版比预发布新", "0.2.0", "0.2.0-beta1", true},
		{"预发布比稳定版旧", "0.2.0-beta1", "0.2.0", false},
		// 两个预发布版
		{"beta2比beta1新", "0.2.0-beta2", "0.2.0-beta1", true},
		{"beta1比beta2旧", "0.2.0-beta1", "0.2.0-beta2", false},
		// 不同基础版本
		{"更高基础版本的预发布", "0.3.0-alpha1", "0.2.0", true},
		{"更低基础版本的预发布", "0.1.0-rc1", "0.2.0", false},
		// 带v前缀
		{"带v前缀更高版本", "v0.3.0", "v0.2.0", true},
		// 边界情况
		{"相同预发布版本", "0.2.0-beta1", "0.2.0-beta1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNewerVersion(tt.candidate, tt.current)
			if result != tt.expected {
				t.Errorf("IsNewerVersion(%q, %q) = %v, 期望 %v",
					tt.candidate, tt.current, result, tt.expected)
			}
		})
	}
}
