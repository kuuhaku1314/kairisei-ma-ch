'use strict';
// 卡池配置 › 随机附赠: groups of extra rewards per draw method (config.random_gifts). After each successful
// draw every group whose play window covers the variant's play count rolls once: at most one reward by
// weight, or nothing when the empty weight wins. Fixed gifts of built-in pools stay as they are.

const POOL_GIFT_GROUPS = 10;
const POOL_GIFT_REWARDS = 100;
const POOL_GIFT_MAX = 1000000;

const poolGiftInt = (value, min, max) => Number.isInteger(value) && value >= min && value <= max;

/** Editable copy of a saved random_gifts list; absent to_play / empty_weight read as 0. */
function poolGiftState(gifts) {
  return structuredClone(gifts || []).map((g) => ({
    from_play: g.from_play,
    to_play: g.to_play || 0,
    empty_weight: g.empty_weight || 0,
    pool: g.pool || [],
  }));
}

/** Imported files are checked for shape only; ranges and weights go through poolGiftProblems() on save. */
function weightedPoolShaped(pool) {
  return (
    Array.isArray(pool) &&
    pool.every(
      (e) =>
        e &&
        e.reward &&
        typeof e.reward === 'object' &&
        !Array.isArray(e.reward) &&
        Number.isInteger(e.weight),
    )
  );
}

function poolGiftsShaped(gifts) {
  return Array.isArray(gifts) && gifts.every((g) => g && typeof g === 'object' && weightedPoolShaped(g.pool));
}

/** The saved form: zero to_play and empty_weight stay out, like the server's omitempty fields. */
function poolGiftConfig(id) {
  const gifts = poolEditor.randomGifts?.get(id) || [];
  if (!gifts.length) return {};
  return {
    random_gifts: gifts.map((g) => ({
      from_play: g.from_play,
      ...(g.to_play !== 0 && { to_play: g.to_play }),
      pool: g.pool.map((e) => ({ reward: { ...e.reward }, weight: e.weight })),
      ...(g.empty_weight !== 0 && { empty_weight: g.empty_weight }),
    })),
  };
}

/** Save checks matching ValidateGachaRandomGifts(); fields of the shown variant are marked in place. */
function poolGiftProblems(configs, label) {
  const problems = [];
  for (const c of configs) {
    const gifts = c.random_gifts || [];
    const field = (i, key) =>
      c.gacha_id === poolEditor.activeID
        ? $(`#pool-gifts-body [data-gift="${i}"][data-gift-field="${key}"]`)
        : null;
    const bad = (el, text) => {
      problems.push(text);
      markInvalid(el, text);
    };
    if (gifts.length > POOL_GIFT_GROUPS) problems.push(`${label(c)}随机附赠最多 ${POOL_GIFT_GROUPS} 组`);
    gifts.forEach((g, i) => {
      const at = `${label(c)}随机附赠第 ${i + 1} 组`;
      const to = g.to_play || 0;
      if (!poolGiftInt(g.from_play, 1, POOL_GIFT_MAX)) {
        bad(field(i, 'from_play'), `${at}：起始次数须为 1–1000000`);
      } else if (to !== 0 && !poolGiftInt(to, g.from_play, POOL_GIFT_MAX)) {
        bad(field(i, 'to_play'), `${at}：结束次数须不小于起始次数、不超过 1000000；留空表示不设上限`);
      }
      if (!poolGiftInt(g.empty_weight || 0, 0, POOL_GIFT_MAX))
        bad(field(i, 'empty_weight'), `${at}：空奖权重须为 0–1000000`);
      if (!g.pool.length) problems.push(`${at}：至少添加一项奖励`);
      if (g.pool.length > POOL_GIFT_REWARDS) problems.push(`${at}：最多 ${POOL_GIFT_REWARDS} 项奖励`);
      if (g.pool.some((e) => !poolGiftInt(e.weight, 1, POOL_GIFT_MAX)))
        problems.push(`${at}：奖励权重须为 1–1000000`);
      if (g.pool.some((e) => !poolGiftInt(e.reward.num, 1, rewardMax(e.reward.type))))
        problems.push(`${at}：奖励数量超出允许范围`);
      if (g.pool.some((e) => e.reward.type === 8 && e.reward.num !== 1))
        problems.push(`${at}：道具每项数量须为 1`);
    });
  }
  return problems;
}

// Rendering ----------------------------------------------------------------------------------------

const poolGifts = () => poolEditor.randomGifts?.get(poolEditor.activeID) || [];
const poolGiftWeights = (g) => poolWeightSum(g.pool) + (Number(g.empty_weight) || 0);

function poolGiftRangeText(g) {
  const from = Number(g.from_play) || 0;
  const to = Number(g.to_play) || 0;
  if (from < 1) return '请填写起始次数';
  const start = from === 1 ? '从第 1 次抽取起' : `从第 ${num(from)} 次抽取起`;
  return to ? `${start}，到第 ${num(to)} 次为止` : `${start}一直判定`;
}

function poolGiftChanceText(g) {
  const sum = poolGiftWeights(g);
  if (!g.pool.length || !sum) return '还没有奖励';
  const hit = poolWeightSum(g.pool) / sum;
  return `每次判定附赠概率 ${(hit * 100).toFixed(2)}%${g.empty_weight ? `，空奖 ${((1 - hit) * 100).toFixed(2)}%` : ''}`;
}

function renderPoolGifts() {
  const panel = $('#pool-gifts-panel');
  const row = poolActiveRow();
  panel.hidden = !row || !poolEditor.randomGifts;
  if (panel.hidden) return;
  const gifts = poolGifts();
  const locked = !!row.deleted;
  $('#pool-gifts-count').textContent = `${gifts.length} / ${POOL_GIFT_GROUPS} 组`;
  $('#pool-gifts-add').disabled = locked || gifts.length >= POOL_GIFT_GROUPS;
  $('#pool-gifts-copy').hidden = poolEditor.variants.length < 2;
  $('#pool-gifts-copy').disabled = locked;
  // The reward table of mixed pools already lists the fixed gifts.
  const fixed = poolEditor.mixed ? [] : row.base.gift_rules || [];
  setHTML(
    $('#pool-gifts-body'),
    html`
      ${
        fixed.length > 0 &&
        html`<p class="pool-gifts-fixed">
          <b>包内固定赠礼（照常发放）</b>
          ${fixed.map(
            (g) =>
              html`<span>
                第 ${g.from_play} 至 ${g.to_play || '以后'} 次：${g.rewards
                  .map((r) => `${rewardLabel(r)} ×${r.num}`)
                  .join('、')}
              </span>`,
          )}
        </p>`
      }
      <fieldset class="pool-gifts-list" ${attr(locked, 'disabled')}>
        ${
          gifts.length
            ? gifts.map(poolGiftGroupHTML)
            : html`<div class="pool-gifts-empty">
                <b>本分池没有随机附赠</b>
                <span>点「添加一组」，设定抽取次数区间，再添加候选奖励和权重。</span>
              </div>`
        }
      </fieldset>
    `,
  );
}

function poolGiftGroupHTML(g, i) {
  const sum = poolGiftWeights(g);
  return html`
    <section class="gift-group" aria-label="随机附赠第 ${i + 1} 组">
      <header class="gift-group-head">
        <b class="gift-group-title">第 ${i + 1} 组</b>
        <span class="gift-range">
          <label>
            第
            <input type="number" min="1" max="${POOL_GIFT_MAX}" value="${g.from_play || ''}"
              data-gift="${i}" data-gift-field="from_play" aria-label="第 ${i + 1} 组起始次数" />
            次起
          </label>
          <label>
            至第
            <input type="number" min="1" max="${POOL_GIFT_MAX}" value="${g.to_play || ''}" placeholder="不限"
              data-gift="${i}" data-gift-field="to_play" aria-label="第 ${i + 1} 组结束次数（留空不限）" />
            次
          </label>
        </span>
        <label class="gift-empty">
          空奖权重
          <input type="number" min="0" max="${POOL_GIFT_MAX}" value="${g.empty_weight}"
            data-gift="${i}" data-gift-field="empty_weight" aria-label="第 ${i + 1} 组空奖权重" />
        </label>
        <span class="spacer"></span>
        <button type="button" class="ghost danger-text" data-gift-remove="${i}">删除本组</button>
      </header>
      <p class="gift-group-facts">
        <span data-gift-range="${i}">${poolGiftRangeText(g)}</span>
        <span data-gift-chance="${i}">${poolGiftChanceText(g)}</span>
      </p>
      ${
        g.pool.length > 0 &&
        html`<div class="table-wrap">
          <table class="dense pool-mixed-table gift-table">
            <thead>
              <tr>
                <th>奖励</th>
                <th>数量</th>
                <th>权重</th>
                <th>概率</th>
                <th><span class="sr-only">操作</span></th>
              </tr>
            </thead>
            <tbody>
              ${g.pool.map((e, j) => poolGiftRowHTML(e, i, j, sum))}
            </tbody>
          </table>
        </div>`
      }
      <footer class="gift-group-foot">
        <button type="button" class="secondary sm" data-gift-add="${i}"
          ${attr(g.pool.length >= POOL_GIFT_REWARDS, 'disabled')}>
          <svg class="icon"><use href="#i-plus" /></svg>添加奖励
        </button>
        <span class="hint">${g.pool.length} / ${POOL_GIFT_REWARDS} 项</span>
      </footer>
    </section>
  `;
}

function poolGiftRowHTML(e, i, j, sum) {
  const entry = poolCatalogEntry(e.reward);
  const label = rewardLabel(e.reward);
  const share = sum ? (Number(e.weight) / sum) * 100 : 0;
  return html`
    <tr>
      <td>
        <span class="mixed-reward">
          <span class="thumb">${raw(image(entry?.image_url, label))}</span>
          <span>
            <b class="pool-card-name">${label}</b>
            <span class="sub">
              ${pickerKindNames[entry?.kind] || `类型 ${e.reward.type}`}
              ${e.reward.reward_typeid ? ` · ${e.reward.reward_typeid}` : ''}
            </span>
          </span>
        </span>
      </td>
      <td>
        ${
          e.reward.type === 8
            ? html`<span class="hint" title="直接附赠的道具每项数量固定为 1">×1</span>`
            : html`<input type="number" min="1" max="${rewardMax(e.reward.type)}" value="${e.reward.num}"
                data-gift="${i}" data-gift-num="${j}" aria-label="${label}数量" />`
        }
      </td>
      <td>
        <input type="number" min="1" max="${POOL_GIFT_MAX}" value="${e.weight}" data-gift="${i}"
          data-gift-weight="${j}" aria-label="${label}权重" />
      </td>
      <td class="share-cell">
        <span class="share-bar"><i data-gift-bar="${i}:${j}" style="width:${share.toFixed(2)}%"></i></span>
        <span data-gift-share="${i}:${j}">${poolShareText(e.weight, sum)}</span>
      </td>
      <td>
        <button type="button" class="x-button" data-gift="${i}" data-gift-drop="${j}" aria-label="移除${label}">
          <svg class="icon"><use href="#i-close" /></svg>
        </button>
      </td>
    </tr>
  `;
}

/** Number edits refresh only the group's texts and shares, so the input keeps focus. */
function refreshPoolGiftGroup(i) {
  const g = poolGifts()[i];
  if (!g) return;
  const sum = poolGiftWeights(g);
  $(`#pool-gifts-body [data-gift-range="${i}"]`).textContent = poolGiftRangeText(g);
  $(`#pool-gifts-body [data-gift-chance="${i}"]`).textContent = poolGiftChanceText(g);
  g.pool.forEach((e, j) => {
    const weight = Number(e.weight) || 0;
    $(`#pool-gifts-body [data-gift-share="${i}:${j}"]`).textContent = poolShareText(weight, sum);
    $(`#pool-gifts-body [data-gift-bar="${i}:${j}"]`).style.width =
      `${sum ? ((weight / sum) * 100).toFixed(2) : 0}%`;
  });
}

// Editing ------------------------------------------------------------------------------------------

function poolGiftsChanged({ render = true } = {}) {
  poolChanged();
  if (render) renderPoolGifts();
}

function addPoolGiftRewards(i) {
  const group = poolGifts()[i];
  openContentPicker(
    (rows) => {
      if (group.pool.length + rows.length > POOL_GIFT_REWARDS)
        throw new Error(
          `每组最多 ${POOL_GIFT_REWARDS} 项奖励，本组还可添加 ${POOL_GIFT_REWARDS - group.pool.length} 项`,
        );
      rememberCatalogRows(rows);
      let added = 0;
      for (const row of rows) {
        const reward = contentReward(row);
        if (
          group.pool.some(
            (e) => e.reward.type === reward.type && e.reward.reward_typeid === reward.reward_typeid,
          )
        )
          continue;
        group.pool.push({ reward, weight: 1 });
        added++;
      }
      poolGiftsChanged();
      if (added < rows.length) toast(`已添加 ${added} 项；${rows.length - added} 项已在本组中，未重复添加`);
    },
    ['currency', 'card', 'material', 'item', 'sphere', 'buddy'],
    {
      title: `添加随机附赠 · 第 ${i + 1} 组`,
      accept: (entry) => POOL_MIXED_REWARD_TYPES.has(entry.reward_type) || '随机附赠不支持此类型',
    },
  );
}

async function removePoolGiftGroup(i) {
  const gifts = poolGifts();
  if (
    gifts[i].pool.length &&
    !(await askConfirm(
      `删除第 ${i + 1} 组随机附赠？`,
      `本组的 ${gifts[i].pool.length} 项奖励一起删除，保存草稿后生效。`,
      {
        ok: '删除',
        danger: true,
      },
    ))
  )
    return;
  gifts.splice(i, 1);
  poolGiftsChanged();
}

async function copyPoolGifts() {
  const gifts = poolGifts();
  const others = poolEditor.variants.filter((v) => v.gacha_id !== poolEditor.activeID);
  const replaced = others.filter((v) => poolEditor.randomGifts.get(v.gacha_id)?.length).length;
  if (
    !(await askConfirm(
      gifts.length
        ? `把本分池的 ${gifts.length} 组随机附赠应用到其他 ${others.length} 个分池？`
        : `清空其他 ${others.length} 个分池的随机附赠？`,
      `${replaced ? `其中 ${replaced} 个分池已有随机附赠，会被替换。` : ''}抽取次数仍按各分池分别累计；保存草稿后生效。`,
      { ok: gifts.length ? '应用' : '清空', danger: !gifts.length },
    ))
  )
    return;
  for (const v of others) poolEditor.randomGifts.set(v.gacha_id, structuredClone(gifts));
  poolChanged();
  renderStrategies();
  toast(gifts.length ? `已应用到 ${others.length} 个分池草稿` : `已清空 ${others.length} 个分池的随机附赠`);
}

$('#pool-gifts-panel').addEventListener('input', (event) => {
  const el = event.target;
  const group = poolGifts()[Number(el.dataset.gift)];
  if (!group) return;
  const value = el.value === '' ? 0 : Number(el.value);
  if (el.dataset.giftField) group[el.dataset.giftField] = value;
  else if (el.dataset.giftWeight != null) group.pool[Number(el.dataset.giftWeight)].weight = value;
  else if (el.dataset.giftNum != null) group.pool[Number(el.dataset.giftNum)].reward.num = value;
  else return;
  el.classList.remove('invalid');
  poolGiftsChanged({ render: false });
  refreshPoolGiftGroup(Number(el.dataset.gift));
});

$('#pool-gifts-panel').addEventListener('click', (event) => {
  const pick = (selector) => event.target.closest(selector);
  let el;
  if (policyBusy('pool-editor')) return;
  if (pick('#pool-gifts-add')) {
    const gifts = poolGifts();
    if (gifts.length >= POOL_GIFT_GROUPS) return;
    gifts.push({ from_play: 1, to_play: 0, empty_weight: 0, pool: [] });
    poolGiftsChanged();
    $(`#pool-gifts-body [data-gift-add="${gifts.length - 1}"]`)?.focus();
  } else if (pick('#pool-gifts-copy')) copyPoolGifts();
  else if ((el = pick('[data-gift-add]'))) addPoolGiftRewards(Number(el.dataset.giftAdd));
  else if ((el = pick('[data-gift-remove]'))) removePoolGiftGroup(Number(el.dataset.giftRemove));
  else if ((el = pick('[data-gift-drop]'))) {
    poolGifts()[Number(el.dataset.gift)].pool.splice(Number(el.dataset.giftDrop), 1);
    poolGiftsChanged();
  }
});
