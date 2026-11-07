'use strict';
// 圣剑杯与探索 ------------------------------------------------------------------
const activityEditor = { config: null, revision: 0, dirty: false, names: {}, targets: {}, saved: null };
const activityRewardLabel = (r) => ({ 4: '金币', 10: '水晶', 12: '体力 BP' })[r.type] || contentLabel(r);
const activityCurrency = (r) => [4, 10, 12].includes(r.type);
function activityChanged() {
  activityEditor.dirty = true;
  setSaveState('#activity-summary', '有未保存的修改（保存后下次出发生效）', 'dirty');
  updateDraftIndicators();
  updateActivityTabs();
}
async function loadActivityRewards() {
  const data = await api('/api/activity-rewards');
  activityEditor.config = data.config;
  activityEditor.revision = data.revision;
  activityEditor.dirty = false;
  activityEditor.names = data.names || {};
  activityEditor.targets = data.boss_targets;
  const rewards = [
    ...Object.values(data.config.cups).flatMap((p) => p.grades.flatMap((g) => g.rewards || [])),
    ...data.config.explore.flatMap((s) => s.rewards),
  ];
  await resolveContentLabels(rewards.filter((r) => !activityCurrency(r)));
  await resolveCatalogEntries(rewards.filter(activityCurrency).map((r) => [r.type, r.reward_typeid])).catch(
    () => {},
  );
  const selected = $('#cup-select').value;
  $('#cup-select').innerHTML =
    Object.keys(data.config.cups)
      .map((id) => `<option value="${id}">${esc(activityEditor.names[id] || id)} · ${id}</option>`)
      .join('') || '<option value="">当前资源尚未配置圣剑杯</option>';
  if (data.config.cups[selected]) $('#cup-select').value = selected;
  $('#activity-version').textContent = `配置 v${data.revision}`;
  setSaveState('#activity-summary', '已载入，与服务器一致', 'ok');
  renderActivityRewards();
  activityEditor.saved = activityComparable(activityEditor.config);
  updateActivityTabs();
}
// Normalized view used only to summarize what a save would change.
function activityComparable(config) {
  const c = structuredClone(config);
  for (const p of Object.values(c.cups)) {
    p.team_cost_initial = p.team_cost_initial || 3;
    p.member_dead_end = !!p.member_dead_end;
    for (const g of p.grades) g.rewards ??= [];
  }
  for (const s of c.explore) s.chance_per_million ??= s.rewards.map(() => 1000000);
  return c;
}
function activityChances(key) {
  if (!key.startsWith('explore:')) return null;
  const slot = activityEditor.config.explore[Number(key.split(':')[1])];
  return (slot.chance_per_million ??= slot.rewards.map(() => 1000000));
}
function activityPool(key) {
  const [kind, index] = key.split(':');
  return kind === 'cup'
    ? activityEditor.config.cups[$('#cup-select').value].grades[Number(index)].rewards
    : activityEditor.config.explore[Number(index)].rewards;
}
const scoreHuman = (v) => {
  v = Number(v) || 0;
  return v >= 1e12
    ? `${+(v / 1e12).toFixed(2)} 兆`
    : v >= 1e8
      ? `${+(v / 1e8).toFixed(2)} 亿`
      : v >= 1e4
        ? `${+(v / 1e4).toFixed(2)} 万`
        : num(v);
};
function chanceTag(pct) {
  if (!Number.isFinite(pct) || pct < 0 || pct > 100) return '<span class="tag bad">无效</span>';
  if (pct === 100) return '<span class="tag ok">必得</span>';
  if (pct === 0) return '<span class="tag bad">不掉落</span>';
  return `<span class="tag info">${pct <= 50 ? `约 1/${+(100 / pct).toFixed(pct < 1 ? 0 : 1)}` : '独立判定'}</span>`;
}
function rewardChips(rewards, key) {
  return rewards
    .map((r, i) => {
      const label = activityRewardLabel(r);
      return `<span class="reward-chip">${contentThumb(r)}<span class="reward-name" title="${esc(label)}${activityCurrency(r) ? '' : ' · ' + esc(r.reward_typeid)}">${contentName(r, label)}</span><span class="times">×</span><input type="number" min="1" max="10000000" step="1" value="${esc(r.num)}" data-activity-num="${key}:${i}" aria-label="${esc(label)}数量"><button type="button" class="x-button" data-activity-remove="${key}:${i}" aria-label="移除${esc(label)}"><svg class="icon"><use href="#i-close"/></svg></button></span>`;
    })
    .join('');
}
// Score for defeating every listed target at the current rate; only a reference for thresholds.
function cupFullScore(bossID, p) {
  const targets = activityEditor.targets[bossID] || [];
  if (
    !targets.length ||
    targets.some((t) => !t.stats) ||
    !Number.isInteger(p.rate) ||
    p.rate < 1 ||
    p.rate > 1000
  )
    return 0;
  return Math.floor((targets.reduce((n, t) => n + Number(t.stats.hp), 0) * p.rate) / 100);
}
const sharePct = (v) => (v < 1 ? '<1%' : v > 99 && v < 100 ? '>99%' : `${+v.toFixed(v < 10 ? 1 : 0)}%`);
const scoreShare = (v) => (v > 100 ? '超出全部击破' : sharePct(v));
// Keep small positive shares visible and zero empty; without a full-clear reference use a log ramp.
function renderGradeBars(p) {
  const max = Math.max(1, ...p.grades.map((g) => Number(g.score) || 0)),
    full = cupFullScore($('#cup-select').value, p),
    total = $('#cup-full-score');
  if (total)
    total.innerHTML = full ? `全部击破约 <b>${scoreHuman(full)}</b> 分<span>倍率 ${p.rate}%</span>` : '';
  p.grades.forEach((g, i) => {
    const row = $(`#cup-grades tr[data-grade="${i}"]`);
    if (!row) return;
    const share = full && Number(g.score) > 0 ? (Number(g.score) / full) * 100 : 0,
      human = row.querySelector('.score-human'),
      bar = row.querySelector('.score-bar');
    human.textContent = scoreHuman(g.score) + (share ? ` · ${scoreShare(share)}` : '');
    human.classList.toggle('warn-text', share > 100);
    human.title = share ? '占全部击破积分的比例（参考）' : '';
    bar.classList.toggle('over', share > 100);
    bar.querySelector('i').style.width =
      `${full ? (share > 0 ? Math.min(100, Math.max(2, share)) : 0) : Math.max(2, (Math.log10((Number(g.score) || 0) + 1) / Math.log10(max + 1)) * 100)}%`;
  });
}
function renderDeadEnd(on) {
  $('#cup-dead-state').textContent = on ? '开启' : '关闭';
  $('#cup-dead-hint').textContent = on
    ? '任一亚瑟倒下即按当前伤害结算，不可续关'
    : '单名亚瑟倒下不强制结束；是否允许续关仍按副本规则';
}
// One colour per target, shared by the composition bar and its legend.
function renderCupEnemyHP(bossID) {
  const targets = activityEditor.targets[bossID] || [],
    total = targets.reduce((n, t) => n + (t.stats ? Number(t.stats.hp) : 0), 0);
  const multipleWaves = new Set(targets.map((t) => t.battle_index)).size > 1,
    label = (t) => `${multipleWaves ? `第 ${t.battle_index + 1} 波 · ` : ''}${t.name}`,
    share = (t) => sharePct((Number(t.stats.hp) / total) * 100);
  $('#cup-boss-hp').innerHTML = targets.length
    ? `<div class="cup-hp-heading"><b>敌方血量构成</b><span class="hint">当前资源配置，已计入编队血量倍率</span><span class="cup-hp-total" id="cup-full-score"></span></div>${total ? `<div class="cup-hp-bar" aria-hidden="true">${targets.map((t, i) => (t.stats ? `<i class="tone-${i % 5}" style="flex-grow:${Number(t.stats.hp) / total}" title="${esc(label(t))} · ${num(t.stats.hp)} HP · ${share(t)}"></i>` : '')).join('')}</div>` : ''}<dl class="cup-hp-grid">${targets.map((t, i) => `<div><dt><i class="cup-hp-dot tone-${i % 5}"></i><span>${esc(label(t))}</span>${t.stats?.attribute ? `<span class="tag">${esc(attributeName(t.stats.attribute))}</span>` : ''}</dt><dd>${t.stats ? `<strong>${scoreHuman(t.stats.hp)}</strong><span>${num(t.stats.hp)} HP · 占 ${share(t)}</span>` : '<span class="hint">未提供血量数据</span>'}</dd></div>`).join('')}</dl>`
    : '<p class="hint">当前目录未提供敌方血量数据。</p>';
}
function renderActivityRewards() {
  const p = activityEditor.config.cups[$('#cup-select').value];
  $('#cup-boss-hp').hidden = !p;
  if (p) renderCupEnemyHP($('#cup-select').value);
  $('#cup-turns').value = p?.end_turn ?? '';
  $('#cup-rate').value = p?.rate || '';
  $('#cup-team-cost').value = p ? p.team_cost_initial || 3 : '';
  $('#cup-turns').disabled = $('#cup-rate').disabled = $('#cup-team-cost').disabled = !p;
  $('#cup-member-dead-end').checked = !!p?.member_dead_end;
  $('#cup-member-dead-end').disabled = !p;
  renderDeadEnd(!!p?.member_dead_end);
  $('#cup-grades').innerHTML = p
    ? p.grades
        .map((g, i) => {
          g.rewards ??= [];
          return `<tr data-grade="${i}"><td><span class="grade-badge">${i + 1}</span></td><td><div class="score-cell"><input type="number" min="0" max="1000000000000" step="1" value="${esc(g.score)}" data-cup-score="${i}" aria-label="第${i + 1}档门槛"><span class="score-human"></span><span class="score-bar"><i></i></span></div></td><td><div class="reward-list">${rewardChips(g.rewards, 'cup:' + i)}${g.rewards.length < 10 ? `<button type="button" class="add-chip" data-activity-add="cup:${i}">+ 添加奖励</button>` : ''}</div>${g.rewards.length ? '' : '<span class="row-error warn-text">此档无奖励：达到门槛也不会发放物品</span>'}</td></tr>`;
        })
        .join('')
    : emptyRow(3, '当前资源尚未配置圣剑杯');
  if (p) renderGradeBars(p);
  $('#explore-reward-slots').innerHTML =
    activityEditor.config.explore
      .map((s, i) => {
        const chances = activityChances('explore:' + i),
          key = 'explore:' + i;
        return `<div class="slot"><div class="slot-head"><b>事件 ${s.event + 1} · ${s.kind === 'symbols' ? '路途奖励' : '宝箱'} ${s.index + 1}</b><span class="tag">${s.rewards.length} / 10 项</span><span class="spacer"></span>${s.rewards.length < 10 ? `<button type="button" class="add-chip" data-activity-add="${key}">+ 添加奖励</button>` : ''}</div><div class="slot-scroll"><table><thead><tr><th>奖励</th><th>数量</th><th>掉落概率</th><th><span class="sr-only">操作</span></th></tr></thead><tbody>${
          s.rewards
            .map((r, j) => {
              const label = activityRewardLabel(r),
                pct = chances[j] / 10000;
              return `<tr><td class="cell-wrap"><div class="reward-cell">${contentThumb(r)}<div><b>${contentName(r, label)}</b>${activityCurrency(r) ? '' : `<span class="sub">ID ${esc(r.reward_typeid)}</span>`}</div></div></td><td><input type="number" min="1" max="10000000" step="1" value="${esc(r.num)}" data-activity-num="${key}:${j}" aria-label="${esc(label)}数量"></td><td><div class="chance-cell"><span class="input-unit"><input type="number" min="0" max="100" step="0.0001" value="${pct}" data-activity-chance="${key}:${j}" aria-label="${esc(label)}掉落概率"><span>%</span></span><span data-chance-tag>${chanceTag(pct)}</span></div></td><td><button type="button" class="x-button" data-activity-remove="${key}:${j}" aria-label="移除${esc(label)}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`;
            })
            .join('') || emptyRow(4, '此事件不发放奖励', '点击“添加奖励”恢复。')
        }</tbody></table></div></div>`;
      })
      .join('') || '<div class="empty"><b>当前场景没有可配置的探索奖励槽</b></div>';
  const split = (key) => {
    const at = key.lastIndexOf(':');
    return [key.slice(0, at), Number(key.slice(at + 1))];
  };
  $$('[data-cup-score]').forEach(
    (el) =>
      (el.oninput = () => {
        p.grades[Number(el.dataset.cupScore)].score = Number(el.value);
        activityChanged();
        renderGradeBars(p);
        validateActivity();
      }),
  );
  $$('[data-activity-num]').forEach(
    (el) =>
      (el.oninput = () => {
        const [pool, i] = split(el.dataset.activityNum);
        activityPool(pool)[i].num = Number(el.value);
        activityChanged();
        validateActivity();
      }),
  );
  $$('[data-activity-chance]').forEach(
    (el) =>
      (el.oninput = () => {
        const [pool, i] = split(el.dataset.activityChance);
        activityChances(pool)[i] = Math.round(Number(el.value) * 10000);
        el.closest('.chance-cell').querySelector('[data-chance-tag]').innerHTML = chanceTag(
          el.value === '' ? NaN : Number(el.value),
        );
        activityChanged();
        validateActivity();
      }),
  );
  $$('[data-activity-remove]').forEach(
    (el) =>
      (el.onclick = () => {
        const [pool, i] = split(el.dataset.activityRemove);
        activityChances(pool)?.splice(i, 1);
        activityPool(pool).splice(i, 1);
        activityChanged();
        renderActivityRewards();
      }),
  );
  $$('[data-activity-add]').forEach(
    (el) =>
      (el.onclick = () =>
        openContentPicker(
          (rows) => {
            const key = el.dataset.activityAdd,
              pool = activityPool(key);
            if (pool.length + rows.length > 10)
              throw new Error(`每档／事件最多10项奖励，当前已有${pool.length}项`);
            if (rows.some((r) => r.kind === 'currency' && ![4, 10, 12].includes(r.reward_type)))
              throw new Error('此处货币支持金币、水晶、体力');
            activityChances(key)?.push(...rows.map(() => 1000000));
            pool.push(...rows.map(contentReward));
            activityChanged();
            renderActivityRewards();
          },
          ['currency', 'card', 'material', 'item', 'sphere', 'buddy', 'costume', 'stamp', 'honor'],
        )),
  );
  renderCupCopyTargets();
  validateActivity();
}
// Mirrors gamestate.ValidateTeamBattleScorePolicy and the explore checks so errors point at the field.
function activityProblems(config) {
  const problems = [],
    intIn = (v, a, b) => Number.isInteger(v) && v >= a && v <= b;
  for (const [id, p] of Object.entries(config.cups)) {
    const name = activityEditor.names[id] || id,
      at = `圣剑杯「${name}」`;
    if (!intIn(p.end_turn, 0, 30))
      problems.push({ cup: id, field: 'turns', text: `${at}回合上限须为0–30，0表示按BOSS脚本收尾` });
    if (!intIn(p.rate, 1, 1000)) problems.push({ cup: id, field: 'rate', text: `${at}积分倍率须为1–1000%` });
    if (!intIn(p.team_cost_initial ?? 3, 3, 10))
      problems.push({ cup: id, field: 'team-cost', text: `${at}起始COST须为3–10` });
    let previous = -1;
    (p.grades || []).forEach((g, i) => {
      if (!intIn(g.score, 0, 1e12))
        problems.push({ cup: id, grade: i, text: `${at}第${i + 1}档门槛须为0–1万亿的整数` });
      else if (g.score <= previous)
        problems.push({ cup: id, grade: i, text: `${at}第${i + 1}档门槛须大于第${i}档（${num(previous)}）` });
      previous = Number.isFinite(g.score) ? Math.max(previous, g.score) : previous;
      if ((g.rewards || []).length > 10)
        problems.push({ cup: id, grade: i, text: `${at}第${i + 1}档最多10项奖励` });
      (g.rewards || []).forEach((r, j) => {
        if (!intIn(r.num, 1, 1e7))
          problems.push({
            cup: id,
            key: `cup:${i}:${j}`,
            text: `${at}第${i + 1}档「${activityRewardLabel(r)}」数量须为1–10000000`,
          });
      });
    });
  }
  config.explore.forEach((s, i) => {
    const at = `探索事件${s.event + 1}·${s.kind === 'symbols' ? '路途奖励' : '宝箱'}${s.index + 1}`;
    s.rewards.forEach((r, j) => {
      if (!intIn(r.num, 1, 1e7))
        problems.push({
          key: `explore:${i}:${j}`,
          text: `${at}「${activityRewardLabel(r)}」数量须为1–10000000`,
        });
      const c = s.chance_per_million?.[j];
      if (c !== undefined && !intIn(c, 0, 1e6))
        problems.push({
          chance: `explore:${i}:${j}`,
          text: `${at}「${activityRewardLabel(r)}」概率须为0–100%`,
        });
    });
  });
  return problems;
}
function validateActivity() {
  const root = $('#activity-rewards');
  clearInvalid(root);
  if (!activityEditor.config) return [];
  const problems = activityProblems(activityEditor.config),
    cup = $('#cup-select').value;
  for (const pr of problems) {
    if (pr.cup && pr.cup !== cup) continue;
    if (pr.field) markInvalid($('#cup-' + pr.field), pr.text);
    if (pr.grade !== undefined) markInvalid($(`[data-cup-score="${pr.grade}"]`), pr.text);
    if (pr.key) markInvalid($(`[data-activity-num="${pr.key}"]`), pr.text);
    if (pr.chance) markInvalid($(`[data-activity-chance="${pr.chance}"]`), pr.text);
  }
  markActivityTabs(problems);
  const here = problems.filter((pr) => pr.cup === cup),
    elsewhere = problems.filter((pr) => pr.cup && pr.cup !== cup).length;
  $('#cup-errors').hidden = !here.length && !elsewhere;
  $('#cup-errors').textContent = `${here
    .slice(0, 3)
    .map((pr) => pr.text)
    .join(
      '；',
    )}${here.length > 3 ? ` 等${here.length}处` : ''}${elsewhere ? `${here.length ? '；' : ''}另有 ${elsewhere} 处问题在其他圣剑杯` : ''}`;
  const explore = problems.filter((pr) => !pr.cup);
  $('#explore-errors').hidden = !explore.length;
  $('#explore-errors').textContent =
    explore
      .slice(0, 3)
      .map((pr) => pr.text)
      .join('；') + (explore.length > 3 ? ` 等${explore.length}处` : '');
  return problems;
}
function renderCupCopyTargets() {
  const current = $('#cup-select').value,
    ids = Object.keys(activityEditor.config?.cups || {}).filter((id) => id !== current);
  const checked = new Set($$('#cup-copy-targets input:checked').map((el) => el.value));
  $('#cup-copy-targets').innerHTML =
    ids
      .map(
        (id) =>
          `<label><input type="checkbox" value="${id}" ${checked.has(id) ? 'checked' : ''}><span title="${esc(activityEditor.names[id] || id)}">${esc(activityEditor.names[id] || id)}</span></label>`,
      )
      .join('') || '<span class="hint">没有其他圣剑杯</span>';
  $('#cup-copy-tool').hidden = !ids.length;
}
$('#cup-copy-all').onclick = () => $$('#cup-copy-targets input').forEach((el) => (el.checked = true));
$('#cup-copy-none').onclick = () => $$('#cup-copy-targets input').forEach((el) => (el.checked = false));
$('#cup-copy-apply').onclick = async () => {
  const source = activityEditor.config?.cups[$('#cup-select').value],
    targets = $$('#cup-copy-targets input:checked').map((el) => el.value),
    parts = new Set($$('#cup-copy-parts input:checked').map((el) => el.value));
  if (!source || !targets.length || !parts.size) return toast('请选择要复制的内容和目标副本', true);
  const labels = { score: '九档门槛', rewards: '九档奖励', limits: '回合上限、倒下结束、起始COST与倍率' };
  if (
    !(await askConfirm(
      `复制到 ${targets.length} 个圣剑杯草稿？`,
      `把「${activityEditor.names[$('#cup-select').value]}」的${[...parts].map((p) => labels[p]).join('、')}复制到：\n${targets.map((id) => activityEditor.names[id] || id).join('\n')}\n\n只修改草稿，保存前可继续逐项调整。`,
      { ok: '复制' },
    ))
  )
    return;
  for (const id of targets) {
    const t = activityEditor.config.cups[id];
    if (parts.has('limits')) {
      t.end_turn = source.end_turn;
      t.rate = source.rate;
      t.team_cost_initial = source.team_cost_initial || 3;
      t.member_dead_end = !!source.member_dead_end;
    }
    t.grades.forEach((g, i) => {
      if (parts.has('score')) g.score = source.grades[i].score;
      if (parts.has('rewards')) g.rewards = structuredClone(source.grades[i].rewards || []);
    });
  }
  activityChanged();
  validateActivity();
  $('#cup-copy-result').textContent = `已复制到 ${targets.length} 个副本草稿，请切换副本核对后保存。`;
};
$('#explore-set-chance').onclick = async () => {
  const input = $('#explore-chance-all'),
    v = Number(input.value);
  clearInvalid(input.parentElement);
  if (input.value === '' || !Number.isFinite(v) || v < 0 || v > 100) {
    markInvalid(input, '概率须为0–100');
    return toast('概率须为0至100', true);
  }
  const count = activityEditor.config.explore.reduce((n, s) => n + s.rewards.length, 0);
  if (
    !count ||
    !(await askConfirm(`把全部 ${count} 项探索奖励的掉落概率设为 ${v}%？`, '数量不变，保存后下次出发生效。', {
      ok: '应用',
    }))
  )
    return;
  activityEditor.config.explore.forEach((s, i) => {
    const c = activityChances('explore:' + i);
    c.splice(0, c.length, ...s.rewards.map(() => Math.round(v * 10000)));
  });
  activityChanged();
  renderActivityRewards();
};
$('#cup-select').onchange = () => {
  $('#cup-copy-result').textContent = '';
  renderActivityRewards();
};
$('#cup-member-dead-end').onchange = () => {
  const p = activityEditor.config?.cups[$('#cup-select').value];
  if (p) {
    p.member_dead_end = $('#cup-member-dead-end').checked;
    renderDeadEnd(p.member_dead_end);
    activityChanged();
    validateActivity();
  }
};
for (const [id, field] of [
  ['#cup-turns', 'end_turn'],
  ['#cup-rate', 'rate'],
  ['#cup-team-cost', 'team_cost_initial'],
])
  $(id).oninput = () => {
    const p = activityEditor.config?.cups[$('#cup-select').value];
    if (p) {
      p[field] = $(id).value === '' ? NaN : Number($(id).value);
      activityChanged();
      renderGradeBars(p);
      validateActivity();
    }
  };
const activityRewardText = (r) => `${activityRewardLabel(r)} ×${r.num}`;
function activityDiff() {
  const saved = activityEditor.saved,
    now = activityComparable(activityEditor.config);
  if (!saved) return [];
  const sections = [];
  for (const id of Object.keys(now.cups)) {
    const a = saved.cups[id],
      b = now.cups[id];
    if (!a || JSON.stringify(a) === JSON.stringify(b)) continue;
    const rows = diffRows([
      ['回合上限', a.end_turn, b.end_turn],
      ['倒下结束挑战', a.member_dead_end ? '开启' : '关闭', b.member_dead_end ? '开启' : '关闭'],
      ['起始COST', a.team_cost_initial, b.team_cost_initial],
      ['积分倍率（%）', a.rate, b.rate],
    ]);
    b.grades.forEach((g, i) => {
      const o = a.grades[i];
      rows.push(
        ...diffRows([[`第${i + 1}档门槛`, o.score, g.score]]),
        ...keyedDiff(
          `第${i + 1}档奖励`,
          o.rewards,
          g.rewards,
          contentKey,
          activityRewardText,
          activityRewardLabel,
        ),
      );
    });
    sections.push({ title: `圣剑杯「${activityEditor.names[id] || id}」`, rows });
  }
  const explore = [];
  const slotItems = (x) => x.rewards.map((r, j) => ({ r, c: x.chance_per_million[j] ?? 1000000 }));
  now.explore.forEach((s, i) => {
    const o = saved.explore[i],
      label = `事件${s.event + 1}·${s.kind === 'symbols' ? '路途奖励' : '宝箱'}${s.index + 1}`;
    explore.push(
      ...keyedDiff(
        label,
        slotItems(o),
        slotItems(s),
        (x) => contentKey(x.r),
        (x) => `${activityRewardText(x.r)} · ${x.c / 10000}%`,
        (x) => activityRewardLabel(x.r),
      ),
    );
  });
  sections.push({ title: '探索奖励', rows: explore });
  return sections;
}
function activityChangeSummary() {
  const saved = activityEditor.saved,
    now = activityComparable(activityEditor.config);
  if (!saved) return '';
  const cups = Object.keys(now.cups)
    .filter((id) => JSON.stringify(now.cups[id]) !== JSON.stringify(saved.cups[id]))
    .map((id) => activityEditor.names[id] || id);
  const slots = now.explore.filter((s, i) => JSON.stringify(s) !== JSON.stringify(saved.explore[i])).length;
  return `将修改：${cups.length ? `${cups.length} 个圣剑杯（${cups.slice(0, 4).join('、')}${cups.length > 4 ? ' 等' : ''}）` : '圣剑杯无变化'}；${slots ? `${slots} 个探索奖励槽` : '探索无变化'}。`;
}
$('#activity-save').onclick = () =>
  reviewedSave(
    'activity-rewards',
    () => {
      const problems = validateActivity();
      if (problems.length) {
        const first = problems[0],
          shown = first.cup
            ? activityTab === 'cup' && first.cup === $('#cup-select').value
            : activityTab === 'explore';
        const go = first.cup
          ? {
              label: '前往第一个问题副本',
              primary: true,
              onClick: () => {
                showActivityTab('cup');
                $('#cup-select').value = first.cup;
                renderActivityRewards();
              },
            }
          : { label: '前往探索奖励', primary: true, onClick: () => showActivityTab('explore') };
        showViewAlert('activity-rewards', {
          title: `不能保存：${problems.length} 处需要修正`,
          message:
            problems
              .slice(0, 6)
              .map((pr) => pr.text)
              .join('；') + (problems.length > 6 ? ' …' : ''),
          actions: shown ? [] : [go],
        });
        return null;
      }
      return {
        review: {
          title: '核对圣剑杯与探索变更',
          lead: activityChangeSummary(),
          sections: activityDiff(),
          note: '新配置用于下次出发；已开始的战斗／探索沿用进入时的规则与抽取结果，已领取档位不会重置。',
        },
      };
    },
    async () => {
      const data = await api('/api/activity-rewards', {
        method: 'PUT',
        body: JSON.stringify({ expected_revision: activityEditor.revision, config: activityEditor.config }),
      });
      activityEditor.revision = data.revision;
      activityEditor.dirty = false;
      activityEditor.saved = activityComparable(activityEditor.config);
      updateActivityTabs();
      $('#activity-version').textContent = `配置 v${data.revision}`;
      setSaveState('#activity-summary', '已保存，下次出发时生效', 'ok');
      clearViewAlert('activity-rewards');
      state.loaded.delete('audit');
      toast('奖励配置已保存');
    },
  );
// 圣剑杯 and 探索奖励 are unrelated reward sets; they share one revision and one save, so each tab
// carries its own change dot and problem mark.
let activityTab = 'cup';
function showActivityTab(tab, focus = false) {
  activityTab = tab;
  for (const name of ['cup', 'explore']) {
    const on = name === tab,
      button = $('#activity-tab-' + name);
    button.setAttribute('aria-selected', String(on));
    button.tabIndex = on ? 0 : -1;
    $('#activity-' + name).hidden = !on;
  }
  if (focus) $('#activity-tab-' + tab).focus();
}
function updateActivityTabs() {
  const config = activityEditor.config;
  if (!config) return;
  const saved = activityEditor.saved,
    now = activityComparable(config),
    cups = Object.keys(now.cups).length,
    items = now.explore.reduce((n, s) => n + s.rewards.length, 0);
  $('#activity-cup-meta').textContent = cups
    ? `${cups} 个副本 · 九档积分奖励与挑战规则`
    : '当前资源尚未配置圣剑杯';
  $('#activity-explore-meta').textContent = now.explore.length
    ? `${now.explore.length} 个事件奖励槽 · ${items} 项奖励`
    : '当前场景没有可配置的奖励槽';
  const cupDirty = !!saved && JSON.stringify(now.cups) !== JSON.stringify(saved.cups),
    exploreDirty = !!saved && JSON.stringify(now.explore) !== JSON.stringify(saved.explore);
  for (const [name, dirty] of [
    ['cup', cupDirty],
    ['explore', exploreDirty],
  ]) {
    const dot = $(`#activity-${name}-dirty`);
    dot.dataset.dirty = dirty ? '1' : '';
    dot.hidden = !dirty && !dot.classList.contains('bad');
  }
}
function markActivityTabs(problems) {
  for (const [name, count] of [
    ['cup', problems.filter((pr) => pr.cup).length],
    ['explore', problems.filter((pr) => !pr.cup).length],
  ]) {
    const dot = $(`#activity-${name}-dirty`);
    dot.classList.toggle('bad', count > 0);
    dot.hidden = !count && !dot.dataset.dirty;
    dot.title = count
      ? `${count} 处需要修正`
      : name === 'cup'
        ? '圣剑杯有未保存的修改'
        : '探索奖励有未保存的修改';
  }
}
$('#activity-rewards .page-switch').addEventListener('click', (event) => {
  const tab = event.target.closest('[data-activity-tab]');
  if (tab) showActivityTab(tab.dataset.activityTab);
});
$('#activity-rewards .page-switch').addEventListener('keydown', (event) => {
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    event.preventDefault();
    showActivityTab(activityTab === 'cup' ? 'explore' : 'cup', true);
  }
});
$('#activity-export').onclick = () => downloadContent(activityEditor.config, 'activity-rewards.json');
$('#activity-import').onchange = () =>
  contentAction(
    'activity-rewards',
    async () => {
      const value = await importContent($('#activity-import'));
      if (!value) return;
      if (
        !value.cups ||
        Object.keys(value.cups).sort().join(',') !==
          Object.keys(activityEditor.config.cups).sort().join(',') ||
        !Array.isArray(value.explore) ||
        value.explore.length !== activityEditor.config.explore.length ||
        value.explore.some((s) => !s || !Array.isArray(s.rewards)) ||
        Object.values(value.cups).some(
          (p) =>
            !p ||
            !Array.isArray(p.grades) ||
            p.grades.length !== 9 ||
            p.grades.some((g) => !g || !Array.isArray(g.rewards)),
        )
      )
        throw new Error('方案格式不正确：圣剑杯集合、九档结构或探索槽数量与当前配置不同');
      await resolveContentLabels(
        [
          ...Object.values(value.cups).flatMap((p) => p.grades.flatMap((g) => g.rewards)),
          ...value.explore.flatMap((s) => s.rewards),
        ].filter((r) => !activityCurrency(r)),
      );
      activityEditor.config = value;
      activityChanged();
      renderActivityRewards();
      toast('已载入方案到草稿，请核对后保存');
    },
    '导入',
  );

// Shared editor helpers ---------------------------------------------------------
const dropEditor = {
  kind: 'all',
  rows: [],
  towers: [],
  row: null,
  config: null,
  base: null,
  revision: 0,
  dirty: false,
  lastBoss: 0,
  modes: {},
  stash: {},
  drafts: new Map(),
};
const exchangeEditor = {
  shops: [],
  shop: null,
  currencies: [],
  selected: new Set(),
  page: 0,
  revision: 0,
  nextShop: 0,
  nextLineup: 0,
  dirty: false,
};
const contentPicker = {
  selected: new Map(),
  rows: [],
  page: 0,
  serial: 0,
  apply: null,
  accept: null,
  labels: new Map(),
  loading: false,
};
const contentKey = (r) => `${r.type ?? r.reward_type}:${r.reward_typeid ?? r.reward_type_id}`;
const contentLabel = (r) => contentPicker.labels.get(contentKey(r)) || `${r.type}:${r.reward_typeid}`;
// Small art for reward rows; honors are text only and get none, entries not resolved yet show their initial.
// Honors show their name as a title plate wherever a reward name is shown.
function contentName(r, label) {
  const info = catalogInfo.get(contentKey(r));
  return info?.kind === 'honor' ? catalogEntryName(info) : esc(label);
}
function contentThumb(r) {
  const info = catalogInfo.get(contentKey(r));
  return (info?.kind || REWARD_TYPE_KIND[r.type]) === 'honor'
    ? ''
    : `<span class="thumb thumb-sm">${info ? catalogImage(info) : image('', contentLabel(r))}</span>`;
}
const contentReward = (c) => ({
  type: c.reward_type,
  reward_typeid: c.reward_type_id,
  num: 1,
  card_lv: c.reward_type === 6 ? c.fixed_card_level || 1 : 0,
  card_fame: c.reward_type === 6 ? 1 : 0,
  card_love: 0,
  card_skill_lv: c.reward_type === 6 ? [1] : [],
});
const targetLabel = (t) => `第${t.battle_index + 1}波 · 槽${t.enemy_index + 1} ${t.name}`;
const rewardMax = (type) => ([14, 16, 18].includes(type) ? 1 : [6, 15, 19].includes(type) ? 100 : 10000000);
function contentDirty(name) {
  if (name === 'activity-rewards') return activityEditor.dirty;
  return name === 'drops'
    ? dropEditor.dirty || dropEditor.drafts.size > 0
    : name === 'exchanges'
      ? exchangeEditor.dirty
      : false;
}
function changedContent(name) {
  const edit = name === 'drops' ? dropEditor : exchangeEditor;
  edit.dirty = true;
  if (name === 'drops' && dropIsTower()) towerSaveState();
  else setSaveState(name === 'drops' ? '#drop-summary' : '#exchange-summary', '有未保存的修改', 'dirty');
  updateDraftIndicators();
}
// Validate and review outside the busy state; only the request itself locks the page.
async function reviewedSave(name, prepare, commit) {
  if (policyBusy(name)) return;
  let plan;
  try {
    plan = prepare();
  } catch (e) {
    return reportError(name, e);
  }
  if (!plan || !(await reviewChanges(plan.review))) return;
  await contentAction(name, () => commit(plan));
}
async function contentAction(name, fn, what = '保存') {
  if (policyBusy(name)) return;
  state.publishing.add(name);
  updatePolicyControls();
  try {
    await fn();
  } catch (e) {
    reportError(name, e, what);
  } finally {
    state.publishing.delete(name);
    updatePolicyControls();
  }
}
async function resolveContentLabels(rewards) {
  const keys = [
    ...new Map(
      rewards.map((r) => [
        contentKey(r),
        { reward_type: r.type, reward_type_id: r.reward_typeid, quantity: 1 },
      ]),
    ).values(),
  ];
  for (let i = 0; i < keys.length; i += 100) {
    const data = await api('/api/catalog/resolve', {
      method: 'POST',
      body: JSON.stringify({ rewards: keys.slice(i, i + 100) }),
    });
    rememberCatalogRows(data.entries || []);
  }
  for (const r of rewards) {
    const fixed = catalogInfo.get(contentKey(r))?.fixed_card_level;
    if (fixed) r.card_lv = fixed;
  }
}
function downloadContent(value, name) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
async function importContent(input) {
  try {
    const file = input.files[0];
    if (!file) return null;
    if (file.size > 1024 * 1024) throw new Error('方案文件超过1MiB');
    try {
      return JSON.parse(await file.text());
    } catch {
      throw new Error('方案文件不是有效的 JSON');
    }
  } finally {
    input.value = '';
  }
}

// Boss 掉落 ---------------------------------------------------------------------
async function loadDropEditor() {
  const [data, towers] = await Promise.all([api('/api/boss-drops'), api('/api/tower-drops')]);
  dropEditor.rows = data.bosses;
  dropEditor.towers = towers.towers;
  dropEditor.drafts.clear();
  dropEditor.dirty = false;
  towerPick.info.clear();
  if (dropEditor.kind === 'tower' && !dropEditor.towers.length) dropEditor.kind = 'all';
  const difficulty = $('#drop-difficulty').value;
  $('#drop-difficulty').innerHTML =
    '<option value="">全部难度</option>' +
    [...new Set(data.bosses.map((b) => b.difficulty))]
      .map((v) => `<option value="${esc(v)}">${esc(v)}</option>`)
      .join('');
  $('#drop-difficulty').value = difficulty;
  if ($('#drop-difficulty').selectedIndex < 0) $('#drop-difficulty').value = '';
  filterDropSelect();
  if (dropEditor.kind === 'tower')
    await openTowerFloor(
      dropIsTower() ? dropEditor.row.tower_id : towerPick.towerID,
      dropIsTower() ? dropEditor.row.floor : 0,
      true,
    );
  else
    await selectDrop(
      Number($('#drop-select').value) ||
        dropEditor.lastBoss ||
        Number($('#drop-select option:not([value=""])')?.value),
    );
}
function filterDropSelect() {
  showDropCatalogControls();
  if (dropEditor.kind === 'tower') {
    renderTowerPicker();
    return;
  }
  // The difficulty box itself is searchable (enhanceSelect); these filters only narrow what it offers.
  const id = dropEditor.row?.boss_id,
    attr = $('#drop-attribute').value,
    difficulty = $('#drop-difficulty').value;
  const rows = dropEditor.rows.filter(
    (b) =>
      (dropEditor.kind === 'all' || b.category === dropEditor.kind) &&
      (!difficulty || b.difficulty === difficulty) &&
      (!attr || b.targets.some((t) => (t.stats?.attribute || '').split('_').includes(attr))),
  );
  $('#drop-filter-count').textContent =
    rows.length === dropEditor.rows.length
      ? `共 ${rows.length} 个难度`
      : `可选 ${rows.length}/${dropEditor.rows.length} 个难度`;
  $$('#drop-kinds button').forEach((b) => b.classList.toggle('active', b.dataset.kind === dropEditor.kind));
  $('#drop-select').innerHTML =
    '<option value=""></option>' +
    rows
      .map(
        (b) => `<option value="${b.boss_id}">${esc(b.name)} · ${esc(b.difficulty)} · ${b.boss_id}</option>`,
      )
      .join('');
  $('#drop-select').value = rows.some((b) => b.boss_id === id) ? id : '';
}
$$('#drop-kinds button').forEach(
  (b) =>
    (b.onclick = () => {
      dropEditor.kind = b.dataset.kind;
      filterDropSelect();
    }),
);
async function selectDrop(id) {
  if (!id) return;
  const data = await api(`/api/boss-drops?boss_id=${id}`);
  return applyDropData(data);
}
// Ordinary bosses and tower floors share this editor; floors are keyed by tower and floor in admin_tower_drops.js.
async function applyDropData(data) {
  dropEditor.row = data.boss;
  dropEditor.base = data.base;
  dropEditor.config = structuredClone(data.config);
  dropEditor.config.enemy_drops ??= [];
  dropEditor.saved = structuredClone(dropEditor.config);
  dropEditor.revision = data.revision;
  dropEditor.dirty = false;
  syncPoolModes();
  if (!data.boss.tower_id) dropEditor.lastBoss = data.boss.boss_id;
  await resolveContentLabels([
    ...dropEditor.config.enemy_drops.map((d) => d.reward),
    ...(dropEditor.config.fame_rewards || []),
  ]);
  await resolveTowerFirstClearLabels();
  renderDropEditor();
  setSaveState('#drop-summary', '已载入，与服务器一致', 'ok');
}
function renderDropEditor() {
  const boss = dropEditor.row,
    siblings = dropSiblings(boss);
  $('#drop-target').innerHTML = boss.targets
    .map((t, i) => `<option value="${i}">${esc(targetLabel(t))}</option>`)
    .join('');
  $('#drop-copy-from').innerHTML = siblings.length
    ? siblings
        .map((b) => `<option value="${dropSiblingKey(b)}">${esc(b.difficulty)} · ${b.boss_id}</option>`)
        .join('')
    : `<option value="">${boss.tower_id ? '本塔没有其他楼层' : '无同组难度'}</option>`;
  $('#drop-copy').disabled = !siblings.length;
  $('#drop-copy-targets').innerHTML = boss.tower_id
    ? ''
    : siblings
        .map(
          (b) =>
            `<label><input type="checkbox" value="${b.boss_id}"> ${esc(b.difficulty)} · ${b.boss_id}</label>`,
        )
        .join('') || '<span class="hint">此副本没有同组其他难度</span>';
  $('#drop-current').innerHTML = boss.tower_id
    ? htmlValue(towerEditingHTML(boss))
    : `正在编辑：${esc(boss.name)} · ${esc(boss.difficulty)} <code>${boss.boss_id}</code><span class="hint">${boss.targets.length} 个掉落目标 · 保存后下次开战生效</span>`;
  if (!boss.tower_id) {
    $('#drop-save').textContent = '保存掉落配置';
    $('#drop-select').value = boss.boss_id;
  }
  $('#drop-version').textContent = `配置 v${dropEditor.revision}`;
  renderDropRows();
}
function renderDropRows() {
  renderFameRows();
  renderTowerFirstClear();
  const targets = dropEditor.row?.targets || [],
    drops = dropEditor.config?.enemy_drops || [];
  $('#drop-add').disabled = !targets.length;
  $('#drop-save').disabled = !targets.length && !dropIsTower();
  // Rows are listed by wave and slot; data-drop keeps the index into the saved array.
  const order = drops
    .map((d, i) => [d, i])
    .sort(([a, i], [b, j]) => a.battle_index - b.battle_index || a.enemy_index - b.enemy_index || i - j);
  $('#drop-rows').innerHTML =
    order
      .map(([d, i]) => {
        const pct = (d.chance_per_million ?? 1000000) / 10000;
        return `<tr data-drop="${i}" class="${dropEditor.fresh?.has(i) ? 'fresh' : ''}"><td><select data-field="target" aria-label="掉落目标">${targets.map((t, j) => `<option value="${j}" ${d.battle_index === t.battle_index && d.enemy_index === t.enemy_index ? 'selected' : ''}>${esc(targetLabel(t))}</option>`).join('')}</select></td><td class="cell-wrap"><div class="reward-cell">${contentThumb(d.reward)}<div><b>${contentName(d.reward, contentLabel(d.reward))}</b><span class="sub">ID ${d.reward.reward_typeid}</span></div></div></td><td><input data-field="num" type="number" min="1" max="${d.reward.type === 6 ? 100 : 10000000}" step="1" value="${d.reward.num}" aria-label="奖励数量"></td><td><div class="chance-cell"><span class="input-unit"><input data-field="chance" type="number" min="0" max="100" step="0.0001" value="${pct}" aria-label="掉落概率"><span>%</span></span><span data-chance-tag>${chanceTag(pct)}</span></div></td><td><button class="x-button" data-remove="${i}" aria-label="移除${esc(contentLabel(d.reward))}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`;
      })
      .join('') || emptyRow(5, '暂无掉落', '选择上方的目标后点击“为此目标添加奖励”。');
  $$('#drop-rows [data-field]').forEach(
    (el) =>
      (el.oninput = el.onchange =
        () => {
          const d = drops[Number(el.closest('tr').dataset.drop)];
          if (el.dataset.field === 'target') {
            const t = targets[Number(el.value)];
            d.battle_index = t.battle_index;
            d.enemy_index = t.enemy_index;
          } else if (el.dataset.field === 'num') d.reward.num = Number(el.value);
          else {
            d.chance_per_million = Math.round(Number(el.value) * 10000);
            el.closest('.chance-cell').querySelector('[data-chance-tag]').innerHTML = chanceTag(
              el.value === '' ? NaN : Number(el.value),
            );
          }
          el.classList.remove('invalid');
          changedContent('drops');
        }),
  );
  $$('#drop-rows [data-remove]').forEach(
    (b) =>
      (b.onclick = () => {
        drops.splice(Number(b.dataset.remove), 1);
        changedContent('drops');
        renderDropRows();
      }),
  );
}
$$('#drop-attribute,#drop-difficulty').forEach((el) => (el.onchange = filterDropSelect));
$('#drop-reset').onclick = () => {
  $('#drop-attribute').value = '';
  $('#drop-difficulty').value = '';
  dropEditor.kind = 'all';
  filterDropSelect();
};
$('#drop-select').onchange = async () => {
  const id = Number($('#drop-select').value);
  if (!id) return;
  if (!(await confirmDiscard(dropEditor.dirty, '切换难度会放弃当前难度未保存的修改。', '放弃并切换'))) {
    $('#drop-select').value = dropEditor.row.boss_id;
    return;
  }
  return contentAction('drops', () => selectDrop(id), '载入');
};
$('#drop-add').onclick = () =>
  openContentPicker((rows) => {
    const target = dropEditor.row.targets[Number($('#drop-target').value)];
    if (!target) throw new Error('请选择掉落目标');
    if (dropEditor.config.enemy_drops.length + rows.length > 120) throw new Error('每个难度最多120项');
    const start = dropEditor.config.enemy_drops.length;
    for (const row of rows)
      dropEditor.config.enemy_drops.push({
        battle_index: target.battle_index,
        enemy_index: target.enemy_index,
        reward: contentReward(row),
        chance_per_million: 1000000,
      });
    dropEditor.fresh = new Set(rows.map((_, k) => start + k));
    changedContent('drops');
    renderDropRows();
    dropEditor.fresh = null;
    $('#drop-rows tr.fresh')?.scrollIntoView({ block: 'center', behavior: 'smooth' });
  });
$('#drop-set-chance').onclick = async () => {
  const input = $('#drop-chance-all'),
    chance = Number(input.value);
  input.classList.remove('invalid');
  if (input.value === '' || !Number.isFinite(chance) || chance < 0 || chance > 100) {
    markInvalid(input, '概率须为0至100');
    return toast('概率须为0至100', true);
  }
  if (!dropEditor.config.enemy_drops.length) return;
  if (
    !(await askConfirm(
      `把本难度全部 ${dropEditor.config.enemy_drops.length} 项掉落的概率设为 ${chance}%？`,
      '',
      { ok: '应用' },
    ))
  )
    return;
  for (const d of dropEditor.config.enemy_drops) d.chance_per_million = Math.round(chance * 10000);
  changedContent('drops');
  renderDropRows();
};
$('#drop-base').onclick = async () => {
  if (!dropEditor.base) return;
  if (
    !(await askConfirm(
      dropIsTower() ? '把此楼层的掉落、名声和首通奖励恢复为包内默认？' : '载入包内默认掉落？',
      '当前草稿会被替换，保存前可继续编辑。',
      { ok: '载入默认' },
    ))
  )
    return;
  dropEditor.config = structuredClone(dropEditor.base);
  if (dropIsTower()) dropEditor.config.first_clear_rewards = null;
  dropEditor.config.enemy_drops ??= [];
  syncPoolModes();
  changedContent('drops');
  renderDropRows();
};
$('#drop-copy').onclick = () =>
  contentAction(
    'drops',
    async () => {
      const key = $('#drop-copy-from').value,
        source = dropSiblings(dropEditor.row).find((b) => String(dropSiblingKey(b)) === key);
      if (!source) return;
      if (
        !(await askConfirm(
          '用所选难度／楼层替换当前草稿？',
          '当前草稿的掉落与名声奖励会换成所选的内容，首通奖励保持不变。保存前可继续编辑。',
          { ok: '替换' },
        ))
      )
        return;
      const data = await fetchDropData(source);
      const rows = data.config.enemy_drops || [];
      if (!dropTargetsMatch(rows, dropEditor.row)) throw new Error('此难度的波次或部位不同，不能直接复制');
      await resolveContentLabels(rows.map((d) => d.reward));
      dropEditor.config.enemy_drops = structuredClone(rows);
      dropEditor.config.fame_rewards = structuredClone(data.config.fame_rewards ?? null);
      await resolveContentLabels(dropEditor.config.fame_rewards || []);
      syncPoolModes('fame');
      changedContent('drops');
      renderDropRows();
    },
    '复制',
  );
function dropTargetsMatch(rows, boss) {
  return rows.every((d) =>
    boss.targets.some((t) => t.battle_index === d.battle_index && t.enemy_index === d.enemy_index),
  );
}
$('#drop-export').onclick = () =>
  downloadContent(
    dropEditor.config,
    dropIsTower()
      ? `tower-${dropEditor.row.tower_id}-floor-${dropEditor.row.floor}-drops.json`
      : `boss-drops-${dropEditor.row.boss_id}.json`,
  );
$('#drop-import').onchange = () =>
  contentAction(
    'drops',
    async () => {
      const config = await importContent($('#drop-import'));
      if (!config) return;
      if (
        !Array.isArray(config.enemy_drops) ||
        config.enemy_drops.length > 120 ||
        !dropTargetsMatch(config.enemy_drops, dropEditor.row)
      )
        throw new Error('掉落数组或波次／部位不匹配');
      await resolveContentLabels(config.enemy_drops.map((d) => d.reward));
      if (config.fame_rewards != null && !Array.isArray(config.fame_rewards))
        throw new Error('名声奖励须为数组');
      await resolveContentLabels(config.fame_rewards || []);
      const extras = towerDropImportExtras(config);
      dropEditor.config = {
        boss_id: dropEditor.row.boss_id,
        enemy_drops: config.enemy_drops,
        fame_rewards: config.fame_rewards ?? null,
        ...extras,
      };
      await resolveTowerFirstClearLabels();
      syncPoolModes();
      changedContent('drops');
      renderDropRows();
      toast('已载入方案到草稿，请核对后保存');
    },
    '导入',
  );
function dropDiff() {
  const targets = dropEditor.row?.targets || [],
    t = (d) => targets.find((x) => x.battle_index === d.battle_index && x.enemy_index === d.enemy_index);
  const text = (d) =>
    `${t(d) ? targetLabel(t(d)) : `第${d.battle_index + 1}波·槽${d.enemy_index + 1}`} · ${contentLabel(d.reward)} ×${d.reward.num} · ${(d.chance_per_million ?? 1000000) / 10000}%`;
  const fame = (v) => (v == null ? '沿用默认奖励' : v.length ? `自定义 ${v.length} 项` : '关闭');
  const before = dropEditor.saved || { enemy_drops: [], fame_rewards: null },
    after = dropEditor.config;
  const dropKey = (d) => `${d.battle_index}:${d.enemy_index}:${contentKey(d.reward)}`;
  return [
    {
      title: '怪物／部位掉落',
      rows: keyedDiff('掉落', before.enemy_drops, after.enemy_drops, dropKey, text, (d) =>
        contentLabel(d.reward),
      ),
    },
    {
      title: '名声奖励',
      rows: [
        ...diffRows([['名声奖励模式', fame(before.fame_rewards), fame(after.fame_rewards)]]),
        ...keyedDiff(
          '名声候选',
          before.fame_rewards || [],
          after.fame_rewards || [],
          contentKey,
          (r) => `${contentLabel(r)} ×${r.num}`,
          contentLabel,
        ),
      ],
    },
    ...towerFirstClearDiff(),
  ];
}
// mark=false checks a floor draft that is not on screen (admin_tower_drops.js) without touching the page.
function dropProblems(mark = true) {
  const problems = [],
    flag = (el) => mark && markInvalid(el);
  if (mark) clearInvalid($('#drops'));
  dropEditor.config.enemy_drops.forEach((d, i) => {
    const row = $(`#drop-rows tr[data-drop="${i}"]`),
      max = d.reward.type === 6 ? 100 : 10000000,
      c = d.chance_per_million ?? 1000000;
    if (!Number.isInteger(d.reward.num) || d.reward.num < 1 || d.reward.num > max) {
      problems.push(`第${i + 1}行「${contentLabel(d.reward)}」数量须为1–${max}`);
      flag(row?.querySelector('[data-field="num"]'));
    }
    if (!Number.isInteger(c) || c < 0 || c > 1000000) {
      problems.push(`第${i + 1}行「${contentLabel(d.reward)}」概率须为0–100%`);
      flag(row?.querySelector('[data-field="chance"]'));
    }
  });
  (dropEditor.config.fame_rewards || []).forEach((r, i) => {
    const max = r.type === 6 ? 100 : 10000000;
    if (!Number.isInteger(r.num) || r.num < 1 || r.num > max) {
      problems.push(`名声奖励「${contentLabel(r)}」数量须为1–${max}`);
      flag($(`[data-fame-num="${i}"]`));
    }
  });
  if (dropEditor.modes.fame === 'custom' && !dropEditor.config.fame_rewards?.length)
    problems.push('名声奖励选了“自定义奖励池”但池子是空的：请添加奖励，或改选“关闭”');
  validateTowerFirstClear(problems, mark);
  return problems;
}
$('#drop-save').onclick = () =>
  dropIsTower()
    ? saveTowerDrops()
    : reviewedSave(
        'drops',
        () => {
          if (!dropEditor.config) return null;
          const problems = dropProblems();
          if (problems.length) {
            showViewAlert('drops', {
              title: `不能保存：${problems.length} 处需要修正`,
              message: problems.slice(0, 6).join('；'),
            });
            return null;
          }
          const ids = [
            dropEditor.row.boss_id,
            ...$$('#drop-copy-targets input:checked').map((el) => Number(el.value)),
          ];
          for (const id of ids) {
            const boss = dropEditor.rows.find((b) => b.boss_id === id);
            if (!dropTargetsMatch(dropEditor.config.enemy_drops, boss))
              throw new Error(`${boss.difficulty}的波次或部位不同，请单独配置`);
          }
          return {
            ids,
            review: {
              title: `核对掉落变更 · ${dropEditor.row.name} ${dropEditor.row.difficulty}`,
              sections: dropDiff(),
              note: `${ids.length > 1 ? `同时写入同组 ${ids.length - 1} 个难度（${ids.slice(1).join('、')}）；` : ''}仅用于后续开战，已开始的战斗保留原掉落表。`,
            },
          };
        },
        async ({ ids }) => {
          const data = await api('/api/boss-drops', {
            method: 'PUT',
            body: JSON.stringify({
              expected_revision: dropEditor.revision,
              configs: ids.map((id) => ({ ...dropEditor.config, boss_id: id })),
            }),
          });
          dropEditor.revision = data.revision;
          dropEditor.dirty = false;
          dropEditor.saved = structuredClone(dropEditor.config);
          $('#drop-version').textContent = `配置 v${data.revision}`;
          setSaveState('#drop-summary', '已保存；掉落展示和结算使用同一配置', 'ok');
          clearViewAlert('drops');
          state.loaded.delete('audit');
          toast('掉落配置已保存');
        },
      );

// 兑换所 -------------------------------------------------------------------------
async function loadExchangeEditor() {
  const data = await api('/api/exchanges');
  exchangeEditor.shops = data.shops;
  exchangeEditor.persisted = new Set(data.shops.map((s) => s.trade_shopid));
  exchangeEditor.revision = data.revision;
  exchangeEditor.nextShop = data.next_shop_id;
  exchangeEditor.nextLineup = data.next_lineup_id;
  exchangeEditor.templates = data.templates;
  if (!exchangeEditor.currencies.length) {
    const rows = await fetchAllCatalog({ kind: 'item' });
    exchangeEditor.currencies = rows;
    exchangeEditor.currencyNames = new Map(rows.map((c) => [c.reward_type_id, c.name]));
    for (const c of rows) contentPicker.labels.set(contentKey(c), c.name);
  }
  renderExchangeCurrency(Number($('#exchange-currency').value) || 4000);
  const id = exchangeEditor.shop?.trade_shopid;
  renderExchangeSelect();
  await selectExchange(
    exchangeEditor.shops.some((s) => s.trade_shopid === id) ? id : exchangeEditor.shops[0]?.trade_shopid,
  );
}
function currencyOptions(id, rows = exchangeEditor.currencies) {
  return rows
    .map(
      (c) =>
        `<option value="${c.reward_type_id}" ${id === c.reward_type_id ? 'selected' : ''}>${esc(c.name)} · ${c.reward_type_id}</option>`,
    )
    .join('');
}
const currencyName = (id) => `${exchangeEditor.currencyNames?.get(id) || '道具'} · ${id}`;
// The batch currency box is a searchable select (enhanceSelect in admin_kit.js): typing lists matches at once.
function renderExchangeCurrency(selected = Number($('#exchange-currency').value)) {
  $('#exchange-currency').innerHTML = currencyOptions(selected);
}
function exchangeStatus(s) {
  const now = Date.now() / 1000;
  return s.deleted
    ? '已删除'
    : s.disabled
      ? '关闭'
      : s.end_time <= now
        ? '已结束'
        : s.start_time > now
          ? '未开始'
          : '开放中';
}
function exchangeScheduleText(s) {
  const now = Date.now() / 1000,
    start = s.start_time || 0,
    end = s.end_time && s.end_time < 2147483647 ? s.end_time : 0;
  return `玩家可见时间：${start ? describeTime(start) : '立即'} → ${end ? describeTime(end) : '不限'}${start > now ? '（尚未开始，此前玩家看不到本兑换所）' : end && end <= now ? '（已结束）' : ''}${s.disabled ? '；当前为关闭状态，开放后才按时间生效' : ''}`;
}
function renderExchangeSelect() {
  const id = exchangeEditor.shop?.trade_shopid,
    rows = exchangeEditor.shops.filter(exchangeShopMatches);
  const placeholder =
    id || !rows.length
      ? `<option value="">${rows.length ? '请选择兑换所（当前草稿保留）' : '没有匹配的兑换所'}</option>`
      : '';
  $('#exchange-select').innerHTML =
    placeholder +
    rows
      .map(
        (s) =>
          `<option value="${s.trade_shopid}">${esc(s.name)} · ${s.trade_shopid}（${exchangeStatus(s)}）</option>`,
      )
      .join('');
  if (rows.some((s) => s.trade_shopid === id)) $('#exchange-select').value = id;
  const total = exchangeEditor.shops.length;
  $('#exchange-filter-count').textContent =
    rows.length === total ? `共 ${total} 个兑换所` : `可选 ${rows.length}/${total} 个兑换所`;
}
async function selectExchange(id) {
  const shop = exchangeEditor.shops.find((s) => s.trade_shopid === id);
  if (!shop) return;
  exchangeEditor.shop = structuredClone(shop);
  exchangeEditor.selected.clear();
  exchangeEditor.page = 0;
  exchangeEditor.dirty = false;
  $('#exchange-select').value = id;
  await resolveContentLabels(shop.lineups.flatMap((l) => l.rewards));
  renderExchangeForm();
  setSaveState(
    '#exchange-summary',
    shop.deleted ? '已删除：兑换记录保留，恢复后默认关闭' : '已载入 · 按商品 ID 保留每位玩家的累计兑换次数',
    shop.deleted ? 'dirty' : 'ok',
  );
}
function renderExchangeForm() {
  const s = exchangeEditor.shop,
    status = exchangeStatus(s);
  $('#exchange-current').innerHTML =
    `正在编辑：${esc(s.name)} <code>${s.trade_shopid}</code> <span class="tag ${s.deleted ? 'bad' : status === '开放中' ? 'ok' : 'warn'}">${status}</span>${exchangeEditor.persisted?.has(s.trade_shopid) ? '' : '<span class="tag info">新建未保存</span>'}<span class="hint">${s.lineups.length} 项商品 · ${s.lineups.filter((l) => !l.disabled).length} 项上架</span>`;
  $('#exchange-fields').disabled = !!s.deleted;
  $('#exchange-delete').hidden = !!s.deleted;
  $('#exchange-delete').disabled = !exchangeEditor.persisted?.has(s.trade_shopid);
  $('#exchange-restore').hidden = !s.deleted;
  $('#exchange-name').value = s.name;
  $('#exchange-text').value = s.text;
  $('#exchange-enabled').checked = !s.disabled;
  $('#exchange-tab').value = s.tab_type;
  $('#exchange-start').value = s.start_time ? localTimeInput(s.start_time) : '';
  $('#exchange-end').value = s.end_time && s.end_time < 2147483647 ? localTimeInput(s.end_time) : '';
  $('#exchange-schedule').textContent = exchangeScheduleText(s);
  $('#exchange-version').textContent = `配置 v${exchangeEditor.revision}`;
  renderExchangeRows();
}
function filteredOffers() {
  const q = $('#exchange-search').value.trim().toLowerCase(),
    status = $('#exchange-offer-status').value;
  return (exchangeEditor.shop?.lineups || []).filter(
    (l) =>
      (!q ||
        `${l.lineup_name} ${l.lineupid} ${l.rewards[0].reward_typeid} ${contentLabel(l.rewards[0])}`
          .toLowerCase()
          .includes(q)) &&
      (!status || (status === 'on') === !l.disabled),
  );
}
function updateExchangeSelection() {
  const n = exchangeEditor.selected.size;
  $('#exchange-selection').textContent = `已选 ${n} 项 / 全店 ${exchangeEditor.shop?.lineups.length || 0} 项`;
  for (const id of ['#exchange-batch', '#exchange-on', '#exchange-off']) $(id).disabled = !n;
}
function renderExchangeRows() {
  const rows = filteredOffers();
  exchangeEditor.page = renderPager('exchange', exchangeEditor.page, rows.length, 30, (page) => {
    exchangeEditor.page = page;
    renderExchangeRows();
  });
  updateExchangeSelection();
  $('#exchange-rows').innerHTML =
    rows
      .slice(exchangeEditor.page * 30, (exchangeEditor.page + 1) * 30)
      .map(
        (l) =>
          `<tr data-offer="${l.lineupid}" class="${exchangeEditor.selected.has(l.lineupid) ? 'selected' : ''}"><td><input data-field="select" type="checkbox" aria-label="选择商品 ${esc(l.lineup_name)}" ${exchangeEditor.selected.has(l.lineupid) ? 'checked' : ''}></td><td><label class="switch"><input data-field="enabled" type="checkbox" aria-label="上架 ${esc(l.lineup_name)}" ${l.disabled ? '' : 'checked'}></label></td><td><div class="reward-cell">${contentThumb(l.rewards[0])}<div><input class="offer-name" data-field="name" maxlength="60" value="${esc(l.lineup_name)}" aria-label="商品名称"><span class="sub">${esc(contentLabel(l.rewards[0]))} · ID ${l.rewards[0].reward_typeid} · 商品 ${l.lineupid}</span></div></div></td><td><input data-field="num" type="number" min="1" max="${rewardMax(l.rewards[0].type)}" value="${l.rewards[0].num}" aria-label="商品数量"></td><td><button type="button" class="currency-button" data-currency-open title="更换兑换货币">${esc(currencyName(l.prices[0].id))}</button></td><td><input data-field="price" type="number" min="1" max="10000000" value="${l.prices[0].num}" aria-label="单价"></td><td><input data-field="stock" type="number" min="1" max="999999" value="${l.stock_num === -1 ? '' : l.stock_num}" placeholder="不限" aria-label="每人限兑" title="留空为不限量"></td></tr>`,
      )
      .join('') ||
    emptyRow(
      7,
      exchangeEditor.shop?.lineups.length ? '没有匹配的商品' : '本店还没有商品',
      '使用上方“从目录批量添加商品”加入草稿。',
    );
}
// Delegated so rows stay light: the full currency list (hundreds of items) is only built for the row being edited.
function onExchangeRowEdit(e) {
  const el = e.target,
    row = el.closest('tr[data-offer]');
  if (!row || !el.dataset.field) return;
  const id = Number(row.dataset.offer),
    l = exchangeEditor.shop.lineups.find((l) => l.lineupid === id);
  switch (el.dataset.field) {
    case 'select':
      el.checked ? exchangeEditor.selected.add(id) : exchangeEditor.selected.delete(id);
      row.classList.toggle('selected', el.checked);
      updateExchangeSelection();
      return;
    case 'enabled':
      l.disabled = !el.checked;
      break;
    case 'name':
      l.lineup_name = el.value.trim();
      break;
    case 'num':
      l.rewards[0].num = Number(el.value);
      break;
    case 'currency':
      l.prices[0].id = Number(el.value);
      break;
    case 'price':
      l.prices[0].num = Number(el.value);
      break;
    case 'stock':
      l.stock_num = el.value === '' ? -1 : Number(el.value);
      break;
  }
  el.classList.remove('invalid');
  changedContent('exchanges');
}
$('#exchange-rows').addEventListener('input', onExchangeRowEdit);
$('#exchange-rows').addEventListener('change', onExchangeRowEdit);
$('#exchange-rows').addEventListener('click', (e) => {
  const b = e.target.closest('[data-currency-open]');
  if (!b) return;
  const l = exchangeEditor.shop.lineups.find((l) => l.lineupid === Number(b.closest('tr').dataset.offer));
  const select = document.createElement('select');
  select.dataset.field = 'currency';
  select.dataset.search = '搜索货币名称或 ID';
  select.setAttribute('aria-label', '兑换货币');
  select.innerHTML = currencyOptions(l.prices[0].id);
  b.replaceWith(select);
  enhanceSelect(select).focus();
});
function exchangeForm() {
  const s = exchangeEditor.shop;
  s.name = $('#exchange-name').value.trim();
  s.text = $('#exchange-text').value.trim();
  s.disabled = !$('#exchange-enabled').checked;
  s.tab_type = Number($('#exchange-tab').value);
  const start = unixInput('#exchange-start');
  if (start) s.start_time = start;
  else delete s.start_time;
  s.end_time = unixInput('#exchange-end') || 2147483647;
  return s;
}
[
  '#exchange-name',
  '#exchange-text',
  '#exchange-enabled',
  '#exchange-tab',
  '#exchange-start',
  '#exchange-end',
].forEach(
  (id) =>
    ($(id).oninput = $(id).onchange =
      () => {
        exchangeForm();
        $(id).classList.remove('invalid');
        $('#exchange-schedule').textContent = exchangeScheduleText(exchangeEditor.shop);
        changedContent('exchanges');
      }),
);
$('#exchange-select').onchange = async () => {
  const id = Number($('#exchange-select').value);
  if (!id) return;
  if (!(await confirmDiscard(exchangeEditor.dirty, '切换兑换所会放弃当前未保存的修改。', '放弃并切换'))) {
    $('#exchange-select').value = exchangeEditor.shop.trade_shopid;
    return;
  }
  return contentAction('exchanges', () => selectExchange(id), '载入');
};
async function newExchange(copy) {
  if (
    !(await confirmDiscard(
      exchangeEditor.dirty,
      copy ? '副本带有页面上的当前内容；原兑换所未保存的修改不会保存。' : '新建会放弃当前未保存的修改。',
      copy ? '复制' : '放弃并新建',
    ))
  )
    return;
  const shop = copy
    ? structuredClone(exchangeForm())
    : {
        name: '新兑换所',
        text: '常驻兑换，库存按账号累计。',
        shop_type: 1,
        tab_type: 1,
        end_time: 2147483647,
        is_new: 0,
        pictid: 0,
        lineups: [],
        evidence: 'LOCAL_POLICY_ADMIN',
      };
  shop.trade_shopid = exchangeEditor.nextShop++;
  shop.disabled = true;
  delete shop.deleted;
  if (copy) {
    shop.name = `${shop.name}副本`.slice(0, 40);
    for (const l of shop.lineups) l.lineupid = exchangeEditor.nextLineup++;
  }
  exchangeEditor.shops.push(shop);
  renderExchangeSelect();
  exchangeEditor.shop = structuredClone(shop);
  exchangeEditor.selected.clear();
  exchangeEditor.page = 0;
  $('#exchange-select').value = shop.trade_shopid;
  renderExchangeForm();
  changedContent('exchanges');
  toast(
    copy
      ? '已复制为新兑换所草稿（新商品ID，默认关闭），保存后生效'
      : '已新建兑换所草稿（默认关闭），添加商品后保存',
  );
}
$('#exchange-new').onclick = () => newExchange(false);
$('#exchange-clone').onclick = () => newExchange(true);
for (const [id, restore] of [
  ['#exchange-delete', false],
  ['#exchange-restore', true],
])
  $(id).onclick = () =>
    contentAction(
      'exchanges',
      async () => {
        const shop = exchangeEditor.shop;
        if (!shop) return;
        if (
          !(await askConfirm(
            `${restore ? '恢复' : '删除'}「${shop.name}」？`,
            `${restore ? '恢复后保持关闭，请检查商品再开放。' : '玩家将无法进入或兑换；历史限购保留，可从已删除列表恢复。'}${exchangeEditor.dirty ? '\n未保存的修改将放弃。' : ''}`,
            { ok: restore ? '恢复' : '删除', danger: !restore },
          ))
        )
          return;
        await api(`/api/exchanges/${shop.trade_shopid}${restore ? '/restore' : ''}`, {
          method: restore ? 'POST' : 'DELETE',
          body: JSON.stringify({ expected_revision: exchangeEditor.revision }),
        });
        exchangeEditor.dirty = false;
        await loadExchangeEditor();
        clearViewAlert('exchanges');
        state.loaded.delete('audit');
        toast(restore ? '已恢复，当前关闭' : '已删除，历史兑换记录保留');
      },
      restore ? '恢复' : '删除',
    );
function exchangeDefaults() {
  const price = Number($('#exchange-price').value),
    stock = $('#exchange-stock').value.trim() === '' ? -1 : Number($('#exchange-stock').value),
    currency = Number($('#exchange-currency').value);
  clearInvalid($('#exchanges .batch-box'));
  const bad = [];
  if (!Number.isInteger(price) || price < 1 || price > 10000000) bad.push('#exchange-price');
  if (!Number.isInteger(stock) || stock === 0 || stock < -1 || stock > 999999) bad.push('#exchange-stock');
  if (!currency) bad.push('#exchange-currency');
  if (bad.length) {
    bad.forEach((id) => markInvalid($(id)));
    throw new Error('请检查货币、单价（1–10000000）和限兑数量（留空为不限量，或填 1–999999）');
  }
  return { price, stock, currency };
}
function addExchangeOffers(rows) {
  const d = exchangeDefaults(),
    s = exchangeEditor.shop;
  if (s.lineups.length + rows.length > 500) throw new Error('每个兑换所最多500项');
  let added = 0,
    skipped = 0;
  for (const row of rows) {
    const reward = contentReward(row);
    if (s.lineups.some((l) => contentKey(l.rewards[0]) === contentKey(reward) && !l.disabled)) {
      skipped++;
      continue;
    }
    const id = exchangeEditor.nextLineup++;
    s.lineups.push({
      lineupid: id,
      lineup_name: row.name,
      stock_num: d.stock,
      is_lineup_new: 0,
      is_lineup_old: 0,
      pictid: 0,
      prices: [{ type: 4, id: d.currency, num: d.price, point_card_condition: [] }],
      rewards: [reward],
      evidence: 'LOCAL_POLICY_ADMIN',
    });
    exchangeEditor.selected.add(id);
    added++;
  }
  changedContent('exchanges');
  renderExchangeForm();
  toast(`已加入 ${added} 项商品草稿${skipped ? `，跳过 ${skipped} 项本店已上架的奖励` : ''}`);
}
$('#exchange-add').onclick = () => {
  try {
    exchangeDefaults();
  } catch (e) {
    return toast(e.message, true);
  }
  openContentPicker(addExchangeOffers, [
    'material',
    'card',
    'item',
    'sphere',
    'buddy',
    'costume',
    'stamp',
    'honor',
  ]);
};
$('#exchange-currency').onchange = () => $('#exchange-currency').classList.remove('invalid');
for (const kind of ['fusion', 'evolution'])
  $('#exchange-' + kind).onclick = () => {
    try {
      const rows = exchangeEditor.templates[kind] || [];
      for (const c of rows) contentPicker.labels.set(contentKey(c), c.name);
      addExchangeOffers(rows);
    } catch (e) {
      toast(e.message, true);
    }
  };
$('#exchange-search').oninput = $('#exchange-offer-status').onchange = () => {
  exchangeEditor.page = 0;
  renderExchangeRows();
};
$('#exchange-select-page').onclick = () => {
  filteredOffers()
    .slice(exchangeEditor.page * 30, (exchangeEditor.page + 1) * 30)
    .forEach((l) => exchangeEditor.selected.add(l.lineupid));
  renderExchangeRows();
};
$('#exchange-clear').onclick = () => {
  exchangeEditor.selected.clear();
  renderExchangeRows();
};
$('#exchange-batch').onclick = async () => {
  try {
    const d = exchangeDefaults(),
      n = exchangeEditor.selected.size;
    if (!n) return;
    const name = currencyName(d.currency);
    if (
      !(await askConfirm(
        `修改所选的 ${n} 项商品？`,
        `改为：${name} × ${d.price}，每人限兑 ${d.stock === -1 ? '不限' : d.stock}。\n已兑换次数保留。`,
        { ok: '应用' },
      ))
    )
      return;
    for (const l of exchangeEditor.shop.lineups)
      if (exchangeEditor.selected.has(l.lineupid)) {
        l.prices = [{ type: 4, id: d.currency, num: d.price, point_card_condition: [] }];
        l.stock_num = d.stock;
      }
    changedContent('exchanges');
    renderExchangeRows();
  } catch (e) {
    toast(e.message, true);
  }
};
for (const [id, disabled] of [
  ['#exchange-on', false],
  ['#exchange-off', true],
])
  $(id).onclick = () => {
    for (const l of exchangeEditor.shop.lineups)
      if (exchangeEditor.selected.has(l.lineupid)) l.disabled = disabled;
    changedContent('exchanges');
    renderExchangeForm();
  };
$('#exchange-export').onclick = () =>
  downloadContent(exchangeForm(), `exchange-${exchangeEditor.shop.trade_shopid}.json`);
$('#exchange-import').onchange = () =>
  contentAction(
    'exchanges',
    async () => {
      const shop = await importContent($('#exchange-import'));
      if (!shop) return;
      if (
        !Array.isArray(shop.lineups) ||
        shop.lineups.length > 500 ||
        shop.lineups.some(
          (l) =>
            !Array.isArray(l.rewards) ||
            l.rewards.length !== 1 ||
            !Array.isArray(l.prices) ||
            l.prices.length !== 1,
        )
      )
        throw new Error('商品格式无效');
      if (shop.trade_shopid !== exchangeEditor.shop.trade_shopid)
        throw new Error('方案与当前兑换所ID不同；如需复制请使用“复制为新兑换所”');
      await resolveContentLabels(shop.lineups.flatMap((l) => l.rewards));
      exchangeEditor.shop = shop;
      for (const l of shop.lineups)
        exchangeEditor.nextLineup = Math.max(exchangeEditor.nextLineup, l.lineupid + 1);
      exchangeEditor.selected.clear();
      renderExchangeForm();
      changedContent('exchanges');
      toast('已载入方案到草稿，请核对后保存');
    },
    '导入',
  );
// Mirrors validateExchange so the operator sees which product is wrong instead of one generic server error.
function exchangeDiff(before, after) {
  const time = (v) => (v && v < 2147483647 ? describeTime(v) : '不限'),
    stock = (v) => (v === -1 ? '不限' : v);
  const shopRows = diffRows([
    ['名称', before?.name, after.name],
    ['说明', before?.text, after.text],
    ['开放兑换所', before ? !before.disabled : null, !after.disabled],
    ['游戏分类', before ? (before.tab_type ? '活动' : '普通') : null, after.tab_type ? '活动' : '普通'],
    [
      '开始时间',
      before ? (before.start_time ? describeTime(before.start_time) : '立即') : null,
      after.start_time ? describeTime(after.start_time) : '立即',
    ],
    ['结束时间', before ? time(before.end_time) : null, time(after.end_time)],
  ]);
  const old = new Map((before?.lineups || []).map((l) => [l.lineupid, l])),
    offers = [];
  for (const l of after.lineups) {
    const b = old.get(l.lineupid),
      label = `商品 ${l.lineupid}「${l.lineup_name}」`;
    if (!b) {
      offers.push([
        `${label}（新增）`,
        '',
        `${contentLabel(l.rewards[0])} ×${l.rewards[0].num} · ${currencyName(l.prices[0].id)} × ${l.prices[0].num} · 限兑 ${stock(l.stock_num)}${l.disabled ? ' · 下架' : ''}`,
      ]);
      continue;
    }
    offers.push(
      ...diffRows([
        [`${label} 名称`, b.lineup_name, l.lineup_name],
        [`${label} 上架`, !b.disabled, !l.disabled],
        [`${label} 数量`, b.rewards[0].num, l.rewards[0].num],
        [`${label} 货币`, currencyName(b.prices[0].id), currencyName(l.prices[0].id)],
        [`${label} 单价`, b.prices[0].num, l.prices[0].num],
        [`${label} 限兑`, stock(b.stock_num), stock(l.stock_num)],
      ]),
    );
  }
  return [
    { title: '兑换所', rows: shopRows },
    { title: '商品', rows: offers },
  ];
}
function exchangeProblems(shop) {
  const problems = [];
  clearInvalid($('#exchanges'));
  if (!shop.name) {
    problems.push('兑换所名称不能为空');
    markInvalid($('#exchange-name'));
  }
  if (shop.start_time && shop.end_time && shop.end_time < 2147483647 && shop.start_time >= shop.end_time) {
    problems.push('开始时间须早于结束时间');
    markInvalid($('#exchange-end'));
  }
  if (!shop.lineups.length) problems.push('至少需要一项商品');
  shop.lineups.forEach((l) => {
    const label = `商品 ${l.lineupid}「${l.lineup_name || contentLabel(l.rewards[0])}」`,
      max = rewardMax(l.rewards[0].type),
      row = $(`#exchange-rows tr[data-offer="${l.lineupid}"]`);
    const check = (ok, field, text) => {
      if (!ok) {
        problems.push(`${label}${text}`);
        markInvalid(row?.querySelector(`[data-field="${field}"]`));
      }
    };
    check(!!l.lineup_name && l.lineup_name.length <= 60, 'name', '名称不能为空且不超过60字');
    check(
      Number.isInteger(l.rewards[0].num) && l.rewards[0].num >= 1 && l.rewards[0].num <= max,
      'num',
      `数量须为1–${max}`,
    );
    check(
      Number.isInteger(l.prices[0].num) && l.prices[0].num >= 1 && l.prices[0].num <= 10000000,
      'price',
      '单价须为1–10000000',
    );
    check(
      Number.isInteger(l.stock_num) && l.stock_num !== 0 && l.stock_num >= -1 && l.stock_num <= 999999,
      'stock',
      '限兑须留空（不限）或为1–999999',
    );
  });
  return problems;
}
$('#exchange-save').onclick = () =>
  reviewedSave(
    'exchanges',
    () => {
      const shop = exchangeForm(),
        problems = exchangeProblems(shop);
      if (problems.length) {
        showViewAlert('exchanges', {
          title: `不能保存：${problems.length} 处需要修正`,
          message: problems.slice(0, 6).join('；') + (problems.length > 6 ? ' …' : ''),
        });
        return null;
      }
      const saved = exchangeEditor.persisted?.has(shop.trade_shopid)
        ? exchangeEditor.shops.find((s) => s.trade_shopid === shop.trade_shopid)
        : null;
      return {
        shop,
        review: {
          title: `核对兑换所变更 · ${shop.name}`,
          lead: saved ? '' : '这是新建的兑换所，以下为全部内容。',
          sections: exchangeDiff(saved, shop),
          note: `${shop.disabled ? '保存后兑换所保持关闭' : `开放，${shop.lineups.filter((l) => !l.disabled).length} 项上架商品`}；已有兑换次数按商品 ID 保留。`,
        },
      };
    },
    async ({ shop }) => {
      const data = await api(`/api/exchanges/${shop.trade_shopid}`, {
        method: 'PUT',
        body: JSON.stringify({ expected_revision: exchangeEditor.revision, shop }),
      });
      exchangeEditor.revision = data.revision;
      exchangeEditor.shop = data.shop;
      exchangeEditor.persisted.add(shop.trade_shopid);
      const i = exchangeEditor.shops.findIndex((s) => s.trade_shopid === shop.trade_shopid);
      exchangeEditor.shops[i] = structuredClone(data.shop);
      exchangeEditor.dirty = false;
      renderExchangeForm();
      renderExchangeSelect();
      $('#exchange-select').value = shop.trade_shopid;
      $('#exchange-version').textContent = `配置 v${data.revision}`;
      setSaveState('#exchange-summary', '已保存；玩家重新打开兑换所后可见', 'ok');
      clearViewAlert('exchanges');
      state.loaded.delete('audit');
      toast('兑换所已保存');
    },
  );

// 通用奖励选择器 ---------------------------------------------------------------------
const pickerKindNames = {
  currency: '货币',
  card: '卡牌',
  material: '素材卡',
  item: '道具',
  sphere: '召唤石',
  buddy: '传承卡',
  costume: '变身皮肤',
  stamp: '对话／表情',
  stamp_dialog: '对话',
  stamp_image: '表情',
  stamp_voice: '语音表情',
  honor: '称号',
};
function catalogKindQuery(kind, stampKind = '') {
  return kind === 'stamp' ? { kind, stamp_kind: stampKind } : { kind };
}
// Honors are text only, so they show as a title plate instead of a thumbnail; 10000000 is the official blank title.
// Voice stamps carry a speaker badge on thumbnails, or an inline mark in lists without one.
const voiceMark = (cls) =>
  `<span class="${cls}" role="img" aria-label="语音表情" title="带语音"><svg class="icon"><use href="#i-megaphone"/></svg></span>`;
function catalogEntryName(c, inline = false) {
  if (c.kind === 'honor')
    return c.name
      ? `<span class="honor-plate">${esc(c.name)}</span>`
      : '<span class="honor-plate blank">空白称号</span>';
  return esc(c.name) + (inline && c.stamp_kind === 'voice' ? ' ' + voiceMark('stamp-voice') : '');
}
function catalogImage(c, badge = false) {
  return c.stamp_kind === 'dialog'
    ? `<span class="dialog-bubble">${esc(c.name)}</span>`
    : image(c.image_url, c.name) + (badge && c.stamp_kind === 'voice' ? voiceMark('voice-badge') : '');
}
function catalogThumb(c) {
  return c.kind === 'honor' ? '' : `<div class="thumb">${catalogImage(c, true)}</div>`;
}
// Stamp and honor details only repeat the kind and where players use it.
function catalogFacts(c) {
  return c.kind === 'stamp' || c.kind === 'honor'
    ? ''
    : `<span class="facts-text">${esc(c.kind === 'card' ? cardSourceDescription(c) : c.detail || '')}</span>`;
}
// options.accept(entry) returns true, or a reason string that disables the entry; options.title renames the dialog.
function openContentPicker(
  apply,
  kinds = ['material', 'card', 'item'],
  { accept = null, title = '选择奖励' } = {},
) {
  const names = pickerKindNames;
  $('#content-kind').innerHTML = kinds.map((k) => `<option value="${k}">${names[k]}</option>`).join('');
  $('#content-search').value = '';
  $('#content-stamp-kind').value = '';
  $('#content-picker-title').textContent = title;
  contentPicker.apply = apply;
  contentPicker.accept = accept;
  contentPicker.selected.clear();
  contentPicker.page = 0;
  $('#content-picker').classList.add('open');
  renderContentCatalog();
  loadContentCatalog();
}
function pickerRefusal(c) {
  if (c.resource_state === 'unavailable') return '资源暂不可用';
  const verdict = contentPicker.accept ? contentPicker.accept(c) : true;
  return verdict === true ? '' : verdict || '不可选择';
}
function updatePickerApply() {
  const n = contentPicker.selected.size;
  $('#content-apply').disabled = contentPicker.loading || !n;
  $('#content-apply').textContent = n ? `加入已选奖励（${n}）` : '加入已选奖励';
}
async function loadContentCatalog() {
  const serial = ++contentPicker.serial;
  contentPicker.loading = true;
  updatePickerApply();
  const isCard = $('#content-kind').value === 'card';
  $('#content-job').hidden = !isCard;
  $('#content-rarity').hidden = !isCard;
  $('#content-stamp-kind').hidden = $('#content-kind').value !== 'stamp';
  $('#content-catalog').classList.toggle('honor-grid', $('#content-kind').value === 'honor');
  $('#content-catalog').setAttribute('aria-busy', 'true');
  try {
    const kind = $('#content-kind').value,
      p = new URLSearchParams({
        ...cardFilterQuery('content', kind === 'card'),
        ...catalogKindQuery(kind, $('#content-stamp-kind').value),
        q: $('#content-search').value.trim(),
        limit: '48',
        offset: String(contentPicker.page * 48),
        arthur_type: kind === 'card' ? $('#content-job').value : '0',
      });
    const data = await api(`/api/catalog?${p}`);
    if (serial !== contentPicker.serial) return;
    contentPicker.rows = data.entries;
    for (const c of data.entries) contentPicker.labels.set(contentKey(c), c.name);
    contentPicker.page = renderPager('content', contentPicker.page, data.total, 48, (page) => {
      contentPicker.page = page;
      loadContentCatalog();
    });
    renderContentCatalog();
  } catch (e) {
    if (serial !== contentPicker.serial) return;
    contentPicker.rows = [];
    $('#content-catalog').innerHTML = `<div class="empty"><b>目录加载失败</b>${esc(errorText(e))}</div>`;
    toast(errorText(e), true);
  } finally {
    if (serial === contentPicker.serial) {
      contentPicker.loading = false;
      $('#content-catalog').removeAttribute('aria-busy');
      updatePickerApply();
    }
  }
}
function catalogMeta(c) {
  const id = c.kind === 'currency' ? '' : `<code>${c.reward_type_id}</code>`;
  return c.kind === 'card'
    ? `${id}<span>${cardJobName(c.arthur_type)}</span><span class="card-stars">${cardStarsHTML(c)}</span>${c.fixed_card_level ? '<span>获得即满级</span>' : ''}`
    : c.kind === 'honor'
      ? id
      : `${id}<span>${esc(pickerKindNames[c.kind === 'stamp' ? 'stamp_' + c.stamp_kind : c.kind] || '')}</span>`;
}
function renderContentCatalog() {
  const selectedHere = contentPicker.rows.filter((c) => contentPicker.selected.has(contentKey(c))).length;
  $('#content-selected').textContent = `已选 ${contentPicker.selected.size} 项`;
  $('#content-page-summary').textContent = contentPicker.rows.length
    ? `本页 ${contentPicker.rows.length} 项 · 其中已选 ${selectedHere} 项`
    : '';
  $('#content-catalog').innerHTML =
    contentPicker.rows
      .map(
        (c) =>
          `<button type="button" class="catalog-card ${contentPicker.selected.has(contentKey(c)) ? 'selected' : ''}${c.kind === 'honor' ? ' no-thumb' : ''}" data-key="${contentKey(c)}" aria-pressed="${contentPicker.selected.has(contentKey(c))}" ${pickerRefusal(c) ? `disabled title="${esc(pickerRefusal(c))}"` : ''}>${catalogThumb(c)}<div class="catalog-copy"><b class="name" title="${esc(c.name)}">${catalogEntryName(c)}</b><span class="meta">${catalogMeta(c)}</span>${catalogFacts(c)}</div></button>`,
      )
      .join('') ||
    (contentPicker.loading
      ? '<div class="empty">正在加载…</div>'
      : '<div class="empty"><b>没有匹配的奖励</b>换个关键词或调整类型与筛选条件。</div>');
  $$('#content-catalog [data-key]').forEach(
    (b) =>
      (b.onclick = () => {
        const c = contentPicker.rows.find((c) => contentKey(c) === b.dataset.key);
        contentPicker.selected.has(b.dataset.key)
          ? contentPicker.selected.delete(b.dataset.key)
          : contentPicker.selected.set(b.dataset.key, c);
        renderContentCatalog();
      }),
  );
  const list = [...contentPicker.selected.entries()];
  $('#content-selected-list').innerHTML =
    list
      .map(
        ([key, c]) =>
          `<li>${c.kind === 'honor' ? '' : `<span class="thumb">${catalogImage(c)}</span>`}<span class="sel-name" title="${esc(c.name)}">${catalogEntryName(c, true)}</span>${c.kind === 'currency' ? `<span class="tag">货币</span>` : `<code>${c.reward_type_id}</code>`}<button type="button" class="x-button" data-unselect="${esc(key)}" aria-label="移除${esc(c.name)}"><svg class="icon"><use href="#i-close"/></svg></button></li>`,
      )
      .join('') || '<li class="empty">尚未选择。点击左侧条目加入，再次点击取消。</li>';
  $$('#content-selected-list [data-unselect]').forEach(
    (b) =>
      (b.onclick = () => {
        contentPicker.selected.delete(b.dataset.unselect);
        renderContentCatalog();
      }),
  );
  updatePickerApply();
}
['#content-kind', '#content-job', '#content-rarity', '#content-stamp-kind'].forEach(
  (id) =>
    ($(id).onchange = () => {
      contentPicker.page = 0;
      loadContentCatalog();
    }),
);
let contentSearchTimer;
$('#content-search').oninput = () => {
  clearTimeout(contentSearchTimer);
  contentSearchTimer = setTimeout(() => {
    contentPicker.page = 0;
    loadContentCatalog();
  }, 200);
};
$('#content-select-page').onclick = () => {
  contentPicker.rows
    .filter((c) => !pickerRefusal(c))
    .forEach((c) => contentPicker.selected.set(contentKey(c), c));
  renderContentCatalog();
};
$('#content-clear').onclick = async () => {
  if (
    contentPicker.selected.size > 5 &&
    !(await askConfirm(`清空已选的 ${contentPicker.selected.size} 项？`, '', { ok: '清空', danger: true }))
  )
    return;
  contentPicker.selected.clear();
  renderContentCatalog();
};
$('#content-cancel').onclick = () => {
  $('#content-picker').classList.remove('open');
  contentPicker.serial++;
  contentPicker.loading = false;
};
$('#content-apply').onclick = () => {
  if (!contentPicker.selected.size) return;
  try {
    rememberCatalogRows([...contentPicker.selected.values()]);
    contentPicker.apply([...contentPicker.selected.values()]);
    $('#content-picker').classList.remove('open');
  } catch (e) {
    toast(e.message, true);
  }
};

$('#exchange-unselect-page').onclick = () => {
  filteredOffers()
    .slice(exchangeEditor.page * 30, (exchangeEditor.page + 1) * 30)
    .forEach((l) => exchangeEditor.selected.delete(l.lineupid));
  renderExchangeRows();
};
$('#content-unselect-page').onclick = () => {
  contentPicker.rows.forEach((c) => contentPicker.selected.delete(contentKey(c)));
  renderContentCatalog();
};

// Fame and tower first-clear pools: null inherits the packaged rewards, [] gives nothing, a list replaces them.
const POOL_FIELDS = { fame: 'fame_rewards', first: 'first_clear_rewards' };
const poolMode = (v) => (v == null ? 'default' : v.length ? 'custom' : 'off');
function syncPoolModes(...keys) {
  if (!keys.length) {
    dropEditor.modes = {};
    dropEditor.stash = {};
  }
  for (const k of keys.length ? keys : Object.keys(POOL_FIELDS))
    dropEditor.modes[k] = poolMode(dropEditor.config?.[POOL_FIELDS[k]]);
}
// Leaving 自定义 keeps its list, so switching back restores it instead of starting over.
function setPoolMode(key, mode, fresh) {
  const field = POOL_FIELDS[key],
    was = dropEditor.modes[key];
  if (!dropEditor.config || was === mode) return;
  if (was === 'custom') dropEditor.stash[key] = dropEditor.config[field];
  dropEditor.config[field] =
    mode === 'default'
      ? null
      : mode === 'off'
        ? []
        : dropEditor.stash[key]?.length
          ? dropEditor.stash[key]
          : fresh();
  dropEditor.modes[key] = mode;
  changedContent('drops');
}
function renderFameRows() {
  const pool = dropEditor.config?.fame_rewards,
    mode = dropEditor.modes.fame || 'default';
  $$('#fame-mode input').forEach((el) => {
    el.checked = el.value === mode;
    el.disabled = !dropEditor.config;
  });
  $('#fame-add').hidden = mode !== 'custom';
  $('#fame-rows').innerHTML =
    mode === 'default'
      ? emptyRow(3, '沿用默认名声奖励', '名声宝箱从副本默认掉落中抽取；要指定奖励请选“自定义奖励池”。')
      : mode === 'off'
        ? emptyRow(3, '已关闭名声奖励', '保存后名声宝箱不再发放物品；怪物掉落照常。')
        : pool
            .map(
              (r, i) =>
                `<tr><td class="cell-wrap"><div class="reward-cell">${contentThumb(r)}<div><b>${contentName(r, contentLabel(r))}</b><span class="sub">ID ${r.reward_typeid}</span></div></div></td><td><input data-fame-num="${i}" type="number" min="1" max="${r.type === 6 ? 100 : 10000000}" value="${r.num}" aria-label="名声奖励数量"></td><td><button class="x-button" data-fame-remove="${i}" aria-label="移除${esc(contentLabel(r))}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`,
            )
            .join('') || emptyRow(3, '奖励池还是空的', '点击“添加名声奖励”；不想发放请选“关闭”。');
  $$('[data-fame-num]').forEach(
    (el) =>
      (el.oninput = el.onchange =
        () => {
          pool[Number(el.dataset.fameNum)].num = Number(el.value);
          el.classList.remove('invalid');
          changedContent('drops');
        }),
  );
  $$('[data-fame-remove]').forEach(
    (el) =>
      (el.onclick = () => {
        pool.splice(Number(el.dataset.fameRemove), 1);
        changedContent('drops');
        renderFameRows();
      }),
  );
}
$('#fame-mode').onchange = (e) => {
  setPoolMode('fame', e.target.value, () => []);
  renderFameRows();
};
$('#fame-add').onclick = () =>
  openContentPicker((rows) => {
    if (!dropEditor.config) throw new Error('请选择难度');
    const pool = (dropEditor.config.fame_rewards ??= []);
    if (pool.length + rows.length > 120) throw new Error('名声候选最多120项');
    pool.push(...rows.map(contentReward));
    changedContent('drops');
    renderFameRows();
  });
