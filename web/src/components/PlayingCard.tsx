import { cardLabel, parseCard } from '../format'

interface Props {
  card?: string
  hidden?: boolean
  small?: boolean
}

// 公共牌/底牌统一渲染；hidden 时只画牌背，绝不泄露牌值。
export function PlayingCard({ card, hidden = false, small = false }: Props) {
  const parsed = card ? parseCard(card) : null
  const classes = ['card']
  if (small) classes.push('small')
  if (hidden || !parsed) {
    classes.push('card-back')
    return <div className={classes.join(' ')} aria-hidden="true" />
  }
  if (parsed.red) classes.push('red')
  return (
    <div className={classes.join(' ')} aria-label={cardLabel(card as string)} title={cardLabel(card as string)}>
      <span className="rank">{parsed.rank}</span>
      <span className="suit">{parsed.glyph}</span>
    </div>
  )
}
