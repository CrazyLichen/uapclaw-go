package toolguard

import "testing"

// TestCanonicalizeShellCommandForPermission 测试 launcher 脱壳与不脱壳场景
func TestCanonicalizeShellCommandForPermission(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"cmd /c 双引号", `cmd /c "rm -rf /"`, "rm -rf /"},
		{"cmd.exe /c 单引号", `cmd.exe /c 'del x'`, "del x"},
		{"cmd /c bare", `cmd /c rm`, "rm"},
		{"cmd //c 双斜杠", `cmd //c "rm x"`, "rm x"},
		{"pwsh -Command", `pwsh -Command "rm x"`, "rm x"},
		{"powershell -c", `powershell -c 'rm x'`, "rm x"},
		{"pwsh 带其他开关", `pwsh -NoProfile -Command "rm x"`, "rm x"},
		{"powershell.exe -NoLogo -c", `powershell.exe -NoLogo -c "rm x"`, "rm x"},
		{"bash -c", `bash -c "rm x"`, "rm x"},
		{"bash -lc 组合", `bash -lc 'rm x'`, "rm x"},
		{"bash -cl 组合", `bash -cl 'rm x'`, "rm x"},
		{"sh -c", `sh -c 'rm x'`, "rm x"},
		{"zsh -c", `zsh -c 'rm x'`, "rm x"},
		{"dash -c", `dash -c 'rm x'`, "rm x"},
		{"ash -c", `ash -c 'rm x'`, "rm x"},
		{"绝对路径 bash", `/bin/bash -c "rm x"`, "rm x"},
		{"引号包住的 launcher", `"/bin/bash" -c "rm x"`, "rm x"},
		{"分隔符后接 bash 空格还原", `cd d && bash -c "rm x"`, "cd d && rm x"},
		{"分号后接 sh", `a; sh -c 'rm y'`, "a; rm y"},
		{"纯空白前缀不脱壳", `echo bash -c 'rm'`, `echo bash -c 'rm'`},
		{"无 launcher 原样", `ls -la`, `ls -la`},
		{"空串", ``, ``},
		{"嵌套只脱一层", `bash -c "bash -c 'rm x'"`, `bash -c 'rm x'`},
		{"括号后接 bash", `( bash -c "rm z" )`, `rm z )`},
		{"竖线后接 bash", `| bash -c "rm w"`, `rm w`},
		{"与号后接 bash", `& bash -c "rm v"`, `rm v`},
		{"制表符后接 bash", "\tbash -c 'rm u'", `rm u`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanonicalizeShellCommandForPermission(c.in); got != c.want {
				t.Errorf("Canonicalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestStripFdAliasTokens 测试 fd 复制标记的移除
func TestStripFdAliasTokens(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ls 2>&1", "ls "},
		{"ls >&2", "ls "},
		{"ls <3", "ls "},
		{"ls &1", "ls "},
		{"ls -la", "ls -la"},
		{"", ""},
	}
	for _, c := range cases {
		if got := StripFdAliasTokens(c.in); got != c.want {
			t.Errorf("StripFdAliasTokens(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIsFdAliasToken 测试 fd 标记识别
func TestIsFdAliasToken(t *testing.T) {
	yes := []string{"2>&1", ">&2", "<3", "&1", " 2>&1 "}
	no := []string{"", "ls", "2>", "2>&1x", "x2>&1"}
	for _, s := range yes {
		if !IsFdAliasToken(s) {
			t.Errorf("IsFdAliasToken(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsFdAliasToken(s) {
			t.Errorf("IsFdAliasToken(%q) = true, want false", s)
		}
	}
}

// TestCanonicalizeShellCommandForPermission_中文路径 测试非 ASCII 命令不被截断
//
// ⚠️ 回归测试：regexp2 的 Match.Index/Length 是 rune 索引，不是字节索引。
// 早期实现直接用 Index 切字符串，含中文的命令会被截断错位
// （`.../探针373/001/y.txt` 变成 `.../探针373/001/y.txt963/001/y.txt`）。
func TestCanonicalizeShellCommandForPermission_中文路径(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"中文目录 bash -c", `bash -c "cat /tmp/中文目录/文件.txt"`, `cat /tmp/中文目录/文件.txt`},
		{"中文前缀后接 bash", `中文命令 && bash -c "rm x"`, `中文命令 && rm x`},
		{"emoji 路径", `sh -c 'cat /tmp/测试😀/a.txt'`, `cat /tmp/测试😀/a.txt`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanonicalizeShellCommandForPermission(c.in); got != c.want {
				t.Errorf("Canonicalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
