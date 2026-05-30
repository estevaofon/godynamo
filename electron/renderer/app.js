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

const conn = { profile: '', profiles: [], regions: [], tabs: [], activeId: null, nextId: 1 }
let state = null // alias to the active tab object, or null when no tab is open

function newTab(name, region) {
  return {
    id: conn.nextId++, currentTable: name, region,
    loaded: false, status: '', scrollTop: 0, filterOpen: false,
    keys: { partition: '', sort: '' }, indexes: [], schemaRaw: '',
    cursor: '', items: [], rendered: [],
    conditions: [], filterActive: false, mode: '', scanned: 0,
    strategy: { mode: '', index: '' }, override: { mode: 'auto', index: '' },
    sort: { column: null, dir: 'asc' }, selectedIdx: -1, selectedItem: null, detailText: '',
  }
}

function showEmptyState() {
  hide($('grid-wrap'))
  show($('content-empty'))
  hide($('filter-panel'))
  hide($('filter-strategy'))
  $('status').textContent = ''
  $('mode-badge').textContent = ''
  $('current-table').textContent = ''
  ;['schema-btn', 'filter-btn', 'new-item-btn', 'export-json', 'export-csv', 'more-btn']
    .forEach((id) => { $(id).disabled = true })
}

function syncToolbar() {
  const ready = !!(state && state.loaded)
  ;['schema-btn', 'filter-btn', 'new-item-btn', 'export-json', 'export-csv']
    .forEach((id) => { $(id).disabled = !ready })
  $('more-btn').disabled = !(state && state.cursor)
}

let ctxTarget = null

function showContextMenu(x, y, name, region) {
  ctxTarget = { name, region }
  const menu = $('ctx-menu')
  menu.style.left = x + 'px'
  menu.style.top = y + 'px'
  show(menu)
}

function hideContextMenu() { hide($('ctx-menu')) }

function renderTabs() {
  const bar = $('tab-bar')
  bar.innerHTML = ''
  conn.tabs.forEach((t) => {
    const el = document.createElement('div')
    el.className = 'tab' + (t.id === conn.activeId ? ' active' : '')
    const label = document.createElement('span')
    label.className = 'tab-label'
    label.textContent = t.currentTable
    el.title = t.currentTable + ' · ' + t.region
    el.appendChild(label)
    const close = document.createElement('button')
    close.className = 'tab-close'
    close.textContent = '✕'
    close.title = 'Close tab'
    close.addEventListener('click', (e) => { e.stopPropagation(); closeTab(t.id) })
    el.appendChild(close)
    el.addEventListener('click', () => activate(t.id))
    bar.appendChild(el)
  })
}

function openTable(name, region, opts) {
  const forceNew = !!(opts && opts.forceNew)
  if (!forceNew) {
    const existing = conn.tabs.find((t) => t.currentTable === name && t.region === region)
    if (existing) { activate(existing.id); return }
  }
  const tab = newTab(name, region)
  conn.tabs.push(tab)
  activate(tab.id)
  loadTab(tab)
}

function activate(id) {
  conn.activeId = id
  state = conn.tabs.find((t) => t.id === id) || null
  hide($('detail'))
  hide($('editor'))
  hideContextMenu()
  if (!state) { showEmptyState(); renderTabs(); renderSidebar(); return }
  show($('grid-wrap'))
  hide($('content-empty'))
  renderTabs()
  renderSidebar()
  renderFilterRows()
  if (state.filterOpen) show($('filter-panel')); else hide($('filter-panel'))
  renderStrategyBar()
  updateAttrSuggestions()
  renderGrid()
  $('grid-wrap').scrollTop = state.scrollTop
  syncToolbar()
  if (state.loaded) {
    updateStatus()
  } else {
    $('mode-badge').textContent = ''
    $('status').textContent = state.status || 'Loading…'
  }
}

async function loadTab(tab) {
  tab.status = 'Loading…'
  if (tab.id === conn.activeId) $('status').textContent = tab.status
  try {
    const schema = await window.api.schema(tab.currentTable, tab.region)
    const info = schema.info || {}
    tab.keys = { partition: info.PartitionKey || '', sort: info.SortKey || '' }
    tab.indexes = buildIndexList(info)
    tab.schemaRaw = schema.rawJSON || JSON.stringify(info, null, 2)
    tab.loaded = true
    if (tab.id === conn.activeId) syncToolbar()
    await loadPage(tab, true)
  } catch (err) {
    tab.status = 'Error: ' + err.message
    if (tab.id === conn.activeId) $('status').textContent = tab.status
  }
}

function closeTab(id) {
  const i = conn.tabs.findIndex((t) => t.id === id)
  if (i === -1) return
  conn.tabs.splice(i, 1)
  if (conn.activeId === id) {
    const next = conn.tabs[i] || conn.tabs[i - 1]
    if (next) { activate(next.id) }
    else { conn.activeId = null; state = null; hide($('detail')); hide($('editor')); showEmptyState(); renderTabs(); renderSidebar() }
  } else {
    renderTabs()
    renderSidebar()
  }
}

const $ = (id) => document.getElementById(id)
const show = (el) => el.classList.remove('hidden')
const hide = (el) => el.classList.add('hidden')

function populateRegionPicker() {
  const sel = $('ct-region')
  sel.innerHTML = ''
  AWS_REGIONS.forEach((r) => {
    const opt = document.createElement('option')
    opt.value = r
    opt.textContent = r
    sel.appendChild(opt)
  })
}

function renderProfileSelect(selected) {
  const sel = $('profile-select')
  sel.innerHTML = ''
  conn.profiles.forEach((p) => {
    const opt = document.createElement('option')
    opt.value = p
    opt.textContent = p
    if (p === selected) opt.selected = true
    sel.appendChild(opt)
  })
}

function showSidebarMessage(text) {
  $('table-list').innerHTML = ''
  const m = $('sidebar-msg')
  m.textContent = text
  show(m)
}

async function init() {
  populateRegionPicker()
  try {
    const data = await window.api.listProfiles()
    conn.profiles = data.profiles || []
    if (conn.profiles.length === 0) {
      renderProfileSelect('')
      showSidebarMessage('No AWS profiles found at ~/.aws/credentials')
      return
    }
    const def = conn.profiles.includes(data.default) ? data.default : conn.profiles[0]
    renderProfileSelect(def)
    await discoverInto(def, true)
  } catch (err) {
    showSidebarMessage('Failed to load profiles: ' + err.message)
  }
}

async function discoverInto(profile, resetTabs) {
  conn.profile = profile
  if (resetTabs) {
    conn.tabs = []
    conn.activeId = null
    state = null
    hide($('detail')); hide($('editor')); hideContextMenu()
    showEmptyState(); renderTabs()
  }
  const wasExpanded = new Set(conn.regions.filter((r) => r.expanded).map((r) => r.region))
  showSidebarMessage('Discovering regions…')
  try {
    const data = await window.api.discover(profile)
    conn.regions = (data.regions || []).map((r) => ({
      region: r.region,
      tables: r.tables || [],
      expanded: resetTabs ? false : wasExpanded.has(r.region),
    }))
    if (conn.regions.length === 1) conn.regions[0].expanded = true
    if (conn.regions.length === 0) {
      showSidebarMessage('No tables found in any region — check this profile’s credentials, then Refresh (⟳)')
      return
    }
    renderSidebar()
  } catch (err) {
    showSidebarMessage('Discovery failed: ' + err.message)
  }
}

function renderSidebar() {
  hide($('sidebar-msg'))
  const filter = $('table-filter').value.toLowerCase()
  const ul = $('table-list')
  ul.innerHTML = ''
  const sep = ' '
  const activeKey = state ? state.region + sep + state.currentTable : null
  const openKeys = new Set(conn.tabs.map((t) => t.region + sep + t.currentTable))
  conn.regions.forEach((rg) => {
    const matches = rg.tables.filter((t) => t.toLowerCase().includes(filter))
    if (filter && matches.length === 0) return
    const expanded = rg.expanded || (filter !== '' && matches.length > 0)

    const head = document.createElement('li')
    head.className = 'region-head'
    head.textContent = (expanded ? '▾ ' : '▸ ') + rg.region + ' (' + rg.tables.length + ')'
    head.addEventListener('click', () => { rg.expanded = !expanded; renderSidebar() })
    ul.appendChild(head)
    if (!expanded) return

    matches.forEach((t) => {
      const li = document.createElement('li')
      li.className = 'table-row'
      const key = rg.region + sep + t
      if (key === activeKey) li.classList.add('active')
      if (openKeys.has(key)) li.classList.add('open')
      li.textContent = t
      li.addEventListener('click', () => openTable(t, rg.region))
      li.addEventListener('contextmenu', (e) => {
        e.preventDefault()
        showContextMenu(e.clientX, e.clientY, t, rg.region)
      })
      ul.appendChild(li)
    })
  })
}

function pageSize() {
  return parseInt($('page-size').value, 10) || 500
}

function activeConditions(tab) {
  return tab.conditions
    .filter((c) => c.name.trim() !== '' && (!VALUE_OPS.has(c.op) || c.value.trim() !== ''))
    .map((c) => ({ name: c.name, op: c.op, value: c.value }))
}

async function loadPage(tab, reset) {
  const cursor = reset ? '' : tab.cursor
  try {
    let data
    if (tab.filterActive) {
      data = await window.api.query(tab.currentTable, tab.region, {
        conditions: activeConditions(tab),
        limit: pageSize(),
        cursor,
        strategy: tab.override,
      })
      tab.mode = data.mode || ''
      tab.strategy = { mode: data.mode || '', index: data.index || '' }
    } else {
      data = await window.api.scan(tab.currentTable, tab.region, cursor, pageSize())
      tab.mode = ''
    }
    if (reset) {
      tab.items = []
      tab.scanned = 0
      tab.selectedIdx = -1
      tab.selectedItem = null
    }
    tab.items = tab.items.concat(data.items || [])
    tab.cursor = data.cursor || ''
    if (tab.filterActive) tab.scanned += data.scannedCount || 0
    if (tab.id === conn.activeId) {
      syncToolbar()
      updateStatus()
      renderGrid()
      renderStrategyBar()
      updateAttrSuggestions()
    }
  } catch (err) {
    tab.status = 'Error: ' + err.message
    if (tab.id === conn.activeId) { $('status').textContent = tab.status; syncToolbar() }
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
  state.status = s
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
    nameIn.setAttribute('list', 'attr-suggestions')
    nameIn.autocomplete = 'off'
    nameIn.addEventListener('input', () => { state.conditions[i].name = nameIn.value })
    nameIn.addEventListener('keydown', filterKeydown)

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
    valIn.autocomplete = 'off'
    valIn.addEventListener('input', () => { state.conditions[i].value = valIn.value })
    valIn.addEventListener('keydown', filterKeydown)

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
    state.filterOpen = true
  } else {
    hide(panel)
    state.filterOpen = false
  }
}

async function applyFilter() {
  state.filterActive = activeConditions(state).length > 0
  state.override = { mode: 'auto', index: '' }
  state.cursor = ''
  await loadPage(state, true)
}

async function clearFilter() {
  state.conditions = []
  state.filterActive = false
  state.override = { mode: 'auto', index: '' }
  renderFilterRows()
  hide($('filter-strategy'))
  state.cursor = ''
  await loadPage(state, true)
}

function filterKeydown(e) {
  if (e.key === 'Enter') {
    e.preventDefault()
    applyFilter()
  }
}

function buildIndexList(info) {
  const list = []
  if (info.PartitionKey) {
    list.push({ name: '', kind: 'table', pk: info.PartitionKey, sk: info.SortKey || '' })
  }
  ;(info.GSIs || []).forEach((g) => {
    list.push({ name: g.Name, kind: 'gsi', pk: g.PartitionKey || '', sk: g.SortKey || '' })
  })
  ;(info.LSIs || []).forEach((l) => {
    list.push({ name: l.Name, kind: 'lsi', pk: l.PartitionKey || '', sk: l.SortKey || '' })
  })
  return list
}

function updateAttrSuggestions() {
  const names = new Set()
  state.indexes.forEach((ix) => {
    if (ix.pk) names.add(ix.pk)
    if (ix.sk) names.add(ix.sk)
  })
  state.items.forEach((it) => Object.keys(it).forEach((k) => names.add(k)))
  const dl = $('attr-suggestions')
  dl.innerHTML = ''
  ;[...names].sort().forEach((n) => {
    const opt = document.createElement('option')
    opt.value = n
    dl.appendChild(opt)
  })
}

const DATE_NAME_RE = /(_at$|date|time|timestamp|created|updated|modified)/i
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}/

function isDateColumn(col) {
  if (DATE_NAME_RE.test(col)) return true
  let sawValue = false
  for (const it of state.items) {
    const v = it[col]
    if (v === null || v === undefined || v === '') continue
    sawValue = true
    if (typeof v !== 'string' || !ISO_DATE_RE.test(v)) return false
  }
  return sawValue
}

function firstDirFor(col) {
  return isDateColumn(col) ? 'desc' : 'asc'
}

function onHeaderClick(col) {
  if (state.sort.column === col) {
    state.sort.dir = state.sort.dir === 'asc' ? 'desc' : 'asc'
  } else {
    state.sort.column = col
    state.sort.dir = firstDirFor(col)
  }
  renderGrid()
}

function compareValues(a, b, dateCol) {
  // Callers (sortedItems) handle missing/null; a and b are present values here.
  if (typeof a === 'number' && typeof b === 'number') return a - b
  if (dateCol) {
    const at = Date.parse(a), bt = Date.parse(b)
    if (!isNaN(at) && !isNaN(bt)) return at - bt
  }
  return String(cellText(a)).localeCompare(String(cellText(b)))
}

function sortedItems() {
  const col = state.sort.column
  const dateCol = isDateColumn(col)
  const sign = state.sort.dir === 'asc' ? 1 : -1
  return state.items.slice().sort((x, y) => {
    const a = x[col], b = y[col]
    const am = a === null || a === undefined || a === ''
    const bm = b === null || b === undefined || b === ''
    if (am && bm) return 0
    if (am) return 1   // missing always sorts last, regardless of direction
    if (bm) return -1
    return compareValues(a, b, dateCol) * sign
  })
}

function strategyTarget() {
  if (state.strategy.mode === 'scan') return { kind: 'scan' }
  if (state.strategy.index) return { kind: 'gsi', name: state.strategy.index }
  return { kind: 'table' }
}

function viableIndexes() {
  const eqAttrs = new Set(
    activeConditions(state).filter((c) => c.op === 'eq').map((c) => c.name)
  )
  return state.indexes.filter((ix) => (ix.kind === 'table' || ix.kind === 'gsi') && ix.pk && eqAttrs.has(ix.pk))
}

function renderStrategyBar() {
  const bar = $('filter-strategy')
  if (!state.filterActive || !state.strategy.mode) {
    hide(bar)
    return
  }
  bar.innerHTML = ''
  const target = strategyTarget()
  const label = document.createElement('span')
  label.className = 'strategy-label'
  let text = 'Strategy: '
  if (target.kind === 'scan') text += 'SCAN'
  else if (target.kind === 'gsi') text += 'QUERY · index: ' + target.name
  else text += 'QUERY · table'
  if (state.override.mode === 'auto') text += ' (auto)'
  label.textContent = text
  bar.appendChild(label)

  const addBtn = (caption, override) => {
    const b = document.createElement('button')
    b.className = 'strategy-override'
    b.textContent = caption
    b.addEventListener('click', () => {
      state.override = override
      state.cursor = ''
      loadPage(state, true)
    })
    bar.appendChild(b)
  }

  viableIndexes().forEach((ix) => {
    if (ix.kind === 'gsi' && target.name !== ix.name) {
      addBtn('Use ' + ix.name + ' instead', { mode: 'query', index: ix.name })
    }
    if (ix.kind === 'table' && target.kind !== 'table') {
      addBtn('Use Table instead', { mode: 'query', index: '' })
    }
  })
  if (target.kind !== 'scan') {
    addBtn('Use Scan instead', { mode: 'scan', index: '' })
  }
  show(bar)
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

  const view = (state.sort.column && cols.includes(state.sort.column)) ? sortedItems() : state.items
  state.rendered = view

  const hr = document.createElement('tr')
  cols.forEach((c) => {
    const th = document.createElement('th')
    let label = c
    if (state.sort.column === c) label += state.sort.dir === 'asc' ? ' ▲' : ' ▼'
    th.textContent = label
    th.className = 'sortable'
    th.addEventListener('click', () => onHeaderClick(c))
    hr.appendChild(th)
  })
  thead.appendChild(hr)

  view.forEach((item, idx) => {
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

function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function renderDetailBody() {
  const body = $('detail-body')
  const term = $('detail-search').value
  if (!term) {
    body.textContent = state.detailText
    $('detail-matches').textContent = ''
    return
  }
  const escaped = escapeHtml(state.detailText)
  const escTerm = escapeHtml(term)
  const lower = escaped.toLowerCase()
  const tlower = escTerm.toLowerCase()
  let result = ''
  let i = 0
  let count = 0
  while (true) {
    const idx = lower.indexOf(tlower, i)
    if (idx === -1) {
      result += escaped.slice(i)
      break
    }
    result += escaped.slice(i, idx) + '<mark>' + escaped.slice(idx, idx + escTerm.length) + '</mark>'
    i = idx + escTerm.length
    count++
  }
  body.innerHTML = result
  $('detail-matches').textContent = count + (count === 1 ? ' match' : ' matches')
}

function openDetail(title, text, withEditDelete) {
  state.detailText = text
  $('detail-title').textContent = title
  $('detail-search').value = ''
  renderDetailBody()
  if (withEditDelete) {
    show($('detail-edit'))
    show($('detail-delete'))
  } else {
    hide($('detail-edit'))
    hide($('detail-delete'))
  }
  show($('detail'))
}

function showItem(idx) {
  const item = state.rendered[idx]
  state.selectedIdx = idx
  state.selectedItem = item
  openDetail('Item', JSON.stringify(item, null, 2), true)
}

function showSchema() {
  openDetail('Schema: ' + state.currentTable, state.schemaRaw || '', false)
}

function copyDetail() {
  const btn = $('detail-copy')
  const flash = (label) => {
    clearTimeout(btn._restoreTimer)
    btn.textContent = label
    btn._restoreTimer = setTimeout(() => { btn.textContent = 'Copy' }, 1200)
  }
  navigator.clipboard.writeText(state.detailText)
    .then(() => flash('Copied!'))
    .catch(() => flash('Copy failed'))
}

function csvEscape(s) {
  if (/[",\n\r]/.test(s)) {
    return '"' + s.replace(/"/g, '""') + '"'
  }
  return s
}

function buildCSV() {
  const cols = columnOrder()
  const lines = [cols.map(csvEscape).join(',')]
  state.items.forEach((item) => {
    lines.push(cols.map((c) => csvEscape(cellText(item[c]))).join(','))
  })
  return lines.join('\r\n')
}

async function exportJSON() {
  if (!state.currentTable) return
  try {
    await window.api.exportFile(state.currentTable + '.json', JSON.stringify(state.items, null, 2))
  } catch (err) {
    $('status').textContent = 'Export failed: ' + err.message
  }
}

async function exportCSV() {
  if (!state.currentTable) return
  try {
    await window.api.exportFile(state.currentTable + '.csv', buildCSV())
  } catch (err) {
    $('status').textContent = 'Export failed: ' + err.message
  }
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
    await window.api.saveItem(state.currentTable, state.region, text)
    hide($('editor'))
    await loadPage(state, true)
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
    await window.api.deleteItem(state.currentTable, state.region, json)
    hide($('detail'))
    await loadPage(state, true)
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
  const defRegion = (state && state.region) || (conn.regions[0] && conn.regions[0].region) || AWS_REGIONS[0]
  $('ct-region').value = defRegion
  $('ct-error').textContent = ''
  show($('createtable'))
}

async function submitCreateTable() {
  const region = $('ct-region').value
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
    await window.api.createTable(form, region)
    hide($('createtable'))
    await discoverInto(conn.profile, false)
  } catch (err) {
    $('ct-error').textContent = err.message
  } finally {
    $('ct-create').disabled = false
  }
}

window.addEventListener('DOMContentLoaded', () => {
  $('table-filter').addEventListener('input', renderSidebar)
  $('profile-select').addEventListener('change', (e) => discoverInto(e.target.value, true))
  $('refresh-btn').addEventListener('click', () => { if (conn.profile) discoverInto(conn.profile, false) })
  $('schema-btn').addEventListener('click', showSchema)
  $('filter-btn').addEventListener('click', toggleFilter)
  $('filter-add').addEventListener('click', addCondition)
  $('filter-apply').addEventListener('click', applyFilter)
  $('filter-clear').addEventListener('click', clearFilter)
  $('more-btn').addEventListener('click', () => loadPage(state, false))
  $('page-size').addEventListener('change', () => { if (state) loadPage(state, true) })
  $('grid-wrap').addEventListener('scroll', () => { if (state) state.scrollTop = $('grid-wrap').scrollTop })
  $('detail-close').addEventListener('click', () => hide($('detail')))
  $('detail-search').addEventListener('input', renderDetailBody)
  $('detail-copy').addEventListener('click', copyDetail)
  $('new-item-btn').addEventListener('click', openNewItem)
  $('create-table-btn').addEventListener('click', openCreateTable)
  $('export-json').addEventListener('click', exportJSON)
  $('export-csv').addEventListener('click', exportCSV)
  $('detail-edit').addEventListener('click', openEditItem)
  $('detail-delete').addEventListener('click', confirmDelete)
  $('editor-close').addEventListener('click', () => hide($('editor')))
  $('editor-save').addEventListener('click', saveEditor)
  $('ct-close').addEventListener('click', () => hide($('createtable')))
  $('ct-create').addEventListener('click', submitCreateTable)
  $('confirm-no').addEventListener('click', () => hide($('confirm')))
  $('confirm-yes').addEventListener('click', doDelete)
  $('ctx-open-new').addEventListener('click', () => {
    if (ctxTarget) openTable(ctxTarget.name, ctxTarget.region, { forceNew: true })
    hideContextMenu()
  })
  document.addEventListener('click', (e) => {
    if (!$('ctx-menu').contains(e.target)) hideContextMenu()
  })
  document.addEventListener('scroll', hideContextMenu, true)
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') hideContextMenu() })
  init()
})
