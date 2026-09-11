const test = require('node:test')
const assert = require('node:assert/strict')
const { renderToString } = require('@vue/server-renderer')
const { loader, fixture, tick, nodes, text, vue } = require('./vue-fixture.cjs')

test('late responses cannot replace the selected instance or target an action incorrectly', async () => {
  const view = fixture()
  await tick()
  view.selected.value = 'B'
  await tick()
  assert.equal(view.pending.find(request => request.id === 'A').signal.aborted, true)
  view.pending.find(request => request.id === 'B').resolve({ id: 'B', name: 'Guest B', ports: [] })
  await tick()
  view.pending.find(request => request.id === 'A').resolve({ id: 'A', name: 'Guest A', ports: [] })
  await tick()
  assert.deepEqual(nodes(view.root, node => node.tag === 'h1').map(node => node.text), ['Guest B'])
  assert.ok(view.calls.some(([type, id]) => type === 'metrics' && id === 'B'))
  nodes(view.root, node => node.tag === 'button' && node.text === '启动')[0].props.onClick()
  await tick()
  assert.ok(view.calls.some(([type, id, operation]) => type === 'action' && id === 'B' && operation === 'start'))
  view.app.unmount()
  assert.equal(view.timers.size, 0)
})

test('unmount before an initial response prevents future polling', async () => {
  const view = fixture()
  await tick()
  view.app.unmount()
  view.pending[0].resolve({ id: 'A', name: 'Guest A', ports: [] })
  await tick()
  assert.equal(view.pending[0].signal.aborted, true)
  assert.equal(view.timers.size, 0)
})

test('hidden pages cancel requests and resume when visible', async () => {
  const view = fixture()
  await tick()
  view.visibility(true)
  assert.equal(view.timers.size, 0)
  assert.equal(view.pending[0].signal.aborted, true)
  view.visibility(false)
  await tick()
  assert.equal(view.pending.length, 2)
  view.app.unmount()
})

test('real process and chart components render empty API collections', async () => {
  for (const [filename, name] of [['src/components/instance/ProcessTable.vue', 'processes'], ['src/components/charts/ResourceCharts.vue', 'series']]) {
    for (const value of [[], null]) {
      const component = loader()(filename).default
      await assert.doesNotReject(renderToString(vue.createSSRApp(component, { [name]: value })))
    }
  }
})

test('CPU chart uses historical quota percentage and breaks at invalid samples', async () => {
  const component = loader()('src/components/charts/ResourceCharts.vue').default
  const series = [
    { ts: 0, cpuCores: 0.25, quota: 0.5, quotaPercent: 50, quality: 'ok' },
    { ts: 5, cpuCores: 0, quota: 0.5, quotaPercent: 0, quality: 'missing' },
    { ts: 10, cpuCores: 0.5, quota: 1, quotaPercent: 50, quality: 'ok' },
  ]
  const html = await renderToString(vue.createSSRApp(component, { series }))
  const path = html.match(/ d="([^"]*)"/)[1]
  assert.equal((path.match(/M/g) ?? []).length, 2)
  assert.equal((path.match(/42\.0/g) ?? []).length, 2)
  assert.ok(!path.includes('L'))
})

test('credential response for a previous task never appears on the current task', async () => {
  let completeClaim
  const view = fixture('src/views/TaskView.vue', {
    async task(id) { return { id, status: 'ok', kind: 'create', items: [{ name: id, status: 'ok', step: 'done', credentialAvailable: true }] } },
    credentials() { return new Promise(resolve => { completeClaim = resolve }) },
  })
  await tick()
  nodes(view.root, node => node.tag === 'button' && node.text.includes('领取初始凭据'))[0].props.onClick()
  await tick()
  view.selected.value = 'B'
  await tick()
  completeClaim({ credentials: [{ instanceId: 'A', name: 'A', password: 'previous-task-secret' }] })
  await tick()
  assert.ok(!text(view.root).includes('previous-task-secret'))
  view.app.unmount()
})

test('later credential claims preserve already displayed one-time credentials', async () => {
  let claim = 0
  const view = fixture('src/views/TaskView.vue', {
    async task(id) { return { id, status: 'running', kind: 'create', items: [{ name: 'batch', status: 'ok', credentialAvailable: true }] } },
    async credentials() { claim++; return { credentials: [{ instanceId: String(claim), name: String(claim), password: `fixture-password-${claim}` }] } },
  })
  await tick()
  for (let i = 0; i < 2; i++) {
    nodes(view.root, node => node.tag === 'button' && node.text.includes('领取初始凭据'))[0].props.onClick()
    await tick()
  }
  assert.ok(text(view.root).includes('fixture-password-1'))
  assert.ok(text(view.root).includes('fixture-password-2'))
  view.app.unmount()
})

test('clearing credentials invalidates a claim already in flight', async () => {
  let completeClaim, claims = 0
  const view = fixture('src/views/TaskView.vue', {
    async task(id) { return { id, status: 'running', kind: 'create', items: [{ name: 'batch', status: 'ok', credentialAvailable: true }] } },
    credentials() {
      if (++claims === 1) return Promise.resolve({ credentials: [{ instanceId: '1', name: '1', password: 'fixture-visible' }] })
      return new Promise(resolve => { completeClaim = resolve })
    },
  })
  await tick()
  nodes(view.root, node => node.tag === 'button' && node.text.includes('领取初始凭据'))[0].props.onClick()
  await tick()
  nodes(view.root, node => node.tag === 'button' && node.text.includes('领取初始凭据'))[0].props.onClick()
  await tick()
  nodes(view.root, node => node.tag === 'button' && node.text === '清除显示')[0].props.onClick()
  completeClaim({ credentials: [{ instanceId: '2', name: '2', password: 'fixture-late' }] })
  await tick()
  assert.ok(!text(view.root).includes('fixture-visible'))
  assert.ok(!text(view.root).includes('fixture-late'))
  view.app.unmount()
})

test('name routes accept processes after resolving their canonical instance identity', async () => {
  const view = fixture('src/views/InstanceDetailView.vue', {
    async processes() { return { instanceId: 'uuid-for-A', processes: [{ pid: 7, name: 'fixture-process', user: '0', state: 'S', cpu: 0, rss: 0 }] } },
  })
  await tick()
  for (const request of view.pending) request.resolve({ id: 'uuid-for-A', name: 'A', ports: [] })
  await tick()
  assert.ok(text(view.root).includes('fixture-process'))
  assert.ok(!text(view.root).includes('进程数据与所选实例不一致'))
  view.app.unmount()
})

test('host CPU stays visible when the uplink sample is missing', async () => {
  const component = loader()('src/components/host/HostOverview.vue').default
  const html = await renderToString(vue.createSSRApp(component, { snapshot: { host: {
    cpuUsedCores: 1, cpuPercent: 50, logicalCpus: 2, cpuQuality: 'ok', networkQuality: 'missing', quality: 'missing',
  } } }))
  assert.ok(html.includes('1.00 / 2 核'))
  assert.ok(!html.includes('NaN'))
  assert.ok(!html.includes('0 ↓'))
})
