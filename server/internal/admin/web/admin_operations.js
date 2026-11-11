'use strict';
const poolEditor = {
  rows: [],
  categories: [],
  cards: [],
  selected: new Map(),
  fames: new Map(),
  loves: new Map(),
  coverPath: '',
  customRevision: 0,
  variants: [],
  strategies: new Map(),
  mixedByID: new Map(),
  mixedVariant: 0,
  row: null,
  page: 0,
  selectedPage: 0,
  job: 0,
  dirty: false,
  preview: null,
  editSerial: 0,
};
const poolCoverPattern = /^gacha-covers\/[0-9a-f]{64}\.(png|jpg)$/;
const localTimeInput = (unix) =>
  unix
    ? new Date(unix * 1000 - new Date(unix * 1000).getTimezoneOffset() * 60000).toISOString().slice(0, 16)
    : '';
const unixInput = (id) => ($(id).value ? Math.floor(new Date($(id).value).getTime() / 1000) : 0);
const describeTime = (unix) =>
  unix ? new Date(unix * 1000).toLocaleString('zh-CN', { hour12: false }) : '不限';
const paymentName = (profile) => {
  if (profile.pay_type === 4) {
    const item = poolEditor.catalog?.find(
      (c) => c.reward_type === 8 && c.reward_type_id === profile.pay_typeid,
    );
    return item ? `${item.name}（道具 ${profile.pay_typeid}）` : `道具 ${profile.pay_typeid}`;
  }
  return (
    { 2: '友情点', 3: '水晶（先免费后付费）', 6: '付费水晶' }[profile.pay_type] || `类型 ${profile.pay_type}`
  );
};

// New operator pools need a first publication even when their draft matches the template.
// Existing pools have a pending draft when it differs from the effective config.
function poolHasDraft(row) {
  return !!row && row.draft.revision > 0 && row.draft.payload != null;
}
function poolPendingDraft(row) {
  return (
    poolHasDraft(row) &&
    ((row.custom && !row.live?.revision) || JSON.stringify(row.draft.payload) !== JSON.stringify(row.config))
  );
}
// One atomic publication group, independently editable variants, one uploaded cover.
// poolEditor.row identifies the group; activeID identifies the displayed variant.
const poolProfessionName = (t) =>
  ({ 1: '佣兵', 2: '富豪', 3: '盗贼', 4: '歌姬', 5: '混合职业' })[t] || '不分职业';
function poolActiveRow() {
  return poolEditor.variants.find((v) => v.gacha_id === poolEditor.activeID) || poolEditor.row;
}
function poolVariantState(c) {
  return {
    selected: new Map((c.card_ids || []).map((id, i) => [id, c.weights[i]])),
    fames: new Map(Object.entries(c.card_fames || {}).map(([id, f]) => [Number(id), Number(f)])),
    loves: new Map(Object.entries(c.card_loves || {}).map(([id, v]) => [Number(id), Number(v)])),
    filters: {},
  };
}
function poolMixedState(c, base = {}) {
  return structuredClone({
    reward_pool: c.reward_pool || base.reward_pool || [],
    steps: (c.steps || base.steps || []).map((step) => ({ ...step, reward_pool: step.reward_pool || [] })),
  });
}
function poolRememberActive() {
  const s = poolEditor.contents?.get(poolEditor.activeID);
  if (!s) return;
  s.selected = poolEditor.selected;
  s.fames = poolEditor.fames;
  s.loves = poolEditor.loves;
  s.filters = { job: poolEditor.job };
  for (const key of ['search', 'rarity', 'attribute', 'cost', 'eligible-only']) {
    const el = $(`#pool-${key}`);
    if (el) s.filters[key] = el.type === 'checkbox' ? el.checked : el.value;
  }
}
function selectPoolVariant(id, remember = true) {
  if (remember) poolRememberActive();
  const s = poolEditor.contents.get(id);
  if (!s) return;
  poolEditor.activeID = id;
  poolEditor.editSerial++;
  poolEditor.selected = s.selected;
  poolEditor.fames = s.fames;
  poolEditor.loves = s.loves;
  poolEditor.job = s.filters.job || 0;
  for (const key of ['search', 'rarity', 'attribute', 'cost', 'eligible-only']) {
    const el = $(`#pool-${key}`);
    if (el) {
      if (el.type === 'checkbox') el.checked = s.filters[key] ?? key === 'eligible-only';
      else el.value = s.filters[key] ?? (el.tagName === 'SELECT' ? (el.options[0]?.value ?? '') : '');
    }
  }
  poolEditor.page = poolEditor.selectedPage = 0;
  renderRarityTool.touched = false;
  setupMixedPool();
  renderPoolGifts();
  renderPoolCards();
  renderSelectedCards();
  renderPoolCopyTargets();
  renderPoolSplit();
  renderVariantBar();
  if (poolEditor.strategies?.size) renderStrategies();
}
function renderVariantBar() {
  const row = poolActiveRow(),
    s = poolEditor.strategies?.get(row?.gacha_id);
  if (!row) return;
  $('#pool-variant-name').innerHTML =
    `${row.base.card_num} 抽 · ${esc(poolProfessionName(row.base.arthur_type))} <code>${row.gacha_id}</code>${s ? `<span class="sub">${esc(s.name)}${s.open ? '' : ' · 已关闭'}</span>` : ''}`;
}
// Card count and draft marker of one variant, shown in its 分池 row.
function poolVariantSummary(v) {
  const s = poolEditor.contents?.get(v.gacha_id),
    m = poolEditor.mixedByID?.get(v.gacha_id);
  if (!s) return '';
  const count = m
    ? `${(m.steps.length ? m.steps[0].reward_pool : m.reward_pool).length} 项奖励${m.steps.length ? ` · ${m.steps.length} 个阶段` : ''}`
    : `${s.selected.size} 张${v.base.user_select_max ? '候选' : ''}卡牌`;
  const fames = m ? 0 : [...s.fames.keys()].filter((id) => s.selected.has(id)).length,
    loves = m ? 0 : [...s.loves].filter(([id, v]) => v && s.selected.has(id)).length;
  const gifts = poolEditor.randomGifts?.get(v.gacha_id)?.length || 0;
  return `<b>${count}</b>${poolPendingDraft(v) ? ' <span class="tag warn">草稿</span>' : ''}${fames ? `<span class="sub">${fames} 张自定义名声</span>` : ''}${loves ? `<span class="sub">${loves} 张初始忠诚度</span>` : ''}${gifts ? `<span class="sub">随机附赠 ${gifts} 组</span>` : ''}`;
}
function renderPoolCopyTargets() {
  const rows = poolEditor.variants.filter(
    (v) => v.gacha_id !== poolEditor.activeID && !v.base.reward_pool?.length,
  );
  $('#pool-copy-menu').hidden = !rows.length || !!poolEditor.mixed;
  $('#pool-copy-menu').open = false;
  $('#pool-copy-targets').innerHTML =
    rows
      .map(
        (v) =>
          `<label><input type="checkbox" value="${v.gacha_id}"> ${esc(poolProfessionName(v.base.arthur_type))} · ${v.base.card_num} 抽 · ${v.gacha_id}</label>`,
      )
      .join('') || '<span class="hint">没有其他普通卡牌分池</span>';
}
// A 混合职业 variant can hand each profession its cards. Targets are the profession variants of the same
// draw method: operator variants share their rule; built-in pools compare draw count and cost.
const poolDrawKey = (v) =>
  v.rule
    ? `r${v.rule.type}:${v.rule.draw_count}`
    : `n${v.base.card_num}:${v.base.pay_type}:${v.base.pay_typeid || 0}`;
function poolSplitTargets() {
  const row = poolActiveRow();
  if (!row || row.base.arthur_type !== 5 || poolEditor.mixedByID.has(row.gacha_id)) return [];
  return poolEditor.variants.filter(
    (v) =>
      v.base.arthur_type >= 1 &&
      v.base.arthur_type <= 4 &&
      !poolEditor.mixedByID.has(v.gacha_id) &&
      poolDrawKey(v) === poolDrawKey(row),
  );
}
function poolSplitPlan() {
  const cards = new Map(poolEditor.cards.map((c) => [c.reward_type_id, c])),
    common = $('#pool-split-common').checked,
    source = [...poolEditor.selected];
  return poolSplitTargets().map((v) => {
    const job = v.base.arthur_type,
      own = source.filter(([id]) => cards.get(id)?.arthur_type === job),
      shared = common ? source.filter(([id]) => cards.has(id) && !cards.get(id).arthur_type) : [];
    return {
      v,
      job,
      own: own.length,
      shared: shared.length,
      selected: new Map([...own, ...shared].filter(([id]) => poolCardEligibleFor(cards.get(id), v))),
    };
  });
}
// A profession with no matching card is skipped: its variant keeps its current cards, never becomes empty.
const poolSplitLabel = (p) => `${poolProfessionName(p.job)} · ${p.v.base.card_num} 抽（${p.v.gacha_id}）`;
function renderPoolSplit() {
  const targets = poolSplitTargets(),
    menu = $('#pool-split-menu');
  menu.hidden = !targets.length;
  if (!targets.length) {
    menu.open = false;
    return;
  }
  const plan = poolSplitPlan(),
    covered = new Set(plan.map((p) => p.job)),
    missing = [...new Set(poolEditor.variants.map((v) => v.base.arthur_type))]
      .filter((j) => j >= 1 && j <= 4 && !covered.has(j))
      .sort();
  $('#pool-split-targets').innerHTML =
    plan
      .map((p) => {
        const had = poolEditor.contents.get(p.v.gacha_id)?.selected.size || 0,
          skip = !p.selected.size;
        return (
          `<div class="pool-split-row${skip ? ' skip' : ''}"><b>${esc(poolProfessionName(p.job))}</b><span>${p.v.base.card_num} 抽 <code>${p.v.gacha_id}</code>${skip ? `<small>跳过，保留原配置${had ? `（现有 ${num(had)} 张）` : ''}</small>` : had ? `<small class="replace">现有 ${num(had)} 张将被替换</small>` : ''}</span>` +
          `<span class="pool-split-count">${skip ? '无匹配卡牌' : `${num(p.selected.size)} 张${p.shared ? `<small>本职业 ${num(p.own)} ＋ 通用 ${num(p.shared)}</small>` : ''}`}</span></div>`
        );
      })
      .join('') +
    (missing.length
      ? `<p class="hint">${missing.map(poolProfessionName).join('、')}没有这种抽法，不分配。</p>`
      : '');
  $('#pool-split-apply').disabled = !plan.some((p) => p.selected.size);
}
function poolGroupRows(row = poolEditor.row) {
  return row ? poolEditor.rows.filter((r) => r.base.groupid === row.base.groupid) : [];
}
function poolGroupPending(row = poolEditor.row) {
  return poolGroupRows(row).some(poolPendingDraft);
}
function poolPublished() {
  return (
    !!poolEditor.row &&
    !poolGroupPending() &&
    (!poolEditor.row.custom || poolEditor.variants.every((r) => r.live.revision > 0))
  );
}
function poolSavedConfig(row) {
  return poolHasDraft(row) ? row.draft.payload : row.config;
}
const poolPayName = (type, id) =>
  type === 4
    ? poolEditor.catalog?.find((e) => e.reward_type === 8 && e.reward_type_id === id)?.name || `道具 ${id}`
    : { 2: '友情点', 3: '水晶', 6: '付费水晶' }[type] || `类型 ${type}`;
function poolVariantLabel(row) {
  const c = poolSavedConfig(row);
  return `${poolPayName(c.pay_type || row.base.pay_type, c.pay_type ? c.pay_typeid : row.base.pay_typeid)} ×${num(c.price)} · ${row.base.card_num} 抽${c.closed ? '（关闭）' : ''}`;
}
const poolIsMixed = (row) => ['mixed', 'step'].includes(row.rule?.type) || !!row.base.reward_pool?.length;
function poolCopyable(row) {
  return (
    !row.deleted &&
    (!row.custom || row.live.revision > 0) &&
    !poolIsMixed(row) &&
    !row.base.unowned_only &&
    !row.base.user_select_max &&
    row.base.gacha_type <= 1
  );
}
function updatePoolControls() {
  const busy = policyBusy('pool-editor') || !poolEditor.row;
  const deleted = !!poolEditor.row?.deleted;
  $('#pool-save-preview').disabled = busy || deleted;
  $('#pool-export').disabled = busy;
  $('#pool-import').disabled = busy || deleted;
  $('#pool-create').disabled = policyBusy('pool-editor') || !poolEditor.ruleTypes?.length;
  $('#pool-delete').disabled = $('#pool-restore').disabled = busy;
  $('#pool-cover-upload').disabled = busy || deleted;
  $('#pool-cover-upload-label').classList.toggle('disabled', busy || deleted);
  $('#pool-cover-reset').disabled = busy || deleted || !poolEditor.coverPath;
  $('#pool-publish').disabled =
    busy ||
    poolEditor.dirty ||
    !poolEditor.preview?.publishable ||
    poolPublished() ||
    !poolEditor.variants.every(poolHasDraft);
  $('#pool-preview-publish').disabled = $('#pool-publish').disabled;
  $('#pool-discard').disabled =
    busy || deleted || !(poolEditor.dirty || poolEditor.variants.some(poolHasDraft));
  const step = !poolEditor.row
    ? ''
    : poolEditor.dirty
      ? 'edit'
      : poolEditor.preview?.publishable && !poolPublished()
        ? 'publish'
        : poolPublished() && !poolEditor.preview
          ? 'done'
          : 'preview';
  const order = ['edit', 'preview', 'publish'];
  $$('#pool-flow li').forEach((li) => {
    const i = order.indexOf(li.dataset.flow),
      at = step === 'done' ? 3 : order.indexOf(step);
    li.classList.toggle('current', i === at);
    li.classList.toggle('done', i < at);
  });
  $('#pool-publish').title = poolEditor.dirty
    ? '有未保存的修改：请先保存草稿并预览'
    : !poolEditor.preview
      ? '请先保存草稿并核对预览'
      : !poolEditor.preview.publishable
        ? '预览中含未闭包卡牌，不能发布'
        : poolPublished()
          ? '草稿与已发布版本相同'
          : '';
  $('#pool-preview-reason').textContent =
    poolEditor.preview && !poolEditor.preview.publishable
      ? '含资源未闭包的卡牌，移除后重新保存才能发布'
      : poolPublished() && poolEditor.preview
        ? '与已发布版本相同，无需发布'
        : '';
}
async function runPoolEdit(action, what = '保存') {
  if (!poolEditor.row) return;
  return runPoolTask(action, what);
}
async function runPoolTask(action, what) {
  if (policyBusy('pool-editor')) return;
  state.publishing.add('pool-editor');
  updatePolicyControls();
  try {
    await action();
  } catch (e) {
    reportError('pool-editor', e, what);
  } finally {
    state.publishing.delete('pool-editor');
    updatePolicyControls();
  }
}

async function loadPoolEditor(selectID) {
  const [data, policy] = await Promise.all([
    api('/api/gacha-editor'),
    api('/api/gacha-policy').catch(() => null),
  ]);
  poolEditor.openGroups = policy ? new Set(policy.publication?.group_ids || []) : null;
  const cards = poolEditor.catalog?.length ? poolEditor.catalog : await fetchAllCatalog();
  const previous = selectID ?? poolEditor.row?.gacha_id;
  poolEditor.rows = data.pools;
  poolEditor.categories = data.categories || [];
  renderPoolCategoryOptions(0);
  poolEditor.ruleTypes = data.rule_types || [];
  poolEditor.customRevision = data.custom_revision || 0;
  poolEditor.maxDrawCounts = data.max_draw_counts || 2;
  poolEditor.catalog = cards;
  poolEditor.cards = cards.filter((c) => c.kind === 'card');
  renderPoolSelect();
  selectPool(
    data.pools.some((row) => row.gacha_id === previous) ? previous : Number($('#pool-select').value),
  );
}

function selectPool(id = Number($('#pool-select').value)) {
  const picked = poolEditor.rows.find((row) => row.gacha_id === id);
  if (poolEditor.row?.base.groupid !== picked?.base.groupid) $('#pool-cost').value = '';
  poolEditor.editSerial++;
  poolEditor.variants = picked ? poolGroupRows(picked) : [];
  poolEditor.row = poolEditor.variants[0] || null;
  const row = poolEditor.row;
  if (!row) {
    poolEditor.preview = null;
    $('#pool-current').textContent = '请先选择一个卡池';
    updatePoolControls();
    return;
  }
  $('#pool-select').value = row.gacha_id;
  $('#pool-current').innerHTML =
    `正在编辑：${esc(row.config.name)} <code>${poolEditor.variants.map((v) => v.gacha_id).join('、')}</code>${row.custom ? ' <span class="tag info">后台新建</span>' : ''}${row.deleted ? ' <span class="tag bad">已删除</span>' : ''}${poolOpenBadge(row)}`;
  $('#pool-delete').hidden = !row.custom || row.deleted;
  $('#pool-restore').hidden = !row.custom || !row.deleted;
  $('#pool-deleted-note').hidden = !row.deleted;
  const config = poolSavedConfig(row);
  poolEditor.contents = new Map(
    poolEditor.variants.map((v) => [v.gacha_id, poolVariantState(poolSavedConfig(v))]),
  );
  poolEditor.activeID = 0;
  poolEditor.coverPath = config.cover_path || '';
  $('#pool-play-limit').value = config.play_count_max || 0;
  $('#pool-daily-limit').value = config.play_count_1day_max || 0;
  renderPoolCategoryOptions(config.category_num || 0);
  $('#pool-category').value = String(config.category_num || '');
  $('#pool-odds-msg').value = config.odds_msg || '';
  renderPoolOddsCount();
  $('#pool-start').value = localTimeInput(config.start_unix);
  $('#pool-end').value = localTimeInput(config.end_unix);
  // Schedules were per variant before they became pool-wide; show the first one and let a save unify them.
  const schedules = new Set(
    poolEditor.variants.map((v) => {
      const c = poolSavedConfig(v);
      return `${c.start_unix || 0}-${c.end_unix || 0}`;
    }),
  );
  $('#pool-schedule-note').hidden = schedules.size < 2;
  $('#pool-schedule-note').textContent =
    schedules.size < 2
      ? ''
      : `各分池已保存的排期不一致（${schedules.size} 种）。这里显示第一个分池的排期，保存草稿后全部分池统一使用它。`;
  poolEditor.strategies = new Map(
    poolEditor.variants.map((v) => {
      const c = poolSavedConfig(v),
        type = c.pay_type || v.base.pay_type,
        item = c.pay_type ? c.pay_typeid : v.base.pay_typeid;
      return [
        v.gacha_id,
        {
          open: !c.closed,
          name: c.name,
          pay_type: type,
          pay_typeid: type === 4 ? item || 0 : 0,
          price: c.price,
        },
      ];
    }),
  );
  poolEditor.mixedByID = new Map(
    poolEditor.variants
      .filter(poolIsMixed)
      .map((v) => [v.gacha_id, poolMixedState(poolSavedConfig(v), v.base)]),
  );
  poolEditor.randomGifts = new Map(
    poolEditor.variants.map((v) => [v.gacha_id, poolGiftState(poolSavedConfig(v).random_gifts)]),
  );
  poolEditor.dirty = false;
  poolEditor.preview = null;
  $('#pool-publish').disabled = true;
  $('#pool-rarity-rows').innerHTML = '';
  renderRarityTool.touched = false;
  $('#pool-preview').textContent = '';
  clearInvalid($('#pool-editor'));
  renderPoolVersion();
  selectPoolVariant(row.gacha_id, false);
  renderStrategies();
  renderPoolCover();
  renderPoolCards();
  renderSelectedCards();
  updatePoolControls();
  if (schedules.size > 1) poolChanged();
}
// Whether 「扭蛋发布」 currently opens this pool; a published pool that is not opened stays invisible to players.
function poolOpenBadge(row) {
  const open = poolEditor.openGroups?.has(row.base.groupid);
  return poolEditor.openGroups && !row.deleted
    ? ` <button type="button" class="tag-link ${open ? 'ok' : 'warn'}" data-jump-gachas title="到「扭蛋发布」${open ? '查看' : '开启'}此卡池">${open ? '扭蛋发布：已开启' : '扭蛋发布：未开启'}</button>`
    : '';
}
$('#pool-current').addEventListener('click', (e) => {
  if (e.target.closest('[data-jump-gachas]')) switchView('gachas');
});
async function switchPool(id) {
  if (policyBusy('pool-editor') || poolGroupRows().some((r) => r.gacha_id === id)) return;
  if (!(await confirmDiscard(poolEditor.dirty, '切换卡池会放弃未保存的编辑。', '放弃并切换'))) return;
  selectPool(id);
  renderPoolSelect();
}
function renderPoolVersion() {
  const row = poolEditor.row,
    n = poolEditor.variants.length;
  $('#pool-version').textContent =
    `${poolEditor.variants.some(poolHasDraft) ? '有已保存草稿' : '无已保存草稿'} · ${row.live.revision ? '已发布 v' + row.live.revision : row.custom ? '尚未发布' : '包内默认配置'}${n > 1 ? ` · ${n} 种抽法` : ''}`;
}
function poolCoverURL(path) {
  return path ? '/' + path : poolEditor.row?.banner_url || '';
}
function renderPoolCover() {
  const path = poolEditor.coverPath;
  $('#pool-cover-alert').innerHTML = '';
  $('#pool-cover-preview').innerHTML = image(poolCoverURL(path), poolEditor.row?.config.name || '');
  $('#pool-cover-state').textContent = path ? '自定义封面' : '包内封面';
  $('#pool-cover-state').className = 'tag' + (path ? ' info' : '');
}

// Draw methods: per-variant name, payment and price. Draw counts and special rules come from the pool itself.
function renderStrategies() {
  // Each payment box is a searchable select (enhanceSelect in admin_kit.js).
  const available = (poolEditor.catalog || []).filter(
    (c) => c.reward_type === 8 && c.resource_state !== 'unavailable',
  );
  const methods = [
    [3, '水晶（先免费后付费）'],
    [4, '道具'],
    [2, '友情点'],
    [6, '付费水晶'],
  ];
  $('#pool-strategies').innerHTML = poolEditor.variants
    .map((v) => {
      const s = poolEditor.strategies.get(v.gacha_id),
        b = v.base,
        max = s.pay_type !== 2 && b.pay_type === 2 ? b.card_num : b.card_num_max,
        stepped = !!poolEditor.mixedByID.get(v.gacha_id)?.steps?.length;
      const active = v.gacha_id === poolEditor.activeID;
      return (
        `<tr data-variant="${v.gacha_id}" class="${s.open ? '' : 'closed'}${active ? ' current' : ''}"><td><input type="radio" name="pool-variant-pick" data-pick="${v.gacha_id}" ${active ? 'checked' : ''} aria-label="编辑分池 ${v.gacha_id}"></td><td><label class="switch"><input type="checkbox" data-strategy="open" ${s.open ? 'checked' : ''} aria-label="开放抽法 ${v.gacha_id}"></label></td><td><b>${b.card_num} 抽 · ${poolProfessionName(b.arthur_type)}</b><span class="sub">${max > b.card_num ? `最多 ${max} 抽 · ` : ''}<code>${v.gacha_id}</code></span></td>` +
        `<td data-label="显示名称"><input data-strategy="name" maxlength="60" value="${esc(s.name)}" aria-label="抽法 ${v.gacha_id} 显示名称"></td>` +
        `<td data-label="消耗方式"><select data-strategy="pay_type" aria-label="抽法 ${v.gacha_id} 消耗方式">${methods.map(([t, l]) => `<option value="${t}" ${s.pay_type === t ? 'selected' : ''}>${l}</option>`).join('')}</select></td>` +
        `<td data-label="消耗道具">${s.pay_type === 4 ? `<select data-strategy="pay_typeid" data-search="搜索道具名称或 ID" aria-label="抽法 ${v.gacha_id} 消耗道具"><option value="">请选择消耗道具</option>${available.map((c) => `<option value="${c.reward_type_id}" ${c.reward_type_id === s.pay_typeid ? 'selected' : ''}>${esc(c.name)} · ${c.reward_type_id}</option>`).join('')}</select>` : '<span class="hint">—</span>'}</td>` +
        `<td data-label="价格">${stepped ? '<span class="hint">按阶段设置</span>' : `<input data-strategy="price" type="number" min="1" max="10000000" step="1" value="${esc(s.price)}" aria-label="抽法 ${v.gacha_id} 价格">`}</td><td class="pool-variant-sum" data-label="奖池与排期" data-summary>${poolVariantSummary(v)}</td></tr>`
      );
    })
    .join('');
  enhanceSelects($('#pool-strategies'));
  renderPoolContract();
  renderPoolButtons();
}
// Game-list entry: one of the server's categories. Configs carry it (the server starts them from the pool's own
// data or rule type), and the editor reads and writes only that value.
const poolCategoryName = (id) => poolEditor.categories?.find((c) => c.id === id)?.name || `分类 ${id}`;
/** The entry players see now (live settings); a pending draft is flagged separately in the picker. */
const poolLiveCategory = (row) => row.config.category_num || 0;

/** The listed categories; a stored unlisted one stays selectable, and a config without one asks for a choice. */
function renderPoolCategoryOptions(current) {
  const ids = poolEditor.categories.map((c) => c.id);
  if (current && !ids.includes(current)) ids.unshift(current);
  $('#pool-category').replaceChildren(
    ...(current ? [] : [new Option('请选择扭蛋分类', '')]),
    ...ids.map((id) => new Option(poolCategoryName(id), id)),
  );
}

// The rule badge reads the pool's own `rule`; built-in pools without one keep their original profile.
function renderPoolContract() {
  const total = poolEditor.variants.length;
  const open = poolEditor.variants.filter((v) => poolEditor.strategies.get(v.gacha_id).open).length;
  const row = poolEditor.row;
  const type = poolRuleType(row);
  const facts = poolRuleFacts(row?.rule).slice(1);
  const entry = Number($('#pool-category').value);
  setHTML(
    $('#pool-contract'),
    html`
      <span class="tag ${type ? 'info' : ''}">${type ? type.name : '内置规则'}</span>
      ${
        entry > 0 &&
        html`<span class="tag" title="游戏扭蛋列表中的入口">
          入口：${poolCategoryName(entry)}
        </span>`
      }
      ${facts.length > 0 && html`<span>${facts.join(' · ')}</span>`}
      <span>${total} 个分池${open < total ? `（开放 ${open} 个）` : ''}</span>
    `,
  );
}

// Native draw buttons -------------------------------------------------------------------------------
// The game list shows at most max_draw_counts draw counts per pool (validateGachaDrawButtons on the
// server). Open variants with the same count share its button: profession variants are chosen in the
// profession window, and several single-draw payments are alternatives the server picks per player.

// Items first, then friend points, crystals and paid crystals, as the server tries them.
const POOL_PAY_ORDER = [4, 2, 3, 6];
// A friend-point variant paid another way loses its balance-based draw count.
const poolDrawMax = (v, s) =>
  s.pay_type !== 2 && v.base.pay_type === 2 ? v.base.card_num : v.base.card_num_max;
const poolButtonName = (min, max) => (max > min ? `${min}–${max} 抽` : `${min} 抽`);

/** Buttons of the open variants in table order, and the rules they break as { ids, text }. */
function poolDrawButtons() {
  const open = poolEditor.variants
    .map((v) => ({ v, s: poolEditor.strategies.get(v.gacha_id) }))
    .filter(({ s }) => s.open)
    .map(({ v, s }) => ({ v, s, min: v.base.card_num, max: poolDrawMax(v, s) }));
  const buttons = [];
  for (const entry of open) {
    const button = buttons.find((b) => b.min === entry.min);
    if (button) button.entries.push(entry);
    else buttons.push({ min: entry.min, max: entry.max, entries: [entry] });
  }
  const issues = [];
  const issue = (entries, text) => {
    if (entries.length) issues.push({ ids: entries.map((e) => e.v.gacha_id), text });
  };
  const limit = poolEditor.maxDrawCounts || 2;
  if (buttons.length > limit) {
    issue(
      buttons.slice(limit).flatMap((b) => b.entries),
      `游戏内每个卡池最多显示 ${limit} 种抽数，请关闭多余抽法`,
    );
  }
  for (const b of buttons) {
    if (b.entries.some((e) => e.max !== b.max))
      issue(b.entries, '同一卡池不能同时开放最少抽数相同、最多抽数不同的抽法');
  }
  for (const max of new Set(open.map((e) => e.max))) {
    const same = open.filter((e) => e.max === max);
    if (new Set(same.map((e) => e.min)).size > 1)
      issue(same, '同一卡池不能同时开放最多抽数相同、最少抽数不同的抽法');
  }
  const multi = new Map();
  for (const e of open.filter((e) => e.max > 1)) {
    const key = `${e.max}:${e.v.base.arthur_type}`;
    multi.set(key, [...(multi.get(key) || []), e]);
  }
  for (const same of multi.values()) {
    if (same.length > 1) issue(same, '同一职业的同一连抽按钮只能开放一种抽法');
  }
  return { buttons, issues, limit };
}

function renderPoolButtons() {
  const box = $('#pool-buttons');
  if (!poolEditor.variants?.length) return setHTML(box, '');
  const { buttons, issues, limit } = poolDrawButtons();
  const card = (b) => {
    const jobs = [...new Set(b.entries.map((e) => e.v.base.arthur_type))];
    const pays = new Map();
    for (const { v, s } of b.entries) {
      const key = `${s.pay_type}:${s.pay_type === 4 ? s.pay_typeid : 0}`;
      if (!pays.has(key)) {
        const name =
          s.pay_type === 4 && !s.pay_typeid ? '道具（未选择）' : poolPayName(s.pay_type, s.pay_typeid);
        const free = s.pay_type === 2 && v.base.daily_first_free ? '，每日首次免费' : '';
        pays.set(key, { type: s.pay_type, name: name + free });
      }
    }
    const payList = [...pays.values()].sort(
      (a, c) => POOL_PAY_ORDER.indexOf(a.type) - POOL_PAY_ORDER.indexOf(c.type),
    );
    return html`
      <div class="pool-button">
        <b>${poolButtonName(b.min, b.max)}</b>
        <span>${b.entries.length} 个分池</span>
        ${
          jobs.length > 1 &&
          html`<span class="pool-button-line"><i>职业</i>${jobs.map(poolProfessionName).join('、')}（玩家在职业窗口选择）</span>`
        }
        ${
          payList.length > 1
            ? html`<span class="pool-button-line"><i>备选支付</i>${payList.map((p) => p.name).join(' → ')}</span>
                <span class="pool-button-note">按玩家能支付的方式依次选用（同类道具取第一种）；都付不起时游戏提示余额或道具不足</span>`
            : html`<span class="pool-button-line"><i>支付</i>${payList[0].name}</span>`
        }
      </div>
    `;
  };
  setHTML(
    box,
    html`
      <div class="pool-buttons-head">
        <b>游戏内按钮</b>
        <span class="tag ${buttons.length > limit ? 'bad' : ''}">${buttons.length} / ${limit} 种抽数</span>
        <span class="hint">同一抽数的开放分池共用一个按钮：职业分池在职业窗口选择，多种单抽支付互为备选。</span>
      </div>
      ${
        buttons.length
          ? html`<div class="pool-button-list">${buttons.map(card)}</div>`
          : html`<p class="hint">没有开放的抽法。</p>`
      }
      ${issues.map((i) => html`<p class="field-error">${i.text}</p>`)}
    `,
  );
}

function onPoolStrategyEdit(e) {
  const el = e.target.closest('[data-strategy]');
  if (!el) return;
  // Searchable selects commit through change; text fields update as the user types.
  if (e.type !== (el.tagName === 'SELECT' ? 'change' : 'input')) return;
  const s = poolEditor.strategies.get(Number(el.closest('tr').dataset.variant)),
    field = el.dataset.strategy;
  if (field === 'open') {
    s.open = el.checked;
    el.closest('tr').classList.toggle('closed', !s.open);
  } else if (field === 'name') s.name = el.value;
  else if (field === 'price') s.price = Number(el.value);
  else if (field === 'pay_typeid') s.pay_typeid = Number(el.value) || 0;
  else {
    s.pay_type = Number(el.value);
    if (s.pay_type !== 4) s.pay_typeid = 0;
  }
  el.classList.remove('invalid');
  poolChanged();
  if (field === 'pay_type') renderStrategies();
  else if (field === 'open') {
    renderPoolContract();
    renderPoolButtons();
  } else if (field === 'pay_typeid') renderPoolButtons();
}
$('#pool-strategies').addEventListener('input', onPoolStrategyEdit);
$('#pool-strategies').addEventListener('change', onPoolStrategyEdit);

function poolChanged() {
  poolRememberActive();
  const cell = $(`#pool-strategies tr[data-variant="${poolEditor.activeID}"] [data-summary]`),
    row = poolActiveRow();
  if (cell && row) cell.innerHTML = poolVariantSummary(row);
  poolEditor.editSerial++;
  poolEditor.dirty = true;
  poolEditor.preview = null;
  $('#pool-publish').disabled = true;
  $('#pool-version').textContent = '有未保存的修改';
  $('#pool-preview').textContent = '';
  updatePoolControls();
}
$('#pool-strategies').addEventListener('click', (e) => {
  const tr = e.target.closest('tr[data-variant]');
  if (!tr || e.target.closest('input:not([data-pick]),select,label.switch')) return;
  const id = Number(tr.dataset.variant);
  if (id === poolEditor.activeID || policyBusy('pool-editor')) {
    $(`#pool-strategies [data-pick="${poolEditor.activeID}"]`).checked = true;
    return;
  }
  selectPoolVariant(id);
  $(`#pool-strategies [data-pick="${id}"]`)?.focus();
});
$('#pool-copy-apply').onclick = async () => {
  poolRememberActive();
  const ids = $$('#pool-copy-targets input:checked').map((el) => Number(el.value));
  if (!ids.length) return toast('请选择目标分池', true);
  if (
    !(await askConfirm(
      `复制到 ${ids.length} 个分池草稿？`,
      '当前分池的卡牌、权重、名声、初始忠诚度和筛选会复制过去；目标的消耗和价格各自保留。',
      { ok: '复制' },
    ))
  )
    return;
  const source = poolEditor.contents.get(poolEditor.activeID);
  for (const id of ids) {
    const target = poolEditor.contents.get(id),
      row = poolEditor.variants.find((v) => v.gacha_id === id),
      cards = new Map(poolEditor.cards.map((c) => [c.reward_type_id, c]));
    target.selected = new Map(
      [...source.selected].filter(([card]) => poolCardEligibleFor(cards.get(card), row)),
    );
    target.fames = new Map([...source.fames].filter(([card]) => target.selected.has(card)));
    target.loves = new Map([...source.loves].filter(([card]) => target.selected.has(card)));
    target.filters = structuredClone(source.filters);
  }
  poolChanged();
  renderStrategies();
  $('#pool-copy-menu').open = false;
  toast('已复制到所选分池草稿，可逐个切换核对后保存');
};
$('#pool-split-common').onchange = renderPoolSplit;
$('#pool-split-apply').onclick = async () => {
  poolRememberActive();
  const all = poolSplitPlan(),
    plan = all.filter((p) => p.selected.size),
    skipped = all.filter((p) => !p.selected.size);
  if (!plan.length) return toast('本分池没有可分配的职业卡牌', true);
  const replacing = plan.filter((p) => poolEditor.contents.get(p.v.gacha_id).selected.size).length;
  if (
    !(await askConfirm(
      `按职业把卡牌分到 ${plan.length} 个职业分池草稿？`,
      `${plan.map((p) => `${poolSplitLabel(p)}：${p.selected.size} 张`).join('\n')}${replacing ? `\n其中 ${replacing} 个分池已有卡牌，会被替换。` : ''}${skipped.length ? `\n\n无匹配卡牌，跳过并保留原配置：\n${skipped.map(poolSplitLabel).join('\n')}` : ''}\n\n权重、名声和初始忠诚度随卡牌一起分配；各分池的消耗和价格不变。`,
      { ok: '分配' },
    ))
  )
    return;
  for (const p of plan) {
    const target = poolEditor.contents.get(p.v.gacha_id);
    target.selected = p.selected;
    target.fames = new Map([...poolEditor.fames].filter(([id]) => p.selected.has(id)));
    target.loves = new Map([...poolEditor.loves].filter(([id]) => p.selected.has(id)));
  }
  poolChanged();
  renderStrategies();
  $('#pool-split-menu').open = false;
  toast(
    `已分到 ${plan.length} 个职业分池草稿${skipped.length ? `；${skipped.map(poolSplitLabel).join('、')} 无匹配卡牌，已跳过并保留原配置` : ''}。可在分池表逐个核对后保存`,
  );
};
// The two pop-up menus share the bar: opening one closes the other.
['#pool-split-menu', '#pool-copy-menu'].forEach((id, i, all) =>
  $(id).addEventListener('toggle', () => {
    if (!$(id).open) return;
    $(all[1 - i]).open = false;
    if (id === '#pool-split-menu') renderPoolSplit();
  }),
);
// Mixed/staged reward tables and pool creation live in admin_pool_rules.js.
function poolIneligibleReason() {
  return '不符合当前分池的职业或星级限制';
}
// Reads the filter controls once and returns the per-card test; callers run it over the whole catalog (10k+ cards).
function poolCardFilter() {
  const q = $('#pool-search').value.trim().toLowerCase(),
    eligibleOnly = $('#pool-eligible-only').checked,
    f = cardFilterQuery('pool', true),
    row = poolActiveRow();
  return (card) =>
    (!eligibleOnly || poolCardEligibleFor(card, row) || poolEditor.selected.has(card.reward_type_id)) &&
    matchesCardQuery(card, f) &&
    cardMatchesJob(card, poolEditor.job) &&
    (!q || matchesWords(cardSearchText(card), q));
}
// Any existing card may join an operator pool; only the unowned-six-star pool keeps its six-star identity (server rule).
function poolCardEligibleFor(card, row) {
  const job = row?.base?.arthur_type || 0;
  return (
    !!card &&
    !(row?.base?.unowned_only && card.rarity !== 6) &&
    (!(job >= 1 && job <= 4) || !card.arthur_type || card.arthur_type === job)
  );
}
function poolCardEligible(card) {
  return poolCardEligibleFor(card, poolActiveRow());
}
function renderPoolCards() {
  $$('#pool-jobs button').forEach((b) =>
    b.classList.toggle('active', Number(b.dataset.job) === poolEditor.job),
  );

  const matches = poolEditor.cards.filter(poolCardFilter()),
    pages = Math.max(1, Math.ceil(matches.length / 48));
  poolEditor.page = Math.max(0, Math.min(poolEditor.page, pages - 1));
  $('#pool-card-count').innerHTML =
    `<b>筛选结果 ${num(matches.length)} 张</b><span>目录 ${num(poolEditor.cards.length)} 张 · ${poolEditor.page + 1} / ${pages} 页</span>`;
  const base = poolEditor.row?.base;
  $('#pool-eligibility-hint').textContent = base?.unowned_only
    ? '点击卡牌加入或移出草稿。此池为“必得未入手六星”，只接受6星卡牌。'
    : '点击卡牌加入或移出草稿。任意卡牌都可加入（含非扭蛋来源和新卡）；带保底的卡池仍须符合保底星级。';
  $('#pool-eligible-only-field').hidden = !base?.unowned_only;
  $('#pool-prev').disabled = poolEditor.page === 0;
  $('#pool-next').disabled = poolEditor.page >= pages - 1;
  $('#pool-cards').innerHTML =
    matches
      .slice(poolEditor.page * 48, (poolEditor.page + 1) * 48)
      .map((card) => {
        const on = poolEditor.selected.has(card.reward_type_id),
          eligible = poolCardEligible(card);
        return `<button class="catalog-card ${on ? 'selected' : ''}" data-card="${card.reward_type_id}" aria-pressed="${on}" ${!on && !eligible ? `disabled title="不能加入此卡池：${poolIneligibleReason(card)}（服务器规则）"` : ''}><div class="thumb">${image(card.image_url, card.name)}</div><div class="catalog-copy"><b class="name" title="${esc(card.name)}">${esc(card.name)}</b><span class="meta">${catalogMeta(card)}${!on && !eligible ? `<span class="tag why">${poolIneligibleReason(card)}</span>` : ''}${card.resource_state === 'unavailable' ? '<span class="tag bad">资源待补齐</span>' : card.image_url ? '' : '<span class="tag">无缩略图</span>'}${on ? `<span class="tag ok">权重 ${esc(poolEditor.selected.get(card.reward_type_id))}</span>` : ''}</span><span class="facts-text">${esc(cardSourceDescription(card))}</span></div></button>`;
      })
      .join('') || '<div class="empty"><b>没有匹配的卡牌</b>调整搜索或筛选条件。</div>';
  $$('#pool-cards [data-card]').forEach(
    (button) =>
      (button.onclick = () => {
        const id = Number(button.dataset.card);
        poolEditor.selected.has(id) ? poolEditor.selected.delete(id) : poolEditor.selected.set(id, 1);
        poolChanged();
        renderPoolCards();
        renderSelectedCards();
      }),
  );
}
// 初始忠诚度: points up to the card's love_max; the game's odds page shows floor(points × 100 / love_max)%.
const poolLovePercent = (points, max) => (max ? Math.floor((points * 100) / max) : 0);
const poolLoveValid = (v, card) => Number.isInteger(v) && v >= 0 && v <= (card?.love_max || 0);
// The note shows what the game's odds page shows; the card's cap stays in the input's tooltip.
function poolLoveNote(v, card) {
  const max = card?.love_max || 0;
  if (!max) return v ? '<span class="pool-love-note bad">无忠诚度，须为 0</span>' : '无忠诚度';
  if (!poolLoveValid(v, card)) return `<span class="pool-love-note bad">0–${num(max)} 的整数</span>`;
  return `${poolLovePercent(v, max)}%`;
}
function poolLoveCell(id, card) {
  const max = card?.love_max || 0,
    v = poolEditor.loves.get(id) ?? 0;
  return `<input aria-label="${id} 初始忠诚度" data-love="${id}" type="number" min="0" max="${max}" step="1" value="${esc(v)}" title="${max ? `0–${num(max)} 点` : '此卡无忠诚度'}" class="${poolLoveValid(v, card) ? '' : 'invalid'}" ${max || v ? '' : 'disabled'}><span class="sub" data-love-note>${poolLoveNote(v, card)}</span>`;
}
function syncPoolLoveCell(input) {
  const id = Number(input.dataset.love),
    card = poolEditor.cards.find((c) => c.reward_type_id === id),
    v = poolEditor.loves.get(id) ?? 0;
  input.classList.toggle('invalid', !poolLoveValid(v, card));
  input.nextElementSibling.innerHTML = poolLoveNote(v, card);
}
function renderSelectedCards() {
  const cards = new Map(poolEditor.cards.map((card) => [card.reward_type_id, card]));
  const match = poolCardFilter(),
    rows = [...poolEditor.selected].filter(([id]) => !cards.has(id) || match(cards.get(id)));
  const total = [...poolEditor.selected.values()].reduce((a, b) => a + (Number(b) || 0), 0);
  const pick = poolActiveRow()?.base.user_select_max || 0;
  $('#pool-selected-count').textContent =
    `${pick ? '候选' : '已选'} ${poolEditor.selected.size} 张${rows.length !== poolEditor.selected.size ? ` · 当前筛选显示 ${rows.length} 张` : ''}`;
  poolEditor.selectedPage = renderPager(
    'pool-selected',
    poolEditor.selectedPage,
    rows.length,
    100,
    (page) => {
      poolEditor.selectedPage = page;
      renderSelectedCards();
    },
  );
  $('#pool-selected').innerHTML =
    rows
      .slice(poolEditor.selectedPage * 100, (poolEditor.selectedPage + 1) * 100)
      .map(
        ([id, weight]) =>
          `<tr><td class="cell-wrap"><div class="reward-cell"><span class="thumb thumb-sm">${image(cards.get(id)?.image_url, cards.get(id)?.name || '?')}</span><div><b class="pool-card-name">${esc(cards.get(id)?.name || '未知卡牌')}</b><span class="sub"><code>${id}</code>${cards.get(id)?.rarity ? ` · ${cardStars(cards.get(id))}` : ''}${cards.get(id)?.resource_state === 'unavailable' ? ' <span class="tag bad">资源待补齐</span>' : ''}</span></div></div></td><td><input aria-label="${id} 权重" data-weight="${id}" type="number" min="1" max="1000000" step="1" value="${esc(weight)}"><span class="sub">约 ${total ? ((Number(weight) / total) * 100).toFixed(3) : '0'}%</span></td><td><input aria-label="${id} 抽出名声" data-fame="${id}" type="number" min="1" max="${cards.get(id)?.fame_max || 1}" step="1" value="${poolEditor.fames.get(id) || 1}" title="1–${cards.get(id)?.fame_max || 1}"><span class="sub">上限 ${cards.get(id)?.fame_max || '—'}</span></td><td>${poolLoveCell(id, cards.get(id))}</td><td><button class="x-button" data-remove="${id}" aria-label="移除 ${id}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`,
      )
      .join('') ||
    emptyRow(
      5,
      poolEditor.selected.size ? '当前筛选下没有已选卡牌' : '还没有选择卡牌',
      '在左侧目录点击卡牌加入草稿。',
    );
  $('#pool-selected-hint').textContent = pick
    ? `自选卡池：玩家先从这些候选中选满 ${pick} 张，之后每次只从所选卡牌按相对权重抽取（可重复）。候选须至少 ${pick} 张${poolEditor.selected.size < pick ? `，还差 ${pick - poolEditor.selected.size} 张` : ''}。`
    : '翻页与筛选保留已选卡牌与权重；约占比按全部已选卡的相对权重估算，以保存后的预览为准。';
  $('#pool-selected-hint').classList.toggle('pool-pick-short', pick > poolEditor.selected.size);
  renderRarityTool();
  $$('#pool-selected [data-weight]').forEach(
    (input) =>
      (input.oninput = () => {
        poolEditor.selected.set(Number(input.dataset.weight), Number(input.value));
        input.classList.remove('invalid');
        poolChanged();
      }),
  );
  $$('#pool-selected [data-fame]').forEach(
    (input) =>
      (input.oninput = () => {
        const id = Number(input.dataset.fame),
          v = Number(input.value);
        v > 1 ? poolEditor.fames.set(id, v) : poolEditor.fames.delete(id);
        input.classList.remove('invalid');
        poolChanged();
      }),
  );
  $$('#pool-selected [data-love]').forEach(
    (input) =>
      (input.oninput = () => {
        const id = Number(input.dataset.love),
          v = input.value.trim() === '' ? 0 : Number(input.value);
        v === 0 ? poolEditor.loves.delete(id) : poolEditor.loves.set(id, v);
        syncPoolLoveCell(input);
        poolChanged();
      }),
  );
  $$('#pool-selected [data-remove]').forEach(
    (button) =>
      (button.onclick = () => {
        poolEditor.selected.delete(Number(button.dataset.remove));
        poolEditor.fames.delete(Number(button.dataset.remove));
        poolEditor.loves.delete(Number(button.dataset.remove));
        poolChanged();
        renderPoolCards();
        renderSelectedCards();
      }),
  );
}
// Per-rarity totals: current share from weights, and a target that is spread evenly over that rarity's cards.
function poolRarityGroups() {
  const cards = new Map(poolEditor.cards.map((c) => [c.reward_type_id, c])),
    groups = new Map();
  let total = 0;
  for (const [id, w] of poolEditor.selected) {
    const r = cards.get(id)?.rarity || 0,
      g = groups.get(r) || { rarity: r, ids: [], weight: 0 };
    g.ids.push(id);
    g.weight += Number(w) || 0;
    groups.set(r, g);
    total += Number(w) || 0;
  }
  return { groups: [...groups.values()].sort((a, b) => b.rarity - a.rarity), total };
}
function renderRarityTool() {
  const { groups, total } = poolRarityGroups();
  $('#pool-rarity-tool').hidden = !groups.length;
  if (!groups.length) return;
  const keep = new Map(
    $$('#pool-rarity-rows [data-rarity]').map((el) => [Number(el.dataset.rarity), el.value]),
  );
  $('#pool-rarity-rows').innerHTML = groups
    .map((g) => {
      const pct = total ? (g.weight / total) * 100 : 0,
        value = keep.has(g.rarity) ? keep.get(g.rarity) : +pct.toFixed(6);
      return `<tr><td><span class="stars">${'★'.repeat(Math.min(g.rarity, 8))}</span> ${g.rarity || '?'} 星</td><td class="num">${g.ids.length}</td><td class="num">${pct.toFixed(3)}%</td><td><span class="input-unit"><input type="number" min="0" max="100" step="0.0001" data-rarity="${g.rarity}" value="${value}" aria-label="${g.rarity}星目标合计概率"><span>%</span></span></td><td class="num" data-per="${g.rarity}"></td></tr>`;
    })
    .join('');
  updateRarityTool();
}
function rarityTargets() {
  return $$('#pool-rarity-rows [data-rarity]').map((el) => ({
    rarity: Number(el.dataset.rarity),
    pct: el.value === '' ? NaN : Number(el.value),
    el,
  }));
}
function updateRarityTool() {
  const { groups } = poolRarityGroups(),
    targets = rarityTargets(),
    sum = targets.reduce((n, t) => n + (Number.isFinite(t.pct) ? t.pct : 0), 0),
    ok =
      targets.every((t) => Number.isFinite(t.pct) && t.pct > 0 && t.pct <= 100) &&
      Math.abs(sum - 100) < 0.001;
  for (const t of targets) {
    const g = groups.find((g) => g.rarity === t.rarity);
    $(`#pool-rarity-rows [data-per="${t.rarity}"]`).textContent =
      g && Number.isFinite(t.pct) ? `${(t.pct / g.ids.length).toFixed(4)}%` : '—';
    t.el.classList.toggle('invalid', !(Number.isFinite(t.pct) && t.pct > 0 && t.pct <= 100));
  }
  const touched = renderRarityTool.touched;
  $('#pool-rarity-sum').textContent = !touched
    ? '修改目标合计后点击“平均分配到每张卡”'
    : `目标合计 ${+sum.toFixed(4)}%${ok ? '' : Math.abs(sum - 100) < 0.001 ? '（每个星级须大于 0；不想出现的星级请移除其卡牌）' : '（须等于 100%）'}`;
  $('#pool-rarity-sum').className = ok || !touched ? 'hint' : 'state-error';
  if (!touched) targets.forEach((t) => t.el.classList.remove('invalid'));
  $('#pool-rarity-apply').disabled = !ok || !touched;
}
$('#pool-rarity-rows').oninput = () => {
  renderRarityTool.touched = true;
  updateRarityTool();
};
$('#pool-rarity-reset').onclick = () => {
  $('#pool-rarity-rows').innerHTML = '';
  renderRarityTool.touched = false;
  renderRarityTool();
};
$('#pool-rarity-apply').onclick = async () => {
  const { groups } = poolRarityGroups(),
    targets = rarityTargets();
  // Per-card weight on a 1,000,000 scale, then scaled up so the largest weight stays within the server limit.
  const raw = new Map(
      targets.map((t) => {
        const g = groups.find((g) => g.rarity === t.rarity);
        return [t.rarity, (t.pct * 10000) / g.ids.length];
      }),
    ),
    scale = Math.max(1, Math.floor(1000000 / Math.max(...raw.values())));
  const lines = targets.map((t) => {
    const g = groups.find((g) => g.rarity === t.rarity);
    return `${t.rarity} 星：${g.ids.length} 张，合计 ${t.pct}%（每张约 ${(t.pct / g.ids.length).toFixed(4)}%）`;
  });
  if (
    !(await askConfirm(
      `按星级平均分配已选的 ${poolEditor.selected.size} 张卡牌的权重？`,
      `${lines.join('\n')}\n\n会覆盖这些卡牌当前的权重，保存草稿后才生效。`,
      { ok: '分配权重' },
    ))
  )
    return;
  for (const g of groups) {
    const w = Math.min(1000000, Math.max(1, Math.round(raw.get(g.rarity) * scale)));
    for (const id of g.ids) poolEditor.selected.set(id, w);
  }
  $('#pool-rarity-rows').innerHTML = '';
  renderRarityTool.touched = false;
  poolChanged();
  renderSelectedCards();
  renderPoolCards();
  toast('已按星级分配权重，请保存草稿并预览核对实际概率');
};
function poolVariantConfig(id) {
  const state = poolEditor.contents.get(id),
    entries = [...state.selected].sort((a, b) => a[0] - b[0]);
  const shared = {
    start_unix: unixInput('#pool-start'),
    end_unix: unixInput('#pool-end'),
    odds_msg: $('#pool-odds-msg').value,
    card_ids: entries.map((e) => e[0]),
    weights: entries.map((e) => e[1]),
  };
  const fames = Object.fromEntries(
    [...state.fames].filter(([id, f]) => f > 1 && state.selected.has(id)).sort((a, b) => a[0] - b[0]),
  );
  if (!poolEditor.mixedByID.has(id) && Object.keys(fames).length) shared.card_fames = fames;
  const loves = Object.fromEntries(
    [...state.loves].filter(([id, v]) => v !== 0 && state.selected.has(id)).sort((a, b) => a[0] - b[0]),
  );
  if (!poolEditor.mixedByID.has(id) && Object.keys(loves).length) shared.card_loves = loves;
  if (poolEditor.coverPath) shared.cover_path = poolEditor.coverPath;
  return shared;
}
// Pool-wide daily limit and category; zero values stay out of the document like the server's omitempty fields.
function poolSharedLimits() {
  const daily = Number($('#pool-daily-limit').value),
    category = Number($('#pool-category').value);
  return { ...(daily !== 0 && { play_count_1day_max: daily }), ...(category && { category_num: category }) };
}
function poolConfigs() {
  poolRememberActive();
  return poolEditor.variants.map((v) => {
    const s = poolEditor.strategies.get(v.gacha_id),
      m = poolEditor.mixedByID.get(v.gacha_id);
    const extra = m
      ? { reward_pool: m.steps.length ? m.steps[0].reward_pool : m.reward_pool, steps: m.steps }
      : {};
    if (!s.open) extra.closed = true;
    return {
      ...extra,
      ...poolVariantConfig(v.gacha_id),
      pay_type: s.pay_type,
      pay_typeid: s.pay_type === 4 ? s.pay_typeid : 0,
      gacha_id: v.gacha_id,
      name: s.name.trim(),
      price: m?.steps.length ? m.steps[0].price : s.price,
      play_count_max: Number($('#pool-play-limit').value),
      ...poolSharedLimits(),
      ...poolGiftConfig(v.gacha_id),
    };
  });
}
function poolProblems(configs) {
  const problems = [];
  clearInvalid($('#pool-editor'));
  const bad = (el, text) => {
    problems.push(text);
    markInvalid(el, text);
  };
  const cell = (id, field) => $(`#pool-strategies tr[data-variant="${id}"] [data-strategy="${field}"]`);
  const label = (c) => (configs.length > 1 ? `抽法 ${c.gacha_id} 的` : '');
  const cards = new Map(poolEditor.cards.map((c) => [c.reward_type_id, c]));
  for (const c of configs) {
    if (!c.name) bad(cell(c.gacha_id, 'name'), `${label(c)}显示名称不能为空`);
    if (
      !poolEditor.mixedByID.get(c.gacha_id)?.steps?.length &&
      (!Number.isInteger(c.price) || c.price < 1 || c.price > 10000000)
    )
      bad(cell(c.gacha_id, 'price'), `${label(c)}价格须为1–10000000的整数`);
    if (c.pay_type === 4 && !c.pay_typeid) bad(cell(c.gacha_id, 'pay_typeid'), `${label(c)}消耗道具未选择`);
    if (!poolEditor.mixedByID.has(c.gacha_id)) {
      if (!c.card_ids.length) problems.push(`${label(c)}至少选择一张卡牌`);
      if (c.weights.some((w) => !Number.isInteger(w) || w < 1 || w > 1000000))
        problems.push(`${label(c)}权重须为1–1000000的整数`);
      if (
        Object.entries(c.card_fames || {}).some(
          ([id, f]) => !Number.isInteger(f) || f < 1 || f > (cards.get(Number(id))?.fame_max || 1),
        )
      )
        problems.push(`${label(c)}抽出名声超出对应卡牌上限`);
      const loves = Object.entries(c.card_loves || {}).filter(
        ([id, v]) => !poolLoveValid(v, cards.get(Number(id))),
      );
      if (loves.length) {
        problems.push(`${label(c)}有 ${loves.length} 张卡牌的初始忠诚度不是 0 到该卡上限的整数`);
        if (c.gacha_id === poolEditor.activeID)
          for (const [id] of loves)
            markInvalid($(`#pool-selected [data-love="${id}"]`), '超出范围或不是整数');
      }
      const row = poolEditor.variants.find((v) => v.gacha_id === c.gacha_id);
      if (c.card_ids.some((id) => !poolCardEligibleFor(cards.get(id), row)))
        problems.push(`${label(c)}有卡牌不符合职业或星级限制`);
      const pick = row?.base.user_select_max || 0;
      if (pick && c.card_ids.length < pick)
        problems.push(`${label(c)}候选卡牌须至少 ${pick} 张（玩家自选 ${pick} 张）`);
    }
  }
  if (configs.every((c) => c.closed))
    bad($('#pool-strategies [data-strategy="open"]'), '至少开放一种抽法；要关闭整个卡池请在「扭蛋发布」操作');
  for (const { ids, text } of poolDrawButtons().issues) {
    problems.push(text);
    ids.forEach((id) => markInvalid(cell(id, 'open'), text));
  }
  if (configs[0].start_unix && configs[0].end_unix && configs[0].end_unix <= configs[0].start_unix)
    bad($('#pool-end'), '结束时间须晚于开始时间');
  if (Array.from(configs[0].odds_msg || '').length > 1000) bad($('#pool-odds-msg'), '概率页说明最多1000字');
  const limit = configs[0].play_count_max;
  if (!Number.isInteger(limit) || limit < 0 || limit > 1000000)
    bad($('#pool-play-limit'), '整池累计次数须为0–1000000，0表示不限');
  if (!configs[0].category_num) bad($('#pool-category'), '请选择扭蛋分类');
  const daily = configs[0].play_count_1day_max ?? 0;
  if (!Number.isInteger(daily) || daily < 0 || daily > 1000000)
    bad($('#pool-daily-limit'), '每日限抽次数须为0–1000000，0表示不限');
  problems.push(...poolGiftProblems(configs, label));
  {
    const mixed = [...poolEditor.mixedByID.values()],
      pools = mixed.flatMap((m) => (m.steps.length ? m.steps.map((s) => s.reward_pool) : [m.reward_pool]));
    if (pools.some((pool) => !pool.length)) problems.push('每个混合奖励表／阶段至少添加一项奖励');
    if (
      pools.some((pool) =>
        pool.some(
          (r) =>
            !Number.isInteger(r.reward.num) || r.reward.num < 1 || r.reward.num > rewardMax(r.reward.type),
        ),
      )
    )
      problems.push('奖励数量超出允许范围');
    if (
      pools.some((pool) =>
        pool.some((r) => !Number.isInteger(r.weight) || r.weight < 1 || r.weight > 1000000),
      )
    )
      problems.push('混合奖励权重须为1–1000000的整数');
    if (
      mixed.some((m) => m.steps.some((s) => !Number.isInteger(s.price) || s.price < 1 || s.price > 10000000))
    )
      problems.push('各阶段价格须为1–10000000的整数');
    if (mixed.some((m) => m.steps.some((s) => s.pay_type === 4 && !s.pay_typeid)))
      problems.push('阶段消耗道具未选择');
  }
  return problems;
}
// Preview of the whole pool: shared facts once, then each draw method's cost and odds.
// Preview of the whole pool, shown both in the dialog and the inline panel: cover and pool-wide facts once,
// then each draw method's cost, schedule and contents, warnings, blocked cards, and per-method odds.
function renderPoolPreview(previews) {
  const list = poolEditor.variants.map((v) => previews[v.gacha_id]).filter(Boolean),
    c = list[0].config,
    ready = list.every((p) => p.publishable);
  const category = poolEditor.categories.find(({ id }) => id === list[0].base.category_num);
  const categoryName = category?.name || '默认分类';
  const blocked = [...new Set(list.flatMap((p) => p.blocked_card_ids || []))],
    warnings = [...new Set(list.flatMap((p) => p.warnings || []))];
  poolEditor.preview = { publishable: ready, blocked_card_ids: blocked, config: c, previews };
  const names = new Map(poolEditor.cards.map((card) => [card.reward_type_id, card.name])),
    loveMax = new Map(poolEditor.cards.map((card) => [card.reward_type_id, card.love_max || 0]));
  const when = (p) =>
    p.config.start_unix || p.config.end_unix
      ? `${describeTime(p.config.start_unix)} → ${describeTime(p.config.end_unix)}`
      : '不限';
  const contents = (p) => {
    const fames = Object.keys(p.config.card_fames || {}).length,
      loves = Object.keys(p.config.card_loves || {}).length;
    const extras = `${p.base.user_select_max ? ` · 玩家自选 ${p.base.user_select_max} 张` : ''}${p.config.random_gifts?.length ? ` · 随机附赠 ${p.config.random_gifts.length} 组` : ''}`;
    return p.config.card_ids?.length
      ? `${num(p.config.card_ids.length)} 张${p.base.user_select_max ? '候选' : ''}卡牌${fames ? ` · ${fames} 张卡牌自定义名声` : ''}${loves ? ` · ${loves} 张卡牌初始忠诚度` : ''}${extras}`
      : `${num((p.config.steps?.[0]?.reward_pool || p.config.reward_pool || []).length)} 项奖励${p.config.steps?.length ? ` · ${p.config.steps.length} 个阶段` : ''}${extras}`;
  };
  const odds = (p) =>
    (p.stages.length
      ? p.stages
      : [{ draw_count: p.base.card_num, card_ids: p.config.card_ids, odds_scaled: p.odds_scaled }]
    )
      .map(
        (stage) =>
          `<details><summary>${p.base.card_num} 抽 · ${esc(poolProfessionName(p.base.arthur_type))} <code>${p.config.gacha_id}</code> · ${esc(p.config.steps?.length ? stage.name : paymentName(p.base))} · ${stage.draw_count} 抽 · ${(stage.card_ids || stage.rewards || []).length} 项 · 展开概率</summary><div class="table-wrap" style="max-height:280px"><table><thead><tr><th>卡牌／奖励</th><th class="num">单次概率</th></tr></thead><tbody>${(stage.rewards || stage.card_ids || []).map((r, i) => `<tr><td>${esc(typeof r === 'number' ? names.get(r) || r : rewardLabel(r))}${typeof r === 'number' ? '' : ` × ${r.num}`}${typeof r === 'number' && p.config.card_fames?.[r] > 1 ? ` <span class="tag info">名声 ${p.config.card_fames[r]}</span>` : ''}${typeof r === 'number' && p.config.card_loves?.[r] > 0 ? ` <span class="tag info" title="${num(p.config.card_loves[r])} 点">忠诚度 ${poolLovePercent(p.config.card_loves[r], loveMax.get(r))}%</span>` : ''}</td><td class="num">${(stage.odds_scaled[i] / p.odds_scale).toFixed(5)}%</td></tr>`).join('')}</tbody></table></div></details>`,
      )
      .join('');
  const html =
    `<div class="preview-head"><div class="gacha-art preview-cover">${image(poolCoverURL(c.cover_path), c.name)}</div><div><h3>${esc(c.name)} ${ready ? '<span class="tag ok">可发布</span>' : '<span class="tag bad">不能发布</span>'}</h3><p class="hint">${list.length} 个分池一起保存和发布；开放由「扭蛋发布」控制，并须处于排期内。</p></div></div>` +
    `<div class="facts"><div class="fact"><span>封面</span><b>${c.cover_path ? '自定义封面' : '包内封面'}</b></div><div class="fact"><span>排期</span><b>${esc(when(list[0]))}</b></div><div class="fact"><span>累计限抽</span><b>${c.play_count_max ? `每人 ${num(c.play_count_max)} 次（单抽或连抽都计一次）` : '不限'}</b></div><div class="fact"><span>每日限抽</span><b>${c.play_count_1day_max ? `每人每天 ${num(c.play_count_1day_max)} 次（北京时间 0 点换日）` : '不限'}</b></div><div class="fact"><span>扭蛋分类</span><b>${esc(categoryName)}</b></div><div class="fact"><span>概率页说明</span>${c.odds_msg ? `<p class="pool-odds-text">${esc(c.odds_msg)}</p>` : '<b>未填写，不显示</b>'}</div></div>` +
    `<div class="table-wrap"><table class="dense preview-variants"><thead><tr><th>分池</th><th>显示名称</th><th>费用</th><th>内容</th><th>状态</th></tr></thead><tbody>${list.map((p) => `<tr class="${p.config.closed ? 'closed' : ''}"><td><b>${p.base.card_num} 抽 · ${esc(poolProfessionName(p.base.arthur_type))}</b><span class="sub"><code>${p.config.gacha_id}</code></span></td><td>${esc(p.config.name)}</td><td>${esc(paymentName(p.base))} × ${num(p.config.price)}${p.base.daily_first_free ? ' · 每日首次免费' : ''}</td><td>${contents(p)}</td><td>${p.config.closed ? '<span class="tag">关闭</span> ' : ''}${p.publishable ? '<span class="tag ok">可发布</span>' : '<span class="tag bad">不能发布</span>'}</td></tr>`).join('')}</tbody></table></div>` +
    `${warnings.length ? `<div class="pool-warning">${warnings.map(esc).join('<br>')}</div>` : ''}${blocked.length ? `<div class="pool-blocked">未闭包卡牌（资源缺失，不能发布）：${blocked.map((id) => `${esc(names.get(id) || id)}（${id}）`).join('、')} <button class="secondary sm" type="button" data-remove-blocked>从各分池草稿移除这 ${blocked.length} 张</button></div>` : ''}` +
    `${list.map(odds).join('')}<p class="hint">以上为本地运营配置概率。</p>`;
  $('#pool-preview').innerHTML = $('#pool-preview-modal-body').innerHTML = html;
  updatePoolControls();
}
$('#pool-select').onchange = async () => {
  if (!$('#pool-select').value) return;
  if (policyBusy('pool-editor')) {
    $('#pool-select').value = poolEditor.row?.gacha_id || '';
    return;
  }
  if (!(await confirmDiscard(poolEditor.dirty, '切换卡池会放弃未保存的编辑。', '放弃并切换'))) {
    $('#pool-select').value = poolEditor.row.gacha_id;
    return;
  }
  selectPool();
};
[
  '#pool-start',
  '#pool-end',
  '#pool-play-limit',
  '#pool-daily-limit',
  '#pool-category',
  '#pool-odds-msg',
].forEach(
  (id) =>
    ($(id).oninput = () => {
      $(id).classList.remove('invalid');
      poolChanged();
      if (id === '#pool-category') renderPoolContract();
    }),
);
// Counted in characters like the save check; an empty note shows nothing in game, so say that instead of 0 / 1000.
function renderPoolOddsCount() {
  const n = Array.from($('#pool-odds-msg').value).length,
    el = $('#pool-odds-msg-count');
  el.textContent = !n
    ? '留空，不显示'
    : n > 1000
      ? `超出 ${num(n - 1000)} 字（最多 1000）`
      : `${num(n)} / 1000 字`;
  el.classList.toggle('over', n > 1000);
}
$('#pool-odds-msg').addEventListener('input', renderPoolOddsCount);
['#pool-search', '#pool-rarity', '#pool-eligible-only'].forEach(
  (id) =>
    ($(id).oninput = () => {
      poolEditor.page = 0;
      poolEditor.selectedPage = 0;
      renderPoolCards();
      renderSelectedCards();
    }),
);
$$('#pool-jobs button').forEach(
  (b) =>
    (b.onclick = () => {
      poolEditor.job = Number(b.dataset.job);
      poolEditor.page = 0;
      poolEditor.selectedPage = 0;
      renderPoolCards();
      renderSelectedCards();
    }),
);

$('#pool-prev').onclick = () => {
  poolEditor.page--;
  renderPoolCards();
};
$('#pool-next').onclick = () => {
  poolEditor.page++;
  renderPoolCards();
};
$('#pool-add-matches').onclick = async () => {
  const match = poolCardFilter(),
    add = poolEditor.cards.filter(
      (card) => match(card) && poolCardEligible(card) && !poolEditor.selected.has(card.reward_type_id),
    );
  if (!add.length) return toast('当前筛选结果中没有可加入的新卡牌');
  if (poolEditor.selected.size + add.length > 6000)
    return toast(
      `加入后共 ${poolEditor.selected.size + add.length} 张，超过每个卡池 6000 张上限；请缩小筛选范围`,
      true,
    );
  if (
    !(await askConfirm(
      `把当前筛选结果中 ${add.length} 张可用卡牌加入草稿？`,
      '权重均为 1，可之后逐张调整。',
      { ok: '加入' },
    ))
  )
    return;
  add.forEach((card) => poolEditor.selected.set(card.reward_type_id, 1));
  poolChanged();
  renderPoolCards();
  renderSelectedCards();
};
$('#pool-clear').onclick = async () => {
  if (
    await askConfirm(`清空当前草稿中的全部 ${poolEditor.selected.size} 张卡牌？`, '', {
      ok: '清空',
      danger: true,
    })
  ) {
    poolEditor.selected.clear();
    poolChanged();
    renderPoolCards();
    renderSelectedCards();
  }
};
$('#pool-weight-all').onclick = async () => {
  const input = $('#pool-batch-weight'),
    weight = Number(input.value);
  input.classList.remove('invalid');
  if (!Number.isInteger(weight) || weight < 1 || weight > 1000000) {
    markInvalid(input);
    return toast('权重须为 1–1000000 的整数', true);
  }
  const ids = new Set(poolEditor.cards.filter(poolCardFilter()).map((card) => card.reward_type_id)),
    hit = [...poolEditor.selected.keys()].filter((id) => ids.has(id));
  if (!hit.length) return toast('当前筛选下没有已选卡牌');
  if (!(await askConfirm(`把当前筛选下的 ${hit.length} 张已选卡牌权重设为 ${weight}？`, '', { ok: '应用' })))
    return;
  for (const id of hit) poolEditor.selected.set(id, weight);
  poolChanged();
  renderSelectedCards();
  renderPoolCards();
};
async function setPoolFames(value) {
  const ids = new Set(poolEditor.cards.filter(poolCardFilter()).map((c) => c.reward_type_id)),
    cards = new Map(poolEditor.cards.map((c) => [c.reward_type_id, c])),
    hit = [...poolEditor.selected.keys()].filter((id) => ids.has(id));
  if (!hit.length) return toast('当前筛选下没有已选卡牌');
  if (
    !(await askConfirm(
      `把当前筛选下 ${hit.length} 张已选卡牌的抽出名声设为${value === 'max' ? '各自上限' : ` ${value}`}？`,
      value === 'max' ? '' : '超过卡牌上限的按上限设置。',
      { ok: '应用' },
    ))
  )
    return;
  for (const id of hit) {
    const f = value === 'max' ? cards.get(id)?.fame_max || 1 : Math.min(value, cards.get(id)?.fame_max || 1);
    f > 1 ? poolEditor.fames.set(id, f) : poolEditor.fames.delete(id);
  }
  poolChanged();
  renderSelectedCards();
}
$('#pool-fame-all').onclick = () => {
  const input = $('#pool-batch-fame'),
    v = Number(input.value);
  input.classList.remove('invalid');
  if (!Number.isInteger(v) || v < 1) {
    markInvalid(input);
    return toast('名声须为正整数', true);
  }
  setPoolFames(v);
};
$('#pool-fame-max').onclick = () => setPoolFames('max');
async function setPoolLoves(value) {
  const ids = new Set(poolEditor.cards.filter(poolCardFilter()).map((c) => c.reward_type_id)),
    cards = new Map(poolEditor.cards.map((c) => [c.reward_type_id, c])),
    hit = [...poolEditor.selected.keys()].filter((id) => ids.has(id));
  if (!hit.length) return toast('当前筛选下没有已选卡牌');
  if (
    !(await askConfirm(
      `把当前筛选下 ${hit.length} 张已选卡牌的初始忠诚度设为${value === 'max' ? '各自上限' : ` ${num(value)} 点`}？`,
      value === 'max' ? '' : '超过卡牌上限的按上限设置；没有忠诚度的卡牌保持 0。',
      { ok: '应用' },
    ))
  )
    return;
  for (const id of hit) {
    const max = cards.get(id)?.love_max || 0,
      v = value === 'max' ? max : Math.min(value, max);
    v > 0 ? poolEditor.loves.set(id, v) : poolEditor.loves.delete(id);
  }
  poolChanged();
  renderSelectedCards();
}
$('#pool-love-all').onclick = () => {
  const input = $('#pool-batch-love'),
    v = Number(input.value);
  input.classList.remove('invalid');
  if (input.value.trim() === '' || !Number.isInteger(v) || v < 0) {
    markInvalid(input);
    return toast('初始忠诚度须为 0 或正整数', true);
  }
  setPoolLoves(v);
};
$('#pool-love-max').onclick = () => setPoolLoves('max');
$('#pool-export').onclick = () => {
  const url = URL.createObjectURL(
    new Blob([JSON.stringify({ group_id: poolEditor.row.base.groupid, configs: poolConfigs() }, null, 2)], {
      type: 'application/json',
    }),
  );
  const link = document.createElement('a');
  link.href = url;
  link.download = `gacha-pool-${poolEditor.row.base.groupid}.json`;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
};
// Import accepts a whole-pool export ({group_id, configs}) or an older single-variant file of this pool.
$('#pool-import').onchange = (event) =>
  runPoolEdit(async () => {
    try {
      const file = event.target.files[0];
      if (!file) return;
      if (file.size > 8 * 1024 * 1024) throw new Error('配置文件超过 8 MiB');
      let data;
      try {
        data = JSON.parse(await file.text());
      } catch {
        throw new Error('配置文件不是有效的 JSON');
      }
      const ids = new Set(poolEditor.variants.map((v) => v.gacha_id)),
        configs = Array.isArray(data?.configs) ? data.configs : [data];
      if (
        !configs.length ||
        configs.some((c) => !ids.has(c?.gacha_id)) ||
        new Set(configs.map((c) => c.gacha_id)).size !== configs.length ||
        (Array.isArray(data?.configs) &&
          (data.group_id !== poolEditor.row.base.groupid || configs.length !== ids.size))
      )
        throw new Error('导入身份须与当前卡池的分池一致');
      for (const c of configs) {
        if (
          !Array.isArray(c.card_ids) ||
          !Array.isArray(c.weights) ||
          c.card_ids.length !== c.weights.length ||
          c.card_ids.length > 6000 ||
          new Set(c.card_ids).size !== c.card_ids.length ||
          c.card_ids.some((id) => !Number.isInteger(id) || id <= 0) ||
          c.weights.some((w) => !Number.isInteger(w) || w < 1 || w > 1000000)
        )
          throw new Error(`分池 ${c.gacha_id} 的卡牌／权重无效`);
        if (
          typeof c.name !== 'string' ||
          !Number.isInteger(c.price) ||
          (c.pay_type != null && ![0, 2, 3, 4, 6].includes(c.pay_type))
        )
          throw new Error('名称、价格或消耗方式无效');
        if (c.odds_msg != null && (typeof c.odds_msg !== 'string' || Array.from(c.odds_msg).length > 1000))
          throw new Error('概率页说明须为最多1000字的文本');
        if (
          c.card_fames != null &&
          (typeof c.card_fames !== 'object' ||
            Object.entries(c.card_fames).some(
              ([id, f]) => !c.card_ids.includes(Number(id)) || !Number.isInteger(f) || f < 1,
            ))
        )
          throw new Error('抽出名声设置无效');
        if (
          c.card_loves != null &&
          (typeof c.card_loves !== 'object' ||
            Array.isArray(c.card_loves) ||
            Object.entries(c.card_loves).some(
              ([id, v]) => !c.card_ids.includes(Number(id)) || !Number.isInteger(v) || v < 0,
            ))
        )
          throw new Error(`分池 ${c.gacha_id} 的初始忠诚度设置无效`);
        if (
          c.play_count_1day_max != null &&
          (!Number.isInteger(c.play_count_1day_max) || c.play_count_1day_max < 0)
        )
          throw new Error('每日限抽次数无效');
        if (
          c.category_num != null &&
          !poolEditor.categories.some(({ id }) => id === c.category_num) &&
          c.category_num !== poolEditor.variants.find((v) => v.gacha_id === c.gacha_id).base.category_num
        )
          throw new Error('扭蛋分类无效');
        if (c.random_gifts != null && !poolGiftsShaped(c.random_gifts))
          throw new Error(`分池 ${c.gacha_id} 的随机附赠格式无效`);
        if (
          poolEditor.mixedByID.has(c.gacha_id) &&
          (!weightedPoolShaped(c.reward_pool) ||
            (c.steps != null &&
              (!Array.isArray(c.steps) || c.steps.some((s) => !s || !weightedPoolShaped(s.reward_pool)))))
        )
          throw new Error(`分池 ${c.gacha_id} 的混合奖励表或阶段格式无效`);
        localTimeInput(c.start_unix);
        localTimeInput(c.end_unix);
      }
      const first = configs[0];
      if (first.cover_path && !poolCoverPattern.test(first.cover_path)) throw new Error('封面路径无效');
      const imported = configs.map((c) => ({
        config: c,
        contents: poolVariantState(c),
        gifts: poolGiftState(c.random_gifts),
        mixed: poolEditor.mixedByID.has(c.gacha_id) ? poolMixedState(c) : null,
      }));
      if (
        !(await askConfirm(
          `以导入内容替换 ${configs.length} 个分池草稿？`,
          '卡牌、权重、名声、初始忠诚度、消耗和随机附赠分别载入；封面、排期、概率页说明、限抽次数和分类使用第一项。',
          { ok: '替换' },
        ))
      )
        return;
      poolEditor.coverPath = first.cover_path || '';
      $('#pool-play-limit').value = first.play_count_max || 0;
      $('#pool-daily-limit').value = first.play_count_1day_max || 0;
      renderPoolCategoryOptions(first.category_num || 0);
      $('#pool-category').value = String(first.category_num || '');
      $('#pool-odds-msg').value = first.odds_msg || '';
      renderPoolOddsCount();
      $('#pool-start').value = localTimeInput(first.start_unix);
      $('#pool-end').value = localTimeInput(first.end_unix);
      $('#pool-schedule-note').hidden = true;
      for (const { config: c, contents, gifts, mixed } of imported) {
        const base = poolEditor.variants.find((v) => v.gacha_id === c.gacha_id).base,
          type = c.pay_type || base.pay_type;
        poolEditor.contents.set(c.gacha_id, contents);
        poolEditor.randomGifts.set(c.gacha_id, gifts);
        poolEditor.strategies.set(c.gacha_id, {
          open: !c.closed,
          name: c.name,
          pay_type: type,
          pay_typeid: type === 4 ? (c.pay_type ? c.pay_typeid : base.pay_typeid) || 0 : 0,
          price: c.price,
        });
        if (mixed) poolEditor.mixedByID.set(c.gacha_id, mixed);
      }
      selectPoolVariant(poolEditor.activeID, false);
      renderStrategies();
      renderPoolCover();
      poolChanged();
      toast('已分别载入分池草稿，请保存并预览');
    } finally {
      event.target.value = '';
    }
  }, '导入');
$('#pool-save-preview').onclick = () =>
  runPoolEdit(async () => {
    const rows = poolEditor.variants,
      serial = poolEditor.editSerial,
      configs = poolConfigs(),
      problems = poolProblems(configs);
    if (problems.length) {
      showViewAlert('pool-editor', {
        title: `不能保存草稿：${problems.length} 处需要修正`,
        message: problems.join('；'),
      });
      return;
    }
    const result = await api('/api/gacha-editor/group/draft', {
      method: 'POST',
      body: JSON.stringify({
        group_id: rows[0].base.groupid,
        configs,
        expected: Object.fromEntries(rows.map((r) => [r.gacha_id, { draft_revision: r.draft.revision }])),
      }),
    });
    for (const r of rows) r.draft = result.documents[r.gacha_id];
    clearViewAlert('pool-editor');
    if (poolEditor.variants === rows && poolEditor.editSerial === serial) {
      poolEditor.dirty = false;
      renderPoolVersion();
      renderPoolPreview(result.previews);
      $('#pool-preview-modal').classList.add('open');
    }
    renderPoolSelect();
    state.loaded.delete('audit');
    if (!rows.every((r) => result.previews[r.gacha_id].publishable))
      toast('草稿已保存，但含未闭包卡牌，暂不能发布', true);
  });
// Blocked cards cannot be published; remove them from the draft in one step, then save and preview again.
function removeBlockedPoolCards() {
  const ids = poolEditor.preview?.blocked_card_ids || [];
  if (!ids.length || policyBusy('pool-editor')) return;
  for (const state of poolEditor.contents.values())
    ids.forEach((id) => {
      state.selected.delete(id);
      state.fames.delete(id);
      state.loves.delete(id);
    });
  $('#pool-preview-modal').classList.remove('open');
  poolChanged();
  renderPoolCards();
  renderSelectedCards();
  toast(`已从草稿移除 ${ids.length} 张未闭包卡牌，请重新保存草稿并预览`);
}
['#pool-preview', '#pool-preview-modal-body'].forEach((id) =>
  $(id).addEventListener('click', (e) => {
    if (e.target.closest('[data-remove-blocked]')) removeBlockedPoolCards();
  }),
);
$('#pool-publish').onclick = () => publishPool(true);
$('#pool-preview-publish').onclick = () => publishPool(false);
$('#pool-preview-close').onclick = () => $('#pool-preview-modal').classList.remove('open');
async function publishPool(ask) {
  if (policyBusy('pool-editor') || !poolEditor.row || !poolEditor.preview?.publishable || poolEditor.dirty)
    return;
  if (poolPublished()) return;
  const rows = poolEditor.variants,
    row = rows[0];
  if (
    ask &&
    !(await askConfirm(
      `发布「${poolEditor.preview.config.name}」？`,
      `${rows.length > 1 ? `${rows.length} 种抽法一起发布；` : ''}草稿 v${row.draft.revision} 将替换已发布 v${row.live.revision}，玩家下次请求使用新配置。`,
      { ok: '发布' },
    ))
  )
    return;
  return runPoolEdit(async () => {
    const result = await api('/api/gacha-editor/group/publish', {
      method: 'POST',
      body: JSON.stringify({
        group_id: row.base.groupid,
        expected: Object.fromEntries(
          rows.map((r) => [
            r.gacha_id,
            { draft_revision: r.draft.revision, live_revision: r.live.revision, sha256: r.draft.sha256 },
          ]),
        ),
      }),
    });
    for (const r of rows) {
      r.live = result.documents[r.gacha_id];
      r.config = r.draft.payload;
      r.draft = result.drafts[r.gacha_id];
    }
    $('#pool-preview-modal').classList.remove('open');
    if (poolEditor.variants === rows) {
      renderPoolVersion();
      poolEditor.preview = null;
      $('#pool-preview').innerHTML =
        `<p class="state-ok">已发布 v${row.live.revision}：${esc(row.config.name)}${rows.length > 1 ? `（${rows.length} 种抽法）` : ''}。玩家下次请求使用新配置。</p>`;
    }
    renderPoolSelect();
    clearViewAlert('pool-editor');
    state.loaded.delete('audit');
    state.loaded.delete('gachas');
    if (poolEditor.openGroups && !poolEditor.openGroups.has(row.base.groupid))
      toast('已发布配置。此卡池尚未在「扭蛋发布」开启，玩家仍看不到它', true);
    else toast(rows.length > 1 ? `卡池已发布（${rows.length} 种抽法）` : '卡池配置已发布');
  }, '发布');
}
// Discard removes the saved body and reloads the published config or template.
$('#pool-discard').onclick = async () => {
  const rows = poolEditor.variants,
    row = poolEditor.row;
  if (!row || policyBusy('pool-editor')) return;
  const pending = rows.filter(poolHasDraft),
    published = row.live.revision > 0;
  if (!pending.length && !poolEditor.dirty) return toast('当前没有需要放弃的草稿');
  if (
    !(pending.length
      ? await askConfirm(
          `删除此卡池的已保存草稿${poolEditor.dirty ? '和未保存的修改' : ''}？`,
          `页面将载入${published ? `已发布版本 v${row.live.revision}` : '模板默认配置；此卡池仍未发布'}。`,
          { ok: '删除草稿', danger: true },
        )
      : await confirmDiscard(true, '页面会恢复为已保存的内容。'))
  )
    return;
  return runPoolEdit(async () => {
    if (pending.length) {
      const result = await api('/api/gacha-editor/group/discard', {
        method: 'POST',
        body: JSON.stringify({
          group_id: row.base.groupid,
          expected: Object.fromEntries(
            rows.map((r) => [
              r.gacha_id,
              { draft_revision: r.draft.revision, live_revision: r.live.revision },
            ]),
          ),
        }),
      });
      for (const r of rows) r.draft = result.documents[r.gacha_id];
    }
    poolEditor.dirty = false;
    selectPool(row.gacha_id);
    renderPoolSelect();
    state.loaded.delete('audit');
    clearViewAlert('pool-editor');
    toast(
      pending.length
        ? `已删除草稿内容，载入${published ? '已发布版本' : '模板默认配置（仍未发布）'}`
        : '已放弃未保存的修改',
    );
  }, '放弃草稿');
};

// Cover upload: the server stores the image by content hash; the draft references it until published.
// Upload errors stay under the button (uploadAlertHTML), not in the page-top alert.
$('#pool-cover-upload').onchange = (event) =>
  runPoolEdit(async () => {
    const file = event.target.files[0];
    event.target.value = '';
    if (!file) return;
    $('#pool-cover-alert').innerHTML = '';
    try {
      if (!/^image\/(png|jpeg)$/.test(file.type)) throw new Error('不是 PNG 或 JPEG 图片');
      if (file.size > 2 * 1024 * 1024) throw new Error(`${fileSizeText(file.size)}，超过 2 MiB 上限`);
      const data = await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result).split(',')[1]);
        reader.onerror = () => reject(new Error('读取封面文件失败'));
        reader.readAsDataURL(file);
      });
      const result = await api('/api/gacha-covers', {
        method: 'POST',
        body: JSON.stringify({ data_base64: data }),
      });
      poolEditor.coverPath = result.cover_path;
      poolChanged();
      renderPoolCover();
      const ratio = result.width / result.height;
      toast(
        `封面已上传（${result.width}×${result.height}）${Math.abs(ratio - 934 / 372) > 0.3 ? '，比例与建议的 934×372 不同，游戏内可能被裁切' : ''}；保存草稿并发布后生效`,
        Math.abs(ratio - 934 / 372) > 0.3,
      );
    } catch (e) {
      setHTML(
        $('#pool-cover-alert'),
        uploadAlertHTML(
          '封面没有上传',
          [`「${file.name}」：${errorText(e)}`],
          e.status === undefined || e.status === 400 ? UPLOAD_IMAGE_HINT : '',
        ),
      );
      $('#pool-cover-alert .upload-alert').scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, '上传封面');
$('#pool-cover-reset').onclick = () => {
  if (!poolEditor.coverPath) return;
  poolEditor.coverPath = '';
  poolChanged();
  renderPoolCover();
};

function renderPoolTrash(rows) {
  $('#pool-trash-summary').textContent = `卡池回收站（${rows.length}）`;
  $('#pool-trash-rows').innerHTML =
    rows
      .map(
        (row) =>
          `<tr><td>${esc(row.config.name)}<span class="sub">组 ${row.base.groupid}</span></td><td>${poolGroupRows(
            row,
          )
            .map((r) => r.gacha_id)
            .join(
              '、',
            )}</td><td>${row.live.revision ? '有发布版' : '从未发布'}${poolGroupRows(row).some(poolHasDraft) ? ' · 有草稿' : ''}</td><td><button class="secondary sm" data-trash-restore="${row.gacha_id}">恢复</button> <button class="danger sm" data-trash-purge="${row.base.groupid}">永久删除…</button></td></tr>`,
      )
      .join('') || emptyRow(4, '回收站为空');
}
$('#pool-trash-rows').onclick = (event) => {
  const restore = event.target.closest('[data-trash-restore]'),
    purge = event.target.closest('[data-trash-purge]');
  if (restore)
    changePoolDeletion(
      'restore',
      poolEditor.rows.find((r) => r.gacha_id === Number(restore.dataset.trashRestore)),
    );
  if (purge) openGachaPurge(Number(purge.dataset.trashPurge));
};
async function changePoolDeletion(action, row = poolEditor.row) {
  if (!row?.custom || policyBusy('pool-editor')) return;
  const ids = poolGroupRows(row)
    .map((r) => r.gacha_id)
    .join('、');
  const [title, detail] =
    action === 'delete'
      ? [
          `将卡池「${row.config.name}」移入回收站？`,
          `同组抽法（${ids}）会立即对玩家隐藏，也不能在「扭蛋发布」开启。\n抽取次数、草稿和历史记录都会保留，可从回收站恢复。`,
        ]
      : [
          `恢复卡池「${row.config.name}」（${ids}）？`,
          '恢复后按原有的配置和排期；若「扭蛋发布」中仍开启此卡池，玩家下次打开扭蛋页即可看到。',
        ];
  if (
    !(await askConfirm(title, detail + (poolEditor.dirty ? '\n\n当前未保存的编辑将放弃。' : ''), {
      ok: action === 'delete' ? '移入回收站' : '恢复',
      danger: action === 'delete',
    }))
  )
    return;
  return runPoolTask(
    async () => {
      try {
        await api(`/api/gacha-pools/${row.gacha_id}/${action}`, {
          method: 'POST',
          body: JSON.stringify({ expected_revision: poolEditor.customRevision }),
        });
      } catch (e) {
        if (e.status === 409) {
          await loadPoolEditor();
          toast('卡池列表刚被其他操作更新，已重新载入；请确认后重试', true);
          return;
        }
        throw e;
      }
      poolEditor.dirty = false;
      $('#pool-config-status').value = action === 'delete' ? 'deleted' : '';
      await loadPoolEditor(row.gacha_id);
      $('#pool-trash-panel').open = action === 'delete';
      state.loaded.delete('gachas');
      state.loaded.delete('audit');
      clearViewAlert('pool-editor');
      toast(action === 'delete' ? `已移入回收站「${row.config.name}」` : `已恢复「${row.config.name}」`);
    },
    action === 'delete' ? '删除卡池' : '恢复卡池',
  );
}
$('#pool-delete').onclick = () => changePoolDeletion('delete');
$('#pool-restore').onclick = () => changePoolDeletion('restore');
// A pending reward job keeps the same immutable payload and keys across
// network errors and page reloads. Retry only unacknowledged recipients.
