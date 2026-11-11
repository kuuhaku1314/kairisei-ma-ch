'use strict';
const cardJobName = (job) =>
  ({ 0: '通用', 1: '佣兵', 2: '富豪', 3: '盗贼', 4: '歌姬', 5: '炼金' })[job ?? 0] || '';
const cardMatchesJob = (card, job) => job === 0 || (card.arthur_type ?? 0) === (job === -1 ? 0 : job);
const cardLimitCount = (card) => (card?.combat ? card.combat.limit_count || 0 : null);
const cardRarityLabel = (rarity, limit = 0) =>
  rarity === 6
    ? `MR${'+'.repeat(Math.max(0, Math.min(2, limit)))} · 6 星`
    : `${rarity} 星${rarity === 7 && limit > 0 ? ' · 因子突破' : ''}`;
const cardStars = (card) => (card.rarity ? cardRarityLabel(card.rarity, cardLimitCount(card) || 0) : '');
function cardStarsHTML(card) {
  const rarity = Math.max(0, Math.min(card.rarity || 0, 8)),
    limit = cardLimitCount(card);
  const factor = rarity === 7,
    marked = (rarity === 6 || factor) && limit > 0;
  return (
    `<span class="stars" role="img" aria-label="${rarity} 星">${'★'.repeat(rarity)}</span>` +
    (marked
      ? `<span class="limit-stars limit-stars-${factor ? 'factor' : limit}" role="img" aria-label="${factor ? '因子突破' : `点星 +${limit}`}">${'★'.repeat(Math.min(limit, 2))}</span>`
      : '')
  );
}
const cardSourceDescription = (card) => cardFacts(card);
const state = {
  loading: new Set(),
  publishing: new Set(),
  status: null,
  accounts: [],
  groups: [],
  policy: null,
  gachaPresets: [],
  gachaPolicy: null,
  gachaSelected: new Set(),
  audit: [],
  selected: new Set(),
  mode: 'all',
  grantUser: null,
  catalog: [],
  catalogKind: 'card',
  catalogJob: 0,
  catalogStampKind: '',
  catalogTotal: 0,
  catalogPage: 0,
  catalogRequest: 0,
  mailReward: null,
  loaded: new Set(),
  currentView: 'dashboard',
  bossCatalog: 'activity',
  bossKind: 'all',
  bossPage: 0,
  towers: [],
};
const titles = {
  missions: '任务与成就',
  'gift-codes': '兑换码',
  maintenance: '维护工具',
  evolution: '卡牌进化链',
  'activity-rewards': '圣剑杯与探索',
  'player-policy': '公告与奖励',
  drops: 'Boss 掉落',
  exchanges: '兑换所配置',
  shop: '道具商店',
  'pool-editor': '卡池配置',
  dashboard: '运行概览',
  accounts: '账号管理',
  mail: '礼物发放',
  bosses: 'Boss 发布',
  gachas: '扭蛋发布',
  audit: '操作审计',
  'runtime-logs': '运行日志',
  settings: '运营设置',
};
const viewMeta = {
  missions: ['玩家', '任务系列、成就链与每日任务：编辑 → 预览 → 发布，每日按 UTC+8 切日'],
  'gift-codes': ['玩家', '礼包批次、兑换码生成与导出、启停和领取记录'],
  evolution: ['扭蛋与商店', '按星级查看进化链，独立控制每条进化方向的开放状态'],
  dashboard: ['概览', '待处理事项、玩家与对局、服务运行，以及内容与部署状态'],
  accounts: ['玩家', '筛选玩家、查看存档、调整资源与绑定，跨页选择收件人'],
  mail: ['玩家', '选择收件人与奖励，预览后按批次发放；中断后可从批次继续'],
  bosses: [
    '战斗与活动',
    '活动／素材、往期 BOSS 与炼金塔各自的开放名单和开放时间，各难度入口规则，以及体力经验与开场设置',
  ],
  drops: ['战斗与活动', '按难度配置怪物／部位掉落与名声奖励，新开战生效'],
  'activity-rewards': ['战斗与活动', '圣剑杯九档固定奖励、回合与倍率，探索逐项概率奖励'],
  'pool-editor': ['扭蛋与商店', '编辑卡池内容与价格 → 保存草稿并预览 → 发布'],
  gachas: ['扭蛋与商店', '常规卡池组的开放开关，保存后即时生效'],
  exchanges: ['扭蛋与商店', '兑换所与商品、价格、限兑；删除可恢复且保留兑换记录'],
  shop: ['扭蛋与商店', '道具、表情与扩容商品的上架、价格、页签和限购'],
  'player-policy': ['系统', '公告、签到、新手毕业邮件、剧情首通与看板购买'],
  settings: ['系统', '游戏规则、登录与访问、游戏功能开关，以及上传图片的来源与同步'],
  maintenance: ['系统', '维护开关，以及需在维护中执行的状态／审计／卡池清理'],
  audit: ['系统', '全部写操作的事务审计记录，可按类型、时间与内容检索'],
  'runtime-logs': ['系统', '本次运行的服务器日志：按级别、接口、状态码、耗时与时间检索，由新到旧'],
};
const $ = (s) => document.querySelector(s),
  $$ = (s) => [...document.querySelectorAll(s)];
const esc = (v) =>
  String(v ?? '').replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
  );
// One shared formatter: toLocaleString builds a new Intl.NumberFormat per call, which stalls large lists.
const numFormat = new Intl.NumberFormat('zh-CN'),
  num = (v) => numFormat.format(Number(v || 0));
// Writes markup only when it differs from this function's last write, so the 15 s dashboard refresh leaves
// unchanged blocks — and the hover, focus and text selection in them — alone. Writers of an element must all use it.
function setMarkup(el, markup) {
  if (el.__markup !== markup) {
    el.__markup = markup;
    el.innerHTML = markup;
  }
}
async function api(path, options = {}) {
  const opt = { ...options, headers: { ...(options.headers || {}) } };
  if (opt.body) {
    opt.headers['Content-Type'] = 'application/json';
    opt.headers['X-Kairisei-Admin-Action'] = 'apply';
  }
  let r;
  try {
    r = await fetch(path, opt);
  } catch {
    const e = new Error('无法连接后台服务：网络中断或服务已停止');
    e.status = 0;
    throw e;
  }
  let data;
  try {
    data = await r.json();
  } catch {
    const e = new Error(`后台返回了无效 JSON（HTTP ${r.status}）`);
    e.status = r.status;
    throw e;
  }
  if (!r.ok || data.state === 'FAIL') {
    const e = new Error(data.error || `HTTP ${r.status}`);
    e.status = r.status;
    throw e;
  }
  return data;
}
function toast(message, error = false) {
  const stack = $('#toast-stack'),
    same = [...stack.children].find((el) => el.querySelector('.toast-text')?.textContent === message);
  if (same) {
    clearTimeout(same.timer);
    same.timer = setTimeout(() => same.remove(), error ? 9000 : 3500);
    return;
  }
  const item = document.createElement('div');
  item.className = 'toast-item' + (error ? ' error' : '');
  item.setAttribute('role', error ? 'alert' : 'status');
  item.innerHTML = `<svg class="icon" aria-hidden="true"><use href="#i-${error ? 'alert' : 'check'}"/></svg><span class="toast-text"></span><button class="x-button" type="button" aria-label="关闭提示"><svg class="icon"><use href="#i-close"/></svg></button>`;
  item.querySelector('.toast-text').textContent = message;
  const close = () => item.remove();
  item.querySelector('button').onclick = close;
  stack.append(item);
  while (stack.children.length > 3) stack.firstElementChild.remove();
  item.timer = setTimeout(close, error ? 9000 : 3500);
}
const conflictHint = '配置已被其他页面或操作更新（版本冲突）。当前草稿仍保留在页面上，不会被强制覆盖。';
function errorText(e) {
  return e?.status === 409 ? conflictHint : e?.status === 0 ? e.message : e?.message || String(e);
}
const draftExporters = {
  'activity-rewards': '#activity-export',
  drops: '#drop-export',
  exchanges: '#exchange-export',
  'pool-editor': '#pool-export',
};
function viewAlert(view) {
  return $(`#${view} > .view-alert`);
}
function clearViewAlert(view) {
  const el = viewAlert(view);
  if (el) {
    el.hidden = true;
    el.innerHTML = '';
  }
}
function showViewAlert(view, { kind = 'error', title, message, actions = [] }) {
  const el = viewAlert(view);
  if (!el) return;
  el.className = 'view-alert' + (kind === 'error' ? '' : ' ' + kind);
  el.hidden = false;
  el.innerHTML = `<svg class="icon" aria-hidden="true"><use href="#i-${kind === 'info' ? 'info' : 'alert'}"/></svg><div class="alert-text"><b></b><span></span></div><div class="alert-actions"></div><button class="x-button" type="button" aria-label="关闭提示"><svg class="icon"><use href="#i-close"/></svg></button>`;
  el.querySelector('b').textContent = title;
  el.querySelector('.alert-text span').textContent = message || '';
  for (const a of actions) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = (a.primary ? 'primary' : 'secondary') + ' sm';
    b.textContent = a.label;
    b.onclick = a.onClick;
    el.querySelector('.alert-actions').append(b);
  }
  el.querySelector('.x-button').onclick = () => clearViewAlert(view);
  // Bring the problem into view: the first highlighted field, otherwise the alert itself.
  if (view === state.currentView) {
    const target = $(`#${view} .invalid`) || el,
      r = target.getBoundingClientRect();
    if (r.top < 80 || r.bottom > innerHeight - 70)
      target.scrollIntoView({ block: target === el ? 'start' : 'center', behavior: 'smooth' });
  }
}
// Save/load failures stay next to the work; 409 never becomes a forced overwrite.
function reportError(view, e, what = '保存') {
  // The inline alert carries the message on the visible page; an extra toast would cover its action buttons.
  if (!view || !viewAlert(view) || view !== state.currentView) toast(errorText(e), true);
  if (!view || !viewAlert(view)) return;
  if (e?.status === 409) {
    const actions = [];
    if (draftExporters[view])
      actions.push({ label: '导出当前草稿', onClick: () => $(draftExporters[view]).click() });
    actions.push({ label: '重新载入最新版本', primary: true, onClick: () => loadView(view, true) });
    showViewAlert(view, {
      kind: 'warn',
      title: `${what}未完成：版本冲突`,
      message:
        conflictHint +
        (draftExporters[view] ? '可先导出草稿留底，' : '请记下需要的修改，') +
        '再重新载入最新版本后重做修改。',
      actions,
    });
  } else
    showViewAlert(view, {
      title: `${what}失败`,
      message: errorText(e) + (e?.status === 0 ? '；恢复连接后可重试。' : ''),
      actions: what === '载入' ? [{ label: '重试', primary: true, onClick: () => loadView(view, true) }] : [],
    });
}
function setSaveState(id, text, kind = '') {
  const el = $(id);
  if (!el) return;
  el.textContent = text;
  el.className = kind ? `state-${kind}` : '';
}
function emptyRow(cols, title, hint = '') {
  return `<tr><td colspan="${cols}" class="empty"><b>${esc(title)}</b>${hint ? esc(hint) : ''}</td></tr>`;
}
function markInvalid(el, message) {
  if (!el) return;
  el.classList.add('invalid');
  el.setAttribute('aria-invalid', 'true');
  if (message) el.title = message;
}
function clearInvalid(root) {
  root.querySelectorAll('.invalid').forEach((el) => {
    el.classList.remove('invalid');
    el.removeAttribute('aria-invalid');
    el.removeAttribute('title');
  });
}
// Loads every catalog page for the given filter; pages after the first run in parallel (server limit is 200 per page).
async function fetchAllCatalog(params = {}) {
  const size = 200,
    query = (o) => '/api/catalog?' + new URLSearchParams({ ...params, limit: size, offset: o });
  const first = await api(query(0)),
    rows = [...first.entries],
    offsets = [];
  for (let o = size; o < first.total; o += size) offsets.push(o);
  const pages = new Array(offsets.length);
  let next = 0;
  await Promise.all(
    Array.from({ length: Math.min(6, offsets.length) }, async () => {
      while (next < offsets.length) {
        const i = next++;
        pages[i] = (await api(query(offsets[i]))).entries;
      }
    }),
  );
  for (const page of pages) rows.push(...page);
  if (rows.length !== first.total) throw new Error('目录分页读取不完整，请刷新重试');
  return rows;
}
// Structured before/after review shown before a save. Resolves true only when the operator confirms.
function reviewChanges({ title, lead = '', sections = [], note = '', confirmText = '确认保存' }) {
  const shown = sections.filter((s) => s.rows.length),
    total = shown.reduce((n, s) => n + s.rows.length, 0);
  const cell = (v) => (v === null || v === undefined || v === '' ? '<span class="none">—</span>' : esc(v));
  $('#review-title').textContent = title;
  $('#review-lead').textContent = lead || (total ? `共 ${total} 处变化，请逐项核对。` : '');
  // A section with summary:true lists [label, value] rows for something that has no previous version.
  $('#review-body').innerHTML =
    shown
      .map(
        (s) =>
          `<section class="review-section"><h3>${esc(s.title)}<span class="tag">${s.rows.length} 项</span></h3><div class="table-wrap"><table class="diff"><thead><tr><th>项目</th>${s.summary ? '<th>内容</th>' : '<th>修改前</th><th>修改后</th>'}</tr></thead><tbody>${s.rows
            .slice(0, 200)
            .map(([label, before, after]) =>
              s.summary
                ? `<tr><td>${esc(label)}</td><td class="after">${cell(before)}</td></tr>`
                : `<tr><td>${esc(label)}</td><td class="before">${cell(before)}</td><td class="after">${cell(after)}</td></tr>`,
            )
            .join(
              '',
            )}${s.rows.length > 200 ? `<tr><td colspan="${s.summary ? 2 : 3}" class="hint">另有 ${s.rows.length - 200} 项未列出</td></tr>` : ''}</tbody></table></div></section>`,
      )
      .join('') || '<p class="review-empty">未检测到内容变化；仍可保存以刷新版本。</p>';
  $('#review-note').textContent = note;
  $('#review-confirm').textContent = confirmText;
  $('#review-confirm').disabled = false;
  $('#review-modal').classList.add('open');
  return new Promise((resolve) => {
    const done = (ok) => {
      $('#review-confirm').onclick = $('#review-cancel').onclick = null;
      $('#review-modal').classList.remove('open');
      resolve(ok);
    };
    $('#review-confirm').onclick = () => done(true);
    $('#review-cancel').onclick = () => done(false);
  });
}
// In-page replacement for confirm()/prompt(): the confirm button names the action, danger marks a destructive one.
// Resolves true (askText: the trimmed text) on confirm, false (null) on 取消 or Esc. Enter confirms only safe actions.
let askDone = null;
function askDialog(title, message, { ok = '确定', danger = false, input = null } = {}) {
  askDone?.(false);
  const text = $('#ask-message'),
    field = $('#ask-field'),
    box = $('#ask-input'),
    yes = $('#ask-confirm');
  $('#ask-title').textContent = title;
  text.textContent = message || '';
  text.hidden = !message;
  field.hidden = !input;
  $('#ask-modal .dialog-body').hidden = !message && !input;
  if (input) {
    $('#ask-label').textContent = input.label;
    box.value = input.value || '';
    box.maxLength = input.maxLength || 40;
  }
  yes.textContent = ok;
  yes.className = danger ? 'danger' : 'primary';
  yes.disabled = !!input && !box.value.trim();
  yes.toggleAttribute('data-autofocus', !danger);
  $('#ask-modal').classList.add('open');
  return new Promise((resolve) => {
    askDone = (confirmed) => {
      askDone = null;
      $('#ask-modal').classList.remove('open');
      resolve(input ? (confirmed ? box.value.trim() : null) : confirmed);
    };
  });
}
const askConfirm = (title, message = '', options = {}) => askDialog(title, message, options);
const askText = (title, { message = '', ...input } = {}, options = {}) =>
  askDialog(title, message, { ...options, input });
$('#ask-confirm').onclick = () => askDone?.(true);
$('#ask-cancel').onclick = () => askDone?.(false);
$('#ask-input').oninput = () => {
  $('#ask-confirm').disabled = !$('#ask-input').value.trim();
};
$('#ask-input').onkeydown = (e) => {
  if (e.key === 'Enter' && !e.isComposing && !$('#ask-confirm').disabled) {
    e.preventDefault();
    askDone?.(true);
  }
};
const diffText = (v) =>
  v === true ? '是' : v === false ? '否' : v === null || v === undefined ? '' : String(v);
function diffRows(pairs) {
  return pairs
    .filter(([, a, b]) => diffText(a) !== diffText(b))
    .map(([label, a, b]) => [label, diffText(a), diffText(b)]);
}
// Items matched by identity (repeats counted), so a quantity or probability edit reads as one changed row.
function keyedDiff(label, before, after, key, text, name = key) {
  const index = (list) => {
    const seen = new Map();
    return new Map(
      list.map((x) => {
        const k = key(x),
          n = (seen.get(k) || 0) + 1;
        seen.set(k, n);
        return [`${k}#${n}`, x];
      }),
    );
  };
  const a = index(before),
    b = index(after),
    rows = [];
  for (const [k, x] of a) {
    const y = b.get(k);
    if (!y) rows.push([`${label}（移除）`, text(x), '']);
    else if (text(x) !== text(y)) rows.push([`${label}「${name(y)}」`, text(x), text(y)]);
  }
  for (const [k, y] of b) if (!a.has(k)) rows.push([`${label}（新增）`, '', text(y)]);
  return rows;
}

async function switchView(name) {
  if (!titles[name]) name = 'dashboard';
  state.currentView = name;
  $$('.view').forEach((v) => v.classList.toggle('active', v.id === name));
  $$('.nav button').forEach((v) => {
    const on = v.dataset.view === name;
    v.classList.toggle('active', on);
    on ? v.setAttribute('aria-current', 'page') : v.removeAttribute('aria-current');
  });
  $('#view-title').textContent = titles[name] || name;
  const [group, desc] = viewMeta[name] || ['', ''];
  $('#view-group').textContent = group;
  $('#view-desc').textContent = desc;
  document.title = `${titles[name]} · Kairisei MA 本地运营后台`;
  try {
    history.replaceState(null, '', '#' + name);
  } catch {
    /* Deep links are optional. */
  }
  closeNavDrawer();
  window.scrollTo(0, 0);
  updatePolicyControls();
  await loadView(name);
}
async function loadView(name, force = false) {
  if (policyBusy(name) || (state.loaded.has(name) && !force)) return;
  if (
    force &&
    !(await confirmDiscard(adminPolicyDirty(name), '刷新会放弃当前页面未保存的修改。', '放弃并刷新'))
  )
    return;
  state.loading.add(name);
  updatePolicyControls();
  try {
    if (name === 'missions') await loadMissionSeries();
    else if (name === 'gift-codes') await loadGiftBatches();
    else if (name === 'evolution') await loadEvolutionEditor();
    else if (name === 'activity-rewards') await loadActivityRewards();
    else if (name === 'player-policy') await loadPlayerPolicy();
    else if (name === 'dashboard') {
      [state.status, state.towerPolicy] = await Promise.all([api('/api/status'), towerPolicy()]);
      renderDashboard();
      loadDashboardTodos();
    } else if (name === 'accounts') await loadAccountsPage();
    else if (name === 'mail') await loadMailWorkspace();
    else if (name === 'bosses') await loadBossPublication(state.bossCatalog);
    else if (name === 'gachas') {
      const [presets, policy] = await Promise.all([api('/api/gacha-presets'), api('/api/gacha-policy')]);
      state.gachaPresets = presets.presets;
      state.gachaPolicy = policy.publication;
      state.gachaSelected = new Set(state.gachaPolicy.group_ids || []);
      renderGachas();
    } else if (name === 'pool-editor') await loadPoolEditor();
    else if (name === 'audit') await loadAuditPage();
    else if (name === 'runtime-logs') await loadRuntimeLogs();
    else if (name === 'settings') await loadRuntimeSettings();
    else if (name === 'shop') await loadItemShop();
    else if (name === 'maintenance') await loadMaintenance();
    else if (name === 'drops') await loadDropEditor();
    else if (name === 'exchanges') await loadExchangeEditor();
    state.loaded.add(name);
    clearViewAlert(name);
    if (force) toast('当前页面已刷新');
  } catch (e) {
    reportError(name, e, '载入');
  } finally {
    state.loading.delete(name);
    updatePolicyControls();
  }
}
// Maintenance pauses the game for every player: keep it visible on every page, not only on its own tool page.
let maintenanceBannerRevision = -1;
function setMaintenanceBanner(m) {
  if (!m) return;
  const revision = Number(m.revision || 0);
  if (revision < maintenanceBannerRevision) return;
  maintenanceBannerRevision = revision;
  const on = !!m.enabled;
  $('#maintenance-banner').hidden = !on;
  $('#nav-maintenance-flag').hidden = !on;
  document.body.classList.toggle('maintenance-on', on);
}
// 运行概览 reads top to bottom: status strip, 待处理, 玩家与对局, 服务运行, 内容与部署.
function renderDashboard() {
  const s = state.status;
  setMaintenanceBanner(s.maintenance);
  $('#m-state').textContent = s.state === 'PASS' ? '服务运行正常' : `服务状态：${s.state}`;
  $('#ov-state').classList.toggle('bad', s.state !== 'PASS');
  $('#m-endpoint').textContent = s.game_endpoint;
  $('#ov-updated').textContent = formatTime(s.server_time_utc);
  $('#m-accounts').textContent = num(s.account_count);
  setMarkup(
    $('#deployment-facts'),
    [
      ['客户端 Profile', s.client_profile],
      ['游戏 API', s.game_endpoint],
      ['BattleSv 端口', s.battle_port],
      ['SQLite schema', s.sqlite_schema_version],
      ['服务端 UTC', s.server_time_utc],
    ]
      .map(([a, b]) => `<div class="fact"><span>${esc(a)}</span><code>${esc(b)}</code></div>`)
      .join(''),
  );
  renderPolicyFacts();
  renderCreationRejections();
  renderProcessCPU();
  renderNetwork();
  renderDatabase();
  renderGoRuntime();
  renderInsights();
}
// 概览 › 新账号创建拦截: process-lifetime counts from /api/status, hidden when the server reports none.
const creationRejectionKinds = [
  ['暂停创建时', 'account_creation_disabled', '每日上限设为 0'],
  ['达到每日上限', 'account_creation_daily_limit', '同一 IP 当天已满'],
  ['尝试过于频繁', 'account_creation_rate_limited', '同一 IP 短时过多'],
];
function renderCreationRejections() {
  const counts = state.status?.account_creation_rejections,
    panel = $('#creation-rejections');
  panel.hidden = !counts;
  if (!counts) return;
  const values = creationRejectionKinds.map(([, key]) => Number(counts[key]) || 0),
    total = values.reduce((a, b) => a + b, 0);
  setMarkup(
    $('#creation-rejection-metrics'),
    creationRejectionKinds
      .map(
        ([label, , sub], i) =>
          `<div class="metric"><span>${label}</span><strong>${num(values[i])}</strong><em>${sub}</em></div>`,
      )
      .join(''),
  );
  const pill = $('#creation-rejections-total');
  pill.textContent = total ? `共 ${num(total)} 次` : '暂无拦截';
  pill.className = 'pill' + (total ? ' gold' : '');
}
// 概览 › 服务运行 counters are cumulative since the process started, sampled at go_runtime.sampled_unix_ms.
// Rates compare two refreshes at least 5 s apart, so two loads in quick succession do not show a noisy figure.
// A new process, a missing value or a counter that went down starts a new baseline and shows "—", never a false
// zero; after the tab was hidden the figure is the average over the real gap, and its label says how long.
const counterBases = new Map();
function counterDeltas(key, r, values) {
  const base = counterBases.get(key),
    at = r.sampled_unix_ms,
    process = r.process_started_unix_ms,
    seconds = base ? (at - base.at) / 1000 : 0;
  if (
    !base ||
    base.process !== process ||
    seconds < 0 ||
    values.some((v, i) => v == null || base.values[i] == null || v < base.values[i])
  ) {
    counterBases.set(key, { at, process, values, deltas: null });
    return null;
  }
  if (seconds < 5) return base.deltas;
  const deltas = { values: values.map((v, i) => v - base.values[i]), seconds };
  counterBases.set(key, { at, process, values, deltas });
  return deltas;
}
const goSpanText = (s) =>
  s < 3600
    ? `${Math.max(1, Math.round(s / 60))} 分钟`
    : s < 86400
      ? `${Math.floor(s / 3600)} 小时 ${Math.floor((s % 3600) / 60)} 分`
      : `${Math.floor(s / 86400)} 天 ${Math.floor((s % 86400) / 3600)} 小时`;
const goAgoText = (s) =>
  s < 60
    ? `${Math.max(0, Math.round(s))} 秒前`
    : s < 3600
      ? `${Math.round(s / 60)} 分钟前`
      : `${Math.round(s / 3600)} 小时前`;
const goPauseText = (ns) =>
  ns < 1e6 ? `${Math.max(1, Math.round(ns / 1e3))} µs` : `${(ns / 1e6).toFixed(1)} ms`;
const windowText = (s) => (s < 90 ? `${Math.round(s)} 秒` : goSpanText(s));
const percentText = (p) => `${p < 10 ? p.toFixed(1) : Math.round(p)}%`;
const rateText = (b) => `${bytesText(b < 1024 ? Math.round(b) : b)}/s`;
const ovTile = ([label, value, sub, tone]) =>
  `<div class="metric${tone ? ' is-' + tone : ''}"><span>${esc(label)}</span><strong>${esc(value)}</strong>${sub ? `<em>${esc(sub)}</em>` : ''}</div>`;
// 概览 › 进程 CPU: user + kernel time of every thread; 100% is one logical core, so a busy process can exceed it.
function renderProcessCPU() {
  const r = state.status?.go_runtime,
    panel = $('#process-cpu');
  panel.hidden = !r || !('process_cpu_seconds_total' in r);
  if (panel.hidden) return;
  const cpu = r.process_cpu_seconds_total,
    cores = r.logical_cpus;
  $('#process-cpu-cores').textContent = cores ? `${cores} 个逻辑核` : '';
  if (cpu == null) {
    counterBases.delete('cpu');
    setMarkup($('#process-cpu-metrics'), '<p class="hint">这个平台读不到进程 CPU 时间。</p>');
    return;
  }
  const d = counterDeltas('cpu', r, [cpu]),
    busy = d && (d.values[0] / d.seconds) * 100,
    average = r.uptime_seconds > 0 ? (cpu / r.uptime_seconds) * 100 : null;
  setMarkup(
    $('#process-cpu-metrics'),
    [
      ['当前占用', d ? percentText(busy) : '—', d ? `最近 ${windowText(d.seconds)}平均` : '下次刷新后显示'],
      [
        '占全机',
        d && cores ? percentText(busy / cores) : '—',
        cores ? `按 ${cores} 个逻辑核折算` : '读不到逻辑核数',
      ],
      [
        '启动以来平均',
        average == null ? '—' : percentText(average),
        `累计 CPU 时间 ${cpu < 60 ? `${cpu.toFixed(1)} 秒` : goSpanText(cpu)}`,
      ],
    ]
      .map(ovTile)
      .join(''),
  );
}
// 概览 › 网络流量: bytes read and written on the three listening ports, from the server's side. A port an older
// server does not report shows "—" rather than zero, and the total then stays unknown too.
const networkPorts = [
  ['http', '游戏 HTTP', '接口与资源下载'],
  ['battle_tcp', '战斗 TCP', '组队对战'],
  ['admin', 'Admin 后台', '含本页刷新'],
];
function renderNetwork() {
  const s = state.status,
    n = s?.network,
    r = s?.go_runtime,
    panel = $('#network-traffic');
  panel.hidden = !n || !r;
  if (panel.hidden) return;
  const rows = networkPorts.map(([key, label, sub]) => {
    const p = n[key] || {},
      totals = [p.received_bytes_total, p.sent_bytes_total];
    return { label, sub, totals, d: counterDeltas('net-' + key, r, totals) };
  });
  const known = rows.every((row) => row.totals.every((v) => v != null)),
    timed = rows.every((row) => row.d);
  rows.push({
    label: '合计',
    total: true,
    totals: [0, 1].map((i) => (known ? rows.reduce((a, row) => a + row.totals[i], 0) : null)),
    d: timed
      ? {
          values: [0, 1].map((i) => rows.reduce((a, row) => a + row.d.values[i], 0)),
          seconds: rows[0].d.seconds,
        }
      : null,
  });
  const cell = (v) => `<td class="num">${v}</td>`;
  setMarkup(
    $('#network-rows'),
    rows
      .map(
        ({ label, sub, total, totals, d }) =>
          `<tr${total ? ' class="total"' : ''}><td>${esc(label)}${sub ? `<span class="sub">${esc(sub)}</span>` : ''}</td>${[0, 1].map((i) => cell(d ? rateText(d.values[i] / d.seconds) : '—')).join('')}${totals.map((v) => cell(v == null ? '—' : bytesText(v))).join('')}</tr>`,
      )
      .join(''),
  );
  const d = rows.find((row) => row.d)?.d;
  $('#network-window').textContent = d ? `速率为最近 ${windowText(d.seconds)}平均` : '速率下次刷新后显示';
}
// 概览 › 数据库: the save database's connection pools and file sizes. Waits are connection acquisition (the
// writer has a single connection), not SQL time; the interval shows how many waits happened since the last figure.
function renderDatabase() {
  const s = state.status,
    db = s?.database,
    r = s?.go_runtime,
    panel = $('#database-stats');
  panel.hidden = !db;
  if (!db) return;
  const pool = (label, p) =>
    p.initialized
      ? [
          label,
          `${num(p.in_use)} / ${num(p.max_open_connections)}`,
          `已打开 ${num(p.open_connections)} · 空闲 ${num(p.idle)}`,
        ]
      : [label, '未建立', `上限 ${num(p.max_open_connections)} · 还没有请求用到`];
  let waitWindow = null;
  const waits = (label, key, p) => {
    const d = r && p.initialized ? counterDeltas(key, r, [p.wait_count, p.wait_duration_ns]) : null;
    if (!d) return [label, '—', p.initialized ? `启动以来 ${num(p.wait_count)} 次` : '连接尚未建立'];
    const [count, ns] = d.values;
    waitWindow = d.seconds;
    return [label, `${num(count)} 次`, count ? `平均每次 ${goPauseText(ns / count)}` : '没有排队'];
  };
  const f = db.files,
    size = (v) => (v == null ? '读不到' : bytesText(v));
  setMarkup(
    $('#database-metrics'),
    [
      pool('写入连接', db.writer),
      waits('写入排队', 'db-writer', db.writer),
      pool('读取连接', db.reader),
      waits('读取排队', 'db-reader', db.reader),
      ['数据库文件', size(f.database_bytes), '主库，不含 WAL'],
      ['WAL 文件', size(f.wal_bytes), `共享内存 ${size(f.shared_memory_bytes)}`],
    ]
      .map(ovTile)
      .join(''),
  );
  const parts = [f.database_bytes, f.wal_bytes, f.shared_memory_bytes];
  $('#database-size').textContent = [
    waitWindow ? `排队为最近 ${windowText(waitWindow)}` : '排队增量下次刷新后显示',
    parts.every((v) => v != null) && `文件共 ${bytesText(parts.reduce((a, b) => a + b, 0))}`,
  ]
    .filter(Boolean)
    .join(' · ');
}
// 概览 › Go 运行时: this server process, one snapshot per refresh.
function renderGoRuntime() {
  const r = state.status?.go_runtime,
    panel = $('#go-runtime');
  panel.hidden = !r;
  if (!r) return;
  const alloc = counterDeltas('alloc', r, [r.alloc_bytes_total]);
  const gc = r.last_gc_unix_ms
    ? `最近一次 ${goAgoText((r.sampled_unix_ms - r.last_gc_unix_ms) / 1000)}${r.last_gc_pause_ns ? ` · 停顿 ${goPauseText(r.last_gc_pause_ns)}` : ''}`
    : '启动后尚未 GC';
  const tiles = [
    ['Goroutine', num(r.goroutines), `GOMAXPROCS ${r.gomaxprocs}`],
    ['堆对象内存', bytesText(r.heap_live_bytes), `下次 GC 目标 ${bytesText(r.heap_goal_bytes)}`],
    ['Go 占用内存', bytesText(r.memory_bytes), '不含已归还系统的部分'],
    ['GC 次数', num(r.gc_cycles), gc],
    [
      '内存分配速率',
      alloc ? rateText(alloc.values[0] / alloc.seconds) : '—',
      alloc ? `最近 ${windowText(alloc.seconds)}平均` : '下次刷新后显示',
    ],
    [
      '运行时长',
      goSpanText(r.uptime_seconds),
      `启动于 ${formatTime(r.sampled_unix_ms - r.uptime_seconds * 1000)}`,
    ],
  ];
  setMarkup($('#go-runtime-metrics'), tiles.map(ovTile).join(''));
  $('#go-runtime-note').textContent =
    `启动以来 GC 约占 CPU ${(r.gc_cpu_fraction * 100).toFixed(r.gc_cpu_fraction < 0.1 ? 1 : 0)}%。读数来自 Go runtime/metrics，读取时不暂停服务；统计的是运行本后台的服务进程。`;
  $('#go-runtime-updated').textContent = `${r.go_version} · ${r.platform}`;
}
// 活动／素材 and 炼金塔 are separate publication catalogs, each with its own mode, list and open window. The
// tower catalog is read together with the status (state.towerPolicy), so each line is written once per refresh.
const towerPolicy = () => api('/api/boss-policy?catalog=tower').catch(() => state.towerPolicy);
function renderPolicyFacts() {
  const s = state.status,
    p = s.publication,
    now = Date.parse(s.server_time_utc) / 1000,
    { publication: tp, towers } = state.towerPolicy || {};
  // [text, closed now] for one catalog's open window.
  const openTime = (c) => {
    const span =
      c.start_unix || c.end_unix
        ? `${c.start_unix ? describeTime(c.start_unix) : '立即'} → ${c.end_unix ? describeTime(c.end_unix) : '不限'}`
        : '未设置，一直开放';
    return now < (c.start_unix || 0)
      ? [`未到开放时间：${span}`, true]
      : c.end_unix && now >= c.end_unix
        ? [`开放时间已过：${span}`, true]
        : [`开放时间：${span}`, false];
  };
  const fact = (label, value, lines) =>
    `<div class="fact"><span>${label}</span><div class="fact-value"><b>${esc(value)}</b>${lines
      .filter((l) => l[0])
      .map(([text, warn]) => `<small class="${warn ? 'warn' : ''}">${esc(text)}</small>`)
      .join('')}</div></div>`;
  const groupTime = openTime(p),
    towerTime = tp ? openTime(tp) : ['', false],
    off = (t) => (t[1] ? ' · 现在关闭' : '');
  // Names only the smaller side (open or closed), at most three, so nine or more towers stay on short lines.
  let tower = '暂时读不到',
    names = '';
  if (towers && !towers.length) tower = '当前资源没有炼金塔';
  else if (towers) {
    const open = towers.filter((t) => t.operator_enabled),
      closed = towers.filter((t) => !t.operator_enabled),
      [word, few] = closed.length <= open.length ? ['关闭', closed] : ['开放', open];
    tower = !closed.length
      ? `全部开放 · ${towers.length} 座`
      : !open.length
        ? `全部关闭 · ${towers.length} 座`
        : `开放 ${open.length} / ${towers.length} 座`;
    if (closed.length && open.length)
      names = `${word}：${few
        .slice(0, 3)
        .map((t) => t.name)
        .join('、')}${few.length > 3 ? ` 等 ${few.length} 座` : ''}`;
  }
  $('#policy-pill').textContent =
    groupTime[1] && towerTime[1]
      ? '现在都关闭'
      : groupTime[1]
        ? '活动／素材现在关闭'
        : towerTime[1]
          ? '炼金塔现在关闭'
          : p.mode === 'all' && tp?.mode === 'all'
            ? '全部开放'
            : '按名单开放';
  setMarkup(
    $('#policy-facts'),
    fact('BOSS 目录', `${num(s.boss_count)} 个难度`, [[`${num(s.boss_group_count)} 个目录组`, false]]) +
      fact(
        '活动／素材',
        (p.mode === 'all'
          ? `全部开放 · ${num(s.boss_group_count)} 组`
          : `开放 ${num((p.group_ids || []).length)} / ${num(s.boss_group_count)} 组`) + off(groupTime),
        [groupTime],
      ) +
      fact('炼金塔', tower + off(towerTime), [[names, false], towerTime]),
  );
  $('#policy-copy').textContent = '活动／素材、往期 BOSS 和炼金塔在 Boss 发布页分别设置开放方式和开放时间。';
}
// Best-effort operator to-do list; each source fails independently inside the panel.
async function loadDashboardTodos() {
  const box = $('#dashboard-todos'),
    items = [],
    now = Date.now() / 1000,
    soon = now + 3 * 86400,
    when = (unix) => describeTime(unix);
  for (const [view, title] of Object.entries(titles))
    if (view !== 'dashboard' && state.loaded.has(view) && adminPolicyDirty(view))
      items.push({
        text: `「${title}」有未保存的修改`,
        hint: '草稿仍在页面中，保存后才生效',
        tag: ['warn', '草稿'],
        go: () => switchView(view),
      });
  const sources = [
    [
      '发放批次',
      async () => {
        const data = await api('/api/mail-batches?limit=20&offset=0&status=unfinished');
        for (const b of data.batches)
          items.push({
            text: `礼物批次「${b.title}」未完成`,
            hint: `已完成 ${b.done}/${b.recipients} 名玩家 · ${formatTime(b.created_utc)}${data.total > 20 ? ` · 共 ${data.total} 个未完成` : ''}`,
            tag: ['bad', '未完成'],
            go: async () => {
              await switchView('mail');
              await viewMailBatch(b.batch_id);
            },
          });
      },
    ],
    [
      '卡池',
      async () => {
        const data = await api('/api/gacha-editor'),
          pools = new Map();
        for (const row of data.pools)
          pools.set(row.base.groupid, [...(pools.get(row.base.groupid) || []), row]);
        for (const group of pools.values()) {
          const row = group[0],
            c = row.config,
            pending = group.filter(poolPendingDraft);
          if (pending.length)
            items.push({
              text: `卡池「${c.name}」有未发布草稿`,
              hint:
                group.length > 1
                  ? `${pending.length}/${group.length} 种抽法待发布`
                  : `草稿 v${row.draft.revision} · 已发布 v${row.live.revision}`,
              tag: ['warn', '待发布'],
              go: async () => {
                await switchView('pool-editor');
                if (poolEditor.row?.gacha_id !== row.gacha_id && !poolEditor.dirty) {
                  $('#pool-select').value = row.gacha_id;
                  selectPool(row.gacha_id);
                }
              },
            });
          if (c.end_unix > now && c.end_unix < soon)
            items.push({
              text: `卡池「${c.name}」即将结束`,
              hint: `结束于 ${when(c.end_unix)}`,
              tag: ['info', '排期'],
              go: () => switchView('pool-editor'),
            });
          if (c.start_unix > now && c.start_unix < soon)
            items.push({
              text: `卡池「${c.name}」即将开始`,
              hint: `开始于 ${when(c.start_unix)}`,
              tag: ['info', '排期'],
              go: () => switchView('pool-editor'),
            });
        }
      },
    ],
    [
      '兑换所',
      async () => {
        const data = await api('/api/exchanges');
        for (const s of data.shops) {
          if (s.deleted || s.disabled) continue;
          if (s.end_time > now && s.end_time < soon)
            items.push({
              text: `兑换所「${s.name}」即将结束`,
              hint: `结束于 ${when(s.end_time)}`,
              tag: ['info', '排期'],
              go: () => switchView('exchanges'),
            });
          if (s.start_time > now)
            items.push({
              text: `兑换所「${s.name}」尚未开始`,
              hint: `开始于 ${when(s.start_time)}，此前玩家看不到`,
              tag: ['info', '排期'],
              go: () => switchView('exchanges'),
            });
        }
      },
    ],
    [
      '公告',
      async () => {
        const n = (await api('/api/player-policy')).config.notice;
        if (n.enabled && n.end_unix && n.end_unix <= now)
          items.push({
            text: '公告已过结束时间',
            hint: `结束于 ${when(n.end_unix)}；玩家现在看到“暂无公告”`,
            tag: ['warn', '公告'],
            go: () => switchView('player-policy'),
          });
        else if (n.enabled && n.start_unix > now)
          items.push({
            text: '公告尚未开始显示',
            hint: `开始于 ${when(n.start_unix)}`,
            tag: ['info', '公告'],
            go: () => switchView('player-policy'),
          });
      },
    ],
  ];
  await Promise.all(
    sources.map(async ([name, load]) => {
      try {
        await load();
      } catch (e) {
        items.push({ text: `${name}读取失败`, hint: errorText(e), tag: ['bad', '错误'] });
      }
    }),
  );
  const rank = { bad: 0, warn: 1, info: 2 };
  items.sort((a, b) => rank[a.tag[0]] - rank[b.tag[0]]);
  $('#todo-count').textContent = items.length ? `${items.length} 项` : '无';
  $('#todo-count').className =
    'pill ' + (items.some((t) => t.tag[0] !== 'info') ? 'gold' : items.length ? 'blue' : 'green');
  box.innerHTML = items.length
    ? items
        .map(
          (t, i) =>
            `<div class="todo"><span class="tag ${t.tag[0]}">${esc(t.tag[1])}</span><div class="todo-text"><b>${esc(t.text)}</b><span class="hint">${esc(t.hint)}</span></div>${t.go ? `<button class="secondary sm" data-todo="${i}">去处理</button>` : ''}</div>`,
        )
        .join('')
    : '<p class="hint">暂无未完成批次、待发布卡池、即将到期排期或未保存草稿。</p>';
  box.querySelectorAll('[data-todo]').forEach((b) => (b.onclick = () => items[Number(b.dataset.todo)].go()));
}
function renderPager(key, page, total, size, onPage) {
  const pages = Math.max(1, Math.ceil(total / size));
  page = Math.max(0, Math.min(page, pages - 1));
  $(`#${key}-page-info`).textContent = `${page + 1} / ${pages} 页 · 共 ${num(total)} 项`;
  const input = $(`#${key}-page`);
  input.value = page + 1;
  input.max = pages;
  $(`#${key}-prev`).disabled = page === 0;
  $(`#${key}-next`).disabled = page === pages - 1;
  $(`#${key}-prev`).onclick = () => onPage(page - 1);
  $(`#${key}-next`).onclick = () => onPage(page + 1);
  input.onchange = () => {
    const next = Number(input.value);
    if (Number.isInteger(next) && next >= 1 && next <= pages) onPage(next - 1);
    else input.value = page + 1;
  };
  return page;
}
// 活动／素材, 往期 BOSS and 炼金塔 are three publication catalogs, each its own document with its own mode, list
// and open window. The tower catalog lists whole towers (policy.towers); its group_ids are tower IDs.
// The settings tabs after them (体力与经验, 开场设置) are no catalogs: each module registers itself here as
// { name, load, render, save, dirty, ready } and shares only the save bar.
const bossSettingTabs = {};
const bossCatalogName = (catalog) =>
  ({ activity: '活动／素材副本', past: '往期 BOSS', tower: '炼金塔' })[catalog] ||
  bossSettingTabs[catalog]?.name;
function bossSaveReady() {
  const tab = bossSettingTabs[state.bossCatalog];
  return tab ? tab.ready() : !!state.policy;
}
async function loadBossPublication(catalog) {
  const tab = bossSettingTabs[catalog];
  if (tab) {
    await tab.load();
    state.bossCatalog = catalog;
    renderBosses();
    return;
  }
  const [groups, policy] = await Promise.all([
    api('/api/boss-groups?catalog=' + catalog),
    api('/api/boss-policy?catalog=' + catalog),
  ]);
  state.bossCatalog = catalog;
  state.groups = groups.groups;
  state.policy = policy.publication;
  state.mode = state.policy.mode;
  state.selected = new Set(state.policy.group_ids || []);
  state.towers = policy.towers || [];
  $('#boss-start').value = localTimeInput(state.policy.start_unix);
  $('#boss-end').value = localTimeInput(state.policy.end_unix);
  updateBossDifficultyFilter();
  state.bossPage = 0;
  renderBosses();
}
$$('#boss-catalogs button').forEach(
  (b) =>
    (b.onclick = async () => {
      const catalog = b.dataset.catalog;
      if (catalog === state.bossCatalog || policyBusy('bosses')) return;
      if (
        !(await confirmDiscard(adminPolicyDirty('bosses'), '切换会放弃当前页签未保存的修改。', '放弃并切换'))
      )
        return;
      state.loading.add('bosses');
      updatePolicyControls();
      try {
        await loadBossPublication(catalog);
        clearViewAlert('bosses');
      } catch (e) {
        reportError('bosses', e, '载入');
      } finally {
        state.loading.delete('bosses');
        updatePolicyControls();
      }
    }),
);
function filteredGroups() {
  const q = $('#boss-search').value.trim();
  return state.groups.filter(
    (g) =>
      (state.bossKind === 'all' || g.category === state.bossKind) &&
      bossMatchesFilters(g) &&
      matchesWords(
        `${g.name} ${g.past_name || ''} ${g.group_id} ${g.picture_id} ${g.boss_ids.join(' ')} ${(g.difficulties || []).join(' ')}`,
        q,
      ),
  );
}
function retryImage(img) {
  const attempt = Number(img.dataset.retry || 0);
  if (attempt < 2) {
    img.dataset.retry = String(attempt + 1);
    const url = new URL(img.src, location.href);
    url.searchParams.set('_retry', `${attempt + 1}-${Date.now()}`);
    setTimeout(() => (img.src = url.pathname + url.search), 150 * (attempt + 1));
    return;
  }
  img.dataset.failed = 'true';
  img.hidden = true;
}
function image(url, label) {
  return url
    ? `<img src="${esc(url)}" alt="" loading="lazy" data-retry="0" onload="this.dataset.ok=1" onerror="retryImage(this)"><span>${esc((label || '?').slice(0, 1))}</span>`
    : `<span>${esc((label || '?').slice(0, 1))}</span>`;
}
const bossKindLabel = (c) => (c === 'material' ? '素材副本' : c === '3d' ? '3D Boss' : '2D Boss');
state.bossView = (() => {
  try {
    return localStorage.getItem('kairisei-admin-boss-view') || 'list';
  } catch {
    return 'list';
  }
})();
$$('#boss-view button').forEach(
  (b) =>
    (b.onclick = () => {
      state.bossView = b.dataset.mode;
      try {
        localStorage.setItem('kairisei-admin-boss-view', state.bossView);
      } catch {
        /* Per-browser convenience only. */
      }
      state.bossPage = 0;
      renderBosses();
    }),
);
function renderBosses() {
  $$('#boss-catalogs button').forEach((b) =>
    b.classList.toggle('active', b.dataset.catalog === state.bossCatalog),
  );
  const tab = bossSettingTabs[state.bossCatalog];
  $$('#bosses [data-setting-tab]').forEach((p) => (p.hidden = p.dataset.settingTab !== state.bossCatalog));
  $('#boss-publish-panel').hidden = !!tab;
  $('#save-policy').textContent = tab ? '保存设置' : '发布策略';
  if (tab) {
    $('#boss-tower-panel').hidden = $('#boss-groups-panel').hidden = true;
    tab.render();
    updateBossDraftState();
    return;
  }
  // Offer only the kinds this catalog has; 往期 BOSS holds 3D groups only, so its kind filter disappears.
  const kinds = new Set(state.groups.map((g) => g.category));
  $$('#boss-kinds button').forEach(
    (b) => (b.hidden = b.dataset.kind !== 'all' && !kinds.has(b.dataset.kind)),
  );
  $('#boss-kinds').hidden = kinds.size <= 1;
  if (!kinds.has(state.bossKind)) state.bossKind = 'all';
  const name = bossCatalogName(state.bossCatalog);
  $('#boss-publish-scope').textContent = '仅' + name;
  $('#boss-publish-hint').textContent =
    `开放方式、${state.bossCatalog === 'tower' ? '开放的塔' : '名单'}和开放时间只作用于${name}；其他目录各自单独发布。已开始的战斗不受影响。`;
  renderTowers();
  renderBossWindow();
  $('#boss-groups-panel').hidden = state.bossCatalog === 'tower';
  if (state.bossCatalog === 'tower') {
    $('#mode-all').classList.toggle('active', state.mode === 'all');
    $('#mode-selected').classList.toggle('active', state.mode === 'allowlist');
    $('#boss-revision').textContent = `revision ${state.policy?.revision || 0}`;
    $('#save-policy').disabled = policyBusy('bosses') || !state.policy;
    updateBossDraftState();
    return;
  }
  $$('#boss-view button').forEach((b) => b.classList.toggle('active', b.dataset.mode === state.bossView));
  const list = state.bossView === 'list',
    size = list ? 100 : 60;
  const rows = filteredGroups();
  state.bossPage = renderPager('boss', state.bossPage, rows.length, size, (page) => {
    state.bossPage = page;
    renderBosses();
    $('#boss-rows').scrollIntoView({ block: 'start' });
  });
  const visible = rows.slice(state.bossPage * size, (state.bossPage + 1) * size);
  $$('#boss-kinds button').forEach((b) => b.classList.toggle('active', b.dataset.kind === state.bossKind));
  $('#mode-all').classList.toggle('active', state.mode === 'all');
  $('#mode-selected').classList.toggle('active', state.mode === 'allowlist');
  $('#boss-revision').textContent = `revision ${state.policy?.revision || 0}`;
  $('#boss-summary').innerHTML =
    `<b>${state.mode === 'all' ? '全部开放模式（勾选不生效）' : `已选 ${state.selected.size} 组`}</b><span>筛选结果 ${rows.length} / 全部 ${state.groups.length} 组 · 本页 ${visible.length} 组</span>`;
  $('#save-policy').disabled = policyBusy('bosses') || !state.policy;
  updateBossDraftState();
  const check = (g) => {
    const on = state.selected.has(g.group_id);
    return `<input class="check boss-check" type="checkbox" data-id="${g.group_id}" aria-label="选择${esc(g.name)}" title="${state.mode === 'all' ? '全部开放模式下无需勾选' : '勾选即列入开放名单'}" ${on ? 'checked' : ''} ${state.mode === 'all' ? 'disabled' : ''}>`;
  };
  const openTag = (g) => {
    const open = bossIsPublished(g);
    return `<span class="tag ${open ? 'ok' : ''}">${open ? '当前开放' : '当前关闭'}</span>`;
  };
  const ids = (g) => `${esc(g.boss_ids.slice(0, 4).join(', '))}${g.boss_ids.length > 4 ? '…' : ''}`;
  $('#boss-rows').className = list ? 'boss-list-wrap' : 'boss-grid';
  if (!visible.length)
    $('#boss-rows').innerHTML =
      `<div class="empty"><b>没有匹配的 Boss 组</b>调整搜索或点击“清除筛选”。</div>`;
  else if (list)
    $('#boss-rows').innerHTML =
      `<div class="table-wrap"><table class="boss-list"><thead><tr><th class="check-col"><span class="sr-only">选择</span></th><th><span class="sr-only">图片</span></th><th>名称</th><th>类型 / 难度</th><th>组 ID / BOSS ID</th><th>状态</th><th><span class="sr-only">操作</span></th></tr></thead><tbody>${visible.map((g) => `<tr class="${state.selected.has(g.group_id) && state.mode !== 'all' ? 'selected' : ''}"><td>${check(g)}</td><td><div class="thumb">${image(g.image_url, g.name)}</div></td><td class="name-cell"><b>${esc(g.name)}</b>${g.past_name ? `<span class="sub">${esc(g.past_name)}</span>` : ''}</td><td>${bossKindLabel(g.category)} · ${g.bosses?.length || g.boss_count} 个难度 · 最多 ${g.max_segments || 1} 波<span class="sub">${esc((g.difficulties || []).join(' / ') || '未标注难度')}</span></td><td><code>${g.group_id}</code><span class="sub">${ids(g)}</span></td><td>${openTag(g)}</td><td><button class="secondary sm" data-boss-detail="${g.group_id}">难度规则</button></td></tr>`).join('')}</tbody></table></div>`;
  else
    $('#boss-rows').innerHTML = visible
      .map((g) => {
        const on = state.selected.has(g.group_id);
        return `<div class="boss-card ${on && state.mode !== 'all' ? 'selected' : ''}"><label class="boss-select">${check(g)}</label><span class="card-state">${openTag(g)}</span><div class="boss-art">${image(g.image_url, g.name)}</div><div class="boss-copy"><span class="name" title="${esc(g.name)}">${esc(g.name)}</span>${g.past_name ? `<span class="sub">${esc(g.past_name)}</span>` : ''}<span class="sub">${bossKindLabel(g.category)} · ${g.bosses?.length || g.boss_count} 个难度 · 最多 ${g.max_segments || 1} 波</span><span class="sub">${esc((g.difficulties || []).join(' / ') || '未标注难度')}</span><span class="sub"><code>${g.group_id}</code> ${ids(g)}</span><div class="card-actions"><button class="secondary sm" data-boss-detail="${g.group_id}">难度规则与属性</button></div></div></div>`;
      })
      .join('');
  $$('.boss-check').forEach(
    (c) =>
      (c.onchange = () => {
        const id = Number(c.dataset.id);
        c.checked ? state.selected.add(id) : state.selected.delete(id);
        renderBosses();
      }),
  );
}
// Each card states both what players see now (published) and what saving will change.
function gachaOpenState(g) {
  const on = state.gachaSelected.has(g.group_id),
    live = new Set(state.gachaPolicy?.group_ids || []).has(g.group_id);
  if (on === live)
    return on
      ? { key: 'open', text: '开放中', note: '玩家可见' }
      : { key: 'closed', text: '已关闭', note: '玩家不可见' };
  return on
    ? { key: 'opening', text: '保存后开放', note: '当前玩家不可见' }
    : { key: 'closing', text: '保存后关闭', note: '当前玩家可见' };
}
// Category filter: the entries the groups are listed under in game, with counts.
function renderGachaCategoryFilter() {
  const el = $('#gacha-category'),
    value = el.value,
    counts = new Map();
  for (const g of state.gachaPresets || [])
    if (g.category) counts.set(g.category, (counts.get(g.category) || 0) + 1);
  el.innerHTML =
    '<option value="">全部扭蛋分类</option>' +
    [...counts].map(([name, n]) => `<option value="${esc(name)}">${esc(name)}（${n}）</option>`).join('');
  el.value = counts.has(value) ? value : '';
}
// One line per draw count, as the game shows one button per count: payments sharing it are alternatives,
// profession variants collapse into a count, and a closed method stays listed but struck through.
function gachaMethods(g) {
  if (g.unpublished)
    return '<span class="sub warn-text">尚未发布：到「卡池配置」发布后，开放才对玩家生效</span>';
  const lines = new Map();
  for (const m of [...g.methods].sort((a, b) => a.draw_count - b.draw_count)) {
    const friend = m.max_draw_count > m.draw_count,
      draws = friend ? `${m.draw_count}–${m.max_draw_count} 抽` : `${m.draw_count} 抽`,
      cost = `${m.payment} ×${num(m.price)}${friend ? '／抽' : ''}${m.stages ? ` · ${m.stages} 阶段` : ''}${m.daily_first_free ? ' · 每日首抽免费' : ''}`;
    if (!lines.has(draws)) lines.set(draws, new Map());
    const costs = lines.get(draws);
    if (!costs.has(cost)) costs.set(cost, []);
    costs.get(cost).push(m);
  }
  const option = ([cost, list]) => {
    const open = list.filter((m) => !m.closed),
      professions = new Set((open.length ? open : list).map((m) => m.profession)).size,
      title = list.map(
        (m) =>
          `Gacha ${m.gacha_id}${m.profession ? ` · ${poolProfessionName(m.profession)}` : ''}${m.closed ? ' · 已关闭' : ''}`,
      );
    if (list[0].stages) title.push('价格为第 1 阶段；每次成功抽取进入下一阶段，最后阶段重复');
    return `<span class="${open.length ? '' : 'off'}" title="${esc(title.join('\n'))}">${esc(cost)}${professions > 1 ? ` · ${professions} 个职业` : ''}</span>`;
  };
  return `<div class="gacha-methods">${[...lines]
    .map(([draws, costs]) => {
      const off = [...costs.values()].flat().every((m) => m.closed);
      return `<b class="${off ? 'off' : ''}">${draws}</b><span>${[...costs].map(option).join('<i>／</i>')}</span>`;
    })
    .join('')}</div>`;
}
function renderGachas() {
  renderGachaCategoryFilter();
  const rows = filteredGachas(),
    open = state.gachaPresets.filter((g) => state.gachaSelected.has(g.group_id)).length,
    pending = state.gachaPresets.filter((g) => ['opening', 'closing'].includes(gachaOpenState(g).key)).length;
  $('#gacha-revision').textContent = `revision ${state.gachaPolicy?.revision || 0}`;
  $('#gacha-summary').innerHTML =
    `<b>开放 ${open} 个</b><span>关闭 ${state.gachaPresets.length - open} 个${pending ? ` · <span class="state-dirty">${pending} 项待保存</span>` : ''} · 筛选结果 ${rows.length} / ${state.gachaPresets.length}</span>`;
  const dirty = state.gachaPolicy && adminPolicyDirty('gachas');
  setSaveState(
    '#gacha-draft-state',
    dirty ? `有 ${pending} 项未发布的开关修改` : '与已发布设置一致',
    dirty ? 'dirty' : 'ok',
  );
  $('#gacha-rows').innerHTML =
    rows
      .map((g) => {
        const s = gachaOpenState(g),
          on = state.gachaSelected.has(g.group_id);
        return `<div class="gacha-card is-${s.key}">
      <div class="gacha-art">${image(g.image_url, g.name)}<span class="gacha-badge ${s.key}"><i aria-hidden="true"></i>${s.text}</span></div>
      <div class="gacha-copy"><span class="name" title="${esc(g.name)}">${esc(g.name)}${g.publication_key === 'operator' ? ' <span class="tag info">后台新建</span>' : ''}</span>${g.category ? `<span class="gacha-entry" title="游戏扭蛋列表中的入口">${esc(g.category)}</span>` : ''}${gachaMethods(g)}<span class="sub">卡池 ${num(g.card_count)} 张 · <code>Group ${g.group_id}</code></span></div>
      <label class="gacha-toggle"><span class="gacha-toggle-text"><b>对玩家开放</b><small>${s.note}</small></span><span class="switch"><input class="gacha-check" type="checkbox" role="switch" data-id="${g.group_id}" aria-label="对玩家开放 ${esc(g.name)}" ${on ? 'checked' : ''}></span></label>
    </div>`;
      })
      .join('') || `<div class="empty"><b>没有匹配的扭蛋组</b>调整搜索或开放状态筛选。</div>`;
}
$('#gacha-rows').addEventListener('change', (event) => {
  const c = event.target.closest('.gacha-check');
  if (!c) return;
  const id = Number(c.dataset.id);
  c.checked ? state.gachaSelected.add(id) : state.gachaSelected.delete(id);
  renderGachas();
  $(`#gacha-rows .gacha-check[data-id="${id}"]`)?.focus();
});
// 炼金塔: the tower catalog opens whole towers by tower ID (its group_ids), never as Boss groups.
function updateBossDraftState() {
  const setting = !!bossSettingTabs[state.bossCatalog],
    dirty = bossSaveReady() && adminPolicyDirty('bosses');
  const [pending, clean] = setting
    ? ['有未保存的修改', '与已保存设置一致']
    : ['有未发布的修改', '与已发布设置一致'];
  setSaveState('#boss-draft-state', dirty ? pending : clean, dirty ? 'dirty' : 'ok');
  if (typeof updateDraftIndicators === 'function') updateDraftIndicators();
}

/** The window is a whole-catalog gate: outside it every group or tower of this tab is closed. */
function renderBossWindow() {
  const start = unixInput('#boss-start');
  const end = unixInput('#boss-end');
  const now = Date.now() / 1000;
  const scope = bossCatalogName(state.bossCatalog);
  const el = $('#boss-window-state');
  el.className = 'boss-window';
  if (!start && !end) {
    el.textContent = `未设置时间：${scope}按开放方式一直有效。设置后只在这段时间内对玩家开放，时间外全部关闭；游戏内不显示倒计时。`;
    return;
  }
  const range = `${start ? describeTime(start) : '立即'} → ${end ? describeTime(end) : '不限'}`;
  const before = start && now < start;
  const after = end && now >= end;
  el.classList.add(before || after ? 'closed' : 'open');
  el.textContent = before
    ? `${range}：尚未开始，开始前${scope}全部对玩家关闭。`
    : after
      ? `${range}：已结束，${scope}现在全部对玩家关闭。`
      : `${range}：当前在开放时间内，到期后${scope}全部关闭。`;
}

function bossDraftInWindow() {
  const start = unixInput('#boss-start');
  const end = unixInput('#boss-end');
  const now = Date.now() / 1000;
  return (!start || now >= start) && (!end || now < end);
}

/** [text, tone] of a tower for players: open, chosen but outside the open window, or closed. */
function towerPlayerState(open, inWindow) {
  if (!open) return ['关闭', 'muted'];
  return inWindow ? ['开放中', 'ok'] : ['不在开放时间内', 'warn'];
}

function towerRowHTML(tower) {
  const all = state.mode === 'all';
  const on = all || state.selected.has(tower.tower_id);
  const name = tower.name || `炼金塔 ${tower.tower_id}`;
  const now = tower.effective_enabled
    ? ['开放中', 'ok']
    : tower.operator_enabled
      ? ['不在开放时间内', 'warn']
      : ['关闭', 'muted'];
  const next = towerPlayerState(on, bossDraftInWindow());
  const pending = next[0] !== now[0];
  return html`
    <tr class="tower-row ${on ? 'on' : 'off'} ${pending ? 'pending' : ''}">
      <td>
        <div class="tower-name"><b>${name}</b><small>塔 ID ${tower.tower_id}</small></div>
      </td>
      <td class="num">${tower.floor_count}</td>
      <td>
        <div class="tower-status">
          ${stateTag(...now)}
          ${
            pending
              ? html`<span class="tower-next"><span aria-hidden="true">→</span>保存后${stateTag(...next)}</span>`
              : ''
          }
        </div>
      </td>
      <td>
        <label class="switch tower-switch" title="${all ? '全部开放模式下所有塔都开放；要单独关闭，请改为按塔选择' : '开关只改草稿，发布后对玩家生效'}">
          <input type="checkbox" role="switch" data-tower="${tower.tower_id}" ${attr(on, 'checked')}
            ${attr(all, 'disabled')} aria-label="开放 ${name}" />
        </label>
      </td>
      <td class="tower-actions">
        <button class="ghost sm" type="button" data-tower-drops="${tower.tower_id}">掉落与首通</button>
      </td>
    </tr>
  `;
}

function towerScopeHTML(open, total) {
  if (state.mode === 'all') {
    return html`
      <div class="tower-scope">
        <div><b>全部开放</b><span>所有炼金塔都对玩家开放，包括以后资源新增的塔。</span></div>
        <button class="secondary sm" type="button" data-tower-scope="pick">改为按塔选择</button>
      </div>
    `;
  }
  return html`
    <div class="tower-scope pick">
      <div><b>按塔选择</b><span>只开放下面打开开关的塔；以后资源新增的塔默认关闭。</span></div>
      <button class="secondary sm" type="button" data-tower-scope="open" ${attr(open === total, 'disabled')}>
        开放全部塔
      </button>
      <button class="secondary sm" type="button" data-tower-scope="close" ${attr(!open, 'disabled')}>
        关闭全部塔
      </button>
    </div>
  `;
}

function renderTowers() {
  const panel = $('#boss-tower-panel');
  panel.hidden = state.bossCatalog !== 'tower';
  if (panel.hidden) return;
  const towers = state.towers;
  const open =
    state.mode === 'all' ? towers.length : towers.filter((t) => state.selected.has(t.tower_id)).length;
  $('#tower-count').textContent = towers.length ? `开放 ${open} / ${towers.length} 座` : '';
  setHTML($('#tower-scope'), towers.length ? towerScopeHTML(open, towers.length) : '');
  setHTML(
    $('#tower-rows'),
    towers.length
      ? towers.map(towerRowHTML)
      : raw(emptyRow(5, '当前资源没有炼金塔', '服务端未提供可发布的塔定义。')),
  );
}

function setTowerSelection(ids, focus = 0) {
  state.selected = new Set(ids);
  renderTowers();
  updateBossDraftState();
  if (focus) $(`#tower-rows [data-tower="${focus}"]`)?.focus();
}

/** One review per catalog: its list (Boss groups, or whole towers) and its open window. */
function bossPublicationReview(body) {
  const p = state.policy;
  const tower = state.bossCatalog === 'tower';
  const name = bossCatalogName(state.bossCatalog);
  const listText = (mode, ids) =>
    mode === 'all' ? '全部开放' : `仅开放选中的 ${ids.length} ${tower ? '座' : '组'}`;
  const before = new Set(p.group_ids || []);
  const after = new Set(body.group_ids);
  const added = body.group_ids.filter((id) => !before.has(id)).length;
  const removed = [...before].filter((id) => !after.has(id)).length;
  const rows = diffRows([
    ['开放方式', listText(p.mode, p.group_ids || []), listText(body.mode, body.group_ids)],
  ]);
  if (tower) {
    rows.push(
      ...diffRows(
        state.towers.map((t) => [
          `${t.name || '炼金塔'} · ID ${t.tower_id}`,
          p.mode === 'all' || before.has(t.tower_id) ? '开放' : '关闭',
          body.mode === 'all' || after.has(t.tower_id) ? '开放' : '关闭',
        ]),
      ),
    );
  } else if (body.mode === 'allowlist' && p.mode === 'allowlist' && (added || removed)) {
    rows.push(['名单变化', '—', `新增 ${added} 组，移除 ${removed} 组`]);
  }
  return {
    title: `核对${name}发布`,
    sections: [
      { title: name, rows },
      {
        title: '开放时间',
        rows: diffRows([
          ['开始', describeTime(p.start_unix), describeTime(body.start_unix)],
          ['结束', describeTime(p.end_unix), describeTime(body.end_unix)],
        ]),
      },
    ],
    note:
      (p.mode === 'all' && body.mode === 'allowlist'
        ? `改为“仅开放选中项”后，以后资源新增的${tower ? '炼金塔' : ' Boss 组'}不会自动开放。`
        : '') + '保存后即时生效；已开始的战斗和剧情按进入时的规则完成，玩家进度保留。',
    confirmText: '发布策略',
  };
}

$('#tower-rows').addEventListener('change', (event) => {
  const input = event.target.closest('[data-tower]');
  if (!input) return;
  const id = Number(input.dataset.tower);
  const next = new Set(state.selected);
  input.checked ? next.add(id) : next.delete(id);
  setTowerSelection(next, id);
});
$('#tower-rows').addEventListener('click', (event) => {
  const link = event.target.closest('[data-tower-drops]');
  if (link) openTowerDrops(Number(link.dataset.towerDrops));
});
// The scope is this catalog's 开放方式; choosing towers one by one starts with every tower open (setBossMode).
$('#tower-scope').addEventListener('click', (event) => {
  const scope = event.target.closest('[data-tower-scope]')?.dataset.towerScope;
  if (scope === 'pick') {
    setBossMode('allowlist');
    renderBosses();
  } else if (scope) setTowerSelection(scope === 'open' ? state.towers.map((t) => t.tower_id) : []);
});
$$('.nav button').forEach((b) => (b.onclick = () => switchView(b.dataset.view)));
$$('[data-jump]').forEach((b) => (b.onclick = () => switchView(b.dataset.jump)));
$('#refresh').onclick = () => loadView(state.currentView, true);
$('#boss-search').oninput = () => {
  state.bossPage = 0;
  renderBosses();
};
$$('#boss-kinds button').forEach(
  (b) =>
    (b.onclick = () => {
      state.bossKind = b.dataset.kind;
      state.bossPage = 0;
      renderBosses();
    }),
);
// Switching to an explicit list begins with every entry of this catalog open, so nothing closes silently.
function setBossMode(mode) {
  if (mode === 'allowlist' && state.mode === 'all')
    state.selected = new Set(
      state.bossCatalog === 'tower'
        ? state.towers.map((t) => t.tower_id)
        : state.groups.map((g) => g.group_id),
    );
  state.mode = mode;
}
$('#mode-all').onclick = () => {
  setBossMode('all');
  renderBosses();
};
$('#mode-selected').onclick = () => {
  setBossMode('allowlist');
  renderBosses();
};
$('#select-visible').onclick = () => {
  setBossMode('allowlist');
  filteredGroups().forEach((g) => state.selected.add(g.group_id));
  renderBosses();
};
$('#clear-selected').onclick = () => {
  setBossMode('allowlist');
  state.selected.clear();
  renderBosses();
};
$('#boss-start').onchange = $('#boss-end').onchange = () => renderBosses();
function policyBusy(name) {
  return state.loading.has(name) || state.publishing.has(name);
}
function updatePolicyControls() {
  for (const name of [
    'missions',
    'gift-codes',
    'bosses',
    'gachas',
    'pool-editor',
    'settings',
    'shop',
    'drops',
    'exchanges',
    'player-policy',
    'activity-rewards',
    'evolution',
  ])
    $('#' + name).inert = policyBusy(name);
  for (const name of Object.keys(titles)) {
    const busy = policyBusy(name);
    busy ? $('#' + name).setAttribute('aria-busy', 'true') : $('#' + name).removeAttribute('aria-busy');
  }
  document.body.classList.toggle('busy', policyBusy(state.currentView));
  if (typeof updatePoolControls === 'function') updatePoolControls();
  $('#save-policy').disabled = policyBusy('bosses') || !bossSaveReady();
  $('#gacha-save').disabled = policyBusy('gachas') || !state.gachaPolicy;
  $('#refresh').disabled = policyBusy(state.currentView);
  if (typeof updateDraftIndicators === 'function') updateDraftIndicators();
}
$('#save-policy').onclick = async () => {
  if (bossSettingTabs[state.bossCatalog]) return bossSettingTabs[state.bossCatalog].save();
  if (policyBusy('bosses') || !state.policy) return;
  const start = unixInput('#boss-start'),
    end = unixInput('#boss-end');
  clearInvalid($('#bosses'));
  if (start && end && end <= start) {
    markInvalid($('#boss-end'), '结束时间须晚于开始时间');
    showViewAlert('bosses', { title: '不能发布：开放时间无效', message: '结束时间须晚于开始时间。' });
    return;
  }
  if (!adminPolicyDirty('bosses')) return toast('与已发布设置一致，无需发布');
  if (
    state.mode === 'allowlist' &&
    !state.selected.size &&
    !(await askConfirm(
      state.bossCatalog === 'tower' ? '发布后关闭全部炼金塔？' : '发布后关闭本目录的全部内容？',
      state.bossCatalog === 'tower'
        ? '没有开放任何炼金塔，发布后所有塔都对玩家关闭。'
        : '没有选择任何 Boss 组，发布后本目录将全部关闭。',
      { ok: '仍然发布', danger: true },
    ))
  )
    return;
  const body = {
    mode: state.mode,
    group_ids: state.mode === 'all' ? [] : [...state.selected].sort((a, b) => a - b),
    start_unix: start,
    end_unix: end,
    expected_revision: state.policy.revision,
  };
  if (!(await reviewChanges(bossPublicationReview(body)))) return;
  state.publishing.add('bosses');
  updatePolicyControls();
  try {
    const data = await api('/api/boss-policy?catalog=' + state.bossCatalog, {
      method: 'PUT',
      body: JSON.stringify(body),
    });
    state.policy = data.publication;
    state.mode = state.policy.mode;
    state.selected = new Set(state.policy.group_ids || []);
    state.towers = data.towers || [];
    state.loaded.delete('dashboard');
    state.loaded.delete('audit');
    clearViewAlert('bosses');
    renderBosses();
    toast(`${bossCatalogName(state.bossCatalog)}发布设置已保存`);
  } catch (e) {
    reportError('bosses', e, '发布');
  } finally {
    state.publishing.delete('bosses');
    updatePolicyControls();
  }
};
$('#gacha-select-all').onclick = () => {
  state.gachaSelected = new Set(state.gachaPresets.map((g) => g.group_id));
  renderGachas();
};
$('#gacha-clear-all').onclick = () => {
  state.gachaSelected.clear();
  renderGachas();
};
$('#gacha-save').onclick = async () => {
  if (policyBusy('gachas') || !state.gachaPolicy) return;
  const body = {
    group_ids: [...state.gachaSelected].sort((a, b) => a - b),
    expected_revision: state.gachaPolicy.revision,
  };
  const before = new Set(state.gachaPolicy.group_ids || []),
    opened = body.group_ids.filter((id) => !before.has(id)).length,
    closed = [...before].filter((id) => !state.gachaSelected.has(id)).length;
  if (
    !(await askConfirm(
      `即时发布 ${body.group_ids.filter((id) => state.gachaPresets.some((g) => g.group_id === id)).length} 个常规卡池组？`,
      `本次新开放 ${opened} 个、关闭 ${closed} 个；玩家下次请求即生效。`,
      { ok: '发布' },
    ))
  )
    return;
  state.publishing.add('gachas');
  updatePolicyControls();
  try {
    const data = await api('/api/gacha-policy', { method: 'PUT', body: JSON.stringify(body) });
    state.gachaPolicy = data.publication;
    state.gachaSelected = new Set(state.gachaPolicy.group_ids || []);
    if (typeof poolEditor !== 'undefined' && poolEditor.openGroups) {
      poolEditor.openGroups = new Set(state.gachaSelected);
      if (poolEditor.row)
        $('#pool-current .tag-link')?.replaceWith(
          document.createRange().createContextualFragment(poolOpenBadge(poolEditor.row)),
        );
    }
    state.loaded.delete('audit');
    clearViewAlert('gachas');
    renderGachas();
    toast('扭蛋发布策略已即时生效');
  } catch (e) {
    reportError('gachas', e, '发布');
  } finally {
    state.publishing.delete('gachas');
    updatePolicyControls();
  }
};

// Shell: theme, drawer navigation, keyboard and dialog focus handling.
const themeKey = 'kairisei-admin-theme';
function effectiveTheme() {
  return (
    document.documentElement.dataset.theme ||
    (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
  );
}
function renderThemeLabel() {
  $('#theme-label').textContent = effectiveTheme() === 'dark' ? '浅色' : '深色';
  $('#theme-toggle').title = `切换为${$('#theme-label').textContent}模式`;
}
$('#theme-toggle').onclick = () => {
  const next = effectiveTheme() === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  try {
    localStorage.setItem(themeKey, next);
  } catch {
    /* Per-browser convenience only. */
  }
  renderThemeLabel();
};
matchMedia('(prefers-color-scheme: dark)').addEventListener?.('change', renderThemeLabel);
renderThemeLabel();
function closeNavDrawer() {
  document.body.classList.remove('nav-open');
  $('#scrim').hidden = true;
  $('#nav-toggle').setAttribute('aria-expanded', 'false');
}
$('#nav-toggle').onclick = () => {
  const open = !document.body.classList.contains('nav-open');
  document.body.classList.toggle('nav-open', open);
  $('#scrim').hidden = !open;
  $('#nav-toggle').setAttribute('aria-expanded', String(open));
  if (open) $('.nav button.active')?.focus();
};
$('#scrim').onclick = closeNavDrawer;
const focusable =
  'a[href],button:not(:disabled),input:not(:disabled):not([type=hidden]),select:not(:disabled),textarea:not(:disabled),[tabindex]:not([tabindex="-1"])';
const openModal = () => $$('.modal.open').pop();
function isVisible(el) {
  return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
}
for (const modal of $$('.modal')) {
  modal.querySelector('.dialog-x')?.addEventListener('click', () => {
    const d = modal.querySelector('[data-dismiss]');
    if (d && !d.disabled) d.click();
  });
  new MutationObserver(() => {
    const open = modal.classList.contains('open');
    if (open === !!modal.dataset.wasOpen) return;
    if (open) {
      modal.dataset.wasOpen = '1';
      modal.returnFocus = document.activeElement;
      requestAnimationFrame(() => {
        const target =
          [
            ...modal.querySelectorAll(
              '.dialog-body input:not([type=checkbox]):not([type=file]),.dialog-body select,.dialog-body textarea',
            ),
          ].find((el) => !el.disabled && isVisible(el)) ||
          modal.querySelector('[data-autofocus]') ||
          modal.querySelector('[data-dismiss]');
        target?.focus();
      });
    } else {
      delete modal.dataset.wasOpen;
      if (modal.returnFocus?.isConnected) modal.returnFocus.focus();
    }
  }).observe(modal, { attributes: true, attributeFilter: ['class'] });
}
// Border box: the scrolled topbar shrinks by its padding, which a content-box observer never sees.
new ResizeObserver(() =>
  document.documentElement.style.setProperty('--topbar-h', $('.topbar').offsetHeight + 'px'),
).observe($('.topbar'), { box: 'border-box' });
addEventListener(
  'scroll',
  () => {
    const b = document.body;
    if (scrollY > 64) b.classList.add('scrolled');
    else if (scrollY < 8) b.classList.remove('scrolled');
  },
  { passive: true },
);
$('#global-search-pages').innerHTML = Object.entries(titles)
  .map(([, t]) => `<option value="${esc(t)}">`)
  .join('');
$('#global-search').onkeydown = async (e) => {
  if (e.key === 'Escape') {
    e.target.value = '';
    e.target.blur();
    return;
  }
  if (e.key !== 'Enter') return;
  const q = e.target.value.trim();
  if (!q) return;
  e.target.value = '';
  e.target.blur();
  const page =
    Object.entries(titles).find(([, t]) => t === q) ||
    (q.length >= 2 && !/^\d+$/.test(q) ? Object.entries(titles).find(([, t]) => t.includes(q)) : null);
  if (page) return switchView(page[0]);
  $('#account-search').value = q;
  accountPage.page = 0;
  if (state.loaded.has('accounts')) {
    await switchView('accounts');
    await loadAccountsPage().catch((err) => reportError('accounts', err, '载入'));
  } else await switchView('accounts');
  if (accountPage.total === 1 && state.accounts[0]) openAccountDetail(state.accounts[0].user_id);
};
document.addEventListener('keydown', (e) => {
  const modal = openModal();
  if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    if (!modal) {
      const g = $('#global-search');
      if (isVisible(g)) g.focus();
      else $('#nav-toggle').click();
    }
    return;
  }
  if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 's') {
    e.preventDefault();
    if (modal) return;
    const b = $(`#${state.currentView} [data-save-shortcut]`);
    if (b && !b.disabled && !b.closest('[inert]')) b.click();
    else toast('当前页面没有可保存的内容');
    return;
  }
  if (e.key === 'Escape') {
    if (modal) {
      const d = modal.querySelector('[data-dismiss]');
      if (d && !d.disabled) {
        e.preventDefault();
        d.click();
      }
    } else if (document.body.classList.contains('nav-open')) closeNavDrawer();
    return;
  }
  if (e.key === 'Tab' && modal) {
    const items = [...modal.querySelectorAll(focusable)].filter(isVisible);
    if (!items.length) return;
    const first = items[0],
      last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    } else if (!modal.contains(document.activeElement)) {
      e.preventDefault();
      first.focus();
    }
    return;
  }
  if (
    e.key === '/' &&
    !modal &&
    !e.ctrlKey &&
    !e.metaKey &&
    !e.altKey &&
    !/^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement?.tagName || '')
  ) {
    const search = [...$$('.view.active .search')].find(isVisible);
    if (search) {
      e.preventDefault();
      search.focus();
      search.select?.();
    }
  }
});
addEventListener('hashchange', () => {
  const view = decodeURIComponent(location.hash.replace(/^#\/?/, ''));
  if (titles[view] && view !== state.currentView) switchView(view);
});
// The #view fragment names a page, not a scroll target: at load the browser would jump to the view's
// section and hide the top of the page. Undo that unless the operator has already moved.
let userMovedBeforeLoad = false;
for (const type of ['wheel', 'touchstart', 'keydown', 'pointerdown'])
  addEventListener(
    type,
    () => {
      userMovedBeforeLoad = true;
    },
    { once: true, passive: true },
  );
addEventListener(
  'load',
  () => {
    if (!userMovedBeforeLoad && scrollY) scrollTo(0, 0);
  },
  { once: true },
);
document.addEventListener('DOMContentLoaded', () => {
  const initial = decodeURIComponent(location.hash.replace(/^#\/?/, ''));
  if (titles[initial] && initial !== 'dashboard') {
    api('/api/status')
      .then((s) => setMaintenanceBanner(s.maintenance))
      .catch(() => {});
    switchView(initial);
  } else {
    switchView('dashboard');
  }
});
document.addEventListener('keydown', (e) => {
  const label = e.target.closest?.('.file-button');
  if (label && (e.key === 'Enter' || e.key === ' ')) {
    e.preventDefault();
    label.querySelector('input[type=file]:not(:disabled)')?.click();
  }
});
