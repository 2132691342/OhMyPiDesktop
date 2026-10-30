<script setup lang="ts">
// 服务商登录：哪些已登录、哪些可用、点一下发起登录。
//
// 只有支持界面登录的引擎才渲染这一节（pi 的登录在终端里做），
// 判据由父组件按 Caps.login 决定——能力缺失不渲染，不渲染成灰控件。
//
// 登录要走浏览器完成授权，后端会把地址推给对话页；这里只负责发起。
import { useI18n } from 'vue-i18n'
import type { LoginProvider } from '@/types/api'

const props = defineProps<{
  logins: LoginProvider[]
  /** 正在登录的服务商 id（空表示没有） */
  loggingIn: string
  /** 已登录的数量，显示在标题上 */
  authenticated: number
}>()

const emit = defineEmits<{ login: [provider: LoginProvider] }>()

const { t } = useI18n()

function tone(item: LoginProvider): string {
  if (item.authenticated) return 'ok'
  return item.available ? '' : 'warn'
}

function label(item: LoginProvider): string {
  if (item.authenticated) return t('models.loggedIn')
  return item.available ? t('models.notLoggedIn') : t('models.unavailable')
}
</script>

<template>
  <section>
    <div class="omp-sec-head">
      <span class="omp-sec-title">
        {{ t('models.login') }}
        <span class="count">{{ props.authenticated }}/{{ props.logins.length }}</span>
      </span>
    </div>
    <p class="hint">{{ t('models.loginHint') }}</p>
    <div v-if="props.logins.length" class="login-grid">
      <div v-for="item in props.logins" :key="item.id" class="login-row">
        <div class="login-main">
          <span class="login-name">{{ item.name }}</span>
          <span class="login-id mono">{{ item.id }}</span>
        </div>
        <span class="tag" :class="tone(item)">{{ label(item) }}</span>
        <button
          class="omp-btn sm"
          :class="{ 'is-loading': props.loggingIn === item.id }"
          :disabled="!item.available || Boolean(props.loggingIn)"
          @click="emit('login', item)"
        >
          {{ t('models.login') }}
        </button>
      </div>
    </div>
    <p v-else-if="!props.loggingIn" class="hint">{{ t('chat.engineDown') }}</p>
  </section>
</template>

<style scoped>
.login-grid {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.login-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-3);
  padding: var(--omp-sp-2) var(--omp-sp-3);
  border-radius: var(--omp-r-sm);
}

.login-row:hover {
  background: var(--omp-hover);
}

.login-main {
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex: 1;
  min-width: 0;
}

.login-name {
  font-size: var(--omp-fs-sm);
  color: var(--omp-ink);
}

.login-id {
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-mute);
  letter-spacing: 0;
  text-transform: none;
}

.hint {
  margin: var(--omp-sp-2) 0 0;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}
</style>
