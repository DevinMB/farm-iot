import { useState } from 'react'
import type { Node, SensorType } from '../types'

const SENSOR_ICONS: Record<SensorType, string> = {
  dendrometer: '🌲',
  soil_moisture: '💧',
  temperature: '🌡️',
  solar: '☀️',
  camera: '📷',
  other: '📡',
}

const SENSOR_LABELS: Record<SensorType, string> = {
  dendrometer: 'Dendrometer',
  soil_moisture: 'Soil Moisture',
  temperature: 'Temperature',
  solar: 'Solar',
  camera: 'Camera',
  other: 'Other',
}

interface NodeCardProps {
  node: Node
  latestValue?: number
  onDelete?: () => void
}

export default function NodeCard({ node, latestValue, onDelete }: NodeCardProps) {
  const [copied, setCopied] = useState(false)
  const icon = SENSOR_ICONS[node.sensor_type] ?? '📡'
  const label = SENSOR_LABELS[node.sensor_type] ?? 'Sensor'

  function copyId() {
    navigator.clipboard.writeText(node.id).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <div className="node-card card">
      <div className="node-card-icon">{icon}</div>
      <div className="node-card-info">
        <div className="node-card-name">{node.name}</div>
        <div className="node-card-type">{label}</div>
        <div className="node-card-id">
          <span className="node-id-short" title={node.id}>{node.id.substring(0, 8)}…</span>
          <button className="btn-copy-id" onClick={copyId} title="Copy full Node ID">
            {copied ? '✓' : '⎘'}
          </button>
        </div>
      </div>
      <div className="node-card-value">
        {latestValue !== undefined ? (
          <>
            <span className="node-card-reading">{latestValue.toFixed(2)}</span>
            <span className="node-card-unit">{node.unit}</span>
          </>
        ) : (
          <span className="node-card-no-data">—</span>
        )}
      </div>
      {onDelete && (
        <button
          className="btn-node-delete"
          title="Delete node"
          onClick={() => {
            if (confirm(`Delete node "${node.name}"?\n\nThis cannot be undone.`)) onDelete()
          }}
        >
          ✕
        </button>
      )}
    </div>
  )
}
