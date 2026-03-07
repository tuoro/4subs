export async function apiRequest(path, options = {}) {
  const response = await fetch(path, {
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers || {})
    },
    ...options
  })

  const text = await response.text()
  const payload = text ? JSON.parse(text) : null

  if (!response.ok) {
    const message = payload?.error || `请求失败: ${response.status}`
    throw new Error(message)
  }

  return payload
}

export function getOverview() {
  return apiRequest('/api/v1/overview')
}

export function getPipeline() {
  return apiRequest('/api/v1/pipeline')
}

export function getSettings() {
  return apiRequest('/api/v1/settings')
}

export function saveSettings(payload) {
  return apiRequest('/api/v1/settings', {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function listMedia(limit = 200) {
  return apiRequest(`/api/v1/media?limit=${limit}`)
}

export function scanMedia() {
  return apiRequest('/api/v1/media/scan', {
    method: 'POST'
  })
}

export function listJobs(limit = 100) {
  return apiRequest(`/api/v1/jobs?limit=${limit}`)
}

export function createJob(payload) {
  return apiRequest('/api/v1/jobs', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

