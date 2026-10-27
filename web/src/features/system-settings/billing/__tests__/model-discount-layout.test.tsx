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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { ModelDiscountSection } from '../model-discount-section'

let client: QueryClient

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})

afterEach(() => {
  client.clear()
})

function renderDiscounts() {
  const discounts = Object.fromEntries(
    Array.from({ length: 12 }, (_, index) => [
      `model-${index + 1}`,
      { discount: 0.8 },
    ])
  )
  render(
    <QueryClientProvider client={client}>
      <ModelDiscountSection defaultValue={JSON.stringify(discounts)} />
    </QueryClientProvider>
  )
}

describe('model discount page scrolling', () => {
  it('keeps a long visual editor at its content height so the settings page can scroll to Save', () => {
    renderDiscounts()

    expect(screen.getAllByPlaceholderText('gpt-4o*')).toHaveLength(12)
    const card = screen
      .getByRole('button', { name: 'Save model discounts' })
      .closest('[data-slot=card]')

    // The page owns scrolling; shrinking this overflow-hidden card clips the
    // last rows and Save rather than making them reachable through that page.
    expect(card).toHaveClass('shrink-0')
  })

  it('keeps the JSON editor and Save in the same non-shrinking page content', async () => {
    renderDiscounts()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))

    const editor = screen.getByRole('textbox')
    const card = editor.closest('[data-slot=card]')
    expect(card).toHaveClass('shrink-0')
    expect(card).toContainElement(
      screen.getByRole('button', { name: 'Save model discounts' })
    )
  })
})
