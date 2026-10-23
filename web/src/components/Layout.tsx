// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Counts, List } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { ListModal, WishModal } from './forms'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pw-sidebar'

/** Something was added or changed from the shell (or a page): the sidebar counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pw:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pw:changed', h)
    return () => window.removeEventListener('pw:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb (a list's or a wish's name). */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** The lists, for every page that needs them (pickers, the sidebar). */
const ListsCtx = createContext<{ lists: List[]; shared: List[]; reload: () => void }>({ lists: [], shared: [], reload: () => {} })
export const useLists = () => useContext(ListsCtx)

export type CreateKind = 'wish' | 'link' | 'list' | 'gifts'

/** Open a create dialog from anywhere (a page's button). */
export const openCreate = (k: CreateKind) => window.dispatchEvent(new CustomEvent('pw:create', { detail: k }))

/** The create menu: the sidebar's "New" button and the phone's + button. */
export function createItems(onCreate: (k: CreateKind) => void): MenuItem[] {
  return [
    { label: 'Add from a link', icon: 'link', hint: 'L', onSelect: () => onCreate('link') },
    { label: 'Add a wish', icon: 'plus', hint: 'N', onSelect: () => onCreate('wish') },
    'sep',
    { label: 'New wishlist', icon: 'heart', onSelect: () => onCreate('list') },
    { label: 'Gift ideas for someone', icon: 'gift', hint: 'G', onSelect: () => onCreate('gifts') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const [counts, setCounts] = useState<Counts | null>(null)
  const [lists, setLists] = useState<List[]>([])
  const [shared, setShared] = useState<List[]>([])
  const load = useCallback(() => {
    api.get<Counts>(`${V1}/dashboard`).then(setCounts).catch(() => {})
    api.get<List[]>(`${V1}/lists`).then(setLists).catch(() => {})
    api.get<List[]>(`${V1}/shared`).then(setShared).catch(() => {})
  }, [])
  useEffect(() => { load() }, [loc.pathname, load])
  useOnChange(load)

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [modal, setModal] = useState<CreateKind | null>(null)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  // /add?url=…&title=… (the bookmarklet, the phone's share sheet) opens "Add from a link"
  const sharedURL = loc.pathname === '/add' ? (sp.get('url') || sp.get('text') || '') : ''
  useEffect(() => { if (loc.pathname === '/add') setModal('link') }, [loc.pathname])

  const create = (k: CreateKind) => { closeDrawer(); setModal(k) }
  const createRef = useRef(create)
  createRef.current = create
  const currentList = (() => { const m = loc.pathname.match(/^\/lists\/(\d+)/); const id = m ? Number(m[1]) : 0; return lists.find(l => l.id === id) })()

  useEffect(() => {
    const open = () => setShortcuts(true)
    const make = (e: Event) => createRef.current((e as CustomEvent<CreateKind>).detail)
    window.addEventListener('pw:shortcuts', open)
    window.addEventListener('pw:create', make)
    return () => { window.removeEventListener('pw:shortcuts', open); window.removeEventListener('pw:create', make) }
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      const mk = (k: CreateKind) => { e.preventDefault(); createRef.current(k) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': mk('wish'); break
        case 'l': mk('link'); break
        case 'g': mk('gifts'); break
        case 'h': go('/'); break
        case 'w': go('/wishes'); break
        case 'd': go('/wishes?view=drops'); break
        case 'o': go('/occasions'); break
        case 's': go('/shared'); break
        case 'b': go('/wishes?view=bought'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'link', label: 'Add from a link', icon: 'link', hint: 'L', keywords: 'url shop paste product', run: () => create('link') },
    { id: 'wish', label: 'Add a wish', icon: 'plus', hint: 'N', keywords: 'new item', run: () => create('wish') },
    { id: 'list', label: 'New wishlist', icon: 'heart', keywords: 'create', run: () => create('list') },
    { id: 'gifts', label: 'Gift ideas for someone', icon: 'gift', hint: 'G', keywords: 'present birthday christmas', run: () => create('gifts') },
    { id: 'home', label: 'Home', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'all', label: 'All wishes', icon: 'list', hint: 'W', run: () => navigate('/wishes') },
    { id: 'drops', label: 'Price drops', icon: 'trending-down', hint: 'D', keywords: 'cheaper target sale', run: () => navigate('/wishes?view=drops') },
    { id: 'tracked', label: 'Watched prices', icon: 'chart', keywords: 'tracked price check', run: () => navigate('/wishes?view=tracked') },
    { id: 'bought', label: 'Bought', icon: 'check-circle', hint: 'B', keywords: 'purchased history', run: () => navigate('/wishes?view=bought') },
    { id: 'occasions', label: 'Occasions', icon: 'calendar', hint: 'O', keywords: 'birthday upcoming gifts', run: () => navigate('/occasions') },
    { id: 'shared', label: 'Shared with me', icon: 'users', hint: 'S', keywords: 'reservations family', run: () => navigate('/shared') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'inventory money split contacts core integrations', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  const closeModal = () => {
    setModal(null)
    if (loc.pathname === '/add') { setSp(new URLSearchParams(), { replace: true }); navigate('/', { replace: true }) }
  }
  const done = () => { closeModal(); announceChange() }
  const defaultList = currentList?.id
  return (
    <CrumbCtx.Provider value={setCrumb}>
      <ListsCtx.Provider value={{ lists, shared, reload: load }}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onCreate={create} onNavigate={closeDrawer} counts={counts} lists={lists} shared={shared} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(create)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} lists={lists} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
          {(modal === 'wish' || modal === 'link') && <WishModal lists={lists} listId={defaultList} fromLink={modal === 'link'} initialURL={sharedURL} initialTitle={loc.pathname === '/add' ? sp.get('title') || '' : ''}
            onClose={closeModal} onSaved={w => { done(); navigate(`/lists/${w.list_id}`) }} />}
          {(modal === 'list' || modal === 'gifts') && <ListModal kind={modal === 'gifts' ? 'gifts' : 'mine'} onClose={closeModal} onSaved={l => { done(); navigate(`/lists/${l.id}`) }} />}
        </div>
      </ListsCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Home', '/'], lists: ['Lists', '/'], wishes: ['Wishes', '/wishes'], occasions: ['Occasions', '/occasions'], shared: ['Shared with me', '/shared'], add: ['Home', '/'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pw:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
