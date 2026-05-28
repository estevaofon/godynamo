const AWS_REGIONS = [
  'us-east-1','us-east-2','us-west-1','us-west-2','af-south-1','ap-east-1',
  'ap-south-1','ap-south-2','ap-northeast-1','ap-northeast-2','ap-northeast-3',
  'ap-southeast-1','ap-southeast-2','ap-southeast-3','ap-southeast-4',
  'ca-central-1','eu-central-1','eu-central-2','eu-west-1','eu-west-2','eu-west-3',
  'eu-south-1','eu-south-2','eu-north-1','il-central-1','me-south-1','me-central-1','sa-east-1',
]

const state = {
  tables: [],
  currentTable: null,
  keys: { partition: '', sort: '' },
  schemaRaw: '',
  cursor: '',
  items: [],
}

const $ = (id) => document.getElementById(id)
const show = (el) => el.classList.remove('hidden')
const hide = (el) => el.classList.add('hidden')

function initConnectScreen() {
  const regionSel = $('region')
  AWS_REGIONS.forEach((r) => {
    const opt = document.createElement('option')
    opt.value = r
    opt.textContent = r
    regionSel.appendChild(opt)
  })
  document.querySelectorAll('input[name="mode"]').forEach((radio) => {
    radio.addEventListener('change', () => {
      const mode = document.querySelector('input[name="mode"]:checked').value
      if (mode === 'aws') { show($('aws-fields')); hide($('local-fields')) }
      else { hide($('aws-fields')); show($('local-fields')) }
    })
  })
  $('connect-btn').addEventListener('click', onConnect)
}

async function onConnect() {
  const mode = document.querySelector('input[name="mode"]:checked').value
  const cfg = { mode }
  if (mode === 'aws') cfg.region = $('region').value
  else cfg.endpoint = $('endpoint').value

  $('connect-error').textContent = ''
  $('connect-btn').disabled = true
  try {
    const data = await window.api.connect(cfg)
    state.tables = data.tables || []
    renderTableList()
    hide($('connect-screen'))
    show($('main-screen'))
  } catch (err) {
    $('connect-error').textContent = err.message
  } finally {
    $('connect-btn').disabled = false
  }
}

function renderTableList() {
  const filter = $('table-filter').value.toLowerCase()
  const ul = $('table-list')
  ul.innerHTML = ''
  state.tables
    .filter((t) => t.toLowerCase().includes(filter))
    .forEach((t) => {
      const li = document.createElement('li')
      li.textContent = t
      li.className = t === state.currentTable ? 'active' : ''
      li.addEventListener('click', () => selectTable(t))
      ul.appendChild(li)
    })
}

async function selectTable(name) {
  state.currentTable = name
  state.cursor = ''
  state.items = []
  $('current-table').textContent = name
  $('status').textContent = 'Loading…'
  $('schema-btn').disabled = true
  $('more-btn').disabled = true
  renderTableList()
  try {
    const schema = await window.api.schema(name)
    state.keys = {
      partition: (schema.info && schema.info.PartitionKey) || '',
      sort: (schema.info && schema.info.SortKey) || '',
    }
    state.schemaRaw = schema.rawJSON || JSON.stringify(schema.info, null, 2)
    $('schema-btn').disabled = false
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}

async function loadPage(reset) {
  try {
    const data = await window.api.scan(state.currentTable, reset ? '' : state.cursor)
    if (reset) state.items = []
    state.items = state.items.concat(data.items || [])
    state.cursor = data.cursor || ''
    $('more-btn').disabled = !state.cursor
    $('status').textContent = `${state.items.length} items` + (state.cursor ? ' (more available)' : '')
    renderGrid()
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
    $('more-btn').disabled = !state.cursor
  }
}

function columnOrder() {
  const cols = new Set()
  state.items.forEach((it) => Object.keys(it).forEach((k) => cols.add(k)))
  const { partition, sort } = state.keys
  const ordered = []
  if (partition && cols.has(partition)) { ordered.push(partition); cols.delete(partition) }
  if (sort && cols.has(sort)) { ordered.push(sort); cols.delete(sort) }
  return ordered.concat([...cols].sort())
}

function cellText(value) {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function renderGrid() {
  const cols = columnOrder()
  const thead = $('grid').querySelector('thead')
  const tbody = $('grid').querySelector('tbody')
  thead.innerHTML = ''
  tbody.innerHTML = ''

  const hr = document.createElement('tr')
  cols.forEach((c) => {
    const th = document.createElement('th')
    th.textContent = c
    hr.appendChild(th)
  })
  thead.appendChild(hr)

  state.items.forEach((item, idx) => {
    const tr = document.createElement('tr')
    cols.forEach((c) => {
      const td = document.createElement('td')
      const text = cellText(item[c])
      td.textContent = text.length > 80 ? text.slice(0, 77) + '…' : text
      tr.appendChild(td)
    })
    tr.addEventListener('click', () => showItem(idx))
    tbody.appendChild(tr)
  })
}

function showItem(idx) {
  $('detail-title').textContent = 'Item'
  $('detail-body').textContent = JSON.stringify(state.items[idx], null, 2)
  show($('detail'))
}

function showSchema() {
  $('detail-title').textContent = 'Schema: ' + state.currentTable
  $('detail-body').textContent = state.schemaRaw || ''
  show($('detail'))
}

window.addEventListener('DOMContentLoaded', () => {
  initConnectScreen()
  $('table-filter').addEventListener('input', renderTableList)
  $('schema-btn').addEventListener('click', showSchema)
  $('more-btn').addEventListener('click', () => loadPage(false))
  $('detail-close').addEventListener('click', () => hide($('detail')))
})
