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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useId, useMemo, useState, type ReactElement } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import {
  getCurrentLogCleanupTask,
  getSystemTask,
  startLogCleanupTask,
} from '../api'
import type { LogCleanupTask } from '../types'

const HOURS_IN_DAY = 24

const getDateHoursAgo = (hours: number) => {
  const date = new Date()
  date.setHours(date.getHours() - hours)
  return date
}

const getDateDaysAgo = (days: number) => getDateHoursAgo(days * HOURS_IN_DAY)

const quickSelectOptions = [
  {
    label: '24 hours ago',
    getValue: () => getDateHoursAgo(24),
  },
  {
    label: '7 days ago',
    getValue: () => getDateDaysAgo(7),
  },
  {
    label: '30 days ago',
    getValue: () => getDateDaysAgo(30),
  },
]

function isActiveLogCleanupTask(task: LogCleanupTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

/**
 * The history log module of the log maintenance page. It deletes the logs
 * created before a chosen time as a background task and shows its progress.
 */
export function HistoryLogCleanup(props: { trigger?: ReactElement }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const titleId = useId()
  const [open, setOpen] = useState(false)
  const [isLoadingStatus, setIsLoadingStatus] = useState(true)
  const [statusLoadFailed, setStatusLoadFailed] = useState(false)
  const [statusRetry, setStatusRetry] = useState(0)
  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [isStartingLogCleanup, setIsStartingLogCleanup] = useState(false)
  const [logCleanupTask, setLogCleanupTask] = useState<LogCleanupTask | null>(
    null
  )
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)

  useEffect(() => {
    let cancelled = false

    async function fetchCurrentLogCleanupTask() {
      setIsLoadingStatus(true)
      setStatusLoadFailed(false)
      try {
        const res = await getCurrentLogCleanupTask()
        if (!res.success) {
          throw createServerError(res, t('Failed to load log cleanup status.'))
        }
        if (!cancelled) setLogCleanupTask(res.data ?? null)
      } catch {
        if (!cancelled) setStatusLoadFailed(true)
      } finally {
        if (!cancelled) setIsLoadingStatus(false)
      }
    }

    fetchCurrentLogCleanupTask()

    return () => {
      cancelled = true
    }
  }, [statusRetry, t])

  const purgeTimestamp = useMemo(() => {
    if (!purgeDate) return null
    return Math.floor(purgeDate.getTime() / 1000)
  }, [purgeDate])

  const formattedPurgeDate = useMemo(() => {
    if (!purgeDate) return ''
    return formatTimestampToDate(purgeDate.getTime(), 'milliseconds')
  }, [purgeDate])

  const logCleanupActive = isActiveLogCleanupTask(logCleanupTask)
  const logCleanupState = logCleanupTask?.state
  const logCleanupProgress = Math.min(
    100,
    Math.max(0, logCleanupState?.progress ?? 0)
  )
  const logCleanupProcessed = logCleanupState?.processed ?? 0
  const logCleanupTotal = logCleanupState?.total ?? 0
  const logCleanupTaskId = logCleanupTask?.task_id

  useEffect(() => {
    if (!logCleanupTaskId || !logCleanupActive) return

    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const res = await getSystemTask(logCleanupTaskId)
        if (cancelled || !res.success || !res.data) return

        setLogCleanupTask(res.data)
        if (!isActiveLogCleanupTask(res.data)) {
          if (res.data.status === 'succeeded') {
            void queryClient.invalidateQueries({ queryKey: ['logs'] })
            void queryClient.invalidateQueries({
              queryKey: ['model-call-logs'],
            })
            void queryClient.invalidateQueries({
              queryKey: ['usage-logs-stats'],
            })
            const count =
              res.data.result?.deleted_count ?? res.data.state?.processed ?? 0
            toast.success(
              count > 0
                ? t('{{count}} log entries removed.', { count })
                : t('No log entries matched the selected time.')
            )
          } else if (res.data.status === 'failed') {
            handleServerError(res.data, t('Failed to clean logs'))
          }
        }
      } catch {
        /* keep polling */
      }
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [logCleanupActive, logCleanupTaskId, queryClient, t])

  const handleRequestCleanLogs = () => {
    if (
      purgeTimestamp === null ||
      !Number.isFinite(purgeTimestamp) ||
      purgeTimestamp <= 0 ||
      purgeTimestamp > Math.floor(Date.now() / 1000)
    ) {
      toast.error(t('Select a valid timestamp in the past.'))
      return
    }

    setShowConfirmDialog(true)
  }

  const handleCleanLogs = async () => {
    if (
      purgeTimestamp === null ||
      !Number.isFinite(purgeTimestamp) ||
      purgeTimestamp <= 0 ||
      purgeTimestamp > Math.floor(Date.now() / 1000)
    ) {
      toast.error(t('Select a valid timestamp in the past.'))
      return
    }
    if (
      isLoadingStatus ||
      statusLoadFailed ||
      isStartingLogCleanup ||
      logCleanupActive
    ) {
      return
    }

    setIsStartingLogCleanup(true)
    try {
      const res = await startLogCleanupTask(purgeTimestamp)
      if (!res.success) {
        throw createServerError(res, t('Failed to clean logs'))
      }
      if (!res.data) {
        throw new Error(t('Failed to clean logs'))
      }
      setLogCleanupTask(res.data)
      setShowConfirmDialog(false)
      toast.success(t('Log cleanup task started.'))
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to clean logs')
      handleServerError(error, message)
    } finally {
      setIsStartingLogCleanup(false)
    }
  }

  const content = (
    <div
      role='group'
      aria-labelledby={titleId}
      className='flex min-w-0 flex-col gap-4'
    >
      <div>
        <h4 id={titleId} className={props.trigger ? 'sr-only' : 'font-medium'}>
          {t('Clean history logs')}
        </h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t(
            "Deletes all users' general logs and saved model inputs and outputs before the selected time. Current filters do not apply."
          )}
        </p>
      </div>
      <fieldset
        disabled={
          isLoadingStatus ||
          statusLoadFailed ||
          isStartingLogCleanup ||
          logCleanupActive
        }
        className='flex min-w-0 flex-col gap-4'
      >
        <DateTimePicker
          value={purgeDate}
          onChange={setPurgeDate}
          className='flex-wrap'
        />
        <div className='flex flex-wrap gap-3'>
          {quickSelectOptions.map((option) => (
            <Button
              key={option.label}
              type='button'
              variant='outline'
              onClick={() => setPurgeDate(option.getValue())}
            >
              {t(option.label)}
            </Button>
          ))}
          <Button
            type='button'
            variant='destructive'
            onClick={handleRequestCleanLogs}
            aria-busy={isStartingLogCleanup || logCleanupActive}
          >
            {isStartingLogCleanup || logCleanupActive
              ? t('Cleaning...')
              : t('Clean logs')}
          </Button>
        </div>
      </fieldset>
      {statusLoadFailed && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Failed to load log cleanup status.')}
            <Button
              type='button'
              variant='outline'
              onClick={() => setStatusRetry((value) => value + 1)}
            >
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      <Alert>
        <AlertDescription>
          <p>{t('Audit logs, task records, and server log files are kept.')}</p>
          <p>
            {t(
              'Deleting logs makes database space reusable, but may not immediately reduce disk usage.'
            )}
          </p>
        </AlertDescription>
      </Alert>
      {logCleanupTask && (
        <div className='rounded-md border p-3'>
          <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
            <span className='font-medium'>{t('Log cleanup progress')}</span>
            <span className='text-muted-foreground tabular-nums'>
              {logCleanupProgress}%
            </span>
          </div>
          <Progress
            value={logCleanupProgress}
            aria-label={t('Log cleanup progress')}
          />
          <div className='text-muted-foreground mt-2 text-xs'>
            {t('{{processed}} of {{total}} log entries processed.', {
              processed: logCleanupProcessed,
              total: logCleanupTotal,
            })}
          </div>
          {logCleanupTask.status === 'failed' && logCleanupTask.error && (
            <div className='text-destructive mt-2 text-xs'>
              {logCleanupTask.error}
            </div>
          )}
        </div>
      )}

      <ConfirmDialog
        open={showConfirmDialog}
        onOpenChange={setShowConfirmDialog}
        title={t('Confirm log cleanup')}
        desc={
          <>
            {t(
              "This will permanently remove all users' general logs created before {{date}}, including saved model inputs and outputs.",
              { date: formattedPurgeDate }
            )}{' '}
            {t('This action cannot be undone.')}
          </>
        }
        destructive
        isLoading={isStartingLogCleanup}
        confirmText={isStartingLogCleanup ? t('Cleaning...') : t('Delete logs')}
        handleConfirm={handleCleanLogs}
      />
    </div>
  )

  if (!props.trigger) return content

  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      trigger={props.trigger}
      title={t('Clean history logs')}
      description={t(
        'Cleanup continues in the background when this dialog is closed.'
      )}
      contentClassName='sm:max-w-xl'
    >
      {content}
    </Dialog>
  )
}
