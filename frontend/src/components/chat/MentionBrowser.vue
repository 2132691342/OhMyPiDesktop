<script setup lang="ts">
// @ 引用的文件浏览器：工作区内逐级浏览，点文件即加入待发送附件。
//
// 为什么不是模糊搜索整个工作区：索引全部文件名要在前端维护一份文件表，
// 而工作区可能几十万个文件。逐级浏览用的是后端已有的 /files 接口，
// 与「工作区」页看到的是同一份目录，不会出现「这里搜得到、那里打不开」。
//
// 键盘导航自己实现：↑↓ 移动高亮、回车选中。父组件把按键转给这里，
// 因为菜单列表在这一层（父层只是个壳）。
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/http'
import type { FileEntry } from '@/types/api'
import AppIcon from '@/components/common/AppIcon.vue'

const emit = defineEmits<{
  pick: [relPath: string, name: string]
  close: []
}>()

const { t } = useI18n()

const dirPath = ref('')
const entries = ref<FileEntry[]>([])
const filter = ref('')
const loading = ref(false)
const failed = ref('')
// 当前高亮的下标。-1 表示还没高亮（列表为空或正在加载）。
const index = ref(-1)

/** 面包屑：根 + 每一级。点任意一级跳过去。 */
const crumbs = computed(() => {
  const parts = dirPath.value ? dirPath.value.split('/').filter(Boolean) : []
  return [
    { name: t('chat.mentionRoot'), path: '' },
    ...parts.map((part, i) => ({ name: part, path: parts.slice(0, i + 1).join('/') })),
  ]
})

/** 按过滤词筛。目录始终显示——用户是在找路，不是在做精确搜索。 */
const shown = computed(() => {
  const q = filter.value.trim().toLowerCase()
  if (!q) return entries.value
  return entries.value.filter((e) => e.dir || e.entry_name.toLowerCase().includes(q))
})

// 连点两个目录时两次请求会并发返回：只靠 loading 判断会让列表错位。
// 用序号判新旧，旧响应直接丢掉。
let seq = 0

async function load(path: string): Promise<void> {
  const mine = ++seq
  loading.value = true
  failed.value = ''
  dirPath.value = path
  filter.value = ''
  index.value = -1
  try {
    const list = await api.files(path)
    if (mine !== seq) return
    entries.value = list.entries ?? []
  } catch (err) {
    if (mine !== seq) return
    entries.value = []
    failed.value = err instanceof Error ? err.message : String(err)
  } finally {
    if (mine === seq) loading.value = false
  }
}

function open(entry: FileEntry): void {
  if (entry.dir) {
    void load(dirPath.value ? `${dirPath.value}/${entry.entry_name}` : entry.entry_name)
    return
  }
  const rel = dirPath.value ? `${dirPath.value}/${entry.entry_name}` : entry.entry_name
  emit('pick', rel, entry.entry_name)
}

function up(): void {
  const idx = dirPath.value.lastIndexOf('/')
  void load(idx > 0 ? dirPath.value.slice(0, idx) : '')
}

/**
 * 把第 i 项带进可视区。
 *
 * 用容器自己的 scrollTop 算，而不是 scrollIntoView：
 * 后者会滚动所有可滚动的祖先，于是「菜单里按一下 ↓」把整个聊天区一起挪走。
 */
function ensureVisible(i: number): void {
  const box = listEl.value
  if (!box) return
  const el = box.querySelector<HTMLElement>(`[data-idx="${i}"]`)
  if (!el) return
  const top = el.offsetTop - box.offsetTop
  const bottom = top + el.offsetHeight
  if (top < box.scrollTop) box.scrollTop = top
  else if (bottom > box.scrollTop + box.clientHeight) box.scrollTop = bottom - box.clientHeight
}

const listEl = ref<HTMLElement | null>(null)

/** 上下移动高亮。到底部后回到顶部（循环）。 */
function move(delta: number): boolean {
  const total = shown.value.length
  if (!total) return false
  index.value = (index.value + delta + total) % total
  void Promise.resolve().then(() => ensureVisible(index.value))
  return true
}

/** 选中当前高亮项。 */
function pickCurrent(): boolean {
  const entry = shown.value[index.value]
  if (!entry) return false
  open(entry)
  return true
}

const listElRef = listEl

onMounted(() => void load(''))

// 暴露给父组件的重载入口（@ 菜单再次打开时刷新当前目录）。
defineExpose({ reload: () => load(dirPath.value), move, pickCurrent, listEl: listElRef })
</script>

<template>
  <div class="mention">
    <header class="mention-head">
      <button
        class="omp-btn icon ghost"
        :disabled="!dirPath"
        :title="t('chat.mentionUp')"
        @click="up"
      >
        <AppIcon name="arrow-left" :size="13" />
      </button>
      <nav class="crumbs">
        <template v-for="(crumb, i) in crumbs" :key="crumb.path">
          <span v-if="i > 0" class="sep">/</span>
          <button class="crumb" @click="load(crumb.path)">{{ crumb.name }}</button>
        </template>
      </nav>
    </header>

    <input
      v-model="filter"
      class="omp-input mention-filter"
      :placeholder="t('chat.mentionFilter')"
      spellcheck="false"
      @input="index = -1"
    />

    <div ref="listEl" class="mention-body">
      <p v-if="loading" class="mention-note">{{ t('common.loading') }}</p>
      <p v-else-if="failed" class="mention-note bad">{{ failed }}</p>
      <p v-else-if="!shown.length" class="mention-note">{{ t('chat.mentionEmpty') }}</p>
      <button
        v-for="(entry, i) in shown"
        :key="entry.entry_name"
        :data-idx="i"
        class="mention-row"
        :class="{ dir: entry.dir, 'is-hi': i === index }"
        @mouseenter="index = i"
        @click="open(entry)"
      >
        <AppIcon :name="entry.dir ? 'folder' : 'file'" :size="13" />
        <span class="mention-name">{{ entry.entry_name }}</span>
        <span v-if="entry.dir" class="mention-go">›</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
/* 填满 .pop 的可用高度：菜单面板自己定了上限，这里只负责把内容铺进去，
   滚动交给内部列表。min-height: 0 是 flex 子项能真正收缩的前提。 */
.mention {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-2);
  min-height: 0;
  flex: 1;
  padding: 0 var(--omp-sp-1) var(--omp-sp-1);
}

.mention-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  min-width: 0;
}

.crumbs {
  display: flex;
  align-items: center;
  gap: 2px;
  min-width: 0;
  overflow-x: auto;
  white-space: nowrap;
  scrollbar-width: none;
}

.crumbs::-webkit-scrollbar {
  display: none;
}

.crumb {
  border: 0;
  background: transparent;
  padding: 1px 4px;
  border-radius: var(--omp-r-xs);
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  cursor: pointer;
}

.crumb:hover {
  background: var(--omp-hover);
  color: var(--omp-brand);
}

.sep {
  color: var(--omp-faint);
  font-size: var(--omp-fs-2xs);
}

.mention-filter {
  height: var(--omp-ctl-sm);
  font-size: var(--omp-fs-2xs);
}

.mention-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.mention-row {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 4px 6px;
  border: 0;
  border-radius: var(--omp-r-xs);
  background: transparent;
  color: var(--omp-ink);
  font-size: var(--omp-fs-2xs);
  text-align: left;
  cursor: pointer;
}

.mention-row:hover,
.mention-row.is-hi {
  background: var(--omp-brand-soft);
  color: var(--omp-brand);
}

.mention-row.dir {
  color: var(--omp-ink-2);
}

.mention-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--omp-font-mono);
}

.mention-go {
  color: var(--omp-faint);
}

.mention-note {
  margin: 0;
  padding: var(--omp-sp-2);
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
}

.mention-note.bad {
  color: var(--omp-bad);
}
</style>
