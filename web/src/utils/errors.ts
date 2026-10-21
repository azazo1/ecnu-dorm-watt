import { ApiError } from '@/api/client'

export interface UiError {
  code: string
  title: string
  detail: string
  advice: string
  status: number
  requestId: string | null
}

interface ErrorCopy {
  title: string
  advice: string
}

const ERROR_COPY: Record<string, ErrorCopy> = {
  unauthorized: {
    title: '服务端 token 无效',
    advice: '到设置页更新服务端 token, 或运行 dormwatt client login 重新配置.',
  },
  forbidden: {
    title: 'token 权限不足',
    advice: '更换带 write scope 的 token 后重试.',
  },
  room_not_configured: {
    title: '未选择房间',
    advice: '在仪表盘或设置页选择一个活动房间.',
  },
  ecnu_not_logged_in: {
    title: 'ECNU 登录已失效',
    advice: '运行 dormwatt client login, 或在设置页手动填写会话信息.',
  },
  ecnu_upstream_error: {
    title: '学校接口异常',
    advice: '稍后重试, 必要时查看服务端日志.',
  },
  ecnu_no_degree: {
    title: '学校接口缺少电量字段',
    advice: '稍后重试, 或检查学校页面是否改版.',
  },
  catalog_incomplete: {
    title: '学校目录数据不完整',
    advice: '稍后重试, 或改用已有房间.',
  },
  archive_empty: {
    title: '该范围没有样本',
    advice: '调整时间范围后重试.',
  },
  archive_exists: {
    title: '归档名称已存在',
    advice: '换一个归档名称.',
  },
  records_too_many: {
    title: '记录太多',
    advice: '收窄时间范围, 或使用 CSV 导出全部记录.',
  },
  upstream_unreachable: {
    title: '服务端不可达',
    advice: '检查服务端地址, 网络与代理配置.',
  },
  network_error: {
    title: '无法连接本地守护进程',
    advice: '确认 dormwatt client serve 正在运行, 然后刷新页面.',
  },
  local_forbidden: {
    title: '本地请求被拒绝',
    advice: '请通过 127.0.0.1 或 localhost 打开页面, 并确认 Origin 与 Host 一致.',
  },
  rate_limited: {
    title: '请求过于频繁',
    advice: '等待一段时间后重试.',
  },
  validation_failed: {
    title: '输入不合法',
    advice: '检查时间, 名称与房间等字段.',
  },
  config_write_failed: {
    title: '配置写入失败',
    advice: '检查配置文件权限与磁盘空间.',
  },
  storage_error: {
    title: '存储读写失败',
    advice: '检查数据目录权限与磁盘空间, 然后重试.',
  },
  not_found: {
    title: '资源不存在',
    advice: '刷新列表后重试.',
  },
  unavailable: {
    title: '服务暂时不可用',
    advice: '守护进程可能正在启动或关闭, 稍后重试.',
  },
  invalid_response: {
    title: '响应格式错误',
    advice: '检查客户端与服务端版本是否匹配.',
  },
  upstream_error: {
    title: '上游查询失败',
    advice: '稍后重试, 或查看服务端状态.',
  },
  tls_error: {
    title: 'TLS 校验失败',
    advice: '检查代理设置与系统证书配置.',
  },
  server_down: {
    title: '服务端不可达',
    advice: '检查服务端地址, 网络与代理配置.',
  },
  no_net: {
    title: '网络已断开',
    advice: '恢复网络后页面会自动重试.',
  },
}

const FALLBACK: ErrorCopy = {
  title: '请求失败',
  advice: '稍后重试. 如果问题持续, 请查看守护进程日志.',
}

export function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

export function toUiError(error: unknown): UiError {
  if (error instanceof ApiError) {
    const copy = ERROR_COPY[error.code] ?? FALLBACK
    return {
      code: error.code,
      title: copy.title,
      detail: error.detail,
      advice: copy.advice,
      status: error.status,
      requestId: error.requestId,
    }
  }
  if (error instanceof Error) {
    return {
      code: 'unknown_error',
      title: FALLBACK.title,
      detail: error.message,
      advice: FALLBACK.advice,
      status: 0,
      requestId: null,
    }
  }
  return {
    code: 'unknown_error',
    title: FALLBACK.title,
    detail: '发生未知错误.',
    advice: FALLBACK.advice,
    status: 0,
    requestId: null,
  }
}
