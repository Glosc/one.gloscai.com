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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CommonLogsFilterBar } from '../common-logs-filter-bar'
import { UsageLogsProvider, useLogsViewScope } from '../usage-logs-provider'

function FilterFixture() {
  const { canManageScope, viewScope, setViewScope } = useLogsViewScope()
  const table = useReactTable({
    data: [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <>
      {canManageScope && (
        <Tabs
          value={viewScope}
          onValueChange={(value) => {
            if (value === 'all' || value === 'self') setViewScope(value)
          }}
        >
          <TabsList>
            <TabsTrigger value='all'>All</TabsTrigger>
            <TabsTrigger value='self'>Only Mine</TabsTrigger>
          </TabsList>
        </Tabs>
      )}
      <CommonLogsFilterBar table={table} />
    </>
  )
}

async function renderFilter(role: number = ROLE.USER) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'tester', role })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    let data: unknown = { quota: 0, rpm: 0, tpm: 0 }
    if (url === '/api/user/self/groups') data = {}
    if (url === '/api/group/') data = []
    if (url === '/api/system-task/current') data = null
    return { data: { success: true, data } }
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: FilterFixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({ initialEntries: ['/usage-logs/common'] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <UsageLogsProvider>
        <RouterProvider router={router} />
      </UsageLogsProvider>
    </QueryClientProvider>
  )
  await screen.findByRole('combobox', { name: 'Type' })
  await waitFor(() => expect(client.isFetching()).toBe(0))
  return router
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  localStorage.clear()
})

it('marks only retired log types as deprecated while keeping historical filters selectable', async () => {
  const router = await renderFilter()
  await userEvent.click(screen.getByRole('combobox', { name: 'Type' }))
  for (const label of ['Manage', 'Login']) {
    expect(
      within(
        screen.getByRole('option', { name: new RegExp(`^${label}`) })
      ).getByText('Deprecated')
    ).toBeVisible()
  }
  for (const label of [
    'All Types',
    'Top-up',
    'Consume',
    'System',
    'Error',
    'Refund',
  ]) {
    expect(
      within(screen.getByRole('option', { name: label })).queryByText(
        'Deprecated'
      )
    ).not.toBeInTheDocument()
  }
  await userEvent.click(screen.getByRole('option', { name: /^Manage/ }))
  expect(screen.getByRole('combobox', { name: 'Type' })).toHaveTextContent(
    'Deprecated'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({ type: ['3'], page: 1 })
  )
  await userEvent.click(screen.getByRole('combobox', { name: 'Type' }))
  await userEvent.click(screen.getByRole('option', { name: /^Login/ }))
  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({ type: ['7'], page: 1 })
  )
})

it('shows history cleanup in the root all-logs view and opens its dialog', async () => {
  await renderFilter(ROLE.SUPER_ADMIN)

  await userEvent.click(
    screen.getByRole('button', { name: 'Clean history logs' })
  )

  expect(
    await screen.findByRole('dialog', { name: 'Clean history logs' })
  ).toBeVisible()
  expect(api.get).toHaveBeenCalledWith('/api/system-task/current', {
    params: { type: 'log_cleanup' },
  })
})

it.each([ROLE.ADMIN, ROLE.USER])(
  'hides history cleanup and avoids cleanup task requests for role %s',
  async (role) => {
    await renderFilter(role)

    expect(
      screen.queryByRole('button', { name: 'Clean history logs' })
    ).not.toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalledWith(
      '/api/system-task/current',
      expect.anything()
    )
  }
)

it('removes history cleanup and stops task requests when root switches to Only Mine', async () => {
  await renderFilter(ROLE.SUPER_ADMIN)
  expect(
    screen.getByRole('button', { name: 'Clean history logs' })
  ).toBeVisible()
  vi.mocked(api.get).mockClear()

  await userEvent.click(screen.getByRole('tab', { name: 'Only Mine' }))

  expect(screen.getByRole('tab', { name: 'Only Mine' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  expect(
    screen.queryByRole('button', { name: 'Clean history logs' })
  ).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalledWith(
    '/api/system-task/current',
    expect.anything()
  )
})
