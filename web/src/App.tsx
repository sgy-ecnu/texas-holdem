import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, api } from './api'
import { ConfigPanel } from './components/ConfigPanel'
import { SpectatorView } from './components/SpectatorView'
import { TableEditor } from './components/TableEditor'
import { TableList } from './components/TableList'
import type { GameDTO, HealthDTO, ModelDTO, TableDTO } from './types'

// 页面导航与初始化状态分离：初始化只加载 health/models/current；默认进入牌桌列表。
// 不引入完整路由框架，用 page 状态切换；离开观战页不等于 stop。
type Page = 'tables' | 'editor' | 'quick' | 'play'

export default function App() {
  const [page, setPage] = useState<Page>('tables')
  const [game, setGame] = useState<GameDTO | null>(null)
  const [models, setModels] = useState<ModelDTO[]>([])
  const [health, setHealth] = useState<HealthDTO | null>(null)
  const [healthOk, setHealthOk] = useState(true)
  const [notice, setNotice] = useState<string | null>(null)
  const [recovering, setRecovering] = useState(false)
  // 模板编辑器当前编辑的 id：null 新建；非 null 编辑。
  const [editingId, setEditingId] = useState<string | null>(null)
  // 从模板开局第二步（start）失败时进入 play 的提示；成功则为 null。
  const [playNotice, setPlayNotice] = useState<string | null>(null)
  // 递增触发 TableList 重新拉取（保存/清空/删除后）。
  const [tablesVersion, setTablesVersion] = useState(0)
  const [booting, setBooting] = useState(true)
  const started = useRef(false)

  const bootstrap = useCallback(async () => {
    setNotice(null)
    const [h, m, c] = await Promise.allSettled([api.health(), api.models(), api.current()])

    if (h.status === 'fulfilled') {
      setHealth(h.value.health)
      setHealthOk(h.value.ok)
    } else {
      setHealth(null)
      setHealthOk(false)
    }

    if (m.status === 'fulfilled') {
      setModels(m.value.models)
    } else {
      setModels([])
      setNotice('模型目录加载失败，暂时只能使用自定义 model_ref。')
    }

    // 读取当前牌局仅用于展示「返回当前牌局」入口，不再强制跳转。
    if (c.status === 'fulfilled') {
      setGame(c.value)
    } else {
      setGame(null)
      if (c.reason instanceof ApiError && c.reason.status === 503) {
        setNotice('存储不可用，未能读取当前牌局。')
      }
    }
    setBooting(false)
  }, [])

  useEffect(() => {
    // 开发环境 StrictMode 会重复挂载，这里避免重复请求。
    if (started.current) return
    started.current = true
    void bootstrap()
  }, [bootstrap])

  const recover = useCallback(async () => {
    setRecovering(true)
    setNotice(null)
    try {
      const current = await api.current()
      setGame(current)
      setPlayNotice(null)
      setPage('play')
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setNotice('未找到进行中的牌局，可直接创建新牌局。')
      } else if (err instanceof ApiError) {
        setNotice(err.message)
      } else {
        setNotice('检查进行中的牌局失败，请稍后重试。')
      }
    } finally {
      setRecovering(false)
    }
  }, [])

  const goTables = useCallback(() => {
    setNotice(null)
    setPage('tables')
  }, [])

  const goQuick = useCallback(() => {
    setNotice(null)
    setPage('quick')
  }, [])

  const openEditor = useCallback((id: string | null) => {
    setEditingId(id)
    setNotice(null)
    setPage('editor')
  }, [])

  // 进入观战：notice 为「已创建、启动失败」等提示（成功则为 null）。
  const enterGame = useCallback((next: GameDTO, startedOk: boolean) => {
    setGame(next)
    setPlayNotice(
      startedOk
        ? null
        : '牌局已创建，但启动失败：可在本页点「开始」重试，不会重新创建牌局。',
    )
    setPage('play')
  }, [])

  const returnToCurrent = useCallback(() => {
    if (game) {
      setPlayNotice(null)
      setPage('play')
    }
  }, [game])

  const onTemplateSaved = useCallback((table: TableDTO) => {
    setGame((prev) => prev)
    setTablesVersion((n) => n + 1)
    setNotice(`模板「${table.name}」已保存。`)
    setPage('tables')
  }, [])

  const showCurrentEntry = game !== null && page !== 'play'

  return (
    <div className="app">
      <header className="masthead">
        <h1 className="brand">
          德州扑克<em>·</em>观战台
        </h1>
        <span className="tagline">MVP · 模板 / 观战 / 真人上桌（本机单桌）</span>
        <nav className="topnav">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            aria-current={page === 'tables'}
            onClick={goTables}
          >
            牌桌列表
          </button>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            aria-current={page === 'quick'}
            onClick={goQuick}
          >
            快速开局
          </button>
          {showCurrentEntry && (
            <button type="button" className="btn btn-primary btn-sm" onClick={returnToCurrent}>
              返回当前牌局
            </button>
          )}
        </nav>
        <span className="spacer" />
        <span className="health">
          {health ? (
            <>
              <span>MySQL {health.mysql === 'available' ? '正常' : health.mysql}</span>
              <span>·</span>
              <span>Redis {health.redis === 'available' ? '正常' : health.redis}</span>
              <span>·</span>
              <span>付费模型 {health.allow_paid_models ? '已授权' : '未授权'}</span>
            </>
          ) : (
            <span>存储状态未知</span>
          )}
        </span>
      </header>

      {notice && (
        <div className="container">
          <div className="banner info">{notice}</div>
        </div>
      )}

      {booting && page === 'tables' ? (
        <div className="splash">
          <span>
            <span className="spinner" /> 正在连接服务…
          </span>
        </div>
      ) : (
        <>
          {page === 'tables' && (
            <TableList
              refreshKey={tablesVersion}
              onCreateNew={() => openEditor(null)}
              onEdit={(id) => openEditor(id)}
              onEnterGame={enterGame}
            />
          )}

          {page === 'editor' && (
            <TableEditor
              tableId={editingId}
              models={models}
              health={health}
              healthOk={healthOk}
              onSaved={onTemplateSaved}
              onCancel={goTables}
            />
          )}

          {page === 'quick' && (
            <ConfigPanel
              models={models}
              health={health}
              healthOk={healthOk}
              recovering={recovering}
              onRecover={recover}
              onCreated={(created) => {
                // 快速开局只创建，不自动 start；用户在当前牌局页手动开始。
                enterGame(created, true)
              }}
            />
          )}

          {page === 'play' && game && (
            <SpectatorView
              key={game.id}
              initial={game}
              models={models}
              health={health}
              notice={playNotice}
              onExit={goTables}
            />
          )}

          {page === 'play' && !game && (
            <div className="container">
              <div className="empty">当前没有可展示的牌局。</div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
