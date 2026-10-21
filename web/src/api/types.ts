export type HealthState =
  | 'starting'
  | 'ok'
  | 'no_room'
  | 'not_logged_in'
  | 'server_down'
  | 'no_net'
  | 'tls_error'
  | 'upstream_error'
  | 'unknown'

export type SessionState = 'missing' | 'unknown' | 'ok' | 'invalid'

export interface ProblemDetails {
  type?: string
  title?: string
  status?: number
  code?: string
  detail?: string
  request_id?: string
}

export interface HealthResponse {
  status: string
  version: string
  pid?: number
}

export interface SessionInfo {
  state: SessionState
  updated_at: string | null
  last_checked_at?: string | null
}

export interface RoomBrief {
  id: number
  room_no: string
  elcarea: number
  elcbuis: string
}

export interface Room extends RoomBrief {
  active: boolean
  created_at: string
  sample_count: number
  last_sample_at: string | null
}

export interface MonitorError {
  code: string
  message: string
}

export interface MonitorStatus {
  poll_interval_seconds: number
  last_attempt_at: string | null
  last_success_at: string | null
  last_degree: number | null
  last_error: MonitorError | null
  next_poll_at: string | null
}

export interface RemoteStatus {
  version: string
  server_time: string
  uptime_seconds: number
  active_room: RoomBrief | null
  session: SessionInfo
  monitor: MonitorStatus
  storage: {
    samples: number
    archives: number
  }
}

export interface LocalStatus {
  version: string
  uptime_seconds: number
  health: HealthState
  health_detail: string | null
  last_check_at: string | null
  consecutive_failures: number
  upstream_base: string
}

export interface StatusResponse {
  local: LocalStatus
  remote: RemoteStatus | null
}

export interface DegreeResponse {
  degree: number
  queried_at: string
  source: 'cache' | 'upstream'
  cached_until: string
}

export interface RecordSample {
  time: string
  degree: number
}

export interface RecordsResponse {
  room_id: number
  room_no: string
  from: string | null
  to: string | null
  records: RecordSample[]
}

export interface RoomsResponse {
  rooms: Room[]
}

export interface RoomInfo {
  area: { area_id: string; area_name: string } | null
  district: { district_id: string; district_name: string } | null
  building: { building_id: string; building_name: string } | null
  floor: { floor_id: string; floor_name: string } | null
  room: { room_id: string; room_name: string } | null
}

export interface ArchiveMeta {
  name: string
  room_id: number
  start_time: string
  end_time: string
  records_num: number
  created_at: string
}

export interface ArchivesResponse {
  archives: ArchiveMeta[]
}

export interface AreaOption {
  area_id: string
  area_name: string
}

export interface DistrictOption {
  district_id: string
  district_name: string
}

export interface BuildingOption {
  building_id: string
  building_name: string
}

export interface FloorOption {
  floor_id: string
  floor_name: string
}

export interface CatalogRoom {
  room_id: string
  room_name: string
}

export interface DistrictsResponse {
  areas: AreaOption[]
  districts: DistrictOption[]
}

export interface BuildingsResponse {
  buildings: BuildingOption[]
}

export interface FloorsResponse {
  floors: FloorOption[]
}

export interface CatalogRoomsResponse {
  rooms: CatalogRoom[]
}

export interface ServerSettings {
  base_url: string
  token_set: boolean
  token_prefix?: string | null
  request_timeout?: string
}

export interface LocalSettings {
  bind: string
  allow_remote: boolean
  tray: string
  notify: boolean
}

export interface MonitorSettings {
  poll_interval: string
  degree_threshold: number
}

export interface UiSettings {
  timezone: string
  refresh_interval: string
}

export interface SettingsResponse {
  server: ServerSettings
  local: LocalSettings
  monitor: MonitorSettings
  ui: UiSettings
}

export interface SettingsUpdate {
  server?: {
    base_url?: string
    token?: string | null
  }
  local?: {
    notify?: boolean
  }
  monitor?: {
    degree_threshold?: number
  }
  ui?: {
    timezone?: string
    refresh_interval?: string
  }
}

export interface SettingsUpdateResponse extends SettingsResponse {
  restart_required?: boolean
}

export interface CreateRoomInput {
  room_no: string
  elcarea: number
  elcbuis: string
  activate?: boolean
}

export interface UpdateSessionInput {
  jsession_id: string
  cookie: string
  x_csrf_token: string
}

export interface CreateArchiveInput {
  room_id?: number
  from?: string
  to?: string
  name?: string
}

export interface RecordsQuery {
  room_id?: number | null
  from?: string | null
  to?: string | null
  limit?: number
}

export interface ArchiveQuery {
  room_id?: number | 'all' | null
  include_deleted?: boolean
}

export interface CatalogQuery {
  area_id?: string | null
  district_id?: string | null
  building_id?: string | null
  floor_id?: string | null
}
