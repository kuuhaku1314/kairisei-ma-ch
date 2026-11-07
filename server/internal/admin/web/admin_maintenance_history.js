'use strict';
// 维护工具 › 清理历史数据: finished mail batches, clear decks with the yesterday ranking, and Boss
// score leaderboards. Preview and apply run through the shared maintenance cleanup job.

const historyCleanup = {
  overview: null,
  bosses: [],
  selected: new Set(),
  previewed: '',
  appliedJob: '',
  loading: false,
};

const HISTORY_MAIL_DEFAULT_DAYS = 30;

function bytesText(n) {
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(2)} GiB`;
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MiB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${num(n)} B`;
}

/** Clear-statistics days count UTC+8 calendar days. */
const clearDayText = (day) => chinaInputValue(day * 86400 - CHINA_OFFSET_SECONDS).slice(0, 10);

function historyRequest() {
  const request = {};
  if ($('#history-mail').checked && $('#history-mail-before').value)
    request.mail_before_utc = new Date($('#history-mail-before').value).toISOString();
  if ($('#history-clear').checked) request.clear_keep_days = Number($('#history-clear-days').value);
  if ($('#history-rank').checked && historyCleanup.selected.size)
    request.rank_boss_ids = [...historyCleanup.selected].sort((a, b) => a - b);
  if ($('#history-vacuum').checked) request.vacuum = true;
  return request;
}

function historyRequestProblem(request) {
  if ($('#history-mail').checked && !request.mail_before_utc) return '请选择礼物批次的截止时间';
  if ($('#history-mail').checked && new Date(request.mail_before_utc) > new Date())
    return '礼物批次的截止时间不能晚于现在';
  const days = request.clear_keep_days;
  if ($('#history-clear').checked && !(Number.isInteger(days) && days >= 2 && days <= 29))
    return '通关卡组须保留 2–29 天';
  if ($('#history-rank').checked && !request.rank_boss_ids) return '请选择要重置排行榜的 Boss';
  if (!Object.keys(request).length) return '请至少选择一项清理内容';
  return '';
}

async function loadHistoryOverview() {
  if (historyCleanup.loading) return;
  historyCleanup.loading = true;
  try {
    const data = await api('/api/maintenance/history');
    historyCleanup.overview = data.overview;
    historyCleanup.bosses = data.rank_bosses || [];
  } finally {
    historyCleanup.loading = false;
  }
  const known = new Set(historyCleanup.bosses.map((b) => b.boss_id));
  historyCleanup.selected.forEach((id) => known.has(id) || historyCleanup.selected.delete(id));
  renderHistoryOverview();
  renderHistoryCleanup();
}

function renderHistoryOverview() {
  const o = historyCleanup.overview;
  if (!o) return;
  setHTML(
    $('#history-size'),
    html`<div><span>数据库文件</span><b>${bytesText(o.file_bytes)}</b></div>
      <div><span>可复用的空闲空间</span><b>${bytesText(o.free_bytes)}</b></div>
      <p class="hint">删除记录后空间先留在文件内供新数据复用；勾选「整理数据库文件」才会让文件变小。</p>`,
  );
  $('#history-mail-now').textContent = o.mail_batches
    ? `共 ${num(o.mail_batches)} 个批次，其中 ${num(o.mail_finished)} 个已全部完成；回执 ${num(o.mail_receipts)} 条，约 ${bytesText(o.mail_bytes)}` +
      (o.mail_oldest_finished_utc
        ? `；最早完成的创建于 ${new Date(o.mail_oldest_finished_utc).toLocaleDateString()}`
        : '')
    : '暂无发放批次';
  $('#history-clear-now').textContent = o.clear_rows
    ? `共 ${num(o.clear_rows)} 条，约 ${bytesText(o.clear_bytes)}；最早 ${clearDayText(o.clear_oldest_day)}（${num(o.clear_today - o.clear_oldest_day + 1)} 天）`
    : '暂无通关记录';
  const ranked = historyCleanup.bosses.reduce((n, b) => n + b.rows, 0);
  $('#history-rank-now').textContent = historyCleanup.bosses.length
    ? `${num(historyCleanup.bosses.length)} 个 Boss 有排行，个人榜与组队榜共 ${num(ranked)} 条记录`
    : '暂无排行记录';
  setHTML(
    $('#history-rank-bosses'),
    historyCleanup.bosses.length
      ? historyCleanup.bosses.map(
          (b) => html`<label class="hc-boss">
            <input type="checkbox" value="${b.boss_id}" ${attr(historyCleanup.selected.has(b.boss_id), 'checked')} />
            <span class="hc-boss-name" title="${b.name || ''} · ${b.boss_id}"
              ><b>${b.name || '未命名 Boss'}</b><code>${b.boss_id}</code></span
            >
            <small>${num(b.rows)} 条</small>
            ${b.scoring ? stateTag('计分开放中', 'warn') : stateTag('已停止计分')}
          </label>`,
        )
      : html`<p class="hint">没有可重置的排行榜。</p>`,
  );
}

function renderHistoryCleanup() {
  if (!$('#maintenance-history-tool')) return;
  const m = maintenance.value;
  const job = m?.cleanup;
  const report = job?.scope === 'history' ? job.history_report : null;
  const locked = $('#maintenance-preview').disabled;
  const request = historyRequest();
  const problem = historyRequestProblem(request);
  for (const [check, card] of [
    ['#history-mail', 'mail'],
    ['#history-clear', 'clear'],
    ['#history-rank', 'rank'],
  ]) {
    const on = $(check).checked;
    const el = $(`#maintenance-history-tool [data-hc="${card}"]`);
    el.classList.toggle('off', !on);
    el.querySelectorAll('.hc-body input, .hc-body button').forEach((input) => (input.disabled = !on));
  }
  $('#history-preview').disabled = locked || !!problem;
  $('#history-preview').title = problem;
  const fresh = JSON.stringify(request) === historyCleanup.previewed;
  const work =
    report &&
    (report.mail_batches || report.clear_rows || report.rank_rows?.length || report.request?.vacuum);
  $('#history-apply').disabled =
    locked || !report || job.mode !== 'preview' || job.state !== 'complete' || !fresh || !work;
  $('#history-apply').title = report && !fresh ? '选择已改变，请重新预览' : '';

  // An applied job changes the sizes shown above; refresh them once.
  const key = report?.applied ? report.digest : '';
  if (key && historyCleanup.appliedJob !== key) {
    historyCleanup.appliedJob = key;
    loadHistoryOverview().catch((e) => reportError('maintenance', e));
  }
  setHTML($('#history-report'), historyReportHTML(job, report, fresh));
}

function historyReportHTML(job, report, fresh) {
  if (job?.scope !== 'history') return '';
  const lines = [];
  if (job.state === 'failed')
    lines.push(
      html`<p class="hc-error">${report?.applied ? '清理已提交，但' : '未确认清理成功：'}${job.error}</p>`,
    );
  if (job.state === 'running')
    lines.push(html`<p>正在${job.mode === 'apply' ? '清理' : '预览'}，请稍候…</p>`);
  if (!report || job.state === 'running') return lines;
  const done = report.applied;
  const rankRows = (report.rank_rows || []).reduce((n, r) => n + r.rows, 0);
  const rows = [];
  if (report.request.mail_before_utc)
    rows.push([
      '礼物发放批次',
      `${new Date(report.request.mail_before_utc).toLocaleString()} 之前创建、已全部完成`,
      `${num(report.mail_batches)} 个批次 · ${num(report.mail_receipts)} 条回执`,
      bytesText(report.mail_bytes),
    ]);
  if (report.request.clear_keep_days)
    rows.push([
      '通关卡组与昨日排行',
      `${clearDayText(report.clear_before_day)} 之前（保留 ${report.request.clear_keep_days} 天）`,
      `${num(report.clear_rows)} 条`,
      bytesText(report.clear_bytes),
    ]);
  if (report.request.rank_boss_ids)
    rows.push([
      'Boss 得分排行榜',
      `${report.request.rank_boss_ids.length} 个 Boss（个人榜与组队榜）`,
      `${num(rankRows)} 条`,
      '—',
    ]);
  lines.push(html`<p>
    <b>${done ? '已清理' : job.mode === 'preview' && !fresh ? '预览结果（选择已改变，需重新预览）' : '预览：将清理'}</b>
  </p>`);
  if (rows.length)
    lines.push(html`<div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>内容</th>
            <th>范围</th>
            <th class="num">数量</th>
            <th class="num">数据量</th>
          </tr>
        </thead>
        <tbody>
          ${rows.map(
            ([name, scope, count, size]) =>
              html`<tr>
                <td>${name}</td>
                <td>${scope}</td>
                <td class="num">${count}</td>
                <td class="num">${size}</td>
              </tr>`,
          )}
        </tbody>
      </table>
    </div>`);
  if (report.request.vacuum)
    lines.push(
      html`<p>
        整理数据库文件：${
          done && report.file_bytes_after
            ? `${bytesText(report.file_bytes)} → ${bytesText(report.file_bytes_after)}`
            : `当前 ${bytesText(report.file_bytes)}，其中空闲 ${bytesText(report.free_bytes)}；清理后会一并释放`
        }
      </p>`,
    );
  if (report.backup) lines.push(html`<p>恢复备份：<code>${report.backup}</code></p>`);
  else if (!done && (report.mail_batches || rankRows))
    lines.push(
      html`<p class="hint">执行时先把礼物批次、回执与排行名次压缩备份到数据库旁的 maintenance-backups。</p>`,
    );
  return lines;
}

async function previewHistoryCleanup() {
  const request = historyRequest();
  if (historyRequestProblem(request)) return;
  historyCleanup.previewed = JSON.stringify(request);
  return changeMaintenance(() =>
    api('/api/maintenance/cleanup', {
      method: 'POST',
      body: JSON.stringify({ scope: 'history', mode: 'preview', history: request }),
    }),
  );
}

async function applyHistoryCleanup() {
  const job = maintenance.value?.cleanup;
  const report = job?.history_report;
  if (job?.scope !== 'history' || !report || $('#history-apply').disabled) return;
  const request = report.request;
  const rows = [];
  if (request.mail_before_utc)
    rows.push([
      '礼物发放批次',
      `${num(report.mail_batches)} 个批次、${num(report.mail_receipts)} 条回执`,
      '删除（先备份）',
    ]);
  if (request.clear_keep_days)
    rows.push([
      '通关卡组与昨日排行',
      `${num(report.clear_rows)} 条`,
      `只保留最近 ${request.clear_keep_days} 天`,
    ]);
  if (request.rank_boss_ids)
    rows.push([
      'Boss 得分排行榜',
      `${request.rank_boss_ids.length} 个 Boss、${num(report.rank_rows.reduce((n, r) => n + r.rows, 0))} 条`,
      '个人榜与组队榜一并清空，从零开始（先备份）',
    ]);
  if (request.vacuum) rows.push(['数据库文件', bytesText(report.file_bytes), '整理并缩小']);
  const ok = await reviewChanges({
    title: '清理历史数据',
    lead: '以下内容将被删除，请核对后执行。',
    sections: [
      {
        title: '将要执行',
        summary: true,
        rows: rows.map(([name, count, action]) => [name, `${count} · ${action}`]),
      },
    ],
    note: '玩家的礼物箱、卡牌、货币、进度和领奖记录不受影响。清理后的礼物批次不能再次发放。',
    confirmText: '备份并清理',
  });
  if (!ok) return;
  return changeMaintenance(() =>
    api('/api/maintenance/cleanup', {
      method: 'POST',
      body: JSON.stringify({ scope: 'history', mode: 'apply', history: request, digest: report.digest }),
    }),
  );
}

function initHistoryCleanup() {
  const before = new Date(Date.now() - HISTORY_MAIL_DEFAULT_DAYS * 86400000);
  $('#history-mail-before').value = localTimeInput(Math.floor(before.getTime() / 1000));
  const tool = $('#maintenance-history-tool');
  tool.addEventListener('input', renderHistoryCleanup);
  tool.addEventListener('change', (event) => {
    if (event.target.closest('#history-rank-bosses')) {
      const id = Number(event.target.value);
      event.target.checked ? historyCleanup.selected.add(id) : historyCleanup.selected.delete(id);
    }
    renderHistoryCleanup();
  });
  $('#history-rank-closed').addEventListener('click', () => {
    historyCleanup.selected = new Set(historyCleanup.bosses.filter((b) => !b.scoring).map((b) => b.boss_id));
    renderHistoryOverview();
    renderHistoryCleanup();
  });
  $('#history-rank-none').addEventListener('click', () => {
    historyCleanup.selected.clear();
    renderHistoryOverview();
    renderHistoryCleanup();
  });
  $('#history-preview').addEventListener('click', previewHistoryCleanup);
  $('#history-apply').addEventListener('click', applyHistoryCleanup);
  $('#history-refresh').addEventListener('click', () =>
    loadHistoryOverview().catch((e) => reportError('maintenance', e)),
  );
}

initHistoryCleanup();
