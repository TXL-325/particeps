const test = require('node:test')
const assert = require('node:assert/strict')
const { webcrypto } = require('node:crypto')
const { loader, fixture, tick, nodes, text } = require('./vue-fixture.cjs')

const images = ['alpine/3.21/cloud', 'debian/13/cloud']
const task = { id: 'batch-fixture', kind: 'create', status: 'running', items: [] }
const uuidV4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const httpCrypto = () => ({ getRandomValues: bytes => webcrypto.getRandomValues(bytes) })
const button = (view, label) => nodes(view.root, node => node.tag === 'button' && node.text === label)[0]
const form = view => nodes(view.root, node => node.tag === 'form' && node.props['aria-label'] === '创建小鸡')[0]
const submit = view => form(view).props.onSubmit({ preventDefault() {} })
const submitButton = view => nodes(form(view), node => node.tag === 'button' && node.props.type === 'submit')[0]

function setField(view, label, value) {
  const container = nodes(form(view), node => node.tag === 'label' && text(node).trim().startsWith(label))[0]
  const field = nodes(container, node => ['input', 'select', 'textarea'].includes(node.tag))[0]
  field.props['onUpdate:modelValue'](value)
}

async function creationView(t, api = {}, globals = {}) {
  const view = fixture('src/views/InstancesView.vue', {
    async instances() { return { instances: [] } },
    async images() { return { images: images.map(alias => ({ alias })) } },
    ...api,
  }, { crypto: httpCrypto(), ...globals })
  t.after(() => view.app.unmount())
  await tick()
  button(view, '创建').props.onClick()
  await tick()
  return view
}

test('request keys remain distinct UUID v4 values with either Web Crypto interface', () => {
  for (const crypto of [webcrypto, httpCrypto()]) {
    const { createRequestKey } = loader({}, { crypto })('src/utils/requestKey.ts')
    const keys = Array.from({ length: 16 }, () => createRequestKey())
    for (const key of keys) assert.match(key, uuidV4)
    assert.equal(new Set(keys).size, keys.length)
  }
})

test('a browser without randomUUID can send an Alpine batch with an Idempotency-Key header', async t => {
  const requests = []
  const api = loader({}, {
    Headers,
    async fetch(path, init) {
      let data
      if (init.method === 'POST') {
        requests.push({ path, body: JSON.parse(init.body), key: init.headers.get('Idempotency-Key') })
        data = task
      } else if (path === '/api/v1/images') {
        data = { images: images.map(alias => ({ alias })) }
      } else {
        data = { instances: [] }
      }
      return { ok: true, status: 200, async text() { return JSON.stringify(data) } }
    },
  })('src/api.ts').api
  const view = await creationView(t, api)
  setField(view, '数量', 2)
  submit(view)
  await tick()
  assert.equal(requests.length, 1)
  assert.equal(requests[0].path, '/api/v1/instances')
  assert.equal(requests[0].body.count, 2)
  assert.equal(requests[0].body.image, 'alpine/3.21/cloud')
  assert.equal(requests[0].body.memoryMib, 128)
  assert.equal(requests[0].body.diskGib, 1)
  assert.equal(requests[0].body.allocateUDP, false)
  assert.match(requests[0].key, uuidV4)
  assert.ok(view.calls.some(([type, path]) => type === 'route' && path === '/tasks/batch-fixture'))
  assert.equal(form(view), undefined)
  assert.ok(!text(view.root).includes('randomUUID'))
})

test('UDP reservation is an explicit creation option and is included in its request', async t => {
  let request
  const view = await creationView(t, {
    async create(body) { request = body; return task },
  })
  setField(view, '同时分配 UDP 端口', true)
  submit(view)
  await tick()
  assert.equal(request.allocateUDP, true)
  assert.equal(request.passwordLogin, true)
  assert.ok(view.calls.some(([type, path]) => type === 'route' && path === '/tasks/batch-fixture'))
})

test('duplicate submit events send one batch and keep its form locked until accepted', async t => {
  let finish, writes = 0
  const view = await creationView(t, {
    create() { writes++; return new Promise(resolve => { finish = resolve }) },
  })
  submit(view)
  submit(view)
  assert.equal(writes, 1)
  await tick()
  submit(view)
  assert.equal(writes, 1)
  assert.equal(submitButton(view).props.disabled, true)
  assert.equal(submitButton(view).text, '提交中…')
  assert.equal(nodes(form(view), node => node.tag === 'fieldset')[0].props.disabled, true)
  assert.equal(button(view, '取消').props.disabled, true)
  button(view, '取消').props.onClick()
  await tick()
  assert.ok(form(view))
  finish(task)
  await tick()
  assert.equal(form(view), undefined)
  assert.equal(view.calls.filter(([type]) => type === 'route').length, 1)
})

test('retry after a lost response reuses its key and a later new creation gets a fresh key', async t => {
  const requests = []
  const view = await creationView(t, {
    async create(body, key) {
      requests.push({ body, key })
      if (requests.length === 1) throw new Error('fixture response lost')
      return task
    },
  })
  submit(view)
  await tick()
  assert.ok(text(view.root).includes('fixture response lost'))
  assert.equal(submitButton(view).props.disabled, false)
  assert.equal(nodes(form(view), node => node.tag === 'fieldset')[0].props.disabled, false)
  submit(view)
  await tick()
  assert.equal(requests.length, 2)
  assert.deepEqual(requests[0].body, requests[1].body)
  assert.equal(requests[0].key, requests[1].key)
  assert.ok(!text(view.root).includes('fixture response lost'))
  button(view, '创建').props.onClick()
  await tick()
  submit(view)
  await tick()
  assert.equal(requests.length, 3)
  assert.notEqual(requests[1].key, requests[2].key)
})

test('changing a failed request to Debian creates a new key and sends its resource defaults', async t => {
  const requests = []
  const view = await creationView(t, {
    async create(body, key) {
      requests.push({ body, key })
      if (requests.length === 1) throw new Error('fixture validation failure')
      return task
    },
  })
  submit(view)
  await tick()
  setField(view, '镜像', 'debian/13/cloud')
  setField(view, '数量', 2)
  await tick()
  submit(view)
  await tick()
  assert.equal(requests.length, 2)
  assert.notEqual(requests[0].key, requests[1].key)
  assert.equal(requests[1].body.image, 'debian/13/cloud')
  assert.equal(requests[1].body.memoryMib, 256)
  assert.equal(requests[1].body.diskGib, 4)
  assert.equal(requests[1].body.count, 2)
})

test('returning to an uncertain batch after a rejected edit reuses its original task', async t => {
  const requests = [], accepted = new Map()
  const view = await creationView(t, {
    async create(body, key) {
      requests.push({ body, key })
      if (body.cpuCores > 2) throw new Error('fixture quota exceeds aggregate cap')
      if (!accepted.has(key)) accepted.set(key, { ...task, id: `batch-${accepted.size + 1}` })
      if (requests.length === 1) throw new Error('fixture accepted response lost')
      return accepted.get(key)
    },
  })
  setField(view, '数量', 2)
  submit(view)
  await tick()
  assert.ok(text(view.root).includes('fixture accepted response lost'))

  setField(view, 'CPU 核', 8)
  submit(view)
  await tick()
  assert.ok(text(view.root).includes('fixture quota exceeds aggregate cap'))

  setField(view, 'CPU 核', 0.5)
  submit(view)
  await tick()
  assert.equal(requests.length, 3)
  assert.equal(accepted.size, 1)
  assert.deepEqual(requests[0].body, requests[2].body)
  assert.equal(requests[0].key, requests[2].key)
  assert.notEqual(requests[0].key, requests[1].key)
  assert.ok(view.calls.some(([type, path]) => type === 'route' && path === '/tasks/batch-1'))

  // Confirming A must not discard the still-unconfirmed request B.
  button(view, '创建').props.onClick()
  await tick()
  setField(view, '数量', 2)
  setField(view, 'CPU 核', 8)
  submit(view)
  await tick()
  assert.equal(requests.length, 4)
  assert.equal(requests[1].key, requests[3].key)
})

test('missing secure randomness reports an error without sending a request or leaving the form busy', async t => {
  const crypto = {}
  let writes = 0
  const view = await creationView(t, {
    async create() { writes++; return task },
  }, { crypto })
  submit(view)
  await tick()
  assert.equal(writes, 0)
  assert.ok(text(view.root).includes('当前浏览器无法生成安全的请求标识'))
  assert.equal(submitButton(view).props.disabled, false)
  crypto.getRandomValues = bytes => webcrypto.getRandomValues(bytes)
  submit(view)
  await tick()
  assert.equal(writes, 1)
  assert.equal(form(view), undefined)
})

test('an invalid creation response leaves the same request available for retry', async t => {
  const keys = []
  const view = await creationView(t, {
    async create(body, key) { keys.push(key); return keys.length === 1 ? {} : task },
  })
  submit(view)
  await tick()
  assert.equal(view.calls.filter(([type]) => type === 'route').length, 0)
  assert.ok(text(view.root).includes('创建响应缺少任务编号'))
  submit(view)
  await tick()
  assert.equal(keys.length, 2)
  assert.equal(keys[0], keys[1])
  assert.equal(form(view), undefined)
})

test('failure to open an accepted task preserves the form and its retry key', async t => {
  const keys = [], routes = []
  let fail = true
  const view = await creationView(t, {
    async create(body, key) { keys.push(key); return task },
  }, {
    router: { async push(path) { routes.push(path); return fail ? { type: 4 } : undefined } },
  })
  submit(view)
  await tick()
  assert.ok(text(view.root).includes('任务已受理，但无法打开任务页面'))
  assert.equal(submitButton(view).props.disabled, false)
  fail = false
  submit(view)
  await tick()
  assert.equal(keys.length, 2)
  assert.equal(keys[0], keys[1])
  assert.deepEqual(routes, ['/tasks/batch-fixture', '/tasks/batch-fixture'])
  assert.equal(form(view), undefined)
})

test('a creation response arriving after leaving the page cannot redirect the user', async t => {
  let finish
  const view = await creationView(t, {
    create() { return new Promise(resolve => { finish = resolve }) },
  })
  submit(view)
  await tick()
  view.app.unmount()
  finish(task)
  await tick()
  assert.equal(view.calls.filter(([type]) => type === 'route').length, 0)
})
