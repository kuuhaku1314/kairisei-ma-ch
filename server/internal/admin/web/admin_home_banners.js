'use strict';
// 首页轮播 inside 公告与奖励: one ordered list of images that the Home screen rotates every 5.5 s.
// The section keeps its own state and hands a plain policy to the page-wide save (playerPolicyDraft).

const HOME_BANNER_MAX = 20;
const HOME_BANNER_ROTATE_MS = 5500;
const HOME_BANNER_SIZE = { width: 408, height: 150 }; // The client's own default banner.
const HOME_BANNER_TARGETS = [
  ['none', '仅展示'],
  ['notice', '游戏公告'],
  ['gacha_group', '扭蛋卡池组'],
  ['boss_score_rank', '圣剑杯排行榜'],
];
const homeBannerTargets = Object.fromEntries(HOME_BANNER_TARGETS);

const homeBannerEditor = {
  custom: false,
  rows: [],
  gachas: {},
  scoreBosses: {},
  bossQuery: new Map(), // row key → Boss filter text
  nextKey: 1,
  live: 0, // index into the currently visible rows shown on the stage
  hover: false,
  timer: 0,
  uploadAlert: null, // { key, title, lines, hint } of the last upload with errors; key 0 = new rows
};

// Model ------------------------------------------------------------------------------------------

function homeBannerRow(item) {
  return { key: homeBannerEditor.nextKey++, size: null, ...structuredClone(item) };
}

/** Wire form in the Go field order with zero values omitted, so the page dirty check stays exact. */
function homeBannerItem(row) {
  const item = {
    title: row.title.trim(),
    enabled: row.enabled,
    cover_path: row.cover_path || '',
    target: row.target,
  };
  if (row.target_id && (row.target === 'gacha_group' || row.target === 'boss_score_rank'))
    item.target_id = row.target_id;
  if (row.start_unix) item.start_unix = row.start_unix;
  if (row.end_unix) item.end_unix = row.end_unix;
  return item;
}

function homeBannerDraft() {
  return { custom: homeBannerEditor.custom, items: homeBannerEditor.rows.map(homeBannerItem) };
}

const homeBannerNow = () => Math.floor(Date.now() / 1000);
const homeBannerShown = (row, now = homeBannerNow()) =>
  !!row.cover_path && row.enabled && !(row.start_unix > now) && !(row.end_unix && row.end_unix <= now);

function homeBannerShortTime(unix) {
  const d = new Date(unix * 1000);
  const pad = (v) => String(v).padStart(2, '0');
  return `${d.getMonth() + 1}月${d.getDate()}日 ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function homeBannerStatus(row, now = homeBannerNow()) {
  if (!row.cover_path) return ['缺少图片', 'bad'];
  if (!row.enabled) return ['已停用', 'muted'];
  if (row.start_unix > now) return [`${homeBannerShortTime(row.start_unix)} 开始`, 'info'];
  if (row.end_unix && row.end_unix <= now) return ['已结束', 'muted'];
  return [row.end_unix ? `显示中 · ${homeBannerShortTime(row.end_unix)} 结束` : '显示中', 'ok'];
}

function homeBannerProblems(row) {
  const problems = [];
  if (!row.title.trim()) problems.push('请填写名称');
  if (!row.cover_path) problems.push('请上传图片');
  if (row.target === 'gacha_group' && !(row.target_id in homeBannerEditor.gachas))
    problems.push('请选择有效的目标卡池组');
  if (row.target === 'boss_score_rank' && !row.target_id) problems.push('请选择目标 Boss');
  if (row.start_unix && row.end_unix && row.end_unix <= row.start_unix)
    problems.push('结束时间须晚于开始时间');
  return problems;
}

/** Marks the fields behind each row's problems so the page alert scrolls to the first one. */
function markHomeBannerProblems() {
  for (const row of homeBannerEditor.rows) {
    const el = $(`#home-banner-list [data-key="${row.key}"]`);
    if (!el) continue;
    const mark = (selector, text) => markInvalid(el.querySelector(selector), text);
    if (!row.title.trim()) mark('[data-hb="title"]', '请填写名称');
    if (!row.cover_path) mark('.hb-thumb', '请上传图片');
    if (
      (row.target === 'gacha_group' && !(row.target_id in homeBannerEditor.gachas)) ||
      (row.target === 'boss_score_rank' && !row.target_id)
    )
      mark('[data-hb="target_id"]', '请选择跳转目标');
    if (row.start_unix && row.end_unix && row.end_unix <= row.start_unix)
      mark('[data-hb="end"]', '结束时间须晚于开始时间');
  }
}

function homeBannerValidation(policy) {
  for (const [i, row] of policy.items.entries()) {
    const problems = homeBannerProblems(row);
    if (problems.length) return `第 ${i + 1} 张：${problems.join('，')}。`;
  }
  return '';
}

function homeBannerTargetText(row) {
  if (row.target === 'gacha_group')
    return `扭蛋卡池组 · ${homeBannerEditor.gachas[row.target_id] || row.target_id || '未选择'}`;
  if (row.target === 'boss_score_rank')
    return `圣剑杯排行榜 · ${homeBannerEditor.scoreBosses[row.target_id] || row.target_id || '未选择'}`;
  return homeBannerTargets[row.target] || row.target;
}

function homeBannerSummary(policy) {
  return (
    (policy?.items || [])
      .map(
        (row, i) =>
          `${i + 1}. ${row.title}（${row.enabled ? '显示' : '停用'}；${homeBannerTargets[row.target] || row.target}` +
          `${row.target_id ? ' ' + row.target_id : ''}；${row.start_unix ? describeTime(row.start_unix) : '立即'} → ` +
          `${row.end_unix ? describeTime(row.end_unix) : '不限'}；${row.cover_path}）`,
      )
      .join('\n') || '无自定义图片'
  );
}

/** A soft hint only: the client scales the image into its 408×150 banner slot. */
function homeBannerRatioNote(size) {
  if (!size) return '';
  const expected = HOME_BANNER_SIZE.width / HOME_BANNER_SIZE.height;
  if (Math.abs(size.width / size.height - expected) / expected <= 0.06) return '';
  return `图片 ${size.width}×${size.height} 与横幅 ${HOME_BANNER_SIZE.width}×${HOME_BANNER_SIZE.height}（约 2.7 : 1）比例不同，游戏内可能被拉伸或裁切`;
}

// Rendering --------------------------------------------------------------------------------------

function renderHomeBanners(policy) {
  homeBannerEditor.custom = !!policy?.custom;
  homeBannerEditor.rows = (policy?.items || []).map(homeBannerRow);
  homeBannerEditor.bossQuery.clear();
  homeBannerEditor.live = 0;
  homeBannerEditor.uploadAlert = null;
  renderHomeBannerSection();
  showHomeBannerSource();
}

// Banners have their own home-banners folder and follow the 运营设置 image source like the pool
// covers; a failed read keeps the static wording.
async function showHomeBannerSource() {
  try {
    const { settings } = await api('/api/settings');
    const storage = settings.gacha_cover_source === 'storage';
    setHTML(
      $('#home-banner-source'),
      storage
        ? html`<b>存储容器</b> <code>${settings.gacha_cover_base_url}home-banners/</code>，新上传的图片需在「运营设置」同步`
        : html`<b>本服务器</b>，存档目录下的 <code>home-banners</code> 文件夹，可改为 S3／CDN`,
    );
  } catch {
    /* The static hint stays. */
  }
}

function renderHomeBannerSection() {
  const { custom, rows } = homeBannerEditor;
  $('#home-banner-count').textContent = `${rows.length} / ${HOME_BANNER_MAX} 张`;
  $$('#home-banner-mode input').forEach((el) => {
    el.checked = el.value === (custom ? 'custom' : 'system');
  });
  $('#home-banner-mode-note').textContent = custom
    ? '只显示下方列表中已启用、在显示时间内的图片；列表为空时首页不显示轮播。'
    : rows.length
      ? '首页显示默认横幅。下方图片已保留但不会显示，切换为自定义轮播即可启用。'
      : '首页显示默认横幅。添加图片后会自动切换为自定义轮播。';
  $('#pp-banners').classList.toggle('hb-system', !custom);
  $('#home-banner-add').disabled = rows.length >= HOME_BANNER_MAX;
  setHTML(
    $('#home-banner-list'),
    rows.length
      ? rows.map(homeBannerRowHTML)
      : html`<li class="hb-empty">
          <svg class="icon"><use href="#i-image" /></svg><b>还没有自定义图片</b>
          <span>点击下方「添加图片」，或把多张 PNG／JPEG 拖到这里。</span>
        </li>`,
  );
  setHTML($('#home-banner-upload-alert'), homeBannerUploadAlert(0));
  renderHomeBannerStage();
}

/** The last upload's errors, shown under the drop zone (key 0) or inside the replaced row. */
function homeBannerUploadAlert(key) {
  const alert = homeBannerEditor.uploadAlert;
  return alert?.key === key ? uploadAlertHTML(alert.title, alert.lines, alert.hint) : '';
}

function homeBannerTargetOptions(row) {
  const source = row.target === 'gacha_group' ? homeBannerEditor.gachas : homeBannerEditor.scoreBosses;
  const query = (homeBannerEditor.bossQuery.get(row.key) || '').trim().toLowerCase();
  const entries = Object.entries(source)
    .map(([id, name]) => [Number(id), name])
    .filter(
      ([id, name]) =>
        !query || id === row.target_id || String(id).includes(query) || name.toLowerCase().includes(query),
    )
    .sort((a, b) => a[0] - b[0]);
  if (row.target_id && !(row.target_id in source))
    entries.unshift([row.target_id, `${row.target_id}（已不可用）`]);
  const none =
    row.target === 'gacha_group'
      ? '选择卡池组'
      : source && Object.keys(source).length
        ? '选择 Boss'
        : '没有可选的计分 Boss';
  return html`<option value="0">${none}</option>
    ${entries.map(
      ([id, name]) =>
        html`<option value="${id}" ${attr(id === row.target_id, 'selected')}>${name} · ${id}</option>`,
    )}`;
}

function homeBannerRowHTML(row, index) {
  const [statusText, tone] = homeBannerStatus(row);
  const problems = homeBannerProblems(row);
  const ratio = homeBannerRatioNote(row.size);
  const picksTarget = row.target === 'gacha_group' || row.target === 'boss_score_rank';
  const bossCount = Object.keys(homeBannerEditor.scoreBosses).length;
  const label = `第 ${index + 1} 张`;
  return html`
    <li class="hb-row ${row.enabled ? '' : 'off'}" data-key="${row.key}">
      <div class="hb-grip" draggable="true" title="拖动调整顺序">
        <b>${index + 1}</b><span class="hb-grip-dots" aria-hidden="true"></span>
      </div>
      <button type="button" class="hb-thumb ${row.cover_path ? '' : 'empty'}" data-hb-pick title="点击选择或拖入图片">
        ${
          row.cover_path
            ? html`<img src="/${row.cover_path}" alt="${row.title}" draggable="false" />
              <span class="hb-thumb-hint">更换图片</span>`
            : html`<span class="hb-thumb-empty"
              ><svg class="icon"><use href="#i-upload" /></svg>上传图片</span
            >`
        }
      </button>
      <div class="hb-fields">
        <div class="hb-line">
          <input
            class="hb-title"
            data-hb="title"
            maxlength="80"
            value="${row.title}"
            placeholder="名称（仅后台识别）"
            aria-label="${label}名称"
          />
          <span class="hb-status">${stateTag(statusText, tone)}</span>
          <label class="switch hb-enabled" title="${row.enabled ? '停用后保留在列表中，不向玩家显示' : '启用此图'}">
            <input type="checkbox" data-hb="enabled" ${attr(row.enabled, 'checked')} aria-label="${label}对玩家显示" />
            <span>显示</span>
          </label>
        </div>
        <div class="hb-line">
          <span class="hb-label">点击后</span>
          <select data-hb="target" aria-label="${label}点击后打开">
            ${HOME_BANNER_TARGETS.map(
              ([value, text]) =>
                html`<option value="${value}" ${attr(row.target === value, 'selected')}>${text}</option>`,
            )}
          </select>
          ${
            picksTarget
              ? html`${
                  row.target === 'boss_score_rank' && bossCount > 8
                    ? html`<input
                      class="hb-search"
                      type="search"
                      data-hb-query
                      value="${homeBannerEditor.bossQuery.get(row.key) || ''}"
                      placeholder="筛选 Boss"
                      aria-label="${label}筛选 Boss"
                    />`
                    : ''
                }
                <select
                  class="hb-target-id"
                  data-hb="target_id"
                  aria-label="${label}跳转目标"
                  title="${row.target === 'boss_score_rank' ? '游戏内默认显示个人榜，玩家可切换到组队榜' : ''}"
                >
                  ${homeBannerTargetOptions(row)}
                </select>`
              : ''
          }
        </div>
        <div class="hb-line">
          <span class="hb-label">显示时间</span>
          <input
            type="datetime-local"
            data-hb="start"
            value="${localTimeInput(row.start_unix)}"
            title="留空为立即开始"
            aria-label="${label}开始显示（留空立即）"
          />
          <span class="hb-arrow">→</span>
          <input
            type="datetime-local"
            data-hb="end"
            value="${localTimeInput(row.end_unix)}"
            title="留空为不限"
            aria-label="${label}结束显示（留空不限）"
          />
          <span class="hb-quick" role="group" aria-label="快速设置结束时间">
            <button type="button" data-hb-days="7">+7 天</button>
            <button type="button" data-hb-days="30">+30 天</button>
            <button type="button" data-hb-days="0">不限</button>
          </span>
        </div>
        <p class="hb-note ${problems.length ? 'bad' : ''}" ${attr(!problems.length && !ratio, 'hidden')}>
          ${problems.length ? problems.join('；') : ratio}
        </p>
        ${homeBannerUploadAlert(row.key)}
      </div>
      <div class="hb-actions">
        <button type="button" class="icon-button" data-hb-move="-1" title="上移" aria-label="${label}上移" ${attr(index === 0, 'disabled')}>
          <svg class="icon"><use href="#i-up" /></svg>
        </button>
        <button
          type="button"
          class="icon-button"
          data-hb-move="1"
          title="下移"
          aria-label="${label}下移"
          ${attr(index === homeBannerEditor.rows.length - 1, 'disabled')}
        >
          <svg class="icon"><use href="#i-down" /></svg>
        </button>
        <button
          type="button"
          class="icon-button"
          data-hb-copy
          title="复制一张"
          aria-label="复制${label}"
          ${attr(homeBannerEditor.rows.length >= HOME_BANNER_MAX, 'disabled')}
        >
          <svg class="icon"><use href="#i-copy" /></svg>
        </button>
        <button type="button" class="icon-button hb-remove" data-hb-remove title="移除" aria-label="移除${label}">
          <svg class="icon"><use href="#i-trash" /></svg>
        </button>
      </div>
    </li>
  `;
}

/** Refreshes one row's status and notes in place, keeping focus in its inputs. */
function refreshHomeBannerRow(row) {
  const el = $(`#home-banner-list [data-key="${row.key}"]`);
  if (!el) return;
  const [statusText, tone] = homeBannerStatus(row);
  const problems = homeBannerProblems(row);
  const ratio = homeBannerRatioNote(row.size);
  setHTML(el.querySelector('.hb-status'), stateTag(statusText, tone));
  el.classList.toggle('off', !row.enabled);
  const note = el.querySelector('.hb-note');
  note.textContent = problems.length ? problems.join('；') : ratio;
  note.classList.toggle('bad', problems.length > 0);
  note.hidden = !problems.length && !ratio;
  renderHomeBannerStage();
}

/** The stage previews the edited rows at the current time, rotating like the client does. */
function renderHomeBannerStage() {
  const live = homeBannerEditor.custom ? homeBannerEditor.rows.filter((row) => homeBannerShown(row)) : [];
  if (homeBannerEditor.live >= live.length) homeBannerEditor.live = 0;
  const current = live[homeBannerEditor.live];
  const caption = !homeBannerEditor.custom
    ? '使用默认横幅'
    : live.length
      ? `${live.length} 张显示中 · 每 5.5 秒轮换`
      : '没有正在显示的图片';
  setHTML(
    $('#home-banner-stage'),
    html`
      <figcaption><b>当前草稿预览</b><span>${caption}</span></figcaption>
      <div class="hb-frame ${current ? '' : 'empty'}">
        ${
          current
            ? html`<img src="/${current.cover_path}" alt="${current.title}" />`
            : html`<span
              >${homeBannerEditor.custom ? '首页不显示轮播' : '默认横幅'}</span
            >`
        }
      </div>
      ${
        current
          ? html`<div class="hb-stage-meta">
            <span class="hb-stage-title">${current.title}</span>
            <span class="hint">点击后：${homeBannerTargetText(current)}</span>
          </div>`
          : ''
      }
      ${
        live.length > 1
          ? html`<div class="hb-dots" role="group" aria-label="切换预览">
            ${live.map(
              (row, i) =>
                html`<button
                  type="button"
                  data-hb-live="${i}"
                  aria-label="预览 ${row.title}"
                  ${attr(i === homeBannerEditor.live, 'aria-current="true"')}
                ></button>`,
            )}
          </div>`
          : ''
      }
      <p class="hint hb-stage-note">卡池组没有可见抽法时自动隐藏横幅。圣剑杯排行榜在 Boss 关闭后仍显示，供玩家查看最终成绩。</p>
    `,
  );
}

function rotateHomeBannerStage() {
  if (document.hidden || homeBannerEditor.hover || state.currentView !== 'player-policy') return;
  const live = homeBannerEditor.custom ? homeBannerEditor.rows.filter((row) => homeBannerShown(row)) : [];
  if (live.length < 2) return;
  homeBannerEditor.live = (homeBannerEditor.live + 1) % live.length;
  renderHomeBannerStage();
}

// Editing ----------------------------------------------------------------------------------------

const homeBannerRowOf = (el) =>
  homeBannerEditor.rows.find((row) => row.key === Number(el.closest('[data-key]')?.dataset.key));

function homeBannerChanged() {
  playerPolicyChanged();
}

function setHomeBannerField(row, field, el) {
  if (field === 'title') row.title = el.value;
  else if (field === 'enabled') row.enabled = el.checked;
  else if (field === 'start' || field === 'end') {
    const unix = el.value ? Math.floor(new Date(el.value).getTime() / 1000) : 0;
    row[field === 'start' ? 'start_unix' : 'end_unix'] = unix;
  } else if (field === 'target_id') row.target_id = Number(el.value) || 0;
  else if (field === 'target') {
    row.target = el.value;
    row.target_id = 0;
    homeBannerEditor.bossQuery.delete(row.key);
    renderHomeBannerSection();
    homeBannerChanged();
    $(`#home-banner-list [data-key="${row.key}"] [data-hb="target_id"]`)?.focus();
    return;
  }
  refreshHomeBannerRow(row);
  homeBannerChanged();
}

function setHomeBannerEndDays(row, days) {
  const now = homeBannerNow();
  // Whole minutes, matching what datetime-local can show and edit.
  row.end_unix = days ? Math.floor((Math.max(row.start_unix || 0, now) + days * 86400) / 60) * 60 : 0;
  const input = $(`#home-banner-list [data-key="${row.key}"] [data-hb="end"]`);
  if (input) input.value = localTimeInput(row.end_unix);
  refreshHomeBannerRow(row);
  homeBannerChanged();
}

function moveHomeBanner(from, to) {
  const rows = homeBannerEditor.rows;
  if (to < 0 || to >= rows.length || from === to) return;
  const [row] = rows.splice(from, 1);
  rows.splice(to, 0, row);
  renderHomeBannerSection();
  homeBannerChanged();
}

function setHomeBannerMode(custom) {
  homeBannerEditor.custom = custom;
  homeBannerEditor.live = 0;
  renderHomeBannerSection();
  homeBannerChanged();
}

// Uploads ----------------------------------------------------------------------------------------

/** Checks the file locally first; a rejection's message names the problem, not the file. */
function readHomeBannerFile(file) {
  if (!/^image\/(png|jpeg)$/.test(file.type)) return Promise.reject(new Error('不是 PNG 或 JPEG 图片'));
  if (file.size > 2 * 1024 * 1024)
    return Promise.reject(new Error(`${fileSizeText(file.size)}，超过 2 MiB 上限`));
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(',')[1]);
    reader.onerror = () => reject(new Error('读取文件失败'));
    reader.readAsDataURL(file);
  });
}

/**
 * Uploads files into existing rows (replace) or as new rows. The page stays locked while the
 * requests run so a page save cannot go out with a half-uploaded list. Files that fail are listed
 * next to where they were added; the others still go in.
 */
async function uploadHomeBanners(files, target = null) {
  files = [...files];
  if (!files.length || policyBusy('player-policy')) return;
  const failures = [];
  const room = HOME_BANNER_MAX - homeBannerEditor.rows.length;
  if (!target && files.length > room) {
    const skipped = files.splice(room).map((file) => `「${file.name}」`);
    failures.push(`最多 ${HOME_BANNER_MAX} 张，未添加：${skipped.join('、')}`);
  }
  let added = 0;
  let badFile = false;
  if (files.length) {
    state.publishing.add('player-policy');
    updatePolicyControls();
  }
  try {
    for (const file of target ? files.slice(0, 1) : files) {
      try {
        const data_base64 = await readHomeBannerFile(file);
        const result = await api('/api/home-banners', {
          method: 'POST',
          body: JSON.stringify({ data_base64 }),
        });
        const size = { width: result.width, height: result.height };
        if (target) Object.assign(target, { cover_path: result.cover_path, size });
        else {
          const title =
            file.name.replace(/\.[^.]+$/, '').slice(0, 80) || `轮播 ${homeBannerEditor.rows.length + 1}`;
          homeBannerEditor.rows.push(
            homeBannerRow({ title, enabled: true, cover_path: result.cover_path, target: 'none' }),
          );
          homeBannerEditor.rows.at(-1).size = size;
        }
        added++;
      } catch (error) {
        badFile ||= error.status === undefined || error.status === 400;
        failures.push(`「${file.name}」：${errorText(error)}`);
      }
    }
  } finally {
    state.publishing.delete('player-policy');
    updatePolicyControls();
  }
  if (added && !target && !homeBannerEditor.custom) {
    homeBannerEditor.custom = true;
    toast('已切换为自定义轮播');
  }
  homeBannerEditor.uploadAlert = failures.length
    ? {
        key: target?.key || 0,
        title: target ? '图片没有更换' : added ? `已添加 ${added} 张，其余没有添加` : '图片没有添加',
        lines: failures,
        hint: badFile ? UPLOAD_IMAGE_HINT : '',
      }
    : null;
  renderHomeBannerSection();
  homeBannerChanged();
  if (failures.length)
    $('#pp-banners .upload-alert').scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  else if (added) toast(target ? '图片已更换，保存整页后生效' : `已添加 ${added} 张图片，保存整页后生效`);
}

// Events -----------------------------------------------------------------------------------------

let homeBannerPickTarget = null;

function onHomeBannerInput(event) {
  const el = event.target;
  if (el.matches('[data-hb-query]')) {
    const row = homeBannerRowOf(el);
    homeBannerEditor.bossQuery.set(row.key, el.value);
    setHTML(el.parentElement.querySelector('[data-hb="target_id"]'), homeBannerTargetOptions(row));
    return;
  }
  if (!el.dataset.hb) return;
  const lazy = el.type === 'checkbox' || el.tagName === 'SELECT' || el.type === 'datetime-local';
  if ((event.type === 'input') === lazy) return;
  setHomeBannerField(homeBannerRowOf(el), el.dataset.hb, el);
}

async function onHomeBannerClick(event) {
  const pick = (selector) => event.target.closest(selector);
  let el;
  if ((el = pick('[data-hb-live]'))) {
    homeBannerEditor.live = Number(el.dataset.hbLive);
    renderHomeBannerStage();
  } else if ((el = pick('[data-hb-pick]'))) {
    homeBannerPickTarget = homeBannerRowOf(el);
    $('#home-banner-files').multiple = false;
    $('#home-banner-files').click();
  } else if ((el = pick('[data-hb-days]'))) {
    setHomeBannerEndDays(homeBannerRowOf(el), Number(el.dataset.hbDays));
  } else if ((el = pick('[data-hb-move]'))) {
    const index = homeBannerEditor.rows.indexOf(homeBannerRowOf(el));
    const to = index + Number(el.dataset.hbMove);
    moveHomeBanner(index, to);
    $(
      `#home-banner-list [data-key="${homeBannerEditor.rows[to].key}"] [data-hb-move="${el.dataset.hbMove}"]`,
    )?.focus();
  } else if ((el = pick('[data-hb-copy]'))) {
    const row = homeBannerRowOf(el);
    const index = homeBannerEditor.rows.indexOf(row);
    const copy = homeBannerRow({ ...homeBannerItem(row), title: `${row.title.trim()} 副本`.slice(0, 80) });
    copy.size = row.size;
    homeBannerEditor.rows.splice(index + 1, 0, copy);
    renderHomeBannerSection();
    homeBannerChanged();
    $(`#home-banner-list [data-key="${copy.key}"] .hb-title`)?.select();
  } else if ((el = pick('[data-upload-alert-close]'))) {
    homeBannerEditor.uploadAlert = null; // The shared handler removes the element.
  } else if ((el = pick('[data-hb-remove]'))) {
    const row = homeBannerRowOf(el);
    const title = `移除「${row.title || '未命名'}」？`;
    if (
      row.cover_path &&
      !(await askConfirm(title, '保存整页前可以放弃修改恢复。', { ok: '移除', danger: true }))
    )
      return;
    homeBannerEditor.rows.splice(homeBannerEditor.rows.indexOf(row), 1);
    renderHomeBannerSection();
    homeBannerChanged();
  }
}

// Drag and drop: the grip reorders rows; image files dropped on a thumbnail replace it, and
// files dropped anywhere else in the section are added as new rows.
const homeBannerDrag = { key: 0 };
const dragHasFiles = (event) => [...(event.dataTransfer?.types || [])].includes('Files');

function clearHomeBannerDropMarks() {
  $$('#pp-banners .drop-before, #pp-banners .drop-after, #pp-banners .drop-file').forEach((el) =>
    el.classList.remove('drop-before', 'drop-after', 'drop-file'),
  );
}

function onHomeBannerDragStart(event) {
  const grip = event.target.closest('.hb-grip');
  if (!grip) return;
  const row = grip.closest('.hb-row');
  homeBannerDrag.key = Number(row.dataset.key);
  event.dataTransfer.effectAllowed = 'move';
  event.dataTransfer.setData('text/plain', `home-banner:${homeBannerDrag.key}`);
  event.dataTransfer.setDragImage(row, 24, 24);
  row.classList.add('dragging');
}

function onHomeBannerDragOver(event) {
  const files = dragHasFiles(event);
  if (!files && !homeBannerDrag.key) return;
  event.preventDefault();
  clearHomeBannerDropMarks();
  if (files) {
    const thumb = event.target.closest('.hb-thumb');
    event.dataTransfer.dropEffect = 'copy';
    (thumb || $('#pp-banners .hb-dropzone')).classList.add('drop-file');
    return;
  }
  const row = event.target.closest('.hb-row');
  if (!row || Number(row.dataset.key) === homeBannerDrag.key) return;
  const box = row.getBoundingClientRect();
  row.classList.add(event.clientY < box.top + box.height / 2 ? 'drop-before' : 'drop-after');
}

function onHomeBannerDrop(event) {
  const files = dragHasFiles(event);
  if (!files && !homeBannerDrag.key) return;
  event.preventDefault();
  const before = $('#home-banner-list .drop-before');
  const after = $('#home-banner-list .drop-after');
  clearHomeBannerDropMarks();
  if (files) {
    const thumb = event.target.closest('.hb-thumb');
    uploadHomeBanners(event.dataTransfer.files, thumb ? homeBannerRowOf(thumb) : null);
    return;
  }
  const marked = before || after;
  if (!marked) return;
  const rows = homeBannerEditor.rows;
  const from = rows.findIndex((row) => row.key === homeBannerDrag.key);
  let to = rows.findIndex((row) => row.key === Number(marked.dataset.key)) + (after ? 1 : 0);
  if (from < to) to--;
  moveHomeBanner(from, to);
}

function onHomeBannerDragEnd() {
  homeBannerDrag.key = 0;
  clearHomeBannerDropMarks();
  $$('#home-banner-list .dragging').forEach((el) => el.classList.remove('dragging'));
}

const homeBannerSection = $('#pp-banners');
homeBannerSection.addEventListener('input', onHomeBannerInput);
homeBannerSection.addEventListener('change', onHomeBannerInput);
homeBannerSection.addEventListener('click', onHomeBannerClick);
homeBannerSection.addEventListener('dragstart', onHomeBannerDragStart);
homeBannerSection.addEventListener('dragover', onHomeBannerDragOver);
homeBannerSection.addEventListener('dragleave', (event) => {
  if (!homeBannerSection.contains(event.relatedTarget)) clearHomeBannerDropMarks();
});
homeBannerSection.addEventListener('drop', onHomeBannerDrop);
homeBannerSection.addEventListener('dragend', onHomeBannerDragEnd);
// Record each image's pixel size once it loads, for the ratio hint (load does not bubble).
$('#home-banner-list').addEventListener(
  'load',
  (event) => {
    if (event.target.tagName !== 'IMG') return;
    const row = homeBannerRowOf(event.target);
    if (!row || row.size) return;
    row.size = { width: event.target.naturalWidth, height: event.target.naturalHeight };
    if (homeBannerRatioNote(row.size)) refreshHomeBannerRow(row);
  },
  true,
);
$('#home-banner-mode').addEventListener('change', (event) =>
  setHomeBannerMode(event.target.value === 'custom'),
);
$('#home-banner-add').addEventListener('click', () => {
  homeBannerPickTarget = null;
  $('#home-banner-files').multiple = true;
  $('#home-banner-files').click();
});
$('#home-banner-files').addEventListener('change', (event) => {
  const files = [...event.target.files];
  event.target.value = '';
  uploadHomeBanners(files, homeBannerPickTarget);
  homeBannerPickTarget = null;
});
$('#home-banner-stage').addEventListener('mouseenter', () => (homeBannerEditor.hover = true));
$('#home-banner-stage').addEventListener('mouseleave', () => (homeBannerEditor.hover = false));
homeBannerEditor.timer = setInterval(rotateHomeBannerStage, HOME_BANNER_ROTATE_MS);
