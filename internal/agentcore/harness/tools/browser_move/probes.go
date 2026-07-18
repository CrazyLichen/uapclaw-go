package browser_move

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildInteractiveProbeJS 构建交互元素探测的 JavaScript 代码。
// 供 browser_run_code 调用，返回紧凑可见交互元素列表。
//
// 对齐 Python: build_interactive_probe_js (probes.py L18-281)
// JS 代码逐字符复制 Python 原文，不做自行翻译
// JS 反引号在 Go 反引号字符串中用 JS_BACKTICK 占位，运行时替换
func BuildInteractiveProbeJS(maxItems int, viewportOnly bool, query string) string {
	params := map[string]any{
		"max_items":     clampInt(maxItems, 1, 100),
		"viewport_only": viewportOnly,
		"query":         strings.TrimSpace(strings.ToLower(query)),
	}
	paramsJSON, _ := json.Marshal(params)

	// 对齐 Python: probes.py L33-281 的 f-string 模板
	// JS 代码逐字符复制 Python 原文，JS_BACKTICK 运行时替换为反引号
	js := fmt.Sprintf(`async (page) => {{
  const params = %s;

  return await page.evaluate((params) => {{
    const maxItems = Math.max(1, Math.min(Number(params.max_items || 50), 100));
    const viewportOnly = params.viewport_only !== false;
    const query = String(params.query || '').trim().toLowerCase();

    const selectors = [
      'button',
      'a[href]',
      'input',
      'select',
      'textarea',
      '[role="button"]',
      '[role="link"]',
      '[role="textbox"]',
      '[role="checkbox"]',
      '[role="radio"]',
      '[aria-label]',
      '[data-testid]',
      '[data-test]',
      '[data-cy]'
    ];

    const normalize = (value, limit = 140) => {{
      return String(value || '')
        .replace(/\\s+/g, ' ')
        .trim()
        .slice(0, limit);
    }};

    const attrEscape = (value) => {{
      return String(value || '').replace(/\\\\/g, '\\\\\\\\').replace(/"/g, '\\\\"');
    }};

    const cssEscape = (value) => {{
      const raw = String(value || '');
      if (window.CSS && typeof window.CSS.escape === 'function') {{
        return window.CSS.escape(raw);
      }}
      return raw.replace(/[^a-zA-Z0-9_-]/g, '\\\\$&');
    }};

    const roleFromTag = (el) => {{
      const explicit = el.getAttribute('role');
      if (explicit) return explicit;

      const tag = el.tagName.toLowerCase();
      const type = String(el.getAttribute('type') || '').toLowerCase();

      if (tag === 'button') return 'button';
      if (tag === 'a') return 'link';
      if (tag === 'select') return 'combobox';
      if (tag === 'textarea') return 'textbox';
      if (tag === 'input') {{
        if (type === 'checkbox') return 'checkbox';
        if (type === 'radio') return 'radio';
        if (type === 'submit' || type === 'button') return 'button';
        return 'textbox';
      }}
      return '';
    }};

    const isVisible = (el, rect) => {{
      if (!el || !rect) return false;
      if (rect.width < 2 || rect.height < 2) return false;

      const style = window.getComputedStyle(el);
      if (!style) return false;
      if (style.display === 'none') return false;
      if (style.visibility === 'hidden') return false;
      if (Number(style.opacity) === 0) return false;

      if (viewportOnly) {{
        if (rect.bottom < 0) return false;
        if (rect.right < 0) return false;
        if (rect.top > window.innerHeight) return false;
        if (rect.left > window.innerWidth) return false;
      }}

      return true;
    }};

    const buildSelectorHint = (el) => {{
      const tag = el.tagName.toLowerCase();

      const testid =
        el.getAttribute('data-testid') ||
        el.getAttribute('data-test') ||
        el.getAttribute('data-cy');
      if (testid) {{
        if (el.getAttribute('data-testid')) return JS_BACKTICK[data-testid="${{attrEscape(testid)}}"]JS_BACKTICK;
        if (el.getAttribute('data-test')) return JS_BACKTICK[data-test="${{attrEscape(testid)}}"]JS_BACKTICK;
        return JS_BACKTICK[data-cy="${{attrEscape(testid)}}"]JS_BACKTICK;
      }}

      const id = el.getAttribute('id');
      if (id) return JS_BACKTICK#${{cssEscape(id)}}JS_BACKTICK;

      const aria = el.getAttribute('aria-label');
      if (aria) return JS_BACKTICK${{tag}}[aria-label="${{attrEscape(aria)}}"]JS_BACKTICK;

      const name = el.getAttribute('name');
      if (name) return JS_BACKTICK${{tag}}[name="${{attrEscape(name)}}"]JS_BACKTICK;

      const placeholder = el.getAttribute('placeholder');
      if (placeholder) return JS_BACKTICK${{tag}}[placeholder="${{attrEscape(placeholder)}}"]JS_BACKTICK;

      const path = [];
      let node = el;
      let depth = 0;

      while (node && node.nodeType === Node.ELEMENT_NODE && depth < 4) {{
        const nodeTag = node.tagName.toLowerCase();
        let index = 1;
        let prev = node.previousElementSibling;
        while (prev) {{
          if (prev.tagName.toLowerCase() === nodeTag) index += 1;
          prev = prev.previousElementSibling;
        }}
        path.unshift(JS_BACKTICK${{nodeTag}}:nth-of-type(${{index}})JS_BACKTICK);
        node = node.parentElement;
        depth += 1;
      }}

      return path.join(' > ');
    }};

    const elementText = (el) => {{
      const tag = el.tagName.toLowerCase();
      if (tag === 'input' || tag === 'textarea') {{
        return normalize(el.value || el.getAttribute('value') || '');
      }}
      return normalize(el.innerText || el.textContent || '');
    }};

    const accessibleName = (el) => {{
      return normalize(
        el.getAttribute('aria-label') ||
        el.getAttribute('title') ||
        el.getAttribute('placeholder') ||
        el.getAttribute('alt') ||
        ''
      );
    }};

    const scoreElement = (el, rect, text, name) => {{
      let score = 0;
      const tag = el.tagName.toLowerCase();
      const role = roleFromTag(el);

      if (el.getAttribute('data-testid')) score += 40;
      if (el.getAttribute('data-test') || el.getAttribute('data-cy')) score += 30;
      if (el.getAttribute('aria-label')) score += 25;
      if (tag === 'button') score += 25;
      if (tag === 'input' || tag === 'select' || tag === 'textarea') score += 22;
      if (tag === 'a') score += 18;
      if (role) score += 15;
      if (text) score += Math.min(20, text.length / 4);
      if (name) score += Math.min(15, name.length / 5);

      if (rect.top >= 0 && rect.top <= window.innerHeight) score += 15;
      if (rect.left >= 0 && rect.left <= window.innerWidth) score += 5;

      if (el.disabled || el.getAttribute('aria-disabled') === 'true') score -= 50;

      return score;
    }};

    const all = Array.from(document.querySelectorAll(selectors.join(',')));
    const seen = new Set();
    const candidates = [];

    for (const el of all) {{
      if (!el || seen.has(el)) continue;
      seen.add(el);

      const rect = el.getBoundingClientRect();
      if (!isVisible(el, rect)) continue;

      const tag = el.tagName.toLowerCase();
      const role = roleFromTag(el);
      const text = elementText(el);
      const name = accessibleName(el);
      const testid =
        el.getAttribute('data-testid') ||
        el.getAttribute('data-test') ||
        el.getAttribute('data-cy') ||
        '';

      const searchable = JS_BACKTICK${{tag}} ${{role}} ${{text}} ${{name}} ${{testid}}JS_BACKTICK.toLowerCase();
      if (query && !searchable.includes(query)) continue;

      candidates.push({{
        tag,
        role,
        text,
        accessible_name: name,
        aria_label: normalize(el.getAttribute('aria-label') || ''),
        testid: normalize(testid),
        placeholder: normalize(el.getAttribute('placeholder') || ''),
        href: normalize(el.getAttribute('href') || '', 180),
        disabled: Boolean(el.disabled || el.getAttribute('aria-disabled') === 'true'),
        bbox: [
          Math.round(rect.x),
          Math.round(rect.y),
          Math.round(rect.width),
          Math.round(rect.height)
        ],
        selector_hint: buildSelectorHint(el),
        score: scoreElement(el, rect, text, name)
      }});
    }}

    candidates.sort((a, b) => {{
      if (b.score !== a.score) return b.score - a.score;
      if (a.bbox[1] !== b.bbox[1]) return a.bbox[1] - b.bbox[1];
      return a.bbox[0] - b.bbox[0];
    }});

    const elements = candidates.slice(0, maxItems).map((item, index) => {{
      const copy = {{ ...item }};
      copy.id = JS_BACKTICKe${{index + 1}}JS_BACKTICK;
      delete copy.score;
      return copy;
    }});

    return {{
      ok: true,
      url: window.location.href,
      title: document.title,
      viewport: {{
        width: window.innerWidth,
        height: window.innerHeight,
        scroll_x: window.scrollX,
        scroll_y: window.scrollY
      }},
      query,
      viewport_only: viewportOnly,
      total_candidates: candidates.length,
      returned: elements.length,
      elements,
      error: null
    }};
  }}, params);
}}
`, string(paramsJSON))
	return strings.ReplaceAll(js, "JS_BACKTICK", "`")
}

// BuildCardProbeJS 构建卡片/列表结构探测的 JavaScript 代码。
// 供 browser_run_code_unsafe 调用，返回紧凑重复卡片结构列表。
//
// 对齐 Python: build_card_probe_js (probes.py L284-1251)
// JS 代码逐字符复制 Python 原文，不做自行翻译
// JS 反引号在 Go 反引号字符串中用 JS_BACKTICK 占位，运行时替换
func BuildCardProbeJS(maxCards int, viewportOnly bool, includeButtons bool, query string, siteProfiles []map[string]any, selectorCacheRecords []map[string]any) string {
	params := map[string]any{
		"max_cards":              clampInt(maxCards, 1, 50),
		"viewport_only":          viewportOnly,
		"include_buttons":        includeButtons,
		"query":                  strings.TrimSpace(strings.ToLower(query)),
		"site_profiles":          siteProfiles,
		"selector_cache_records": selectorCacheRecords,
	}
	paramsJSON, _ := json.Marshal(params)

	// 对齐 Python: probes.py L305-1251 的 template 字符串
	// JS 代码逐字符复制 Python 原文，JS_BACKTICK 运行时替换为反引号
	template := `async (page) => {
  const params = __PARAMS__;

  return await page.evaluate((params) => {
    const maxCards = Math.max(1, Math.min(Number(params.max_cards || 20), 50));
    const viewportOnly = params.viewport_only !== false;
    const includeButtons = params.include_buttons !== false;
    const query = String(params.query || '').trim().toLowerCase();
    const host = String(window.location.hostname || '').toLowerCase();
    const path = String(window.location.pathname || '/').toLowerCase();

    const unique = (items, limit = 80) => {
      const result = [];
      const seen = new Set();

      for (const item of items || []) {
        const value = String(item || '').trim();
        if (!value || seen.has(value)) continue;
        seen.add(value);
        result.push(value);
        if (result.length >= limit) break;
      }

      return result;
    };

    const routeMatches = (patterns) => {
      if (!Array.isArray(patterns) || patterns.length === 0) return true;

      return patterns.some((pattern) => {
        try {
          return new RegExp(String(pattern), 'i').test(path);
        } catch(_) {
          return false;
        }
      });
    };
    
    const domainMatches = (domains) => {
      if (!Array.isArray(domains) || domains.length === 0) return false;

      return domains.some((domain) => {
        const value = String(domain || '').toLowerCase();
        return host === value || host.endsWith(JS_BACKTICK.${value}JS_BACKTICK);
      });
    };

    const activeProfiles = Array.isArray(params.site_profiles)
      ? params.site_profiles.filter((profile) => {
          return domainMatches(profile.domains) && routeMatches(profile.route_patterns);
        })
      : [];

    const normalizeRouteSignature = (value) => {
      let route = String(value || '/').toLowerCase();
      route = route.replace(/\d+/g, '*').replace(/\/+/g, '/')
      if (route !== '/' && route.endsWith('/')) route = route.slice(0, -1);
      return route || '/';
    };

    const currentRouteSignature = normalizeRouteSignature(path);

    const activeCacheRecords = Array.isArray(params.selector_cache_records)
      ? params.selector_cache_records.filter((record) => {
          const domain = String(record.domain || '').toLowerCase();
          if (!domain) return false;
          const domainOk = host === domain || host.endsWith(JS_BACKTICK.${domain}JS_BACKTICK);
          if (!domainOk) return false;

          const route = String(record.route_signature || '').toLowerCase();
          return !route || route === currentRouteSignature;
        })
      : [];
    
    const cacheSelectors = (name) => {
      const values = [];

      for (const record of activeCacheRecords) {
        const selectors = record.selectors || {};
        if (Array.isArray(selectors[name])) {
          values.push(...selectors[name]);
        }
      }

      return unique(values);
    };

    const siteProfileSelectors = (name) => {
      const values = [];

      for (const profile of activeProfiles) {
        if (Array.isArray(profile[name])) {
          values.push(...profile[name]);
        }
      }

      return unique(values);
    };

    const profileSelectors = (name) => {
      return unique([
        ...cacheSelectors(name),
        ...siteProfileSelectors(name)
      ]);
    };

    const mergeSelectors = (...groups) => {
      const values = [];
      for (const group of groups) {
        values.push(...(Array.isArray(group) ? group : []));
      }
      return unique(values);
    };

    const normalize = (value, limit = 180) => {
      return String(value || '')
        .replace(/\s+/g, ' ')
        .trim()
        .slice(0, limit);
    };

    const attrEscape = (value) => {
      return String(value || '').replace(/\\/g, '\\\\').replace(/"/g, '\\"');
    };

    const cssEscape = (value) => {
      const raw = String(value || '');
      if (window.CSS && typeof window.CSS.escape === 'function') {
        return window.CSS.escape(raw);
      }
      return raw.replace(/[^a-zA-Z0-9_-]/g, '\\$&');
    };

    const isVisible = (el, rect) => {
      if (!el || !rect) return false;
      if (rect.width < 20 || rect.height < 20) return false;

      const style = window.getComputedStyle(el);
      if (!style) return false;
      if (style.display === 'none') return false;
      if (style.visibility === 'hidden') return false;
      if (Number(style.opacity) === 0) return false;

      if (viewportOnly) {
        if (rect.bottom < 0) return false;
        if (rect.right < 0) return false;
        if (rect.top > window.innerHeight) return false;
        if (rect.left > window.innerWidth) return false;
      }

      return true;
    };

    const buildSelectorHint = (el) => {
      if (!el || !el.tagName) return '';

      const tag = el.tagName.toLowerCase();

      const testid =
        el.getAttribute('data-testid') ||
        el.getAttribute('data-test') ||
        el.getAttribute('data-cy');
      if (testid) {
        if (el.getAttribute('data-testid')) return JS_BACKTICK[data-testid="${attrEscape(testid)}"]JS_BACKTICK;
        if (el.getAttribute('data-test')) return JS_BACKTICK[data-test="${attrEscape(testid)}"]JS_BACKTICK;
        return JS_BACKTICK[data-cy="${attrEscape(testid)}"]JS_BACKTICK;
      }

      const id = el.getAttribute('id');
      if (id) return JS_BACKTICK#${cssEscape(id)}JS_BACKTICK;

      const stableClasses = normalize(el.getAttribute('class') || '', 160)
        .split(' ')
        .filter(Boolean)
        .filter((item) => !/^(active|selected|disabled|hover|focus|show|hide|open|closed)$/i.test(item))
        .slice(0, 3);

      const simple = stableClasses.length
        ? JS_BACKTICK${tag}${stableClasses.map((item) => JS_BACKTICK.${cssEscape(item)}JS_BACKTICK).join('')}JS_BACKTICK
        : tag;

      try {
        if (document.querySelectorAll(simple).length === 1) {
          return simple;
        }
      } catch (_) {
        // 当选择器无法测试时，回退到完整的 nth-of-type 路径。
      }

      const path = [];
      let node = el;
      let depth = 0;

      while (node && node.nodeType === Node.ELEMENT_NODE && depth < 8) {
        const nodeTag = node.tagName.toLowerCase();

        const nodeTestid =
          node.getAttribute('data-testid') ||
          node.getAttribute('data-test') ||
          node.getAttribute('data-cy');

        if (nodeTestid) {
          if (node.getAttribute('data-testid')) {
            path.unshift(JS_BACKTICK[data-testid="${attrEscape(nodeTestid)}"]JS_BACKTICK);
          } else if (node.getAttribute('data-test')) {
            path.unshift(JS_BACKTICK[data-test="${attrEscape(nodeTestid)}"]JS_BACKTICK);
          } else {
            path.unshift(JS_BACKTICK[data-cy="${attrEscape(nodeTestid)}"]JS_BACKTICK);
          }
          break;
        }

        const nodeId = node.getAttribute('id');
        if (nodeId) {
          path.unshift(JS_BACKTICK#${cssEscape(nodeId)}JS_BACKTICK);
          break;
        }

        let index = 1;
        let prev = node.previousElementSibling;
        while (prev) {
          if (prev.tagName.toLowerCase() === nodeTag) index += 1;
          prev = prev.previousElementSibling;
        }

        const cls = normalize(node.getAttribute('class') || '', 100)
          .split(' ')
          .filter(Boolean)
          .filter((item) => !/^(active|selected|disabled|hover|focus|show|hide|open|closed)$/i.test(item))
          .slice(0, 2)
          .map((item) => JS_BACKTICK.${cssEscape(item)}JS_BACKTICK)
          .join('');

        path.unshift(JS_BACKTICK${nodeTag}${cls}:nth-of-type(${index})JS_BACKTICK);

        const parentNode = node.parentElement;
        if (
          parentNode &&
          ['ol', 'ul', 'main', 'section', 'body'].includes(parentNode.tagName.toLowerCase()) &&
          depth >= 3
        ) {
          const parentTag = parentNode.tagName.toLowerCase();
          let parentIndex = 1;
          let parentPrev = parentNode.previousElementSibling;
          while (parentPrev) {
            if (parentPrev.tagName.toLowerCase() === parentTag) parentIndex += 1;
            parentPrev = parentPrev.previousElementSibling;
          }
          path.unshift(JS_BACKTICK${parentTag}:nth-of-type(${parentIndex})JS_BACKTICK);
          break;
        }

        node = parentNode;
        depth += 1;
      }

      return path.join(' > ');
    };

    const directText = (el) => {
      const clone = el.cloneNode(true);
      clone.querySelectorAll('script, style, noscript, svg').forEach((node) => node.remove());
      return normalize(clone.innerText || clone.textContent || '', 600);
    };

    const findFirst = (root, selectors) => {
      for (const selector of selectors) {
        try {
          const found = root.querySelector(selector);
          if (found) return found;
        } catch (_) {
          // 忽略无效的浏览器特定选择器处理。
        }
      }
      return null;
    };

    const textOf = (el, limit = 180) => {
      if (!el) return '';
      return normalize(
        el.getAttribute('title') ||
        el.getAttribute('aria-label') ||
        el.getAttribute('alt') ||
        el.innerText ||
        el.textContent ||
        '',
        limit
      );
    };

    const extractTitle = (root) => {
      const titleEl = findFirst(root, mergeSelectors(
        profileSelectors('title_selectors'),
        [
          'h1', 'h2', 'h3', 'h4',
          '[class*="title" i]',
          '[class*="name" i]',
          '[data-testid*="title" i]',
          '[data-test*="title" i]',
          'a[title]',
          'img[alt]',
          'a'
        ]
      ));

      let title = textOf(titleEl, 220);
      if (!title) {
        const link = findFirst(root, ['a']);
        title = textOf(link, 220);
      }

      return {
        value: title,
        selector_hint: titleEl ? buildSelectorHint(titleEl) : ''
      };
    };

    const extractPrimaryLink = (root) => {
      const link = findFirst(root, mergeSelectors(
        profileSelectors('primary_link_selectors'),
        [
          'a[href][title]',
          'h1 a[href]', 'h2 a[href]', 'h3 a[href]', 'h4 a[href]',
          'a[href]'
        ]
      ));

      if (!link) {
        return {
          text: '',
          href: '',
          selector_hint: ''
        };
      }

      return {
        text: textOf(link, 180),
        href: normalize(link.href || link.getAttribute('href') || '', 260),
        selector_hint: buildSelectorHint(link)
      };
    };

    const PRICE_RE =
      /(?:S\$|US\$|A\$|HK\$|\$|£|€|¥|￥|Rp|RM|SGD|USD|IDR|MYR)\s?\d[\d,.]*(?:\.\d+)?|\d[\d,.]*(?:\.\d+)?\s?(?:SGD|USD|IDR|MYR|円)/i;

    const normalizePriceValue = (value) => {
      const cleaned = normalize(value, 120);
      const match = cleaned.match(PRICE_RE);
      return match ? normalize(match[0], 80) : '';
    };

    const extractPrice = (rootText, root) => {
      const priceEl = findFirst(root, mergeSelectors(
        profileSelectors('price_selectors'),
        [
          '[class*="price" i]',
          '[data-testid*="price" i]',
          '[data-test*="price" i]',
          '[aria-label*="price" i]'
        ]
      ));

      const fromElement = normalizePriceValue(textOf(priceEl, 120));
      if (fromElement) {
        return {
          value: fromElement,
          selector_hint: buildSelectorHint(priceEl)
        };
      }

      const fromText = normalizePriceValue(rootText);

      return {
        value: fromText,
        selector_hint: priceEl ? buildSelectorHint(priceEl) : ''
      };
    };

    const ratingClassValue = (el) => {
      if (!el) return '';

      const raw = JS_BACKTICK${el.getAttribute('class') || ''} ${el.getAttribute('aria-label') || ''} ${el.getAttribute('title') || ''}JS_BACKTICK;

      const wordMap = [
        ['Five', 'Five stars'],
        ['Four', 'Four stars'],
        ['Three', 'Three stars'],
        ['Two', 'Two stars'],
        ['One', 'One star']
      ];

      for (const [needle, value] of wordMap) {
        if (new RegExp(JS_BACKTICK\\b${needle}\\bJS_BACKTICK, 'i').test(raw)) {
          return value;
        }
      }

      const numeric = raw.match(/(?:rating|star)[^0-9]*(\d(?:\.\d)?)/i);
      if (numeric) {
        return JS_BACKTICK${numeric[1]} starsJS_BACKTICK;
      }

      return '';
    };

    const extractRating = (rootText, root) => {
      const ratingEl = findFirst(root, mergeSelectors(
        profileSelectors('rating_selectors'),
        [
          '[class*="rating" i]',
          '[aria-label*="rating" i]',
          '[title*="rating" i]',
          '[class*="star" i]',
          '[aria-label*="star" i]'
        ]
      ));

      const fromClass = ratingClassValue(ratingEl);
      if (fromClass) {
        return {
          value: fromClass,
          selector_hint: buildSelectorHint(ratingEl)
        };
      }

      const ratingText = JS_BACKTICK${textOf(ratingEl, 120)} ${rootText}JS_BACKTICK;
      const match = ratingText.match(
        /(?:\d(?:\.\d)?\s*(?:out of|\/)\s*5)|(?:\d(?:\.\d)?\s*★)|(?:rating\s*[:\-]?\s*\d(?:\.\d)?)/i
      );

      return {
        value: match ? normalize(match[0], 80) : '',
        selector_hint: ratingEl ? buildSelectorHint(ratingEl) : ''
      };
    };

    const extractReviewCount = (rootText) => {
      const match = rootText.match(
        /(?:\(?\d[\d,.]*\)?\s*(?:reviews?|ratings?|sold|bought|orders?))|(?:(?:reviews?|ratings?)\s*[:\-]?\s*\d[\d,.]*)/i
      );
      return match ? normalize(match[0], 80) : '';
    };

    const extractAvailability = (rootText) => {
      const match = rootText.match(
        /\b(?:in stock|out of stock|available|unavailable|sold out|limited stock|only \d+ left)\b/i
      );
      return match ? normalize(match[0], 80) : '';
    };

    const extractButtons = (root) => {
      if (!includeButtons) return [];

      const buttonSelectors = mergeSelectors(
        profileSelectors('button_selectors'),
        [
          'button',
          '[role="button"]',
          'input[type="button"]',
          'input[type="submit"]',
          'a[href]'
        ]
      );

      const buttons = Array.from(root.querySelectorAll(buttonSelectors.join(',')));

      return buttons
        .map((el) => {
          const rect = el.getBoundingClientRect();
          if (!el || rect.width < 2 || rect.height < 2) return null;

          const style = window.getComputedStyle(el);
          if (!style) return null;
          if (style.display === 'none') return null;
          if (style.visibility === 'hidden') return null;
          if (Number(style.opacity) === 0) return null;

          const text = normalize(
            el.getAttribute('aria-label') ||
            el.getAttribute('value') ||
            el.innerText ||
            el.textContent ||
            '',
            120
          );

          if (!text) return null;

          return {
            text,
            tag: el.tagName.toLowerCase(),
            role: el.getAttribute('role') || '',
            selector_hint: buildSelectorHint(el),
            bbox: [
              Math.round(rect.x),
              Math.round(rect.y),
              Math.round(rect.width),
              Math.round(rect.height)
            ]
          };
        })
        .filter(Boolean)
        .slice(0, 8);
    };

    const looksLikePageChrome = (data) => {
      const selector = String(data.selector_hint || '').toLowerCase();
      const title = String(data.title || '').trim().toLowerCase();
      const preview = String(data.text_preview || '').trim().toLowerCase();

      const chromeFragments = [
        '#nav',
        'nav-',
        'navbar',
        'breadcrumb',
        'header',
        'footer',
        'menu',
        'sidebar'
      ];

      if (chromeFragments.some((fragment) => selector.includes(fragment))) {
        return true;
      }

      const chromeTitles = new Set([
        'fresh & fast',
        'sell',
        'best sellers',
        'customer service',
        "today's deals",
        'new releases',
        'help',
        'login',
        'sign in'
      ]);

      if (chromeTitles.has(title) || chromeTitles.has(preview)) {
        return true;
      }

      if (preview.length < 4 && !data.price && !data.rating && !data.has_image) {
        return true;
      }

      return false;
    };

    const cardQualityScore = (data) => {
      if (looksLikePageChrome(data)) return 0;

      let score = 0;

      const title = String(data.title || '').trim();
      const preview = String(data.text_preview || '').trim();
      const buttons = Array.isArray(data.buttons) ? data.buttons : [];

      if (title.length >= 8) score += 20;
      if (preview.length >= 60) score += 15;
      if (data.primary_link) score += 12;
      if (data.price) score += 18;
      if (data.rating) score += 14;
      if (data.review_count) score += 10;
      if (data.availability) score += 8;
      if (data.has_image) score += 12;
      if (buttons.length > 0) score += 8;

      return score;
    };

    const isHighQualityCard = (item) => {
      const score = item.quality_score || cardQualityScore(item.data || {});
      if (score >= 42) return true;

      const data = item.data || {};
      const preview = String(data.text_preview || '').trim();
      const buttons = Array.isArray(data.buttons) ? data.buttons : [];

      // 允许没有价格/评分/图片的引用/文章样式卡片。
      return (
        score >= 30 &&
        preview.length >= 80 &&
        (data.primary_link || buttons.length > 0)
      );
    };

    const hasEnoughGoodCards = (items) => {
      if (!Array.isArray(items) || items.length === 0) return false;

      const good = items.filter(isHighQualityCard);
      if (good.length >= Math.min(maxCards, 3)) return true;

      const signatureCounts = new Map();
      for (const item of good) {
        const count = signatureCounts.get(item.signature) || 0;
        signatureCounts.set(item.signature, count + 1);
      }

      return Array.from(signatureCounts.values()).some((count) => count >= 2);
    };

    const hasImage = (root) => {
      return Boolean(root.querySelector('img, picture, source[srcset]'));
    };

    const structuralSignature = (el, fields) => {
      const tag = el.tagName.toLowerCase();
      const classTokens = normalize(el.getAttribute('class') || '', 160)
        .split(' ')
        .filter(Boolean)
        .slice(0, 4)
        .join('.');
      const children = Array.from(el.children)
        .slice(0, 8)
        .map((child) => child.tagName.toLowerCase())
        .join('>');
      const fieldBits = [
        fields.title ? 'title' : '',
        fields.price ? 'price' : '',
        fields.rating ? 'rating' : '',
        fields.buttons && fields.buttons.length ? 'button' : '',
        fields.has_image ? 'image' : ''
      ].filter(Boolean).join('|');

      return JS_BACKTICK${tag}|${classTokens}|${children}|${fieldBits}JS_BACKTICK;
    };

    const queryAllSafe = (selectors) => {
      const result = [];
      const seen = new Set();

      for (const selector of selectors || []) {
        try {
          const nodes = Array.from(document.querySelectorAll(selector));
          for (const node of nodes) {
            if (!node || seen.has(node)) continue;
            seen.add(node);
            result.push(node);
          }
        } catch (_) {
          // 忽略无效选择器。
        }
      }

      return result;
    };

    const buildCandidatesFromContainers = (containers, selectorSource) => {
      const seen = new Set();
      const localCandidates = [];

      for (const el of containers || []) {
        if (!el || seen.has(el)) continue;
        seen.add(el);

        const tag = el.tagName.toLowerCase();
        if (
          tag === 'html' ||
          tag === 'body' ||
          tag === 'main' ||
          tag === 'nav' ||
          tag === 'header' ||
          tag === 'footer'
        ) {
          continue;
        }

        const rect = el.getBoundingClientRect();
        if (!isVisible(el, rect)) continue;

        const area = rect.width * rect.height;
        const viewportArea = Math.max(1, window.innerWidth * window.innerHeight);
        if (area > viewportArea * 0.85) continue;

        const rootText = directText(el);
        if (!rootText || rootText.length < 4) continue;
        if (query && !rootText.toLowerCase().includes(query)) continue;

        const title = extractTitle(el);
        const price = extractPrice(rootText, el);
        const rating = extractRating(rootText, el);
        const primaryLink = extractPrimaryLink(el);
        const buttons = extractButtons(el);
        const reviewCount = extractReviewCount(rootText);
        const availability = extractAvailability(rootText);
        const imagePresent = hasImage(el);

        const fields = {
          title: title.value,
          price: price.value,
          rating: rating.value,
          review_count: reviewCount,
          availability,
          primary_link: primaryLink.href,
          buttons,
          has_image: imagePresent
        };

        const fieldCount = [
          fields.title,
          fields.price,
          fields.rating,
          fields.review_count,
          fields.availability,
          fields.primary_link,
          fields.has_image,
          fields.buttons && fields.buttons.length
        ].filter(Boolean).length;

        if (fieldCount < 2) continue;

        const data = {
          selector_source: selectorSource,
          selector_hint: buildSelectorHint(el),
          title: title.value,
          title_selector_hint: title.selector_hint,
          price: price.value,
          price_selector_hint: price.selector_hint,
          rating: rating.value,
          rating_selector_hint: rating.selector_hint,
          review_count: reviewCount,
          availability,
          primary_link: primaryLink.href,
          primary_link_text: primaryLink.text,
          primary_link_selector_hint: primaryLink.selector_hint,
          has_image: imagePresent,
          buttons,
          text_preview: normalize(rootText, 280),
          bbox: [
            Math.round(rect.x),
            Math.round(rect.y),
            Math.round(rect.width),
            Math.round(rect.height)
          ]
        };

        const qualityScore = cardQualityScore(data);
        const signature = structuralSignature(el, fields);

        localCandidates.push({
          el,
          signature,
          fieldCount,
          area,
          top: rect.top,
          left: rect.left,
          quality_score: qualityScore,
          data: {
            ...data,
            quality_score: qualityScore
          }
        });
      }

      return localCandidates;
    };

    const cachedContainerSelectors = cacheSelectors('card_container_selectors');
    const profileContainerSelectors = siteProfileSelectors('card_container_selectors');

    const genericContainerSelectors = [
      'article',
      'li',
      'section',
      '[data-testid*="card" i]',
      '[data-testid*="item" i]',
      '[data-testid*="product" i]',
      '[data-test*="card" i]',
      '[data-test*="item" i]',
      '[data-test*="product" i]',
      '[class*="card" i]',
      '[class*="item" i]',
      '[class*="product" i]',
      'div'
    ];

    const cachedContainers = queryAllSafe(cachedContainerSelectors);
    const cachedCandidates = buildCandidatesFromContainers(cachedContainers, 'cache');

    let selectorSource = 'generic';
    let candidates = [];

    if (hasEnoughGoodCards(cachedCandidates)) {
      selectorSource = 'cache';
      candidates = cachedCandidates;
    } else {
      const profileContainers = queryAllSafe(profileContainerSelectors);
      const profileCandidates = buildCandidatesFromContainers(profileContainers, 'profile');

      if (hasEnoughGoodCards(profileCandidates)) {
        selectorSource = 'profile';
        candidates = profileCandidates;
      } else {
        const genericContainers = queryAllSafe(genericContainerSelectors);
        selectorSource = 'generic';
        candidates = buildCandidatesFromContainers(genericContainers, 'generic');
      }
    }

    const groups = new Map();
    for (const item of candidates) {
      const group = groups.get(item.signature) || [];
      group.push(item);
      groups.set(item.signature, group);
    }

    const recurringSignatures = Array.from(groups.entries())
      .map(([signature, group]) => ({
        signature,
        count: group.length,
        sample_selector_hint: group[0]?.data?.selector_hint || '',
        fields_detected: [
          group.some((x) => x.data.title) ? 'title' : '',
          group.some((x) => x.data.price) ? 'price' : '',
          group.some((x) => x.data.rating) ? 'rating' : '',
          group.some((x) => x.data.review_count) ? 'review_count' : '',
          group.some((x) => x.data.availability) ? 'availability' : '',
          group.some((x) => x.data.buttons && x.data.buttons.length) ? 'buttons' : '',
          group.some((x) => x.data.has_image) ? 'image' : ''
        ].filter(Boolean)
      }))
      .filter((item) => item.count >= 2)
      .sort((a, b) => b.count - a.count)
      .slice(0, 10);

    const scored = candidates.map((item) => {
      const groupCount = groups.get(item.signature)?.length || 1;
      let score = 0;

      score += item.fieldCount * 20;
      score += item.quality_score || 0;
      if (groupCount >= 2) score += 50 + Math.min(groupCount, 20) * 4;
      if (item.data.price) score += 20;
      if (item.data.title) score += 15;
      if (item.data.buttons && item.data.buttons.length) score += 12;
      if (item.data.has_image) score += 8;
      if (item.top >= 0 && item.top <= window.innerHeight) score += 8;

      // 对超大容器进行惩罚，因为它们通常是网格/区域，而非卡片。
      const viewportArea = Math.max(1, window.innerWidth * window.innerHeight);
      if (item.area > viewportArea * 0.45) score -= 40;

      return {
        ...item,
        groupCount,
        score
      };
    });

    scored.sort((a, b) => {
      if (b.score !== a.score) return b.score - a.score;
      if (b.groupCount !== a.groupCount) return b.groupCount - a.groupCount;
      if (a.top !== b.top) return a.top - b.top;
      return a.left - b.left;
    });

    const hasRichCandidates = scored.some((candidate) => {
      return Boolean(
        candidate.data.price ||
        candidate.data.rating ||
        candidate.data.review_count ||
        candidate.data.availability ||
        candidate.data.has_image
      );
    });

    const selectable = scored.filter((item) => {
      if (looksLikePageChrome(item.data)) return false;

      // 如果已有丰富的列表类候选，则移除弱导航类条目。
      if (!hasRichCandidates) return true;

      return Boolean(
        item.data.price ||
        item.data.rating ||
        item.data.review_count ||
        item.data.availability ||
        item.data.has_image ||
        item.quality_score >= 45
      );
    });

    const selected = [];
    for (const item of selectable) {
      const conflictsWithExisting = selected.find((chosen) => {
        return item.el.contains(chosen.el) || chosen.el.contains(item.el);
      });

      if (conflictsWithExisting) {
        // 优先选择提取字段更多的候选。若字段数相同，则优先选择较小的重复卡片式容器，而非大型区域/网格包装器。
        const itemBetter =
          item.fieldCount > conflictsWithExisting.fieldCount ||
          (
            item.fieldCount === conflictsWithExisting.fieldCount &&
            item.groupCount >= conflictsWithExisting.groupCount &&
            item.area < conflictsWithExisting.area
          );

        if (itemBetter) {
          const idx = selected.indexOf(conflictsWithExisting);
          selected.splice(idx, 1, item);
        }

        continue;
      }

      selected.push(item);
      if (selected.length >= maxCards) break;
    }

    const cards = selected.map((item, index) => ({
      id: JS_BACKTICKcard_${index + 1}JS_BACKTICK,
      group_count: item.groupCount,
      ...item.data
    }));

    return {
      ok: true,
      url: window.location.href,
      title: document.title,
      viewport: {
        width: window.innerWidth,
        height: window.innerHeight,
        scroll_x: window.scrollX,
        scroll_y: window.scrollY
      },
      query,
      viewport_only: viewportOnly,
      profile_ids: activeProfiles.map((profile) => profile.id || '').filter(Boolean),
      cache_records_used: activeCacheRecords.length,
      selector_source: selectorSource,
      cache_accepted: selectorSource === 'cache',
      cached_container_selectors: cachedContainerSelectors.length,
      profile_container_selectors: profileContainerSelectors.length,
      total_candidates: candidates.length,
      returned: cards.length,
      recurring_signatures: recurringSignatures,
      cards,
      error: null
    };
  }, params);
}
`

	js := strings.Replace(template, "__PARAMS__", string(paramsJSON), 1)
	return strings.ReplaceAll(js, "JS_BACKTICK", "`")
}
