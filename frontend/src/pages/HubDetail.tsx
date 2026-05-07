import { useState, useEffect, type FormEvent } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import Layout from '../components/Layout'
import HubCard from '../components/HubCard'
import NodeCard from '../components/NodeCard'
import SensorChart from '../components/SensorChart'
import AddModal from '../components/AddModal'
import { apiHub, apiHubNodes, apiCreateNode, apiDeleteNode, apiDeleteHub, downloadProvisionScript } from '../api/client'
import FlashESP32 from '../components/FlashESP32'
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

  const [piArch, setPiArch] = useState('armv6')
  const [showSetupSteps, setShowSetupSteps] = useState(false)

  const [showAddNode, setShowAddNode] = useState(false)
  const [nodeName, setNodeName] = useState('')
  const [nodeSensorType, setNodeSensorType] = useState<SensorType>('temperature')
  const [nodeUnit, setNodeUnit] = useState(DEFAULT_UNITS['temperature'])
  const [addingNode, setAddingNode] = useState(false)
  const [addNodeError, setAddNodeError] = useState<string | null>(null)
  const [newNodeId, setNewNodeId] = useState<string | null>(null)
  const [copiedNodeId, setCopiedNodeId] = useState(false)

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
      setNewNodeId(node.id)
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

  function closeAddNode() {
    setShowAddNode(false)
    setAddNodeError(null)
    setNewNodeId(null)
    setCopiedNodeId(false)
  }

  function copyNodeId() {
    if (!newNodeId) return
    navigator.clipboard.writeText(newNodeId).then(() => {
      setCopiedNodeId(true)
      setTimeout(() => setCopiedNodeId(false), 2000)
    })
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
        <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
          <select
            className="form-select"
            value={piArch}
            onChange={(e) => setPiArch(e.target.value)}
            style={{ width: 'auto' }}
            title="Select your Raspberry Pi model"
          >
            <option value="armv6">Pi Zero / Pi 1 (ARMv6)</option>
            <option value="armv7">Pi 2 / Pi 3 32-bit (ARMv7)</option>
            <option value="arm64">Pi 3 / Pi 4 / Pi 5 64-bit (ARM64)</option>
          </select>
          <button
            className="btn btn-outline"
            onClick={() => hubId && downloadProvisionScript(hubId, piArch)}
          >
            Download Setup Script
          </button>
          <button
            className="btn btn-ghost"
            onClick={() => setShowSetupSteps((v) => !v)}
            title="Show setup instructions"
          >
            {showSetupSteps ? 'Hide Guide' : 'How to'}
          </button>
          <button
            className="btn btn-danger"
            onClick={async () => {
              if (!hubId || !hub) return
              if (!confirm(`Delete "${hub.name}"?\n\nThis will permanently delete the hub and all its sensor nodes. This cannot be undone.`)) return
              try {
                await apiDeleteHub(hubId)
                navigate('/')
              } catch (err) {
                alert(err instanceof Error ? err.message : 'Failed to delete hub')
              }
            }}
          >
            Delete Hub
          </button>
        </div>
      </div>

      {/* Setup instructions */}
      {showSetupSteps && <div className="setup-steps card">
        <div className="setup-steps-title">How to set up a Raspberry Pi hub</div>
        <ol className="setup-steps-list">
          <li>Select your Pi model from the dropdown above, then click <strong>Download Setup Script</strong></li>
          <li>Copy the script to your Pi:<br/><code>scp farm-setup.sh pi@&lt;pi-ip&gt;:~/</code></li>
          <li>SSH in and run it as root:<br/><code>ssh pi@&lt;pi-ip&gt;</code><br/><code>sudo bash farm-setup.sh</code></li>
          <li>If the script says <em>"SPI was just enabled — reboot required"</em>, run <code>sudo reboot</code> and the daemon will start on its own</li>
          <li>Verify it&apos;s running:<br/><code>journalctl -u farmsense-hub -f</code></li>
        </ol>
        <div className="setup-lora-note">
          <strong>LoRa HAT defaults</strong> (Waveshare SX1262): RESET=GPIO18 · BUSY=GPIO20 · DIO1=GPIO16 · CS=GPIO21 · RXEN=GPIO6 · 915 MHz · SF7 · BW125.
          Edit <code>/etc/farmsense/config.env</code> if your HAT uses different pins, then <code>sudo systemctl restart farmsense-hub</code>.
        </div>
      </div>}

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
              <NodeCard
                key={node.id}
                node={node}
                onDelete={async () => {
                  try {
                    await apiDeleteNode(node.id)
                    setNodes((prev) => prev.filter((n) => n.id !== node.id))
                  } catch (err) {
                    alert(err instanceof Error ? err.message : 'Failed to delete node')
                  }
                }}
              />
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
          title={newNodeId ? 'Node Created' : 'Add Sensor Node'}
          onClose={closeAddNode}
          onSubmit={newNodeId ? (e) => { e.preventDefault(); closeAddNode() } : handleAddNode}
          submitting={addingNode}
          error={addNodeError}
          submitLabel={newNodeId ? 'Done' : 'Add Node'}
        >
          {newNodeId ? (
            <div className="node-created">
              <div className="node-created-check">✓</div>
              <p className="node-created-desc">Node created — flash your ESP32 to configure it:</p>
              <FlashESP32 nodeId={newNodeId} />
              <div className="node-created-divider">or configure manually</div>
              <div className="node-id-row">
                <code className="node-id">{newNodeId}</code>
                <button type="button" className="btn btn-ghost btn-sm" onClick={copyNodeId}>
                  {copiedNodeId ? 'Copied!' : 'Copy'}
                </button>
              </div>
              <p className="node-created-hint">
                Serial monitor: <code>set node_id {newNodeId}</code>
              </p>
            </div>
          ) : (
            <>
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
            </>
          )}
        </AddModal>
      )}
    </Layout>
  )
}
