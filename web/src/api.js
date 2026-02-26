const API_BASE = '/api/v1'

async function request(path, options = {}) {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers || {})
    },
    ...options
  })

  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error(data.error || `Request failed: ${response.status}`)
  }
  return data
}

export function getHealth() {
  return request('/health')
}

export function getJobs() {
  return request('/jobs?limit=100')
}

export function triggerScan() {
  return request('/scan', { method: 'POST' })
}

export function getMedia(options = {}) {
  const params = new URLSearchParams()
  if (options.missingOnly) {
    params.set('missing_sub', 'true')
  }
  if (options.limit) {
    params.set('limit', String(options.limit))
  }
  const suffix = params.toString() ? `?${params.toString()}` : ''
  return request(`/media${suffix}`)
}

export function searchMediaSubtitles(mediaId) {
  return request(`/media/${mediaId}/search-subtitles`, {
    method: 'POST'
  })
}

export function getMediaCandidates(mediaId, limit = 100) {
  return request(`/media/${mediaId}/candidates?limit=${limit}`)
}

export function downloadCandidate(candidateId) {
  return request(`/candidates/${candidateId}/download`, {
    method: 'POST'
  })
}

export function getSettings() {
  return request('/settings')
}

export function saveSettings(payload) {
  return request('/settings', {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function getProviders() {
  return request('/providers')
}

export function saveProviderCredential(provider, payload) {
  return request(`/providers/${provider}/credential`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}
