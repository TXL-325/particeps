const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const vue = require('vue')
const { parse, compileScript } = require('@vue/compiler-sfc')
const { transformSync } = require('esbuild')

function loader(api = {}, globals = {}) {
  const cache = new Map()
  function load(filename) {
    filename = path.resolve(filename)
    if (cache.has(filename)) return cache.get(filename).exports
    let source = fs.readFileSync(filename, 'utf8')
    if (filename.endsWith('.vue')) {
      const { descriptor } = parse(source, { filename })
      source = compileScript(descriptor, { id: filename, inlineTemplate: true }).content
    }
    const code = transformSync(source, { loader: 'ts', format: 'cjs' }).code
    const module = { exports: {} }
    cache.set(filename, module)
    function resolve(name) {
      if (name === 'vue-router') return { useRouter: () => ({ push: async () => {} }) }
      if (!name.startsWith('.')) return require(name)
      let target = path.resolve(path.dirname(filename), name)
      if (!path.extname(target)) target += '.ts'
      if (target === path.resolve('src/api.ts')) return { api }
      return load(target)
    }
    vm.runInNewContext(code, { module, exports: module.exports, require: resolve, AbortController, Error, ...globals }, { filename })
    return module.exports
  }
  return load
}

const renderer = vue.createRenderer({
  createElement: tag => ({ tag, children: [], parent: null, text: '', props: {} }),
  createText: text => ({ tag: '#text', children: [], parent: null, text, props: {} }),
  createComment: text => ({ tag: '#comment', children: [], parent: null, text, props: {} }),
  insert(node, parent, anchor) {
    if (node.parent) node.parent.children = node.parent.children.filter(item => item !== node)
    node.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    if (index < 0) parent.children.push(node)
    else parent.children.splice(index, 0, node)
  },
  remove(node) { if (node.parent) node.parent.children = node.parent.children.filter(item => item !== node) },
  setText(node, text) { node.text = text },
  setElementText(node, text) { node.text = text; node.children = [] },
  parentNode: node => node.parent,
  nextSibling: node => node.parent?.children[node.parent.children.indexOf(node) + 1] ?? null,
  patchProp(node, name, previous, value) { node.props[name] = value },
})

function fixture(filename = 'src/views/InstanceDetailView.vue', apiOverrides = {}) {
  const pending = [], calls = [], timers = new Map(), listeners = new Map()
  let timerID = 0
  const fakeWindow = {
    setTimeout(fn) { timers.set(++timerID, fn); return timerID },
    clearTimeout(id) { timers.delete(id) },
    setInterval(fn) { timers.set(++timerID, fn); return timerID },
    clearInterval(id) { timers.delete(id) },
    confirm() { return true },
  }
  const fakeDocument = {
    hidden: false,
    addEventListener(name, fn) { if (!listeners.has(name)) listeners.set(name, new Set()); listeners.get(name).add(fn) },
    removeEventListener(name, fn) { listeners.get(name)?.delete(fn) },
  }
  const api = {
    instance(id, signal) { return new Promise(resolve => pending.push({ id, signal, resolve })) },
    async processes(id) { calls.push(['processes', id]); return { instanceId: id, processes: [] } },
    async instSeries(id) { calls.push(['metrics', id]); return { series: [] } },
    async action(id, operation) { calls.push(['action', id, operation]) },
    ...apiOverrides,
  }
  const Component = loader(api, { window: fakeWindow, document: fakeDocument })(filename).default
  const selected = vue.ref('A')
  const root = { tag: 'root', children: [], parent: null, text: '', props: {} }
  const app = renderer.createApp({ render: () => vue.h(Component, { id: selected.value }) })
  app.mount(root)
  function visibility(hidden) { fakeDocument.hidden = hidden; for (const fn of listeners.get('visibilitychange') ?? []) fn() }
  return { pending, calls, timers, selected, root, app, visibility }
}

async function tick() { for (let i = 0; i < 20; i++) await Promise.resolve(); await vue.nextTick() }
function nodes(root, predicate) { return (predicate(root) ? [root] : []).concat(...root.children.map(child => nodes(child, predicate))) }
function text(root) { return [root.text, ...root.children.map(text)].join(' ') }
module.exports = { loader, fixture, tick, nodes, text, vue }
