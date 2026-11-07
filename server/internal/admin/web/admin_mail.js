'use strict';

const mailWorkspace = {
  basket: new Map(),
  running: false,
  paused: false,
  busy: false,
  current: null,
  jobsPage: 0,
  errors: [],
};
const rewardKey = (e) => `${e.reward_type}:${e.reward_type_id}`;
const rewardLimit = (e) =>
  [14, 16, 18].includes(e.reward_type) ? 1 : [6, 15, 19].includes(e.reward_type) ? 100 : 10000000;
// Reward lines of the basket, the send review and batch details: small art (honors are text plates), name and kind.
const mailThumb = (e) => (e.kind === 'honor' ? '' : `<span class="thumb thumb-sm">${catalogImage(e)}</span>`);
const mailKindName = (e) =>
  e.kind === 'currency'
    ? '货币'
    : pickerKindNames[e.kind === 'stamp' ? 'stamp_' + e.stamp_kind : e.kind] || '';
const cardTerms = (r) =>
  r.reward_type === 19
    ? `Lv.${r.card_level || 1}`
    : `Lv.${r.card_level || 1} · 名声 ${r.card_fame || 1} · 忠诚度 ${r.card_love || 0}`;
const rewardLine = (e, r) =>
  `<li>${mailThumb(e)}<b>${catalogEntryName(e, true)}</b><span>× ${num(r.quantity)}</span><small>${esc(mailKindName(e))}${[6, 19].includes(r.reward_type) ? ' · ' + cardTerms(r) : ''}</small></li>`;
const pendingBatchKey = 'cn602-admin-pending-batch-v2';
// All-player mode is resolved by the server at preview, independent of manual selection.
// The manual selection is stashed while it is active, so switching back restores it.
let recipientScope = null;
const mailRecipientCount = () => recipientScope?.count ?? selectedPlayers.size;
async function selectAllPlayers() {
  const data = await api('/api/accounts?include_system=0&limit=1');
  if (!data.total) throw new Error('当前没有可发放礼物的玩家');
  if (!recipientScope) {
    recipientScope = { count: data.total, stash: new Map(selectedPlayers) };
    selectedPlayers.clear();
    state.loaded.has('accounts') && renderAccounts();
  }
  recipientScope.count = data.total;
  updatePlayerSelection();
}
function selectSomePlayers() {
  const stash = recipientScope?.stash;
  recipientScope = null;
  stash?.forEach((a, id) => selectedPlayers.set(id, a));
  updatePlayerSelection();
  state.loaded.has('accounts') && renderAccounts();
}
function renderMailRecipients() {
  const all = !!recipientScope,
    n = mailRecipientCount();
  $$('#mail-scope input').forEach((el) => {
    el.checked = el.value === (all ? 'all' : 'some');
  });
  $('#mail-scope-some').hidden = all;
  $('#mail-scope-all').hidden = !all;
  $('#mail-recipients').textContent = all ? `全部玩家 · ${num(n)} 名` : n ? `已选 ${n} / 500 名` : '尚未选择';
  $('#mail-all-count').textContent = all ? num(n) : '—';
  $('#mail-scope-all .scope-notes li:last-child').hidden = !recipientScope?.stash?.size;
  $('#mail-clear-players').disabled = !selectedPlayers.size;
  const shown = [...selectedPlayers.values()].slice(0, 12);
  $('#mail-recipient-preview').innerHTML = shown.length
    ? shown
        .map(
          (a) =>
            `<span class="recipient-chip" title="${esc(playerLabel(a))}"><span>${esc(playerLabel(a))}</span><button type="button" class="x-button" data-unselect="${a.user_id}" aria-label="移除 ${esc(playerLabel(a))}"><svg class="icon"><use href="#i-close"/></svg></button></span>`,
        )
        .join('') +
      (selectedPlayers.size > shown.length
        ? `<span class="recipient-more">等共 ${selectedPlayers.size} 名</span>`
        : '')
    : '<span class="hint">在账号页勾选玩家，或按 ID 添加。</span>';
}
let pendingBatch = null;
try {
  pendingBatch = JSON.parse(localStorage.getItem(pendingBatchKey) || 'null');
} catch {
  /* The server batch list remains authoritative. */
}

async function loadMailWorkspace() {
  await Promise.all([loadCatalog(), loadMailJobs()]);
  if (!mailWorkspace.draftLoaded) {
    mailWorkspace.draftLoaded = true;
    try {
      const draft = JSON.parse(localStorage.getItem('cn602-admin-reward-draft-v2') || '[]');
      if (draft.length) await importRewardRequests(draft);
    } catch (e) {
      toast('奖励草稿未恢复：' + e.message, true);
    }
  }
  updatePlayerSelection();
  renderBasket();
  if (pendingBatch && !mailWorkspace.current) {
    try {
      await openMailBatch(pendingBatch.batch_id);
    } catch (e) {
      toast('上次批次尚未确认创建，点击继续时会使用原编号重试', true);
      updateMailControls();
    }
  }
}
async function loadCatalog() {
  const serial = ++state.catalogRequest;
  $('#catalog-status').textContent = '正在加载目录…';
  $('#catalog-grid').setAttribute('aria-busy', 'true');
  try {
    const data = await api(
      '/api/catalog?' +
        new URLSearchParams({
          ...cardFilterQuery('catalog', state.catalogKind === 'card'),
          ...catalogKindQuery(state.catalogKind, state.catalogStampKind),
          arthur_type: state.catalogKind === 'card' ? state.catalogJob : 0,
          q: $('#catalog-search').value.trim(),
          limit: 120,
          offset: state.catalogPage * 120,
        }),
    );
    if (serial !== state.catalogRequest) return;
    state.catalog = data.entries;
    state.catalogTotal = data.total;
    if (state.catalogPage > 0 && state.catalogPage * 120 >= data.total) {
      state.catalogPage = Math.max(0, Math.ceil(data.total / 120) - 1);
      return loadCatalog();
    }
    renderCatalog();
  } catch (e) {
    if (serial !== state.catalogRequest) return;
    $('#catalog-status').textContent = '目录加载失败';
    $('#catalog-grid').innerHTML = `<div class="empty"><b>目录加载失败</b>${esc(errorText(e))}</div>`;
    toast(errorText(e), true);
  } finally {
    if (serial === state.catalogRequest) $('#catalog-grid').removeAttribute('aria-busy');
  }
}
function catalogStatusText() {
  const here = state.catalog.filter((e) => mailWorkspace.basket.has(rewardKey(e))).length;
  return `<b>奖励篮已选 ${mailWorkspace.basket.size} 种</b><span>本页 ${state.catalog.length} 项，其中已选 ${here} 项</span>`;
}
function renderCatalog() {
  const card = state.catalogKind === 'card';
  $('#catalog-stamp-filter').hidden = state.catalogKind !== 'stamp';
  $$('#catalog-stamp-kinds button').forEach((b) =>
    b.classList.toggle('active', b.dataset.stampKind === state.catalogStampKind),
  );
  $('#catalog-rarity').hidden = !card;
  $('#catalog-jobs').closest('.chip-row').hidden = !card;
  for (const [group, prop] of [
    ['kinds', 'kind'],
    ['jobs', 'job'],
  ])
    $$(`#catalog-${group} button`).forEach((b) =>
      b.classList.toggle(
        'active',
        String(b.dataset[prop]) === String(state['catalog' + prop[0].toUpperCase() + prop.slice(1)]),
      ),
    );
  const filters = [...$$('#catalog-kinds button.active')].map((b) => b.textContent);
  if (state.catalogKind === 'stamp' && state.catalogStampKind)
    filters.push(pickerKindNames['stamp_' + state.catalogStampKind]);
  if (card) {
    const job = $('#catalog-jobs button.active');
    if (job && job.dataset.job !== '0') filters.push('职业：' + job.textContent);
  }
  if (card && $('#catalog-rarity').value !== '0')
    filters.push($('#catalog-rarity').selectedOptions[0].textContent);
  if (card)
    for (const key of ['attribute', 'cost', 'resource']) {
      const el = $(`#catalog-${key}`);
      if (el?.value) filters.push(el.selectedOptions[0].textContent);
    }
  if ($('#catalog-search').value.trim()) filters.push(`搜索：${$('#catalog-search').value.trim()}`);
  $('#catalog-filter-summary').textContent =
    `当前筛选：${filters.join(' · ')} · 共 ${num(state.catalogTotal)} 项`;
  $('#catalog-count').textContent = `${num(state.catalogTotal)} 项`;
  $('#catalog-status').innerHTML = catalogStatusText();
  $('#catalog-grid').classList.toggle('honor-grid', state.catalogKind === 'honor');
  $('#catalog-grid').innerHTML =
    state.catalog
      .map((e, i) => {
        const on = mailWorkspace.basket.has(rewardKey(e)),
          bad = e.resource_state === 'unavailable';
        return `<label class="catalog-card ${on ? 'selected' : ''} ${bad ? 'unavailable' : ''}${e.kind === 'honor' ? ' no-thumb' : ''}" title="${bad ? '资源暂不可用，不能发放' : ''}"><input type="checkbox" class="catalog-check check" data-index="${i}" ${on ? 'checked' : ''} ${bad ? 'disabled' : ''} aria-label="选择${esc(e.name || '空白称号')}">${catalogThumb(e)}<div class="catalog-copy"><b class="name" title="${esc(e.name)}">${catalogEntryName(e)}</b><span class="meta">${catalogMeta(e)}${bad ? '<span class="tag bad">资源暂不可用</span>' : ''}</span>${catalogFacts(e)}</div></label>`;
      })
      .join('') || '<div class="empty"><b>没有匹配的奖励</b>换个关键词，或点击“清除筛选”。</div>';
  let anchor = null;
  $$('.catalog-check').forEach(
    (box) =>
      (box.onclick = (e) => {
        const index = Number(box.dataset.index),
          first = e.shiftKey && anchor !== null ? Math.min(anchor, index) : index,
          last = e.shiftKey && anchor !== null ? Math.max(anchor, index) : index;
        try {
          for (let i = first; i <= last; i++) {
            const entry = state.catalog[i];
            if (entry.resource_state === 'unavailable') continue;
            if (box.checked) addReward(entry);
            else mailWorkspace.basket.delete(rewardKey(entry));
          }
        } catch (err) {
          toast(err.message, true);
        }
        anchor = index;
        renderBasket();
        $$('.catalog-check').forEach((c) => {
          c.checked = mailWorkspace.basket.has(rewardKey(state.catalog[Number(c.dataset.index)]));
          c.closest('.catalog-card').classList.toggle('selected', c.checked);
        });
      }),
  );
  renderPager('catalog', state.catalogPage, state.catalogTotal, 120, (page) => {
    state.catalogPage = page;
    loadCatalog();
  });
}
function rewardDefaults(e) {
  const quantity = Number($('#mail-quantity').value);
  if (!Number.isInteger(quantity) || quantity < 1 || quantity > 10000000) {
    markInvalid($('#mail-quantity'));
    throw new Error('默认数量必须为1–10000000的整数');
  }
  $('#mail-quantity').classList.remove('invalid');
  const card = e.reward_type === 6,
    leveled = card || e.reward_type === 19;
  return {
    reward_type: e.reward_type,
    reward_type_id: e.reward_type_id,
    quantity: Math.min(quantity, rewardLimit(e)),
    card_level: leveled ? e.fixed_card_level || ($('#mail-max-level').checked ? e.level_max : 1) : 0,
    card_fame: card ? ($('#mail-max-fame').checked ? e.fame_max : 1) : 0,
    card_love: card && $('#mail-max-love').checked ? e.love_max : 0,
  };
}
function addReward(entry, input) {
  const key = rewardKey(entry);
  if (mailWorkspace.basket.has(key)) return;
  if (mailWorkspace.basket.size >= 120) throw new Error('每批最多120种奖励，请拆分成多个方案');
  if (entry.resource_state === 'unavailable') throw new Error(`${entry.name} 资源暂不可用`);
  mailWorkspace.basket.set(key, { entry, request: input || rewardDefaults(entry) });
}
function renderBasket() {
  $('#catalog-status').innerHTML = catalogStatusText();
  $('#mail-basket-count').textContent = `${mailWorkspace.basket.size} / 120 种`;
  $('#mail-basket').innerHTML =
    [...mailWorkspace.basket.entries()]
      .map(
        ([key, { entry: e, request: r }]) =>
          `<tr><td><div class="basket-item">${mailThumb(e)}<div class="basket-copy"><b>${catalogEntryName(e, true)}</b><span class="meta">${e.kind === 'currency' ? '<span>货币</span>' : catalogMeta(e)}</span>${[6, 19].includes(e.reward_type) ? `<span class="sub">${cardTerms(r)}</span>` : ''}</div></div></td><td>${rewardLimit(e) === 1 ? '<span class="qty-fixed" title="变身皮肤、对话／表情、称号每项固定发放 1 个">1<small>固定</small></span>' : `<input class="basket-quantity" type="number" min="1" max="${rewardLimit(e)}" value="${r.quantity}" data-key="${key}" aria-label="${esc(e.name)}数量">`}</td><td><button class="x-button basket-remove" data-key="${key}" aria-label="移除${esc(e.name)}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`,
      )
      .join('') || emptyRow(3, '奖励篮是空的', '在左侧目录中勾选奖励，或导入奖励方案。');
  $$('.basket-quantity').forEach(
    (input) =>
      (input.onchange = () => {
        const item = mailWorkspace.basket.get(input.dataset.key),
          n = Number(input.value);
        if (!Number.isInteger(n) || n < 1 || n > rewardLimit(item.entry)) {
          markInvalid(input);
          input.value = item.request.quantity;
          return toast(
            `「${item.entry.name}」数量须为1–${rewardLimit(item.entry)}的整数，已恢复为 ${item.request.quantity}`,
            true,
          );
        }
        input.classList.remove('invalid');
        item.request.quantity = n;
        saveMailDraft();
      }),
  );
  $$('.basket-remove').forEach(
    (b) =>
      (b.onclick = () => {
        mailWorkspace.basket.delete(b.dataset.key);
        renderBasket();
        renderCatalog();
      }),
  );
  saveMailDraft();
  updateMailControls();
}
function saveMailDraft() {
  try {
    localStorage.setItem(
      'cn602-admin-reward-draft-v2',
      JSON.stringify([...mailWorkspace.basket.values()].map((x) => x.request)),
    );
  } catch {
    /* Export remains available if browser storage is full. */
  }
}
function updateMailSteps() {
  const players = mailRecipientCount(),
    rewards = mailWorkspace.basket.size,
    title = $('#mail-title').value.trim(),
    message = $('#mail-message').value.trim();
  const set = (step, done, text, warn = false) => {
    const li = $(`#mail-steps [data-step="${step}"]`);
    li.classList.toggle('done', done);
    li.classList.toggle('warn', warn);
    if (text !== undefined) li.querySelector('em').textContent = text;
  };
  set('players', players > 0, recipientScope ? `全部 ${players} 名` : `${players} 名`, !players);
  set('rewards', rewards > 0, `${rewards} 种`, !rewards);
  set('content', !!title && !!message, title && message ? '已填写' : '待填写', !title || !message);
  set(
    'send',
    !!mailWorkspace.current && !mailWorkspace.current.pending.length,
    mailWorkspace.running ? '发放中…' : undefined,
  );
}
function updateMailControls() {
  const w = mailWorkspace,
    c = w.current;
  $('#mail-send').disabled = w.busy || w.running || !w.basket.size || !mailRecipientCount();
  $('#mail-send').title = !mailRecipientCount() ? '请先选择收件人' : !w.basket.size ? '请先选择奖励' : '';
  $('#mail-job-retry').disabled = w.busy || w.running || !(c?.pending.length || (pendingBatch && !c));
  $('#mail-job-pause').disabled = !w.running;
  $('#mail-job-export').disabled = !c;
  $('#mail-job-reuse').disabled = !c || w.running || w.busy;
  if (c) {
    const total = c.batch.user_ids.length,
      done = c.done.length;
    $('#mail-job-status').innerHTML =
      `<span>${esc(c.batch.title)} <code>${esc(c.batch.batch_id)}</code></span> <span class="tag ${w.running ? 'info' : c.pending.length ? 'warn' : 'ok'}">${w.running ? '发放中' : c.pending.length ? '待继续' : '全部完成'}</span><span class="sub">已完成 ${done}/${total} 名玩家 · 每人 ${c.batch.rewards.length} 种奖励</span><div class="progress-line"><i style="width:${total ? Math.round((done / total) * 100) : 0}%"></i></div>`;
  } else if (pendingBatch)
    $('#mail-job-status').textContent =
      `上次发放「${pendingBatch.title || pendingBatch.batch_id}」尚未确认创建；点击“继续／重试”会使用原批次编号，不会重复发放。`;
  updateMailSteps();
}
async function loadMailJobs() {
  const serial = (loadMailJobs.serial || 0) + 1;
  loadMailJobs.serial = serial;
  const data = await api(
    '/api/mail-batches?' +
      new URLSearchParams({
        limit: 20,
        offset: mailWorkspace.jobsPage * 20,
        q: $('#mail-jobs-search').value.trim(),
        status: $('#mail-jobs-status').value,
      }),
  );
  if (serial !== loadMailJobs.serial) return;
  $('#mail-jobs').innerHTML =
    data.batches
      .map(
        (b) =>
          `<tr class="${mailWorkspace.current?.batch.batch_id === b.batch_id ? 'selected' : ''}"><td class="cell-wrap"><b>${esc(b.title)}</b>${b.all_players ? ' <span class="tag info">全部玩家</span>' : ''}<span class="sub"><code>${esc(b.batch_id)}</code></span></td><td>${esc(formatTime(b.created_utc))}</td><td class="num">${b.rewards}</td><td>${b.done}/${b.recipients} ${b.done < b.recipients ? '<span class="tag warn">未完成</span>' : '<span class="tag ok">完成</span>'}</td><td><button class="secondary sm mail-job-open" data-id="${esc(b.batch_id)}">查看／继续</button></td></tr>`,
      )
      .join('') ||
    emptyRow(
      5,
      $('#mail-jobs-search').value.trim() || $('#mail-jobs-status').value ? '没有匹配的批次' : '暂无发放批次',
      $('#mail-jobs-search').value.trim() || $('#mail-jobs-status').value
        ? '调整搜索或状态筛选。'
        : '确认发放后批次会出现在这里。',
    );
  $$('.mail-job-open').forEach((b) => (b.onclick = () => viewMailBatch(b.dataset.id)));
  const total = data.total ?? data.batches.length;
  $('#mail-jobs-info').textContent =
    `${mailWorkspace.jobsPage + 1} / ${Math.max(1, Math.ceil(total / 20))} 页 · 共 ${total} 个批次`;
  $('#mail-jobs-prev').disabled = mailWorkspace.jobsPage === 0;
  $('#mail-jobs-next').disabled = (mailWorkspace.jobsPage + 1) * 20 >= total;
}
async function openMailBatch(id) {
  mailWorkspace.current = await api(`/api/mail-batches/${encodeURIComponent(id)}`);
  mailWorkspace.errors = [];
  $('#mail-job-errors').textContent = '';
  updateMailControls();
}
async function viewMailBatch(id) {
  if (mailWorkspace.running || mailWorkspace.busy) return toast('请先等待当前操作完成或暂停当前批次');
  const modal = $('#mail-batch-modal'),
    body = $('#mail-batch-detail'),
    serial = (viewMailBatch.serial || 0) + 1;
  viewMailBatch.serial = serial;
  $('#mail-batch-lead').textContent = id;
  body.textContent = '正在读取批次详情…';
  $('#mail-batch-actions').disabled = true;
  modal.classList.add('open');
  try {
    const data = await api(`/api/mail-batches/${encodeURIComponent(id)}`);
    if (serial !== viewMailBatch.serial || !modal.classList.contains('open')) return;
    mailWorkspace.current = data;
    mailWorkspace.errors = [];
    $('#mail-job-errors').textContent = '';
    updateMailControls();
    const { batch, done, pending } = data;
    const entries = new Map(
      [...state.catalog, ...[...mailWorkspace.basket.values()].map((x) => x.entry)].map((e) => [
        rewardKey(e),
        e,
      ]),
    );
    const currencies = { 0: '亚瑟经验', 4: '金币', 9: '友情点', 10: '免费水晶', 12: '体力' };
    const kinds = {
      6: '卡牌',
      8: '道具',
      13: '素材卡',
      14: '变身皮肤',
      15: '召唤石',
      16: '对话／表情',
      18: '称号',
      19: '传承卡',
    };
    const ids = (list) => list.map((id) => `#${id}`).join('、') || '无';
    const render = (notice = '') => {
      const rewards = batch.rewards.map((r) => {
        const e = entries.get(rewardKey(r));
        return e
          ? rewardLine(e, r)
          : `<li><b>${esc(currencies[r.reward_type] || `${kinds[r.reward_type] || '奖励'} ${r.reward_type_id}`)}</b><span>× ${num(r.quantity)}</span>${[6, 19].includes(r.reward_type) ? `<small>${cardTerms(r)}</small>` : ''}</li>`;
      });
      const total = done.length + pending.length;
      $('#mail-batch-lead').textContent = `${batch.batch_id} · ${formatTime(data.created_utc)}`;
      // All-player batches can hold every account, so only their counts are listed.
      body.innerHTML = `<div class="review-facts"><div><span>完成进度</span><b>${num(done.length)} / ${num(total)}</b><small>${pending.length ? `${num(pending.length)} 名待继续` : '全部完成'}</small></div><div><span>收件人</span><b>${batch.all_players ? '全部玩家' : '指定玩家'}</b><small>${batch.all_players ? '名单在预览时固定' : `${num(total)} 名`}</small></div><div><span>每人奖励</span><b>${rewards.length} 种</b><small>每种一封礼物</small></div></div>
        <div class="review-block"><h4>奖励</h4><ul class="review-rewards">${rewards.join('')}</ul>${notice ? `<p class="hint">${esc(notice)}</p>` : ''}</div>
        <div class="review-block"><h4>邮件</h4><b>${esc(batch.title)}</b><p>${esc(batch.message)}</p></div>
        ${batch.all_players ? '' : `${pending.length ? `<details class="review-names" open><summary>未完成玩家（${pending.length}）</summary><p>${ids(pending)}</p></details>` : ''}<details class="review-names"><summary>已完成玩家（${done.length}）</summary><p>${ids(done)}</p></details>`}`;
    };
    render();
    $('#mail-batch-actions').disabled = false;
    loadMailJobs().catch((e) => toast(errorText(e), true));
    // Old rewards can leave the current catalog; their recorded IDs stay viewable.
    try {
      const resolved = await api('/api/catalog/resolve', {
        method: 'POST',
        body: JSON.stringify({ rewards: batch.rewards }),
      });
      if (serial !== viewMailBatch.serial || !modal.classList.contains('open')) return;
      for (const e of resolved.entries) entries.set(rewardKey(e), e);
      render();
    } catch {
      if (serial === viewMailBatch.serial && modal.classList.contains('open'))
        render('部分奖励名称暂无法解析，以上保留批次记录的类型与编号。');
    }
  } catch (e) {
    if (serial === viewMailBatch.serial && modal.classList.contains('open'))
      body.textContent = `批次详情读取失败：${errorText(e)}`;
  }
}
$('#mail-batch-close').onclick = () => {
  $('#mail-batch-modal').classList.remove('open');
  viewMailBatch.serial = (viewMailBatch.serial || 0) + 1;
};
$('#mail-batch-actions').onclick = () => {
  $('#mail-batch-close').click();
  $('.batch-panel .job-status').scrollIntoView({ behavior: 'smooth', block: 'center' });
};
async function runMailBatch() {
  const w = mailWorkspace;
  if (w.running || w.busy) return;
  w.running = true;
  w.paused = false;
  w.errors = [];
  updateMailControls();
  try {
    if (!w.current && pendingBatch) {
      await api('/api/mail-batches', { method: 'POST', body: JSON.stringify(pendingBatch) });
      await openMailBatch(pendingBatch.batch_id);
    }
    if (!w.current) return;
    const id = w.current.batch.batch_id;
    await openMailBatch(id);
    const queue = [...w.current.pending];
    for (let i = 0; i < queue.length && !w.paused; i += 10) {
      const data = await api(`/api/mail-batches/${encodeURIComponent(id)}/run`, {
        method: 'POST',
        body: JSON.stringify({ user_ids: queue.slice(i, i + 10) }),
      });
      const delivered = new Set(data.results.filter((r) => r.ok).map((r) => r.user_id));
      data.results.filter((r) => !r.ok).forEach((r) => w.errors.push(`#${r.user_id}：${r.error}`));
      w.current.done = [...new Set([...w.current.done, ...delivered])];
      w.current.pending = w.current.pending.filter((id) => !delivered.has(id));
      $('#mail-job-errors').textContent = w.errors.join('\n');
      updateMailControls();
    }
    const failures = [...w.errors];
    w.current = await api(`/api/mail-batches/${encodeURIComponent(id)}`);
    w.errors = failures;
    if (!w.current.pending.length) {
      pendingBatch = null;
      try {
        localStorage.removeItem(pendingBatchKey);
      } catch {}
      toast('批次全部发放完成');
    } else
      toast(
        w.paused
          ? '已暂停；已完成玩家不会重复发放'
          : `仍有${w.current.pending.length}人未完成，可点击“继续／重试未完成玩家”`,
        !!w.errors.length,
      );
    if (failures.length)
      $('#mail-job-errors').textContent =
        `${failures.length} 名玩家发放失败（其他玩家不受影响，可修正后重试）：\n${failures.join('\n')}`;
    state.loaded.delete('accounts');
    state.loaded.delete('audit');
    await loadMailJobs();
  } catch (e) {
    $('#mail-job-errors').textContent =
      `${w.errors.join('\n')}\n连接或执行中断：${errorText(e)}。可用同一批次重试，服务端会跳过已完成玩家。`.trim();
    toast('发放已暂停，请查看批次详情', true);
  } finally {
    w.running = false;
    updateMailControls();
  }
}
$('#mail-send').onclick = async () => {
  const w = mailWorkspace;
  if (w.running || w.busy) return;
  clearInvalid($('#mail .mail-form'));
  const missing = [];
  if (!$('#mail-title').value.trim()) {
    missing.push('邮件标题');
    markInvalid($('#mail-title'));
  }
  if (!$('#mail-message').value.trim()) {
    missing.push('正文');
    markInvalid($('#mail-message'));
  }
  if (missing.length) return toast(`请填写${missing.join('和')}`, true);
  w.busy = true;
  updateMailControls();
  try {
    const body = {
      batch_id: crypto.randomUUID(),
      user_ids: recipientScope ? [] : [...selectedPlayers.keys()],
      all_players: !!recipientScope,
      title: $('#mail-title').value,
      message: $('#mail-message').value,
      rewards: [...w.basket.values()].map((x) => ({ ...x.request })),
    };
    const preview = await api('/api/mail-batches/preview', { method: 'POST', body: JSON.stringify(body) });
    clearViewAlert('mail');
    const all = preview.batch.all_players,
      list = preview.batch.rewards.map((r, i) => rewardLine(preview.catalog[i], r));
    if (all && recipientScope) {
      recipientScope.count = preview.recipient_count;
      updatePlayerSelection();
    }
    const names = all
      ? ''
      : `<details class="review-names"><summary>收件名单（${preview.recipient_count} 名）</summary><p>${preview.batch.user_ids.map((id) => esc(playerLabel(selectedPlayers.get(id) || { user_id: id }))).join('、')}</p></details>`;
    $('#mail-review').innerHTML =
      `${all ? `<p class="review-warn"><svg class="icon"><use href="#i-alert"/></svg><span>将发给<b>全部 ${num(preview.recipient_count)} 名玩家</b>（不含系统伙伴，名单已在此固定）。确认后立即开始，可暂停，但已送达的礼物不会自动撤回。</span></p>` : ''}
      <div class="review-facts"><div><span>收件人</span><b>${all ? '全部玩家' : '指定玩家'}</b><small>${num(preview.recipient_count)} 名</small></div><div><span>每人奖励</span><b>${list.length} 种</b><small>每种一封礼物</small></div><div><span>礼物总数</span><b>${num(preview.mail_count)} 封</b><small>按每 10 人推进</small></div></div>
      <div class="review-block"><h4>奖励</h4><ul class="review-rewards">${list.join('')}</ul></div>
      <div class="review-block"><h4>邮件</h4><b>${esc(preview.batch.title)}</b><p>${esc(preview.batch.message)}</p></div>${names}`;
    $('#mail-review-confirm').textContent = `确认向 ${preview.recipient_count} 人发放`;
    $('#mail-review-modal').classList.add('open');
    $('#mail-review-confirm').disabled = false;
    let confirmed = false;
    $('#mail-review-confirm').onclick = async () => {
      if (confirmed || w.running || w.busy) return;
      confirmed = true;
      w.busy = true;
      $('#mail-review-confirm').disabled = true;
      updateMailControls();
      try {
        pendingBatch = preview.confirmation;
        try {
          localStorage.setItem(pendingBatchKey, JSON.stringify(pendingBatch));
        } catch {
          /* The server batch remains resumable if browser storage is unavailable. */
        }
        await api('/api/mail-batches', { method: 'POST', body: JSON.stringify(pendingBatch) });
        await openMailBatch(pendingBatch.batch_id);
        $('#mail-review-modal').classList.remove('open');
        w.busy = false;
        $('.batch-panel').scrollIntoView({ behavior: 'smooth', block: 'start' });
        await runMailBatch();
      } catch (e) {
        toast(errorText(e) + '；可从批次“继续／重试”按钮使用原编号重试', true);
        $('#mail-review-modal').classList.remove('open');
        mailWorkspace.current = null;
      } finally {
        w.busy = false;
        updateMailControls();
      }
    };
  } catch (e) {
    showViewAlert('mail', { title: '预览未通过', message: errorText(e) });
    toast(errorText(e), true);
  } finally {
    w.busy = false;
    updateMailControls();
  }
};
$('#mail-job-retry').onclick = runMailBatch;
$('#mail-scope').addEventListener('change', async (event) => {
  if (event.target.value === 'some') return selectSomePlayers();
  $$('#mail-scope input').forEach((el) => (el.disabled = true));
  try {
    await selectAllPlayers();
  } catch (e) {
    toast(errorText(e), true);
    renderMailRecipients();
  } finally {
    $$('#mail-scope input').forEach((el) => (el.disabled = false));
  }
});
$('#mail-all-refresh').onclick = async () => {
  const b = $('#mail-all-refresh');
  b.disabled = true;
  try {
    await selectAllPlayers();
  } catch (e) {
    toast(errorText(e), true);
  } finally {
    b.disabled = false;
  }
};
$('#mail-recipient-preview').addEventListener('click', (event) => {
  const b = event.target.closest('[data-unselect]');
  if (!b) return;
  selectedPlayers.delete(Number(b.dataset.unselect));
  updatePlayerSelection();
  state.loaded.has('accounts') && renderAccounts();
});
$('#mail-clear-players').onclick = async () => {
  if (
    selectedPlayers.size > 10 &&
    !(await askConfirm(`清空已选的 ${selectedPlayers.size} 名收件人？`, '', { ok: '清空', danger: true }))
  )
    return;
  selectedPlayers.clear();
  recipientScope = null;
  updatePlayerSelection();
  state.loaded.has('accounts') && renderAccounts();
};
$('#mail-job-pause').onclick = () => {
  mailWorkspace.paused = true;
  $('#mail-job-pause').disabled = true;
  toast('当前10人小批完成后暂停');
};
$('#mail-job-export').onclick = () =>
  downloadJSON(`礼物批次-${mailWorkspace.current.batch.batch_id}.json`, mailWorkspace.current);
// Rebuild a recurring compensation draft from a past batch; recipients are never copied.
$('#mail-job-reuse').onclick = async () => {
  const c = mailWorkspace.current;
  if (!c || mailWorkspace.running) return;
  if (
    !(await askConfirm(
      `用批次「${c.batch.title}」替换当前奖励篮？`,
      `载入该批次的 ${c.batch.rewards.length} 种奖励与标题／正文。收件人不会复制，请另行选择；发放前仍会再次预览。`,
      { ok: '替换' },
    ))
  )
    return;
  try {
    await importRewardRequests(c.batch.rewards);
    $('#mail-title').value = c.batch.title;
    $('#mail-message').value = c.batch.message;
    updateMailControls();
    $('#mail .mail-side').scrollIntoView({ behavior: 'smooth', block: 'start' });
    toast('已载入奖励与文案，请选择收件人后预览');
  } catch (e) {
    toast(errorText(e), true);
  }
};
$('#mail-title').oninput = $('#mail-message').oninput = (e) => {
  e.target.classList.remove('invalid');
  updateMailSteps();
};
$('#mail-jobs-refresh').onclick = () => loadMailJobs().catch((e) => toast(errorText(e), true));
let mailJobsTimer;
$('#mail-jobs-search').oninput = () => {
  clearTimeout(mailJobsTimer);
  mailJobsTimer = setTimeout(() => {
    mailWorkspace.jobsPage = 0;
    loadMailJobs().catch((e) => toast(errorText(e), true));
  }, 250);
};
$('#mail-jobs-status').onchange = () => {
  mailWorkspace.jobsPage = 0;
  loadMailJobs().catch((e) => toast(errorText(e), true));
};
$('#mail-jobs-prev').onclick = () => {
  mailWorkspace.jobsPage--;
  loadMailJobs().catch((e) => toast(errorText(e), true));
};
$('#mail-jobs-next').onclick = () => {
  mailWorkspace.jobsPage++;
  loadMailJobs().catch((e) => toast(errorText(e), true));
};
$('#mail-review-cancel').onclick = () => $('#mail-review-modal').classList.remove('open');
$('#catalog-select-page').onclick = () => {
  try {
    const rows = state.catalog.filter((e) => e.resource_state !== 'unavailable');
    if (new Set([...mailWorkspace.basket.keys(), ...rows.map(rewardKey)]).size > 120)
      throw new Error('本页与现有选择合计超过120种，请缩小范围');
    rows.forEach((e) => addReward(e));
    renderBasket();
    renderCatalog();
  } catch (e) {
    toast(e.message, true);
  }
};
$('#basket-clear').onclick = async () => {
  if (
    mailWorkspace.basket.size > 3 &&
    !(await askConfirm(`清空奖励篮中的 ${mailWorkspace.basket.size} 种奖励？`, '', {
      ok: '清空',
      danger: true,
    }))
  )
    return;
  mailWorkspace.basket.clear();
  renderBasket();
  renderCatalog();
};
$('#basket-apply-defaults').onclick = async () => {
  try {
    if (!mailWorkspace.basket.size) return;
    if (
      !(await askConfirm(
        `应用到奖励篮全部 ${mailWorkspace.basket.size} 种奖励？`,
        '按上方的默认数量与卡牌属性重新设置，逐项修改过的数量会被覆盖。',
        { ok: '应用' },
      ))
    )
      return;
    for (const item of mailWorkspace.basket.values()) item.request = rewardDefaults(item.entry);
    renderBasket();
  } catch (e) {
    toast(e.message, true);
  }
};
$('#basket-export').onclick = () =>
  downloadJSON('奖励方案.json', {
    schema_version: 1,
    rewards: [...mailWorkspace.basket.values()].map((x) => x.request),
  });
async function importRewardRequests(rewards) {
  if (!Array.isArray(rewards) || !rewards.length || rewards.length > 120)
    throw new Error('奖励方案需要1–120项');
  const data = await api('/api/catalog/resolve', { method: 'POST', body: JSON.stringify({ rewards }) });
  const next = new Map();
  rewards.forEach((r, i) => {
    const e = data.entries[i];
    if (next.has(rewardKey(e))) throw new Error('奖励方案含重复物品');
    if (e.fixed_card_level && r.card_level && r.card_level !== e.fixed_card_level)
      throw new Error(`${e.name} 获得时固定 Lv.${e.fixed_card_level}，请修正奖励方案`);
    next.set(rewardKey(e), {
      entry: e,
      request: {
        ...r,
        card_level: [6, 19].includes(e.reward_type) ? e.fixed_card_level || r.card_level || 1 : 0,
        card_fame: e.reward_type === 6 ? r.card_fame || 1 : 0,
        card_love: r.card_love || 0,
      },
    });
  });
  mailWorkspace.basket = next;
  renderBasket();
  renderCatalog();
}
$('#basket-import').onchange = async (e) => {
  try {
    const file = e.target.files[0];
    if (!file) return;
    if (file.size > 65536) throw new Error('方案文件超过64KB');
    let data;
    try {
      data = JSON.parse(await file.text());
    } catch {
      throw new Error('方案文件不是有效的 JSON');
    }
    if (data.schema_version !== 1) throw new Error('方案版本不支持');
    if (
      mailWorkspace.basket.size &&
      !(await askConfirm(
        '用导入的方案替换奖励篮？',
        `奖励篮中现有的 ${mailWorkspace.basket.size} 种奖励会被替换。`,
        { ok: '替换' },
      ))
    )
      return;
    await importRewardRequests(data.rewards);
    toast('已载入奖励方案，发放前会再次验证');
  } catch (err) {
    toast(err.message, true);
  } finally {
    e.target.value = '';
  }
};
$('#mail-add-ids').onclick = async () => {
  const input = $('#mail-recipient-list'),
    ids = [
      ...new Set(
        input.value
          .split(/[\s,，]+/)
          .filter(Boolean)
          .map(Number),
      ),
    ];
  const button = $('#mail-add-ids');
  button.disabled = true;
  input.classList.remove('invalid');
  try {
    const invalid = ids.filter((id) => !Number.isInteger(id) || id < 1000001 || id >= 1900000000);
    if (!ids.length || invalid.length) {
      markInvalid(input);
      throw new Error(
        invalid.length ? `这些不是有效的玩家ID：${invalid.slice(0, 10).join('、')}` : '请输入玩家ID',
      );
    }
    if (new Set([...selectedPlayers.keys(), ...ids]).size > 500) throw new Error('与现有收件人合计超过500人');
    const data = await api('/api/accounts/resolve', {
      method: 'POST',
      body: JSON.stringify({ user_ids: ids }),
    });
    const known = new Set(data.accounts.map((a) => a.user_id));
    if (ids.some((id) => !known.has(id))) {
      markInvalid(input);
      throw new Error('这些玩家不存在：' + ids.filter((id) => !known.has(id)).join('、'));
    }
    recipientScope = null;
    data.accounts.forEach((a) => selectedPlayers.set(a.user_id, a));
    updatePlayerSelection();
    input.value = '';
    toast(`已添加 ${data.accounts.length} 名收件人`);
  } catch (e) {
    toast(errorText(e), true);
  } finally {
    button.disabled = false;
  }
};
for (const [group, prop] of [
  ['kinds', 'kind'],
  ['jobs', 'job'],
])
  $$(`#catalog-${group} button`).forEach(
    (b) =>
      (b.onclick = () => {
        state['catalog' + prop[0].toUpperCase() + prop.slice(1)] =
          prop === 'job' ? Number(b.dataset[prop]) : b.dataset[prop];
        state.catalogPage = 0;
        loadCatalog();
      }),
  );
$$('#catalog-stamp-kinds button').forEach(
  (b) =>
    (b.onclick = () => {
      state.catalogStampKind = b.dataset.stampKind;
      state.catalogPage = 0;
      loadCatalog();
    }),
);
$('#catalog-search').oninput = () => {
  state.catalogPage = 0;
  state.catalogRequest++;
  clearTimeout(loadCatalog.timer);
  loadCatalog.timer = setTimeout(loadCatalog, 200);
};
$('#catalog-rarity').onchange = () => {
  state.catalogPage = 0;
  loadCatalog();
};
$('#catalog-reset-filters').onclick = () => {
  state.catalogJob = 0;
  state.catalogStampKind = '';
  $('#catalog-rarity').value = '0';
  resetCardFilters('catalog');
  state.catalogPage = 0;
  $('#catalog-search').value = '';
  clearTimeout(loadCatalog.timer);
  loadCatalog();
};
window.addEventListener('beforeunload', (event) => {
  if (mailWorkspace.running) {
    event.preventDefault();
    event.returnValue = '';
  }
});
updateMailControls();

$('#catalog-unselect-page').onclick = () => {
  state.catalog.forEach((e) => mailWorkspace.basket.delete(rewardKey(e)));
  renderBasket();
  renderCatalog();
};
