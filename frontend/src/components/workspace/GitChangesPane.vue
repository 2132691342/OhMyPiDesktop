<script setup lang="ts">
// 改动清单：暂存 / 未暂存两组，组内按目录折叠。
//
// 按目录折叠的理由：一次改动动辄十几个文件，平铺成一列时
// 「都在哪儿改的」这个信息被拍没了——而那正是判断「这次改动范围对不对」
// 的第一眼。折叠起来后一眼看到的是几个目录，点开才是文件。
//
// 本组件只渲染与派发事件，不自己发任何写请求：暂存 / 丢弃 / 取消暂存
// 都是不可逆或影响别人的动作，由父组件统一挂 loading 并在失败时给原文。
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { GitFile } from '@/types/api'

const props = defineProps<{
  staged: GitFile[]
  unstaged: GitFile[]
  /** 当前选中的文件路径 */
  selected: string
  /** 进行中的动作名（空表示空闲） */
  pending: string
}>()

const emit = defineEmits<{
  pick: [file: GitFile]
  stage: [file: GitFile]
  unstage: [file: GitFile]
  discard: [file: GitFile]
  stageAll: []
  history: [file: GitFile]
}>()

const { t } = useI18n()

/** 被折叠的目录集合。默认全展开：改动通常不多，先全看到。 */
const collapsed = ref<Set<string>>(new Set())

/** 一个目录分组。 */
interface Group {
  dir: string
  files: GitFile[]
}

/** 按路径里的目录分段。根目录的文件归到 '.'。 */
function dirOf(file: GitFile): string {
  const path = file.file_path.replace(/\\/g, '/')
  const idx = path.lastIndexOf('/')
  return idx < 0 ? '.' : path.slice(0, idx)
}

function groupBy(files: GitFile[]): Group[] {
  const map = new Map<string, GitFile[]>()
  for (const file of files) {
    const dir = dirOf(file)
    const list = map.get(dir)
    if (list) list.push(file)
    else map.set(dir, [file])
  }
  return [...map.entries()]
    .map(([dir, files]) => ({ dir, files }))
    .sort((a, b) => a.dir.localeCompare(b.dir))
}

const stagedGroups = computed(() => groupBy(props.staged))
const unstagedGroups = computed(() => groupBy(props.unstaged))

function isCollapsed(key: string): boolean {
  return collapsed.value.has(key)
}

function toggle(key: string): void {
  const next = new Set(collapsed.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  collapsed.value = next
}

function changeTag(file: GitFile): string {
  if (file.conflict) return 'U'
  return file.change_code || '?'
}

/** 目录里改动的合计，折叠时也能看到规模。 */
function groupStat(files: GitFile[]): { adds: number; dels: number } {
  let adds = 0
  let dels = 0
  for (const f of files) {
    adds += f.additions ?? 0
    dels += f.deletions ?? 0
  }
  return { adds, dels }
}
</script>

<template>
  <div class="changes">
    <div v-if="!props.staged.length && !props.unstaged.length" class="empty-mini">
      {{ t('workspace.noChanges') }}
    </div>

    <template v-else>
      <section v-if="props.staged.length" class="group">
        <div class="group-head">
          <span>{{ t('workspace.staged') }}</span>
          <span class="count">{{ props.staged.length }}</span>
        </div>
        <div v-for="g in stagedGroups" :key="`s-${g.dir}`" class="dir">
          <button class="dir-head" @click="toggle(`s-${g.dir}`)">
            <AppIcon
              name="chevron"
              :size="11"
              class="dir-ic"
              :class="{ collapsed: isCollapsed(`s-${g.dir}`) }"
            />
            <AppIcon name="folder" :size="12" />
            <span class="dir-name">{{ g.dir }}</span>
            <span class="dir-stat">
              <b class="add">+{{ groupStat(g.files).adds }}</b>
              <b class="del">−{{ groupStat(g.files).dels }}</b>
            </span>
          </button>
          <template v-if="!isCollapsed(`s-${g.dir}`)">
            <div
              v-for="file in g.files"
              :key="`s:${file.file_path}`"
              class="file-line omp-row-click"
              :class="{ active: props.selected === file.file_path }"
              @click="emit('pick', file)"
            >
              <span class="chg" :class="`chg-${changeTag(file)}`">{{ changeTag(file) }}</span>
              <span class="path">{{ file.file_path.split(/[\\/]/).pop() }}</span>
              <span class="num">
                <b class="add">+{{ file.additions }}</b><b class="del">−{{ file.deletions }}</b>
              </span>
              <button
                class="omp-btn text sm"
                :title="t('history.forFile')"
                @click.stop="emit('history', file)"
              >
                {{ t('workspace.history') }}
              </button>
              <button
                class="omp-btn text sm"
                :disabled="Boolean(props.pending)"
                @click.stop="emit('unstage', file)"
              >
                {{ t('workspace.unstage') }}
              </button>
            </div>
          </template>
        </div>
      </section>

      <section v-if="props.unstaged.length" class="group">
        <div class="group-head">
          <span>{{ t('workspace.unstaged') }}</span>
          <span class="count">{{ props.unstaged.length }}</span>
          <button
            class="omp-btn text sm push"
            :disabled="Boolean(props.pending)"
            @click="emit('stageAll')"
          >
            {{ t('workspace.stageAll') }}
          </button>
        </div>
        <div v-for="g in unstagedGroups" :key="`u-${g.dir}`" class="dir">
          <button class="dir-head" @click="toggle(`u-${g.dir}`)">
            <AppIcon
              name="chevron"
              :size="11"
              class="dir-ic"
              :class="{ collapsed: isCollapsed(`u-${g.dir}`) }"
            />
            <AppIcon name="folder" :size="12" />
            <span class="dir-name">{{ g.dir }}</span>
            <span class="dir-stat">
              <b class="add">+{{ groupStat(g.files).adds }}</b>
              <b class="del">−{{ groupStat(g.files).dels }}</b>
            </span>
          </button>
          <template v-if="!isCollapsed(`u-${g.dir}`)">
            <div
              v-for="file in g.files"
              :key="`u:${file.file_path}`"
              class="file-line omp-row-click"
              :class="{ active: props.selected === file.file_path }"
              @click="emit('pick', file)"
            >
              <span class="chg" :class="`chg-${changeTag(file)}`">{{ changeTag(file) }}</span>
              <span class="path">{{ file.file_path.split(/[\\/]/).pop() }}</span>
              <span class="num">
                <b class="add">+{{ file.additions }}</b><b class="del">−{{ file.deletions }}</b>
              </span>
              <button
                class="omp-btn text sm"
                :title="t('history.forFile')"
                @click.stop="emit('history', file)"
              >
                {{ t('workspace.history') }}
              </button>
              <button
                class="omp-btn text sm"
                :disabled="Boolean(props.pending)"
                @click.stop="emit('stage', file)"
              >
                {{ t('workspace.stage') }}
              </button>
              <button
                class="omp-btn text sm danger"
                :disabled="Boolean(props.pending)"
                @click.stop="emit('discard', file)"
              >
                {{ t('workspace.discard') }}
              </button>
            </div>
          </template>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.changes {
  display: flex;
  flex-direction: column;
  gap: var(--omp-sp-3);
}

.group-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  font-size: var(--omp-fs-2xs);
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--omp-faint);
  margin-bottom: var(--omp-sp-1);
}

.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-mute);
}

.push {
  margin-left: auto;
}

/* 目录头：可点的横条，带这一组的增删合计 */
.dir-head {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 3px var(--omp-sp-2);
  border: 0;
  background: transparent;
  color: var(--omp-mute);
  font: inherit;
  font-size: var(--omp-fs-3xs);
  text-align: left;
  border-radius: var(--omp-r-sm);
  cursor: pointer;
}

.dir-head:hover {
  background: var(--omp-hover);
  color: var(--omp-ink);
}

.dir-ic {
  flex: none;
  transform: rotate(90deg);
  transition: transform var(--omp-dur-fast) var(--omp-ease);
}

.dir-ic.collapsed {
  transform: rotate(0deg);
}

.dir-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--omp-font-mono);
}

.dir-stat {
  display: flex;
  gap: var(--omp-sp-2);
  font-family: var(--omp-font-mono);
  flex: none;
}

.dir-stat .add {
  color: var(--omp-ok);
}

.dir-stat .del {
  color: var(--omp-bad);
}

.file-line {
  display: flex;
  align-items: center;
  gap: var(--omp-sp-2);
  width: 100%;
  padding: 3px var(--omp-sp-2);
  border: none;
  background: transparent;
  color: var(--omp-ink);
  font-size: var(--omp-fs-xs);
  text-align: left;
  border-radius: var(--omp-r-sm);
  cursor: pointer;
}

.file-line .path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--omp-font-mono);
}

.num {
  display: flex;
  gap: 6px;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  flex: none;
}

.num .add {
  color: var(--omp-ok);
}

.num .del {
  color: var(--omp-bad);
}

.chg {
  width: 16px;
  text-align: center;
  font-family: var(--omp-font-mono);
  font-size: var(--omp-fs-3xs);
  font-weight: 700;
  flex: none;
}

.chg-M {
  color: var(--omp-warn);
}
.chg-A {
  color: var(--omp-ok);
}
.chg-D {
  color: var(--omp-bad);
}
.chg-R,
.chg-C {
  color: var(--omp-brand);
}
.chg-U {
  color: var(--omp-bad);
}
.chg-\? {
  color: var(--omp-faint);
}

.empty-mini {
  padding: var(--omp-sp-4);
  color: var(--omp-faint);
  font-size: var(--omp-fs-2xs);
  text-align: center;
}
</style>
