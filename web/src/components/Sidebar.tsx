// Left column: create button, sections with counts, my wishlists, gift ideas, lists shared with me — the Pocket Calendar sidebar.
import { type ReactNode, useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Counts, List } from '../lib/types'
import { Icon, KIND_ICON } from './icons'
import { createItems, type CreateKind } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onCreate, onNavigate, counts, lists, shared }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; counts: Counts | null; lists: List[]; shared: List[] }) {
  const { status, settings } = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Wishlist'
  const mine = lists.filter(l => l.kind === 'mine')
  const gifts = lists.filter(l => l.kind === 'gifts')
  const nav: { to: string; label: string; icon: string; end?: boolean; badge?: number; badgeKind?: string; badgeTitle?: string; count?: number }[] = [
    { to: '/', label: 'Home', icon: 'home', end: true },
    { to: '/wishes', label: 'All wishes', icon: 'list', end: true, count: counts ? counts.wishes + counts.gift_ideas : undefined },
    { to: '/wishes?view=drops', label: 'Price drops', icon: 'trending-down', badge: counts?.at_target, badgeKind: 'accent', badgeTitle: 'at or below your target price' },
    { to: '/occasions', label: 'Occasions', icon: 'calendar', badge: counts?.occasions, badgeKind: '', badgeTitle: 'coming up' },
    { to: '/shared', label: 'Shared with me', icon: 'users', badge: counts?.to_buy_for_others, badgeKind: 'warn', badgeTitle: 'gifts you reserved and have not bought yet' },
  ]
  return (
    <aside className="sidebar" aria-label="Wishlist">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Wishlist' ? title : <>Pocket <b>Wishlist</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^\d/.test(status.version) ? 'v' + status.version : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive && (n.to.includes('?') ? window.location.search === n.to.slice(n.to.indexOf('?')) : !window.location.search || n.to !== '/wishes') ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.badge && <span className={`badge ${n.badgeKind || ''}`} title={`${n.badge} ${n.badgeTitle || ''}`}>{n.badge}</span>}
            {!n.badge && !!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <ListGroup title="My wishlists" lists={mine} onNavigate={onNavigate} add={() => onCreate('list')} addLabel="New wishlist" empty="Make a list of things you would like." />
      <ListGroup title="Gift ideas" lists={gifts} onNavigate={onNavigate} add={() => onCreate('gifts')} addLabel="Gift ideas for someone" empty="Collect ideas for the people you buy for." />
      {shared.length > 0 && <ListGroup title="Shared with me" lists={shared} onNavigate={onNavigate} shared />}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pw:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function Group({ title, add, addLabel, children }: { title: string; add?: () => void; addLabel?: string; children: ReactNode }) {
  const key = `pw-sb-${title}`
  const [open, setOpen] = useState(() => localStorage.getItem(key) !== '0')
  const toggle = () => setOpen(o => { localStorage.setItem(key, o ? '0' : '1'); return !o })
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={toggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />{title}</button>
        {add && <button type="button" className="btn ghost icon-only xs" aria-label={addLabel} title={addLabel} onClick={add}><Icon name="plus" size={14} /></button>}
      </h4>
      {open && children}
    </section>
  )
}

function ListGroup({ title, lists, onNavigate, add, addLabel, empty, shared }: { title: string; lists: List[]; onNavigate: () => void; add?: () => void; addLabel?: string; empty?: string; shared?: boolean }) {
  return (
    <Group title={title} add={add} addLabel={addLabel}>
      {lists.length === 0
        ? <p className="sb-empty">{empty} {add && <button type="button" className="link-btn" onClick={add}>Start one</button>}</p>
        : <ul className="cal-list">{lists.map(l => (
          <li key={l.id} className="cal-row sb-row">
            <NavLink to={shared ? `/shared/${l.id}` : `/lists/${l.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={[l.name, l.recipient, l.owner && shared ? `from ${l.owner}` : ''].filter(Boolean).join(' · ')}>
              <span className={`sb-ico ${l.emoji ? 'emoji' : ''}`}>{l.emoji || <Icon name={KIND_ICON[l.kind] || 'heart'} size={13} />}</span>
              <span className="cal-name ellipsis">{shared ? `${l.owner}: ${l.name}` : l.name}</span>
              {l.drops > 0 && !shared && <span className="badge accent" title={`${l.drops} at your target price`}>{l.drops}</span>}
              {!l.drops && l.wanted > 0 && <span className="sb-count" title={`${l.wanted} wanted`}>{l.wanted}</span>}
              {l.shared && !shared && <Icon name="share" size={12} className="muted" title="Shared" />}
            </NavLink>
          </li>
        ))}</ul>}
    </Group>
  )
}
