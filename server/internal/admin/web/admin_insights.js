'use strict';

const attributeLabels = { FIRE: '火', ICE: '冰', WIND: '风', LIGHT: '光', DARK: '暗', NONE: '无' };
const attributeName = (value) =>
  String(value || '')
    .split('_')
    .map((v) => attributeLabels[v] || v)
    .filter(Boolean)
    .join('／') || '未标注';
const matchesWords = (text, query) =>
  query
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .every((word) => String(text).toLowerCase().includes(word));
const attributeOptions =
  '<option value="">全部属性</option>' +
  Object.entries(attributeLabels)
    .filter(([key]) => key !== 'NONE')
    .map(([key, name]) => `<option value="${key}">${name}属性</option>`)
    .join('');
// The source table shares limit_count between MR point stars and MMR factor breakthrough.
const cardRarityOptions =
  '<option value="0">全部星级</option>' +
  Array.from({ length: 8 }, (_, i) => {
    const rarity = i + 1,
      limits = rarity === 6 ? 2 : rarity === 7 ? 1 : 0;
    return (
      `<option value="${rarity}">${cardRarityLabel(rarity)}</option>` +
      Array.from(
        { length: limits },
        (_, n) => `<option value="${rarity}:${n + 1}">${cardRarityLabel(rarity, n + 1)}</option>`,
      ).join('')
    );
  }).join('');
function cardRarityQuery(value) {
  const [rarity, limit = '0'] = String(value || '0').split(':');
  return { rarity: Number(rarity), limit_count: Number(rarity) ? limit : '' };
}

// Search text of a catalog card, kept until its combat or parameter data changes: building it formats
// several numbers, and the pool editor tests 10k+ cards on every render.
const cardSearchTexts = new WeakMap();
function cardSearchText(card) {
  const hit = cardSearchTexts.get(card);
  if (hit && hit[1] === card.combat && hit[2] === card.parameters) return hit[0];
  const text = `${card.name} ${card.reward_type_id} ${card.pict_id} ${cardFacts(card)}`;
  cardSearchTexts.set(card, [text, card.combat, card.parameters]);
  return text;
}
function cardFacts(card) {
  if (card.kind !== 'card') return card.detail || '';
  const c = card.combat,
    p = card.parameters;
  return [
    c ? `${attributeName(c.attribute)} · ${c.cost} COST` : '属性／费用未载入',
    p ? `满级 HP ${num(p.hp)} · 物攻 ${num(p.attack)} · 魔攻 ${num(p.magic)} · 回复 ${num(p.mind)}` : '',
    c?.arthur_skill ? `职业技能：${c.arthur_skill}` : '',
    card.detail || '',
  ]
    .filter(Boolean)
    .join('\n');
}

// Point stars are card.csv limit_count, separate from base rarity and premium rarity.
// Shared by the gift catalog, pool editor and reward picker.
function installCardFilters(prefix, anchor, change) {
  $(`#${prefix}-rarity`).innerHTML = cardRarityOptions;
  const row = document.createElement('div');
  row.className = 'toolbar';
  row.id = prefix + '-advanced';
  row.innerHTML = `<select id="${prefix}-attribute" aria-label="属性">${attributeOptions}</select><select id="${prefix}-cost" aria-label="费用" autocomplete="off"><option value="" selected>全部 COST</option>${Array.from({ length: 11 }, (_, i) => `<option value="${i}">${i} COST</option>`).join('')}</select><select id="${prefix}-resource" aria-label="资源状态"><option value="">全部资源状态</option><option value="available">资源可用</option><option value="unavailable">资源缺失</option></select>`;
  $(anchor).after(row);
  row.querySelectorAll('select').forEach((el) => {
    el.value = '';
    el.onchange = change;
  });
}
function cardFilterQuery(prefix, isCard) {
  const result = cardRarityQuery(isCard ? $(`#${prefix}-rarity`).value : '0');
  for (const key of ['attribute', 'cost', 'resource'])
    result[key] = isCard ? $(`#${prefix}-${key}`).value : '';
  $(`#${prefix}-advanced`).hidden = !isCard;
  return result;
}
function resetCardFilters(prefix) {
  for (const key of ['attribute', 'cost', 'resource']) $(`#${prefix}-${key}`).value = '';
}
function matchesCardFilters(card, prefix) {
  return matchesCardQuery(card, cardFilterQuery(prefix, true));
}
function matchesCardQuery(card, f) {
  return (
    (!f.rarity || (card.rarity === f.rarity && cardLimitCount(card) === Number(f.limit_count))) &&
    (!f.attribute || (card.combat?.attribute || '').split('_').includes(f.attribute)) &&
    (f.cost === '' || card.combat?.cost === Number(f.cost)) &&
    (!f.resource ||
      (f.resource === 'available'
        ? card.resource_state !== 'unavailable'
        : card.resource_state === 'unavailable'))
  );
}
installCardFilters('catalog', '#catalog-rarity', () => {
  state.catalogPage = 0;
  loadCatalog();
});
installCardFilters('pool', '#pool-rarity', () => {
  poolEditor.page = 0;
  renderPoolCards();
  renderSelectedCards();
});
installCardFilters('content', '#content-rarity', () => {
  contentPicker.page = 0;
  loadContentCatalog();
});

// Account filters. Level / job are optional server filters; time presets fill the existing login/created ranges.
function accountFilterQuery() {
  const q = {
    q: $('#account-search').value.trim(),
    include_system: $('#account-system').checked ? '1' : '0',
    binding: $('#account-binding').value,
    sort: $('#account-sort').value,
    active: $('#account-active').checked ? '1' : '0',
  };
  for (const kind of ['created', 'login'])
    for (const side of ['after', 'before']) {
      const value = $(`#account-${kind}-${side}`).value;
      if (value) {
        const stamp = new Date(value);
        if (!Number.isFinite(stamp.getTime())) throw new Error('时间范围无效');
        q[`${kind}_${side}`] = stamp.toISOString();
      }
    }
  const min = $('#account-level-min').value,
    max = $('#account-level-max').value;
  if (min && max && Number(min) > Number(max)) throw new Error('最低等级不能高于最高等级');
  if (min) q.level_min = min;
  if (max) q.level_max = max;
  if ($('#account-job').value) q.arthur_type = $('#account-job').value;
  if ($('#account-login-block').value) q.login_block = $('#account-login-block').value;
  return q;
}
const reloadAccounts = () => {
  accountPage.page = 0;
  clearInvalid($('#accounts'));
  loadAccountsPage().catch((e) => {
    if (/等级|时间/.test(e.message)) {
      markInvalid(/等级/.test(e.message) ? $('#account-level-max') : $('#account-login-before'));
    }
    reportError('accounts', e, '载入');
  });
};
const accountPresets = {
  'login-7': { login_after: 7 },
  'idle-7': { login_before: 7 },
  'idle-30': { login_before: 30 },
  'new-7': { created_after: 7 },
};
function renderAccountPresets() {
  $$('#account-presets button').forEach((b) =>
    b.classList.toggle('active', b.dataset.preset === state.accountPreset),
  );
}
$$('#account-presets button').forEach(
  (b) =>
    (b.onclick = () => {
      const preset = b.dataset.preset,
        active = state.accountPreset === preset;
      for (const id of [
        '#account-created-after',
        '#account-created-before',
        '#account-login-after',
        '#account-login-before',
      ])
        $(id).value = '';
      state.accountPreset = active ? '' : preset;
      if (!active)
        for (const [key, days] of Object.entries(accountPresets[preset])) {
          const [kind, side] = key.split('_');
          $(`#account-${kind}-${side}`).value = localTimeInput(Math.floor(Date.now() / 1000) - days * 86400);
        }
      $('#account-time-range').open = !active;
      renderAccountPresets();
      reloadAccounts();
    }),
);
$$('#account-binding,#account-sort,#account-active,#account-job,#account-login-block').forEach(
  (el) => (el.onchange = reloadAccounts),
);
$$('#account-created-after,#account-created-before,#account-login-after,#account-login-before').forEach(
  (el) =>
    (el.onchange = () => {
      state.accountPreset = '';
      renderAccountPresets();
      reloadAccounts();
    }),
);
let accountLevelTimer;
$$('#account-level-min,#account-level-max').forEach(
  (el) =>
    (el.oninput = () => {
      clearTimeout(accountLevelTimer);
      accountLevelTimer = setTimeout(reloadAccounts, 300);
    }),
);
$('#account-reset').onclick = () => {
  $$('#accounts input:not(.player-check)').forEach((el) => {
    if (el.type === 'checkbox') el.checked = false;
    else if (el.id !== 'accounts-page') el.value = '';
  });
  $('#account-binding').value = '';
  $('#account-login-block').value = '';
  $('#account-job').value = '';
  $('#account-sort').value = 'id';
  state.accountPreset = '';
  renderAccountPresets();
  reloadAccounts();
};

function renderAccountFacts(a) {
  const battle = a.active_boss_id
    ? `BOSS ${a.active_boss_id}${a.active_room_id ? ' · 房间 ' + a.active_room_id : ' · 单人'}（未结算）`
    : a.active_pvp
      ? 'PVP 未结算'
      : '无未结算对局';
  const facts = [
    ['账号类型', a.user_id >= 1900000000 ? '系统伙伴' : a.username ? '已绑定 · ' + a.username : '游客'],
    ['登录状态', typeof loginBlockText === 'function' ? loginBlockText(a.login_block) || '正常' : '—'],
    ['等级 / 职业', `Lv.${a.level} / ${cardJobName(a.active_arthur_type)}`],
    ['经验（总计／下级阈值）', `${num(a.experience)} / ${num(a.next_level_experience)}`],
    ['金币 / 友情点', `${num(a.gold)} / ${num(a.friend_point)}`],
    [
      '水晶',
      `${num(a.free_crystal + a.paid_crystal)}（免费 ${num(a.free_crystal)}／付费 ${num(a.paid_crystal)}）`,
    ],
    ['AP / BP', `${a.ap}/${a.ap_max} · ${a.bp}/${a.bp_max}（存档值）`],
    [
      '背包 / 仓库',
      `${num(a.card_count)}/${num(a.card_max)} · ${num(a.container_cards)}/${num(a.container_max)}`,
    ],
    ['素材 / 道具种类', `${num(a.stack_card_kinds)} / ${num(a.item_kinds)}`],
    ['召唤石 / 传承卡', `${num(a.spheres)} / ${num(a.buddies)}`],
    ['卡组数', num(a.decks)],
    ['未领取邮件', num(a.pending_mail)],
    ['新手训练', a.training_step >= 9 ? '已完成' : `已完成 ${a.training_step}/9 步`],
    ['历史卡组评价 / PVP 点', `${a.arthur_rank} / ${num(a.pvp_point)}`],
    ['看板', `当前 ID ${a.navi_id} · 已解锁 ${a.unlocked_navis}`],
    ['对局存档', battle],
    ['最近业务请求', formatTime(a.recent_activity)],
    ['注册时间', formatTime(a.created_utc)],
    ['最近登录', formatTime(a.last_login_utc)],
    ['最近保存', formatTime(a.updated_utc)],
    ['已解锁功能位', (a.unlocked_features || []).join('、') || '—'],
  ];
  $('#account-detail-body').innerHTML = facts
    .map(([k, v]) => `<div class="fact"><span>${esc(k)}</span><b>${esc(v)}</b></div>`)
    .join('');
  if (typeof accessDetailShown === 'function') accessDetailShown(a);
}

// Dashboard activity & rooms.
// Ten rooms a page keeps the overview's service band in reach; the filter and pager cover the rest.
const roomView = { rows: [], page: 0, size: 10, available: false };
const roomStateLabel = { open: '等待加入', countdown: '倒计时', battle: '战斗中' };
function filteredRooms() {
  return roomView.rows.filter(
    (r) =>
      (!$('#room-state').value || r.state === $('#room-state').value) &&
      matchesWords(`${r.room_id} ${r.boss_id} ${r.players.join(' ')}`, $('#room-search').value),
  );
}
function renderRooms() {
  $('#room-export').disabled = !roomView.available;
  if (!roomView.available) {
    setMarkup($('#room-rows'), emptyRow(5, '活动数据源未接入'));
    $('#room-page').textContent = '';
    $('#room-prev').disabled = $('#room-next').disabled = true;
    return;
  }
  const rows = filteredRooms(),
    pages = Math.max(1, Math.ceil(rows.length / roomView.size));
  roomView.page = Math.min(roomView.page, pages - 1);
  setMarkup(
    $('#room-rows'),
    rows
      .slice(roomView.page * roomView.size, (roomView.page + 1) * roomView.size)
      .map(
        (r) =>
          `<tr><td><code>${r.room_id}</code><span class="sub">BOSS ${r.boss_id}</span></td><td><span class="tag ${r.state === 'battle' ? 'info' : r.state === 'countdown' ? 'warn' : ''}">${esc(roomStateLabel[r.state] || r.state)}</span></td><td>${r.players.map((id) => `<button class="secondary" data-player="${id}">#${id}</button>`).join(' ') || '—'}</td><td class="num">${r.ai} / ${r.disconnected}</td><td>${r.state === 'battle' ? `第 ${r.wave} 波 · 第 ${r.turn} 回合` : '—'} · ${r.game_speed / 100}×</td></tr>`,
      )
      .join('') || emptyRow(5, '暂无匹配房间'),
  );
  $('#room-page').textContent = `${roomView.page + 1} / ${pages} 页 · 共 ${rows.length} 间`;
  $('#room-prev').disabled = roomView.page === 0;
  $('#room-next').disabled = roomView.page === pages - 1;
}
function renderInsights() {
  const s = state.status,
    a = s.activity,
    counts = s.accounts;
  $('#m-account-types').textContent = `已绑定 ${num(counts.bound)} · 游客 ${num(counts.guests)}`;
  if (!a?.available) {
    const off = '<p class="hint">活动数据源未接入</p>';
    setMarkup($('#activity-metrics'), off);
    setMarkup($('#request-metrics'), off);
    roomView.available = false;
    roomView.rows = [];
    roomView.page = 0;
    renderRooms();
    return;
  }
  const tile = ([label, value, tone]) =>
    `<div class="metric${tone ? ' is-' + tone : ''}"><span>${esc(label)}</span><strong>${esc(value)}</strong></div>`;
  setMarkup(
    $('#activity-metrics'),
    [
      ['活跃玩家（估算）', num(a.online_ids.length)],
      ['组队战斗真人', num(a.team_players)],
      ['近期单人未结算', num(a.solo_players)],
      ['战斗中房间', num(a.battle_rooms)],
      ['大厅房间', num(a.lobby_rooms)],
    ]
      .map(tile)
      .join(''),
  );
  const h = a.http,
    minutes = Math.max(1, Math.round((h.window_seconds || 300) / 60));
  $('#request-window').textContent = `近 ${minutes} 分钟`;
  $('#ov-window').textContent = `活跃为最近 ${minutes} 分钟估算`;
  setMarkup(
    $('#request-metrics'),
    [
      ['业务请求', num(h.requests)],
      ['HTTP 失败', num(h.http_errors), h.http_errors ? 'bad' : ''],
      ['超过 1 秒', num(h.slow_requests), h.slow_requests ? 'warn' : ''],
      ['平均耗时', `${Number(h.mean_ms).toFixed(1)} ms`],
    ]
      .map(tile)
      .join(''),
  );
  roomView.available = true;
  roomView.rows = a.rooms;
  renderRooms();
}
$('#room-search').oninput = $('#room-state').onchange = () => {
  roomView.page = 0;
  renderRooms();
};
$('#room-prev').onclick = () => {
  roomView.page--;
  renderRooms();
};
$('#room-next').onclick = () => {
  roomView.page++;
  renderRooms();
};
$('#room-export').onclick = () =>
  downloadJSON('房间观测.json', { observed_at: state.status?.server_time_utc, rooms: filteredRooms() });
$('#room-rows').onclick = (e) => {
  const b = e.target.closest('[data-player]');
  if (b) openAccountDetail(Number(b.dataset.player));
};
let insightsRefreshing = false;
setInterval(async () => {
  if (
    document.hidden ||
    state.currentView !== 'dashboard' ||
    !$('#insights-auto').checked ||
    insightsRefreshing ||
    state.loading.has('dashboard')
  )
    return;
  insightsRefreshing = true;
  try {
    [state.status, state.towerPolicy] = await Promise.all([api('/api/status'), towerPolicy()]);
    renderDashboard();
  } catch (e) {
    $('#ov-updated').textContent = '刷新失败：' + errorText(e);
  } finally {
    insightsRefreshing = false;
  }
}, 15000);

// Boss difficulty rules dialog.
const bossRuleEditor = { rules: new Map(), revision: 0, group: null, dirty: false, busy: false, request: 0 };
$('#boss-attribute').innerHTML = attributeOptions;
$('#drop-attribute').innerHTML = attributeOptions;
function bossIsPublished(g) {
  const p = state.policy,
    now = Date.now() / 1000;
  return (
    !!p &&
    (!p.start_unix || now >= p.start_unix) &&
    (!p.end_unix || now < p.end_unix) &&
    (p.mode === 'all' || (p.group_ids || []).includes(g.group_id))
  );
}
function bossMatchesFilters(g) {
  const attr = $('#boss-attribute').value,
    difficulty = $('#boss-difficulty').value,
    published = $('#boss-published').value;
  return (
    (!attr ||
      (g.bosses || []).some((b) =>
        b.targets.some((t) => (t.stats?.attribute || '').split('_').includes(attr)),
      )) &&
    (!difficulty || (g.difficulties || []).includes(difficulty)) &&
    (!published || bossIsPublished(g) === (published === 'open'))
  );
}
function updateBossDifficultyFilter() {
  const selected = $('#boss-difficulty').value;
  $('#boss-difficulty').innerHTML =
    '<option value="">全部难度</option>' +
    [...new Set(state.groups.flatMap((g) => g.difficulties || []))]
      .map((v) => `<option value="${esc(v)}">${esc(v)}</option>`)
      .join('');
  $('#boss-difficulty').value = selected;
  if ($('#boss-difficulty').selectedIndex < 0) $('#boss-difficulty').value = '';
}
$$('#boss-attribute,#boss-difficulty,#boss-published').forEach(
  (el) =>
    (el.onchange = () => {
      state.bossPage = 0;
      renderBosses();
    }),
);
$('#boss-reset').onclick = () => {
  $('#boss-search').value = '';
  ['attribute', 'difficulty', 'published'].forEach((k) => ($(`#boss-${k}`).value = ''));
  state.bossKind = 'all';
  state.bossPage = 0;
  renderBosses();
};
function bossStatTable(b) {
  return `<div class="table-wrap"><table class="dense"><thead><tr><th>波 / 部位</th><th>属性 / HP</th><th>物攻 / 魔攻</th><th>物防 / 魔防</th></tr></thead><tbody>${
    b.targets
      .map((t) => {
        const s = t.stats;
        return `<tr><td>${esc(targetLabel(t))}</td><td>${s ? `${esc(attributeName(s.attribute))} / ${num(s.hp)}` : '资料未载入'}</td><td>${s ? `${num(s.attack)} / ${num(s.magic)}` : '—'}</td><td>${s ? `${num(s.defense)} / ${num(s.magic_defense)}` : '—'}</td></tr>`;
      })
      .join('') || emptyRow(4, '此难度暂无可展示的敌人基础资料')
  }</tbody></table></div>`;
}
async function openBossDetails(id) {
  const group = state.groups.find((g) => g.group_id === id);
  if (!group || bossRuleEditor.busy) return;
  if (!(await confirmDiscard(bossRuleEditor.dirty, 'BOSS 规则有未保存的修改，打开其他 Boss 会放弃。')))
    return;
  const serial = ++bossRuleEditor.request;
  bossRuleEditor.group = null;
  bossRuleEditor.dirty = false;
  $('#boss-detail-title').textContent = group.name + ' · 各难度';
  $('#boss-detail-rules').innerHTML = '<p class="hint">正在读取…</p>';
  $('#boss-detail-modal').classList.add('open');
  $('#boss-detail-save').disabled = true;
  try {
    const data = await api('/api/boss-rules');
    if (serial !== bossRuleEditor.request) return;
    bossRuleEditor.revision = data.revision;
    bossRuleEditor.rules = new Map(
      data.rules.map((row) => [
        row.boss_id,
        {
          ...row,
          draft: {
            boss_id: row.boss_id,
            standard: row.standard,
            own_deck: row.own_deck,
            continue: row.continue,
          },
        },
      ]),
    );
    bossRuleEditor.group = group;
    bossRuleEditor.dirty = false;
    renderBossRuleForms();
  } catch (e) {
    if (serial !== bossRuleEditor.request) return;
    $('#boss-detail-rules').innerHTML = `<div class="empty"><b>读取失败</b>${esc(errorText(e))}</div>`;
  }
}
const ruleColumns = [
  ['standard', '单人／组队', '普通入口：单人与组队均可进入'],
  ['own_deck', '单人（仅自己的卡组）', '只能使用自己卡组的单人入口；标“无配置”的难度没有对应战斗配置'],
  ['continue', '允许复活', '战斗失败后可按游戏原费用扣水晶复活'],
];
const ruleLabel = (k) => ruleColumns.find((c) => c[0] === k)[1];
// One row per difficulty, one switch per rule; column buttons apply to the whole group. Unsaved cells are highlighted.
function renderBossRuleForms() {
  const bosses = bossRuleEditor.group.bosses || [];
  $('#boss-detail-save').disabled = bossRuleEditor.busy || !bosses.length;
  const head = ruleColumns
    .map(
      ([k, label, help]) =>
        `<th><span title="${help}">${label}</span><span class="col-actions"><button type="button" class="ghost" data-col="${k}" data-value="1" title="本组全部难度开启「${label}」">全开</button><button type="button" class="ghost" data-col="${k}" data-value="0" title="本组全部难度关闭「${label}」">全关</button></span></th>`,
    )
    .join('');
  const drops = (b) =>
    `<button type="button" class="ghost" data-edit-drops="${b.boss_id}">配置掉落 →</button>`;
  const rows = bosses
    .map((b) => {
      const row = bossRuleEditor.rules.get(b.boss_id),
        rule = row?.draft,
        name = `<b>${esc(b.difficulty)}</b> <code>#${b.boss_id}</code>`;
      if (!rule)
        return `<tr data-boss="${b.boss_id}"><td>${name}</td><td colspan="4" class="hint">沿用原生入口规则，不能在此修改</td><td>${drops(b)}</td></tr>`;
      const cell = (k) => {
        const off = k === 'own_deck' && !row.own_deck_boss_id;
        return `<td class="${rule[k] !== row[k] ? 'changed' : ''}"><label class="switch"><input data-rule="${k}" type="checkbox" aria-label="${esc(b.difficulty)} ${ruleLabel(k)}" ${rule[k] ? 'checked' : ''} ${off ? 'disabled' : ''}></label>${off ? '<span class="tag" title="此难度没有自卡组战斗配置">无配置</span>' : ''}</td>`;
      };
      return `<tr data-boss="${b.boss_id}"><td>${name}</td><td>单人 ${row.bp_use}／组队 ${row.bp_use_half}</td>${ruleColumns.map(([k]) => cell(k)).join('')}<td>${drops(b)}</td></tr>`;
    })
    .join('');
  $('#boss-detail-rules').innerHTML =
    `<p class="hint">每个开关只影响后续开战，已开始的战斗不变。列标题的“全开／全关”作用于本组全部难度；普通与自卡组入口共用掉落和名声奖励。体力和通关经验在「Boss 发布 › 体力与经验」按难度统一设置。</p><div class="table-wrap"><table class="rule-matrix"><thead><tr><th>难度</th><th>BP 消耗</th>${head}<th><span class="sr-only">掉落</span></th></tr></thead><tbody>${rows || emptyRow(6, '此组没有可配置的难度')}</tbody></table></div><p class="hint" id="boss-rule-changes" aria-live="polite"></p><details class="tool-drawer"><summary>各难度敌人属性（HP 已计入编队倍率）</summary><p class="hint">基础参数来自当前战斗编队；不含开场被动、战斗BUFF和动态变身。</p>${bosses.map((b) => `<h3>${esc(b.difficulty)} <code>#${b.boss_id}</code></h3>${bossStatTable(b)}`).join('')}</details>`;
  updateBossRuleChanges();
}
function bossRuleChanges() {
  const rows = [];
  for (const b of bossRuleEditor.group?.bosses || []) {
    const row = bossRuleEditor.rules.get(b.boss_id);
    if (!row) continue;
    for (const [k, label] of ruleColumns)
      if (row.draft[k] !== row[k])
        rows.push([
          `${b.difficulty} #${b.boss_id} · ${label}`,
          row[k] ? '开' : '关',
          row.draft[k] ? '开' : '关',
        ]);
  }
  return rows;
}
function updateBossRuleChanges() {
  const n = bossRuleChanges().length;
  bossRuleEditor.dirty = n > 0;
  const el = $('#boss-rule-changes');
  if (el) {
    el.textContent = n ? `已修改 ${n} 处（黄色高亮），点击“保存难度规则”后生效。` : '';
    el.className = n ? 'state-dirty' : 'hint';
  }
}
$('#boss-rows').addEventListener('click', (e) => {
  const b = e.target.closest('[data-boss-detail]');
  if (b) openBossDetails(Number(b.dataset.bossDetail));
});
$('#boss-detail-rules').onchange = (e) => {
  const el = e.target,
    kind = el.dataset.rule;
  if (!kind) return;
  const row = bossRuleEditor.rules.get(Number(el.closest('[data-boss]').dataset.boss));
  row.draft[kind] = el.checked;
  el.closest('td').classList.toggle('changed', row.draft[kind] !== row[kind]);
  updateBossRuleChanges();
};
$('#boss-detail-rules').onclick = async (e) => {
  const col = e.target.closest('[data-col]'),
    drop = e.target.closest('[data-edit-drops]');
  if (col) {
    const k = col.dataset.col,
      on = col.dataset.value === '1';
    let applied = 0,
      skipped = 0;
    for (const b of bossRuleEditor.group.bosses || []) {
      const row = bossRuleEditor.rules.get(b.boss_id);
      if (!row) continue;
      if (on && k === 'own_deck' && !row.own_deck_boss_id) {
        skipped++;
        continue;
      }
      row.draft[k] = on;
      applied++;
    }
    renderBossRuleForms();
    toast(
      `已${on ? '开启' : '关闭'} ${applied} 个难度的「${ruleLabel(k)}」${skipped ? `；${skipped} 个难度没有自卡组配置，已跳过` : ''}（保存后生效）`,
    );
  }
  if (drop) {
    if (!(await closeBossDetails())) return;
    await switchView('drops');
    if (!(await confirmDiscard(dropEditor.dirty, '切换难度会放弃掉落草稿中未保存的修改。', '放弃并切换')))
      return;
    await contentAction('drops', () => selectDrop(Number(drop.dataset.editDrops)), '载入');
    $('#drop-select').value = drop.dataset.editDrops;
  }
};
async function closeBossDetails() {
  if (bossRuleEditor.busy) return false;
  if (!(await confirmDiscard(bossRuleEditor.dirty, 'BOSS 规则有未保存的修改，关闭后会放弃。', '放弃并关闭')))
    return false;
  $('#boss-detail-modal').classList.remove('open');
  bossRuleEditor.request++;
  bossRuleEditor.group = null;
  bossRuleEditor.dirty = false;
  return true;
}
$('#boss-detail-close').onclick = closeBossDetails;
$('#boss-detail-default').onclick = async () => {
  if (bossRuleEditor.busy || !bossRuleEditor.group) return;
  if (!(await askConfirm('把本组全部难度的开关恢复为默认值？', '只改草稿，保存后生效。', { ok: '恢复默认' })))
    return;
  for (const b of bossRuleEditor.group.bosses || []) {
    const row = bossRuleEditor.rules.get(b.boss_id);
    if (row) row.draft = structuredClone(row.default);
  }
  renderBossRuleForms();
};
$('#boss-detail-save').onclick = async () => {
  if (bossRuleEditor.busy || !bossRuleEditor.group) return;
  const rules = (bossRuleEditor.group.bosses || [])
    .map((b) => bossRuleEditor.rules.get(b.boss_id)?.draft)
    .filter(Boolean);
  const changes = bossRuleChanges();
  if (!rules.length) return;
  if (!changes.length) return toast('没有需要保存的修改');
  if (
    !(await reviewChanges({
      title: `核对难度规则变更 · ${bossRuleEditor.group.name}`,
      sections: [{ title: '入口与复活', rows: changes }],
      note: '用于后续开战，已开始的战斗不变；复活按游戏原费用扣水晶。',
    }))
  )
    return;
  bossRuleEditor.busy = true;
  $('#boss-detail-rules').inert = true;
  $('#boss-detail-save').disabled = true;
  try {
    const data = await api('/api/boss-rules', {
      method: 'PUT',
      body: JSON.stringify({ expected_revision: bossRuleEditor.revision, rules }),
    });
    bossRuleEditor.revision = data.revision;
    for (const rule of rules) {
      const row = bossRuleEditor.rules.get(rule.boss_id);
      for (const [k] of ruleColumns) row[k] = rule[k];
    }
    state.loaded.delete('audit');
    renderBossRuleForms();
    toast('难度规则已保存，用于后续开战');
  } catch (e) {
    toast(
      e?.status === 409
        ? '难度规则已被其他页面更新（版本冲突）：请关闭后重新打开再修改，当前修改未保存'
        : errorText(e),
      true,
    );
  } finally {
    bossRuleEditor.busy = false;
    $('#boss-detail-rules').inert = false;
    $('#boss-detail-save').disabled = false;
  }
};

// Exchange / gacha / pool selector filters (markup lives in admin_ui.html). The exchange and pool
// pickers are searchable selects (enhanceSelect in admin_kit.js); these filters only narrow what they offer.
function exchangeShopMatches(s) {
  const status = $('#exchange-shop-status').value,
    label = exchangeStatus(s);
  return (
    !status ||
    (status === 'deleted'
      ? label === '已删除'
      : status === 'active'
        ? label === '开放中'
        : status === 'scheduled'
          ? label === '未开始'
          : label === '关闭' || label === '已结束')
  );
}
$('#exchange-shop-status').onchange = () => {
  renderExchangeSelect();
  if (exchangeEditor.shop) $('#exchange-select').value = exchangeEditor.shop.trade_shopid;
};

function filteredGachas() {
  const status = $('#gacha-status').value,
    category = $('#gacha-category').value,
    pending = (g) => ['opening', 'closing'].includes(gachaOpenState(g).key);
  return (state.gachaPresets || []).filter(
    (g) =>
      matchesWords(
        `${g.name} ${g.group_id} ${g.gacha_ids.join(' ')} ${g.payment_item || ''} ${g.payment_item_id} ${g.category || ''}`,
        $('#gacha-search').value,
      ) &&
      (!category || g.category === category) &&
      (!status ||
        (status === 'pending' ? pending(g) : state.gachaSelected.has(g.group_id) === (status === 'open'))),
  );
}
$('#gacha-search').oninput = $('#gacha-status').onchange = $('#gacha-category').onchange = renderGachas;
$('#gacha-select-filtered').onclick = () => {
  filteredGachas().forEach((g) => state.gachaSelected.add(g.group_id));
  renderGachas();
};
$('#gacha-clear-filtered').onclick = () => {
  filteredGachas().forEach((g) => state.gachaSelected.delete(g.group_id));
  renderGachas();
};

function renderPoolSelect() {
  const id = poolEditor.row?.gacha_id,
    status = $('#pool-config-status').value,
    now = Date.now() / 1000;
  const pools = poolEditor.rows.filter((row) => poolGroupRows(row)[0] === row);
  renderPoolCategoryFilter(pools);
  const category = Number($('#pool-category-filter').value) || 0;
  const rows = pools.filter((row) => {
    const c = row.config,
      group = poolGroupRows(row),
      open = (!c.start_unix || now >= c.start_unix) && (!c.end_unix || now < c.end_unix);
    return (
      (status === 'deleted' ? row.deleted : !row.deleted) &&
      (!category || poolLiveCategory(row) === category) &&
      (!status ||
        (status === 'draft'
          ? group.some(poolPendingDraft)
          : status === 'custom'
            ? row.custom
            : status === 'deleted'
              ? true
              : open === (status === 'open')))
    );
  });
  const placeholder =
    id || !rows.length
      ? `<option value="">${rows.length ? '请选择卡池（当前草稿保留）' : '没有匹配的卡池'}</option>`
      : '';
  $('#pool-select').innerHTML =
    placeholder +
    rows
      .map((row) => {
        const group = poolGroupRows(row);
        return `<option value="${row.gacha_id}" data-terms="${esc(group.map((r) => `${r.gacha_id} ${r.config.name}`).join(' ') + ' ' + poolCategoryName(poolLiveCategory(row)))}">${esc(row.config.name)} · ${row.gacha_id} · ${esc(poolCategoryName(poolLiveCategory(row)))}${row.custom ? ' · 后台新建' : ''}${row.deleted ? '（已删除）' : group.some(poolPendingDraft) ? '（有未发布草稿）' : ''}</option>`;
      })
      .join('');
  if (id) $('#pool-select').value = id;
  $('#pool-filter-count').textContent =
    `${rows.length === pools.length ? `共 ${pools.length}` : `可选 ${rows.length}/${pools.length}`} 个卡池${id && !rows.some((row) => row.gacha_id === id) ? '；当前编辑的卡池不在筛选结果中' : ''}`;
  renderPoolTrash(pools.filter((row) => row.deleted));
}
$('#pool-config-status').onchange = renderPoolSelect;
// Entries that hold at least one pool (deleted pools included, so the trash filter still works), with counts.
function renderPoolCategoryFilter(pools) {
  const el = $('#pool-category-filter'),
    value = el.value,
    counts = new Map();
  for (const row of pools) {
    const id = poolLiveCategory(row);
    counts.set(id, (counts.get(id) || 0) + 1);
  }
  const ids = (poolEditor.categories || [])
    .map((c) => c.id)
    .filter((id) => counts.has(id))
    .concat([...counts.keys()].filter((id) => !(poolEditor.categories || []).some((c) => c.id === id)));
  el.innerHTML =
    `<option value="">全部扭蛋分类</option>` +
    ids
      .map((id) => `<option value="${id}">${esc(poolCategoryName(id))}（${counts.get(id)}）</option>`)
      .join('');
  el.value = ids.includes(Number(value)) ? value : '';
}
$('#pool-category-filter').onchange = renderPoolSelect;
