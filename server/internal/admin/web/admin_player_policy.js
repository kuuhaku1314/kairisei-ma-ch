'use strict';
const playerPolicy = {
  saved: null,
  defaults: null,
  revision: 0,
  names: {},
  mailRewards: [],
  mailEntries: new Map(),
};
const playerLoginKinds = ['cycle', 'beginner', 'total_milestones'];
// Sign-in rewards are gold (4) or crystals (10); the icon follows the selected currency.
const loginThumb = (type) => {
  const info = catalogInfo.get(`${type}:0`);
  return `<span class="thumb thumb-sm">${info ? catalogImage(info) : image('', type === 10 ? '水晶' : '金币')}</span>`;
};
function playerPolicyDraft() {
  const p = structuredClone(playerPolicy.saved);
  p.notice = {
    enabled: $('#player-notice-enabled').checked,
    title: $('#player-notice-title').value.trim(),
    body: $('#player-notice-body').value,
  };
  const start = unixInput('#player-notice-start'),
    end = unixInput('#player-notice-end');
  if (start) p.notice.start_unix = start;
  if (end) p.notice.end_unix = end;
  p.tutorial_completion_mail = {
    enabled: $('#player-mail-enabled').checked,
    title: $('#player-mail-title').value.trim(),
    message: $('#player-mail-message').value.trim(),
    rewards: playerMailDraft(),
  };
  p.story_first_clear_crystals = Number($('#player-story-crystals').value);
  for (const kind of playerLoginKinds)
    p.login_rewards[kind] = $$(`#player-login-${kind} tr[data-day]`).map((row, i) => {
      const day = structuredClone(p.login_rewards[kind][i]);
      day.reward.type = Number(row.querySelector('.login-kind').value);
      day.reward.num = Number(row.querySelector('.login-amount').value);
      day.comment = row.querySelector('.login-comment').value.trim();
      return day;
    });
  p.navigators = $$('#player-navis [data-id]').map((row) => ({
    navi_id: Number(row.dataset.id),
    enabled: row.querySelector('.navi-enabled').checked,
    price: Number(row.querySelector('.navi-price').value),
  }));
  p.home_banners = homeBannerDraft();
  return p;
}
function playerPolicyDirty() {
  return !!playerPolicy.saved && JSON.stringify(playerPolicy.saved) !== JSON.stringify(playerPolicyDraft());
}
function noticeScheduleText() {
  const now = Date.now() / 1000,
    start = unixInput('#player-notice-start'),
    end = unixInput('#player-notice-end');
  if (!$('#player-notice-enabled').checked) return '当前关闭：玩家看到“暂无公告”。';
  if (start && end && end <= start) return '结束时间须晚于开始时间。';
  return `显示时间：${start ? describeTime(start) : '立即'} → ${end ? describeTime(end) : '不限'}${start > now ? '（尚未开始，此前玩家看到“暂无公告”）' : end && end <= now ? '（已结束，玩家看到“暂无公告”）' : '（当前显示中）'}`;
}
function playerPolicyChanged() {
  $('#player-notice-schedule').textContent = noticeScheduleText();
  $$('#player-navis [data-id]').forEach((el) =>
    el.classList.toggle('off', !el.querySelector('.navi-enabled').checked),
  );
  const dirty = playerPolicyDirty();
  $('#player-policy-save').disabled = !dirty || policyBusy('player-policy');
  setSaveState(
    '#player-policy-state',
    dirty ? '有未保存的修改（整页一次保存）' : '已保存，与服务器一致',
    dirty ? 'dirty' : 'ok',
  );
}
function renderPlayerPolicy(p) {
  renderHomeBanners(p.home_banners);
  $('#player-policy-version').textContent = `配置 v${playerPolicy.revision}`;
  $('#player-notice-enabled').checked = p.notice.enabled;
  $('#player-notice-title').value = p.notice.title;
  $('#player-notice-body').value = p.notice.body;
  $('#player-notice-start').value = localTimeInput(p.notice.start_unix);
  $('#player-notice-end').value = localTimeInput(p.notice.end_unix);
  const mail = p.tutorial_completion_mail;
  $('#player-mail-enabled').checked = mail.enabled;
  $('#player-mail-title').value = mail.title;
  $('#player-mail-message').value = mail.message;
  playerPolicy.mailRewards = structuredClone(mail.rewards || []);
  for (const r of playerPolicy.mailRewards) {
    const fixed = playerPolicy.mailEntries.get(contentKey(r))?.fixed_card_level;
    if (fixed) r.card_lv = fixed;
  }
  renderPlayerMail();
  $('#player-story-crystals').value = p.story_first_clear_crystals;
  for (const kind of playerLoginKinds)
    $('#player-login-' + kind).innerHTML =
      p.login_rewards[kind]
        .map(
          (day) =>
            `<tr data-day="${day.day}"><td>第 ${day.day} 天</td><td><div class="login-reward">${loginThumb(day.reward.type)}<select class="login-kind" aria-label="第${day.day}天奖励类型"><option value="4" ${day.reward.type === 4 ? 'selected' : ''}>金币</option><option value="10" ${day.reward.type === 10 ? 'selected' : ''}>水晶</option></select></div></td><td><input class="login-amount" type="number" min="1" max="10000000" step="1" value="${day.reward.num}" aria-label="第${day.day}天奖励数量"></td><td><input class="login-comment" maxlength="100" value="${esc(day.comment)}" aria-label="第${day.day}天奖励说明"></td></tr>`,
        )
        .join('') || emptyRow(4, '无配置');
  $('#player-navis').innerHTML =
    p.navigators
      .map((n) => {
        const name = playerPolicy.names[n.navi_id] || '看板';
        return `<div class="navi-item ${n.enabled ? '' : 'off'}" data-id="${n.navi_id}"><label class="switch" title="开放购买"><input class="navi-enabled" type="checkbox" ${n.enabled ? 'checked' : ''} aria-label="开放购买 ${esc(name)}"></label><span class="navi-name" title="${esc(name)} · ${n.navi_id}"><b>${esc(name)}</b><small>ID ${n.navi_id}</small></span><span class="input-unit"><input class="navi-price" type="number" min="1" max="10000000" step="1" value="${n.price}" aria-label="${esc(name)}水晶价格"><span>水晶</span></span></div>`;
      })
      .join('') || '<p class="empty">没有可配置的看板</p>';
  playerPolicyChanged();
}
function playerMailDraft() {
  return $$('#player-mail-rewards tr[data-index]').map((row) => {
    const r = structuredClone(playerPolicy.mailRewards[Number(row.dataset.index)]);
    for (const input of row.querySelectorAll('[data-field]')) r[input.dataset.field] = Number(input.value);
    return r;
  });
}
function rememberPlayerMailEntries(entries) {
  for (const e of entries || []) playerPolicy.mailEntries.set(contentKey(e), e);
  rememberCatalogRows(entries || []);
}
function renderPlayerMail() {
  $('#player-mail-count').textContent = `${playerPolicy.mailRewards.length} / 120 种奖励`;
  $('#player-mail-rewards').innerHTML =
    playerPolicy.mailRewards
      .map((r, i) => {
        const e = playerPolicy.mailEntries.get(contentKey(r)),
          card = r.type === 6,
          max = [14, 16, 18].includes(r.type) ? 1 : [6, 15, 19].includes(r.type) ? 100 : 10000000;
        const input = (field, value, min, max, label, fixed = false) =>
          `<input data-field="${field}" aria-label="${esc(contentLabel(r))} ${label}" title="${fixed ? '炼金技能卡获得即满级' : ''}" type="number" required step="1" min="${min}" max="${max}" value="${value}" ${fixed ? 'disabled' : ''}>`;
        return `<tr data-index="${i}"><td class="cell-wrap"><div class="reward-cell">${contentThumb(r)}<div><b>${contentName(r, e?.name || contentLabel(r))}</b><span class="sub">${r.reward_typeid ? `<code>${r.reward_typeid}</code>` : '货币'} ${esc(card ? cardJobName(e?.arthur_type) : '')}</span></div></div></td><td>${input('num', r.num, 1, max, '数量')}</td><td>${card ? input('card_lv', e?.fixed_card_level || r.card_lv, 1, e?.level_max || r.card_lv, '卡牌等级', !!e?.fixed_card_level) : '—'}</td><td>${card ? input('card_fame', r.card_fame, 1, e?.fame_max || r.card_fame, '名声') : '—'}</td><td>${card ? input('card_love', r.card_love, 0, e?.love_max || 0, '忠诚度') : '—'}</td><td><button class="x-button" data-remove="${i}" aria-label="移除${esc(e?.name || contentLabel(r))}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`;
      })
      .join('') || emptyRow(6, '尚未选择奖励', '开启毕业邮件前请至少添加一项。');
  $$('#player-mail-rewards [data-remove]').forEach(
    (b) =>
      (b.onclick = () => {
        playerPolicy.mailRewards = playerMailDraft();
        playerPolicy.mailRewards.splice(Number(b.dataset.remove), 1);
        renderPlayerMail();
        playerPolicyChanged();
      }),
  );
}
$('#player-mail-add').onclick = () =>
  openContentPicker(
    (rows) => {
      const rewards = playerMailDraft(),
        keys = new Set(rewards.map(contentKey)),
        added = rows.filter((r) => !keys.has(contentKey(r)));
      if (rewards.length + added.length > 120) throw new Error('毕业奖励最多120种');
      rememberPlayerMailEntries(rows);
      playerPolicy.mailRewards = [...rewards, ...added.map(contentReward)];
      renderPlayerMail();
      playerPolicyChanged();
    },
    ['currency', 'card', 'material', 'item', 'sphere', 'buddy', 'costume', 'stamp', 'honor'],
  );
async function loadPlayerPolicy() {
  const data = await api('/api/player-policy');
  playerPolicy.saved = data.config;
  playerPolicy.defaults = data.defaults;
  playerPolicy.revision = data.revision;
  playerPolicy.names = data.navi_names;
  homeBannerEditor.gachas = data.home_banner_gachas || {};
  homeBannerEditor.scoreBosses = data.home_banner_score_bosses || {};
  rememberPlayerMailEntries(data.mail_reward_entries);
  await resolveCatalogEntries([
    [4, 0],
    [10, 0],
  ]).catch(() => {});
  renderPlayerPolicy(data.config);
}
$('#player-policy').oninput = playerPolicyChanged;
$('#player-policy').onchange = (e) => {
  if (e.target.classList.contains('login-kind'))
    e.target.previousElementSibling.outerHTML = loginThumb(Number(e.target.value));
  playerPolicyChanged();
};
$('#player-policy-default').onclick = async () => {
  if (await askConfirm('载入包内默认配置？', '当前页面的修改会被替换，保存后才生效。', { ok: '载入默认' }))
    renderPlayerPolicy(playerPolicy.defaults);
};
$('#player-navi-batch').onclick = async () => {
  const input = $('#player-navi-batch-price'),
    price = Number(input.value);
  input.classList.remove('invalid');
  if (!Number.isInteger(price) || price < 1 || price > 10000000) {
    markInvalid(input);
    toast('价格须为1至10000000的整数', true);
    return;
  }
  if (
    !(await askConfirm(
      `把全部 ${$$('#player-navis .navi-price').length} 个看板的价格设为 ${num(price)} 水晶？`,
      '保存前可在变更核对中查看。',
      { ok: '应用' },
    ))
  )
    return;
  $$('#player-navis .navi-price').forEach((el) => (el.value = price));
  playerPolicyChanged();
};
$('#player-policy-save').onclick = () =>
  reviewedSave(
    'player-policy',
    () => {
      const section = $('#player-policy');
      clearInvalid(section);
      const invalid = [...section.querySelectorAll('input,textarea')].filter((el) => !el.checkValidity());
      if (invalid.length) {
        invalid.forEach((el) => markInvalid(el));
        invalid[0].scrollIntoView({ block: 'center' });
        invalid[0].reportValidity();
        showViewAlert('player-policy', {
          title: `不能保存：${invalid.length} 个字段超出范围`,
          message: '已用红框标出，请修正后再保存。',
        });
        return null;
      }
      const config = playerPolicyDraft(),
        mail = config.tutorial_completion_mail;
      if (mail.enabled && (!mail.title || !mail.message || !mail.rewards.length)) {
        if (!mail.title) markInvalid($('#player-mail-title'));
        if (!mail.message) markInvalid($('#player-mail-message'));
        $('#pp-mail').scrollIntoView({ block: 'start' });
        showViewAlert('player-policy', {
          title: '不能保存：新手毕业邮件未填写完整',
          message: '开启毕业邮件时须填写标题、正文并至少选择一项奖励。',
        });
        return null;
      }
      const bannerError = homeBannerValidation(config.home_banners);
      if (bannerError) {
        markHomeBannerProblems();
        showViewAlert('player-policy', { title: '不能保存：首页轮播未填写完整', message: bannerError });
        return null;
      }
      if (
        config.notice.start_unix &&
        config.notice.end_unix &&
        config.notice.end_unix <= config.notice.start_unix
      ) {
        markInvalid($('#player-notice-end'));
        $('#pp-notice').scrollIntoView({ block: 'start' });
        showViewAlert('player-policy', {
          title: '不能保存：公告显示时间无效',
          message: '结束显示时间须晚于开始显示时间。',
        });
        return null;
      }
      return {
        config,
        review: {
          title: '核对公告与奖励变更',
          sections: playerPolicyDiff(playerPolicy.saved, config),
          note: `毕业邮件${mail.enabled ? '开启' : '关闭'}，仅在整个新手训练完成时发放；已有领取记录和看板所有权保留。`,
        },
      };
    },
    async ({ config }) => {
      const data = await api('/api/player-policy', {
        method: 'PUT',
        body: JSON.stringify({ expected_revision: playerPolicy.revision, config }),
      });
      playerPolicy.saved = data.config;
      playerPolicy.revision = data.revision;
      rememberPlayerMailEntries(data.mail_reward_entries);
      renderPlayerPolicy(data.config);
      clearViewAlert('player-policy');
      state.loaded.delete('audit');
      toast('公告与奖励配置已保存');
    },
  );

const loginKindNames = { cycle: '循环签到', beginner: '新手签到', total_milestones: '累计登录' };
function playerPolicyDiff(a, b) {
  const t = (v) => (v ? describeTime(v) : ''),
    mailText = (r) =>
      `${contentLabel(r)} ×${r.num}${r.type === 6 ? ` · Lv${r.card_lv}／名声${r.card_fame}／忠诚${r.card_love}` : ''}`,
    clip = (v) => (v.length > 80 ? v.slice(0, 80) + '…' : v);
  const sections = [
    {
      title: '游戏公告',
      rows: diffRows([
        ['显示公告', a.notice.enabled, b.notice.enabled],
        ['标题', a.notice.title, b.notice.title],
        ['正文', clip(a.notice.body), clip(b.notice.body)],
        ['开始显示', t(a.notice.start_unix), t(b.notice.start_unix)],
        ['结束显示', t(a.notice.end_unix), t(b.notice.end_unix)],
      ]),
    },
    {
      title: '新手毕业邮件',
      rows: [
        ...diffRows([
          ['发送毕业邮件', a.tutorial_completion_mail.enabled, b.tutorial_completion_mail.enabled],
          ['邮件标题', a.tutorial_completion_mail.title, b.tutorial_completion_mail.title],
          ['邮件正文', a.tutorial_completion_mail.message, b.tutorial_completion_mail.message],
        ]),
        ...keyedDiff(
          '奖励',
          a.tutorial_completion_mail.rewards || [],
          b.tutorial_completion_mail.rewards || [],
          contentKey,
          mailText,
          contentLabel,
        ),
      ],
    },
    {
      title: '剧情首通',
      rows: diffRows([['每话首通水晶', a.story_first_clear_crystals, b.story_first_clear_crystals]]),
    },
  ];
  sections.push({
    title: '首页轮播',
    rows: diffRows([
      ['使用自定义轮播', !!a.home_banners?.custom, b.home_banners.custom],
      ['图片、顺序和跳转', homeBannerSummary(a.home_banners), homeBannerSummary(b.home_banners)],
    ]),
  });
  const login = [];
  for (const kind of playerLoginKinds)
    b.login_rewards[kind].forEach((d, i) => {
      const o = a.login_rewards[kind][i],
        txt = (x) =>
          `${x.reward.type === 10 ? '水晶' : '金币'} ×${x.reward.num}${x.comment ? ` · ${x.comment}` : ''}`;
      login.push(...diffRows([[`${loginKindNames[kind]} 第${d.day}天`, o && txt(o), txt(d)]]));
    });
  sections.push({ title: '签到奖励', rows: login });
  const old = new Map(a.navigators.map((n) => [n.navi_id, n])),
    navis = [];
  for (const n of b.navigators) {
    const o = old.get(n.navi_id),
      name = `${playerPolicy.names[n.navi_id] || '看板'} ${n.navi_id}`;
    navis.push(
      ...diffRows([
        [`${name} 开放购买`, o?.enabled, n.enabled],
        [`${name} 价格（水晶）`, o?.price, n.price],
      ]),
    );
  }
  sections.push({ title: '看板购买', rows: navis });
  return sections;
}
function maximizePlayerMail(field, catalogField) {
  const rewards = playerMailDraft();
  for (const r of rewards) {
    if (r.type !== 6) continue;
    const e = playerPolicy.mailEntries.get(contentKey(r));
    if (!e || !Number.isInteger(e[catalogField]) || e[catalogField] < 1) {
      toast('卡牌上限数据缺失，请重新加载配置', true);
      return;
    }
    r[field] = e[catalogField];
  }
  playerPolicy.mailRewards = rewards;
  renderPlayerMail();
  playerPolicyChanged();
}
$('#player-mail-max-level').onclick = () => maximizePlayerMail('card_lv', 'level_max');
$('#player-mail-max-fame').onclick = () => maximizePlayerMail('card_fame', 'fame_max');
$('#player-mail-max-love').onclick = () => {
  const rewards = playerMailDraft();
  for (const r of rewards) {
    if (r.type !== 6) continue;
    const e = playerPolicy.mailEntries.get(contentKey(r));
    if (!e || !Number.isInteger(e.love_max)) {
      toast('卡牌忠诚度上限数据缺失，请重新加载配置', true);
      return;
    }
    r.card_love = e.love_max;
  }
  playerPolicy.mailRewards = rewards;
  renderPlayerMail();
  playerPolicyChanged();
};
