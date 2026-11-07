'use strict';
// 系统 › 运行日志: read-only search over the current run's structured log (GET /api/runtime-logs).
// The server scans newest → oldest, at most 32 MiB and 100 rows per call. A page can be empty while
// next_cursor still points further back; an empty cursor means the start of the file was reached.

const LOG_LEVELS = [
  ['', '全部'],
  ['ERROR', '错误'],
  ['WARN', '警告'],
  ['INFO', '信息'],
  ['DEBUG', '调试'],
];
const LOG_LEVEL_TEXT = { ERROR: '错误', WARN: '警告', INFO: '信息', DEBUG: '调试' };
const LOG_SLOW_MS = 1000;
const LOG_VERY_SLOW_MS = 3000;
const LOG_AUTO_PAGES = 8; // Empty pages followed per click before asking again: 8 × 32 MiB.
const LOG_MAX_ROWS = 5000;
const LOG_FOLLOW_MS = 5000;
const LOG_BINS = 48;
const LOG_PROBLEMS = {
  429: ['日志检索繁忙', '另一个日志查询仍在进行（可能来自其他浏览器窗口），稍后再试。'],
  503: [
    '运行日志暂不可读取',
    '日志文件可能正在轮换，或已被移走。稍后重试；一直如此时，检查本次运行目录中的日志文件。',
  ],
};
const LOG_PRESETS = [
  ['15m', '最近 15 分钟'],
  ['1h', '最近 1 小时'],
  ['today', '今天'],
  ['error', '只看错误'],
  ['500', '状态码 500'],
  ['slow', '慢请求 ≥ 1 秒'],
];

// Plain-language names for the messages the server writes; the original text stays next to them.
const LOG_MESSAGES = {
  'http request': '接口请求',
  'server ready': '服务已就绪',
  'server failed': '服务异常退出',
  'server stopping; draining requests and SQLite transactions': '服务正在停止，等待请求与数据库事务完成',
  'server stopped; HTTP shutdown completed': '服务已停止',
  'HTTP shutdown grace period ended; closing connections': '停止等待超时，强制关闭连接',
  'close SQLite connection pools': '关闭数据库连接失败',
  'initialized CN account in SQLite': '已初始化账号数据库',
  'validated CN runtime content gate': '运行内容校验通过',
  'business wire bytes': '通信流量统计',
  'Admin image sync disabled': '图片同步未启用',
  'skip failed request capture': '请求抓包写入失败，已跳过',
  'persist local state': '保存玩家存档失败',
  'persist solo battle outcome': '保存单人战斗结果失败',
  'reject native solo battle report': '拒绝单人战斗结算',
  'reject quest reading story settlement': '拒绝剧情结算',
  'read gift code policy': '读取兑换码配置失败',
  'commit gift redemption': '写入兑换码领取失败',
  'list local PVP opponents': '读取竞技场对手失败',
  'apply shared battle catalog to account': '同步战斗目录到账号失败',
  'BattleSv client request': '组队请求',
  'BattleSv client request rejected': '组队请求被拒绝',
  'BattleSv client frame failed': '组队连接数据异常',
  'BattleSv temporary accept failure; retrying': '组队连接接入暂时失败，正在重试',
  'local multiplayer room created': '创建组队房间',
  'local multiplayer room joined': '加入组队房间',
  'local multiplayer room member left': '离开组队房间',
  'local multiplayer room member ready': '组队成员已准备',
  'local multiplayer room dissolved': '组队房间已解散',
  'local multiplayer room countdown started': '组队开战倒计时开始',
  'local multiplayer room entered battle loading': '组队进入战斗载入',
  'local multiplayer Go battle start broadcast': '组队战斗开始',
  'local multiplayer Go battle completed': '组队战斗结束',
  'local multiplayer battle member became offline CPU': '组队成员掉线，改由 CPU 代打',
  'local multiplayer comeback credential accepted': '掉线成员重新连入',
  'local multiplayer comeback window expired; member remains CPU': '掉线成员未在时限内返回，继续由 CPU 代打',
  'multiplayer start rejected': '组队开战被拒绝',
  'multiplayer completion pending; retry scheduled': '组队结算未完成，已安排重试',
  'multiplayer completion retry failed': '组队结算重试失败',
  'multiplayer completion retry window expired': '组队结算重试已超时',
};

// Extra attributes: Chinese names for the common keys; the key itself stays visible in the detail.
const LOG_FIELD_LABELS = {
  room_id: '房间',
  user_id: '玩家',
  owner_user_id: '房主',
  boss_id: 'BOSS',
  story_id: '剧情',
  code_id: '兑换码',
  remote: '对端地址',
  member_type: '成员类型',
  turn: '回合',
  timed_out: '已超时',
  battle_end: '战斗结束',
  connected_members: '在线成员',
  remaining_connections: '剩余连接',
  comeback_allowed: '允许重连',
  auto_start: '自动开战',
  seed: '随机种子',
  channel: '通道',
  window_ms: '统计窗口（毫秒）',
  messages: '消息数',
  compressed_messages: '压缩消息数',
  raw_bytes: '原始字节',
  wire_bytes: '实际字节',
  write_ms: '写出耗时（毫秒）',
  revision: '版本',
  retry_after: '重试间隔',
  listen: '游戏监听',
  battle_listen: '组队监听',
  admin_listen: '后台监听',
  advertise_base_url: '对外地址',
  battle_advertise: '组队对外地址',
  client_profile: '客户端配置',
  request_log: '请求抓包',
  admin_image_sync: '图片同步',
  database_path: '数据库',
  config: '配置文件',
  problem: '问题',
  uncommitted_outcome: '未提交的结果',
};
// Shown first in the row; the rest follow in written order.
const LOG_KEY_FIELDS = 'room_id user_id owner_user_id boss_id story_id code_id remote channel'.split(' ');
const LOG_INLINE_FIELDS = 3;
const LOG_ACCOUNT_FIELDS = new Set(['user_id', 'owner_user_id']);

const runtimeLogs = {
  entries: [],
  cursor: '',
  done: false,
  available: null,
  busy: false,
  serial: 0,
  scanAbort: null,
  scanned: 0,
  query: null,
  problem: null, // { title, message } for 429, 503 and network failures
  stopped: false,
  notice: '',
  open: new Set(),
  uid: 0,
  fresh: new Set(),
  followTimer: 0,
  followedAt: 0,
};

// Time: the Admin reads every time in UTC+8, with seconds and milliseconds for logs ---------------

const logChina = (time) => new Date(Date.parse(time) + CHINA_OFFSET_SECONDS * 1000).toISOString();
const logClock = (time) => logChina(time).slice(11, 23);
const logStamp = (time) => logChina(time).slice(0, 23).replace('T', ' ');
const LOG_WEEKDAYS = '日一二三四五六';

function logDayText(time) {
  const iso = logChina(time);
  const day = new Date(iso.slice(0, 10) + 'T00:00:00Z');
  return `${Number(iso.slice(5, 7))} 月 ${Number(iso.slice(8, 10))} 日 周${LOG_WEEKDAYS[day.getUTCDay()]}`;
}

/** Unix seconds → <input type="datetime-local" step="1"> value in UTC+8. */
const logInputValue = (unix) => new Date((unix + CHINA_OFFSET_SECONDS) * 1000).toISOString().slice(0, 19);

/** datetime-local value read as UTC+8 → RFC3339 in UTC, or '' when empty. */
function logRFC3339(value) {
  if (!value) return '';
  const millis = Date.parse(`${value.length === 16 ? value + ':00' : value}+08:00`);
  if (!Number.isFinite(millis)) return null;
  return new Date(millis).toISOString().replace(/\.\d+Z$/, 'Z');
}

const logBytesText = (bytes) =>
  bytes >= 1048576 ? `${(bytes / 1048576).toFixed(1)} MiB` : `${Math.max(1, Math.round(bytes / 1024))} KiB`;
const logShortID = (id) => id.slice(id.lastIndexOf('/') + 1);
const logIsRequest = (row) => row.msg === 'http request' && !!row.path;
const LOG_SERVICE = /^server |^HTTP |wire bytes|content gate/;
const logTopic = (msg) =>
  msg === 'http request'
    ? ''
    : /multiplayer|BattleSv/.test(msg)
      ? '组队'
      : LOG_SERVICE.test(msg)
        ? '服务'
        : '';

// Query ---------------------------------------------------------------------------------------------

const logFields = () => ({
  q: $('#log-q'),
  path: $('#log-path'),
  status: $('#log-status'),
  elapsed: $('#log-elapsed'),
  request: $('#log-request'),
  user: $('#log-user'),
  from: $('#log-from'),
  to: $('#log-to'),
});
const logLevel = () => $('#log-level input:checked')?.value || '';

function showLogFieldError(message) {
  const el = $('#log-error');
  el.textContent = message;
  el.hidden = !message;
}

/** Reads the filter bar into query parameters; marks the offending field and throws on bad input. */
function readLogQuery() {
  const f = logFields();
  clearInvalid($('#log-form'));
  showLogFieldError('');
  const fail = (el, message) => {
    markInvalid(el, message);
    const error = new Error(message);
    error.field = true;
    throw error;
  };
  const query = {};
  const q = f.q.value.trim();
  if (new TextEncoder().encode(q).length > 256) fail(f.q, '搜索内容最多 256 字节（约 85 个汉字）');
  if (q) query.q = q;
  const level = logLevel();
  if (level) query.level = level;
  const path = f.path.value.trim();
  if (path) query.path = path;
  const status = f.status.value.trim();
  if (status) {
    if (!/^\d+$/.test(status) || Number(status) < 100 || Number(status) > 599)
      fail(f.status, '状态码须为 100–599 的整数');
    query.status = status;
  }
  const elapsed = f.elapsed.value.trim();
  if (elapsed) {
    if (!/^\d+$/.test(elapsed)) fail(f.elapsed, '耗时须为不小于 0 的整数毫秒');
    query.min_elapsed_ms = String(Number(elapsed));
  }
  const request = f.request.value.trim();
  if (request) query.request_id = request;
  const user = f.user.value.trim();
  if (user) {
    // The server parses an int64; 18 digits always fit.
    if (!/^[1-9]\d{0,17}$/.test(user)) fail(f.user, '玩家 UID 须为正整数');
    query.user_id = user;
  }
  const from = logRFC3339(f.from.value);
  const to = logRFC3339(f.to.value);
  if (from === null) fail(f.from, '起始时间无效');
  if (to === null) fail(f.to, '结束时间无效');
  if (from && to && Date.parse(from) >= Date.parse(to)) fail(f.to, '结束时间须晚于起始时间');
  if (from) query.from = from;
  if (to) query.to = to;
  return query;
}

const logFiltered = (query) => Object.keys(query).length > 0;
const logScanEnd = (query) => (query?.from || query?.to ? '已查完所选时段' : '已到日志开头');

function logScopeText(query) {
  if (!query) return '';
  const parts = [];
  if (query.level) parts.push(`级别「${LOG_LEVEL_TEXT[query.level]}」`);
  if (query.q) parts.push(`包含「${query.q}」`);
  if (query.path) parts.push(`接口 ${query.path}`);
  if (query.status) parts.push(`状态码 ${query.status}`);
  if (query.min_elapsed_ms) parts.push(`耗时 ≥ ${num(query.min_elapsed_ms)} 毫秒`);
  if (query.request_id) parts.push(`请求 ${logShortID(query.request_id)}`);
  if (query.user_id) parts.push(`玩家 ${query.user_id}`);
  if (query.from || query.to) {
    const at = (value) => logStamp(value).slice(0, 19);
    const range = query.from && query.to ? `${at(query.from)} 至 ${at(query.to)}` : '';
    parts.push(range || (query.from ? `${at(query.from)} 之后` : `${at(query.to)} 之前`));
  }
  return parts.length ? `筛选：${parts.join('，')}` : '全部记录，由新到旧';
}

// Fetching ------------------------------------------------------------------------------------------

const logWait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/** One page; another running search answers 429 with Retry-After: 1, so wait and ask again a few times. */
async function fetchLogPage(query, cursor, retries = 3, signal) {
  const params = new URLSearchParams(cursor ? { ...query, cursor } : query);
  for (let attempt = 0; ; attempt++) {
    signal?.throwIfAborted();
    try {
      return await api('/api/runtime-logs?' + params, { signal });
    } catch (e) {
      if (signal?.aborted || e.status !== 429 || attempt >= retries) throw e;
      await logWait(1000);
    }
  }
}

function logAdopt(rows) {
  return rows.map((row) => ({ ...row, uid: ++runtimeLogs.uid, key: JSON.stringify(row) }));
}

/** Starts again from the newest record with the filters as they are now. */
async function runLogQuery({ notice = '' } = {}) {
  let query;
  try {
    query = readLogQuery();
  } catch (e) {
    if (!e.field) throw e;
    showLogFieldError(e.message);
    return;
  }
  runtimeLogs.scanAbort?.abort();
  const serial = ++runtimeLogs.serial;
  Object.assign(runtimeLogs, {
    entries: [],
    cursor: '',
    done: false,
    scanned: 0,
    query,
    problem: null,
    notice,
    open: new Set(),
    fresh: new Set(),
    stopped: false,
  });
  await scanLogs(serial, true);
}

/** Follows the cursor back until at least one row arrives, the file starts, or the page budget runs out. */
async function scanLogs(serial, first = false) {
  const controller = new AbortController();
  runtimeLogs.scanAbort = controller;
  runtimeLogs.busy = true;
  renderRuntimeLogs();
  let pages = 0;
  let found = false;
  try {
    while (pages < LOG_AUTO_PAGES) {
      const page = await fetchLogPage(
        runtimeLogs.query,
        first && !pages ? '' : runtimeLogs.cursor,
        3,
        controller.signal,
      );
      if (serial !== runtimeLogs.serial) return;
      pages++;
      runtimeLogs.available = page.available;
      if (!page.available) break;
      const rows = page.entries || [];
      runtimeLogs.entries.push(...logAdopt(rows.slice(0, LOG_MAX_ROWS - runtimeLogs.entries.length)));
      runtimeLogs.scanned += page.scanned_bytes || 0;
      runtimeLogs.cursor = page.next_cursor || '';
      runtimeLogs.done = !runtimeLogs.cursor;
      found = found || rows.length > 0;
      if (found || runtimeLogs.done || runtimeLogs.entries.length >= LOG_MAX_ROWS) break;
      renderLogMore();
    }
  } catch (e) {
    if (serial !== runtimeLogs.serial) return;
    runtimeLogs.busy = false;
    if (e.status === 409 && !first) {
      // The file was replaced or truncated: old cursors are void, so restart from the newest record.
      return runLogQuery({ notice: '日志文件已被替换或截断，已从最新记录重新查询。' });
    }
    if (e.status === 400) {
      showLogFieldError(e.message);
    } else {
      const [title, message] = LOG_PROBLEMS[e.status] || [
        e.status === 0 ? '无法连接后台服务' : '读取运行日志失败',
        e.message,
      ];
      runtimeLogs.problem = { title, message };
    }
  } finally {
    if (runtimeLogs.scanAbort === controller) runtimeLogs.scanAbort = null;
    if (serial === runtimeLogs.serial) {
      runtimeLogs.busy = false;
      renderRuntimeLogs();
    }
  }
}

function loadOlderLogs() {
  if (runtimeLogs.busy || runtimeLogs.done || runtimeLogs.entries.length >= LOG_MAX_ROWS) return;
  runtimeLogs.notice = '';
  runtimeLogs.stopped = false;
  scanLogs(runtimeLogs.serial).catch((e) => reportError('runtime-logs', e, '载入'));
}

function stopLogScan() {
  runtimeLogs.serial++;
  runtimeLogs.scanAbort?.abort();
  runtimeLogs.busy = false;
  runtimeLogs.stopped = true;
  renderRuntimeLogs();
}

const loadRuntimeLogs = () => runLogQuery();

// Live follow: re-reads the newest page and prepends rows above the newest one already shown ---------

async function followLogs() {
  if (state.currentView !== 'runtime-logs' || document.hidden || runtimeLogs.busy) return;
  if (!runtimeLogs.available || runtimeLogs.problem || !runtimeLogs.query) return;
  const serial = runtimeLogs.serial;
  const timer = runtimeLogs.followTimer;
  let page;
  try {
    page = await fetchLogPage(runtimeLogs.query, '', 0);
  } catch {
    return; // A busy or briefly unreadable log just waits for the next tick.
  }
  if (
    serial !== runtimeLogs.serial ||
    timer !== runtimeLogs.followTimer ||
    state.currentView !== 'runtime-logs' ||
    document.hidden ||
    runtimeLogs.busy
  )
    return;
  runtimeLogs.followedAt = Date.now();
  const rows = page.entries || [];
  const top = runtimeLogs.entries[0]?.key;
  const at = top ? rows.findIndex((row) => JSON.stringify(row) === top) : -1;
  if (at === 0 || (!rows.length && !runtimeLogs.entries.length)) return renderLogFollow();
  if (at > 0) {
    const fresh = logAdopt(rows.slice(0, at));
    runtimeLogs.entries.unshift(...fresh);
    while (runtimeLogs.entries.length > LOG_MAX_ROWS) {
      runtimeLogs.open.delete(runtimeLogs.entries.pop().uid);
    }
    runtimeLogs.fresh = new Set(fresh.map((row) => row.uid));
  } else {
    // More new rows than one page, or the file changed: show the newest page on its own.
    const fresh = logAdopt(rows);
    Object.assign(runtimeLogs, {
      entries: fresh,
      cursor: page.next_cursor || '',
      done: !page.next_cursor,
      scanned: page.scanned_bytes || 0,
      open: new Set(),
      fresh: new Set(fresh.map((row) => row.uid)),
      notice: runtimeLogs.entries.length ? '新记录较多，已切换到最新一页。' : '',
    });
  }
  renderRuntimeLogs();
}

function setLogFollow(on) {
  clearInterval(runtimeLogs.followTimer);
  runtimeLogs.followTimer = on ? setInterval(followLogs, LOG_FOLLOW_MS) : 0;
  runtimeLogs.followedAt = on ? Date.now() : 0;
  if (on) followLogs();
  renderLogFollow();
}

function renderLogFollow() {
  const on = !!runtimeLogs.followTimer;
  const label = $('#log-follow-state');
  label.classList.toggle('on', on);
  const seen = runtimeLogs.followedAt && logInputValue(Math.floor(runtimeLogs.followedAt / 1000)).slice(11);
  label.textContent = on ? `每 5 秒刷新${seen ? ` · ${seen}` : ''}` : '';
}

// Rendering -----------------------------------------------------------------------------------------

function renderRuntimeLogs() {
  const unset = runtimeLogs.available === false;
  $('#log-scope').textContent = unset ? '' : logScopeText(runtimeLogs.query);
  $('#log-follow').disabled = unset;
  $('#log-export').disabled = !runtimeLogs.entries.length;
  $('#log-run').disabled = runtimeLogs.busy;
  $('#runtime-logs').classList.toggle('log-busy', runtimeLogs.busy);
  renderLogOverview();
  renderLogStream();
  renderLogMore();
  renderLogFollow();
}

function renderLogOverview() {
  const box = $('#log-overview');
  const rows = runtimeLogs.entries;
  box.hidden = !rows.length;
  if (!rows.length) return box.replaceChildren();
  const count = (test) => rows.reduce((n, row) => n + (test(row) ? 1 : 0), 0);
  const requests = rows.filter(logIsRequest);
  const slowest = requests.reduce((max, row) => Math.max(max, row.elapsed_ms ?? 0), 0);
  const stat = (label, value, { tone = '', action = '', title = '' } = {}) =>
    action
      ? html`<button type="button" class="log-stat ${tone}" data-log-preset="${action}" title="${title}">
          <b>${value}</b><span>${label}</span>
        </button>`
      : html`<div class="log-stat ${tone}" title="${title}"><b>${value}</b><span>${label}</span></div>`;
  const errors = count((row) => row.level === 'ERROR');
  const warns = count((row) => row.level === 'WARN');
  const clientErrors = count((row) => row.status >= 400 && row.status < 500);
  const serverErrors = count((row) => row.status >= 500);
  const slow = count((row) => (row.elapsed_ms ?? 0) >= LOG_SLOW_MS);
  setHTML(
    box,
    html`<div class="log-stats">
        ${stat('已载入', num(rows.length), { title: `已向前扫描 ${logBytesText(runtimeLogs.scanned)}` })}
        ${stat('错误', num(errors), { tone: errors ? 'bad' : '', action: 'error', title: '只看错误级别' })}
        ${stat('警告', num(warns), { tone: warns ? 'warn' : '', action: 'warn', title: '只看警告级别' })}
        ${stat('接口请求', num(requests.length), { title: '消息为 http request 的记录' })}
        ${stat('4xx / 5xx', `${num(clientErrors)} / ${num(serverErrors)}`, {
          tone: serverErrors ? 'bad' : clientErrors ? 'warn' : '',
        })}
        ${stat('慢请求 ≥ 1 秒', num(slow), { tone: slow ? 'warn' : '', action: 'slow', title: '只看业务耗时 ≥ 1 秒' })}
        ${stat('最长耗时', requests.length ? `${slowest ? num(slowest) : '<1'} ms` : '—', {
          tone: slowest >= LOG_VERY_SLOW_MS ? 'bad' : slowest >= LOG_SLOW_MS ? 'warn' : '',
        })}
      </div>
      ${logHistogram(rows)}`,
  );
}

/** Stacked bars of the loaded rows over their own time span; a click narrows the time filter to the bar. */
function logHistogram(rows) {
  const times = rows.map((row) => Date.parse(row.time));
  const newest = Math.max(...times);
  const oldest = Math.min(...times);
  const span = Math.max(newest - oldest, 1000);
  const width = span / LOG_BINS;
  const bins = Array.from({ length: LOG_BINS }, () => ({ error: 0, warn: 0, other: 0 }));
  rows.forEach((row, i) => {
    const bin = bins[Math.min(LOG_BINS - 1, Math.floor((times[i] - oldest) / width))];
    bin[row.level === 'ERROR' ? 'error' : row.level === 'WARN' ? 'warn' : 'other']++;
  });
  const peak = Math.max(...bins.map((bin) => bin.error + bin.warn + bin.other));
  const at = (millis) => logStamp(new Date(millis).toISOString()).slice(5, 19);
  const bar = (bin, i) => {
    const total = bin.error + bin.warn + bin.other;
    const start = oldest + i * width;
    const title = `${at(start)} – ${at(start + width)}：${num(total)} 条${bin.error ? `，错误 ${bin.error}` : ''}${
      bin.warn ? `，警告 ${bin.warn}` : ''
    }`;
    const part = (n, tone) => (n ? html`<i class="${tone}" style="flex-grow:${n}"></i>` : '');
    return html`<button
      type="button"
      class="log-bar"
      data-log-zoom="${Math.floor(start)}:${Math.ceil(start + width)}"
      title="${title}"
      aria-label="${title}"
      ${attr(!total, 'disabled')}
    >
      <span style="height:${total ? Math.max(8, Math.round((total / peak) * 100)) : 0}%">
        ${part(bin.error, 'error')}${part(bin.warn, 'warn')}${part(bin.other, 'other')}
      </span>
    </button>`;
  };
  return html`<div class="log-chart">
    <div class="log-chart-head">
      <span>已载入记录的时间分布</span>
      <span class="log-legend"><i class="error"></i>错误<i class="warn"></i>警告<i class="other"></i>其他</span>
    </div>
    <div class="log-bars">${bins.map(bar)}</div>
    <div class="log-axis"><span>${at(oldest)}</span><span>点击柱条可缩小到该时段</span><span>${at(newest)}</span></div>
  </div>`;
}

function renderLogStream() {
  const box = $('#log-stream');
  const { entries, available, problem, busy } = runtimeLogs;
  const notice = runtimeLogs.notice
    ? html`<p class="log-notice"><svg class="icon"><use href="#i-info" /></svg>${runtimeLogs.notice}</p>`
    : '';
  if (available === false) return setHTML(box, logUnsetHTML());
  if (problem && !entries.length) return setHTML(box, html`${notice}${logProblemHTML(problem)}`);
  if (!entries.length) {
    if (busy || available == null) {
      return setHTML(box, html`${notice}<div class="log-empty">${logScanHTML()}</div>`);
    }
    if (runtimeLogs.stopped && !runtimeLogs.cursor && !runtimeLogs.done) {
      return setHTML(
        box,
        html`<div class="log-empty">
          <b>查询已停止</b>
          <button type="button" class="secondary" data-log-retry>重新查询</button>
        </div>`,
      );
    }
    const scanned = logBytesText(runtimeLogs.scanned);
    return setHTML(
      box,
      html`${notice}
        <div class="log-empty">
          <svg class="icon"><use href="#i-search" /></svg>
          <b>${logFiltered(runtimeLogs.query) ? '没有符合条件的记录' : '日志里还没有记录'}</b>
          <p class="hint">
            ${
              runtimeLogs.done
                ? `${logScanEnd(runtimeLogs.query)}，共扫描 ${scanned}。`
                : `最近的 ${scanned} 里没有找到，更早的部分还没有检查。`
            }
          </p>
          ${runtimeLogs.cursor ? logOlderButton('继续向前查找') : ''}
        </div>`,
    );
  }
  let day = '';
  const parts = [];
  for (const row of entries) {
    const rowDay = logChina(row.time).slice(0, 10);
    if (rowDay !== day) {
      day = rowDay;
      parts.push(html`<div class="log-day"><b>${logDayText(row.time)}</b><span>${rowDay}</span></div>`);
    }
    parts.push(logRowHTML(row));
  }
  setHTML(box, html`${notice}${problem ? logProblemHTML(problem) : ''}<div class="log-list">${parts}</div>`);
  runtimeLogs.fresh.clear();
}

function logRowHTML(row) {
  const level = row.level || 'INFO';
  const gloss = LOG_MESSAGES[row.msg];
  const topic = logTopic(row.msg);
  const elapsed = row.elapsed_ms;
  const speed = elapsed >= LOG_VERY_SLOW_MS ? 'very-slow' : elapsed >= LOG_SLOW_MS ? 'slow' : '';
  const statusTone = row.status >= 500 ? 's5' : row.status >= 400 ? 's4' : row.status >= 300 ? 's3' : 's2';
  const body = logIsRequest(row)
    ? html`<span class="log-method">${row.method || '—'}</span><code class="log-path">${row.path}</code>${
        row.status ? html`<span class="log-code ${statusTone}">${row.status}</span>` : ''
      }${logInlineFields(row.fields)}`
    : html`${topic ? html`<span class="log-topic">${topic}</span>` : ''}
        <span class="log-msg">${gloss || row.msg}</span>
        ${gloss ? html`<span class="log-raw">${row.msg}</span>` : ''}
        ${logInlineFields(row.fields)}`;
  return html`<details
    class="log-row lv-${level.toLowerCase()} ${attr(runtimeLogs.fresh.has(row.uid), 'fresh')}"
    data-log-uid="${row.uid}"
    ${attr(runtimeLogs.open.has(row.uid), 'open')}
  >
    <summary>
      <time datetime="${row.time}" title="${logStamp(row.time)}（UTC+8）">${logClock(row.time)}</time>
      <span class="log-level">${LOG_LEVEL_TEXT[level] || level}</span>
      <span class="log-body">${body}</span>
      <span class="log-side">
        ${
          elapsed != null
            ? html`<span class="log-ms ${speed}">${elapsed ? num(elapsed) : '<1'} ms</span>`
            : ''
        }
        ${
          row.request_id
            ? html`<code class="log-rid" title="${row.request_id}">${logShortID(row.request_id)}</code>`
            : ''
        }
      </span>
      ${row.error ? html`<span class="log-error">${row.error}</span>` : ''}
    </summary>
    ${runtimeLogs.open.has(row.uid) ? logDetailHTML(row) : ''}
  </details>`;
}

/** Detail facts; each filterable value offers a one-click filter, and the row can be shown in context. */
function logDetailHTML(row) {
  const filter = (field, value, label) =>
    html`<button type="button" class="ghost" data-log-filter="${field}" data-value="${value}">
      ${label}
    </button>`;
  const fact = (label, value, extra = '') => html`<dt>${label}</dt><dd>${value}${extra}</dd>`;
  return html`<div class="log-detail">
    <dl>
      ${fact('时间', html`${logStamp(row.time)}（UTC+8）<code>${row.time}</code>`)}
      ${fact('级别', `${LOG_LEVEL_TEXT[row.level] || row.level}（${row.level}）`)}
      ${fact('消息原文', html`<code>${row.msg}</code>`)}
      ${row.method ? fact('方法', html`<code>${row.method}</code>`) : ''}
      ${row.path ? fact('接口路径', html`<code>${row.path}</code>`, filter('path', row.path, '只看此接口')) : ''}
      ${
        row.status
          ? fact('状态码', html`<code>${row.status}</code>`, filter('status', row.status, '只看此状态码'))
          : ''
      }
      ${
        row.elapsed_ms != null
          ? fact(
              '业务耗时',
              row.elapsed_ms ? `${num(row.elapsed_ms)} 毫秒` : '不足 1 毫秒',
              html`<small>日志记录的处理耗时，与运行概览的请求总耗时口径不同</small>`,
            )
          : ''
      }
      ${
        row.request_id
          ? fact(
              '请求 ID',
              html`<code>${row.request_id}</code>`,
              html`${filter('request', row.request_id, '只看此请求')}<button
                type="button"
                class="ghost"
                data-log-copy="${row.request_id}"
              >
                复制
              </button>`,
            )
          : ''
      }
      ${row.error ? fact('错误', html`<pre class="log-error-text">${row.error}</pre>`) : ''}
      ${row.fields?.length ? html`<dt class="log-fields-head">附加字段</dt><dd></dd>` : ''}
      ${(row.fields || []).map((field) =>
        fact(
          html`${LOG_FIELD_LABELS[field.key] || field.key}${
            LOG_FIELD_LABELS[field.key] ? html`<small>${field.key}</small>` : ''
          }`,
          field.value.length > 120
            ? html`<pre class="log-field-long">${field.value}</pre>`
            : html`<code>${field.value}</code>`,
          logFieldActions(field),
        ),
      )}
    </dl>
    <div class="log-detail-actions">
      <button type="button" class="secondary sm" data-log-around="${row.uid}">查看前后 1 分钟</button>
      <button type="button" class="secondary sm" data-log-copy-row="${row.uid}">复制整行 JSON</button>
    </div>
  </div>`;
}

/** The row's key attributes, e.g. 房间 6020000205 · 对端地址 …; the count of the others follows. */
function logInlineFields(fields = []) {
  if (!fields.length) return '';
  const rank = (field) => {
    const at = LOG_KEY_FIELDS.indexOf(field.key);
    return at < 0 ? LOG_KEY_FIELDS.length : at;
  };
  const shown = fields
    .filter((field) => field.value.length <= 40)
    .sort((a, b) => rank(a) - rank(b))
    .slice(0, LOG_INLINE_FIELDS);
  if (!shown.length)
    return html`<span class="log-fields-more" title="点开查看附加字段">+${fields.length}</span>`;
  const more = fields.length - shown.length;
  return html`<span class="log-fields">
    ${shown.map(
      (field) =>
        html`<span title="${field.key}: ${field.value}">
          <i>${LOG_FIELD_LABELS[field.key] || field.key}</i>${field.value}
        </span>`,
    )}
    ${more ? html`<span class="log-fields-more" title="点开查看全部附加字段">+${more}</span>` : ''}
  </span>`;
}

function logFieldActions(field) {
  // Very short values such as 0 or 10 would match almost every line.
  const searchable = field.value.length >= 4 && new TextEncoder().encode(field.value).length <= 256;
  const account = LOG_ACCOUNT_FIELDS.has(field.key) && /^[1-9]\d{0,17}$/.test(field.value);
  return html`${
    field.key === 'user_id' && account
      ? html`<button type="button" class="ghost" data-log-filter="user" data-value="${field.value}">
          只看此玩家
        </button>`
      : ''
  }${
    searchable
      ? html`<button type="button" class="ghost" data-log-search="${field.value}">搜索此值</button>`
      : ''
  }${
    account
      ? html`<button type="button" class="ghost" data-log-account="${field.value}">查看账号</button>`
      : ''
  }`;
}

function logUnsetHTML() {
  return html`<div class="log-empty log-unset">
    <svg class="icon"><use href="#i-terminal" /></svg>
    <b>本次运行没有接入运行日志</b>
    <p class="hint">
      由启动器（Windows 启动器、<code>Start-Server.ps1</code> 或 Linux 便携启动器）启动时，会自动接入本次运行的
      <code>stdout.log</code>。直接运行 <code>kairi-server</code> 时，加上
      <code>-admin-log-file &lt;日志路径&gt;</code> 指定当前运行的结构化日志。
    </p>
    <p class="hint">操作审计和请求抓包不受影响，仍在各自页面查看。</p>
  </div>`;
}

function logProblemHTML(problem) {
  return html`<div class="log-problem" role="alert">
    <svg class="icon"><use href="#i-alert" /></svg>
    <div><b>${problem.title}</b><span>${problem.message}</span></div>
    <button type="button" class="secondary sm" data-log-retry>重试</button>
  </div>`;
}

function logScanHTML() {
  const scanned = runtimeLogs.scanned;
  return html`<div class="log-scan">
      <span class="log-spinner"></span>${scanned ? `正在向前查找… 已扫描 ${logBytesText(scanned)}` : '正在读取运行日志…'}
    </div>
    <button type="button" class="ghost" id="log-stop">停止</button>`;
}

const logOlderButton = (label) =>
  html`<button type="button" class="secondary" id="log-older">
    <svg class="icon"><use href="#i-down" /></svg>${label}
  </button>`;

/** Footer under loaded rows; with nothing loaded, the empty box carries progress and the next step. */
function renderLogMore() {
  const box = $('#log-more');
  const { entries, busy, done, cursor, available, scanned } = runtimeLogs;
  if (available === false || !entries.length) return box.replaceChildren();
  if (busy) return setHTML(box, logScanHTML());
  if (entries.length >= LOG_MAX_ROWS)
    return setHTML(
      box,
      html`<p class="hint">已载入 ${num(entries.length)} 条，请缩小时间或筛选范围后再向前查看。</p>`,
    );
  if (done)
    return setHTML(
      box,
      html`<p class="hint log-start">${logScanEnd(runtimeLogs.query)} · 共扫描 ${logBytesText(scanned)}</p>`,
    );
  if (!cursor) return box.replaceChildren();
  setHTML(
    box,
    html`${logOlderButton('加载更早的记录')}
      <span class="hint">已扫描 ${logBytesText(scanned)}；每次最多再向前查 ${LOG_AUTO_PAGES * 32} MiB</span>`,
  );
}

// Filters from the page: presets, chart bars, detail buttons -------------------------------------------

function applyLogPreset(name) {
  const f = logFields();
  const now = Math.floor(Date.now() / 1000);
  const level = (value) => {
    const input = $(`#log-level input[value="${value}"]`);
    if (input) input.checked = true;
  };
  if (name === '15m' || name === '1h') {
    f.from.value = logInputValue(now - (name === '15m' ? 900 : 3600));
    f.to.value = '';
  } else if (name === 'today') {
    f.from.value = logInputValue(now).slice(0, 10) + 'T00:00:00';
    f.to.value = '';
  } else if (name === 'error' || name === 'warn') level(name.toUpperCase());
  else if (name === '500') f.status.value = '500';
  else if (name === 'slow') f.elapsed.value = String(LOG_SLOW_MS);
  runLogQuery();
}

function resetLogFilters() {
  const f = logFields();
  Object.values(f).forEach((el) => (el.value = ''));
  $('#log-level input[value=""]').checked = true;
  runLogQuery();
}

function logRowByUID(uid) {
  return runtimeLogs.entries.find((row) => row.uid === Number(uid));
}

async function copyLogText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast('已复制');
  } catch {
    toast('浏览器不允许写入剪贴板，请手动选中复制', true);
  }
}

function initRuntimeLogs() {
  const view = $('#runtime-logs');
  if (!view) return;
  setHTML($('#log-level'), segmented('log-level', '', LOG_LEVELS));
  setHTML(
    $('#log-presets'),
    LOG_PRESETS.map(([key, label]) => html`<button type="button" data-log-preset="${key}">${label}</button>`),
  );
  $('#log-form').addEventListener('submit', (event) => {
    event.preventDefault();
    runLogQuery();
  });
  $('#log-level').addEventListener('change', () => runLogQuery());
  $('#log-reset').onclick = resetLogFilters;
  $('#log-follow').onchange = (event) => setLogFollow(event.target.checked);
  $('#log-export').onclick = () =>
    downloadJSON(
      '运行日志.json',
      runtimeLogs.entries.map(({ uid, key, ...row }) => row),
    );
  view.addEventListener(
    'toggle',
    (event) => {
      const row = event.target.closest?.('.log-row');
      if (!row) return;
      const uid = Number(row.dataset.logUid);
      if (row.open === runtimeLogs.open.has(uid)) return;
      if (row.open) {
        runtimeLogs.open.add(uid);
        row.insertAdjacentHTML('beforeend', htmlValue(logDetailHTML(logRowByUID(uid))));
      } else {
        runtimeLogs.open.delete(uid);
        row.querySelector('.log-detail')?.remove();
      }
    },
    true,
  );
  view.addEventListener('click', (event) => {
    const target = event.target.closest('button');
    if (!target) return;
    const f = logFields();
    if (target.dataset.logPreset) applyLogPreset(target.dataset.logPreset);
    else if (target.id === 'log-older') loadOlderLogs();
    else if (target.id === 'log-stop') stopLogScan();
    else if (target.hasAttribute('data-log-retry')) runLogQuery();
    else if (target.dataset.logZoom) {
      const [start, end] = target.dataset.logZoom.split(':').map(Number);
      f.from.value = logInputValue(Math.floor(start / 1000));
      f.to.value = logInputValue(Math.ceil(end / 1000) + (Math.ceil(end / 1000) <= Math.floor(start / 1000)));
      runLogQuery();
    } else if (target.dataset.logSearch) {
      f.q.value = target.dataset.logSearch;
      runLogQuery();
    } else if (target.dataset.logAccount) {
      openAccountDetail(Number(target.dataset.logAccount));
    } else if (target.dataset.logFilter) {
      f[target.dataset.logFilter].value = target.dataset.value;
      runLogQuery();
    } else if (target.dataset.logAround) {
      const row = logRowByUID(target.dataset.logAround);
      const at = Math.floor(Date.parse(row.time) / 1000);
      Object.values(f).forEach((el) => (el.value = ''));
      $('#log-level input[value=""]').checked = true;
      f.from.value = logInputValue(at - 60);
      f.to.value = logInputValue(at + 61);
      runLogQuery();
    } else if (target.dataset.logCopy) copyLogText(target.dataset.logCopy);
    else if (target.dataset.logCopyRow) {
      const { uid, key, ...row } = logRowByUID(target.dataset.logCopyRow);
      copyLogText(JSON.stringify(row));
    }
  });
}

initRuntimeLogs();
