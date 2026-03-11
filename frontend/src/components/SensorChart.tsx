import { useState, useEffect } from 'react'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from 'recharts'
import type { SensorType } from '../types'
import { apiNodeReadings } from '../api/client'

type Range = '1h' | '24h' | '7d' | '30d'

const LINE_COLORS: Record<string, string> = {
  dendrometer: '#16a34a',
  soil_moisture: '#2563eb',
  temperature: '#f59e0b',
  solar: '#f97316',
  camera: '#8b5cf6',
  other: '#8b5cf6',
}

function formatTick(ts: string, range: Range): string {
  const d = new Date(ts)
  if (range === '1h' || range === '24h') {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

interface SensorChartProps {
  nodeId: string
  sensorType: SensorType | string
  unit: string
  nodeName?: string
}

export default function SensorChart({ nodeId, sensorType, unit, nodeName }: SensorChartProps) {
  const [range, setRange] = useState<Range>('24h')
  const [data, setData] = useState<Array<{ timestamp: string; value: number }>>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    apiNodeReadings(nodeId, range)
      .then((res) => {
        if (!cancelled) {
          setData(res.readings)
          setLoading(false)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
          setLoading(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [nodeId, range])

  const color = LINE_COLORS[sensorType] ?? '#8b5cf6'
  const ranges: Range[] = ['1h', '24h', '7d', '30d']

  const chartData = data.map((r) => ({
    ...r,
    label: formatTick(r.timestamp, range),
  }))

  return (
    <div className="sensor-chart card">
      <div className="sensor-chart-header">
        <div className="sensor-chart-title">{nodeName ?? nodeId}</div>
        <div className="range-tabs">
          {ranges.map((r) => (
            <button
              key={r}
              className={`range-tab ${range === r ? 'active' : ''}`}
              onClick={() => setRange(r)}
            >
              {r.toUpperCase()}
            </button>
          ))}
        </div>
      </div>

      {sensorType === 'dendrometer' && (
        <div className="chart-note">
          Circumference measurement — values represent tree diameter change in mm
        </div>
      )}

      {loading && <div className="chart-state">Loading...</div>}
      {!loading && error && <div className="chart-state chart-error">{error}</div>}
      {!loading && !error && chartData.length === 0 && (
        <div className="chart-state chart-empty">No data yet</div>
      )}
      {!loading && !error && chartData.length > 0 && (
        <ResponsiveContainer width="100%" height={200}>
          <LineChart data={chartData} margin={{ top: 8, right: 16, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
            <XAxis
              dataKey="label"
              tick={{ fontSize: 11, fill: '#6b7280' }}
              interval="preserveStartEnd"
            />
            <YAxis
              tick={{ fontSize: 11, fill: '#6b7280' }}
              unit={` ${unit}`}
              width={60}
            />
            <Tooltip
              formatter={(val: number) => [`${val.toFixed(2)} ${unit}`, 'Value']}
              labelFormatter={(label: string) => label}
              contentStyle={{ borderRadius: '8px', border: '1px solid #e5e7eb', fontSize: '12px' }}
            />
            <Line
              type="monotone"
              dataKey="value"
              stroke={color}
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
          </LineChart>
        </ResponsiveContainer>
      )}
    </div>
  )
}
