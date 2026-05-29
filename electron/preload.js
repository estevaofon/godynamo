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
  connect: (cfg) => call('POST', '/connect', cfg),
  listTables: () => call('GET', '/tables'),
  schema: (name) => call('GET', `/tables/${encodeURIComponent(name)}/schema`),
  scan: (name, cursor, limit) => {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    if (limit) params.set('limit', String(limit))
    const qs = params.toString()
    return call('GET', `/tables/${encodeURIComponent(name)}/scan${qs ? '?' + qs : ''}`)
  },
  query: (name, body) => call('POST', `/tables/${encodeURIComponent(name)}/query`, body),
})
