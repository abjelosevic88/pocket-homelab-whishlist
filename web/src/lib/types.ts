export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }
export interface Currency { code: string; name: string; decimals: number }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'
export type AppSlug = 'inventory' | 'money' | 'split' | 'contacts'

export interface Settings {
  core_configured: boolean
  /** false while Pocket Core is set but this app is not registered with it */
  core_registered?: boolean
  title: string; home_currency: string; date_format: string; time_format: string
  price_check_hours: number; drop_percent: number; occasion_days: number
  notify_url: string; notify_format: string; notify_drops: boolean; notify_occasions: boolean; notify_hour: number; notify_configured: boolean
  inventory_mode: ConnMode; inventory_via: ConnVia; inventory_detail: string; inventory_open_url: string
  inventory_url: string; inventory_token: string; inventory_location: string; inventory_category: string; inventory_tag: string
  money_mode: ConnMode; money_via: ConnVia; money_detail: string; money_open_url: string
  money_url: string; money_token: string; money_account_id: number; money_category_id: number
  split_mode: ConnMode; split_via: ConnVia; split_detail: string; split_open_url: string
  split_url: string; split_token: string; split_public_url: string; split_group_id: number; split_member_id: number; split_category: string
  contacts_mode: ConnMode; contacts_via: ConnVia; contacts_detail: string; contacts_open_url: string
  contacts_url: string; contacts_token: string; contacts_public_url: string
  bought_inventory: boolean; bought_money: boolean
  max_upload_bytes: number; setup_complete: boolean; base_url: string
}

export type Kind = 'mine' | 'gifts'
export type Status = 'wanted' | 'bought' | 'archived'

export interface List {
  id: number; user_id: number; name: string; description: string; kind: Kind; emoji: string; color: string
  recipient: string; contact_id: number | null; occasion: string; occasion_date: string; budget: number; currency: string
  show_reserved: boolean; archived: boolean; sort_order: number; created_at: string; updated_at: string
  wanted: number; bought: number; total: number; spent: number; shared: boolean; owner: string; cover: string; drops: number; reserved: number
  role: 'owner' | 'viewer'; share_url: string
}

export interface Wish {
  id: number; user_id: number; list_id: number; title: string; url: string; store: string; notes: string; tags: string; image_url: string
  price: number; currency: string; target_price: number; priority: number; quantity: number; status: Status; track_price: boolean
  availability: '' | 'in_stock' | 'out_of_stock' | 'preorder'; checked_at: string; check_error: string; lowest_price: number
  bought_at: string; bought_price: number; bought_currency: string; bought_store: string
  inventory_item_id: number | null; money_tx_id: number | null; split_expense_id: number | null; split_group_id: number | null; handoff_note: string
  source: string; sort_order: number; created_at: string; updated_at: string
  photo: string; list_name: string; list_kind: Kind; recipient: string; at_target: boolean; change: number
  reserved: number; mine: number; mine_note: string; mine_id: number; mine_bought: boolean
}

export interface PricePoint { at: string; price: number; currency: string; source: string }

export interface Share { id: number; list_id: number; kind: 'link' | 'user'; token?: string; user_id: number | null; user_name: string; user_email: string; url?: string; created_at: string }

export interface Reservation {
  id: number; wish_id: number; list_id: number; name: string; quantity: number; note: string; bought: boolean; created_at: string
  wish_title: string; list_name: string; owner: string; photo: string; url: string; price: number; currency: string
}

export interface Occasion {
  kind: 'list' | 'birthday' | 'anniversary'; title: string; date: string; days: number; age?: number
  list_id?: number; list_name?: string; contact_id?: number; person: string; initials?: string; color?: string
  wanted: number; bought: number; budget: number; spent: number; currency: string
}

export interface Product { url: string; title: string; description: string; image: string; store: string; brand: string; price: string; currency: string; availability: string }
export interface Preview { ok: boolean; error?: string; warning?: string; product?: Product; currency?: string; price?: number; store?: string }

export interface HomeData {
  lists: List[]; at_target: Wish[]; cheaper: Wish[]; recent: Wish[]; bought: Wish[]
  occasions: Occasion[]; contacts: boolean; contacts_detail: string; reservations: Reservation[]; shared: List[]
}

export interface Counts { wishes: number; gift_ideas: number; lists: number; at_target: number; tracked: number; value_text: string; occasions: number; next_occasion: string; to_buy_for_others: number }

export interface Contact { id: number; fn: string; nickname: string; emails: string[]; birthday: string; initials: string; color: string; photo_url: string; starred: boolean }
export interface MoneyAccount { id: number; name: string; kind: string; currency: string; everyday: boolean; role: string }
export interface MoneyCategory { id: number; name: string; full_name: string; kind: string }
export interface SplitMember { id: number; user_id: number | null; name: string }
export interface SplitGroup { id: number; name: string; currency: string; archived: boolean; members: SplitMember[] }

export const PRIORITY: Record<number, string> = { 1: 'Nice to have', 2: 'Want', 3: 'Must have' }
export const AVAIL: Record<string, string> = { in_stock: 'In stock', out_of_stock: 'Out of stock', preorder: 'Pre-order' }
export const EMOJIS = ['🎁', '❤️', '🎂', '🎄', '🏠', '🛋️', '👕', '👟', '🎧', '📚', '🎮', '🍳', '🌿', '🧸', '💍', '✈️', '🚲', '🛠️', '💐', '⭐']

/** How a wish's price moved since it was added, in percent (negative = cheaper). */
export function changePct(w: Pick<Wish, 'price' | 'change'>): number {
  const first = w.price - w.change
  return first > 0 && w.change ? Math.round((w.change * 100) / first) : 0
}

export function cap(s: string): string { return s ? s[0].toUpperCase() + s.slice(1) : '' }
