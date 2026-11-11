'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

// Run the actual classic-script callbacks and data builders. Only browser
// widgets/rendering and the catalog lookup are replaced; no running server.
function editor() {
  const elements = new Map(),
    downloads = [],
    errors = [];
  function element(id = '') {
    if (!elements.has(id))
      elements.set(id, {
        value: '',
        files: [],
        dataset: {},
        checked: false,
        style: {},
        classList: {
          add() {},
          remove() {},
          toggle() {},
          contains() {
            return false;
          },
        },
        addEventListener() {},
        setAttribute() {},
        removeAttribute() {},
        focus() {},
        click() {},
        querySelector() {
          return element(id + ' child');
        },
        querySelectorAll() {
          return [];
        },
      });
    return elements.get(id);
  }
  const context = vm.createContext({
    console,
    Blob,
    structuredClone,
    URLSearchParams,
    Intl,
    $: element,
    $$: () => [],
    document: {
      createElement: () => element('link'),
      addEventListener() {},
      querySelectorAll() {
        return [];
      },
    },
    window: { addEventListener() {} },
    URL: {
      createObjectURL(blob) {
        downloads.push(blob);
        return 'blob:test';
      },
      revokeObjectURL() {},
    },
    setTimeout() {},
    clearTimeout() {},
    localStorage: {
      getItem() {
        return null;
      },
      setItem() {},
    },
    state: { loaded: new Set(), publishing: new Set() },
    selectedPlayers: new Map(),
    esc: String,
    num: String,
    catalogInfo: new Map(),
    toast(message, error) {
      if (error) errors.push(message);
    },
    askConfirm: async () => true,
    api: async (_route, request) => ({
      entries: JSON.parse(request.body).rewards.map((r) => {
        if ((r.reward_type_id ?? r.reward_typeid) === 9999) throw new Error('unknown reward');
        return { reward_type: r.reward_type, reward_type_id: r.reward_type_id, name: 'reward' };
      }),
    }),
  });
  const run = (source) => vm.runInContext(source, context);
  for (const name of [
    'admin_kit.js',
    'admin_content.js',
    'admin_tower_drops.js',
    'admin_pool_gifts.js',
    'admin_operations.js',
    'admin_mail.js',
  ]) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../web', name), 'utf8'), context, {
      filename: name,
    });
  }
  run(`
    contentAction = async (_view, action) => action();
    runPoolEdit = async action => action();
    rememberCatalogRows = () => {};
    renderActivityRewards = activityChanged = renderDropRows = syncPoolModes = changedContent = () => {};
    renderExchangeForm = renderBasket = renderCatalog = () => {};
    renderPoolOddsCount = renderPoolCategoryOptions = renderStrategies = renderPoolCover = poolChanged = () => {};
    selectPoolVariant = id => {
      poolEditor.activeID = id;
      poolEditor.selected = poolEditor.contents.get(id).selected;
      poolEditor.fames = poolEditor.contents.get(id).fames;
    };
    downloadJSON = (name,value) => downloadContent(value,name);
    resolveCatalogEntries = async () => {};
  `);
  return {
    run,
    errors,
    async export(id) {
      await element(id).onclick();
      return JSON.parse(await downloads.at(-1).text());
    },
    async import(id, data) {
      const input = element(id);
      input.files = [new Blob([JSON.stringify(data)])];
      await input.onchange({ target: input });
      assert.equal(input.value, '');
    },
    get(expression) {
      return JSON.parse(run(`JSON.stringify(${expression})`));
    },
    set(expression, value) {
      context.input = value;
      run(`${expression} = structuredClone(input)`);
    },
  };
}

const reward = {
  type: 6,
  num: 2,
  reward_typeid: 101,
  card_lv: 1,
  card_fame: 7,
  card_love: 40,
  card_skill_lv: [],
};
const cup = {
  end_turn: 0,
  rate: 100,
  team_cost_initial: 7,
  member_dead_end: false,
  burst_gauge_initial: 300,
  grades: Array.from({ length: 9 }, (_, i) => ({ score: i * 100 })),
};

test('activity export reimports omitted empty rewards and retains cup rules and zero chances', async () => {
  const page = editor();
  const value = {
    cups: { 11: cup, 12: cup },
    explore: [{ event: 0, kind: 'symbols', index: 0, rewards: [reward], chance_per_million: [0] }],
  };
  page.set('activityEditor.config', value);
  const file = await page.export('#activity-export');
  await page.import('#activity-import', file);
  const imported = page.get('activityEditor.config');
  for (const p of Object.values(imported.cups)) {
    assert.equal(p.burst_gauge_initial, 300);
    assert.equal(p.team_cost_initial, 7);
    assert.ok(p.grades.every((g) => Array.isArray(g.rewards) && g.rewards.length === 0));
  }
  assert.deepEqual(imported.explore, value.explore);
});

test('activity catalog rejection leaves the previous draft intact', async () => {
  const page = editor();
  const value = {
    cups: { 11: { ...cup, grades: cup.grades.map((g) => ({ ...g, rewards: [] })) } },
    explore: [],
  };
  page.set('activityEditor.config', value);
  const bad = structuredClone(value);
  bad.cups[11].grades[0].rewards = [{ ...reward, reward_typeid: 9999 }];
  await assert.rejects(page.import('#activity-import', bad), /unknown reward/);
  assert.deepEqual(page.get('activityEditor.config'), value);
});

function tower(page) {
  page.set('dropEditor.row', {
    tower_id: 7,
    floor: 2,
    boss_id: 702,
    targets: [{ battle_index: 0, enemy_index: 0 }],
  });
  page.set('dropEditor.base', { first_clear_rewards: [] });
  page.set('dropEditor.config', {
    tower_id: 7,
    floor: 2,
    boss_id: 702,
    enemy_drops: [],
    fame_rewards: [],
    first_clear_rewards: [],
  });
}

test('tower first-clear catalog rejection does not replace the old draft', async () => {
  const page = editor();
  tower(page);
  const before = page.get('dropEditor.config'),
    file = await page.export('#drop-export');
  file.first_clear_rewards = [{ ...reward, reward_typeid: 9999 }];
  await assert.rejects(page.import('#drop-import', file), /unknown reward/);
  assert.deepEqual(page.get('dropEditor.config'), before);
});

test('tower export reimports inheritance/disabled pools and card instance fields', async () => {
  const page = editor();
  tower(page);
  for (const first of [null, [], [reward]]) {
    const value = page.get('dropEditor.config');
    value.enemy_drops = [{ battle_index: 0, enemy_index: 0, chance_per_million: 0, reward }];
    value.first_clear_rewards = first;
    page.set('dropEditor.config', value);
    await page.import('#drop-import', await page.export('#drop-export'));
    assert.deepEqual(page.get('dropEditor.config'), value);
  }
});

test('reward basket export reimports quantity, card and LE level fields', async () => {
  const page = editor();
  const requests = [
    { reward_type: 6, reward_type_id: 101, quantity: 2, card_level: 50, card_fame: 30, card_love: 60 },
    { reward_type: 19, reward_type_id: 201, quantity: 3, card_level: 70, card_fame: 0, card_love: 0 },
  ];
  page.set('mailWorkspace.basket', new Map(requests.map((r, i) => [i, { request: r }])));
  const file = await page.export('#basket-export');
  await page.import('#basket-import', file);
  assert.deepEqual(page.get('[...mailWorkspace.basket.values()].map(x=>x.request)'), requests);
});

function seedPool(page, configs) {
  page.set('globalThis.poolFixture', configs);
  page.run(`{
    const configs = poolFixture;
    poolEditor.variants = configs.map(c => ({gacha_id:c.gacha_id, base:{groupid:10,pay_type:c.pay_type,pay_typeid:c.pay_typeid,category_num:c.category_num}}));
    poolEditor.row = poolEditor.variants[0];
    poolEditor.activeID = configs[0].gacha_id;
    poolEditor.categories = [{id:4}];
    poolEditor.contents = new Map(configs.map(c => [c.gacha_id,poolVariantState(c)]));
    poolEditor.strategies = new Map(configs.map(c=>[c.gacha_id,{open:!c.closed,name:c.name,pay_type:c.pay_type,pay_typeid:c.pay_typeid,price:c.price}]));
    poolEditor.mixedByID = new Map(configs.filter(c=>c.reward_pool).map(c=>[c.gacha_id,poolMixedState(c)]));
    poolEditor.randomGifts = new Map(configs.map(c=>[c.gacha_id,poolGiftState(c.random_gifts)]));
    poolEditor.selected = poolEditor.contents.get(poolEditor.activeID).selected;
    poolEditor.fames = poolEditor.contents.get(poolEditor.activeID).fames;
    poolEditor.coverPath = configs[0].cover_path || '';
    $('#pool-play-limit').value = configs[0].play_count_max ?? 0;
    $('#pool-daily-limit').value = configs[0].play_count_1day_max ?? 0;
    $('#pool-category').value = configs[0].category_num;
    $('#pool-odds-msg').value = configs[0].odds_msg || '';
    $('#pool-start').value = $('#pool-end').value = '';
  }`);
}

test('whole-pool export reimports item payment, per-method gifts, fame and step rewards', async () => {
  const page = editor();
  const base = {
    name: 'pool',
    price: 50,
    play_count_max: 0,
    play_count_1day_max: 3,
    category_num: 4,
    odds_msg: 'odds',
    start_unix: 0,
    end_unix: 0,
    card_ids: [],
    weights: [],
    pay_typeid: 0,
  };
  seedPool(page, [
    {
      ...base,
      gacha_id: 11,
      pay_type: 4,
      pay_typeid: 1304,
      card_ids: [101],
      weights: [4],
      card_fames: { 101: 30 },
      random_gifts: [{ from_play: 1, to_play: 3, empty_weight: 7, pool: [{ weight: 2, reward }] }],
    },
    {
      ...base,
      gacha_id: 12,
      pay_type: 6,
      closed: true,
      reward_pool: [{ weight: 7, reward }],
      steps: [
        { price: 50, pay_type: 6, reward_pool: [{ weight: 7, reward }] },
        { price: 100, pay_type: 4, pay_typeid: 1304, reward_pool: [{ weight: 1, reward }] },
      ],
    },
  ]);
  const file = await page.export('#pool-export');
  await page.import('#pool-import', file);
  assert.deepEqual(await page.export('#pool-export'), file);
});

test('pool import retains its own special category while rejecting an unrelated category', async () => {
  const page = editor();
  seedPool(page, [
    {
      gacha_id: 11,
      name: 'pool',
      price: 50,
      pay_type: 3,
      pay_typeid: 0,
      category_num: 99,
      card_ids: [101],
      weights: [1],
    },
  ]);
  const file = await page.export('#pool-export');
  await page.import('#pool-import', file);
  assert.deepEqual(await page.export('#pool-export'), file);
  const invalid = structuredClone(file);
  invalid.configs[0].category_num = 98;
  await assert.rejects(page.import('#pool-import', invalid), /扭蛋分类无效/);
  assert.deepEqual(await page.export('#pool-export'), file);
});

test('malformed mixed-pool steps do not replace any part of the previous draft', async () => {
  const page = editor();
  seedPool(page, [
    {
      gacha_id: 11,
      name: 'pool',
      price: 50,
      pay_type: 3,
      pay_typeid: 0,
      category_num: 4,
      card_ids: [],
      weights: [],
      reward_pool: [{ weight: 1, reward }],
      steps: [{ price: 50, reward_pool: [{ weight: 1, reward }] }],
    },
  ]);
  const file = await page.export('#pool-export'),
    invalid = structuredClone(file);
  invalid.configs[0].price = 100;
  invalid.configs[0].play_count_1day_max = 9;
  invalid.configs[0].steps = { reward_pool: [] };
  await assert.rejects(page.import('#pool-import', invalid), /混合奖励表或阶段格式无效/);
  assert.deepEqual(await page.export('#pool-export'), file);
});

test('exchange export reimports prices, stock, scheduling and reward fields', async () => {
  const page = editor();
  page.set('exchangeEditor.shop', {
    trade_shopid: 7,
    name: 'shop',
    text: 'description',
    tab_type: 1,
    disabled: false,
    end_time: 2147483647,
    lineups: [
      {
        lineupid: 701,
        lineup_name: 'item',
        stock_num: 2,
        disabled: true,
        rewards: [reward],
        prices: [{ type: 8, price_typeid: 8000, num: 3 }],
      },
    ],
  });
  page.run(
    `$('#exchange-name').value='shop';$('#exchange-text').value='description';$('#exchange-enabled').checked=true;$('#exchange-tab').value='1';`,
  );
  const file = await page.export('#exchange-export');
  await page.import('#exchange-import', file);
  assert.deepEqual(await page.export('#exchange-export'), file);
});
