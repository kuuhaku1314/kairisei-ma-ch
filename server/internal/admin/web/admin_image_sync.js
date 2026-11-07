'use strict';
// 运营设置 › 同步到存储容器: copies the home-banner and pool-cover images the current configuration uses
// to the storage container address saved in the row just above, keeping their relative paths. Check first,
// then upload what is missing; every image shows its thumbnail, where it is used and what happened to it.

const imageSync = { data: null, timer: null, sending: false, onlyIssues: false, wasActive: false, drawn: '' };

const IMAGE_SYNC_KIND = { home: '首页轮播', gacha: '卡池封面' };

/** Content-addressed names are long hashes; the folder is already shown by the kind badge. */
const imageSyncName = (path) => path.replace(/^.*\/(.{8}).*(\.\w+)$/, '$1…$2');

/**
 * Display state of one image: [key, label, tone, reason]. Every problem shares the 有问题 tag and the
 * reason line under the card says what to do. Results apply only to the list they were run for.
 */
function imageSyncState(image, result) {
  if (!image.local) return ['issue', '有问题', 'bad', '本服务器缺少这张图片，请在原页面重新上传'];
  if (!result) {
    const job = imageSync.data?.job;
    const running = job?.state === 'running' && job.digest === imageSync.data.digest;
    return running ? ['waiting', '等待处理', 'muted', ''] : ['unchecked', '未检查', 'muted', ''];
  }
  if (result.remote === 'missing') return ['missing', '待上传', 'warn', ''];
  if (!['present', 'uploaded'].includes(result.remote) || result.public !== 'ok')
    return ['issue', '有问题', 'bad', result.error || '检查未通过，请重试'];
  return result.remote === 'uploaded' ? ['uploaded', '已上传', 'ok', ''] : ['present', '已在存储', 'ok', ''];
}

function imageSyncResults() {
  const { job, digest } = imageSync.data;
  return job && job.digest === digest ? job.results : {};
}

function imageSyncCounts() {
  const results = imageSyncResults();
  const counts = {
    unchecked: 0,
    waiting: 0,
    missing: 0,
    present: 0,
    uploaded: 0,
    issues: 0,
    retry: 0,
    bytes: 0,
  };
  for (const image of imageSync.data.images) {
    const [key] = imageSyncState(image, results[image.path]);
    if (key === 'missing') counts.bytes += image.bytes;
    if (key !== 'issue') counts[key]++;
    else if (image.local) counts.issues++, counts.retry++;
    else counts.issues++; // A file missing on this server is fixed on its own page, not by a retry.
  }
  return counts;
}

/** The last job covered this exact list and proved every image readable at the public address. */
function imageSyncReady() {
  const { job, digest, base_url: base, images } = imageSync.data;
  if (!job || job.state !== 'complete' || job.digest !== digest || job.base_url !== base) return false;
  return images.every((image) =>
    ['present', 'uploaded'].includes(imageSyncState(image, job.results[image.path])[0]),
  );
}

/**
 * Unsaved edits in the settings form above. Checks and uploads use the saved address, so an edited
 * address holds all three steps back; switching the source reloads the form, so any edit holds that one.
 */
function imageSyncDraft() {
  if (!runtimeSettings.rendered.has('settings')) return { address: false, any: false };
  return { address: coverSourceDraft().base !== coverSourceSaved().base, any: runtimeSettingsDirty() };
}

async function loadImageSync() {
  clearTimeout(imageSync.timer);
  imageSync.data = await api('/api/image-sync');
  renderImageSync();
  if (imageSync.data.job?.state === 'running')
    imageSync.timer = setTimeout(() => {
      if ($('#settings').classList.contains('active')) reloadImageSync();
    }, 1000);
}

const reloadImageSync = () => loadImageSync().catch((e) => reportError('settings', e, '读取'));

/**
 * Called by the settings form after every edit, load and save. A newer saved revision may carry another
 * address or source, so the list is read again; otherwise it is redrawn only when the edits that hold
 * steps back changed.
 */
function imageSyncSettingsChanged() {
  if (!imageSync.data || imageSync.sending || !$('#settings').classList.contains('active')) return;
  if (runtimeSettings.revision > imageSync.data.settings_revision) reloadImageSync();
  else if (JSON.stringify(imageSyncDraft()) !== imageSync.drawn) renderImageSync();
}

/** One line under the address row: the bucket the credentials point at and the object prefix. */
function imageSyncTargetHTML() {
  const d = imageSync.data;
  const s = d.storage;
  return html`<p class="is-target">
    <span>上传到</span>
    ${
      s.configured
        ? html`<b>${s.bucket}</b><small>${s.endpoint} · 密钥 ${s.key_hint}</small>`
        : html`<b class="is-none">未配置</b><small>${s.problem ? '上传凭据有误' : '没有读到上传凭据'}</small>`
    }
    ${
      d.base_url
        ? html`<span>对象前缀</span>${d.object_prefix ? html`<code>${d.object_prefix}</code>` : html`<small>桶根目录</small>`}`
        : ''
    }
  </p>`;
}

function imageSyncNoticesHTML(counts, draft) {
  const d = imageSync.data;
  const s = d.storage;
  const notes = [];
  if (s.problem)
    notes.push(html`<div class="is-callout warn">
      <b>上传凭据有误，同步已关闭</b>
      <p>读取 <code>${s.file}</code> 时：${s.problem}。游戏服务不受影响；修正文件后重启服务即可。</p>
    </div>`);
  else if (!s.configured)
    notes.push(html`<div class="is-callout">
      <b>未配置上传凭据</b>
      <p>
        在服务端目录的 <code>cdn-sync.json</code> 填写 endpoint_url、bucket、access_key_id 和
        secret_access_key（与资源 CDN 同步共用这个文件），启动时带上
        <code>-cdn-sync-config cdn-sync.json</code>，重启后即可同步。下面先列出需要同步的图片。
      </p>
    </div>`);
  // The saved address is the one being replaced while an edit is pending, so its problem is not shown.
  if (draft.address)
    notes.push(html`<div class="is-callout warn">
      <b>存储容器地址还没有保存</b>
      <p>检查和上传都按已保存的地址进行。请先保存设置，再检查存储。</p>
    </div>`);
  else if (d.base_url && d.base_problem)
    notes.push(html`<div class="is-callout warn">
      <b>存储容器地址不能用于同步</b>
      <p>${d.base_problem}。图片会上传到这个地址对应的位置，玩家也从这里读取。</p>
    </div>`);
  else if (d.base_problem)
    notes.push(html`<div class="is-callout">
      <b>还没有存储容器地址</b>
      <p>在上方填写桶的公开下载地址并保存（图片来源可以先保持「本服务器」）。图片会上传到这个地址对应的位置。</p>
    </div>`);
  const results = imageSyncResults();
  const checked = d.images.some((image) => results[image.path]);
  if (d.source === 'storage' && checked && (counts.missing || counts.issues))
    notes.push(html`<div class="is-callout bad">
      <b>玩家正从存储容器读取，但有 ${counts.missing + counts.issues} 张图片读不到</b>
      <p>这些图片在游戏里会显示为空白。请上传缺少的图片，或处理下面标红的问题。</p>
    </div>`);
  return notes;
}

function imageSyncStepsHTML(counts, draft) {
  const d = imageSync.data;
  const job = d.job;
  const running = job?.state === 'running';
  const blocked = !d.storage.configured || !!d.base_problem || !d.images.length || draft.address;
  const checked = job && job.digest === d.digest && !running;
  const ready = imageSyncReady();
  const pending = counts.missing + counts.retry;
  const runLabel = !checked
    ? '上传'
    : counts.missing
      ? `上传 ${num(counts.missing)} 张`
      : pending
        ? '重试'
        : '无需上传';
  return html`<ol class="mt-steps">
    <li>
      <span>检查</span
      ><button class="secondary" type="button" data-is-action="check" ${attr(blocked || running || imageSync.sending, 'disabled')}>
        ${checked ? '重新检查' : '检查存储'}
      </button>
    </li>
    <li>
      <span>同步</span
      ><button
        class="primary"
        type="button"
        data-is-action="sync"
        title="${checked ? '' : '请先检查，确认要上传哪些图片'}"
        ${attr(blocked || running || imageSync.sending || !checked || !pending, 'disabled')}
      >
        ${runLabel}
      </button>
    </li>
    ${
      d.source === 'server'
        ? html`<li>
          <span>启用</span
          ><button
            class="secondary"
            type="button"
            data-is-action="activate"
            title="${!ready ? '全部图片确认可从公开地址读取后才能切换' : draft.any ? '请先保存或放弃上面的修改' : ''}"
            ${attr(!ready || draft.any || running || imageSync.sending, 'disabled')}
          >
            改为从存储容器读取
          </button>
        </li>`
        : ''
    }
  </ol>`;
}

function imageSyncSummaryHTML(counts) {
  const d = imageSync.data;
  const job = d.job;
  if (job?.state === 'running')
    return html`<div class="is-progress">
      <progress max="${job.total}" value="${job.done}"></progress>
      <span>正在${job.mode === 'sync' ? '上传并检查公开地址' : '检查存储'}… ${num(job.done)} / ${num(job.total)}</span>
    </div>`;
  const kinds = Object.entries(IMAGE_SYNC_KIND)
    .map(([kind, name]) => [name, d.images.filter((image) => image.kind === kind).length])
    .filter(([, n]) => n)
    .map(([name, n]) => `${name} ${num(n)}`)
    .join(' · ');
  const chips = [];
  if (counts.present) chips.push(stateTag(`已在存储 ${num(counts.present)}`, 'ok'));
  if (counts.uploaded) chips.push(stateTag(`已上传 ${num(counts.uploaded)}`, 'ok'));
  if (counts.missing)
    chips.push(stateTag(`待上传 ${num(counts.missing)} · ${bytesText(counts.bytes)}`, 'warn'));
  if (counts.issues) chips.push(stateTag(`有问题 ${num(counts.issues)}`, 'bad'));
  if (counts.unchecked) chips.push(stateTag(`未检查 ${num(counts.unchecked)}`));
  const stale = job && job.digest !== d.digest;
  return html`<div class="is-summary">
    <span><b>正在使用 ${num(d.images.length)} 张</b>${kinds ? `（${kinds}）` : ''}</span>
    ${chips}
    ${stale ? html`<span class="hint">图片配置或地址在上次检查后有变化，请重新检查</span>` : ''}
    ${
      counts.missing + counts.issues && d.images.length > 6
        ? html`<label class="is-filter"
          ><input type="checkbox" data-is-filter ${attr(imageSync.onlyIssues, 'checked')} />只看需要处理的</label
        >`
        : ''
    }
  </div>`;
}

function imageSyncCardHTML(image, result) {
  const [key, label, tone, reason] = imageSyncState(image, result);
  const shown = image.uses.slice(0, 2);
  return html`<figure class="is-card is-${key}">
    <div class="is-thumb">
      ${
        image.local
          ? html`<a href="/${image.path}" target="_blank" rel="noopener" title="查看原图"
              ><img src="/${image.path}" alt="" loading="lazy" decoding="async"
            /></a>`
          : html`<span>本服务器上没有这张图片</span>`
      }
      <span class="is-kind">${IMAGE_SYNC_KIND[image.kind]}</span>
    </div>
    <figcaption>
      <ul class="is-uses">
        ${shown.map(
          (use) => html`<li>
            <button type="button" class="link" data-is-view="${use.view}" title="前往该配置">${use.label}</button
            ><small>${use.note}</small>
          </li>`,
        )}
        ${
          image.uses.length > shown.length
            ? html`<li class="is-more" title="${image.uses
                .slice(shown.length)
                .map((use) => `${use.label}（${use.note}）`)
                .join('\n')}">
              另有 ${image.uses.length - shown.length} 处使用
            </li>`
            : ''
        }
      </ul>
      <div class="is-meta">
        ${stateTag(label, tone)}<code title="${image.path}">${imageSyncName(image.path)}</code
        ><span>${image.local ? bytesText(image.bytes) : '—'}</span>
      </div>
      ${reason ? html`<p class="is-error">${reason}</p>` : ''}
    </figcaption>
  </figure>`;
}

function imageSyncReportHTML(draft) {
  const d = imageSync.data;
  const job = d.job;
  if (!job || job.state !== 'complete') return '';
  const results = Object.values(job.results);
  const count = (test) => results.filter(test).length;
  const uploaded = count((r) => r.remote === 'uploaded');
  const present = count((r) => r.remote === 'present');
  const missing = count((r) => r.remote === 'missing');
  const publicOK = count((r) => r.public === 'ok');
  const problems = count((r) => ['conflict', 'error'].includes(r.remote) || r.public === 'failed');
  const when = new Date(job.finished_utc).toLocaleString();
  const lines = [];
  if (job.mode === 'sync')
    lines.push(html`<p>
      <b>${when} 同步完成</b>：上传 ${num(uploaded)} 张，已存在 ${num(present)} 张；公开地址读取通过
      ${num(publicOK)} 张${problems ? html`，<b class="is-bad">${num(problems)} 张有问题</b>` : ''}。
    </p>`);
  else
    lines.push(html`<p>
      <b>${when} 检查完成</b>：已在存储 ${num(present)} 张（公开地址读取通过 ${num(publicOK)} 张），待上传
      ${num(missing)} 张${problems ? html`，<b class="is-bad">${num(problems)} 张有问题</b>` : ''}。
    </p>`);
  if (job.mode === 'sync' && !job.audit_error) lines.push(html`<p class="hint">本次同步已记入操作审计。</p>`);
  if (job.audit_error) lines.push(html`<p class="is-bad">${job.audit_error}</p>`);
  if (imageSyncReady())
    lines.push(
      d.source === 'storage'
        ? html`<p>玩家从存储容器读取，正在使用的图片都能正常下载。之后新上传或更换图片，记得再来同步一次。</p>`
        : draft.any
          ? html`<p>全部图片都能从公开地址读取。保存或放弃上面的修改后，可以点「改为从存储容器读取」切换图片来源。</p>`
          : html`<p>全部图片都能从公开地址读取。可以点「改为从存储容器读取」切换图片来源。</p>`,
    );
  return lines;
}

function renderImageSync() {
  const body = $('#image-sync-body');
  if (!body || !imageSync.data) return;
  const d = imageSync.data;
  const counts = imageSyncCounts();
  const results = imageSyncResults();
  const draft = imageSyncDraft();
  imageSync.drawn = JSON.stringify(draft);
  const visible = d.images.filter(
    (image) =>
      !imageSync.onlyIssues ||
      !['present', 'uploaded'].includes(imageSyncState(image, results[image.path])[0]),
  );
  setHTML(
    body,
    html`${imageSyncTargetHTML()} ${imageSyncNoticesHTML(counts, draft)}
      ${
        d.images.length
          ? html`${imageSyncSummaryHTML(counts)} ${imageSyncStepsHTML(counts, draft)}
            <div class="is-grid">${visible.map((image) => imageSyncCardHTML(image, results[image.path]))}</div>`
          : html`<p class="is-empty">
            首页轮播和卡池目前都没有使用上传的图片。在「公告与奖励」或「卡池配置」上传图片后，这里会列出需要同步的文件。
          </p>`
      }
      <div class="mt-report" aria-live="polite">${imageSyncReportHTML(draft)}</div>`,
  );
}

async function sendImageSync(action) {
  if (imageSync.sending) return;
  imageSync.sending = true;
  renderImageSync();
  try {
    await action();
    await loadImageSync();
  } catch (e) {
    reportError('settings', e, '同步');
    await loadImageSync().catch(() => {});
  } finally {
    imageSync.sending = false;
    renderImageSync();
  }
}

function startImageSync(mode) {
  return sendImageSync(() =>
    api('/api/image-sync', { method: 'POST', body: JSON.stringify({ mode, digest: imageSync.data.digest }) }),
  );
}

async function confirmImageSync() {
  const d = imageSync.data;
  const results = imageSyncResults();
  const ACTION = { missing: '上传', conflict: '重新检查（存储里的不同文件不会被覆盖）' };
  const pending = d.images
    .filter((image) => image.local)
    .map((image) => [image, imageSyncState(image, results[image.path])[0]])
    .filter(([, key]) => !['present', 'uploaded'].includes(key));
  const uploads = pending.filter(([, key]) => key === 'missing').length;
  const retries = pending.length - uploads;
  const ok = await reviewChanges({
    title: '同步到存储容器',
    lead:
      `上传 ${uploads} 张图片到 ${d.storage.bucket}` +
      (retries ? `，另有 ${retries} 张重新检查` : '') +
      `。玩家通过 ${d.base_url} 读取。`,
    sections: [
      {
        title: '本次处理',
        summary: true,
        rows: pending.map(([image]) => [
          image.uses[0].label + (image.uses.length > 1 ? ` 等 ${image.uses.length} 处` : ''),
          `${IMAGE_SYNC_KIND[image.kind]} ${imageSyncName(image.path)} · ${bytesText(image.bytes)} · ${ACTION[results[image.path]?.remote] || '重试'}`,
        ]),
      },
    ],
    note: '已在存储且内容一致的文件会跳过，不会覆盖或删除存储里的文件；上传后逐张检查公开地址。',
    confirmText: '开始上传',
  });
  if (ok) return startImageSync('sync');
}

async function activateImageSync() {
  const d = imageSync.data;
  const ok = await reviewChanges({
    title: '改为从存储容器读取',
    sections: [
      {
        title: '上传图片',
        rows: diffRows([['图片来源', '本服务器', '存储容器']]),
      },
    ],
    note: `玩家下次打开扭蛋页或回到首页时，从 ${d.base_url} 读取卡池封面和首页轮播图。之后新上传或更换的图片要再同步一次。`,
    confirmText: '确认切换',
  });
  if (!ok) return;
  await sendImageSync(async () => {
    // The answer is the new settings document; the form above has no edits (the step waits for that).
    const saved = await api('/api/image-sync/activate', {
      method: 'POST',
      body: JSON.stringify({ digest: d.digest, expected_revision: d.settings_revision }),
    });
    acceptRuntimeSettings(saved, 'settings');
    toast('已改为从存储容器读取图片');
  });
}

function initImageSync() {
  const tool = $('#image-sync-tool');
  tool.addEventListener('click', (event) => {
    const view = event.target.closest('[data-is-view]')?.dataset.isView;
    if (view) return switchView(view);
    const action = event.target.closest('[data-is-action]')?.dataset.isAction;
    if (action === 'check') startImageSync('check');
    else if (action === 'sync') confirmImageSync();
    else if (action === 'activate') activateImageSync();
  });
  tool.addEventListener('change', (event) => {
    if (!event.target.matches('[data-is-filter]')) return;
    imageSync.onlyIssues = event.target.checked;
    renderImageSync();
  });
  // Load whenever 运营设置 becomes the active view (navigation, direct link or refresh).
  const view = $('#settings');
  new MutationObserver(() => {
    if (view.classList.contains('active') && !imageSync.wasActive) reloadImageSync();
    imageSync.wasActive = view.classList.contains('active');
  }).observe(view, { attributes: true, attributeFilter: ['class'] });
  $('#refresh').addEventListener('click', () => view.classList.contains('active') && reloadImageSync());
  if (view.classList.contains('active')) reloadImageSync();
}

initImageSync();
