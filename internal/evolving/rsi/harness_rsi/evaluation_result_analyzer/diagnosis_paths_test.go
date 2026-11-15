package evaluationresultanalyzer

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateDiagnosisPaths_非Windows直接通过 对照 Python if not _IS_WINDOWS: return。
// Python: diagnosis_paths.py:69-70
func TestValidateDiagnosisPaths_非Windows直接通过(t *testing.T) {
	if err := validateDiagnosisPathsWithGOOS("/very/long/"+strings.Repeat("x", 300), "/tmp/attempt", "", "", false); err != nil {
		t.Fatalf("非 Windows 不校验: %v", err)
	}
}

// TestValidateDiagnosisPaths_预算内通过 对照 Python 正常路径。
func TestValidateDiagnosisPaths_预算内通过(t *testing.T) {
	dir := "/tmp/rd/" + strings.Repeat("a", 100)
	if err := validateDiagnosisPathsWithGOOS(dir, dir+"/attempt", "c1", "stage", true); err != nil {
		t.Fatalf("预算内应通过: %v", err)
	}
}

// TestValidateDiagnosisPaths_超预算报错 对照 Python raise DiagnosisPathTooLongError。
// Python: diagnosis_paths.py:77-80, 41-53
func TestValidateDiagnosisPaths_超预算报错(t *testing.T) {
	// runtime 根即超限（最长者为其自身）
	longRoot := "/tmp/rd/" + strings.Repeat("x", 300)
	err := validateDiagnosisPathsWithGOOS(longRoot, "/tmp/attempt", "case-7", "copy_evidence", true)
	if err == nil {
		t.Fatal("超预算应报错")
	}
	var tooLong *DiagnosisPathTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("错误类型应为 DiagnosisPathTooLongError: %T", err)
	}
	if tooLong.PathRole != "runtime" || tooLong.Budget != 247 {
		t.Fatalf("角色/预算不符: %v %v", tooLong.PathRole, tooLong.Budget)
	}
	if tooLong.Length <= 247 {
		t.Fatalf("长度应超 247, got %d", tooLong.Length)
	}
	// 文案逐字对齐（品牌键按映射表 jiuwenswarm→uapclawswarm）
	want := "Diagnosis runtime path too long: " + itoa(tooLong.Length) +
		" UTF-16 units exceeds budget 247; case_id=\"case-7\"; stage=\"copy_evidence\"; path=\"" + longRoot +
		"\". Shorten UAPCLAWSWARM_DATA_DIR for task artifacts or TEMP/TMP for diagnosis runtime, " +
		"then restart the service. Existing task references are not relocated automatically."
	if err.Error() != want {
		t.Fatalf("错误文案不符:\nwant=%s\ngot =%s", want, err.Error())
	}
}

// TestValidateDiagnosisPaths_报告最长限制文件 对照 Python max(paths, key=...)。
// Python: diagnosis_paths.py:76-77（报告限制文件而非同样超限的较短父目录）
func TestValidateDiagnosisPaths_报告最长限制文件(t *testing.T) {
	// 根较短（不超限），但某固定文件拼接后超限 → 报告该文件
	root := "/tmp/rd/" + strings.Repeat("r", 200)
	err := validateDiagnosisPathsWithGOOS(root, "/tmp/attempt-x", "", "", true)
	if err == nil {
		t.Fatal("拼接后超限应报错")
	}
	var tooLong *DiagnosisPathTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("错误类型不符: %T", err)
	}
	if tooLong.PathRole != "runtime" {
		t.Fatalf("runtime 先校验: %v", tooLong.PathRole)
	}
	// 触发的路径应为根拼某固定文件（长度 > 根）
	if tooLong.Path == root {
		t.Fatalf("应报告限制文件而非父根: %s", tooLong.Path)
	}
	if !strings.HasPrefix(tooLong.Path, root) {
		t.Fatalf("路径应在根下: %s", tooLong.Path)
	}
}

// TestValidateDiagnosisPaths_固定清单对照 对照 Python _RUNTIME_FILES/_ARCHIVE_FILES。
// Python: diagnosis_paths.py:10-38（清单逐项）
func TestValidateDiagnosisPaths_固定清单对照(t *testing.T) {
	if len(runtimeFiles) != 16 {
		t.Fatalf("runtimeFiles 应 16 项, got %d", len(runtimeFiles))
	}
	if len(archiveFiles) != 8 {
		t.Fatalf("archiveFiles 应 8 项, got %d", len(archiveFiles))
	}
	if invocationFilename != "invocation-00000000000000000000000000000000.jsonl" {
		t.Fatalf("invocation 文件名不符: %s", invocationFilename)
	}
	// 关键成员抽查
	if runtimeFiles[9] != "runtime_evidence/invocation_001.jsonl" {
		t.Fatalf("runtime 清单第 10 项不符: %s", runtimeFiles[9])
	}
	if archiveFiles[7] != "logs/"+invocationFilename {
		t.Fatalf("archive 清单末项不符: %s", archiveFiles[7])
	}
}

// TestUTF16Length_增补字符计两单元 对照 Python encode("utf-16-le") 长度语义。
func TestUTF16Length_增补字符计两单元(t *testing.T) {
	if got := utf16Length("abc"); got != 3 {
		t.Fatalf("ASCII 应 1:1, got %d", got)
	}
	// 😀 为增补字符（U+1F600）→ 2 单元
	if got := utf16Length("a😀b"); got != 4 {
		t.Fatalf("增补字符应计 2 单元, got %d", got)
	}
}

// itoa 整数转字符串（测试辅助）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
