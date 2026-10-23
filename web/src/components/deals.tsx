// The deals a hunt found for a wish: what the AI said, the offers best first,
// and "Use this" to make one the wish's link and price.
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { ago } from '../lib/format'
import { AVAIL, MATCH, type Offer, type Wish } from '../lib/types'
import { Card, Spinner, useToast } from './ui'
import { Icon } from './icons'
import { useMoney } from './wish'
import { HuntModal } from './forms'
import { useLists } from './Layout'
import { useApp } from '../lib/app-context'

export function DealsCard({ w, onChanged }: { w: Wish; onChanged: () => void }) {
  const money = useMoney()
  const { toast } = useToast()
  const { lists } = useLists()
  const [offers, setOffers] = useState<Offer[] | null>(null)
  const [home, setHome] = useState('')
  const [hunt, setHunt] = useState(false)
  const { settings } = useApp()
  const deliverTo = settings?.deliver_to || ''
  const running = w.hunt_status === 'running'
  const load = () => api.get<{ offers: Offer[]; home_currency: string }>(`${V1}/wishes/${w.id}/offers`).then(r => { setOffers(r.offers); setHome(r.home_currency) }).catch(() => setOffers([]))
  useEffect(() => { load() }, [w.id, w.hunt_at]) // eslint-disable-line react-hooks/exhaustive-deps
  // while a hunt runs, look again every two seconds
  useEffect(() => {
    if (!running) return
    const t = setInterval(onChanged, 2000)
    return () => clearInterval(t)
  }, [running, onChanged])
  const act = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); toast(msg); load(); onChanged() } catch (e) { toast((e as Error).message, 'err') } }
  const keep = (auto: boolean) => act(() => api.post(`${V1}/wishes/${w.id}/hunt`, { auto }), auto ? 'Hunting again, and every week from now on' : 'Hunting again')
  const never = !w.hunt_status && !w.hunt_at
  if (never && w.status !== 'wanted') return null
  return (
    <Card title={<><Icon name="sparkles" size={16} />Best deals</>}
      actions={!running && w.status === 'wanted' && <button type="button" className="btn sm" onClick={() => setHunt(true)}><Icon name="sparkles" size={14} />{never ? 'Find the best deal' : 'Hunt again'}</button>}>
      {never ? <p className="muted">Let Wishlist search the shops for this: it reads every page it finds and keeps only offers for exactly this item, cheapest first.</p> : <>
        {running && <div className="row muted" style={{ gap: 8 }}><Spinner />{w.hunt_step || 'Hunting'}… <span className="small">(a minute or two)</span></div>}
        {!running && w.hunt_status === 'error' && <div className="callout danger"><Icon name="alert" size={15} /><span>{w.hunt_error}</span></div>}
        {!running && w.hunt_summary && <p className="deal-summary">{w.hunt_summary}</p>}
        {!running && w.hunt_at && <p className="muted small">
          {w.hunt_ai ? `Checked by ${w.hunt_ai === 'pocket' ? 'Pocket AI' : 'your OpenAI key'}` : 'Matched by keywords'} · {ago(w.hunt_at)}
          {w.hunt_auto ? ' · hunting again every week' : <> · <button type="button" className="link-btn" onClick={() => keep(true)}>keep hunting weekly</button></>}
        </p>}
        {offers && offers.length > 0 && <div className="offers">{offers.map((o, i) => {
          const current = o.url === w.url
          return (
            <div key={o.id} className={`offer ${i === 0 && !o.over_budget && o.availability !== 'out_of_stock' ? 'best' : ''}`}>
              <span className="offer-img">{o.image ? <img src={o.image} alt="" referrerPolicy="no-referrer" loading="lazy" /> : <Icon name="store" size={18} />}</span>
              <div className="offer-main">
                <div className="offer-top"><b className="ellipsis">{o.store}</b>
                  <span className={`badge ${o.match === 'exact' ? 'ok' : o.match === 'comparison' ? '' : 'warn'}`}>{MATCH[o.match] || o.match}</span>
                  {o.availability && <span className={`badge ${o.availability === 'out_of_stock' ? 'danger' : ''}`}>{AVAIL[o.availability]}</span>}
                  {o.over_budget && <span className="badge warn">over budget</span>}
                  {current && <span className="badge accent">your link</span>}
                  {deliverTo && o.match !== 'comparison' && <span className={`badge ${o.ships === 'yes' ? 'ok' : o.ships === 'no' ? 'danger' : ''}`} title={o.ship_note}>
                    <Icon name={o.ships === 'yes' ? 'check' : o.ships === 'no' ? 'x' : 'help'} size={11} />{o.ships === 'yes' ? `Delivers to ${deliverTo}` : o.ships === 'no' ? `No delivery to ${deliverTo}` : 'Delivery not confirmed'}
                  </span>}
                </div>
                <div className="offer-title ellipsis" title={o.title}>{o.title}</div>
                {(o.note || o.source === 'snippet') && <div className="muted small">{o.note || 'Price from the search result — check the page'}</div>}
                {deliverTo && o.ship_note && o.match !== 'comparison' && <div className="muted small"><Icon name="package" size={11} /> {o.ship_note}</div>}
              </div>
              <div className="offer-price">
                <b>{money(o.price, o.currency)}</b>
                {o.ship_cost === 0 && <span className="muted small">free delivery</span>}
                {o.ship_cost > 0 && <span className="muted small">+ {money(o.ship_cost, o.ship_currency)} delivery</span>}
                {(o.currency !== home || o.ship_cost > 0) && o.price_home > 0 && <span className="muted small">≈ {money(o.price_home, home)}{o.ship_cost > 0 ? ' in all' : ''}</span>}
              </div>
              <div className="offer-actions">
                <a className="btn sm ghost" href={o.url} target="_blank" rel="noreferrer noopener" aria-label={`Open ${o.store}`}><Icon name="external" size={14} /></a>
                {!current && o.match !== 'comparison' && <button type="button" className="btn sm" onClick={() => act(() => api.post(`${V1}/wishes/${w.id}/offers/${o.id}/use`), 'Using this offer — its price is watched')}>Use this</button>}
                <button type="button" className="btn sm ghost" title="Not this one (stays hidden in later hunts)" aria-label="Dismiss" onClick={() => act(() => api.post(`${V1}/wishes/${w.id}/offers/${o.id}/dismiss`), 'Hidden')}><Icon name="x" size={14} /></button>
              </div>
            </div>
          )
        })}</div>}
        {!running && offers && offers.length === 0 && w.hunt_status === 'done' && <p className="muted small">No offers to show.</p>}
      </>}
      {hunt && <HuntModal wish={w} lists={lists} onClose={() => setHunt(false)} onStarted={() => { setHunt(false); onChanged() }} />}
    </Card>
  )
}
