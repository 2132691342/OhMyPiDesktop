// 校验两个语言文件的键结构完全一致（AGENTS.md §7）。
//
// 用真实执行而不是文本解析：文案里含换行、引号与 : 号（界面上要显示的
// 句子本来就长这样），任何基于行/正则的解析都会在某个句子上翻车。
// 这里让 Node 直接加载 TS 模块，拿到的就是组件实际看到的那棵树。
//
// 用法：node tools/check-i18n.mjs
import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const LANGS = ['zh-CN', 'en']
const tmp = mkdtempSync(join(tmpdir(), 'i18n-'))

function loadMessages(lang) {
  const src = readFileSync(`frontend/src/locales/${lang}.ts`, 'utf8')
  // 把 `export default` 换成一个普通赋值，再落成 .mjs 交给 Node 执行。
  const js = src.replace(/export\s+default\s+/, 'globalThis.__messages = ')
  const file = join(tmp, `${lang}.mjs`)
  writeFileSync(file, js + '\nexport default globalThis.__messages\n')
  const out = execFileSync(process.execPath, [
    '-e',
    `import(${JSON.stringify('file://' + file)}).then(m=>console.log(JSON.stringify(m.default)))`,
  ], { encoding: 'utf8' })
  return JSON.parse(out)
}

function flatten(obj, prefix = '') {
  const out = []
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) out.push(...flatten(v, path))
    else out.push(path)
  }
  return out
}

const sets = LANGS.map((lang) => ({ lang, keys: new Set(flatten(loadMessages(lang))) }))
const [base, ...rest] = sets
let failed = false
for (const other of rest) {
  for (const k of base.keys) {
    if (!other.keys.has(k)) {
      console.error(`${other.lang} 缺少 ${base.lang} 里的键：${k}`)
      failed = true
    }
  }
  for (const k of other.keys) {
    if (!base.keys.has(k)) {
      console.error(`${base.lang} 缺少 ${other.lang} 里的键：${k}`)
      failed = true
    }
  }
}
if (failed) process.exit(1)
console.log(`i18n 结构一致：${base.keys.size} 个键 × ${sets.length} 个语言`)
