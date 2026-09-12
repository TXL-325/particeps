const test = require('node:test')
const assert = require('node:assert/strict')
const { fixture, tick, nodes, text } = require('./vue-fixture.cjs')
const token = { id: 'fixture-token', name: 'automation', role: 'manage', revoked: false, createdAt: 0 }
const settingsAPI = {
  async detected() { return { addresses: [] } },
  async pool() { return { ipv4: [], ipv6: [], prefixes: [], natIPv4: '', nat66: '', dedicatedV4: [] } },
  async host() { return { cpuCapCores: 2 } },
  async tokens() { return { tokens: [{ ...token }] } },
}
const tokenForm = view => nodes(view.root, n => n.tag === 'form' && n.props['aria-label'] === '签发 API Token')[0]
const submit = view => tokenForm(view).props.onSubmit({ preventDefault() {} })
const button = (view, label) => nodes(view.root, n => n.tag === 'button' && n.text === label)[0]

test('Token management survives unrelated network failure', async t => {
  let reads = 0
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    async detected() { throw new Error('network fixture unavailable') },
    async tokens() { reads++; return settingsAPI.tokens() },
  })
  t.after(() => view.app.unmount())
  await tick()
  assert.equal(reads, 1)
  assert.ok(text(view.root).includes('automation'))
  assert.ok(text(view.root).includes('network fixture unavailable'))
  assert.ok(button(view, '撤销'))
})

test('Token issuance prevents duplicate submissions and preserves the one-time result', async t => {
  let finish, writes = 0
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    newToken() { writes++; return new Promise(resolve => { finish = resolve }) },
  })
  t.after(() => view.app.unmount())
  await tick()
  submit(view)
  await tick()
  submit(view)
  assert.equal(writes, 1)
  assert.equal(button(view, '签发中…').props.disabled, true)
  finish({ id: 'new-token', token: 'fixture-one-time-value', role: 'manage' })
  await tick()
  assert.ok(text(view.root).includes('fixture-one-time-value'))
  button(view, '清除显示').props.onClick()
  await tick()
  assert.ok(!text(view.root).includes('fixture-one-time-value'))
  assert.equal(button(view, '签发').props.disabled, false)
})

test('Token errors are visible and allow a later retry', async t => {
  let fail = true
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    async newToken() {
      if (fail) throw new Error('issuance fixture failure')
      return { id: 'new-token', token: 'fixture-recovered-value', role: 'manage' }
    },
  })
  t.after(() => view.app.unmount())
  await tick()
  submit(view); await tick()
  assert.ok(text(view.root).includes('issuance fixture failure'))
  assert.equal(button(view, '签发').props.disabled, false)
  fail = false
  submit(view); await tick()
  assert.ok(text(view.root).includes('fixture-recovered-value'))
  assert.ok(!text(view.root).includes('issuance fixture failure'))
})

test('Token revocation requires confirmation and refreshes the confirmed state', async t => {
  let revoked = false, writes = 0
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    async tokens() { return { tokens: [{ ...token, revoked }] } },
    async revoke(id) { assert.equal(id, token.id); writes++; revoked = true },
  })
  t.after(() => view.app.unmount())
  await tick()
  button(view, '撤销').props.onClick(); await tick()
  assert.equal(writes, 0)
  button(view, '取消').props.onClick(); await tick()
  assert.equal(writes, 0)
  button(view, '撤销').props.onClick(); await tick()
  button(view, '确认撤销').props.onClick(); await tick()
  assert.equal(writes, 1)
  assert.ok(text(view.root).includes('已撤销'))
  assert.equal(button(view, '撤销'), undefined)
})

test('A failed revoke stays visible and does not mark an active Token revoked', async t => {
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    async revoke() { throw new Error('revoke fixture failure') },
  })
  t.after(() => view.app.unmount())
  await tick()
  button(view, '撤销').props.onClick(); await tick()
  button(view, '确认撤销').props.onClick(); await tick()
  assert.ok(text(view.root).includes('revoke fixture failure'))
  assert.ok(!text(view.root).includes('已撤销'))
  assert.equal(button(view, '撤销').props.disabled, false)
})

test('Confirmed Token revocation stays visible if the follow-up read fails', async t => {
  let reads = 0
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    async tokens() {
      if (++reads > 1) throw new Error('refresh after revoke unavailable')
      return settingsAPI.tokens()
    },
    async revoke() {},
  })
  t.after(() => view.app.unmount())
  await tick()
  button(view, '撤销').props.onClick(); await tick()
  button(view, '确认撤销').props.onClick(); await tick()
  assert.ok(text(view.root).includes('已撤销'))
  assert.ok(text(view.root).includes('refresh after revoke unavailable'))
  assert.equal(button(view, '撤销'), undefined)
})

test('A late initial Token list cannot replace a post-creation refresh', async t => {
  const reads = []
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    tokens() { return new Promise(resolve => reads.push(resolve)) },
    async newToken() { return { id: token.id, token: 'fixture-new-secret', role: 'manage' } },
  })
  t.after(() => view.app.unmount())
  await tick()
  submit(view); await tick()
  assert.equal(reads.length, 2)
  reads[1]({ tokens: [{ ...token }] }); await tick()
  reads[0]({ tokens: [] }); await tick()
  assert.ok(text(view.root).includes('automation'))
  assert.ok(text(view.root).includes('fixture-new-secret'))
})

test('A successful issuance keeps its secret when only the list refresh fails', async t => {
  let reads = 0
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    async tokens() {
      if (++reads > 1) throw new Error('list refresh fixture failure')
      return { tokens: [] }
    },
    async newToken() { return { id: 'new-token', token: 'fixture-important-value', role: 'manage' } },
  })
  t.after(() => view.app.unmount())
  await tick()
  submit(view); await tick()
  assert.ok(text(view.root).includes('fixture-important-value'))
  assert.ok(text(view.root).includes('list refresh fixture failure'))
})

test('Token responses after leaving the page do not restart requests', async () => {
  let finish, reads = 0
  const view = fixture('src/views/SettingsView.vue', {
    ...settingsAPI,
    async tokens() { reads++; return { tokens: [] } },
    newToken() { return new Promise(resolve => { finish = resolve }) },
  })
  await tick()
  submit(view); await tick()
  view.app.unmount()
  finish({ id: 'late', token: 'late-fixture-secret', role: 'manage' })
  await tick()
  assert.equal(reads, 1)
  assert.ok(!text(view.root).includes('late-fixture-secret'))
})

test('Malformed Token list responses become an error rather than a render crash', async t => {
  const view = fixture('src/views/SettingsView.vue', { ...settingsAPI, async tokens() { return { tokens: null } } })
  t.after(() => view.app.unmount())
  await tick()
  assert.ok(text(view.root).includes('Token 列表响应无效'))
})

test('Failed logout stays on the current page and successful retry navigates to login', async t => {
  let fail = true
  const view = fixture('src/App.vue', {
    async logout() { if (fail) throw new Error('logout fixture failure') },
  })
  t.after(() => view.app.unmount())
  await tick()
  await button(view, '退出').props.onClick(); await tick()
  assert.ok(text(view.root).includes('logout fixture failure'))
  assert.equal(view.calls.filter(call => call[0] === 'route').length, 0)
  fail = false
  await button(view, '退出').props.onClick(); await tick()
  assert.ok(view.calls.some(call => call[0] === 'route' && call[1] === '/login'))
})

test('Login prevents overlapping submissions and explains rate limiting', async t => {
  let finish, writes = 0
  const view = fixture('src/views/LoginView.vue', {
    login() { writes++; return new Promise((resolve, reject) => { finish = reject }) },
  })
  t.after(() => view.app.unmount())
  await tick()
  const form = nodes(view.root, n => n.tag === 'form')[0]
  const first = form.props.onSubmit({ preventDefault() {} })
  await tick()
  form.props.onSubmit({ preventDefault() {} })
  assert.equal(writes, 1)
  finish(Object.assign(new Error('limited'), { status: 429 }))
  await first; await tick()
  assert.ok(text(view.root).includes('登录尝试过于频繁'))
  assert.equal(button(view, '进入').props.disabled, false)
})
