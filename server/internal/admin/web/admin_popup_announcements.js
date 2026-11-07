'use strict';
// 公告与奖励 › 首页弹窗公告 and 维护工具 › 清理首页弹窗公告.
// Unlike 游戏公告 (a page the player opens), these pop up by themselves: once a player has finished every
// tutorial task, each published, unread announcement shows once on reaching Home, in publish order.
// Published content is fixed; changing the text means copying it into a new announcement.
// Both views read one board from GET /api/popup-announcements; every change goes through its POST.

const POPUP_LIMITS = { title: 40, body: 500, button_text: 12, live: 20, total: 1000 };
const POPUP_DEFAULT_BUTTON = '确定';
const POPUP_PHASES = {
  live: ['生效中', 'ok'],
  pending: ['待生效', 'info'],
  draft: ['草稿', 'warn'],
  ended: ['已过期', 'muted'],
  stopped: ['已停用', 'muted'],
};
const POPUP_FIELDS = {
  title: '#popup-title',
  body: '#popup-body',
  button_text: '#popup-button',
  start_unix: '#popup-start',
  end_unix: '#popup-end',
};

const popupBoard = {
  items: [],
  revision: 0,
  limits: POPUP_LIMITS,
  skew: 0, // server clock − browser clock, in seconds; phases follow the server
  loaded: false,
  busy: false,
};

const popupEditor = {
  selected: null, // announcement ID; 0 = a new announcement not saved yet; null = nothing open
  form: null, // { title, body, button_text, start_unix, end_unix } of the open draft
  saved: null, // that draft as the server has it; null while it is new
  query: '',
  alert: null, // { kind, title, message, retry } shown above the editor
  note: '', // why the open draft looks the way it does (copied, or its original vanished)
};

const popupCleanup = { selected: new Set(), report: '' };

// Model ------------------------------------------------------------------------------------------

const popupNow = () => Math.floor(Date.now() / 1000) + popupBoard.skew;
const popupLength = (text) => [...text].length; // the server counts characters, not UTF-16 units
const popupItem = (id) => popupBoard.items.find((p) => p.id === id);
const popupBlank = () => ({
  title: '',
  body: '',
  button_text: POPUP_DEFAULT_BUTTON,
  start_unix: 0,
  end_unix: 0,
});
const popupContent = (p) => ({
  title: p.title,
  body: p.body,
  button_text: p.button_text,
  start_unix: p.start_unix || 0,
  end_unix: p.end_unix || 0,
});

function popupPhase(p, now = popupNow()) {
  if (p.status === 'draft') return 'draft';
  if (p.status === 'stopped') return 'stopped';
  if (p.end_unix && p.end_unix <= now) return 'ended';
  if (p.start_unix > now) return 'pending';
  return 'live';
}

const popupShown = (p) => ['live', 'pending'].includes(popupPhase(p));
const popupCleanable = (p) => !popupShown(p);

/** Published, not yet ended announcements in the order players receive them. */
const popupQueue = () =>
  popupBoard.items.filter(popupShown).sort((a, b) => a.published_at - b.published_at || a.id - b.id);

function popupDirty() {
  const e = popupEditor;
  if (e.selected == null || !e.form) return false;
  return stableJSON(e.form) !== stableJSON(e.saved || popupBlank());
}

const popupShort = (unix) => chinaText(unix).slice(5); // MM-DD HH:MM, UTC+8

function popupScheduleText(p) {
  if (!p.start_unix && !p.end_unix) return '立即生效，不限结束';
  const start = p.start_unix ? `${chinaText(p.start_unix)} 开始` : '立即生效';
  return `${start}，${p.end_unix ? `${chinaText(p.end_unix)} 结束` : '不限结束'}`;
}

function popupListMeta(p, phase) {
  if (!p.id) return '尚未保存';
  if (phase === 'live')
    return `${popupShort(p.published_at)} 发布 · ${p.end_unix ? `${popupShort(p.end_unix)} 结束` : '不限结束'}`;
  if (phase === 'pending') return `${popupShort(p.start_unix)} 开始`;
  if (phase === 'ended') return `${popupShort(p.end_unix)} 已结束`;
  if (phase === 'stopped') return `${popupShort(p.published_at)} 发布 · 已停用`;
  return p.start_unix || p.end_unix ? `计划：${popupScheduleText(p)}` : '未发布';
}

function popupProblems(form, publish) {
  const l = popupBoard.limits;
  const out = [];
  if (popupLength(form.title) > l.title) out.push(['title', `标题最多 ${l.title} 字`]);
  else if (publish && !form.title.trim()) out.push(['title', '发布前请填写标题']);
  if (popupLength(form.body) > l.body) out.push(['body', `正文最多 ${l.body} 字`]);
  else if (publish && !form.body.trim()) out.push(['body', '发布前请填写正文']);
  if (popupLength(form.button_text) > l.button_text)
    out.push(['button_text', `按钮文字最多 ${l.button_text} 字`]);
  if (form.start_unix && form.end_unix && form.end_unix <= form.start_unix)
    out.push(['end_unix', '结束时间须晚于开始时间']);
  else if (publish && form.end_unix && form.end_unix <= popupNow())
    out.push(['end_unix', '结束时间已过，请改晚或清空']);
  return out;
}

// Loading ----------------------------------------------------------------------------------------

/**
 * select: open this ID afterwards with its saved content (after our own change; null closes the editor).
 * reset: the page was reloaded on purpose, so unsaved edits are dropped.
 * Otherwise an edited draft keeps its text, even when the board changed under it.
 */
async function loadPopupBoard({ select, reset = false } = {}) {
  const data = await api('/api/popup-announcements');
  popupBoard.items = data.items || [];
  popupBoard.revision = data.revision;
  popupBoard.limits = { ...POPUP_LIMITS, ...data.limits };
  popupBoard.skew = data.server_time ? data.server_time - Math.floor(Date.now() / 1000) : 0;
  popupBoard.loaded = true;
  if (select !== undefined) openPopupItem(select);
  else if (reset)
    openPopupItem(popupEditor.selected && popupItem(popupEditor.selected) ? popupEditor.selected : null);
  else reconcilePopupEditor();
  if (popupEditor.selected == null && (reset || select === undefined))
    openPopupItem(popupListOrder()[0]?.id ?? null);
  renderPopupBoard();
}

/** Loads for 公告与奖励; a failure stays inside this section so the rest of the page still works. */
async function reloadPopupSection(options) {
  try {
    await loadPopupBoard(options);
  } catch (e) {
    popupEditor.alert = { kind: 'error', title: '首页弹窗公告读取失败', message: errorText(e), retry: true };
    renderPopupAlert();
  }
}

function openPopupItem(id) {
  const e = popupEditor;
  const item = id ? popupItem(id) : null;
  e.selected = item ? item.id : null;
  e.saved = item?.status === 'draft' ? popupContent(item) : null;
  e.form = e.saved ? { ...e.saved } : null;
  e.note = '';
}

function reconcilePopupEditor() {
  const e = popupEditor;
  if (e.selected == null || e.selected === 0) return;
  const item = popupItem(e.selected);
  if (!popupDirty()) return openPopupItem(item ? item.id : null);
  if (item?.status === 'draft') {
    e.saved = popupContent(item);
    return;
  }
  // The edited draft was published, deleted or cleaned elsewhere: keep the text as a new announcement.
  e.selected = 0;
  e.saved = null;
  e.note = '原草稿已在其他页面发布或删除，这里的内容保留为新公告，保存或发布时另起编号。';
}

// 公告与奖励 › list --------------------------------------------------------------------------------

function popupListOrder() {
  const order = { live: 0, pending: 0, draft: 1, ended: 2, stopped: 2 };
  const queue = new Map(popupQueue().map((p, i) => [p.id, i]));
  return [...popupBoard.items].sort((a, b) => {
    const pa = popupPhase(a);
    const pb = popupPhase(b);
    if (order[pa] !== order[pb]) return order[pa] - order[pb];
    if (order[pa] === 0) return queue.get(a.id) - queue.get(b.id);
    if (pa === 'draft') return b.id - a.id;
    return b.published_at - a.published_at || b.id - a.id;
  });
}

const POPUP_GROUPS = [
  [0, '玩家会看到', '按发布顺序弹出'],
  [1, '草稿', ''],
  [2, '已结束', '可在维护工具中清理'],
];

function renderPopupList() {
  const e = popupEditor;
  const q = e.query.trim().toLowerCase();
  const match = (p) => !q || p.title.toLowerCase().includes(q) || String(p.id).includes(q);
  const group = (p) => ({ live: 0, pending: 0, draft: 1 })[popupPhase(p)] ?? 2;
  const rows = popupListOrder().filter(match);
  const unsaved = e.selected === 0 ? [{ id: 0, status: 'draft', ...e.form }] : [];
  const item = (p) => {
    const phase = popupPhase(p);
    const [text, tone] = POPUP_PHASES[phase];
    return html`<button type="button" class="ws-item ${attr(p.id === e.selected, 'selected')}" data-pa-id="${p.id}">
      <span class="ws-item-title"
        ><b>${p.title.trim() || '未命名弹窗'}</b>${stateTag(text, tone)}${
          p.id ? html`<small>${p.id}</small>` : stateTag('未保存', 'warn')
        }</span
      >
      <span class="ws-item-meta">${popupListMeta(p, phase)}</span>
    </button>`;
  };
  const sections = POPUP_GROUPS.map(([key, title, hint]) => {
    const list = rows.filter((p) => group(p) === key);
    if (key === 1) list.unshift(...unsaved);
    return list.length
      ? html`<div class="pa-group"><span>${title} · ${list.length}</span>${hint ? html`<small>${hint}</small>` : ''}</div>
          ${list.map(item)}`
      : '';
  });
  const empty = !popupBoard.items.length && !unsaved.length;
  setHTML(
    $('#popup-list'),
    empty
      ? html`<p class="ws-list-empty">还没有首页弹窗公告。<br />点「新建」写第一条。</p>`
      : rows.length || unsaved.length
        ? sections
        : html`<p class="ws-list-empty">没有标题或编号匹配的公告。</p>`,
  );
}

function renderPopupCount() {
  const queue = popupQueue();
  const live = queue.filter((p) => popupPhase(p) === 'live').length;
  const l = popupBoard.limits;
  $('#popup-count').textContent = popupBoard.loaded
    ? `生效中 ${live} · 待生效 ${queue.length - live}（上限 ${l.live}）`
    : '';
  $('#popup-count').className = `pill${queue.length >= l.live ? ' gold' : ''}`;
  const full = popupBoard.items.length >= l.total;
  $('#popup-new').disabled = !popupBoard.loaded || full;
  $('#popup-new').title = full ? `已有 ${l.total} 条公告，请先在维护工具中清理` : '';
}

function renderPopupAlert() {
  const a = popupEditor.alert;
  setHTML(
    $('#popup-alert'),
    a
      ? html`<div class="view-alert ${a.kind === 'warn' ? 'warn' : ''}" role="alert">
          <svg class="icon" aria-hidden="true"><use href="#i-alert" /></svg>
          <div class="alert-text"><b>${a.title}</b><span>${a.message}</span></div>
          ${
            a.retry
              ? html`<div class="alert-actions">
                <button type="button" class="primary sm" data-pa-act="reload">重新获取列表</button>
              </div>`
              : ''
          }
          <button class="x-button" type="button" data-pa-act="close-alert" aria-label="关闭提示">
            <svg class="icon"><use href="#i-close" /></svg>
          </button>
        </div>`
      : '',
  );
}

// 公告与奖励 › editor ------------------------------------------------------------------------------

const popupPreviewHTML = () => html`<figure class="pa-preview">
  <div class="pa-screen" aria-label="玩家看到的弹窗">
    <div class="pa-dialog">
      <b class="pa-dialog-title" data-pv="title"></b>
      <div class="pa-dialog-body" data-pv="body"></div>
      <span class="pa-dialog-button" data-pv="button"></span>
    </div>
  </div>
  <figcaption>玩家看到的样子 · 示意图，字体与排版以游戏为准</figcaption>
</figure>`;

function popupFormHTML(item) {
  const f = popupEditor.form;
  const l = popupBoard.limits;
  const label = (id, text, field) =>
    html`<div class="pa-label"><label for="${id}">${text}</label><span class="pa-count" data-count="${field}"></span></div>`;
  return html`<div class="ws-head">
      <div class="ws-title">
        <span class="eyebrow">${item ? `草稿 · 编号 ${item.id}` : '新公告 · 保存后分配编号'}</span>
        <h3 data-pa="title"></h3>
        <div class="ws-tags">${stateTag('草稿', 'warn')}<span class="pa-dirty" data-pa="dirty" hidden>有未保存的修改</span></div>
      </div>
      <div class="ws-actions">
        ${
          item
            ? html`<button type="button" class="ghost danger-text" data-pa-act="delete">
              <svg class="icon"><use href="#i-trash" /></svg>删除草稿
            </button>`
            : html`<button type="button" class="ghost" data-pa-act="discard">放弃</button>`
        }
        <button type="button" class="secondary" data-pa-act="save">保存草稿</button>
        <button type="button" class="primary" data-pa-act="publish">发布…</button>
      </div>
    </div>
    ${
      popupEditor.note
        ? html`<div class="ws-lock"><svg class="icon"><use href="#i-info" /></svg><span>${popupEditor.note}</span></div>`
        : ''
    }
    <div class="pa-grid">
      <div class="pa-form">
        <div class="field">
          ${label('popup-title', '标题', 'title')}
          <input
            id="popup-title"
            data-pa-field="title"
            maxlength="${l.title}"
            value="${f.title}"
            placeholder="例如：版本更新说明"
            autocomplete="off"
          />
        </div>
        <div class="field">
          ${label('popup-body', '正文', 'body')}
          <textarea id="popup-body" data-pa-field="body" rows="8" maxlength="${l.body}" placeholder="纯文字，可以换行">${f.body}</textarea>
        </div>
        <div class="pa-times">
          <div class="field">
            ${label('popup-button', '按钮文字', 'button_text')}
            <input
              id="popup-button"
              data-pa-field="button_text"
              maxlength="${l.button_text}"
              value="${f.button_text}"
              placeholder="${POPUP_DEFAULT_BUTTON}"
              autocomplete="off"
            />
          </div>
          <div class="field">
            <label for="popup-start">开始时间（北京时间，留空立即）</label>
            <input id="popup-start" type="datetime-local" data-pa-field="start_unix" value="${chinaInputValue(f.start_unix)}" />
          </div>
          <div class="field">
            <label for="popup-end">结束时间（北京时间，留空不限）</label>
            <input id="popup-end" type="datetime-local" data-pa-field="end_unix" value="${chinaInputValue(f.end_unix)}" />
          </div>
        </div>
        <p class="pa-schedule" data-pa="schedule"></p>
      </div>
      ${popupPreviewHTML()}
    </div>
    <p class="pa-delivery">
      <svg class="icon" aria-hidden="true"><use href="#i-info" /></svg>
      <span
        >玩家完成全部新手任务后，下次登录或回到首页时弹出，每位玩家确认一次后不再弹出；没确认就退出的，下次回首页还会弹出。发布后内容不能修改。</span
      >
    </p>`;
}

function popupReadonlyHTML(p) {
  const phase = popupPhase(p);
  const [text, tone] = POPUP_PHASES[phase];
  const queue = popupQueue();
  const position = queue.findIndex((q) => q.id === p.id);
  const status = {
    live: '生效中：完成新手任务的玩家回到首页时弹出',
    pending: `待生效：${chinaText(p.start_unix)} 开始弹出`,
    ended: `已过期：${chinaText(p.end_unix)} 起不再弹出`,
    stopped: '已停用：不再弹出',
  }[phase];
  const lock = {
    live: '已发布的内容不能修改。要改文字或再提醒一次，请复制为新公告后发布。停用后不再发给新玩家，已收到但还没确认的玩家仍会看到这一条。',
    pending:
      '已发布的内容不能修改。要改文字或时间，请复制为新公告后发布，再停用这一条。停用后不再发给新玩家，已收到但还没确认的玩家仍会看到这一条。',
    ended: '已过期，不再弹出。需要再提醒一次时复制为新公告；不再需要时可在维护工具中清理。',
    stopped: '已停用，不能重新发布。需要再提醒一次时复制为新公告；不再需要时可在维护工具中清理。',
  }[phase];
  const facts = [
    ['状态', status],
    ['生效时间', `${popupScheduleText(p)}（北京时间）`],
    ['发布时间', chinaText(p.published_at)],
    ['按钮文字', p.button_text],
  ];
  if (position >= 0)
    facts.push(['弹出顺序', `第 ${position + 1} 条，共 ${queue.length} 条按发布时间依次弹出`]);
  return html`<div class="ws-head">
      <div class="ws-title">
        <span class="eyebrow">编号 ${p.id}</span>
        <h3>${p.title}</h3>
        <div class="ws-tags">${stateTag(text, tone)}</div>
      </div>
      <div class="ws-actions">
        <button type="button" class="secondary" data-pa-act="copy">
          <svg class="icon"><use href="#i-copy" /></svg>复制为新公告
        </button>
        ${position >= 0 ? html`<button type="button" class="danger" data-pa-act="stop">停用</button>` : ''}
      </div>
    </div>
    <div class="ws-lock"><svg class="icon"><use href="#i-info" /></svg><span>${lock}</span></div>
    <div class="pa-grid">
      <dl class="pa-facts">
        ${facts.map(([k, v]) => html`<div><dt>${k}</dt><dd>${v}</dd></div>`)}
      </dl>
      ${popupPreviewHTML()}
    </div>`;
}

function renderPopupEditor() {
  const box = $('#popup-editor');
  const e = popupEditor;
  if (e.selected == null) {
    setHTML(
      box,
      html`<div class="ws-empty">
        <b>${popupBoard.items.length ? '选择一条公告' : '还没有首页弹窗公告'}</b>
        <span>从左侧选择查看，或点「新建」写一条。发布后，完成新手任务的玩家回到首页时会看到它。</span>
      </div>`,
    );
    return;
  }
  const item = e.selected ? popupItem(e.selected) : null;
  setHTML(box, item && item.status !== 'draft' ? popupReadonlyHTML(item) : popupFormHTML(item));
  syncPopupEditor();
}

/** Updates what follows the typed text — counters, preview, schedule, state — without rebuilding inputs. */
function syncPopupEditor() {
  const e = popupEditor;
  const box = $('#popup-editor');
  if (e.selected == null) return;
  const item = e.selected ? popupItem(e.selected) : null;
  const editable = !item || item.status === 'draft';
  const c = editable ? e.form : popupContent(item);
  const preview = (key, text, fallback) => {
    const el = box.querySelector(`[data-pv="${key}"]`);
    el.textContent = text.trim() || fallback;
    el.classList.toggle('empty', !text.trim());
  };
  preview('title', c.title, '（未填写标题）');
  preview('body', c.body, '（未填写正文）');
  box.querySelector('[data-pv="button"]').textContent = c.button_text.trim() || POPUP_DEFAULT_BUTTON;
  box.querySelectorAll('[data-pa-act]').forEach((b) => (b.disabled = popupBoard.busy));
  if (!editable) return;

  box.querySelector('[data-pa="title"]').textContent = c.title.trim() || '未命名弹窗';
  const listTitle = $('#popup-list .ws-item.selected b');
  if (listTitle) listTitle.textContent = c.title.trim() || '未命名弹窗';
  box.querySelector('[data-pa="dirty"]').hidden = !popupDirty();
  for (const field of ['title', 'body', 'button_text']) {
    const el = box.querySelector(`[data-count="${field}"]`);
    const n = popupLength(c[field]);
    el.textContent = `${n} / ${popupBoard.limits[field]}`;
    el.classList.toggle('over', n > popupBoard.limits[field]);
  }
  const now = popupNow();
  const when = c.end_unix ? `，${chinaText(c.end_unix)} 停止` : '，不限结束';
  const [text, bad] =
    c.start_unix && c.end_unix && c.end_unix <= c.start_unix
      ? ['结束时间须晚于开始时间', true]
      : c.end_unix && c.end_unix <= now
        ? ['结束时间已过，这样不能发布；请改晚或清空', true]
        : c.start_unix > now
          ? [`发布后等到 ${chinaText(c.start_unix)} 才开始弹出${when}`, false]
          : [`发布后立即开始弹出${when}`, false];
  const schedule = box.querySelector('[data-pa="schedule"]');
  schedule.textContent = text;
  schedule.classList.toggle('bad', bad);
  const publish = box.querySelector('[data-pa-act="publish"]');
  const full = popupQueue().length >= popupBoard.limits.live;
  publish.title = full ? `已有 ${popupBoard.limits.live} 条生效中或待生效的弹窗，请先停用一条` : '';
}

function renderPopupBoard() {
  if (!$('#pp-popups')) return;
  renderPopupCount();
  renderPopupList();
  renderPopupAlert();
  renderPopupEditor();
  renderPopupCleanup();
}

// 公告与奖励 › actions -----------------------------------------------------------------------------

function showPopupAlert(title, message, { kind = 'error', retry = false } = {}) {
  popupEditor.alert = { kind, title, message, retry };
  renderPopupAlert();
}

async function choosePopup(open) {
  if (popupBoard.busy || !(await confirmDiscard(popupDirty()))) return;
  open();
  popupEditor.alert = null;
  renderPopupList();
  renderPopupAlert();
  renderPopupEditor();
}

function newPopup() {
  return choosePopup(() => {
    popupEditor.selected = 0;
    popupEditor.saved = null;
    popupEditor.form = popupBlank();
    popupEditor.note = '';
  }).then(() => $('#popup-title')?.focus());
}

function copyPopup(p) {
  const now = popupNow();
  return choosePopup(() => {
    popupEditor.selected = 0;
    popupEditor.saved = null;
    // Past times would block publishing; only times still ahead are kept.
    popupEditor.form = {
      ...popupContent(p),
      start_unix: p.start_unix > now ? p.start_unix : 0,
      end_unix: p.end_unix > now ? p.end_unix : 0,
    };
    popupEditor.note = `复制自「${p.title}」（编号 ${p.id}）。保存或发布时另起编号，已看过原公告的玩家也会再收到这一条。`;
  });
}

/** Sends one change; on success the board reloads and `select` opens. Unsaved text survives a failure. */
async function popupRequest(body, { select, done }) {
  popupBoard.busy = true;
  syncPopupEditor();
  try {
    await api('/api/popup-announcements', {
      method: 'POST',
      body: JSON.stringify({ expected_revision: popupBoard.revision, ...body }),
    }).then((r) => {
      if (select === 'result') select = r.id;
    });
  } catch (e) {
    if (e.status === 409)
      showPopupAlert(
        '未完成：公告已在其他页面修改',
        '这里填写的内容仍保留，不会被覆盖。重新获取列表后再保存或发布。',
        { kind: 'warn', retry: true },
      );
    else showPopupAlert('未完成', errorText(e));
    return false;
  } finally {
    popupBoard.busy = false;
    syncPopupEditor();
  }
  popupEditor.alert = null;
  state.loaded.delete('audit');
  toast(done);
  try {
    await loadPopupBoard({ select });
  } catch (e) {
    showPopupAlert('已完成，但刷新列表失败', errorText(e), { retry: true });
  }
  return true;
}

async function submitPopup(action) {
  const e = popupEditor;
  if (popupBoard.busy || e.selected == null || !e.form) return;
  const box = $('#popup-editor');
  clearInvalid(box);
  const publish = action === 'publish';
  const problems = popupProblems(e.form, publish);
  if (problems.length) {
    problems.forEach(([field, message]) => markInvalid($(POPUP_FIELDS[field]), message));
    $(POPUP_FIELDS[problems[0][0]]).focus();
    showPopupAlert(publish ? '还不能发布' : '还不能保存', `${problems.map(([, m]) => m).join('；')}。`);
    return;
  }
  if (publish) {
    const l = popupBoard.limits;
    if (popupQueue().length >= l.live) {
      showPopupAlert('还不能发布', `已有 ${l.live} 条生效中或待生效的弹窗。请先停用不再需要的一条，再发布。`);
      return;
    }
    const f = e.form;
    const ok = await reviewChanges({
      title: '发布首页弹窗公告',
      lead: '发布后内容不能修改，请核对。',
      sections: [
        {
          title: '弹窗内容',
          summary: true,
          rows: [
            ['标题', f.title.trim()],
            ['正文', f.body.trim()],
            ['按钮文字', f.button_text.trim() || POPUP_DEFAULT_BUTTON],
            ['生效时间', `${popupScheduleText(f)}（北京时间）`],
          ],
        },
      ],
      note: '完成全部新手任务的玩家，下次登录或回到首页时弹出；每位玩家确认一次后不再弹出。',
      confirmText: '发布',
    });
    if (!ok) return;
  }
  await popupRequest(
    { action, id: e.selected, announcement: e.form },
    { select: 'result', done: publish ? '首页弹窗公告已发布' : '草稿已保存' },
  );
}

async function stopPopup(p) {
  const ok = await askConfirm(
    `停用「${p.title}」？`,
    '停用后不再发给还没收到的玩家；已收到但还没确认的玩家仍会看到这一条。停用后不能重新发布，需要时可复制为新公告。',
    { ok: '停用', danger: true },
  );
  if (ok) await popupRequest({ action: 'stop', id: p.id }, { select: p.id, done: '已停用' });
}

async function deletePopupDraft(p) {
  const ok = await askConfirm(`删除草稿「${p.title.trim() || '未命名弹窗'}」？`, '草稿删除后不能恢复。', {
    ok: '删除草稿',
    danger: true,
  });
  if (ok) await popupRequest({ action: 'delete', id: p.id }, { select: null, done: '草稿已删除' });
}

function onPopupInput(event) {
  // This section saves on its own; the page-wide handler only tracks the 整页保存 draft.
  event.stopPropagation();
  const el = event.target;
  if (el.id === 'popup-search') {
    popupEditor.query = el.value;
    renderPopupList();
    return;
  }
  const field = el.dataset.paField;
  if (!field || !popupEditor.form) return;
  if (field === 'start_unix' || field === 'end_unix') {
    try {
      popupEditor.form[field] = chinaUnix(el.value);
    } catch {
      return markInvalid(el, '请输入有效的日期时间');
    }
  } else popupEditor.form[field] = el.value;
  el.classList.remove('invalid');
  el.removeAttribute('aria-invalid');
  el.removeAttribute('title');
  syncPopupEditor();
}

function onPopupClick(event) {
  const pick = event.target.closest('[data-pa-id]');
  if (pick) {
    const id = Number(pick.dataset.paId);
    if (id !== popupEditor.selected) choosePopup(() => openPopupItem(id));
    return;
  }
  const act = event.target.closest('[data-pa-act]')?.dataset.paAct;
  const item = popupEditor.selected ? popupItem(popupEditor.selected) : null;
  if (act === 'save' || act === 'publish') submitPopup(act);
  else if (act === 'delete' && item) deletePopupDraft(item);
  else if (act === 'discard') choosePopup(() => openPopupItem(null));
  else if (act === 'copy' && item) copyPopup(item);
  else if (act === 'stop' && item) stopPopup(item);
  else if (act === 'reload') reloadPopupSection();
  else if (act === 'close-alert') {
    popupEditor.alert = null;
    renderPopupAlert();
  }
}

// 维护工具 › 清理首页弹窗公告 ------------------------------------------------------------------------

function renderPopupCleanup() {
  const tool = $('#maintenance-popup-tool');
  if (!tool) return;
  const now = popupNow();
  const order = { draft: 0, stopped: 1, ended: 2 };
  const cleanable = popupBoard.items
    .filter(popupCleanable)
    .sort((a, b) => order[popupPhase(a, now)] - order[popupPhase(b, now)] || b.id - a.id);
  const ids = new Set(cleanable.map((p) => p.id));
  popupCleanup.selected.forEach((id) => ids.has(id) || popupCleanup.selected.delete(id));
  const count = (phase) => cleanable.filter((p) => popupPhase(p, now) === phase).length;
  const shown = popupQueue().length;

  setMarkup(
    $('#popup-cleanup-now'),
    String(
      !popupBoard.loaded
        ? html`<span class="hint">正在读取…</span>`
        : html`<span
              >共 ${num(popupBoard.items.length)} 条，可清理 <b>${num(cleanable.length)}</b> 条：草稿
              ${count('draft')}、已停用 ${count('stopped')}、已过期 ${count('ended')}</span
            >${
              shown
                ? html`<span class="hint">另有 ${shown} 条生效中或待生效，须先在「公告与奖励」停用</span>`
                : ''
            }`,
    ),
  );
  const when = (p, phase) =>
    phase === 'draft'
      ? '未发布'
      : phase === 'ended'
        ? `${popupShort(p.end_unix)} 结束`
        : `${popupShort(p.published_at)} 发布`;
  setMarkup(
    $('#popup-cleanup-list'),
    String(
      html`${
        cleanable.length
          ? cleanable.map((p) => {
              const phase = popupPhase(p, now);
              const [text, tone] = POPUP_PHASES[phase];
              return html`<label class="hc-boss">
              <input type="checkbox" value="${p.id}" ${attr(popupCleanup.selected.has(p.id), 'checked')} />
              <span class="hc-boss-name" title="${p.title || '未命名弹窗'} · ${p.id}"
                ><b>${p.title.trim() || '未命名弹窗'}</b><code>${p.id}</code></span
              >
              <small>${when(p, phase)}</small>
              ${stateTag(text, tone)}
            </label>`;
            })
          : html`<p class="hint">${popupBoard.loaded ? '没有可清理的公告。' : '正在读取…'}</p>`
      }`,
    ),
  );
  const locked = $('#maintenance-preview').disabled;
  const picked = popupCleanup.selected.size;
  $('#popup-cleanup-picked').textContent = picked ? `已选 ${picked} 条` : '未选择';
  $('#popup-cleanup-all').disabled = !cleanable.length;
  $('#popup-cleanup-none').disabled = !picked;
  $('#popup-cleanup-apply').disabled = locked || popupBoard.busy || !picked;
  setMarkup($('#popup-cleanup-report'), String(popupCleanup.report));
}

async function applyPopupCleanup() {
  const now = popupNow();
  const picked = popupBoard.items.filter((p) => popupCleanup.selected.has(p.id));
  if (!picked.length || $('#popup-cleanup-apply').disabled) return;
  const ok = await reviewChanges({
    title: '清理首页弹窗公告',
    lead: `以下 ${picked.length} 条公告将被删除，请核对。`,
    sections: [
      {
        title: '将删除',
        summary: true,
        rows: picked.map((p) => {
          const phase = popupPhase(p, now);
          const time =
            phase === 'draft'
              ? '未发布'
              : `${chinaText(p.published_at)} 发布${phase === 'ended' ? `，${chinaText(p.end_unix)} 结束` : ''}`;
          return [`${p.title.trim() || '未命名弹窗'}（${p.id}）`, `${POPUP_PHASES[phase][0]} · ${time}`];
        }),
      },
    ],
    note: '内容不备份，删除后不能恢复。玩家的已读记录不变，这些编号也不会再分配给新公告。',
    confirmText: '删除所选公告',
  });
  if (!ok) return;
  popupBoard.busy = true;
  renderPopupCleanup();
  try {
    await api('/api/popup-announcements', {
      method: 'POST',
      body: JSON.stringify({
        expected_revision: popupBoard.revision,
        action: 'cleanup',
        ids: picked.map((p) => p.id),
      }),
    });
    popupCleanup.selected.clear();
    popupCleanup.report = html`<p><b>已删除 ${picked.length} 条首页弹窗公告</b></p>
      <p>${picked.map((p) => p.title.trim() || `未命名弹窗（${p.id}）`).join('、')}</p>
      <p class="hint">玩家的已读记录未改变；这些编号不会再分配。</p>`;
    state.loaded.delete('audit');
    toast('首页弹窗公告已清理');
  } catch (e) {
    // 409 is either the announcement revision or the maintenance state (a request or Admin write still running).
    const listChanged = e.status === 409 && e.message.includes('公告已被修改');
    popupCleanup.report = html`<p class="hc-error">
      清理未完成：${
        listChanged
          ? '公告列表已在其他页面修改，已重新读取，请核对选择后再执行。'
          : e.status === 409
            ? `${e.message}。`
            : errorText(e)
      }
    </p>`;
  } finally {
    popupBoard.busy = false;
  }
  try {
    await Promise.all([loadPopupBoard(), loadMaintenance()]);
  } catch (e) {
    reportError('maintenance', e, '载入');
  }
  renderPopupCleanup();
}

function initPopupCleanup() {
  const tool = $('#maintenance-popup-tool');
  tool.addEventListener('change', (event) => {
    if (!event.target.closest('#popup-cleanup-list')) return;
    const id = Number(event.target.value);
    event.target.checked ? popupCleanup.selected.add(id) : popupCleanup.selected.delete(id);
    renderPopupCleanup();
  });
  $('#popup-cleanup-all').addEventListener('click', () => {
    popupCleanup.selected = new Set(popupBoard.items.filter(popupCleanable).map((p) => p.id));
    renderPopupCleanup();
  });
  $('#popup-cleanup-none').addEventListener('click', () => {
    popupCleanup.selected.clear();
    renderPopupCleanup();
  });
  $('#popup-cleanup-apply').addEventListener('click', applyPopupCleanup);
  $('#popup-cleanup-refresh').addEventListener('click', () =>
    loadPopupBoard().catch((e) => reportError('maintenance', e, '载入')),
  );
}

function initPopupAnnouncements() {
  editorDirtyChecks['player-policy'] = popupDirty;
  const panel = $('#pp-popups');
  panel.addEventListener('input', onPopupInput);
  panel.addEventListener('change', onPopupInput);
  panel.addEventListener('click', onPopupClick);
  $('#popup-new').addEventListener('click', newPopup);
  initPopupCleanup();
}

initPopupAnnouncements();
