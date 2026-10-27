<script setup lang="ts">
// 自绘标题栏：无边框窗口的唯一拖动与窗控区域。
// 中区放当前位置，右区放连接灯与窗控——只回答「我在哪、连上没有」。
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { windowCtl } from '@/api/wails'

const app = useAppStore()
const route = useRoute()

const title = computed(() => String(route.meta.title ?? '对话'))
const connection = computed(() => {
  if (!app.ready) return { cls: 'warn', text: '启动中' }
  if (!app.hasEngine) return { cls: 'bad', text: '引擎未安装' }
  return { cls: 'ok', text: '已连接' }
})
</script>

<template>
  <header class="titlebar" style="--wails-draggable: drag">
    <div class="brand">
      <span class="brand-mark" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round">
          <path d="M4 7.5 12 3l8 4.5v9L12 21l-8-4.5z" />
          <path d="M12 12v9" />
        </svg>
      </span>
      <span class="brand-name">OhMyPi</span>
    </div>
    <div class="crumb">
      <span class="crumb-sep">/</span>
      <span class="crumb-cur">{{ title }}</span>
    </div>
    <div class="right">
      <span class="conn" :title="connection.text">
        <span class="dot" :class="connection.cls" />
        <span class="conn-text">{{ connection.text }}</span>
      </span>
      <div class="win-ctl" style="--wails-draggable: no-drag">
        <button class="win-btn" title="最小化" @click="windowCtl.minimize()">
          <svg viewBox="0 0 12 12" width="11" height="11" stroke="currentColor" stroke-width="1.2"><path d="M2 6h8" /></svg>
        </button>
        <button class="win-btn" title="最大化" @click="windowCtl.toggleMaximize()">
          <svg viewBox="0 0 12 12" width="11" height="11" fill="none" stroke="currentColor" stroke-width="1.2"><rect x="2.5" y="2.5" width="7" height="7" rx="1" /></svg>
        </button>
        <button class="win-btn close" title="关闭" @click="windowCtl.close()">
          <svg viewBox="0 0 12 12" width="11" height="11" stroke="currentColor" stroke-width="1.2"><path d="M3 3l6 6M9 3l-6 6" /></svg>
        </button>
      </div>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  height: var(--omp-topbar);
  flex: none;
  padding-left: var(--omp-sp-3);
  background: var(--omp-window);
  border-bottom: 1px solid var(--omp-line);
  user-select: none;
}

.brand {
  display: flex;
  align-items: center;
  gap: 7px;
  width: calc(var(--omp-rail) + var(--omp-sp-4));
  flex: none;
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: var(--omp-r-sm);
  background: var(--omp-brand);
  color: #fff;
  flex: none;
}

.brand-name {
  font-family: var(--omp-font-display);
  font-size: var(--omp-fs-sm);
  font-weight: 650;
  letter-spacing: 0.01em;
  color: var(--omp-ink);
}

.crumb {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
}

.crumb-sep {
  color: var(--omp-faint);
  font-size: var(--omp-fs-sm);
}

.crumb-cur {
  font-size: var(--omp-fs-sm);
  font-weight: 600;
  color: var(--omp-ink);
  letter-spacing: 0.01em;
}

.right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
}

.conn {
  display: flex;
  align-items: center;
  gap: 6px;
}

.conn-text {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-mute);
  letter-spacing: 0.02em;
}

.win-ctl {
  display: flex;
  height: 100%;
}

.win-btn {
  display: grid;
  place-items: center;
  width: 42px;
  height: 100%;
  border: none;
  background: transparent;
  color: var(--omp-mute);
  transition:
    background-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease);
}

.win-btn:hover {
  background: var(--omp-hover);
  color: var(--omp-ink);
}

.win-btn:active {
  background: var(--omp-selected);
}

.win-btn.close:hover {
  background: var(--omp-bad);
  color: #fff;
}
</style>
