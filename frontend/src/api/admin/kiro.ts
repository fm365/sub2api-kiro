import { apiClient } from '../client'

export interface KiroDeviceAuthRequest {
  auth_type: 'builder-id' | 'identity-center'
  region?: string
  start_url?: string
  proxy_id?: number
}

export interface KiroSocialAuthRequest {
  provider: 'google' | 'github' | 'cognito'
  region?: string
  redirect_uri: string
  proxy_id?: number
}

export interface KiroCompleteSocialAuthRequest {
  session_id: string
  callback_or_code: string
}

export interface KiroDeviceAuthResponse {
  session_id: string
  auth_url: string
  user_code: string
  device_code: string
  expires_at: string
  interval_seconds: number
  auth_method: string
  region: string
  start_url?: string
}

export interface KiroSessionStatus {
  session_id: string
  type?: string
  status: 'pending' | 'completed' | 'error' | 'expired'
  error?: string
  user_code?: string
  auth_url?: string
  credentials?: Record<string, unknown>
}

export interface KiroDetectedToken {
  source: string
  file_path: string
  file_name: string
  is_expired: boolean
  is_usable: boolean
  has_client_credentials: boolean
  client_credentials_source?: string
  client_credentials_expires_at?: unknown
  credentials: Record<string, unknown>
}

export interface KiroScanTokensResponse {
  tokens: KiroDetectedToken[]
  errors?: string[]
}

export async function startDeviceAuth(payload: KiroDeviceAuthRequest): Promise<KiroDeviceAuthResponse> {
  const { data } = await apiClient.post<KiroDeviceAuthResponse>('/admin/kiro/oauth/device', payload)
  return data
}

export async function startSocialAuth(payload: KiroSocialAuthRequest): Promise<KiroDeviceAuthResponse> {
  const { data } = await apiClient.post<KiroDeviceAuthResponse>('/admin/kiro/oauth/social', payload)
  return data
}

export async function completeSocialAuth(payload: KiroCompleteSocialAuthRequest): Promise<KiroSessionStatus> {
  const { data } = await apiClient.post<KiroSessionStatus>('/admin/kiro/oauth/social/complete', payload)
  return data
}

export async function getSession(sessionId: string): Promise<KiroSessionStatus> {
  const { data } = await apiClient.get<KiroSessionStatus>(`/admin/kiro/oauth/session/${encodeURIComponent(sessionId)}`)
  return data
}

export async function cancelSession(sessionId: string): Promise<{ cancelled: boolean }> {
  const { data } = await apiClient.delete<{ cancelled: boolean }>(`/admin/kiro/oauth/session/${encodeURIComponent(sessionId)}`)
  return data
}

export async function scanTokens(): Promise<KiroScanTokensResponse> {
  const { data } = await apiClient.get<KiroScanTokensResponse>('/admin/kiro/tokens/scan')
  return data
}

export default { startDeviceAuth, startSocialAuth, completeSocialAuth, getSession, cancelSession, scanTokens }
