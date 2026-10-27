<script setup lang="ts">
// 应用外壳：自绘标题栏 + 左侧导航 rail + 主区。
// 启动失败与首屏加载都在这里处理，视图层不需要各自兜底。
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import AppRail from '@/components/layout/AppRail.vue'
import TitleBar from '@/components/layout/TitleBar.vue'
import ToastHost from '@/components/common/ToastHost.vue'
import StartupError from '@/components/common/StartupError.vue'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const { t } = useI18n()

onMounted(() => {
  app.watchStartupFatal()
  void app.load()
})
</script>

<template>
  <div class="shell">
    <TitleBar />
    <div class="shell-main">
      <AppRail v-if="app.ready" />
      <main class="shell-view">
        <div v-if="app.fatal" class="shell-fallback">
          <StartupError :message="app.fatal" />
        </div>
        <div v-else-if="!app.ready" class="shell-fallback">
          <div class="boot">
            <div class="boot-spinner" />
            <p class="boot-text">{{ t('app.starting') }}</p>
          </div>
        </div>
        <router-view v-else v-slot="{ Component }">
          <transition name="omp-fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
    <ToastHost />
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--omp-window);
}

.shell-main {
  display: flex;
  flex: 1;
  min-height: 0;
}

.shell-view {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--omp-work);
}

.shell-fallback {
  flex: 1;
  display: grid;
  place-items: center;
  padding: var(--omp-sp-8);
}

.boot {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--omp-sp-4);
}

.boot-spinner {
  width: 22px;
  height: 22px;
  border: 2px solid var(--omp-line-strong);
  border-top-color: var(--omp-brand);
  border-radius: 50%;
  animation: boot-spin 720ms linear infinite;
}

@keyframes boot-spin {
  to {
    transform: rotate(360deg);
  }
}

.boot-text {
  margin: 0;
  font-size: var(--omp-fs-sm);
  color: var(--omp-mute);
}
</style>
