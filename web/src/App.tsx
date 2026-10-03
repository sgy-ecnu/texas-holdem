import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, api } from './api'
import { ConfigPanel } from './components/ConfigPanel'
import { SpectatorView } from './components/SpectatorView'
import type { GameDTO, HealthDTO, ModelDTO } from './types'

type Phase = 'loading' | 'config' | 'play'

export default function App() {
  const [phase, setPhase] = useState<Phase>('loading')
  const [game, setGame] = useState<GameDTO | null>(null)
  const [models, setModels] = useState<ModelDTO[]>([])
  const [health, setHealth] = useState<HealthDTO | null>(null)
  const [healthOk, setHealthOk] = useState(true)
  const [notice, setNotice] = useState<string | null>(null)
  const [recovering, setRecovering] = useState(false)
  const started = useRef(false)

  const bootstrap = useCallback(async () => {
    setPhase('loading')
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

    if (c.status === 'fulfilled') {
      setGame(c.value)
      setPhase('play')
    } else {
      setGame(null)
      setPhase('config')
      if (c.reason instanceof ApiError && c.reason.status === 503) {
        setNotice('存储不可用，未能读取当前牌局。')
      }
    }
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
      setPhase('play')
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

  const exitToConfig = useCallback(() => {
    setPhase('config')
    setGame(null)
    void bootstrap()
  }, [bootstrap])

  return (
    <div className="app">
      <header className="masthead">
        <h1 className="brand">
          德州扑克<em>·</em>观战台
        </h1>
        <span className="tagline">MVP · 观战 / 真人上桌（本机单桌）</span>
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

      {phase === 'loading' && (
        <div className="splash">
          <span>
            <span className="spinner" /> 正在连接服务…
          </span>
        </div>
      )}

      {phase === 'config' && (
        <ConfigPanel
          models={models}
          health={health}
          healthOk={healthOk}
          recovering={recovering}
          onRecover={recover}
          onCreated={(created) => {
            setGame(created)
            setPhase('play')
          }}
        />
      )}

      {phase === 'play' && game && (
        <SpectatorView initial={game} models={models} health={health} onExit={exitToConfig} />
      )}
    </div>
  )
}
