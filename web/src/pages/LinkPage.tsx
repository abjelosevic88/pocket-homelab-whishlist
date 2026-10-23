// /s/:token — a shared wishlist opened from its link: no account needed.
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { List, Wish } from '../lib/types'
import { Loading, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { ViewerList } from './Shared'
import { WishCard } from '../components/wish'

export default function LinkPage() {
  const { token } = useParams()
  const { user } = useApp()
  const { data, error, loading, reload } = useAsync(() => api.get<{ list: List; wishes: Wish[]; owner?: boolean }>(`${V1}/s/${token}`), [token])
  const [open, setOpen] = useState<Wish | null>(null)
  return (
    <div className="public">
      <div className="public-inner">
        <div className="public-top"><img src="/favicon.svg" alt="" /><span>Pocket Wishlist</span><span className="spacer" />{user ? <Link to="/">Your lists</Link> : null}</div>
        {loading && !data ? <Loading /> : error ? (
          <div className="callout warn"><Icon name="alert" size={15} /><span>{error === 'this link does not work any more' || error === 'Not Found' ? 'This link does not work any more. Ask for a new one.' : error}</span></div>
        ) : data && (data.owner ? <>
          <div className="callout info public-note"><Icon name="eye-off" size={15} /><span>This is your own list, as you see it: who reserved what stays a surprise. Send this page’s address to the people you want to share it with.</span></div>
          <div className="list-hero"><span className="list-emoji">{data.list.emoji || '🎁'}</span><div><h1>{data.list.name}</h1><div className="meta"><span>{data.wishes.length} wishes</span></div></div></div>
          <div className="wish-grid">{data.wishes.filter(w => w.status === 'wanted').map(w => <WishCard key={w.id} w={w} onOpen={() => { window.location.href = `/wishes/${w.id}` }} />)}</div>
        </> : <>
          {!user && <p className="muted small public-note">Pick something and say “I’ll get this” — the others see it is taken, and {data.list.owner || 'the owner'} never sees who. This browser remembers your choices.</p>}
          <ViewerList list={data.list} wishes={data.wishes} base={`${V1}/s/${token}/wishes`} anonymous={!user} open={open} setOpen={setOpen} onChanged={reload} />
        </>)}
      </div>
    </div>
  )
}
