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
import type { Table } from '@tanstack/react-table'
import {
  Eye,
  EyeOff,
  Trash2,
  Copy,
  Building2,
  Unlink,
  Loader2,
  Tags,
  Shapes,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { MultiSelect, type Option } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { MODEL_CATEGORY_TAGS, getModelCategoryTagOptions } from '../constants'
import {
  handleBatchEnableModels,
  handleBatchDisableModels,
  handleBatchUpdateModelVendor,
  handleBatchUpdateModelTags,
  handleBatchUpdateModelCategories,
  parseModelCategories,
  parseModelTags,
} from '../lib'
import type { Model, Vendor } from '../types'
import type { VendorOperation } from '../vendor-api'
import { ModelDeleteDialog } from './dialogs/model-delete-dialog'
import { VendorOperationDialog } from './dialogs/vendor-operation-dialog'

interface DataTableBulkActionsProps<TData> {
  table: Table<TData>
  vendors: Vendor[]
  categoryOptions: Option[]
}

export function DataTableBulkActions<TData>({
  table,
  vendors,
  categoryOptions,
}: DataTableBulkActionsProps<TData>) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [vendorOperation, setVendorOperation] =
    useState<VendorOperation | null>(null)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [showVendorDialog, setShowVendorDialog] = useState(false)
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoriesDialog, setShowCategoriesDialog] = useState(false)
  const [selectedVendorId, setSelectedVendorId] = useState<string>('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [selectedCategories, setSelectedCategories] = useState<string[]>([])
  const [modelIcon, setModelIcon] = useState('')
  const [isUpdatingVendor, setIsUpdatingVendor] = useState(false)
  const [isUpdatingTags, setIsUpdatingTags] = useState(false)
  const [isUpdatingCategories, setIsUpdatingCategories] = useState(false)

  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedIds = selectedRows.reduce<number[]>((ids, row) => {
    const id = (row.original as Model).id

    if (typeof id === 'number' && id > 0) {
      ids.push(id)
    }

    return ids
  }, [])

  const hasMissingMetadata = selectedRows.some(
    (row) => !((row.original as Model).id > 0)
  )

  const selectedModels = selectedRows.map((row) => row.original as Model)
  const categoryTagSet = new Set<string>(MODEL_CATEGORY_TAGS)

  const handleClearSelection = () => {
    table.resetRowSelection()
  }

  const handleEnableAll = () => {
    handleBatchEnableModels(selectedIds, queryClient, handleClearSelection)
  }

  const handleDisableAll = () => {
    handleBatchDisableModels(selectedIds, queryClient, handleClearSelection)
  }

  const handleVendorChange = (value: string | null) => {
    const nextVendorId = value ?? ''
    setSelectedVendorId(nextVendorId)
    const selectedVendor = vendors.find(
      (vendor) => String(vendor.id) === nextVendorId
    )
    setModelIcon(selectedVendor?.icon?.trim() || '')
  }

  const openTagsDialog = () => {
    const commonTags = selectedModels.reduce<string[] | null>((acc, model) => {
      const modelTags = parseModelTags(model.tags).filter((tag) =>
        categoryTagSet.has(tag)
      )
      if (acc === null) {
        return modelTags
      }
      return acc.filter((tag) => modelTags.includes(tag))
    }, null)

    setSelectedTags(commonTags || [])
    setShowTagsDialog(true)
  }

  const openCategoriesDialog = () => {
    const commonCategories = selectedModels.reduce<string[] | null>(
      (acc, model) => {
        const modelCategories = parseModelCategories(model.categories)
        if (acc === null) {
          return modelCategories
        }
        return acc.filter((category) => modelCategories.includes(category))
      },
      null
    )

    setSelectedCategories(commonCategories || [])
    setShowCategoriesDialog(true)
  }

  const handleUpdateVendor = async () => {
    if (!selectedVendorId) {
      toast.error(t('Please select a vendor'))
      return
    }

    setIsUpdatingVendor(true)
    try {
      await handleBatchUpdateModelVendor(
        selectedIds,
        Number(selectedVendorId),
        modelIcon.trim(),
        queryClient,
        () => {
          setShowVendorDialog(false)
          setSelectedVendorId('')
          setModelIcon('')
          handleClearSelection()
        }
      )
    } finally {
      setIsUpdatingVendor(false)
    }
  }

  const handleUpdateTags = async () => {
    setIsUpdatingTags(true)
    try {
      await handleBatchUpdateModelTags(
        selectedIds,
        selectedTags,
        queryClient,
        () => {
          setShowTagsDialog(false)
          setSelectedTags([])
          handleClearSelection()
        }
      )
    } finally {
      setIsUpdatingTags(false)
    }
  }

  const handleUpdateCategories = async () => {
    setIsUpdatingCategories(true)
    try {
      await handleBatchUpdateModelCategories(
        selectedIds,
        selectedCategories,
        queryClient,
        () => {
          setShowCategoriesDialog(false)
          setSelectedCategories([])
          handleClearSelection()
        }
      )
    } finally {
      setIsUpdatingCategories(false)
    }
  }

  const handleCopyNames = async () => {
    const names = selectedModels.map((m) => m.model_name).join(',')
    const success = await copyToClipboard(names)
    if (success) {
      toast.success(t('Model names copied to clipboard'))
    } else {
      toast.error(t('Failed to copy model names'))
    }
  }

  return (
    <>
      {vendorOperation && (
        <VendorOperationDialog
          selection={vendorOperation}
          onClose={() => setVendorOperation(null)}
          onSuccess={handleClearSelection}
        />
      )}
      <BulkActionsToolbar table={table} entityName='model'>
        {hasMissingMetadata && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span
                  tabIndex={0}
                  aria-description={t(
                    'Add metadata to all selected models first.'
                  )}
                  className='text-muted-foreground max-w-28 truncate text-xs'
                />
              }
            >
              {t('Missing metadata')}
            </TooltipTrigger>
            <TooltipContent>
              {t('Add metadata to all selected models first.')}
            </TooltipContent>
          </Tooltip>
        )}
        <Button
          variant='outline'
          size='icon'
          className='size-8'
          disabled={hasMissingMetadata}
          title={t(
            hasMissingMetadata
              ? 'Add metadata to all selected models first.'
              : 'Change vendor'
          )}
          aria-label={t('Change vendor')}
          onClick={() =>
            setVendorOperation({ action: 'assign', model_ids: selectedIds })
          }
        >
          <Building2 />
        </Button>
        <Button
          variant='outline'
          size='icon'
          className='size-8'
          disabled={hasMissingMetadata}
          title={t(
            hasMissingMetadata
              ? 'Add metadata to all selected models first.'
              : 'Clear vendor'
          )}
          aria-label={t('Clear vendor')}
          onClick={() =>
            setVendorOperation({
              action: 'assign',
              model_ids: selectedIds,
              target_vendor_id: 0,
            })
          }
        >
          <Unlink />
        </Button>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                disabled={hasMissingMetadata}
                onClick={handleEnableAll}
                className='size-8'
                aria-label={t('Show selected models in model square')}
                title={t('Show selected models in model square')}
              />
            }
          >
            <Eye />
            <span className='sr-only'>
              {t('Show selected models in model square')}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            <p>
              {t(
                hasMissingMetadata
                  ? 'Add metadata to all selected models first.'
                  : 'Show selected models in model square'
              )}
            </p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                disabled={hasMissingMetadata}
                onClick={openTagsDialog}
                className='size-8'
                aria-label={t('Update selected models tags')}
                title={t('Update selected models tags')}
              />
            }
          >
            <Tags />
            <span className='sr-only'>{t('Update selected models tags')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Update selected models tags')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                disabled={hasMissingMetadata}
                onClick={openCategoriesDialog}
                className='size-8'
                aria-label={t('Update selected models categories')}
                title={t('Update selected models categories')}
              />
            }
          >
            <Shapes />
            <span className='sr-only'>
              {t('Update selected models categories')}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Update selected models categories')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                disabled={hasMissingMetadata}
                onClick={handleDisableAll}
                className='size-8'
                aria-label={t('Hide selected models from model square')}
                title={t('Hide selected models from model square')}
              />
            }
          >
            <EyeOff />
            <span className='sr-only'>
              {t('Hide selected models from model square')}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            <p>
              {t(
                hasMissingMetadata
                  ? 'Add metadata to all selected models first.'
                  : 'Hide selected models from model square'
              )}
            </p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={handleCopyNames}
                className='size-8'
                aria-label={t('Copy model names')}
                title={t('Copy model names')}
              />
            }
          >
            <Copy />
            <span className='sr-only'>{t('Copy model names')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Copy model names')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                disabled={hasMissingMetadata}
                onClick={() => setShowVendorDialog(true)}
                className='size-8'
                aria-label={t('Update selected models vendor')}
                title={t('Update selected models vendor')}
              />
            }
          >
            <Building2 />
            <span className='sr-only'>
              {t('Update selected models vendor')}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Update selected models vendor')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='destructive'
                size='icon'
                disabled={hasMissingMetadata}
                onClick={() => setShowDeleteConfirm(true)}
                className='size-8'
                aria-label={t('Delete selected models')}
                title={t('Delete selected models')}
              />
            }
          >
            <Trash2 />
            <span className='sr-only'>{t('Delete selected models')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>
              {t(
                hasMissingMetadata
                  ? 'Add metadata to all selected models first.'
                  : 'Delete selected models'
              )}
            </p>
          </TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      {showDeleteConfirm && (
        <ModelDeleteDialog
          models={selectedModels}
          onClose={() => setShowDeleteConfirm(false)}
          onSuccess={handleClearSelection}
        />
      )}

      <Dialog
        open={showTagsDialog}
        onOpenChange={(open) => {
          setShowTagsDialog(open)
          if (!open) {
            setSelectedTags([])
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Update')} {t('Tags')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Choose tags for {{count}} selected model(s). Custom tags outside this set will be kept.',
                { count: selectedIds.length }
              )}
            </DialogDescription>
          </DialogHeader>

          <div className='flex flex-col gap-2'>
            <label className='text-sm font-medium'>{t('Tags')}</label>
            <MultiSelect
              options={getModelCategoryTagOptions(t)}
              selected={selectedTags}
              onChange={setSelectedTags}
              placeholder={t('Select tags...')}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Leaving this empty removes the supported tag values from selected models.'
              )}
            </p>
          </div>

          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setShowTagsDialog(false)}
              disabled={isUpdatingTags}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={handleUpdateTags} disabled={isUpdatingTags}>
              {isUpdatingTags && <Loader2 className='animate-spin' />}
              {t('Update')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={showCategoriesDialog}
        onOpenChange={(open) => {
          setShowCategoriesDialog(open)
          if (!open) {
            setSelectedCategories([])
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Update')} {t('Categories')}
            </DialogTitle>
            <DialogDescription>
              {t('Replace categories for {{count}} selected model(s).', {
                count: selectedIds.length,
              })}
            </DialogDescription>
          </DialogHeader>

          <div className='flex flex-col gap-2'>
            <label className='text-sm font-medium'>{t('Categories')}</label>
            <MultiSelect
              options={categoryOptions}
              selected={selectedCategories}
              onChange={setSelectedCategories}
              allowCreate
              placeholder={t('Select or add categories...')}
            />
            <p className='text-muted-foreground text-xs'>
              {t('Leaving this empty clears categories from selected models.')}
            </p>
          </div>

          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setShowCategoriesDialog(false)}
              disabled={isUpdatingCategories}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleUpdateCategories}
              disabled={isUpdatingCategories}
            >
              {isUpdatingCategories && <Loader2 className='animate-spin' />}
              {t('Update')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={showVendorDialog}
        onOpenChange={(open) => {
          setShowVendorDialog(open)
          if (!open) {
            setSelectedVendorId('')
            setModelIcon('')
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Update')} {t('Vendor')}
            </DialogTitle>
            <DialogDescription>
              {t('Choose a vendor for {{count}} selected model(s).', {
                count: selectedIds.length,
              })}
            </DialogDescription>
          </DialogHeader>

          <div className='flex flex-col gap-2'>
            <label className='text-sm font-medium'>{t('Vendor')}</label>
            <Select
              items={[
                { value: '0', label: t('None') },
                ...vendors.map((vendor) => ({
                  value: String(vendor.id),
                  label: vendor.name,
                })),
              ]}
              onValueChange={handleVendorChange}
              value={selectedVendorId || undefined}
            >
              <SelectTrigger className='w-full'>
                <SelectValue placeholder={t('Select vendor')} />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='0'>{t('None')}</SelectItem>
                  {vendors.map((vendor) => (
                    <SelectItem key={vendor.id} value={String(vendor.id)}>
                      {vendor.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <div className='flex flex-col gap-2'>
            <label className='text-sm font-medium'>{t('Icon')}</label>
            <Input
              value={modelIcon}
              onChange={(event) => setModelIcon(event.target.value)}
              placeholder={t('@lobehub/icons key')}
            />
          </div>

          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setShowVendorDialog(false)}
              disabled={isUpdatingVendor}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleUpdateVendor}
              disabled={!selectedVendorId || isUpdatingVendor}
            >
              {isUpdatingVendor && <Loader2 className='animate-spin' />}
              {t('Update')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
