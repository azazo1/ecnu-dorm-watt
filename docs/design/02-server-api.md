# 服务端 HTTP API

本文档定义 `dormwatt server` 对客户端暴露的正式 API. 旧 Rust 实现的接口作为语义来源, 但路径, 包裹格式与错误模型重新设计. 旧接口到新接口的对照见 [09-migration-roadmap.md](09-migration-roadmap.md).

## 1. 通用约定

### 1.1 挂载前缀

- 配置项 `server.base_path` 指定公网挂载前缀, 例如 `/dormwatt/api`.
- 所有 API 路由形如 `{base_path}/v1/...`.
- `base_path` 规则:
  - 必须以 `/` 开头, 不允许以 `/` 结尾, 不允许空段, `.` 或 `..`.
  - 默认值 `/dormwatt/api`.
  - 允许为 `/` 表示挂在域名根, 此时路由为 `/v1/...`.
- 服务端不使用绝对 URL 重定向, 响应中不出现依赖域名的主机名.
- 请求路径不在 `base_path` 之下时返回 404, 不泄露其他路由.
- 反向代理必须保留完整路径转发, 不能去掉前缀. 具体示例见 [08-deployment.md](08-deployment.md).

示例:

```
公网 URL:   https://example.com/dormwatt/api/v1/records
配置:       base_path = "/dormwatt/api"
内部路由:   /v1/records
```

### 1.2 客户端基址拼接规则

客户端配置的 `server.base_url` 可以包含路径, 例如 `https://example.com/dormwatt/api`. 拼接时必须使用字符串规则追加 `/v1/...`, 不允许使用会把路径替换掉的绝对路径解析.

硬性规则:

- 禁止使用 Go 的 `url.URL.ResolveReference` 配 `/v1/...` 形式.
- 禁止 `path.Join(base, "/v1/records")` 后直接当 URL 路径使用.
- 统一通过 `joinBaseURL(base, "/v1/records")` 工具函数拼接, 保留 `base` 的已有路径.
- 这一步是对旧实现最危险的回归点, 旧客户端 12 处 `Url::join("/get-degree")` 都会丢弃前缀.

### 1.3 版本

- URL 固定包含 `/v1`.
- 未来不兼容变更使用 `/v2`, 并行提供至少一个发布周期.
- 破坏性字段变更必须提升主版本段, 不允许在同一版本内静默改语义.

### 1.4 请求与响应格式

- 请求体与响应体均为 JSON 时使用 `Content-Type: application/json`.
- 写请求拒绝未知字段, 使用严格解码.
- 响应 JSON 使用 snake_case 字段名. 旧实现混用 camelCase 与 snake_case, 新 API 不延续.
- 响应中的时间使用 RFC3339 带时区偏移, 精确到秒, 例如 `2026-01-24T14:35:32+08:00`.
- 内部存储使用 UTC 毫秒, 输出时按 `ui.timezone` 转换, 默认 `Asia/Shanghai`.
- 日期或时间解析只接受带时区偏移的 RFC3339. 不带偏移的输入返回 422.
- 所有响应附带 `X-Request-Id`. 服务端接受合法客户端提供的 `X-Request-Id`, 否则生成随机值. 合法格式为 `[A-Za-z0-9._-]{8,64}`.

### 1.5 鉴权

除 `GET /v1/health` 外, 所有接口需要 `Authorization: Bearer <token>`. 权限范围:

- `read`: 所有 GET 接口.
- `write`: 包含 `read`, 并允许修改会话, 房间与归档.

鉴权细节, token 生成与撤销见 [03-auth.md](03-auth.md).

### 1.6 错误模型

错误统一使用 RFC 9457 Problem Details, `Content-Type: application/problem+json`, 并增加 `code` 扩展字段.

```json
{
  "type": "about:blank",
  "title": "ECNU session is not usable",
  "status": 502,
  "code": "ecnu_not_logged_in",
  "detail": "the upstream portal redirected to SSO",
  "request_id": "2f9c1a7b8e4d4c3a"
}
```

- `title`: 稳定短句, 可展示.
- `detail`: 诊断信息, 可以展示给用户, 不含凭据.
- `code`: 稳定机器码, 客户端按它分支.

错误码总表:

| code | HTTP | 含义 |
|------|------|------|
| `invalid_request` | 400 | JSON 结构或参数格式错误 |
| `unauthorized` | 401 | 缺少或无效 token |
| `forbidden` | 403 | token 缺少所需 scope |
| `not_found` | 404 | 资源不存在 |
| `method_not_allowed` | 405 | 方法不支持 |
| `room_not_configured` | 409 | 当前没有活动房间 |
| `room_exists` | 409 | 相同房间已存在但请求要求新建 |
| `archive_exists` | 409 | 归档名称冲突 |
| `conflict` | 409 | 其他状态冲突 |
| `payload_too_large` | 413 | 请求体超过限制 |
| `unsupported_media_type` | 415 | Content-Type 不支持 |
| `validation_failed` | 422 | 字段值不合法 |
| `archive_empty` | 422 | 时间范围内没有样本 |
| `records_too_many` | 422 | 记录数超过单次上限 |
| `rate_limited` | 429 | 触发速率限制 |
| `storage_error` | 500 | SQLite 或文件读写失败 |
| `internal` | 500 | 未分类内部错误 |
| `ecnu_not_logged_in` | 502 | 上游要求重新登录 |
| `ecnu_upstream_error` | 502 | 上游网络或协议错误 |
| `ecnu_no_degree` | 502 | 上游响应缺少电量字段 |
| `catalog_incomplete` | 502 | 上游目录数据缺少对应项 |
| `unavailable` | 503 | 服务正在启动或关闭 |

注意事项:

- 不再使用 `200 + {"Err":...}` 表达业务错误.
- `401` 响应必须附带 `WWW-Authenticate: Bearer realm="dormwatt"`.
- 认证失败不区分 token 不存在, 已撤销或格式错误, 统一返回 `unauthorized`.
- `429` 响应附带 `Retry-After`.

### 1.7 限制

| 项目 | 值 |
|------|-----|
| JSON 请求体上限 | 64 KiB |
| 会话上传请求体上限 | 32 KiB |
| 单次 JSON 记录返回上限 | 200000 条 |
| 实时电量上游查询缓存 | 15 秒 |
| ECNU 目录缓存 | 10 分钟 |
| HTTP 读超时 | 30 秒 |
| HTTP 写超时 | 60 秒 |
| 空闲超时 | 120 秒 |

超过记录上限时返回 `records_too_many`, 提示使用 `from` / `to` 收窄范围, 或使用 CSV 流式导出.

### 1.8 缓存与压缩

- 鉴权接口的 JSON 响应统一 `Cache-Control: no-store`.
- `GET /v1/health` 允许 `Cache-Control: no-store` 之外的默认缓存策略.
- 支持 `Accept-Encoding: gzip` 时, 对超过 1 KiB 的 JSON 响应启用 gzip.

## 2. 资源模型

### 2.1 Room

```json
{
  "id": 1,
  "room_no": "4408_MH_83_257",
  "elcarea": 102,
  "elcbuis": "new-83_MH",
  "active": true,
  "created_at": "2026-01-01T00:00:00+08:00",
  "sample_count": 1234,
  "last_sample_at": "2026-01-24T14:35:32+08:00"
}
```

- `room_no` 保留旧命名, 这是 ECNU 的房间代码.
- `elcarea` 是 ECNU 的地区编号.
- `elcbuis` 是 ECNU 的建筑编号.
- `active` 表示是否为当前采集目标.
- 一个服务端实例最多有一个活动房间, 但可保留多个房间的历史数据.

### 2.2 Sample

```json
{
  "time": "2026-01-24T14:35:32+08:00",
  "degree": 33.43
}
```

- `degree` 为剩余电量, 单位度, 保留两位小数输出.
- 同一房间内按时间升序返回.

### 2.3 Session

```json
{
  "state": "ok",
  "updated_at": "2026-01-24T14:30:00+08:00",
  "last_checked_at": "2026-01-24T14:35:32+08:00"
}
```

`state` 取值:

- `missing`: 从未上传.
- `unknown`: 已上传但尚未查询过.
- `ok`: 最近一次上游查询成功.
- `invalid`: 最近一次上游查询提示需要重新登录.

会话值本身永远不出现在任何响应中.

### 2.4 ArchiveMeta

```json
{
  "name": "20260101-20260131",
  "room_id": 1,
  "start_time": "2026-01-01T00:20:00+08:00",
  "end_time": "2026-01-31T23:50:00+08:00",
  "records_num": 892,
  "created_at": "2026-02-01T10:00:00+08:00"
}
```

- `start_time` 与 `end_time` 是归档内实际最早与最晚样本时间, 均包含.
- 归档创建后, 这些样本从活动记录流中移除, 详见第 6 节.

### 2.5 Status

```json
{
  "version": "0.1.0",
  "server_time": "2026-01-24T14:35:32+08:00",
  "uptime_seconds": 3600,
  "base_path": "/dormwatt/api",
  "active_room": {
    "id": 1,
    "room_no": "4408_MH_83_257",
    "elcarea": 102,
    "elcbuis": "new-83_MH"
  },
  "session": {
    "state": "ok",
    "updated_at": "2026-01-24T14:30:00+08:00"
  },
  "monitor": {
    "poll_interval_seconds": 300,
    "last_attempt_at": "2026-01-24T14:35:30+08:00",
    "last_success_at": "2026-01-24T14:35:30+08:00",
    "last_degree": 33.43,
    "last_error": null,
    "next_poll_at": "2026-01-24T14:40:30+08:00"
  },
  "storage": {
    "samples": 1234,
    "archives": 2
  }
}
```

- `active_room` 不存在时为 `null`.
- `last_error` 为 `null` 或 `{ "code": "...", "message": "..." }`.
- `/status` 不触发上游查询, 只反映最近状态.

## 3. 接口总表

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/v1/health` | 无 | 存活检查 |
| GET | `/v1/whoami` | 任意有效 token | 当前 token 信息 |
| GET | `/v1/status` | read | 服务状态汇总 |
| GET | `/v1/degree` | read | 当前电量 |
| GET | `/v1/records` | read | 样本查询与 CSV 导出 |
| GET | `/v1/rooms` | read | 房间列表 |
| POST | `/v1/rooms` | write | 新建或复用房间 |
| GET | `/v1/rooms/active` | read | 活动房间 |
| PUT | `/v1/rooms/{id}/active` | write | 切换活动房间 |
| DELETE | `/v1/rooms/active` | write | 取消活动房间 |
| GET | `/v1/rooms/{id}/info` | read | 解析房间名称信息 |
| GET | `/v1/ecnu/districts` | read | 上游地区列表 |
| GET | `/v1/ecnu/buildings` | read | 上游建筑列表 |
| GET | `/v1/ecnu/floors` | read | 上游楼层列表 |
| GET | `/v1/ecnu/rooms` | read | 上游房间列表 |
| GET | `/v1/session` | read | 会话状态 |
| PUT | `/v1/session` | write | 上传会话 |
| DELETE | `/v1/session` | write | 清除会话 |
| GET | `/v1/archives` | read | 归档列表 |
| POST | `/v1/archives` | write | 创建归档 |
| GET | `/v1/archives/{name}` | read | 下载归档 CSV |
| DELETE | `/v1/archives/{name}` | write | 软删除归档 |

## 4. 基础接口

### 4.1 GET /v1/health

无需鉴权. 用于反向代理与进程存活检查.

```json
{
  "status": "ok",
  "version": "0.1.0"
}
```

- 启动中或关闭中返回 503 `unavailable`.
- 不查询数据库与上游.

### 4.2 GET /v1/whoami

```json
{
  "name": "alice-laptop",
  "scopes": ["read", "write"]
}
```

用于 `dormwatt client doctor` 验证 token 与 scope.

### 4.3 GET /v1/status

返回 2.5 节结构. 不触发上游查询.

## 5. 电量与样本

### 5.1 GET /v1/degree

查询参数:

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `fresh` | bool | false | true 时跳过缓存直接查询上游 |

响应:

```json
{
  "degree": 33.43,
  "queried_at": "2026-01-24T14:35:32+08:00",
  "source": "upstream",
  "cached_until": "2026-01-24T14:35:47+08:00"
}
```

- `source` 为 `cache` 或 `upstream`.
- 没有活动房间返回 409 `room_not_configured`.
- 会话无效返回 502 `ecnu_not_logged_in`.
- 上游失败返回 502 `ecnu_upstream_error` 或 `ecnu_no_degree`.
- 同一房间的实时查询在 15 秒内复用结果, 避免多个客户端同时打学校接口.

### 5.2 GET /v1/records

查询参数:

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `room_id` | int | 活动房间 | 房间 id |
| `from` | RFC3339 | 无下界 | 起始时间, 包含 |
| `to` | RFC3339 | 无上界 | 结束时间, 包含 |
| `limit` | int | 100000 | 最大 200000 |
| `format` | string | json | `json` 或 `csv` |
| `header` | bool | false | CSV 是否输出表头 |

JSON 响应:

```json
{
  "room_id": 1,
  "room_no": "4408_MH_83_257",
  "from": "2026-01-01T00:00:00+08:00",
  "to": null,
  "records": [
    {
      "time": "2026-01-24T14:35:32+08:00",
      "degree": 33.43
    }
  ]
}
```

- `from` 与 `to` 原样回显解析结果或 `null`.
- 结果按时间升序.
- `room_id` 不存在返回 404 `not_found`.
- JSON 记录数超过 `limit` 返回 422 `records_too_many`.
- `format=csv` 时返回 `text/csv; charset=utf-8`, 响应头 `Content-Disposition: attachment; filename="records-<room_id>.csv"`, 流式输出, 不受 JSON 记录上限约束.
- CSV 列固定为 `time,degree`, 时间 RFC3339, 电量两位小数, 行尾 `\n`, 无表头除非 `header=true`.

## 6. 房间

### 6.1 GET /v1/rooms

```json
{
  "rooms": [
    {
      "id": 1,
      "room_no": "4408_MH_83_257",
      "elcarea": 102,
      "elcbuis": "new-83_MH",
      "active": true,
      "created_at": "2026-01-01T00:00:00+08:00",
      "sample_count": 1234,
      "last_sample_at": "2026-01-24T14:35:32+08:00"
    }
  ]
}
```

按 `created_at` 升序.

### 6.2 POST /v1/rooms

请求:

```json
{
  "room_no": "4408_MH_83_257",
  "elcarea": 102,
  "elcbuis": "new-83_MH",
  "activate": true
}
```

- `activate` 默认 true.
- 校验规则:
  - `room_no` 非空, 长度不超过 128, 不允许 `/`, `\`, 控制字符, 不允许 `.` 或 `..`.
  - `elcarea` 为非负整数.
  - `elcbuis` 非空, 长度不超过 128, 不允许控制字符.
- 相同 `room_no` + `elcarea` + `elcbuis` 已存在时返回 200 与已有房间, 不重复创建.
- 新建返回 201.
- `activate=true` 时切换活动房间, 并让采集循环使用新房间.
- 切换房间不会删除旧房间数据.

### 6.3 GET /v1/rooms/active

返回 Room 对象. 没有活动房间返回 409 `room_not_configured`.

### 6.4 PUT /v1/rooms/{id}/active

将指定房间设为活动房间. 成功 204.

- 切换时服务端记录一条日志, 包含旧房间与新房间 id.
- 采集循环立即刷新状态, 不必等待下一个周期.
- 若该房间与当前活动房间相同, 仍返回 204, 幂等.

### 6.5 DELETE /v1/rooms/active

取消活动房间. 成功 204. 采集循环暂停查询, 直到再次设置活动房间. 历史数据保留.

### 6.6 GET /v1/rooms/{id}/info

解析房间名称. 结果来自 ECNU 目录接口, 缓存 10 分钟.

```json
{
  "area": {
    "area_id": "102",
    "area_name": "华东师范大学"
  },
  "district": {
    "district_id": "MH",
    "district_name": "剑川路公寓"
  },
  "building": {
    "building_id": "new-83_MH",
    "building_name": "闵行本科生4号楼"
  },
  "floor": {
    "floor_id": "257",
    "floor_name": "4"
  },
  "room": {
    "room_id": "4408_MH_83_257",
    "room_name": "4408"
  }
}
```

- `room_no` 按 `_` 最多拆四段: `room_name_district_building? _floor_id`.
- 与旧实现相同, `room_name` 取第一段, `district_id` 取第二段, `floor_id` 取第四段, 建筑取 `elcbuis`, 地区取 `elcarea`.
- 解析失败返回 422 `validation_failed`.
- 上游数据缺少对应项返回 502 `catalog_incomplete`.

## 7. ECNU 目录

这些接口需要有效会话, 否则 502 `ecnu_not_logged_in`. 结果缓存 10 分钟, 以父级 id 组合为键.

### 7.1 GET /v1/ecnu/districts

```json
{
  "areas": [
    {
      "area_id": "102",
      "area_name": "华东师范大学"
    }
  ],
  "districts": [
    {
      "district_id": "MH",
      "district_name": "剑川路公寓"
    }
  ]
}
```

### 7.2 GET /v1/ecnu/buildings

查询参数: `area_id`, `district_id`.

```json
{
  "buildings": [
    {
      "building_id": "new-83_MH",
      "building_name": "闵行本科生4号楼"
    }
  ]
}
```

### 7.3 GET /v1/ecnu/floors

查询参数: `area_id`, `district_id`, `building_id`.

```json
{
  "floors": [
    {
      "floor_id": "257",
      "floor_name": "4"
    }
  ]
}
```

### 7.4 GET /v1/ecnu/rooms

查询参数: `area_id`, `district_id`, `building_id`, `floor_id`.

```json
{
  "rooms": [
    {
      "room_id": "4408_MH_83_257",
      "room_name": "4408"
    }
  ]
}
```

## 8. 会话

### 8.1 GET /v1/session

```json
{
  "state": "ok",
  "updated_at": "2026-01-24T14:30:00+08:00",
  "last_checked_at": "2026-01-24T14:35:32+08:00"
}
```

### 8.2 PUT /v1/session

请求:

```json
{
  "jsession_id": "ABC123...",
  "cookie": "value",
  "x_csrf_token": "token-value"
}
```

- 三个字段均非空, 长度上限 8192 字节.
- 拒绝控制字符与空白字符, 允许 ECNU 常见 cookie 字符.
- 成功 204, 无响应体.
- 直接覆盖旧会话.
- 服务端不校验会话是否真实有效, 下一次查询会给出 `ok` 或 `invalid`.
- 该请求体与响应体不得写入日志.

### 8.3 DELETE /v1/session

清除会话, 成功 204. 采集循环下一次查询会得到 `missing` 状态并暂停上游请求.

## 9. 归档

### 9.1 GET /v1/archives

查询参数:

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `room_id` | int 或 `all` | 活动房间 | 房间 id; `all` 表示所有房间 |
| `include_deleted` | bool | false | 是否包含软删除归档 |

```json
{
  "archives": [
    {
      "name": "20260101-20260131",
      "room_id": 1,
      "start_time": "2026-01-01T00:20:00+08:00",
      "end_time": "2026-01-31T23:50:00+08:00",
      "records_num": 892,
      "created_at": "2026-02-01T10:00:00+08:00"
    }
  ]
}
```

- `room_id=all` 时返回所有房间的归档, 每条都带 `room_id`.
- 按 `start_time` 升序, 相同则按 `room_id`, `name` 升序.

### 9.2 POST /v1/archives

请求:

```json
{
  "room_id": 1,
  "from": "2026-01-01T00:00:00+08:00",
  "to": "2026-01-31T23:59:59+08:00",
  "name": "20260101-20260131"
}
```

- `room_id` 默认活动房间.
- `from` / `to` 可省略, 表示无界. 两端均包含.
- `from > to` 返回 422 `validation_failed`.
- `name` 可省略. 省略时生成 `YYYYMMDD-YYYYMMDD`, 若冲突追加 `-2`, `-3` 直到唯一.
- 名称规则: 1 到 64 个 Unicode 字符, 不允许 `/`, `\`, 控制字符, 不允许 `.` 或 `..`, 不允许以 `.` 开头.
- 归档范围与房间内活动样本求交集. 没有样本返回 422 `archive_empty`.
- 显式名称冲突返回 409 `archive_exists`.
- 成功返回 201 与 ArchiveMeta.
- 归档是破坏性操作, 见 9.4.

### 9.3 GET /v1/archives/{name}

- `name` 为 URL 编码的路径段, 服务端解码后按名称查找.
- `room_id` 查询参数默认活动房间.
- 返回 `text/csv; charset=utf-8`.
- 响应头:
  - `Content-Disposition: attachment; filename="<name>.csv"; filename*=UTF-8''<percent-encoded>`
  - `Cache-Control: no-store`
- CSV 格式与 `/records` 的 `format=csv` 一致.
- 不存在或已删除返回 404 `not_found`.
- 名称为空或非法返回 400 `invalid_request`.

### 9.4 归档事务语义

归档创建在一个 SQLite 事务内完成:

1. 选出范围内样本.
2. 计算实际起止时间与数量.
3. 插入 archives 与 archive_samples.
4. 从 samples 删除同一批样本.
5. 提交.

任一步失败则整体回滚, 不会出现"样本消失但归档没写成"的状态. 这修复旧实现的已知非原子问题.

### 9.5 DELETE /v1/archives/{name}

- 软删除, 设置 `deleted_at`.
- 成功 204.
- 已删除或不存在返回 404.
- 数据保留, 由 `dormwatt server purge --deleted-before <RFC3339>` 物理清理.
- V1 不提供恢复接口, 但数据库保留数据, 可手工恢复.

## 10. 幂等性汇总

| 接口 | 幂等 | 说明 |
|------|------|------|
| GET 类 | 是 | 无副作用, 实时查询除外 |
| PUT /v1/session | 是 | 覆盖写 |
| DELETE /v1/session | 是 | 重复删除仍 204 |
| POST /v1/rooms | 是 | 相同三元组返回已有资源 |
| PUT /v1/rooms/{id}/active | 是 | 重复设置仍 204 |
| DELETE /v1/rooms/active | 是 | 无活动房间时仍 204 |
| POST /v1/archives | 否 | 破坏性切分样本 |
| DELETE /v1/archives/{name} | 否 | 第二次返回 404 |

## 11. 与旧接口的差异摘要

- 路径全部加 `{base_path}/v1` 前缀, 不再假设域名根.
- 成功响应不再有 `{"Ok":...}` 包裹.
- 错误不再用 `200 + {"Err":...}`, 改为 HTTP 状态码与 problem+json.
- `POST /post-cookies` 改为 `PUT /v1/session`, `POST /clear-cookies` 改为 `DELETE /v1/session`.
- `POST /post-room` / `POST /clear-room` 改为房间资源接口, 支持多房间历史.
- `GET /get-records` 返回对象而非二元组数组, 并支持时间范围与 CSV.
- 归档名称进入 URL 路径, 下载不再用 query 参数.
- 新增目录接口, 支持 Web 端选房, 不再需要浏览器注入.
- 旧 mTLS 客户端证书与 `epu-cert` 工具不再需要.

完整字段级映射见 [09-migration-roadmap.md](09-migration-roadmap.md).
