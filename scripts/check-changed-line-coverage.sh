#!/usr/bin/env bash
# 修改文件「改动行」覆盖率门禁（单测与集成共用）。
#
# 为什么按改动行而不是整文件：巨型文件（如 deep_adapter_team.go 4000+ 行）的整文件
# 覆盖率结构性达不到阈值，于是"加豁免"成了唯一可行操作；而豁免一开，真实行为改动
# 就失去回归信号。门禁粒度必须与改动粒度对齐。
#
# 用法:
#   check-changed-line-coverage.sh <coverprofile> <base_ref> <threshold> [exempt_file]
#
# 豁免文件格式（每行一条，# 开头为注释）：
#   path/to/file.go              整文件豁免（确实无法覆盖，如纯进程边界、无集成入口）
#   path/to/file.go#L120-L140    行级豁免（只有这几行无法覆盖）
#
# 统计口径：
#   1. 覆盖块按 (文件:起止位置) 去重取最大计数。多包合并的 profile 里同一块会重复
#      出现（没执行它的测试包记 0），直接累加会把分母放大、覆盖率被稀释到失真。
#   2. 归属按"语句起始行"判定。覆盖块可跨多行，按区间重叠会把整块计入，
#      分母与改动规模脱钩。起始行落在改动行内、且不在豁免行内 → 计入改动行统计。
set -uo pipefail

COVER_FILE="${1:?需要覆盖率文件路径}"
BASE_REF="${2:?需要对比基准提交}"
THRESHOLD="${3:?需要阈值（百分数，如 5 或 85）}"
EXEMPT_FILE="${4:-}"

if [ ! -f "$COVER_FILE" ]; then
  echo "::error::覆盖率文件不存在: ${COVER_FILE}"
  exit 1
fi

# 收集修改的源码文件（排除 _test.go 与 doc.go）
if ! DIFF_OUT=$(git diff --name-only --diff-filter=ACM "$BASE_REF" HEAD -- '*.go' 2>&1); then
  echo "::error::计算修改文件失败（基准 ${BASE_REF}）：${DIFF_OUT}"
  exit 1
fi
CHANGED_FILES=$(echo "$DIFF_OUT" | grep -v '_test\.go$' | grep -v '/doc\.go$' || true)

if [ -z "$CHANGED_FILES" ]; then
  echo "没有检测到修改的源码文件，跳过改动行覆盖率检查"
  exit 0
fi

echo "修改的源码文件："
echo "$CHANGED_FILES"
echo ""

# 覆盖率文件可能上百 MB：先按修改文件过滤一次落到临时文件，避免逐文件全量扫描。
CHANGED_COVER=$(mktemp)
PATTERNS=$(mktemp)
trap 'rm -f "$CHANGED_COVER" "$PATTERNS"' EXIT
while IFS= read -r f; do
  [ -z "$f" ] && continue
  echo "github.com/uapclaw/uapclaw-go/${f}:" >> "$PATTERNS"
done <<< "$CHANGED_FILES"
grep -F -f "$PATTERNS" "$COVER_FILE" > "$CHANGED_COVER" || true
echo "修改文件相关覆盖行数: $(wc -l < "$CHANGED_COVER")"
echo ""

EXEMPT_ENTRIES=""
if [ -n "$EXEMPT_FILE" ] && [ -f "$EXEMPT_FILE" ]; then
  EXEMPT_ENTRIES=$(grep -v '^#' "$EXEMPT_FILE" | grep -v '^$' || true)
fi

# 单文件统计：输出 "整文件总行 整文件已覆盖行 改动行总行 改动行已覆盖行"
#
# 行级展开：把每个覆盖块展开成它覆盖的所有行并取最大计数。
# 不用块起始行归属，是因为 Go 的块起点常落在函数签名行（签名未改、函数体整段重写时
# 起点落在改动区间之外），会把整段新代码判成"未改动"。
stats() {
  local mod_path="$1" ranges="$2" exempt="$3"
  awk -v mod_path="$mod_path" -v ranges="$ranges" -v exempt="$exempt" '
    BEGIN {
      n = split(ranges, lines, "\n")
      for (i = 1; i <= n; i++) {
        if (lines[i] == "") continue
        split(lines[i], se, " ")
        nc++; cs[nc] = se[1] + 0; ce[nc] = se[2] + 0
      }
      m = split(exempt, elines, "\n")
      for (i = 1; i <= m; i++) {
        if (elines[i] == "") continue
        split(elines[i], xr, " ")
        nx++; xs[nx] = xr[1] + 0; xe[nx] = xr[2] + 0
      }
    }
    index($0, mod_path ":") == 1 {
      spec = $0
      sub("^[^:]*:", "", spec)
      split(spec, f, " ")
      loc = f[1]; count = f[3] + 0
      split(loc, ab, ",")
      split(ab[1], s1, ".")
      split(ab[2], e1, ".")
      bs = s1[1] + 0; be = e1[1] + 0
      for (l = bs; l <= be; l++) {
        if (!(l in lc)) lc[l] = count
        else if (count > lc[l]) lc[l] = count
      }
    }
    END {
      total = 0; covered = 0; in_tot = 0; in_cov = 0
      for (k in lc) {
        # 数组下标是字符串，必须显式转数字：否则 "1010" >= 6 会走字典序比较
        ln = k + 0
        total++
        if (lc[k] > 0) covered++
        hit = 0
        for (i = 1; i <= nc; i++) { if (ln >= cs[i] && ln <= ce[i]) { hit = 1; break } }
        if (!hit) continue
        skip = 0
        for (i = 1; i <= nx; i++) { if (ln >= xs[i] && ln <= xe[i]) { skip = 1; break } }
        if (skip) continue
        in_tot++
        if (lc[k] > 0) in_cov++
      }
      printf "%d %d %d %d", total, covered, in_tot, in_cov
    }' "$CHANGED_COVER"
}

HAS_ERROR=false
echo "修改文件「改动行」覆盖率检查结果（阈值 ${THRESHOLD}%）："
echo "| 文件 | 改动行覆盖率 | 整文件（参考） | 状态 |"
echo "|------|--------------|----------------|------|"

for file in $CHANGED_FILES; do
  # 解析豁免：整文件 or 行级
  IS_EXEMPT=false
  EXEMPT_RANGES=""
  if [ -n "$EXEMPT_ENTRIES" ]; then
    while IFS= read -r exempt; do
      [ -z "$exempt" ] && continue
      if [ "$file" = "$exempt" ]; then
        IS_EXEMPT=true
      else
        case "$exempt" in
          "$file"#L*)
            # 支持 path#L120-L140 与 path#L120 两种写法；尾部可能带 L 前缀
            seg=${exempt#*#L}
            rs=${seg%%-*}
            re=${seg##*-}
            re=${re#L}
            EXEMPT_RANGES="${EXEMPT_RANGES}${rs#L} ${re}"$'\n'
            ;;
        esac
      fi
    done <<< "$EXEMPT_ENTRIES"
  fi

  mod_path="github.com/uapclaw/uapclaw-go/${file}"

  # 新文件侧的改动行区间：@@ -a,b +c,d @@ → c .. c+d-1（纯删除不产生新行）
  CHANGED_RANGES=$(git diff -U0 "$BASE_REF" HEAD -- "$file" | awk '
    /^@@/ {
      if (!match($0, /\+[0-9]+(,[0-9]+)?/)) next
      seg = substr($0, RSTART + 1, RLENGTH - 1)
      n = split(seg, parts, ",")
      start = parts[1] + 0
      len = (n > 1) ? parts[2] + 0 : 1
      if (len > 0) printf "%d %d\n", start, start + len - 1
    }')

  read -r total_stmts covered_stmts in_total in_covered <<< "$(stats "$mod_path" "$CHANGED_RANGES" "$EXEMPT_RANGES")"
  total_stmts=${total_stmts:-0}
  covered_stmts=${covered_stmts:-0}
  in_total=${in_total:-0}
  in_covered=${in_covered:-0}

  if [ "$total_stmts" -eq 0 ]; then
    file_pct="-"
  else
    file_pct=$(echo "scale=1; $covered_stmts * 100 / $total_stmts" | bc)
  fi

  if [ "$IS_EXEMPT" = true ]; then
    echo "| \`${file}\` | - | 整文件 ${file_pct}% | 整文件豁免 |"
    echo "::notice file=${file}::整文件已在 ${EXEMPT_FILE} 中豁免"
    continue
  fi

  if [ -z "$CHANGED_RANGES" ]; then
    echo "| \`${file}\` | 无改动行 | 整文件 ${file_pct}% | 跳过 |"
    continue
  fi

  if [ "$in_total" -eq 0 ]; then
    # 改动行没有可执行语句落进覆盖块：要么只改了注释/空行，
    # 要么改动所在的函数从未被执行。用"改动行是否像代码"区分。
    CODE_LINES=0
    while IFS= read -r rng; do
      [ -z "$rng" ] && continue
      s=${rng%% *}; e=${rng##* }
      cnt=$(sed -n "${s},${e}p" "$file" | grep -vcE '^[[:space:]]*(//|$)' || true)
      CODE_LINES=$((CODE_LINES + cnt))
    done <<< "$CHANGED_RANGES"
    if [ "$CODE_LINES" -eq 0 ]; then
      echo "| \`${file}\` | 仅注释/空行 | 整文件 ${file_pct}% | 跳过 |"
      continue
    fi
    echo "| \`${file}\` | 0% | 整文件 ${file_pct}% | 改动行未被执行 |"
    echo "::error file=${file}::改动行未被测试执行（0%），请补充覆盖这些行的测试或登记行级豁免 path#Lstart-Lend"
    HAS_ERROR=true
    continue
  fi

  IN_PCT=$(echo "scale=1; $in_covered * 100 / $in_total" | bc)

  if [ "$(echo "$IN_PCT < ${THRESHOLD}" | bc -l)" = "1" ]; then
    echo "| \`${file}\` | ${IN_PCT}% | 整文件 ${file_pct}% | 覆盖率不足 |"
    echo "::error file=${file}::改动行覆盖率 ${IN_PCT}% 低于 ${THRESHOLD}%（整文件 ${file_pct}%），请补充覆盖改动行的测试"
    HAS_ERROR=true
  else
    echo "| \`${file}\` | ${IN_PCT}% | 整文件 ${file_pct}% | OK |"
  fi
done

if [ "$HAS_ERROR" = true ]; then
  echo ""
  echo "::error::存在「改动行」覆盖率低于 ${THRESHOLD}% 的修改文件，请补充覆盖这些行的测试"
  exit 1
fi

echo ""
echo "改动行覆盖率检查通过（阈值 ${THRESHOLD}%）"
