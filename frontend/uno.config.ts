// UnoCSS 配置：刻度全部接到 CSS 令牌上，原子类不绕过令牌。
// 这样「改一个令牌 = 全站跟着变」这条纪律才成立。
import { defineConfig, presetUno, presetAttributify, transformerDirectives } from 'unocss'

export default defineConfig({
  presets: [presetUno(), presetAttributify()],
  transformers: [transformerDirectives()],
  theme: {
    colors: {
      ink: 'var(--omp-ink)',
      mute: 'var(--omp-mute)',
      faint: 'var(--omp-faint)',
      line: 'var(--omp-line)',
      line2: 'var(--omp-line-strong)',
      surface: 'var(--omp-surface)',
      raised: 'var(--omp-raised)',
      hover: 'var(--omp-hover)',
      selected: 'var(--omp-selected)',
      work: 'var(--omp-work)',
      brand: 'var(--omp-brand)',
      brand2: 'var(--omp-brand-2)',
      brand3: 'var(--omp-brand-3)',
      ink5: 'var(--omp-ink-5)',
      ok: 'var(--omp-ok)',
      warn: 'var(--omp-warn)',
      bad: 'var(--omp-bad)',
    },
    spacing: {
      1: 'var(--omp-sp-1)',
      2: 'var(--omp-sp-2)',
      3: 'var(--omp-sp-3)',
      4: 'var(--omp-sp-4)',
      5: 'var(--omp-sp-5)',
      6: 'var(--omp-sp-6)',
      8: 'var(--omp-sp-8)',
      10: 'var(--omp-sp-10)',
      12: 'var(--omp-sp-12)',
      16: 'var(--omp-sp-16)',
    },
    borderRadius: {
      xs: 'var(--omp-r-xs)',
      sm: 'var(--omp-r-sm)',
      DEFAULT: 'var(--omp-r)',
      lg: 'var(--omp-r-lg)',
      xl: 'var(--omp-r-xl)',
    },
    fontSize: {
      '3xs': ['var(--omp-fs-3xs)', '1.35'],
      '2xs': ['var(--omp-fs-2xs)', '1.4'],
      xs: ['var(--omp-fs-xs)', '1.5'],
      sm: ['var(--omp-fs-sm)', '1.55'],
      base: ['var(--omp-fs-base)', '1.6'],
      lg: ['var(--omp-fs-lg)', '1.45'],
      xl: ['var(--omp-fs-xl)', '1.3'],
      '2xl': ['var(--omp-fs-2xl)', '1.25'],
    },
  },
  shortcuts: {
    // 卡片：分层只靠圆角 + 描边 + 一层极轻阴影，不做渐变
    card: 'bg-raised border border-line rounded-xl',
    'card-pad': 'card p-4',
    // 标签：全大写微字，靠字距拉开
    plate: 'text-2xs font-600 uppercase tracking-[0.08em] text-faint',
    row: 'flex items-center gap-3',
    'row-between': 'flex items-center justify-between gap-3',
    // 只读读数：不套描边胶囊、不做悬停缩放（有盒子没动作等于骗用户去点）
    readout: 'font-mono text-xs text-mute tabular-nums',
  },
})
