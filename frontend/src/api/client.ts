import type { Farm, FarmMember, Hub, Node, Reading, FarmStats, User } from '../types'

const BASE = '/api'
const TOKEN_KEY = 'farmsense_token'

function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

function authHeaders(): HeadersInit {
  const token = getToken()
  return token
    ? { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }
    : { 'Content-Type': 'application/json' }
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // ignore parse error
    }
    throw new Error(message)
  }
  return res.json() as Promise<T>
}

// Auth
export async function apiRegister(
  email: string,
  password: string,
  name: string
): Promise<{ token: string; user: User }> {
  const res = await fetch(`${BASE}/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, name }),
  })
  return handleResponse(res)
}

export async function apiLogin(
  email: string,
  password: string
): Promise<{ token: string; user: User }> {
  const res = await fetch(`${BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  return handleResponse(res)
}

// Farms
export async function apiFarms(): Promise<{ farms: Farm[] }> {
  const res = await fetch(`${BASE}/farms`, { headers: authHeaders() })
  return handleResponse(res)
}

export async function apiCreateFarm(name: string, location: string): Promise<Farm> {
  const res = await fetch(`${BASE}/farms`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ name, location }),
  })
  return handleResponse(res)
}

export async function apiFarm(farmId: string): Promise<Farm> {
  const res = await fetch(`${BASE}/farms/${farmId}`, { headers: authHeaders() })
  return handleResponse(res)
}

export async function apiFarmStats(farmId: string): Promise<FarmStats> {
  const res = await fetch(`${BASE}/farms/${farmId}/stats`, { headers: authHeaders() })
  return handleResponse(res)
}

export async function apiFarmHubs(farmId: string): Promise<{ hubs: Hub[] }> {
  const res = await fetch(`${BASE}/farms/${farmId}/hubs`, { headers: authHeaders() })
  return handleResponse(res)
}

export async function apiCreateHub(farmId: string, name: string): Promise<Hub> {
  const res = await fetch(`${BASE}/farms/${farmId}/hubs`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ name }),
  })
  return handleResponse(res)
}

// Hubs
export async function apiHub(hubId: string): Promise<Hub> {
  const res = await fetch(`${BASE}/hubs/${hubId}`, { headers: authHeaders() })
  return handleResponse(res)
}

export async function apiHubNodes(hubId: string): Promise<{ nodes: Node[] }> {
  const res = await fetch(`${BASE}/hubs/${hubId}/nodes`, { headers: authHeaders() })
  return handleResponse(res)
}

export async function apiCreateNode(
  hubId: string,
  name: string,
  sensor_type: string,
  unit: string
): Promise<Node> {
  const res = await fetch(`${BASE}/hubs/${hubId}/nodes`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ name, sensor_type, unit }),
  })
  return handleResponse(res)
}

export async function apiDeleteFarm(farmId: string): Promise<void> {
  const res = await fetch(`${BASE}/farms/${farmId}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try { const b = (await res.json()) as { error?: string }; if (b.error) message = b.error } catch { /* ignore */ }
    throw new Error(message)
  }
}

export async function apiFarmMembers(farmId: string): Promise<{ members: FarmMember[] }> {
  const res = await fetch(`${BASE}/farms/${farmId}/members`, { headers: authHeaders() })
  return handleResponse<{ members: FarmMember[] }>(res)
}

export async function apiAddFarmMember(farmId: string, email: string): Promise<FarmMember> {
  const res = await fetch(`${BASE}/farms/${farmId}/members`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ email }),
  })
  return handleResponse<FarmMember>(res)
}

export async function apiRemoveFarmMember(farmId: string, userId: string): Promise<void> {
  const res = await fetch(`${BASE}/farms/${farmId}/members/${userId}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try { const b = (await res.json()) as { error?: string }; if (b.error) message = b.error } catch { /* ignore */ }
    throw new Error(message)
  }
}

export async function apiDeleteNode(nodeId: string): Promise<void> {
  const res = await fetch(`${BASE}/nodes/${nodeId}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch { /* ignore */ }
    throw new Error(message)
  }
}

export async function apiDeleteHub(hubId: string): Promise<void> {
  const res = await fetch(`${BASE}/hubs/${hubId}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch { /* ignore */ }
    throw new Error(message)
  }
}

export function downloadProvisionScript(hubId: string, arch: string): void {
  const token = getToken()
  const params = new URLSearchParams({ arch })
  if (token) params.set('token', token)
  const url = `${BASE}/hubs/${hubId}/provision?${params.toString()}`
  // Use fetch + blob to trigger download with auth header
  fetch(url, { headers: authHeaders() })
    .then(async (res) => {
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const blob = await res.blob()
      const blobUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = `farmsense-hub-${hubId}.sh`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(blobUrl)
    })
    .catch((err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err)
      alert(`Failed to download script: ${msg}`)
    })
}

// Nodes
export async function apiNodeReadings(
  nodeId: string,
  range: '1h' | '24h' | '7d' | '30d'
): Promise<{ readings: Reading[] }> {
  const res = await fetch(`${BASE}/nodes/${nodeId}/readings?range=${range}`, {
    headers: authHeaders(),
  })
  return handleResponse(res)
}
