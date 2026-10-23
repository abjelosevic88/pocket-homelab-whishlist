import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { HomeData, List, Occasion } from '../lib/types'
import { Card, Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { Icon, KIND_ICON } from '../components/icons'
import { openCreate, useOnChange } from '../components/Layout'
import { useMoney, WishCard } from '../components/wish'
import { OccasionRow } from './Occasions'

export default function Home() {
  const { user, settings } = useApp()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useAsync(() => api.get<HomeData>(`${V1}/home`), [])
  useOnChange(() => { reload() })
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return null
  const hour = new Date().getHours()
  const greet = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'
  const first = (user?.name || '').split(' ')[0] || 'there'
  const soon = data.occasions.filter(o => o.days <= 14)
  const openRes = data.reservations.filter(r => !r.bought)
  const sub = [data.at_target.length ? `${data.at_target.length} at your target price` : '', soon.length ? `${soon.length} occasion${soon.length === 1 ? '' : 's'} in the next two weeks` : '', openRes.length ? `${openRes.length} gift${openRes.length === 1 ? '' : 's'} to get for others` : ''].filter(Boolean).join(' · ')
  const empty = data.lists.length === 0
  return (
    <>
      <PageHeader title={`${greet}, ${first}`} sub={sub || (empty ? 'Start a wishlist, or collect gift ideas for someone.' : 'Nothing needs you right now.')}
        actions={<><button type="button" className="btn primary" onClick={() => openCreate('link')}><Icon name="link" size={15} />Add from a link</button><Link className="btn" to="/occasions"><Icon name="calendar" size={15} />Occasions</Link></>} />

      {empty ? (
        <Card className="mb"><Empty icon="gift" title="Your wishlists live here">
          Paste a link to anything you would like: the name, picture and price are read from the shop, the price is watched and you get a push when it drops. Share a list with family so they can reserve gifts — without you seeing who gets what.
        </Empty></Card>
      ) : <>
        <div className="stats">
          <Link className="stat" to="/wishes"><Icon name="heart" size={16} /><div className="n">{data.lists.filter(l => l.kind === 'mine').reduce((n, l) => n + l.wanted, 0)}</div><div className="l">things you want</div></Link>
          <Link className="stat" to="/wishes?view=drops"><Icon name="trending-down" size={16} /><div className="n">{data.at_target.length}</div><div className="l">at your target price</div></Link>
          <Link className="stat" to="/occasions"><Icon name="calendar" size={16} /><div className="n">{data.occasions.length}</div><div className="l">occasions in {settings?.occasion_days || 30} days</div></Link>
          <Link className="stat" to="/shared"><Icon name="hand" size={16} /><div className="n">{openRes.length}</div><div className="l">gifts you are getting for others</div></Link>
        </div>
      </>}

      <div className="home-grid">
        <div>
          {data.at_target.length > 0 && <>
            <h2 className="section-h"><Icon name="trending-down" size={16} />At your target price</h2>
            <div className="wish-grid mb">{data.at_target.map(w => <WishCard key={w.id} w={w} onOpen={() => navigate(`/wishes/${w.id}`)} />)}</div>
          </>}
          {data.cheaper.length > 0 && <>
            <h2 className="section-h"><Icon name="chart" size={16} />Cheaper than when you added them</h2>
            <div className="wish-grid mb">{data.cheaper.map(w => <WishCard key={w.id} w={w} onOpen={() => navigate(`/wishes/${w.id}`)} />)}</div>
          </>}
          {data.lists.length > 0 && <>
            <h2 className="section-h"><Icon name="list" size={16} />Your lists</h2>
            <div className="list-grid mb">{data.lists.map(l => <ListTile key={l.id} l={l} to={`/lists/${l.id}`} />)}</div>
          </>}
          {data.recent.length > 0 && <>
            <h2 className="section-h"><Icon name="clock" size={16} />Recently added</h2>
            <div className="wish-grid mb">{data.recent.map(w => <WishCard key={w.id} w={w} showList onOpen={() => navigate(`/wishes/${w.id}`)} />)}</div>
          </>}
        </div>
        <div className="grid" style={{ gap: 16 }}>
          <Card title={<><Icon name="calendar" size={16} />Coming up</>} actions={<Link className="btn ghost sm" to="/occasions">All</Link>}>
            {data.occasions.length === 0 ? <p className="muted">No birthdays or gift lists with a date in the next {settings?.occasion_days || 30} days.{!data.contacts && <> Connect Pocket Contacts to see birthdays here.</>}</p>
              : <div>{data.occasions.slice(0, 6).map((o: Occasion, i) => <OccasionRow key={i} o={o} />)}</div>}
          </Card>
          {openRes.length > 0 && <Card title={<><Icon name="hand" size={16} />You’re getting</>} actions={<Link className="btn ghost sm" to="/shared">All</Link>}>
            <div className="inline-list">{openRes.slice(0, 6).map(r => <Link key={r.id} className="li" to={`/shared/${r.list_id}`}><Icon name="gift" size={15} /><span className="grow"><b>{r.wish_title}</b> <span className="muted small">for {r.owner}</span></span>{r.quantity > 1 && <span className="muted small">× {r.quantity}</span>}</Link>)}</div>
          </Card>}
          {data.shared.length > 0 && <Card title={<><Icon name="users" size={16} />Shared with you</>}>
            <div className="inline-list">{data.shared.map(l => <Link key={l.id} className="li" to={`/shared/${l.id}`}><span>{l.emoji || '🎁'}</span><span className="grow"><b>{l.name}</b> <span className="muted small">· {l.owner}</span></span><span className="muted small">{l.wanted} wishes</span></Link>)}</div>
          </Card>}
          {data.bought.length > 0 && <Card title={<><Icon name="check-circle" size={16} />Recently bought</>} actions={<Link className="btn ghost sm" to="/wishes?view=bought">All</Link>}>
            <BoughtList items={data.bought} />
          </Card>}
        </div>
      </div>
    </>
  )
}

function BoughtList({ items }: { items: HomeData['bought'] }) {
  const money = useMoney()
  return <div className="inline-list">{items.map(w => <Link key={w.id} className="li" to={`/wishes/${w.id}`}><Icon name={KIND_ICON[w.list_kind] || 'check'} size={15} /><span className="grow"><b>{w.title}</b>{w.recipient && <span className="muted small"> · for {w.recipient}</span>}</span>{w.bought_price > 0 && <span className="muted small">{money(w.bought_price, w.bought_currency || w.currency)}</span>}</Link>)}</div>
}

export function ListTile({ l, to }: { l: List; to: string }) {
  const money = useMoney()
  const sub = l.kind === 'gifts'
    ? [l.recipient ? `for ${l.recipient}` : '', `${l.wanted} idea${l.wanted === 1 ? '' : 's'}`, l.bought ? `${l.bought} bought` : ''].filter(Boolean).join(' · ')
    : [`${l.wanted} wish${l.wanted === 1 ? '' : 'es'}`, l.total ? money(l.total, l.currency) : '', l.shared ? 'shared' : '', l.owner && l.role === 'viewer' ? `from ${l.owner}` : ''].filter(Boolean).join(' · ')
  return (
    <Link className="list-tile" to={to}>
      <span className="thumb">{l.cover ? <img src={l.cover} alt="" /> : (l.emoji || <Icon name={KIND_ICON[l.kind]} size={22} />)}</span>
      <span style={{ minWidth: 0 }}><b>{l.emoji && l.cover ? `${l.emoji} ` : ''}{l.name}</b><span className="s">{sub}</span></span>
    </Link>
  )
}
