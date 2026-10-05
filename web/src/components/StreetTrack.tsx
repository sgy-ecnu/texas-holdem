import { useEffect, useRef, useState } from 'react'
import { chips, STREET_LABEL } from '../format'
import type { Street } from '../types'

// 街进度指示器：五节点轨道（翻牌前→翻牌→转牌→河牌→结束）+ 状态行。
// 放在原街名位置（公共牌→底池→轨道→状态行的纵向顺序），不遮挡公共牌/底池。

const BETTING_STREETS: Street[] = ['preflop', 'flop', 'turn', 'river']
const ALL_NODES: Array<{ key: Street; label: string }> = [
  { key: 'preflop', label: STREET_LABEL.preflop },
  { key: 'flop', label: STREET_LABEL.flop },
  { key: 'turn', label: STREET_LABEL.turn },
  { key: 'river', label: STREET_LABEL.river },
  { key: 'complete', label: '结束' },
]

type NodeState = 'done' | 'current' | 'future' | 'skipped' | 'ended'
type TerminalFail = boolean

interface Props {
  street: Street | null
  boardLength: number
  currentBet: number
  status: string
  // 仅当能拿到该手 CompletedHandDTO.reason=="showdown" 时，第五节点才显示「摊牌」。
  completedReason?: string | null
  handNumber?: number | null
}

// 停止/失败不是正常结束，绝不把中止画成「本手已结束」。
function isTerminalFail(status: string): boolean {
  return status === 'stopped' || status === 'failed'
}

// 由 board 长度反推本手实际进行到的最后一条下注街：0→翻牌前、3→翻牌、4→转牌、5→河牌。
function reachedIndex(boardLength: number): number {
  if (boardLength >= 5) return 3
  if (boardLength >= 4) return 2
  if (boardLength >= 3) return 1
  return 0
}

function nodeStates(
  street: Street | null,
  boardLength: number,
  terminalFail: TerminalFail,
): NodeState[] {
  const states: NodeState[] = ['future', 'future', 'future', 'future', 'future']
  if (street === null) return states

  if (street === 'complete') {
    const reached = reachedIndex(boardLength)
    for (let i = 0; i <= reached; i++) states[i] = 'done'
    for (let i = reached + 1; i < 4; i++) states[i] = 'skipped'
    states[4] = terminalFail ? 'future' : 'ended'
    return states
  }

  const current = BETTING_STREETS.indexOf(street)
  if (current < 0) return states
  for (let i = 0; i < current; i++) states[i] = 'done'
  states[current] = 'current'
  return states
}

export function StreetTrack({ street, boardLength, currentBet, status, completedReason, handNumber }: Props) {
  const terminalFail = isTerminalFail(status)
  const states = nodeStates(street, boardLength, terminalFail)
  const ended = street === 'complete' && !terminalFail

  // 切街提示：仅同一手、且恰好前进一条下注街时临时显示 1.5s；跳街直接显示最新，不伪造。
  const prevRef = useRef<{ handNo: number | null; street: Street | null }>({ handNo: null, street: null })
  const [transition, setTransition] = useState<{ from: Street; to: Street } | null>(null)
  useEffect(() => {
    const prev = prevRef.current
    prevRef.current = { handNo: handNumber ?? null, street: street ?? null }
    if (handNumber == null || street == null || prev.handNo !== handNumber || prev.street == null) {
      setTransition(null)
      return
    }
    const from = prev.street
    const to = street
    const fi = BETTING_STREETS.indexOf(from)
    const ti = BETTING_STREETS.indexOf(to)
    if (fi >= 0 && ti === fi + 1) {
      setTransition({ from, to })
      const t = window.setTimeout(() => setTransition(null), 1500)
      return () => window.clearTimeout(t)
    }
    setTransition(null)
  }, [handNumber, street])

  const fifthLabel = ended && completedReason === 'showdown' ? '摊牌' : ALL_NODES[4].label

  let statusLine: string
  if (terminalFail) {
    statusLine = status === 'failed' ? '牌局失败' : '牌局已停止'
  } else if (status === 'paused') {
    statusLine = '牌局已暂停 · 本街尚未结束'
  } else if (street === null) {
    statusLine = '等待开始'
  } else if (street === 'complete') {
    statusLine = '本手已结束'
  } else if (transition) {
    statusLine = `${STREET_LABEL[transition.from]}下注已结束 · ${STREET_LABEL[transition.to]}下注进行中`
  } else if (currentBet > 0) {
    statusLine = `下注进行中 · 本街当前注 ${chips(currentBet)}`
  } else {
    statusLine = '下注进行中 · 本街尚无人下注'
  }

  return (
    <div className="street-wrap">
      <div className="street-track" aria-hidden="true">
        {ALL_NODES.map((node, i) => {
          const state = states[i]
          const label = i === 4 ? fifthLabel : node.label
          return (
            <div className={`street-node is-${node.key} is-${state}`} data-node={node.key} data-state={state} key={node.key}>
              <span className="node-dot">{state === 'done' ? '✓' : state === 'skipped' ? '–' : ''}</span>
              <span className="node-name">{label}</span>
            </div>
          )
        })}
      </div>
      <div className="street-status" role="status" aria-live="polite">
        {statusLine}
      </div>
    </div>
  )
}
