import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDate } from '../lib/format'
import type { Kind, List, Wish } from '../lib/types'
import { Confirm, Empty, Loading, MenuButton, useAsync, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { Icon, KIND_ICON } from '../components/icons'
import { announceChange, openCreate, useCrumb, useLists, useOnChange } from '../components/Layout'
import { ListModal, ShareModal, WishModal } from '../components/forms'
import { useMoney, WishCard } from '../components/wish'

const SORTS = [{ value: '', label: 'Your order' }, { value: 'priority', label: 'Most wanted' }, { value: 'price', label: 'Cheapest' }, { value: 'price_desc', label: 'Dearest' }, { value: 'added', label: 'Newest' }, { value: 'title', label: 'A → Z' }]

export default function ListPage() {
  const { id } = useParams()
  if (id === 'new') return <NewList />
  return <OwnList id={Number(id)} />
}

/** /lists/new?kind=gifts&recipient=… (from Occasions: "no gift ideas yet"). */
function NewList() {
  const [sp] = useSearchParams()
  const navigate = useNavigate()
  const contact = Number(sp.get('contact') || 0)
  return <ListModal kind={(sp.get('kind') as Kind) || 'mine'} initial={{ recipient: sp.get('recipient') || '', name: sp.get('recipient') || '', contact_id: contact || null, occasion: sp.get('occasion') || '', occasion_date: sp.get('date') || '' }}
    onClose={() => navigate(-1)} onSaved={l => { announceChange(); navigate(`/lists/${l.id}`, { replace: true }) }} />
}

function OwnList({ id }: { id: number }) {
  const navigate = useNavigate()
  const { toast } = useToast()
  const money = useMoney()
  const { lists } = useLists()
  const [sp, setSp] = useSearchParams()
  const sort = sp.get('sort') || ''
  const show = sp.get('show') || 'wanted'
  const { data, error, loading, reload } = useAsync(() => api.get<{ list: List; wishes: Wish[] }>(`${V1}/lists/${id}?sort=${sort}`), [id, sort])
  useOnChange(() => { reload() })
  useCrumb(data?.list.name)
  const [edit, setEdit] = useState(false)
  const [share, setShare] = useState(false)
  const [add, setAdd] = useState(false)
  const [del, setDel] = useState(false)
  useEffect(() => { if (sp.get('share') === '1' && data) { setShare(true); const n = new URLSearchParams(sp); n.delete('share'); setSp(n, { replace: true }) } }, [data]) // eslint-disable-line react-hooks/exhaustive-deps
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return null
  const { list: l, wishes } = data
  const gifts = l.kind === 'gifts'
  const wanted = wishes.filter(w => w.status === 'wanted')
  const bought = wishes.filter(w => w.status === 'bought')
  const shown = show === 'bought' ? bought : wanted
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }) }
  const archive = async () => { try { await api.put(`${V1}/lists/${l.id}`, { archived: !l.archived }); toast(l.archived ? 'Back from the archive' : 'Archived'); announceChange(); if (!l.archived) navigate('/') } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async () => { try { await api.del(`${V1}/lists/${l.id}`); toast('List deleted'); announceChange(); navigate('/') } catch (e) { toast((e as Error).message, 'err') } }
  const over = l.budget > 0 && l.spent > l.budget
  return (
    <>
      <div className="list-hero">
        <span className="list-emoji">{l.emoji || <Icon name={KIND_ICON[l.kind]} size={26} />}</span>
        <div style={{ minWidth: 0 }}>
          <h1>{l.name}</h1>
          <div className="meta">
            {gifts && l.recipient && <span><Icon name="user" size={13} />for {l.recipient}</span>}
            {l.occasion_date && <span><Icon name="calendar" size={13} />{l.occasion || 'On'} {fmtDate(l.occasion_date)}</span>}
            <span><Icon name="list" size={13} />{wanted.length} {gifts ? 'idea' : 'wish'}{wanted.length === 1 ? '' : gifts ? 's' : 'es'}{l.total ? ` · ${money(l.total, l.currency)}` : ''}</span>
            {l.budget > 0 && <span title={`${money(l.spent, l.currency)} of ${money(l.budget, l.currency)} spent`}><span className={`budget-bar ${over ? 'over' : ''}`}><span style={{ width: `${Math.min(100, (l.spent * 100) / l.budget)}%` }} /></span>{money(l.spent, l.currency)} / {money(l.budget, l.currency)}</span>}
            {l.shared && <span><Icon name="share" size={13} />shared{l.show_reserved && l.reserved ? ` · ${l.reserved} reserved` : ''}</span>}
          </div>
          {l.description && <p className="muted mt-sm" style={{ maxWidth: 680 }}>{l.description}</p>}
        </div>
        <div className="actions">
          <button type="button" className="btn primary" onClick={() => openCreate('link')}><Icon name="link" size={15} />Add from a link</button>
          <button type="button" className="btn" onClick={() => setAdd(true)}><Icon name="plus" size={15} />Add</button>
          {!gifts && <button type="button" className="btn" onClick={() => setShare(true)}><Icon name="share" size={15} />Share</button>}
          <MenuButton label="List options" align="right" items={[
            { label: 'Edit list', icon: 'edit', onSelect: () => setEdit(true) },
            ...(!gifts && l.share_url ? [{ label: 'Open the shared page', icon: 'external', onSelect: () => window.open(l.share_url, '_blank') }] : []),
            { label: l.archived ? 'Unarchive' : 'Archive', icon: 'archive', onSelect: archive },
            'sep',
            { label: 'Delete list…', icon: 'trash', danger: true, onSelect: () => setDel(true) },
          ]}><Icon name="more" size={18} /></MenuButton>
        </div>
      </div>
      <div className="toolbar">
        <div className="segmented sm" role="tablist">
          <button type="button" className={show !== 'bought' ? 'active' : ''} onClick={() => setParam('show', '')}>{gifts ? 'Ideas' : 'Wanted'} <span className="muted">{wanted.length}</span></button>
          <button type="button" className={show === 'bought' ? 'active' : ''} onClick={() => setParam('show', 'bought')}>Bought <span className="muted">{bought.length}</span></button>
        </div>
        <span className="spacer" />
        <Select value={sort} onChange={v => setParam('sort', v)} options={SORTS} ariaLabel="Sort" />
      </div>
      {shown.length === 0 ? <Empty icon={show === 'bought' ? 'check-circle' : KIND_ICON[l.kind]} title={show === 'bought' ? 'Nothing bought yet' : gifts ? 'No ideas yet' : 'Nothing on this list yet'}>
        {show !== 'bought' && <>Paste a shop link and the name, picture and price fill in by themselves. <button type="button" className="link-btn" onClick={() => openCreate('link')}>Add from a link</button></>}
      </Empty> : <div className="wish-grid">{shown.map(w => <WishCard key={w.id} w={w} onOpen={() => navigate(`/wishes/${w.id}`)} />)}</div>}
      {edit && <ListModal list={l} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); reload(); announceChange() }} />}
      {share && <ShareModal list={l} onClose={() => { setShare(false); reload() }} onChanged={announceChange} />}
      {add && <WishModal lists={lists} listId={l.id} onClose={() => setAdd(false)} onSaved={() => { setAdd(false); reload(); announceChange() }} />}
      {del && <Confirm title={`Delete “${l.name}”?`} confirmLabel="Delete" message={`Its ${wishes.length} wishes go with it${l.shared ? ', and the people you shared it with lose it (their reservations too)' : ''}. Rows already filed in Inventory, Money or Split stay there.`} onCancel={() => setDel(false)} onConfirm={remove} />}
    </>
  )
}
