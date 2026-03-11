export interface User {
  id: string
  email: string
  name: string
}

export interface Farm {
  id: string
  user_id: string
  name: string
  location: string
  created_at: string
}

export interface Hub {
  id: string
  farm_id: string
  name: string
  last_seen: string | null
  online: boolean
  created_at: string
  nodes?: Node[]
}

export interface Node {
  id: string
  hub_id: string
  name: string
  sensor_type: SensorType
  unit: string
  last_seen: string | null
  created_at: string
}

export type SensorType =
  | 'dendrometer'
  | 'soil_moisture'
  | 'temperature'
  | 'solar'
  | 'camera'
  | 'other'

export interface Reading {
  timestamp: string
  value: number
}

export interface FarmStats {
  hub_count: number
  node_count: number
  online_hubs: number
  latest_readings: Record<string, number>
}
