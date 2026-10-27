<script setup lang="ts">
// 一级导航：图标 + 一行短标签竖排。全站唯一的一级入口，
// 位置固定、宽度恒定，扫一眼就知道「我在哪」。
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppIcon, { type IconName } from '@/components/common/AppIcon.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

interface NavItem {
  name: string
  icon: IconName
  key: string
}

const primary: NavItem[] = [
  { name: 'chat', icon: 'chat', key: 'nav.chat' },
  { name: 'workspace', icon: 'workspace', key: 'nav.workspace' },
  { name: 'sessions', icon: 'clock', key: 'nav.sessions' },
  { name: 'models', icon: 'cpu', key: 'nav.models' },
  { name: 'skills', icon: 'spark', key: 'nav.skills' },
  { name: 'plugins', icon: 'plug', key: 'nav.plugins' },
  { name: 'usage', icon: 'chart', key: 'nav.usage' },
]

const bottom: NavItem[] = [{ name: 'settings', icon: 'gear', key: 'nav.settings' }]

function go(name: string) {
  if (route.name !== name) void router.push({ name })
}
</script>

<template>
  <nav class="rail">
    <div class="rail-group">
      <button
        v-for="item in primary"
        :key="item.name"
        class="rail-item"
        :class="{ active: route.name === item.name }"
        :title="t(item.key)"
        @click="go(item.name)"
      >
        <AppIcon :name="item.icon" :size="19" />
        <span class="rail-label">{{ t(item.key) }}</span>
      </button>
    </div>
    <div class="rail-group bottom">
      <button
        v-for="item in bottom"
        :key="item.name"
        class="rail-item"
        :class="{ active: route.name === item.name }"
        :title="t(item.key)"
        @click="go(item.name)"
      >
        <AppIcon :name="item.icon" :size="19" />
        <span class="rail-label">{{ t(item.key) }}</span>
      </button>
    </div>
  </nav>
</template>

<style scoped>
.rail {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  width: var(--omp-rail);
  flex: none;
  padding: var(--omp-sp-2) 0 var(--omp-sp-3);
  background: var(--omp-window);
  border-right: 1px solid var(--omp-line);
}

.rail-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

/* 五态：悬停放大、按下缩小；选中态 = 淡底 + 左缘竖条 + 主色 */
.rail-item {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  margin: 0 6px;
  padding: 7px 0 6px;
  border: none;
  border-radius: var(--omp-r);
  background: transparent;
  color: var(--omp-mute);
  /* 整行元素：只缩不放。rail 只有 60px 宽，放大 1.06 会溢出被裁。 */
  transition:
    transform var(--omp-dur-fast) var(--omp-ease),
    background-color var(--omp-dur-fast) var(--omp-ease),
    color var(--omp-dur-fast) var(--omp-ease);
}

.rail-item:hover {
  background: var(--omp-hover);
  color: var(--omp-ink);
}

.rail-item:active {
  transform: scale(0.985);
}

.rail-item.active {
  background: var(--omp-selected);
  color: var(--omp-brand);
}

.rail-item.active::before {
  content: '';
  position: absolute;
  left: -6px;
  top: 8px;
  bottom: 8px;
  width: 2px;
  border-radius: var(--omp-r-full);
  background: var(--omp-brand);
}

.rail-label {
  font-size: var(--omp-fs-3xs);
  font-weight: 600;
  letter-spacing: 0.01em;
  line-height: 1;
}
</style>
