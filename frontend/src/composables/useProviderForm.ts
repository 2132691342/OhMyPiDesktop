// 服务商的编辑草稿：表单内容与「打开 / 保存 / 拉模型」三个动作。
//
// 拆出来的理由：模型页要管三件事（服务商清单、可用模型表、登录区），
// 而服务商的编辑草稿与上游探测是自成一块的——它有自己的表单状态、
// 自己的弹窗、自己的几个动作。留在页面里会让那个页面一半在管清单
// 一半在管一张草稿表。
//
// 草稿与清单是两份数据：草稿是「正在编辑的这一份」，
// 清单是「已保存的那些」。混成一份会让取消编辑变成不可能。
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/http'
import { useToasts } from '@/composables/useToasts'
import type { EngineKind, ModelEntry, ProviderView } from '@/types/api'

/** 草稿形态：与 ProviderView 对齐，但密钥是可以改的空串而不是掩码。 */
export interface ProviderForm {
  name: string
  base_url: string
  api: string
  api_key: string
  models: ModelEntry[]
}

/**
 * 模型能力的底：留空不能等于 0。
 *
 * 0 会让水位环永远显示 0%，引擎也拿不到可用预算。
 * 这两个值与引擎侧的默认值一致。
 */
export const DEFAULT_CONTEXT_WINDOW = 128000
export const DEFAULT_MAX_TOKENS = 8192

/**
 * 服务商草稿的操作集。
 *
 * reload 是保存后的刷新回调，由调用方给——它还要同时刷新模型表与登录区，
 * 那些不在本文件里。
 */
export function useProviderForm(
  reload: () => Promise<void>,
  /** 当前作用域的引擎：新建时写入哪个引擎的配置 */
  defaultKind: () => EngineKind,
) {
  const { t } = useI18n()
  const toasts = useToasts()

  const dialogOpen = ref(false)
  const saving = ref(false)
  // 正在编辑的服务商名（空表示新建）
  const editing = ref('')
  // 正在编辑的那一行属于哪个引擎：两个引擎的配置完全独立，同名可以各有一份。
  const editingKind = ref<EngineKind>('omp')
  // 弹窗里的「拉模型」按钮单独一个标志：它与卡片上的探测不是一回事。
  const fetching = ref(false)

  const form = ref<ProviderForm>({
    name: '',
    base_url: '',
    api: 'openai-completions',
    api_key: '',
    models: [],
  })

  function reset(): void {
    editing.value = ''
    form.value = { name: '', base_url: '', api: 'openai-completions', api_key: '', models: [] }
  }

  function openCreate(): void {
    reset()
    editingKind.value = defaultKind()
    dialogOpen.value = true
  }

  function openEdit(view: ProviderView): void {
    editing.value = view.provider_name
    // 记住这一行属于哪个引擎：编辑同名服务商时不能写到另一个引擎的文件里。
    editingKind.value = (view.engine_kind || defaultKind()) as EngineKind
    form.value = {
      name: view.provider_name,
      base_url: view.base_url,
      api: view.api || 'openai-completions',
      // 打码后的密钥不回填：用户没改就提交空串，后端会保留原值
      api_key: '',
      models: (view.models ?? []).map((m) => ({ ...m })),
    }
    dialogOpen.value = true
  }

  /** 把一批模型追加进草稿；已有的不重复加。返回新加了几条。 */
  function appendModels(rows: { model_id: string; model_name?: string }[]): number {
    const existing = new Set(form.value.models.map((m) => m.model_id))
    let added = 0
    for (const row of rows) {
      if (!row.model_id || existing.has(row.model_id)) continue
      existing.add(row.model_id)
      form.value.models.push({
        model_id: row.model_id,
        model_name: row.model_name || '',
        context_window: DEFAULT_CONTEXT_WINDOW,
        max_tokens: DEFAULT_MAX_TOKENS,
        reasoning: false,
        input: ['text'],
      })
      added++
    }
    return added
  }

  /**
   * 向上游问模型清单并填进当前草稿。
   *
   * 与卡片上的「拉模型」的区别：那个是「拉出来给你看看」，
   * 这个是「正在填的服务商，拉回来直接补进表里」。两者共用同一个接口。
   */
  async function fetchModels(): Promise<void> {
    if (fetching.value) return
    fetching.value = true
    try {
      const res = await api.fetchProviderModels({
        engine_kind: editingKind.value,
        provider_name: editing.value || undefined,
        base_url: form.value.base_url.trim(),
        api: form.value.api,
        api_key: form.value.api_key.trim(),
      })
      const rows = res.models ?? []
      if (!rows.length) {
        toasts.push('info', t('models.fetchedEmpty'))
        return
      }
      const added = appendModels(rows)
      toasts.push('ok', added ? t('models.fetched', { n: added }) : t('models.fetchedEmpty'))
    } catch (err) {
      toasts.push('error', err instanceof Error ? err.message : String(err))
    } finally {
      fetching.value = false
    }
  }

  async function save(): Promise<void> {
    const name = form.value.name.trim()
    if (!name) return
    saving.value = true
    try {
      const payload: Partial<ProviderView> & { models?: ModelEntry[] } = {
        base_url: form.value.base_url.trim(),
        api: form.value.api,
        api_key: form.value.api_key.trim(),
        models: form.value.models.filter((m) => m.model_id.trim()),
      }
      // 写入必须落到一个具体引擎：编辑已有行时用它自己的 engine_kind，
      // 新建时用当前作用域（「全部」时退回 omp）。
      const target = editing.value ? editingKind.value : defaultKind()
      await api.upsertProvider(name, payload, target)
      dialogOpen.value = false
      toasts.push('ok', t('common.saved'))
      await reload()
    } catch (err) {
      toasts.push('error', err instanceof Error ? err.message : String(err))
    } finally {
      saving.value = false
    }
  }

  return {
    dialogOpen,
    saving,
    fetching,
    editing,
    editingKind,
    form,
    openCreate,
    openEdit,
    appendModels,
    fetchModels,
    save,
  }
}
