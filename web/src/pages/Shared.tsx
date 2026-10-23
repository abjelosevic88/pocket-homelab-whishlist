import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { List, Reservation, Wish } from '../lib/types'
import { Card, Empty, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { announceChange, useCrumb, useLists, useOnChange } from '../components/Layout'
import { ReserveModal } from '../components/forms'
import { useMoney, WishCard } from '../components/wish'
import { ListTile } from './Home'

/** /shared — lists people on this box shared with me, and what I promised to get. */
export function SharedLists() {
  const { shared } = useLists()
  const money = useMoney()
  const { toast } = useToast()
  const { data, loading, reload } = useAsync(() => api.get<Reservation[]>(`${V1}/reservations`), [])
  useOnChange(() => { reload() })
  const toggle = async (r: Reservation) => { try { await api.post(`${V1}/shared/${r.list_id}/wishes/${r.wish_id}/bought`, { bought: !r.bought }); reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <>
      <PageHeader title="Shared with me" sub="Wishlists other people on this box shared with you. Say “I’ll get this” so nobody buys the same gift twice — the owner never sees who." />
      <h2 className="section-h"><Icon name="hand" size={16} />You’re getting</h2>
      {loading && !data ? <Loading /> : !data || data.length === 0 ? <Card className="mb"><p className="muted">Nothing reserved yet. Open a list below and pick something.</p></Card> : (
        <Card className="mb"><div className="inline-list">{data.map(r => (
          <div key={r.id} className="li">
            <button type="button" className={`icon-btn ${r.bought ? 'ok' : ''}`} onClick={() => toggle(r)} title={r.bought ? 'Bought — tap to undo' : 'Tap when you bought it'} aria-pressed={r.bought}><Icon name={r.bought ? 'check-circle' : 'circle'} size={18} /></button>
            <span className="grow" style={{ textDecoration: r.bought ? 'line-through' : undefined }}><Link to={`/shared/${r.list_id}`}><b>{r.wish_title}</b></Link> <span className="muted small">for {r.owner}{r.quantity > 1 ? ` · × ${r.quantity}` : ''}{r.note ? ` · ${r.note}` : ''}</span></span>
            {r.price > 0 && <span className="muted small">{money(r.price, r.currency)}</span>}
            {r.url && <a className="icon-btn" href={r.url} target="_blank" rel="noreferrer noopener" aria-label="Open the shop page"><Icon name="external" size={15} /></a>}
          </div>
        ))}</div></Card>
      )}
      <h2 className="section-h"><Icon name="users" size={16} />Lists</h2>
      {shared.length === 0 ? <Empty icon="users" title="Nobody shared a list with you yet">When someone on this box shares a wishlist with you it appears here.</Empty>
        : <div className="list-grid">{shared.map((l: List) => <ListTile key={l.id} l={l} to={`/shared/${l.id}`} />)}</div>}
    </>
  )
}

/** /shared/:id — a list shared with me: reserve gifts on it. */
export function SharedList() {
  const { id } = useParams()
  const { data, error, loading, reload } = useAsync(() => api.get<{ list: List; wishes: Wish[] }>(`${V1}/shared/${id}`), [id])
  useOnChange(() => { reload() })
  useCrumb(data ? `${data.list.owner}: ${data.list.name}` : undefined)
  const [open, setOpen] = useState<Wish | null>(null)
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error === 'not found' ? 'This list is not shared with you (any more).' : error}</span></div>
  if (!data) return null
  return <ViewerList list={data.list} wishes={data.wishes} base={`${V1}/shared/${data.list.id}/wishes`} anonymous={false} open={open} setOpen={setOpen} onChanged={() => { reload(); announceChange() }} />
}

/** A wishlist as a gift-giver sees it (shared with me, or through a link). */
export function ViewerList({ list: l, wishes, base, anonymous, open, setOpen, onChanged }: { list: List; wishes: Wish[]; base: string; anonymous: boolean; open: Wish | null; setOpen: (w: Wish | null) => void; onChanged: () => void }) {
  const wanted = wishes.filter(w => w.status === 'wanted')
  const mine = wanted.filter(w => w.mine > 0)
  const free = wanted.filter(w => !w.mine && w.quantity - w.reserved > 0)
  const taken = wanted.filter(w => !w.mine && w.quantity - w.reserved <= 0)
  const bought = wishes.filter(w => w.status === 'bought')
  const section = (title: string, icon: string, ws: Wish[]) => ws.length > 0 && <>
    <h2 className="section-h"><Icon name={icon} size={16} />{title}</h2>
    <div className="wish-grid mb">{ws.map(w => <WishCard key={w.id} w={w} viewer onOpen={() => setOpen(w)} />)}</div>
  </>
  return (
    <>
      <div className="list-hero">
        <span className="list-emoji">{l.emoji || '🎁'}</span>
        <div style={{ minWidth: 0 }}>
          <h1>{l.name}</h1>
          <div className="meta"><span><Icon name="user" size={13} />{l.owner}’s wishlist</span><span>{wanted.length} wishes · {free.length} still free</span></div>
          {l.description && <p className="muted mt-sm" style={{ maxWidth: 680 }}>{l.description}</p>}
        </div>
      </div>
      {wanted.length === 0 && bought.length === 0 ? <Empty icon="gift" title="This list is empty">Nothing on it yet.</Empty> : <>
        {section('You’re getting', 'hand', mine)}
        {section('Still free', 'gift', free)}
        {section('Someone is getting these', 'users', taken)}
        {section('They already have', 'check-circle', bought)}
      </>}
      {open && <ReserveModal wish={open} base={`${base}/${open.id}`} anonymous={anonymous} onClose={() => setOpen(null)} onDone={() => { setOpen(null); onChanged() }} />}
    </>
  )
}
