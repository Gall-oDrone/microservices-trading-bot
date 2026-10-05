import { Link } from 'react-router'
import { Empty } from '../components/ui'
import { usePageTitle } from '../lib/usePageTitle'

export function NotFoundPage() {
  usePageTitle('Not found')
  return (
    <div className="card">
      <Empty title="Page not found">
        <Link to="/">Back to forward tests</Link>
      </Empty>
    </div>
  )
}
