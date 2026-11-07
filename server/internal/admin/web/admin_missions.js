'use strict';
// 任务与成就: series list | series settings, task outline and task inspector | preview → publish.
// The working copy is compared with the saved draft (stableJSON), so undoing an edit clears the
// unsaved state. Freezing, validation and catalog rules stay with POST /api/mission-series.

const MISSION_CONDITIONS = {
  card_fusion: {
    group: 'event',
    jump: 'exp_fusion',
    name: '强化卡牌',
    unit: '次',
    title: (t) => `强化卡牌 ${t} 次`,
    hint: '统计开放并解锁后新完成的强化；解锁前的强化不计入。',
  },
  card_evolution: {
    group: 'event',
    jump: 'evo_fusion',
    name: '进化卡牌',
    unit: '次',
    subject: 'card',
    sets: true,
    optionalSubject: true,
    title: (t) => `进化卡牌 ${t} 次`,
    hint: '统计解锁后的新进化，可限定进化结果卡牌。目标集合按不同的进化结果去重，每张至少一次。',
  },
  explore_clear: {
    group: 'event',
    jump: 'explore',
    name: '完成探索',
    unit: '次',
    title: (t) => `完成探索 ${t} 次`,
    hint: '统计解锁后完成的探索。',
  },
  quest_clear: {
    group: 'event',
    jump: 'mainquest',
    name: '成功通关',
    unit: '次',
    filters: true,
    title: (t) => `成功通关 ${t} 次`,
    hint: '统计解锁后成功通关的战斗，可按 BOSS、模式、职业、人数和积分筛选；跳过券默认不计。',
  },
  pvp_played: {
    group: 'event',
    jump: 'arena',
    name: '竞技场对局',
    unit: '局',
    title: (t) => `参加竞技场 ${t} 局`,
    hint: '统计已受理并提交的竞技场对局。',
  },
  player_level: {
    group: 'state',
    name: '玩家等级',
    unit: '级',
    title: (t) => `等级达到 ${t}`,
    hint: '读取存档当前等级，达到即可领取。',
  },
  collection_count: {
    group: 'state',
    name: '图鉴收集数量',
    unit: '种',
    title: (t) => `图鉴收集 ${t} 种`,
    hint: '读取当前图鉴数量。成就链的下一档直接沿用当前数量，不从零开始。',
  },
  login_days: {
    group: 'state',
    name: '累计登录天数',
    unit: '天',
    title: (t) => `累计登录 ${t} 天`,
    hint: '直接读取自动登录奖励的累计天数，不另建计数。',
  },
  login_streak: {
    group: 'state',
    name: '连续登录天数',
    unit: '天',
    title: (t) => `连续登录 ${t} 天`,
    hint: '从接入后实际进入 Home 的日期起连续计数；断日归零，已达成的档位保留。',
  },
  pvp_rank: {
    group: 'state',
    name: '竞技场段位',
    unit: '',
    targetLabel: '段位',
    title: (t) => `竞技场段位达到${missionRankName(t)}`,
    hint: '读取当前竞技场段位；达到所选段位或更高即可领取。',
  },
  card_owned: {
    group: 'object',
    name: '持有指定卡牌',
    subject: 'card',
    sets: true,
    fixedTarget: true,
    title: () => '获得指定卡牌',
    hint: '读取当前是否持有该卡牌（含素材卡）；集合要求每张都持有。',
  },
  card_fame: {
    group: 'object',
    name: '指定卡牌最高名声',
    subject: 'card',
    sets: true,
    unit: '名声',
    title: (t) => `卡牌名声达到 ${t}`,
    hint: '读取该卡牌达到过的最高名声；集合要求每张都达到。',
  },
  quest_cleared: {
    group: 'object',
    name: '指定 BOSS 已通关',
    subject: 'boss',
    sets: true,
    fixedTarget: true,
    title: () => '通关指定 BOSS',
    hint: '读取存档中该主线关卡的通关标记。期间限定和活动的 BOSS 任务请用「成功通关」并按 BOSS 筛选。',
  },
  story_cleared: {
    group: 'object',
    name: '指定剧情已完成',
    subject: 'story',
    sets: true,
    fixedTarget: true,
    title: () => '完成指定剧情',
    hint: '只认剧情完成位；已解锁但未完成不计。',
  },
};

const MISSION_GROUPS = [
  ['event', '动作计数 · 解锁后的新动作'],
  ['state', '存档状态'],
  ['object', '指定目标'],
];
// The client's three mission tabs (tab_type 2 stays hidden). The tab decides the series type, its
// conditions and time rules; 期间限定 and 活动 show each task's remaining time.
const MISSION_TABS = [
  [0, '普通', '常驻成就：只读取存档状态和指定目标，长期有效，不重置'],
  [1, '期间限定', '任务：只用动作计数；须开启每日重置或填写结束时间'],
  [3, '活动', '任务：只用动作计数；须填写结束时间，可另开每日重置'],
];
const missionTabName = (tab) => MISSION_TABS.find(([v]) => v === tab)?.[1] || `页签 ${tab}`;
const missionTimedTab = (tab) => tab === 1 || tab === 3;
// Achievements read what the save already holds; timed tasks count only actions made while open, so a
// state read would pay out at once to everyone who already had it.
const missionGroupAllowed = (group, kind) => (group === 'event') === (kind === 'mission');
const missionGroupProblem = (kind) =>
  kind === 'mission' ? '期间限定和活动页签只能使用动作计数' : '普通页签的成就不能使用动作计数';

// Where the 挑战 button of an unfinished task leads: plain client commands without IDs. Action counts
// suggest their own page (MISSION_CONDITIONS.jump); state achievements show no button.
const MISSION_JUMPS = [
  ['', '不显示挑战按钮'],
  ['mainquest', '主线关卡'],
  ['eventboss', '活动 BOSS'],
  ['pastboss', '往期 BOSS'],
  ['towerquest', '塔'],
  ['explore', '探索（点击后立即开始探索）'],
  ['exp_fusion', '卡牌强化'],
  ['evo_fusion', '卡牌进化'],
  ['card_collection', '图鉴'],
  ['gacha', '扭蛋'],
  ['arena', '竞技场'],
  ['mainstory', '主线剧情'],
  ['substory', '角色剧情'],
  ['eventstory', '活动剧情'],
  ['exchange', '兑换所'],
];
function missionJumpName(command, rule) {
  const destination = command === 'teambattle' && rule?.event === 'quest_clear' ? 'mainquest' : command;
  return MISSION_JUMPS.find(([value]) => value === destination)?.[1] || destination;
}
const QUEST_SCOPES = [
  ['', '不限'],
  ['solo', '单人'],
  ['multiplayer', '组队'],
  ['host', '组队房主'],
  ['alchemist', '炼金'],
];
const MISSION_STATUS = {
  new: ['未保存的新系列', 'warn'],
  draft: ['草稿', 'info'],
  active: ['开放中', 'ok'],
  scheduled: ['待开始', 'info'],
  ended: ['已结束', 'muted'],
  disabled: ['已停用', 'warn'],
  archived: ['已归档', 'muted'],
};
const MISSION_PROGRESS_STATE = [
  ['进行中', 'muted'],
  ['可领奖', 'ok'],
  ['已领取', 'info'],
];

const MISSION_TEMPLATES = [
  {
    id: 'collection',
    name: '图鉴成就链',
    note: '普通 · 成就链 · 图鉴 50／100／200 种',
    series: { name: '图鉴收集', kind: 'achievement', layout: 'chain' },
    tasks: [
      ['collection_count', 50],
      ['collection_count', 100],
      ['collection_count', 200],
    ],
  },
  {
    id: 'level',
    name: '等级成就链',
    note: '普通 · 成就链 · 等级 10／30／50',
    series: { name: '等级成长', kind: 'achievement', layout: 'chain' },
    tasks: [
      ['player_level', 10],
      ['player_level', 30],
      ['player_level', 50],
    ],
  },
  {
    id: 'login',
    name: '累计登录成就链',
    note: '普通 · 成就链 · 累计 7／30／100 天',
    series: { name: '累计登录', kind: 'achievement', layout: 'chain' },
    tasks: [
      ['login_days', 7],
      ['login_days', 30],
      ['login_days', 100],
    ],
  },
  {
    id: 'daily',
    name: '每日任务',
    note: '期间限定 · 独立目标 · 每日 00:00（UTC+8）重置',
    series: { name: '每日任务', kind: 'mission', layout: 'independent', tab_type: 1, daily: true },
    tasks: [
      ['quest_clear', 3],
      ['card_fusion', 1],
      ['explore_clear', 1],
    ],
  },
  {
    id: 'chain',
    name: '活动任务链',
    note: '活动 · 任务链 · 填写结束时间后可发布',
    series: { name: '活动任务', kind: 'mission', layout: 'chain', tab_type: 3 },
    tasks: [
      ['quest_clear', 1],
      ['quest_clear', 5],
      ['quest_clear', 10],
    ],
  },
  {
    id: 'blank',
    name: '空白系列',
    note: '期间限定 · 独立目标 · 一项任务',
    series: { name: '新任务系列', kind: 'mission', layout: 'independent', tab_type: 1 },
    tasks: [['quest_clear', 1]],
  },
];

const missionsPage = {
  rows: [], // [{ draft, published? }] from the server
  revision: 0,
  current: null, // working copy of one MissionSeries
  savedJSON: '', // stableJSON of the saved draft; '' while the series is new
  task: 0, // index of the task in the inspector
  tab: 'edit', // 'edit' | 'preview'
  preview: null, // { json, series } as validated by the server
};

// Model ------------------------------------------------------------------------------------------

const missionRuleKey = (rule) => rule?.event || rule?.metric || '';
const missionCondition = (rule) => MISSION_CONDITIONS[missionRuleKey(rule)];
const missionRecord = (id) => (id ? missionsPage.rows.find((r) => r.draft.series_id === id) : null);
const missionCurrentRecord = () => missionRecord(missionsPage.current?.series_id);
const missionDirty = () =>
  !!missionsPage.current && stableJSON(missionsPage.current) !== missionsPage.savedJSON;
editorDirtyChecks.missions = missionDirty;

function missionStatus(record) {
  if (!record) return 'new';
  const live = record.published;
  if (live?.archived || (!live && record.draft.archived)) return 'archived';
  if (!live) return 'draft';
  if (!live.enabled) return 'disabled';
  const now = Date.now() / 1000;
  if (live.starts_at > now) return 'scheduled';
  if (live.ends_at && live.ends_at <= now) return 'ended';
  return 'active';
}

/** A published series whose saved draft has text or visibility changes not yet published. */
const missionPending = (record) =>
  !!record?.published && stableJSON(record.draft) !== stableJSON(record.published);

const missionLocked = () => !!missionCurrentRecord()?.published;
const missionArchived = () => missionStatus(missionCurrentRecord()) === 'archived';

function newMissionRule(key, target = 1) {
  const condition = MISSION_CONDITIONS[key];
  const value = condition.fixedTarget ? 1 : Math.max(1, target);
  return condition.group === 'event'
    ? { kind: '', event: key, target: value }
    : { kind: 'state', event: '', metric: key, target: value };
}

function missionDefaultTitle(rule) {
  const condition = missionCondition(rule);
  return condition ? condition.title(rule.kind === 'targets' ? rule.minimum || 1 : rule.target) : '';
}

const missionDefaultJump = (rule) => missionCondition(rule)?.jump || '';
const MISSION_AUTO_JUMP = '@condition';
// The editor remembers follow mode for the current draft. Stored commands reopen as fixed choices.
const autoMissionJumps = new WeakSet();

function newMissionTask(key, target) {
  const rule = newMissionRule(key, target);
  const task = {
    missionid: 0,
    title: missionDefaultTitle(rule),
    description: '',
    open_url: missionDefaultJump(rule),
    view_prio: 0,
    rewards: [],
    rule,
  };
  autoMissionJumps.add(task);
  return task;
}

function missionTargetUnit(rule) {
  const condition = missionCondition(rule);
  return condition?.targetLabel ? '' : condition?.unit || '';
}

/** One-line description of a rule for the outline, previews and the publish review. */
function missionConditionText(rule) {
  const key = missionRuleKey(rule);
  const condition = MISSION_CONDITIONS[key];
  if (!condition) return '未选择条件';
  const isSet = rule.kind === 'targets';
  const targets = rule.targets || [];
  const subjectName = (id) =>
    condition.subject === 'card' ? cardSubjectLabel(id) : missionTargetLabel(condition.subject, id);
  let text;
  if (isSet) {
    const list = targets.length ? `${targets.length} 个指定目标` : '指定目标（未选择）';
    text =
      key === 'card_fame'
        ? `${list}名声均 ≥ ${rule.minimum || 1}`
        : key === 'card_evolution'
          ? `进化出 ${list}（每个至少一次）`
          : `${condition.name}：${list}全部完成`;
  } else if (condition.group === 'object') {
    const subject = rule.subject_id ? subjectName(rule.subject_id) : '未选择目标';
    text = key === 'card_fame' ? `「${subject}」名声 ≥ ${rule.target}` : `${condition.name}「${subject}」`;
  } else if (key === 'pvp_rank') {
    text = `${condition.name} ≥${missionRankName(rule.target)}`;
  } else if (condition.group === 'state') {
    text = `${condition.name} ≥ ${rule.target}${missionTargetUnit(rule)}`;
  } else {
    text = `${condition.name} ${rule.target} ${condition.unit}`;
    if (key === 'card_evolution' && rule.subject_id) text += `（进化为「${subjectName(rule.subject_id)}」）`;
  }
  const filters = [
    rule.boss_ids?.length && missionBossFilterText(rule.boss_ids),
    rule.quest_scope && QUEST_SCOPES.find(([v]) => v === rule.quest_scope)?.[1],
    rule.arthur_type && cardJobName(rule.arthur_type),
    rule.min_players && `至少 ${rule.min_players} 名真人`,
    rule.own_decks_only && '四职业自卡组',
    rule.score_grade && `积分档位 ≥ ${rule.score_grade}`,
    rule.include_skip && '计入跳过券',
  ].filter(Boolean);
  return filters.length ? `${text} · ${filters.join(' · ')}` : text;
}

/** The condition as an operator note under a title: only what the title does not already say. */
function missionConditionNote(task) {
  const text = missionConditionText(task.rule);
  const title = task.title.trim();
  // A generated title already names the condition; the note keeps the targets and filters.
  const said =
    title === missionDefaultTitle(task.rule) ? [title, missionCondition(task.rule)?.name] : [title];
  const prefix = said.find((words) => words && text.startsWith(words));
  return prefix ? text.slice(prefix.length).replace(/^[\s·]+/, '') : text;
}

function missionCardIDs(series) {
  return series.tasks.flatMap((t) => {
    const condition = missionCondition(t.rule);
    if (condition?.subject !== 'card') return [];
    return [t.rule.subject_id, ...(t.rule.targets || [])].filter(Boolean);
  });
}

/**
 * Page-tab rules as [field, message] pairs: 'tab', 'period' or 'time', shown next to that setting.
 */
function missionTabProblems(series) {
  const problems = [];
  const tab = series.tab_type;
  if (tab === 0) {
    if (series.kind !== 'achievement') problems.push(['tab', '普通页签只能使用成就']);
    if (series.daily) problems.push(['period', '普通成就不按日重置']);
    if (series.starts_at || series.ends_at) problems.push(['time', '普通成就长期有效，请清空开放时间']);
  } else if (missionTimedTab(tab)) {
    if (series.kind !== 'mission') {
      problems.push(['tab', `${missionTabName(tab)}页签使用任务，不能使用成就`]);
    }
    if (tab === 1 && !series.daily && !series.ends_at) {
      problems.push(['time', '期间限定须开启每日重置或填写结束时间']);
    }
    if (tab === 3 && !series.ends_at) problems.push(['time', '活动须填写结束时间']);
  } else problems.push(['tab', '请选择页签']);
  return problems;
}

/** What a task still needs before preview and publish; drafts may be saved without these. */
function missionTaskProblems(task, series = missionsPage.current) {
  const rule = task.rule;
  const problems = [];
  const condition = missionCondition(rule);
  const scope =
    condition && !missionGroupAllowed(condition.group, series.kind)
      ? missionGroupProblem(series.kind)
      : missionTargetScopeProblem(task, series.tab_type);
  if (scope) problems.push(scope);
  if (!task.title.trim()) problems.push('缺少标题');
  if (
    (rule.kind === 'targets' && !rule.targets?.length) ||
    (condition?.group === 'object' && rule.kind !== 'targets' && !rule.subject_id)
  ) {
    problems.push('尚未选择目标');
  }
  if (rule.kind !== 'targets' && rule.target < 1) problems.push('目标值须为正整数');
  if (!task.rewards.length) problems.push('至少需要 1 份奖励');
  return problems;
}

function missionProblems(series, ready) {
  const problems = [];
  const name = series.name.trim();
  if (!name || [...name].length > 80) problems.push('系列名称须为 1–80 字');
  if (!series.tasks.length || series.tasks.length > 100) problems.push('每个系列须有 1–100 项任务');
  if (series.ends_at && series.ends_at <= series.starts_at) problems.push('结束时间须晚于开始时间');
  if (ready) problems.push(...missionTabProblems(series).map(([, message]) => message));
  series.tasks.forEach((task, i) => {
    const label = `第 ${i + 1} 项`;
    const rule = task.rule;
    if ([...task.title].length > 80 || [...task.description].length > 300) {
      problems.push(`${label}标题最多 80 字、描述最多 300 字`);
    }
    if (!Number.isInteger(rule.target) || rule.target < 0) problems.push(`${label}目标值须为正整数`);
    if (!ready) return;
    problems.push(...missionTaskProblems(task, series).map((problem) => `${label}${problem}`));
    problems.push(...rewardProblems(task.rewards, `${label}：`));
  });
  return problems;
}

// Target pickers --------------------------------------------------------------------------------
// BOSS, story and arena rank IDs come from /api/mission-targets: the same lists the server validates
// against, so operators pick names instead of typing IDs. A BOSS group is a grid of its difficulties
// against the standard and own-deck modes. 主线 stages belong to 普通 achievements; every other BOSS
// belongs to the timed tabs, as the server checks on preview and publish.

const missionTargets = { loading: null, bosses: new Map(), stories: new Map(), ranks: [] };

const MISSION_TARGET_KINDS = {
  boss: [
    ['', '全部'],
    ['boss', '活动'],
    ['past', '往期'],
    ['material', '素材'],
    ['tower', '塔'],
  ],
  story: [
    ['', '全部'],
    ['main', '主线'],
    ['alchemist', '炼金'],
    ['quest', '七圣王'],
    ['cn_main', '国服专属'],
    ['sub', '角色'],
    ['event', '活动'],
  ],
};
const MISSION_TARGET_GROUPS = 120;
// The server takes at most 100 IDs in a target set and in a BOSS filter (game mission catalog).
const MISSION_TARGET_MAX = 100;

function loadMissionTargets() {
  missionTargets.loading ||= api('/api/mission-targets').then(
    (data) => {
      for (const boss of data.bosses) missionTargets.bosses.set(boss.boss_id, boss);
      for (const story of data.stories) missionTargets.stories.set(story.story_id, story);
      missionTargets.ranks = data.pvp_ranks || [];
    },
    (error) => {
      missionTargets.loading = null;
      throw error;
    },
  );
  return missionTargets.loading;
}

function missionBossLabel(id) {
  const boss = missionTargets.bosses.get(id);
  if (!boss) return `BOSS ${id}`;
  const name = boss.difficulty ? `${boss.group} · ${boss.difficulty}` : boss.group || `BOSS ${id}`;
  return boss.own_deck_of ? `${name} · 自卡组` : name;
}

function missionStoryLabel(id) {
  const story = missionTargets.stories.get(id);
  return story ? `${story.group} · ${story.title}` : `剧情 ${id}`;
}

function missionRankName(rank) {
  const found = missionTargets.ranks.find((r) => r.rank_id === rank);
  return ` ${found ? found.name : rank}`;
}

const missionTargetLabel = (kind, id) => (kind === 'boss' ? missionBossLabel(id) : missionStoryLabel(id));

/** 主线 stages for 普通, everything else for the timed tabs; unknown IDs are left to the server. */
function missionBossInScope(id, tab) {
  const boss = missionTargets.bosses.get(id);
  return !boss || (boss.category === 'main') === (tab === 0);
}

/** Targets a task may not name: BOSSes outside the series' page tab; stories outside 普通 or as a set. */
function missionTargetScopeProblem(task, tab) {
  const rule = task.rule;
  if (rule.metric === 'story_cleared' && tab !== 0) return '剧情条件只用于普通页签的成就';
  // Most stories open one after another, so one story says what a set of them would.
  if (rule.metric === 'story_cleared' && rule.kind === 'targets') return '剧情条件只能指定一段剧情';
  // 主线 stages open one after another, so a set of them says no more than its last stage.
  if (tab === 0 && rule.metric === 'quest_cleared' && rule.kind === 'targets')
    return '普通页签只能指定一个主线 BOSS';
  const ids = [...(rule.boss_ids || [])];
  if (rule.metric === 'quest_cleared')
    ids.push(...(rule.targets || []), ...(rule.subject_id ? [rule.subject_id] : []));
  const outside = ids.filter((id) => !missionBossInScope(id, tab));
  if (!outside.length) return '';
  return tab === 0 ? '普通页签只能选择主线 BOSS' : '主线 BOSS 属于普通页签的成就';
}

function missionBossFilterText(ids) {
  const first = missionBossLabel(ids[0]);
  return ids.length > 1 ? `${first} 等 ${ids.length} 个 BOSS` : first;
}

/** Picked targets as chips with a 选择 button; `field` is subject_id, targets or boss_ids. */
function missionTargetChipsHTML(kind, field, ids, { locked, empty }) {
  return html`
    <div class="target-chips">
      ${ids.map(
        (id) => html`
          <span class="target-chip">
            ${missionTargetLabel(kind, id)}<code>${id}</code>
            <button type="button" class="x-button" data-ms-pick-remove="${id}" data-ms-pick-field="${field}"
              aria-label="移除 ${id}" ${attr(locked, 'disabled')}>
              <svg class="icon"><use href="#i-close" /></svg>
            </button>
          </span>
        `,
      )}
      ${!ids.length && html`<span class="target-empty">${empty}</span>`}
      <button type="button" class="add-chip" data-ms-pick="${kind}" data-ms-pick-field="${field}" ${attr(locked, 'disabled')}>
        <svg class="icon"><use href="#i-plus" /></svg>${ids.length ? '修改' : '选择'}${kind === 'boss' ? ' BOSS' : '剧情'}
      </button>
    </div>
  `;
}

const targetPicker = { kind: 'boss', field: '', tab: 0, selected: [], category: '', query: '' };

async function openMissionTargetPicker(kind, field) {
  try {
    await loadMissionTargets();
  } catch (error) {
    return toast(`目标目录读取失败：${error.message}`, true);
  }
  const rule = missionTask().rule;
  const tab = missionsPage.current.tab_type;
  Object.assign(targetPicker, {
    kind,
    field,
    tab,
    selected: field === 'subject_id' ? (rule.subject_id ? [rule.subject_id] : []) : [...(rule[field] || [])],
    category: '',
    query: '',
  });
  const noun = kind === 'boss' ? ' BOSS' : '剧情';
  const scope =
    kind === 'boss' ? (tab === 0 ? '普通页签只列主线关卡。' : '主线关卡属于普通页签，这里不列出。') : '';
  $('#ms-target-title').textContent = `选择${kind === 'boss' ? ' BOSS' : '剧情'}`;
  $('#ms-target-lead').textContent =
    (field === 'subject_id'
      ? `点选一个${noun}。`
      : field === 'boss_ids'
        ? `可多选，最多 ${MISSION_TARGET_MAX} 个：通关其中任意一个都计数；标准和自卡组分开勾选；不选表示不限。`
        : `可多选，最多 ${MISSION_TARGET_MAX} 个：每个${noun}都须完成。`) + scope;
  $('#ms-target-search').value = '';
  // 普通 lists 主线 stages only, so it has no category to switch.
  const kinds = kind === 'boss' && tab === 0 ? [] : MISSION_TARGET_KINDS[kind];
  setHTML($('#ms-target-kinds'), kinds.length ? segmented('ms-target-kind', '', kinds) : '');
  renderMissionTargetPicker();
  $('#ms-target-picker').classList.add('open');
}

/** Groups for the list: a BOSS group keeps its difficulties in catalog order with their own-deck twins. */
function missionTargetGroups() {
  const { kind, tab, category, query } = targetPicker;
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const groups = new Map();
  if (kind === 'boss') {
    for (const boss of missionTargets.bosses.values()) {
      if (boss.own_deck_of || !missionBossInScope(boss.boss_id, tab)) continue;
      if (category && boss.category !== category) continue;
      const key = `${boss.category}:${boss.group_id || boss.group}`;
      if (!groups.has(key)) groups.set(key, { name: boss.group, cells: [], ownDeck: false });
      groups.get(key).cells.push({ id: boss.boss_id, name: boss.difficulty || `BOSS ${boss.boss_id}` });
    }
    for (const boss of missionTargets.bosses.values()) {
      const source = boss.own_deck_of && missionTargets.bosses.get(boss.own_deck_of);
      if (!source) continue;
      const group = groups.get(`${source.category}:${source.group_id || source.group}`);
      const cell = group?.cells.find((c) => c.id === source.boss_id);
      if (cell) {
        cell.ownDeck = boss.boss_id;
        group.ownDeck = true;
      }
    }
  } else {
    for (const story of missionTargets.stories.values()) {
      if (category && story.category !== category) continue;
      const key = `${story.category}:${story.group}`;
      if (!groups.has(key)) groups.set(key, { name: story.group, cells: [], ownDeck: false });
      groups.get(key).cells.push({ id: story.story_id, name: story.title });
    }
  }
  return [...groups.values()].filter((group) => {
    const text = [group.name, ...group.cells.flatMap((c) => [c.name, c.id, c.ownDeck || ''])]
      .join(' ')
      .toLowerCase();
    return words.every((word) => text.includes(word));
  });
}

function renderMissionTargetPicker() {
  const single = targetPicker.field === 'subject_id';
  const picked = new Set(targetPicker.selected);
  const groups = missionTargetGroups();
  const cell = (id, name) => html`
    <label class="mt-cell">
      <input type="${single ? 'radio' : 'checkbox'}" name="mt-pick" value="${id}" ${attr(picked.has(id), 'checked')} />
      <span>${name}</span>
    </label>
  `;
  const row = (label, cells) => html`
    <div class="mt-mode">
      ${label && html`<span class="mt-mode-name">${label}</span>`}
      <div class="mt-cells">${cells}</div>
    </div>
  `;
  setHTML(
    $('#ms-target-list'),
    html`
      ${groups.slice(0, MISSION_TARGET_GROUPS).map((group) => {
        const ids = group.cells.flatMap((c) => [c.id, ...(c.ownDeck ? [c.ownDeck] : [])]);
        const all = ids.every((id) => picked.has(id));
        return html`
          <section class="mt-group">
            <div class="mt-group-head">
              <b>${group.name || '未命名'}</b>
              ${
                !single &&
                html`<button type="button" class="secondary sm" data-mt-group="${ids.join(',')}" data-mt-on="${!all}">
                  ${all ? '取消本组' : '本组全选'}
                </button>`
              }
            </div>
            ${
              group.ownDeck
                ? html`
                    ${row(
                      '标准',
                      group.cells.map((c) => cell(c.id, c.name)),
                    )}
                    ${row(
                      '自卡组',
                      group.cells.map((c) =>
                        c.ownDeck
                          ? cell(c.ownDeck, c.name)
                          : html`<span class="mt-cell none">${c.name}</span>`,
                      ),
                    )}
                  `
                : row(
                    '',
                    group.cells.map((c) => cell(c.id, c.name)),
                  )
            }
          </section>
        `;
      })}
      ${!groups.length && html`<p class="ws-list-empty">没有匹配的目标。</p>`}
      ${
        groups.length > MISSION_TARGET_GROUPS &&
        html`<p class="hint">已显示前 ${MISSION_TARGET_GROUPS} 组，共 ${groups.length} 组；输入名称或编号缩小范围。</p>`
      }
    `,
  );
  renderMissionTargetSelected();
}

function renderMissionTargetSelected() {
  const { kind, field, selected } = targetPicker;
  const over = selected.length > MISSION_TARGET_MAX;
  $('#ms-target-count').textContent =
    field === 'subject_id'
      ? `已选 ${selected.length} 项`
      : `已选 ${selected.length} / ${MISSION_TARGET_MAX} 项`;
  $('#ms-target-error').textContent = over
    ? `最多 ${MISSION_TARGET_MAX} 个，当前 ${selected.length} 个；请移除 ${selected.length - MISSION_TARGET_MAX} 个`
    : '';
  $('#ms-target-error').hidden = !over;
  $('#ms-target-apply').disabled = over;
  setHTML(
    $('#ms-target-selected'),
    selected.length
      ? selected.map(
          (id) => html`
            <li>
              <span class="sel-name">${missionTargetLabel(kind, id)}</span><code>${id}</code>
              <button type="button" class="x-button" data-mt-unpick="${id}" aria-label="移除 ${id}">
                <svg class="icon"><use href="#i-close" /></svg>
              </button>
            </li>
          `,
        )
      : html`<li class="empty">${targetPicker.field === 'boss_ids' ? '未选择：不限 BOSS' : '尚未选择'}</li>`,
  );
}

function pickMissionTarget(id, on) {
  if (targetPicker.field === 'subject_id') targetPicker.selected = on ? [id] : [];
  else if (on && !targetPicker.selected.includes(id)) targetPicker.selected.push(id);
  else if (!on) targetPicker.selected = targetPicker.selected.filter((x) => x !== id);
  renderMissionTargetSelected();
}

function applyMissionTargetPicker() {
  const { field, selected } = targetPicker;
  if (selected.length > MISSION_TARGET_MAX) return;
  const rule = missionTask().rule;
  if (field === 'subject_id') rule.subject_id = selected[0] || 0;
  else {
    rule[field] = [...selected];
    if (field === 'targets') rule.target = selected.length;
  }
  $('#ms-target-picker').classList.remove('open');
  renderMissionInspector();
  missionTouched();
}

function removeMissionTarget(id, field) {
  const rule = missionTask().rule;
  if (field === 'subject_id') rule.subject_id = 0;
  else {
    rule[field] = (rule[field] || []).filter((x) => x !== id);
    if (field === 'targets') rule.target = rule.targets.length;
  }
  renderMissionInspector();
  missionTouched();
}

$('#ms-target-search').addEventListener('input', (event) => {
  targetPicker.query = event.target.value;
  renderMissionTargetPicker();
});
$('#ms-target-kinds').addEventListener('change', (event) => {
  targetPicker.category = event.target.value;
  renderMissionTargetPicker();
});
$('#ms-target-list').addEventListener('change', (event) => {
  if (event.target.name !== 'mt-pick') return;
  pickMissionTarget(Number(event.target.value), event.target.checked);
  if (targetPicker.field !== 'subject_id') renderMissionTargetPicker();
});
$('#ms-target-list').addEventListener('click', (event) => {
  const button = event.target.closest('[data-mt-group]');
  if (!button) return;
  const on = button.dataset.mtOn === 'true';
  for (const id of button.dataset.mtGroup.split(',').map(Number)) pickMissionTarget(id, on);
  renderMissionTargetPicker();
});
$('#ms-target-selected').addEventListener('click', (event) => {
  const button = event.target.closest('[data-mt-unpick]');
  if (!button) return;
  pickMissionTarget(Number(button.dataset.mtUnpick), false);
  renderMissionTargetPicker();
});
$('#ms-target-clear').addEventListener('click', () => {
  targetPicker.selected = [];
  renderMissionTargetPicker();
});
$('#ms-target-cancel').addEventListener('click', () => $('#ms-target-picker').classList.remove('open'));
$('#ms-target-apply').addEventListener('click', applyMissionTargetPicker);

// Loading and selection ----------------------------------------------------------------------------

async function loadMissionSeries() {
  const data = await api('/api/mission-series');
  missionsPage.rows = data.series;
  missionsPage.revision = data.revision;
  selectMissionRecord(missionRecord(missionsPage.current?.series_id) || data.series[0] || null);
  // Older servers have no target catalog; their IDs stay readable as plain numbers.
  await Promise.all([
    resolveMissionLabels(data.series.map((r) => r.draft)),
    loadMissionTargets().catch(() => {}),
  ]);
  renderMissions();
}

async function resolveMissionLabels(seriesList) {
  await resolveRewardInfo(seriesList.flatMap((s) => s.tasks.flatMap((t) => t.rewards)));
  await resolveCardSubjects([...new Set(seriesList.flatMap(missionCardIDs))]);
}

function selectMissionRecord(record) {
  missionsPage.current = record ? structuredClone(record.draft) : null;
  missionsPage.savedJSON = record ? stableJSON(record.draft) : '';
  missionsPage.task = 0;
  missionsPage.tab = 'edit';
  missionsPage.preview = null;
}

/** Applies a successful write response and reselects the affected series. */
function applyMissionResponse(data, seriesID, savedSeries) {
  missionsPage.rows = data.series;
  missionsPage.revision = data.revision;
  const keepTask = missionsPage.current?.series_id === seriesID ? missionsPage.task : 0;
  selectMissionRecord(missionRecord(seriesID));
  if (savedSeries) {
    missionsPage.current.tasks.forEach((task, index) => {
      if (autoMissionJumps.has(savedSeries.tasks[index])) autoMissionJumps.add(task);
    });
  }
  missionsPage.task = Math.min(keepTask, Math.max(0, (missionsPage.current?.tasks.length || 1) - 1));
  state.loaded.delete('audit');
  clearViewAlert('missions');
  renderMissions();
}

async function newMissionFromTemplate(id) {
  if (!(await confirmDiscard(missionDirty(), '新建系列会放弃当前未保存的修改。', '放弃并新建'))) return;
  const template = MISSION_TEMPLATES.find((t) => t.id === id);
  missionsPage.current = {
    series_id: 0,
    kind: 'mission',
    layout: 'chain',
    tab_type: 0,
    daily: false,
    starts_at: 0,
    ends_at: 0,
    enabled: true,
    archived: false,
    ...template.series,
    tasks: template.tasks.map(([key, target]) => newMissionTask(key, target)),
  };
  missionsPage.savedJSON = '';
  missionsPage.task = 0;
  missionsPage.tab = 'edit';
  missionsPage.preview = null;
  $('#ms-new-menu').open = false;
  clearViewAlert('missions');
  renderMissions();
  $('#ms-settings [data-series="name"]')?.select();
}

// Rendering --------------------------------------------------------------------------------------

function renderMissions() {
  renderMissionList();
  renderMissionEditor();
  updateDraftIndicators();
}

function missionStatusTags(record, { dirty = false } = {}) {
  const [label, tone] = MISSION_STATUS[missionStatus(record)];
  return html`
    ${stateTag(label, tone)}
    ${missionPending(record) && stateTag('有未发布的修改', 'warn')}
    ${dirty && record && stateTag('未保存', 'warn')}
  `;
}

function renderMissionList() {
  const query = $('#ms-search').value.trim().toLowerCase();
  const filter = $('#ms-status').value;
  const current = missionsPage.current;
  const dirty = missionDirty();
  const rows = missionsPage.rows.filter((record) => {
    const s = record.draft;
    if (filter === 'pending' ? !missionPending(record) : filter && missionStatus(record) !== filter) {
      return false;
    }
    return `${s.name} ${s.series_id}`.toLowerCase().includes(query);
  });
  const item = (record) => {
    const s = record ? record.draft : current;
    const selected = s.series_id === current?.series_id;
    return html`
      <button type="button" class="ws-item ${selected ? 'selected' : ''}" data-ms-select="${s.series_id}">
        <span class="ws-item-title">
          <b>${s.name || '未命名系列'}</b>
          ${missionStatusTags(record, { dirty: selected && dirty })}
        </span>
        <span class="ws-item-meta">
          ${s.kind === 'achievement' ? '成就' : '任务'} · ${s.layout === 'chain' ? '任务链' : '独立目标'}
          ${s.daily ? ' · 每日' : ''} · ${s.tasks.length} 项
        </span>
        <small>${s.series_id ? `#${s.series_id}` : '尚未保存'}</small>
      </button>
    `;
  };
  setHTML(
    $('#ms-list'),
    html`
      ${current && !current.series_id && item(null)}
      ${rows.map(item)}
      ${
        !rows.length &&
        html`<p class="ws-list-empty">
        ${missionsPage.rows.length ? '没有匹配的系列。' : '还没有任务系列。从「新建」选择模板开始。'}
      </p>`
      }
    `,
  );
  $('#ms-count').textContent = `${missionsPage.rows.length} 个系列`;
}

function renderMissionEditor() {
  const s = missionsPage.current;
  $('#ms-empty').hidden = !!s;
  $('#ms-editor').hidden = !s;
  if (!s) {
    renderMissionSavebar();
    return;
  }
  renderMissionHead();
  renderMissionLock();
  setHTML($('#ms-settings'), missionSettingsHTML(s));
  renderMissionSettingErrors();
  renderMissionTabs();
  renderMissionSavebar();
}

function renderMissionHead() {
  const s = missionsPage.current;
  const record = missionCurrentRecord();
  const status = missionStatus(record);
  const dirty = missionDirty();
  setHTML(
    $('#ms-head'),
    html`
      <div class="ws-title">
        <span class="eyebrow">${s.kind === 'achievement' ? '成就系列' : '任务系列'}${s.series_id ? ` · #${s.series_id}` : ''}</span>
        <h2>${s.name || '未命名系列'}</h2>
        <div class="ws-tags">${missionStatusTags(record, { dirty })}</div>
      </div>
      <div class="ws-actions">
        <button type="button" class="secondary sm" data-ms-action="clone" ${attr(!s.series_id, 'disabled')}
          title="以已保存的草稿生成新编号的副本，可修改条件和奖励">复制为新系列</button>
        ${
          record?.published &&
          status !== 'archived' &&
          html`<button type="button" class="secondary sm" data-ms-action="toggle">
          ${record.published.enabled ? '停用' : '重新开放'}
        </button>`
        }
        ${
          !!s.series_id &&
          status !== 'archived' &&
          html`<button type="button" class="danger sm" data-ms-action="archive">归档</button>`
        }
      </div>
    `,
  );
}

function renderMissionLock() {
  const lock = $('#ms-lock');
  const archived = missionArchived();
  lock.hidden = !missionLocked() && !archived;
  if (lock.hidden) return;
  setHTML(
    lock,
    archived
      ? html`
          <svg class="icon"><use href="#i-alert" /></svg>
          <span><b>已归档</b>系列对玩家隐藏，已有进度和邮件保留。归档后不能再编辑，可复制为新系列。</span>
        `
      : html`
          <svg class="icon"><use href="#i-info" /></svg>
          <span>
            <b>已发布，含义已固定</b>类型、结构、周期、开放时间、任务的条件和奖励不能再改，玩家进度依赖这些定义。
            仍可修改名称、任务文案、显示顺序、挑战按钮和开放状态；要改变条件或奖励，请复制为新系列。
          </span>
          <button type="button" class="secondary sm" data-ms-action="clone">复制为新系列</button>
        `,
  );
}

/** Label of the 开放时间 field: what each tab requires of the start and end. */
function missionTimeLabel(s) {
  if (s.tab_type === 3) return '开放时间（UTC+8）· 开始可留空，结束必填';
  if (s.tab_type === 1) return '开放时间（UTC+8）· 开始可留空；未开每日重置时结束必填';
  return '开放时间（UTC+8）· 普通成就不设时间';
}

function missionSettingsHTML(s) {
  const frozen = missionLocked() || missionArchived();
  const archived = missionArchived();
  const ordinary = s.tab_type === 0;
  const tab = MISSION_TABS.find(([v]) => v === s.tab_type);
  // An ordinary achievement has no period or time; values left over from older drafts stay clearable.
  const noTime = ordinary && !s.starts_at && !s.ends_at;
  return html`
    <label class="field span-2">
      <span class="field-label">系列名称</span>
      <input data-series="name" maxlength="80" value="${s.name}" ${attr(archived, 'disabled')} />
    </label>
    <div class="field span-2">
      <span class="field-label">客户端页签</span>
      ${segmented(
        'ms-tab',
        s.tab_type,
        MISSION_TABS.map(([value, label, note]) => [value, label, note]),
        { disabled: frozen, data: 'data-series="tab_type"' },
      )}
      <span class="hint">${tab ? tab[2] : '旧数据使用的页签，玩家端不显示'}</span>
      <p class="field-error" data-ms-error="tab" hidden></p>
    </div>
    <div class="field">
      <span class="field-label">结构</span>
      ${segmented(
        'ms-layout',
        s.layout,
        [
          ['chain', '任务链', '领取前一项后出现下一项'],
          ['independent', '独立目标', '各项同时累计'],
        ],
        { disabled: frozen, data: 'data-series="layout"' },
      )}
    </div>
    <div class="field">
      <span class="field-label">周期</span>
      ${segmented(
        'ms-period',
        s.daily ? 'daily' : 'total',
        [
          ['total', '累计'],
          ['daily', '每日重置', '每天 00:00（UTC+8）切日'],
        ],
        { disabled: frozen || (s.kind === 'achievement' && !s.daily), data: 'data-series="daily"' },
      )}
      <p class="field-error" data-ms-error="period" hidden></p>
    </div>
    <div class="field span-2">
      <span class="field-label">${missionTimeLabel(s)}</span>
      <div class="range-pair">
        <input type="datetime-local" data-series="starts_at" value="${chinaInputValue(s.starts_at)}"
          aria-label="开始时间（UTC+8）" ${attr(frozen || noTime, 'disabled')} />
        <span>至</span>
        <input type="datetime-local" data-series="ends_at" value="${chinaInputValue(s.ends_at)}"
          aria-label="结束时间（UTC+8）" ${attr(frozen || noTime, 'disabled')} />
      </div>
      <p class="field-error" data-ms-error="time" hidden></p>
    </div>
    <div class="field field-switch">
      <label class="switch">
        <input type="checkbox" data-series="enabled" ${attr(s.enabled, 'checked')} ${attr(archived, 'disabled')} />
        <span>${missionLocked() ? '对玩家开放（发布修改后生效）' : '发布后对玩家开放'}</span>
      </label>
      <span class="hint">关闭时发布为“已停用”，玩家看不到，进度保留。</span>
    </div>
  `;
}

/** Fills the error lines under 页签, 周期 and 开放时间 without re-rendering the focused inputs. */
function renderMissionSettingErrors() {
  const s = missionsPage.current;
  const problems = missionTabProblems(s);
  if (s.ends_at && s.ends_at <= s.starts_at) problems.push(['time', '结束时间须晚于开始时间']);
  for (const box of $$('#ms-settings [data-ms-error]')) {
    const messages = problems
      .filter(([field]) => field === box.dataset.msError)
      .map(([, message]) => message);
    box.textContent = messages.join('；');
    box.hidden = !messages.length;
  }
}

function renderMissionTabs() {
  const tab = missionsPage.tab;
  $$('#ms-tabs [data-ms-tab]').forEach((b) => {
    b.classList.toggle('active', b.dataset.msTab === tab);
    b.setAttribute('aria-selected', String(b.dataset.msTab === tab));
  });
  const stale = missionsPage.preview && missionsPage.preview.json !== stableJSON(missionsPage.current);
  $('#ms-preview-flag').hidden = !stale;
  $('#ms-board').hidden = tab !== 'edit';
  $('#ms-preview').hidden = tab !== 'preview';
  if (tab === 'edit') {
    renderMissionOutline();
    renderMissionInspector();
  } else renderMissionPreview();
}

function renderMissionOutline() {
  const s = missionsPage.current;
  const chain = s.layout === 'chain';
  const frozen = missionLocked() || missionArchived();
  $('#ms-outline-title').textContent = `${s.tasks.length} 项任务 · ${chain ? '依次解锁' : '同时累计'}`;
  setHTML(
    $('#ms-outline'),
    html`
      <ol class="ms-nodes ${chain ? 'chain' : ''}">
        ${s.tasks.map((task, i) => {
          const note = missionConditionNote(task);
          const missing = frozen ? [] : missionTaskProblems(task);
          return html`
            <li class="ms-node ${i === missionsPage.task ? 'selected' : ''}">
              <button type="button" class="ms-node-main" data-ms-task="${i}" aria-current="${i === missionsPage.task}">
                <span class="ms-node-index">${i + 1}</span>
                <span class="ms-node-body">
                  <b>${task.title || '（未填写标题）'}</b>
                  ${note && html`<span class="ms-node-rule">${note}</span>`}
                  ${
                    task.rewards.length > 0 &&
                    html`<span class="ms-node-rewards">${rewardChipsHTML(task.rewards, 2)}</span>`
                  }
                  ${missing.length > 0 && html`<span class="ms-node-issue">发布前：${missing.join('、')}</span>`}
                </span>
              </button>
              ${chain && i < s.tasks.length - 1 && html`<span class="ms-link">领取后出现下一项</span>`}
            </li>
          `;
        })}
      </ol>
    `,
  );
  $('#ms-add-task').disabled = frozen || s.tasks.length >= 100;
}

function renderMissionInspector() {
  const s = missionsPage.current;
  const index = Math.min(missionsPage.task, s.tasks.length - 1);
  missionsPage.task = index;
  const task = s.tasks[index];
  const frozen = missionLocked() || missionArchived();
  const archived = missionArchived();
  setHTML(
    $('#ms-inspector'),
    html`
      <div class="insp-head">
        <div>
          <span class="eyebrow">第 ${index + 1} 项${task.missionid ? ` · #${task.missionid}` : ' · 保存后分配编号'}</span>
          <h3>${task.title || '（未填写标题）'}</h3>
        </div>
        <div class="toolbar">
          <button type="button" class="secondary sm" data-ms-task-op="up" ${attr(frozen || index === 0, 'disabled')}>上移</button>
          <button type="button" class="secondary sm" data-ms-task-op="down"
            ${attr(frozen || index === s.tasks.length - 1, 'disabled')}>下移</button>
          <button type="button" class="secondary sm" data-ms-task-op="copy"
            ${attr(frozen || s.tasks.length >= 100, 'disabled')}>复制</button>
          <button type="button" class="danger sm" data-ms-task-op="remove"
            ${attr(frozen || s.tasks.length === 1, 'disabled')}>删除</button>
        </div>
      </div>
      <section class="insp-group">
        <h4>玩家看到的内容</h4>
        <div class="ws-form">
          <label class="field span-2">
            <span class="field-label">标题</span>
            <input data-task="title" maxlength="80" value="${task.title}" ${attr(archived, 'disabled')} />
          </label>
          <label class="field span-2">
            <span class="field-label">描述（可留空）</span>
            <textarea data-task="description" rows="2" maxlength="300" ${attr(archived, 'disabled')}>${task.description}</textarea>
          </label>
          <label class="field">
            <span class="field-label">显示优先级（小的在前）</span>
            <input type="number" data-task="view_prio" value="${task.view_prio}" ${attr(archived, 'disabled')} />
          </label>
          <label class="field">
            <span class="field-label">挑战按钮（未完成时显示，点击前往）</span>
            <select data-task="open_url" ${attr(archived, 'disabled')}>${missionJumpOptions(task)}</select>
          </label>
        </div>
      </section>
      <section class="insp-group">
        <h4>完成条件${frozen && html`<span class="lock-note">已固定</span>`}</h4>
        <div id="ms-rule">${missionRuleHTML(task, frozen)}</div>
      </section>
      <section class="insp-group">
        <h4>领取奖励 · 通过邮件发送${frozen && html`<span class="lock-note">已固定</span>`}</h4>
        <div id="ms-rewards">${rewardListHTML(task.rewards, { locked: frozen, note: '发布前至少添加 1 份' })}</div>
      </section>
    `,
  );
}

function missionJumpOptions(task) {
  const selected = autoMissionJumps.has(task) ? MISSION_AUTO_JUMP : task.open_url || '';
  const [hidden, ...destinations] = MISSION_JUMPS;
  const options = [
    hidden,
    [MISSION_AUTO_JUMP, `跟随完成条件（${missionJumpName(missionDefaultJump(task.rule))}）`],
    ...destinations,
  ];
  // A command from imported or older data (e.g. mainquest:<id>) stays selectable until replaced.
  if (!options.some(([value]) => value === selected)) {
    const name = missionJumpName(selected, task.rule);
    options.unshift([
      selected,
      name === selected ? `保留原命令：${selected}` : `${name}（原命令：${selected}）`,
    ]);
  }
  return options.map(
    ([value, label]) =>
      html`<option value="${value}" ${attr(value === selected, 'selected')}>${label}</option>`,
  );
}

/** Only the conditions the series' tab allows; a stored one outside them stays shown until replaced. */
function missionConditionOptions(selected) {
  const kind = missionsPage.current.kind;
  return MISSION_GROUPS.map(([group, label]) => {
    const allowed = missionGroupAllowed(group, kind);
    const keys = Object.keys(MISSION_CONDITIONS).filter(
      (key) => MISSION_CONDITIONS[key].group === group && (allowed || key === selected),
    );
    return (
      keys.length > 0 &&
      html`
        <optgroup label="${label}">
          ${keys.map(
            (key) => html`
              <option value="${key}" ${attr(key === selected, 'selected')} ${attr(!allowed, 'disabled')}>
                ${MISSION_CONDITIONS[key].name}${allowed ? '' : '（当前页签不可用）'}
              </option>
            `,
          )}
        </optgroup>
      `
    );
  });
}

function cardTargetsHTML(ids, { locked, max }) {
  return html`
    <div class="target-chips">
      ${ids.map(
        (id) => html`
          <span class="target-chip">
            ${cardSubjectLabel(id)}<code>${id}</code>
            <button type="button" class="x-button" data-ms-target-remove="${id}" aria-label="移除 ${id}"
              ${attr(locked, 'disabled')}>
              <svg class="icon"><use href="#i-close" /></svg>
            </button>
          </span>
        `,
      )}
      ${
        ids.length < max &&
        html`
        <button type="button" class="add-chip" data-ms-target-pick ${attr(locked, 'disabled')}>
          <svg class="icon"><use href="#i-plus" /></svg>从卡牌目录选择
        </button>
        <span class="joined target-id">
          <input type="number" min="1" placeholder="或输入卡牌 ID" data-ms-target-id aria-label="卡牌 ID"
            ${attr(locked, 'disabled')} />
          <button type="button" class="secondary sm" data-ms-target-add ${attr(locked, 'disabled')}>添加</button>
        </span>
      `
      }
    </div>
  `;
}

function missionRuleHTML(task, locked) {
  const rule = task.rule;
  const key = missionRuleKey(rule);
  const condition = MISSION_CONDITIONS[key];
  const isSet = rule.kind === 'targets';
  const targetLabel = condition.targetLabel || (condition.group === 'event' ? '次数' : '达到');
  const targetField = (field, label, unit, value) => html`
    <label class="field">
      <span class="field-label">${label}</span>
      <span class="input-unit">
        <input type="number" min="1" max="2147483647" data-rule="${field}" value="${value}"
          ${attr(locked, 'disabled')} />
        ${unit && html`<span>${unit}</span>`}
      </span>
    </label>
  `;
  let subject = '';
  if (condition.subject === 'card') {
    const ids = isSet ? rule.targets || [] : rule.subject_id ? [rule.subject_id] : [];
    subject = html`
      <div class="field span-2">
        <span class="field-label">
          ${
            isSet
              ? '目标卡牌（每张都须完成，最多 100 张）'
              : key === 'card_evolution'
                ? '进化结果卡牌（可留空，统计任意进化）'
                : '目标卡牌'
          }
        </span>
        ${cardTargetsHTML(ids, { locked, max: isSet ? 100 : 1 })}
      </div>
    `;
  } else if (condition.subject) {
    const noun = condition.subject === 'boss' ? 'BOSS' : '剧情';
    const field = isSet ? 'targets' : 'subject_id';
    const ids = isSet ? rule.targets || [] : rule.subject_id ? [rule.subject_id] : [];
    const scope = missionTargetScopeProblem(task, missionsPage.current.tab_type);
    subject = html`
      <div class="field span-2">
        <span class="field-label">${isSet ? `目标${noun}（每个都须完成，最多 100 个）` : `目标${noun}`}</span>
        ${missionTargetChipsHTML(condition.subject, field, ids, { locked, empty: `尚未选择${noun}` })}
        ${!locked && scope && html`<p class="field-error">${scope}</p>`}
      </div>
    `;
  }
  let target = '';
  if (isSet) {
    if (key === 'card_fame') target = targetField('minimum', '每张名声至少', '', rule.minimum || 1);
  } else if (key === 'pvp_rank') {
    const known = missionTargets.ranks.some((r) => r.rank_id === rule.target);
    target = html`
      <label class="field">
        <span class="field-label">段位达到</span>
        <select data-rule="target" ${attr(locked, 'disabled')}>
          ${!known && html`<option value="${rule.target}" selected>段位 ${rule.target}</option>`}
          ${missionTargets.ranks
            .filter((r) => r.rank_id > 0)
            .map(
              (r) =>
                html`<option value="${r.rank_id}" ${attr(r.rank_id === rule.target, 'selected')}>${r.name}</option>`,
            )}
        </select>
      </label>
    `;
  } else if (!condition.fixedTarget) {
    target = targetField(
      'target',
      key === 'card_fame' ? '名声至少' : targetLabel,
      missionTargetUnit(rule),
      rule.target,
    );
  }
  return html`
    <div class="ws-form">
      <label class="field span-2">
        <span class="field-label">条件</span>
        <select data-rule="condition" ${attr(locked, 'disabled')}>${missionConditionOptions(key)}</select>
        ${
          !locked &&
          !missionGroupAllowed(condition.group, missionsPage.current.kind) &&
          html`<span class="field-error">${missionGroupProblem(missionsPage.current.kind)}，请改选条件</span>`
        }
      </label>
      <p class="rule-hint span-2">${condition.hint}</p>
      ${
        condition.sets &&
        (isSet || (key !== 'story_cleared' && key !== 'quest_cleared')) &&
        html`
        <div class="field span-2">
          <span class="field-label">目标方式</span>
          ${segmented(
            'ms-rule-mode',
            isSet ? 'set' : 'single',
            [
              ['single', key === 'card_evolution' ? '计数' : '单个目标'],
              ['set', '目标集合'],
            ],
            { disabled: locked, data: 'data-rule="mode"' },
          )}
        </div>
      `
      }
      ${subject} ${target}
      ${isSet && html`<p class="rule-hint span-2">达成数等于目标数量：${(rule.targets || []).length} 个。</p>`}
    </div>
    ${condition.filters && missionQuestFiltersHTML(rule, locked)}
  `;
}

function missionQuestFiltersHTML(rule, locked) {
  const active = [
    rule.quest_scope,
    rule.boss_ids?.length,
    rule.arthur_type,
    rule.min_players,
    rule.score_grade,
    rule.own_decks_only,
    rule.include_skip,
  ].filter(Boolean).length;
  const scope = missionTargetScopeProblem({ rule }, missionsPage.current.tab_type);
  return html`
    <details class="tool-drawer rule-filters" ${attr(active, 'open')}>
      <summary>通关筛选${active ? `（${active} 项）` : '（不限）'}</summary>
      <div class="ws-form">
        <div class="field span-2">
          <span class="field-label">模式</span>
          ${segmented('ms-quest-scope', rule.quest_scope || '', QUEST_SCOPES, {
            disabled: locked,
            data: 'data-rule="quest_scope"',
          })}
        </div>
        <div class="field span-2">
          <span class="field-label">BOSS（可多选，通关任意一个都计数；不选表示不限）</span>
          ${missionTargetChipsHTML('boss', 'boss_ids', rule.boss_ids || [], { locked, empty: '不限 BOSS' })}
          ${!locked && scope && html`<p class="field-error">${scope}</p>`}
        </div>
        <label class="field">
          <span class="field-label">参战职业</span>
          <select data-rule="arthur_type" ${attr(locked, 'disabled')}>
            ${[0, 1, 2, 3, 4].map(
              (job) => html`<option value="${job}" ${attr((rule.arthur_type || 0) === job, 'selected')}>
                ${job ? cardJobName(job) : '不限'}
              </option>`,
            )}
          </select>
        </label>
        <label class="field">
          <span class="field-label">最少真人数（0 不限）</span>
          <input type="number" min="0" max="4" data-rule="min_players" value="${rule.min_players || 0}" ${attr(locked, 'disabled')} />
        </label>
        <label class="field">
          <span class="field-label">最低积分档位（0 不限，1–8）</span>
          <input type="number" min="0" max="8" data-rule="score_grade" value="${rule.score_grade || 0}" ${attr(locked, 'disabled')} />
        </label>
        <div class="field span-2 checks">
          <label><input type="checkbox" data-rule="own_decks_only" ${attr(rule.own_decks_only, 'checked')} ${attr(locked, 'disabled')} />
            仅完整四职业自卡组</label>
          <label><input type="checkbox" data-rule="include_skip" ${attr(rule.include_skip, 'checked')} ${attr(locked, 'disabled')} />
            计入跳过券（跳过没有职业、人数与积分，不能满足这些筛选）</label>
        </div>
      </div>
    </details>
  `;
}

/** When a task stops counting: the end of the day for daily series, never later than the series end. */
function missionDeadlineText(s) {
  if (s.daily && s.ends_at) return '每天 24:00（UTC+8）；最后一天到结束时间';
  if (s.daily) return '每天 24:00（UTC+8）';
  if (s.ends_at) return `${chinaText(s.ends_at)}（UTC+8）`;
  return '长期有效';
}

function missionSummaryFacts(s) {
  return [
    ['页签', `${missionTabName(s.tab_type)} · ${s.kind === 'achievement' ? '成就' : '任务'}`],
    ['结构', s.layout === 'chain' ? '任务链：领取前一项后出现下一项' : '独立目标：各项同时累计'],
    ['周期', s.daily ? '每日 00:00（UTC+8）重置' : '累计，不重置'],
    ['开放时间', chinaWindowText(s.starts_at, s.ends_at)],
    ['截止', missionDeadlineText(s)],
    ['发布后', s.enabled && !s.archived ? '对玩家开放' : '停用（玩家看不到）'],
  ];
}

/**
 * Remaining time the client shows under a 期间限定 or 活动 task, as of now (or of the start when the
 * series has not started): the earlier of the day's end (daily) and the series end, as the server does.
 */
function missionRemainingText(s) {
  if (!missionTimedTab(s.tab_type) || (!s.daily && !s.ends_at)) return '';
  const now = Math.floor(Date.now() / 1000);
  const from = Math.max(now, s.starts_at || 0);
  let end = s.ends_at ? s.ends_at - 1 : 0;
  if (s.daily) {
    const dayEnd = (Math.floor((from + CHINA_OFFSET_SECONDS) / 86400) + 1) * 86400 - CHINA_OFFSET_SECONDS - 1;
    if (!end || dayEnd < end) end = dayEnd;
  }
  const left = Math.max(0, end - from);
  const days = Math.floor(left / 86400);
  const hours = Math.floor((left % 86400) / 3600);
  const minutes = Math.floor((left % 3600) / 60);
  const text = days ? `${days} 天 ${hours} 小时` : hours ? `${hours} 小时 ${minutes} 分` : `${minutes} 分`;
  return `${from > now ? '开始时剩余' : '剩余'} ${text}`;
}

const MISSION_PREVIEW_NOTE =
  '按玩家视角显示本系列；同一页签还会列出其他系列的任务，已领取的移到「领取记录」。' +
  '达成后可领取，奖励以邮件发送；已发布任务不重置历史进度。';

function renderMissionPreview() {
  const box = $('#ms-preview');
  const preview = missionsPage.preview;
  if (!preview) {
    setHTML(
      box,
      html`<div class="empty"><b>尚未预览</b>点击「预览」由服务端校验当前内容，并按玩家视角显示任务列表。</div>`,
    );
    return;
  }
  const s = preview.series;
  const stale = preview.json !== stableJSON(missionsPage.current);
  const remaining = missionRemainingText(s);
  // Like the client's task list: title and progress on top, rewards with 详细 below, remaining time
  // under 详细 on the timed tabs. The condition is an operator note with what the title leaves out.
  const item = (task) => {
    const note = missionConditionNote(task);
    const progressMax = task.rule.metric === 'pvp_rank' ? 1 : (task.progress_max ?? task.rule.target);
    return html`
      <li class="ms-game-item">
        <div class="ms-game-top">
          <b class="ms-game-title">${task.title}</b>
          <span class="ms-game-count">0/${num(progressMax)}</span>
        </div>
        <div class="ms-game-bottom">
          <span class="ms-game-rewards">${rewardChipsHTML(task.rewards, 10)}</span>
          <span class="ms-game-side">
            ${task.open_url && html`<span class="ms-game-go" title="前往${missionJumpName(task.open_url, task.rule)}">挑战</span>`}
            <details class="ms-game-detail">
              <summary>详细</summary>
              <span class="ms-game-popup">
                <b>${task.title}</b>
                ${task.description ? html`<span>${task.description}</span>` : html`<i>未填写描述，任务框中间为空</i>`}
                ${task.open_url && html`<small>「挑战」前往${missionJumpName(task.open_url, task.rule)}</small>`}
              </span>
            </details>
            ${remaining && html`<span class="ms-game-time">${remaining}</span>`}
          </span>
        </div>
        ${note && html`<p class="ms-game-note">条件：${note}</p>`}
      </li>
    `;
  };
  const chain = s.layout === 'chain';
  const later = chain ? s.tasks.slice(1) : [];
  setHTML(
    box,
    html`
      ${
        stale &&
        html`<div class="pool-warning">预览之后又修改了内容。<button type="button" class="secondary sm"
        data-ms-action="preview">重新预览</button></div>`
      }
      <div class="facts preview-facts">
        ${missionSummaryFacts(s).map(([label, value]) => html`<div class="fact"><span>${label}</span><b>${value}</b></div>`)}
      </div>
      <div class="ms-game">
        <div class="ms-game-tabs">
          ${MISSION_TABS.map(([value, label]) => html`<span class="${value === s.tab_type ? 'active' : ''}">${label}</span>`)}
        </div>
        <ol class="ms-game-list">${(chain ? s.tasks.slice(0, 1) : s.tasks).map(item)}</ol>
        ${
          later.length > 0 &&
          html`
            <details class="ms-game-later">
              <summary>后续 ${later.length} 项 · 领取前一项后依次出现</summary>
              <ol class="ms-game-list">${later.map(item)}</ol>
            </details>
          `
        }
      </div>
      <p class="hint">${MISSION_PREVIEW_NOTE}</p>
    `,
  );
}

function renderMissionSavebar() {
  const s = missionsPage.current;
  const record = missionCurrentRecord();
  const dirty = missionDirty();
  const archived = missionArchived();
  const pending = missionPending(record);
  $('#ms-version').textContent = `配置 r${missionsPage.revision}`;
  let text = '选择或新建一个系列';
  let kind = '';
  if (s && !record) [text, kind] = ['新系列尚未保存；草稿保存前不会分配编号', 'dirty'];
  else if (archived) text = '已归档，只读';
  else if (dirty) [text, kind] = ['有未保存的修改', 'dirty'];
  else if (!record?.published) text = '草稿已保存 · 尚未发布，玩家看不到';
  else if (pending) [text, kind] = ['已发布 · 草稿中有未发布的修改', 'dirty'];
  else [text, kind] = ['已发布 · 与线上一致', 'ok'];
  setSaveState('#ms-state', text, kind);
  $('#ms-discard').hidden = !dirty;
  $('#ms-discard').textContent = record ? '放弃修改' : '放弃新系列';
  $('#ms-preview-btn').disabled = !s || archived;
  $('#ms-save').disabled = !s || archived || !dirty;
  const publishable = s && !archived && (dirty || !record?.published || pending);
  $('#ms-publish').disabled = !publishable;
  $('#ms-publish').textContent = record?.published ? '发布修改' : '发布';
}

/** Lightweight refresh after a field edit; the focused inputs are not re-rendered. */
function missionTouched({ outline = true } = {}) {
  renderMissionHead();
  renderMissionSettingErrors();
  renderMissionSavebar();
  renderMissionList();
  if (outline && missionsPage.tab === 'edit') renderMissionOutline();
  $('#ms-preview-flag').hidden = !missionsPage.preview;
  updateDraftIndicators();
}

// Editing ----------------------------------------------------------------------------------------

const missionTask = () => missionsPage.current.tasks[missionsPage.task];

/**
 * The tab decides the type: 普通 holds achievements over save states without period or time, the timed
 * tabs hold tasks over action counts. Conditions the new tab does not allow are replaced after a confirm.
 */
async function changeMissionTab(tab) {
  const s = missionsPage.current;
  const kind = tab === 0 ? 'achievement' : 'mission';
  const [replacement, target, replacementName] =
    kind === 'achievement' ? ['player_level', 10, '玩家等级'] : ['quest_clear', 1, '成功通关'];
  const outside = s.tasks.filter((t) => {
    const condition = missionCondition(t.rule);
    return condition && !missionGroupAllowed(condition.group, kind);
  });
  const timed = kind === 'achievement' && (s.daily || s.starts_at || s.ends_at);
  if (outside.length || timed) {
    const ok = await askConfirm(
      `改为${missionTabName(tab)}页签？`,
      [
        kind === 'achievement'
          ? '普通页签只放成就：只读取存档状态和指定目标，长期有效。'
          : `${missionTabName(tab)}页签只放任务：只统计开放后的新动作。`,
        outside.length &&
          `${outside.length} 项任务使用${kind === 'achievement' ? '动作计数' : '存档状态或指定目标'}，切换后改为「${replacementName}」，请再逐项调整。`,
        timed && '每日重置和开放时间会清空。',
      ]
        .filter(Boolean)
        .join(''),
      { ok: `改为${missionTabName(tab)}` },
    );
    if (!ok) return renderMissionEditor();
  }
  for (const task of outside) {
    const oldTitle = missionDefaultTitle(task.rule);
    task.rule = newMissionRule(replacement, target);
    if (autoMissionJumps.has(task)) task.open_url = missionDefaultJump(task.rule);
    if (!task.title || task.title === oldTitle) task.title = missionDefaultTitle(task.rule);
  }
  if (kind === 'achievement') Object.assign(s, { daily: false, starts_at: 0, ends_at: 0 });
  s.kind = kind;
  s.tab_type = tab;
  renderMissionEditor();
  missionTouched();
}

function setMissionSeriesField(field, element) {
  const s = missionsPage.current;
  if (field === 'tab_type') return changeMissionTab(Number(element.value));
  if (field === 'starts_at' || field === 'ends_at') {
    try {
      s[field] = chinaUnix(element.value);
      element.classList.remove('invalid');
    } catch (error) {
      markInvalid(element, error.message);
      return toast(error.message, true);
    }
  } else if (field === 'daily') s.daily = element.value === 'daily';
  else if (field === 'enabled') s.enabled = element.checked;
  else s[field] = element.value;
  missionTouched({ outline: field === 'layout' });
}

function setMissionTaskField(field, element) {
  const task = missionTask();
  if (field === 'open_url') {
    if (element.value === MISSION_AUTO_JUMP) {
      autoMissionJumps.add(task);
      task.open_url = missionDefaultJump(task.rule);
    } else {
      autoMissionJumps.delete(task);
      task.open_url = element.value;
    }
  } else task[field] = field === 'view_prio' ? Number(element.value) || 0 : element.value;
  if (field === 'title') $('#ms-inspector .insp-head h3').textContent = task.title || '（未填写标题）';
  missionTouched();
}

function setMissionRuleMode(task, mode) {
  const rule = task.rule;
  const key = missionRuleKey(rule);
  const base = { event: rule.event, metric: rule.metric };
  if (mode === 'set') {
    const seed = rule.subject_id ? [rule.subject_id] : [];
    task.rule = {
      ...base,
      kind: 'targets',
      targets: seed,
      target: seed.length,
      minimum: key === 'card_fame' ? rule.target || 1 : 0,
    };
  } else {
    task.rule = {
      ...base,
      kind: rule.event ? '' : 'state',
      subject_id: rule.targets?.[0] || 0,
      target: key === 'card_fame' ? rule.minimum || 1 : 1,
    };
  }
}

function setMissionRuleField(field, element) {
  const task = missionTask();
  const rule = task.rule;
  const oldTitle = missionDefaultTitle(rule);
  let rerender = false;
  try {
    if (field === 'condition') {
      task.rule = newMissionRule(element.value, rule.target);
      if (autoMissionJumps.has(task)) task.open_url = missionDefaultJump(task.rule);
      rerender = true;
    } else if (field === 'mode') {
      setMissionRuleMode(task, element.value);
      rerender = true;
    } else if (field === 'quest_scope') {
      rule.quest_scope = element.value;
    } else if (element.type === 'checkbox') {
      rule[field] = element.checked;
    } else {
      const value = Number(element.value);
      if (!Number.isInteger(value) || value < 0) throw new Error('请输入非负整数');
      rule[field] = value;
      element.classList.remove('invalid');
    }
  } catch (error) {
    markInvalid(element, error.message);
    return toast(error.message, true);
  }
  // A title that still reads like the generated one follows the rule; a custom title is kept.
  const followTitle = !task.title || task.title === oldTitle;
  if (followTitle) task.title = missionDefaultTitle(task.rule);
  if (rerender) renderMissionInspector();
  else if (followTitle) {
    $('#ms-inspector [data-task="title"]').value = task.title;
    $('#ms-inspector .insp-head h3').textContent = task.title || '（未填写标题）';
  }
  missionTouched();
}

function addMissionCardTargets(ids) {
  const task = missionTask();
  const rule = task.rule;
  if (rule.kind === 'targets') {
    const merged = [...new Set([...(rule.targets || []), ...ids])];
    if (merged.length > 100) throw new Error('目标集合最多 100 张卡牌');
    rule.targets = merged;
    rule.target = merged.length;
  } else {
    if (ids.length !== 1) throw new Error('单个目标只能选择一张卡牌；需要多张请切换为目标集合');
    rule.subject_id = ids[0];
  }
  renderMissionInspector();
  missionTouched();
}

function removeMissionCardTarget(id) {
  const rule = missionTask().rule;
  if (rule.kind === 'targets') {
    rule.targets = rule.targets.filter((t) => t !== id);
    rule.target = rule.targets.length;
  } else rule.subject_id = 0;
  renderMissionInspector();
  missionTouched();
}

function pickMissionCards() {
  const key = missionRuleKey(missionTask().rule);
  openContentPicker(
    (rows) => {
      rememberCatalogRows(rows);
      addMissionCardTargets([...new Set(rows.map((r) => r.reward_type_id))]);
    },
    key === 'card_owned' ? ['card', 'material'] : ['card'],
    { title: '选择目标卡牌' },
  );
}

async function addMissionCardByID() {
  const input = $('#ms-inspector [data-ms-target-id]');
  const id = Number(input.value);
  if (!Number.isSafeInteger(id) || id <= 0) {
    markInvalid(input, '请输入卡牌 ID');
    return;
  }
  await resolveCardSubjects([id]);
  if (!catalogInfo.has(`6:${id}`) && !catalogInfo.has(`13:${id}`)) {
    markInvalid(input, '当前目录中没有这张卡牌');
    return toast(`当前目录中没有卡牌 ${id}`, true);
  }
  try {
    addMissionCardTargets([id]);
  } catch (error) {
    toast(error.message, true);
  }
}

function missionTaskOperation(op) {
  const s = missionsPage.current;
  const i = missionsPage.task;
  if (op === 'add') {
    const key = s.kind === 'achievement' ? 'collection_count' : 'quest_clear';
    s.tasks.push(newMissionTask(key, 1));
    missionsPage.task = s.tasks.length - 1;
  } else if (op === 'copy') {
    const copy = { ...structuredClone(s.tasks[i]), missionid: 0 };
    if (autoMissionJumps.has(s.tasks[i])) autoMissionJumps.add(copy);
    s.tasks.splice(i + 1, 0, copy);
    missionsPage.task = i + 1;
  } else if (op === 'remove') {
    s.tasks.splice(i, 1);
    missionsPage.task = Math.min(i, s.tasks.length - 1);
  } else {
    const j = op === 'up' ? i - 1 : i + 1;
    [s.tasks[i], s.tasks[j]] = [s.tasks[j], s.tasks[i]];
    missionsPage.task = j;
  }
  renderMissionOutline();
  renderMissionInspector();
  missionTouched({ outline: false });
}

// Server actions ---------------------------------------------------------------------------------

const missionRequest = (action, series) =>
  api('/api/mission-series', {
    method: 'POST',
    body: JSON.stringify({ expected_revision: missionsPage.revision, action, series }),
  });

function missionBlocked(problems, title) {
  if (!problems.length) return false;
  showViewAlert('missions', { title, message: problems.slice(0, 8).join('；') });
  return true;
}

async function saveMissionDraft() {
  const s = missionsPage.current;
  if (!s || missionBlocked(missionProblems(s, false), '不能保存')) return;
  await contentAction('missions', async () => {
    const data = await missionRequest('save', s);
    applyMissionResponse(data, data.series_id, s);
    toast('草稿已保存；玩家看不到草稿');
  });
}

async function previewMission() {
  const s = missionsPage.current;
  if (!s || missionBlocked(missionProblems(s, true), '预览前请先修正')) return;
  await contentAction(
    'missions',
    async () => {
      const data = await missionRequest('preview', s);
      missionsPage.preview = { json: stableJSON(s), series: data.series };
      missionsPage.tab = 'preview';
      clearViewAlert('missions');
      renderMissionTabs();
    },
    '预览',
  );
}

function missionPublishReview(checked, record) {
  const live = record?.published;
  if (!live) {
    return [
      { title: '系列', summary: true, rows: missionSummaryFacts(checked) },
      {
        title: '任务 · 条件 · 奖励',
        summary: true,
        rows: checked.tasks.map((t, i) => [
          `${i + 1}. ${t.title}`,
          `${missionConditionText(t.rule)}；奖励：${rewardListText(t.rewards)}`,
        ]),
      },
    ];
  }
  const rows = diffRows([
    ['系列名称', live.name, checked.name],
    ['对玩家开放', live.enabled, checked.enabled],
  ]);
  checked.tasks.forEach((task, i) => {
    const before = live.tasks[i] || {};
    rows.push(
      ...diffRows([
        [`第 ${i + 1} 项标题`, before.title, task.title],
        [`第 ${i + 1} 项描述`, before.description, task.description],
        [`第 ${i + 1} 项挑战按钮`, missionJumpName(before.open_url ?? ''), missionJumpName(task.open_url)],
        [`第 ${i + 1} 项显示优先级`, before.view_prio, task.view_prio],
      ]),
    );
  });
  return [{ title: '相对线上版本的变化', rows }];
}

async function publishMission(
  series = missionsPage.current,
  { title = '发布任务系列', confirmText = '确认发布' } = {},
) {
  if (!series) return;
  // The server skips readiness only when an already published series is being disabled.
  const ready = series.enabled || !missionRecord(series.series_id)?.published;
  if (missionBlocked(missionProblems(series, ready), '发布前请先修正')) return;
  let checked = null;
  await contentAction(
    'missions',
    async () => {
      checked = (await missionRequest('preview', series)).series;
    },
    '发布检查',
  );
  if (!checked) return;
  const record = missionRecord(series.series_id);
  const ok = await reviewChanges({
    title,
    lead: `${checked.name} · ${checked.tasks.length} 项 · ${checked.enabled ? '发布后立即对玩家开放' : '发布为停用状态'}`,
    sections: missionPublishReview(checked, record),
    note: record?.published
      ? '只改变文案、顺序或开放状态；条件与奖励保持线上版本。'
      : '发布后类型、结构、周期、开放时间、条件和奖励固定。',
    confirmText,
  });
  if (!ok) return;
  await contentAction(
    'missions',
    async () => {
      const data = await missionRequest('publish', series);
      applyMissionResponse(data, data.series_id);
      toast(checked.enabled ? '任务系列已发布，玩家下次请求时生效' : '任务系列已发布为停用状态');
    },
    '发布',
  );
}

async function toggleMissionEnabled() {
  if (missionDirty()) return toast('请先保存或放弃当前修改，再切换开放状态', true);
  const next = structuredClone(missionsPage.current);
  next.enabled = !missionCurrentRecord().published.enabled;
  await publishMission(next, {
    title: next.enabled ? '重新开放任务系列' : '停用任务系列',
    confirmText: next.enabled ? '确认开放' : '确认停用',
  });
}

async function archiveMission() {
  if (missionDirty()) return toast('请先保存或放弃当前修改，再归档', true);
  const s = missionsPage.current;
  const ok = await reviewChanges({
    title: '归档任务系列',
    lead: s.name,
    sections: [
      {
        title: '影响',
        rows: [
          ['玩家入口', MISSION_STATUS[missionStatus(missionCurrentRecord())][0], '隐藏整个系列'],
          ['后续编辑', '可改文案与开放状态', '只读；可复制为新系列'],
          ['已有进度与邮件', '保留', '保留，不撤回'],
        ],
      },
    ],
    confirmText: '确认归档',
  });
  if (!ok) return;
  await contentAction(
    'missions',
    async () => {
      const data = await missionRequest('archive', s);
      applyMissionResponse(data, data.series_id);
      toast('系列已归档');
    },
    '归档',
  );
}

async function cloneMission() {
  if (missionDirty()) return toast('请先保存草稿，复制会以已保存的内容为准', true);
  await contentAction(
    'missions',
    async () => {
      const data = await missionRequest('clone', missionsPage.current);
      applyMissionResponse(data, data.series_id);
      toast('已复制为新系列草稿：新编号，可修改条件和奖励');
    },
    '复制',
  );
}

async function discardMissionChanges() {
  if (!(await confirmDiscard(true, '页面会恢复为已保存的版本。'))) return;
  selectMissionRecord(missionCurrentRecord() || missionsPage.rows[0] || null);
  clearViewAlert('missions');
  renderMissions();
}

async function loadMissionProgress(event) {
  event.preventDefault();
  const input = $('#ms-user');
  const id = Number(input.value);
  if (!Number.isSafeInteger(id) || id < 1000001) {
    markInvalid(input, '请输入玩家 ID');
    return toast('请输入有效的玩家 ID', true);
  }
  input.classList.remove('invalid');
  const series = new Map(
    missionsPage.rows.flatMap((r) =>
      (r.published?.tasks || []).map((t) => [
        t.missionid,
        { name: r.published.name, archived: r.published.archived },
      ]),
    ),
  );
  try {
    const data = await api(`/api/accounts/${id}/missions`);
    $('#ms-progress-note').textContent =
      `${data.name || '未命名'} · #${data.user_id} · 含已停用及归档系列的历史进度`;
    setHTML(
      $('#ms-progress'),
      data.missions.length
        ? data.missions.map((m) => {
            const source = series.get(m.missionid);
            const availability = {
              locked: ['待解锁', 'warn'],
              inactive: [source?.archived ? '已归档' : '已停用', 'muted'],
              scheduled: ['待开始', 'info'],
              expired: ['已结束', 'muted'],
            }[m.availability];
            const [label, tone] =
              m.state === 2 || !availability
                ? MISSION_PROGRESS_STATE[m.state] || ['未知', 'muted']
                : availability;
            const pct = m.progress_max ? Math.min(100, (m.progress_now / m.progress_max) * 100) : 0;
            return html`
              <tr>
                <td class="cell-wrap"><b>${m.title}</b><span class="sub">#${m.missionid} · ${source?.name || '—'}</span></td>
                <td>
                  <span class="progress-cell">
                    <span class="progress-line"><i style="width:${pct.toFixed(1)}%"></i></span>
                    ${num(m.progress_now)} / ${num(m.progress_max)}
                  </span>
                </td>
                <td>${stateTag(label, tone)} ${m.state === 2 && availability ? stateTag(...availability) : ''}</td>
                <td>${chinaText(m.clear_limit_time)}</td>
              </tr>
            `;
          })
        : raw(emptyRow(4, '暂无任务进度', '当前目录没有可供查询的任务。')),
    );
  } catch (error) {
    reportError('missions', error, '查询');
  }
}

// Events: one delegated listener per kind on the view --------------------------------------------

const updatesOnInput = (el) =>
  !('lazy' in el.dataset) &&
  (el.tagName === 'TEXTAREA' || ['text', 'number', 'search', ''].includes(el.type));

function onMissionEdit(event) {
  const el = event.target;
  if (!missionsPage.current || !el.matches('input, select, textarea')) return;
  if (el.closest('#ms-progress-panel') || el.matches('#ms-search, #ms-status, [data-ms-target-id]')) return;
  if (el.closest('#ms-rewards')) {
    if (applyRewardEvent(event, missionTask().rewards)) missionTouched();
    return;
  }
  if ((event.type === 'input') !== updatesOnInput(el)) return;
  if (el.dataset.series) setMissionSeriesField(el.dataset.series, el);
  else if (el.dataset.task) setMissionTaskField(el.dataset.task, el);
  else if (el.dataset.rule) setMissionRuleField(el.dataset.rule, el);
}

async function onMissionClick(event) {
  const target = event.target;
  const pick = (selector) => target.closest(selector);
  let el;
  if ((el = pick('[data-ms-select]'))) {
    const id = Number(el.dataset.msSelect);
    if (id === missionsPage.current?.series_id) return;
    if (!(await confirmDiscard(missionDirty(), '切换系列会放弃当前未保存的修改。', '放弃并切换'))) return;
    selectMissionRecord(missionRecord(id));
    clearViewAlert('missions');
    renderMissions();
  } else if ((el = pick('[data-ms-template]'))) {
    newMissionFromTemplate(el.dataset.msTemplate);
  } else if ((el = pick('[data-ms-task]'))) {
    missionsPage.task = Number(el.dataset.msTask);
    renderMissionOutline();
    renderMissionInspector();
  } else if ((el = pick('[data-ms-task-op]'))) {
    missionTaskOperation(el.dataset.msTaskOp);
  } else if ((el = pick('[data-ms-tab]'))) {
    missionsPage.tab = el.dataset.msTab;
    renderMissionTabs();
  } else if ((el = pick('[data-ms-pick]'))) {
    openMissionTargetPicker(el.dataset.msPick, el.dataset.msPickField);
  } else if ((el = pick('[data-ms-pick-remove]'))) {
    removeMissionTarget(Number(el.dataset.msPickRemove), el.dataset.msPickField);
  } else if ((el = pick('[data-ms-target-pick]'))) {
    pickMissionCards();
  } else if ((el = pick('[data-ms-target-add]'))) {
    addMissionCardByID();
  } else if ((el = pick('[data-ms-target-remove]'))) {
    removeMissionCardTarget(Number(el.dataset.msTargetRemove));
  } else if (pick('#ms-rewards [data-reward-add]')) {
    const task = missionTask();
    pickRewards(task.rewards, {
      onAdded: () => {
        setHTML($('#ms-rewards'), rewardListHTML(task.rewards, { note: '发布前至少添加 1 份' }));
        missionTouched();
      },
    });
  } else if (pick('#ms-rewards')) {
    const task = missionTask();
    if (applyRewardEvent(event, task.rewards) === 'structure') {
      setHTML($('#ms-rewards'), rewardListHTML(task.rewards, { note: '发布前至少添加 1 份' }));
      missionTouched();
    }
  } else if ((el = pick('[data-ms-action]'))) {
    const action = el.dataset.msAction;
    if (action === 'save') saveMissionDraft();
    else if (action === 'preview') previewMission();
    else if (action === 'publish') publishMission();
    else if (action === 'clone') cloneMission();
    else if (action === 'toggle') toggleMissionEnabled();
    else if (action === 'archive') archiveMission();
    else if (action === 'discard') discardMissionChanges();
  }
}

setHTML(
  $('#ms-templates'),
  MISSION_TEMPLATES.map(
    (t) => html`
      <button type="button" class="ws-template" data-ms-template="${t.id}">
        <b>${t.name}</b><small>${t.note}</small>
      </button>
    `,
  ),
);
$('#missions').addEventListener('input', onMissionEdit);
$('#missions').addEventListener('change', onMissionEdit);
$('#missions').addEventListener('click', onMissionClick);
$('#missions').addEventListener('keydown', (event) => {
  if (event.key === 'Enter' && event.target.matches('[data-ms-target-id]')) {
    event.preventDefault();
    addMissionCardByID();
  }
});
// The template menu behaves like a popover: outside click or Esc closes it.
document.addEventListener('click', (event) => {
  const menu = $('#ms-new-menu');
  if (menu.open && !menu.contains(event.target)) menu.open = false;
});
$('#ms-new-menu').addEventListener('keydown', (event) => {
  if (event.key !== 'Escape') return;
  event.stopPropagation();
  event.currentTarget.open = false;
  event.currentTarget.querySelector('summary').focus();
});
$('#ms-search').addEventListener('input', renderMissionList);
$('#ms-status').addEventListener('change', renderMissionList);
$('#ms-progress-form').addEventListener('submit', loadMissionProgress);
