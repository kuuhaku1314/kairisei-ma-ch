'use strict';
// 运营设置 › 登录与访问 (登录白名单, IP 黑名单) and 游戏功能开关, plus 账号管理 › 封禁登录.
// Each settings block is saved on its own with its own revision; the server answers 409 when someone else
// saved in between, and the block then offers to read the current version again.

const access = {
  whitelist: null, // saved { enabled, user_ids, revision }
  wlDraft: null, // { enabled, ids }
  ip: null, // saved { entries, revision }
  modules: null, // saved { modules: [{ key, label, enabled }], revision }
  moduleDraft: null, // Set of the keys switched off in the draft
  players: new Map(), // user_id → account row, for names
  busy: new Set(),
  loading: null,
  conflict: new Set(), // blocks whose save met a newer version
  failed: {}, // block → load error
  built: false,
  wasActive: false,
  accountsActive: false,
  suggest: { serial: 0, rows: [], active: -1 },
};

const WHITELIST_MAX = 2000;
const IP_MAX = 512;
// Groups follow what the client shows; a key the server adds later lands under 其他.
const MODULE_GROUPS = [
  ['入口', 'everyday_task activity boss_coin gift_code pvp past_boss copy_card change_cloth change_model'],
  ['提示与战斗', 'month_card_tip failure_advise multi_chain chat_define return_to_dungeon'],
].map(([name, keys]) => [name, keys.split(' ')]);
const MODULE_NOTES = { pvp: '关闭后服务器立即拒绝新的竞技场对局，已开始的对局仍可结算。' };
const ACCESS_BLOCKS = {
  wl: ['/api/settings/login-whitelist', acceptWhitelist],
  ip: ['/api/settings/ip-blacklist', acceptIPBlacklist],
  mod: ['/api/settings/module-switches', acceptModules],
};

const sameList = (a, b) => a.length === b.length && a.every((v, i) => v === b[i]);
const playerName = (id) => {
  const a = access.players.get(id);
  return a ? a.name || a.username || '未命名玩家' : `玩家 ${id}`;
};

// Loading --------------------------------------------------------------------------------------------

async function loadAccess(blocks = Object.keys(ACCESS_BLOCKS)) {
  await Promise.all(
    blocks.map(async (block) => {
      const [path, accept] = ACCESS_BLOCKS[block];
      try {
        accept(await api(path));
        delete access.failed[block];
      } catch (error) {
        access.failed[block] = error;
        if (access.conflict.has(block)) toast(errorText(error), true);
      }
    }),
  );
  renderAccess();
  if (blocks.includes('wl') && access.whitelist && !access.failed.wl) {
    await resolveWhitelistNames(access.whitelist.user_ids);
    renderWhitelist();
  }
}

/** One read at a time: activating the page and its first load both ask for it. */
function reloadAccess() {
  access.loading ||= loadAccess()
    .catch((e) => reportError('settings', e, '读取'))
    .finally(() => {
      access.loading = null;
    });
  return access.loading;
}

async function reloadAccessBlock(block) {
  if (access.loading || access.busy.has(block)) return;
  access.busy.add(block);
  renderAccess();
  try {
    await loadAccess([block]);
  } catch (error) {
    reportError('settings', error, '读取');
  } finally {
    access.busy.delete(block);
    renderAccess();
  }
}

function acceptWhitelist(data) {
  access.whitelist = { enabled: data.enabled, user_ids: data.user_ids || [], revision: data.revision };
  access.wlDraft = { enabled: data.enabled, ids: [...access.whitelist.user_ids] };
  access.conflict.delete('wl');
}

function acceptIPBlacklist(data) {
  access.ip = { entries: data.entries || [], revision: data.revision };
  access.conflict.delete('ip');
  const box = $('#ip-list');
  if (box) box.value = access.ip.entries.join('\n');
}

function acceptModules(data) {
  access.modules = { modules: data.modules || [], revision: data.revision };
  access.moduleDraft = new Set(access.modules.modules.filter((m) => !m.enabled).map((m) => m.key));
  access.conflict.delete('mod');
}

/** Names for whitelist chips; the resolve endpoint takes 500 IDs per call. */
async function resolveWhitelistNames(ids) {
  const missing = ids.filter((id) => !access.players.has(id));
  for (let i = 0; i < missing.length; i += 500) {
    const data = await api('/api/accounts/resolve', {
      method: 'POST',
      body: JSON.stringify({ user_ids: missing.slice(i, i + 500) }),
    });
    for (const a of data.accounts) access.players.set(a.user_id, a);
  }
}

// Shared pieces --------------------------------------------------------------------------------------

/** The save line at the foot of a block: what changed on the left, the buttons on the right. */
function accessSaveHTML(block, { dirty, summary, label, extra = '' }) {
  const busy = access.busy.has(block);
  const conflict = access.conflict.has(block);
  const [text, tone] = conflict
    ? ['已被其他人修改：请重新读取，再重新做这次修改', 'error']
    : dirty
      ? [summary || '有未保存的修改', 'dirty']
      : ['已保存，与服务器一致', 'ok'];
  return html`
    <div class="access-save">
      <span class="access-save-state state-${tone}" aria-live="polite">${busy ? '正在处理…' : text}</span>
      <span class="access-save-actions">
        ${extra}
        ${
          conflict
            ? html`<button type="button" class="secondary" data-access="${block}-reload" ${attr(busy, 'disabled')}>重新读取</button>`
            : html`<button
                type="button"
                class="secondary"
                data-access="${block}-revert"
                ${attr(!dirty || busy, 'disabled')}
              >
                撤销修改
              </button>`
        }
        <button
          type="button"
          class="primary"
          data-access="${block}-save"
          ${attr(!dirty || busy || conflict, 'disabled')}
        >
          ${label}
        </button>
      </span>
    </div>
  `;
}

function accessLoadFailedHTML(error) {
  return html`<p class="field-error">读取失败：${errorText(error)}。可点击右上角「刷新数据」重试。</p>`;
}

async function accessPut(block, path, body) {
  access.busy.add(block);
  renderAccess();
  try {
    return await api(path, { method: 'PUT', body: JSON.stringify(body) });
  } catch (error) {
    if (error.status === 409) access.conflict.add(block);
    throw error;
  } finally {
    access.busy.delete(block);
  }
}

function renderAccess() {
  if (!access.built) buildAccess();
  renderWhitelist();
  renderIPBlacklist();
  renderModules();
  updateDraftIndicators();
}

/** Static parts are written once so typing in the inputs survives every later render. */
function buildAccess() {
  access.built = true;
  setHTML(
    $('#access-whitelist-body'),
    html`
      <div class="setting-inline">
        <label class="switch"><input type="checkbox" id="wl-enabled" disabled /><span>只允许名单内的玩家登录</span></label>
        <span id="wl-live"></span>
      </div>
      <div class="wl-add">
        <div class="wl-search">
          <input
            id="wl-input"
            type="search"
            autocomplete="off"
            aria-label="添加白名单玩家"
            aria-controls="wl-suggest"
            placeholder="输入玩家 ID、角色名或登录账号；多个 ID 用逗号或空格分隔"
          />
          <div class="wl-suggest" id="wl-suggest" role="listbox" hidden></div>
        </div>
        <button type="button" class="secondary" data-access="wl-add">添加</button>
        <button type="button" class="ghost" data-access="wl-accounts">从账号管理选择</button>
      </div>
      <p class="field-error" id="wl-input-error" hidden></p>
      <div class="wl-head"><b id="wl-count"></b><span class="hint" id="wl-diff"></span></div>
      <div class="target-chips wl-list" id="wl-chips"></div>
      <p class="field-error" id="wl-problem" hidden></p>
      <p class="hint">
        开启后，名单外的玩家无法登录，新设备也无法注册；正在游戏的玩家不受影响，下次登录时检查。账号封禁和 IP
        黑名单仍然优先生效。
      </p>
      <div id="wl-save"></div>
    `,
  );
  setHTML(
    $('#access-ip-body'),
    html`
      <textarea
        id="ip-list"
        rows="6"
        disabled
        spellcheck="false"
        autocomplete="off"
        aria-describedby="ip-count ip-error"
        placeholder="${'每行一项，例如：\n203.0.113.7\n198.51.100.0/24\n2001:db8::/32'}"></textarea>
      <span class="hint" id="ip-count"></span>
      <p class="field-error" id="ip-error" hidden></p>
      <p class="warn-text" id="ip-warn" hidden></p>
      <p class="hint">
        支持 IPv4、IPv6 和 CIDR 网段，最多 ${IP_MAX} 项，保存时自动整理格式并去重。按连接的实际来源地址匹配：
        如果服务器在反向代理或端口转发之后，看到的可能是代理的地址，不要把它加入名单。只影响游戏服务，后台不受影响。
      </p>
      <div id="ip-save"></div>
    `,
  );
  if (access.ip) $('#ip-list').value = access.ip.entries.join('\n');
}

// 登录白名单 -------------------------------------------------------------------------------------------

function whitelistDirty() {
  const saved = access.whitelist;
  const draft = access.wlDraft;
  if (!saved) return false;
  return (
    draft.enabled !== saved.enabled ||
    !sameList(
      [...draft.ids].sort((a, b) => a - b),
      saved.user_ids,
    )
  );
}

function renderWhitelist() {
  const body = $('#access-whitelist-body');
  if (!access.whitelist) {
    if (access.failed.wl) setHTML($('#wl-save'), accessLoadFailedHTML(access.failed.wl));
    return;
  }
  const saved = access.whitelist;
  const draft = access.wlDraft;
  const busy = access.busy.has('wl');
  $('#wl-enabled').checked = draft.enabled;
  $('#wl-enabled').disabled = busy;
  setHTML(
    $('#wl-live'),
    stateTag(saved.enabled ? '当前已开启' : '当前未开启', saved.enabled ? 'warn' : 'muted'),
  );
  const before = new Set(saved.user_ids);
  const now = new Set(draft.ids);
  const added = draft.ids.filter((id) => !before.has(id)).length;
  const removed = saved.user_ids.filter((id) => !now.has(id)).length;
  $('#wl-count').textContent = `名单 ${num(draft.ids.length)} / ${num(WHITELIST_MAX)} 名`;
  $('#wl-diff').textContent = [added && `新增 ${added} 名`, removed && `移除 ${removed} 名`]
    .filter(Boolean)
    .join('，');
  setHTML(
    $('#wl-chips'),
    draft.ids.length
      ? draft.ids.map(
          (id) => html`
            <span class="target-chip ${before.has(id) ? '' : 'added'}">
              ${playerName(id)}<code>#${id}</code>
              <button type="button" class="x-button" data-wl-remove="${id}" aria-label="移出 ${playerName(id)}"
                ${attr(busy, 'disabled')}>
                <svg class="icon"><use href="#i-close" /></svg>
              </button>
            </span>
          `,
        )
      : html`<span class="target-empty">名单为空</span>`,
  );
  const problem = $('#wl-problem');
  problem.hidden = !(draft.enabled && !draft.ids.length);
  problem.textContent = '名单为空时开启白名单，所有玩家都无法登录。';
  const dirty = whitelistDirty();
  const summary = [
    draft.enabled !== saved.enabled && (draft.enabled ? '将开启白名单' : '将关闭白名单'),
    added && `新增 ${added} 名`,
    removed && `移除 ${removed} 名`,
  ]
    .filter(Boolean)
    .join('，');
  setHTML($('#wl-save'), accessSaveHTML('wl', { dirty, summary, label: '保存白名单' }));
}

function whitelistInputError(message) {
  const error = $('#wl-input-error');
  error.hidden = !message;
  error.textContent = message || '';
  $('#wl-input').classList.toggle('invalid', !!message);
}

/** Adds players to the draft; returns the IDs that were not in it yet. */
function addWhitelistPlayers(rows) {
  for (const a of rows) access.players.set(a.user_id, a);
  const ids = rows.map((a) => a.user_id).filter((id) => !access.wlDraft.ids.includes(id));
  if (access.wlDraft.ids.length + ids.length > WHITELIST_MAX) {
    const room = WHITELIST_MAX - access.wlDraft.ids.length;
    whitelistInputError(`名单最多 ${num(WHITELIST_MAX)} 名，还能再加 ${num(room)} 名`);
    return null;
  }
  access.wlDraft.ids.push(...ids);
  return ids;
}

/** The input takes either a list of IDs or one search word, which picks the first suggestion. */
async function addWhitelistInput() {
  if (!access.wlDraft || access.busy.has('wl')) return;
  const input = $('#wl-input');
  const text = input.value.trim();
  const draft = access.wlDraft;
  whitelistInputError('');
  if (!text) return whitelistInputError('请输入玩家 ID、角色名或登录账号');
  if (/^[\d\s,，、]+$/.test(text)) {
    const ids = [
      ...new Set(
        text
          .split(/[\s,，、]+/)
          .filter(Boolean)
          .map(Number),
      ),
    ];
    const invalid = ids.filter((id) => !Number.isSafeInteger(id) || id < 1000001 || id >= 1900000000);
    if (invalid.length)
      return whitelistInputError(`这些不是有效的玩家 ID：${invalid.slice(0, 10).join('、')}`);
    try {
      const found = new Map();
      for (let i = 0; i < ids.length; i += 500) {
        const data = await api('/api/accounts/resolve', {
          method: 'POST',
          body: JSON.stringify({ user_ids: ids.slice(i, i + 500) }),
        });
        for (const a of data.accounts) found.set(a.user_id, a);
      }
      if (input.value.trim() !== text || access.wlDraft !== draft || access.busy.has('wl')) return;
      const unknown = ids.filter((id) => !found.has(id));
      if (unknown.length) return whitelistInputError(`这些玩家不存在：${unknown.slice(0, 10).join('、')}`);
      const added = addWhitelistPlayers([...found.values()]);
      if (!added) return;
      input.value = '';
      closeWhitelistSuggest();
      renderWhitelist();
      updateDraftIndicators();
      toast(added.length ? `已加入 ${added.length} 名玩家，保存后生效` : '这些玩家已在名单中');
    } catch (error) {
      if (input.value.trim() === text && access.wlDraft === draft) whitelistInputError(errorText(error));
    }
    return;
  }
  if (!access.suggest.rows.length) await searchWhitelistPlayers();
  if (input.value.trim() !== text || access.wlDraft !== draft || access.busy.has('wl')) return;
  const pick = access.suggest.rows[Math.max(0, access.suggest.active)];
  if (!pick) return whitelistInputError('没有找到匹配的玩家');
  pickWhitelistSuggestion(pick);
}

function pickWhitelistSuggestion(a) {
  if (!a || !access.wlDraft || access.busy.has('wl')) return;
  const added = addWhitelistPlayers([a]);
  if (!added) return;
  $('#wl-input').value = '';
  closeWhitelistSuggest();
  renderWhitelist();
  updateDraftIndicators();
  if (!added.length) toast(`「${playerLabel(a)}」已在名单中`);
}

async function searchWhitelistPlayers() {
  clearTimeout(searchWhitelistPlayers.timer);
  if (!access.wlDraft || access.busy.has('wl')) return;
  const text = $('#wl-input').value.trim();
  const serial = ++access.suggest.serial;
  if (!text || (/^[\d\s,，、]+$/.test(text) && /[\s,，、]/.test(text))) return closeWhitelistSuggest();
  try {
    const data = await api('/api/accounts?' + new URLSearchParams({ q: text, limit: 8 }));
    if (serial !== access.suggest.serial) return;
    access.suggest.rows = data.accounts.filter((a) => a.user_id < 1900000000);
    access.suggest.active = access.suggest.rows.length ? 0 : -1;
    renderWhitelistSuggest();
  } catch {
    if (serial === access.suggest.serial) closeWhitelistSuggest();
  }
}

function renderWhitelistSuggest() {
  const box = $('#wl-suggest');
  const { rows, active } = access.suggest;
  const listed = new Set(access.wlDraft.ids);
  box.hidden = false;
  setHTML(
    box,
    rows.length
      ? rows.map(
          (a, i) => html`
            <button
              type="button"
              role="option"
              class="${i === active ? 'active' : ''}"
              aria-selected="${i === active}"
              data-wl-pick="${i}"
            >
              <b>${a.name || '未命名'}</b><code>#${a.user_id}</code>
              <span class="sub">Lv.${a.level}${a.username ? ` · ${a.username}` : ' · 游客'}</span>
              ${listed.has(a.user_id) && stateTag('已在名单', 'info')}
            </button>
          `,
        )
      : html`<p class="wl-suggest-empty">没有匹配的玩家</p>`,
  );
}

function closeWhitelistSuggest() {
  access.suggest.serial++;
  access.suggest.rows = [];
  access.suggest.active = -1;
  $('#wl-suggest').hidden = true;
}

async function saveWhitelist() {
  const saved = access.whitelist;
  const draft = access.wlDraft;
  const ids = [...draft.ids].sort((a, b) => a - b);
  if (draft.enabled && (!saved.enabled || !ids.length)) {
    const ok = await askConfirm(
      saved.enabled ? '保存空的登录白名单？' : '开启登录白名单？',
      ids.length
        ? `只有名单内的 ${num(ids.length)} 名玩家可以登录，其他玩家和新设备将无法进入游戏。`
        : '名单为空：保存后所有玩家都无法登录，直到关闭白名单或加入玩家。',
      { ok: saved.enabled ? '保存' : '开启白名单', danger: true },
    );
    if (!ok) return;
  }
  try {
    const data = await accessPut('wl', '/api/settings/login-whitelist', {
      enabled: draft.enabled,
      user_ids: ids,
      expected_revision: saved.revision,
    });
    acceptWhitelist(data);
    toast(
      data.enabled
        ? `登录白名单已保存：只允许 ${num(data.user_ids.length)} 名玩家登录`
        : '登录白名单已保存（未开启）',
    );
    if (state.accounts?.length && access.accountsActive) renderAccounts();
  } catch (error) {
    toast(errorText(error), true);
  }
  renderAccess();
}

// IP 黑名单 --------------------------------------------------------------------------------------------

/** The browser's URL parser checks an IPv6 literal exactly as written. */
function validIPv6(text) {
  if (!/^[0-9a-f:.]+$/i.test(text) || !text.includes(':')) return false;
  try {
    new URL(`http://[${text}]/`);
    return true;
  } catch {
    return false;
  }
}

const validIPv4 = (text) =>
  /^(?:0|[1-9]\d{0,2})(?:\.(?:0|[1-9]\d{0,2})){3}$/.test(text) &&
  text.split('.').every((p) => Number(p) <= 255);

/** Lines of the box as entries, with the problems found before saving: [{ line, text, error?, wide? }]. */
function ipDraftLines() {
  return $('#ip-list')
    .value.split('\n')
    .map((text, i) => ({ line: i + 1, text: text.trim() }))
    .filter((row) => row.text)
    .map((row) => {
      const [addr, prefix, extra] = row.text.split('/');
      const v4 = validIPv4(addr);
      const ok = v4 || validIPv6(addr);
      const max = v4 ? 32 : 128;
      const bits = prefix === undefined ? max : /^\d{1,3}$/.test(prefix) ? Number(prefix) : -1;
      if (!ok || extra !== undefined || bits < 0 || bits > max) return { ...row, error: true };
      return { ...row, wide: bits <= (v4 ? 8 : 16) ? bits : null };
    });
}

const ipDraftEntries = () => ipDraftLines().map((row) => row.text);
const ipDirty = () => !!access.ip && !sameList(ipDraftEntries(), access.ip.entries);

function renderIPBlacklist() {
  if (!access.ip) {
    if (access.failed.ip) setHTML($('#ip-save'), accessLoadFailedHTML(access.failed.ip));
    return;
  }
  const rows = ipDraftLines();
  const errors = rows.filter((row) => row.error);
  const unique = new Set(rows.map((row) => row.text.toLowerCase())).size;
  const duplicates = rows.length - unique;
  $('#ip-count').textContent =
    `共 ${num(unique)} / ${IP_MAX} 项` +
    (duplicates ? `（${duplicates} 行重复，保存时合并）` : '') +
    ` · 当前生效 ${num(access.ip.entries.length)} 项`;
  const error = $('#ip-error');
  const over = unique > IP_MAX;
  error.hidden = !errors.length && !over;
  error.textContent = over
    ? `最多 ${IP_MAX} 项，当前 ${num(unique)} 项`
    : errors
        .slice(0, 3)
        .map((row) => `第 ${row.line} 行「${row.text}」不是有效的 IPv4、IPv6 地址或网段`)
        .join('；') + (errors.length > 3 ? `；另有 ${errors.length - 3} 行` : '');
  $('#ip-list').classList.toggle('invalid', !error.hidden);
  const wide = rows.filter((row) => row.wide != null);
  const warn = $('#ip-warn');
  warn.hidden = !wide.length;
  warn.textContent = wide
    .slice(0, 3)
    .map((row) =>
      row.wide === 0
        ? `第 ${row.line} 行「${row.text}」会阻止所有 ${row.text.includes(':') ? 'IPv6' : 'IPv4'} 地址`
        : `第 ${row.line} 行「${row.text}」覆盖的地址范围很大`,
    )
    .join('；');
  $('#ip-list').disabled = access.busy.has('ip');
  const saved = new Set(access.ip.entries.map((e) => e.toLowerCase()));
  const fresh = rows.map((row) => !row.error && row.text.toLowerCase()).filter((e) => e && !saved.has(e));
  const added = new Set(fresh).size;
  setHTML(
    $('#ip-save'),
    accessSaveHTML('ip', {
      dirty: ipDirty(),
      summary: `有未保存的修改：共 ${num(unique)} 项${added ? `，新增 ${added} 项` : ''}`,
      label: '保存黑名单',
    }),
  );
  if (!error.hidden) $('#ip-save [data-access="ip-save"]').disabled = true;
}

async function saveIPBlacklist() {
  const entries = [...new Map(ipDraftEntries().map((e) => [e.toLowerCase(), e])).values()];
  try {
    const data = await accessPut('ip', '/api/settings/ip-blacklist', {
      entries,
      expected_revision: access.ip.revision,
    });
    acceptIPBlacklist(data);
    toast(
      data.entries.length
        ? `IP 黑名单已保存：${num(data.entries.length)} 项，新的请求和组队连接立即生效`
        : 'IP 黑名单已清空',
    );
  } catch (error) {
    $('#ip-error').hidden = false;
    $('#ip-error').textContent = errorText(error);
    toast(errorText(error), true);
  }
  renderAccess();
}

// 游戏功能开关 ------------------------------------------------------------------------------------------

function modulesDirty() {
  if (!access.modules) return false;
  return access.modules.modules.some((m) => m.enabled === access.moduleDraft.has(m.key));
}

function renderModules() {
  const body = $('#modules-body');
  if (!access.modules) {
    if (access.failed.mod) setHTML(body, accessLoadFailedHTML(access.failed.mod));
    return;
  }
  const busy = access.busy.has('mod');
  const draft = access.moduleDraft;
  const all = access.modules.modules;
  const known = new Set(MODULE_GROUPS.flatMap(([, keys]) => keys));
  const groups = [
    ...MODULE_GROUPS.map(([name, keys]) => [
      name,
      keys.map((k) => all.find((m) => m.key === k)).filter(Boolean),
    ]),
    ['其他', all.filter((m) => !known.has(m.key))],
  ].filter(([, modules]) => modules.length);
  const offSaved = all.filter((m) => !m.enabled).length;
  const pill = $('#modules-state');
  pill.textContent = offSaved ? `已关闭 ${offSaved} 项` : '全部开启';
  pill.className = `pill ${offSaved ? 'gold' : 'green'}`;
  const changed = all.filter((m) => m.enabled === draft.has(m.key));
  const item = (m) => {
    const on = !draft.has(m.key);
    const moved = on !== m.enabled;
    return html`
      <label class="module-item ${on ? '' : 'off'} ${moved ? 'changed' : ''}">
        <span class="switch">
          <input type="checkbox" data-module="${m.key}" ${attr(on, 'checked')} ${attr(busy, 'disabled')} />
        </span>
        <span class="module-text">
          <b>${m.label}</b>
          ${MODULE_NOTES[m.key] && html`<small>${MODULE_NOTES[m.key]}</small>`}
          ${moved && html`<small class="module-moved">保存后${on ? '开启' : '关闭'}</small>`}
        </span>
      </label>
    `;
  };
  setHTML(
    body,
    html`
      ${groups.map(
        ([name, modules]) => html`
          <section class="module-group">
            <h3>${name}</h3>
            <div class="module-grid">${modules.map(item)}</div>
          </section>
        `,
      )}
      ${accessSaveHTML('mod', {
        dirty: changed.length > 0,
        summary: `有未保存的修改：${changed.map((m) => `${draft.has(m.key) ? '关闭' : '开启'}${m.label}`).join('、')}`,
        label: '保存功能开关',
        extra: html`<button
          type="button"
          class="ghost"
          data-access="mod-all"
          ${attr(busy || !draft.size, 'disabled')}
        >
          全部开启
        </button>`,
      })}
    `,
  );
}

async function saveModules() {
  try {
    const data = await accessPut('mod', '/api/settings/module-switches', {
      disabled: [...access.moduleDraft],
      expected_revision: access.modules.revision,
    });
    acceptModules(data);
    toast('功能开关已保存，玩家下次回到标题画面时生效');
  } catch (error) {
    toast(errorText(error), true);
  }
  renderAccess();
}

function accessSettingsDirty() {
  return whitelistDirty() || ipDirty() || modulesDirty();
}

// 账号管理 › 封禁登录 ----------------------------------------------------------------------------------

function remainingText(seconds) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  if (days) return `剩 ${days} 天${hours ? ` ${hours} 小时` : ''}`;
  if (hours) return `剩 ${hours} 小时`;
  return `剩 ${Math.max(1, Math.ceil(seconds / 60))} 分钟`;
}

/** '' when the player may log in, else how long the block lasts; UTC+8 like the other operations. */
function loginBlockText(block) {
  if (!block?.active) return '';
  if (!block.until_unix) return '永久封禁';
  return `封禁至 ${chinaText(block.until_unix)}（${remainingText(block.remaining_seconds)}）`;
}

const whitelisted = (id) => !!access.whitelist?.user_ids.includes(id);

/** Tags after a name in the account list. */
function accountAccessTags(a) {
  const tags = [];
  if (a.login_block?.active)
    tags.push(`<span class="tag bad" title="${esc(loginBlockText(a.login_block))}">封禁中</span>`);
  if (whitelisted(a.user_id))
    tags.push(
      `<span class="tag info" title="${access.whitelist.enabled ? '白名单已开启' : '白名单未开启'}">白名单</span>`,
    );
  return tags.join('');
}

const accountBlockButton = (a) =>
  `<button class="ghost login-block" data-user="${a.user_id}">` +
  `${a.login_block?.active ? '管理封禁' : '封禁登录'}</button>`;

const BLOCK_TERMS = [
  ['1', '1 天'],
  ['3', '3 天'],
  ['7', '7 天'],
  ['30', '30 天'],
  ['custom', '指定时间'],
  ['forever', '永久'],
];
const blockState = { user: null, term: '7', busy: false };

function openLoginBlock(account) {
  const block = account.login_block || {};
  blockState.user = account;
  blockState.term = !block.active ? '7' : block.until_unix ? 'custom' : 'forever';
  $('#block-title').textContent = `${block.active ? '管理封禁' : '封禁登录'} · ${playerLabel(account)}`;
  $('#block-lead').textContent = block.active ? `当前：${loginBlockText(block)}` : '玩家当前可以正常登录。';
  setHTML($('#block-terms'), segmented('block-term', blockState.term, BLOCK_TERMS));
  $('#block-until').value = block.active && block.until_unix ? chinaInputValue(block.until_unix) : '';
  $('#block-lift').hidden = !block.active;
  $('#block-apply').textContent = block.active ? '更新封禁' : '封禁';
  renderLoginBlockTerm();
  $('#block-modal').classList.add('open');
}

/** Unix seconds the chosen term ends at: 0 for a permanent block, null while the custom time is invalid. */
function loginBlockUntil() {
  const now = Math.floor(Date.now() / 1000);
  if (blockState.term === 'forever') return 0;
  if (blockState.term !== 'custom') return now + Number(blockState.term) * 86400;
  try {
    const until = chinaUnix($('#block-until').value);
    return until > now ? until : null;
  } catch {
    return null;
  }
}

function renderLoginBlockTerm() {
  const custom = blockState.term === 'custom';
  $('#block-until-field').hidden = !custom;
  const until = loginBlockUntil();
  const error = $('#block-until-error');
  const showError = custom && until === null && $('#block-until').value !== '';
  error.hidden = !showError;
  error.textContent = '解封时间须晚于现在';
  $('#block-until').classList.toggle('invalid', showError);
  $('#block-result').textContent =
    until === 0
      ? '永久封禁：不会自动解除，需要在这里手动解封。'
      : until
        ? `${chinaText(until)}（UTC+8）自动解封。`
        : '请选择解封时间。';
  $('#block-apply').disabled = blockState.busy || until === null;
  $('#block-lift').disabled = blockState.busy;
}

async function changeLoginBlock(lift) {
  if (blockState.busy) return;
  const a = blockState.user;
  const until = loginBlockUntil();
  if (!lift && until === null) return;
  blockState.busy = true;
  renderLoginBlockTerm();
  try {
    // A body marks the request as an Admin write, for DELETE too.
    const data = await api(`/api/accounts/${a.user_id}/login-block`, {
      method: lift ? 'DELETE' : 'PUT',
      body: JSON.stringify(lift ? {} : { until_unix: until }),
    });
    const row = state.accounts?.find((x) => x.user_id === a.user_id);
    if (row) row.login_block = data.login_block;
    a.login_block = data.login_block;
    $('#block-modal').classList.remove('open');
    // Filtered by 登录状态, the player may have left the result.
    if (access.accountsActive && $('#account-login-block').value)
      loadAccountsPage().catch((e) => reportError('accounts', e, '载入'));
    else if (access.accountsActive) renderAccounts();
    if ($('#account-modal').classList.contains('open') && accessDetail.account?.user_id === a.user_id)
      renderAccountFacts({ ...accessDetail.account, login_block: data.login_block });
    toast(
      lift
        ? `已解除「${playerLabel(a)}」的封禁，玩家可以重新登录`
        : `已封禁「${playerLabel(a)}」：${loginBlockText(data.login_block)}`,
    );
  } catch (error) {
    toast(errorText(error), true);
  } finally {
    blockState.busy = false;
    renderLoginBlockTerm();
  }
}

/** 账号详情 shows the block next to the other facts and offers the same dialog. */
const accessDetail = { account: null };
function accessDetailShown(a) {
  accessDetail.account = a;
  const button = $('#account-detail-block');
  button.hidden = a.user_id >= 1900000000;
  button.textContent = a.login_block?.active ? '管理封禁' : '封禁登录';
}

// 账号管理 › 已选玩家 → 登录白名单 ---------------------------------------------------------------------

async function addSelectedToWhitelist() {
  if (whitelistDirty() || access.busy.has('wl')) return toast('请先在运营设置中保存或撤销白名单草稿', true);
  const ids = [...selectedPlayers.keys()].filter((id) => id < 1900000000);
  if (!ids.length) return toast('请先选择玩家', true);
  const button = $('#accounts-whitelist');
  button.disabled = true;
  try {
    const data = await api('/api/settings/login-whitelist');
    const current = new Set(data.user_ids);
    const fresh = ids.filter((id) => !current.has(id));
    if (!fresh.length) return toast('已选玩家都已在登录白名单中');
    if (current.size + fresh.length > WHITELIST_MAX)
      return toast(
        `登录白名单最多 ${num(WHITELIST_MAX)} 名，还能再加 ${num(WHITELIST_MAX - current.size)} 名`,
        true,
      );
    const ok = await askConfirm(
      `把 ${num(fresh.length)} 名玩家加入登录白名单？`,
      (ids.length > fresh.length ? `另有 ${ids.length - fresh.length} 名已在名单中。` : '') +
        (data.enabled
          ? '白名单已开启，加入后这些玩家将通过白名单检查。'
          : '白名单当前未开启，加入后需要在「运营设置」中开启才生效。'),
      { ok: '加入白名单' },
    );
    if (!ok) return;
    const saved = await api('/api/settings/login-whitelist', {
      method: 'PUT',
      body: JSON.stringify({
        enabled: data.enabled,
        user_ids: [...current, ...fresh],
        expected_revision: data.revision,
      }),
    });
    for (const id of fresh) access.players.set(id, selectedPlayers.get(id));
    acceptWhitelist(saved);
    if (access.built) renderAccess();
    renderAccounts();
    toast(`已把 ${num(fresh.length)} 名玩家加入登录白名单`);
  } catch (error) {
    toast(errorText(error), true);
  } finally {
    button.disabled = false;
  }
}

// Wiring ---------------------------------------------------------------------------------------------

function initAccess() {
  editorDirtyChecks.settings = accessSettingsDirty;
  const settings = $('#settings');
  settings.addEventListener('click', (event) => {
    const action = event.target.closest('[data-access]')?.dataset.access;
    const remove = event.target.closest('[data-wl-remove]')?.dataset.wlRemove;
    const pick = event.target.closest('[data-wl-pick]')?.dataset.wlPick;
    if (remove) {
      access.wlDraft.ids = access.wlDraft.ids.filter((id) => id !== Number(remove));
      renderWhitelist();
    } else if (pick) pickWhitelistSuggestion(access.suggest.rows[Number(pick)]);
    else if (action === 'wl-add') addWhitelistInput();
    else if (action === 'wl-accounts') switchView('accounts');
    else if (action === 'wl-save') saveWhitelist();
    else if (action === 'wl-revert') {
      acceptWhitelist({ ...access.whitelist });
      whitelistInputError('');
      renderWhitelist();
    } else if (action === 'ip-save') saveIPBlacklist();
    else if (action === 'ip-revert') {
      $('#ip-list').value = access.ip.entries.join('\n');
      renderIPBlacklist();
    } else if (action === 'mod-save') saveModules();
    else if (action === 'mod-revert') {
      acceptModules({ ...access.modules });
      renderModules();
    } else if (action === 'mod-all') {
      access.moduleDraft.clear();
      renderModules();
    } else if (action?.endsWith('-reload')) reloadAccessBlock(action.slice(0, -7));
  });
  settings.addEventListener('change', (event) => {
    if (event.target.id === 'wl-enabled') {
      access.wlDraft.enabled = event.target.checked;
      renderWhitelist();
    } else if (event.target.dataset.module) {
      const key = event.target.dataset.module;
      if (event.target.checked) access.moduleDraft.delete(key);
      else access.moduleDraft.add(key);
      renderModules();
    }
  });
  settings.addEventListener('input', (event) => {
    if (event.target.id === 'ip-list') renderIPBlacklist();
    else if (event.target.id === 'wl-input') {
      whitelistInputError('');
      closeWhitelistSuggest();
      clearTimeout(searchWhitelistPlayers.timer);
      searchWhitelistPlayers.timer = setTimeout(searchWhitelistPlayers, 200);
    }
  });
  settings.addEventListener('keydown', (event) => {
    if (event.target.id !== 'wl-input' || event.isComposing) return;
    const s = access.suggest;
    if (event.key === 'Enter') {
      event.preventDefault();
      addWhitelistInput();
    } else if (event.key === 'Escape') closeWhitelistSuggest();
    else if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && s.rows.length) {
      event.preventDefault();
      s.active = (s.active + (event.key === 'ArrowDown' ? 1 : -1) + s.rows.length) % s.rows.length;
      renderWhitelistSuggest();
    }
  });
  settings.addEventListener('focusout', (event) => {
    if (event.target.id !== 'wl-input' || event.relatedTarget?.closest('#wl-suggest')) return;
    closeWhitelistSuggest();
  });
  // 刷新数据 reads the page through loadRuntimeSettings once unsaved edits are confirmed away; coming back to
  // the page reads again unless an edit is still waiting to be saved.
  const loadSettingsBase = loadRuntimeSettings;
  loadRuntimeSettings = async () => {
    await loadSettingsBase();
    await reloadAccess();
  };
  new MutationObserver(() => {
    const active = settings.classList.contains('active');
    if (active && !access.wasActive && !accessSettingsDirty()) reloadAccess();
    access.wasActive = active;
  }).observe(settings, { attributes: true, attributeFilter: ['class'] });
  if (settings.classList.contains('active')) reloadAccess();

  // 账号管理: list tags need the whitelist; rows and the detail dialog open the block dialog.
  const accounts = $('#accounts');
  new MutationObserver(() => {
    const active = accounts.classList.contains('active');
    if (active && !access.accountsActive)
      api('/api/settings/login-whitelist').then(
        (data) => {
          if (!whitelistDirty() && data.revision >= (access.whitelist?.revision ?? 0)) acceptWhitelist(data);
          if (state.accounts?.length) renderAccounts();
        },
        () => {},
      );
    access.accountsActive = active;
  }).observe(accounts, { attributes: true, attributeFilter: ['class'] });
  $('#account-rows').addEventListener('click', (event) => {
    const button = event.target.closest('.login-block');
    const a = button && state.accounts.find((x) => x.user_id === Number(button.dataset.user));
    if (a) openLoginBlock(a);
  });
  $('#accounts-whitelist').addEventListener('click', addSelectedToWhitelist);
  $('#account-detail-block').addEventListener('click', () => {
    if (accessDetail.account) openLoginBlock(accessDetail.account);
  });
  $('#block-terms').addEventListener('change', (event) => {
    blockState.term = event.target.value;
    renderLoginBlockTerm();
    if (blockState.term === 'custom') $('#block-until').focus();
  });
  $('#block-until').addEventListener('input', renderLoginBlockTerm);
  $('#block-cancel').addEventListener(
    'click',
    () => !blockState.busy && $('#block-modal').classList.remove('open'),
  );
  $('#block-apply').addEventListener('click', () => changeLoginBlock(false));
  $('#block-lift').addEventListener('click', () => changeLoginBlock(true));
}

initAccess();
