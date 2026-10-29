// 工作区页的 git 写动作：暂存 / 取消暂存 / 丢弃 / 提交 / 分支 / 同步。
//
// 全部收口在 act() 一个入口里，理由是所有写动作共享同一套后果处理：
// 挂 loading → 成功后重拉状态与差异 → 失败把引擎（git）的原话给用户。
// 散着写会让某个动作忘了重拉，界面就停在旧状态上却看不出原因。
//
// 不可逆的动作（丢弃改动、强推、中止合并）各自先问一次确认。
// 确认放在这里而不是调用方：判据是「这个动作有没有可能毁掉别人的东西」，
// 与谁点它无关。
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { api } from '@/api/http'
import { useToasts } from '@/composables/useToasts'
import type { GitFile, GitStatus } from '@/types/api'

/**
 * 丢弃改动 / 强推 / 中止这类动作的确认。
 *
 * 话术与按钮文字由调用方给：确认框要复述「将发生什么」，
 * 而那句话只有调用方知道（丢弃的是哪个文件、中止的是哪次合并）。
 */
async function confirmDanger(
  body: string,
  title: string,
  okText: string,
  cancelText: string,
): Promise<boolean> {
  try {
    await ElMessageBox.confirm(body, title, {
      confirmButtonText: okText,
      cancelButtonText: cancelText,
      type: 'warning',
    })
    return true
  } catch {
    return false
  }
}

/**
 * 绑定一个「当前状态 + 刷新动作」。
 *
 * pending 暴露给界面挂 loading（按钮靠它禁用与转圈），
 * 刷新回调由调用方给——它还要同时刷新分支与历史，那些不在本文件里。
 */
export function useGitActions(
  status: () => GitStatus | null,
  reload: () => Promise<void>,
) {
  const { t } = useI18n()
  const toasts = useToasts()
  const pending = ref('')

  async function act(name: string, fn: () => Promise<GitStatus | void>): Promise<void> {
    pending.value = name
    try {
      await fn()
      // 动作自己的返回值不直接写进 status：它只是这一次的响应，
      // 而界面上还有分支、历史、差异要看——统一重拉才不会只更新一半。
      await reload()
    } catch (err) {
      toasts.push('error', err instanceof Error ? err.message : String(err))
    } finally {
      pending.value = ''
    }
  }

  /** 唯一远端时直接用它；有多个或没有就留空，让后端挑默认。 */
  function soleRemote(): string {
    const remotes = status()?.remotes ?? []
    return remotes.length === 1 ? remotes[0].remote_name : ''
  }

  async function stageAll(files: GitFile[]): Promise<void> {
    await act('stage', () => api.gitStage(files.map((f) => f.file_path)))
  }

  async function stage(file: GitFile): Promise<void> {
    await act(`stage:${file.file_path}`, () => api.gitStage([file.file_path]))
  }

  async function unstage(file: GitFile): Promise<void> {
    await act(`unstage:${file.file_path}`, () => api.gitUnstage([file.file_path]))
  }

  async function discard(file: GitFile): Promise<void> {
    const ok = await confirmDanger(
      t('workspace.discardConfirm', { path: file.file_path }),
      t('workspace.discardTitle'),
      t('workspace.discard'),
      t('common.cancel'),
    )
    if (!ok) return
    await act(`discard:${file.file_path}`, () => api.gitDiscard([file.file_path]))
  }

  async function resolveConflicts(files: GitFile[]): Promise<void> {
    await act('resolve', () => api.gitResolve(files.map((f) => f.file_path)))
  }

  async function stateAction(action: 'continue' | 'abort'): Promise<void> {
    if (action === 'abort') {
      const ok = await confirmDanger(
        t('workspace.abortConfirm'),
        t('workspace.abortTitle'),
        t('workspace.abort'),
        t('common.cancel'),
      )
      if (!ok) return
    }
    await act(`state:${action}`, () => api.gitState(action))
  }

  async function initRepo(): Promise<void> {
    await act('init', () => api.gitInit('main'))
  }

  async function switchBranch(name: string): Promise<void> {
    await act(`switch:${name}`, () => api.gitSwitchBranch(name))
  }

  async function createBranch(): Promise<void> {
    let name = ''
    try {
      const result = await ElMessageBox.prompt(t('workspace.branchName'), t('workspace.newBranch'), {
        confirmButtonText: t('common.save'),
        cancelButtonText: t('common.cancel'),
      })
      name = result.value
    } catch {
      return // 用户取消
    }
    await act('branch', () => api.gitCreateBranch(name))
  }

  /**
   * 推送。
   *
   * force 分三档：none 普通、lease 带租约、force 无条件。
   * 无条件强推会静默覆盖别人的提交，所以要用户输入分支名确认。
   */
  async function push(force = 'none'): Promise<void> {
    const branch = status()?.branch_name ?? ''
    let confirm = ''
    if (force === 'force') {
      try {
        const { value } = await ElMessageBox.prompt(
          t('workspace.forceConfirmPlaceholder', { name: branch }),
          t('workspace.forcePush'),
          { confirmButtonText: t('workspace.forcePush'), cancelButtonText: t('common.cancel') },
        )
        confirm = value
      } catch {
        return
      }
    }
    await act(`push:${force}`, () =>
      api.gitPush(soleRemote(), branch, force, !status()?.upstream_ref, confirm),
    )
  }

  async function pull(): Promise<void> {
    const branch = status()?.branch_name ?? ''
    await act('pull', () => api.gitPull(soleRemote(), branch, false))
  }

  async function fetchRemote(): Promise<void> {
    await act('fetch', () => api.gitFetch(soleRemote()))
  }

  return {
    pending,
    stageAll,
    stage,
    unstage,
    discard,
    resolveConflicts,
    stateAction,
    initRepo,
    switchBranch,
    createBranch,
    push,
    pull,
    fetchRemote,
  }
}
