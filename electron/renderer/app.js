const AWS_REGIONS = [
  'us-east-1','us-east-2','us-west-1','us-west-2','af-south-1','ap-east-1',
  'ap-south-1','ap-south-2','ap-northeast-1','ap-northeast-2','ap-northeast-3',
  'ap-southeast-1','ap-southeast-2','ap-southeast-3','ap-southeast-4',
  'ca-central-1','eu-central-1','eu-central-2','eu-west-1','eu-west-2','eu-west-3',
  'eu-south-1','eu-south-2','eu-north-1','il-central-1','me-south-1','me-central-1','sa-east-1',
]

const OPERATORS = [
  { op: 'eq', label: '= Equals' },
  { op: 'ne', label: '≠ Not Equals' },
  { op: 'gt', label: '> Greater Than' },
  { op: 'lt', label: '< Less Than' },
  { op: 'ge', label: '≥ Greater or Equal' },
  { op: 'le', label: '≤ Less or Equal' },
  { op: 'contains', label: '∋ Contains' },
  { op: 'not_contains', label: '∌ Not Contains' },
  { op: 'begins_with', label: '^ Begins With' },
  { op: 'exists', label: '∃ Exists' },
  { op: 'not_exists', label: '∄ Not Exists' },
]

const VALUE_OPS = new Set(['eq', 'ne', 'gt', 'lt', 'ge', 'le', 'contains', 'not_contains', 'begins_with'])

const state = {
  tables: [],
  currentTable: null,
  keys: { partition: '', sort: '' },
  schemaRaw: '',
  cursor: '',
  items: [],
  conditions: [],
  filterActive: false,
  mode: '',
  scanned: 0,
  selectedIdx: -1,
  selectedItem: null,
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

async function refreshTables() {
  const data = await window.api.listTables()
  state.tables = data.tables || []
  renderTableList()
}

async function selectTable(name) {
  state.currentTable = name
  state.cursor = ''
  state.items = []
  state.conditions = []
  state.filterActive = false
  state.mode = ''
  state.scanned = 0
  state.selectedIdx = -1
  state.selectedItem = null
  $('current-table').textContent = name
  $('status').textContent = 'Loading…'
  $('mode-badge').textContent = ''
  $('schema-btn').disabled = true
  $('filter-btn').disabled = true
  $('new-item-btn').disabled = true
  $('more-btn').disabled = true
  hide($('filter-panel'))
  renderFilterRows()
  renderTableList()
  try {
    const schema = await window.api.schema(name)
    state.keys = {
      partition: (schema.info && schema.info.PartitionKey) || '',
      sort: (schema.info && schema.info.SortKey) || '',
    }
    state.schemaRaw = schema.rawJSON || JSON.stringify(schema.info, null, 2)
    $('schema-btn').disabled = false
    $('filter-btn').disabled = false
    $('new-item-btn').disabled = false
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}

function pageSize() {
  return parseInt($('page-size').value, 10) || 500
}

function activeConditions() {
  return state.conditions
    .filter((c) => c.name.trim() !== '' && (!VALUE_OPS.has(c.op) || c.value.trim() !== ''))
    .map((c) => ({ name: c.name, op: c.op, value: c.value }))
}

async function loadPage(reset) {
  const cursor = reset ? '' : state.cursor
  try {
    let data
    if (state.filterActive) {
      data = await window.api.query(state.currentTable, {
        conditions: activeConditions(),
        limit: pageSize(),
        cursor,
      })
      state.mode = data.mode || ''
    } else {
      data = await window.api.scan(state.currentTable, cursor, pageSize())
      state.mode = ''
    }
    if (reset) {
      state.items = []
      state.scanned = 0
      state.selectedIdx = -1
      state.selectedItem = null
    }
    state.items = state.items.concat(data.items || [])
    state.cursor = data.cursor || ''
    if (state.filterActive) {
      state.scanned += data.scannedCount || 0
    }
    $('more-btn').disabled = !state.cursor
    updateStatus()
    renderGrid()
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
    $('more-btn').disabled = !state.cursor
  }
}

function updateStatus() {
  let s = `${state.items.length} returned`
  if (state.filterActive) {
    s += ` · scanned ${state.scanned}`
    $('mode-badge').textContent = state.mode ? state.mode.toUpperCase() : ''
  } else {
    $('mode-badge').textContent = ''
  }
  if (state.cursor) s += ' · more available'
  $('status').textContent = s
}

function renderFilterRows() {
  const wrap = $('filter-rows')
  wrap.innerHTML = ''
  state.conditions.forEach((cond, i) => {
    const row = document.createElement('div')
    row.className = 'filter-row'

    const nameIn = document.createElement('input')
    nameIn.type = 'text'
    nameIn.placeholder = 'attribute'
    nameIn.value = cond.name
    nameIn.addEventListener('input', () => { state.conditions[i].name = nameIn.value })

    const opSel = document.createElement('select')
    OPERATORS.forEach((o) => {
      const opt = document.createElement('option')
      opt.value = o.op
      opt.textContent = o.label
      if (o.op === cond.op) opt.selected = true
      opSel.appendChild(opt)
    })
    opSel.addEventListener('change', () => { state.conditions[i].op = opSel.value })

    const valIn = document.createElement('input')
    valIn.type = 'text'
    valIn.placeholder = 'value'
    valIn.value = cond.value
    valIn.addEventListener('input', () => { state.conditions[i].value = valIn.value })

    const rm = document.createElement('button')
    rm.textContent = '✕'
    rm.className = 'filter-remove'
    rm.addEventListener('click', () => removeCondition(i))

    row.appendChild(nameIn)
    row.appendChild(opSel)
    row.appendChild(valIn)
    row.appendChild(rm)
    wrap.appendChild(row)
  })
}

function addCondition() {
  state.conditions.push({ name: '', op: 'eq', value: '' })
  renderFilterRows()
}

function removeCondition(i) {
  state.conditions.splice(i, 1)
  renderFilterRows()
}

function toggleFilter() {
  const panel = $('filter-panel')
  if (panel.classList.contains('hidden')) {
    if (state.conditions.length === 0) addCondition()
    show(panel)
  } else {
    hide(panel)
  }
}

async function applyFilter() {
  state.filterActive = activeConditions().length > 0
  state.cursor = ''
  await loadPage(true)
}

async function clearFilter() {
  state.conditions = []
  state.filterActive = false
  renderFilterRows()
  state.cursor = ''
  await loadPage(true)
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
  state.selectedIdx = idx
  state.selectedItem = state.items[idx]
  $('detail-title').textContent = 'Item'
  $('detail-body').textContent = JSON.stringify(state.items[idx], null, 2)
  show($('detail-edit'))
  show($('detail-delete'))
  show($('detail'))
}

function showSchema() {
  $('detail-title').textContent = 'Schema: ' + state.currentTable
  $('detail-body').textContent = state.schemaRaw || ''
  hide($('detail-edit'))
  hide($('detail-delete'))
  show($('detail'))
}

function openNewItem() {
  const tmpl = {}
  if (state.keys.partition) tmpl[state.keys.partition] = ''
  if (state.keys.sort) tmpl[state.keys.sort] = ''
  $('editor-title').textContent = 'New item'
  $('editor-text').value = JSON.stringify(tmpl, null, 2)
  $('editor-error').textContent = ''
  show($('editor'))
  $('editor-text').focus()
}

function openEditItem() {
  if (!state.selectedItem) return
  $('editor-title').textContent = 'Edit item'
  $('editor-text').value = JSON.stringify(state.selectedItem, null, 2)
  $('editor-error').textContent = ''
  hide($('detail'))
  show($('editor'))
  $('editor-text').focus()
}

async function saveEditor() {
  const text = $('editor-text').value
  try {
    JSON.parse(text)
  } catch (e) {
    $('editor-error').textContent = 'Invalid JSON: ' + e.message
    return
  }
  $('editor-error').textContent = ''
  $('editor-save').disabled = true
  try {
    await window.api.saveItem(state.currentTable, text)
    hide($('editor'))
    await loadPage(true)
  } catch (err) {
    $('editor-error').textContent = err.message
  } finally {
    $('editor-save').disabled = false
  }
}

function confirmDelete() {
  if (!state.selectedItem) return
  $('confirm-text').textContent = 'Delete this item? This cannot be undone.'
  show($('confirm'))
}

async function doDelete() {
  hide($('confirm'))
  if (!state.selectedItem) return
  const json = JSON.stringify(state.selectedItem)
  try {
    await window.api.deleteItem(state.currentTable, json)
    hide($('detail'))
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}

function openCreateTable() {
  $('ct-name').value = ''
  $('ct-pk').value = ''
  $('ct-pktype').value = 'S'
  $('ct-sk').value = ''
  $('ct-sktype').value = 'S'
  $('ct-billing').value = 'PAY_PER_REQUEST'
  $('ct-rcu').value = '5'
  $('ct-wcu').value = '5'
  $('ct-error').textContent = ''
  show($('createtable'))
}

async function submitCreateTable() {
  const form = {
    name: $('ct-name').value.trim(),
    pk: $('ct-pk').value.trim(),
    pkType: $('ct-pktype').value,
    sk: $('ct-sk').value.trim(),
    skType: $('ct-sktype').value,
    billingMode: $('ct-billing').value,
    rcu: parseInt($('ct-rcu').value, 10) || 0,
    wcu: parseInt($('ct-wcu').value, 10) || 0,
  }
  if (!form.name || !form.pk) {
    $('ct-error').textContent = 'Table name and partition key are required.'
    return
  }
  $('ct-error').textContent = ''
  $('ct-create').disabled = true
  try {
    await window.api.createTable(form)
    hide($('createtable'))
    await refreshTables()
  } catch (err) {
    $('ct-error').textContent = err.message
  } finally {
    $('ct-create').disabled = false
  }
}

window.addEventListener('DOMContentLoaded', () => {
  initConnectScreen()
  $('table-filter').addEventListener('input', renderTableList)
  $('schema-btn').addEventListener('click', showSchema)
  $('filter-btn').addEventListener('click', toggleFilter)
  $('filter-add').addEventListener('click', addCondition)
  $('filter-apply').addEventListener('click', applyFilter)
  $('filter-clear').addEventListener('click', clearFilter)
  $('more-btn').addEventListener('click', () => loadPage(false))
  $('page-size').addEventListener('change', () => { if (state.currentTable) loadPage(true) })
  $('detail-close').addEventListener('click', () => hide($('detail')))
  $('new-item-btn').addEventListener('click', openNewItem)
  $('create-table-btn').addEventListener('click', openCreateTable)
  $('detail-edit').addEventListener('click', openEditItem)
  $('detail-delete').addEventListener('click', confirmDelete)
  $('editor-close').addEventListener('click', () => hide($('editor')))
  $('editor-save').addEventListener('click', saveEditor)
  $('ct-close').addEventListener('click', () => hide($('createtable')))
  $('ct-create').addEventListener('click', submitCreateTable)
  $('confirm-no').addEventListener('click', () => hide($('confirm')))
  $('confirm-yes').addEventListener('click', doDelete)
})
