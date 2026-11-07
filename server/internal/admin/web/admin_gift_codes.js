'use strict';
// 兑换码: batch list | batch header + tabs (礼包配置 · 兑换码 · 领取记录).
// A generation request keeps its operation number until the server answers; when the outcome is
// unknown (network loss, 5xx) only a retry with the identical request is offered. Export only reads.

const GIFT_MODES = {
  public: {
    name: '公共码',
    note: '所有玩家输入同一个码，每个账号用这个码只能领 1 次。',
  },
  single: {
    name: '单次码',
    note: '每个码只能被兑换 1 次，谁先用归谁，适合逐个私发。',
  },
};
const GIFT_SCOPES = {
  batch: {
    name: '本批次限 1 次',
    note: '不管拿到本批次几个码，同一账号只能领到 1 次。最常用。',
  },
  group: {
    name: '多个批次合计 1 次',
    note: '填同一个「领取组」的批次算作一份礼包，同一账号在这些批次里合计只能领 1 次。适合分渠道发码。',
  },
  code: {
    name: '每个码各 1 次',
    note: '同一账号可以用本批次的不同码各领 1 次，拿到几个码就能领几次。',
  },
};
// What three redemptions in a row would do under each code type × claim limit; ok marks a success.
const GIFT_EXAMPLES = {
  'public:batch': () => [
    ['玩家甲输入码 A', true, '领到奖励'],
    ['玩家甲再输入码 A，或本批次的其他码', false, '已领过'],
    ['玩家乙输入码 A', true, '领到奖励'],
  ],
  'public:group': (group) => [
    ['玩家甲输入本批次的码', true, '领到奖励'],
    [`玩家甲输入${group}里其他批次的码`, false, '已领过'],
    ['玩家乙输入同一个码', true, '领到奖励'],
  ],
  'public:code': () => [
    ['玩家甲输入码 A', true, '领到奖励'],
    ['玩家甲再输入码 A', false, '这个码已用过'],
    ['玩家甲输入本批次的码 B', true, '再领一份'],
  ],
  'single:batch': () => [
    ['玩家甲输入码 1', true, '领到奖励，码 1 作废'],
    ['玩家甲输入本批次的码 2', false, '已领过，码 2 仍可给别人'],
    ['玩家乙输入码 1', false, '码已被使用'],
  ],
  'single:group': (group) => [
    ['玩家甲输入码 1', true, '领到奖励，码 1 作废'],
    [`玩家甲输入${group}里其他批次的码`, false, '已领过'],
    ['玩家乙输入码 1', false, '码已被使用'],
  ],
  'single:code': () => [
    ['玩家甲输入码 1', true, '领到奖励，码 1 作废'],
    ['玩家甲输入码 2', true, '再领一份'],
    ['玩家乙输入码 1', false, '码已被使用'],
  ],
};
const GIFT_STATUS = {
  new: ['未保存的新批次', 'warn'],
  draft: ['草稿', 'info'],
  active: ['可兑换', 'ok'],
  scheduled: ['待开始', 'info'],
  expired: ['已过期', 'muted'],
  exhausted: ['额度已用完', 'bad'],
  disabled: ['已停用', 'warn'],
};
const GIFT_PAGE_SIZE = 100;

const giftPage = {
  rows: [],
  revision: 0,
  counts: {}, // batch id → { issued, used }
  current: null, // working copy of one GiftCodeBatch
  savedJSON: '', // stableJSON of the saved batch; '' while the batch is new
  tab: 'config', // 'config' | 'codes' | 'redemptions'
  list: { kind: '', page: 0, rows: [], serial: 0, loading: false, error: '' },
  pending: new Map(), // batch id → { body, unconfirmed } — body carries the operation number
  genMode: 'random',
};

// Model ------------------------------------------------------------------------------------------

const giftRecord = (id) => (id ? giftPage.rows.find((b) => b.id === id) : null);
const giftCurrentRecord = () => giftRecord(giftPage.current?.id);
const giftCounts = (id) => giftPage.counts[id] || { issued: 0, used: 0 };
const giftDirty = () => !!giftPage.current && stableJSON(giftPage.current) !== giftPage.savedJSON;
editorDirtyChecks['gift-codes'] = giftDirty;

function giftStatus(batch) {
  if (!batch) return 'new';
  if (!batch.published) return 'draft';
  if (!batch.enabled) return 'disabled';
  const now = Date.now() / 1000;
  if (batch.starts_at > now) return 'scheduled';
  if (batch.ends_at && batch.ends_at <= now) return 'expired';
  if (batch.limit && giftCounts(batch.id).used >= batch.limit) return 'exhausted';
  return 'active';
}

/** Codes exist: mode, claim scope, group and rewards are frozen by the server. */
const giftIssued = () => giftCounts(giftPage.current?.id).issued > 0;

function giftRuleText(b) {
  const code = b.mode === 'single' ? '每个码只能兑换 1 次' : '所有玩家共用同一个码';
  const account =
    b.claim_scope === 'batch'
      ? '同一账号在本批次限领 1 次'
      : b.claim_scope === 'group'
        ? `同一账号在领取组「${b.group.trim() || '未填写'}」的所有批次合计限领 1 次`
        : '同一账号可用本批次的不同码各领 1 次';
  return `${code}；${account}；${b.limit ? `全批次最多兑换 ${num(b.limit)} 次` : '全批次兑换次数不限'}。`;
}

function giftRuleExampleHTML(b) {
  const group = `领取组「${b.group.trim() || '…'}」`;
  const steps = GIFT_EXAMPLES[`${b.mode}:${b.claim_scope}`](group);
  return html`
    <span class="rule-example-title">举例</span>
    <ol class="rule-steps">
      ${steps.map(
        ([action, ok, result]) => html`<li>
          <span>${action}</span>
          <b class="${ok ? 'ok' : 'no'}"><svg class="icon"><use href="#i-${ok ? 'check' : 'close'}" /></svg>${result}</b>
        </li>`,
      )}
    </ol>
    <p>
      ${
        b.limit
          ? `全批次合计最多兑换 ${num(b.limit)} 次，用完后所有码都停止兑换。`
          : '全批次兑换次数不限，只受上面的规则约束。'
      }
    </p>
  `;
}

function giftProblems(b, ready) {
  const problems = [];
  const length = (text) => [...text.trim()].length;
  if (!length(b.name) || length(b.name) > 80) problems.push('批次名称须为 1–80 字');
  if (!length(b.title) || length(b.title) > 40) problems.push('邮件标题须为 1–40 字');
  if (!length(b.message) || length(b.message) > 200) problems.push('邮件正文须为 1–200 字');
  if (b.claim_scope === 'group' && (ready || b.group.trim()) && !length(b.group)) {
    problems.push('按领取组限领时须填写领取组');
  }
  if (length(b.group) > 80) problems.push('领取组最多 80 字');
  const used = giftCounts(b.id).used;
  if (!Number.isInteger(b.limit) || b.limit < 0) problems.push('总额度须为 0 或正整数（0 为不限）');
  else if (b.limit && b.limit < used) problems.push(`总额度不能小于已领取的 ${used} 次`);
  if (b.ends_at && b.ends_at <= b.starts_at) problems.push('结束时间须晚于开始时间');
  if (ready && !b.rewards.length) problems.push('每次兑换至少需要 1 份奖励');
  problems.push(...rewardProblems(b.rewards));
  return problems;
}

// Loading and selection ----------------------------------------------------------------------------

async function loadGiftBatches() {
  const data = await api('/api/gift-code-batches');
  applyGiftRegistry(data);
  selectGiftBatch(giftRecord(giftPage.current?.id) || data.batches[0] || null);
  await resolveRewardInfo(data.batches.flatMap((b) => b.rewards));
  renderGiftCodes();
}

function applyGiftRegistry(data) {
  giftPage.rows = data.batches;
  giftPage.revision = data.revision;
  if (data.counts) giftPage.counts = data.counts;
}

function selectGiftBatch(batch, tab = 'config') {
  giftPage.current = batch ? structuredClone(batch) : null;
  giftPage.savedJSON = batch ? stableJSON(batch) : '';
  giftPage.tab = tab;
  giftPage.list = {
    kind: '',
    page: 0,
    rows: [],
    serial: giftPage.list.serial + 1,
    loading: false,
    error: '',
  };
}

/** New empty batch, or a copy of the working copy (its unsaved edits move to the copy). */
async function newGiftBatch(copy = null) {
  if (!copy && !(await confirmDiscard(giftDirty(), '新建礼包会放弃当前未保存的修改。', '放弃并新建'))) return;
  const base = copy
    ? { ...structuredClone(copy), name: `${[...copy.name].slice(0, 76).join('')}（副本）` }
    : {
        name: '新礼包',
        mode: 'public',
        claim_scope: 'batch',
        group: '',
        title: '礼包奖励',
        message: '感谢支持，请领取礼包。',
        limit: 0,
        starts_at: 0,
        ends_at: 0,
        rewards: [],
      };
  giftPage.current = { ...base, id: 0, revision: 0, enabled: true, published: false };
  giftPage.savedJSON = '';
  giftPage.tab = 'config';
  clearViewAlert('gift-codes');
  renderGiftCodes();
  $('#gc-config [data-gift="name"]')?.select();
}

// Rendering --------------------------------------------------------------------------------------

function renderGiftCodes() {
  renderGiftList();
  renderGiftEditor();
  updateDraftIndicators();
}

function giftStatusTags(batch, { dirty = false } = {}) {
  const [label, tone] = GIFT_STATUS[giftStatus(batch)];
  return html`${stateTag(label, tone)}${dirty && batch && stateTag('未保存', 'warn')}`;
}

function renderGiftList() {
  const query = $('#gc-search').value.trim().toLowerCase();
  const filter = $('#gc-status').value;
  const current = giftPage.current;
  const dirty = giftDirty();
  const rows = giftPage.rows.filter(
    (b) => (!filter || giftStatus(b) === filter) && `${b.id} ${b.name}`.toLowerCase().includes(query),
  );
  const item = (batch) => {
    const b = batch || current;
    const counts = giftCounts(b.id);
    const selected = b.id === current?.id;
    return html`
      <button type="button" class="ws-item ${selected ? 'selected' : ''}" data-gc-select="${b.id}">
        <span class="ws-item-title">
          <b>${b.name || '未命名批次'}</b>
          ${giftStatusTags(batch, { dirty: selected && dirty })}
        </span>
        <span class="ws-item-meta">${GIFT_MODES[b.mode].name} · ${GIFT_SCOPES[b.claim_scope].name}</span>
        <small>
          ${b.id ? `#${b.id} · ` : '尚未保存 · '}已生成 ${num(counts.issued)} · 已领取
          ${num(counts.used)}${b.limit ? ` / ${num(b.limit)}` : ''}
        </small>
      </button>
    `;
  };
  setHTML(
    $('#gc-list'),
    html`
      ${current && !current.id && item(null)}
      ${rows.map(item)}
      ${
        !rows.length &&
        html`<p class="ws-list-empty">
        ${giftPage.rows.length ? '没有匹配的批次。' : '还没有兑换码批次。点「新建」配置礼包。'}
      </p>`
      }
    `,
  );
  $('#gc-count').textContent = `${giftPage.rows.length} 个批次`;
}

function renderGiftEditor() {
  const b = giftPage.current;
  $('#gc-empty').hidden = !!b;
  $('#gc-editor').hidden = !b;
  renderGiftSavebar();
  if (!b) return;
  renderGiftHead();
  renderGiftLock();
  renderGiftTabs();
}

function renderGiftHead() {
  const b = giftPage.current;
  const record = giftCurrentRecord();
  setHTML(
    $('#gc-head'),
    html`
      <div class="ws-title">
        <span class="eyebrow">兑换码批次${b.id ? ` · #${b.id}` : ''}</span>
        <h2>${b.name || '未命名批次'}</h2>
        <div class="ws-tags">
          ${giftStatusTags(record, { dirty: giftDirty() })}
          <span class="tag">${GIFT_MODES[b.mode].name}</span>
        </div>
      </div>
      <div class="ws-actions">
        <button type="button" class="secondary sm" data-gc-action="copy" title="复制配置与奖励为新的草稿批次，不复制兑换码">
          复制为新批次
        </button>
        ${
          record?.published &&
          html`<button type="button" class="secondary sm" data-gc-action="toggle">
          ${record.enabled ? '停用整批' : '重新开放'}
        </button>`
        }
      </div>
    `,
  );
}

function renderGiftLock() {
  const lock = $('#gc-lock');
  const counts = giftCounts(giftPage.current.id);
  lock.hidden = !counts.issued;
  if (lock.hidden) return;
  setHTML(
    lock,
    html`
      <svg class="icon"><use href="#i-info" /></svg>
      <span>
        <b>已生成 ${num(counts.issued)} 个兑换码</b>码类型、限领方式、领取组和奖励已固定；
        名称、邮件文案、有效期、总额度（不低于已领取次数）和开放状态仍可修改。需要不同的奖励或规则时请复制为新批次。
      </span>
      <button type="button" class="secondary sm" data-gc-action="copy">复制为新批次</button>
    `,
  );
}

function renderGiftTabs() {
  const b = giftPage.current;
  const counts = giftCounts(b.id);
  $$('#gc-tabs [data-gc-tab]').forEach((button) => {
    const active = button.dataset.gcTab === giftPage.tab;
    button.classList.toggle('active', active);
    button.setAttribute('aria-selected', String(active));
  });
  $('#gc-tab-codes').textContent = counts.issued ? num(counts.issued) : '';
  $('#gc-tab-redemptions').textContent = counts.used ? num(counts.used) : '';
  for (const tab of ['config', 'codes', 'redemptions']) $(`#gc-${tab}`).hidden = tab !== giftPage.tab;
  if (giftPage.tab === 'config') setHTML($('#gc-config'), giftConfigHTML(b));
  else renderGiftListTab();
}

function giftChoiceHTML(name, field, value, options, disabled) {
  return html`
    <div class="choice-row">
      ${Object.entries(options).map(
        ([key, option]) => html`
          <label class="choice">
            <input type="radio" name="${name}" value="${key}" data-gift="${field}"
              ${attr(value === key, 'checked')} ${attr(disabled, 'disabled')} />
            <span><b>${option.name}</b><small>${option.note}</small></span>
          </label>
        `,
      )}
    </div>
  `;
}

function giftConfigHTML(b) {
  const frozen = giftIssued();
  const used = giftCounts(b.id).used;
  return html`
    <section class="ws-section">
      <h3 class="section-title">基本信息</h3>
      <div class="ws-form">
        <label class="field span-2">
          <span class="field-label">批次名称（仅后台显示）</span>
          <input data-gift="name" maxlength="80" value="${b.name}" />
        </label>
        <div class="field span-2">
          <span class="field-label">有效期（UTC+8，留空不限）</span>
          <div class="range-pair">
            <input type="datetime-local" data-gift="starts_at" value="${chinaInputValue(b.starts_at)}"
              aria-label="开始时间（UTC+8）" />
            <span>至</span>
            <input type="datetime-local" data-gift="ends_at" value="${chinaInputValue(b.ends_at)}"
              aria-label="结束时间（UTC+8）" />
          </div>
        </div>
        <div class="field field-switch span-2">
          <label class="switch">
            <input type="checkbox" data-gift="enabled" ${attr(b.enabled, 'checked')} />
            <span>${b.published ? '允许兑换' : '发布后允许兑换'}</span>
          </label>
          <span class="hint">关闭后所有码暂停兑换；已领取的奖励不会收回。</span>
        </div>
      </div>
    </section>
    <section class="ws-section">
      <h3 class="section-title">兑换规则${frozen && html`<span class="lock-note">已发码，已固定</span>`}</h3>
      <div class="field">
        <span class="field-label">码类型</span>
        ${giftChoiceHTML('gc-mode', 'mode', b.mode, GIFT_MODES, frozen)}
      </div>
      <div class="field">
        <span class="field-label">同一账号最多领几次</span>
        ${giftChoiceHTML('gc-scope', 'claim_scope', b.claim_scope, GIFT_SCOPES, frozen)}
      </div>
      <div class="ws-form">
        ${
          b.claim_scope === 'group' &&
          html`
          <label class="field">
            <span class="field-label">领取组</span>
            <input data-gift="group" maxlength="80" value="${b.group}" placeholder="例如 2026-秋季礼包"
              ${attr(frozen, 'disabled')} />
          </label>
        `
        }
        <label class="field">
          <span class="field-label">全批次总额度（0 为不限）</span>
          <span class="input-unit">
            <input type="number" min="0" data-gift="limit" value="${b.limit}" />
            <span>次</span>
          </span>
          ${used > 0 && html`<span class="hint">已领取 ${num(used)} 次，额度不能低于此数</span>`}
        </label>
      </div>
      <div class="rule-example" id="gc-rule-summary" aria-live="polite">${giftRuleExampleHTML(b)}</div>
    </section>
    <section class="ws-section">
      <h3 class="section-title">兑换成功后发送的邮件${frozen && html`<span class="lock-note">奖励已固定</span>`}</h3>
      <div class="ws-form">
        <label class="field span-2">
          <span class="field-label">邮件标题</span>
          <input data-gift="title" maxlength="40" value="${b.title}" />
        </label>
        <label class="field span-2">
          <span class="field-label">邮件正文</span>
          <textarea data-gift="message" rows="3" maxlength="200">${b.message}</textarea>
        </label>
      </div>
      <div id="gc-rewards">${rewardListHTML(b.rewards, { locked: frozen, note: '发布前至少添加 1 份' })}</div>
    </section>
  `;
}

/** Codes and redemptions tabs. The generation form is kept unless `generation` asks to redraw it. */
function renderGiftListTab({ generation = true } = {}) {
  const b = giftPage.current;
  const tab = giftPage.tab;
  const box = $(`#gc-${tab}`);
  const record = giftCurrentRecord();
  if (!record?.published) {
    setHTML(
      box,
      html`<div class="empty"><b>${tab === 'codes' ? '发布后才能生成兑换码' : '暂无领取记录'}</b>
        ${record ? '在「礼包配置」核对后点击「发布」。' : '先保存并发布这个批次。'}</div>`,
    );
    return;
  }
  if (giftPage.list.kind !== tab) {
    giftPage.list = { ...giftPage.list, kind: tab, page: 0, rows: [], error: '' };
    loadGiftListPage();
  }
  if (tab === 'redemptions') {
    setHTML(box, giftRedemptionsTabHTML());
    return;
  }
  if (!box.querySelector('#gc-gen')) {
    setHTML(box, html`<div id="gc-gen"></div><div id="gc-code-list"></div>`);
    generation = true;
  }
  if (generation) setHTML($('#gc-gen'), giftGenerationHTML());
  setHTML($('#gc-code-list'), giftCodesListHTML(b));
}

function giftPagerHTML(total) {
  const list = giftPage.list;
  const pages = Math.max(1, Math.ceil(total / GIFT_PAGE_SIZE));
  return html`
    <div class="pager">
      <span class="page-info">第 ${list.page + 1} / ${pages} 页 · 每页 ${GIFT_PAGE_SIZE} 条</span>
      <button type="button" class="secondary" data-gc-page="-1" ${attr(list.page === 0 || list.loading, 'disabled')}>上一页</button>
      <button type="button" class="secondary" data-gc-page="1"
        ${attr(list.page >= pages - 1 || list.loading, 'disabled')}>下一页</button>
    </div>
  `;
}

function giftTableBody(cols, rowsHTML) {
  const list = giftPage.list;
  if (list.error) return raw(emptyRow(cols, '读取失败', list.error));
  if (list.loading && !list.rows.length) return raw(emptyRow(cols, '正在读取…'));
  if (!list.rows.length) return raw(emptyRow(cols, '暂无记录'));
  return rowsHTML;
}

function giftGenerationHTML() {
  const pending = giftPage.pending.get(giftPage.current.id);
  const dirty = giftDirty();
  if (pending?.unconfirmed) {
    const body = pending.body;
    return html`
      <div class="gc-pending" role="alert">
        <svg class="icon"><use href="#i-alert" /></svg>
        <div>
          <b>上次生成的结果未确认</b>
          <span>
            请求在服务器答复前中断（${body.codes.length ? `导入 ${body.codes.length} 个指定码` : `随机 ${body.count} 个`}）。
            重试会沿用同一操作编号 <code>${body.request_key.slice(0, 8)}</code>，服务器若已生成会直接返回原结果，不会重复发码。
          </span>
        </div>
        <div class="toolbar">
          <button type="button" class="primary sm" data-gc-action="retry">重试同一请求</button>
          <button type="button" class="secondary sm" data-gc-action="abandon">放弃并核对列表</button>
        </div>
      </div>
    `;
  }
  const random = giftPage.genMode === 'random';
  return html`
    <div class="gc-gen">
      <div class="gc-gen-head">
        <b>生成兑换码</b>
        ${segmented(
          'gc-gen-mode',
          giftPage.genMode,
          [
            ['random', '随机生成'],
            ['manual', '导入指定码'],
          ],
          { data: 'data-gc-gen="mode"' },
        )}
      </div>
      ${
        random
          ? html`
            <div class="ws-form">
              <label class="field">
                <span class="field-label">数量（1–5000）</span>
                <input type="number" min="1" max="5000" value="10" data-gc-gen="count" />
              </label>
              <label class="field">
                <span class="field-label">前缀（可选，字母／数字／连字符）</span>
                <input maxlength="20" placeholder="例如 AUTUMN-" data-gc-gen="prefix" />
              </label>
            </div>
            <p class="hint">随机部分为 20 位十六进制；码统一转大写，可用连字符。</p>
          `
          : html`
            <label class="field">
              <span class="field-label">每行一个码（6–64 位字母、数字或连字符，最多 5000 个）</span>
              <textarea rows="5" data-gc-gen="codes" placeholder="SPRING-2026-01"></textarea>
            </label>
          `
      }
      <div class="toolbar">
        <button type="button" class="primary sm" data-gc-action="generate" ${attr(dirty, 'disabled')}>核对并生成</button>
        <span class="hint">${dirty ? '请先保存批次修改。' : '生成后不能删除，只能逐个停用；导出不会生成新码。'}</span>
      </div>
    </div>
  `;
}

function giftCodesListHTML(b) {
  const counts = giftCounts(b.id);
  return html`
    <div class="toolbar list-toolbar">
      <b>兑换码</b><span class="hint">共 ${num(counts.issued)} 个 · 已领取 ${num(counts.used)} 次</span>
      <span class="spacer"></span>
      <button type="button" class="secondary sm" data-gc-action="export" ${attr(!counts.issued, 'disabled')}>
        <svg class="icon"><use href="#i-download" /></svg>导出全部（CSV）
      </button>
    </div>
    <div class="table-wrap">
      <table class="dense gc-table">
        <thead>
          <tr><th>编号</th><th>兑换码</th><th class="num">已兑换</th><th>状态</th><th><span class="sr-only">操作</span></th></tr>
        </thead>
        <tbody>
          ${giftTableBody(
            5,
            giftPage.list.rows.map(
              (row) => html`
                <tr class="${row.enabled ? '' : 'closed'}">
                  <td><code>${row.id}</code></td>
                  <td><code class="gc-code">${row.code}</code></td>
                  <td class="num">${num(row.used)}</td>
                  <td>${row.enabled ? stateTag('启用', 'ok') : stateTag('停用', 'warn')}</td>
                  <td>
                    <button type="button" class="ghost ${row.enabled ? 'danger-text' : ''}" data-gc-code="${row.id}">
                      ${row.enabled ? '停用' : '启用'}
                    </button>
                  </td>
                </tr>
              `,
            ),
          )}
        </tbody>
      </table>
    </div>
    ${giftPagerHTML(counts.issued)}
  `;
}

/** present_ids is a JSON array of 64-bit mail IDs; split the text so large IDs keep every digit. */
function giftMailIDs(value) {
  const match = /^\[([\d,\s]*)\]$/.exec(String(value).trim());
  return match
    ? match[1]
        .split(',')
        .map((id) => id.trim())
        .filter(Boolean)
        .join('、')
    : value;
}

function giftRedemptionsTabHTML() {
  const counts = giftCounts(giftPage.current.id);
  return html`
    <div class="toolbar list-toolbar">
      <b>领取记录</b><span class="hint">共 ${num(counts.used)} 次 · 新的在前；领取记录与邮件同时提交</span>
    </div>
    <div class="table-wrap">
      <table class="dense">
        <thead>
          <tr><th>玩家</th><th>兑换码编号</th><th>领取时间（UTC+8）</th><th>邮件 ID</th><th class="num">奖励版本</th></tr>
        </thead>
        <tbody>
          ${giftTableBody(
            5,
            giftPage.list.rows.map(
              (row) => html`
                <tr>
                  <td><code>${row.user_id}</code></td>
                  <td><code>${row.code_id}</code></td>
                  <td>${chinaText(row.claimed_at)}</td>
                  <td class="cell-wrap">${giftMailIDs(row.present_ids)}</td>
                  <td class="num">r${row.revision}</td>
                </tr>
              `,
            ),
          )}
        </tbody>
      </table>
    </div>
    ${giftPagerHTML(counts.used)}
  `;
}

function renderGiftSavebar() {
  const b = giftPage.current;
  const record = giftCurrentRecord();
  const dirty = giftDirty();
  $('#gc-version').textContent = `配置 r${giftPage.revision}`;
  let text = '选择或新建一个批次';
  let kind = '';
  if (b && !record) [text, kind] = ['新批次尚未保存', 'dirty'];
  else if (dirty)
    [text, kind] = [record.published ? '有未保存的修改 · 保存后立即生效' : '有未保存的修改', 'dirty'];
  else if (record && !record.published) text = '草稿已保存 · 发布后才能生成兑换码';
  else if (record) [text, kind] = ['已发布 · 修改保存后立即生效', 'ok'];
  setSaveState('#gc-state', text, kind);
  const live = !!record?.published;
  $('#gc-discard').hidden = !dirty;
  $('#gc-discard').textContent = record ? '放弃修改' : '放弃新批次';
  $('#gc-save').hidden = live;
  $('#gc-save').disabled = !b || !dirty;
  $('#gc-publish').disabled = !b || (live && !dirty);
  $('#gc-publish').textContent = live ? '保存并生效' : '发布';
}

/** Refresh after a field edit; focused inputs are not re-rendered. */
function giftTouched() {
  renderGiftHead();
  renderGiftSavebar();
  renderGiftList();
  const summary = $('#gc-rule-summary');
  if (summary) setHTML(summary, giftRuleExampleHTML(giftPage.current));
  updateDraftIndicators();
}

// Editing ----------------------------------------------------------------------------------------

function setGiftField(field, element) {
  const b = giftPage.current;
  if (field === 'starts_at' || field === 'ends_at') {
    try {
      b[field] = chinaUnix(element.value);
      element.classList.remove('invalid');
    } catch (error) {
      markInvalid(element, error.message);
      return toast(error.message, true);
    }
  } else if (field === 'enabled') b.enabled = element.checked;
  else if (field === 'limit') b.limit = Number(element.value) || 0;
  else b[field] = element.value;
  if (field === 'claim_scope') {
    setHTML($('#gc-config'), giftConfigHTML(b));
    if (b.claim_scope === 'group') $('#gc-config [data-gift="group"]')?.focus();
  }
  giftTouched();
}

// Server actions ---------------------------------------------------------------------------------

const giftRequest = (batch, preview = false) =>
  api('/api/gift-code-batches', {
    method: 'POST',
    body: JSON.stringify({ expected_revision: giftPage.revision, batch, preview }),
  });

function giftBlocked(problems, title) {
  if (!problems.length) return false;
  showViewAlert('gift-codes', { title, message: problems.join('；') });
  return true;
}

function giftReviewRows(batch, saved) {
  const scope = (b) =>
    b.claim_scope === 'group' ? `领取组「${b.group}」合计 1 次` : GIFT_SCOPES[b.claim_scope].name;
  const pairs = [
    ['批次名称', saved?.name, batch.name],
    ['码类型', saved && GIFT_MODES[saved.mode].name, GIFT_MODES[batch.mode].name],
    ['同一账号可领', saved && scope(saved), scope(batch)],
    ['总额度', saved && (saved.limit || '不限'), batch.limit || '不限'],
    [
      '有效期（UTC+8）',
      saved && chinaWindowText(saved.starts_at, saved.ends_at),
      chinaWindowText(batch.starts_at, batch.ends_at),
    ],
    ['允许兑换', saved?.enabled, batch.enabled],
    ['邮件标题', saved?.title, batch.title],
    ['邮件正文', saved?.message, batch.message],
    ['奖励', saved && rewardListText(saved.rewards), rewardListText(batch.rewards)],
  ];
  return saved?.published ? diffRows(pairs) : pairs.map(([label, , after]) => [label, diffText(after)]);
}

async function saveGiftBatch({ publish = false, batch = giftPage.current, title, confirmText } = {}) {
  if (!batch) return;
  const record = giftRecord(batch.id);
  const next = { ...structuredClone(batch), published: publish || !!record?.published };
  // Mirrors the server: only disabling an already published batch skips the readiness checks.
  const ready = next.published && (next.enabled || !record?.published);
  if (giftBlocked(giftProblems(next, ready), '不能保存')) return;
  if (next.published) {
    let checked = null;
    await contentAction(
      'gift-codes',
      async () => {
        checked = (await giftRequest(next, true)).batch;
      },
      '校验',
    );
    if (!checked) return;
    const ok = await reviewChanges({
      title: title || (record?.published ? '保存兑换批次（立即生效）' : '发布兑换批次'),
      lead: `${checked.name} · ${giftRuleText(checked)}`,
      sections: [
        {
          title: record?.published ? '相对当前版本的变化' : '批次配置',
          summary: !record?.published,
          rows: giftReviewRows(checked, record),
        },
      ],
      note: record?.published
        ? '保存后玩家下次兑换即按新配置判断。'
        : '发布后可生成兑换码；首次发码后码类型、限领方式和奖励固定。',
      confirmText: confirmText || (record?.published ? '确认保存' : '确认发布'),
    });
    if (!ok) return;
  }
  await contentAction('gift-codes', async () => {
    const data = await giftRequest(next);
    applyGiftRegistry(data);
    const saved = giftRecord(next.id) || data.batches.at(-1);
    selectGiftBatch(saved, giftPage.tab);
    state.loaded.delete('audit');
    clearViewAlert('gift-codes');
    renderGiftCodes();
    toast(
      publish ? '兑换批次已发布，可以生成兑换码' : next.published ? '兑换批次已保存并生效' : '草稿已保存',
    );
  });
}

async function toggleGiftBatch() {
  if (giftDirty()) return toast('请先保存或放弃当前修改，再切换开放状态', true);
  const record = giftCurrentRecord();
  const batch = { ...structuredClone(record), enabled: !record.enabled };
  await saveGiftBatch({
    batch,
    title: batch.enabled ? '重新开放兑换' : '停用整批兑换码',
    confirmText: batch.enabled ? '确认开放' : '确认停用',
  });
}

async function loadGiftListPage() {
  const b = giftPage.current;
  const list = giftPage.list;
  const serial = ++list.serial;
  list.loading = true;
  list.error = '';
  try {
    const kind = list.kind === 'redemptions' ? '&kind=redemptions' : '';
    const data = await api(
      `/api/gift-code-batches/${b.id}/codes?offset=${list.page * GIFT_PAGE_SIZE}${kind}`,
    );
    if (serial !== giftPage.list.serial) return;
    list.rows = data.rows;
  } catch (error) {
    if (serial !== giftPage.list.serial) return;
    list.rows = [];
    list.error = errorText(error);
  } finally {
    if (serial === giftPage.list.serial) {
      list.loading = false;
      if (giftPage.tab === list.kind) renderGiftListTab({ generation: false });
    }
  }
}

async function refreshGiftCounts() {
  const data = await api('/api/gift-code-batches');
  applyGiftRegistry(data);
  const saved = giftRecord(giftPage.current?.id);
  if (saved && !giftDirty()) {
    giftPage.current = structuredClone(saved);
    giftPage.savedJSON = stableJSON(saved);
  }
}

function readGiftGeneration() {
  const box = $('#gc-codes');
  const b = giftPage.current;
  const body = { batch_id: b.id, expected_revision: giftPage.revision, count: 0, prefix: '', codes: [] };
  clearInvalid(box);
  if (giftPage.genMode === 'manual') {
    const input = box.querySelector('[data-gc-gen="codes"]');
    const codes = input.value
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean);
    const bad = codes.find((code) => !/^[A-Za-z0-9-]{6,64}$/.test(code));
    const fail = (message) => {
      markInvalid(input, message);
      throw new Error(message);
    };
    if (!codes.length) fail('请输入至少一个兑换码');
    if (codes.length > 5000) fail('每次最多导入 5000 个码');
    if (bad) fail(`「${bad}」不是 6–64 位的字母、数字或连字符`);
    const canonical = codes.map((code) => code.toUpperCase());
    if (new Set(canonical).size !== canonical.length) fail('输入中有重复的码（大小写视为相同）');
    body.codes = codes;
  } else {
    const count = box.querySelector('[data-gc-gen="count"]');
    const prefix = box.querySelector('[data-gc-gen="prefix"]');
    body.count = Number(count.value);
    body.prefix = prefix.value.trim();
    if (!Number.isInteger(body.count) || body.count < 1 || body.count > 5000) {
      markInvalid(count, '数量须为 1–5000');
      throw new Error('数量须为 1–5000');
    }
    if (!/^[A-Za-z0-9-]{0,20}$/.test(body.prefix)) {
      markInvalid(prefix, '前缀最多 20 位字母、数字或连字符');
      throw new Error('前缀最多 20 位字母、数字或连字符');
    }
  }
  return body;
}

async function generateGiftCodes() {
  const b = giftPage.current;
  if (giftDirty()) return toast('请先保存批次修改，再生成兑换码', true);
  let body;
  try {
    body = readGiftGeneration();
  } catch (error) {
    return toast(error.message, true);
  }
  const ok = await reviewChanges({
    title: '生成兑换码',
    lead: `${b.name} · ${GIFT_MODES[b.mode].name}`,
    sections: [
      {
        title: '本次生成',
        summary: true,
        rows: [
          [
            '方式',
            body.codes.length ? '导入指定码' : `随机生成${body.prefix ? `（前缀 ${body.prefix}）` : ''}`,
          ],
          ['数量', body.codes.length || body.count],
          ['每次兑换的奖励', rewardListText(b.rewards)],
          ['兑换规则', giftRuleText(b)],
        ],
      },
    ],
    note: '码生成后不能删除，只能停用；首次发码后码类型、限领方式和奖励固定。',
    confirmText: '确认生成',
  });
  if (!ok) return;
  giftPage.pending.set(b.id, { body: { ...body, request_key: crypto.randomUUID() }, unconfirmed: false });
  await runGiftGeneration();
}

async function runGiftGeneration() {
  const batchID = giftPage.current.id;
  const pending = giftPage.pending.get(batchID);
  if (!pending) return;
  await contentAction(
    'gift-codes',
    async () => {
      let result;
      try {
        result = await api('/api/gift-codes/generate', {
          method: 'POST',
          body: JSON.stringify(pending.body),
        });
      } catch (error) {
        // No status or a server error: the transaction may have committed, so keep the same key.
        if (!error.status || error.status >= 500) pending.unconfirmed = true;
        else giftPage.pending.delete(batchID);
        throw error;
      }
      giftPage.pending.delete(batchID);
      await refreshGiftCounts();
      const issued = giftCounts(batchID).issued;
      giftPage.list = {
        ...giftPage.list,
        kind: 'codes',
        page: Math.max(0, Math.ceil(issued / GIFT_PAGE_SIZE) - 1),
        rows: [],
      };
      clearViewAlert('gift-codes');
      state.loaded.delete('audit');
      renderGiftCodes();
      loadGiftListPage();
      toast(`已生成 ${result.count} 个兑换码，已跳到列表最后一页`);
    },
    '生成',
  );
  if (giftPage.tab === 'codes' && giftPage.current?.id === batchID) renderGiftListTab();
}

async function abandonGiftGeneration() {
  const ok = await askConfirm(
    '放弃这次未确认的生成？',
    '如果服务器已经生成，这些码会出现在列表中；之后若再生成会使用新的操作编号。',
    { ok: '放弃', danger: true },
  );
  if (!ok) return;
  giftPage.pending.delete(giftPage.current.id);
  await contentAction(
    'gift-codes',
    async () => {
      await refreshGiftCounts();
      giftPage.list = { ...giftPage.list, kind: '', rows: [] };
      renderGiftCodes();
    },
    '刷新',
  );
}

async function toggleGiftCode(button) {
  const id = Number(button.dataset.gcCode);
  const row = giftPage.list.rows.find((r) => r.id === id);
  if (!row) return;
  await contentAction(
    'gift-codes',
    async () => {
      try {
        const data = await api('/api/gift-codes/state', {
          method: 'POST',
          body: JSON.stringify({ id, expected_enabled: row.enabled, enabled: !row.enabled }),
        });
        row.enabled = data.enabled;
        toast(`兑换码 ${row.code} 已${row.enabled ? '启用' : '停用'}`);
      } catch (error) {
        if (error.status === 409) loadGiftListPage();
        throw error;
      }
      renderGiftListTab({ generation: false });
    },
    '切换兑换码',
  );
}

async function exportGiftCodes() {
  const b = giftCurrentRecord();
  const total = giftCounts(b.id).issued;
  if (!total) return toast('本批次还没有兑换码');
  await contentAction(
    'gift-codes',
    async () => {
      const offsets = Array.from({ length: Math.ceil(total / GIFT_PAGE_SIZE) }, (_, i) => i * GIFT_PAGE_SIZE);
      const pages = await mapLimited(offsets, 4, async (offset) => {
        const data = await api(`/api/gift-code-batches/${b.id}/codes?offset=${offset}`);
        return data.rows;
      });
      const rows = pages.flat();
      const quote = (value) => `"${String(value).replaceAll('"', '""')}"`;
      const lines = [
        ['编号', '兑换码', '启用', '已兑换次数'].join(','),
        ...rows.map((r) => [r.id, quote(r.code), r.enabled ? 1 : 0, r.used].join(',')),
      ];
      downloadText(`﻿${lines.join('\r\n')}\r\n`, `兑换码-${b.id}.csv`, 'text/csv;charset=utf-8');
      toast(`已导出 ${rows.length} 个兑换码（只读取，没有生成新码）`);
    },
    '导出',
  );
}

async function discardGiftChanges() {
  if (!(await confirmDiscard(true, '页面会恢复为已保存的版本。'))) return;
  selectGiftBatch(giftCurrentRecord() || giftPage.rows[0] || null);
  clearViewAlert('gift-codes');
  renderGiftCodes();
}

// Events -----------------------------------------------------------------------------------------

function onGiftEdit(event) {
  const el = event.target;
  if (!giftPage.current || !el.matches('input, select, textarea')) return;
  if (el.closest('#gc-rewards')) {
    if (applyRewardEvent(event, giftPage.current.rewards)) giftTouched();
    return;
  }
  if (el.dataset.gcGen === 'mode' && event.type === 'change') {
    giftPage.genMode = el.value;
    renderGiftListTab();
    return;
  }
  if (!el.dataset.gift || (event.type === 'input') !== updatesOnInput(el)) return;
  setGiftField(el.dataset.gift, el);
}

async function onGiftClick(event) {
  const pick = (selector) => event.target.closest(selector);
  let el;
  if ((el = pick('[data-gc-select]'))) {
    const id = Number(el.dataset.gcSelect);
    if (id === giftPage.current?.id) return;
    if (!(await confirmDiscard(giftDirty(), '切换礼包会放弃当前未保存的修改。', '放弃并切换'))) return;
    selectGiftBatch(giftRecord(id));
    clearViewAlert('gift-codes');
    renderGiftCodes();
  } else if ((el = pick('[data-gc-tab]'))) {
    giftPage.tab = el.dataset.gcTab;
    renderGiftTabs();
  } else if ((el = pick('[data-gc-page]'))) {
    giftPage.list.page = Math.max(0, giftPage.list.page + Number(el.dataset.gcPage));
    loadGiftListPage();
    renderGiftListTab({ generation: false });
  } else if ((el = pick('[data-gc-code]'))) {
    toggleGiftCode(el);
  } else if (pick('#gc-rewards [data-reward-add]')) {
    const rewards = giftPage.current.rewards;
    pickRewards(rewards, {
      onAdded: () => {
        setHTML($('#gc-rewards'), rewardListHTML(rewards, { note: '发布前至少添加 1 份' }));
        giftTouched();
      },
    });
  } else if (pick('#gc-rewards')) {
    const rewards = giftPage.current.rewards;
    if (applyRewardEvent(event, rewards) === 'structure') {
      setHTML(
        $('#gc-rewards'),
        rewardListHTML(rewards, { locked: giftIssued(), note: '发布前至少添加 1 份' }),
      );
      giftTouched();
    }
  } else if ((el = pick('[data-gc-action]'))) {
    const action = el.dataset.gcAction;
    if (action === 'new') newGiftBatch();
    else if (action === 'copy') newGiftBatch(giftPage.current);
    else if (action === 'save') saveGiftBatch();
    else if (action === 'publish') saveGiftBatch({ publish: true });
    else if (action === 'toggle') toggleGiftBatch();
    else if (action === 'discard') discardGiftChanges();
    else if (action === 'generate') generateGiftCodes();
    else if (action === 'retry') runGiftGeneration();
    else if (action === 'abandon') abandonGiftGeneration();
    else if (action === 'export') exportGiftCodes();
  }
}

$('#gift-codes').addEventListener('input', onGiftEdit);
$('#gift-codes').addEventListener('change', onGiftEdit);
$('#gift-codes').addEventListener('click', onGiftClick);
$('#gc-search').addEventListener('input', renderGiftList);
$('#gc-status').addEventListener('change', renderGiftList);
