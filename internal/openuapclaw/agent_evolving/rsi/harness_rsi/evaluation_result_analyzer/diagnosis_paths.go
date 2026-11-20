// diagnosis_paths.go 在拷贝证据或调用模型前预算固定的诊断文件路径。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/diagnosis_paths.py（全量对齐）。
// 偏差 D-3：os.name == "nt" → runtime.GOOS == "windows"；预算函数为纯函数可注入。

package evaluationresultanalyzer

import (
	"fmt"
	"runtime"
	"unicode/utf16"
)

// ──────────────────────────── 常量 ────────────────────────────

// windowsPathBudget 便携式 Windows 路径预算（UTF-16 单元）。
// Python: diagnosis_paths.py:9 _WINDOWS_PATH_BUDGET
const windowsPathBudget = 247

// invocationFilename 固定调用日志文件名。
// Python: diagnosis_paths.py:10 _INVOCATION_FILENAME = "invocation-" + "0" * 32 + ".jsonl"
const invocationFilename = "invocation-" + "00000000000000000000000000000000" + ".jsonl"

// runtimeFiles runtime 目录的固定生成文件清单。
// Python: diagnosis_paths.py:11-28 _RUNTIME_FILES
var runtimeFiles = []string{
	"evidence_manifest.json",
	"runtime_log_search.json",
	"task_clauses.json",
	"execution_history.json",
	"evidence_summary.md",
	"repository_snapshot.json",
	"source_patch.diff",
	"runtime_evidence/runtime_config.json",
	"runtime_evidence/termination.json",
	"runtime_evidence/invocation_001.jsonl",
	"current_harness/index.json",
	"current_harness/role_001/runtime.yaml",
	"current_harness/role_001/prompt_001.json",
	"current_harness/role_001/skill_001.md",
	"current_harness/role_001/tools_001.py",
	"current_harness/role_001/rails_001.py",
}

// archiveFiles archive 目录的固定生成文件清单。
// Python: diagnosis_paths.py:29-38 _ARCHIVE_FILES
var archiveFiles = []string{
	"attempt.json",
	"evidence/evidence_manifest.json",
	"evidence/runtime_log_search.json",
	"evidence/task_clauses.json",
	"evidence/runtime_evidence/runtime_config.json",
	"evidence/runtime_evidence/termination.json",
	"evidence/runtime_evidence/invocation_001.jsonl",
	"logs/" + invocationFilename,
}

// ──────────────────────────── 结构体 ────────────────────────────

// DiagnosisPathTooLongError 生成的诊断路径超出便携式 Windows 预算。
// Python: diagnosis_paths.py:41-53 DiagnosisPathTooLongError(OSError)。
type DiagnosisPathTooLongError struct {
	// PathRole 路径角色（runtime / archive）
	PathRole string
	// Length 实际 UTF-16 单元长度
	Length int
	// Budget 预算值
	Budget int
	// Path 触发的具体路径（Python str(path)!r 语义）
	Path string
	// message 错误全文（含提示文案）
	message string
}

// Error 实现 error 接口（文案逐字对齐 Python，品牌键按映射表替换）。
func (e *DiagnosisPathTooLongError) Error() string {
	return e.message
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateDiagnosisPaths 校验固定生成文件路径（排除任意仓库/包目录树）。
// 调用方提供绝对路径。UTF-16 计数将增补字符计为两单元（对齐 Windows 路径
// 而非 Python 码点计数）。预算即使在支持长路径的主机上也刻意保守。
// Python: diagnosis_paths.py:56-80 validate_diagnosis_paths
func ValidateDiagnosisPaths(runtimeDir, attemptDir string, caseID, sourceStage string) error {
	// D-3：GOOS 门卫（测试经诊断注入点覆盖双平台值）
	return validateDiagnosisPathsWithGOOS(runtimeDir, attemptDir, caseID, sourceStage, isWindowsGOOS())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isWindowsGOOS GOOS 判断（测试注入点；规范 3.3：可 mock 代码禁止 build tag）。
func isWindowsGOOS() bool { return runtime.GOOS == "windows" }

// validateDiagnosisPathsWithGOOS 带平台注入的校验实现。
// Python: diagnosis_paths.py:69-80
func validateDiagnosisPathsWithGOOS(runtimeDir, attemptDir, caseID, sourceStage string, isWindows bool) error {
	// Python: if not _IS_WINDOWS: return
	if !isWindows {
		return nil
	}
	// Python: for path_role, root, filenames in ((runtime, ...), (archive, ...))
	for _, spec := range []struct {
		pathRole  string
		root      string
		filenames []string
	}{
		{"runtime", runtimeDir, runtimeFiles},
		{"archive", attemptDir, archiveFiles},
	} {
		// Python: paths = [root, *(root / filename for filename in filenames)]
		paths := []string{spec.root}
		for _, filename := range spec.filenames {
			paths = append(paths, joinPathPortable(spec.root, filename))
		}
		// Python: path = max(paths, key=lambda item: len(str(item).encode("utf-16-le")))
		// 报告最长的限制文件而非同样超限的较短父目录
		limiter := paths[0]
		limiterLen := utf16Length(limiter)
		for _, p := range paths[1:] {
			if l := utf16Length(p); l > limiterLen {
				limiter = p
				limiterLen = l
			}
		}
		// Python: length = len(str(path).encode("utf-16-le")) // 2
		// （UTF-16LE 字节数 // 2 == UTF-16 单元数，与 utf16Length 直接等值）
		length := limiterLen
		if length > windowsPathBudget {
			// Python: raise DiagnosisPathTooLongError(...)（文案逐字对齐，jiuwenswarm→uapclawswarm）
			return &DiagnosisPathTooLongError{
				PathRole: spec.pathRole,
				Length:   length,
				Budget:   windowsPathBudget,
				Path:     limiter,
				message: fmt.Sprintf("Diagnosis %s path too long: %d UTF-16 units exceeds budget "+
					"%d; case_id=%q; stage=%q; path=%q. "+
					"Shorten UAPCLAWSWARM_DATA_DIR for task artifacts or TEMP/TMP for diagnosis runtime, "+
					"then restart the service. Existing task references are not relocated automatically.",
					spec.pathRole, length, windowsPathBudget, caseID, sourceStage, limiter),
			}
		}
	}
	return nil
}

// utf16Length 计算字符串的 UTF-16 单元数（增补字符计 2 单元）。
func utf16Length(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// joinPathPortable 便携路径拼接（Windows 分隔符语义按 Go filepath 由注入方保证）。
// Python: root / filename
func joinPathPortable(root, filename string) string {
	return root + "/" + filename
}
