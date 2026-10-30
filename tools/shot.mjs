// 临时调试脚本：用 CDP 驱动 Edge 截图。
// 用法：node tools/shot.mjs <url> <out.png> [waitMs] [width] [height]
import { spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { setTimeout as sleep } from 'node:timers/promises'

const EDGE = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe'
const [url, out, waitMs = '6000', width = '1600', height = '1000'] = process.argv.slice(2)
const port = 9222 + Math.floor(Math.random() * 500)

const child = spawn(EDGE, [
  '--headless=new', '--disable-gpu', '--no-sandbox', '--hide-scrollbars', '--mute-audio',
  `--remote-debugging-port=${port}`, `--window-size=${width},${height}`,
  `--user-data-dir=${process.env.TEMP}\\edge-shot-${port}`, 'about:blank',
], { stdio: 'ignore' })

let page
for (let i = 0; i < 60; i++) {
  try {
    const list = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
    page = list.find((t) => t.type === 'page')
    if (page) break
  } catch { /* 还没起来 */ }
  await sleep(250)
}

const ws = new WebSocket(page.webSocketDebuggerUrl)
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej })
let seq = 0
const pend = new Map()
const errors = []
ws.onmessage = (evt) => {
  const msg = JSON.parse(evt.data)
  if (msg.id && pend.has(msg.id)) { pend.get(msg.id)(msg); pend.delete(msg.id) }
  if (msg.method === 'Runtime.consoleAPICalled' && msg.params.type === 'error') {
    errors.push(msg.params.args.map((a) => a.value ?? a.description ?? '').join(' '))
  }
  if (msg.method === 'Runtime.exceptionThrown') {
    errors.push(msg.params.exceptionDetails.exception?.description ?? msg.params.exceptionDetails.text)
  }
}
const send = (method, params = {}) => {
  const id = ++seq
  return new Promise((res) => { pend.set(id, res); ws.send(JSON.stringify({ id, method, params })) })
}

await send('Page.enable')
await send('Runtime.enable')
await send('Emulation.setDeviceMetricsOverride', {
  width: Number(width), height: Number(height), deviceScaleFactor: 1, mobile: false,
})
await send('Page.navigate', { url })
await sleep(Number(waitMs))

const shot = await send('Page.captureScreenshot', { format: 'png' })
if (!shot.result?.data) {
  console.error('截图失败')
  if (errors.length) console.error(errors.join('\n'))
  child.kill(); process.exit(1)
}
writeFileSync(out, Buffer.from(shot.result.data, 'base64'))
console.log('已保存', out)
if (errors.length) console.log('控制台错误:\n' + errors.join('\n'))
ws.close(); child.kill(); process.exit(0)
