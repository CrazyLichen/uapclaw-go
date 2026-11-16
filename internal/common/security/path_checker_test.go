package security

import "testing"

// TestIsSensitivePath_默认敏感清单 对照 Python path_checker.py 用例面逐条核对
func TestIsSensitivePath_默认敏感清单(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"passwd命中", "/etc/passwd", true},
		{"shadow命中", "/etc/shadow", true},
		{"hosts命中", "/etc/hosts", true},
		{"ssh目录前缀命中", "/etc/ssh/sshd_config", true},
		{"proc前缀命中", "/proc/self/status", true},
		{"sys前缀命中", "/sys/kernel/mm", true},
		{"dev前缀命中", "/dev/null", true},
		{"普通路径不命中", "/home/user/project/file.go", false},
		{"空串不命中", "", false},
		{"相对路径不命中", "relative/path.txt", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSensitivePath(c.path); got != c.want {
				t.Fatalf("IsSensitivePath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

// TestIsSensitivePath_前缀语义 Python 用 startswith 判定：/etc/passwdX 也命中 /etc/passwd 前缀
func TestIsSensitivePath_前缀语义(t *testing.T) {
	// /etc/passwd 是清单成员，任何以其为前缀的路径都命中
	if !IsSensitivePath("/etc/passwd-backup") {
		t.Fatal("前缀 startswith 语义未对齐：/etc/passwd-backup 应命中")
	}
}

// TestIsSensitivePath_用户目录展开 Python expanduser 语义：~ 展开后再判定
func TestIsSensitivePath_用户目录展开(t *testing.T) {
	// ~/tmp 不在敏感清单，应不命中且不报错
	if IsSensitivePath("~/tmp/normal.txt") {
		t.Fatal("~/tmp/normal.txt 不应命中")
	}
}
