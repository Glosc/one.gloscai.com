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
import { useQuery } from '@tanstack/react-query'
import { Code2, Copy, Eye, Plus, Search, Trash2, Wand2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TitledCard } from '@/components/ui/titled-card'

import { getDiscountChannel, getUpstreamChannels } from '../api'
import { useUpdateOption } from '../hooks/use-update-option'
import type { UpstreamChannel } from '../types'

const OPTION_KEY = 'billing_setting.model_discounts'

type ModelDiscountConfig = {
  discount: number
  end_time?: number
}

type ModelDiscountRow = {
  id: number
  model: string
  discount: number
  endTime: string
}

type ModelDiscountSectionProps = {
  defaultValue: string
}

type AddModelsOptions = {
  models: string[]
  discount: number
  endTime: string
  overwrite: boolean
}

type AddModelsResult = {
  added: number
  updated: number
}

function endTimeToInputValue(endTime?: number) {
  if (!endTime) return ''
  const date = new Date(endTime * 1000)
  if (Number.isNaN(date.getTime())) return ''
  const offsetMs = date.getTimezoneOffset() * 60 * 1000
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16)
}

function inputValueToEndTime(value: string) {
  if (!value) return 0
  const timestamp = Math.floor(new Date(value).getTime() / 1000)
  return Number.isFinite(timestamp) ? timestamp : 0
}

function normalizeDiscount(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 1
  return value
}

function getNextRowId(rows: ModelDiscountRow[]) {
  return rows.reduce((max, row) => Math.max(max, row.id), 0) + 1
}

function parseModelsList(models: string | undefined) {
  if (!models) return []
  return models
    .split(',')
    .map((model) => model.trim())
    .filter(Boolean)
}

function isWildcardModel(model: string) {
  return model.includes('*') || model.includes('?')
}

function rowsToObject(
  rows: ModelDiscountRow[]
): Record<string, ModelDiscountConfig> {
  const discounts: Record<string, ModelDiscountConfig> = {}
  for (const row of rows) {
    const model = row.model.trim()
    if (!model) continue
    const endTime = inputValueToEndTime(row.endTime)
    discounts[model] = {
      discount: normalizeDiscount(Number(row.discount)),
      ...(endTime > 0 ? { end_time: endTime } : {}),
    }
  }
  return discounts
}

function objectToRows(
  discounts: Record<string, ModelDiscountConfig>
): ModelDiscountRow[] {
  return Object.entries(discounts).map(([model, config], index) => ({
    id: index + 1,
    model,
    discount: normalizeDiscount(Number(config?.discount)),
    endTime: endTimeToInputValue(config?.end_time),
  }))
}

function parseInitialDiscounts(
  rawValue: string | undefined
): Record<string, ModelDiscountConfig> {
  if (!rawValue) return {}
  try {
    const parsed = JSON.parse(rawValue) as unknown
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, ModelDiscountConfig>
    }
  } catch {
    // fall through to empty config
  }
  return {}
}

export function ModelDiscountSection({
  defaultValue,
}: ModelDiscountSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [rows, setRows] = useState<ModelDiscountRow[]>(() =>
    objectToRows(parseInitialDiscounts(defaultValue))
  )
  const [jsonText, setJsonText] = useState(() =>
    JSON.stringify(parseInitialDiscounts(defaultValue), null, 2)
  )
  const [jsonError, setJsonError] = useState('')
  const [nextRowId, setNextRowId] = useState(() => getNextRowId(rows))
  const [search, setSearch] = useState('')
  const [selectedRowIds, setSelectedRowIds] = useState<Set<number>>(
    () => new Set()
  )
  const [bulkDiscount, setBulkDiscount] = useState(0.8)
  const [bulkEndTime, setBulkEndTime] = useState('')
  const [channelDialogOpen, setChannelDialogOpen] = useState(false)

  const currentDiscounts = useMemo(() => rowsToObject(rows), [rows])
  const filteredRows = useMemo(() => {
    const query = search.trim().toLowerCase()
    if (!query) return rows
    return rows.filter((row) => row.model.toLowerCase().includes(query))
  }, [rows, search])
  const selectedRows = useMemo(
    () => rows.filter((row) => selectedRowIds.has(row.id)),
    [rows, selectedRowIds]
  )
  const visibleRowIds = useMemo(
    () => filteredRows.map((row) => row.id),
    [filteredRows]
  )
  const allVisibleSelected =
    visibleRowIds.length > 0 &&
    visibleRowIds.every((id) => selectedRowIds.has(id))
  const someVisibleSelected =
    !allVisibleSelected && visibleRowIds.some((id) => selectedRowIds.has(id))

  const syncFromRows = useCallback((nextRows: ModelDiscountRow[]) => {
    setRows(nextRows)
    setJsonText(JSON.stringify(rowsToObject(nextRows), null, 2))
    setJsonError('')
  }, [])

  useEffect(() => {
    setSelectedRowIds((prev) => {
      const validIds = new Set(rows.map((row) => row.id))
      const next = new Set<number>()
      for (const id of prev) {
        if (validIds.has(id)) next.add(id)
      }
      return next.size === prev.size ? prev : next
    })
  }, [rows])

  const handleJsonChange = useCallback(
    (text: string) => {
      setJsonText(text)
      try {
        const parsed = JSON.parse(text) as unknown
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          setJsonError(t('JSON must be an object'))
          return
        }
        const nextRows = objectToRows(
          parsed as Record<string, ModelDiscountConfig>
        )
        setRows(nextRows)
        setNextRowId(getNextRowId(nextRows))
        setJsonError('')
      } catch (error) {
        setJsonError(error instanceof Error ? error.message : t('Invalid JSON'))
      }
    },
    [t]
  )

  const updateRow = useCallback(
    (
      id: number,
      field: 'model' | 'discount' | 'endTime',
      value: string | number
    ) => {
      syncFromRows(
        rows.map((row) => (row.id === id ? { ...row, [field]: value } : row))
      )
    },
    [rows, syncFromRows]
  )

  const addRow = useCallback(() => {
    const newRow: ModelDiscountRow = {
      id: nextRowId,
      model: '',
      discount: 0.8,
      endTime: '',
    }
    setNextRowId((prev) => prev + 1)
    syncFromRows([...rows, newRow])
  }, [nextRowId, rows, syncFromRows])

  const removeRow = useCallback(
    (id: number) => {
      syncFromRows(rows.filter((row) => row.id !== id))
    },
    [rows, syncFromRows]
  )

  const toggleRowSelection = useCallback((id: number, checked: boolean) => {
    setSelectedRowIds((prev) => {
      const next = new Set(prev)
      if (checked) {
        next.add(id)
      } else {
        next.delete(id)
      }
      return next
    })
  }, [])

  const toggleVisibleSelection = useCallback(
    (checked: boolean) => {
      setSelectedRowIds((prev) => {
        const next = new Set(prev)
        for (const id of visibleRowIds) {
          if (checked) {
            next.add(id)
          } else {
            next.delete(id)
          }
        }
        return next
      })
    },
    [visibleRowIds]
  )

  const updateSelectedRows = useCallback(
    (updater: (row: ModelDiscountRow) => ModelDiscountRow) => {
      if (selectedRowIds.size === 0) return
      syncFromRows(
        rows.map((row) => (selectedRowIds.has(row.id) ? updater(row) : row))
      )
    },
    [rows, selectedRowIds, syncFromRows]
  )

  const applyBulkDiscount = useCallback(() => {
    const discount = normalizeDiscount(Number(bulkDiscount))
    updateSelectedRows((row) => ({ ...row, discount }))
  }, [bulkDiscount, updateSelectedRows])

  const applyBulkEndTime = useCallback(() => {
    updateSelectedRows((row) => ({ ...row, endTime: bulkEndTime }))
  }, [bulkEndTime, updateSelectedRows])

  const clearBulkEndTime = useCallback(() => {
    setBulkEndTime('')
    updateSelectedRows((row) => ({ ...row, endTime: '' }))
  }, [updateSelectedRows])

  const deleteSelectedRows = useCallback(() => {
    if (selectedRowIds.size === 0) return
    syncFromRows(rows.filter((row) => !selectedRowIds.has(row.id)))
    setSelectedRowIds(new Set())
  }, [rows, selectedRowIds, syncFromRows])

  const addModelsFromChannels = useCallback(
    ({ models, discount, endTime, overwrite }: AddModelsOptions) => {
      const uniqueModels = [
        ...new Set(models.map((model) => model.trim()).filter(Boolean)),
      ].sort((a, b) => a.localeCompare(b))
      const existingIndex = new Map<string, number>()
      rows.forEach((row, index) => {
        const model = row.model.trim()
        if (model) existingIndex.set(model, index)
      })

      let added = 0
      let updated = 0
      let nextId = Math.max(nextRowId, getNextRowId(rows))
      const nextRows = [...rows]

      for (const model of uniqueModels) {
        const existing = existingIndex.get(model)
        if (existing !== undefined) {
          if (overwrite) {
            nextRows[existing] = {
              ...nextRows[existing],
              discount,
              endTime,
            }
            updated++
          }
          continue
        }

        nextRows.push({
          id: nextId,
          model,
          discount,
          endTime,
        })
        nextId++
        added++
      }

      setNextRowId(nextId)
      syncFromRows(nextRows)
      return { added, updated }
    },
    [nextRowId, rows, syncFromRows]
  )

  const handleCopyJson = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(jsonText)
      toast.success(t('Copied to clipboard'))
    } catch {
      toast.error(t('Failed to copy'))
    }
  }, [jsonText, t])

  const handleSave = useCallback(async () => {
    if (editMode === 'json' && jsonError) {
      toast.error(t('Please fix JSON errors before saving'))
      return
    }
    await updateOption.mutateAsync({
      key: OPTION_KEY,
      value: JSON.stringify(currentDiscounts),
    })
  }, [currentDiscounts, editMode, jsonError, t, updateOption])

  const toggleEditMode = useCallback(() => {
    setEditMode((prev) => (prev === 'visual' ? 'json' : 'visual'))
  }, [])

  return (
    <TitledCard
      title={t('Model Discounts')}
      description={t(
        'Apply temporary or permanent discounts after model and group pricing.'
      )}
      className='shrink-0'
      contentClassName='flex flex-col gap-4'
    >
      <Alert>
        <AlertDescription className='flex flex-col gap-1 text-sm'>
          <div>
            {t(
              'Enter the model name, discount multiplier, and optional end time. Leave end time empty for a permanent discount.'
            )}
          </div>
          <div>
            <span className='font-medium'>{t('Example')}:</span>{' '}
            <code className='bg-muted rounded px-1 py-0.5 text-xs'>0.8</code>{' '}
            {t('means 20% off.')}
          </div>
          <div>
            {t(
              'Use * and ? wildcards to match multiple models, for example gpt-4o* or */claude-*.'
            )}{' '}
            {t('Exact model entries take precedence over wildcard entries.')}
          </div>
        </AlertDescription>
      </Alert>

      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex flex-wrap items-center gap-2'>
          {editMode === 'visual' ? (
            <>
              <Button variant='outline' size='sm' onClick={addRow}>
                <Plus data-icon='inline-start' />
                {t('Add')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setChannelDialogOpen(true)}
              >
                <Wand2 data-icon='inline-start' />
                {t('Add by channel')}
              </Button>
            </>
          ) : (
            <Button variant='ghost' size='sm' onClick={handleCopyJson}>
              <Copy data-icon='inline-start' />
              {t('Copy')}
            </Button>
          )}
        </div>
        <Button variant='outline' size='sm' onClick={toggleEditMode}>
          {editMode === 'visual' ? (
            <>
              <Code2 data-icon='inline-start' />
              {t('Switch to JSON')}
            </>
          ) : (
            <>
              <Eye data-icon='inline-start' />
              {t('Switch to Visual')}
            </>
          )}
        </Button>
      </div>

      {editMode === 'visual' && (
        <div className='flex flex-col gap-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <div className='relative min-w-56 flex-1 sm:max-w-sm'>
              <Search className='text-muted-foreground absolute top-1/2 left-2 size-4 -translate-y-1/2' />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search model discounts...')}
                className='ps-8'
              />
            </div>
            {selectedRows.length > 0 && (
              <Badge variant='secondary'>
                {t('Selected {{count}} discounts', {
                  count: selectedRows.length,
                })}
              </Badge>
            )}
          </div>

          {selectedRows.length > 0 && (
            <div className='bg-muted/40 flex flex-wrap items-center gap-2 rounded-md border p-2'>
              <Input
                type='number'
                min={0.0001}
                step={0.01}
                value={bulkDiscount}
                onChange={(event) =>
                  setBulkDiscount(Number(event.target.value) || 1)
                }
                className='w-32'
                aria-label={t('Discount')}
              />
              <Button variant='outline' size='sm' onClick={applyBulkDiscount}>
                {t('Apply discount')}
              </Button>
              <Input
                type='datetime-local'
                value={bulkEndTime}
                onChange={(event) => setBulkEndTime(event.target.value)}
                className='w-[230px]'
                aria-label={t('End Time')}
              />
              <Button variant='outline' size='sm' onClick={applyBulkEndTime}>
                {t('Apply end time')}
              </Button>
              <Button variant='ghost' size='sm' onClick={clearBulkEndTime}>
                {t('Clear end time')}
              </Button>
              <Button
                variant='destructive'
                size='sm'
                onClick={deleteSelectedRows}
              >
                <Trash2 data-icon='inline-start' />
                {t('Delete selected')}
              </Button>
            </div>
          )}
        </div>
      )}

      {editMode === 'visual' ? (
        <StaticDataTable
          data={filteredRows}
          getRowKey={(row) => row.id}
          emptyClassName='text-muted-foreground py-8'
          emptyContent={
            rows.length > 0
              ? t('No matching results')
              : t('No model discounts configured')
          }
          columns={[
            {
              id: 'select',
              header: (
                <Checkbox
                  checked={allVisibleSelected}
                  indeterminate={someVisibleSelected}
                  onCheckedChange={(value) => toggleVisibleSelection(!!value)}
                  aria-label={t('Select all visible discounts')}
                />
              ),
              className: 'w-[48px]',
              cell: (row) => (
                <Checkbox
                  checked={selectedRowIds.has(row.id)}
                  onCheckedChange={(value) =>
                    toggleRowSelection(row.id, !!value)
                  }
                  aria-label={t('Select discount row')}
                />
              ),
            },
            {
              id: 'model',
              header: t('Model'),
              cell: (row) => (
                <div className='flex min-w-[240px] items-center gap-2'>
                  <Input
                    value={row.model}
                    placeholder='gpt-4o*'
                    className='font-mono'
                    onChange={(e) => updateRow(row.id, 'model', e.target.value)}
                  />
                  {isWildcardModel(row.model) && (
                    <Badge variant='outline'>{t('Wildcard')}</Badge>
                  )}
                </div>
              ),
            },
            {
              id: 'discount',
              header: t('Discount'),
              className: 'w-[160px]',
              cell: (row) => (
                <Input
                  type='number'
                  min={0.0001}
                  step={0.01}
                  value={row.discount}
                  onChange={(e) =>
                    updateRow(row.id, 'discount', Number(e.target.value) || 1)
                  }
                />
              ),
            },
            {
              id: 'endTime',
              header: t('End Time'),
              className: 'w-[230px]',
              cell: (row) => (
                <Input
                  type='datetime-local'
                  value={row.endTime}
                  onChange={(e) => updateRow(row.id, 'endTime', e.target.value)}
                />
              ),
            },
            {
              id: 'actions',
              header: t('Actions'),
              className: 'w-[80px] text-right',
              cellClassName: 'text-right',
              cell: (row) => (
                <Button
                  variant='ghost'
                  size='icon'
                  onClick={() => removeRow(row.id)}
                  aria-label={t('Delete')}
                >
                  <Trash2 className='text-destructive' />
                </Button>
              ),
            },
          ]}
        />
      ) : (
        <div className='flex flex-col gap-2'>
          <Textarea
            value={jsonText}
            onChange={(e) => handleJsonChange(e.target.value)}
            className='font-mono text-sm'
            rows={12}
            spellCheck={false}
          />
          {jsonError && <p className='text-destructive text-sm'>{jsonError}</p>}
        </div>
      )}

      <div className='flex justify-end'>
        <Button
          onClick={handleSave}
          disabled={
            updateOption.isPending || (editMode === 'json' && !!jsonError)
          }
        >
          {t('Save model discounts')}
        </Button>
      </div>

      <ChannelQuickAddDialog
        open={channelDialogOpen}
        onOpenChange={setChannelDialogOpen}
        onAddModels={addModelsFromChannels}
      />
    </TitledCard>
  )
}

type ChannelQuickAddDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onAddModels: (options: AddModelsOptions) => AddModelsResult
}

function ChannelQuickAddDialog({
  open,
  onOpenChange,
  onAddModels,
}: ChannelQuickAddDialogProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [selectedChannelIds, setSelectedChannelIds] = useState<Set<number>>(
    () => new Set()
  )
  const [defaultDiscount, setDefaultDiscount] = useState(0.8)
  const [defaultEndTime, setDefaultEndTime] = useState('')
  const [overwrite, setOverwrite] = useState(false)
  const [isConfirming, setIsConfirming] = useState(false)

  const channelsQuery = useQuery({
    queryKey: ['system-settings', 'model-discount-channels'],
    queryFn: getUpstreamChannels,
    enabled: open,
  })

  const channels = useMemo(() => {
    if (!channelsQuery.data?.success) return []
    return channelsQuery.data.data.filter((channel) => channel.id > 0)
  }, [channelsQuery.data])

  const filteredChannels = useMemo(() => {
    const query = search.trim().toLowerCase()
    if (!query) return channels
    return channels.filter(
      (channel) =>
        channel.name.toLowerCase().includes(query) ||
        channel.base_url.toLowerCase().includes(query)
    )
  }, [channels, search])

  const visibleChannelIds = useMemo(
    () => filteredChannels.map((channel) => channel.id),
    [filteredChannels]
  )
  const allVisibleSelected =
    visibleChannelIds.length > 0 &&
    visibleChannelIds.every((id) => selectedChannelIds.has(id))
  const someVisibleSelected =
    !allVisibleSelected &&
    visibleChannelIds.some((id) => selectedChannelIds.has(id))

  const toggleChannelSelection = useCallback(
    (channelId: number, checked: boolean) => {
      setSelectedChannelIds((prev) => {
        const next = new Set(prev)
        if (checked) {
          next.add(channelId)
        } else {
          next.delete(channelId)
        }
        return next
      })
    },
    []
  )

  const toggleVisibleSelection = useCallback(
    (checked: boolean) => {
      setSelectedChannelIds((prev) => {
        const next = new Set(prev)
        for (const id of visibleChannelIds) {
          if (checked) {
            next.add(id)
          } else {
            next.delete(id)
          }
        }
        return next
      })
    },
    [visibleChannelIds]
  )

  const handleConfirm = useCallback(async () => {
    const selectedChannels = channels.filter((channel) =>
      selectedChannelIds.has(channel.id)
    )
    if (selectedChannels.length === 0) {
      toast.warning(t('Please select at least one channel'))
      return
    }

    setIsConfirming(true)
    try {
      const responses = await Promise.all(
        selectedChannels.map((channel) => getDiscountChannel(channel.id))
      )
      const failed = responses.find((response) => !response.success)
      if (failed) {
        toast.error(failed.message || t('Failed to load channel models'))
        return
      }

      const models = responses.flatMap((response) =>
        parseModelsList(response.data?.models)
      )
      if (models.length === 0) {
        toast.warning(t('No configured models found for selected channels'))
        return
      }

      const result = onAddModels({
        models,
        discount: normalizeDiscount(Number(defaultDiscount)),
        endTime: defaultEndTime,
        overwrite,
      })

      if (result.added === 0 && result.updated === 0) {
        toast.info(t('No new model discounts to add'))
        return
      }

      if (result.updated > 0) {
        toast.success(
          t('Updated {{updated}} and added {{added}} model discounts', {
            updated: result.updated,
            added: result.added,
          })
        )
      } else {
        toast.success(
          t('Added {{added}} model discounts', { added: result.added })
        )
      }
      onOpenChange(false)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load channel models')
      )
    } finally {
      setIsConfirming(false)
    }
  }, [
    channels,
    defaultDiscount,
    defaultEndTime,
    onAddModels,
    onOpenChange,
    overwrite,
    selectedChannelIds,
    t,
  ])

  const columns = useMemo(
    () => [
      {
        id: 'select',
        header: (
          <Checkbox
            checked={allVisibleSelected}
            indeterminate={someVisibleSelected}
            onCheckedChange={(value) => toggleVisibleSelection(!!value)}
            aria-label={t('Select all visible channels')}
          />
        ),
        className: 'w-[48px]',
        cell: (channel: UpstreamChannel) => (
          <Checkbox
            checked={selectedChannelIds.has(channel.id)}
            onCheckedChange={(value) =>
              toggleChannelSelection(channel.id, !!value)
            }
            aria-label={t('Select channel row')}
          />
        ),
      },
      {
        id: 'name',
        header: t('Name'),
        cell: (channel: UpstreamChannel) => (
          <span className='font-medium'>{channel.name}</span>
        ),
      },
      {
        id: 'baseUrl',
        header: t('Base URL'),
        cell: (channel: UpstreamChannel) => (
          <span
            className='text-muted-foreground block max-w-sm truncate font-mono text-xs'
            title={channel.base_url}
          >
            {channel.base_url}
          </span>
        ),
      },
    ],
    [
      allVisibleSelected,
      selectedChannelIds,
      someVisibleSelected,
      t,
      toggleChannelSelection,
      toggleVisibleSelection,
    ]
  )

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Select Discount Channels')}
      description={t(
        'Choose channels whose configured models should receive a discount.'
      )}
      contentClassName='flex max-h-[90vh] max-w-[calc(100%-2rem)] flex-col sm:max-w-[860px]'
      contentHeight='min(70vh, 640px)'
      bodyClassName='flex flex-col gap-4'
      footer={
        <>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isConfirming || selectedChannelIds.size === 0}
          >
            {t('Add selected models')}
          </Button>
        </>
      }
    >
      <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px_230px]'>
        <div className='relative'>
          <Search className='text-muted-foreground absolute top-1/2 left-2 size-4 -translate-y-1/2' />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('Search channels...')}
            className='ps-8'
          />
        </div>
        <Input
          type='number'
          min={0.0001}
          step={0.01}
          value={defaultDiscount}
          onChange={(event) =>
            setDefaultDiscount(Number(event.target.value) || 1)
          }
          aria-label={t('Default discount')}
        />
        <Input
          type='datetime-local'
          value={defaultEndTime}
          onChange={(event) => setDefaultEndTime(event.target.value)}
          aria-label={t('Default end time')}
        />
      </div>

      <label className='flex items-center gap-2 text-sm'>
        <Checkbox
          checked={overwrite}
          onCheckedChange={(value) => setOverwrite(!!value)}
        />
        <span>{t('Overwrite existing discounts')}</span>
      </label>

      <StaticDataTable
        data={filteredChannels}
        getRowKey={(channel) => channel.id}
        emptyClassName='text-muted-foreground py-8'
        emptyContent={
          channelsQuery.isLoading ? t('Loading...') : t('No channels found')
        }
        columns={columns}
      />
    </Dialog>
  )
}
