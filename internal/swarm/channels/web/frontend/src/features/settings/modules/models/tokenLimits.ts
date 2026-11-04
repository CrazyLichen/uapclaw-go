/**
 * 模型 token 上限草稿的解析与格式化。
 *
 * 后端约定（Go `config_apply.go` 与 `models.list` 扁平化）：`max_input_tokens`
 * 与 `max_output_tokens` 落在模型条目的 `model_client_config` 内，0 表示未配置。
 * 草稿层统一用字符串承载：空串即「未配置」，保存时转 0，回填时 0/缺省还原为空串。
 */

const NON_NEGATIVE_INTEGER_PATTERN = /^\d+$/;

/**
 * 解析 token 上限草稿。
 *
 * 空串/null/undefined 表示未配置，返回 0；非负整数返回其数值；
 * 其余（负数、小数、字母）返回 null，由调用方报校验错误。
 */
export function parseTokenLimitDraft(value: unknown): number | null {
  if (value === null || value === undefined) return 0;
  const normalized = String(value).trim();
  if (normalized === '') return 0;
  if (!NON_NEGATIVE_INTEGER_PATTERN.test(normalized)) return null;
  const parsed = Number(normalized);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

/** 后端数值转草稿字符串：0/负数/非数字一律视为未配置，显示为空串。 */
export function formatTokenLimitDraft(value: unknown): string {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '';
  return String(Math.trunc(value));
}
