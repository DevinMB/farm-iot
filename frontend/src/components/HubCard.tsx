import type { Hub } from '../types'

interface HubCardProps {
  hub: Hub
  onClick?: () => void
}

function formatLastSeen(ts: string | null): string {
  if (!ts) return 'Never'
  const d = new Date(ts)
  const now = new Date()
  const diff = Math.floor((now.getTime() - d.getTime()) / 1000)
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

export default function HubCard({ hub, onClick }: HubCardProps) {
  const nodeCount = hub.nodes?.length ?? 0

  return (
    <div className="hub-card card" onClick={onClick} role={onClick ? 'button' : undefined} tabIndex={onClick ? 0 : undefined} onKeyDown={onClick ? (e) => { if (e.key === 'Enter') onClick() } : undefined}>
      <div className="hub-card-header">
        <div className="hub-card-name">{hub.name}</div>
        <span className={hub.online ? 'badge badge-online' : 'badge badge-offline'}>
          {hub.online ? 'Online' : 'Offline'}
        </span>
      </div>
      <div className="hub-card-meta">
        <div className="hub-card-stat">
          <span className="hub-card-stat-label">Nodes</span>
          <span className="hub-card-stat-value">{nodeCount}</span>
        </div>
        <div className="hub-card-stat">
          <span className="hub-card-stat-label">Last seen</span>
          <span className="hub-card-stat-value">{formatLastSeen(hub.last_seen)}</span>
        </div>
      </div>
      {onClick && (
        <div className="hub-card-footer">
          <span className="hub-card-link">View details →</span>
        </div>
      )}
    </div>
  )
}
