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

const portFixture = id => ({ id, name: `Guest ${id}`, networkStatus: 'ready', ports: [
  { number: 20000, proto: 'tcp', listenIp: '192.0.2.1', target: 22 },
  { number: 20000, proto: 'udp', listenIp: '192.0.2.1', target: 20000 },
] })

test('failed port addition reloads pending ownership instead of showing success', async () => {
  const saved = portFixture('A')
  const view = fixture('src/views/InstanceDetailView.vue', {
    async instance() { return { ...saved, ports: [...saved.ports] } },
    async addPort(id, number) {
      assert.equal(id, 'A')
      assert.equal(number, 20001)
      saved.networkStatus = 'pending'
      saved.ports.push({ number, proto: 'tcp', listenIp: '192.0.2.1', target: number })
      throw new Error('forward write rejected')
    },
  })
  await tick()
  nodes(view.root, node => node.props['aria-label'] === '追加端口号码')[0].props.onInput({ target: { value: '20001' } })
  nodes(view.root, node => node.tag === 'form')[0].props.onSubmit({ preventDefault() {} })
  await tick()
  assert.ok(text(view.root).includes('forward write rejected'))
  assert.ok(text(view.root).includes('转发尚未确认生效'))
  assert.ok(text(view.root).includes('20001'))
  assert.ok(!text(view.root).includes('（SSH）'))
  view.app.unmount()
})

test('port edit sends the selected number and protocol and preserves its number', async () => {
  const saved = portFixture('A')
  let changed
  const view = fixture('src/views/InstanceDetailView.vue', {
    async instance() { return { ...saved, ports: saved.ports.map(p => ({ ...p })) } },
    async editPort(id, number, proto, target) {
      changed = { id, number, proto, target }
      saved.ports[0].target = target
    },
  })
  await tick()
  nodes(view.root, node => node.tag === 'button' && node.text === '编辑目标')[0].props.onClick()
  await tick()
  nodes(view.root, node => node.props['aria-label'] === '20000 tcp 目标端口')[0].props.onInput({ target: { value: '2222' } })
  nodes(view.root, node => node.tag === 'button' && node.text === '保存目标')[0].props.onClick()
  await tick()
  assert.deepEqual(changed, { id: 'A', number: 20000, proto: 'tcp', target: 2222 })
  assert.ok(text(view.root).includes('2222'))
  assert.ok(text(view.root).includes('20000'))
  assert.ok(!text(view.root).includes('（SSH）'))
  view.app.unmount()
})

test('late port mutation error cannot affect the next instance', async () => {
  let rejectEdit
  const view = fixture('src/views/InstanceDetailView.vue', {
    async instance(id) { return portFixture(id) },
    editPort() { return new Promise((resolve, reject) => { rejectEdit = reject }) },
  })
  await tick()
  nodes(view.root, node => node.tag === 'button' && node.text === '编辑目标')[0].props.onClick()
  await tick()
  nodes(view.root, node => node.props['aria-label'] === '20000 tcp 目标端口')[0].props.onInput({ target: { value: '2222' } })
  nodes(view.root, node => node.tag === 'button' && node.text === '保存目标')[0].props.onClick()
  await tick()
  view.selected.value = 'B'
  await tick()
  rejectEdit(new Error('old instance error'))
  await tick()
  assert.ok(text(view.root).includes('Guest B'))
  assert.ok(!text(view.root).includes('old instance error'))
  assert.equal(nodes(view.root, node => node.tag === 'fieldset' && node.props.class === 'actions')[0].props.disabled, false)
  view.app.unmount()
})

test('unknown and cleanup states cannot offer blind reapplication', async () => {
  const component = loader()('src/components/instance/PortForwardStatus.vue').default
  for (const status of ['needs-reconciliation', 'cleanup-pending']) {
    const html = await renderToString(vue.createSSRApp(component, { status, busy: false }))
    assert.ok(html.includes('保留'))
    assert.ok(!html.includes('<button'))
  }
})

test('port mutation remains busy and unverified until fresh details arrive', async () => {
  let reads = 0, finishRead
  const saved = portFixture('A')
  const view = fixture('src/views/InstanceDetailView.vue', {
    instance() {
      if (++reads === 1) return Promise.resolve(portFixture('A'))
      return new Promise(resolve => { finishRead = resolve })
    },
    async addPort() { saved.networkStatus = 'pending'; throw new Error('apply failed') },
  })
  await tick()
  nodes(view.root, node => node.tag === 'form')[0].props.onSubmit({ preventDefault() {} })
  await tick()
  assert.ok(!text(view.root).includes('端口规则已应用'))
  assert.ok(!text(view.root).includes('（SSH）'))
  assert.equal(nodes(view.root, node => node.tag === 'fieldset' && node.props.class === 'actions')[0].props.disabled, true)
  finishRead(saved)
  await tick()
  assert.ok(text(view.root).includes('转发尚未确认生效'))
  assert.equal(nodes(view.root, node => node.tag === 'fieldset' && node.props.class === 'actions')[0].props.disabled, false)
  view.app.unmount()
})

test('failed detail refresh never restores old ready or SSH claims', async () => {
  let reads = 0, unavailable = true, mutations = 0
  const saved = portFixture('A')
  const view = fixture('src/views/InstanceDetailView.vue', {
    async instance() {
      if (++reads > 1 && unavailable) throw new Error('details unavailable')
      return { ...saved, ports: saved.ports.map(p => ({ ...p })) }
    },
    async addPort() { mutations++; saved.networkStatus = 'pending'; throw new Error('apply failed') },
  })
  await tick()
  nodes(view.root, node => node.tag === 'form')[0].props.onSubmit({ preventDefault() {} })
  await tick()
  assert.ok(text(view.root).includes('最新端口状态尚未确认'))
  assert.ok(!text(view.root).includes('端口规则已应用'))
  assert.ok(!text(view.root).includes('（SSH）'))
  assert.ok(nodes(view.root, node => node.tag === 'button' && node.text === '编辑目标').every(node => node.props.disabled))
  unavailable = false
  nodes(view.root, node => node.tag === 'button' && node.text === '刷新端口状态')[0].props.onClick()
  await tick()
  assert.equal(mutations, 1)
  assert.ok(text(view.root).includes('转发尚未确认生效'))
  view.app.unmount()
})
