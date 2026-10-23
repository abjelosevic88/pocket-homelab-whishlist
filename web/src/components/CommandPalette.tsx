// ⌘K / "/" — jump to a wish, a list or a page, or run a command from the keyboard.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { List, Wish } from '../lib/types'
import { Icon, KIND_ICON } from './icons'
import { useApp } from '../lib/app-context'
import { decimalsOf, fmtMoney } from '../lib/format'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

export function CommandPalette({ onClose, commands, lists }: { onClose: () => void; commands: PaletteCommand[]; lists: List[] }) {
  const navigate = useNavigate()
  const { currencies } = useApp()
  const [q, setQ] = useState('')
  const [hi, setHi] = useState(0)
  const [wishes, setWishes] = useState<Wish[]>([])
  const list = useRef<HTMLDivElement>(null)
  useEffect(() => { api.get<Wish[]>(`${V1}/wishes?status=all&sort=added`).then(setWishes).catch(() => {}) }, [])

  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    const n = q.trim().toLowerCase()
    const words = n.split(/\s+/).filter(Boolean)
    const hit = (text: string) => words.every(w => text.toLowerCase().includes(w))
    const cmds = commands.filter(c => !n || hit(`${c.label} ${c.keywords || ''}`)).map(c => ({ key: c.id, group: 'Commands', icon: <Icon name={c.icon} size={16} />, label: c.label, hint: c.hint, run: c.run }))
    const ws = wishes.filter(w => w.status !== 'archived' && (!n || hit(`${w.title} ${w.store} ${w.tags} ${w.list_name} ${w.recipient} ${w.notes}`))).slice(0, n ? 30 : 6)
      .map(w => ({ key: `w${w.id}`, group: 'Wishes', icon: w.photo ? <img src={w.photo} alt="" className="pal-thumb" /> : <Icon name={KIND_ICON[w.list_kind] || 'gift'} size={16} />, label: w.title,
        sub: [w.list_name, w.price ? fmtMoney(w.price, w.currency, decimalsOf(w.currency, currencies)) : '', w.status === 'bought' ? 'bought' : '', w.at_target ? 'at target' : ''].filter(Boolean).join(' · '), run: () => navigate(`/wishes/${w.id}`) }))
    const ls = lists.filter(l => !n || hit(`${l.name} ${l.recipient} ${l.occasion} list`)).map(l => ({ key: `l${l.id}`, group: 'Lists', icon: l.emoji ? <span>{l.emoji}</span> : <Icon name={KIND_ICON[l.kind]} size={16} />, label: l.name, sub: `${l.wanted} wanted${l.recipient ? ' · for ' + l.recipient : ''}`, run: () => navigate(`/lists/${l.id}`) }))
    if (n) out.push(...ws, ...ls, ...cmds)
    else out.push(...cmds, ...ls, ...ws)
    return out
  }, [q, wishes, lists, commands, navigate, currencies])
  useEffect(() => setHi(0), [q])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Search and commands">
        <div className="pal-input">
          <Icon name="search" size={18} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a wish, a shop, a list, or run a command…" aria-label="Search"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {rows.length === 0 && <div className="pal-empty">Nothing matches.</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow"><span className="pal-label">{r.label}</span>{r.sub && <span className="pal-sub">{r.sub}</span>}</span>
                  {r.hint && <kbd>{r.hint}</kbd>}
                  {i === hi && <Icon name="chevron-right" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
      </div>
    </div>, document.body)
}
