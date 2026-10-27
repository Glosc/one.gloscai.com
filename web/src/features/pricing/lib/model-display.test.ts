import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { getPricingModelDisplayName } from './model-display'

describe('pricing model display names', () => {
  test('uses a vendor alias for a provider-prefixed model name', () => {
    assert.equal(
      getPricingModelDisplayName('moonshotai/kimi-k2', 'Moonshot', '月之暗面'),
      '月之暗面/kimi-k2'
    )
  })

  test('falls back to the canonical vendor name', () => {
    assert.equal(
      getPricingModelDisplayName('openai/gpt-5-pro', 'OpenAI'),
      'OpenAI/gpt-5-pro'
    )
  })

  test('preserves nested model path segments', () => {
    assert.equal(
      getPricingModelDisplayName('@cf/meta/llama-3', 'Cloudflare'),
      'Cloudflare/meta/llama-3'
    )
  })

  test('keeps raw names when a display prefix cannot be derived', () => {
    assert.equal(getPricingModelDisplayName('gpt-5-pro', 'OpenAI'), 'gpt-5-pro')
    assert.equal(
      getPricingModelDisplayName('/gpt-5-pro', 'OpenAI'),
      '/gpt-5-pro'
    )
    assert.equal(getPricingModelDisplayName('openai/', 'OpenAI'), 'openai/')
    assert.equal(
      getPricingModelDisplayName('openai/gpt-5-pro'),
      'openai/gpt-5-pro'
    )
  })
})
