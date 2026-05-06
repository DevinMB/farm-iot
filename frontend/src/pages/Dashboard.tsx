import { useState, useEffect, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import Layout from '../components/Layout'
import HubCard from '../components/HubCard'
import AddModal from '../components/AddModal'
import {
  apiFarms,
  apiCreateFarm,
  apiDeleteFarm,
  apiFarmStats,
  apiFarmHubs,
  apiCreateHub,
  apiFarmMembers,
  apiAddFarmMember,
  apiRemoveFarmMember,
  downloadProvisionScript,
} from '../api/client'
import type { Farm, FarmMember, Hub, FarmStats } from '../types'

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
  const [provisionArch, setProvisionArch] = useState<string>('armv6')

  // Farm settings modal
  const [showFarmSettings, setShowFarmSettings] = useState(false)
  const [farmMembers, setFarmMembers] = useState<FarmMember[]>([])
  const [loadingMembers, setLoadingMembers] = useState(false)
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviting, setInviting] = useState(false)
  const [inviteError, setInviteError] = useState<string | null>(null)

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
        } else {
          setProvisionHubId('')
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
      setShowAddHub(false)
      setHubName('')
    } catch (err: unknown) {
      setAddHubError(err instanceof Error ? err.message : 'Failed to add hub')
    } finally {
      setAddingHub(false)
    }
  }


  function openFarmSettings() {
    if (!selectedFarmId) return
    setShowFarmSettings(true)
    setLoadingMembers(true)
    setInviteError(null)
    setInviteEmail('')
    apiFarmMembers(selectedFarmId)
      .then((res) => { setFarmMembers(res.members); setLoadingMembers(false) })
      .catch(() => setLoadingMembers(false))
  }

  async function handleInvite(e: FormEvent) {
    e.preventDefault()
    if (!selectedFarmId) return
    setInviteError(null)
    setInviting(true)
    try {
      const member = await apiAddFarmMember(selectedFarmId, inviteEmail)
      setFarmMembers((prev) => [...prev, member])
      setInviteEmail('')
    } catch (err: unknown) {
      setInviteError(err instanceof Error ? err.message : 'Failed to invite member')
    } finally {
      setInviting(false)
    }
  }

  async function handleRemoveMember(userId: string) {
    if (!selectedFarmId) return
    try {
      await apiRemoveFarmMember(selectedFarmId, userId)
      setFarmMembers((prev) => prev.filter((m) => m.user_id !== userId))
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : 'Failed to remove member')
    }
  }

  async function handleDeleteFarm() {
    if (!selectedFarmId || !selectedFarm) return
    if (!confirm(`Delete "${selectedFarm.name}"?\n\nThis will permanently delete the farm, all its hubs, and all sensor nodes. This cannot be undone.`)) return
    try {
      await apiDeleteFarm(selectedFarmId)
      const remaining = farms.filter((f) => f.id !== selectedFarmId)
      setFarms(remaining)
      setSelectedFarmId(remaining.length > 0 ? remaining[0].id : null)
      setShowFarmSettings(false)
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : 'Failed to delete farm')
    }
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
        {/* Farm selector + settings */}
        <div className="farm-tabs-row">
          {farms.length > 1 && (
            <div className="farm-tabs">
              {farms.map((f) => (
                <button
                  key={f.id}
                  className={`farm-tab ${f.id === selectedFarmId ? 'active' : ''}`}
                  onClick={() => setSelectedFarmId(f.id)}
                >
                  {f.name}
                  {!f.owned && <span className="farm-tab-shared" title="Shared with you">↗</span>}
                </button>
              ))}
            </div>
          )}
          {selectedFarmId && (
            <button className="btn btn-ghost btn-sm farm-settings-btn" onClick={openFarmSettings} title="Farm settings">
              ⚙ Manage Farm
            </button>
          )}
        </div>

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
                <select
                  className="form-select"
                  value={provisionArch}
                  onChange={(e) => setProvisionArch(e.target.value)}
                  title="Select your Raspberry Pi model"
                >
                  <option value="armv6">Pi Zero / Pi 1 (ARMv6)</option>
                  <option value="armv7">Pi 2 / Pi 3 32-bit (ARMv7)</option>
                  <option value="arm64">Pi 3 / Pi 4 / Pi 5 64-bit (ARM64)</option>
                </select>
                <button
                  className="btn btn-primary"
                  onClick={() => provisionHubId && downloadProvisionScript(provisionHubId, provisionArch)}
                  disabled={!provisionHubId}
                >
                  Download Setup Script
                </button>
              </div>
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

      {/* Farm Settings Modal */}
      {showFarmSettings && selectedFarm && (
        <div className="modal-backdrop" onClick={() => setShowFarmSettings(false)}>
          <div className="modal farm-settings-modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2 className="modal-title">{selectedFarm.name} — Settings</h2>
              <button className="modal-close" onClick={() => setShowFarmSettings(false)}>✕</button>
            </div>

            <div className="modal-body">
              {/* Members section */}
              <div className="settings-section">
                <h3 className="settings-section-title">Members</h3>
                {loadingMembers ? (
                  <p className="muted-text">Loading…</p>
                ) : farmMembers.length === 0 ? (
                  <p className="muted-text">No shared members yet.</p>
                ) : (
                  <ul className="members-list">
                    {farmMembers.map((m) => (
                      <li key={m.user_id} className="member-row">
                        <div className="member-info">
                          <span className="member-name">{m.name}</span>
                          <span className="member-email">{m.email}</span>
                        </div>
                        {selectedFarm.owned && (
                          <button
                            className="btn btn-danger-sm"
                            onClick={() => handleRemoveMember(m.user_id)}
                          >
                            Remove
                          </button>
                        )}
                      </li>
                    ))}
                  </ul>
                )}

                {selectedFarm.owned && (
                  <form onSubmit={handleInvite} className="invite-form">
                    <input
                      type="email"
                      className="form-input"
                      placeholder="Enter email address to invite"
                      value={inviteEmail}
                      onChange={(e) => setInviteEmail(e.target.value)}
                      required
                    />
                    {inviteError && <div className="form-error">{inviteError}</div>}
                    <button type="submit" className="btn btn-primary" disabled={inviting}>
                      {inviting ? 'Inviting…' : 'Invite'}
                    </button>
                  </form>
                )}
              </div>

              {/* Danger zone — owner only */}
              {selectedFarm.owned && (
                <div className="settings-section danger-zone">
                  <h3 className="settings-section-title danger-title">Danger Zone</h3>
                  <p className="muted-text">Permanently delete this farm and all its hubs and sensor nodes.</p>
                  <button className="btn btn-danger" onClick={handleDeleteFarm}>
                    Delete Farm
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </Layout>
  )
}
