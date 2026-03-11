import { useState, useEffect, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import Layout from '../components/Layout'
import HubCard from '../components/HubCard'
import AddModal from '../components/AddModal'
import {
  apiFarms,
  apiCreateFarm,
  apiFarmStats,
  apiFarmHubs,
  apiCreateHub,
  apiCreateNode,
  downloadProvisionScript,
} from '../api/client'
import type { Farm, Hub, FarmStats, SensorType } from '../types'

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

export default function Dashboard() {
  const { user } = useAuth()
  const navigate = useNavigate()

  const [farms, setFarms] = useState<Farm[]>([])
  const [selectedFarmId, setSelectedFarmId] = useState<string | null>(null)
  const [stats, setStats] = useState<FarmStats | null>(null)
  const [hubs, setHubs] = useState<Hub[]>([])
  const [loadingFarms, setLoadingFarms] = useState(true)
  const [loadingHubs, setLoadingHubs] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Create farm form
  const [showCreateFarm, setShowCreateFarm] = useState(false)
  const [farmName, setFarmName] = useState('')
  const [farmLocation, setFarmLocation] = useState('')
  const [creatingFarm, setCreatingFarm] = useState(false)
  const [createFarmError, setCreateFarmError] = useState<string | null>(null)

  // Add hub modal
  const [showAddHub, setShowAddHub] = useState(false)
  const [hubName, setHubName] = useState('')
  const [addingHub, setAddingHub] = useState(false)
  const [addHubError, setAddHubError] = useState<string | null>(null)

  // Pi builder
  const [provisionHubId, setProvisionHubId] = useState<string>('')

  // ESP32 node builder
  const [esp32HubId, setEsp32HubId] = useState<string>('')
  const [esp32SensorType, setEsp32SensorType] = useState<SensorType>('temperature')
  const [esp32NodeName, setEsp32NodeName] = useState('')
  const [esp32Unit, setEsp32Unit] = useState(DEFAULT_UNITS['temperature'])
  const [registeringNode, setRegisteringNode] = useState(false)
  const [registeredNodeId, setRegisteredNodeId] = useState<string | null>(null)
  const [esp32Error, setEsp32Error] = useState<string | null>(null)
  const [copiedNodeId, setCopiedNodeId] = useState(false)

  // Load farms
  useEffect(() => {
    setLoadingFarms(true)
    apiFarms()
      .then((res) => {
        setFarms(res.farms)
        if (res.farms.length > 0) {
          setSelectedFarmId(res.farms[0].id)
        }
        setLoadingFarms(false)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : 'Failed to load farms')
        setLoadingFarms(false)
      })
  }, [])

  // Load hubs + stats when farm changes
  useEffect(() => {
    if (!selectedFarmId) return
    setLoadingHubs(true)
    Promise.all([apiFarmHubs(selectedFarmId), apiFarmStats(selectedFarmId)])
      .then(([hubsRes, statsRes]) => {
        setHubs(hubsRes.hubs)
        setStats(statsRes)
        if (hubsRes.hubs.length > 0) {
          setProvisionHubId(hubsRes.hubs[0].id)
          setEsp32HubId(hubsRes.hubs[0].id)
        } else {
          setProvisionHubId('')
          setEsp32HubId('')
        }
        setLoadingHubs(false)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : 'Failed to load hubs')
        setLoadingHubs(false)
      })
  }, [selectedFarmId])

  const selectedFarm = farms.find((f) => f.id === selectedFarmId)

  async function handleCreateFarm(e: FormEvent) {
    e.preventDefault()
    setCreateFarmError(null)
    setCreatingFarm(true)
    try {
      const farm = await apiCreateFarm(farmName, farmLocation)
      setFarms((prev) => [...prev, farm])
      setSelectedFarmId(farm.id)
      setShowCreateFarm(false)
      setFarmName('')
      setFarmLocation('')
    } catch (err: unknown) {
      setCreateFarmError(err instanceof Error ? err.message : 'Failed to create farm')
    } finally {
      setCreatingFarm(false)
    }
  }

  async function handleAddHub(e: FormEvent) {
    e.preventDefault()
    if (!selectedFarmId) return
    setAddHubError(null)
    setAddingHub(true)
    try {
      const hub = await apiCreateHub(selectedFarmId, hubName)
      setHubs((prev) => [...prev, hub])
      if (!provisionHubId) setProvisionHubId(hub.id)
      if (!esp32HubId) setEsp32HubId(hub.id)
      setShowAddHub(false)
      setHubName('')
    } catch (err: unknown) {
      setAddHubError(err instanceof Error ? err.message : 'Failed to add hub')
    } finally {
      setAddingHub(false)
    }
  }

  async function handleRegisterNode(e: FormEvent) {
    e.preventDefault()
    if (!esp32HubId) return
    setEsp32Error(null)
    setRegisteringNode(true)
    setRegisteredNodeId(null)
    try {
      const node = await apiCreateNode(esp32HubId, esp32NodeName, esp32SensorType, esp32Unit)
      setRegisteredNodeId(node.id)
      setEsp32NodeName('')
    } catch (err: unknown) {
      setEsp32Error(err instanceof Error ? err.message : 'Failed to register node')
    } finally {
      setRegisteringNode(false)
    }
  }

  function handleSensorTypeChange(st: SensorType) {
    setEsp32SensorType(st)
    setEsp32Unit(DEFAULT_UNITS[st])
  }

  function copyNodeId() {
    if (!registeredNodeId) return
    navigator.clipboard.writeText(registeredNodeId).then(() => {
      setCopiedNodeId(true)
      setTimeout(() => setCopiedNodeId(false), 2000)
    })
  }

  if (loadingFarms) {
    return (
      <Layout>
        <div className="page-loading">Loading your farms…</div>
      </Layout>
    )
  }

  if (error) {
    return (
      <Layout>
        <div className="page-error">{error}</div>
      </Layout>
    )
  }

  // No farms state
  if (farms.length === 0) {
    return (
      <Layout>
        <div className="empty-farm-page">
          <div className="hero-banner">
            <div className="hero-content">
              <h1 className="hero-title">Welcome to FarmSense</h1>
              <p className="hero-subtitle">Set up your first farm to start monitoring</p>
            </div>
          </div>
          <div className="empty-farm-wrap">
            <div className="card empty-farm-card">
              <h2 className="section-title">Create your first farm</h2>
              <p className="section-desc">Enter your farm details to get started.</p>
              <form onSubmit={handleCreateFarm} className="create-farm-form">
                <div className="form-group">
                  <label htmlFor="farm-name" className="form-label">Farm name</label>
                  <input
                    id="farm-name"
                    type="text"
                    className="form-input"
                    value={farmName}
                    onChange={(e) => setFarmName(e.target.value)}
                    required
                    placeholder="My Farm"
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="farm-location" className="form-label">Location</label>
                  <input
                    id="farm-location"
                    type="text"
                    className="form-input"
                    value={farmLocation}
                    onChange={(e) => setFarmLocation(e.target.value)}
                    required
                    placeholder="Napa Valley, CA"
                  />
                </div>
                {createFarmError && <div className="form-error">{createFarmError}</div>}
                <button type="submit" className="btn btn-primary" disabled={creatingFarm}>
                  {creatingFarm ? 'Creating…' : 'Create farm'}
                </button>
              </form>
            </div>
          </div>
        </div>
      </Layout>
    )
  }

  return (
    <Layout>
      {/* Hero banner */}
      <div className="hero-banner">
        <div className="hero-content">
          <h1 className="hero-title">
            Welcome back, {selectedFarm?.name ?? user?.name ?? 'Farmer'}!
          </h1>
          <p className="hero-subtitle">
            {selectedFarm?.location ? `📍 ${selectedFarm.location}` : 'Monitor your farm sensors in real time'}
          </p>
        </div>
        <button className="btn btn-ghost-light" onClick={() => setShowCreateFarm(true)}>
          + New Farm
        </button>
      </div>

      <div className="dashboard-body">
        {/* Farm selector */}
        {farms.length > 1 && (
          <div className="farm-tabs">
            {farms.map((f) => (
              <button
                key={f.id}
                className={`farm-tab ${f.id === selectedFarmId ? 'active' : ''}`}
                onClick={() => setSelectedFarmId(f.id)}
              >
                {f.name}
              </button>
            ))}
          </div>
        )}

        {/* Stats row */}
        {stats && (
          <div className="stats-row">
            <div className="stat-card card">
              <div className="stat-value">{stats.hub_count}</div>
              <div className="stat-label">Total Hubs</div>
            </div>
            <div className="stat-card card">
              <div className="stat-value">{stats.node_count}</div>
              <div className="stat-label">Total Nodes</div>
            </div>
            <div className="stat-card card">
              <div className="stat-value online-value">{stats.online_hubs}</div>
              <div className="stat-label">Online Hubs</div>
            </div>
            <div className="stat-card card">
              <div className="stat-value">{Object.keys(stats.latest_readings).length}</div>
              <div className="stat-label">Active Sensors</div>
            </div>
          </div>
        )}

        {/* Hubs section */}
        <div className="section">
          <div className="section-header">
            <h2 className="section-title">Hubs</h2>
            <button className="btn btn-primary" onClick={() => setShowAddHub(true)}>
              + Add Hub
            </button>
          </div>

          {loadingHubs ? (
            <div className="loading-inline">Loading hubs…</div>
          ) : hubs.length === 0 ? (
            <div className="empty-state card">
              <div className="empty-icon">📡</div>
              <div className="empty-text">No hubs yet — add your first hub to get started</div>
            </div>
          ) : (
            <div className="hubs-grid">
              {hubs.map((hub) => (
                <HubCard
                  key={hub.id}
                  hub={hub}
                  onClick={() => navigate(`/hubs/${hub.id}`)}
                />
              ))}
            </div>
          )}
        </div>

        {/* Pi Builder */}
        <div className="section">
          <div className="card pi-builder">
            <h2 className="section-title">🍓 Deploy a Raspberry Pi Hub</h2>
            <p className="section-desc">
              Generate a setup script for a new Pi hub. Run it on a fresh Raspberry Pi OS install —
              it will auto-configure and connect to FarmSense.
            </p>
            {hubs.length === 0 ? (
              <p className="muted-text">Add a hub first to generate a provisioning script.</p>
            ) : (
              <div className="pi-builder-controls">
                <select
                  className="form-select"
                  value={provisionHubId}
                  onChange={(e) => setProvisionHubId(e.target.value)}
                >
                  {hubs.map((h) => (
                    <option key={h.id} value={h.id}>{h.name}</option>
                  ))}
                </select>
                <button
                  className="btn btn-primary"
                  onClick={() => provisionHubId && downloadProvisionScript(provisionHubId)}
                  disabled={!provisionHubId}
                >
                  Download Setup Script
                </button>
              </div>
            )}
          </div>
        </div>

        {/* ESP32 section */}
        <div className="section">
          <div className="card esp32-builder">
            <h2 className="section-title">⚡ Add an ESP32 Sensor Node</h2>
            <p className="section-desc">
              Register a sensor node, then flash your ESP32 with FarmSense firmware and enter the
              Node ID when prompted.
            </p>
            {hubs.length === 0 ? (
              <p className="muted-text">Add a hub first before registering sensor nodes.</p>
            ) : (
              <>
                <form onSubmit={handleRegisterNode} className="esp32-form">
                  <div className="esp32-steps">
                    <div className="esp32-step">
                      <div className="step-num">1</div>
                      <div className="form-group">
                        <label className="form-label">Select Hub</label>
                        <select
                          className="form-select"
                          value={esp32HubId}
                          onChange={(e) => setEsp32HubId(e.target.value)}
                        >
                          {hubs.map((h) => (
                            <option key={h.id} value={h.id}>{h.name}</option>
                          ))}
                        </select>
                      </div>
                    </div>
                    <div className="esp32-step">
                      <div className="step-num">2</div>
                      <div className="form-group">
                        <label className="form-label">Sensor Type</label>
                        <select
                          className="form-select"
                          value={esp32SensorType}
                          onChange={(e) => handleSensorTypeChange(e.target.value as SensorType)}
                        >
                          {SENSOR_TYPES.map((st) => (
                            <option key={st} value={st}>{SENSOR_LABELS[st]}</option>
                          ))}
                        </select>
                      </div>
                    </div>
                    <div className="esp32-step">
                      <div className="step-num">3</div>
                      <div className="form-group">
                        <label className="form-label">Node Name</label>
                        <input
                          type="text"
                          className="form-input"
                          value={esp32NodeName}
                          onChange={(e) => setEsp32NodeName(e.target.value)}
                          required
                          placeholder="Tree A Dendrometer"
                        />
                      </div>
                    </div>
                    <div className="esp32-step">
                      <div className="step-num">4</div>
                      <div className="form-group">
                        <label className="form-label">Unit</label>
                        <input
                          type="text"
                          className="form-input"
                          value={esp32Unit}
                          onChange={(e) => setEsp32Unit(e.target.value)}
                          placeholder="mm"
                        />
                      </div>
                    </div>
                  </div>
                  {esp32Error && <div className="form-error">{esp32Error}</div>}
                  <button type="submit" className="btn btn-primary" disabled={registeringNode}>
                    {registeringNode ? 'Registering…' : 'Register Node'}
                  </button>
                </form>

                {registeredNodeId && (
                  <div className="node-registered">
                    <div className="node-registered-title">Node registered!</div>
                    <p className="node-registered-desc">
                      Flash your ESP32 with FarmSense firmware and enter this Node ID when prompted:
                    </p>
                    <div className="node-id-row">
                      <code className="node-id">{registeredNodeId}</code>
                      <button className="btn btn-ghost" onClick={copyNodeId}>
                        {copiedNodeId ? 'Copied!' : 'Copy'}
                      </button>
                    </div>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      </div>

      {/* Modals */}
      {showCreateFarm && (
        <AddModal
          title="Create Farm"
          onClose={() => { setShowCreateFarm(false); setCreateFarmError(null) }}
          onSubmit={handleCreateFarm}
          submitting={creatingFarm}
          error={createFarmError}
          submitLabel="Create Farm"
        >
          <div className="form-group">
            <label className="form-label">Farm name</label>
            <input
              type="text"
              className="form-input"
              value={farmName}
              onChange={(e) => setFarmName(e.target.value)}
              required
              placeholder="My Farm"
            />
          </div>
          <div className="form-group">
            <label className="form-label">Location</label>
            <input
              type="text"
              className="form-input"
              value={farmLocation}
              onChange={(e) => setFarmLocation(e.target.value)}
              required
              placeholder="Napa Valley, CA"
            />
          </div>
        </AddModal>
      )}

      {showAddHub && selectedFarmId && (
        <AddModal
          title="Add Hub"
          onClose={() => { setShowAddHub(false); setAddHubError(null) }}
          onSubmit={handleAddHub}
          submitting={addingHub}
          error={addHubError}
          submitLabel="Add Hub"
        >
          <div className="form-group">
            <label className="form-label">Hub name</label>
            <input
              type="text"
              className="form-input"
              value={hubName}
              onChange={(e) => setHubName(e.target.value)}
              required
              placeholder="North Field Hub"
            />
          </div>
        </AddModal>
      )}
    </Layout>
  )
}
