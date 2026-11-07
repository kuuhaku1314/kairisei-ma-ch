'use strict';
// Shared building blocks for the formatted editor modules: missions, gift codes and pool rules.
// Classic scripts share one global scope, so everything here is a plain function or a frozen table.

// Markup ----------------------------------------------------------------------------------------

/** Markup that is already safe; interpolating it into html`` again does not escape it twice. */
class SafeHTML {
  constructor(value) {
    this.value = value;
  }

  toString() {
    return this.value;
  }
}

const raw = (value) => new SafeHTML(String(value ?? ''));

function htmlValue(value) {
  if (value == null || value === false) return '';
  if (value instanceof SafeHTML) return value.value;
  if (Array.isArray(value)) return value.map(htmlValue).join('');
  return esc(value);
}

/** Tagged template: every interpolation is escaped unless it is SafeHTML; arrays are joined. */
function html(strings, ...values) {
  let out = strings[0];
  values.forEach((value, i) => {
    out += htmlValue(value) + strings[i + 1];
  });
  return new SafeHTML(out);
}

function setHTML(element, content) {
  element.innerHTML = htmlValue(content);
}

const attr = (condition, name) => (condition ? raw(name) : '');

// Time: operations use fixed UTC+8 business days ---------------------------------------------------

const CHINA_OFFSET_SECONDS = 8 * 3600;

/** Unix seconds → value for <input type="datetime-local"> in UTC+8. */
function chinaInputValue(unix) {
  if (!unix) return '';
  return new Date((unix + CHINA_OFFSET_SECONDS) * 1000).toISOString().slice(0, 16);
}

/** <input type="datetime-local"> value read as UTC+8 → unix seconds (0 when empty). */
function chinaUnix(value) {
  if (!value) return 0;
  const millis = Date.parse(`${value}:00+08:00`);
  if (!Number.isFinite(millis)) throw new Error('请输入有效的 UTC+8 日期时间');
  return Math.floor(millis / 1000);
}

const chinaText = (unix, fallback = '不限') => (unix ? chinaInputValue(unix).replace('T', ' ') : fallback);

function chinaWindowText(startsAt, endsAt) {
  if (!startsAt && !endsAt) return '不限时间';
  return `${chinaText(startsAt, '立即')} 至 ${chinaText(endsAt, '不限')}（UTC+8）`;
}

// Draft comparison ------------------------------------------------------------------------------

/**
 * Canonical JSON for "has this draft changed" checks. Keys are sorted and Go zero values
 * ('' / 0 / false / [] / null) are dropped, matching how omitempty documents round-trip.
 */
function stableJSON(value) {
  const canonical = (v) => {
    if (Array.isArray(v)) return v.map(canonical);
    if (v && typeof v === 'object') {
      const out = {};
      for (const key of Object.keys(v).sort()) {
        const item = canonical(v[key]);
        const empty =
          item == null ||
          item === '' ||
          item === 0 ||
          item === false ||
          (Array.isArray(item) && !item.length);
        if (!empty) out[key] = item;
      }
      return out;
    }
    return v;
  };
  return JSON.stringify(canonical(value));
}

/** Dirty checks per view, consulted by adminPolicyDirty() for the sidebar and leave guards. */
const editorDirtyChecks = {};
const editorDirty = (view) => !!editorDirtyChecks[view]?.();

/** Resolves true when nothing is unsaved or the operator agrees to drop the edits; callers await it. */
async function confirmDiscard(dirty, message = '当前编辑尚未保存，继续会放弃这些修改。', ok = '放弃修改') {
  return !dirty || askConfirm('放弃未保存的修改？', message, { ok, danger: true });
}

/**
 * Upload errors stay next to the upload control: a corner toast is easy to miss on a wide screen.
 * Any [data-upload-alert-close] button removes its alert.
 */
function uploadAlertHTML(title, lines, hint = '') {
  return html`<div class="view-alert upload-alert" role="alert">
    <svg class="icon" aria-hidden="true"><use href="#i-alert" /></svg>
    <div class="alert-text">
      <b>${title}</b>
      <ul>
        ${lines.map((line) => html`<li>${line}</li>`)}
      </ul>
      ${hint ? html`<span>${hint}</span>` : ''}
    </div>
    <button class="x-button" type="button" data-upload-alert-close aria-label="关闭提示">
      <svg class="icon"><use href="#i-close" /></svg>
    </button>
  </div>`;
}

const UPLOAD_IMAGE_HINT = '图片须为 2 MiB 以内的 PNG 或 JPEG；可压缩或另存为 JPEG 后重新上传。';
const fileSizeText = (bytes) => `${(bytes / 1048576).toFixed(1)} MiB`;

document.addEventListener('click', (event) => {
  event.target.closest('[data-upload-alert-close]')?.closest('.upload-alert').remove();
});

// Small controls ---------------------------------------------------------------------------------

const TONE_CLASS = { ok: 'ok', warn: 'warn', bad: 'bad', info: 'info', muted: '' };

function stateTag(text, tone = 'muted') {
  return html`<span class="tag ${TONE_CLASS[tone] ?? ''}">${text}</span>`;
}

/**
 * Radio-based segmented control. options: [[value, label, title?], ...].
 * The inputs carry data-* attributes from `data` so a delegated handler can read them.
 */
function segmented(name, value, options, { disabled = false, data = '' } = {}) {
  return html`
    <div class="seg" role="radiogroup">
      ${options.map(
        ([optionValue, label, title]) => html`
          <label class="seg-option" title="${title || ''}">
            <input
              type="radio"
              name="${name}"
              value="${optionValue}"
              ${raw(data)}
              ${attr(String(value) === String(optionValue), 'checked')}
              ${attr(disabled, 'disabled')}
            />
            <span>${label}</span>
          </label>
        `,
      )}
    </div>
  `;
}

/** Parses "1, 2 3，4" into distinct positive integers, or throws with the given label. */
function parseIDList(text, label = '编号') {
  const ids = String(text)
    .split(/[\s,，、]+/)
    .filter(Boolean)
    .map(Number);
  if (ids.some((id) => !Number.isSafeInteger(id) || id <= 0)) {
    throw new Error(`${label}须为正整数，用逗号或空格分隔`);
  }
  if (new Set(ids).size !== ids.length) throw new Error(`${label}不能重复`);
  return ids;
}

function downloadText(text, filename, type = 'text/plain;charset=utf-8') {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** Runs async page loads in parallel with a small concurrency limit; results keep their order. */
async function mapLimited(items, limit, task) {
  const results = new Array(items.length);
  let next = 0;
  const worker = async () => {
    while (next < items.length) {
      const index = next++;
      results[index] = await task(items[index], index);
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  return results;
}

// Rewards ----------------------------------------------------------------------------------------

/** Catalog facts (name, thumbnail, kind) remembered from the picker and the resolve API. */
const catalogInfo = new Map();

const REWARD_TYPE_KIND = {
  0: 'currency',
  4: 'currency',
  6: 'card',
  8: 'item',
  9: 'currency',
  10: 'currency',
  12: 'currency',
  13: 'material',
  14: 'costume',
  15: 'sphere',
  16: 'stamp',
  18: 'honor',
  19: 'buddy',
};

function rememberCatalogRows(rows) {
  for (const row of rows) {
    catalogInfo.set(contentKey(row), row);
    contentPicker.labels.set(contentKey(row), row.name);
  }
}

const resolveRequest = (requests) =>
  api('/api/catalog/resolve', { method: 'POST', body: JSON.stringify({ rewards: requests }) });

/**
 * Looks up catalog facts for {reward_type, reward_type_id} pairs that are not known yet.
 * One unknown entry rejects a whole batch (HTTP 400), so a failed batch is retried item by
 * item and entries that left the catalog simply keep their numeric label.
 */
async function resolveCatalogEntries(pairs) {
  const requests = [
    ...new Map(
      pairs
        .filter(([type, id]) => !catalogInfo.has(`${type}:${id}`))
        .map(([type, id]) => [`${type}:${id}`, { reward_type: type, reward_type_id: id, quantity: 1 }]),
    ).values(),
  ];
  for (let i = 0; i < requests.length; i += 100) {
    const chunk = requests.slice(i, i + 100);
    try {
      rememberCatalogRows((await resolveRequest(chunk)).entries || []);
    } catch (error) {
      if (error.status !== 400) throw error;
      await mapLimited(chunk, 6, async (request) => {
        try {
          rememberCatalogRows((await resolveRequest([request])).entries || []);
        } catch (single) {
          if (single.status !== 400) throw single;
        }
      });
    }
  }
}

/** Resolves names and thumbnails for rewards that were saved earlier. */
async function resolveRewardInfo(rewards) {
  await resolveCatalogEntries(rewards.map((r) => [r.type, r.reward_typeid]));
  for (const reward of rewards) {
    const fixed = catalogInfo.get(contentKey(reward))?.fixed_card_level;
    if (fixed) reward.card_lv = fixed;
  }
}

/** Card targets may also be material cards (持有指定卡牌); try cards first, then materials. */
async function resolveCardSubjects(ids) {
  await resolveCatalogEntries(ids.map((id) => [6, id]));
  await resolveCatalogEntries(ids.filter((id) => !catalogInfo.has(`6:${id}`)).map((id) => [13, id]));
}

function cardSubjectLabel(id) {
  const info = catalogInfo.get(`6:${id}`) || catalogInfo.get(`13:${id}`);
  return info ? info.name : `卡牌 ${id}`;
}

function rewardKindName(reward) {
  const info = catalogInfo.get(contentKey(reward));
  const kind = info?.stamp_kind ? `stamp_${info.stamp_kind}` : info?.kind || REWARD_TYPE_KIND[reward.type];
  return pickerKindNames[kind] || `类型 ${reward.type}`;
}

// Honors have no art, so they get no thumbnail and their name is drawn as a title plate.
function rewardThumb(reward) {
  const info = catalogInfo.get(contentKey(reward));
  if ((info?.kind || REWARD_TYPE_KIND[reward.type]) === 'honor') return '';
  return html`<span class="thumb rw-thumb">${raw(info ? catalogImage(info) : image('', contentLabel(reward)))}</span>`;
}

function rewardNameHTML(reward) {
  const info = catalogInfo.get(contentKey(reward));
  return info?.kind === 'honor' || info?.stamp_kind === 'voice'
    ? raw(catalogEntryName(info, true))
    : contentLabel(reward);
}

function rewardText(reward) {
  const card = reward.type === 6 ? `（Lv${reward.card_lv || 1}／名声${reward.card_fame || 1}）` : '';
  return `${contentLabel(reward)} ×${num(reward.num)}${card}`;
}

const rewardListText = (rewards) => rewards.map(rewardText).join('、') || '无奖励';

/** Compact chips for summaries (outline rows, previews). */
function rewardChipsHTML(rewards, limit = 3) {
  if (!rewards.length) return html`<span class="rw-none">未设置奖励</span>`;
  const shown = rewards.slice(0, limit);
  return html`
    ${shown.map((r) => html`<span class="rw-chip">${contentLabel(r)}<b>×${num(r.num)}</b></span>`)}
    ${rewards.length > limit && html`<span class="rw-more">+${rewards.length - limit}</span>`}
  `;
}

/**
 * Editable reward list. Inputs carry data-reward-* attributes; the owner forwards its delegated
 * input/click events to applyRewardEvent() and re-renders on structural changes.
 */
function rewardListHTML(rewards, { locked = false, max = 10, note = '' } = {}) {
  const cardFields = [
    ['card_lv', 'Lv', 1],
    ['card_fame', '名声', 1],
    ['card_love', '忠诚', 0],
  ];
  return html`
    <div class="rw">
      ${
        rewards.length
          ? html`
            <ul class="rw-list">
              ${rewards.map(
                (r, i) => html`
                  <li class="rw-item">
                    ${rewardThumb(r)}
                    <span class="rw-name">
                      <b title="${contentLabel(r)}">${rewardNameHTML(r)}</b>
                      <small>${rewardKindName(r)}${r.reward_typeid ? ` · ${r.reward_typeid}` : ''}</small>
                    </span>
                    <label class="rw-field">
                      <span>数量</span>
                      <input
                        type="number"
                        min="1"
                        max="${rewardMax(r.type)}"
                        value="${r.num}"
                        data-reward-index="${i}"
                        data-reward-field="num"
                        aria-label="${`${contentLabel(r)}数量`}"
                        ${attr(locked, 'disabled')}
                      />
                    </label>
                    ${
                      r.type === 6 &&
                      cardFields.map(([field, label, min]) => {
                        const fixed =
                          field === 'card_lv' ? catalogInfo.get(contentKey(r))?.fixed_card_level : 0;
                        return html`
                        <label class="rw-field narrow">
                          <span>${label}</span>
                          <input
                            type="number"
                            min="${min}"
                            value="${fixed || (r[field] ?? min)}"
                            data-reward-index="${i}"
                            data-reward-field="${field}"
                            aria-label="${`${contentLabel(r)}${label}`}"
                            title="${fixed ? '炼金技能卡获得即满级' : ''}"
                            ${attr(locked || !!fixed, 'disabled')}
                          />
                        </label>
                      `;
                      })
                    }
                    <button
                      type="button"
                      class="x-button"
                      data-reward-remove="${i}"
                      aria-label="${`移除${contentLabel(r)}`}"
                      ${attr(locked, 'disabled')}
                    >
                      <svg class="icon"><use href="#i-close" /></svg>
                    </button>
                  </li>
                `,
              )}
            </ul>
          `
          : html`<p class="rw-empty">尚未添加奖励${note ? `。${note}` : ''}</p>`
      }
      <div class="rw-foot">
        <button
          type="button"
          class="add-chip"
          data-reward-add
          ${attr(locked || rewards.length >= max, 'disabled')}
        >
          <svg class="icon"><use href="#i-plus" /></svg>添加奖励
        </button>
        <span class="hint">${rewards.length} / ${max} 份</span>
      </div>
    </div>
  `;
}

/** Applies an input or click inside a reward list. Returns 'value', 'structure' or null. */
function applyRewardEvent(event, rewards) {
  const field = event.target.closest('[data-reward-field]');
  if (field && event.type === 'input') {
    rewards[Number(field.dataset.rewardIndex)][field.dataset.rewardField] = Number(field.value);
    return 'value';
  }
  const remove = event.target.closest('[data-reward-remove]');
  if (remove && event.type === 'click') {
    rewards.splice(Number(remove.dataset.rewardRemove), 1);
    return 'structure';
  }
  return null;
}

const MAIL_REWARD_KINDS = [
  'currency',
  'card',
  'material',
  'item',
  'sphere',
  'buddy',
  'costume',
  'stamp',
  'honor',
];

/** Opens the shared picker and appends the chosen rewards at their acquisition level. */
function pickRewards(rewards, { max = 10, kinds = MAIL_REWARD_KINDS, accept, title, onAdded }) {
  openContentPicker(
    (rows) => {
      if (rewards.length + rows.length > max) {
        throw new Error(`最多 ${max} 份奖励，当前已有 ${rewards.length} 份`);
      }
      rememberCatalogRows(rows);
      rewards.push(...rows.map(contentReward));
      onAdded();
    },
    kinds,
    { accept, title },
  );
}

/** Validates reward quantities before a request; returns problem texts. */
function rewardProblems(rewards, label = '') {
  const problems = [];
  rewards.forEach((r) => {
    const max = rewardMax(r.type);
    if (!Number.isInteger(r.num) || r.num < 1 || r.num > max) {
      problems.push(`${label}${contentLabel(r)} 的数量须为 1–${num(max)}`);
    }
  });
  return problems;
}

// Searchable select -------------------------------------------------------------------------------
// A <select data-search="placeholder"> is shown as one text box. Focusing it lists every option,
// typing narrows the list at once, and picking one sets the select and fires its change event, so
// the page's own handlers keep working. The select stays in the page, hidden, as the value holder;
// its options, disabled/hidden state and invalid mark are mirrored onto the box.

const COMBO_LIMIT = 200;
const combo = { input: null, rows: [], active: -1 };
let comboPop = null;

const comboLabel = (select) => {
  const option = select.selectedOptions[0];
  return option && option.value !== '' ? option.textContent.trim() : '';
};

function syncCombo(select) {
  const input = select.comboInput;
  if (!input) return;
  if (input !== combo.input) input.value = comboLabel(select);
  input.title = input.value;
  input.disabled = select.disabled;
  input.classList.toggle('invalid', select.classList.contains('invalid'));
  input.parentElement.hidden = select.hidden;
}

/** Wraps one select; returns its text box. Safe to call again on an already wrapped select. */
function enhanceSelect(select) {
  if (select.comboInput) return select.comboInput;
  const box = document.createElement('span');
  box.className = 'combo';
  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'combo-input';
  input.autocomplete = 'off';
  input.spellcheck = false;
  input.placeholder = select.dataset.search || '输入名称或 ID 搜索';
  input.setAttribute('role', 'combobox');
  input.setAttribute('aria-autocomplete', 'list');
  input.setAttribute('aria-expanded', 'false');
  input.setAttribute('aria-label', select.getAttribute('aria-label') || input.placeholder);
  if (select.id) {
    input.id = `${select.id}-search`;
    document.querySelectorAll(`label[for="${select.id}"]`).forEach((label) => (label.htmlFor = input.id));
  }
  select.before(box);
  box.append(input, select);
  select.tabIndex = -1;
  // Pages also set the value in code (a refused switch, a reload); the box follows those too.
  for (const key of ['value', 'selectedIndex']) {
    const native = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, key);
    Object.defineProperty(select, key, {
      configurable: true,
      get: () => native.get.call(select),
      set: (value) => {
        native.set.call(select, value);
        syncCombo(select);
      },
    });
  }
  select.comboInput = input;
  input.comboSelect = select;
  new MutationObserver(() => syncCombo(select)).observe(select, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ['disabled', 'hidden', 'class'],
  });
  syncCombo(select);
  return input;
}

const enhanceSelects = (root = document) =>
  root.querySelectorAll('select[data-search]').forEach(enhanceSelect);

function comboPopup() {
  if (comboPop) return comboPop;
  comboPop = document.createElement('ul');
  comboPop.id = 'combo-pop';
  comboPop.className = 'combo-pop';
  comboPop.setAttribute('role', 'listbox');
  comboPop.hidden = true;
  // mousedown keeps the focus in the text box, so the pick happens before it blurs.
  comboPop.addEventListener('mousedown', (event) => {
    event.preventDefault();
    const row = event.target.closest('[data-index]');
    if (row) comboPick(Number(row.dataset.index));
  });
  document.body.append(comboPop);
  return comboPop;
}

/** The option text with every query word marked, so a row shows why it matched. */
function comboHighlight(text, query) {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const lower = text.toLowerCase();
  const hit = new Array(text.length).fill(false);
  for (const word of words) {
    for (let i = lower.indexOf(word); i >= 0; i = lower.indexOf(word, i + word.length)) {
      hit.fill(true, i, i + word.length);
    }
  }
  const parts = [];
  for (let start = 0, i = 1; i <= text.length; i++) {
    if (i < text.length && hit[i] === hit[start]) continue;
    const part = text.slice(start, i);
    parts.push(hit[start] ? html`<mark>${part}</mark>` : part);
    start = i;
  }
  return parts;
}

function comboOpen(input, query = '') {
  const select = input.comboSelect;
  const options = [...select.options].filter((o) => o.value !== '' && !o.disabled);
  // data-terms adds words an option should be found by without showing them (other IDs of a group).
  const rows = query
    ? options.filter((o) => matchesWords(`${o.textContent} ${o.dataset.terms || ''}`, query))
    : options;
  combo.input = input;
  combo.rows = rows;
  combo.active = query
    ? 0
    : Math.max(
        0,
        rows.findIndex((o) => o.selected),
      );
  const pop = comboPopup();
  const shown = rows.slice(0, COMBO_LIMIT);
  setHTML(
    pop,
    shown.length
      ? [
          shown.map(
            (o, i) =>
              html`<li role="option" id="combo-option-${i}" data-index="${i}" class="${o.selected ? 'chosen' : ''}">
                ${comboHighlight(o.textContent.trim(), query)}
              </li>`,
          ),
          rows.length > shown.length
            ? html`<li class="combo-more">另有 ${num(rows.length - shown.length)} 项，继续输入以缩小范围</li>`
            : '',
        ]
      : html`<li class="combo-more">没有匹配“${query}”的选项</li>`,
  );
  pop.hidden = false;
  input.setAttribute('aria-expanded', 'true');
  input.setAttribute('aria-controls', pop.id);
  comboPlace();
  comboMark();
}

function comboPlace() {
  if (!combo.input || comboPop.hidden) return;
  const r = combo.input.getBoundingClientRect();
  const width = Math.max(r.width, 260);
  const below = innerHeight - r.bottom;
  const up = below < 220 && r.top > below;
  Object.assign(comboPop.style, {
    left: `${Math.max(8, Math.min(r.left, innerWidth - width - 8))}px`,
    width: `${width}px`,
    maxHeight: `${Math.min(340, (up ? r.top : below) - 12)}px`,
    top: up ? '' : `${r.bottom + 4}px`,
    bottom: up ? `${innerHeight - r.top + 4}px` : '',
  });
}

function comboMark() {
  const rows = comboPop.querySelectorAll('[data-index]');
  rows.forEach((row, i) => row.setAttribute('aria-selected', String(i === combo.active)));
  const row = rows[combo.active];
  if (row) {
    row.scrollIntoView({ block: 'nearest' });
    combo.input.setAttribute('aria-activedescendant', row.id);
  } else combo.input.removeAttribute('aria-activedescendant');
}

function comboClose() {
  const input = combo.input;
  if (!input) return;
  combo.input = null;
  comboPop.hidden = true;
  input.setAttribute('aria-expanded', 'false');
  input.removeAttribute('aria-activedescendant');
  syncCombo(input.comboSelect);
}

function comboPick(index) {
  const input = combo.input;
  const option = combo.rows[index];
  if (!input || !option) return;
  const select = input.comboSelect;
  comboClose();
  if (select.value !== option.value) {
    select.value = option.value;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  }
  // A change handler may refuse the pick (for example after an unsaved-draft prompt) and reset the value.
  syncCombo(select);
}

// Typing in the box is a search, not an edit: page-level input/change handlers never see it.
document.addEventListener(
  'input',
  (event) => {
    if (!event.target.comboSelect) return;
    event.stopPropagation();
    comboOpen(event.target, event.target.value.trim());
  },
  true,
);
document.addEventListener('change', (event) => event.target.comboSelect && event.stopPropagation(), true);
document.addEventListener('focusin', (event) => {
  if (!event.target.comboSelect) return;
  event.target.select();
  comboOpen(event.target);
});
document.addEventListener('focusout', (event) => event.target === combo.input && comboClose());
document.addEventListener('mousedown', (event) => {
  const input = event.target.comboSelect ? event.target : null;
  if (input && document.activeElement === input && !combo.input) comboOpen(input);
});
document.addEventListener(
  'keydown',
  (event) => {
    const input = event.target.comboSelect ? event.target : null;
    if (!input) return;
    const open = combo.input === input;
    const shown = Math.min(combo.rows.length, COMBO_LIMIT);
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!open) return comboOpen(input);
      if (!shown) return;
      combo.active = (combo.active + (event.key === 'ArrowDown' ? 1 : -1) + shown) % shown;
      comboMark();
    } else if (event.key === 'Enter' && open) {
      event.preventDefault();
      event.stopPropagation();
      comboPick(combo.active);
    } else if (event.key === 'Escape' && open) {
      event.preventDefault();
      event.stopPropagation();
      comboClose();
      input.select();
    }
  },
  true,
);
window.addEventListener('resize', comboPlace);
document.addEventListener('scroll', (event) => event.target !== comboPop && comboPlace(), true);

enhanceSelects();
