// markdown 渲染：唯一出口。所有视图都只能用这里的函数，
// 这样「流式时按块缓存」这条性能纪律才不会被某个视图绕开。
//
// 为什么按块切分：全量重解析是 O(字数²)。流式回答里只有最后一块在长，
// 已稳定的块原样复用 HTML，成本降到 O(字数)。
import { marked } from 'marked'

marked.setOptions({ gfm: true, breaks: true })

interface BlockCache {
  parts: string[]
  html: string[]
}

const caches = new Map<string, BlockCache>()

/**
 * 流式 markdown：按顶层块（空行分隔）切分并缓存。
 * cacheKey 是这一段的身份（例如 `run-1:2`），同一段反复渲染只重算变化的尾块。
 */
export function renderStreaming(markdown: string, cacheKey: string): string {
  const blocks = splitBlocks(markdown)
  let cache = caches.get(cacheKey)
  if (!cache) {
    cache = { parts: [], html: [] }
    caches.set(cacheKey, cache)
  }
  const html: string[] = []
  for (let i = 0; i < blocks.length; i++) {
    if (cache.parts[i] === blocks[i]) {
      html.push(cache.html[i])
      continue
    }
    cache.parts[i] = blocks[i]
    cache.html[i] = renderBlocks([blocks[i]])
    html.push(cache.html[i])
  }
  cache.parts.length = blocks.length
  cache.html.length = blocks.length
  return html.join('')
}

/** 一次性渲染（历史消息用；没有后续增长，不需要缓存）。 */
export function renderMarkdown(markdown: string): string {
  return renderBlocks([markdown])
}

function renderBlocks(blocks: string[]): string {
  return blocks
    .map((block) => {
      if (!block.trim()) return ''
      if (block.startsWith('```')) return renderCode(block)
      return marked.parse(block, { async: false }) as string
    })
    .join('')
}

/** 代码块：包一层带语言标签与复制按钮的容器。 */
function renderCode(block: string): string {
  const match = /^```([\w+-]*)\n([\s\S]*?)\n?```$/.exec(block.trim())
  if (!match) return marked.parse(block, { async: false }) as string
  const lang = match[1] || 'text'
  const code = escapeHTML(match[2])
  return `<div class="code-wrap"><div class="code-head"><span class="code-lang">${lang}</span><button class="omp-btn text sm" data-copy="1" type="button">复制</button></div><pre><code>${code}</code></pre></div>`
}

/** 按空行切顶层块；围栏代码块内部不切。 */
function splitBlocks(markdown: string): string[] {
  const lines = markdown.split('\n')
  const blocks: string[] = []
  let current: string[] = []
  let fence = false
  for (const line of lines) {
    const trimmed = line.trimStart()
    if (trimmed.startsWith('```')) fence = !fence
    if (!fence && line.trim() === '') {
      if (current.length) blocks.push(current.join('\n'))
      current = []
      continue
    }
    current.push(line)
  }
  if (current.length) blocks.push(current.join('\n'))
  return blocks.length ? blocks : ['']
}

function escapeHTML(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/** 清掉某个渲染缓存的位（一轮结束后调用，避免长会话累积）。 */
export function dropCache(cacheKey: string): void {
  caches.delete(cacheKey)
}

/** 复制按钮的统一处理：在容器上挂一次委托即可。 */
export function handleCopyClick(event: MouseEvent): boolean {
  const target = event.target
  if (!(target instanceof HTMLElement)) return false
  const button = target.closest('[data-copy]')
  if (!(button instanceof HTMLElement)) return false
  const wrap = button.closest('.code-wrap')
  const code = wrap?.querySelector('pre code')?.textContent
  if (!code) return false
  void navigator.clipboard.writeText(code)
  button.textContent = '已复制'
  window.setTimeout(() => {
    button.textContent = '复制'
  }, 1400)
  return true
}
