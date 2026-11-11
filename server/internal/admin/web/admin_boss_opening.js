'use strict';
// Boss 发布 › 开场设置: opening COST and burst gauge of 3D 活动, 2D 活动 and 往期, shared by solo and team.
// Each field merges resource default → global → per-BOSS override, and an empty input inherits the level
// above it; a burst gauge of 0 is a real value (an empty gauge). The settings are one document
// (GET/PUT /api/boss-battle-opening, docs/BOSS_BATTLE_OPENING.md) and the PUT replaces it whole, so the
// draft keeps every untouched BOSS override. The tab shares the save bar through bossSettingTabs.opening.

const OP_FIELDS = {
  cost: { key: 'cost_initial', label: '开局 COST', short: 'COST', unit: 'COST' },
  gauge: { key: 'burst_gauge_initial', label: '开场圣剑槽', short: '圣剑槽', unit: '/ 300' },
};
const OP_LIMITS = {
  cost_initial_min: 3,
  cost_initial_max: 10,
  burst_gauge_initial_min: 0,
  burst_gauge_initial_max: 300,
};
const OP_GAUGE = { normal_threshold: 100, maximum: 300 };
const opEmpty = () => ({ cost: '', gauge: '' });

const bossOpening = {
  data: null, // last GET
  groups: [], // [{ id, name, past, rows }] by group ID; rows by BOSS ID (the difficulty order)
  rows: new Map(), // boss_id → row
  saved: { global: opEmpty(), bosses: new Map() }, // the stored config as input text
  draft: { global: opEmpty(), bosses: new Map() }, // boss_id → { cost, gauge }; a missing entry inherits both
  selected: 0, // group ID open in the editor
  query: '',
  filter: '', // '' | 'set' | 'changed'
};

// Model ------------------------------------------------------------------------------------------

const opLimits = () => ({ ...OP_LIMITS, ...bossOpening.data?.limits });
const opGauge = () => ({ ...OP_GAUGE, ...bossOpening.data?.burst_gauge });
const opText = (value) => (value === null || value === undefined ? '' : String(value));
const opFromRule = (rule) => ({ cost: opText(rule?.cost_initial), gauge: opText(rule?.burst_gauge_initial) });
const opBossDraft = (id) => bossOpening.draft.bosses.get(id) || opEmpty();
const opBossSaved = (id) => bossOpening.saved.bosses.get(id) || opEmpty();
const opHasValue = (d) => d.cost.trim() !== '' || d.gauge.trim() !== '';

/** Input text → { value: null (inherit) | integer, error } against the server's limits. */
function opParse(field, text) {
  const t = String(text ?? '').trim();
  if (t === '') return { value: null, error: '' };
  const limits = opLimits(),
    min = limits[`${OP_FIELDS[field].key}_min`],
    max = limits[`${OP_FIELDS[field].key}_max`],
    n = Number(t);
  if (!/^\d+$/.test(t) || !Number.isSafeInteger(n) || n < min || n > max)
    return { value: null, error: `须为 ${min}～${max} 的整数` };
  return { value: n, error: '' };
}

function opCanonical(field, text) {
  const { value, error } = opParse(field, text);
  return error ? `?${String(text).trim()}` : opText(value);
}

const opChanged = (draft, saved, field) => opCanonical(field, draft[field]) !== saved[field];
const opGlobalChanged = (field) => opChanged(bossOpening.draft.global, bossOpening.saved.global, field);
const opBossChanged = (id, field) => opChanged(opBossDraft(id), opBossSaved(id), field);
const opBossAnyChanged = (id) => opBossChanged(id, 'cost') || opBossChanged(id, 'gauge');
const opTouchedIDs = () => new Set([...bossOpening.draft.bosses.keys(), ...bossOpening.saved.bosses.keys()]);

function bossOpeningDirty() {
  if (!bossOpening.data) return false;
  if (opGlobalChanged('cost') || opGlobalChanged('gauge')) return true;
  for (const id of opTouchedIDs()) if (opBossAnyChanged(id)) return true;
  return false;
}

function bossOpeningReady() {
  return !!bossOpening.data;
}

/** [where an empty field takes its value from, that value] under the current draft. */
function opInherited(row, field) {
  const { value } = opParse(field, bossOpening.draft.global[field]);
  return value === null ? ['资源默认', row.defaults[OP_FIELDS[field].key]] : ['全局', value];
}

function opCurrent(row, field) {
  const { value } = opParse(field, opBossDraft(row.boss_id)[field]);
  return value === null ? opInherited(row, field)[1] : value;
}

/** Every distinct resource default of a field across the listed BOSS, as text. */
function opDefaultsText(field) {
  const key = OP_FIELDS[field].key,
    values = [...new Set((bossOpening.data?.bosses || []).map((r) => r.defaults[key]))].sort((a, b) => a - b);
  if (!values.length) return '资源默认';
  return values.length === 1
    ? `资源默认 ${values[0]}`
    : `资源默认 各 BOSS 不同（${values[0]}～${values[values.length - 1]}）`;
}

async function loadBossOpening({ keep = false } = {}) {
  const changedGlobal = {},
    changedBosses = new Map();
  if (keep) {
    for (const field of Object.keys(OP_FIELDS))
      if (opGlobalChanged(field)) changedGlobal[field] = bossOpening.draft.global[field];
    for (const id of opTouchedIDs()) if (opBossAnyChanged(id)) changedBosses.set(id, { ...opBossDraft(id) });
  }
  const data = await api('/api/boss-battle-opening');
  bossOpening.data = data;
  bossOpening.rows = new Map(data.bosses.map((r) => [r.boss_id, r]));
  const groups = new Map();
  for (const r of data.bosses) {
    const g = groups.get(r.group_id) || {
      id: r.group_id,
      name: r.name,
      past: r.category === 'past',
      rows: [],
    };
    g.rows.push(r);
    groups.set(r.group_id, g);
  }
  bossOpening.groups = [...groups.values()].sort((a, b) => a.id - b.id);
  for (const g of bossOpening.groups) g.rows.sort((a, b) => a.boss_id - b.boss_id);
  const saved = {
    global: opFromRule(data.config.global),
    bosses: new Map(
      Object.entries(data.config.bosses || {}).map(([id, rule]) => [Number(id), opFromRule(rule)]),
    ),
  };
  bossOpening.saved = saved;
  bossOpening.draft = {
    global: { ...saved.global, ...changedGlobal },
    bosses: new Map([...saved.bosses].map(([id, d]) => [id, { ...d }])),
  };
  const gone = [];
  for (const [id, d] of changedBosses)
    bossOpening.rows.has(id) ? bossOpening.draft.bosses.set(id, d) : gone.push(id);
  if (!groups.has(bossOpening.selected))
    bossOpening.selected = (
      bossOpening.groups.find((g) => g.rows.some((r) => opHasValue(opBossDraft(r.boss_id)))) ||
      bossOpening.groups[0]
    )?.id;
  return { kept: Object.keys(changedGlobal).length + changedBosses.size - gone.length, gone };
}

// Text and small visuals --------------------------------------------------------------------------

/** One gauge: 0–maximum with the burst line marked; `ready` once the value reaches it. */
function opGaugeBar(value, small = false) {
  const { maximum, normal_threshold } = opGauge(),
    fill = Math.max(0, Math.min(100, (value / maximum) * 100)),
    line = (normal_threshold / maximum) * 100;
  return html`<span
    class="op-gauge${small ? ' sm' : ''}${value >= normal_threshold ? ' ready' : ''}"
    style="--fill:${fill}%;--line:${line}%"
    aria-hidden="true"
    ><i></i
  ></span>`;
}

function opGaugeWords(value) {
  const { maximum, normal_threshold } = opGauge();
  if (value >= maximum) return ['ok', '满槽，首回合可解放'];
  if (value >= normal_threshold) return ['ok', '首回合可解放'];
  return value ? ['', `距可解放差 ${normal_threshold - value}`] : ['', '开场为空'];
}

/** The review cell of one stored field: its value, or where an empty field inherits from. */
function opReviewValue(field, text, scope) {
  const { value } = opParse(field, text);
  if (value === null) return scope === 'global' ? '资源默认' : '继承';
  return field === 'gauge' ? `${value}（${opGaugeWords(value)[1]}）` : String(value);
}

// Rendering --------------------------------------------------------------------------------------

function syncOpeningGlobal() {
  const limits = opLimits();
  for (const field of Object.keys(OP_FIELDS)) {
    const input = $(`#op-global-${field}`),
      text = bossOpening.draft.global[field],
      { value, error } = opParse(field, text),
      note = $(`#op-global-${field}-note`);
    if (document.activeElement !== input && input.value !== text) input.value = text;
    input.min = limits[`${OP_FIELDS[field].key}_min`];
    input.max = limits[`${OP_FIELDS[field].key}_max`];
    input.disabled = !bossOpening.data;
    input.classList.toggle('invalid', !!error);
    error ? input.setAttribute('aria-invalid', 'true') : input.removeAttribute('aria-invalid');
    input.closest('.setting-control').classList.toggle('changed', opGlobalChanged(field));
    note.className = 'op-note' + (error ? ' bad' : '');
    note.textContent = error
      ? error
      : value === null
        ? `留空：每个 BOSS 用${opDefaultsText(field)}`
        : field === 'gauge'
          ? `全部 BOSS 开场 ${value}，${opGaugeWords(value)[1]}；单独设置的 BOSS 除外`
          : `全部 BOSS 开局 ${value} COST；单独设置的 BOSS 除外`;
  }
  const { value } = opParse('gauge', bossOpening.draft.global.gauge),
    shown = value ?? (bossOpening.data?.bosses[0]?.defaults.burst_gauge_initial || 0);
  setHTML($('#op-global-gauge-bar'), opGaugeBar(shown));
}

function opGroupStats(group) {
  let set = 0,
    changed = false;
  for (const r of group.rows) {
    if (opHasValue(opBossDraft(r.boss_id))) set++;
    if (opBossAnyChanged(r.boss_id)) changed = true;
  }
  return { set, changed };
}

const opRange = (rows) =>
  rows.length <= 3
    ? rows.map((r) => r.difficulty).join(' / ')
    : `${rows[0].difficulty} – ${rows[rows.length - 1].difficulty}`;

function opListItemHTML(group, stats) {
  return html`<button
    type="button"
    class="ws-item${group.id === bossOpening.selected ? ' selected' : ''}"
    data-group="${group.id}"
    aria-pressed="${group.id === bossOpening.selected}"
  >
    <span class="ws-item-title"
      ><b title="${group.name}">${group.name}</b>${group.past ? stateTag('往期') : ''}${
        stats.set ? stateTag(`单独设置 ${stats.set}`, 'info') : ''
      }${stats.changed ? stateTag('未保存', 'warn') : ''}</span
    >
    <span class="ws-item-meta">${group.rows.length} 个难度 · ${opRange(group.rows)}</span>
  </button>`;
}

function renderOpeningList() {
  const f = bossOpening.filter,
    items = [];
  for (const g of bossOpening.groups) {
    const stats = opGroupStats(g);
    if (f === 'set' && !stats.set) continue;
    if (f === 'changed' && !stats.changed) continue;
    const words = `${g.name} ${g.id} ${g.rows.map((r) => `${r.difficulty} ${r.boss_id}`).join(' ')}`;
    if (matchesWords(words, bossOpening.query)) items.push(opListItemHTML(g, stats));
  }
  $('#op-list-count').textContent = `${items.length} / ${bossOpening.groups.length}`;
  setHTML(
    $('#op-list'),
    items.length ? items : html`<p class="ws-list-empty">没有匹配的 BOSS，换个关键词或清除筛选。</p>`,
  );
}

/** Repaints one list entry in place, keeping the list's scroll position and the other entries. */
function syncOpeningListItem(groupID) {
  const button = $(`#op-list [data-group="${groupID}"]`),
    group = bossOpening.groups.find((g) => g.id === groupID);
  if (button && group) button.outerHTML = String(opListItemHTML(group, opGroupStats(group)));
}

function opInputHTML(row, field) {
  const f = OP_FIELDS[field],
    limits = opLimits();
  return html`<td class="op-cell" data-cell="${field}">
    <div class="op-field">
      <div class="input-unit">
        <input
          type="number"
          inputmode="numeric"
          min="${limits[`${f.key}_min`]}"
          max="${limits[`${f.key}_max`]}"
          step="1"
          data-field="${field}"
          value="${opBossDraft(row.boss_id)[field]}"
          aria-label="${row.difficulty} ${f.label}"
        /><span>${f.unit}</span>
      </div>
      <small class="op-note" data-note="${field}"></small>
    </div>
  </td>`;
}

function opRowHTML(row) {
  return html`<tr data-boss="${row.boss_id}">
    <td class="op-name"><b>${row.difficulty}</b><code>${row.boss_id}</code><span class="op-tags"></span></td>
    ${opInputHTML(row, 'cost')} ${opInputHTML(row, 'gauge')}
    <td class="op-now"></td>
    <td class="op-action">
      <button class="ghost" type="button" data-reset title="两项都改回继承">恢复继承</button>
    </td>
  </tr>`;
}

function paintOpeningRow(tr, { values = false } = {}) {
  const row = bossOpening.rows.get(Number(tr.dataset.boss)),
    draft = opBossDraft(row.boss_id);
  setHTML(tr.querySelector('.op-tags'), [
    opHasValue(draft) ? stateTag('单独设置', 'info') : '',
    opBossAnyChanged(row.boss_id) ? stateTag('未保存', 'warn') : '',
  ]);
  for (const field of Object.keys(OP_FIELDS)) {
    const cell = tr.querySelector(`[data-cell="${field}"]`),
      input = cell.querySelector('input'),
      note = cell.querySelector('.op-note'),
      { value, error } = opParse(field, draft[field]),
      [source, inherited] = opInherited(row, field);
    if (values && input.value !== draft[field]) input.value = draft[field];
    input.placeholder = `继承 ${inherited}`;
    input.classList.toggle('invalid', !!error);
    error ? input.setAttribute('aria-invalid', 'true') : input.removeAttribute('aria-invalid');
    cell.classList.toggle('changed', opBossChanged(row.boss_id, field));
    note.className = 'op-note' + (error ? ' bad' : '');
    note.textContent = error || (value === null ? `来自${source}` : '');
  }
  const cost = opCurrent(row, 'cost'),
    gauge = opCurrent(row, 'gauge'),
    [tone, words] = opGaugeWords(gauge);
  setHTML(
    tr.querySelector('.op-now'),
    html`<span class="op-now-line"
        ><span><b>${cost}</b> COST</span><span class="op-now-gauge">${opGaugeBar(gauge, true)}<b>${gauge}</b></span></span
      ><small class="op-now-words ${tone}">${words}</small>`,
  );
  tr.querySelector('[data-reset]').disabled = !opHasValue(draft);
}

function renderOpeningEditor() {
  const group = bossOpening.groups.find((g) => g.id === bossOpening.selected),
    editor = $('#op-editor');
  if (!group) {
    setHTML(
      editor,
      html`<div class="ws-empty"><b>还没有选择 BOSS</b><span>从左侧选择一个 BOSS 后，可逐个难度单独设置。</span></div>`,
    );
    return;
  }
  const stats = opGroupStats(group),
    limits = opLimits();
  setHTML(
    editor,
    html`<div class="ws-head">
        <div class="ws-title">
          <span class="eyebrow">组 ${group.id}${group.past ? ' · 往期' : ''}</span>
          <h2>${group.name}</h2>
          <div class="ws-tags">
            ${stateTag(`${group.rows.length} 个难度`)}
            ${stats.set ? stateTag(`${stats.set} 个难度单独设置`, 'info') : stateTag('全部继承')}
          </div>
        </div>
      </div>
      <section class="ws-section">
        <div class="op-bulk">
          <span class="op-bulk-label">本组统一填写</span>
          <div class="input-unit">
            <input
              id="op-bulk-cost"
              type="number"
              inputmode="numeric"
              min="${limits.cost_initial_min}"
              max="${limits.cost_initial_max}"
              step="1"
              placeholder="${limits.cost_initial_min}～${limits.cost_initial_max}"
              aria-label="本组开局 COST"
            /><span>COST</span>
          </div>
          <div class="input-unit">
            <input
              id="op-bulk-gauge"
              type="number"
              inputmode="numeric"
              min="${limits.burst_gauge_initial_min}"
              max="${limits.burst_gauge_initial_max}"
              step="1"
              placeholder="${limits.burst_gauge_initial_min}～${limits.burst_gauge_initial_max}"
              aria-label="本组开场圣剑槽"
            /><span>/ 300</span>
          </div>
          <button class="secondary sm" type="button" data-bulk="apply">填入各难度</button>
          <button class="secondary sm" type="button" data-bulk="reset" ${attr(!stats.set, 'disabled')}>
            本组恢复继承
          </button>
          <span class="op-bulk-note" id="op-bulk-note" aria-live="polite">空着的一项不改</span>
        </div>
        <div class="table-wrap">
          <table class="op-table">
            <thead>
              <tr>
                <th>难度</th>
                <th>开局 COST</th>
                <th>开场圣剑槽</th>
                <th>开场时</th>
                <th><span class="sr-only">恢复继承</span></th>
              </tr>
            </thead>
            <tbody>
              ${group.rows.map(opRowHTML)}
            </tbody>
          </table>
        </div>
        <p class="hint">留空继承全局设置，全局也留空时用资源默认值。自卡组入口跟随对应难度。</p>
      </section>`,
  );
  for (const tr of $$('#op-editor tr[data-boss]')) paintOpeningRow(tr);
}

function syncOpeningSummary() {
  const set = [...bossOpening.draft.bosses.values()].filter(opHasValue).length;
  $('#op-count').textContent = bossOpening.data
    ? `${set ? `${set} 个难度单独设置` : '没有单独设置'} · 共 ${num(bossOpening.rows.size)} 个难度`
    : '';
  $('#op-reset-all').disabled =
    !bossOpening.data || (!set && !opHasValue(bossOpening.draft.global) && !bossOpeningDirty());
  if (typeof updateBossDraftState === 'function') updateBossDraftState();
}

/** Full redraw of the tab: on load, after a save and when the tab opens. */
function renderBossOpening() {
  const data = bossOpening.data;
  $('#boss-revision').textContent = `revision ${data?.revision || 0}`;
  $('#save-policy').disabled = policyBusy('bosses') || !data;
  syncOpeningGlobal();
  if (!data) return;
  renderOpeningList();
  renderOpeningEditor();
  syncOpeningSummary();
}

// Editing ----------------------------------------------------------------------------------------

function opSetBoss(id, patch) {
  const next = { ...opBossDraft(id), ...patch };
  opHasValue(next) ? bossOpening.draft.bosses.set(id, next) : bossOpening.draft.bosses.delete(id);
}

/** After an edit inside the open group: its rows, its list entry and the counters. */
function syncOpeningGroup({ values = false } = {}) {
  for (const tr of $$('#op-editor tr[data-boss]')) paintOpeningRow(tr, { values });
  const group = bossOpening.groups.find((g) => g.id === bossOpening.selected);
  if (group) {
    const stats = opGroupStats(group),
      reset = $('#op-editor [data-bulk="reset"]');
    if (reset) reset.disabled = !stats.set;
  }
  syncOpeningListItem(bossOpening.selected);
  syncOpeningSummary();
}

function onOpeningEditorInput(event) {
  const input = event.target.closest('#op-editor tr[data-boss] input[data-field]');
  if (input) {
    opSetBoss(Number(input.closest('tr').dataset.boss), { [input.dataset.field]: input.value });
    syncOpeningGroup();
  } else if (event.target.closest('#op-bulk-cost, #op-bulk-gauge')) {
    clearInvalid($('#op-editor .op-bulk'));
  }
}

function applyOpeningBulk() {
  const group = bossOpening.groups.find((g) => g.id === bossOpening.selected),
    note = $('#op-bulk-note'),
    patch = {};
  clearInvalid($('#op-editor .op-bulk'));
  for (const field of Object.keys(OP_FIELDS)) {
    const input = $(`#op-bulk-${field}`);
    if (input.value.trim() === '') continue;
    const { value, error } = opParse(field, input.value);
    if (error) {
      markInvalid(input, `${OP_FIELDS[field].label}${error}`);
      note.className = 'op-bulk-note bad';
      note.textContent = `${OP_FIELDS[field].label}${error}`;
      input.focus();
      return;
    }
    patch[field] = String(value);
  }
  if (!Object.keys(patch).length) {
    note.className = 'op-bulk-note bad';
    note.textContent = '先填写开局 COST 或开场圣剑槽';
    $('#op-bulk-cost').focus();
    return;
  }
  for (const r of group.rows) opSetBoss(r.boss_id, patch);
  syncOpeningGroup({ values: true });
  note.className = 'op-bulk-note ok';
  note.textContent = `已填入 ${group.rows.length} 个难度，保存后生效`;
}

function onOpeningEditorClick(event) {
  const reset = event.target.closest('#op-editor tr[data-boss] [data-reset]');
  if (reset) {
    opSetBoss(Number(reset.closest('tr').dataset.boss), opEmpty());
    syncOpeningGroup({ values: true });
    return;
  }
  const bulk = event.target.closest('#op-editor [data-bulk]');
  if (!bulk) return;
  if (bulk.dataset.bulk === 'apply') return applyOpeningBulk();
  const group = bossOpening.groups.find((g) => g.id === bossOpening.selected);
  for (const r of group.rows) opSetBoss(r.boss_id, opEmpty());
  syncOpeningGroup({ values: true });
  $('#op-bulk-note').className = 'op-bulk-note ok';
  $('#op-bulk-note').textContent = `已将 ${group.rows.length} 个难度改回继承，保存后生效`;
}

function onOpeningGlobalInput(event) {
  const field = event.target.id === 'op-global-cost' ? 'cost' : 'gauge';
  bossOpening.draft.global[field] = event.target.value;
  syncOpeningGlobal();
  for (const tr of $$('#op-editor tr[data-boss]')) paintOpeningRow(tr);
  syncOpeningSummary();
}

async function resetAllOpening() {
  if (
    !(await askConfirm(
      '全部恢复默认？',
      '清空全局设置和所有 BOSS 的单独设置，全部改用资源默认值。只修改草稿，保存时会再列出每一处变化。',
      { ok: '全部恢复默认', danger: true },
    ))
  )
    return;
  bossOpening.draft.global = opEmpty();
  for (const id of opTouchedIDs()) opSetBoss(id, opEmpty());
  renderBossOpening();
}

// Saving -----------------------------------------------------------------------------------------

function opRule(draft) {
  const rule = {};
  for (const field of Object.keys(OP_FIELDS)) {
    const { value } = opParse(field, draft[field]);
    if (value !== null) rule[OP_FIELDS[field].key] = value;
  }
  return rule;
}

/** The whole document for the PUT: every BOSS override still set, untouched ones included. */
function opConfig() {
  const bosses = {};
  for (const [id, draft] of [...bossOpening.draft.bosses].sort((a, b) => a[0] - b[0])) {
    const rule = opRule(draft);
    if (Object.keys(rule).length) bosses[String(id)] = rule;
  }
  return { global: opRule(bossOpening.draft.global), bosses };
}

const opBossLabel = (row) => `${row.name} · ${row.difficulty}（${row.boss_id}）`;

function opReviewSections() {
  const global = [],
    bosses = [];
  for (const field of Object.keys(OP_FIELDS))
    if (opGlobalChanged(field))
      global.push([
        OP_FIELDS[field].label,
        opReviewValue(field, bossOpening.saved.global[field], 'global'),
        opReviewValue(field, bossOpening.draft.global[field], 'global'),
      ]);
  const ids = [...opTouchedIDs()].filter(opBossAnyChanged).sort((a, b) => a - b);
  for (const id of ids)
    for (const field of Object.keys(OP_FIELDS))
      if (opBossChanged(id, field))
        bosses.push([
          `${opBossLabel(bossOpening.rows.get(id))} · ${OP_FIELDS[field].short}`,
          opReviewValue(field, opBossSaved(id)[field], 'boss'),
          opReviewValue(field, opBossDraft(id)[field], 'boss'),
        ]);
  return [
    { title: '全局', rows: global },
    { title: '按 BOSS 单独设置', rows: bosses },
  ];
}

/** Every invalid field as text; the first invalid BOSS group is opened so its inputs show the error. */
function opProblems() {
  const problems = [];
  for (const field of Object.keys(OP_FIELDS)) {
    const { error } = opParse(field, bossOpening.draft.global[field]);
    if (error) problems.push(`全局${OP_FIELDS[field].label}${error}`);
  }
  let first = null;
  for (const [id, draft] of bossOpening.draft.bosses)
    for (const field of Object.keys(OP_FIELDS)) {
      const { error } = opParse(field, draft[field]);
      if (!error) continue;
      const row = bossOpening.rows.get(id);
      problems.push(`${opBossLabel(row)}的${OP_FIELDS[field].short}${error}`);
      first ??= row.group_id;
    }
  if (first && !$(`#op-editor tr[data-boss] .invalid`)) {
    bossOpening.selected = first;
    bossOpening.query = $('#op-search').value = '';
    bossOpening.filter = $('#op-filter').value = '';
    renderOpeningList();
    renderOpeningEditor();
  }
  return problems;
}

async function reloadBossOpeningKeeping() {
  if (policyBusy('bosses')) return;
  state.loading.add('bosses');
  updatePolicyControls();
  try {
    const { kept, gone } = await loadBossOpening({ keep: true });
    renderBossOpening();
    showViewAlert('bosses', {
      kind: 'info',
      title: '已读取最新设置',
      message:
        (kept ? `保留了你修改的 ${kept} 处设置，请核对后再保存。` : '你的修改与最新设置相同，无需再保存。') +
        (gone.length ? `BOSS ${gone.join('、')} 已不在可设置范围内，其修改已丢弃。` : ''),
    });
  } catch (e) {
    reportError('bosses', e, '载入');
  } finally {
    state.loading.delete('bosses');
    updatePolicyControls();
  }
}

async function saveBossOpening() {
  if (policyBusy('bosses') || !bossOpening.data) return;
  if (!bossOpeningDirty()) return toast('与已保存设置一致，无需保存');
  const problems = opProblems();
  if (problems.length) {
    const limits = opLimits();
    showViewAlert('bosses', {
      title: '不能保存：数值无效',
      message: `${problems.slice(0, 3).join('；')}${problems.length > 3 ? ` 等 ${problems.length} 处` : ''}。开局 COST 须为 ${limits.cost_initial_min}～${limits.cost_initial_max}、开场圣剑槽须为 ${limits.burst_gauge_initial_min}～${limits.burst_gauge_initial_max} 的整数；留空表示继承。`,
    });
    return;
  }
  if (
    !(await reviewChanges({
      title: '保存开场设置',
      sections: opReviewSections(),
      note: '单人和组队共用，只影响之后新开的战斗和新建的房间；已开始的战斗和已建好的房间不变。圣剑杯不受影响。',
    }))
  )
    return;
  state.publishing.add('bosses');
  updatePolicyControls();
  let saved = false;
  try {
    await api('/api/boss-battle-opening', {
      method: 'PUT',
      body: JSON.stringify({ expected_revision: bossOpening.data.revision, config: opConfig() }),
    });
    saved = true;
    state.loaded.delete('audit');
    await loadBossOpening();
    clearViewAlert('bosses');
    renderBossOpening();
    toast('开场设置已保存，之后新开的战斗生效');
  } catch (e) {
    if (saved) reportError('bosses', e, '保存成功，但重新载入');
    else if (e.status === 409)
      showViewAlert('bosses', {
        kind: 'warn',
        title: '保存未完成：设置已在别处修改',
        message:
          '开场设置刚被其他页面或操作改过，这次没有写入。可以读取最新设置并保留你改过的部分，核对后再保存。',
        actions: [{ label: '读取最新并保留我的修改', primary: true, onClick: reloadBossOpeningKeeping }],
      });
    else reportError('bosses', e, '保存');
  } finally {
    state.publishing.delete('bosses');
    updatePolicyControls();
  }
}

// Wiring -----------------------------------------------------------------------------------------

function initBossOpening() {
  for (const id of ['#op-global-cost', '#op-global-gauge'])
    $(id).addEventListener('input', onOpeningGlobalInput);
  $('#op-reset-all').addEventListener('click', resetAllOpening);
  $('#op-search').addEventListener('input', () => {
    bossOpening.query = $('#op-search').value.trim();
    renderOpeningList();
  });
  $('#op-filter').addEventListener('change', () => {
    bossOpening.filter = $('#op-filter').value;
    renderOpeningList();
  });
  $('#op-list').addEventListener('click', (event) => {
    const item = event.target.closest('[data-group]');
    if (!item) return;
    bossOpening.selected = Number(item.dataset.group);
    for (const b of $$('#op-list [data-group]')) {
      const on = b === item;
      b.classList.toggle('selected', on);
      b.setAttribute('aria-pressed', String(on));
    }
    renderOpeningEditor();
  });
  $('#op-editor').addEventListener('input', onOpeningEditorInput);
  $('#op-editor').addEventListener('click', onOpeningEditorClick);
}

initBossOpening();
bossSettingTabs.opening = {
  name: '开场设置',
  load: loadBossOpening,
  render: renderBossOpening,
  save: saveBossOpening,
  dirty: bossOpeningDirty,
  ready: bossOpeningReady,
};
