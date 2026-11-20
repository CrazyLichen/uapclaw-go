package infra

import (
	"context"
	"regexp"
	"strings"
)

// testFileRe 验证输出中的测试文件正则（对齐 _TEST_FILE_RE）
var testFileRe = regexp.MustCompile(`(tests/(?:unit_tests|system_tests)/[^\s:'"]+\.py)`)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsDocumentationFile 判断 path 是否指向 docs/ 下的 markdown 文件
// （对齐 is_documentation_file）
func IsDocumentationFile(path string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	return strings.HasPrefix(normalized, "docs/") && strings.HasSuffix(normalized, ".md")
}

// IsAllowedDocumentationFile 判断文档文件是否在允许的 docs 布局内
// （对齐 is_allowed_documentation_file）
func IsAllowedDocumentationFile(ctx context.Context, path string) bool {
	return IsDocumentationFile(path) && IsAllowedRepoEditPath(ctx, path)
}

// DeriveTestFiles 从声明的源文件派生候选测试文件（对齐 derive_test_files）
func DeriveTestFiles(taskFiles []string) []string {
	var derived []string

	for _, path := range taskFiles {
		normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
		if normalized == "" || !strings.HasSuffix(normalized, ".py") {
			continue
		}
		if strings.HasPrefix(normalized, "tests/") {
			continue
		}
		if posixBase(normalized) == "__init__.py" {
			continue
		}

		stem := posixStem(normalized)
		testName := "test_" + stem + ".py"
		derived = append(derived,
			"tests/unit_tests/**/"+testName,
			"tests/system_tests/**/"+testName,
		)
	}

	return dedupePreserveOrder(derived)
}

// IsDerivedTestFile 检查 candidate 是否符合 source→test 映射规则
// （对齐 is_derived_test_file）
func IsDerivedTestFile(sourceFiles []string, candidate string) bool {
	normalizedCandidate := strings.ReplaceAll(strings.TrimSpace(candidate), "\\", "/")
	if !strings.HasPrefix(normalizedCandidate, "tests/") {
		return false
	}
	candidateName := posixBase(normalizedCandidate)
	if !strings.HasPrefix(candidateName, "test_") {
		return false
	}

	for _, path := range sourceFiles {
		normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
		if !isNonTestSourceFile(normalized) {
			continue
		}
		stem := posixStem(normalized)
		if candidateName == "test_"+stem+".py" {
			return true
		}
	}
	return false
}

// ExtractVerifyRelatedFiles 从验证输出提取显式提及的测试文件路径
// （对齐 extract_verify_related_files）
func ExtractVerifyRelatedFiles(ciResult map[string]any, fixLogs string) []string {
	var texts []string
	if ciResult != nil {
		texts = append(texts, anyToString(ciResult["errors"]))
		for _, gate := range itemList(ciResult["gates"]) {
			if gm, ok := gate.(map[string]any); ok {
				texts = append(texts, anyToString(gm["output"]))
			}
		}
	}
	if fixLogs != "" {
		texts = append(texts, fixLogs)
	}

	var files []string
	for _, text := range texts {
		files = append(files, testFileRe.FindAllString(text, -1)...)
	}
	return dedupePreserveOrder(files)
}

// DeriveLegacyRelatedTestFiles 仅当编辑过且被验证输出直接引用时放行适配的遗留测试
// （对齐 derive_legacy_related_test_files）
func DeriveLegacyRelatedTestFiles(editedFiles, verifyRelatedFiles []string) []string {
	verifySet := map[string]bool{}
	for _, path := range verifyRelatedFiles {
		if s := strings.TrimSpace(path); s != "" {
			verifySet[strings.ReplaceAll(s, "\\", "/")] = true
		}
	}
	var related []string
	for _, path := range editedFiles {
		normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
		if strings.HasPrefix(normalized, "tests/") && verifySet[normalized] {
			related = append(related, normalized)
		}
	}
	return dedupePreserveOrder(related)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isNonTestSourceFile 判断是否普通源文件（对齐 _is_non_test_source_file）
func isNonTestSourceFile(path string) bool {
	if path == "" {
		return false
	}
	if !strings.HasSuffix(path, ".py") {
		return false
	}
	if strings.HasPrefix(path, "tests/") {
		return false
	}
	return posixBase(path) != "__init__.py"
}

// posixBase POSIX 路径末段（对齐 PurePosixPath.name；保持 / 分隔）
func posixBase(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// posixStem POSIX 路径去扩展名（对齐 PurePosixPath.stem）
func posixStem(path string) string {
	base := posixBase(path)
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

// anyToString 任意值转字符串（对齐 str(...)；nil → ""）
func anyToString(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	default:
		return jsonStrOf(v)
	}
}
