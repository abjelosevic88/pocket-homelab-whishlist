import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Wish } from '../lib/types'
import { Empty, Loading, PageHeader, useAsync, useDebounce, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { openCreate, useOnChange } from '../components/Layout'
import { useMoney, WishCard } from '../components/wish'

const VIEWS: Record<string, { title: string; sub: string; q: string; icon: string; empty: string }> = {
  '': { title: 'All wishes', sub: 'Everything you want and every gift idea, across your lists.', q: '', icon: 'list', empty: 'Nothing here yet' },
  drops: { title: 'Price drops', sub: 'Wishes at or below the price you said you would pay.', q: '&at_target=1', icon: 'trending-down', empty: 'No wish is at its target price' },
  tracked: { title: 'Watched prices', sub: 'Wishes whose shop page is checked for a new price.', q: '&tracked=1&sort=checked', icon: 'chart', empty: 'No prices are watched' },
  bought: { title: 'Bought', sub: 'What you bought for yourself and the gifts you got.', q: '&status=bought&sort=bought', icon: 'check-circle', empty: 'Nothing bought yet' },
  archived: { title: 'Archive', sub: 'Wishes you put away.', q: '&status=archived', icon: 'archive', empty: 'The archive is empty' },
}

export default function Wishes() {
  const navigate = useNavigate()
  const money = useMoney()
  const { toast } = useToast()
  const [sp, setSp] = useSearchParams()
  const view = VIEWS[sp.get('view') || ''] ? sp.get('view') || '' : ''
  const v = VIEWS[view]
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q, 250)
  const kind = sp.get('kind') || ''
  const tag = sp.get('tag') || ''
  const sort = sp.get('sort') || ''
  const setParam = (k: string, val: string) => { const n = new URLSearchParams(sp); if (val) n.set(k, val); else n.delete(k); setSp(n, { replace: true }) }
  useEffect(() => { setParam('q', dq) }, [dq]) // eslint-disable-line react-hooks/exhaustive-deps
  const url = `${V1}/wishes?q=${encodeURIComponent(dq)}&kind=${kind}&tag=${encodeURIComponent(tag)}${v.q}${sort ? `&sort=${sort}` : v.q.includes('sort=') ? '' : '&sort=priority'}`
  const { data, loading, reload } = useAsync(() => api.get<Wish[]>(url), [url])
  const { data: tags } = useAsync(() => api.get<{ tag: string; count: number }[]>(`${V1}/tags`), [])
  useOnChange(() => { reload() })
  const [running, setRunning] = useState(false)
  const runAll = async () => { setRunning(true); try { const r = await api.post<{ checked: number; failed: number }>(`${V1}/checks/run`); toast(`Checked ${r.checked} page${r.checked === 1 ? '' : 's'}${r.failed ? `, ${r.failed} without a price` : ''}`); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setRunning(false) } }
  const total = (data || []).reduce<Record<string, number>>((m, w) => { const c = view === 'bought' ? (w.bought_currency || w.currency) : w.currency; const p = view === 'bought' ? w.bought_price : w.price * w.quantity; if (c && p) m[c] = (m[c] || 0) + p; return m }, {})
  const totalText = Object.entries(total).map(([c, p]) => money(p, c)).join(' + ')
  return (
    <>
      <PageHeader title={v.title} sub={data ? `${data.length} · ${v.sub}${totalText ? ` · ${totalText}` : ''}` : v.sub}
        actions={<>{view === 'tracked' && <button type="button" className="btn" onClick={runAll} disabled={running}><Icon name="refresh" size={15} />{running ? 'Checking…' : 'Check all now'}</button>}<button type="button" className="btn primary" onClick={() => openCreate('link')}><Icon name="link" size={15} />Add from a link</button></>} />
      <div className="filters">
        {Object.entries(VIEWS).map(([k, x]) => <button key={k} type="button" className={`chip ${view === k ? 'active' : ''}`} onClick={() => { const n = new URLSearchParams(sp); if (k) n.set('view', k); else n.delete('view'); n.delete('sort'); setSp(n, { replace: true }) }}><Icon name={x.icon} size={14} />{x.title}</button>)}
      </div>
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search name, shop, tag, person…" /></div>
        <Select value={kind} onChange={val => setParam('kind', val)} options={[{ value: 'mine', label: 'For me' }, { value: 'gifts', label: 'Gift ideas' }]} placeholder="Every list" clearLabel="Every list" ariaLabel="Lists" />
        {tags && tags.length > 0 && <Select value={tag} onChange={val => setParam('tag', val)} options={tags.map(t => ({ value: t.tag, label: '#' + t.tag, hint: String(t.count) }))} placeholder="Any tag" clearLabel="Any tag" ariaLabel="Tag" />}
        <Select value={sort} onChange={val => setParam('sort', val)} options={[{ value: 'priority', label: 'Most wanted' }, { value: 'price', label: 'Cheapest' }, { value: 'price_desc', label: 'Dearest' }, { value: 'added', label: 'Newest' }, { value: 'title', label: 'A → Z' }]} placeholder="Sort" ariaLabel="Sort" />
      </div>
      {loading && !data ? <Loading /> : !data || data.length === 0 ? <Empty icon={v.icon} title={q || tag || kind ? 'Nothing matches' : v.empty}>{view === 'drops' && 'Give a wish a target price and watch its link: it shows up here the moment the shop gets there.'}</Empty>
        : <div className="wish-grid">{data.map(w => <WishCard key={w.id} w={w} showList onOpen={() => navigate(`/wishes/${w.id}`)} />)}</div>}
    </>
  )
}
