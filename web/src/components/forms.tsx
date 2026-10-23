import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { todayISO } from '../lib/format'
import { EMOJIS, type Contact, type Kind, type List, type MoneyAccount, type MoneyCategory, type Preview, type Share, type SplitGroup, type Wish } from '../lib/types'
import { CoreHint, Field, Modal, Segmented, Spinner, Switch, copyText, useDebounce, useToast } from './ui'
import { Select } from './Select'
import { AmountInput, CurrencySelect, MoneyInput } from './Money'
import { Icon, KIND_ICON } from './icons'
import { PriorityPick, useMoney } from './wish'

// ---------- a wish: add (from a link or by hand) and edit ----------

export function WishModal({ wish, lists, listId, fromLink, initialURL = '', initialTitle = '', onClose, onSaved }: {
  wish?: Wish; lists: List[]; listId?: number; fromLink?: boolean; initialURL?: string; initialTitle?: string; onClose: () => void; onSaved: (w: Wish) => void
}) {
  const { settings } = useApp()
  const { toast } = useToast()
  const own = lists.filter(l => !l.archived)
  const first = listId || own.find(l => l.kind === 'mine')?.id || own[0]?.id || 0
  const [f, setF] = useState({
    list_id: wish?.list_id || first, title: wish?.title || initialTitle, url: wish?.url || initialURL, store: wish?.store || '', notes: wish?.notes || '', tags: wish?.tags || '',
    image_url: wish?.image_url || '', price: wish?.price || 0, currency: wish?.currency || settings?.home_currency || 'EUR', target_price: wish?.target_price || 0,
    priority: wish?.priority || 2, quantity: wish?.quantity || 1, track_price: wish ? wish.track_price : false,
  })
  const [preview, setPreview] = useState<Preview | null>(null)
  const [reading, setReading] = useState(false)
  const [more, setMore] = useState(!!wish || !fromLink)
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const urlRef = useRef<HTMLInputElement>(null)

  const read = async (raw = f.url) => {
    const url = raw.trim()
    if (!/^https?:\/\//i.test(url)) return
    setReading(true); setPreview(null)
    try {
      const p = await api.post<Preview>(`${V1}/preview`, { url })
      setPreview(p)
      if (p.ok && p.product) {
        const pr = p.product
        setF(x => ({ ...x, url, title: x.title && wish ? x.title : (pr.title || x.title), image_url: pr.image || x.image_url, store: pr.store || x.store,
          price: p.price || x.price, currency: p.currency || x.currency, track_price: wish ? x.track_price : !!p.price || x.track_price }))
      } else setF(x => ({ ...x, store: x.store || p.store || '' }))
      setMore(true)
    } catch (e) { setPreview({ ok: false, error: (e as Error).message }); setMore(true) } finally { setReading(false) }
  }
  useEffect(() => { if (!wish && initialURL) read(initialURL) }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!f.title.trim() && !f.url.trim()) { toast('Give it a name, or paste a link', 'err'); return }
    setBusy(true)
    try {
      const body = { ...f, no_lookup: !!preview || !!wish || !f.url }
      const w = wish ? await api.put<Wish>(`${V1}/wishes/${wish.id}`, body) : await api.post<Wish>(`${V1}/wishes`, body)
      toast(wish ? 'Saved' : 'Added')
      onSaved(w)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const listOpts = own.map(l => ({ value: String(l.id), label: `${l.emoji ? l.emoji + ' ' : ''}${l.name}`, group: l.kind === 'gifts' ? 'Gift ideas' : 'My wishlists', hint: l.recipient }))
  return (
    <Modal title={wish ? 'Edit wish' : fromLink ? 'Add from a link' : 'Add a wish'} onClose={onClose} size="lg">
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="link-input">
          <Icon name="link" size={17} />
          <input ref={urlRef} value={f.url} onChange={e => set('url', e.target.value)} placeholder="Paste a link to the product (shop page)" autoFocus={fromLink || !wish}
            onPaste={e => { const t = e.clipboardData.getData('text'); if (/^https?:\/\//i.test(t.trim())) { e.preventDefault(); set('url', t.trim()); read(t) } }}
            onKeyDown={e => { if (e.key === 'Enter' && /^https?:\/\//i.test(f.url.trim()) && !preview) { e.preventDefault(); read() } }} inputMode="url" />
        </div>
        {f.url && !preview && !reading && /^https?:\/\//i.test(f.url.trim()) && <div className="row"><button type="button" className="btn sm" onClick={() => read()}><Icon name="refresh" size={14} />Read the page</button><span className="muted small">Fills in the name, picture and price.</span></div>}
        {reading && <div className="row muted small"><Spinner />Reading the page…</div>}
        {preview && (preview.ok && preview.product ? (
          <div className="preview-card">
            {preview.product.image ? <img src={preview.product.image} alt="" referrerPolicy="no-referrer" /> : <span className="ph"><Icon name="gift" size={26} /></span>}
            <div className="grid" style={{ gap: 3, minWidth: 0 }}>
              <b>{preview.product.title || 'No name on the page'}</b>
              <span className="muted small">{[preview.product.store, preview.product.brand].filter(Boolean).join(' · ')}</span>
              <span className="small">{preview.price ? <b>{preview.product.price} {preview.currency}</b> : <span className="muted">No price found — type it below</span>}{preview.product.availability === 'out_of_stock' && <span className="bad"> · out of stock</span>}</span>
            </div>
          </div>
        ) : <div className="callout warn"><Icon name="alert" size={15} /><span>{preview.error || 'Could not read that page'}. Fill in the details yourself; the link is kept.</span></div>)}
        {!more && <button type="button" className="link-btn" onClick={() => setMore(true)}>…or fill it in by hand</button>}
        {more && <>
          <Field label="Name"><input value={f.title} onChange={e => set('title', e.target.value)} placeholder="Noise-cancelling headphones" required={!f.url} autoFocus={!!wish === false && !fromLink} /></Field>
          <div className="two-col tight">
            <Field label="List">{listOpts.length ? <Select value={String(f.list_id || '')} onChange={v => set('list_id', Number(v))} options={listOpts} placeholder="My wishlist (made for you)" /> : <input disabled value="My wishlist (made for you)" />}</Field>
            <Field label="Price"><AmountInput amount={f.price || null} currency={f.currency} onAmount={m => set('price', m || 0)} onCurrency={c => set('currency', c)} /></Field>
          </div>
          <div className="two-col tight">
            <Field label="Tell me when it costs" help="At or below this you get a push (needs a link to watch)."><MoneyInput value={f.target_price || null} code={f.currency} onChange={m => set('target_price', m || 0)} placeholder="target price" /></Field>
            <Field group label="How much do you want it?"><PriorityPick value={f.priority} onChange={p => set('priority', p)} /></Field>
          </div>
          <div className="two-col tight">
            <Field label="Shop"><input value={f.store} onChange={e => set('store', e.target.value)} placeholder="from the link" /></Field>
            <Field label="How many"><input type="number" min={1} max={999} value={f.quantity} onChange={e => set('quantity', Math.max(1, Number(e.target.value) || 1))} /></Field>
          </div>
          <Field label="Tags" help="Comma separated."><input value={f.tags} onChange={e => set('tags', e.target.value)} placeholder="audio, travel" /></Field>
          <Field label="Notes" help="Size, colour, the model you mean…"><textarea rows={2} value={f.notes} onChange={e => set('notes', e.target.value)} /></Field>
          <label className="check-row"><Switch checked={f.track_price && !!f.url} onChange={v => set('track_price', v)} disabled={!f.url} label="Watch the price" /><span>Watch the price{settings?.price_check_hours ? ` (checked every ${settings.price_check_hours} h)` : ' (price checks are off in Settings)'}</span></label>
        </>}
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || reading}>{wish ? 'Save' : 'Add'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- a list ----------

export function ListModal({ list, kind = 'mine', initial, onClose, onSaved }: { list?: List; kind?: Kind; initial?: Partial<Pick<List, 'recipient' | 'contact_id' | 'occasion' | 'occasion_date' | 'name'>>; onClose: () => void; onSaved: (l: List) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({
    name: list?.name || initial?.name || '', kind: list?.kind || kind, emoji: list?.emoji || (kind === 'gifts' ? '🎁' : '❤️'), description: list?.description || '',
    recipient: list?.recipient || initial?.recipient || '', contact_id: list?.contact_id || initial?.contact_id || 0, occasion: list?.occasion || initial?.occasion || '', occasion_date: list?.occasion_date || initial?.occasion_date || '',
    budget: list?.budget || 0, currency: list?.currency || settings?.home_currency || 'EUR', show_reserved: list?.show_reserved || false,
  })
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const gifts = f.kind === 'gifts'
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    const body = { ...f, name: f.name.trim() || (gifts ? f.recipient.trim() : ''), contact_id: f.contact_id || null }
    try { const l = list ? await api.put<List>(`${V1}/lists/${list.id}`, body) : await api.post<List>(`${V1}/lists`, body); toast(list ? 'Saved' : 'List created'); onSaved(l) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const pickContact = (c: Contact) => {
    setF(x => {
      const n = { ...x, recipient: c.fn, contact_id: c.id, name: x.name || c.fn }
      const next = nextBirthday(c.birthday)
      if (next && !x.occasion_date) { n.occasion = x.occasion || 'Birthday'; n.occasion_date = next }
      return n
    })
  }
  return (
    <Modal title={list ? 'Edit list' : gifts ? 'Gift ideas for someone' : 'New wishlist'} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        {!list && <Field group label="What is it"><Segmented value={f.kind} onChange={v => set('kind', v as Kind)} options={[{ value: 'mine', label: <span className="row" style={{ gap: 5 }}><Icon name="heart" size={13} />Things I want</span> }, { value: 'gifts', label: <span className="row" style={{ gap: 5 }}><Icon name="gift" size={13} />Gift ideas for someone</span> }]} size="sm" /></Field>}
        {gifts && <Field label="For" help="Pick someone from Pocket Contacts to bring their birthday along, or just type a name."><ContactPicker value={f.recipient} onText={v => setF(x => ({ ...x, recipient: v, contact_id: 0 }))} onPick={pickContact} linked={!!f.contact_id} /></Field>}
        <Field label="Name"><input value={f.name} onChange={e => set('name', e.target.value)} placeholder={gifts ? (f.recipient || 'Mum') : 'My wishlist'} required={!gifts} autoFocus={!gifts} maxLength={120} /></Field>
        <Field group label="Icon"><div className="row wrap" style={{ gap: 4 }}>{EMOJIS.map(em => <button key={em} type="button" className={`chip ${f.emoji === em ? 'active' : ''}`} onClick={() => set('emoji', em)} aria-label={em}>{em}</button>)}</div></Field>
        {gifts && <div className="two-col tight">
          <Field label="Occasion"><input value={f.occasion} onChange={e => set('occasion', e.target.value)} placeholder="Birthday, Christmas…" list="occasions" /><datalist id="occasions">{['Birthday', 'Christmas', 'Anniversary', 'Name day', 'Valentine’s Day', 'Mother’s Day', 'Father’s Day', 'Wedding', 'Graduation'].map(o => <option key={o} value={o} />)}</datalist></Field>
          <Field label="Date" help="Birthdays and other yearly days roll on by themselves."><input type="date" value={f.occasion_date} onChange={e => set('occasion_date', e.target.value)} /></Field>
        </div>}
        {gifts && <Field label="Budget" help="Optional. The list shows how much of it the bought gifts used."><AmountInput amount={f.budget || null} currency={f.currency} onAmount={m => set('budget', m || 0)} onCurrency={c => set('currency', c)} /></Field>}
        {!gifts && <Field label="Currency for totals"><CurrencySelect value={f.currency} onChange={c => set('currency', c)} /></Field>}
        <Field label="Description"><textarea rows={2} value={f.description} onChange={e => set('description', e.target.value)} placeholder={gifts ? 'What they like, sizes…' : 'Shown on the shared page too'} /></Field>
        {!gifts && <label className="check-row"><Switch checked={f.show_reserved} onChange={v => set('show_reserved', v)} label="Show me when something is reserved" /><span>Show me when something is reserved <span className="muted">(never by whom — off keeps every gift a surprise)</span></span></label>}
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{list ? 'Save' : 'Create'}</button></div>
      </form>
    </Modal>
  )
}

function nextBirthday(b: string): string {
  const m = (b || '').match(/(\d{4}|--)-?(\d{2})-?(\d{2})$/)
  if (!m) return ''
  const now = new Date()
  const today = todayISO()
  let y = now.getFullYear()
  let d = `${y}-${m[2]}-${m[3]}`
  if (d < today) { y++; d = `${y}-${m[2]}-${m[3]}` }
  return d
}

/** Type a name, or pick a contact from Pocket Contacts (through the connection in effect). */
function ContactPicker({ value, onText, onPick, linked }: { value: string; onText: (v: string) => void; onPick: (c: Contact) => void; linked: boolean }) {
  const { settings } = useApp()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [res, setRes] = useState<{ connected: boolean; detail?: string; contacts: Contact[] } | null>(null)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!dq.trim()) { setRes(r => r ? { ...r, contacts: [] } : r); return }
    api.get<{ connected: boolean; detail?: string; contacts: Contact[] }>(`${V1}/contacts/search?q=${encodeURIComponent(dq)}`).then(setRes).catch(() => setRes(null))
  }, [dq])
  const off = settings && !settings.contacts_via
  return (
    <div className="grid" style={{ gap: 6, position: 'relative' }}>
      <div className="row" style={{ gap: 6 }}>
        <input value={value} onChange={e => { onText(e.target.value); setQ(e.target.value); setOpen(true) }} onFocus={() => setOpen(true)} onBlur={() => setTimeout(() => setOpen(false), 150)} placeholder="Name, or search your contacts" autoFocus />
        {linked && <span className="badge ok" title="Linked to Pocket Contacts"><Icon name="users" size={11} />contact</span>}
      </div>
      {open && res?.connected && res.contacts.length > 0 && (
        <div className="sel-pop" style={{ position: 'absolute', top: 40, zIndex: 70 }}>
          <div className="sel-list">{res.contacts.map(c => (
            <div key={c.id} className="sel-opt" onMouseDown={e => { e.preventDefault(); onPick(c); setOpen(false) }}>
              <span className="avatar" style={{ width: 22, height: 22, background: c.color || 'var(--accent)', fontSize: 10 }}>{c.initials}</span>
              <span className="sel-lbl">{c.fn}</span>{c.birthday && <span className="sel-hint"><Icon name="cake" size={12} /> {c.birthday.slice(-5)}</span>}
            </div>
          ))}</div>
        </div>
      )}
      {off && <span className="muted small">Pocket Contacts is not connected{settings?.contacts_detail ? `: ${settings.contacts_detail}` : ''}. <Link to="/settings/apps">Connected apps</Link>{!settings?.core_configured && <CoreHint />}</span>}
    </div>
  )
}

// ---------- "Bought it" ----------

interface BoughtResult { wish: Wish; pending: boolean }

/** A connected-app option in the "Bought it" dialog: switch + what it does,
 *  or greyed out with why it is not available. */
function AppOption({ app, via, detail, checked, onChange, label, children }: { app: string; via: string; detail: string; checked: boolean; onChange: (v: boolean) => void; label: string; children?: React.ReactNode }) {
  const ok = via !== ''
  return (
    <div className={`conn-block ${ok ? '' : 'disabled'}`} style={{ opacity: ok ? 1 : 0.6 }}>
      <label className="check-row"><Switch checked={ok && checked} onChange={onChange} disabled={!ok} label={label} /><span><b>{label}</b>{!ok && <span className="muted"> — {app} is not connected{detail ? ` (${detail})` : ''}. <Link to="/settings/apps">Connect it</Link></span>}</span></label>
      {ok && checked && children}
    </div>
  )
}

export function BoughtModal({ wish, onClose, onDone }: { wish: Wish; onClose: () => void; onDone: (w: Wish) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const money = useMoney()
  const s = settings!
  const gift = wish.list_kind === 'gifts'
  const [f, setF] = useState({
    date: todayISO(), price: wish.price, currency: wish.currency || s.home_currency, store: wish.store, notes: '',
    inventory: s.bought_inventory && !gift, inventory_location: s.inventory_location, inventory_category: s.inventory_category,
    money: s.bought_money && wish.price > 0, money_account_id: s.money_account_id, money_category_id: s.money_category_id,
    split: false, split_group_id: s.split_group_id, split_member_id: s.split_member_id,
  })
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const [accounts, setAccounts] = useState<MoneyAccount[]>([])
  const [cats, setCats] = useState<MoneyCategory[]>([])
  const [groups, setGroups] = useState<SplitGroup[]>([])
  const [locs, setLocs] = useState<{ id: number; name: string }[]>([])
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<BoughtResult | null>(null)
  useEffect(() => { if (s.money_via) api.get<MoneyAccount[]>(`${V1}/money/accounts`).then(setAccounts).catch(() => {}) }, [s.money_via])
  useEffect(() => { if (s.money_via) api.get<MoneyCategory[]>(`${V1}/money/categories?account=${f.money_account_id || ''}`).then(setCats).catch(() => {}) }, [s.money_via, f.money_account_id])
  useEffect(() => { if (s.inventory_via) api.get<typeof locs>(`${V1}/inventory/locations`).then(setLocs).catch(() => {}) }, [s.inventory_via])
  useEffect(() => { if (f.split && s.split_via && groups.length === 0) api.get<SplitGroup[]>(`${V1}/split/groups`).then(setGroups).catch(() => {}) }, [f.split, s.split_via]) // eslint-disable-line react-hooks/exhaustive-deps
  const group = groups.find(g => g.id === f.split_group_id)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setRes(await api.post<BoughtResult>(`${V1}/wishes/${wish.id}/bought`, { ...f, inventory: f.inventory && !!s.inventory_via, money: f.money && !!s.money_via, split: f.split && !!s.split_via })) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  if (res) {
    const w = res.wish
    return (
      <Modal title={gift ? `Gift bought for ${wish.recipient || 'them'}` : 'Bought it!'} onClose={() => onDone(w)} size="sm">
        <div className="grid" style={{ gap: 12 }}>
          <p><b>{w.title}</b>{w.bought_price ? <> for <b>{money(w.bought_price, w.bought_currency)}</b></> : null}{w.bought_store ? ` at ${w.bought_store}` : ''}.</p>
          <Handoff w={w} />
          {res.pending && <div className="callout info"><Icon name="clock" size={15} /><span>One of the apps did not answer yet; Wishlist keeps trying in the background and never files it twice.</span></div>}
          <div className="form-actions"><span className="spacer" /><button type="button" className="btn primary" onClick={() => onDone(w)}>Done</button></div>
        </div>
      </Modal>
    )
  }
  return (
    <Modal title={`Bought: ${wish.title}`} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Price paid"><AmountInput amount={f.price || null} currency={f.currency} onAmount={m => set('price', m || 0)} onCurrency={c => set('currency', c)} autoFocus /></Field>
          <Field label="Date"><input type="date" value={f.date} onChange={e => set('date', e.target.value)} max={todayISO()} /></Field>
        </div>
        <Field label="Shop"><input value={f.store} onChange={e => set('store', e.target.value)} /></Field>
        <div className="grid" style={{ gap: 12 }}>
          <AppOption app="Pocket Inventory" via={s.inventory_via} detail={s.inventory_detail} checked={f.inventory} onChange={v => set('inventory', v)} label={gift ? 'Add to my Pocket Inventory (it is a gift, usually not)' : 'Add to Pocket Inventory'}>
            <div className="two-col tight">
              <Field label="Place">{locs.length ? <Select value={f.inventory_location} onChange={v => set('inventory_location', v)} options={locs.map(l => ({ value: l.name, label: l.name }))} clearLabel="None" onCreate={v => v} /> : <input value={f.inventory_location} onChange={e => set('inventory_location', e.target.value)} placeholder="Living room" />}</Field>
              <Field label="Category"><input value={f.inventory_category} onChange={e => set('inventory_category', e.target.value)} placeholder="Electronics" /></Field>
            </div>
          </AppOption>
          <AppOption app="Pocket Money" via={s.money_via} detail={s.money_detail} checked={f.money && f.price > 0} onChange={v => set('money', v)} label="Book the expense in Pocket Money">
            <div className="two-col tight">
              <Field label="Account"><Select value={String(f.money_account_id || '')} onChange={v => set('money_account_id', Number(v))} options={accounts.map(a => ({ value: String(a.id), label: a.name, hint: a.currency + (a.everyday ? ' · everyday' : '') }))} placeholder="Your everyday account" clearLabel="Your everyday account" /></Field>
              <Field label="Category"><Select value={String(f.money_category_id || '')} onChange={v => set('money_category_id', Number(v))} options={cats.map(c => ({ value: String(c.id), label: c.full_name || c.name }))} placeholder="None" clearLabel="None" /></Field>
            </div>
          </AppOption>
          <AppOption app="Pocket Split" via={s.split_via} detail={s.split_detail} checked={f.split && f.price > 0} onChange={v => set('split', v)} label={gift ? 'Split the gift in Pocket Split (a group present)' : 'Share the cost in Pocket Split'}>
            <div className="two-col tight">
              <Field label="Group"><Select value={String(f.split_group_id || '')} onChange={v => set('split_group_id', Number(v))} options={groups.map(g => ({ value: String(g.id), label: g.name, hint: g.currency }))} placeholder="Pick a group" /></Field>
              <Field label="Paid by">{group ? <Select value={String(f.split_member_id || '')} onChange={v => set('split_member_id', Number(v))} options={group.members.map(m => ({ value: String(m.id), label: m.name }))} placeholder="Me" /> : <input disabled placeholder="pick a group first" />}</Field>
            </div>
          </AppOption>
          {!s.core_configured && (!s.inventory_via || !s.money_via || !s.split_via) && <CoreHint />}
        </div>
        <Field label="Note"><input value={f.notes} onChange={e => set('notes', e.target.value)} placeholder="Order number, warranty…" /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}><Icon name="check" size={15} />{busy ? 'Filing it…' : 'Mark bought'}</button></div>
      </form>
    </Modal>
  )
}

/** Where a purchase went: links into Inventory, Money and Split. */
export function Handoff({ w }: { w: Wish }) {
  const { settings } = useApp()
  const open = (app: 'inventory' | 'money' | 'split', path: string) => {
    const base = settings?.[`${app}_open_url` as const]
    return base ? `${base.replace(/\/$/, '')}${path}` : ''
  }
  const rows: [string, string, string][] = []
  if (w.inventory_item_id) rows.push(['package', 'In Pocket Inventory', open('inventory', `/items/${w.inventory_item_id}`)])
  if (w.money_tx_id) rows.push(['wallet', 'Booked in Pocket Money', open('money', `/transactions/${w.money_tx_id}`)])
  if (w.split_expense_id) rows.push(['users', 'Shared in Pocket Split', open('split', `/groups/${w.split_group_id}`)])
  if (!rows.length && !w.handoff_note) return null
  return (
    <div className="handoff">
      {rows.map(([icon, label, href]) => href ? <a key={label} href={href} target="_blank" rel="noreferrer"><Icon name={icon} size={15} />{label}<Icon name="external" size={12} /></a> : <span key={label}><Icon name={icon} size={15} />{label}</span>)}
      {w.handoff_note && <span className="muted small">{w.handoff_note}</span>}
    </div>
  )
}

// ---------- sharing a list ----------

export function ShareModal({ list, onClose, onChanged }: { list: List; onClose: () => void; onChanged: () => void }) {
  const { users, user } = useApp()
  const { toast } = useToast()
  const [shares, setShares] = useState<Share[]>([])
  const [person, setPerson] = useState('')
  const [showRes, setShowRes] = useState(list.show_reserved)
  const load = () => api.get<Share[]>(`${V1}/lists/${list.id}/shares`).then(setShares).catch(e => toast((e as Error).message, 'err'))
  useEffect(() => { load() }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const link = shares.find(s => s.kind === 'link')
  const people = shares.filter(s => s.kind === 'user')
  const run = async (fn: () => Promise<unknown>, msg?: string) => { try { await fn(); await load(); onChanged(); if (msg) toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  const others = users.filter(u => u.id !== user?.id && !people.some(p => p.user_id === u.id))
  return (
    <Modal title={`Share “${list.name}”`} onClose={onClose}>
      <div className="grid" style={{ gap: 16 }}>
        <p className="muted small">People you share with see the list and can say “I’ll get this”, so nobody buys the same gift twice. You never see who reserved what{showRes ? ' — only that something is taken' : ''}.</p>
        <section className="grid" style={{ gap: 8 }}>
          <h4>Link</h4>
          {link?.url ? <>
            <div className="row"><input readOnly value={link.url} onFocus={e => e.currentTarget.select()} /><button type="button" className="btn" onClick={async () => { if (await copyText(link.url!)) toast('Link copied') }}><Icon name="copy" size={15} />Copy</button></div>
            <div className="row wrap"><a className="btn sm ghost" href={link.url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open</a><button type="button" className="btn sm ghost danger-text" onClick={() => run(() => api.del(`${V1}/lists/${list.id}/shares/${link.id}`), 'Link removed — the old one stops working')}>Remove the link</button></div>
            <p className="muted small">Anyone with the link can open it, no account needed (on this box’s network). Removing it and making a new one changes the address.</p>
          </> : <div className="row"><button type="button" className="btn primary" onClick={() => run(() => api.post(`${V1}/lists/${list.id}/shares`, { kind: 'link' }), 'Link created')}><Icon name="link" size={15} />Create a link</button></div>}
        </section>
        <section className="grid" style={{ gap: 8 }}>
          <h4>People on this box</h4>
          {people.map(p => <div key={p.id} className="row"><span className="grow"><b>{p.user_name || p.user_email}</b> <span className="muted small">{p.user_email}</span></span><button type="button" className="btn sm ghost danger-text" onClick={() => run(() => api.del(`${V1}/lists/${list.id}/shares/${p.id}`))}>Remove</button></div>)}
          {others.length > 0 ? <div className="row"><Select value={person} onChange={setPerson} options={others.map(u => ({ value: String(u.id), label: u.name || u.email, hint: u.email }))} placeholder="Share with…" /><button type="button" className="btn" disabled={!person} onClick={() => { const id = Number(person); setPerson(''); run(() => api.post(`${V1}/lists/${list.id}/shares`, { kind: 'user', user_id: id }), 'Shared') }}>Share</button></div>
            : people.length === 0 && <p className="muted small">Nobody else has an account here yet.</p>}
        </section>
        <label className="check-row"><Switch checked={showRes} onChange={v => { setShowRes(v); run(() => api.put(`${V1}/lists/${list.id}`, { show_reserved: v })) }} label="Show me when something is reserved" /><span>Show me when something is reserved <span className="muted">(never by whom)</span></span></label>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn primary" onClick={onClose}>Done</button></div>
      </div>
    </Modal>
  )
}

// ---------- reserving a gift (a giver) ----------

/** "I'll get this" — base is the API path of the wish for this viewer (…/wishes/<id>). */
export function ReserveModal({ wish, base, anonymous, onClose, onDone }: { wish: Wish; base: string; anonymous: boolean; onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const free = Math.max(0, wish.quantity - wish.reserved) + wish.mine
  const [f, setF] = useState({ name: localStorage.getItem('pw-giver-name') || '', quantity: wish.mine || 1, note: wish.mine_note || '' })
  const [busy, setBusy] = useState(false)
  const money = useMoney()
  const act = async (fn: () => Promise<unknown>, msg: string) => { setBusy(true); try { await fn(); toast(msg); onDone() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) } }
  const reserve = (e: React.FormEvent) => {
    e.preventDefault()
    if (anonymous) localStorage.setItem('pw-giver-name', f.name.trim())
    act(() => api.post(`${base}/reserve`, f), wish.mine ? 'Changed' : 'Reserved — it’s yours to get')
  }
  return (
    <Modal title={wish.title} onClose={onClose}>
      <form onSubmit={reserve} className="grid" style={{ gap: 14 }}>
        <div className="preview-card">
          {wish.photo ? <img src={wish.photo} alt="" /> : <span className="ph"><Icon name={KIND_ICON[wish.list_kind] || 'gift'} size={26} /></span>}
          <div className="grid" style={{ gap: 3, minWidth: 0 }}>
            <b>{wish.title}</b>
            <span className="muted small">{[wish.store, wish.price ? money(wish.price, wish.currency) : ''].filter(Boolean).join(' · ')}</span>
            {wish.url && <a className="small" href={wish.url} target="_blank" rel="noreferrer noopener">Open the shop page <Icon name="external" size={11} /></a>}
          </div>
        </div>
        {wish.notes && <p className="muted">{wish.notes}</p>}
        {wish.status === 'bought' ? <div className="callout info"><Icon name="check-circle" size={15} /><span>They already have this one.</span></div>
          : free <= 0 ? <div className="callout warn"><Icon name="hand" size={15} /><span>Someone is already getting this.</span></div> : <>
            {anonymous && <Field label="Your name" help="So the family knows who is getting it — the list owner does not see it."><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus maxLength={80} /></Field>}
            {wish.quantity > 1 && <Field label={`How many (of ${free} left)`}><input type="number" min={1} max={free} value={f.quantity} onChange={e => setF({ ...f, quantity: Math.min(free, Math.max(1, Number(e.target.value) || 1)) })} /></Field>}
            <Field label="Note for yourself"><input value={f.note} onChange={e => setF({ ...f, note: e.target.value })} placeholder="Order on Friday" /></Field>
          </>}
        <div className="form-actions">
          {wish.mine > 0 && <>
            <button type="button" className="btn ghost danger-text" disabled={busy} onClick={() => act(() => api.del(`${base}/reserve`), 'Released — someone else can get it')}>I won’t get it</button>
            <button type="button" className="btn ghost" disabled={busy} onClick={() => act(() => api.post(`${base}/bought`, { bought: !wish.mine_bought }), wish.mine_bought ? 'Marked not bought' : 'Marked bought')}>{wish.mine_bought ? 'Not bought yet' : 'I bought it'}</button>
          </>}
          <span className="spacer" />
          <button type="button" className="btn" onClick={onClose}>Close</button>
          {wish.status === 'wanted' && free > 0 && <button className="btn primary" disabled={busy}><Icon name="hand" size={15} />{wish.mine ? 'Save' : 'I’ll get this'}</button>}
        </div>
      </form>
    </Modal>
  )
}
