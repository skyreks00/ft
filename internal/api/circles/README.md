# Holy graph tables

The 42 API does not expose circles or specialization tracks to students, so
ft ships them, extracted from the intra's holy graph
(`https://projects.intra.42.fr/projects/graph`):

- `<campus>-<cursus>.json`: a campus' graph for a cursus, from
  `project_data.json` (below). It is the graph of the "common-core" quest.
- `<cursus>-<quest>.json`: the core circles of a curriculum variant, such as
  42Next, which the page does not take from `project_data.json` but draws from
  a list in its own script (`hack.list[<cursus>].quests[<quest>].circles`).
  ft uses it for students in that quest (`/v2/users/:login/quests_users`),
  with the campus graph's projects after the common core.

## Campus graph
A project's circle comes from its graph rules ("You must have finished
quest(s) common-core-rank-NN" → circle NN+1; "… common-core" → after the
common core), its tracks from the graph's layers.

To regenerate (e.g. when the cursus changes): open
`https://projects.intra.42.fr/projects/graph?login=<you>` while logged in,
choose the cursus, and run this in the browser console; save the output as
`<campus>-<cursus>.json` here.

```js
(async () => {
  const cursus = 21, campus = 12; // adjust
  const login = new URL(location).searchParams.get('login');
  const g = (await (await fetch(`/project_data.json?cursus_id=${cursus}&campus_id=${campus}&login=${login}`)).json())
    .filter(p => p.x || p.y);
  const layers = [...document.querySelectorAll('select option')]
    .filter(o => o.value.startsWith('['))
    .map(o => ({name: o.textContent.trim(), ids: JSON.parse(o.value)}));
  const circle = p => {
    const r = p.rules || '';
    let m = r.match(/finished quest\(s\) common-core-rank-(\d+)/) || r.match(/quests:? common-core-rank-(\d+)/);
    if (m) return +m[1] + 1;
    if (Math.hypot(p.x - 3000, p.y - 3000) <= 5) return 0;
    return 7;
  };
  const projects = g.map(p => [p.project_id, p.slug, circle(p),
      layers.map((l, i) => l.ids.includes(p.project_id) ? i : -1).filter(i => i >= 0)])
    .sort((a, b) => a[2] - b[2] || a[1].localeCompare(b[1]));
  copy(JSON.stringify({generated: new Date().toISOString().slice(0, 10), campus_id: campus,
    cursus_id: cursus, tracks: layers.map(l => l.name), projects}));
  console.log('copied', projects.length, 'projects');
})();
```

## Curriculum variant (42Next)

On the same page, in the browser console:

```js
(async () => {
  const src = performance.getEntriesByType('resource').map(e => e.name).find(n => /application-.*\.js/.test(n));
  const js = await (await fetch(src)).text();
  const start = js.indexOf('hack.list = ') + 'hack.list = '.length;
  let depth = 0, i = start, q = null;
  for (; i < js.length; i++) {
    const c = js[i];
    if (q) { if (c === '\\') { i++; continue; } if (c === q) q = null; continue; }
    if (c === '"' || c === "'") { q = c; continue; }
    if (c === '{') depth++; else if (c === '}' && --depth === 0) { i++; break; }
  }
  const list = Function('return (' + js.slice(start, i) + ')')();
  const cursus = 21, quest = '42next'; // adjust
  const circles = list[cursus].quests[quest].circles.map(c => c.projects.map(p => {
    const o = {kind: p.kind.replace('inner_', '')};
    if (p.slug) o.slug = p.slug;
    if (p.replacement) o.replacement = p.replacement;
    if (p.slugs || p.satellites) o.slugs = p.slugs || p.satellites;
    return o;
  }));
  copy(JSON.stringify({generated: new Date().toISOString().slice(0, 10), cursus_id: cursus, quest, circles}));
})();
```

Save the output as `<cursus>-<quest>.json` (e.g. `42cursus-42next.json`).
