const { contextBridge, ipcRenderer } = require('electron')

let info = null
async function bridge() {
  if (!info) info = await ipcRenderer.invoke('bridge-info')
  return info
}

async function call(method, pathName, body) {
  const { baseUrl, token } = await bridge()
  const opts = { method, headers: { Authorization: `Bearer ${token}` } }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  const res = await fetch(baseUrl + pathName, opts)
  const text = await res.text()
  let data = {}
  try {
    data = text ? JSON.parse(text) : {}
  } catch {
    data = { error: text }
  }
  if (!res.ok) {
    throw new Error(data.error || `request failed (${res.status})`)
  }
  return data
}

// The token is captured in this closure and never placed on window or in a URL.
contextBridge.exposeInMainWorld('api', {
  listProfiles: () => call('GET', '/profiles'),
  discover: (profile) => call('POST', '/discover', { profile }),
  schema: (name, region) =>
    call('GET', `/tables/${encodeURIComponent(name)}/schema?region=${encodeURIComponent(region)}`),
  scan: (name, region, cursor, limit) => {
    const params = new URLSearchParams({ region })
    if (cursor) params.set('cursor', cursor)
    if (limit) params.set('limit', String(limit))
    return call('GET', `/tables/${encodeURIComponent(name)}/scan?${params.toString()}`)
  },
  query: (name, region, body) =>
    call('POST', `/tables/${encodeURIComponent(name)}/query?region=${encodeURIComponent(region)}`, body),
  saveItem: (name, region, json) =>
    call('POST', `/tables/${encodeURIComponent(name)}/item?region=${encodeURIComponent(region)}`, { json }),
  deleteItem: (name, region, json) =>
    call('DELETE', `/tables/${encodeURIComponent(name)}/item?region=${encodeURIComponent(region)}`, { json }),
  createTable: (form, region) =>
    call('POST', `/tables?region=${encodeURIComponent(region)}`, form),
  exportFile: (defaultName, content) => ipcRenderer.invoke('export-file', { defaultName, content }),
})
