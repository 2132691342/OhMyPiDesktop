<script setup lang="ts">
// MCP 服务器表：名字、传输方式、目标、作用域、状态，加启停 / 编辑 / 删除。
// 状态列取值来源不同、下一步也不同（通了就这样 / 失败看日志 / 原文不猜 /
// disabled 打开就行 / configured 点「检测」）——判据留这里，父组件只给数据与探针开关。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/common/AppIcon.vue'
import type { McpServer } from '@/types/api'

const props = defineProps<{
  servers: McpServer[]
  loading: boolean
  /** 正在操作的那一条的服务器名（空表示空闲） */
  busyName: string
}>()

const emit = defineEmits<{
  create: []
  edit: [row: McpServer]
  remove: [row: McpServer]
  toggle: [row: McpServer, enabled: boolean]
}>()

const { t } = useI18n()

function statusTag(row: McpServer): { cls: string; text: string } {
  if (row.live_status === 'connected') return { cls: 'ok', text: t('mcp.connected') }
  if (row.live_status === 'failed') return { cls: 'bad', text: t('mcp.failed') }
  if (row.live_status) return { cls: 'warn', text: row.live_status }
  if (!row.enabled) return { cls: '', text: t('mcp.disabled') }
  return { cls: 'brand', text: t('mcp.configured') }
}

/** 有服务器被停用名单压住：得说明原因，否则用户只会以为开关坏了。 */
const anyDenied = computed(() => props.servers.some((s) => s.disabled_by_deny))
</script>

<template>
  <section>
    <div class="omp-sec-head">
      <span class="omp-sec-title">
        {{ t('mcp.servers') }} <span class="count">{{ props.servers.length }}</span>
      </span>
    </div>

    <div v-if="!props.servers.length && !props.loading" class="omp-empty">
      <span class="omp-tile omp-tile--lg omp-tile--neutral"><AppIcon name="server" :size="22" /></span>
      <p class="omp-empty-title">{{ t('mcp.emptyTitle') }}</p>
      <p class="omp-empty-hint">{{ t('mcp.emptyHint') }}</p>
      <button class="omp-btn sm primary" @click="emit('create')">{{ t('mcp.add') }}</button>
    </div>

    <el-table v-else :data="props.servers" size="small">
      <el-table-column :label="t('mcp.name')" min-width="140">
        <template #default="{ row }">
          <div class="cell-name">
            <span class="name">{{ row.server_name }}</span>
            <span v-if="row.desc" class="desc">{{ row.desc }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('mcp.transport')" width="92">
        <template #default="{ row }">
          <span class="tag">{{ row.transport_kind || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('mcp.target')" min-width="180">
        <template #default="{ row }">
          <span class="omp-mono clip">{{ row.command_line || row.server_url || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('mcp.scope')" width="86">
        <template #default="{ row }">
          <span class="tag">{{ row.scope_kind }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('mcp.status')" width="104">
        <template #default="{ row }">
          <span class="tag" :class="statusTag(row).cls">{{ statusTag(row).text }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" width="188" align="right">
        <template #default="{ row }">
          <div class="row-actions">
            <el-switch
              :model-value="row.enabled"
              :disabled="Boolean(props.busyName)"
              @change="(v: string | number | boolean) => emit('toggle', row, Boolean(v))"
            />
            <button class="omp-btn text sm" :disabled="Boolean(props.busyName)" @click="emit('edit', row)">
              {{ t('common.edit') }}
            </button>
            <button
              class="omp-btn text sm danger"
              :class="{ 'is-loading': props.busyName === row.server_name }"
              :disabled="Boolean(props.busyName)"
              @click="emit('remove', row)"
            >
              {{ t('common.delete') }}
            </button>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <!-- 停用名单压住时说明原因：否则用户只会以为开关坏了 -->
    <p v-if="anyDenied" class="hint">{{ t('mcp.denyHint') }}</p>
  </section>
</template>

<style scoped>
.count {
  font-family: var(--omp-font-mono);
  color: var(--omp-faint);
  letter-spacing: 0;
  text-transform: none;
}

.cell-name {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.name {
  font-weight: 650;
  color: var(--omp-ink);
}

.desc {
  font-size: var(--omp-fs-2xs);
  color: var(--omp-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.clip {
  display: block;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--omp-sp-2);
}

.hint {
  margin: var(--omp-sp-2) 0 0;
  font-size: var(--omp-fs-3xs);
  color: var(--omp-faint);
}
</style>
