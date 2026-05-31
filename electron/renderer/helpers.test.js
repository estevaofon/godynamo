const test = require('node:test')
const assert = require('node:assert/strict')
const { findMatches, buildItemTemplate } = require('./helpers.js')

test('findMatches: empty term -> []', () => {
  assert.deepEqual(findMatches('{"a":1}', ''), [])
})

test('findMatches: single match returns one {from,to} range', () => {
  assert.deepEqual(findMatches('hello world', 'world'), [{ from: 6, to: 11 }])
})

test('findMatches: multiple matches, case-insensitive', () => {
  assert.deepEqual(findMatches('Aba aba ABA', 'aba'), [
    { from: 0, to: 3 }, { from: 4, to: 7 }, { from: 8, to: 11 },
  ])
})

test('findMatches: no match -> []', () => {
  assert.deepEqual(findMatches('abc', 'xyz'), [])
})

test('buildItemTemplate: partition only', () => {
  assert.deepEqual(buildItemTemplate({ partition: 'pk', sort: '' }), { pk: '' })
})

test('buildItemTemplate: partition + sort', () => {
  assert.deepEqual(buildItemTemplate({ partition: 'pk', sort: 'sk' }), { pk: '', sk: '' })
})

test('buildItemTemplate: no keys -> {}', () => {
  assert.deepEqual(buildItemTemplate({ partition: '', sort: '' }), {})
})
