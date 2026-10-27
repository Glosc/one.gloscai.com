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
import { getRouteApi } from '@tanstack/react-router'
import type { OnChangeFn, SortingState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { useModelPricing } from '@/features/model-pricing/api'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import {
  getServerMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { getModels, searchModels, getVendors } from '../api'
import { DEFAULT_PAGE_SIZE, getPriceStatusOptions } from '../constants'
import { modelsQueryKeys, vendorsQueryKeys } from '../lib'
import type { ModelSortBy, ModelSquareState } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useModelsColumns } from './models-columns'
import { useModels } from './models-provider'

const route = getRouteApi('/_authenticated/models/$section')
const EMPTY_TAG_FILTER_VALUE = '__empty__'
const EMPTY_CATEGORY_FILTER_VALUE = '__empty__'
const MODEL_SORTABLE_COLUMNS = new Set<ModelSortBy>([
  'id',
  'model_name',
  'created_time',
  'updated_time',
])

export function ModelsTable() {
  const { t } = useTranslation()
  const { selectedVendor } = useModels()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [sorting, setSorting] = useState<SortingState>([])

  // URL state management
  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      defaultPage: 1,
      defaultPageSize: isMobile ? 10 : DEFAULT_PAGE_SIZE,
    },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'square_state', searchKey: 'square_state', type: 'array' },
      { columnId: 'vendor_id', searchKey: 'vendor', type: 'array' },
      { columnId: 'tags', searchKey: 'tag', type: 'array' },
      { columnId: 'categories', searchKey: 'category', type: 'array' },
      { columnId: 'sync_official', searchKey: 'sync', type: 'array' },
      { columnId: 'has_price', searchKey: 'price', type: 'array' },
    ],
  })

  // Extract filters from column filters
  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as string[]) || []
  const squareState = (
    columnFilters.find((f) => f.id === 'square_state')?.value as
      | ModelSquareState[]
      | undefined
  )?.[0]
  const vendorFilter =
    (columnFilters.find((f) => f.id === 'vendor_id')?.value as string[]) || []
  const tagFilter =
    (columnFilters.find((f) => f.id === 'tags')?.value as string[]) || []
  const categoryFilter =
    (columnFilters.find((f) => f.id === 'categories')?.value as string[]) || []
  const syncFilter =
    (columnFilters.find((f) => f.id === 'sync_official')?.value as string[]) ||
    []
  const priceFilter =
    (columnFilters.find((f) => f.id === 'has_price')?.value as string[]) || []

  // Fetch vendors for filter
  const { data: vendorsData } = useQuery({
    queryKey: vendorsQueryKeys.list(),
    queryFn: async () =>
      requireServerSuccess(await getVendors({ page_size: 1000 })),
  })

  const vendors = useMemo(
    () => vendorsData?.data?.items || [],
    [vendorsData?.data?.items]
  )

  const vendorOptions = useMemo(() => {
    return vendors.map((v) => ({
      label: v.name,
      value: String(v.id),
    }))
  }, [vendors])

  // Apply selected vendor from context or filter
  const activeVendorFilter =
    selectedVendor ||
    (vendorFilter.length > 0 && !vendorFilter.includes('all')
      ? vendorFilter[0]
      : undefined)
  const activeTagFilter =
    tagFilter.length > 0 && !tagFilter.includes('all')
      ? tagFilter[0]
      : undefined
  const activeCategoryFilter =
    categoryFilter.length > 0 && !categoryFilter.includes('all')
      ? categoryFilter[0]
      : undefined

  const sortParams = useMemo(() => {
    const activeSort = sorting[0]
    if (
      !activeSort ||
      !MODEL_SORTABLE_COLUMNS.has(activeSort.id as ModelSortBy)
    ) {
      return {}
    }

    return {
      sort_by: activeSort.id as ModelSortBy,
      sort_order: activeSort.desc ? 'desc' : 'asc',
    } as const
  }, [sorting])

  const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
    setSorting((previous) => {
      const next = typeof updater === 'function' ? updater(previous) : updater
      if (pagination.pageIndex > 0) {
        onPaginationChange({ ...pagination, pageIndex: 0 })
      }
      return next
    })
  }

  const statusFilterValue =
    statusFilter.length > 0 && !statusFilter.includes('all')
      ? statusFilter[0]
      : undefined
  const syncFilterValue =
    syncFilter.length > 0 && !syncFilter.includes('all')
      ? syncFilter[0]
      : undefined

  // Use search API whenever any filter is active so status/sync are applied server-side
  const shouldSearch = Boolean(
    globalFilter?.trim() ||
    activeVendorFilter ||
    statusFilterValue ||
    activeTagFilter ||
    activeCategoryFilter ||
    priceFilter.length > 0 ||
    squareState ||
    syncFilterValue
  )

  // Fetch models data
  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching, isError, error, refetch } = useQuery({
    queryKey: modelsQueryKeys.list({
      include_channel_models: true,
      keyword: globalFilter,
      tag: activeTagFilter,
      category: activeCategoryFilter,
      has_price: priceFilter[0],
      ...sortParams,
      vendor: activeVendorFilter,
      status: statusFilterValue,
      square_state: squareState,
      sync_official: syncFilterValue,
      p: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
    }),
    queryFn: async () => {
      if (shouldSearch) {
        return requireServerSuccess(
          await searchModels({
            include_channel_models: true,
            tag: activeTagFilter,
            category: activeCategoryFilter,
            has_price: priceFilter[0],
            ...sortParams,
            keyword: globalFilter,
            vendor: activeVendorFilter,
            status: statusFilterValue,
            square_state: squareState,
            sync_official: syncFilterValue,
            p: pagination.pageIndex + 1,
            page_size: pagination.pageSize,
          })
        )
      }
      return requireServerSuccess(
        await getModels({
          include_channel_models: true,
          tag: activeTagFilter,
          category: activeCategoryFilter,
          has_price: priceFilter[0],
          ...sortParams,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
      )
    },
  })

  const models = data?.data?.items || []
  const totalCount = data?.data?.total || 0
  const vendorCounts = data?.data?.vendor_counts
  const tagCounts = data?.data?.tag_counts
  const categoryCounts = data?.data?.category_counts

  // Columns configuration
  const pricingQuery = useModelPricing(
    models
      .filter((item) => item.name_rule === 0)
      .map((item) => item.model_name),
    models.length > 0
  )
  let pricingState: 'loading' | 'error' | undefined
  if (pricingQuery.isError) pricingState = 'error'
  else if (pricingQuery.isLoading) pricingState = 'loading'
  const columns = useModelsColumns(vendors, pricingQuery.data, pricingState)

  // React Table instance
  const { table } = useDataTable({
    data: models,
    getRowId: (model) =>
      model.id > 0 ? `metadata:${model.id}` : `channel:${model.model_name}`,
    columns,
    totalCount,
    initialColumnVisibility: {
      has_price: false,
      description: false,
      id: false,
      vendor_id: false,
      name_rule: false,
      endpoints: false,
      created_time: false,
      updated_time: false,
      status: false,
    },
    columnFilters,
    sorting,
    pagination,
    globalFilter,
    enableRowSelection: true,
    onSortingChange: handleSortingChange,
    onColumnFiltersChange,
    onPaginationChange,
    onGlobalFilterChange,
    manualPagination: true,
    manualFiltering: true,
    ensurePageInRange,
  })

  // Prepare filter options
  const vendorFilterOptions = [
    {
      label: `${t('All Vendors')}${vendorCounts?.all ? ` (${vendorCounts.all})` : ''}`,
      value: 'all',
    },
    ...vendorOptions.map((option) => ({
      label: `${option.label}${vendorCounts?.[option.value] ? ` (${vendorCounts[option.value]})` : ''}`,
      value: option.value,
    })),
  ]

  const tagFilterOptions = [
    {
      label: t('All Tags'),
      value: 'all',
    },
    {
      label: `${t('No Tags')}${tagCounts?.[EMPTY_TAG_FILTER_VALUE] ? ` (${tagCounts[EMPTY_TAG_FILTER_VALUE]})` : ''}`,
      value: EMPTY_TAG_FILTER_VALUE,
    },
    ...Object.entries(tagCounts || {})
      .filter(([tag]) => tag !== EMPTY_TAG_FILTER_VALUE)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([tag, count]) => ({
        label: `${tag}${count ? ` (${count})` : ''}`,
        value: tag,
      })),
  ]
  const categoryFilterOptions = [
    {
      label: t('All Categories'),
      value: 'all',
    },
    {
      label: `${t('No Categories')}${categoryCounts?.[EMPTY_CATEGORY_FILTER_VALUE] ? ` (${categoryCounts[EMPTY_CATEGORY_FILTER_VALUE]})` : ''}`,
      value: EMPTY_CATEGORY_FILTER_VALUE,
    },
    ...Object.entries(categoryCounts || {})
      .filter(([category]) => category !== EMPTY_CATEGORY_FILTER_VALUE)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([category, count]) => ({
        label: `${category}${count ? ` (${count})` : ''}`,
        value: category,
      })),
  ]

  const categoryOptions = categoryFilterOptions.filter(
    (option) =>
      option.value !== 'all' && option.value !== EMPTY_CATEGORY_FILTER_VALUE
  )
  if (isError || data?.success === false) {
    return (
      <ErrorState
        description={error?.message ?? getServerMessage(data)}
        onRetry={() => void refetch()}
      />
    )
  }

  return (
    <DataTablePage
      showMobileBulkActions
      mobileProps={{ enableRowSelection: true }}
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Models Found')}
      emptyDescription={
        shouldSearch
          ? t('Try adjusting your search')
          : t('No models available. Create your first model to get started.')
      }
      skeletonKeyPrefix='model-skeleton'
      applyHeaderSize
      pinnedColumns={[
        { columnId: 'model_name', side: 'left' },
        { columnId: 'actions', side: 'right' },
      ]}
      toolbarProps={{
        searchPlaceholder: t('Filter by model name...'),
        searchDebounceMs: 500,
        filters: [
          {
            columnId: 'status',
            title: t('Display policy'),
            options: [
              { label: t('Allowed'), value: 'enabled' },
              { label: t('Not listed'), value: 'disabled' },
            ],
            singleSelect: true,
          },
          {
            columnId: 'square_state',
            title: t('Model square visibility'),
            options: [
              { label: t('Displayed'), value: 'visible' },
              { label: t('Unavailable'), value: 'unavailable' },
              { label: t('Listing hidden'), value: 'hidden' },
              { label: t('Partly shown'), value: 'partial' },
            ],
            singleSelect: true,
          },
          {
            columnId: 'vendor_id',
            title: t('Vendor'),
            options: vendorFilterOptions,
            singleSelect: true,
          },
          {
            columnId: 'tags',
            title: t('Tags'),
            options: tagFilterOptions,
            singleSelect: true,
          },
          {
            columnId: 'categories',
            title: t('Categories'),
            options: categoryFilterOptions,
            singleSelect: true,
          },
          {
            columnId: 'sync_official',
            title: t('Sync policy'),
            options: [
              { label: t('Allow updates'), value: 'yes' },
              { label: t('Keep local'), value: 'no' },
            ],
            singleSelect: true,
          },
          {
            columnId: 'has_price',
            title: t('Pricing'),
            options: [...getPriceStatusOptions(t)],
            singleSelect: true,
          },
        ],
      }}
      bulkActions={
        <DataTableBulkActions
          table={table}
          vendors={vendors}
          categoryOptions={categoryOptions}
        />
      }
    />
  )
}
