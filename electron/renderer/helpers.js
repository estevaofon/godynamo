// Pure, DOM-free helpers shared by the renderer (browser globals) and node:test.
function findMatches(text, term) {
  if (!term) return []
  const lower = String(text).toLowerCase()
  const tl = String(term).toLowerCase()
  const out = []
  let i = 0
  for (;;) {
    const idx = lower.indexOf(tl, i)
    if (idx === -1) break
    out.push({ from: idx, to: idx + tl.length })
    i = idx + tl.length
  }
  return out
}

function buildItemTemplate(keys) {
  const tmpl = {}
  if (keys && keys.partition) tmpl[keys.partition] = ''
  if (keys && keys.sort) tmpl[keys.sort] = ''
  return tmpl
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { findMatches, buildItemTemplate } // Node (tests)
}
// In the browser these are window globals, consumed by app.js.
