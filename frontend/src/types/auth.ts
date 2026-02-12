export interface AuthUser {
  id: string
  username: string
  email?: string
  gravatar_url: string
  created_at?: string
  last_login_at?: string
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
