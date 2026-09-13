const test = require('node:test')
const assert = require('node:assert/strict')
const { renderToString } = require('@vue/server-renderer')
const { loader, fixture, tick, nodes, text, vue } = require('./vue-fixture.cjs')

const copy = value => structuredClone(value)
const button = (view, label) => nodes(view.root, node => node.tag === 'button' && node.text === label)[0]
const toggle = (view, number, proto, enabled) => nodes(view.root, node => node.tag === 'button' && node.props['aria-label'] === `${number} ${proto} ${enabled ? '启用映射' : '解除映射'}`)[0]
const portTable = view => nodes(view.root, node => node.tag === 'table' && text(node).includes('映射状态'))[0]
const rows = view => nodes(portTable(view), node => node.tag === 'tbody')[0].children.filter(node => node.tag === 'tr')

function reservations(id = 'A', udp = false) {
  return {
    id, name: `Guest ${id}`, status: 'running', networkStatus: 'ready', memUsed: 0,
    ports: Array.from({ length: 20 }, (_, offset) => {
      const number = 20000 + offset
      const tcp = { number, proto: 'tcp', listenIp: '192.0.2.1', target: offset === 0 ? 22 : number, enabled: offset === 0 }
      return udp ? [tcp, { ...tcp, proto: 'udp', target: number, enabled: false }] : [tcp]
    }).flat(),
  }
}

async function detail(t, saved, api = {}) {
  const view = fixture('src/views/InstanceDetailView.vue', {
    async instance() { return copy(saved) },
    ...api,
  })
  t.after(() => view.app.unmount())
  await tick()
  return view
}

test('twenty reserved TCP numbers display one applied SSH mapping and nineteen unbound ports', async t => {
  const view = await detail(t, reservations())
  const content = text(view.root)
  assert.match(content, /可使用号码\s+20000–20019\s+（共 20 个号码）/)
  assert.match(content, /UDP\s+未分配/)
  assert.match(content, /已应用映射 1 条/)
  assert.equal(rows(view).length, 20)
  assert.equal(rows(view).filter(row => text(row).includes('预留 · 未绑定')).length, 19)
  assert.equal(rows(view).filter(row => text(row).includes('（SSH）')).length, 1)
  assert.equal(nodes(view.root, node => node.tag === 'button' && node.text === '启用映射').length, 19)
})

test('optional UDP reservations share the number count and remain unbound', async t => {
  const view = await detail(t, reservations('A', true))
  assert.match(text(view.root), /共 20 个号码/)
  assert.match(text(view.root), /UDP\s+20000–20019/)
  assert.match(text(view.root), /已应用映射 1 条/)
  assert.equal(rows(view).length, 40)
  assert.equal(rows(view).filter(row => text(row).includes('预留 · 未绑定')).length, 39)
  assert.equal(rows(view).filter(row => text(row).includes('（SSH）')).length, 1)
  assert.match(text(view.root), /追加预留 TCP 和 UDP 端口/)
})

test('instance list ranges preserve gaps and count numbers separately from protocol rows', async () => {
  const saved = reservations('A', true)
  saved.ports.push({ number: 20025, proto: 'tcp', listenIp: '192.0.2.1', target: 20025, enabled: false })
  saved.ports.reverse()
  const Component = loader()('src/components/instances/InstanceTable.vue').default
  const html = await renderToString(vue.createSSRApp(Component, { instances: [saved], selectedIds: [] }))
  assert.ok(html.includes('20000–20019、20025'))
  assert.ok(!html.includes('20000–20025'))
  assert.ok(html.includes('21 个号码'))
  assert.ok(html.includes('已应用 1 条'))
  assert.ok(!html.includes('41 个号码'))
})

test('editing a reserved target never enables it or advertises SSH', async t => {
  const saved = reservations()
  const changes = []
  const view = await detail(t, saved, {
    async editPort(id, number, proto, patch) {
      changes.push({ id, number, proto, patch: copy(patch) })
      Object.assign(saved.ports.find(port => port.number === number && port.proto === proto), patch)
    },
  })
  const row = rows(view)[1]
  nodes(row, node => node.tag === 'button' && node.text === '编辑目标')[0].props.onClick()
  await tick()
  nodes(row, node => node.props['aria-label'] === '20001 tcp 目标端口')[0].props.onInput({ target: { value: '22' } })
  button(view, '保存目标').props.onClick()
  await tick()
  assert.deepEqual(changes, [{ id: 'A', number: 20001, proto: 'tcp', patch: { target: 22 } }])
  assert.equal(saved.ports[1].enabled, false)
  assert.ok(text(rows(view)[1]).includes('预留 · 未绑定'))
  assert.ok(!text(rows(view)[1]).includes('（SSH）'))
  assert.ok(toggle(view, 20001, 'tcp', true))
})

test('enabling one reserved UDP mapping waits for a fresh GET and ignores duplicate clicks', async t => {
  const saved = reservations('A', true)
  const changes = []
  let reads = 0, finishRead
  const view = await detail(t, saved, {
    instance() {
      if (++reads === 1) return Promise.resolve(copy(saved))
      return new Promise(resolve => { finishRead = resolve })
    },
    async editPort(id, number, proto, patch) {
      changes.push({ id, number, proto, patch: copy(patch) })
      Object.assign(saved.ports.find(port => port.number === number && port.proto === proto), patch)
      return copy(saved)
    },
  })
  const control = toggle(view, 20000, 'udp', true)
  control.props.onClick()
  control.props.onClick()
  await tick()
  assert.deepEqual(changes, [{ id: 'A', number: 20000, proto: 'udp', patch: { enabled: true } }])
  assert.ok(text(view.root).includes('最新端口状态尚未确认'))
  assert.ok(!text(view.root).includes('（SSH）'))
  assert.ok(!text(view.root).includes('已应用映射'))
  assert.equal(nodes(view.root, node => node.tag === 'fieldset' && node.props.class === 'actions')[0].props.disabled, true)
  finishRead(copy(saved))
  await tick()
  assert.ok(toggle(view, 20000, 'udp', false))
  assert.match(text(view.root), /已应用映射 2 条/)
  assert.match(text(view.root), /共 20 个号码/)
  assert.equal(saved.ports.find(port => port.proto === 'tcp' && port.number === 20000).target, 22)
})

test('waiting for an address permits only disabling an existing mapping and waits for confirmation', async t => {
  const saved = reservations()
  saved.networkStatus = 'waiting-address'
  const changes = []
  let reads = 0, finishRead
  const view = await detail(t, saved, {
    instance() {
      if (++reads === 1) return Promise.resolve(copy(saved))
      return new Promise(resolve => { finishRead = resolve })
    },
    async editPort(id, number, proto, patch) {
      changes.push({ id, number, proto, patch: copy(patch) })
      Object.assign(saved.ports.find(port => port.number === number && port.proto === proto), patch)
    },
    async addPort() { changes.push('unexpected allocation') },
  })
  const disable = toggle(view, 20000, 'tcp', false)
  const enable = toggle(view, 20001, 'tcp', true)
  assert.equal(disable.props.disabled, false)
  assert.equal(enable.props.disabled, true)
  assert.ok(nodes(portTable(view), node => node.tag === 'button' && node.text === '编辑目标').every(node => node.props.disabled))
  assert.equal(nodes(view.root, node => node.tag === 'fieldset' && node.props.class === 'port-fields')[0].props.disabled, true)
  enable.props.onClick()
  button(view, '编辑目标').props.onClick()
  nodes(view.root, node => node.tag === 'form')[0].props.onSubmit({ preventDefault() {} })
  await tick()
  assert.deepEqual(changes, [])
  assert.equal(nodes(portTable(view), node => node.tag === 'input').length, 0)

  disable.props.onClick()
  disable.props.onClick()
  await tick()
  const expected = [{ id: 'A', number: 20000, proto: 'tcp', patch: { enabled: false } }]
  assert.deepEqual(changes, expected)
  assert.equal(disable.props.disabled, true)
  assert.ok(text(view.root).includes('最新端口状态尚未确认'))
  disable.props.onClick()
  await tick()
  assert.deepEqual(changes, expected)

  finishRead(copy(saved))
  await tick()
  assert.equal(toggle(view, 20000, 'tcp', true).props.disabled, true)
  assert.equal(saved.ports[0].enabled, false)
  assert.equal(saved.ports[0].target, 22)
})

test('waiting-address disable remains blocked during an instance operation', async t => {
  const saved = reservations()
  saved.networkStatus = 'waiting-address'
  const changes = []
  let finishPassword
  const view = await detail(t, saved, {
    password() { return new Promise(resolve => { finishPassword = resolve }) },
    async editPort(...args) { changes.push(args) },
  })
  assert.equal(toggle(view, 20000, 'tcp', false).props.disabled, false)
  button(view, '重置密码').props.onClick()
  await tick()
  const disable = toggle(view, 20000, 'tcp', false)
  assert.equal(disable.props.disabled, true)
  assert.ok(text(view.root).includes('小鸡尚未取得可用的内部 IPv4'))
  disable.props.onClick()
  await tick()
  assert.deepEqual(changes, [])
  finishPassword({ password: 'fixture-only' })
  await tick()
  assert.equal(toggle(view, 20000, 'tcp', false).props.disabled, false)
})

test('uncertain or protected network states never permit disabling a mapping', async t => {
  for (const status of ['needs-reconciliation', 'pending', 'cleanup-pending', 'unverified', undefined, 'unknown']) {
    const saved = reservations()
    saved.networkStatus = status
    const changes = []
    const view = await detail(t, saved, {
      async editPort(...args) { changes.push(args) },
    })
    const disable = toggle(view, 20000, 'tcp', false)
    assert.equal(disable.props.disabled, true, String(status))
    disable.props.onClick()
    await tick()
    assert.deepEqual(changes, [], String(status))
  }
})

test('a failed disable never claims the previous rule was removed until reconciliation succeeds', async t => {
  const saved = reservations()
  const view = await detail(t, saved, {
    async editPort(id, number, proto, patch) {
      assert.equal(patch.enabled, false)
      Object.assign(saved.ports[0], patch)
      saved.networkStatus = 'pending'
      throw new Error('fixture forward removal failed')
    },
    async syncPorts() { saved.networkStatus = 'ready' },
  })
  toggle(view, 20000, 'tcp', false).props.onClick()
  await tick()
  assert.ok(text(view.root).includes('fixture forward removal failed'))
  assert.ok(text(view.root).includes('旧规则可能仍然存在'))
  assert.ok(text(rows(view)[0]).includes('设定不绑定 · 待确认'))
  assert.ok(!text(rows(view)[0]).includes('未绑定'))
  assert.ok(!text(view.root).includes('（SSH）'))
  button(view, '重新应用端口规则').props.onClick()
  await tick()
  assert.ok(text(rows(view)[0]).includes('预留 · 未绑定'))
  assert.match(text(view.root), /已应用映射 0 条/)
  assert.match(text(view.root), /共 20 个号码/)
})

test('read-only rejection preserves reservations and does not pretend to enable a mapping', async t => {
  const saved = reservations()
  const view = await detail(t, saved, {
    async editPort() { throw new Error('forbidden: read-only token') },
  })
  toggle(view, 20001, 'tcp', true).props.onClick()
  await tick()
  assert.ok(text(view.root).includes('forbidden: read-only token'))
  assert.equal(saved.ports[1].enabled, false)
  assert.ok(text(rows(view)[1]).includes('预留 · 未绑定'))
  assert.match(text(view.root), /已应用映射 1 条/)
})

test('adding a number reserves it without enabling a new mapping', async t => {
  const saved = reservations()
  const view = await detail(t, saved, {
    async addPort(id, number) {
      assert.equal(id, 'A')
      assert.equal(number, 20025)
      saved.ports.push({ number, proto: 'tcp', listenIp: '192.0.2.1', target: number, enabled: false })
    },
  })
  assert.match(text(view.root), /追加预留 TCP 端口/)
  nodes(view.root, node => node.props['aria-label'] === '追加端口号码')[0].props.onInput({ target: { value: '20025' } })
  nodes(view.root, node => node.tag === 'form')[0].props.onSubmit({ preventDefault() {} })
  await tick()
  assert.match(text(view.root), /20000–20019、20025/)
  assert.match(text(view.root), /共 21 个号码/)
  assert.match(text(view.root), /已应用映射 1 条/)
  assert.ok(toggle(view, 20025, 'tcp', true))
})
