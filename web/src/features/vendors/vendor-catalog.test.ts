import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { buildLobeVendorCatalog } from './vendor-catalog'

const AlibabaCloud = {}
const Anthropic = {}
const Claude = {}
const Moonshot = {}
const Qwen = {}
const Voyage = {}

const toc = [
  {
    docsUrl: 'alibaba-cloud',
    fullTitle: 'AlibabaCloud (阿里云)',
    id: 'AlibabaCloud',
    param: { hasColor: true },
    title: 'AlibabaCloud',
  },
  {
    docsUrl: 'qwen',
    fullTitle: 'Qwen (千问)',
    id: 'Qwen',
    param: { hasColor: true },
    title: 'Qwen',
  },
  {
    docsUrl: 'anthropic',
    fullTitle: 'Anthropic',
    id: 'Anthropic',
    param: { hasColor: false },
    title: 'Anthropic',
  },
  {
    docsUrl: 'claude',
    fullTitle: 'Claude',
    id: 'Claude',
    param: { hasColor: true },
    title: 'Claude',
  },
  {
    docsUrl: 'moonshot',
    fullTitle: 'Moonshot (月之暗面)',
    id: 'Moonshot',
    param: { hasColor: false },
    title: 'MoonshotAI',
  },
  {
    docsUrl: 'voyage',
    fullTitle: 'Voyage',
    id: 'Voyage',
    param: { hasColor: true },
    title: 'Voyage',
  },
]

describe('LobeHub vendor catalog', () => {
  test('derives canonical names, CJK aliases, and color icon variants', () => {
    const catalog = buildLobeVendorCatalog(
      toc,
      [{ Icon: Moonshot, keywords: ['moonshot', 'kimi-coding-plan'] }],
      { AlibabaCloud, Anthropic, Claude, Moonshot, Qwen, Voyage }
    )

    const moonshot = catalog.find((candidate) => candidate.name === 'Moonshot')
    assert.deepEqual(moonshot, {
      alias: '月之暗面',
      icon: 'Moonshot',
      match_keys: ['kimicodingplan', 'moonshot', 'moonshotai'],
      name: 'Moonshot',
    })

    const alibabaCloud = catalog.find(
      (candidate) => candidate.name === 'AlibabaCloud'
    )
    assert.equal(alibabaCloud?.icon, 'AlibabaCloud.Color')
    assert.equal(alibabaCloud?.alias, '阿里云')
  })

  test('gives provider and curated claims priority over toc self keys', () => {
    const catalog = buildLobeVendorCatalog(
      toc,
      [{ Icon: AlibabaCloud, keywords: ['qwen'] }],
      { AlibabaCloud, Anthropic, Claude, Moonshot, Qwen, Voyage }
    )

    assert.ok(
      catalog
        .find((candidate) => candidate.name === 'AlibabaCloud')
        ?.match_keys.includes('qwen')
    )
    assert.equal(
      catalog.find((candidate) => candidate.name === 'Qwen'),
      undefined
    )
    assert.ok(
      catalog
        .find((candidate) => candidate.name === 'Anthropic')
        ?.match_keys.includes('claude')
    )
    assert.equal(
      catalog.find((candidate) => candidate.name === 'Claude'),
      undefined
    )
  })

  test('groups repeated provider mappings by exact icon identity', () => {
    const catalog = buildLobeVendorCatalog(
      toc,
      [
        { Icon: AlibabaCloud, keywords: ['qwen'] },
        { Icon: AlibabaCloud, keywords: ['bailian'] },
      ],
      { AlibabaCloud, Anthropic, Claude, Moonshot, Qwen, Voyage }
    )

    assert.deepEqual(
      catalog.find((candidate) => candidate.name === 'AlibabaCloud')
        ?.match_keys,
      ['alibabacloud', 'bailian', 'qwen']
    )
  })

  test('keeps toc-only components available as exact namespace matches', () => {
    const catalog = buildLobeVendorCatalog(toc, [], {
      AlibabaCloud,
      Anthropic,
      Claude,
      Moonshot,
      Qwen,
      Voyage,
    })

    assert.deepEqual(
      catalog.find((candidate) => candidate.name === 'Voyage'),
      {
        icon: 'Voyage.Color',
        match_keys: ['voyage'],
        name: 'Voyage',
      }
    )
  })
})
