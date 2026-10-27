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
import {
  useState,
  type ChangeEvent,
  type FocusEvent,
  type InputHTMLAttributes,
  type MouseEvent as ReactMouseEvent,
} from 'react'
import { useTranslation } from 'react-i18next'

import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
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
  getVideoResolutionUnitPrice,
  getMediaKindFromPricingMode,
  VIDEO_RESOLUTION_KEYS,
  VIDEO_RESOLUTION_LABELS,
  type MediaPricingMode,
  type MediaUnitConfig,
  type SpeechBillingSide,
  type VideoResolutionKey,
} from './model-media-pricing'

function formatNumberDraft(value: number | string): string {
  if (value === '') return ''
  if (typeof value === 'number') {
    return Number.isFinite(value) ? String(value) : '0'
  }
  return value
}

function parseNumberDraft(value: string): number {
  if (value.trim() === '') return 0
  const next = Number(value)
  return Number.isFinite(next) ? next : 0
}

function isZeroDraft(value: string): boolean {
  return value.trim() !== '' && parseNumberDraft(value) === 0
}

type DraftNumberInputProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'type' | 'value' | 'onChange'
> & {
  value: number | string
  onValueChange: (next: number) => void
  selectZeroOnFocus?: boolean
}

function DraftNumberInput({
  value,
  onValueChange,
  selectZeroOnFocus = true,
  onBlur,
  onFocus,
  onMouseUp,
  ...props
}: DraftNumberInputProps) {
  const [draft, setDraft] = useState(() => formatNumberDraft(value))
  const [focused, setFocused] = useState(false)
  const displayValue = focused ? draft : formatNumberDraft(value)

  const handleChange = (event: ChangeEvent<HTMLInputElement>) => {
    const nextDraft = event.target.value
    setDraft(nextDraft)
    onValueChange(parseNumberDraft(nextDraft))
  }

  const handleFocus = (event: FocusEvent<HTMLInputElement>) => {
    setFocused(true)
    setDraft(event.currentTarget.value)
    onFocus?.(event)
    if (selectZeroOnFocus && isZeroDraft(event.currentTarget.value)) {
      event.currentTarget.select()
    }
  }

  const handleMouseUp = (event: ReactMouseEvent<HTMLInputElement>) => {
    onMouseUp?.(event)
    if (selectZeroOnFocus && isZeroDraft(event.currentTarget.value)) {
      event.preventDefault()
      event.currentTarget.select()
    }
  }

  const handleBlur = (event: FocusEvent<HTMLInputElement>) => {
    const normalized = parseNumberDraft(event.currentTarget.value)
    setFocused(false)
    setDraft(String(normalized))
    onValueChange(normalized)
    onBlur?.(event)
  }

  return (
    <Input
      {...props}
      type='number'
      value={displayValue}
      onChange={handleChange}
      onFocus={handleFocus}
      onMouseUp={handleMouseUp}
      onBlur={handleBlur}
    />
  )
}

function MediaNumberField(props: {
  labelKey: string
  value: number
  onChange: (next: number) => void
  descriptionKey?: string
}) {
  const { t } = useTranslation()

  return (
    <Field>
      <FieldLabel>{t(props.labelKey)}</FieldLabel>
      <DraftNumberInput
        aria-label={t(props.labelKey)}
        min={0}
        step={0.000001}
        value={props.value}
        onValueChange={props.onChange}
      />
      {props.descriptionKey && (
        <FieldDescription>{t(props.descriptionKey)}</FieldDescription>
      )}
    </Field>
  )
}

export function MediaPricingFields(props: {
  mode: MediaPricingMode
  config: MediaUnitConfig
  onChange: (next: MediaUnitConfig) => void
}) {
  const { t } = useTranslation()
  const kind = getMediaKindFromPricingMode(props.mode)

  const updateConfig = (patch: Partial<MediaUnitConfig>) => {
    props.onChange({ ...props.config, kind, ...patch })
  }

  if (props.mode === 'media-image') {
    return (
      <FieldGroup className='gap-5'>
        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
          <MediaNumberField
            labelKey='USD per 1024x1024 image'
            value={props.config.imageBasePrice}
            onChange={(value) => updateConfig({ imageBasePrice: value })}
          />
          <MediaNumberField
            labelKey='Default image count'
            value={props.config.imageDefaultCount}
            onChange={(value) => updateConfig({ imageDefaultCount: value })}
          />
          <MediaNumberField
            labelKey='256x256 multiplier'
            value={props.config.imageSmallSizeMultiplier}
            onChange={(value) =>
              updateConfig({ imageSmallSizeMultiplier: value })
            }
          />
          <MediaNumberField
            labelKey='512x512 multiplier'
            value={props.config.imageMediumSizeMultiplier}
            onChange={(value) =>
              updateConfig({ imageMediumSizeMultiplier: value })
            }
          />
          <MediaNumberField
            labelKey='Portrait/landscape multiplier'
            value={props.config.imageLargeSizeMultiplier}
            onChange={(value) =>
              updateConfig({ imageLargeSizeMultiplier: value })
            }
            descriptionKey='Applied to 1024x1792 and 1792x1024.'
          />
        </div>
      </FieldGroup>
    )
  }

  if (props.mode === 'media-video') {
    const updateResolutionPrice = (
      resolution: VideoResolutionKey,
      value: number
    ) => {
      updateConfig({
        videoResolutionPrices: {
          ...props.config.videoResolutionPrices,
          [resolution]: value,
        },
        videoSecondPrice:
          resolution === '720p' ? value : props.config.videoSecondPrice,
      })
    }

    return (
      <FieldGroup className='gap-5'>
        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
          {VIDEO_RESOLUTION_KEYS.map((resolution) => (
            <MediaNumberField
              key={resolution}
              labelKey={`${VIDEO_RESOLUTION_LABELS[resolution]} USD per second`}
              value={getVideoResolutionUnitPrice(props.config, resolution)}
              onChange={(value) => updateResolutionPrice(resolution, value)}
            />
          ))}
        </div>

        <div className='grid gap-3 sm:grid-cols-2'>
          <MediaNumberField
            labelKey='Default seconds'
            value={props.config.videoDefaultSeconds}
            onChange={(value) => updateConfig({ videoDefaultSeconds: value })}
            descriptionKey='Used when the request omits seconds and duration.'
          />
          <Field>
            <FieldLabel>{t('Default resolution')}</FieldLabel>
            <Select
              items={VIDEO_RESOLUTION_KEYS.map((resolution) => ({
                value: resolution,
                label: VIDEO_RESOLUTION_LABELS[resolution],
              }))}
              value={props.config.videoDefaultResolution}
              onValueChange={(value) =>
                updateConfig({
                  videoDefaultResolution: value as VideoResolutionKey,
                })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {VIDEO_RESOLUTION_KEYS.map((resolution) => (
                    <SelectItem key={resolution} value={resolution}>
                      {VIDEO_RESOLUTION_LABELS[resolution]}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FieldDescription>
              {t('Used when the request omits size or resolution.')}
            </FieldDescription>
          </Field>
        </div>
      </FieldGroup>
    )
  }

  return (
    <FieldGroup className='gap-5'>
      <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_16rem]'>
        <MediaNumberField
          labelKey='USD per audio second'
          value={props.config.speechSecondPrice}
          onChange={(value) => updateConfig({ speechSecondPrice: value })}
        />
        <Field>
          <FieldLabel>{t('Bill audio side')}</FieldLabel>
          <Select
            items={[
              { value: 'both', label: t('Input and output audio') },
              { value: 'input', label: t('Input audio only') },
              { value: 'output', label: t('Output audio only') },
            ]}
            value={props.config.speechSide}
            onValueChange={(value) =>
              updateConfig({ speechSide: value as SpeechBillingSide })
            }
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='both'>
                  {t('Input and output audio')}
                </SelectItem>
                <SelectItem value='input'>{t('Input audio only')}</SelectItem>
                <SelectItem value='output'>{t('Output audio only')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
      </div>
    </FieldGroup>
  )
}
