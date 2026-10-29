// 管理页的引擎作用域：omp / pi / 全部。
//
// 为什么每页各自一份，而不是复用输入框那个「当前引擎」：
// 「我现在跟谁说话」与「我在管谁的东西」是两个问题。
// 把两者绑在一起，就会出现「想看一眼 pi 的技能，顺手把对话引擎也切了、
// 当前会话还丢了」——那正是之前最容易被抱怨的一类耦合。
//
// 选择落在 sessionStorage：同一类操作（比如连着改几个服务商）会反复进出页面，
// 但不该跨重启保留——下次打开时「全部」通常是最安全的起点。
import { ref } from 'vue'
import type { EngineKind } from '@/types/api'

/** 作用域：某个引擎，或两个都要。 */
export type EngineScope = EngineKind | 'all'

const KEY_PREFIX = 'omp.scope.'

/**
 * 一个页面的作用域状态。
 *
 * page 是持久化用的键后缀（models / skills / plugins / mcp），互不干扰：
 * 看技能时选「全部」不该让模型页也变成全部。
 */
export function useEngineScope(page: string, fallback: EngineScope = 'all') {
  const key = KEY_PREFIX + page
  const stored = sessionStorage.getItem(key)
  const scope = ref<EngineScope>(normalize(stored) ?? fallback)

  function set(next: EngineScope) {
    scope.value = next
    sessionStorage.setItem(key, next)
  }

  return { scope, set }
}

// normalize 把存下来的字符串收窄成合法作用域；非法值当没存过。
function normalize(raw: string | null): EngineScope | null {
  if (raw === 'omp' || raw === 'pi' || raw === 'all') return raw
  return null
}
