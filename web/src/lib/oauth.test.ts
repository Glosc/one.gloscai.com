/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { buildWeChatOAuthUrl } from './oauth'

describe('WeChat OAuth URL', () => {
  test('uses the configured callback URI without changing OAuth parameters', () => {
    const callbackURI = 'https://login.example.com/oauth/wechat?source=web'
    const authorizationURL = new URL(
      buildWeChatOAuthUrl('wx-app-id', 'oauth-state', callbackURI)
    )

    assert.equal(
      authorizationURL.origin + authorizationURL.pathname,
      'https://open.weixin.qq.com/connect/qrconnect'
    )
    assert.equal(authorizationURL.searchParams.get('appid'), 'wx-app-id')
    assert.equal(authorizationURL.searchParams.get('redirect_uri'), callbackURI)
    assert.equal(authorizationURL.searchParams.get('response_type'), 'code')
    assert.equal(authorizationURL.searchParams.get('scope'), 'snsapi_login')
    assert.equal(authorizationURL.searchParams.get('state'), 'oauth-state')
    assert.equal(authorizationURL.hash, '#wechat_redirect')
  })

  test('falls back to the current origin for existing installations', () => {
    const windowDescriptor = Object.getOwnPropertyDescriptor(
      globalThis,
      'window'
    )
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: { location: { origin: 'https://console.example.com' } },
    })

    try {
      const authorizationURL = new URL(
        buildWeChatOAuthUrl('wx-app-id', 'oauth-state', '   ')
      )
      assert.equal(
        authorizationURL.searchParams.get('redirect_uri'),
        'https://console.example.com/oauth/wechat'
      )
    } finally {
      if (windowDescriptor) {
        Object.defineProperty(globalThis, 'window', windowDescriptor)
      } else {
        Reflect.deleteProperty(globalThis, 'window')
      }
    }
  })
})
