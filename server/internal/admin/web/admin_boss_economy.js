'use strict';
// Boss 发布 › 体力与经验: one override per difficulty name, shared by 3D 活动, 2D 活动 and 往期 (own-deck entries
// follow their difficulty). An empty field means "use the resource defaults"; a difficulty whose bosses
// differ shows every default value and is never written back from them. The tab shares the page's save bar
// (#save-policy) with the three publication catalogs; admin_ui.js routes to this file when
// state.bossCatalog is 'economy'. GET/PUT /api/boss-economy, contract in docs/BOSS_ECONOMY.md.

const BE_FIELDS = {
  bp: { key: 'bp_use', label: '单人体力', unit: '体力', min: 'bp_use_min', max: 'bp_use_max' },
  exp: { key: 'experience', label: '通关经验', unit: '经验', min: 'experience_min', max: 'experience_max' },
};
const BE_LIMITS = { bp_use_min: 1, bp_use_max: 1000, experience_min: 0, experience_max: 10000000 };
const BE_BATCH = 100; // rows per PUT; the server rejects more
const beCollator = new Intl.Collator('zh-Hans-CN', { numeric: true });

const bossEconomy = {
  data: null, // last GET: { revision, limits, rules }
  rows: [], // display order; each rule plus saved/draft { bp, exp } as input text ('' = default)
  picked: new Set(), // difficulty names ticked for the bulk bar
  bulkNote: null, // [tone, text] after the last bulk action; cleared when the ticks change
  query: '',
  filter: '', // '' | 'set' | 'default' | 'changed'
};

// Model ------------------------------------------------------------------------------------------

const beLimits = () => ({ ...BE_LIMITS, ...bossEconomy.data?.limits });
const beRow = (difficulty) => bossEconomy.rows.find((r) => r.difficulty === difficulty);
const beText = (value) => (value === null || value === undefined ? '' : String(value));

/** Input text → { value: null (default) | integer, error } against the server's limits. */
function beParse(field, text) {
  const t = String(text ?? '').trim();
  if (t === '') return { value: null, error: '' };
  const limits = beLimits(),
    min = limits[BE_FIELDS[field].min],
    max = limits[BE_FIELDS[field].max],
    n = Number(t);
  if (!/^\d+$/.test(t) || !Number.isSafeInteger(n) || n < min || n > max)
    return { value: null, error: `须为 ${num(min)}～${num(max)} 的整数` };
  return { value: n, error: '' };
}

/** Comparable form of an input: '045' and '45' are the same draft. */
function beCanonical(field, text) {
  const { value, error } = beParse(field, text);
  return error ? `?${String(text).trim()}` : beText(value);
}

const beFieldChanged = (row, field) => beCanonical(field, row.draft[field]) !== row.saved[field];
const beChanged = (row) => beFieldChanged(row, 'bp') || beFieldChanged(row, 'exp');
const beHasOverride = (row) => row.saved.bp !== '' || row.saved.exp !== '';

function bossEconomyDirty() {
  return bossEconomy.rows.some(beChanged);
}

function bossEconomyReady() {
  return !!bossEconomy.data;
}

/** Cheapest default stamina first, so 初级 → 超弩级 → the 60-stamina tiers read top to bottom. */
function beOrder(a, b) {
  const low = (r) => Math.min(...r.defaults.bp_use),
    high = (r) => Math.max(...r.defaults.bp_use);
  return low(a) - low(b) || high(a) - high(b) || beCollator.compare(a.difficulty, b.difficulty);
}

function beVisible(row) {
  const f = bossEconomy.filter;
  if (f === 'set' && !beHasOverride(row)) return false;
  if (f === 'default' && beHasOverride(row)) return false;
  if (f === 'changed' && !beChanged(row)) return false;
  return matchesWords(row.difficulty, bossEconomy.query);
}

async function loadBossEconomy({ keep = false } = {}) {
  const kept = new Map(
    keep ? bossEconomy.rows.filter(beChanged).map((r) => [r.difficulty, { ...r.draft }]) : [],
  );
  const data = await api('/api/boss-economy');
  bossEconomy.data = data;
  bossEconomy.rows = data.rules
    .map((rule) => {
      const saved = { bp: beText(rule.override?.bp_use), exp: beText(rule.override?.experience) };
      return { ...rule, saved, draft: kept.get(rule.difficulty) || { ...saved } };
    })
    .sort(beOrder);
  if (!keep) bossEconomy.picked.clear();
  bossEconomy.bulkNote = null;
  const known = new Set(bossEconomy.rows.map((r) => r.difficulty));
  bossEconomy.picked = new Set([...bossEconomy.picked].filter((d) => known.has(d)));
  return {
    kept: [...kept.keys()].filter((d) => known.has(d)),
    gone: [...kept.keys()].filter((d) => !known.has(d)),
  };
}

// Text -------------------------------------------------------------------------------------------

/** Every distinct default, e.g. "45" or "100／150／900"; long lists collapse to a range. */
function beValues(values) {
  if (values.length <= 4) return values.map(num).join('／');
  return `${num(values[0])}～${num(values[values.length - 1])}（${values.length} 种）`;
}

const beDefaultText = (row, field) => {
  const values = row.defaults[BE_FIELDS[field].key];
  return values.length > 1 ? `默认 各副本不同：${beValues(values)}` : `默认 ${beValues(values)}`;
};

/** How a field reads in the review: the explicit value, or "默认 …" when it is left empty. */
function beReviewValue(row, field, text) {
  const { value } = beParse(field, text);
  if (value === null) {
    const values = row.defaults[BE_FIELDS[field].key];
    return `默认 ${beValues(values)}${values.length > 1 ? '（各副本不同）' : ''}`;
  }
  if (field === 'bp') return `${num(value)}（组队建房 ${num(Math.ceil(value / 2))}）`;
  return value ? num(value) : '0（不给通关经验）';
}

function beTeamText(row) {
  const { value, error } = beParse('bp', row.draft.bp);
  if (error) return '—';
  return value === null ? beValues(row.defaults.bp_use_half) : num(Math.ceil(value / 2));
}

/** The line under an input: its default, a zero-experience warning, or its error. */
function beNote(row, field) {
  const { value, error } = beParse(field, row.draft[field]);
  if (error) return ['bad', error];
  if (field === 'exp' && value === 0) return ['warn', '0 = 不给通关经验，其他奖励照常'];
  return ['', beDefaultText(row, field)];
}

// Rendering --------------------------------------------------------------------------------------

function beInputHTML(row, field) {
  const f = BE_FIELDS[field],
    limits = beLimits();
  return html`<td class="be-cell" data-cell="${field}">
    <div class="be-field">
      <div class="input-unit">
        <input
        type="number"
        inputmode="numeric"
        min="${limits[f.min]}"
        max="${limits[f.max]}"
        step="1"
        data-field="${field}"
        value="${row.draft[field]}"
        placeholder="沿用默认"
        aria-label="${row.difficulty} ${f.label}"
        /><span>${f.unit}</span>
      </div>
      <small class="be-note" data-note="${field}"></small>
    </div>
  </td>`;
}

function beRowHTML(row) {
  return html`<tr class="be-row" data-difficulty="${row.difficulty}">
    <td class="check-col">
      <input class="check" type="checkbox" data-pick aria-label="选择${row.difficulty}" />
    </td>
    <td class="be-name"><b>${row.difficulty}</b><span class="be-tags"></span></td>
    <td class="num be-count">${num(row.boss_count)}</td>
    ${beInputHTML(row, 'bp')}
    <td class="num be-team"></td>
    ${beInputHTML(row, 'exp')}
    <td class="be-action">
      <button class="ghost" type="button" data-reset title="清空两项，保存后还原资源包默认值">恢复默认</button>
    </td>
  </tr>`;
}

/** Updates one row in place; `values` also rewrites the inputs (after a bulk fill or a reset). */
function paintBossEconomyRow(tr, row, { values = false } = {}) {
  const picked = bossEconomy.picked.has(row.difficulty);
  tr.classList.toggle('picked', picked);
  tr.querySelector('[data-pick]').checked = picked;
  setHTML(tr.querySelector('.be-tags'), [
    beHasOverride(row) ? stateTag('已调整', 'info') : '',
    beChanged(row) ? stateTag('未保存', 'warn') : '',
  ]);
  for (const field of Object.keys(BE_FIELDS)) {
    const cell = tr.querySelector(`[data-cell="${field}"]`),
      input = cell.querySelector('input'),
      [tone, text] = beNote(row, field),
      note = cell.querySelector('.be-note');
    if (values && input.value !== row.draft[field]) input.value = row.draft[field];
    cell.classList.toggle('changed', beFieldChanged(row, field));
    input.classList.toggle('invalid', tone === 'bad');
    tone === 'bad' ? input.setAttribute('aria-invalid', 'true') : input.removeAttribute('aria-invalid');
    note.className = 'be-note' + (tone ? ' ' + tone : '');
    note.textContent = text;
  }
  const team = tr.querySelector('.be-team');
  team.textContent = beTeamText(row);
  team.classList.toggle('changed', beFieldChanged(row, 'bp'));
  tr.querySelector('[data-reset]').disabled = row.draft.bp.trim() === '' && row.draft.exp.trim() === '';
}

function syncBossEconomySummary() {
  const rows = bossEconomy.rows,
    set = rows.filter(beHasOverride).length,
    changed = rows.filter(beChanged).length,
    visible = $$('#be-rows tr[data-difficulty]').length,
    picked = bossEconomy.picked.size;
  $('#be-count').textContent = `已调整 ${set} / ${rows.length} 个难度`;
  $('#be-shown').textContent =
    `显示 ${visible} / ${rows.length} 个难度` + (changed ? ` · ${changed} 个有未保存修改` : '');
  const [tone, text] = bossEconomy.bulkNote || [
    '',
    picked ? '填写体力或经验后点「填入已选」，空着的一项不改' : '勾选多个难度后可一次填写或恢复默认',
  ];
  setHTML(
    $('#be-picked'),
    html`<b>${picked ? `已选 ${picked} 个难度` : '未选择难度'}</b><span class="be-bulk-note ${tone}"
        >${text}</span
      >`,
  );
  for (const id of ['#be-bulk-apply', '#be-bulk-reset', '#be-bulk-clear']) $(id).disabled = !picked;
  const all = $('#be-pick-all'),
    boxes = $$('#be-rows [data-pick]'),
    ticked = boxes.filter((b) => b.checked).length;
  all.checked = !!boxes.length && ticked === boxes.length;
  all.indeterminate = ticked > 0 && ticked < boxes.length;
  all.disabled = !boxes.length;
  if (typeof updateBossDraftState === 'function') updateBossDraftState();
}

/** Full redraw of the tab: called on load, on a filter change and when the tab opens. */
function renderBossEconomy() {
  const data = bossEconomy.data;
  $('#boss-revision').textContent = `revision ${data?.revision || 0}`;
  $('#save-policy').disabled = policyBusy('bosses') || !data;
  if (!data) return;
  // A search or filter change drops ticks on rows it hides, so the bulk bar acts only on what is visible.
  const rows = bossEconomy.rows.filter(beVisible);
  const shown = new Set(rows.map((r) => r.difficulty));
  bossEconomy.picked = new Set([...bossEconomy.picked].filter((d) => shown.has(d)));
  setHTML(
    $('#be-rows'),
    rows.length
      ? rows.map(beRowHTML)
      : raw(emptyRow(7, '没有匹配的难度', bossEconomy.rows.length ? '换个关键词或清除状态筛选。' : '')),
  );
  for (const tr of $$('#be-rows tr[data-difficulty]')) paintBossEconomyRow(tr, beRow(tr.dataset.difficulty));
  syncBossEconomySummary();
}

function beBulkNote(tone, text) {
  bossEconomy.bulkNote = [tone, text];
  syncBossEconomySummary();
}

function bePick(names, on) {
  for (const name of names) on ? bossEconomy.picked.add(name) : bossEconomy.picked.delete(name);
  bossEconomy.bulkNote = null;
  for (const tr of $$('#be-rows tr[data-difficulty]'))
    if (names.includes(tr.dataset.difficulty)) paintBossEconomyRow(tr, beRow(tr.dataset.difficulty));
  syncBossEconomySummary();
}

// Editing ----------------------------------------------------------------------------------------

function beSetDrafts(names, patch) {
  for (const name of names) Object.assign(beRow(name).draft, patch);
  for (const tr of $$('#be-rows tr[data-difficulty]'))
    if (names.includes(tr.dataset.difficulty))
      paintBossEconomyRow(tr, beRow(tr.dataset.difficulty), { values: true });
  syncBossEconomySummary();
}

function onBossEconomyInput(event) {
  const input = event.target.closest('#be-rows input[data-field]');
  if (!input) return;
  const tr = input.closest('tr'),
    row = beRow(tr.dataset.difficulty);
  row.draft[input.dataset.field] = input.value;
  paintBossEconomyRow(tr, row);
  syncBossEconomySummary();
}

function onBossEconomyClick(event) {
  const tr = event.target.closest('#be-rows tr[data-difficulty]');
  if (!tr) return;
  if (event.target.closest('[data-reset]')) {
    beSetDrafts([tr.dataset.difficulty], { bp: '', exp: '' });
    return;
  }
  const pick = event.target.closest('[data-pick]');
  if (pick) bePick([tr.dataset.difficulty], pick.checked);
}

function applyBossEconomyBulk() {
  const inputs = { bp: $('#be-bulk-bp'), exp: $('#be-bulk-exp') },
    patch = {};
  clearInvalid($('.be-bulk'));
  for (const [field, input] of Object.entries(inputs)) {
    if (input.value.trim() === '') continue;
    const { value, error } = beParse(field, input.value);
    if (error) {
      markInvalid(input, `${BE_FIELDS[field].label}${error}`);
      beBulkNote('bad', `${BE_FIELDS[field].label}${error}`);
      input.focus();
      return;
    }
    patch[field] = String(value);
  }
  if (!Object.keys(patch).length) {
    markInvalid(inputs.bp);
    markInvalid(inputs.exp);
    beBulkNote('bad', '先填写单人体力或通关经验');
    inputs.bp.focus();
    return;
  }
  const names = [...bossEconomy.picked];
  beSetDrafts(names, patch);
  beBulkNote('ok', `已填入 ${names.length} 个难度，保存后生效`);
}

// Saving -----------------------------------------------------------------------------------------

/** Reads the latest settings again and puts the operator's edited rows back on top of them. */
async function reloadBossEconomyKeeping() {
  if (policyBusy('bosses')) return;
  state.loading.add('bosses');
  updatePolicyControls();
  try {
    const { kept, gone } = await loadBossEconomy({ keep: true });
    renderBossEconomy();
    showViewAlert('bosses', {
      kind: 'info',
      title: '已读取最新设置',
      message:
        (kept.length
          ? `保留了你修改的 ${kept.length} 个难度（${kept.slice(0, 5).join('、')}${kept.length > 5 ? ' 等' : ''}），请核对后再保存。`
          : '你的修改与最新设置相同，无需再保存。') +
        (gone.length ? `难度 ${gone.join('、')} 已不在当前目录中，其修改已丢弃。` : ''),
    });
  } catch (e) {
    reportError('bosses', e, '载入');
  } finally {
    state.loading.delete('bosses');
    updatePolicyControls();
  }
}

function beReviewRows(changed) {
  const rows = [];
  for (const row of changed)
    for (const field of Object.keys(BE_FIELDS))
      if (beFieldChanged(row, field))
        rows.push([
          `${row.difficulty} · ${BE_FIELDS[field].label}`,
          beReviewValue(row, field, row.saved[field]),
          beReviewValue(row, field, row.draft[field]),
        ]);
  return rows;
}

async function saveBossEconomy() {
  if (policyBusy('bosses') || !bossEconomy.data) return;
  clearInvalid($('.be-bulk')); // row inputs carry their own state from paintBossEconomyRow
  const changed = bossEconomy.rows.filter(beChanged);
  if (!changed.length) return toast('与已保存设置一致，无需保存');
  const invalid = changed.filter((r) => Object.keys(BE_FIELDS).some((f) => beParse(f, r.draft[f]).error));
  if (invalid.length) {
    // Bring every invalid row on screen before pointing at it.
    if (invalid.some((r) => !beVisible(r))) {
      bossEconomy.query = $('#be-search').value = '';
      bossEconomy.filter = $('#be-filter').value = '';
      renderBossEconomy();
    }
    const names = invalid.map((r) => r.difficulty);
    showViewAlert('bosses', {
      title: '不能保存：数值无效',
      message: `${names.slice(0, 4).join('、')}${names.length > 4 ? ` 等 ${names.length} 个难度` : ''}的输入有误。单人体力须为 ${num(beLimits().bp_use_min)}～${num(beLimits().bp_use_max)}、通关经验须为 ${num(beLimits().experience_min)}～${num(beLimits().experience_max)} 的整数；留空表示沿用默认。`,
    });
    $('#be-rows .invalid')?.focus();
    return;
  }
  if (changed.length > BE_BATCH) {
    showViewAlert('bosses', {
      title: '不能保存：修改的难度太多',
      message: `一次最多保存 ${BE_BATCH} 个难度，当前修改了 ${changed.length} 个。请先保存其中一部分。`,
    });
    return;
  }
  const rules = changed.map((r) => {
    const bp = beParse('bp', r.draft.bp).value,
      exp = beParse('exp', r.draft.exp).value;
    return bp === null && exp === null
      ? { difficulty: r.difficulty, reset: true }
      : { difficulty: r.difficulty, bp_use: bp, experience: exp };
  });
  const resets = rules.filter((r) => r.reset).length;
  if (
    !(await reviewChanges({
      title: '保存体力与经验',
      lead: `共 ${changed.length} 个难度${resets ? `（其中 ${resets} 个恢复默认）` : ''}，请逐项核对。`,
      sections: [{ title: '按难度', rows: beReviewRows(changed) }],
      note: '同名难度在 3D 活动、2D 活动和往期副本中同时生效，自卡组入口跟随。保存后玩家下一次请求即用新值；已开战的体力不重复扣除，通关经验按结算时的设置发放。恢复默认还原资源包的值，不是清零。',
    }))
  )
    return;
  state.publishing.add('bosses');
  updatePolicyControls();
  let saved = false;
  try {
    await api('/api/boss-economy', {
      method: 'PUT',
      body: JSON.stringify({ expected_revision: bossEconomy.data.revision, rules }),
    });
    saved = true;
    state.loaded.delete('audit');
    await loadBossEconomy();
    clearViewAlert('bosses');
    renderBossEconomy();
    toast('体力与经验已保存，玩家下一次请求起生效');
  } catch (e) {
    if (saved) reportError('bosses', e, '保存成功，但重新载入');
    else if (e.status === 409)
      showViewAlert('bosses', {
        kind: 'warn',
        title: '保存未完成：设置已在别处修改',
        message:
          '体力与经验刚被其他页面或操作改过，这次没有写入。可以读取最新设置并保留你改过的难度，核对后再保存。',
        actions: [{ label: '读取最新并保留我的修改', primary: true, onClick: reloadBossEconomyKeeping }],
      });
    else reportError('bosses', e, '保存');
  } finally {
    state.publishing.delete('bosses');
    updatePolicyControls();
  }
}

// Wiring -----------------------------------------------------------------------------------------

function initBossEconomy() {
  $('#be-rows').addEventListener('input', onBossEconomyInput);
  $('#be-rows').addEventListener('click', onBossEconomyClick);
  $('#be-pick-all').addEventListener('change', () =>
    bePick(
      $$('#be-rows tr[data-difficulty]').map((tr) => tr.dataset.difficulty),
      $('#be-pick-all').checked,
    ),
  );
  $('#be-search').addEventListener('input', () => {
    bossEconomy.query = $('#be-search').value.trim();
    bossEconomy.bulkNote = null;
    renderBossEconomy();
  });
  $('#be-filter').addEventListener('change', () => {
    bossEconomy.filter = $('#be-filter').value;
    bossEconomy.bulkNote = null;
    renderBossEconomy();
  });
  $('#be-bulk-apply').addEventListener('click', applyBossEconomyBulk);
  $('#be-bulk-reset').addEventListener('click', () => {
    const names = [...bossEconomy.picked];
    beSetDrafts(names, { bp: '', exp: '' });
    beBulkNote('ok', `已将 ${names.length} 个难度改为沿用默认，保存后生效`);
  });
  $('#be-bulk-clear').addEventListener('click', () => bePick([...bossEconomy.picked], false));
  for (const id of ['#be-bulk-bp', '#be-bulk-exp'])
    $(id).addEventListener('input', () => clearInvalid($('.be-bulk')));
}

initBossEconomy();
