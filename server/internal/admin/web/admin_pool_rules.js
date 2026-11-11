'use strict';
// Pool rules: the creation dialog (rule types from GET /api/gacha-editor rule_types, or a copy of an
// existing card pool) and the reward table editor for mixed and staged variants.
// Each pool's type comes from its own `rule`; built-in pools without one keep their original profile.

const POOL_PROFESSIONS = [
  [1, '佣兵'],
  [2, '富豪'],
  [3, '盗贼'],
  [4, '歌姬'],
  [5, '混合'],
];
const POOL_STAGE_PAYMENTS = [
  [0, '沿用分池设置'],
  [3, '水晶'],
  [4, '道具'],
  [6, '付费水晶'],
];
/** Reward types the mixed gacha settlement can deliver: gold, crystal, BP, card, item, material, sphere, buddy. */
const POOL_MIXED_REWARD_TYPES = new Set([4, 6, 8, 10, 12, 13, 15, 19]);
const POOL_MIXED_PAGE = 100;
const POOL_RULE_FIXED_DRAWS = { friend: [1], unowned: [1] };

// Shared helpers ---------------------------------------------------------------------------------

const poolRuleType = (row) => poolEditor.ruleTypes?.find((t) => t.id === row?.rule?.type);

/** Facts of a pool rule for badges and summaries; empty for built-in pools. */
function poolRuleFacts(rule) {
  if (!rule) return [];
  const facts = [`${rule.draw_count} 抽`];
  if (rule.type === 'guaranteed') {
    facts.push(
      `保底 ${rule.guaranteed_count} 张 ${rule.guaranteed_rarity} 星`,
      `其余 ${rule.remainder_rarity} 星`,
    );
  } else if (rule.type === 'friend') {
    facts.push(`连抽上限 ${rule.max_draw_count}`, rule.daily_first_free ? '每日首抽免费' : '无每日免费');
  } else if (rule.type === 'step') {
    facts.push(`${rule.step_count} 个阶段`);
  } else if (rule.type === 'select') {
    facts.push(`玩家自选 ${rule.user_select_max} 张`);
  }
  return facts;
}

let poolCatalogIndexSource = null;
let poolCatalogIndex = new Map();

function poolCatalogEntry(reward) {
  if (poolCatalogIndexSource !== poolEditor.catalog) {
    poolCatalogIndexSource = poolEditor.catalog;
    poolCatalogIndex = new Map((poolEditor.catalog || []).map((c) => [contentKey(c), c]));
  }
  return poolCatalogIndex.get(contentKey(reward)) || catalogInfo.get(contentKey(reward));
}

function rewardLabel(reward) {
  return poolCatalogEntry(reward)?.name || `${reward.type}:${reward.reward_typeid}`;
}

// Mixed and staged reward tables ------------------------------------------------------------------

/** Called by selectPoolVariant(): shows the card editor or the reward table for the active variant. */
function setupMixedPool() {
  const mixed = poolEditor.mixedByID.get(poolEditor.activeID);
  $('#pool-card-editor').hidden = !!mixed;
  $('#pool-mixed-editor').hidden = !mixed;
  poolEditor.mixed = mixed || null;
  poolEditor.mixedVariant = mixed ? poolEditor.activeID : 0;
  poolEditor.mixedPage = 0;
  poolEditor.stage = 0;
  if (!mixed) return;
  $('#pool-mixed-search').value = '';
  const row = poolActiveRow();
  const type = poolRuleType(row);
  setHTML(
    $('#pool-mixed-rule'),
    type ? html`${type.name} · ${poolRuleFacts(row.rule).join(' · ')}` : '内置规则',
  );
  const gifts = (row.base.gift_rules || []).map(
    (g) =>
      `第 ${g.from_play} 至 ${g.to_play || '以后'} 次赠送：${g.rewards.map((r) => `${rewardLabel(r)} ×${r.num}`).join('、')}`,
  );
  $('#pool-mixed-gifts').textContent = gifts.join('；');
  $('#pool-mixed-gifts').hidden = !gifts.length;
  renderMixedPool();
}

const poolMixedEditable = () => !!poolActiveRow()?.rule;
const poolMixedStep = () => poolEditor.mixed.steps[poolEditor.stage] || null;
const poolMixedRewards = () => poolMixedStep()?.reward_pool || poolEditor.mixed.reward_pool;
const poolWeightSum = (pool) => pool.reduce((sum, r) => sum + (Number(r.weight) || 0), 0);
const poolShareText = (weight, sum) => (sum ? `${((Number(weight) / sum) * 100).toFixed(3)}%` : '—');

function renderMixedPool() {
  if (!poolEditor.mixed) return;
  const steps = poolEditor.mixed.steps;
  const editable = poolMixedEditable();
  const stage = poolEditor.stage;
  $('#pool-mixed-title').textContent = steps.length ? `阶段奖励 · 第 ${stage + 1} 阶段` : '混合奖励表';
  $('#pool-mixed-note').textContent = editable
    ? steps.length
      ? '每个阶段的奖励表、价格和消耗互相独立；成功抽取才推进，最后阶段之后一直重复。'
      : '每次抽取按权重从奖励表独立抽出，允许重复。'
    : '内置卡池保留原奖励身份与赠礼规则，可调整价格和权重。';
  $('#pool-mixed-add').hidden = !editable;
  $('#pool-mixed-copy').hidden = !editable || !steps.length || stage === 0;
  renderStageTabs();
  renderStageSettings();
  renderMixedRows();
}

function renderStageTabs() {
  const steps = poolEditor.mixed?.steps || [];
  const box = $('#pool-stage-tabs');
  box.hidden = !steps.length;
  if (!steps.length) return;
  setHTML(
    box,
    steps.map(
      (step, i) => html`
        ${i > 0 && html`<span class="stage-arrow" aria-hidden="true">→</span>`}
        <button type="button" role="tab" class="stage-tab ${i === poolEditor.stage ? 'active' : ''}"
          data-stage="${i}" aria-selected="${i === poolEditor.stage}">
          <b>第 ${i + 1} 阶段${i === steps.length - 1 ? ' · 之后重复' : ''}</b>
          <small data-stage-facts="${i}">${poolStageFacts(step)}</small>
        </button>
      `,
    ),
  );
}

function poolStageFacts(step) {
  const count = step.reward_pool.length;
  return `${num(step.price)} · ${count ? `${count} 项奖励` : '奖励表为空'}`;
}

function renderStageSettings() {
  const step = poolMixedStep();
  const box = $('#pool-stage-settings');
  box.hidden = !step;
  if (!step) return;
  const strategy = poolEditor.strategies.get(poolEditor.mixedVariant);
  const type = step.pay_type || 0;
  const selected = step.pay_typeid || 0;
  $('#pool-stage-price').value = step.price || '';
  $('#pool-stage-price-label').textContent =
    `本阶段价格（${poolPayName(type || strategy.pay_type, type ? selected : strategy.pay_typeid)}）`;
  $('#pool-stage-pay-type').value = String(type);
  $('#pool-stage-pay-item').hidden = type !== 4;
  if (type !== 4) return;
  const items = poolEditor.catalog.filter((c) => c.reward_type === 8 && c.resource_state !== 'unavailable');
  setHTML(
    $('#pool-stage-pay-item'),
    html`
      <option value="">请选择道具</option>
      ${items.map(
        (c) => html`<option value="${c.reward_type_id}" ${attr(c.reward_type_id === selected, 'selected')}>
          ${c.name} · ${c.reward_type_id}
        </option>`,
      )}
    `,
  );
}

function renderMixedRows() {
  const pool = poolMixedRewards();
  const editable = poolMixedEditable();
  const query = $('#pool-mixed-search').value.trim().toLowerCase();
  const rows = pool
    .map((reward, index) => ({ ...reward, index }))
    .filter((r) => `${rewardLabel(r.reward)} ${r.reward.reward_typeid}`.toLowerCase().includes(query));
  const pages = Math.max(1, Math.ceil(rows.length / POOL_MIXED_PAGE));
  poolEditor.mixedPage = Math.max(0, Math.min(poolEditor.mixedPage, pages - 1));
  const sum = poolWeightSum(pool);
  $('#pool-mixed-total').textContent = `${num(pool.length)} 项 · 权重合计 ${num(sum)}`;
  $('#pool-mixed-page').textContent = `${poolEditor.mixedPage + 1} / ${pages} 页 · 共 ${rows.length} 项`;
  $('#pool-mixed-prev').disabled = poolEditor.mixedPage === 0;
  $('#pool-mixed-next').disabled = poolEditor.mixedPage === pages - 1;
  $('#pool-mixed-pager').hidden = pages === 1;
  const visible = rows.slice(
    poolEditor.mixedPage * POOL_MIXED_PAGE,
    (poolEditor.mixedPage + 1) * POOL_MIXED_PAGE,
  );
  if (!visible.length) {
    const hint = pool.length ? '换个关键词试试。' : editable ? '点击「添加奖励」选择本表的候选奖励。' : '';
    setHTML($('#pool-mixed-rows'), raw(emptyRow(5, pool.length ? '没有匹配的奖励' : '奖励表为空', hint)));
    return;
  }
  setHTML(
    $('#pool-mixed-rows'),
    visible.map((r) => {
      const entry = poolCatalogEntry(r.reward);
      const label = rewardLabel(r.reward);
      const share = sum ? (Number(r.weight) / sum) * 100 : 0;
      return html`
        <tr>
          <td>
            <span class="mixed-reward">
              <span class="thumb">${raw(image(entry?.image_url, label))}</span>
              <span>
                <b class="pool-card-name">${label}</b>
                <span class="sub"
                  >${pickerKindNames[entry?.kind] || `类型 ${r.reward.type}`}${r.reward.reward_typeid ? ` · ${r.reward.reward_typeid}` : ''}</span
                >
              </span>
            </span>
          </td>
          <td>
            ${
              editable
                ? html`<input type="number" min="1" max="${rewardMax(r.reward.type)}" value="${r.reward.num}"
                  data-mixed-num="${r.index}" aria-label="${label}数量" />`
                : html`×${num(r.reward.num)}`
            }
          </td>
          <td>
            <input type="number" min="1" max="1000000" value="${r.weight}" data-mixed-weight="${r.index}"
              aria-label="${label}权重" />
          </td>
          <td class="share-cell">
            <span class="share-bar"><i data-share-bar="${r.index}" style="width:${share.toFixed(2)}%"></i></span>
            <span data-share="${r.index}">${poolShareText(r.weight, sum)}</span>
          </td>
          <td>
            ${
              editable &&
              html`<button type="button" class="x-button" data-mixed-remove="${r.index}" aria-label="移除${label}">
              <svg class="icon"><use href="#i-close" /></svg>
            </button>`
            }
          </td>
        </tr>
      `;
    }),
  );
}

/** Weight edits only refresh the visible shares and the total; the inputs keep focus. */
function refreshMixedShares() {
  const pool = poolMixedRewards();
  const sum = poolWeightSum(pool);
  $('#pool-mixed-total').textContent = `${num(pool.length)} 项 · 权重合计 ${num(sum)}`;
  $$('#pool-mixed-rows [data-share]').forEach((el) => {
    el.textContent = poolShareText(pool[Number(el.dataset.share)].weight, sum);
  });
  $$('#pool-mixed-rows [data-share-bar]').forEach((el) => {
    const weight = Number(pool[Number(el.dataset.shareBar)].weight) || 0;
    el.style.width = `${sum ? ((weight / sum) * 100).toFixed(2) : 0}%`;
  });
}

function refreshStageFacts() {
  const step = poolMixedStep();
  const facts = step && $(`#pool-stage-tabs [data-stage-facts="${poolEditor.stage}"]`);
  if (facts) facts.textContent = poolStageFacts(step);
}

function addMixedRewards() {
  const pool = poolMixedRewards();
  openContentPicker(
    (rows) => {
      if (pool.length + rows.length > 6000) throw new Error('每个奖励表最多 6000 项');
      rememberCatalogRows(rows);
      let added = 0;
      for (const row of rows) {
        const reward = contentReward(row);
        if (
          pool.some((r) => r.reward.type === reward.type && r.reward.reward_typeid === reward.reward_typeid)
        )
          continue;
        pool.push({ reward, weight: 1 });
        added++;
      }
      poolChanged();
      renderMixedPool();
      // The table shows what was added; only duplicates that were skipped need a word.
      if (added < rows.length) toast(`已添加 ${added} 项；${rows.length - added} 项已在表中，未重复添加`);
    },
    ['currency', 'card', 'material', 'item', 'sphere', 'buddy'],
    {
      title: '添加扭蛋奖励',
      accept: (entry) => POOL_MIXED_REWARD_TYPES.has(entry.reward_type) || '扭蛋奖励不支持此类型',
    },
  );
}

async function copyPreviousStage() {
  const steps = poolEditor.mixed.steps;
  const i = poolEditor.stage;
  if (i < 1) return;
  if (
    steps[i].reward_pool.length &&
    !(await askConfirm(
      `用第 ${i} 阶段的 ${steps[i - 1].reward_pool.length} 项奖励替换本阶段奖励？`,
      '本阶段价格和消耗不变。',
      { ok: '替换' },
    ))
  ) {
    return;
  }
  steps[i].reward_pool = structuredClone(steps[i - 1].reward_pool);
  poolChanged();
  renderMixedPool();
  toast(`已复制第 ${i} 阶段奖励，可继续单独调整`);
}

function onMixedInput(event) {
  const el = event.target;
  const pool = poolMixedRewards();
  if (el.dataset.mixedWeight != null) {
    pool[Number(el.dataset.mixedWeight)].weight = Number(el.value);
    el.classList.remove('invalid');
    poolChanged();
    refreshMixedShares();
  } else if (el.dataset.mixedNum != null) {
    pool[Number(el.dataset.mixedNum)].reward.num = Number(el.value);
    el.classList.remove('invalid');
    poolChanged();
  } else if (el.id === 'pool-stage-price') {
    const price = Number(el.value);
    poolMixedStep().price = price;
    if (poolEditor.stage === 0) poolEditor.strategies.get(poolEditor.mixedVariant).price = price;
    poolChanged();
    refreshStageFacts();
  } else if (el.id === 'pool-mixed-search') {
    poolEditor.mixedPage = 0;
    renderMixedRows();
  }
}

function onMixedChange(event) {
  const el = event.target;
  const step = poolMixedStep();
  if (el.id === 'pool-stage-pay-type') {
    step.pay_type = Number(el.value);
    if (step.pay_type !== 4) step.pay_typeid = 0;
    poolChanged();
    renderStageSettings();
  } else if (el.id === 'pool-stage-pay-item') {
    step.pay_typeid = Number(el.value);
    poolChanged();
    renderStageSettings();
  }
}

function onMixedClick(event) {
  const pick = (selector) => event.target.closest(selector);
  let el;
  if ((el = pick('[data-stage]'))) {
    const stage = Number(el.dataset.stage);
    if (stage === poolEditor.stage) return;
    poolEditor.stage = stage;
    poolEditor.mixedPage = 0;
    renderMixedPool();
    $(`#pool-stage-tabs [data-stage="${stage}"]`)?.focus();
  } else if ((el = pick('[data-mixed-remove]'))) {
    poolMixedRewards().splice(Number(el.dataset.mixedRemove), 1);
    poolChanged();
    renderMixedRows();
    refreshStageFacts();
  } else if (pick('#pool-mixed-add')) addMixedRewards();
  else if (pick('#pool-mixed-copy')) copyPreviousStage();
  else if (pick('#pool-mixed-prev') || pick('#pool-mixed-next')) {
    poolEditor.mixedPage += pick('#pool-mixed-next') ? 1 : -1;
    renderMixedRows();
  }
}

$('#pool-mixed-editor').addEventListener('input', onMixedInput);
$('#pool-mixed-editor').addEventListener('change', onMixedChange);
$('#pool-mixed-editor').addEventListener('click', onMixedClick);
$('#pool-stage-tabs').addEventListener('keydown', (event) => {
  if (!['ArrowLeft', 'ArrowRight'].includes(event.key) || !poolEditor.mixed?.steps.length) return;
  event.preventDefault();
  const last = poolEditor.mixed.steps.length - 1;
  const next = Math.max(0, Math.min(last, poolEditor.stage + (event.key === 'ArrowRight' ? 1 : -1)));
  $(`#pool-stage-tabs [data-stage="${next}"]`)?.click();
});

// Creation dialog --------------------------------------------------------------------------------

const poolCreate = {
  mode: 'rule', // 'rule' | 'copy'
  type: 'standard',
  draws: {}, // rule type → draw counts
  params: {}, // rule type → editable parameters
  professions: new Set(),
  sourceID: 0,
  sourceQuery: '',
};

const poolCreateType = () => poolEditor.ruleTypes.find((t) => t.id === poolCreate.type);

function poolCreateDefaults(type) {
  const { type: _type, draw_count: drawCount, ...params } = type.default;
  poolCreate.draws[type.id] ??=
    POOL_RULE_FIXED_DRAWS[type.id] || (type.id === 'standard' ? [1, 10] : [drawCount]);
  poolCreate.params[type.id] ??= params;
}

function poolCopySources() {
  const seen = new Set();
  return poolEditor.rows.filter((row) => {
    const group = row.base.groupid;
    if (seen.has(group)) return false;
    seen.add(group);
    return poolGroupRows(row).every(poolCopyable);
  });
}

/** Mirrors professionChoicesAllowed(): fixed draw counts, one payment per count, plain card pools. */
function poolCopyAllowsProfessions(members) {
  const seen = new Set();
  return (
    members.length > 0 &&
    members.every((r) => {
      const b = r.base;
      const key = `${b.card_num}:${b.card_num_max}`;
      const plain =
        !b.gacha_type &&
        !b.arthur_type &&
        !b.user_select_max &&
        b.card_num === b.card_num_max &&
        !b.daily_first_free &&
        !b.unowned_only &&
        !poolIsMixed(r) &&
        !seen.has(key);
      seen.add(key);
      return plain;
    })
  );
}

/** Validates the rule form like validateCreationRules(); returns the request rules or throws. */
function poolCreationRules() {
  const type = poolCreateType();
  if (!type) throw new Error('请选择规则类型');
  const draws = poolCreate.draws[type.id];
  const params = poolCreate.params[type.id];
  const integer = (value, min, max, label) => {
    if (!Number.isInteger(value) || value < min || value > max)
      throw new Error(`${label}须为 ${min}–${max} 的整数`);
  };
  const limit = poolEditor.maxDrawCounts || 2;
  if (!draws.length || draws.length > limit) throw new Error(`请设置 1–${limit} 种抽数`);
  draws.forEach((d) => integer(d, 1, 100, '每次抽数'));
  if (new Set(draws).size !== draws.length) throw new Error('抽数不能重复');
  if (type.id === 'step' && draws.length !== 1) throw new Error('阶段扭蛋只创建一种抽数');
  if (type.id === 'friend') integer(params.max_draw_count, 1, 100, '连抽上限');
  if (type.id === 'step') integer(params.step_count, 2, 10, '阶段数量');
  if (type.id === 'select') integer(params.user_select_max, 1, 100, '玩家自选张数');
  if (type.id === 'guaranteed') {
    integer(params.guaranteed_rarity, 2, 7, '保底星级');
    integer(params.remainder_rarity, 1, params.guaranteed_rarity - 1, '其余位置星级');
    integer(params.guaranteed_count, 1, Math.min(...draws) - 1, '保底张数');
  }
  return draws.map((drawCount) => ({ ...params, type: type.id, draw_count: drawCount }));
}

function poolCreateVariants(drawCounts, professions) {
  const jobs = [...professions].sort((a, b) => a - b);
  return drawCounts.flatMap((count) =>
    jobs.length
      ? jobs.map((job) => `${count} 抽 · ${POOL_PROFESSIONS.find(([v]) => v === job)[1]}`)
      : [`${count} 抽`],
  );
}

function poolCreateTypesHTML() {
  return poolEditor.ruleTypes.map(
    (type) => html`
      <label class="create-type">
        <input type="radio" name="pool-create-type" value="${type.id}" ${attr(type.id === poolCreate.type, 'checked')} />
        <span>
          <b>${type.name}</b>
          <small>${type.summary[0]}</small>
        </span>
      </label>
    `,
  );
}

function poolCreateSourcesHTML() {
  const query = poolCreate.sourceQuery.trim().toLowerCase();
  const sources = poolCopySources().filter((row) =>
    `${row.config.name} ${row.base.groupid}`.toLowerCase().includes(query),
  );
  return html`
    <input type="search" class="search" placeholder="搜索卡池名称或组 ID" value="${poolCreate.sourceQuery}"
      data-create-source-search aria-label="搜索复制来源" />
    <div class="create-sources">
      ${sources.map(
        (row) => html`
          <label class="create-type">
            <input type="radio" name="pool-create-source" value="${row.gacha_id}"
              ${attr(row.gacha_id === poolCreate.sourceID, 'checked')} />
            <span>
              <b>${row.config.name}</b>
              <small>组 ${row.base.groupid} · ${poolGroupRows(row).length} 种抽法${row.custom ? ' · 后台新建' : ''}</small>
            </span>
          </label>
        `,
      )}
      ${!sources.length && html`<p class="hint">没有可复制的已发布卡牌池。混合奖励、阶段与未入手池请按类型新建。</p>`}
    </div>
  `;
}

function poolDrawTokensHTML(type) {
  const draws = poolCreate.draws[type.id];
  if (POOL_RULE_FIXED_DRAWS[type.id]) {
    return html`<span class="fixed-value">单抽（固定）</span>`;
  }
  if (type.id === 'step') {
    return html`
      <span class="input-unit">
        <input type="number" min="1" max="100" value="${draws[0]}" data-create-draw-single aria-label="每次抽数" />
        <span>抽</span>
      </span>
    `;
  }
  const presets = [1, 10, 11].filter((n) => !draws.includes(n));
  return html`
    <div class="target-chips">
      ${draws.map(
        (count) => html`
          <span class="target-chip">
            ${count} 抽
            <button type="button" class="x-button" data-create-draw-remove="${count}" aria-label="移除 ${count} 抽"
              ${attr(draws.length === 1, 'disabled')}>
              <svg class="icon"><use href="#i-close" /></svg>
            </button>
          </span>
        `,
      )}
      ${
        draws.length < (poolEditor.maxDrawCounts || 2) &&
        html`
        ${presets.map(
          (count) =>
            html`<button type="button" class="add-chip" data-create-draw-add="${count}">+ ${count} 抽</button>`,
        )}
        <span class="joined target-id">
          <input type="number" min="1" max="100" placeholder="其他抽数" data-create-draw-input aria-label="其他抽数" />
          <button type="button" class="secondary sm" data-create-draw-custom>添加</button>
        </span>
      `
      }
    </div>
  `;
}

function poolParamField(key, label, unit, min, max) {
  const value = poolCreate.params[poolCreate.type][key];
  return html`
    <label class="field">
      <span class="field-label">${label}</span>
      <span class="input-unit">
        <input type="number" min="${min}" max="${max}" step="1" value="${value}" data-create-param="${key}" />
        <span>${unit}</span>
      </span>
    </label>
  `;
}

function poolCreateSummaryHTML(type) {
  let rules = null;
  let error = '';
  try {
    rules = poolCreationRules();
  } catch (e) {
    error = e.message;
  }
  const professions = type.profession_choices ? poolCreate.professions : new Set();
  const variants = poolCreateVariants(poolCreate.draws[type.id], professions);
  const price = (count) => (type.id === 'friend' ? '每张 200 友情点' : `${num(50 * count)} 水晶`);
  if (error) return html`<span class="field-error">${error}</span>`;
  return html`
    <b>将创建 ${variants.length} 个分池草稿</b>
    <span>${variants.join('、')}</span>
    <span>
      初始费用：${poolCreate.draws[type.id].map((c) => `${c} 抽 ${price(c)}`).join('，')}
      ${type.id === 'step' && rules ? `；${rules[0].step_count} 个阶段费用相同` : ''}（可在草稿中修改）
    </span>
    <span>
      内容从空开始：${
        ['mixed', 'step'].includes(type.id)
          ? '添加奖励、数量和权重'
          : type.id === 'select'
            ? `添加至少 ${rules[0].user_select_max} 张候选卡牌与权重`
            : '添加卡牌与权重'
      }后才能发布。
    </span>
  `;
}

function poolCreateRuleHTML(type) {
  const params = poolCreate.params[type.id];
  return html`
    <div class="create-about">
      <h3>${type.name}</h3>
      ${type.summary.map((line) => html`<p>${line}</p>`)}
    </div>
    <div class="ws-form">
      <label class="field span-2">
        <span class="field-label">卡池名称</span>
        <input id="pool-create-name" maxlength="60" placeholder="例如：秋季限定扭蛋" autocomplete="off"
          value="${$('#pool-create-name')?.value || ''}" />
      </label>
      <div class="field span-2">
        <span class="field-label">每次抽数${
          type.id === 'standard'
            ? `（每种抽数一个分池，游戏内最多显示 ${poolEditor.maxDrawCounts || 2} 种）`
            : ''
        }</span>
        ${poolDrawTokensHTML(type)}
      </div>
      ${
        type.id === 'guaranteed' &&
        html`
        ${poolParamField('guaranteed_count', '每次保底张数', '张', 1, 99)}
        ${poolParamField('guaranteed_rarity', '保底星级', '星', 2, 7)}
        ${poolParamField('remainder_rarity', '其余位置星级', '星', 1, 6)}
      `
      }
      ${
        type.id === 'friend' &&
        html`
        ${poolParamField('max_draw_count', '连抽上限（按余额决定实际抽数）', '抽', 1, 100)}
        <div class="field field-switch">
          <label class="switch">
            <input type="checkbox" data-create-param="daily_first_free" ${attr(params.daily_first_free, 'checked')} />
            <span>每日首次免费单抽</span>
          </label>
        </div>
      `
      }
      ${type.id === 'step' && poolParamField('step_count', '阶段数量', '个', 2, 10)}
      ${
        type.id === 'select' &&
        html`
        ${poolParamField('user_select_max', '玩家自选张数', '张', 1, 100)}
        <p class="hint span-2">
          玩家先从候选卡牌中选满这么多张，之后每次抽取只在所选卡牌中按权重抽出，允许重复。候选至少要有这么多张才能保存和发布。
        </p>
      `
      }
      ${
        type.profession_choices &&
        html`
        <div class="field span-2">
          <span class="field-label">职业分池（可选）</span>
          <div class="chips">
            ${POOL_PROFESSIONS.map(
              ([value, label]) => html`
                <button type="button" class="${poolCreate.professions.has(value) ? 'active' : ''}"
                  aria-pressed="${poolCreate.professions.has(value)}" data-create-profession="${value}">${label}</button>
              `,
            )}
          </div>
          <span class="hint">勾选后每种抽数按职业各建一个分池，玩家抽取时在原生窗口选择职业；单职业分池只接受本职业及通用卡牌。</span>
        </div>
      `
      }
    </div>
    <div class="create-summary" id="pool-create-summary" aria-live="polite">${poolCreateSummaryHTML(type)}</div>
    <p class="hint">规则类型、抽数${type.id === 'step' ? '、阶段数量' : ''}和特殊机制在创建后固定；内容、费用、封面与排期可继续编辑。不会附带任何内置卡池的赠礼。</p>
  `;
}

function poolCreateCopyHTML() {
  const source = poolEditor.rows.find((r) => r.gacha_id === poolCreate.sourceID);
  const members = source ? poolGroupRows(source) : [];
  const allowed = poolCopyAllowsProfessions(members);
  if (!allowed) poolCreate.professions.clear();
  const variants = members.flatMap((r) =>
    poolCreate.professions.size
      ? poolCreateVariants([r.base.card_num], poolCreate.professions)
      : [poolVariantLabel(r)],
  );
  return html`
    <div class="create-about">
      <h3>复制现有卡池</h3>
      <p>复制来源已生效的卡牌、权重、价格、抽法、每日首抽免费和赠礼配置，新池保存自己的完整规则；之后修改或删除来源不会影响新池。新池从未发布的草稿开始，玩家抽取次数和免费领取记录重新计算。</p>
    </div>
    <div class="ws-form">
      <label class="field span-2">
        <span class="field-label">新卡池名称</span>
        <input id="pool-create-name" maxlength="60" placeholder="例如：秋季限定扭蛋" autocomplete="off"
          value="${$('#pool-create-name')?.value || ''}" />
      </label>
      ${
        allowed &&
        html`
        <div class="field span-2">
          <span class="field-label">职业分池（可选）</span>
          <div class="chips">
            ${POOL_PROFESSIONS.map(
              ([value, label]) => html`
                <button type="button" class="${poolCreate.professions.has(value) ? 'active' : ''}"
                  aria-pressed="${poolCreate.professions.has(value)}" data-create-profession="${value}">${label}</button>
              `,
            )}
          </div>
        </div>
      `
      }
    </div>
    <div class="create-summary">
      ${
        source
          ? html`
            <b>将复制「${source.config.name}」的 ${members.length} 种抽法，创建 ${variants.length} 个分池草稿</b>
            ${members.map(
              (r) => html`<span>· ${poolVariantLabel(r)} · ${num(r.config.card_ids.length)} 张卡牌</span>`,
            )}
            ${!allowed && html`<span class="hint">此卡池的抽法不支持职业分池。</span>`}
          `
          : html`<span>请在左侧选择复制来源。</span>`
      }
    </div>
  `;
}

function renderPoolCreate({ list = true } = {}) {
  const rule = poolCreate.mode === 'rule';
  $$('#pool-create-modes [data-create-mode]').forEach((button) => {
    const active = button.dataset.createMode === poolCreate.mode;
    button.classList.toggle('active', active);
    button.setAttribute('aria-selected', String(active));
  });
  if (list) setHTML($('#pool-create-options'), rule ? poolCreateTypesHTML() : poolCreateSourcesHTML());
  const focusKey = document.activeElement?.closest('#pool-create-detail') ? poolCreateFocusKey() : null;
  setHTML($('#pool-create-detail'), rule ? poolCreateRuleHTML(poolCreateType()) : poolCreateCopyHTML());
  if (focusKey) $(`#pool-create-detail ${focusKey}`)?.focus();
  $('#pool-create-apply').disabled = rule ? !poolCreateType() : !poolCreate.sourceID;
}

/** Re-rendering the detail panel keeps focus on the same control. */
function poolCreateFocusKey() {
  const el = document.activeElement;
  for (const name of ['data-create-param', 'data-create-profession', 'data-create-draw-add']) {
    if (el.hasAttribute(name)) return `[${name}="${el.getAttribute(name)}"]`;
  }
  if (el.id === 'pool-create-name') return '#pool-create-name';
  if (el.hasAttribute('data-create-draw-single')) return '[data-create-draw-single]';
  return null;
}

function openPoolCreate() {
  if (policyBusy('pool-editor') || !poolEditor.ruleTypes?.length) return;
  for (const type of poolEditor.ruleTypes) poolCreateDefaults(type);
  if (!poolEditor.ruleTypes.some((t) => t.id === poolCreate.type))
    poolCreate.type = poolEditor.ruleTypes[0].id;
  poolCreate.mode = 'rule';
  poolCreate.professions.clear();
  const sources = poolCopySources();
  const current = sources.find((r) => r.base.groupid === poolEditor.row?.base.groupid) || sources[0];
  poolCreate.sourceID = current?.gacha_id || 0;
  poolCreate.sourceQuery = '';
  $('#pool-create-detail').innerHTML = '';
  renderPoolCreate();
  $('#pool-create-name').value = '';
  $('#pool-create-modal').classList.add('open');
  requestAnimationFrame(() => $('#pool-create-name')?.focus());
}

function addPoolDrawCount(count) {
  const draws = poolCreate.draws[poolCreate.type];
  if (!Number.isInteger(count) || count < 1 || count > 100) return toast('抽数须为 1–100 的整数', true);
  if (draws.includes(count)) return toast(`已有 ${count} 抽`, true);
  if (draws.length >= (poolEditor.maxDrawCounts || 2))
    return toast(`游戏内最多显示 ${poolEditor.maxDrawCounts || 2} 种抽数`, true);
  draws.push(count);
  draws.sort((a, b) => a - b);
  renderPoolCreate({ list: false });
}

async function applyPoolCreate() {
  const nameInput = $('#pool-create-name');
  const name = nameInput.value.trim();
  clearInvalid($('#pool-create-modal'));
  if (!name || [...name].length > 60) {
    markInvalid(nameInput, '请填写 1–60 字的卡池名称');
    nameInput.focus();
    return toast('请填写 1–60 字的卡池名称', true);
  }
  let selection;
  try {
    if (poolCreate.mode === 'rule') {
      const allowJobs = poolCreateType().profession_choices;
      selection = { rules: poolCreationRules(), professions: allowJobs ? [...poolCreate.professions] : [] };
    } else {
      if (!poolCreate.sourceID) throw new Error('请选择复制来源');
      selection = { source_id: poolCreate.sourceID, professions: [...poolCreate.professions] };
    }
  } catch (error) {
    return toast(error.message, true);
  }
  selection.professions.sort((a, b) => a - b);
  if (
    !(await confirmDiscard(poolEditor.dirty, '创建后会切换到新卡池，当前未保存的编辑将放弃。', '放弃并创建'))
  )
    return;
  const dialog = $('#pool-create-modal .dialog');
  dialog.inert = true;
  $('#pool-create-apply').disabled = true;
  try {
    await runPoolTask(async () => {
      let result;
      try {
        result = await api('/api/gacha-pools', {
          method: 'POST',
          body: JSON.stringify({ ...selection, name, expected_revision: poolEditor.customRevision }),
        });
      } catch (error) {
        if (error.status !== 409) throw error;
        await loadPoolEditor();
        toast('卡池列表刚被其他操作更新，已重新载入；请确认后再次创建', true);
        return;
      }
      $('#pool-create-modal').classList.remove('open');
      poolEditor.dirty = false;
      $('#pool-config-status').value = '';
      await loadPoolEditor(result.gacha_ids[0]);
      state.loaded.delete('gachas');
      state.loaded.delete('audit');
      clearViewAlert('pool-editor');
      toast(
        `已创建「${name}」的 ${result.gacha_ids.length} 个分池草稿。添加内容、保存并预览后发布，再到「扭蛋发布」开启`,
      );
    }, '新建卡池');
  } finally {
    dialog.inert = false;
    $('#pool-create-apply').disabled = false;
  }
}

function onPoolCreateInput(event) {
  const el = event.target;
  if (el.name === 'pool-create-type') {
    poolCreate.type = el.value;
    renderPoolCreate({ list: false });
  } else if (el.name === 'pool-create-source') {
    poolCreate.sourceID = Number(el.value);
    poolCreate.professions.clear();
    renderPoolCreate({ list: false });
  } else if (el.matches('[data-create-source-search]')) {
    poolCreate.sourceQuery = el.value;
    setHTML($('#pool-create-options'), poolCreateSourcesHTML());
    $('#pool-create-options [data-create-source-search]').focus();
  } else if (el.dataset.createParam || el.matches('[data-create-draw-single]')) {
    if (el.dataset.createParam) {
      const params = poolCreate.params[poolCreate.type];
      params[el.dataset.createParam] = el.type === 'checkbox' ? el.checked : Number(el.value);
    } else poolCreate.draws[poolCreate.type] = [Number(el.value)];
    setHTML($('#pool-create-summary'), poolCreateSummaryHTML(poolCreateType()));
  } else if (el.id === 'pool-create-name') {
    el.classList.remove('invalid');
  }
}

function onPoolCreateClick(event) {
  const pick = (selector) => event.target.closest(selector);
  let el;
  if ((el = pick('[data-create-mode]'))) {
    if (el.dataset.createMode === poolCreate.mode) return;
    poolCreate.mode = el.dataset.createMode;
    poolCreate.professions.clear();
    renderPoolCreate();
  } else if ((el = pick('[data-create-profession]'))) {
    const value = Number(el.dataset.createProfession);
    poolCreate.professions.has(value)
      ? poolCreate.professions.delete(value)
      : poolCreate.professions.add(value);
    renderPoolCreate({ list: false });
  } else if ((el = pick('[data-create-draw-remove]'))) {
    const draws = poolCreate.draws[poolCreate.type];
    draws.splice(draws.indexOf(Number(el.dataset.createDrawRemove)), 1);
    renderPoolCreate({ list: false });
  } else if ((el = pick('[data-create-draw-add]'))) {
    addPoolDrawCount(Number(el.dataset.createDrawAdd));
  } else if (pick('[data-create-draw-custom]')) {
    addPoolDrawCount(Number($('#pool-create-detail [data-create-draw-input]').value));
  }
}

$('#pool-create').onclick = openPoolCreate;
$('#pool-create-cancel').onclick = () => $('#pool-create-modal').classList.remove('open');
$('#pool-create-apply').onclick = applyPoolCreate;
const isToggle = (el) => el.type === 'checkbox' || el.type === 'radio';
$('#pool-create-modal').addEventListener('input', (event) => {
  if (!isToggle(event.target)) onPoolCreateInput(event);
});
$('#pool-create-modal').addEventListener('change', (event) => {
  if (isToggle(event.target)) onPoolCreateInput(event);
});
$('#pool-create-modal').addEventListener('click', onPoolCreateClick);
$('#pool-create-modal').addEventListener('keydown', (event) => {
  if (event.key !== 'Enter') return;
  if (event.target.matches('[data-create-draw-input]')) {
    event.preventDefault();
    addPoolDrawCount(Number(event.target.value));
  } else if (event.target.id === 'pool-create-name') {
    event.preventDefault();
    applyPoolCreate();
  }
});
