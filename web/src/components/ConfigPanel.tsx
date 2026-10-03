import { useMemo, useState, type FormEvent } from 'react'
import { ApiError, api } from '../api'
import {
  AVAILABILITY_LABEL,
  PROVIDER_LABEL,
  chips,
} from '../format'
import type {
  CreateGameRequest,
  GameDTO,
  HealthDTO,
  Mode,
  ModelDTO,
  SeatKind,
} from '../types'

interface SeatDraft {
  seat: number
  name: string
  stack: string
  modelRef: string
}

interface Props {
  models: ModelDTO[]
  health: HealthDTO | null
  healthOk: boolean
  recovering: boolean
  onRecover: () => void
  onCreated: (game: GameDTO) => void
}

const DEFAULT_STACK = '10000'

function pickDefaultModel(models: ModelDTO[]): string {
  const usable = models.find(
    (m) => m.availability !== 'unavailable' && m.availability !== 'unsupported',
  )
  return usable?.ref ?? models[0]?.ref ?? 'mock'
}

function makeDraft(index: number, models: ModelDTO[]): SeatDraft {
  return {
    seat: index,
    name: `AI-${index + 1}`,
    stack: DEFAULT_STACK,
    modelRef: pickDefaultModel(models),
  }
}

// 模型选择：优先下拉已知模型，允许自定义引用；空目录时退回手填。
function ModelPicker({
  value,
  models,
  onChange,
}: {
  value: string
  models: ModelDTO[]
  onChange: (value: string) => void
}) {
  const known = models.some((m) => m.ref === value)
  const useCustom = !known
  return (
    <div className="field f-model">
      <label>模型</label>
      {models.length > 0 && (
        <select
          className="select"
          value={known ? value : '__custom__'}
          onChange={(e) => {
            const v = e.target.value
            if (v === '__custom__') {
              if (known) onChange('')
            } else {
              onChange(v)
            }
          }}
        >
          {models.map((m) => (
            <option key={m.ref} value={m.ref}>
              {m.label}（{AVAILABILITY_LABEL[m.availability]}）
            </option>
          ))}
          <option value="__custom__">自定义引用…</option>
        </select>
      )}
      {useCustom && (
        <input
          className="input"
          style={models.length > 0 ? { marginTop: 6 } : undefined}
          value={value}
          placeholder="provider/model"
          onChange={(e) => onChange(e.target.value)}
          aria-label="自定义模型引用"
        />
      )}
    </div>
  )
}

export function ConfigPanel({ models, health, healthOk, recovering, onRecover, onCreated }: Props) {
  const defaultModel = useMemo(() => pickDefaultModel(models), [models])
  const [seats, setSeats] = useState<SeatDraft[]>(() => [
    makeDraft(0, models),
    makeDraft(1, models),
  ])
  const [button, setButton] = useState(0)
  // 真人座位：null 表示全 AI；单值选择天然保证最多 1 个真人。见契约「二、座位契约」。
  const [humanSeat, setHumanSeat] = useState<number | null>(null)
  const [smallBlind, setSmallBlind] = useState('50')
  const [bigBlind, setBigBlind] = useState('100')
  const [maxHands, setMaxHands] = useState('100')
  const [mode, setMode] = useState<Mode>('mock')
  const [maxLiveCalls, setMaxLiveCalls] = useState('100')
  const [liveConfirmed, setLiveConfirmed] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const canLive = health?.allow_paid_models === true

  function updateSeat(index: number, patch: Partial<SeatDraft>) {
    setSeats((prev) => prev.map((s, i) => (i === index ? { ...s, ...patch } : s)))
  }

  function changeCount(next: number) {
    setSeats((prev) => {
      const out = prev.slice(0, next)
      while (out.length < next) out.push(makeDraft(out.length, models))
      return out
    })
    if (button >= next) setButton(0)
    // 真人座位被裁掉时清空选择（单值选择保证最多 1 个真人）。
    if (humanSeat !== null && humanSeat >= next) setHumanSeat(null)
  }

  function validate(): string | null {
    if (seats.length < 2 || seats.length > 9) return '座位数需在 2 到 9 之间。'
    const seen = new Set<number>()
    let total = 0
    for (const s of seats) {
      const stack = Number(s.stack)
      if (!Number.isInteger(s.seat) || s.seat < 0 || s.seat > 8) return '座位编号需为 0 到 8 的整数。'
      if (seen.has(s.seat)) return '座位编号不能重复。'
      seen.add(s.seat)
      if (!s.name.trim()) return `座位 ${s.seat} 的名称不能为空。`
      if (s.name.length > 128) return `座位 ${s.seat} 的名称过长（最多 128 字符）。`
      if (!Number.isInteger(stack) || stack < 0) return `座位 ${s.seat} 的筹码需为非负整数。`
      total += stack
      // 真人座位不要求模型；AI 座位仍需绑定模型。
      if (s.seat !== humanSeat) {
        if (!s.modelRef.trim()) return `座位 ${s.seat} 需绑定模型。`
        if (s.modelRef.length > 256) return `座位 ${s.seat} 的模型引用过长。`
      }
    }
    if (total > 1e12) return '所有座位筹码总和不能超过 10^12。'
    if (seats.filter((s) => Number(s.stack) > 0).length < 2) return '至少需要两名有筹码的玩家。'
    if (!seats.some((s) => s.seat === button && Number(s.stack) > 0)) return '按钮位必须是有筹码的座位。'

    const sb = Number(smallBlind)
    const bb = Number(bigBlind)
    const mh = Number(maxHands)
    if (!Number.isInteger(sb) || sb <= 0) return '小盲需为正整数。'
    if (!Number.isInteger(bb) || bb <= sb) return '大盲需大于小盲。'
    if (bb > 1e12) return '大盲超出上限。'
    if (!Number.isInteger(mh) || mh < 1) return '最多手数需为正整数。'

    if (mode === 'live') {
      if (!canLive) return '服务未授权付费模型，无法创建 live 牌局。'
      if (!liveConfirmed) return '请先勾选确认 live 模式会消耗付费额度。'
      const mlc = Number(maxLiveCalls)
      if (!Number.isInteger(mlc) || mlc <= 0) return 'live 额度需为正整数。'
      if (health && mlc > health.max_live_calls) return `live 额度不能超过服务上限 ${health.max_live_calls}。`
      for (const s of seats) {
        const m = models.find((x) => x.ref === s.modelRef)
        if (!m || m.provider === 'mock' || m.availability === 'unsupported') {
          return `live 模式下座位 ${s.seat} 需选择可用的真实模型。`
        }
      }
    }
    return null
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    const message = validate()
    if (message) {
      setError(message)
      return
    }
    setSubmitting(true)
    setError(null)
    const body: CreateGameRequest = {
      seats: seats.map((s) => {
        const kind: SeatKind = s.seat === humanSeat ? 'human' : 'ai'
        // 真人省略 model_ref；AI 必须携带。
        return kind === 'human'
          ? { seat: s.seat, name: s.name.trim(), stack: Number(s.stack), kind }
          : { seat: s.seat, name: s.name.trim(), stack: Number(s.stack), kind, model_ref: s.modelRef.trim() }
      }),
      button,
      small_blind: Number(smallBlind),
      big_blind: Number(bigBlind),
      max_hands: Number(maxHands),
      mode,
      max_live_calls: mode === 'live' ? Number(maxLiveCalls) : 0,
    }
    try {
      const game = await api.create(body)
      onCreated(game)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '创建牌局失败，请稍后重试。')
    } finally {
      setSubmitting(false)
    }
  }

  const totalStack = seats.reduce((sum, s) => sum + (Number(s.stack) || 0), 0)

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

      <form className="panel panel-pad" onSubmit={submit} style={{ marginBottom: 18 }}>
        <div className="form-row" style={{ alignItems: 'baseline', justifyContent: 'space-between' }}>
          <div>
            <h2 className="section-title">新建牌局</h2>
            <p className="section-hint">
              配置 AI 座位、可选 1 个真人座位、筹码与盲注；默认 mock 模式不产生任何模型费用。
              真人身份由创建浏览器持有的游戏级 Cookie 决定，其他浏览器只能观战；Cookie 丢失后无法认领。
            </p>
          </div>
          <button type="button" className="btn btn-ghost btn-sm" onClick={onRecover} disabled={recovering}>
            {recovering ? '检查中…' : '检查进行中的牌局'}
          </button>
        </div>

        <div className="form-grid" style={{ marginBottom: 18 }}>
          <div className="field">
            <label>座位数</label>
            <select
              className="select"
              value={seats.length}
              onChange={(e) => changeCount(Number(e.target.value))}
            >
              {[2, 3, 4, 5, 6, 7, 8, 9].map((n) => (
                <option key={n} value={n}>
                  {n} 人
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>真人座位</label>
            <select
              className="select"
              aria-label="真人座位"
              value={humanSeat === null ? '' : String(humanSeat)}
              onChange={(e) => setHumanSeat(e.target.value === '' ? null : Number(e.target.value))}
            >
              <option value="">无（全 AI）</option>
              {seats.map((s) => (
                <option key={s.seat} value={s.seat}>
                  座位 {s.seat} · {s.name || '未命名'}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>小盲 SB</label>
            <input
              className="input"
              type="number"
              min={1}
              value={smallBlind}
              onChange={(e) => setSmallBlind(e.target.value)}
            />
          </div>
          <div className="field">
            <label>大盲 BB</label>
            <input
              className="input"
              type="number"
              min={2}
              value={bigBlind}
              onChange={(e) => setBigBlind(e.target.value)}
            />
          </div>
          <div className="field">
            <label>最多手数</label>
            <input
              className="input"
              type="number"
              min={1}
              value={maxHands}
              onChange={(e) => setMaxHands(e.target.value)}
            />
          </div>
        </div>

        <div className="seat-editor" style={{ marginBottom: 18 }}>
          {seats.map((s, i) => {
            const isHuman = s.seat === humanSeat
            return (
              <div className={`seat-row${isHuman ? ' is-human' : ''}`} key={s.seat} style={{ animationDelay: `${i * 40}ms` }}>
                <div className="seat-badge">{s.seat}</div>
                <div className="field f-name">
                  <label>名称</label>
                  <input
                    className="input"
                    value={s.name}
                    maxLength={128}
                    onChange={(e) => updateSeat(i, { name: e.target.value })}
                  />
                </div>
                <div className="field f-stack">
                  <label>筹码</label>
                  <input
                    className="input"
                    type="number"
                    min={0}
                    value={s.stack}
                    onChange={(e) => updateSeat(i, { stack: e.target.value })}
                  />
                </div>
                {isHuman ? (
                  <div className="field f-model">
                    <label>身份</label>
                    <div className="human-badge">真人 · 创建浏览器持有权限，无需模型</div>
                  </div>
                ) : (
                  <ModelPicker
                    value={s.modelRef}
                    models={models}
                    onChange={(v) => updateSeat(i, { modelRef: v })}
                  />
                )}
                <button
                  type="button"
                  className="icon-btn remove"
                  title="移除座位"
                  disabled={seats.length <= 2}
                  onClick={() => {
                    setSeats((prev) => prev.filter((_, idx) => idx !== i).map((x, idx) => ({ ...x, seat: idx })))
                    setButton(0)
                    // 删除座位导致重排：同步修正真人座位号，避免指向错误座位。
                    setHumanSeat((h) => (h === null ? null : h === i ? null : h > i ? h - 1 : h))
                  }}
                >
                  ×
                </button>
              </div>
            )
          })}
        </div>

        <div className="form-grid" style={{ marginBottom: 14 }}>
          <div className="field">
            <label>按钮位（Button）</label>
            <select className="select" value={button} onChange={(e) => setButton(Number(e.target.value))}>
              {seats.map((s) => (
                <option key={s.seat} value={s.seat}>
                  座位 {s.seat} · {s.name || '未命名'}
                  {Number(s.stack) <= 0 ? '（无筹码）' : ''}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>运行模式</label>
            <div className="segmented">
              <button
                type="button"
                aria-pressed={mode === 'mock'}
                onClick={() => {
                  setMode('mock')
                  setLiveConfirmed(false)
                }}
              >
                Mock（免费）
              </button>
              <button
                type="button"
                aria-pressed={mode === 'live'}
                disabled={!canLive}
                title={canLive ? undefined : '服务未授权付费模型'}
                onClick={() => {
                  setMode('live')
                  if (health && Number(maxLiveCalls) > health.max_live_calls) {
                    setMaxLiveCalls(String(health.max_live_calls))
                  }
                }}
              >
                Live（付费）
              </button>
            </div>
          </div>
        </div>

        {mode === 'live' ? (
          <div className="panel panel-pad" style={{ background: 'rgba(224, 176, 85, 0.06)', marginBottom: 14 }}>
            <label className="checkline">
              <input
                type="checkbox"
                checked={liveConfirmed}
                onChange={(e) => setLiveConfirmed(e.target.checked)}
              />
              <span>
                我确认 live 模式会调用真实付费模型，且每次请求（含超时/取消/失败）都会消耗一次额度；额度耗尽会暂停且本版无法追加预算。
              </span>
            </label>
            <div className="field" style={{ marginTop: 12, maxWidth: 260 }}>
              <label>live 调用额度 max_live_calls（服务上限 {health?.max_live_calls ?? '—'}）</label>
              <input
                className="input"
                type="number"
                min={1}
                value={maxLiveCalls}
                onChange={(e) => setMaxLiveCalls(e.target.value)}
              />
            </div>
          </div>
        ) : (
          <div className="inline-note" style={{ marginBottom: 14 }}>
            Mock 模式：仅保存 model_ref 用于观战展示，不会产生真实 provider 请求。
            {!canLive && <span className="warn">服务未授权付费模型（allow_paid_models=false），live 不可用。</span>}
          </div>
        )}

        <div className="summary" style={{ marginBottom: 16 }}>
          <span>
            总筹码 <strong>{chips(totalStack)}</strong>
          </span>
          <span>
            有筹码玩家 <strong>{seats.filter((s) => Number(s.stack) > 0).length}</strong>
          </span>
          <span>
            模式 <strong>{mode === 'mock' ? 'Mock' : 'Live'}</strong>
          </span>
        </div>

        {error && <div className="banner error" role="alert">{error}</div>}

        <button className="btn btn-primary" type="submit" disabled={submitting}>
          {submitting ? '创建中…' : '创建牌局'}
        </button>
      </form>

      <div className="panel panel-pad">
        <h2 className="section-title">模型目录</h2>
        <p className="section-hint">
          仅展示安全字段（引用 / 标签 / 服务商 / 可用性），不含 endpoint 与凭据。
        </p>
        {models.length === 0 ? (
          <div className="empty">当前未发现可用模型；mock 模式仍可运行，model_ref 可自定义填写。</div>
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
          <b>默认模型</b>
          <span className="mono">{defaultModel}</span>
        </div>
      </div>
    </div>
  )
}
