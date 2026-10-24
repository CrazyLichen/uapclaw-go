package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/reugn/go-quartz/quartz"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeCronExpr 将表达式归一化为 7 域格式（纯补齐，不做 dow 变换）。
// 对齐 Python: normalize_cron_expr (cron_expr.py L12-29)。
// 5 域（分 时 日 月 周）→ 前补 "0 "（秒）后补 " *"（年）；7 域原样返回。
// dow 保持 croniter 约定（0=SUN..6=SAT）——持久化格式与 Python 逐字一致
// （gateway 与 agentserver 共享同一 JSON 文件）；送入 go-quartz 前才做 dow 变换（见 NextFireTime）。
func NormalizeCronExpr(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	n := len(strings.Fields(s))
	if n == 5 {
		return "0 " + s + " *", nil
	}
	if n == 7 {
		return s, nil
	}
	return "", fmt.Errorf("cron_expr must have 5 or 7 fields, got %d fields. 5-field: minute hour day month dow. 7-field (Quartz): second minute hour day month dow year", n)
}

// ISOToSevenFieldCron 将 ISO8601 时间转为 7 域 cron 表达式。
// 对齐 Python: iso_to_seven_field_cron (cron_expr.py L32-49)。
// 无时区信息时按 timezone 参数解释（naive 墙上时间贴时区标签，对齐 Python replace(tzinfo=tz)）。
// 对齐 Python datetime.fromisoformat 接受面：T/空格分隔、可选秒、分数秒、纯日期。
func ISOToSevenFieldCron(atISO string, timezone string) (string, error) {
	s := strings.TrimSpace(atISO)
	if s == "" {
		return "", fmt.Errorf("at_iso is empty")
	}
	if strings.HasSuffix(s, "Z") {
		s = s[:len(s)-1] + "+00:00"
	}
	t, aware, err := parseISOCompatible(s)
	if err != nil {
		return "", err
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return "", err
	}
	var local time.Time
	if aware {
		// 带时区偏移：转换到目标时区（对齐 Python astimezone）
		local = t.In(loc)
	} else {
		// naive：墙上时间直接贴时区标签（对齐 Python replace(tzinfo=tz)）
		u := t.UTC()
		local = time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), u.Minute(), u.Second(), 0, loc)
	}
	// dow 用 ? 通配（Quartz 一次性表达式惯例，对齐 Python 输出 "?"）
	return fmt.Sprintf("%d %d %d %d %d ? %d",
		local.Second(), local.Minute(), local.Hour(), local.Day(), int(local.Month()), local.Year()), nil
}

// ValidateCronExpression 校验表达式（5/7 域）与时区。
// 对齐 Python: validate_cron_expression (cron_expr.py L52-75)（normalize + croniter.is_valid + ZoneInfo）。
// 镜像 croniter is_valid 接受面（实测矩阵）：
//   - dow 数字值域 0-6；7 拒绝（单值/区间/列表含 7 均 reject），步长上下文的 7 容忍（*/7 OK）
//   - dow 名称 SUN-SAT 不区分大小写 OK；dow 域 L 拒绝；dow 域 a#n OK
//   - 月域名称（JAN）拒绝，仅数字 1-12
//   - 日+周同时受限 OK（croniter 标准 cron OR 语义；go-quartz 由 NextFireTime 拆分支持）
//   - 日域 L/W/负偏移 OK；6 域/别名（@daily）拒绝（normalize 层）
func ValidateCronExpression(expr string, timezone string) error {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return fmt.Errorf("cron_expr is empty")
	}
	normalized, err := NormalizeCronExpr(raw)
	if err != nil {
		return err
	}
	if err := validateCronFields(normalized); err != nil {
		return fmt.Errorf("invalid cron expression: '%s'", raw)
	}
	// go-quartz 校验兜底（日+周同时受限时跳过——quartz 拒绝该形态，
	// OR 语义由 NextFireTime 拆分两支取较早者支持）
	if !domDowBothRestricted(normalized) {
		if err := quartz.ValidateCronExpression(transformDow(normalized)); err != nil {
			return fmt.Errorf("invalid cron expression: '%s'", raw)
		}
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid timezone: '%s'", timezone)
	}
	return nil
}

// NextFireTime 计算下一次触发时间。
// 对齐 Python: _cron_next_push_dt (scheduler.py L50-66)。
// expr 为 Python 格式 7 域表达式（croniter dow 约定 0=SUN..6=SAT；调用方负责 NormalizeCronExpr）。
// 内部送 go-quartz 前做 dow 变换（0=SUN..6=SAT → 1=SUN..7=SAT）；
// 日+周同时受限时按 croniter OR 语义拆 dom-only / dow-only 两支取较早者。
// go-quartz ErrTriggerExpired 对应 Python CroniterBadDateError（one-shot 过期）。
func NextFireTime(expr string, base time.Time) (time.Time, error) {
	tokens := strings.Fields(expr)
	if len(tokens) == 7 && domDowBothRestricted(expr) {
		// OR 语义：day 匹配或 dow 匹配的并集 → 两支下次触发的较早者
		branchA := append([]string{}, tokens...) // dom-only：dow 置 ?
		branchA[5] = "?"
		branchB := append([]string{}, tokens...) // dow-only：dom 置 ?
		branchB[3] = "?"
		// 两支均需 dow 变换（branchB 的 dow 域保持 croniter 约定，必须变换后才送 quartz）
		nextA, errA := nextFireTimeQuartz(strings.Join(branchA, " "), base)
		nextB, errB := nextFireTimeQuartz(transformDow(strings.Join(branchB, " ")), base)
		if errA != nil && errB != nil {
			return time.Time{}, errA
		}
		if errA != nil {
			return nextB, nil
		}
		if errB != nil {
			return nextA, nil
		}
		if nextA.Before(nextB) {
			return nextA, nil
		}
		return nextB, nil
	}
	return nextFireTimeQuartz(transformDow(expr), base)
}

// IsNoNextDateError 判断是否为"无下一次日期"错误（one-shot 过期）。
// 对齐 Python: _is_croniter_no_next_date (scheduler.py L231-237)。
func IsNoNextDateError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, quartz.ErrTriggerExpired) ||
		strings.Contains(err.Error(), "failed to find next date")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nextFireTimeQuartz 用 go-quartz 计算下次触发（expr 已是 Quartz 约定）。
func nextFireTimeQuartz(expr string, base time.Time) (time.Time, error) {
	trigger, err := quartz.NewCronTriggerWithLoc(expr, base.Location())
	if err != nil {
		return time.Time{}, err
	}
	nextNano, err := trigger.NextFireTime(base.UnixNano())
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, nextNano).In(base.Location()), nil
}

// parseISOCompatible 对齐 Python datetime.fromisoformat 的接受面
// （T/空格分隔、可选秒、分数秒、纯日期；时区偏移由 RFC3339 布局处理）。
// 第二返回值 aware 表示是否带时区信息（决定 In(loc) 转换还是贴时区标签）。
func parseISOCompatible(s string) (time.Time, bool, error) {
	layouts := []string{
		time.RFC3339Nano,                // 2026-12-28T17:00:00.123+08:00（aware）
		time.RFC3339,                    // 2026-12-28T17:00:00+08:00（aware）
		"2006-01-02T15:04:05.999999999", // 2026-12-28T17:00:00.123（naive）
		"2006-01-02T15:04:05",           // 2026-12-28T17:00:00（naive）
		"2006-01-02T15:04",              // 2026-12-28T17:00（naive）
		"2006-01-02 15:04:05.999999999", // 空格分隔 + 分数秒（naive）
		"2006-01-02 15:04:05",           // 空格分隔（naive）
		"2006-01-02 15:04",              // 空格分隔无秒（naive）
		"2006-01-02",                    // 纯日期（naive）
	}
	var lastErr error
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			aware := layout == time.RFC3339Nano || layout == time.RFC3339
			return t, aware, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, false, fmt.Errorf("invalid at_iso: %w", lastErr)
}

// validateCronFields 逐域校验 7 域表达式（镜像 croniter is_valid 接受面）。
func validateCronFields(normalized string) error {
	tokens := strings.Fields(normalized)
	if len(tokens) != 7 {
		return fmt.Errorf("cron_expr must have 7 fields")
	}
	// 秒/分（0-59）
	if err := validateNumericField(tokens[0], 0, 59); err != nil {
		return fmt.Errorf("second field: %w", err)
	}
	if err := validateNumericField(tokens[1], 0, 59); err != nil {
		return fmt.Errorf("minute field: %w", err)
	}
	// 时（0-23）
	if err := validateNumericField(tokens[2], 0, 23); err != nil {
		return fmt.Errorf("hour field: %w", err)
	}
	// 日（1-31；L/L-n/nW/*/?）
	if err := validateDayOfMonthField(tokens[3]); err != nil {
		return fmt.Errorf("day-of-month field: %w", err)
	}
	// 月（1-12；名称拒绝——对齐 croniter 不支持月名称）
	if err := validateNumericField(tokens[4], 1, 12); err != nil {
		return fmt.Errorf("month field: %w", err)
	}
	// 周（0-6 数字或 SUN-SAT 名称；值域含 7 拒绝；L 拒绝；a#n 前缀 0-6）
	if err := validateDayOfWeekField(tokens[5]); err != nil {
		return fmt.Errorf("day-of-week field: %w", err)
	}
	// 年（数字或 *；croniter 无上界约束且接受通配）
	if tokens[6] != "*" {
		if _, err := strconv.Atoi(tokens[6]); err != nil {
			return fmt.Errorf("year field: %q", tokens[6])
		}
	}
	return nil
}

// validateNumericField 校验通用数字域（*/n、a-b、a/n、列表、单值；min-max）。
func validateNumericField(field string, min, max int) error {
	if field == "*" || field == "?" {
		return nil
	}
	for _, part := range strings.Split(field, ",") {
		if err := validateNumericPart(part, min, max); err != nil {
			return err
		}
	}
	return nil
}

// validateNumericPart 校验单个数字域片段（支持步长/区间/单值）。
func validateNumericPart(part string, min, max int) error {
	// 步长 a/n 或 */n（步长值不参与值域校验——croniter 对步长上限宽容，如 dow */7 合法）
	if idx := strings.Index(part, "/"); idx >= 0 {
		prefix := part[:idx]
		step := part[idx+1:]
		if _, err := strconv.Atoi(step); err != nil || step == "" {
			return fmt.Errorf("invalid step %q", part)
		}
		if prefix == "*" {
			return nil
		}
		return validateNumericPart(prefix, min, max)
	}
	// 区间 a-b
	if idx := strings.Index(part, "-"); idx > 0 {
		lo, loErr := strconv.Atoi(part[:idx])
		hi, hiErr := strconv.Atoi(part[idx+1:])
		if loErr != nil || hiErr != nil {
			return fmt.Errorf("invalid range %q", part)
		}
		if lo < min || hi > max || lo > hi {
			return fmt.Errorf("range %q out of [%d,%d]", part, min, max)
		}
		return nil
	}
	// 单值
	v, err := strconv.Atoi(part)
	if err != nil {
		return fmt.Errorf("invalid value %q", part)
	}
	if v < min || v > max {
		return fmt.Errorf("value %q out of [%d,%d]", part, min, max)
	}
	return nil
}

// validateDayOfMonthField 校验日域（1-31；L/L-n/nW；* / ?）。
func validateDayOfMonthField(field string) error {
	if field == "*" || field == "?" {
		return nil
	}
	for _, part := range strings.Split(field, ",") {
		// L / L-n：月末/倒数第 n 天
		if part == "L" || strings.HasPrefix(part, "L-") {
			continue
		}
		// nW：最近工作日
		if strings.HasSuffix(part, "W") {
			if _, err := strconv.Atoi(strings.TrimSuffix(part, "W")); err == nil {
				continue
			}
		}
		if err := validateNumericPart(part, 1, 31); err != nil {
			return err
		}
	}
	return nil
}

// validateDayOfWeekField 校验周域（数字 0-6；名称 SUN-SAT 不区分大小写；L 拒绝；a#n 前缀 0-6）。
func validateDayOfWeekField(field string) error {
	if field == "*" || field == "?" {
		return nil
	}
	for _, part := range strings.Split(field, ",") {
		// a#n：每月第 n 个星期 a（前缀 0-6 数字或名称）
		if idx := strings.Index(part, "#"); idx > 0 {
			prefix := part[:idx]
			n, nErr := strconv.Atoi(part[idx+1:])
			if nErr != nil {
				return fmt.Errorf("invalid # %q", part)
			}
			if n < 1 || n > 5 {
				return fmt.Errorf("invalid # %q", part)
			}
			if err := validateDowValue(prefix); err != nil {
				return err
			}
			continue
		}
		// L 形态拒绝（对齐 croniter is_valid）
		if part == "L" || strings.HasPrefix(part, "L") {
			return fmt.Errorf("L is not allowed in day-of-week")
		}
		// 步长 a/n 或 */n：步长不参与值域校验
		if idx := strings.Index(part, "/"); idx >= 0 {
			prefix := part[:idx]
			if _, err := strconv.Atoi(part[idx+1:]); err != nil {
				return fmt.Errorf("invalid step %q", part)
			}
			if prefix == "*" {
				continue
			}
			if err := validateDowPart(prefix); err != nil {
				return err
			}
			continue
		}
		if err := validateDowPart(part); err != nil {
			return err
		}
	}
	return nil
}

// validateDowPart 校验周域片段（区间或单值/名称）。
func validateDowPart(part string) error {
	if idx := strings.Index(part, "-"); idx > 0 {
		// 区间：两端各自校验（数字 0-6 或名称）
		if err := validateDowValue(part[:idx]); err != nil {
			return err
		}
		return validateDowValue(part[idx+1:])
	}
	return validateDowValue(part)
}

// validateDowValue 校验周域单值（数字 0-6——含 7 拒绝；名称 SUN-SAT 不区分大小写）。
func validateDowValue(v string) error {
	if isDayName(strings.ToUpper(v)) {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("invalid dow value %q", v)
	}
	if n < 0 || n > 6 {
		return fmt.Errorf("dow value %q out of [0,6]", v)
	}
	return nil
}

// domDowBothRestricted 判断 7 域表达式的日域与周域是否同时受限（非 * 非 ?）。
// croniter 标准 cron 语义为 OR（并集）；go-quartz 拒绝该形态，需拆分支持。
func domDowBothRestricted(expr string) bool {
	tokens := strings.Fields(expr)
	if len(tokens) != 7 {
		return false
	}
	dom := tokens[3]
	dow := tokens[5]
	return dom != "*" && dom != "?" && dow != "*" && dow != "?"
}

// transformDow 将 croniter dow 约定（0=SUN..6=SAT）变换为 Quartz 约定（1=SUN..7=SAT）。
// 变换规则：
//   - 单值 v(0..6) → v+1（0=SUN→1、6=SAT→7）；v==7 容错 → 1（对齐 Python ctor 的 7=SUN 别名）
//   - 区间 a-b → (a+1)-(b+1)
//   - 步长（a/n）前缀变换、步长保持；*/n 不变（两边步长语义一致）
//   - a#n 前缀变换、#n 保持
//   - 名称（SUN/MON/...）不变——两种约定名称映射一致
//
// 该变换只处理第 6 个域（dow），其他域原样。
func transformDow(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 7 {
		return expr
	}
	fields[5] = transformDowField(fields[5])
	return strings.Join(fields, " ")
}

// transformDowField 变换单个 dow 域。
func transformDowField(field string) string {
	if field == "*" || field == "?" {
		return field
	}
	// 名称（SUN/MON/...）归大写不变——两种约定名称映射一致
	upper := strings.ToUpper(field)
	if isDayName(upper) {
		return upper
	}
	// 步长（*/n 或 a/n）：前缀部分变换，步长保持
	if idx := strings.Index(field, "/"); idx > 0 {
		prefix := transformDowField(field[:idx])
		return prefix + field[idx:]
	}
	// 每月第 n 周（a#n）：前缀变换，#n 保持
	if idx := strings.Index(field, "#"); idx > 0 {
		prefix := transformDowField(field[:idx])
		return prefix + field[idx:]
	}
	// 区间 a-b
	if idx := strings.Index(field, "-"); idx > 0 {
		lo, loErr := strconv.Atoi(field[:idx])
		hi, hiErr := strconv.Atoi(field[idx+1:])
		if loErr == nil && hiErr == nil {
			return strconv.Itoa(shiftDowValue(lo)) + "-" + strconv.Itoa(shiftDowValue(hi))
		}
	}
	// 列表 a,b,c
	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, transformDowField(p))
		}
		return strings.Join(out, ",")
	}
	// 单值
	if v, err := strconv.Atoi(field); err == nil {
		return strconv.Itoa(shiftDowValue(v))
	}
	return field
}

// shiftDowValue 单值变换：0..6 → 1..7（0=SUN→1=SUN）；7 容错为 1（对齐 Python croniter ctor 的 7=SUN 别名）；越界原样。
// 注：Python validate 层已拒绝 7（is_valid 值域 0-6），此处仅容错已持久化的越界数据。
func shiftDowValue(v int) int {
	if v < 0 || v > 7 {
		return v
	}
	if v == 7 {
		return 1
	}
	return v + 1
}

// isDayName 判断是否为星期名称。
func isDayName(s string) bool {
	switch s {
	case "SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT":
		return true
	}
	return false
}
