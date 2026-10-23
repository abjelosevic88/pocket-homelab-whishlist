// A wish as a card, its price chart and the priority stars.
import { useApp } from '../lib/app-context'
import { decimalsOf, fmtDate, fmtMoney } from '../lib/format'
import { changePct, PRIORITY, type PricePoint, type Wish } from '../lib/types'
import { Icon, KIND_ICON } from './icons'

export function useMoney() {
  const { currencies } = useApp()
  return (minor: number, code: string, opts: { sign?: boolean; compact?: boolean } = {}) => fmtMoney(minor, code, decimalsOf(code, currencies), opts)
}

export function PriorityStars({ p }: { p: number }) {
  if (p <= 1) return null
  return <span className="prio-stars" title={PRIORITY[p]}>{Array.from({ length: p - 1 }, (_, i) => <Icon key={i} name="star" size={12} />)}</span>
}

export function PriorityPick({ value, onChange }: { value: number; onChange: (p: number) => void }) {
  return (
    <span className="row" style={{ gap: 8 }}>
      <span className="prio-pick" role="radiogroup" aria-label="Priority">
        {[1, 2, 3].map(p => <button key={p} type="button" role="radio" aria-checked={value === p} className={p <= value ? 'on' : ''} title={PRIORITY[p]} onClick={() => onChange(p)}><Icon name="star" size={18} /></button>)}
      </span>
      <span className="muted small">{PRIORITY[value]}</span>
    </span>
  )
}

/** "−12%" since the wish was added (cheaper is good news). */
export function Change({ w }: { w: Pick<Wish, 'price' | 'change'> }) {
  const pct = changePct(w)
  if (!pct) return null
  return <span className={`change ${pct < 0 ? 'down' : 'up'}`} title="since you added it">{pct > 0 ? '+' : '−'}{Math.abs(pct)}%</span>
}

/**
 * One wish. For a gift-giver (viewer) it says whether it is still free and
 * whether they took it; for the owner, prices and what moved.
 */
export function WishCard({ w, onOpen, viewer, showList }: { w: Wish; onOpen: () => void; viewer?: boolean; showList?: boolean }) {
  const money = useMoney()
  const free = Math.max(0, w.quantity - w.reserved)
  const taken = viewer && w.status === 'wanted' && free === 0 && !w.mine
  const cls = `wish-card ${w.status === 'bought' ? 'bought' : ''} ${taken ? 'taken' : ''}`
  return (
    <button type="button" className={cls} onClick={onOpen} aria-label={w.title}>
      <div className="cover">
        {w.photo ? <img src={w.photo} alt="" loading="lazy" /> : <Icon name={KIND_ICON[w.list_kind] || 'gift'} size={40} strokeWidth={1.4} />}
        <div className="flags">
          {!viewer && w.hunt_status === 'running' && <span className="pill hunting"><Icon name="sparkles" size={11} strokeWidth={2.2} />Hunting…</span>}
          {w.status === 'bought' && <span className="pill bought"><Icon name="check" size={11} strokeWidth={2.6} />{viewer ? 'Got it' : 'Bought'}</span>}
          {!viewer && w.status === 'wanted' && w.at_target && <span className="pill target"><Icon name="trending-down" size={11} strokeWidth={2.4} />At target</span>}
          {!viewer && w.status === 'wanted' && !w.at_target && changePct(w) <= -5 && <span className="pill drop"><Icon name="trending-down" size={11} strokeWidth={2.4} />{changePct(w)}%</span>}
          {w.availability === 'out_of_stock' && w.status === 'wanted' && <span className="pill oos">Out of stock</span>}
          {viewer && w.mine > 0 && <span className="pill mine"><Icon name="hand" size={11} strokeWidth={2.2} />{w.mine_bought ? 'You bought it' : 'You’re getting it'}</span>}
          {taken && <span className="pill taken">Taken</span>}
          {viewer && !taken && !w.mine && w.reserved > 0 && w.status === 'wanted' && <span className="pill">{free} of {w.quantity} left</span>}
          {!viewer && w.reserved > 0 && w.status === 'wanted' && <span className="pill taken" title="Someone is getting this (you will not see who)">Reserved</span>}
        </div>
      </div>
      <div className="body">
        <h3>{w.title}</h3>
        {(w.store || showList) && <div className="store">{showList ? <><Icon name={KIND_ICON[w.list_kind] || 'heart'} size={12} /><span>{w.list_name}{w.recipient ? ` · ${w.recipient}` : ''}</span></> : <><Icon name="store" size={12} /><span>{w.store}</span></>}</div>}
        <div className="price-line">
          {w.status === 'bought' && !viewer && w.bought_price > 0
            ? <span className="price">{money(w.bought_price, w.bought_currency || w.currency)}</span>
            : w.price > 0 ? <span className="price">{money(w.price, w.currency)}</span> : <span className="muted small">No price</span>}
          {!viewer && w.status === 'wanted' && <Change w={w} />}
          {!viewer && w.target_price > 0 && w.status === 'wanted' && !w.at_target && <span className="target">target {money(w.target_price, w.currency)}</span>}
          {w.quantity > 1 && <span className="muted small">× {w.quantity}</span>}
          <span className="spacer" />
          <PriorityStars p={w.priority} />
        </div>
      </div>
    </button>
  )
}

/** The price history as a small line chart, with the target as a dashed line. */
export function PriceChart({ points, target, currency }: { points: PricePoint[]; target: number; currency: string }) {
  const money = useMoney()
  const pts = points.filter(p => p.currency === currency || !p.currency)
  if (pts.length < 2) return <p className="muted small">The chart appears once the price has been checked a few times.</p>
  const W = 600, H = 140, pad = { l: 8, r: 8, t: 12, b: 20 }
  const t0 = new Date(pts[0].at).getTime(), t1 = new Date(pts[pts.length - 1].at).getTime() || t0 + 1
  const prices = pts.map(p => p.price).concat(target > 0 ? [target] : [])
  let lo = Math.min(...prices), hi = Math.max(...prices)
  if (lo === hi) { lo = lo * 0.9; hi = hi * 1.1 || 1 }
  const x = (t: number) => pad.l + ((t - t0) / Math.max(1, t1 - t0)) * (W - pad.l - pad.r)
  const y = (p: number) => pad.t + (1 - (p - lo) / (hi - lo)) * (H - pad.t - pad.b)
  // a step line: a price holds until the next observation
  let d = ''
  pts.forEach((p, i) => {
    const px = x(new Date(p.at).getTime()), py = y(p.price)
    d += i === 0 ? `M${px},${py}` : `H${px}V${py}`
  })
  d += `H${W - pad.r}`
  const area = `${d}V${H - pad.b}H${pad.l}Z`
  const low = Math.min(...pts.map(p => p.price))
  return (
    <svg className="chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`Price history, lowest ${money(low, currency)}`}>
      <path className="area" d={area} />
      {target > 0 && <line className="target" x1={pad.l} x2={W - pad.r} y1={y(target)} y2={y(target)} />}
      <path className="line" d={d} vectorEffect="non-scaling-stroke" />
      <text x={pad.l} y={H - 5}>{fmtDate(pts[0].at.slice(0, 10), false)}</text>
      <text x={W - pad.r} y={H - 5} textAnchor="end">{fmtDate(pts[pts.length - 1].at.slice(0, 10), false)}</text>
    </svg>
  )
}
