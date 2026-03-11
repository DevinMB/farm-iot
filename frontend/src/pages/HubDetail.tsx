import { useState, useEffect, type FormEvent } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import Layout from '../components/Layout'
import HubCard from '../components/HubCard'
import NodeCard from '../components/NodeCard'
import SensorChart from '../components/SensorChart'
import AddModal from '../components/AddModal'
import { apiHub, apiHubNodes, apiCreateNode, downloadProvisionScript } from '../api/client'
import type { Hub, Node, SensorType } from '../types'

const SENSOR_TYPES: SensorType[] = [
  'dendrometer',
  'soil_moisture',
  'temperature',
  'solar',
  'camera',
  'other',
]

const SENSOR_LABELS: Record<SensorType, string> = {
  dendrometer: 'Dendrometer',
  soil_moisture: 'Soil Moisture',
  temperature: 'Temperature',
  solar: 'Solar',
  camera: 'Camera',
  other: 'Other',
}

const DEFAULT_UNITS: Record<SensorType, string> = {
  dendrometer: 'mm',
  soil_moisture: '%',
  temperature: '°C',
  solar: 'W/m²',
  camera: '',
  other: '',
}

export default function HubDetail() {
  const { hubId } = useParams<{ hubId: string }>()
  const navigate = useNavigate()

  const [hub, setHub] = useState<Hub | null>(null)
  const [nodes, setNodes] = useState<Node[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [showAddNode, setShowAddNode] = useState(false)
  const [nodeName, setNodeName] = useState('')
  const [nodeSensorType, setNodeSensorType] = useState<SensorType>('temperature')
  const [nodeUnit, setNodeUnit] = useState(DEFAULT_UNITS['temperature'])
  const [addingNode, setAddingNode] = useState(false)
  const [addNodeError, setAddNodeError] = useState<string | null>(null)

  useEffect(() => {
    if (!hubId) return
    setLoading(true)
    Promise.all([apiHub(hubId), apiHubNodes(hubId)])
      .then(([hubRes, nodesRes]) => {
        setHub(hubRes)
        setNodes(nodesRes.nodes)
        setLoading(false)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : 'Failed to load hub')
        setLoading(false)
      })
  }, [hubId])

  async function handleAddNode(e: FormEvent) {
    e.preventDefault()
    if (!hubId) return
    setAddNodeError(null)
    setAddingNode(true)
    try {
      const node = await apiCreateNode(hubId, nodeName, nodeSensorType, nodeUnit)
      setNodes((prev) => [...prev, node])
      setShowAddNode(false)
      setNodeName('')
      setNodeSensorType('temperature')
      setNodeUnit(DEFAULT_UNITS['temperature'])
    } catch (err: unknown) {
      setAddNodeError(err instanceof Error ? err.message : 'Failed to add node')
    } finally {
      setAddingNode(false)
    }
  }

  function handleSensorTypeChange(st: SensorType) {
    setNodeSensorType(st)
    setNodeUnit(DEFAULT_UNITS[st])
  }

  if (loading) {
    return (
      <Layout>
        <div className="page-loading">Loading hub…</div>
      </Layout>
    )
  }

  if (error || !hub) {
    return (
      <Layout>
        <div className="page-error">
          {error ?? 'Hub not found'}
          <button className="btn btn-ghost" onClick={() => navigate('/')} style={{ marginLeft: '1rem' }}>
            Back to Dashboard
          </button>
        </div>
      </Layout>
    )
  }

  return (
    <Layout>
      {/* Back nav */}
      <div className="back-nav">
        <button className="btn btn-ghost" onClick={() => navigate('/')}>
          ← Dashboard
        </button>
      </div>

      {/* Hub header */}
      <div className="hub-detail-header card">
        <div className="hub-detail-header-left">
          <h1 className="hub-detail-name">{hub.name}</h1>
          <div className="hub-detail-meta">
            <span className={hub.online ? 'badge badge-online' : 'badge badge-offline'}>
              {hub.online ? 'Online' : 'Offline'}
            </span>
            {hub.last_seen && (
              <span className="hub-detail-last-seen">
                Last seen: {new Date(hub.last_seen).toLocaleString()}
              </span>
            )}
          </div>
        </div>
        <button
          className="btn btn-outline"
          onClick={() => hubId && downloadProvisionScript(hubId)}
        >
          Download Setup Script
        </button>
      </div>

      {/* Hub summary card (reuse) */}
      <div className="hub-summary-wrap">
        <HubCard hub={{ ...hub, nodes }} />
      </div>

      {/* Nodes section */}
      <div className="section">
        <div className="section-header">
          <h2 className="section-title">Sensor Nodes</h2>
          <button className="btn btn-primary" onClick={() => setShowAddNode(true)}>
            + Add Node
          </button>
        </div>

        {nodes.length === 0 ? (
          <div className="empty-state card">
            <div className="empty-icon">📡</div>
            <div className="empty-text">No sensor nodes yet — add a node to start collecting data</div>
          </div>
        ) : (
          <div className="nodes-grid">
            {nodes.map((node) => (
              <NodeCard key={node.id} node={node} />
            ))}
          </div>
        )}
      </div>

      {/* Sensor charts section */}
      {nodes.length > 0 && (
        <div className="section">
          <h2 className="section-title">Sensor Charts</h2>
          <div className="charts-grid">
            {nodes.map((node) => (
              <SensorChart
                key={node.id}
                nodeId={node.id}
                sensorType={node.sensor_type}
                unit={node.unit}
                nodeName={node.name}
              />
            ))}
          </div>
        </div>
      )}

      {/* Add Node Modal */}
      {showAddNode && (
        <AddModal
          title="Add Sensor Node"
          onClose={() => { setShowAddNode(false); setAddNodeError(null) }}
          onSubmit={handleAddNode}
          submitting={addingNode}
          error={addNodeError}
          submitLabel="Add Node"
        >
          <div className="form-group">
            <label className="form-label">Node name</label>
            <input
              type="text"
              className="form-input"
              value={nodeName}
              onChange={(e) => setNodeName(e.target.value)}
              required
              placeholder="Tree A Dendrometer"
            />
          </div>
          <div className="form-group">
            <label className="form-label">Sensor type</label>
            <select
              className="form-select"
              value={nodeSensorType}
              onChange={(e) => handleSensorTypeChange(e.target.value as SensorType)}
            >
              {SENSOR_TYPES.map((st) => (
                <option key={st} value={st}>{SENSOR_LABELS[st]}</option>
              ))}
            </select>
          </div>
          <div className="form-group">
            <label className="form-label">Unit</label>
            <input
              type="text"
              className="form-input"
              value={nodeUnit}
              onChange={(e) => setNodeUnit(e.target.value)}
              placeholder="mm"
            />
          </div>
        </AddModal>
      )}
    </Layout>
  )
}
