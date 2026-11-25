export interface ApiErrorBody {
  code: string
  message: string
}

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

export interface User {
  id: number
  username: string
  must_change_password: boolean
}

export interface MenuNode {
  id: number
  parent_id: number | null
  key: string
  title: string
  icon: string
  route: string
  sort_order: number
  visible: boolean
  is_builtin: boolean
  children: MenuNode[]
}

export interface MenuTreeInput {
  id: number
  title?: string
  icon?: string
  visible?: boolean
  children?: MenuTreeInput[]
}

export interface PanelSettings {
  panel_name: string
  listen_addr: string
  server_dir: string
  database_dir: string
  project_dir: string
  nginx_dir: string
  memory_limit_mb: number
  session_ttl_hours: number
  safe_entry: string
}

export interface SystemSnapshot {
  timestamp: string
  hostname: string
  os: string
  platform: string
  arch: string
  uptime_sec: number
  go_version: string
  driver_mode: string
  process_mem_bytes: number
  cpu: {
    cores: number
    usage_percent: number
    load1: number
    load5: number
    load15: number
  }
  memory: {
    total_bytes: number
    used_bytes: number
    used_percent: number
  }
  disk: {
    path: string
    total_bytes: number
    used_bytes: number
    free_bytes: number
    used_percent: number
  }
  network: {
    bytes_sent: number
    bytes_recv: number
  }
  directories: Array<{
    key: string
    label: string
    path: string
    exists: boolean
  }>
}

export interface TaskItem {
  id: string
  kind: string
  title: string
  status: 'pending' | 'running' | 'success' | 'failed'
  progress: number
  error_msg: string
  created_at: string
}

export interface AuditLogItem {
  id: number
  username: string
  action: string
  target: string
  detail: string
  ip: string
  created_at: string
}

export interface RuntimeItem {
  id: number
  kind: string
  version: string
  install_dir: string
  binary_path: string
  status: string
  created_at: string
}

export interface ProjectItem {
  id: number
  name: string
  lang: 'go' | 'java' | 'python'
  root_dir: string
  entry: string
  args: string
  runtime_id: number | null
  port: number
  run_user: string
  env: Record<string, string>
  service_name: string
  status: string
  created_at: string
}

export interface NginxInfo {
  installed: boolean
  binary_path: string
  config_path: string
  status: string
  active: boolean
  config_text: string
}

export interface SiteItem {
  id: number
  domain: string
  extra_domains: string
  listen_port: number
  site_type: 'proxy' | 'static'
  root_dir: string
  proxy_pass: string
  ssl_enabled: boolean
  ssl_cert_id: number | null
  config_path: string
  status: string
  created_at: string
}

export interface SSLCertItem {
  id: number
  domain: string
  provider: string
  email: string
  cert_path: string
  key_path: string
  expires_at: string
  auto_renew: boolean
  status: string
  created_at: string
}

export interface DBInstance {
  id: number
  engine: string
  installed: boolean
  version: string
  port: number
  install_dir: string
  data_dir: string
  config_path: string
  service_name: string
  root_user: string
  root_password: string
  status: string
}

export interface DBSchema {
  id: number
  instance_id: number
  db_name: string
  db_user: string
  privileges: string
  charset: string
  created_at: string
}

export interface DBBackup {
  id: number
  instance_id: number
  db_name: string
  file_path: string
  size_bytes: number
  status: string
  created_at: string
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...(init.headers || {}),
    },
    ...init,
  })

  const text = await res.text()
  const data = text ? JSON.parse(text) : {}
  if (!res.ok) {
    const errBody = data as Partial<ApiErrorBody>
    throw new ApiError(
      res.status,
      errBody.code || 'HTTP_ERROR',
      errBody.message || `请求失败 (${res.status})`,
    )
  }
  return data as T
}

export const api = {
  login(username: string, password: string) {
    return request<{ token: string; user: User }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    })
  },
  logout() {
    return request<{ status: string }>('/api/v1/auth/logout', { method: 'POST' })
  },
  me() {
    return request<{ user: User; panel_name: string; driver_mode: string; dev: boolean }>(
      '/api/v1/auth/me',
    )
  },
  changePassword(oldPassword: string, newPassword: string) {
    return request<{ status: string; must_change_password: boolean }>(
      '/api/v1/auth/change-password',
      {
        method: 'POST',
        body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
      },
    )
  },
  snapshot() {
    return request<SystemSnapshot>('/api/v1/monitor/snapshot')
  },
  getSettings() {
    return request<PanelSettings>('/api/v1/settings')
  },
  updateSettings(payload: PanelSettings) {
    return request<PanelSettings>('/api/v1/settings', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  getMenus() {
    return request<{ items: MenuNode[] }>('/api/v1/menus')
  },
  reorderMenus(items: MenuTreeInput[]) {
    return request<{ items: MenuNode[] }>('/api/v1/menus/reorder', {
      method: 'PUT',
      body: JSON.stringify({ items }),
    })
  },
  resetMenus() {
    return request<{ items: MenuNode[] }>('/api/v1/menus/reset', {
      method: 'POST',
    })
  },
  listTasks() {
    return request<{ items: TaskItem[] }>('/api/v1/tasks')
  },
  getTask(id: string) {
    return request<TaskItem>(`/api/v1/tasks/${id}`)
  },
  getTaskLogs(id: string) {
    return request<{ logs: string }>(`/api/v1/tasks/${id}/logs`)
  },
  listAuditLogs() {
    return request<{ items: AuditLogItem[] }>('/api/v1/audit-logs')
  },

  // Runtimes & Projects
  listRuntimes() {
    return request<{ items: RuntimeItem[] }>('/api/v1/runtimes')
  },
  installRuntime(kind: string, version: string) {
    return request<TaskItem>('/api/v1/runtimes/install', {
      method: 'POST',
      body: JSON.stringify({ kind, version }),
    })
  },
  deleteRuntime(id: number) {
    return request<{ status: string }>(`/api/v1/runtimes/${id}`, { method: 'DELETE' })
  },
  listProjects(lang?: string) {
    const q = lang ? `?lang=${encodeURIComponent(lang)}` : ''
    return request<{ items: ProjectItem[] }>(`/api/v1/projects${q}`)
  },
  createProject(payload: {
    name: string
    lang: string
    entry: string
    args: string
    runtime_id: number | null
    port: number
    run_user: string
    env: Record<string, string>
  }) {
    return request<ProjectItem>('/api/v1/projects', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  actionProject(id: number, action: 'start' | 'stop' | 'restart') {
    return request<ProjectItem>(`/api/v1/projects/${id}/action`, {
      method: 'POST',
      body: JSON.stringify({ action }),
    })
  },
  getProjectLogs(id: number) {
    return request<{ logs: string }>(`/api/v1/projects/${id}/logs`)
  },
  deleteProject(id: number) {
    return request<{ status: string }>(`/api/v1/projects/${id}`, { method: 'DELETE' })
  },

  // Nginx & SSL
  getNginxInfo() {
    return request<NginxInfo>('/api/v1/nginx/info')
  },
  installNginx() {
    return request<TaskItem>('/api/v1/nginx/install', { method: 'POST' })
  },
  actionNginx(action: 'start' | 'stop' | 'restart' | 'reload' | 'test') {
    return request<NginxInfo>('/api/v1/nginx/action', {
      method: 'POST',
      body: JSON.stringify({ action }),
    })
  },
  updateNginxConfig(configText: string) {
    return request<{ status: string }>('/api/v1/nginx/config', {
      method: 'PUT',
      body: JSON.stringify({ config_text: configText }),
    })
  },
  listSites() {
    return request<{ items: SiteItem[] }>('/api/v1/nginx/sites')
  },
  createSite(payload: {
    domain: string
    extra_domains: string
    listen_port: number
    site_type: 'proxy' | 'static'
    proxy_pass: string
    ssl_enabled: boolean
    ssl_cert_id: number | null
  }) {
    return request<SiteItem>('/api/v1/nginx/sites', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  getSiteConfig(id: number) {
    return request<{ config_text: string }>(`/api/v1/nginx/sites/${id}/config`)
  },
  updateSiteConfig(id: number, configText: string) {
    return request<{ status: string }>(`/api/v1/nginx/sites/${id}/config`, {
      method: 'PUT',
      body: JSON.stringify({ config_text: configText }),
    })
  },
  deleteSite(id: number) {
    return request<{ status: string }>(`/api/v1/nginx/sites/${id}`, { method: 'DELETE' })
  },
  listCerts() {
    return request<{ items: SSLCertItem[] }>('/api/v1/ssl/certs')
  },
  issueCert(payload: {
    domain: string
    provider: 'selfsigned' | 'manual' | 'letsencrypt'
    email?: string
    cert_pem?: string
    key_pem?: string
  }) {
    return request<SSLCertItem>('/api/v1/ssl/certs', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  deleteCert(id: number) {
    return request<{ status: string }>(`/api/v1/ssl/certs/${id}`, { method: 'DELETE' })
  },

  // Databases
  getDB(engine: string) {
    return request<DBInstance>(`/api/v1/databases/${engine}`)
  },
  installDB(engine: string, payload: { version?: string; port?: number; root_password?: string }) {
    return request<TaskItem>(`/api/v1/databases/${engine}/install`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  actionDB(engine: string, action: 'start' | 'stop' | 'restart') {
    return request<DBInstance>(`/api/v1/databases/${engine}/action`, {
      method: 'POST',
      body: JSON.stringify({ action }),
    })
  },
  getDBConfig(engine: string) {
    return request<{ config_text: string }>(`/api/v1/databases/${engine}/config`)
  },
  updateDBConfig(engine: string, configText: string) {
    return request<{ status: string }>(`/api/v1/databases/${engine}/config`, {
      method: 'PUT',
      body: JSON.stringify({ config_text: configText }),
    })
  },
  listDBSchemas(engine: string) {
    return request<{ items: DBSchema[] }>(`/api/v1/databases/${engine}/schemas`)
  },
  createDBSchema(engine: string, payload: { db_name: string; db_user: string; charset: string }) {
    return request<DBSchema>(`/api/v1/databases/${engine}/schemas`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  deleteDBSchema(engine: string, id: number) {
    return request<{ status: string }>(`/api/v1/databases/${engine}/schemas/${id}`, {
      method: 'DELETE',
    })
  },
  listDBBackups(engine: string) {
    return request<{ items: DBBackup[] }>(`/api/v1/databases/${engine}/backups`)
  },
  createDBBackup(engine: string, dbName: string) {
    return request<DBBackup>(`/api/v1/databases/${engine}/backups`, {
      method: 'POST',
      body: JSON.stringify({ db_name: dbName }),
    })
  },
}
