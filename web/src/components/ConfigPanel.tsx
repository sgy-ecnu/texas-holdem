import { useState } from 'react'
import { ApiError, api } from '../api'
import { AVAILABILITY_LABEL, PROVIDER_LABEL } from '../format'
import type { GameDTO, HealthDTO, ModelDTO, TableConfigInput } from '../types'
import { GameConfigEditor } from './GameConfigEditor'

// 快速开局页：保留原一次性配置入口，表单复用共享 GameConfigEditor，提交到 /games。
// 不写模板；需要复用配置时改用「牌桌列表」中的模板。

interface Props {
  models: ModelDTO[]
  health: HealthDTO | null
  healthOk: boolean
  recovering: boolean
  onRecover: () => void
  onCreated: (game: GameDTO) => void
}

export function ConfigPanel({ models, health, healthOk, recovering, onRecover, onCreated }: Props) {
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(config: TableConfigInput) {
    setSubmitting(true)
    setError(null)
    try {
      // CreateGameRequest 与 TableConfigInput 结构同构；此处直接提交编辑器产出的完整配置。
      const game = await api.create(config)
      onCreated(game)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '创建牌局失败，请稍后重试。')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="container">
      {!healthOk && (
        <div className="banner warn">
          <span>
            <span className="banner-title">存储状态异常：</span>
            后端报告存储降级或不可用，牌局可能无法创建或读取。{health ? `（MySQL：${health.mysql} / Redis：${health.redis}）` : ''}
          </span>
        </div>
      )}

      <GameConfigEditor
        title="新建牌局"
        hint={
          <>
            配置 AI 座位、可选 1 个真人座位、筹码与盲注；默认 mock 模式不产生任何模型费用。
            真人身份由创建浏览器持有的游戏级 Cookie 决定，其他浏览器只能观战；Cookie 丢失后无法认领。
          </>
        }
        models={models}
        health={health}
        initial={null}
        submitLabel="创建牌局"
        submitting={submitting}
        remoteError={error}
        onSubmit={submit}
        toolbar={
          <button type="button" className="btn btn-ghost btn-sm" onClick={onRecover} disabled={recovering}>
            {recovering ? '检查中…' : '检查进行中的牌局'}
          </button>
        }
      />

      <div className="panel panel-pad">
        <h2 className="section-title">模型目录</h2>
        <p className="section-hint">
          仅展示安全字段（引用 / 标签 / 服务商 / 可用性），不含 endpoint 与凭据。
        </p>
        {models.length === 0 ? (
          <div className="empty">当前未发现可用模型；mock 模式固定使用免费 Mock，live 模式仅可选择目录中的真实模型。</div>
        ) : (
          <div className="models-grid">
            {models.map((m) => (
              <div className="model-card" key={m.ref}>
                <div className="ref" title={m.ref}>
                  {m.ref}
                </div>
                <div className="meta">
                  <span className={`avail ${m.availability}`}>{AVAILABILITY_LABEL[m.availability]}</span>
                  <span>{PROVIDER_LABEL[m.provider]}</span>
                  {m.error_code && <span className="muted">{m.error_code}</span>}
                </div>
              </div>
            ))}
          </div>
        )}
        <div className="kv" style={{ marginTop: 14 }}>
          <b>新建座位默认模型</b>
          <span className="mono">Mock（免费）</span>
        </div>
      </div>
    </div>
  )
}
