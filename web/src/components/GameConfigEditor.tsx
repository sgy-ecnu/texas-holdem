import { useState, type FormEvent, type ReactNode } from 'react'
import { AVAILABILITY_LABEL, chips } from '../format'
import type {
  HealthDTO,
  Mode,
  ModelDTO,
  SeatKind,
  TableConfigDTO,
  TableConfigInput,
  TableSeatInput,
} from '../types'

// 共享牌桌配置编辑器：只负责表单与联动，不发起任何 API 请求。
// 「快速开局」提交到 /games；「模板编辑」提交到 /tables。二者复用同一份表单，
// 不复制第二份，也不通过大量布尔参数把页面职责塞回来。

interface SeatDraft {
  seat: number
  name: string
  stack: string
  modelRef: string
}

interface EditorState {
  seats: SeatDraft[]
  humanSeat: number | null
  button: number
  smallBlind: string
  bigBlind: string
  maxHands: string
  mode: Mode
  maxLiveCalls: string
}

export interface GameConfigEditorProps {
  title: string
  hint?: ReactNode
  models: ModelDTO[]
  health: HealthDTO | null
  // null 表示「未配置」空态：空座位、空盲注，并提示「填入默认配置」。
  initial: TableConfigDTO | null
  submitLabel: string
  submitting: boolean
  // 来自父级的 API 错误（校验错误由编辑器内部管理并一起展示）。
  remoteError: string | null
  onSubmit: (config: TableConfigInput) => void
  onCancel?: () => void
  // 表单右上角附加操作（如「检查进行中的牌局」「返回」）。
  toolbar?: ReactNode
}

const DEFAULT_STACK = '10000'
// 保留引用：不可注册为真实模型，也不需要模型目录；「真实调用」判定只认它以外的引用。
const MOCK_REF = 'mock'

// 新增 AI 座位默认 mock：即使随后切到 live 也不会默认产生真实模型费用。
function makeDraft(index: number): SeatDraft {
  return {
    seat: index,
    name: `AI-${index + 1}`,
    stack: DEFAULT_STACK,
    modelRef: MOCK_REF,
  }
}

// 空态：无座位、盲注为空，等待用户点「填入默认配置」。
function emptyState(): EditorState {
  return {
    seats: [],
    humanSeat: null,
    button: 0,
    smallBlind: '',
    bigBlind: '',
    maxHands: '',
    mode: 'mock',
    maxLiveCalls: '100',
  }
}

// 默认完整配置：2 个 mock 座位 + 常规盲注。
function defaultState(): EditorState {
  return {
    seats: [makeDraft(0), makeDraft(1)],
    humanSeat: null,
    button: 0,
    smallBlind: '50',
    bigBlind: '100',
    maxHands: '100',
    mode: 'mock',
    maxLiveCalls: '100',
  }
}

// 从模板配置初始化编辑器；真人座位不绑定模型，引用回填为 mock 占位。
function stateFromConfig(config: TableConfigDTO): EditorState {
  const human = config.seats.find((s) => s.kind === 'human')?.seat ?? null
  return {
    seats: config.seats.map((s) => ({
      seat: s.seat,
      name: s.name,
      stack: String(s.stack),
      modelRef: s.kind === 'human' ? MOCK_REF : s.model_ref || MOCK_REF,
    })),
    humanSeat: human,
    button: config.button,
    smallBlind: String(config.small_blind),
    bigBlind: String(config.big_blind),
    maxHands: String(config.max_hands),
    mode: config.mode,
    maxLiveCalls: String(config.max_live_calls),
  }
}

// 非 mock 引用在 live 模式下不可用的原因；返回 null 表示可作为真实模型。
function unavailableReason(ref: string, models: ModelDTO[]): string | null {
  if (!ref || ref === MOCK_REF) return null
  const m = models.find((x) => x.ref === ref)
  if (!m) return '模型不存在或已从目录移除，请重新选择。'
  if (m.provider === 'mock') return '该引用被保留为 Mock，不能作为真实模型。'
  if (m.availability === 'unsupported') return '该模型不受支持，请重新选择。'
  if (m.availability === 'unverified') return '该模型尚未配置完成，无法调用。'
  if (m.availability === 'unavailable') return '该模型当前不可用，请稍后重试或选择其他模型。'
  return null
}

// 模型选择：
//   - locked（mock 模式）：固定显示「Mock（免费）」，不可编辑，提交 model_ref="mock"；
//   - live 模式：可选手 mock 或目录中的真实模型；不存在/unsupported 等显示原因并禁止提交，
//     绝不静默换模型。
function ModelPicker({
  value,
  models,
  locked,
  onChange,
}: {
  value: string
  models: ModelDTO[]
  locked: boolean
  onChange: (value: string) => void
}) {
  if (locked) {
    return (
      <div className="field f-model">
        <label>模型</label>
        <div className="human-badge" aria-label="模型">Mock（免费）</div>
      </div>
    )
  }
  const known = models.some((m) => m.ref === value)
  const useCustom = !known
  const blocked = unavailableReason(value, models)
  return (
    <div className="field f-model">
      <label>模型</label>
      {models.length > 0 && (
        <select
          className="select"
          aria-label="模型"
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
          <option value={MOCK_REF}>Mock（免费）</option>
          {models
            .filter((m) => m.ref !== MOCK_REF)
            .map((m) => (
              <option key={m.ref} value={m.ref} disabled={m.availability === 'unsupported'}>
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
      {blocked && <div className="inline-note warn">{blocked}</div>}
    </div>
  )
}

export function GameConfigEditor({
  title,
  hint,
  models,
  health,
  initial,
  submitLabel,
  submitting,
  remoteError,
  onSubmit,
  onCancel,
  toolbar,
}: GameConfigEditorProps) {
  const [state, setState] = useState<EditorState>(() =>
    initial ? stateFromConfig(initial) : emptyState(),
  )
  const [localError, setLocalError] = useState<string | null>(null)

  const { seats, humanSeat, button, smallBlind, bigBlind, maxHands, mode, maxLiveCalls } = state
  const canLive = health?.allow_paid_models === true
  const error = localError ?? remoteError

  function patch(next: Partial<EditorState>) {
    setState((prev) => ({ ...prev, ...next }))
  }

  function updateSeat(index: number, p: Partial<SeatDraft>) {
    setState((prev) => ({
      ...prev,
      seats: prev.seats.map((s, i) => (i === index ? { ...s, ...p } : s)),
    }))
  }

  function changeCount(next: number) {
    setState((prev) => {
      const out = prev.seats.slice(0, next)
      while (out.length < next) out.push(makeDraft(out.length))
      const human = prev.humanSeat !== null && prev.humanSeat >= next ? null : prev.humanSeat
      return {
        ...prev,
        seats: out,
        button: prev.button >= next ? 0 : prev.button,
        humanSeat: human,
      }
    })
  }

  function fillDefaults() {
    setState(defaultState())
    setLocalError(null)
  }

  // 是否确实配置了真实模型座位（决定是否需要费用确认与 live 额度语义）。
  const hasRealModels =
    mode === 'live' && seats.some((s) => s.seat !== humanSeat && s.modelRef.trim() !== MOCK_REF)

  function validate(): string | null {
    if (seats.length < 2 || seats.length > 9) {
      return seats.length === 0
        ? '该模板尚未配置：请先点「填入默认配置」或选择座位数。'
        : '座位数需在 2 到 9 之间。'
    }
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
      if (!canLive) return '服务未允许真实模型，无法使用 live 配置。'
      const mlc = Number(maxLiveCalls)
      if (!Number.isInteger(mlc) || mlc <= 0) return 'live 额度需为正整数。'
      if (health && mlc > health.max_live_calls) return `live 额度不能超过服务上限 ${health.max_live_calls}。`
      for (const s of seats) {
        // 真人座位不参与模型校验：真人隐藏的 modelRef 不得误拒绝配置。
        if (s.seat === humanSeat) continue
        const ref = s.modelRef.trim()
        if (ref === MOCK_REF) continue
        const reason = unavailableReason(ref, models)
        if (reason) return `座位 ${s.seat}：${reason}`
      }
      // 费用确认只针对确实配置了真实模型的情况；全 mock 的 live 配置不产生真实调用。
      if (hasRealModels && !liveConfirmed) return '本配置包含真实模型，请先勾选确认会消耗付费额度。'
    }
    return null
  }

  const [liveConfirmed, setLiveConfirmed] = useState(false)

  function submit(event: FormEvent) {
    event.preventDefault()
    const message = validate()
    if (message) {
      setLocalError(message)
      return
    }
    setLocalError(null)
    onSubmit({
      seats: seats.map((s): TableSeatInput => {
        const kind: SeatKind = s.seat === humanSeat ? 'human' : 'ai'
        return kind === 'human'
          ? { seat: s.seat, name: s.name.trim(), stack: Number(s.stack), kind }
          : {
              seat: s.seat,
              name: s.name.trim(),
              stack: Number(s.stack),
              kind,
              model_ref: mode === 'mock' ? MOCK_REF : s.modelRef.trim(),
            }
      }),
      button,
      small_blind: Number(smallBlind),
      big_blind: Number(bigBlind),
      max_hands: Number(maxHands),
      mode,
      max_live_calls: mode === 'live' ? Number(maxLiveCalls) : 0,
    })
  }

  const totalStack = seats.reduce((sum, s) => sum + (Number(s.stack) || 0), 0)
  const configured = seats.length > 0

  return (
    <form className="panel panel-pad config-editor" onSubmit={submit} style={{ marginBottom: 18 }}>
      <div className="form-row" style={{ alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h2 className="section-title">{title}</h2>
          {hint && <p className="section-hint">{hint}</p>}
        </div>
        {toolbar}
      </div>

      {!configured && (
        <div className="empty-config">
          <div className="empty-config-text">
            该模板尚未配置：暂无座位与盲注。可点「填入默认配置」快速开始，或手动选择座位数。
          </div>
          <button type="button" className="btn btn-primary btn-sm" onClick={fillDefaults}>
            填入默认配置
          </button>
        </div>
      )}

      <div className="form-grid" style={{ marginBottom: 18 }}>
        <div className="field">
          <label>座位数</label>
          <select
            className="select"
            aria-label="座位数"
            value={seats.length}
            onChange={(e) => changeCount(Number(e.target.value))}
          >
            {[0, 2, 3, 4, 5, 6, 7, 8, 9].map((n) => (
              <option key={n} value={n}>
                {n === 0 ? '0 人（未配置）' : `${n} 人`}
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
            onChange={(e) => patch({ humanSeat: e.target.value === '' ? null : Number(e.target.value) })}
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
            onChange={(e) => patch({ smallBlind: e.target.value })}
          />
        </div>
        <div className="field">
          <label>大盲 BB</label>
          <input
            className="input"
            type="number"
            min={2}
            value={bigBlind}
            onChange={(e) => patch({ bigBlind: e.target.value })}
          />
        </div>
        <div className="field">
          <label>最多手数</label>
          <input
            className="input"
            type="number"
            min={1}
            value={maxHands}
            onChange={(e) => patch({ maxHands: e.target.value })}
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
                  value={mode === 'mock' ? MOCK_REF : s.modelRef}
                  models={models}
                  locked={mode === 'mock'}
                  onChange={(v) => updateSeat(i, { modelRef: v })}
                />
              )}
              <button
                type="button"
                className="icon-btn remove"
                title="移除座位"
                disabled={seats.length <= 2}
                onClick={() => {
                  setState((prev) => {
                    const filtered = prev.seats
                      .filter((_, idx) => idx !== i)
                      .map((x, idx) => ({ ...x, seat: idx }))
                    const human =
                      prev.humanSeat === null
                        ? null
                        : prev.humanSeat === i
                          ? null
                          : prev.humanSeat > i
                            ? prev.humanSeat - 1
                            : prev.humanSeat
                    return { ...prev, seats: filtered, button: 0, humanSeat: human }
                  })
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
          <select
            className="select"
            aria-label="按钮位"
            value={button}
            onChange={(e) => patch({ button: Number(e.target.value) })}
          >
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
                setLiveConfirmed(false)
                // live→mock：AI 引用全部重置为 Mock，避免保留付费选择。
                setState((prev) => ({
                  ...prev,
                  mode: 'mock',
                  seats: prev.seats.map((s) => ({ ...s, modelRef: MOCK_REF })),
                }))
              }}
            >
              Mock（免费）
            </button>
            <button
              type="button"
              aria-pressed={mode === 'live'}
              disabled={!canLive}
              title={canLive ? undefined : '服务未允许真实模型'}
              onClick={() => {
                // 切回 live 不自动恢复付费选择：新增/已重置座位保持 Mock。
                setState((prev) => {
                  const mlc =
                    health && Number(prev.maxLiveCalls) > health.max_live_calls
                      ? String(health.max_live_calls)
                      : prev.maxLiveCalls
                  return { ...prev, mode: 'live', maxLiveCalls: mlc }
                })
              }}
            >
              Live（付费）
            </button>
          </div>
        </div>
      </div>

      {mode === 'live' ? (
        <div className="panel panel-pad" style={{ background: 'rgba(224, 176, 85, 0.06)', marginBottom: 14 }}>
          {hasRealModels ? (
            <label className="checkline">
              <input
                type="checkbox"
                checked={liveConfirmed}
                onChange={(e) => setLiveConfirmed(e.target.checked)}
              />
              <span>
                我确认本配置包含真实模型座位，且每个真实模型请求（含超时/取消/失败）都会消耗一次额度；额度耗尽会暂停且本版无法追加预算。
              </span>
            </label>
          ) : (
            <div className="inline-note">本配置不产生真实模型调用（所有 AI 座位均为 Mock）。</div>
          )}
          <div className="field" style={{ marginTop: 12, maxWidth: 260 }}>
            <label>live 调用额度 max_live_calls（服务上限 {health?.max_live_calls ?? '—'}）</label>
            <input
              className="input"
              type="number"
              min={1}
              value={maxLiveCalls}
              onChange={(e) => patch({ maxLiveCalls: e.target.value })}
            />
          </div>
        </div>
      ) : (
        <div className="inline-note" style={{ marginBottom: 14 }}>
          Mock 模式：所有 AI 座位固定使用免费 Mock，不产生真实 provider 请求。
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

      <div className="form-row">
        <button className="btn btn-primary" type="submit" disabled={submitting}>
          {submitting ? '提交中…' : submitLabel}
        </button>
        {onCancel && (
          <button type="button" className="btn btn-ghost" disabled={submitting} onClick={onCancel}>
            取消
          </button>
        )}
      </div>
    </form>
  )
}
