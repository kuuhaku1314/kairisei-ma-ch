'use strict';
function settingsBattleSpeedText(speed) {
  return speed === 0 ? '不限（使用房主客户端设置）' : `${speed / 100} 倍`;
}
const maintenance = {
  value: null,
  rooms: 0,
  creating: 0,
  sending: false,
  timer: null,
  trash: [],
  appliedPurge: '',
  loadSerial: 0,
};
const cleanupNames = {
  gacha_plays: '抽取次数',
  gacha_selections: '自选记录',
  gacha_daily_claims: '每日免费记录',
  shop_purchases: '商店累计限额',
  shop_periods: '商店周期限额',
  event_shop_purchases: '活动商店限额',
  trade_shop_purchases: '兑换限额',
};
function auditCleanupRange() {
  const from = $('#audit-cleanup-start').value,
    to = $('#audit-cleanup-end').value;
  return {
    start_utc: from ? new Date(from).toISOString() : '',
    end_utc: to ? new Date(to).toISOString() : '',
  };
}
function renderMaintenance() {
  const m = maintenance.value,
    busy = maintenance.sending || !m || m.busy;
  setMaintenanceBanner(m);
  $('#maintenance-card').classList.toggle('on', !!m?.enabled);
  $('#maintenance-card').classList.toggle('working', !!m?.busy);
  $('#maintenance-state').textContent = !m
    ? '正在读取'
    : m.busy
      ? '维护中 · 操作进行中'
      : m.enabled
        ? '维护中'
        : '正常服务';
  $('#maintenance-rooms').textContent = m ? num(maintenance.rooms) : '—';
  $('#maintenance-game').textContent = m ? num(m.game_requests || 0) : '—';
  $('#maintenance-admin').textContent = m ? num(m.admin_requests || 0) : '—';
  $('#maintenance-toggle').textContent = m?.enabled ? '结束维护，恢复服务' : '开启维护';
  $('#maintenance-toggle').className = m?.enabled ? 'primary' : 'danger';
  $('#maintenance-toggle').disabled = busy;
  $('#maintenance-interrupt').disabled = busy || !m?.enabled;
  const roomsActive = maintenance.rooms > 0 || maintenance.creating > 0;
  const idle = !!m?.enabled && !m.busy && !m.game_requests && !m.admin_requests && !roomsActive;
  $$('#maintenance .mt-tool').forEach((tool) => {
    tool.classList.toggle('locked', !idle);
    tool.querySelector('.mt-lock').textContent = !m?.enabled
      ? '需先开启维护'
      : m.busy
        ? '维护操作进行中'
        : roomsActive
          ? '请先中断未结算对局'
          : !idle
            ? '等待进行中的请求结束'
            : '可以使用';
  });
  $('#maintenance-preview').disabled = busy || !idle;
  const job = m?.cleanup,
    report = job?.scope === 'gacha' ? null : job?.report;
  $('#maintenance-apply').disabled =
    $('#maintenance-preview').disabled ||
    job?.mode !== 'preview' ||
    job?.state !== 'complete' ||
    !report?.accounts;
  $('#maintenance-detail').textContent = !m
    ? ''
    : m.busy
      ? '正在执行维护操作，已开始的结算会正常完成。维护会保持开启。'
      : m.enabled
        ? `新请求及建房、预约、入房、重连已暂停。${maintenance.creating ? `还有 ${maintenance.creating} 个建房请求正在退出。` : ''}可中断未结算的单人和组队对局，再进行清理；完成后记得恢复服务。`
        : `正在正常接受游戏请求。开启维护会停止新入场，现有 ${maintenance.rooms} 个组队房间可随后关闭。`;
  let html =
    job?.state === 'failed' && !['audit', 'gacha', 'history'].includes(job.scope)
      ? `<p class="hint">未确认清理成功：${esc(job.error)}。请保持维护，核对结果后重新预览。</p>${report?.backup ? `<p>已保留恢复备份：<code>${esc(report.backup)}</code></p>` : ''}`
      : '';
  if (report && job.state === 'complete') {
    html += `<p>已检查 ${num(report.scanned)} 个账号，${report.applied ? '已清理' : '可清理'} ${num(report.accounts)} 个账号的状态；元数据${report.applied ? '减少' : '预计减少'} ${num(Math.max(0, report.bytes_before - report.bytes_after))} 字节。</p>`;
    html += `<div class="table-wrap"><table><thead><tr><th>状态类型</th><th>条目数</th></tr></thead><tbody>${
      Object.entries(report.entries)
        .map(([key, n]) => `<tr><td>${esc(cleanupNames[key] || key)}</td><td>${num(n)}</td></tr>`)
        .join('') || emptyRow(2, '没有可清理状态')
    }</tbody></table></div>`;
    if (report.backup) html += `<p>恢复备份：<code>${esc(report.backup)}</code></p>`;
    html +=
      '<p class="hint">这是逻辑数据减少量。SQLite 会复用释放空间，数据库文件不会立即缩小；本操作不压缩数据库。</p>';
  }
  $('#maintenance-report').innerHTML = html;
  const audit = job?.audit_report,
    range = auditCleanupRange(),
    same =
      !!audit &&
      range.start_utc.replace('.000Z', 'Z') === audit.start_utc.replace('.000Z', 'Z') &&
      range.end_utc.replace('.000Z', 'Z') === audit.end_utc.replace('.000Z', 'Z');
  $('#audit-cleanup-preview').disabled = $('#maintenance-preview').disabled;
  $('#audit-cleanup-start').disabled = $('#audit-cleanup-end').disabled = busy;
  $('#audit-cleanup-apply').disabled =
    $('#maintenance-preview').disabled ||
    job?.scope !== 'audit' ||
    job.mode !== 'preview' ||
    job.state !== 'complete' ||
    !audit?.records ||
    !same;
  let history =
    job?.scope === 'audit' && job.state === 'failed' ? `<p>审计清理未确认成功：${esc(job.error)}</p>` : '';
  if (audit && job.state === 'complete')
    history += `<p>时间范围：${audit.start_utc ? esc(new Date(audit.start_utc).toLocaleString()) : '最早记录'}（含）至 ${esc(new Date(audit.end_utc).toLocaleString())}（不含）。${audit.applied ? '已清理' : '可清理'} ${num(audit.records)} 条日志，日志内容 ${num(audit.payload_bytes)} 字节。</p>`;
  if (audit?.backup) history += `<p>恢复备份：<code>${esc(audit.backup)}</code></p>`;
  $('#maintenance-audit-report').innerHTML = history;
  const group = Number($('#maintenance-gacha-select').value),
    purge = job?.scope === 'gacha' ? job.report : null;
  $('#maintenance-gacha-select').disabled = busy;
  $('#maintenance-gacha-preview').disabled = $('#maintenance-preview').disabled || !group;
  $('#maintenance-gacha-apply').disabled =
    $('#maintenance-gacha-preview').disabled ||
    job?.scope !== 'gacha' ||
    job.mode !== 'preview' ||
    job.state !== 'complete' ||
    job.group_id !== group ||
    !purge?.gacha_ids?.length;
  let retirement =
    job?.scope === 'gacha' && job.state === 'failed' ? `<p>永久删除未确认成功：${esc(job.error)}</p>` : '';
  if (purge && job.state === 'complete')
    retirement += `<p>卡池组 ${job.group_id} · 分池 ${purge.gacha_ids.join('、')}：${purge.applied ? '已永久删除' : '将删除'} ${num(purge.removed_documents)} 份草稿／发布配置，清理 ${num(purge.accounts)} 个账号的对应记录。配置与元数据共${purge.applied ? '释放' : '预计释放'} ${num(Math.max(0, (purge.configuration_bytes || 0) + purge.bytes_before - purge.bytes_after))} 字节。</p><p class="hint">备份和历史审计保留，SQLite 空间供后续复用；共用封面文件不在此处删除。</p>`;
  if (purge?.backup) retirement += `<p>恢复备份：<code>${esc(purge.backup)}</code></p>`;
  $('#maintenance-gacha-report').innerHTML = retirement;
  if (typeof renderHistoryCleanup === 'function') renderHistoryCleanup();
}
async function loadMaintenance() {
  const serial = ++maintenance.loadSerial;
  clearTimeout(maintenance.timer);
  if (!$('#audit-cleanup-end').dataset.initialized) {
    $('#audit-cleanup-end').value = localTimeInput(Math.floor(Date.now() / 1000) - 90 * 86400);
    $('#audit-cleanup-end').dataset.initialized = '1';
  }
  const data = await api('/api/maintenance');
  if (serial !== maintenance.loadSerial) return;
  if (typeof loadHistoryOverview === 'function' && !historyCleanup.overview)
    loadHistoryOverview().catch((e) => reportError('maintenance', e));
  maintenance.value = data.maintenance;
  maintenance.rooms = data.rooms || 0;
  maintenance.creating = data.creating_rooms || 0;
  maintenance.trash = data.gacha_trash || [];
  const selection = $('#maintenance-gacha-select').value;
  $('#maintenance-gacha-select').innerHTML =
    '<option value="">请选择回收站卡池</option>' +
    maintenance.trash
      .map(
        (g) =>
          `<option value="${g.group_id}">${esc(g.name)} · ${g.group_id} · ${g.gacha_ids.length} 个分池</option>`,
      )
      .join('');
  $('#maintenance-gacha-select').value = selection;
  const job = data.maintenance.cleanup;
  if (
    job?.scope === 'gacha' &&
    job.state === 'complete' &&
    job.report?.applied &&
    maintenance.appliedPurge !== job.report.digest
  ) {
    maintenance.appliedPurge = job.report.digest;
    const ids = new Set(job.report.gacha_ids);
    poolEditor.rows = poolEditor.rows.filter((r) => !ids.has(r.gacha_id));
    poolEditor.customRevision = Math.max(poolEditor.customRevision, job.report.registry_revision || 0);
    if (poolEditor.row && ids.has(poolEditor.row.gacha_id)) {
      poolEditor.dirty = false;
      selectPool(poolEditor.rows.find((r) => !r.deleted)?.gacha_id);
    }
    if (state.loaded.has('pool-editor')) renderPoolSelect();
    if (!adminPolicyDirty('gachas')) state.loaded.delete('gachas');
    state.loaded.delete('audit');
  }
  renderMaintenance();
  if (
    data.maintenance.busy ||
    data.maintenance.game_requests > 0 ||
    data.maintenance.admin_requests > 0 ||
    (data.maintenance.enabled && (maintenance.rooms || maintenance.creating))
  )
    maintenance.timer = setTimeout(() => {
      if ($('#maintenance').classList.contains('active'))
        loadMaintenance().catch((e) => reportError('maintenance', e));
    }, 1500);
}
async function changeMaintenance(action) {
  if (maintenance.sending) return;
  ++maintenance.loadSerial;
  clearTimeout(maintenance.timer);
  maintenance.sending = true;
  renderMaintenance();
  try {
    await action();
    await loadMaintenance();
    state.loaded.delete('audit');
  } catch (e) {
    reportError('maintenance', e);
  } finally {
    maintenance.sending = false;
    renderMaintenance();
  }
}
$('#maintenance-refresh').onclick = () => changeMaintenance(() => loadMaintenance());
$('.nav [data-view="maintenance"]').addEventListener('click', () => {
  if (state.loaded.has('maintenance')) {
    loadMaintenance().catch((e) => reportError('maintenance', e));
    loadHistoryOverview().catch((e) => reportError('maintenance', e));
  }
});
$('#maintenance-toggle').onclick = async () => {
  const m = maintenance.value;
  if (!m || m.busy) return;
  if (
    !(await reviewChanges({
      title: m.enabled ? '结束服务器维护' : '开启服务器维护',
      sections: [{ title: '服务状态', rows: diffRows([['维护开关', m.enabled, !m.enabled]]) }],
      note: m.enabled
        ? '恢复接受游戏请求。'
        : '暂停新的游戏请求、建房、预约、入房和重连；正在处理的请求先完成。开启后可中断未结算对局，无需等待玩家退出。',
    }))
  )
    return;
  return changeMaintenance(() =>
    api('/api/maintenance', {
      method: 'PUT',
      body: JSON.stringify({ enabled: !m.enabled, expected_revision: m.revision }),
    }),
  );
};
$('#maintenance-interrupt').onclick = async () => {
  const m = maintenance.value;
  if (!m?.enabled || m.busy) return;
  if (
    !(await reviewChanges({
      title: '中断未结算对局',
      sections: [
        {
          title: '影响范围',
          rows: [
            ['组队房间', num(maintenance.rooms) + ' 个', '关闭并断开连接'],
            ['单人对局', '尚未结算的对局', '作废，恢复服务后忽略旧结算请求'],
          ],
        },
      ],
      note: '已经开始的结算会先正常完成，已入库奖励及结算回执保留。其余对局直接中断，不生成胜利奖励。',
      confirmText: '中断未结算对局',
    }))
  )
    return;
  return changeMaintenance(() =>
    api('/api/maintenance/battles/interrupt', {
      method: 'POST',
      body: JSON.stringify({ expected_revision: m.revision }),
    }),
  );
};
$('#maintenance-preview').onclick = () =>
  changeMaintenance(() =>
    api('/api/maintenance/cleanup', { method: 'POST', body: JSON.stringify({ mode: 'preview' }) }),
  );
$('#maintenance-apply').onclick = async () => {
  const job = maintenance.value?.cleanup,
    report = job?.report;
  if (job?.scope === 'gacha' || job?.mode !== 'preview' || job.state !== 'complete' || !report?.accounts)
    return;
  if (
    !(await reviewChanges({
      title: '核对无引用状态清理',
      sections: [
        {
          title: '清理范围',
          rows: Object.entries(report.entries).map(([k, n]) => [
            cleanupNames[k] || k,
            num(n) + ' 条',
            '删除',
          ]),
        },
      ],
      note: `涉及 ${num(report.accounts)} 个账号；执行前保存恢复备份。配置或账号数据变化时会拒绝执行，需要重新预览。`,
    }))
  )
    return;
  return changeMaintenance(() =>
    api('/api/maintenance/cleanup', {
      method: 'POST',
      body: JSON.stringify({ mode: 'apply', digest: report.digest }),
    }),
  );
};
async function openGachaPurge(group) {
  await switchView('maintenance');
  await loadMaintenance();
  $('#maintenance-gacha-select').value = String(group);
  renderMaintenance();
  $('#maintenance-gacha-section').scrollIntoView({ block: 'center', behavior: 'smooth' });
}
$('#maintenance-gacha-select').onchange = renderMaintenance;
$('#maintenance-gacha-preview').onclick = () => {
  const group = Number($('#maintenance-gacha-select').value);
  if (!group) return;
  return changeMaintenance(() =>
    api('/api/maintenance/cleanup', {
      method: 'POST',
      body: JSON.stringify({ scope: 'gacha', mode: 'preview', group_id: group }),
    }),
  );
};
$('#maintenance-gacha-apply').onclick = async () => {
  const job = maintenance.value?.cleanup,
    report = job?.report,
    group = Number($('#maintenance-gacha-select').value);
  if (
    job?.scope !== 'gacha' ||
    job.mode !== 'preview' ||
    job.state !== 'complete' ||
    job.group_id !== group ||
    !report?.gacha_ids?.length
  )
    return;
  const name = maintenance.trash.find((g) => g.group_id === group)?.name || group;
  if (
    !(await reviewChanges({
      title: `永久删除卡池「${name}」`,
      sections: [
        {
          title: '配置和玩家引用',
          rows: [
            ['分池 ID', report.gacha_ids.join('、'), '永久停用，不复用'],
            ['草稿／发布配置', num(report.removed_documents) + ' 份', '删除'],
            ['对应玩家记录', num(report.accounts) + ' 个账号', '清除该池的次数／自选／每日免费记录'],
          ],
        },
      ],
      note: '先备份，再在同一事务中删除。完成后不再提供回收站恢复；已获得卡牌、货币、剧情和 BOSS 进度均保留。',
      confirmText: '备份并永久删除',
    }))
  )
    return;
  return changeMaintenance(() =>
    api('/api/maintenance/cleanup', {
      method: 'POST',
      body: JSON.stringify({ scope: 'gacha', mode: 'apply', group_id: group, digest: report.digest }),
    }),
  );
};
$('#audit-cleanup-start').oninput = $('#audit-cleanup-end').oninput = renderMaintenance;
$('#audit-cleanup-preview').onclick = () => {
  const range = auditCleanupRange();
  if (!range.end_utc || (range.start_utc && range.start_utc >= range.end_utc))
    return toast('请选择有效的开始／结束时间', true);
  return changeMaintenance(() =>
    api('/api/maintenance/cleanup', {
      method: 'POST',
      body: JSON.stringify({ scope: 'audit', mode: 'preview', ...range }),
    }),
  );
};
$('#audit-cleanup-apply').onclick = async () => {
  const job = maintenance.value?.cleanup,
    report = job?.audit_report;
  if (job?.scope !== 'audit' || job.mode !== 'preview' || job.state !== 'complete' || !report?.records)
    return;
  if (
    !(await reviewChanges({
      title: '核对审计历史清理',
      sections: [
        {
          title: '日志范围',
          rows: [
            [
              '开始时间（含）',
              report.start_utc ? new Date(report.start_utc).toLocaleString() : '最早记录',
              '',
            ],
            ['结束时间（不含）', new Date(report.end_utc).toLocaleString(), ''],
            ['操作日志', `${num(report.records)} 条`, `备份后删除`],
          ],
        },
      ],
      note: '只删除这段时间的审计日志；当前配置、草稿、玩家数据及防重复领取的收据不变。',
      confirmText: '确认备份并清理',
    }))
  )
    return;
  return changeMaintenance(() =>
    api('/api/maintenance/cleanup', {
      method: 'POST',
      body: JSON.stringify({
        scope: 'audit',
        mode: 'apply',
        start_utc: report.start_utc,
        end_utc: report.end_utc,
        digest: report.digest,
      }),
    }),
  );
};
// 运营设置 and 道具商店 edit one settings document and share its revision; each page saves only its own part.
const runtimeSettings = {
  value: null,
  revision: 0,
  revisions: { settings: 0, shop: 0 },
  catalog: [],
  shopRows: [],
  products: [],
  rendered: new Set(),
};
const shopTabNames = ['道具', 'BOSS钥匙', '表情', '礼包', '扩容'];
const shopPeriodNames = { '': '不按周期', day: '每日', week: '每周', month: '每月' };
// Limits count purchase units (份); an empty field is unlimited. Both limits apply and the smaller remainder wins.
function shopLimitText(total, period, limit) {
  const parts = [];
  if (period && limit > 0) parts.push(`${shopPeriodNames[period]} ${num(limit)} 份`);
  if (total > 0) parts.push(`总共 ${num(total)} 份`);
  return parts.length ? '每人' + parts.join('，') : '不限购';
}
function updateShopLimit(tr) {
  const value = (key) => Number(tr.querySelector(`[data-shop-field="${key}"]`).value) || 0,
    period = tr.querySelector('[data-shop-field="period"]').value,
    total = value('total_limit'),
    limit = value('period_limit');
  const idle = !!period && limit > 0 && total > 0 && limit >= total,
    note = tr.querySelector('.shop-limit-note');
  // Only the ineffective-period case needs a note; the inputs already read as a sentence.
  note.hidden = !idle;
  note.textContent = idle ? `${shopPeriodNames[period]}额度不小于总共额度，周期限购不起作用` : '';
  note.previousElementSibling.title = shopLimitText(total, period, limit);
}
const shopFilter = { tab: -1 };
function normalizeShopRow(row) {
  const base = runtimeSettings.catalog.find((b) => b.item_shop_lineupid === row.lineup_id) || {};
  return {
    lineup_id: row.lineup_id,
    enabled: row.enabled,
    price: row.price,
    item_id: row.item_id || 0,
    buy_type: row.buy_type || 0,
    name: row.name || '',
    tab_type: row.tab_type || 0,
    pay_type: row.pay_type || base.pay_type || 1,
    quantity: row.quantity || base.interiors?.[0]?.num || 1,
    buy_num_max: row.buy_num_max || base.buy_num_max || 99,
    total_limit: row.total_limit || 0,
    period: row.period || '',
    period_limit: row.period_limit || 0,
  };
}
function itemShopDraft() {
  return $$('#shop-rows tr[data-id]').map((tr) => {
    const row = {
      ...runtimeSettings.shopRows.find((r) => r.lineup_id === Number(tr.dataset.id)),
      enabled: tr.querySelector('.shop-enabled').checked,
    };
    for (const input of tr.querySelectorAll('[data-shop-field]')) {
      const key = input.dataset.shopField;
      row[key] = ['period', 'name'].includes(key) ? input.value.trim() : Number(input.value);
    }
    return row;
  });
}
const shopProductName = (row) =>
  row.name ||
  runtimeSettings.catalog.find((b) => b.item_shop_lineupid === row.lineup_id)?.lineup_name ||
  `商品 ${row.lineup_id}`;
// Item and stamp products show their catalog art (resolved after load); card capacity expansions have none and get a symbol.
function shopRewardKey(row) {
  const base = runtimeSettings.catalog.find((b) => b.item_shop_lineupid === row.lineup_id)?.interiors?.[0],
    type = row.buy_type || base?.buy_type || 1,
    id = row.item_id || base?.buy_typeid || 0;
  return type >= 3 || !id ? '' : `${type === 2 ? 16 : 8}:${id}`;
}
function shopThumbHTML(key, name) {
  if (!key) return '<svg class="icon"><use href="#i-layers"/></svg>';
  const info = catalogInfo.get(key);
  return info ? catalogImage(info) : image('', name);
}
function refreshShopThumbs() {
  $$('#shop-rows [data-thumb-key]').forEach((el) => {
    if (catalogInfo.has(el.dataset.thumbKey)) el.innerHTML = shopThumbHTML(el.dataset.thumbKey, '');
  });
}
function resolveShopThumbs() {
  const pairs = runtimeSettings.shopRows
    .map(shopRewardKey)
    .filter((k) => k && !catalogInfo.has(k))
    .map((k) => k.split(':').map(Number));
  if (pairs.length) resolveCatalogEntries(pairs).then(refreshShopThumbs, () => {});
}
function renderItemShop() {
  const numeric = (row, key, label, min = 0, max = 10000000, disabled = false) =>
    `<input type="number" class="shop-${key === 'price' ? 'price' : key}" data-shop-field="${key}" min="${min}" max="${max}" value="${row[key]}" aria-label="${esc(label)}" ${disabled ? 'disabled' : ''}>`;
  const limit = (row, key, label, disabled = false) =>
    `<input type="number" data-shop-field="${key}" min="0" max="10000000" value="${row[key] || ''}" placeholder="${disabled ? '—' : '不限'}" aria-label="${esc(label)}" ${disabled ? 'disabled' : ''}>`;
  $('#shop-rows').innerHTML =
    runtimeSettings.shopRows
      .map((row) => {
        const name = shopProductName(row),
          tabs =
            row.lineup_id < 70000001 ? [0] : row.buy_type === 2 ? [2] : row.buy_type >= 3 ? [4] : [0, 1, 3];
        const kind =
          row.buy_type === 2
            ? '表情'
            : row.buy_type >= 3
              ? '扩容'
              : row.lineup_id >= 70000001
                ? '道具'
                : '原生商品';
        return (
          `<tr data-id="${row.lineup_id}" data-name="${esc(name)}" class="${row.enabled ? '' : 'off'}"><td data-label="上架"><label class="switch"><input class="shop-enabled" type="checkbox" aria-label="上架${esc(name)}" ${row.enabled ? 'checked' : ''}></label></td>` +
          `<td data-label="商品" class="shop-name"><div class="reward-cell"><span class="thumb thumb-sm${shopRewardKey(row) ? '' : ' thumb-icon'}" data-thumb-key="${shopRewardKey(row)}">${shopThumbHTML(shopRewardKey(row), name)}</span><div>${row.lineup_id >= 70000001 ? `<input data-shop-field="name" maxlength="60" value="${esc(name)}" aria-label="商品名称" placeholder="商品名称">` : `<b>${esc(name)}</b>`}<span class="sub shop-sub"><code>#${row.lineup_id}</code> ${kind}<select class="shop-tab" data-shop-field="tab_type" aria-label="${esc(name)}所在页签" title="商店页签">${tabs.map((t) => `<option value="${t}" ${t === row.tab_type ? 'selected' : ''}>${shopTabNames[t]}页</option>`).join('')}</select></span></div></div></td>` +
          `<td data-label="价格"><div class="joined shop-price-pair"><select data-shop-field="pay_type" aria-label="商品币种">${[
            [1, '金币'],
            [3, '水晶'],
          ]
            .map(([t, n]) => `<option value="${t}" ${t === row.pay_type ? 'selected' : ''}>${n}</option>`)
            .join('')}</select>${numeric(row, 'price', name + '单价', 1)}</div></td>` +
          `<td data-label="购买单位"><div class="shop-line"><span>每份含</span>${numeric(row, 'quantity', name + '每份含的数量', 1, 1000000, row.buy_type === 2)}<em>个</em><span class="next">一次最多</span>${numeric(row, 'buy_num_max', name + '一次最多购买份数', 1, 9999, row.buy_type === 2)}<em>份</em></div></td>` +
          `<td data-label="每人限购"><div class="shop-line shop-limit-line"><span>总共</span>${limit(row, 'total_limit', name + '每人总共限购份数')}<em>份</em><select class="next" data-shop-field="period" aria-label="${esc(name)}限购周期">${Object.entries(
            shopPeriodNames,
          )
            .map(([v, n]) => `<option value="${v}" ${row.period === v ? 'selected' : ''}>${n}</option>`)
            .join(
              '',
            )}</select>${limit(row, 'period_limit', name + '周期内限购份数', !row.period)}<em>份</em></div><small class="shop-limit-note" hidden></small></td></tr>`
        );
      })
      .join('') || emptyRow(5, '没有可配置的商品');
  $$('#shop-rows tr[data-id]').forEach(updateShopLimit);
  resolveShopThumbs();
  applyShopFilter();
}
// Page-tab counts follow the draft; filters only hide rows, so saving always includes every product.
function renderShopTabs(rows = itemShopDraft()) {
  const bar = $('#shop-tabs');
  if (!bar.children.length)
    bar.innerHTML = [-1, 0, 1, 2, 3, 4]
      .map(
        (t) =>
          `<button type="button" data-shop-tab="${t}">${t < 0 ? '全部' : shopTabNames[t]}<span class="count"></span></button>`,
      )
      .join('');
  for (const b of bar.children) {
    const t = Number(b.dataset.shopTab),
      n = rows.filter((r) => t < 0 || r.tab_type === t).length,
      on = t === shopFilter.tab;
    b.querySelector('.count').textContent = n;
    b.classList.toggle('active', on);
    b.setAttribute('aria-pressed', on);
    b.classList.toggle('none', !n && t >= 0);
  }
  const enabled = rows.filter((r) => r.enabled).length;
  $('#shop-summary').textContent = `上架 ${enabled} / 共 ${rows.length} 项`;
  $('#shop-summary').className = 'pill' + (enabled ? ' green' : '');
}
function applyShopFilter() {
  const q = $('#shop-search').value.trim().toLowerCase(),
    status = $('#shop-status').value,
    rows = $$('#shop-rows tr[data-id]');
  let shown = 0;
  for (const tr of rows) {
    const on = tr.querySelector('.shop-enabled').checked,
      tab = Number(tr.querySelector('[data-shop-field="tab_type"]').value),
      name = (tr.querySelector('[data-shop-field="name"]')?.value.trim() || tr.dataset.name).toLowerCase();
    const show =
      (shopFilter.tab < 0 || tab === shopFilter.tab) &&
      (!status || (status === 'on') === on) &&
      (!q || name.includes(q) || tr.dataset.id.includes(q));
    tr.hidden = !show;
    if (show) shown++;
  }
  const note = $('#shop-filter-note');
  note.hidden = shown === rows.length;
  note.textContent = shown
    ? `已筛选：显示 ${shown} / ${rows.length} 项，保存时包含全部商品。`
    : `没有符合筛选的商品（共 ${rows.length} 项）。`;
  renderShopTabs();
}
function resetShopFilter() {
  shopFilter.tab = -1;
  $('#shop-search').value = '';
  $('#shop-status').value = '';
}
function addShopProducts(rows) {
  runtimeSettings.shopRows = itemShopDraft();
  if (runtimeSettings.shopRows.length + rows.length > 500) throw new Error('商店最多500项商品');
  let id = Math.max(70000000, ...runtimeSettings.shopRows.map((r) => r.lineup_id));
  const added = [];
  for (const product of rows) {
    added.push(++id);
    runtimeSettings.shopRows.push(
      normalizeShopRow({
        lineup_id: id,
        enabled: false,
        price: 1000,
        item_id: product.item_id,
        buy_type: product.buy_type,
        name: product.name,
        tab_type:
          product.buy_type === 2 ? 2 : product.buy_type >= 3 ? 4 : product.item_type === 'BOSS_KEY' ? 1 : 0,
        pay_type: 1,
        quantity: product.buy_type >= 3 ? 5 : 1,
        buy_num_max: product.buy_type === 2 ? 1 : 99,
      }),
    );
  }
  resetShopFilter();
  renderItemShop();
  itemShopChanged();
  const fresh = added.map((id) => $(`#shop-rows tr[data-id="${id}"]`)).filter(Boolean);
  fresh.forEach((tr) => tr.classList.add('fresh'));
  fresh.at(-1)?.scrollIntoView({ block: 'center', behavior: 'smooth' });
  if (added.length) toast(`已添加 ${added.length} 项商品（默认下架）。设置价格后上架并保存`);
}
$('#shop-add').onclick = () => {
  if (policyBusy('shop')) return;
  openContentPicker(
    (rows) =>
      addShopProducts(
        rows.map((r) => {
          const kind = r.kind === 'stamp' ? 2 : 1;
          return (
            runtimeSettings.products.find((p) => p.buy_type === kind && p.item_id === r.reward_type_id) || {
              buy_type: kind,
              item_id: r.reward_type_id,
              name: r.name,
            }
          );
        }),
      ),
    ['item', 'stamp'],
  );
};
$('#shop-add-extend').onclick = () => {
  if (policyBusy('shop')) return;
  const kind = Number($('#shop-extend').value);
  addShopProducts([{ buy_type: kind, item_id: 0, name: kind === 3 ? '卡牌仓库扩容' : '卡牌持有上限扩容' }]);
};
$('#shop-tabs').onclick = (e) => {
  const b = e.target.closest('[data-shop-tab]');
  if (!b) return;
  shopFilter.tab = Number(b.dataset.shopTab);
  applyShopFilter();
};
$('#shop-search').oninput = $('#shop-status').onchange = () => applyShopFilter();
$('#shop-rows').oninput = (e) => {
  e.target.classList?.remove('invalid');
  const tr = e.target.closest('tr[data-id]');
  if (tr) updateShopLimit(tr);
  itemShopChanged();
};
$('#shop-rows').onchange = (e) => {
  const tr = e.target.closest('tr[data-id]');
  if (e.target.classList.contains('shop-enabled')) tr.classList.toggle('off', !e.target.checked);
  // Without a period the period amount means nothing, so it is cleared and locked.
  if (e.target.dataset.shopField === 'period') {
    const input = tr.querySelector('[data-shop-field="period_limit"]');
    input.disabled = !e.target.value;
    input.placeholder = e.target.value ? '份数' : '—';
    if (!e.target.value) input.value = '';
    else if (!input.value) input.focus();
    updateShopLimit(tr);
  }
  itemShopChanged();
};
function itemShopDirty() {
  return (
    runtimeSettings.rendered.has('shop') &&
    JSON.stringify(itemShopDraft()) !== JSON.stringify(runtimeSettings.value.item_shop)
  );
}
function itemShopChanged() {
  const dirty = itemShopDirty();
  $('#shop-save').disabled = !dirty || policyBusy('shop');
  setSaveState(
    '#shop-draft-state',
    dirty ? '有未保存的修改' : '已保存，与服务器一致',
    dirty ? 'dirty' : 'ok',
  );
  if (runtimeSettings.rendered.has('shop')) renderShopTabs();
}
async function loadItemShop() {
  acceptRuntimeSettings(await api('/api/settings'), 'shop');
}
$('#shop-save').onclick = async () => {
  if (policyBusy('shop') || !itemShopDirty()) return;
  const expected = runtimeSettings.revisions.shop;
  const itemShop = itemShopDraft(),
    invalid = [];
  clearInvalid($('#shop'));
  for (const row of itemShop) {
    for (const [key, min, max] of [
      ['price', 1, 10000000],
      ['quantity', 1, 1000000],
      ['buy_num_max', 1, 9999],
      ['total_limit', 0, 10000000],
      ['period_limit', 0, 10000000],
    ])
      if (!Number.isInteger(row[key]) || row[key] < min || row[key] > max) invalid.push([row.lineup_id, key]);
    if (row.period_limit > 0 && !row.period) invalid.push([row.lineup_id, 'period']);
    if (row.lineup_id >= 70000001 && !row.name) invalid.push([row.lineup_id, 'name']);
  }
  if (invalid.length) {
    // Filters could hide the rows at fault: show everything before pointing at them.
    resetShopFilter();
    applyShopFilter();
    const fields = invalid
      .map(([id, key]) => $(`#shop-rows tr[data-id="${id}"] [data-shop-field="${key}"]`))
      .filter(Boolean);
    fields.forEach((el) => markInvalid(el));
    fields[0]?.scrollIntoView({ block: 'center', behavior: 'smooth' });
    fields[0]?.focus({ preventScroll: true });
    showViewAlert('shop', {
      title: `不能保存：${invalid.length} 处商品设置无效`,
      message:
        '已用红框标出。单价为1–10000000的整数；每份含与一次最多须为正整数；限购为0–10000000份，留空为不限；新增商品须填写名称。',
    });
    return;
  }
  const old = new Map((runtimeSettings.value.item_shop || []).map((r) => [r.lineup_id, r])),
    pay = (t) => (t === 3 ? '水晶' : '金币');
  const rows = [];
  for (const row of itemShop) {
    const o = old.get(row.lineup_id),
      name = shopProductName(row);
    if (!o) {
      rows.push([
        `新增 ${name}`,
        '—',
        `${row.enabled ? '上架' : '下架'} · ${shopTabNames[row.tab_type]} · ${pay(row.pay_type)} ${num(row.price)} · 每份含 ${num(row.quantity)} 个 · ${shopLimitText(row.total_limit, row.period, row.period_limit)}`,
      ]);
      continue;
    }
    rows.push(
      ...diffRows([
        [`商品 #${row.lineup_id} 名称`, o.name || '', row.name],
        [`${name} 上架`, o.enabled, row.enabled],
        [`${name} 单价（每份）`, o.price, row.price],
        [`${name} 每份含`, o.quantity, row.quantity],
        [`${name} 一次最多`, o.buy_num_max, row.buy_num_max],
        [
          `${name} 每人限购`,
          shopLimitText(o.total_limit || 0, o.period || '', o.period_limit || 0),
          shopLimitText(row.total_limit, row.period, row.period_limit),
        ],
        [`${name} 币种`, pay(o.pay_type), pay(row.pay_type)],
        [`${name} 页签`, shopTabNames[o.tab_type || 0], shopTabNames[row.tab_type]],
      ]),
    );
  }
  if (
    !(await reviewChanges({
      title: '核对道具商店变更',
      sections: [{ title: '商品', rows }],
      note: '玩家下次打开商店时生效；下架与改价不会重置玩家已有的购买计数。',
    }))
  )
    return;
  state.publishing.add('shop');
  updatePolicyControls();
  try {
    acceptRuntimeSettings(
      await api('/api/item-shop', {
        method: 'PUT',
        body: JSON.stringify({ item_shop: itemShop, expected_revision: expected }),
      }),
      'shop',
    );
    clearViewAlert('shop');
    state.loaded.delete('audit');
    toast('道具商店已生效');
  } catch (e) {
    reportError('shop', e);
  } finally {
    state.publishing.delete('shop');
    updatePolicyControls();
    itemShopChanged();
  }
};
// Uploaded pool covers (gacha-covers) and home banners (home-banners) are read from this server or one storage prefix (S3, object storage, CDN).
const coverBaseNormalized = (value) => {
  value = value.trim();
  return value && !value.endsWith('/') ? value + '/' : value;
};
function coverSourceDraft() {
  return {
    source: $('#settings-cover-storage').checked ? 'storage' : 'server',
    base: coverBaseNormalized($('#settings-cover-base').value),
  };
}
function coverSourceSaved() {
  const v = runtimeSettings.value;
  return {
    source: v.gacha_cover_source === 'storage' ? 'storage' : 'server',
    base: v.gacha_cover_base_url || '',
  };
}
const coverSourceName = (source) => (source === 'storage' ? '存储容器' : '本服务器');
function renderCoverSource() {
  const { source, base } = coverSourceDraft();
  // The address stays editable with 本服务器 selected: it is saved first, synced below, and only then switched to.
  $('#settings-cover-example').innerHTML =
    source === 'storage'
      ? `玩家读取卡池封面 <code>${esc(base || 'https://…/')}gacha-covers/&lt;文件名&gt;</code> 与首页轮播图 <code>${esc(base || 'https://…/')}home-banners/&lt;文件名&gt;</code>。使用前请在下方同步正在使用的图片并检查公开地址；之后新上传或更换的图片也要再同步一次。`
      : '玩家从本服务器读取：卡池封面在 <code>/local/gacha-covers/</code>，首页轮播图在 <code>/local/home-banners/</code>，文件分别保存在存档所在目录下的同名文件夹。要改用存储容器，先填写地址并保存，再在下方同步图片，全部可以读取后切换。';
}
// New-account creation: the page shows and sends the server's own values. A 0 limit pauses creation and
// 0 bits turns verification off, so an empty or malformed box is an error to fix, never a default.
const creationLimitText = (n) => (n === 0 ? '0（暂停创建新账号）' : `${num(n)} 个／天`);
// Verification is 0 (off) or 16–32 leading zero bits; the bands only say how hard a strength is on phones.
const creationLevel = (bits) =>
  bits <= 20 ? ['常规', 'green'] : bits <= 24 ? ['较高', 'gold'] : ['很高', 'red'];
const creationBitsText = (bits) => (bits === 0 ? '关闭' : `${bits} bit（${creationLevel(bits)[0]}）`);
// One solve may run this long in game (same rule as the client and the challenge validity on the server):
// 2 minutes up to 20 bits, doubling per bit, at most 30 minutes.
const creationBudget = (bits) => Math.min(120 * 2 ** Math.max(0, bits - 20), 1800);
const creationBudgetText = (s) => (s < 60 ? `${s} 秒` : `${s / 60} 分钟`);
// The game's solver measured on Unity 6 Mono (32-bit) on an i9-13900KF desktop: about 3.36 million tries/s.
const creationPcRate = 3.36e6;
const creationWorkText = (n) =>
  n < 1e5
    ? `约 ${(n / 1e4).toFixed(1)} 万次`
    : n < 1e8
      ? `约 ${Math.round(n / 1e4)} 万次`
      : `约 ${(n / 1e8).toFixed(1)} 亿次`;
const creationTimeText = (s) =>
  s < 1
    ? '不到 1 秒'
    : s < 59.5
      ? `约 ${Math.round(s)} 秒`
      : s < 600
        ? `约 ${Number((s / 60).toFixed(1))} 分钟`
        : `约 ${Math.round(s / 60)} 分钟`;
function creationLimitDraft() {
  const input = $('#settings-creation-limit'),
    text = input.value.trim();
  if (!text && !input.validity.badInput)
    return { value: null, error: '请填写每日上限，0 表示暂停创建新账号' };
  if (/^-\d/.test(text)) return { value: null, error: '每日上限不能小于 0' };
  if (!/^\d+$/.test(text)) return { value: null, error: '每日上限须为整数' };
  const value = Number(text);
  return value > 10000 ? { value: null, error: '每日上限最多为 10000' } : { value, error: '' };
}
// The slider always holds a valid strength; switched off, the draft is 0 and the slider keeps its place.
const creationBitsDraft = () =>
  $('#settings-creation-verification').checked ? Number($('#settings-creation-bits').value) : 0;
// What saving the drafted limit or strength changes; shown under the control and again in the review.
function creationLimitNote(value, before) {
  if (value === null || value === before) return '';
  if (value === 0) return '将暂停创建新账号。已有账号登录和绑定已有游客角色不受此建号配额影响。';
  if (before === 0) return '保存后恢复创建新账号。修改上限不会清空今天已创建的数量。';
  return value < before
    ? `修改上限不会清空今天已创建的数量：今天已创建 ${num(value)} 个或更多的 IP，当天不能再创建。`
    : `修改上限不会清空今天已创建的数量：今天已创建 ${num(before)} 个的 IP，还可以再创建 ${num(value - before)} 个。`;
}
function creationBitsNote(bits, before, paused) {
  if (paused && bits > 0) return '目前暂停创建新账号；验证设置会保留，恢复创建后生效。';
  const notes = [];
  if (before === 0 && bits > 0)
    notes.push('开启前请确认玩家已更新到支持此验证的客户端；旧版客户端无法完成验证，不能新建角色。');
  if (before > 0 && bits === 0) notes.push('关闭后不再验证，新建角色仍受上面的每日上限限制。');
  if (bits > 24)
    notes.push(
      `强度很高：低性能手机可能在 ${creationBudgetText(creationBudget(bits))}内算不完，届时无法新建角色。`,
    );
  else if (bits > 20) notes.push('强度较高：手机上的等待时间尚未实测，可能明显变长。');
  return notes.join('');
}
function renderCreationStrength(bits) {
  const panel = $('#settings-creation-strength');
  panel.hidden = bits === 0;
  if (bits === 0) return;
  const [level, tone] = creationLevel(bits);
  panel.dataset.level = tone;
  panel.style.setProperty('--pow-fill', String((bits - 16) / 16));
  setMarkup($('#settings-creation-bits-value'), `<b>${bits}</b> bit`);
  const pill = $('#settings-creation-bits-level');
  pill.textContent = level;
  pill.className = 'pill ' + tone;
  $('#settings-creation-bits').setAttribute('aria-valuetext', `${bits} bit，${level}`);
  $('#settings-creation-bits-work').textContent = creationWorkText(2 ** bits);
  $('#settings-creation-bits-pc').textContent = creationTimeText(2 ** bits / creationPcRate);
  $('#settings-creation-bits-budget').textContent = creationBudgetText(creationBudget(bits));
}
function setCreationError(input, message, error) {
  const shown = !!error && input.dataset.touched === '1';
  if (shown) markInvalid(input, error);
  else {
    input.classList.remove('invalid');
    input.removeAttribute('aria-invalid');
    input.removeAttribute('title');
  }
  message.hidden = !shown;
  message.textContent = shown ? error : '';
}
function setCreationNote(el, text, warn) {
  el.hidden = !text;
  el.textContent = text;
  el.classList.toggle('warn-text', warn);
}
// Errors show once the operator has typed in a box or tried to save; a fresh load shows none.
function renderCreationDraft() {
  const saved = runtimeSettings.value,
    limit = creationLimitDraft(),
    bits = creationBitsDraft();
  setCreationError($('#settings-creation-limit'), $('#settings-creation-limit-error'), limit.error);
  renderCreationStrength(bits);
  setCreationNote(
    $('#settings-creation-limit-note'),
    creationLimitNote(limit.value, saved.account_creation_daily_limit),
    limit.value === 0,
  );
  const paused = (limit.value ?? saved.account_creation_daily_limit) === 0;
  setCreationNote(
    $('#settings-creation-verification-note'),
    creationBitsNote(bits, saved.account_creation_verification_bits, paused),
    bits > 20,
  );
}
function runtimeSettingsDirty() {
  if (!runtimeSettings.rendered.has('settings')) return false;
  const cover = coverSourceDraft(),
    saved = coverSourceSaved(),
    v = runtimeSettings.value;
  return (
    $('#settings-crystal').checked !== v.crystal_purchase_enabled ||
    Number($('#settings-battle-speed').value) !== v.team_battle_speed ||
    creationLimitDraft().value !== v.account_creation_daily_limit ||
    creationBitsDraft() !== v.account_creation_verification_bits ||
    cover.source !== saved.source ||
    cover.base !== saved.base
  );
}
function settingsChanged() {
  const dirty = runtimeSettingsDirty();
  $('#settings-save').disabled = !dirty || policyBusy('settings');
  setSaveState(
    '#settings-draft-state',
    dirty ? '有未保存的修改' : '已保存，与服务器一致',
    dirty ? 'dirty' : 'ok',
  );
  if (typeof imageSyncSettingsChanged === 'function') imageSyncSettingsChanged();
}
function renderRuntimeSettings() {
  $('#settings-crystal').checked = runtimeSettings.value.crystal_purchase_enabled;
  $('#settings-battle-speed').value = String(runtimeSettings.value.team_battle_speed);
  const limit = runtimeSettings.value.account_creation_daily_limit;
  $('#settings-creation-limit').value = String(limit);
  delete $('#settings-creation-limit').dataset.touched;
  $('#settings-creation-state').hidden = limit !== 0;
  // Switched off, the slider waits at 18 bit, the strength tested first on PC.
  const bits = runtimeSettings.value.account_creation_verification_bits;
  $('#settings-creation-verification').checked = bits > 0;
  $('#settings-creation-bits').value = String(bits > 0 ? bits : 18);
  renderCreationDraft();
  $('#settings-state').textContent = runtimeSettings.value.crystal_purchase_enabled ? '已开启' : '已关闭';
  $('#settings-state').className = 'pill ' + (runtimeSettings.value.crystal_purchase_enabled ? 'green' : '');
  const cover = coverSourceSaved();
  $(cover.source === 'storage' ? '#settings-cover-storage' : '#settings-cover-server').checked = true;
  $('#settings-cover-base').value = cover.base;
  $('#settings-cover-state').textContent = coverSourceName(cover.source);
  $('#settings-cover-state').className = 'pill' + (cover.source === 'storage' ? ' green' : '');
  renderCoverSource();
  runtimeSettings.rendered.add('settings');
}
const settingsPart = (v) => ({
  crystal_purchase_enabled: !!v.crystal_purchase_enabled,
  team_battle_speed: v.team_battle_speed,
  account_creation_daily_limit: v.account_creation_daily_limit,
  account_creation_verification_bits: v.account_creation_verification_bits,
  gacha_cover_source: v.gacha_cover_source || 'server',
  gacha_cover_base_url: v.gacha_cover_base_url || '',
});
// Keep each dirty page's comparison baseline and expected revision. An update to
// the other section may advance its revision only when its saved section agrees.
function acceptRuntimeSettings(data, view) {
  if (data.revision < runtimeSettings.revision) {
    if (!runtimeSettings.rendered.has(view)) {
      if (view === 'settings') renderRuntimeSettings();
      else {
        runtimeSettings.shopRows = structuredClone(runtimeSettings.value.item_shop);
        runtimeSettings.rendered.add('shop');
        renderItemShop();
      }
      settingsChanged();
      itemShopChanged();
    }
    return;
  }
  const redraw = ['settings', 'shop'].filter(
    (v) =>
      v === view ||
      (runtimeSettings.rendered.has(v) && !(v === 'settings' ? runtimeSettingsDirty() : itemShopDirty())),
  );
  runtimeSettings.catalog = data.item_shop_catalog || runtimeSettings.catalog;
  runtimeSettings.products = data.item_shop_products || runtimeSettings.products;
  const incoming = { ...data.settings, item_shop: (data.settings.item_shop || []).map(normalizeShopRow) },
    before = runtimeSettings.value;
  const next = { ...incoming };
  for (const section of ['settings', 'shop']) {
    const same =
      !before ||
      JSON.stringify(section === 'shop' ? before.item_shop : settingsPart(before)) ===
        JSON.stringify(section === 'shop' ? incoming.item_shop : settingsPart(incoming));
    if (!runtimeSettings.rendered.has(section) || redraw.includes(section) || same)
      runtimeSettings.revisions[section] = data.revision;
    if (before && runtimeSettings.rendered.has(section) && !redraw.includes(section)) {
      if (section === 'shop') next.item_shop = before.item_shop;
      else Object.assign(next, settingsPart(before));
    }
  }
  runtimeSettings.value = next;
  runtimeSettings.revision = data.revision;
  if (redraw.includes('settings')) renderRuntimeSettings();
  if (redraw.includes('shop')) {
    runtimeSettings.shopRows = structuredClone(runtimeSettings.value.item_shop);
    runtimeSettings.rendered.add('shop');
    renderItemShop();
  }
  for (const section of ['settings', 'shop'])
    $('#' + section + '-version').textContent = `版本 ${runtimeSettings.revisions[section]}`;
  settingsChanged();
  itemShopChanged();
}
async function loadRuntimeSettings() {
  acceptRuntimeSettings(await api('/api/settings'), 'settings');
}
$('#settings-crystal').onchange = settingsChanged;
$$('input[name="settings-cover-source"]').forEach(
  (input) =>
    (input.onchange = () => {
      renderCoverSource();
      settingsChanged();
      if (input.value === 'storage' && !$('#settings-cover-base').value) $('#settings-cover-base').focus();
    }),
);
$('#settings-cover-base').oninput = () => {
  $('#settings-cover-base').classList.remove('invalid');
  renderCoverSource();
  settingsChanged();
};
$('#settings-battle-speed').onchange = settingsChanged;
$('#settings-creation-limit').oninput = (e) => {
  e.target.dataset.touched = '1';
  renderCreationDraft();
  settingsChanged();
};
$('#settings-creation-verification').onchange = () => {
  renderCreationDraft();
  settingsChanged();
};
$('#settings-creation-bits').oninput = () => {
  renderCreationDraft();
  settingsChanged();
};
$('#settings-save').onclick = async () => {
  if (policyBusy('settings') || !runtimeSettingsDirty()) return;
  const expected = runtimeSettings.revisions.settings;
  clearInvalid($('#settings'));
  const creation = creationLimitDraft(),
    bits = creationBitsDraft();
  if (creation.error) {
    $('#settings-creation-limit').dataset.touched = '1';
    renderCreationDraft();
    showViewAlert('settings', {
      title: '不能保存：同 IP 每日创建账号上限无效',
      message: `${creation.error}。上限为 0–10000 的整数，0 表示暂停创建新账号。`,
    });
    $('#settings-creation-limit').focus({ preventScroll: true });
    return;
  }
  const cover = coverSourceDraft(),
    savedCover = coverSourceSaved();
  if (
    (cover.source === 'storage' || cover.base) &&
    !/^https?:\/\/[^\s\/?#@]+(\/[^\s?#]*)?$/.test(cover.base)
  ) {
    markInvalid($('#settings-cover-base'), '存储容器地址须为 http(s):// 开头的完整地址');
    showViewAlert('settings', {
      title: '不能保存：存储容器地址无效',
      message:
        '请填写 http(s):// 开头的完整地址，例如 https://cdn.example.com/kairisei/，不含账号和查询参数。',
    });
    return;
  }
  const crystal = $('#settings-crystal').checked,
    speed = Number($('#settings-battle-speed').value),
    before = runtimeSettings.value;
  const coverRows = diffRows([
    ['图片来源', coverSourceName(savedCover.source), coverSourceName(cover.source)],
    ['存储容器地址', savedCover.base || '—', cover.base || '—'],
  ]);
  // Switching to an address the sync below has not proven readable leaves blank images in game.
  const unchecked =
    cover.source === 'storage' && !(imageSync.data?.base_url === cover.base && imageSyncReady());
  const coverNote =
    cover.source === 'storage'
      ? unchecked
        ? '还没有确认正在使用的图片都能从这个地址读取，读不到的图片在游戏里会显示为空白。建议先保持「本服务器」只保存地址，在下方同步并检查后再切换。'
        : '图片来源保存后，玩家下次打开扭蛋页或回到首页时生效。'
      : savedCover.source === 'server'
        ? '玩家仍从本服务器读取；保存地址后，可在下方同步图片。'
        : '图片来源保存后，玩家下次打开扭蛋页或回到首页时生效。';
  // One note for each changed setting; pausing account creation leads the review instead.
  const limitBefore = before.account_creation_daily_limit,
    bitsBefore = before.account_creation_verification_bits,
    pausing = creation.value === 0 && limitBefore !== 0;
  const notes = [
    cover.source !== savedCover.source || cover.base !== savedCover.base ? coverNote : '',
    crystal !== before.crystal_purchase_enabled
      ? crystal
        ? '开启后新订单按商品数量增加水晶；关闭期间的订单不会补发。'
        : '关闭后玩家完成购买但水晶余额不变。'
      : '',
    speed !== before.team_battle_speed
      ? '组队倍速只影响新建房间；不限时使用房主客户端设置，指定倍速时统一使用后台设置。'
      : '',
    pausing ? '' : creationLimitNote(creation.value, limitBefore),
    bits !== bitsBefore || (pausing && bits > 0)
      ? creationBitsNote(bits, bitsBefore, creation.value === 0)
      : '',
  ];
  const ruleRows = diffRows([
    ['允许购买增加水晶', before.crystal_purchase_enabled, crystal],
    ['组队演出倍速', settingsBattleSpeedText(before.team_battle_speed), settingsBattleSpeedText(speed)],
    ['同 IP 每日创建账号上限', creationLimitText(limitBefore), creationLimitText(creation.value)],
    ['新账号创建验证', creationBitsText(bitsBefore), creationBitsText(bits)],
  ]);
  const lead = pausing
    ? `${creationLimitNote(0, limitBefore)}共 ${ruleRows.length + coverRows.length} 处变化，请逐项核对。`
    : '';
  if (
    !(await reviewChanges({
      title: '核对运营设置变更',
      lead,
      sections: [
        { title: '游戏规则', rows: ruleRows },
        { title: '上传图片', rows: coverRows },
      ],
      note: notes.filter(Boolean).join(' '),
      confirmText: pausing ? '保存并暂停创建' : '确认保存',
    }))
  )
    return;
  state.publishing.add('settings');
  updatePolicyControls();
  try {
    // The shop is omitted: the server keeps it, and 道具商店 saves it on its own page.
    acceptRuntimeSettings(
      await api('/api/settings', {
        method: 'PUT',
        body: JSON.stringify({
          crystal_purchase_enabled: crystal,
          team_battle_speed: speed,
          account_creation_daily_limit: creation.value,
          account_creation_verification_bits: bits,
          gacha_cover_source: cover.source,
          gacha_cover_base_url: cover.base,
          expected_revision: expected,
        }),
      }),
      'settings',
    );
    clearViewAlert('settings');
    state.loaded.delete('audit');
    toast('运营设置已生效');
  } catch (e) {
    reportError('settings', e);
  } finally {
    state.publishing.delete('settings');
    updatePolicyControls();
    settingsChanged();
  }
};
