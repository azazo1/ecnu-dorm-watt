import type {
  ArchiveMeta,
  ArchiveQuery,
  ArchivesResponse,
  BuildingsResponse,
  CatalogQuery,
  CatalogRoomsResponse,
  CreateArchiveInput,
  CreateRoomInput,
  DegreeResponse,
  DistrictsResponse,
  FloorsResponse,
  Room,
  RoomInfo,
  RoomsResponse,
  SessionInfo,
  SettingsResponse,
  SettingsUpdate,
  SettingsUpdateResponse,
  StatusResponse,
  UpdateSessionInput,
  ProblemDetails,
  RecordsQuery,
  RecordsResponse,
} from './types'

const API_ROOT = '/api'

type QueryValue = string | number | boolean | null | undefined

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  query?: Record<string, QueryValue>
  body?: unknown
  signal?: AbortSignal
}

interface ApiErrorOptions {
  status: number
  code: string
  title: string
  detail: string
  requestId?: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly title: string
  readonly detail: string
  readonly requestId: string | null

  constructor(message: string, options: ApiErrorOptions) {
    super(message)
    this.name = 'ApiError'
    this.status = options.status
    this.code = options.code
    this.title = options.title
    this.detail = options.detail
    this.requestId = options.requestId ?? null
  }
}

function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  if (!query) {
    return `${API_ROOT}${path}`
  }
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') {
      continue
    }
    params.set(key, String(value))
  }
  const suffix = params.toString()
  return suffix ? `${API_ROOT}${path}?${suffix}` : `${API_ROOT}${path}`
}

async function parseProblem(response: Response): Promise<ProblemDetails> {
  let text = ''
  try {
    text = await response.text()
  } catch {
    return {}
  }
  if (!text) {
    return {}
  }
  try {
    const parsed = JSON.parse(text) as unknown
    if (parsed && typeof parsed === 'object') {
      return parsed as ProblemDetails
    }
  } catch {
    // 非 JSON 错误体只保留简短文本, 避免把大段 HTML 显示到页面.
  }
  return { detail: text.slice(0, 500) }
}

function isJsonResponse(response: Response): boolean {
  const type = response.headers.get('Content-Type') ?? ''
  return type.includes('application/json') || type.includes('application/problem+json')
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers = new Headers({ Accept: 'application/json' })
  let body: string | undefined

  if (method !== 'GET') {
    headers.set('Content-Type', 'application/json')
    headers.set('X-Dormwatt-Client', '1')
    body = options.body === undefined ? '{}' : JSON.stringify(options.body)
  }

  let response: Response
  try {
    response = await fetch(buildUrl(path, options.query), {
      method,
      headers,
      body,
      credentials: 'same-origin',
      cache: 'no-store',
      signal: options.signal,
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error
    }
    throw new ApiError('无法连接本地守护进程', {
      status: 0,
      code: 'network_error',
      title: '无法连接本地守护进程',
      detail: '请确认 dormwatt client serve 正在运行, 并且页面与守护进程使用同一地址.',
    })
  }

  const requestId = response.headers.get('X-Request-Id') ?? undefined

  if (!response.ok) {
    const problem = await parseProblem(response)
    const code = problem.code || `http_${response.status}`
    const title = problem.title || response.statusText || '请求失败'
    const detail = problem.detail || '服务端没有返回更多说明.'
    throw new ApiError(detail, {
      status: problem.status ?? response.status,
      code,
      title,
      detail,
      requestId: problem.request_id ?? requestId,
    })
  }

  if (response.status === 204) {
    return undefined as T
  }

  if (!isJsonResponse(response)) {
    throw new ApiError('响应格式不是 JSON', {
      status: response.status,
      code: 'invalid_response',
      title: '响应格式错误',
      detail: '本地 API 返回了非 JSON 内容, 请检查守护进程版本.',
      requestId,
    })
  }

  try {
    return (await response.json()) as T
  } catch {
    throw new ApiError('响应 JSON 解析失败', {
      status: response.status,
      code: 'invalid_response',
      title: '响应解析失败',
      detail: '本地 API 返回的 JSON 无法解析.',
      requestId,
    })
  }
}

export const api = {
  getHealth(signal?: AbortSignal): Promise<{ status: string; version: string; pid?: number }> {
    return request('/health', { signal })
  },

  getStatus(signal?: AbortSignal): Promise<StatusResponse> {
    return request('/status', { signal })
  },

  getDegree(options: { fresh?: boolean; signal?: AbortSignal } = {}): Promise<DegreeResponse> {
    return request('/degree', {
      query: { fresh: options.fresh ? true : undefined },
      signal: options.signal,
    })
  },

  getRecords(query: RecordsQuery, signal?: AbortSignal): Promise<RecordsResponse> {
    return request('/records', {
      query: {
        room_id: query.room_id,
        from: query.from,
        to: query.to,
        limit: query.limit,
      },
      signal,
    })
  },

  getRooms(signal?: AbortSignal): Promise<RoomsResponse> {
    return request('/rooms', { signal })
  },

  createRoom(input: CreateRoomInput): Promise<Room> {
    return request('/rooms', { method: 'POST', body: input })
  },

  getActiveRoom(signal?: AbortSignal): Promise<Room> {
    return request('/rooms/active', { signal })
  },

  setActiveRoom(id: number): Promise<void> {
    return request(`/rooms/${id}/active`, { method: 'PUT' })
  },

  clearActiveRoom(): Promise<void> {
    return request('/rooms/active', { method: 'DELETE' })
  },

  getRoomInfo(id: number, signal?: AbortSignal): Promise<RoomInfo> {
    return request(`/rooms/${id}/info`, { signal })
  },

  getDistricts(signal?: AbortSignal): Promise<DistrictsResponse> {
    return request('/ecnu/districts', { signal })
  },

  getBuildings(query: CatalogQuery, signal?: AbortSignal): Promise<BuildingsResponse> {
    return request('/ecnu/buildings', {
      query: {
        area_id: query.area_id,
        district_id: query.district_id,
      },
      signal,
    })
  },

  getFloors(query: CatalogQuery, signal?: AbortSignal): Promise<FloorsResponse> {
    return request('/ecnu/floors', {
      query: {
        area_id: query.area_id,
        district_id: query.district_id,
        building_id: query.building_id,
      },
      signal,
    })
  },

  getCatalogRooms(query: CatalogQuery, signal?: AbortSignal): Promise<CatalogRoomsResponse> {
    return request('/ecnu/rooms', {
      query: {
        area_id: query.area_id,
        district_id: query.district_id,
        building_id: query.building_id,
        floor_id: query.floor_id,
      },
      signal,
    })
  },

  getSession(signal?: AbortSignal): Promise<SessionInfo> {
    return request('/session', { signal })
  },

  updateSession(input: UpdateSessionInput): Promise<void> {
    return request('/session', { method: 'PUT', body: input })
  },

  clearSession(): Promise<void> {
    return request('/session', { method: 'DELETE' })
  },

  getArchives(query: ArchiveQuery, signal?: AbortSignal): Promise<ArchivesResponse> {
    return request('/archives', {
      query: {
        room_id: query.room_id,
        include_deleted: query.include_deleted ? true : undefined,
      },
      signal,
    })
  },

  createArchive(input: CreateArchiveInput): Promise<ArchiveMeta> {
    return request('/archives', { method: 'POST', body: input })
  },

  deleteArchive(name: string, roomId: number): Promise<void> {
    return request(`/archives/${encodeURIComponent(name)}`, {
      method: 'DELETE',
      query: { room_id: roomId },
    })
  },

  getSettings(signal?: AbortSignal): Promise<SettingsResponse> {
    return request('/settings', { signal })
  },

  updateSettings(input: SettingsUpdate): Promise<SettingsUpdateResponse> {
    return request('/settings', { method: 'PUT', body: input })
  },

  shutdown(): Promise<{ status: string }> {
    return request('/shutdown', { method: 'POST', body: {} })
  },
}

export function recordsCsvUrl(query: RecordsQuery & { header?: boolean }): string {
  return buildUrl('/records', {
    room_id: query.room_id,
    from: query.from,
    to: query.to,
    format: 'csv',
    header: query.header ? true : undefined,
  })
}

export function archiveCsvUrl(name: string, roomId: number): string {
  return buildUrl(`/archives/${encodeURIComponent(name)}`, { room_id: roomId })
}
