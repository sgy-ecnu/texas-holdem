import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from '../api'
import { chips } from '../format'
import type { GameDTO, TableDTO, TableSummaryDTO } from '../types'

// 牌桌模板列表（默认入口）：浏览模板、开一局、编辑、清空、删除。
// 列表摘要不含完整 seats（后端不暴露），模型数量/额度需按需拉取模板详情计算。

interface Props {
  // 由 App 递增以触发重新拉取（保存/清空/删除后）。
  refreshKey: number
  onCreateNew: () => void
  onEdit: (id: string) => void
  // 从模板开局：started=false 表示牌局已创建但启动失败（调用方进入该局并提示重试）。
  onEnterGame: (game: GameDTO, started: boolean) => void
}

interface ConfirmState {
  id: string
  action: 'clear' | 'delete'
}

interface Counts {
  mock: number
  real: number
  budget: number
}

export function TableList({ refreshKey, onCreateNew, onEdit, onEnterGame }: Props) {
  const [tables, setTables] = useState<TableSummaryDTO[]>([])
  const [details, setDetails] = useState<Record<string, TableDTO>>({})
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)

  const loadDetails = useCallback(async (items: TableSummaryDTO[]) => {
    const pairs = await Promise.all(
      items.map(async (s) => {
        try {
          return [s.id, await api.table(s.id)] as const
        } catch {
          return [s.id, null] as const
        }
      }),
    )
    setDetails((prev) => {
      const next = { ...prev }
      for (const [id, dto] of pairs) if (dto) next[id] = dto
      return next
    })
  }, [])

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.tables({ limit: 100 })
      setTables(list.tables)
      setNextCursor(list.next_cursor)
      setDetails({})
      await loadDetails(list.tables)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载模板列表失败，请稍后重试。')
    } finally {
      setLoading(false)
    }
  }, [loadDetails])

  useEffect(() => {
    void load()
  }, [load, refreshKey])

  async function loadMore() {
    if (!nextCursor) return
    setLoadingMore(true)
    setError(null)
    try {
      const list = await api.tables({ limit: 100, cursor: nextCursor })
      setTables((prev) => [...prev, ...list.tables])
      setNextCursor(list.next_cursor)
      await loadDetails(list.tables)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载更多失败，请稍后重试。')
    } finally {
      setLoadingMore(false)
    }
  }

  function countsOf(s: TableSummaryDTO): Counts | null {
    const dto = details[s.id]
    if (!dto || !dto.config) return null
    const aiSeats = dto.config.seats.filter((x) => x.kind !== 'human')
    const mock = aiSeats.filter((x) => x.model_ref === 'mock').length
    return { mock, real: aiSeats.length - mock, budget: dto.config.max_live_calls }
  }

  async function runClear(s: TableSummaryDTO) {
    setBusyId(s.id)
    setError(null)
    try {
      const updated = await api.clearTable(s.id, s.revision)
      setConfirm(null)
      setTables((prev) => prev.map((t) => (t.id === updated.id ? toSummary(updated) : t)))
      setDetails((prev) => ({ ...prev, [updated.id]: updated }))
    } catch (err) {
      setError(actionError(err, '清空配置失败'))
    } finally {
      setBusyId(null)
    }
  }

  async function runDelete(s: TableSummaryDTO) {
    setBusyId(s.id)
    setError(null)
    try {
      await api.deleteTable(s.id, s.revision)
      setConfirm(null)
      setTables((prev) => prev.filter((t) => t.id !== s.id))
    } catch (err) {
      setError(actionError(err, '删除模板失败'))
    } finally {
      setBusyId(null)
    }
  }

  async function openGame(s: TableSummaryDTO) {
    setBusyId(s.id)
    setError(null)
    try {
      // 第一步：从模板创建牌局（服务端使用该模板当前版本的快照，不接受座位/盲注覆盖）。
      const game = await api.openGameFromTable(s.id, s.revision)
      // 第二步：启动。失败时保留已创建的 game_id，由调用方进入该局允许重试 start，不重新创建。
      try {
        const started = await api.command(game.id, 'start')
        onEnterGame(started, true)
      } catch {
        onEnterGame(game, false)
      }
    } catch (err) {
      const message = actionError(err, '从模板开局失败')
      // 版本/单活冲突后刷新列表的 revision，避免用旧 revision 重试；
      // 刷新会先清空错误，故刷新完成后再展示错误，确保提示不被覆盖。
      if (err instanceof ApiError && err.status === 409) {
        await load()
      }
      setError(message)
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div className="container">
      <div className="form-row" style={{ alignItems: 'baseline', justifyContent: 'space-between', marginBottom: 14 }}>
        <div>
          <h2 className="section-title">牌桌模板</h2>
          <p className="section-hint" style={{ marginBottom: 0 }}>
            保存的配置模板可一键开局；「未配置」模板只存名称，需先编辑填写配置。从模板开局使用当前版本快照，
            之后修改模板不影响已开始的牌局。
          </p>
        </div>
        <button type="button" className="btn btn-primary btn-sm" onClick={onCreateNew}>
          新建模板
        </button>
      </div>

      {error && (
        <div className="banner error" role="alert">
          {error}
        </div>
      )}

      {loading ? (
        <div className="splash">
          <span>
            <span className="spinner" /> 正在加载模板…
          </span>
        </div>
      ) : tables.length === 0 ? (
        <div className="panel panel-pad">
          <div className="empty">还没有牌桌模板。点「新建模板」保存一份配置，即可一键开局。</div>
        </div>
      ) : (
        <div className="table-cards">
          {tables.map((s) => {
            const counts = countsOf(s)
            const busy = busyId === s.id
            const confirming = confirm?.id === s.id ? confirm.action : null
            return (
              <div className="panel table-card" key={s.id}>
                <div className="table-card-head">
                  <div className="table-card-name" title={s.name}>
                    {s.name}
                  </div>
                  {s.configured ? (
                    <span className="chip-badge">rev {s.revision}</span>
                  ) : (
                    <span className="chip-badge unconfigured">未配置</span>
                  )}
                </div>

                {s.configured ? (
                  <dl className="table-card-meta">
                    <div>
                      <dt>人数</dt>
                      <dd>{s.seat_count}</dd>
                    </div>
                    <div>
                      <dt>盲注</dt>
                      <dd className="mono">
                        {chips(s.small_blind)}/{chips(s.big_blind)}
                      </dd>
                    </div>
                    <div>
                      <dt>真人位置</dt>
                      <dd>{s.human_seat === null ? '无（全 AI）' : `座位 ${s.human_seat}`}</dd>
                    </div>
                    <div>
                      <dt>模型</dt>
                      <dd>
                        {counts ? `${counts.real} 真实 / ${counts.mock} mock` : '—'}
                      </dd>
                    </div>
                    <div>
                      <dt>live 额度</dt>
                      <dd className="mono">{counts ? counts.budget : '—'}</dd>
                    </div>
                  </dl>
                ) : (
                  <p className="table-card-empty">该模板未配置座位与盲注，先编辑填写后再开局。</p>
                )}

                <div className="table-card-actions">
                  <button
                    type="button"
                    className="btn btn-primary btn-sm"
                    disabled={!s.configured || busy}
                    title={s.configured ? undefined : '未配置模板不能开局'}
                    onClick={() => void openGame(s)}
                  >
                    {busy && confirm === null ? '处理中…' : '开一局'}
                  </button>
                  <button type="button" className="btn btn-sm" disabled={busy} onClick={() => onEdit(s.id)}>
                    编辑
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm"
                    disabled={busy || confirming === 'clear'}
                    onClick={() => setConfirm({ id: s.id, action: 'clear' })}
                  >
                    清空配置
                  </button>
                  <button
                    type="button"
                    className="btn btn-danger btn-sm"
                    disabled={busy || confirming === 'delete'}
                    onClick={() => setConfirm({ id: s.id, action: 'delete' })}
                  >
                    删除
                  </button>
                </div>

                {confirming === 'clear' && (
                  <div className="confirm-bar" role="alertdialog" aria-label="确认清空配置">
                    <span className="confirm-text">
                      清空配置后该模板变为「未配置」空态，名称保留，之后需重新编辑才能开局。
                    </span>
                    <button
                      type="button"
                      className="btn btn-sm"
                      disabled={busy}
                      onClick={() => void runClear(s)}
                    >
                      确认清空
                    </button>
                    <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={() => setConfirm(null)}>
                      取消
                    </button>
                  </div>
                )}

                {confirming === 'delete' && (
                  <div className="confirm-bar danger" role="alertdialog" aria-label="确认删除模板">
                    <span className="confirm-text">
                      删除模板后不可恢复；已从该模板开始的牌局不受影响。
                    </span>
                    <button
                      type="button"
                      className="btn btn-danger btn-sm"
                      disabled={busy}
                      onClick={() => void runDelete(s)}
                    >
                      确认删除
                    </button>
                    <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={() => setConfirm(null)}>
                      取消
                    </button>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {nextCursor && !loading && (
        <div className="form-row" style={{ marginTop: 16, justifyContent: 'center' }}>
          <button type="button" className="btn btn-sm" disabled={loadingMore} onClick={() => void loadMore()}>
            {loadingMore ? '加载中…' : '加载更多'}
          </button>
        </div>
      )}
    </div>
  )
}

function actionError(err: unknown, prefix: string): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return `${prefix}：模板或牌局状态已变化（版本冲突）。请刷新后重试。`
    return err.message
  }
  return `${prefix}，请稍后重试。`
}

// 清空返回的完整 TableDTO 投影为列表摘要（config 已为 null）。
function toSummary(t: TableDTO): TableSummaryDTO {
  const cfg = t.config
  return {
    id: t.id,
    name: t.name,
    revision: t.revision,
    configured: cfg !== null,
    seat_count: cfg ? cfg.seats.length : 0,
    small_blind: cfg ? cfg.small_blind : 0,
    big_blind: cfg ? cfg.big_blind : 0,
    human_seat: cfg ? cfg.seats.find((s) => s.kind === 'human')?.seat ?? null : null,
    created_at: t.created_at,
    updated_at: t.updated_at,
  }
}
