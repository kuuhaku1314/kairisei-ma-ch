'use strict';
// Boss 掉落 › 炼金塔: pick a tower, then one of its battle floors. A floor is edited by tower_id + floor,
// never by its boss ID alone (the same boss may also be an ordinary Boss or sit in another tower).
// Edits of several floors are kept side by side in dropEditor.drafts and saved together in one
// request; leaving the tower catalog asks before dropping them.

const towerPick = {
  towerID: 0,
  lastKind: 'all',
  lastFloor: {},
  info: new Map(),
  serial: 0,
  built: { towers: null, tower: 0 },
};
const DRAFT_FIELDS = ['row', 'base', 'config', 'saved', 'revision', 'modes', 'stash'];

const towerKey = (row) => `${row.tower_id}:${row.floor}`;
const dropIsTower = () => !!dropEditor.row?.tower_id;
const towerFloorBoss = (row) => row.difficulty.replace(/^第\s*\d+\s*层\s*·\s*/, '') || row.name;
const currentTower = () =>
  dropEditor.towers.find((t) => t.tower_id === towerPick.towerID) || dropEditor.towers[0];
const draftSnapshot = () => Object.fromEntries(DRAFT_FIELDS.map((key) => [key, dropEditor[key]]));

/** Runs fn with another floor's draft loaded into dropEditor, for checks and the save review. */
function withDraft(draft, fn) {
  const keep = draftSnapshot();
  Object.assign(dropEditor, draft);
  try {
    return fn();
  } finally {
    Object.assign(dropEditor, keep);
  }
}

/** Every floor with unsaved edits, the open one included, in tower and floor order. */
function towerDrafts() {
  const list = [...dropEditor.drafts.values()];
  if (dropIsTower() && dropEditor.dirty) list.push(draftSnapshot());
  return list.sort((a, b) => a.row.tower_id - b.row.tower_id || a.row.floor - b.row.floor);
}

function towerDraftName(row) {
  const floor = `第 ${row.floor} 层`;
  return dropEditor.towers.length > 1 ? `${row.name} ${floor}` : floor;
}

// Copying from a sibling: ordinary bosses copy within their group, tower floors within their tower.
function dropSiblings(row) {
  if (!row.tower_id) {
    return dropEditor.rows.filter((b) => b.group_id === row.group_id && b.boss_id !== row.boss_id);
  }
  const tower = dropEditor.towers.find((t) => t.tower_id === row.tower_id);
  return (tower?.floors || []).filter((f) => f.floor !== row.floor);
}
const dropSiblingKey = (row) => (row.tower_id ? row.floor : row.boss_id);
function fetchDropData(row) {
  if (!row.tower_id) return api(`/api/boss-drops?boss_id=${row.boss_id}`);
  const draft = dropEditor.drafts.get(towerKey(row));
  if (draft) return Promise.resolve({ config: draft.config });
  return api(`/api/tower-drops?tower_id=${row.tower_id}&floor=${row.floor}`);
}

// "已调整" on a floor tile: its saved configuration differs from the packaged one.
const dropSignature = (drops) =>
  (drops || [])
    .map((d) =>
      [
        d.battle_index,
        d.enemy_index,
        d.reward.type,
        d.reward.reward_typeid,
        d.reward.num,
        d.chance_per_million ?? 1000000,
      ].join(':'),
    )
    .sort()
    .join('|');
const towerFloorChanged = (config, base) =>
  config.first_clear_rewards != null ||
  dropSignature(config.enemy_drops) !== dropSignature(base.enemy_drops) ||
  stableJSON(config.fame_rewards ?? null) !== stableJSON(base.fame_rewards ?? null);

// Catalog switch -----------------------------------------------------------------------------------

const DROP_HINTS = {
  boss: [
    '选择副本难度',
    '同一副本 ID 在活动和往期入口共用掉落配置；切换筛选会保留正在编辑的草稿。',
    '每行独立判断概率；怪物死亡／部位破坏时掉落。新开战使用新配置，已有战斗保留开战时的掉落表。首通、金币和经验奖励保持原配置。',
  ],
  tower: [
    '选择炼金塔楼层',
    '各楼层的修改分别保留，可以一起保存。',
    '每行独立判断概率；怪物死亡／部位破坏时掉落。之后开始的挑战使用新配置；金币和经验奖励保持原配置，首通奖励在下方单独设置。',
  ],
};

function showDropCatalogControls() {
  const tower = dropEditor.kind === 'tower';
  $$('#drop-catalogs button').forEach((b) => {
    const on = (b.dataset.catalog === 'tower') === tower;
    b.classList.toggle('active', on);
    b.setAttribute('aria-pressed', String(on));
  });
  $('#drop-catalogs [data-catalog="tower"]').hidden = !dropEditor.towers?.length;
  $('#drop-boss-pick').hidden = tower;
  $('#drop-tower-pick').hidden = !tower;
  $('#drops').classList.toggle('tower-mode', tower);
  const [title, hint] = DROP_HINTS[tower ? 'tower' : 'boss'];
  $('#drop-pick-title').textContent = title;
  $('#drop-pick-hint').textContent = hint;
}

/** Switches between the ordinary Boss catalog and the towers; unsaved edits are only dropped on a yes. */
async function setDropCatalog(tower, towerID = 0) {
  if (tower === (dropEditor.kind === 'tower')) {
    if (tower && towerID && towerID !== towerPick.towerID) {
      await contentAction('drops', () => openTowerFloor(towerID, towerPick.lastFloor[towerID]), '载入');
    }
    return;
  }
  const pending = dropEditor.drafts.size + (dropEditor.dirty ? 1 : 0);
  const question = dropIsTower()
    ? `炼金塔有 ${pending} 层未保存的修改，切换到 BOSS 副本会放弃这些修改。`
    : '当前难度有未保存的修改，切换到炼金塔会放弃这些修改。';
  if (!(await confirmDiscard(pending > 0, question, '放弃并切换'))) return;
  const kind = dropEditor.kind;
  dropEditor.drafts.clear();
  dropEditor.dirty = false;
  if (tower) {
    towerPick.lastKind = kind;
    dropEditor.kind = 'tower';
    if (towerID) towerPick.towerID = towerID;
  } else dropEditor.kind = towerPick.lastKind;
  filterDropSelect();
  await contentAction(
    'drops',
    async () => {
      if (tower) {
        const id = currentTower()?.tower_id;
        await openTowerFloor(id, towerPick.lastFloor[id]);
      } else {
        await selectDrop(dropEditor.lastBoss || Number($('#drop-select option:not([value=""])')?.value));
      }
    },
    '载入',
  );
  // A failed load leaves the previous editor; put the catalog back so the two never disagree.
  if (dropIsTower() !== (dropEditor.kind === 'tower')) {
    dropEditor.kind = dropIsTower() ? 'tower' : towerPick.lastKind;
    filterDropSelect();
  }
}

/** Opened from Boss 发布 › 炼金塔 › 掉落与首通. */
async function openTowerDrops(towerID) {
  await switchView('drops');
  await setDropCatalog(true, towerID);
}

// Tower and floor picker ---------------------------------------------------------------------------
// Floors are listed in runs of the same boss, so each boss name shows in full once. Floors missing from
// the catalog are story floors (the server lists battle floors only): they have no drops.

const towerFloorCount = (tower) => Math.max(0, ...tower.floors.map((f) => f.floor));
const floorSpan = (first, last) => (first === last ? `第 ${first} 层` : `第 ${first}–${last} 层`);

function floorRuns(tower) {
  const byFloor = new Map(tower.floors.map((f) => [f.floor, f]));
  const runs = [];
  for (let n = 1; n <= towerFloorCount(tower); n++) {
    const row = byFloor.get(n);
    const last = runs.at(-1);
    if (!row) {
      if (last?.story) last.floors.push(n);
      else runs.push({ story: true, floors: [n] });
    } else if (last && !last.story && last.boss === towerFloorBoss(row)) last.floors.push(row);
    else runs.push({ boss: towerFloorBoss(row), floors: [row] });
  }
  return runs;
}

function floorRunHTML(run) {
  if (run.story) {
    return html`<div class="floor-run story">
      <b>${floorSpan(run.floors[0], run.floors.at(-1))}</b><span>剧情层，没有掉落</span>
    </div>`;
  }
  const chip = (f) => {
    const title = `第 ${f.floor} 层 · ${run.boss} · BOSS ${f.boss_id}`;
    return html`<button type="button" class="floor-chip" data-floor="${f.floor}" data-title="${title}"
      aria-label="${title}">${f.floor}</button>`;
  };
  return html`<section class="floor-run" data-run>
    <div class="floor-run-head">
      <b>${run.boss}</b><span>${floorSpan(run.floors[0].floor, run.floors.at(-1).floor)}</span>
    </div>
    <div class="floor-chips">${run.floors.map(chip)}</div>
  </section>`;
}

/** Builds the tower options and the floor list when they change; later calls only repaint states. */
function renderTowerPicker() {
  const tower = currentTower();
  const select = $('#drop-tower-select');
  const box = $('#drop-floors');
  if (towerPick.built.towers !== dropEditor.towers) {
    towerPick.built = { towers: dropEditor.towers, tower: 0 };
    setHTML(
      select,
      dropEditor.towers.map((t) => html`<option value="${t.tower_id}">${t.name}</option>`),
    );
  }
  select.disabled = !tower;
  if (!tower) {
    setHTML(box, html`<div class="empty"><b>当前资源没有炼金塔</b>服务端未提供炼金塔楼层。</div>`);
    $('#drop-tower-summary').textContent = '';
    return;
  }
  towerPick.towerID = tower.tower_id;
  if (towerPick.built.tower !== tower.tower_id) {
    towerPick.built.tower = tower.tower_id;
    setHTML(
      box,
      tower.floors.length
        ? floorRuns(tower).map(floorRunHTML)
        : html`<div class="empty"><b>这座塔没有战斗楼层</b>剧情层不配置掉落。</div>`,
    );
    box.scrollTop = 0;
  }
  paintTowerPicker(tower);
}

// Repainting in place matters: an edit's change event fires on blur, i.e. during the click on a floor,
// and replacing the buttons at that moment would swallow the click.
function paintTowerPicker(tower = currentTower()) {
  if (!tower) return;
  const drafts = towerDrafts();
  const draftCount = (id) => drafts.filter((d) => d.row.tower_id === id).length;
  const pending = new Set(drafts.map((d) => towerKey(d.row)));
  const active = dropIsTower() ? towerKey(dropEditor.row) : '';
  const select = $('#drop-tower-select');
  [...select.options].forEach((option) => {
    const t = dropEditor.towers.find((x) => x.tower_id === Number(option.value));
    const count = draftCount(t.tower_id);
    option.textContent = `${t.name} · ${towerFloorCount(t)} 层${count ? ` · ${count} 层未保存` : ''}`;
  });
  select.value = tower.tower_id;
  $$('#drop-floors [data-floor]').forEach((chip) => {
    const key = `${tower.tower_id}:${chip.dataset.floor}`;
    const draft = pending.has(key);
    const changed = !draft && !!towerPick.info.get(key)?.changed;
    chip.classList.toggle('active', key === active);
    chip.classList.toggle('draft', draft);
    chip.classList.toggle('custom', changed);
    chip.setAttribute('aria-pressed', String(key === active));
    chip.title = chip.dataset.title + (draft ? ' · 未保存' : changed ? ' · 已调整' : '');
  });
  $$('#drop-floors [data-run]').forEach((run) =>
    run.classList.toggle('current', !!run.querySelector('.active')),
  );
  const changed = tower.floors.filter((f) => towerPick.info.get(towerKey(f))?.changed).length;
  $('#drop-tower-summary').textContent =
    `塔 ID ${tower.tower_id} · 共 ${towerFloorCount(tower)} 层，战斗 ${tower.floors.length} 层` +
    (changed ? ` · 已调整 ${changed} 层` : '');
  const others = dropEditor.towers
    .filter((t) => t.tower_id !== tower.tower_id && draftCount(t.tower_id))
    .map((t) => `${t.name} ${draftCount(t.tower_id)} 层`);
  $('#drop-tower-others').hidden = !others.length;
  $('#drop-tower-others').textContent = others.length ? `其他塔未保存：${others.join('、')}` : '';
}

/** Scrolls the floor list, not the page, so the open floor's run is in view. */
function revealActiveFloor() {
  const box = $('#drop-floors');
  const run = box.querySelector('.floor-chip.active')?.closest('.floor-run');
  if (!run || box.scrollHeight <= box.clientHeight) return;
  const view = box.getBoundingClientRect();
  const rect = run.getBoundingClientRect();
  if (rect.top < view.top) box.scrollTop += rect.top - view.top;
  else if (rect.bottom > view.bottom)
    box.scrollTop += Math.min(rect.bottom - view.bottom, rect.top - view.top);
}

/** Opens a floor; the floor being left keeps its unsaved edits as a draft. reload re-reads an open floor. */
async function openTowerFloor(towerID, floor, reload = false) {
  const tower = dropEditor.towers.find((t) => t.tower_id === towerID) || dropEditor.towers[0];
  const row = tower?.floors.find((f) => f.floor === floor) || tower?.floors[0];
  const key = row && towerKey(row);
  const open = dropIsTower() && towerKey(dropEditor.row) === key;
  if (!row || (open && !reload)) {
    towerPick.towerID = tower?.tower_id || 0;
    renderTowerPicker();
    return;
  }
  const draft = dropEditor.drafts.get(key);
  // Fetch first, so a failed request leaves the open floor (and its edits) exactly as it was.
  const data = draft ? null : await api(`/api/tower-drops?tower_id=${row.tower_id}&floor=${row.floor}`);
  towerPick.towerID = tower.tower_id;
  if (dropIsTower() && dropEditor.dirty && !open)
    dropEditor.drafts.set(towerKey(dropEditor.row), draftSnapshot());
  if (draft) {
    dropEditor.drafts.delete(key);
    Object.assign(dropEditor, draft);
    dropEditor.dirty = true;
    renderDropEditor();
  } else {
    towerPick.info.set(key, { changed: towerFloorChanged(data.config, data.base) });
    await applyDropData(data);
  }
  towerPick.lastFloor[row.tower_id] = row.floor;
  towerSaveState();
  revealActiveFloor();
  if (!tower.floors.every((f) => towerPick.info.has(towerKey(f)))) refreshTowerInfo(tower);
}

/** Reads every floor of a tower once, only to mark which floors differ from the package. */
async function refreshTowerInfo(tower) {
  const serial = ++towerPick.serial;
  try {
    await mapLimited(tower.floors, 4, async (f) => {
      const data = await api(`/api/tower-drops?tower_id=${f.tower_id}&floor=${f.floor}`);
      towerPick.info.set(towerKey(f), { changed: towerFloorChanged(data.config, data.base) });
    });
  } catch {
    /* The floor marks are a convenience; editing does not depend on them. */
  }
  if (serial === towerPick.serial && dropEditor.kind === 'tower') paintTowerPicker();
}

/** Summary, save button and floors follow the number of floors with unsaved edits. */
function towerSaveState() {
  const drafts = towerDrafts();
  $('#drop-save').textContent = drafts.length > 1 ? `保存 ${drafts.length} 层修改` : '保存掉落配置';
  if (drafts.length) {
    const names = drafts.map((d) => towerDraftName(d.row)).join('、');
    setSaveState('#drop-summary', `${names} 有未保存的修改`, 'dirty');
  }
  renderTowerPicker();
}

/** The editing strip: exact tower, floor, boss and boss ID, plus steps to the neighbouring battle floors. */
function towerEditingHTML(row) {
  const floors = dropEditor.towers.find((t) => t.tower_id === row.tower_id)?.floors || [];
  const at = floors.findIndex((f) => f.floor === row.floor);
  const step = (f, text) =>
    f
      ? html`<button class="secondary sm" type="button" data-floor-step="${f.floor}" title="${f.difficulty}">
          ${text}
        </button>`
      : '';
  return html`<span>正在编辑：${row.name} · 第 ${row.floor} 层 · ${towerFloorBoss(row)}</span>
    <code>BOSS ${row.boss_id}</code>
    <span class="hint">塔 ID ${row.tower_id} · ${row.targets.length} 个掉落目标</span>
    <span class="floor-step">
      ${step(floors[at - 1], `‹ 第 ${floors[at - 1]?.floor} 层`)}
      ${step(floors[at + 1], `第 ${floors[at + 1]?.floor} 层 ›`)}
    </span>`;
}

$('#drop-catalogs').addEventListener('click', (event) => {
  const button = event.target.closest('[data-catalog]');
  if (button) setDropCatalog(button.dataset.catalog === 'tower');
});
$('#drop-tower-select').addEventListener('change', (event) => {
  const id = Number(event.target.value);
  if (id === towerPick.towerID) return;
  // A refused or failed load keeps the open tower; the repaint puts the select back on it.
  contentAction('drops', () => openTowerFloor(id, towerPick.lastFloor[id]), '载入').finally(() =>
    paintTowerPicker(),
  );
});
$('#drop-floors').addEventListener('click', (event) => {
  const chip = event.target.closest('[data-floor]');
  if (chip)
    contentAction('drops', () => openTowerFloor(towerPick.towerID, Number(chip.dataset.floor)), '载入');
});
$('#drop-current').addEventListener('click', (event) => {
  const button = event.target.closest('[data-floor-step]');
  if (button && dropIsTower())
    contentAction(
      'drops',
      () => openTowerFloor(dropEditor.row.tower_id, Number(button.dataset.floorStep)),
      '载入',
    );
});

// First-clear rewards --------------------------------------------------------------------------------
// null inherits the packaged rewards, [] gives nothing, a list replaces them. Star ratings stay a
// record only: this page has no star or ranking rewards.

async function resolveTowerFirstClearLabels() {
  if (!dropIsTower()) return;
  const rewards = [
    ...(dropEditor.config.first_clear_rewards || []),
    ...(dropEditor.base.first_clear_rewards || []),
  ];
  await resolveContentLabels(rewards.filter((r) => !activityCurrency(r)));
  await resolveCatalogEntries(rewards.filter(activityCurrency).map((r) => [r.type, r.reward_typeid])).catch(
    () => {},
  );
}

function firstClearRowHTML(reward, index, editable) {
  return html`<tr>
    <td>
      <div class="reward-cell">
        ${raw(contentThumb(reward))}
        <div>
          <b>${raw(contentName(reward, activityRewardLabel(reward)))}</b>
          <span class="sub">${activityCurrency(reward) ? '货币' : `ID ${reward.reward_typeid}`}</span>
        </div>
      </div>
    </td>
    <td>
      ${
        editable
          ? html`<input type="number" min="1" max="${rewardMax(reward.type)}" step="1" value="${reward.num}"
            data-tower-first-num="${index}" aria-label="首通奖励数量" />`
          : html`${num(reward.num)}<span class="tag">包内默认</span>`
      }
    </td>
    <td>
      ${
        editable
          ? html`<button class="x-button" data-tower-first-remove="${index}" aria-label="移除${activityRewardLabel(reward)}">
            <svg class="icon"><use href="#i-close" /></svg>
          </button>`
          : ''
      }
    </td>
  </tr>`;
}

function renderTowerFirstClear() {
  const tower = dropIsTower();
  $('#tower-first-panel').hidden = !tower;
  $('#drop-bulk-copy').hidden = tower;
  $('#drop-copy').textContent = tower ? '从其他楼层复制' : '复制同组难度';
  $('#drop-panel-hint').textContent = DROP_HINTS[tower ? 'tower' : 'boss'][2];
  if (!tower) return;
  const mode = dropEditor.modes.first;
  $$('#tower-first-mode input').forEach((el) => (el.checked = el.value === mode));
  $('#tower-first-add').hidden = mode !== 'custom';
  const base = dropEditor.base.first_clear_rewards || [];
  const custom = dropEditor.config.first_clear_rewards || [];
  let rows;
  if (mode === 'off') rows = raw(emptyRow(3, '此楼层首次通关不发奖励', '怪物掉落和名声奖励照常结算。'));
  else if (mode === 'default') {
    rows = base.length
      ? base.map((r, i) => firstClearRowHTML(r, i, false))
      : raw(emptyRow(3, '包内没有首通奖励', '选“自定义”可以为此楼层添加。'));
  } else {
    rows = custom.length
      ? custom.map((r, i) => firstClearRowHTML(r, i, true))
      : raw(emptyRow(3, '还没有自定义奖励', '点击“添加首通奖励”；不想发放请选“不发奖励”。'));
  }
  setHTML($('#tower-first-rows'), rows);
}

$('#tower-first-mode').addEventListener('change', (event) => {
  if (!dropIsTower()) return;
  setPoolMode('first', event.target.value, () => structuredClone(dropEditor.base.first_clear_rewards || []));
  resolveTowerFirstClearLabels()
    .catch(() => {})
    .then(renderTowerFirstClear);
});
$('#tower-first-rows').addEventListener('input', (event) => {
  const index = event.target.dataset.towerFirstNum;
  if (index == null) return;
  dropEditor.config.first_clear_rewards[Number(index)].num = Number(event.target.value);
  event.target.classList.remove('invalid');
  changedContent('drops');
});
$('#tower-first-rows').addEventListener('click', (event) => {
  const button = event.target.closest('[data-tower-first-remove]');
  if (!button) return;
  dropEditor.config.first_clear_rewards.splice(Number(button.dataset.towerFirstRemove), 1);
  changedContent('drops');
  renderTowerFirstClear();
});
$('#tower-first-add').onclick = () =>
  openContentPicker(
    (rows) => {
      const pool = dropEditor.config.first_clear_rewards;
      if (!Array.isArray(pool)) throw new Error('请先选择“自定义”');
      if (pool.length + rows.length > 120) throw new Error('每层首通奖励最多120项');
      rememberCatalogRows(rows);
      pool.push(...rows.map(contentReward));
      changedContent('drops');
      renderTowerFirstClear();
    },
    ['currency', 'card', 'material', 'item', 'sphere', 'buddy', 'costume', 'stamp', 'honor'],
    {
      accept: (c) =>
        c.kind !== 'currency' || [4, 10, 12].includes(c.reward_type) || '首通货币支持金币、水晶和体力',
      title: '选择楼层首通奖励',
    },
  );

// Import, review, checks and save --------------------------------------------------------------------

function towerDropImportExtras(config) {
  if (!dropIsTower()) return {};
  const row = dropEditor.row;
  if (config.tower_id !== row.tower_id || config.floor !== row.floor || config.boss_id !== row.boss_id) {
    throw new Error(`方案属于其他楼层：当前为塔 ${row.tower_id} 第 ${row.floor} 层（BOSS ${row.boss_id}）`);
  }
  if (config.first_clear_rewards != null && !Array.isArray(config.first_clear_rewards)) {
    throw new Error('首通奖励须为数组');
  }
  return {
    tower_id: config.tower_id,
    floor: config.floor,
    first_clear_rewards: structuredClone(config.first_clear_rewards ?? null),
  };
}

const firstClearModeText = (list) =>
  list == null ? '沿用包内默认' : list.length ? `自定义 ${list.length} 项` : '不发奖励';

function towerFirstClearDiff() {
  if (!dropIsTower()) return [];
  const before = dropEditor.saved.first_clear_rewards;
  const after = dropEditor.config.first_clear_rewards;
  const base = dropEditor.base.first_clear_rewards || [];
  const text = (r) => `${activityRewardLabel(r)} ×${r.num}`;
  return [
    {
      title: '楼层首通奖励',
      rows: [
        ...diffRows([['首通奖励', firstClearModeText(before), firstClearModeText(after)]]),
        ...keyedDiff('首通奖励', before ?? base, after ?? base, contentKey, text, activityRewardLabel),
      ],
    },
  ];
}

function validateTowerFirstClear(problems, mark = true) {
  if (!dropIsTower()) return;
  const list = dropEditor.config.first_clear_rewards;
  if (dropEditor.modes.first === 'custom' && !list?.length) {
    problems.push('首通奖励选了“自定义”但还没有添加奖励：请添加，或改选“不发奖励”');
  }
  (list || []).forEach((r, i) => {
    if (Number.isInteger(r.num) && r.num >= 1 && r.num <= rewardMax(r.type)) return;
    problems.push(`首通奖励「${activityRewardLabel(r)}」数量须为1–${rewardMax(r.type)}`);
    if (mark) markInvalid($(`[data-tower-first-num="${i}"]`));
  });
}

/** One review and one request for every floor with unsaved edits. */
function saveTowerDrops() {
  return reviewedSave(
    'drops',
    () => {
      const drafts = towerDrafts();
      if (!drafts.length) {
        toast('没有未保存的修改');
        return null;
      }
      const many = drafts.length > 1;
      const problems = drafts.flatMap((d) => {
        const found = d.row === dropEditor.row ? dropProblems() : withDraft(d, () => dropProblems(false));
        return many ? found.map((p) => `${towerDraftName(d.row)}：${p}`) : found;
      });
      if (problems.length) {
        showViewAlert('drops', {
          title: `不能保存：${problems.length} 处需要修正`,
          message: problems.slice(0, 6).join('；'),
        });
        return null;
      }
      const sections = drafts.flatMap((d) =>
        withDraft(d, dropDiff).map((s) => ({
          ...s,
          title: many ? `${towerDraftName(d.row)} · ${towerFloorBoss(d.row)} · ${s.title}` : s.title,
        })),
      );
      const first = drafts[0].row;
      return {
        drafts,
        review: {
          title: many
            ? `核对炼金塔掉落 · ${drafts.length} 层`
            : `核对掉落变更 · ${first.name} 第 ${first.floor} 层 · ${towerFloorBoss(first)}`,
          lead: many ? `将一起保存：${drafts.map((d) => towerDraftName(d.row)).join('、')}。` : '',
          sections,
          note: '之后开始的挑战使用新配置，已开始的战斗保留原掉落；已领过首通奖励的玩家不会再领，通关和评级记录不变。',
        },
      };
    },
    async ({ drafts }) => {
      const data = await api('/api/tower-drops', {
        method: 'PUT',
        body: JSON.stringify({
          expected_revision: Math.min(...drafts.map((d) => d.revision)),
          configs: drafts.map((d) => ({
            ...d.config,
            boss_id: d.row.boss_id,
            tower_id: d.row.tower_id,
            floor: d.row.floor,
          })),
        }),
      });
      for (const d of drafts)
        towerPick.info.set(towerKey(d.row), { changed: towerFloorChanged(d.config, d.base) });
      dropEditor.drafts.clear();
      dropEditor.revision = data.revision;
      dropEditor.saved = structuredClone(dropEditor.config);
      dropEditor.dirty = false;
      $('#drop-version').textContent = `配置 v${data.revision}`;
      setSaveState('#drop-summary', `已保存 ${drafts.length} 层；掉落展示和结算使用同一配置`, 'ok');
      towerSaveState();
      clearViewAlert('drops');
      state.loaded.delete('audit');
      toast(drafts.length > 1 ? `已保存 ${drafts.length} 层掉落配置` : '掉落配置已保存');
    },
  );
}
