import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { KiroDetectedToken, KiroSessionStatus } from '@/api/admin/kiro'

export type KiroAuthType = 'builder-id' | 'identity-center'
export type KiroSocialProvider = 'google' | 'github' | 'cognito'

export function useKiroOAuth() {
  const appStore = useAppStore()
  const { t } = useI18n()

  const authUrl = ref('')
  const userCode = ref('')
  const sessionId = ref('')
  const status = ref<KiroSessionStatus['status'] | ''>('')
  const credentials = ref<Record<string, unknown> | null>(null)
  const detectedTokens = ref<KiroDetectedToken[]>([])
  const scanErrors = ref<string[]>([])
  const loading = ref(false)
  const polling = ref(false)
  const error = ref('')
  let pollTimer: ReturnType<typeof window.setTimeout> | null = null

  const stopPolling = () => {
    if (pollTimer) {
      window.clearTimeout(pollTimer)
      pollTimer = null
    }
    polling.value = false
  }

  const resetState = () => {
    stopPolling()
    authUrl.value = ''
    userCode.value = ''
    sessionId.value = ''
    status.value = ''
    credentials.value = null
    detectedTokens.value = []
    scanErrors.value = []
    loading.value = false
    error.value = ''
  }

  const errorMessage = (err: any, fallback: string) => {
    return err?.message || err?.response?.data?.message || err?.response?.data?.detail || fallback
  }

  const pollSession = async (onComplete?: (creds: Record<string, unknown>) => void) => {
    if (!sessionId.value) return
    polling.value = true
    try {
      const result = await adminAPI.kiro.getSession(sessionId.value)
      status.value = result.status
      if (result.status === 'completed' && result.credentials) {
        credentials.value = result.credentials
        stopPolling()
        onComplete?.(result.credentials)
        return
      }
      if (result.status === 'error' || result.status === 'expired') {
        error.value = result.error || t('admin.accounts.oauth.kiro.deviceAuthFailed')
        stopPolling()
        appStore.showError(error.value)
        return
      }
      pollTimer = setTimeout(() => pollSession(onComplete), 3000)
    } catch (err: any) {
      error.value = errorMessage(err, t('admin.accounts.oauth.kiro.deviceAuthFailed'))
      stopPolling()
      appStore.showError(error.value)
    }
  }

  const startDeviceAuth = async (params: {
    authType: KiroAuthType
    region?: string
    startUrl?: string
    proxyId?: number | null
    onComplete?: (creds: Record<string, unknown>) => void
  }): Promise<boolean> => {
    loading.value = true
    error.value = ''
    credentials.value = null
    stopPolling()

    try {
      const payload: Record<string, unknown> = {
        auth_type: params.authType,
        region: params.region || 'us-east-1'
      }
      const startUrl = params.startUrl?.trim()
      if (startUrl) payload.start_url = startUrl
      if (params.proxyId) payload.proxy_id = params.proxyId

      const result = await adminAPI.kiro.startDeviceAuth(payload as any)
      authUrl.value = result.auth_url
      userCode.value = result.user_code
      sessionId.value = result.session_id
      status.value = 'pending'
      pollSession(params.onComplete)
      return true
    } catch (err: any) {
      error.value = errorMessage(err, t('admin.accounts.oauth.kiro.failedToStartDeviceAuth'))
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
    }
  }

  const startSocialAuth = async (params: {
    provider: KiroSocialProvider
    region?: string
    redirectUri: string
    proxyId?: number | null
    onComplete?: (creds: Record<string, unknown>) => void
  }): Promise<boolean> => {
    loading.value = true
    error.value = ''
    credentials.value = null
    stopPolling()

    try {
      const payload: Record<string, unknown> = {
        provider: params.provider,
        region: params.region || 'us-east-1',
        redirect_uri: params.redirectUri
      }
      if (params.proxyId) payload.proxy_id = params.proxyId
      const result = await adminAPI.kiro.startSocialAuth(payload as any)
      authUrl.value = result.auth_url
      sessionId.value = result.session_id
      status.value = 'pending'
      pollSession(params.onComplete)
      return true
    } catch (err: any) {
      error.value = errorMessage(err, t('admin.accounts.oauth.kiro.failedToStartSocialAuth'))
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
    }
  }

  const completeSocialAuth = async (callbackOrCode: string, onComplete?: (creds: Record<string, unknown>) => void): Promise<boolean> => {
    if (!sessionId.value) return false
    loading.value = true
    error.value = ''
    try {
      const result = await adminAPI.kiro.completeSocialAuth({
        session_id: sessionId.value,
        callback_or_code: callbackOrCode
      })
      status.value = result.status
      if (result.status === 'completed' && result.credentials) {
        credentials.value = result.credentials
        onComplete?.(result.credentials)
        return true
      }
      error.value = result.error || t('admin.accounts.oauth.kiro.socialAuthFailed')
      appStore.showError(error.value)
      return false
    } catch (err: any) {
      error.value = errorMessage(err, t('admin.accounts.oauth.kiro.socialAuthFailed'))
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
    }
  }

  const scanTokens = async (): Promise<KiroDetectedToken[]> => {
    loading.value = true
    error.value = ''
    try {
      const result = await adminAPI.kiro.scanTokens()
      detectedTokens.value = result.tokens || []
      scanErrors.value = result.errors || []
      return detectedTokens.value
    } catch (err: any) {
      error.value = errorMessage(err, t('admin.accounts.oauth.kiro.scanTokensFailed'))
      appStore.showError(error.value)
      return []
    } finally {
      loading.value = false
    }
  }

  return {
    authUrl,
    userCode,
    sessionId,
    status,
    credentials,
    detectedTokens,
    scanErrors,
    loading,
    polling,
    error,
    resetState,
    stopPolling,
    startDeviceAuth,
    startSocialAuth,
    completeSocialAuth,
    scanTokens
  }
}
