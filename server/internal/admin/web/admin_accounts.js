'use strict';

const accountPage = { page: 0, total: 0, request: 0 };
const selectedPlayers = new Map();
const auditPage = { page: 0, total: 0, request: 0, rows: [] };
const formatTime = (value) => (value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—');
const playerLabel = (a) => `${a.name || a.username || '未命名玩家'} · #${a.user_id}`;
const isSystemPartner = (id) => id >= 1900000000;
function relativeTime(value) {
  if (!value) return '';
  const s = (Date.now() - new Date(value).getTime()) / 1000;
  if (!Number.isFinite(s)) return '';
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} 分钟前`;
  if (s < 86400) return `${Math.round(s / 3600)} 小时前`;
  if (s < 86400 * 60) return `${Math.round(s / 86400)} 天前`;
  return '';
}
function downloadJSON(name, value) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

async function loadAccountsPage() {
  const serial = ++accountPage.request;
  const query = new URLSearchParams({ ...accountFilterQuery(), limit: 50, offset: accountPage.page * 50 });
  const data = await api('/api/accounts?' + query);
  if (serial !== accountPage.request) return;
  accountPage.total = data.total;
  state.accounts = data.accounts;
  if (accountPage.page > 0 && accountPage.page * 50 >= data.total) {
    accountPage.page = Math.max(0, Math.ceil(data.total / 50) - 1);
    return loadAccountsPage();
  }
  renderAccounts();
}
function renderAccounts() {
  $('#account-count').textContent = `筛选结果 ${num(accountPage.total)} 个账号`;
  $('#account-rows').innerHTML =
    state.accounts
      .map((a) => {
        const system = isSystemPartner(a.user_id),
          on = selectedPlayers.has(a.user_id),
          since = relativeTime(a.last_login_utc);
        return `<tr class="${on ? 'selected' : ''}"><td>${system ? '' : `<input class="check player-check" type="checkbox" aria-label="选择${esc(playerLabel(a))}" data-id="${a.user_id}" ${on ? 'checked' : ''}>`}</td><td class="cell-wrap"><b>${esc(a.name || '未命名')}</b>${typeof accountAccessTags === 'function' ? accountAccessTags(a) : ''}<span class="sub"><code>#${a.user_id}</code> 存档 r${a.revision}</span></td><td>Lv.${a.level}<span class="sub">${esc(cardJobName(a.arthur_type))}</span></td><td class="num">${num(a.gold)}<span class="sub">水晶 ${num(a.crystals)}</span></td><td>${a.username ? `<span class="tag ok">已绑定</span> ${esc(a.username)}` : system ? '<span class="tag">系统伙伴</span>' : '<span class="tag">游客</span>'}<details><summary class="sub">安装标识</summary><code>${esc(a.login_uuid)}</code></details></td><td>${esc(formatTime(a.last_login_utc))}<span class="sub">${since ? esc(since) + ' · ' : ''}注册 ${esc(formatTime(a.created_utc))}</span></td><td><div class="row-actions"><button class="ghost account-detail" data-user="${a.user_id}">详情</button>${system ? '' : `<button class="ghost mail-shortcut" data-user="${a.user_id}">寄礼物</button><button class="ghost inbox-open" data-user="${a.user_id}">邮件</button><button class="ghost grant" data-user="${a.user_id}">加资源</button><button class="ghost credentials" data-user="${a.user_id}">${a.username ? '管理绑定' : '绑定账号'}</button>${typeof accountBlockButton === 'function' ? accountBlockButton(a) : ''}`}</div></td></tr>`;
      })
      .join('') || emptyRow(7, '没有匹配的账号', '调整搜索词或点击“清除筛选”。');
  let anchor = null;
  $$('.player-check').forEach(
    (box, index) =>
      (box.onclick = (e) => {
        recipientScope = null;
        const boxes = $$('.player-check'),
          indices =
            e.shiftKey && anchor !== null
              ? [Math.min(anchor, index), Math.max(anchor, index)]
              : [index, index];
        for (let i = indices[0]; i <= indices[1]; i++) {
          const a = state.accounts.find((a) => a.user_id === Number(boxes[i].dataset.id));
          if (box.checked) {
            if (selectedPlayers.size >= 500 && !selectedPlayers.has(a.user_id)) {
              toast('每批最多500名玩家，请缩小范围', true);
              break;
            }
            selectedPlayers.set(a.user_id, a);
          } else selectedPlayers.delete(a.user_id);
          boxes[i].checked = selectedPlayers.has(a.user_id);
          boxes[i].closest('tr').classList.toggle('selected', boxes[i].checked);
        }
        anchor = index;
        updatePlayerSelection();
      }),
  );
  $$('.account-detail').forEach((b) => (b.onclick = () => openAccountDetail(Number(b.dataset.user))));
  $$('.inbox-open').forEach((b) => (b.onclick = () => openPlayerInbox(Number(b.dataset.user))));
  $$('.credentials').forEach((b) => (b.onclick = () => openCredentials(Number(b.dataset.user))));
  $$('.grant').forEach((b) => (b.onclick = () => openGrant(Number(b.dataset.user))));
  $$('.mail-shortcut').forEach(
    (b) =>
      (b.onclick = async () => {
        const a = state.accounts.find((a) => a.user_id === Number(b.dataset.user)),
          count = mailRecipientCount();
        if (
          count &&
          !(selectedPlayers.size === 1 && selectedPlayers.has(a.user_id)) &&
          !(await askConfirm(
            `改为只给「${playerLabel(a)}」寄礼物？`,
            `当前已选 ${count} 名玩家，这些选择会被替换。`,
            { ok: '替换收件人' },
          ))
        )
          return;
        recipientScope = null;
        selectedPlayers.clear();
        selectedPlayers.set(a.user_id, a);
        updatePlayerSelection();
        switchView('mail');
      }),
  );
  renderPager('accounts', accountPage.page, accountPage.total, 50, (page) => {
    accountPage.page = page;
    loadAccountsPage().catch((e) => reportError('accounts', e, '载入'));
  });
  updatePlayerSelection();
}
function updatePlayerSelection() {
  const all = typeof recipientScope !== 'undefined' && !!recipientScope,
    n = all ? recipientScope.count : selectedPlayers.size;
  $('#accounts-selection').textContent = all ? `已选全部玩家（当前 ${n} 名）` : `已选 ${n} 名玩家`;
  $('#accounts-mail').disabled = !n;
  $('#accounts-clear').disabled = !n;
  if (typeof renderMailRecipients === 'function') renderMailRecipients();
  if (typeof updateMailControls === 'function') updateMailControls();
}
async function selectFilteredPlayers() {
  const filter = accountFilterQuery(),
    selected = [];
  if (filter.binding === 'system') throw new Error('系统伙伴不能作为赠礼收件人');
  filter.include_system = '0';
  for (let offset = 0; ; ) {
    const data = await api('/api/accounts?' + new URLSearchParams({ ...filter, limit: 200, offset }));
    if (data.total > 500)
      throw new Error(`筛选结果有 ${data.total} 名玩家，超过每批500人上限，请缩小筛选范围`);
    selected.push(...data.accounts);
    offset += data.accounts.length;
    if (offset >= data.total) break;
    if (!data.accounts.length) throw new Error('账号列表变化，请重试');
  }
  const merged = new Set([...selectedPlayers.keys(), ...selected.map((a) => a.user_id)]).size;
  if (merged > 500) throw new Error(`与原选择合计 ${merged} 人，超过500人，请先清空选择`);
  if (
    !(await askConfirm(
      `选中全部 ${selected.length} 名筛选结果？`,
      `不含系统伙伴。已有选择保留，合计 ${merged} 人。`,
      { ok: '全部选中' },
    ))
  )
    return;
  recipientScope = null;
  selected.forEach((a) => selectedPlayers.set(a.user_id, a));
  renderAccounts();
  toast(`已选中 ${selected.length} 名筛选结果中的玩家`);
}
$('#accounts-select-page').onclick = () => {
  const rows = state.accounts.filter((a) => !isSystemPartner(a.user_id));
  if (new Set([...selectedPlayers.keys(), ...rows.map((a) => a.user_id)]).size > 500)
    return toast('每批最多500人', true);
  recipientScope = null;
  rows.forEach((a) => selectedPlayers.set(a.user_id, a));
  renderAccounts();
};
$('#accounts-select-filter').onclick = async () => {
  const b = $('#accounts-select-filter');
  b.disabled = true;
  try {
    await selectFilteredPlayers();
  } catch (e) {
    toast(e.message, true);
  } finally {
    b.disabled = false;
  }
};
$('#accounts-clear').onclick = async () => {
  if (
    mailRecipientCount() > 10 &&
    !(await askConfirm(`清空已选的 ${mailRecipientCount()} 名收件人？`, '包括在其他页选中的玩家。', {
      ok: '清空',
      danger: true,
    }))
  )
    return;
  recipientScope = null;
  selectedPlayers.clear();
  renderAccounts();
};
$('#accounts-mail').onclick = () => switchView('mail');
$('#mail-select-players').onclick = () => switchView('accounts');
$('#account-search').oninput = () => {
  accountPage.page = 0;
  accountPage.request++;
  clearTimeout(loadAccountsPage.timer);
  loadAccountsPage.timer = setTimeout(
    () => loadAccountsPage().catch((e) => reportError('accounts', e, '载入')),
    200,
  );
};
$('#account-system').onchange = () => {
  accountPage.page = 0;
  loadAccountsPage().catch((e) => reportError('accounts', e, '载入'));
};

// Saved filters live in this browser only. Relative presets are stored by name so "7天未登录" stays relative.
const accountFilterKey = 'kairisei-admin-account-filters';
const accountFilterFields = [
  'account-search',
  'account-binding',
  'account-login-block',
  'account-job',
  'account-level-min',
  'account-level-max',
  'account-sort',
  'account-created-after',
  'account-created-before',
  'account-login-after',
  'account-login-before',
];
function savedAccountFilters() {
  try {
    return JSON.parse(localStorage.getItem(accountFilterKey) || '[]');
  } catch {
    return [];
  }
}
function renderSavedAccountFilters() {
  const list = savedAccountFilters();
  $('#account-saved').innerHTML =
    '<option value="">常用筛选…</option>' +
    list.map((f, i) => `<option value="${i}">${esc(f.name)}</option>`).join('');
  $('#account-saved-delete').disabled = true;
}
$('#account-save-filter').onclick = async () => {
  const name = await askText(
    '保存常用筛选',
    {
      label: '名称',
      value: `筛选 ${new Date().toLocaleDateString('zh-CN')}`,
      message: '只保存在本浏览器，之后可从「常用筛选」直接选用。',
    },
    { ok: '保存' },
  );
  if (!name) return;
  const values = Object.fromEntries(accountFilterFields.map((id) => [id, $('#' + id).value]));
  if (state.accountPreset)
    for (const id of [
      'account-created-after',
      'account-created-before',
      'account-login-after',
      'account-login-before',
    ])
      values[id] = '';
  const list = savedAccountFilters().filter((f) => f.name !== name);
  list.push({
    name,
    values,
    preset: state.accountPreset || '',
    active: $('#account-active').checked,
    system: $('#account-system').checked,
  });
  try {
    localStorage.setItem(accountFilterKey, JSON.stringify(list.slice(-30)));
    renderSavedAccountFilters();
    toast(`已保存常用筛选「${name}」`);
  } catch {
    toast('浏览器存储不可用，无法保存常用筛选', true);
  }
};
$('#account-saved').onchange = () => {
  const f = savedAccountFilters()[Number($('#account-saved').value)];
  $('#account-saved-delete').disabled = !f;
  if (!f) return;
  for (const id of accountFilterFields) $('#' + id).value = f.values[id] ?? '';
  $('#account-active').checked = !!f.active;
  $('#account-system').checked = !!f.system;
  state.accountPreset = '';
  if (f.preset) {
    const button = $(`#account-presets [data-preset="${f.preset}"]`);
    if (button) {
      button.click();
      return;
    }
  }
  renderAccountPresets();
  reloadAccounts();
};
$('#account-saved-delete').onclick = async () => {
  const list = savedAccountFilters(),
    i = Number($('#account-saved').value),
    f = list[i];
  if (!f || !(await askConfirm(`删除常用筛选「${f.name}」？`, '', { ok: '删除', danger: true }))) return;
  list.splice(i, 1);
  try {
    localStorage.setItem(accountFilterKey, JSON.stringify(list));
  } catch {}
  renderSavedAccountFilters();
};
renderSavedAccountFilters();
// CSV of the current filter result (all pages); cells are neutralized against spreadsheet formulas.
$('#account-export').onclick = async () => {
  const button = $('#account-export');
  let filter;
  try {
    filter = accountFilterQuery();
  } catch (e) {
    return toast(e.message, true);
  }
  if (
    accountPage.total > 2000 &&
    !(await askConfirm(`导出 ${accountPage.total} 个账号？`, '账号较多，导出需要一些时间。', { ok: '导出' }))
  )
    return;
  button.disabled = true;
  try {
    const rows = [];
    for (let offset = 0; ; ) {
      const data = await api('/api/accounts?' + new URLSearchParams({ ...filter, limit: 200, offset }));
      if (data.total > 20000 || rows.length + data.accounts.length > 20000)
        throw new Error('每次最多导出20000个账号，请缩小筛选范围');
      rows.push(...data.accounts);
      offset += data.accounts.length;
      if (offset >= data.total) break;
      if (!data.accounts.length) throw new Error('账号列表变化，导出未完成，请重试');
    }
    const cell = (v) => {
      let s = String(v ?? '');
      if (/^[=+\-@\t\r]/.test(s)) s = "'" + s;
      return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
    };
    const header = [
      '玩家ID',
      '角色名',
      '登录账号',
      '账号类型',
      '等级',
      '当前职业',
      '金币',
      '水晶',
      '注册时间',
      '最近登录',
      '存档版本',
    ];
    const lines = [
      header,
      ...rows.map((a) => [
        a.user_id,
        a.name,
        a.username,
        isSystemPartner(a.user_id) ? '系统伙伴' : a.username ? '已绑定' : '游客',
        a.level,
        cardJobName(a.arthur_type),
        a.gold,
        a.crystals,
        formatTime(a.created_utc),
        formatTime(a.last_login_utc),
        a.revision,
      ]),
    ].map((r) => r.map(cell).join(','));
    const stamp = new Date().toISOString().slice(0, 16).replace(/[-:T]/g, '');
    const url = URL.createObjectURL(
      new Blob(['\ufeff' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' }),
    );
    const a = document.createElement('a');
    a.href = url;
    a.download = `玩家筛选结果-${stamp}.csv`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    toast(`已导出 ${rows.length} 个账号`);
  } catch (e) {
    toast(errorText(e), true);
  } finally {
    button.disabled = false;
  }
};

const auditLabel = (op) => $(`#audit-operation option[value="${CSS.escape(op)}"]`)?.textContent || op;
async function loadAuditPage() {
  const serial = ++auditPage.request;
  const query = {
    q: $('#audit-search').value.trim(),
    operation: $('#audit-operation').value,
    limit: 50,
    offset: auditPage.page * 50,
  };
  for (const [key, id] of [
    ['after', '#audit-after'],
    ['before', '#audit-before'],
  ])
    if ($(id).value) {
      const stamp = new Date($(id).value);
      if (!Number.isFinite(stamp.getTime())) throw new Error('时间范围无效');
      query[key] = stamp.toISOString();
    }
  const data = await api('/api/audit?' + new URLSearchParams(query));
  if (serial !== auditPage.request) return;
  if (auditPage.page > 0 && auditPage.page * 50 >= data.total) {
    auditPage.page = Math.max(0, Math.ceil(data.total / 50) - 1);
    return loadAuditPage();
  }
  auditPage.rows = data.records;
  auditPage.total = data.total;
  $('#audit-rows').innerHTML =
    data.records
      .map(
        (r) =>
          `<tr><td><code>#${r.audit_id}</code><span class="sub">${esc(formatTime(r.created_utc))}</span></td><td>${esc(auditLabel(r.operation))}<span class="sub">${esc(r.operation)}</span></td><td class="cell-wrap">${esc(r.target)}</td><td><details><summary>查看内容</summary><pre class="audit-details">${esc(JSON.stringify(r.payload, null, 2))}</pre></details></td></tr>`,
      )
      .join('') || emptyRow(4, '暂无匹配操作', '调整关键词、操作类型或时间范围。');
  renderPager('audit', auditPage.page, data.total, 50, (page) => {
    auditPage.page = page;
    loadAuditPage().catch((e) => reportError('audit', e, '载入'));
  });
}
const reloadAudit = () => {
  auditPage.page = 0;
  loadAuditPage().catch((e) => reportError('audit', e, '载入'));
};
$('#audit-search').oninput = () => {
  auditPage.page = 0;
  auditPage.request++;
  clearTimeout(loadAuditPage.timer);
  loadAuditPage.timer = setTimeout(() => loadAuditPage().catch((e) => reportError('audit', e, '载入')), 200);
};
$('#audit-operation').onchange = reloadAudit;
$('#audit-after').onchange = $('#audit-before').onchange = () => {
  clearInvalid($('#audit'));
  const a = $('#audit-after').value,
    b = $('#audit-before').value;
  if (a && b && a >= b) {
    markInvalid($('#audit-before'), '结束时间须晚于开始时间');
    return toast('结束时间须晚于开始时间', true);
  }
  reloadAudit();
};
$('#audit-reset').onclick = () => {
  $('#audit-search').value = '';
  $('#audit-operation').value = '';
  $('#audit-after').value = '';
  $('#audit-before').value = '';
  clearInvalid($('#audit'));
  reloadAudit();
};
$('#audit-export').onclick = () => downloadJSON('操作记录.json', auditPage.rows);
function adminPolicyDirty(name) {
  if (typeof editorDirty === 'function' && editorDirty(name)) return true;
  if (name === 'player-policy') return playerPolicyDirty();
  if (name === 'evolution') return typeof evolutionDirty === 'function' && evolutionDirty();
  if (name === 'pool-editor') return poolEditor.dirty;
  if (typeof contentDirty === 'function' && contentDirty(name)) return true;
  if (name === 'settings') return runtimeSettingsDirty();
  if (name === 'shop') return itemShopDirty();
  const normalized = (values) => JSON.stringify([...values].sort((a, b) => a - b));
  if (name === 'bosses' && state.bossCatalog === 'economy')
    return typeof bossEconomyDirty === 'function' && bossEconomyDirty();
  if (name === 'bosses' && state.policy)
    return (
      state.mode !== state.policy.mode ||
      (state.mode === 'allowlist' &&
        normalized(state.selected) !== normalized(state.policy.group_ids || [])) ||
      unixInput('#boss-start') !== (state.policy.start_unix || 0) ||
      unixInput('#boss-end') !== (state.policy.end_unix || 0)
    );
  if (name === 'gachas' && state.gachaPolicy)
    return normalized(state.gachaSelected) !== normalized(state.gachaPolicy.group_ids || []);
  return false;
}

function updateDraftIndicators() {
  $$('.nav [data-view]').forEach((button) => {
    const dirty = adminPolicyDirty(button.dataset.view);
    button.classList.toggle('has-draft', dirty);
    button.title = dirty ? '有未保存的修改，切换页面会保留草稿' : '';
    $(`#${button.dataset.view} > .savebar`)?.classList.toggle('dirty', dirty);
  });
}
document.addEventListener('input', updateDraftIndicators);
document.addEventListener('change', updateDraftIndicators);
document.addEventListener('click', updateDraftIndicators);
window.addEventListener('beforeunload', (event) => {
  if (
    Object.keys(titles).some(adminPolicyDirty) ||
    bossRuleEditor.dirty ||
    state.publishing.size ||
    state.grantBusy ||
    state.grantRequest
  ) {
    event.preventDefault();
    event.returnValue = '';
  }
});

async function openAccountDetail(id) {
  const serial = (openAccountDetail.serial || 0) + 1;
  openAccountDetail.serial = serial;
  $('#account-detail-title').textContent = `账号 #${id}`;
  $('#account-detail-lead').textContent = '正在加载该账号存档…';
  $('#account-detail-body').innerHTML = '';
  $('#account-modal').classList.add('open');
  try {
    const data = await api(`/api/accounts/${id}`);
    if (serial !== openAccountDetail.serial) return;
    const a = data.account;
    $('#account-detail-title').textContent = `${a.name || '未命名账号'} · #${a.user_id}`;
    $('#account-detail-lead').textContent = `${a.login_uuid} · 存档 r${a.revision}`;
    renderAccountFacts(a);
  } catch (e) {
    if (serial !== openAccountDetail.serial) return;
    $('#account-detail-lead').textContent = '详情加载失败：' + errorText(e);
    toast(errorText(e), true);
  }
}
function openCredentials(id) {
  const a = state.accounts.find((x) => x.user_id === id);
  if (!a || isSystemPartner(id)) return;
  state.credentialUser = id;
  $('#credentials-title').textContent = `${a.username ? '管理账号绑定' : '绑定账号'} · ${playerLabel(a)}`;
  $('#credentials-name').value = a.username || '';
  $('#credentials-name').disabled = !!a.username;
  $('#credentials-unbind').hidden = !a.username;
  $('#credentials-password').value = '';
  clearInvalid($('#credentials-modal'));
  $('#credentials-modal').classList.add('open');
}
$('#credentials-cancel').onclick = () => {
  $('#credentials-password').value = '';
  $('#credentials-modal').classList.remove('open');
};
function credentialsBusy(busy) {
  ['apply', 'unbind', 'cancel'].forEach((action) => ($(`#credentials-${action}`).disabled = busy));
}
async function refreshCredentials() {
  $('#credentials-password').value = '';
  $('#credentials-modal').classList.remove('open');
  state.loaded.delete('audit');
  await loadView('accounts', true);
}
$('#credentials-apply').onclick = async () => {
  const body = { username: $('#credentials-name').value, password: $('#credentials-password').value };
  clearInvalid($('#credentials-modal'));
  const badName = !/^[a-zA-Z0-9_]{3,32}$/.test(body.username.trim()),
    badPassword = body.password.length < 8;
  if (badName || badPassword) {
    if (badName) markInvalid($('#credentials-name'));
    if (badPassword) markInvalid($('#credentials-password'));
    toast(badName ? '账号名须为3–32位字母、数字或下划线' : '密码至少8位', true);
    return;
  }
  credentialsBusy(true);
  try {
    await api(`/api/accounts/${state.credentialUser}/credentials`, {
      method: 'POST',
      body: JSON.stringify(body),
    });
    await refreshCredentials();
    toast('账号凭据已保存，角色进度保持不变');
  } catch (e) {
    toast(errorText(e), true);
  } finally {
    credentialsBusy(false);
  }
};
$('#credentials-unbind').onclick = async () => {
  const id = state.credentialUser,
    username = $('#credentials-name').value;
  if (
    !(await askConfirm(
      `解除账号“${username}”与角色 #${id} 的绑定？`,
      '原账号密码将失效，角色存档保留，已登录设备不会退出。解除后可重新绑定。',
      { ok: '解除绑定', danger: true },
    ))
  )
    return;
  credentialsBusy(true);
  try {
    await api(`/api/accounts/${id}/credentials`, { method: 'DELETE', body: JSON.stringify({ username }) });
    await refreshCredentials();
    toast('已解除绑定，可点击“绑定账号”重新绑定；角色存档已保留');
  } catch (e) {
    toast(errorText(e), true);
  } finally {
    credentialsBusy(false);
  }
};
function setGrantNote(text, kind = '') {
  const el = $('#grant-note');
  el.textContent = text;
  el.className = 'hint' + (kind ? ` state-${kind}` : '');
}
async function openGrant(id) {
  if (state.grantBusy) return;
  const a = state.accounts.find((x) => x.user_id === id);
  if (!a) return;
  if (
    state.grantRequest &&
    !(await askConfirm('放弃上次发放的重试编号？', '上次发放尚未确认。请先核对账号或审计，再开始新操作。', {
      ok: '放弃并继续',
      danger: true,
    }))
  )
    return;
  state.grantUser = id;
  state.grantRequest = null;
  $('#grant-level').max = state.status?.max_player_level || 999;
  $('#grant-account').textContent =
    `${playerLabel(a)} · 当前 Lv.${a.level} · 金币 ${num(a.gold)} · 水晶 ${num(a.crystals)}`;
  ['gold', 'fp', 'free', 'paid', 'level', 'rank'].forEach((x) => ($(`#grant-${x}`).value = 0));
  $('#grant-ap').checked = false;
  $('#grant-bp').checked = false;
  clearInvalid($('#grant-modal'));
  setGrantNote('确认后生效；网络中断时可用原操作编号重试，不会重复增加。');
  $('#grant-apply').textContent = '确认发放';
  $('#grant-modal').classList.add('open');
}
async function closeGrant() {
  if (state.grantBusy) return;
  if (
    state.grantRequest &&
    !(await askConfirm('关闭发放窗口？', '发放结果尚未确认。关闭后请先核对账号和审计，避免重复操作。', {
      ok: '关闭',
    }))
  )
    return;
  state.grantRequest = null;
  $('#grant-modal').classList.remove('open');
}
$('#grant-cancel').onclick = closeGrant;
$('#grant-modal').onclick = (e) => {
  if (e.target.id === 'grant-modal') closeGrant();
};
$('#grant-apply').onclick = async () => {
  if (state.grantBusy) return;
  if ($$('#grant-modal input').some((el) => !el.reportValidity())) return;
  const id = state.grantUser;
  const body = {
    target_level: Number($('#grant-level').value || 0),
    target_arthur_rank: Number($('#grant-rank').value || 0),
    gold: Number($('#grant-gold').value || 0),
    friend_point: Number($('#grant-fp').value || 0),
    free_crystal: Number($('#grant-free').value || 0),
    paid_crystal: Number($('#grant-paid').value || 0),
    fill_ap: $('#grant-ap').checked,
    fill_bp: $('#grant-bp').checked,
  };
  const signature = JSON.stringify(body);
  if (state.grantRequest && state.grantRequest.signature !== signature)
    return toast('上次请求尚未确认，先用原内容重试或关闭后核对账号', true);
  const lines = [
    ['金币', body.gold],
    ['友情点', body.friend_point],
    ['免费水晶', body.free_crystal],
    ['付费水晶', body.paid_crystal],
    ['目标等级', body.target_level],
    ['历史卡组评价', body.target_arthur_rank],
  ]
    .filter(([, v]) => v > 0)
    .map(([k, v]) => `${k}：${k.startsWith('目标') || k.startsWith('历史') ? '提高至 ' : '+'}${num(v)}`);
  if (body.fill_ap) lines.push('补满探索点');
  if (body.fill_bp) lines.push('补满体力');
  if (!lines.length) return toast('没有填写任何要发放或调整的内容', true);
  if (
    !(await askConfirm(
      `${state.grantRequest ? '用原操作编号重试' : '确认'}向账号 #${id} 发放？`,
      lines.join('\n'),
      { ok: state.grantRequest ? '重试发放' : '确认发放' },
    ))
  )
    return;
  if (!state.grantRequest) state.grantRequest = { signature, key: crypto.randomUUID() };
  body.idempotency_key = state.grantRequest.key;
  state.grantBusy = true;
  $$('#grant-modal input, #grant-modal button').forEach((el) => (el.disabled = true));
  setGrantNote('正在写入存档，请勿关闭页面…');
  try {
    await api(`/api/accounts/${id}/grant`, { method: 'POST', body: JSON.stringify(body) });
    state.grantRequest = null;
    $('#grant-modal').classList.remove('open');
    state.loaded.delete('dashboard');
    state.loaded.delete('accounts');
    state.loaded.delete('audit');
    await loadView('accounts', true);
    toast('资源已写入账号存档');
  } catch (e) {
    setGrantNote(
      `结果未确认：${errorText(e)}。请保持内容不变，点击“重试同一操作”；服务端按原操作编号防重，不会重复增加。`,
      'error',
    );
    $('#grant-apply').textContent = '重试同一操作';
    toast(errorText(e) + '；可用相同内容重试，不会重复增加', true);
  } finally {
    state.grantBusy = false;
    $$('#grant-modal input, #grant-modal button').forEach((el) => (el.disabled = false));
  }
};
$('#account-detail-close').onclick = () => $('#account-modal').classList.remove('open');
$('#account-modal').onclick = (e) => {
  if (e.target.id === 'account-modal') $('#account-modal').classList.remove('open');
};

$('#accounts-unselect-page').onclick = () => {
  state.accounts.forEach((a) => selectedPlayers.delete(a.user_id));
  renderAccounts();
};

const playerInbox = {
  user: 0,
  page: 0,
  total: 0,
  serial: 0,
  busy: false,
  loading: false,
  rows: [],
  selected: new Map(),
};
function updateInboxControls() {
  const w = playerInbox;
  $$('#inbox-modal button, #inbox-modal input, #inbox-modal select').forEach((e) => (e.disabled = w.busy));
  $('#inbox-delete').disabled = w.busy || w.loading || !w.selected.size;
  $('#inbox-delete').textContent = w.selected.size ? `删除所选 ${w.selected.size} 封` : '删除所选邮件';
  $('#inbox-select-page').disabled = w.busy || w.loading || !w.rows.length;
  $('#inbox-prev').disabled = w.busy || w.loading || !w.page;
  $('#inbox-next').disabled = w.busy || w.loading || (w.page + 1) * 50 >= w.total;
  $('#inbox-info').innerHTML =
    `<b>已选 ${w.selected.size} 封</b><span>共 ${w.total} 封 · 第 ${w.page + 1} / ${Math.max(1, Math.ceil(w.total / 50))} 页</span>`;
}
async function openPlayerInbox(id) {
  const w = playerInbox;
  if (w.busy) return;
  clearTimeout(loadPlayerInbox.timer);
  w.user = id;
  w.page = 0;
  w.selected.clear();
  $('#inbox-title').textContent =
    `邮件管理 · ${playerLabel(state.accounts.find((a) => a.user_id === id) || { user_id: id })}`;
  $('#inbox-search').value = '';
  $('#inbox-filter').value = 'pending';
  $('#inbox-modal').classList.add('open');
  await loadPlayerInbox();
}
async function loadPlayerInbox() {
  const w = playerInbox,
    serial = ++w.serial;
  w.loading = true;
  w.rows = [];
  $('#inbox-rows').innerHTML = '<tr><td colspan="5" class="empty">正在加载邮件…</td></tr>';
  updateInboxControls();
  try {
    const query = new URLSearchParams({
      offset: w.page * 50,
      q: $('#inbox-search').value.trim(),
      status: $('#inbox-filter').value,
    });
    const data = await api(`/api/accounts/${w.user}/mail?${query}`);
    if (serial !== w.serial) return;
    w.rows = data.items;
    w.total = data.total;
    if (w.page && w.page * 50 >= w.total) {
      w.page = Math.max(0, Math.ceil(w.total / 50) - 1);
      return loadPlayerInbox();
    }
    $('#inbox-rows').innerHTML =
      w.rows
        .map(
          (p) =>
            `<tr class="${w.selected.has(p.present_id) ? 'selected' : ''}"><td><input class="check inbox-check" type="checkbox" data-id="${esc(p.present_id)}" aria-label="选择${esc(p.title)}" ${w.selected.has(p.present_id) ? 'checked' : ''}></td><td class="cell-wrap"><b>${esc(p.title)}</b><span class="sub">${esc(p.message)}</span><span class="sub">${p.issued_at_unix ? esc(formatTime(p.issued_at_unix * 1000)) : ''}</span></td><td>${esc(p.reward_name || `奖励 ${p.reward_type}/${p.reward_id}`)} × ${esc(p.quantity)}</td><td>${p.state === 0 ? '<span class="tag warn">未领取</span>' : '<span class="tag">已领取</span>'}</td><td><button class="ghost danger-text inbox-delete-one" data-id="${esc(p.present_id)}">删除</button></td></tr>`,
        )
        .join('') || emptyRow(5, '没有符合条件的邮件');
    $$('.inbox-check').forEach(
      (box) =>
        (box.onchange = () => {
          const p = w.rows.find((p) => p.present_id === box.dataset.id);
          if (box.checked) {
            if (w.selected.size >= 100) {
              box.checked = false;
              return toast('每次最多选择 100 封邮件', true);
            }
            w.selected.set(p.present_id, p);
          } else w.selected.delete(p.present_id);
          box.closest('tr').classList.toggle('selected', box.checked);
          updateInboxControls();
        }),
    );
    $$('.inbox-delete-one').forEach(
      (b) => (b.onclick = () => deletePlayerInbox([w.rows.find((p) => p.present_id === b.dataset.id)])),
    );
  } catch (e) {
    if (serial === w.serial) {
      $('#inbox-rows').innerHTML =
        `<tr><td colspan="5" class="empty"><b>邮件加载失败</b>${esc(errorText(e))}；点击“刷新”重试。</td></tr>`;
      toast(errorText(e), true);
    }
  } finally {
    if (serial === w.serial) {
      w.loading = false;
      updateInboxControls();
    }
  }
}
async function deletePlayerInbox(rows) {
  const w = playerInbox;
  if (w.busy || w.loading || !rows.length) return;
  const pending = rows.filter((p) => p.state === 0).length;
  if (
    !(await askConfirm(
      `删除玩家 #${w.user} 的 ${rows.length} 封邮件？`,
      `其中 ${pending} 封尚未领取，删除后无法再领取。已领取奖励不会回收。\n\n${rows
        .slice(0, 8)
        .map((p) => p.title)
        .join('\n')}${rows.length > 8 ? '\n…' : ''}`,
      { ok: '删除', danger: true },
    ))
  )
    return;
  w.busy = true;
  updateInboxControls();
  try {
    const data = await api(`/api/accounts/${w.user}/mail/delete`, {
      method: 'POST',
      body: JSON.stringify({ present_ids: rows.map((p) => p.present_id) }),
    });
    rows.forEach((p) => w.selected.delete(p.present_id));
    state.loaded.delete('audit');
    state.loaded.delete('accounts');
    toast(
      `已删除 ${data.deleted.length} 封邮件${data.already_absent ? `，另有 ${data.already_absent} 封已不存在` : ''}`,
    );
    await loadPlayerInbox();
  } catch (e) {
    toast(errorText(e) + '；刷新列表核对后可重试', true);
  } finally {
    w.busy = false;
    updateInboxControls();
  }
}
$('#inbox-delete').onclick = () => deletePlayerInbox([...playerInbox.selected.values()]);
$('#inbox-close').onclick = () => {
  if (!playerInbox.busy) {
    playerInbox.serial++;
    clearTimeout(loadPlayerInbox.timer);
    $('#inbox-modal').classList.remove('open');
  }
};
$('#inbox-refresh').onclick = () => loadPlayerInbox();
$('#inbox-prev').onclick = () => {
  playerInbox.page--;
  loadPlayerInbox();
};
$('#inbox-next').onclick = () => {
  playerInbox.page++;
  loadPlayerInbox();
};
$('#inbox-filter').onchange = () => {
  playerInbox.page = 0;
  loadPlayerInbox();
};
$('#inbox-search').oninput = () => {
  playerInbox.page = 0;
  playerInbox.serial++;
  clearTimeout(loadPlayerInbox.timer);
  loadPlayerInbox.timer = setTimeout(loadPlayerInbox, 200);
};
$('#inbox-clear').onclick = () => {
  playerInbox.selected.clear();
  loadPlayerInbox();
};
$('#inbox-select-page').onclick = () => {
  for (const p of playerInbox.rows) {
    if (playerInbox.selected.size >= 100 && !playerInbox.selected.has(p.present_id)) {
      toast('每次最多选择 100 封邮件', true);
      break;
    }
    playerInbox.selected.set(p.present_id, p);
  }
  $$('.inbox-check').forEach((b) => {
    b.checked = playerInbox.selected.has(b.dataset.id);
    b.closest('tr').classList.toggle('selected', b.checked);
  });
  updateInboxControls();
};
