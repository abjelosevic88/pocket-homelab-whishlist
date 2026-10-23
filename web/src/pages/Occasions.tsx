import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, weekday } from '../lib/format'
import type { Occasion } from '../lib/types'
import { Card, CoreHint, Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { useMoney } from '../components/wish'

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

export default function Occasions() {
  const { settings } = useApp()
  const { data, loading, error } = useAsync(() => api.get<{ occasions: Occasion[]; contacts: boolean; contacts_detail: string }>(`${V1}/occasions?days=120`), [])
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return null
  return (
    <>
      <PageHeader title="Occasions" sub="Birthdays from Pocket Contacts and your gift lists with a date, for the next four months." />
      {!data.contacts && <div className="callout info mb"><Icon name="cake" size={15} /><div>Birthdays and anniversaries come from Pocket Contacts, which is not connected{data.contacts_detail ? ` (${data.contacts_detail})` : ''}. <Link to="/settings/apps">Connected apps</Link>{!settings?.core_configured && <CoreHint />}</div></div>}
      {data.occasions.length === 0 ? <Card><Empty icon="calendar" title="Nothing coming up">Give a gift list an occasion and a date, or connect Pocket Contacts for birthdays.</Empty></Card>
        : <Card>{data.occasions.map((o, i) => <OccasionRow key={i} o={o} />)}</Card>}
    </>
  )
}

/** One occasion: date tile, who and what, and how far the gifts are. */
export function OccasionRow({ o }: { o: Occasion }) {
  const money = useMoney()
  const [, m, d] = o.date.split('-').map(Number)
  const soon = o.days <= 7
  const when = o.days === 0 ? 'today' : o.days === 1 ? 'tomorrow' : o.days < 14 ? `in ${o.days} days` : `${weekday(o.date)} ${fmtDate(o.date, false)}`
  const who = o.person || o.list_name || ''
  const status = !o.list_id ? 'No gift ideas yet'
    : o.bought ? `${o.bought} bought${o.wanted ? `, ${o.wanted} idea${o.wanted === 1 ? '' : 's'} left` : ''}`
    : o.wanted ? `${o.wanted} idea${o.wanted === 1 ? '' : 's'}, nothing bought yet` : 'The list is empty'
  const budget = o.budget ? ` · ${money(o.spent, o.currency)} of ${money(o.budget, o.currency)}` : ''
  const to = o.list_id ? `/lists/${o.list_id}` : `/lists/new?kind=gifts&recipient=${encodeURIComponent(o.person)}&contact=${o.contact_id || ''}&occasion=${encodeURIComponent(o.title)}&date=${o.date}`
  return (
    <Link className="occ-row" to={to}>
      <span className={`occ-date ${soon ? 'soon' : ''}`}><span className="occ-m">{MONTHS[m - 1]}</span><span className="occ-d">{d}</span></span>
      <span style={{ minWidth: 0 }}>
        <b>{who}{who && ' · '}{o.title}{o.age ? ` (${o.age})` : ''}</b>
        <div className="s">{status}{budget}</div>
      </span>
      <span className={`occ-when ${soon ? 'soon' : ''}`}>{when}</span>
    </Link>
  )
}
