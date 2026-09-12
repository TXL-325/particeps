// Interactive lab client. One JSON command per line; UDP keys retain the same
// socket so target edits and cleanup are tested against existing NAT sessions.
const dgram = require('node:dgram')
const http = require('node:http')
const readline = require('node:readline')
const sockets = new Map()
let sequence = 0

async function socketFor(command) {
  if (sockets.has(command.key)) {
    const current = sockets.get(command.key)
    if (current.host !== command.host || current.port !== command.port) throw new Error('key already binds another endpoint')
    return current
  }
  const socket = dgram.createSocket('udp4')
  const entry = { socket, host: command.host, port: command.port, received: {}, sent: 0, pending: new Map(), timer: null }
  socket.on('message', buffer => {
    const message = buffer.toString()
    const prefix = message.split(' ').slice(0, 3).join(' ')
    entry.received[prefix] = (entry.received[prefix] || 0) + 1
    for (const [nonce, pending] of entry.pending) {
      if (message.endsWith(nonce)) { entry.pending.delete(nonce); pending.resolve(message); break }
    }
  })
  socket.on('error', error => {
    for (const pending of entry.pending.values()) pending.reject(error)
    entry.pending.clear()
  })
  await new Promise((resolve, reject) => {
    socket.once('error', reject)
    socket.connect(command.port, command.host, () => { socket.off('error', reject); resolve() })
  })
  sockets.set(command.key, entry)
  return entry
}

async function udp(command) {
  const entry = await socketFor(command)
  const nonce = `probe-${command.key}-${++sequence}`
  let timer
  try {
    const reply = await new Promise((resolve, reject) => {
      entry.pending.set(nonce, { resolve, reject })
      timer = setTimeout(() => reject(Object.assign(new Error('UDP timeout'), { code: 'ETIMEDOUT' })), 1500)
      entry.socket.send(nonce)
    })
    if (command.absent) throw new Error(`unexpected UDP reply: ${reply}`)
    if (!reply.startsWith(command.expect + ' ')) throw new Error(`unexpected UDP endpoint: ${reply}`)
    return { reply, local: entry.socket.address() }
  } catch (error) {
    if (command.absent && ['ETIMEDOUT', 'ECONNREFUSED', 'ECONNRESET'].includes(error.code)) return { absent: true, reason: error.code }
    throw error
  } finally {
    clearTimeout(timer)
    entry.pending.delete(nonce)
  }
}

async function dispatch(command) {
  if (command.op === 'udp') return udp(command)
  if (command.op === 'stream') {
    const entry = await socketFor(command)
    if (!entry.timer) entry.timer = setInterval(() => { entry.sent++; entry.socket.send(`stream-${command.key}-${++sequence}`) }, 40)
    return { streaming: command.key, local: entry.socket.address() }
  }
  if (command.op === 'stats') {
    const entry = sockets.get(command.key)
    return { sent: entry.sent, received: entry.received, local: entry.socket.address() }
  }
  if (command.op === 'http') {
    const body = await new Promise((resolve, reject) => {
      const request = http.get({ hostname: command.host, port: command.port, path: '/', timeout: 2500, agent: false }, response => {
        let text = ''
        response.setEncoding('utf8')
        response.on('data', chunk => { text += chunk; if (text.length > 4096) request.destroy(new Error('probe response too large')) })
        response.on('end', () => response.statusCode === 200 ? resolve(text.trim()) : reject(new Error(`HTTP ${response.statusCode}`)))
      })
      request.on('timeout', () => request.destroy(new Error('HTTP timeout')))
      request.on('error', reject)
    })
    if (body !== command.expect) throw new Error(`unexpected HTTP endpoint: ${body}`)
    return { body }
  }
  if (command.op === 'close') {
    for (const entry of sockets.values()) { clearInterval(entry.timer); entry.socket.close() }
    input.close()
    process.stdin.pause()
    return { closed: true }
  }
  throw new Error('unknown probe command')
}

const input = readline.createInterface({ input: process.stdin })
let queue = Promise.resolve()
input.on('line', line => {
  queue = queue.then(async () => {
    try { const command = JSON.parse(line); console.log(JSON.stringify({ ok: true, op: command.op, result: await dispatch(command) })) }
    catch (error) { console.log(JSON.stringify({ ok: false, error: error.message })) }
  })
})
console.log(JSON.stringify({ ready: true }))
