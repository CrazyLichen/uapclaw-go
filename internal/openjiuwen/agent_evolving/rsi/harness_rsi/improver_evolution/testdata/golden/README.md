# 17.4e1 黄金样例（方案 α）

生成方式（Python 真实现，绕过包 `__init__` 直接按文件加载两模块，仅 stdlib+yaml 依赖）：

```bash
python3 /tmp/gen_golden_17_4e1.py <go_repo_root>
# 脚本内 _SRC = /home/opensource/agent-core/openjiuwen/rsi/harness_rsi/improver_evolution
```

## 文件清单

| 文件 | 用途 |
|---|---|
| `feedback_analysis_input_1.yaml` | 3 cohort 输入：001 可比较（重复指纹+surface 失败+top_m 外高点+partial repair+feature 证据）、002 可比较（全负增益+重复指纹+not_triggered）、003 单候选不可比较 |
| `feedback_analysis_expected_1.json` | `analyze_candidate_feedback_ledgers(..., min_support_cohorts=2, high_value_gain_threshold=0.1)` 输出（sort_keys+indent=2 仅人读；跨语言比对用 expected_compact_hashes） |
| `digest_record_1.json` | cohorts 紧凑 canonical 串 + `training_ledger_digest`（Go `marshalCanonical` 逐字节比对锚点） |
| `meta_validation_input_{baseline,candidate}.json` | 4 个成对 meta_test checkpoint（candidate 全指标占优 → live eligible） |
| `meta_validation_expected_{offline_rerank,live_generation}.json` | `paired_meta_validate` 双模式输出（offline=inconclusive/ranker_evidence_only；live=accepted/complete/eligible） |
| `expected_compact_hashes.json` | 三份 expected 的 `json.dumps(sort_keys,separators=(",",":"),ensure_ascii=False)` sha256（D-e1-3 float 序列化实证锚点） |

## D-e1-3 实证记录（2026-12-05）

- digest（cohort 输入）：`sha256:b17c12ae03837e91f1d633eaff3ae4c6394b28ec6a9a0bbb0c257f8fff7a26d9`
- expected 紧凑 hash：见 `expected_compact_hashes.json`
- 输入数值特征：含负小数（-0.05/-0.1）、12 位舍入候选值（宏平均 delta）、整数预算（100000，JSON int 非 float）
