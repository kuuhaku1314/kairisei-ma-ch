'use strict';
const evolutionEditor = {
  nodes: new Map(),
  edges: [],
  families: [],
  closed: new Set(),
  saved: new Set(),
  revision: 0,
  selected: 0,
  page: 0,
  origin: 'auto',
  zoom: 1,
  loaded: false,
};
const evolutionKey = (edge) => `${edge.from_cardid}:${edge.to_cardid}`;
const evolutionType = (type) => ['普通进化', '乖离进化', '骑士进化', '限界突破'][type] || '进化';
const evolutionRarity = (n) => cardRarityLabel(n.rarity, n.limit_count || 0);
// Avatar fallback letter: skip the 【title】 prefix so a missing image shows the card's own initial.
const evoLabel = (name) => String(name || '').replace(/^【[^】]*】/, '') || name;
const evolutionOrder = (a, b) => (a.rarity || 0) - (b.rarity || 0) || a.card_id - b.card_id;
function evolutionDirty() {
  const e = evolutionEditor;
  return e.loaded && (e.closed.size !== e.saved.size || [...e.closed].some((key) => !e.saved.has(key)));
}
async function loadEvolutionEditor() {
  const data = await api('/api/evolution-policy'),
    e = evolutionEditor;
  e.nodes = new Map(data.nodes.map((n) => [n.card_id, n]));
  e.edges = data.edges;
  e.revision = data.revision;
  e.closed = new Set((data.closed_paths || []).map(evolutionKey));
  e.saved = new Set(e.closed);
  e.loaded = true;
  const neighbors = new Map(data.nodes.map((n) => [n.card_id, new Set()]));
  for (const edge of e.edges) {
    neighbors.get(edge.from_cardid).add(edge.to_cardid);
    neighbors.get(edge.to_cardid).add(edge.from_cardid);
  }
  const unseen = new Set(e.nodes.keys()),
    familyOf = new Map();
  e.families = [];
  while (unseen.size) {
    const first = unseen.values().next().value,
      queue = [first],
      nodes = [];
    unseen.delete(first);
    for (let i = 0; i < queue.length; i++) {
      const id = queue[i];
      nodes.push(e.nodes.get(id));
      for (const other of neighbors.get(id)) if (unseen.delete(other)) queue.push(other);
    }
    nodes.sort(evolutionOrder);
    const family = {
      id: Math.min(...queue),
      nodes,
      edges: [],
      search: nodes.map((n) => `${n.name} ${n.card_id}`).join(' '),
    };
    e.families.push(family);
    for (const id of queue) familyOf.set(id, family);
  }
  for (const edge of e.edges) familyOf.get(edge.from_cardid).edges.push(edge);
  e.families.sort((a, b) => a.id - b.id);
  if (!e.families.some((f) => f.id === e.selected)) e.selected = e.families[0]?.id || 0;
  selectEvolutionFamily(e.selected);
  updateEvolutionDraft();
}
function evolutionFiltered() {
  return evolutionEditor.families.filter(
    (f) =>
      matchesWords(f.search, $('#evolution-search').value.trim()) &&
      (!$('#evolution-filter-closed').checked ||
        f.edges.some((edge) => evolutionEditor.closed.has(evolutionKey(edge)))),
  );
}
const evolutionClosedCount = (f) =>
  f.edges.filter((edge) => evolutionEditor.closed.has(evolutionKey(edge))).length;
function renderEvolutionFamilies() {
  const e = evolutionEditor,
    rows = evolutionFiltered(),
    pages = Math.max(1, Math.ceil(rows.length / 12));
  e.page = Math.max(0, Math.min(e.page, pages - 1));
  const closedTotal = e.closed.size;
  $('#evolution-total').textContent =
    `${num(e.families.length)} 条链 · ${num(e.edges.length)} 条路线${closedTotal ? ` · 已关闭 ${num(closedTotal)}` : ''}`;
  $('#evolution-total').className = 'pill' + (closedTotal ? ' amber' : '');
  $('#evolution-families').innerHTML =
    rows
      .slice(e.page * 12, (e.page + 1) * 12)
      .map((f) => {
        const card = f.nodes[0],
          closed = evolutionClosedCount(f),
          strip = f.nodes.slice(0, 4);
        return `<button type="button" class="evo-family ${f.id === e.selected ? 'selected' : ''}" data-family="${f.id}" aria-pressed="${f.id === e.selected}"><span class="evo-family-top"><span class="evo-strip">${strip.map((n) => `<span class="thumb">${image(n.image_url, evoLabel(n.name))}</span>`).join('')}${f.nodes.length > 4 ? `<span class="evo-more">+${f.nodes.length - 4}</span>` : ''}</span>${closed ? `<span class="tag bad">关闭 ${closed}</span>` : '<span class="tag ok">全部开放</span>'}</span><span class="evo-family-text"><b title="${esc(card.name)}">${esc(card.name)}</b><small>${card.card_id} · ${f.nodes.length} 张 · ${f.edges.length} 条路线</small></span></button>`;
      })
      .join('') || '<div class="empty"><b>没有匹配的进化链</b>换个名称或 ID 再试。</div>';
  $('#evolution-page').textContent = `${e.page + 1} / ${pages} 页 · ${num(rows.length)} 条链`;
  $('#evolution-prev').disabled = e.page === 0;
  $('#evolution-next').disabled = e.page + 1 === pages;
}
function selectEvolutionFamily(id) {
  const e = evolutionEditor;
  e.selected = id;
  e.origin = 'auto';
  const family = e.families.find((f) => f.id === id);
  $('#evolution-origin').innerHTML =
    '<option value="auto">全部链首形态</option>' +
    (family?.nodes || [])
      .map((n) => `<option value="${n.card_id}">${esc(n.name)} · ${n.card_id}</option>`)
      .join('');
  e.autoFit = true;
  renderEvolutionFamilies();
  renderEvolutionGraph();
}
// Condense native cycles before laying out stages. Each source component gets
// one canonical viewing origin (lowest rarity/ID); an operator can inspect any
// owned intermediate form through the origin selector without changing policy.
function evolutionComponents(family) {
  const next = new Map(family.nodes.map((n) => [n.card_id, []]));
  for (const e of family.edges) next.get(e.from_cardid).push(e.to_cardid);
  let serial = 0;
  const index = new Map(),
    low = new Map(),
    stack = [],
    active = new Set(),
    parts = [],
    partOf = new Map();
  function visit(id) {
    index.set(id, serial);
    low.set(id, serial++);
    stack.push(id);
    active.add(id);
    for (const to of next.get(id)) {
      if (!index.has(to)) {
        visit(to);
        low.set(id, Math.min(low.get(id), low.get(to)));
      } else if (active.has(to)) low.set(id, Math.min(low.get(id), index.get(to)));
    }
    if (low.get(id) === index.get(id)) {
      const members = [];
      let v;
      do {
        v = stack.pop();
        active.delete(v);
        partOf.set(v, parts.length);
        members.push(v);
      } while (v !== id);
      parts.push(members);
    }
  }
  for (const n of family.nodes) if (!index.has(n.card_id)) visit(n.card_id);
  const incoming = parts.map(() => new Set()),
    outgoing = parts.map(() => new Set());
  for (const edge of family.edges) {
    const a = partOf.get(edge.from_cardid),
      b = partOf.get(edge.to_cardid);
    if (a !== b) {
      incoming[b].add(a);
      outgoing[a].add(b);
    }
  }
  const compare = (a, b) => evolutionOrder(evolutionEditor.nodes.get(a), evolutionEditor.nodes.get(b));
  parts.forEach((p) => p.sort(compare));
  const roots = parts.filter((p, i) => incoming[i].size === 0).map((p) => p[0]);
  const levels = parts.map(() => 0),
    counts = incoming.map((s) => s.size),
    queue = counts.flatMap((n, i) => (n === 0 ? [i] : []));
  for (let i = 0; i < queue.length; i++) {
    const a = queue[i];
    for (const b of outgoing[a]) {
      const same =
        evolutionEditor.nodes.get(parts[a][0]).rarity === evolutionEditor.nodes.get(parts[b][0]).rarity;
      if (same) levels[b] = Math.max(levels[b], levels[a] + 1);
      if (--counts[b] === 0) queue.push(b);
    }
  }
  return { next, roots, parts, partOf, levels };
}
// Layered layout: one column per stage (rarity, then MR level), columns centred vertically and ordered by the
// position of their predecessors so parallel branches stay parallel. Route chips avoid nodes and each other.
const EVO = {
  W: 190,
  H: 70,
  gap: 112,
  row: 24,
  stackRow: 56,
  padX: 26,
  top: 52,
  bottom: 28,
  chipW: 84,
  chipH: 24,
};
function evolutionLayout(f) {
  const graph = evolutionComponents(f),
    columns = new Map();
  for (const n of f.nodes) {
    const level = graph.levels[graph.partOf.get(n.card_id)],
      key = `${n.rarity}:${level}`;
    if (!columns.has(key)) columns.set(key, { rarity: n.rarity, level, nodes: [] });
    columns.get(key).nodes.push(n);
  }
  const cols = [...columns.values()].sort((a, b) => a.rarity - b.rarity || a.level - b.level),
    colOf = new Map(),
    preds = new Map(f.nodes.map((n) => [n.card_id, []]));
  cols.forEach((c, i) => c.nodes.forEach((n) => colOf.set(n.card_id, i)));
  for (const edge of f.edges) {
    const a = colOf.get(edge.from_cardid),
      b = colOf.get(edge.to_cardid);
    if (a < b) preds.get(edge.to_cardid).push(edge.from_cardid);
    if (a === b) cols[a].stacked = true;
  }
  cols.forEach((c) => {
    c.gapY = c.stacked ? EVO.stackRow : EVO.row;
    c.height = c.nodes.length * EVO.H + (c.nodes.length - 1) * c.gapY;
  });
  const inner = Math.max(...cols.map((c) => c.height)),
    pos = new Map();
  cols.forEach((c, i) => {
    const centre = (n) => {
      const ys = preds
        .get(n.card_id)
        .map((id) => pos.get(id)?.y)
        .filter((y) => y != null);
      return ys.length ? ys.reduce((a, b) => a + b, 0) / ys.length : Infinity;
    };
    c.nodes.sort((a, b) => centre(a) - centre(b) || evolutionOrder(a, b));
    const y0 = EVO.top + (inner - c.height) / 2,
      x = EVO.padX + i * (EVO.W + EVO.gap);
    c.x = x;
    c.nodes.forEach((n, j) => pos.set(n.card_id, { x, y: y0 + j * (EVO.H + c.gapY), col: i, row: j }));
  });
  return {
    graph,
    cols,
    pos,
    width: EVO.padX * 2 + cols.length * EVO.W + (cols.length - 1) * EVO.gap,
    height: EVO.top + inner + EVO.bottom,
  };
}
function renderEvolutionGraph() {
  const e = evolutionEditor,
    f = e.families.find((f) => f.id === e.selected),
    box = $('#evolution-graph');
  $('#evolution-open-chain').disabled = !f;
  if (!f) {
    box.innerHTML = '<p class="hint">当前目录没有可配置的进化链。</p>';
    $('#evolution-routes').innerHTML = '';
    return;
  }
  const layout = evolutionLayout(f),
    { graph, cols, pos, width } = layout;
  let height = layout.height;
  const roots = e.origin === 'auto' ? graph.roots : [Number(e.origin)],
    reached = new Set(roots),
    queue = [...roots];
  for (let i = 0; i < queue.length; i++) {
    const from = queue[i];
    for (const to of graph.next.get(from) || [])
      if (!e.closed.has(`${from}:${to}`) && !reached.has(to)) {
        reached.add(to);
        queue.push(to);
      }
  }
  const keys = new Set(f.edges.map(evolutionKey)),
    occupied = [...pos.values()].map((p) => ({ x: p.x - 4, y: p.y - 4, w: EVO.W + 8, h: EVO.H + 8 })),
    paths = [],
    chips = [];
  const place = (cx, cy) => {
    for (const dy of [0, -28, 28, -56, 56, -84, 84, -112, 112]) {
      const r = { x: cx - EVO.chipW / 2, y: cy + dy - EVO.chipH / 2, w: EVO.chipW, h: EVO.chipH };
      if (r.y < EVO.top - 30) continue;
      if (
        !occupied.some(
          (o) => r.x < o.x + o.w + 4 && r.x + r.w + 4 > o.x && r.y < o.y + o.h + 4 && r.y + r.h + 4 > o.y,
        )
      ) {
        occupied.push(r);
        return [cx, cy + dy];
      }
    }
    occupied.push({ x: cx - EVO.chipW / 2, y: cy - EVO.chipH / 2, w: EVO.chipW, h: EVO.chipH });
    return [cx, cy];
  };
  const mid = (p0, p1, p2, p3) => (p0 + 3 * p1 + 3 * p2 + p3) / 8;
  const ordered = [...f.edges].sort(
    (a, b) =>
      pos.get(a.from_cardid).col - pos.get(b.from_cardid).col ||
      pos.get(a.from_cardid).y - pos.get(b.from_cardid).y,
  );
  for (const edge of ordered) {
    const a = pos.get(edge.from_cardid),
      b = pos.get(edge.to_cardid),
      key = evolutionKey(edge),
      closed = e.closed.has(key),
      pair = keys.has(`${edge.to_cardid}:${edge.from_cardid}`);
    const shift = pair ? (edge.from_cardid < edge.to_cardid ? -9 : 9) : 0;
    let d, cx, cy;
    if (a.col !== b.col) {
      const fwd = b.col > a.col,
        x1 = fwd ? a.x + EVO.W : a.x,
        x2 = fwd ? b.x : b.x + EVO.W,
        y1 = a.y + EVO.H / 2 + shift,
        y2 = b.y + EVO.H / 2 + shift,
        dx = (fwd ? 1 : -1) * Math.max(48, Math.abs(x2 - x1) / 2);
      d = `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`;
      cx = mid(x1, x1 + dx, x2 - dx, x2);
      cy = mid(y1, y1, y2, y2);
    } else if (Math.abs(a.row - b.row) === 1) {
      const down = b.row > a.row,
        x = a.x + EVO.W / 2 + (pair ? (down ? -46 : 46) : 0),
        y1 = down ? a.y + EVO.H : a.y,
        y2 = down ? b.y : b.y + EVO.H;
      d = `M${x},${y1} L${x},${y2}`;
      cx = x;
      cy = (y1 + y2) / 2;
      occupied.push({ x: cx - EVO.chipW / 2, y: cy - EVO.chipH / 2, w: EVO.chipW, h: EVO.chipH });
    } else {
      const right = edge.from_cardid < edge.to_cardid,
        x = right ? a.x + EVO.W : a.x,
        bend = (right ? 1 : -1) * 70,
        y1 = a.y + EVO.H / 2,
        y2 = b.y + EVO.H / 2;
      d = `M${x},${y1} C${x + bend},${y1} ${x + bend},${y2} ${x},${y2}`;
      cx = mid(x, x + bend, x + bend, x);
      cy = (y1 + y2) / 2;
    }
    if (a.col !== b.col || Math.abs(a.row - b.row) !== 1) [cx, cy] = place(cx, cy);
    height = Math.max(height, cy + EVO.chipH / 2 + 12);
    paths.push(
      `<path d="${d}" class="evo-route ${closed ? 'closed' : 'open'}${reached.has(edge.from_cardid) ? '' : ' faded'}" marker-end="url(#evo-arrow-${closed ? 'closed' : 'open'})"/>`,
    );
    const from = e.nodes.get(edge.from_cardid),
      to = e.nodes.get(edge.to_cardid);
    chips.push(
      `<button type="button" class="evo-chip ${closed ? 'closed' : 'open'}" style="left:${cx - EVO.chipW / 2}px;top:${cy - EVO.chipH / 2}px" data-edge="${key}" aria-pressed="${!closed}" title="${esc(from.name)}（${from.card_id}）→ ${esc(to.name)}（${to.card_id}）：${closed ? '已关闭，点击开放' : '已开放，点击关闭'}" aria-label="${edge.from_cardid} 到 ${edge.to_cardid} ${evolutionType(edge.type)} ${closed ? '已关闭' : '已开放'}"><i aria-hidden="true"></i>${evolutionType(edge.type)}</button>`,
    );
  }
  const bands = cols
    .map(
      (c, i) =>
        `<rect x="${c.x - 14}" y="10" width="${EVO.W + 28}" height="${height - 20}" rx="14" class="evo-band${i % 2 ? ' alt' : ''}"/><text x="${c.x + EVO.W / 2}" y="33" text-anchor="middle" class="evo-col-label">${esc([...new Set(c.nodes.map(evolutionRarity))].join(' / '))}</text>`,
    )
    .join('');
  const nodes = f.nodes
    .map((n) => {
      const p = pos.get(n.card_id),
        origin = roots.includes(n.card_id);
      return `<article class="evo-node ${reached.has(n.card_id) ? '' : 'unreachable'} ${origin ? 'origin' : ''}" data-node="${n.card_id}" style="left:${p.x}px;top:${p.y}px" title="${esc(n.name)} · ${n.card_id}"><span class="thumb">${image(n.image_url, evoLabel(n.name))}</span><span class="evo-node-text"><b>${esc(n.name)}</b><small>${n.card_id} · ${esc(evolutionRarity(n))}</small></span>${origin ? '<em>起点</em>' : ''}</article>`;
    })
    .join('');
  e.graphWidth = width;
  e.graphHeight = height;
  box.innerHTML = `<div class="evolution-stage" style="width:${width}px;height:${height}px"><svg width="${width}" height="${height}" aria-hidden="true"><defs><marker id="evo-arrow-open" markerWidth="9" markerHeight="9" refX="8" refY="4.5" orient="auto" markerUnits="userSpaceOnUse"><path d="M0,0 L9,4.5 L0,9 Z" class="evo-arrow open"/></marker><marker id="evo-arrow-closed" markerWidth="9" markerHeight="9" refX="8" refY="4.5" orient="auto" markerUnits="userSpaceOnUse"><path d="M0,0 L9,4.5 L0,9 Z" class="evo-arrow closed"/></marker></defs>${bands}${paths.join('')}</svg>${nodes}${chips.join('')}</div>`;
  const closed = evolutionClosedCount(f),
    head = f.nodes[0];
  $('#evolution-head-art').innerHTML = image(head.image_url, evoLabel(head.name));
  $('#evolution-title').textContent = head.name;
  $('#evolution-chain-info').innerHTML =
    `${f.nodes.length} 张卡牌 · ${f.edges.length} 条路线 · ${closed ? `<b class="state-error">已关闭 ${closed} 条</b>` : '全部开放'} · 从起点可达 ${reached.size}/${f.nodes.length} 张`;
  renderEvolutionRoutes(f, pos);
  if (e.autoFit) {
    e.autoFit = false;
    evolutionFit();
  } else setEvolutionZoom(e.zoom);
}
function renderEvolutionRoutes(f, pos) {
  const e = evolutionEditor,
    card = (id) => {
      const n = e.nodes.get(id);
      return `<span class="evo-mini"><span class="thumb">${image(n.image_url, evoLabel(n.name))}</span><span><b>${esc(n.name)}</b><small>${n.card_id} · ${esc(evolutionRarity(n))}</small></span></span>`;
    };
  const rows = [...f.edges].sort(
    (a, b) =>
      pos.get(a.from_cardid).col - pos.get(b.from_cardid).col ||
      pos.get(a.from_cardid).y - pos.get(b.from_cardid).y ||
      pos.get(a.to_cardid).y - pos.get(b.to_cardid).y,
  );
  $('#evolution-routes').innerHTML = rows
    .map((edge) => {
      const key = evolutionKey(edge),
        closed = e.closed.has(key);
      return `<tr class="${closed ? 'closed' : ''}"><td>${card(edge.from_cardid)}</td><td class="evo-arrow-cell" aria-hidden="true">→</td><td>${card(edge.to_cardid)}</td><td><span class="tag ${closed ? 'bad' : 'ok'}">${evolutionType(edge.type)}</span></td><td><label class="switch"><input type="checkbox" data-route="${key}" ${closed ? '' : 'checked'} aria-label="${edge.from_cardid} 到 ${edge.to_cardid} ${evolutionType(edge.type)} 开放"></label></td></tr>`;
    })
    .join('');
  const closed = evolutionClosedCount(f);
  $('#evolution-route-summary').textContent =
    `${f.edges.length} 条 · 开放 ${f.edges.length - closed} · 关闭 ${closed}`;
}
function setEvolutionZoom(value) {
  const e = evolutionEditor,
    stage = $('#evolution-graph .evolution-stage');
  if (!stage) return;
  e.zoom = Math.max(0.4, Math.min(1.5, Math.round(value * 100) / 100));
  stage.style.transform = `scale(${e.zoom})`;
  $('#evolution-graph').style.width = e.graphWidth * e.zoom + 'px';
  $('#evolution-graph').style.height = e.graphHeight * e.zoom + 'px';
  $('#evolution-zoom').textContent = Math.round(e.zoom * 100) + '%';
}
// Fit the chain to the canvas; very long chains stop at 70% and scroll sideways instead of becoming unreadable.
function evolutionFit() {
  const width = $('.evolution-scroll').clientWidth - 2;
  setEvolutionZoom(
    evolutionEditor.graphWidth ? Math.max(0.7, Math.min(1, width / evolutionEditor.graphWidth)) : 1,
  );
}
$('#evolution-zoom-out').onclick = () => setEvolutionZoom(evolutionEditor.zoom - 0.1);
$('#evolution-zoom-in').onclick = () => setEvolutionZoom(evolutionEditor.zoom + 0.1);
$('#evolution-fit').onclick = () =>
  setEvolutionZoom(Math.min(1.5, ($('.evolution-scroll').clientWidth - 2) / evolutionEditor.graphWidth));
function updateEvolutionDraft() {
  const e = evolutionEditor,
    dirty = evolutionDirty();
  $('#evolution-version').textContent = `版本 ${e.revision} · 已关闭 ${e.closed.size} 条`;
  setSaveState(
    '#evolution-draft-state',
    dirty ? '有未保存的路线变更' : '与已发布设置一致',
    dirty ? 'dirty' : 'ok',
  );
  $('#evolution-save').disabled = !dirty || policyBusy('evolution');
  updateDraftIndicators();
}
$('#evolution-search').oninput = $('#evolution-filter-closed').onchange = () => {
  evolutionEditor.page = 0;
  renderEvolutionFamilies();
};
$('#evolution-prev').onclick = () => {
  evolutionEditor.page--;
  renderEvolutionFamilies();
};
$('#evolution-next').onclick = () => {
  evolutionEditor.page++;
  renderEvolutionFamilies();
};
$('#evolution-families').onclick = (event) => {
  const button = event.target.closest('[data-family]');
  if (button) selectEvolutionFamily(Number(button.dataset.family));
};
$('#evolution-origin').onchange = () => {
  evolutionEditor.origin = $('#evolution-origin').value;
  renderEvolutionGraph();
};
function toggleEvolutionRoute(key, focus) {
  const set = evolutionEditor.closed;
  if (!set.delete(key)) set.add(key);
  renderEvolutionGraph();
  renderEvolutionFamilies();
  updateEvolutionDraft();
  $(`${focus}[data-${focus === '#evolution-routes ' ? 'route' : 'edge'}="${key}"]`)?.focus();
}
$('#evolution-graph').onclick = (event) => {
  const button = event.target.closest('[data-edge]');
  if (!button || policyBusy('evolution')) return;
  toggleEvolutionRoute(button.dataset.edge, '#evolution-graph ');
};
$('#evolution-routes').onchange = (event) => {
  const input = event.target.closest('[data-route]');
  if (!input || policyBusy('evolution')) return;
  toggleEvolutionRoute(input.dataset.route, '#evolution-routes ');
};
$('#evolution-open-chain').onclick = () => {
  const f = evolutionEditor.families.find((f) => f.id === evolutionEditor.selected);
  if (!f || !evolutionClosedCount(f)) return toast('本链路线已全部开放');
  for (const edge of f.edges) evolutionEditor.closed.delete(evolutionKey(edge));
  renderEvolutionGraph();
  renderEvolutionFamilies();
  updateEvolutionDraft();
};
// Re-fit when the canvas width changes (window resize, sidebar drawer); manual zoom is kept otherwise.
{
  let last = 0;
  new ResizeObserver((entries) => {
    const w = Math.round(entries[0].contentRect.width);
    if (w && Math.abs(w - last) > 40 && evolutionEditor.graphWidth) {
      last = w;
      evolutionFit();
    }
  }).observe($('.evolution-scroll'));
}
$('#evolution-save').onclick = async () => {
  if (policyBusy('evolution') || !evolutionDirty()) return;
  const e = evolutionEditor,
    keys = [...new Set([...e.closed, ...e.saved])].filter((k) => e.closed.has(k) !== e.saved.has(k)),
    label = (id) => `${e.nodes.get(id)?.name || id}（${id}）`;
  if (
    !(await reviewChanges({
      title: '核对进化路线变更',
      sections: [
        {
          title: '方向独立生效',
          rows: keys.map((key) => {
            const [from, to] = key.split(':').map(Number);
            return [
              `${label(from)} → ${label(to)}`,
              e.saved.has(key) ? '关闭' : '开放',
              e.closed.has(key) ? '关闭' : '开放',
            ];
          }),
        },
      ],
      note: '保存后下次进化请求生效。关闭路线会弹出“该进化路线尚未开放”，不消耗素材或金币；当前持有卡牌保持。',
    }))
  )
    return;
  state.publishing.add('evolution');
  updatePolicyControls();
  try {
    const closed_paths = [...e.closed].map((key) => {
        const [from_cardid, to_cardid] = key.split(':').map(Number);
        return { from_cardid, to_cardid };
      }),
      data = await api('/api/evolution-policy', {
        method: 'PUT',
        body: JSON.stringify({ closed_paths, expected_revision: e.revision }),
      });
    e.revision = data.revision;
    e.closed = new Set(data.closed_paths.map(evolutionKey));
    e.saved = new Set(e.closed);
    clearViewAlert('evolution');
    state.loaded.delete('audit');
    toast('进化限制已保存并生效');
  } catch (error) {
    reportError('evolution', error);
  } finally {
    state.publishing.delete('evolution');
    updatePolicyControls();
    updateEvolutionDraft();
  }
};
