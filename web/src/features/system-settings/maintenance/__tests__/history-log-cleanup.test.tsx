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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'

import { HistoryLogCleanup } from '../history-log-cleanup'

const activeTask = {
  task_id: 'systask_cleanup',
  type: 'log_cleanup',
  status: 'running',
  state: { total: 40, processed: 10, progress: 25, remaining: 30 },
  created_at: 1,
  updated_at: 1,
}

function renderCleanup() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <HistoryLogCleanup trigger={<Button>Clean history logs</Button>} />
    </QueryClientProvider>
  )
  return { ...view, queryClient }
}

function mockNoCurrentTask() {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/system-task/current' ? null : activeTask,
    },
  }))
}

beforeEach(() => {
  vi.setSystemTime(new Date(2026, 10, 10, 12, 0, 0))
  mockNoCurrentTask()
})

afterEach(() => {
  vi.useRealTimers()
})

test('cancelling the cleanup confirmation does not start a destructive request', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  await user.click(within(dialog).getByRole('button', { name: 'Clean logs' }))
  await user.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', {
      name: 'Cancel',
    })
  )

  expect(post).not.toHaveBeenCalled()
})

test('the 7-day preset sends the exact cutoff timestamp after confirmation', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: activeTask },
  })
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  expect(
    within(dialog).getByRole('button', { name: '30 days ago' })
  ).toBeVisible()
  await user.click(within(dialog).getByRole('button', { name: '7 days ago' }))
  await user.click(within(dialog).getByRole('button', { name: 'Clean logs' }))
  await user.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', {
      name: 'Delete logs',
    })
  )

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/system-task/log-cleanup', null, {
      params: { target_timestamp: new Date(2026, 10, 3, 12).getTime() / 1000 },
    })
  )
})

test('the default 30-day cutoff is sent unchanged after confirmation', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: activeTask },
  })
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  await user.click(within(dialog).getByRole('button', { name: 'Clean logs' }))
  await user.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', {
      name: 'Delete logs',
    })
  )

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/system-task/log-cleanup', null, {
      params: { target_timestamp: new Date(2026, 9, 11, 12).getTime() / 1000 },
    })
  )
})

test('clearing the selected date prevents opening confirmation or posting', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  await user.click(within(dialog).getByRole('button', { name: 'Clear' }))
  await user.click(within(dialog).getByRole('button', { name: 'Clean logs' }))

  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})

test('selecting a future date prevents opening confirmation or posting', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  await user.click(within(dialog).getByRole('button', { name: '2026-10-11' }))
  await user.click(screen.getByRole('button', { name: /next month/i }))
  await user.click(screen.getByRole('button', { name: /november 11th, 2026/i }))
  await user.click(within(dialog).getByRole('button', { name: 'Clean logs' }))

  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})

test('a restored cleanup task disables a second cleanup request', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: activeTask },
  })
  const post = vi.spyOn(api, 'post')
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  expect(
    await within(dialog).findByRole('button', { name: 'Cleaning...' })
  ).toBeDisabled()
  expect(post).not.toHaveBeenCalled()
})

test('pending status loading disables cleanup until the status request settles', async () => {
  const user = userEvent.setup()
  let resolveStatus: (value: {
    data: { success: boolean; data: null }
  }) => void = () => undefined
  vi.spyOn(api, 'get').mockReturnValue(
    new Promise((resolve) => {
      resolveStatus = resolve
    })
  )
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  const cleanButton = within(dialog).getByRole('button', { name: 'Clean logs' })
  expect(cleanButton).toBeDisabled()

  resolveStatus({ data: { success: true, data: null } })
  await waitFor(() => expect(cleanButton).toBeEnabled())
})

test('a failed status request shows an error and Retry restores cleanup controls', async () => {
  const user = userEvent.setup()
  const get = vi
    .spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('Status unavailable'))
    .mockResolvedValueOnce({ data: { success: true, data: null } })
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  expect(
    await within(dialog).findByText('Failed to load log cleanup status.')
  ).toBeVisible()
  const retry = within(dialog).getByRole('button', { name: 'Retry' })
  await user.click(retry)

  await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
  await waitFor(() =>
    expect(
      within(dialog).getByRole('button', { name: 'Clean logs' })
    ).toBeEnabled()
  )
  expect(
    within(dialog).queryByText('Failed to load log cleanup status.')
  ).not.toBeInTheDocument()
})

test('a failed cleanup request leaves confirmation open so it can be retried', async () => {
  const user = userEvent.setup()
  const post = vi
    .spyOn(api, 'post')
    .mockRejectedValueOnce(new Error('Temporary failure'))
    .mockResolvedValueOnce({ data: { success: true, data: activeTask } })
  renderCleanup()

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  await user.click(within(dialog).getByRole('button', { name: 'Clean logs' }))
  const confirmation = await screen.findByRole('alertdialog')
  const confirmButton = within(confirmation).getByRole('button', {
    name: 'Delete logs',
  })

  await user.click(confirmButton)
  expect(await screen.findByRole('alertdialog')).toBeVisible()
  await user.click(
    within(screen.getByRole('alertdialog')).getByRole('button', {
      name: 'Delete logs',
    })
  )

  await waitFor(() => expect(post).toHaveBeenCalledTimes(2))
})

test('cleanup completion refreshes logs even after the dialog is closed', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/system-task/current'
          ? activeTask
          : {
              ...activeTask,
              status: 'succeeded',
              result: { deleted_count: 30 },
            },
    },
  }))
  const { queryClient } = renderCleanup()
  queryClient.setQueryData(['logs'], { items: [] })
  queryClient.setQueryData(['model-call-logs'], { items: [] })
  queryClient.setQueryData(['usage-logs-stats'], { total: 0 })

  await user.click(screen.getByRole('button', { name: 'Clean history logs' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Clean history logs',
  })
  expect(
    await within(dialog).findByText('10 of 40 log entries processed.')
  ).toBeVisible()
  await user.click(within(dialog).getByRole('button', { name: 'Close' }))

  await waitFor(() =>
    expect(queryClient.getQueryState(['logs'])?.isInvalidated).toBe(true)
  )
  expect(queryClient.getQueryState(['usage-logs-stats'])?.isInvalidated).toBe(
    true
  )
  expect(queryClient.getQueryState(['model-call-logs'])?.isInvalidated).toBe(
    true
  )
})
