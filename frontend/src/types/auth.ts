export interface BrowserNotificationSettings {
  srvlog?: { enabled: boolean; max_severity: number }
  netlog?: { enabled: boolean; max_severity: number }
  applog?: { enabled: boolean; levels: string[] }
}

export interface UserPreferences {
  browser_notifications?: BrowserNotificationSettings
}

export interface AuthUser {
  id: string
  username: string
  email?: string
  is_admin?: boolean
  is_active?: boolean
  auth_source: string
  gravatar_url: string
  preferences?: UserPreferences
  created_at?: string
  last_login_at?: string
}

export interface AdminUser {
  id: string
  username: string
  email?: string
  is_admin: boolean
  is_active: boolean
  auth_source: string
  gravatar_url: string
  created_at: string
  last_login_at?: string
}

export interface ListUsersResponse {
  data: AdminUser[]
}

export interface LoginResponse {
  user: AuthUser
}

export interface MeResponse {
  user: AuthUser
}

export interface ApiKeyInfo {
  id: string
  user_id: string
  name: string
  key_prefix: string
  scopes: string[]
  expires_at?: string
  revoked_at?: string
  last_used_at?: string
  created_at: string
}

export interface CreateKeyRequest {
  name: string
  scopes: string[]
  expires_at?: string
}

export interface CreateKeyResponse {
  key: string
  key_info: ApiKeyInfo
}

export interface ListKeysResponse {
  data: ApiKeyInfo[]
  cursor: string | null
  has_more: boolean
}
