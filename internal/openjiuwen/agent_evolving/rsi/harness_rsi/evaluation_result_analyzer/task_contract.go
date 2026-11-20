// task_contract.go 提供确定性任务条款切分与精确引用兼容校验。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/task_contract.py（全量对齐）。

package evaluationresultanalyzer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTaskClauses 索引非空段落，保留原文与行号区间。
// 返回元素为 map（偏差 D-6：Python dict 动态键）：
// clause_id / line_start / line_end / text。
// Python: task_contract.py:11-29 build_task_clauses
func BuildTaskClauses(task string) []map[string]any {
	// Python: lines = task.splitlines(keepends=True)
	lines := splitLinesKeepends(task)
	var clauses []map[string]any
	start := 0
	for index := 0; index <= len(lines); index++ {
		// Python: if index < len(lines) and lines[index].strip(): continue
		if index < len(lines) && strings.TrimSpace(lines[index]) != "" {
			continue
		}
		// Python: if start < index: 组装条款
		if start < index {
			text := strings.Join(lines[start:index], "")
			// Python: text.rstrip("\r\n")
			text = strings.TrimRight(text, "\r\n")
			digest := sha256.Sum256([]byte(text))
			shortDigest := hex.EncodeToString(digest[:])[:10]
			clauses = append(clauses, map[string]any{
				// Python: f"clause_{start + 1:04d}_{digest}"
				"clause_id":  fmt.Sprintf("clause_%04d_%s", start+1, shortDigest),
				"line_start": start + 1,
				"line_end":   index,
				"text":       text,
			})
		}
		start = index + 1
	}
	return clauses
}

// NormalizeContractChecks 解析条款 ID、回填引用原文、区分引用/模式错误与冲突错误。
// 返回错误清单（元素为 Python 错误文案的逐字对齐）。
// Python: task_contract.py:32-77 normalize_contract_checks
func NormalizeContractChecks(diagnosis map[string]any, task string) []string {
	// Python: checks = diagnosis.get("task_contract_checks")
	//         if checks is None and "task_contract_checks" not in diagnosis: return []
	checksValue, hasKey := diagnosis["task_contract_checks"]
	if !hasKey {
		return nil
	}
	// Python: if not isinstance(checks, list): return ["[contract_schema] ..."]
	checks, isList := checksValue.([]any)
	if !isList {
		return []string{"[contract_schema] task_contract_checks must be a list"}
	}

	clauses := BuildTaskClauses(task)
	// Python: by_id = {clause["clause_id"]: clause for clause in clauses}
	byID := map[string]map[string]any{}
	idOrder := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		id := clause["clause_id"].(string)
		byID[id] = clause
		idOrder = append(idOrder, id)
	}

	var errors []string
	for index, entry := range checks {
		field := fmt.Sprintf("task_contract_checks[%d]", index)
		check, isMap := entry.(map[string]any)
		if !isMap {
			errors = append(errors, fmt.Sprintf("[contract_schema] %s must be an object", field))
			continue
		}

		clauseIDValue, hasClauseID := check["clause_id"]
		clauseID, clauseIDIsStr := clauseIDValue.(string)
		var clause map[string]any
		if hasClauseID && clauseIDIsStr {
			clause = byID[clauseID]
		}
		// Python: if clause_id is None: 尝试 task_quote 唯一精确匹配
		if !hasClauseID {
			quote, quoteIsStr := check["task_quote"].(string)
			if quoteIsStr && strings.TrimSpace(quote) != "" && strings.Count(task, quote) == 1 {
				var matches []map[string]any
				for _, item := range clauses {
					itemText := item["text"].(string)
					if strings.Contains(itemText, strings.TrimRight(quote, "\r\n")) {
						matches = append(matches, item)
					}
				}
				if len(matches) == 1 {
					clause = matches[0]
				} else {
					clause = nil
				}
			}
		}

		if clause == nil {
			// Python: available = ", ".join(list(by_id)[:30])
			limit := len(idOrder)
			if limit > 30 {
				limit = 30
			}
			available := strings.Join(idOrder[:limit], ", ")
			errors = append(errors, fmt.Sprintf(
				"[contract_citation] %s.clause_id must select a supplied task clause; "+
					"legacy task_quote requires one exact unique match. Available IDs (first 30): %s",
				field, available))
		} else {
			// Python: check["clause_id"] / task_quote / line_start / line_end 回填
			check["clause_id"] = clause["clause_id"]
			check["task_quote"] = clause["text"]
			check["line_start"] = clause["line_start"]
			check["line_end"] = clause["line_end"]
		}

		// Python: interpretation 校验
		interpretation, interpIsStr := check["interpretation"].(string)
		if !interpIsStr || strings.TrimSpace(interpretation) == "" {
			errors = append(errors, fmt.Sprintf(
				"[contract_schema] %s.interpretation must state the relevant guarantee", field))
		}

		// Python: recommendation_preserves_contract 布尔校验
		preserves, preservesIsBool := check["recommendation_preserves_contract"].(bool)
		if !preservesIsBool {
			errors = append(errors, fmt.Sprintf(
				"[contract_schema] %s.recommendation_preserves_contract is required (boolean)", field))
		} else if !preserves {
			errors = append(errors, fmt.Sprintf(
				"[contract_conflict] %s.recommendation_preserves_contract "+
					"must preserve the original contract", field))
		}
	}
	return errors
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitLinesKeepends 等价 Python str.splitlines(keepends=True)：
// 保留行界符的行切分（对齐审查要点：splitlines 行界语义）。
// Python 行界集更宽（\v\f\u2028 等）；任务文本场景按 \r\n / \r / \n 处理
// （偏差 D-7：splitlines 宽行界集收窄，回收条件 17.4b2 出现宽行界输入时补齐）。
func splitLinesKeepends(task string) []string {
	if task == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(task); i++ {
		switch task[i] {
		case '\n':
			lines = append(lines, task[start:i+1])
			start = i + 1
		case '\r':
			// \r\n 合并为一个行界；孤立 \r 也算行界
			if i+1 < len(task) && task[i+1] == '\n' {
				continue
			}
			lines = append(lines, task[start:i+1])
			start = i + 1
		}
	}
	if start < len(task) {
		lines = append(lines, task[start:])
	}
	return lines
}
