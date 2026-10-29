// 文件预览抽屉的状态。
//
// 它是模块级的单例而不是 store：这件事只有一个使用场景（对话里的工具卡），
// 放进 store 只会给全局状态加一个没人用的字段。
//
// 为什么用抽屉而不是就地展开：就地展开会把消息列的排版撑开，
// 而用户想看差异时往往已经滚到了别处——抽屉不打断滚动位置。
import { ref } from 'vue'

const path = ref('')
const title = ref('')

/** 打开抽屉。title 是给抽屉头显示的名字（通常是文件名）。 */
export function openFilePeek(target: string, label = ''): void {
  path.value = target
  title.value = label
}

/** 关闭抽屉。 */
export function closeFilePeek(): void {
  path.value = ''
  title.value = ''
}

/** 抽屉状态。path 为空表示没打开。 */
export function useFilePeek() {
  return { path, title, open: openFilePeek, close: closeFilePeek }
}
