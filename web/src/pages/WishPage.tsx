import { useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { ago, fmtDate, fmtDateTime } from '../lib/format'
import { AVAIL, PRIORITY, type PricePoint, type Wish } from '../lib/types'
import { Card, Confirm, Loading, MenuButton, Spinner, useAsync, useToast } from '../components/ui'
import { Icon, KIND_ICON } from '../components/icons'
import { announceChange, useCrumb, useLists, useOnChange } from '../components/Layout'
import { BoughtModal, Handoff, WishModal } from '../components/forms'
import { Change, PriceChart, PriorityStars, useMoney } from '../components/wish'

export default function WishPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { settings } = useApp()
  const { lists } = useLists()
  const money = useMoney()
  const { data, error, loading, reload, setData } = useAsync(() => api.get<{ wish: Wish; prices: PricePoint[] }>(`${V1}/wishes/${id}`), [id])
  useOnChange(() => { reload() })
  useCrumb(data?.wish.title)
  const [edit, setEdit] = useState(false)
  const [bought, setBought] = useState(false)
  const [del, setDel] = useState(false)
  const [checking, setChecking] = useState(false)
  const file = useRef<HTMLInputElement>(null)
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return null
  const w = data.wish
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); toast(msg); reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') } }
  const check = async () => {
    setChecking(true)
    try { const r = await api.post<{ wish: Wish; prices: PricePoint[]; error: string }>(`${V1}/wishes/${w.id}/check`); setData({ wish: r.wish, prices: r.prices }); toast(r.error ? `Checked: ${r.error}` : 'Price checked', r.error ? 'err' : 'ok'); announceChange() } catch (e) { toast((e as Error).message, 'err') } finally { setChecking(false) }
  }
  const setPhoto = async (f: File) => { try { await upload(`${V1}/wishes/${w.id}/photo`, 'file', f); reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') } }
  const gift = w.list_kind === 'gifts'
  const lowest = data.prices.filter(p => p.currency === w.currency).reduce((lo, p) => (lo === 0 || p.price < lo ? p.price : lo), 0)
  return (
    <>
      <div className="wish-detail">
        <div className="grid" style={{ gap: 12 }}>
          <div className="wish-photo">
            {w.photo ? <img src={w.photo} alt="" /> : <span className="ph"><Icon name={KIND_ICON[w.list_kind] || 'gift'} size={64} strokeWidth={1.2} /></span>}
            <div className="photo-tools">
              <button type="button" className="btn sm" onClick={() => file.current?.click()}><Icon name="upload" size={14} />{w.photo ? 'Replace' : 'Add a picture'}</button>
              {w.photo && <button type="button" className="btn sm" aria-label="Remove the picture" onClick={() => run(() => api.del(`${V1}/wishes/${w.id}/photo`), 'Picture removed')}><Icon name="trash" size={14} /></button>}
              <input ref={file} type="file" accept="image/*" hidden onChange={e => e.target.files?.[0] && setPhoto(e.target.files[0])} />
            </div>
          </div>
          {w.url && <a className="btn" href={w.url} target="_blank" rel="noreferrer noopener"><Icon name="external" size={15} />Open at {w.store || 'the shop'}</a>}
        </div>
        <div className="grid" style={{ gap: 16, minWidth: 0 }}>
          <div>
            <div className="row muted small" style={{ gap: 6, marginBottom: 6 }}>
              <Link to={`/lists/${w.list_id}`} className="row" style={{ gap: 5 }}><Icon name={KIND_ICON[w.list_kind]} size={13} />{w.list_name}</Link>
              {w.recipient && <span>· for {w.recipient}</span>}
              {w.source && <span className="badge" title="Added by another Pocket app">from {w.source}</span>}
            </div>
            <h1 style={{ fontSize: 24, letterSpacing: '-.02em' }}>{w.title}</h1>
            <div className="price-meta mt-sm">
              <span className="row" style={{ gap: 5 }}><PriorityStars p={w.priority} />{PRIORITY[w.priority]}</span>
              {w.quantity > 1 && <span>× {w.quantity}</span>}
              {w.availability && <span className={w.availability === 'out_of_stock' ? 'bad' : 'ok'}>{AVAIL[w.availability]}</span>}
              {w.reserved > 0 && w.status === 'wanted' && <span className="badge warn" title="Someone is getting this — you will not see who">reserved</span>}
              {w.tags && w.tags.split(',').map(t => <Link key={t} className="badge tag" to={`/wishes?tag=${encodeURIComponent(t.trim())}`}>{t.trim()}</Link>)}
            </div>
          </div>

          {w.status === 'bought' ? (
            <Card title={<><Icon name="check-circle" size={16} />{gift ? 'Gift bought' : 'Bought'}</>} actions={<button type="button" className="btn ghost sm" onClick={() => run(() => api.post(`${V1}/wishes/${w.id}/status`, { status: 'wanted' }), 'Back on the list')}>Back to wanted</button>}>
              <p>{fmtDate(w.bought_at)}{w.bought_price ? <> for <b>{money(w.bought_price, w.bought_currency || w.currency)}</b></> : null}{w.bought_store ? ` at ${w.bought_store}` : ''}.</p>
              <div className="mt-sm"><Handoff w={w} /></div>
            </Card>
          ) : (
            <Card>
              <div className="row wrap" style={{ alignItems: 'flex-end', gap: 14 }}>
                <div>
                  <div className="price-big">{w.price ? money(w.price, w.currency) : <span className="muted" style={{ fontSize: 18 }}>No price</span>}</div>
                  <div className="price-meta">
                    <Change w={w} />
                    {w.target_price > 0 && <span className={w.at_target ? 'ok' : ''}><Icon name="flag" size={12} /> target {money(w.target_price, w.currency)}{w.at_target ? ' — reached' : ''}</span>}
                    {lowest > 0 && lowest < w.price && <span>lowest {money(lowest, w.currency)}</span>}
                  </div>
                </div>
                <span className="spacer" />
                <button type="button" className="btn primary" onClick={() => setBought(true)}><Icon name="check" size={15} />{gift ? 'Bought the gift' : 'Bought it'}</button>
              </div>
              <div className="mt">
                <PriceChart points={data.prices} target={w.target_price} currency={w.currency} />
              </div>
              <div className="row wrap muted small mt-sm">
                {w.track_price ? <span><Icon name="eye" size={12} /> Watching the price{w.checked_at ? ` · checked ${ago(w.checked_at)}` : ''}{settings?.price_check_hours ? ` · every ${settings.price_check_hours} h` : ''}</span>
                  : <span>{w.url ? 'The price is not watched.' : 'Add a link to watch the price.'}</span>}
                {w.check_error && <span className="bad" title={w.checked_at ? fmtDateTime(w.checked_at) : ''}>Last check: {w.check_error}</span>}
                <span className="spacer" />
                {w.url && <button type="button" className="btn sm" onClick={check} disabled={checking}>{checking ? <Spinner /> : <Icon name="refresh" size={14} />}Check now</button>}
                {w.url && <button type="button" className="btn sm ghost" onClick={() => run(() => api.put(`${V1}/wishes/${w.id}`, { track_price: !w.track_price }), w.track_price ? 'No longer watched' : 'Watching the price')}>{w.track_price ? 'Stop watching' : 'Watch the price'}</button>}
              </div>
            </Card>
          )}

          {w.notes && <Card title="Notes"><p style={{ whiteSpace: 'pre-wrap' }}>{w.notes}</p></Card>}

          <div className="row wrap">
            <button type="button" className="btn" onClick={() => setEdit(true)}><Icon name="edit" size={15} />Edit</button>
            <MenuButton label="More" items={[
              ...(w.status !== 'archived' ? [{ label: 'Archive', icon: 'archive', onSelect: () => run(() => api.post(`${V1}/wishes/${w.id}/status`, { status: 'archived' }), 'Archived') }] : [{ label: 'Back to wanted', icon: 'undo', onSelect: () => run(() => api.post(`${V1}/wishes/${w.id}/status`, { status: 'wanted' }), 'Back on the list') }]),
              ...(w.status === 'wanted' ? [{ label: 'Mark bought (file nothing)', icon: 'check', onSelect: () => run(() => api.post(`${V1}/wishes/${w.id}/status`, { status: 'bought' }), 'Marked bought') }] : []),
              'sep',
              { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setDel(true) },
            ]} className="btn"><Icon name="more" size={16} />More</MenuButton>
            <span className="spacer" />
            <span className="muted small">Added {fmtDate(w.created_at.slice(0, 10))}</span>
          </div>
        </div>
      </div>
      {edit && <WishModal wish={w} lists={lists} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); reload(); announceChange() }} />}
      {bought && <BoughtModal wish={w} onClose={() => setBought(false)} onDone={() => { setBought(false); reload(); announceChange() }} />}
      {del && <Confirm title={`Delete “${w.title}”?`} confirmLabel="Delete" message="Its price history goes with it. Anything already filed in Inventory, Money or Split stays there." onCancel={() => setDel(false)} onConfirm={() => run(() => api.del(`${V1}/wishes/${w.id}`), 'Deleted').then(() => navigate(`/lists/${w.list_id}`))} />}
    </>
  )
}
