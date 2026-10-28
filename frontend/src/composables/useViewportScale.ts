// 视口比例缩放：把「窗口宽 ÷ 1440」写进 --omp-vw-scale。
//
// 存在理由：同一套界面在 1280 与 2560 上不能是同一套像素刻度——
// 小窗口上 40px 的间距会挤掉内容，大窗口上 12px 的字又显得局促。
// 组件全部走 --omp-* 令牌，所以改这一个值就够了，不必为每个断点写媒体查询。
//
// 上下限锁死在 0.75~1.35：再小字就没法读，再大就变成放大模式。
import { onMounted, onUnmounted } from 'vue'

const BASE_WIDTH = 1440
const MIN_SCALE = 0.75
const MAX_SCALE = 1.35

function apply() {
  const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, window.innerWidth / BASE_WIDTH))
  document.documentElement.style.setProperty('--omp-vw-scale', String(Number(scale.toFixed(3))))
}

export function useViewportScale() {
  let timer: number | undefined
  const onResize = () => {
    window.clearTimeout(timer)
    timer = window.setTimeout(apply, 80)
  }
  onMounted(() => {
    apply()
    window.addEventListener('resize', onResize)
  })
  onUnmounted(() => {
    window.clearTimeout(timer)
    window.removeEventListener('resize', onResize)
  })
}
