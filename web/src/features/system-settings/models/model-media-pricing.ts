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

export type MediaPricingMode = 'media-image' | 'media-video' | 'media-audio'
export type MediaUnitKind = 'video' | 'image' | 'speech'
export type SpeechBillingSide = 'input' | 'output' | 'both'
export type VideoResolutionKey = '480p' | '720p' | '1080p' | '4k'
export type VideoResolutionPriceMap = Record<VideoResolutionKey, number>
export type ExpressionBackedPricingMode = 'tiered_expr' | MediaPricingMode

export type MediaUnitConfig = {
  kind: MediaUnitKind
  videoSecondPrice: number
  videoDefaultSeconds: number
  videoLargeSizeMultiplier: number
  videoResolutionPrices: VideoResolutionPriceMap
  videoDefaultResolution: VideoResolutionKey
  imageBasePrice: number
  imageDefaultCount: number
  imageSmallSizeMultiplier: number
  imageMediumSizeMultiplier: number
  imageLargeSizeMultiplier: number
  speechSecondPrice: number
  speechSide: SpeechBillingSide
}

export const MEDIA_PRICING_MODES: MediaPricingMode[] = [
  'media-image',
  'media-video',
  'media-audio',
]

export const VIDEO_RESOLUTION_KEYS = [
  '480p',
  '720p',
  '1080p',
  '4k',
] as const satisfies readonly VideoResolutionKey[]

export const VIDEO_RESOLUTION_LABELS: Record<VideoResolutionKey, string> = {
  '480p': '480P',
  '720p': '720P',
  '1080p': '1080P',
  '4k': '4K',
}

export const DEFAULT_VIDEO_RESOLUTION_PRICES: VideoResolutionPriceMap = {
  '480p': 0.03,
  '720p': 0.05,
  '1080p': 0.083333,
  '4k': 0.2,
}

export const DEFAULT_MEDIA_UNIT_CONFIG: MediaUnitConfig = {
  kind: 'video',
  videoSecondPrice: 0.05,
  videoDefaultSeconds: 1,
  videoLargeSizeMultiplier: 1.666667,
  videoResolutionPrices: DEFAULT_VIDEO_RESOLUTION_PRICES,
  videoDefaultResolution: '720p',
  imageBasePrice: 0.04,
  imageDefaultCount: 1,
  imageSmallSizeMultiplier: 0.4,
  imageMediumSizeMultiplier: 0.45,
  imageLargeSizeMultiplier: 2,
  speechSecondPrice: 0.006,
  speechSide: 'both',
}

const NUMBER_PATTERN = '([-+]?(?:\\d+\\.?\\d*|\\.\\d+)(?:e[-+]?\\d+)?)'

const VIDEO_RESOLUTION_ALIASES: Record<VideoResolutionKey, string[]> = {
  '480p': [
    '480p',
    '480',
    '512p',
    '512',
    '854x480',
    '480x854',
    '832x480',
    '480x832',
    '624x624',
  ],
  '720p': [
    '720p',
    '720',
    '768p',
    '768',
    '1280x720',
    '720x1280',
    '960x960',
    '1088x832',
    '832x1088',
  ],
  '1080p': [
    '1080p',
    '1080',
    '1920x1080',
    '1080x1920',
    '1792x1024',
    '1024x1792',
    '1440x1440',
    '1632x1248',
    '1248x1632',
  ],
  '4k': ['4k', '2160p', '2160', '3840x2160', '2160x3840', '4096x2160'],
}

const VIDEO_RESOLUTION_PARAM_EXPRS = [
  'lower(str(param("resolution")))',
  'lower(str(param("metadata.resolution")))',
  'lower(str(param("size")))',
  'lower(str(param("metadata.size")))',
]

export function isMediaPricingMode(mode?: string): mode is MediaPricingMode {
  return MEDIA_PRICING_MODES.includes(mode as MediaPricingMode)
}

export function isExpressionBackedPricingMode(
  mode?: string
): mode is ExpressionBackedPricingMode {
  return mode === 'tiered_expr' || isMediaPricingMode(mode)
}

export function getMediaKindFromPricingMode(
  mode: MediaPricingMode
): MediaUnitKind {
  if (mode === 'media-image') return 'image'
  if (mode === 'media-audio') return 'speech'
  return 'video'
}

export function getMediaPricingModeFromKind(
  kind: MediaUnitKind
): MediaPricingMode {
  if (kind === 'image') return 'media-image'
  if (kind === 'speech') return 'media-audio'
  return 'media-video'
}

export function getMediaModeLabel(mode: MediaPricingMode): string {
  if (mode === 'media-image') return 'Per-image'
  if (mode === 'media-video') return 'Per-video'
  return 'Per-audio'
}

export function createDefaultMediaUnitConfig(
  kindOrMode: MediaUnitKind | MediaPricingMode = 'video'
): MediaUnitConfig {
  const kind = isMediaPricingMode(kindOrMode)
    ? getMediaKindFromPricingMode(kindOrMode)
    : kindOrMode
  return {
    ...DEFAULT_MEDIA_UNIT_CONFIG,
    kind,
  }
}

function exprNumber(value: number): string {
  if (!Number.isFinite(value)) return '0'
  return Number.parseFloat(value.toFixed(12)).toString()
}

function escapeRegExp(value: string): string {
  return value.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function withStarSizeAliases(values: string[]): string[] {
  const aliases = new Set<string>()
  values.forEach((value) => {
    aliases.add(value)
    if (value.includes('x')) aliases.add(value.replaceAll('x', '*'))
  })
  return [...aliases]
}

function normalizeVideoResolutionPrices(
  prices?: Partial<VideoResolutionPriceMap> | null
): VideoResolutionPriceMap {
  return {
    ...DEFAULT_VIDEO_RESOLUTION_PRICES,
    ...prices,
  }
}

function getVideoResolutionFallback(resolution?: string): VideoResolutionKey {
  return VIDEO_RESOLUTION_KEYS.includes(resolution as VideoResolutionKey)
    ? (resolution as VideoResolutionKey)
    : '720p'
}

function buildVideoResolutionMatchExpr(resolution: VideoResolutionKey): string {
  const aliases = withStarSizeAliases(VIDEO_RESOLUTION_ALIASES[resolution])
  return VIDEO_RESOLUTION_PARAM_EXPRS.flatMap((paramExpr) =>
    aliases.map((alias) => `${paramExpr} == "${alias}"`)
  ).join(' || ')
}

function buildVideoResolutionPriceExpr(config: MediaUnitConfig): string {
  const prices = normalizeVideoResolutionPrices(config.videoResolutionPrices)
  const fallbackResolution = getVideoResolutionFallback(
    config.videoDefaultResolution
  )
  const fallbackPrice = exprNumber(prices[fallbackResolution])
  const orderedResolutions: VideoResolutionKey[] = [
    '4k',
    '1080p',
    '720p',
    '480p',
  ]

  return orderedResolutions.reduceRight((expr, resolution) => {
    const price = exprNumber(prices[resolution])
    return `${buildVideoResolutionMatchExpr(resolution)} ? ${price} : ${expr}`
  }, fallbackPrice)
}

function buildNumberFallbackExpr(paths: string[], fallback: string): string {
  return paths.reduceRight(
    (expr, path) => `num(param("${path}"), ${expr})`,
    fallback
  )
}

export function getVideoResolutionUnitPrice(
  config: MediaUnitConfig,
  resolution: VideoResolutionKey = getVideoResolutionFallback(
    config.videoDefaultResolution
  )
): number {
  return normalizeVideoResolutionPrices(config.videoResolutionPrices)[
    resolution
  ]
}

export function generateMediaUnitExpr(config: MediaUnitConfig): string {
  if (config.kind === 'video') {
    const defaultSeconds = exprNumber(config.videoDefaultSeconds)
    const outputSecondsExpr = `max(${buildNumberFallbackExpr(
      [
        'seconds',
        'duration',
        'durationSeconds',
        'duration_seconds',
        'metadata.durationSeconds',
        'metadata.duration_seconds',
        'metadata.duration',
        'metadata.seconds',
      ],
      defaultSeconds
    )}, 0)`
    const countExpr = `max(${buildNumberFallbackExpr(
      ['n', 'metadata.sampleCount'],
      '1'
    )}, 1)`
    const billableSecondsExpr = `(${outputSecondsExpr} * ${countExpr})`
    return `tier("video", usd(${billableSecondsExpr} * (${buildVideoResolutionPriceExpr(config)})))`
  }

  if (config.kind === 'image') {
    const basePrice = exprNumber(config.imageBasePrice)
    const defaultCount = exprNumber(config.imageDefaultCount)
    const smallMultiplier = exprNumber(config.imageSmallSizeMultiplier)
    const mediumMultiplier = exprNumber(config.imageMediumSizeMultiplier)
    const largeMultiplier = exprNumber(config.imageLargeSizeMultiplier)
    return `tier("image", usd(max(num(param("n"), ${defaultCount}), 1) * ${basePrice} * (str(param("size")) == "256x256" ? ${smallMultiplier} : str(param("size")) == "512x512" ? ${mediumMultiplier} : (str(param("size")) == "1024x1792" || str(param("size")) == "1792x1024" ? ${largeMultiplier} : 1))))`
  }

  let audioTokens = 'ai + ao'
  if (config.speechSide === 'input') {
    audioTokens = 'ai'
  } else if (config.speechSide === 'output') {
    audioTokens = 'ao'
  }

  const requestSeconds =
    config.speechSide === 'input'
      ? 'max(num(param("seconds"), num(param("duration"), 0)), 0)'
      : 'max(num(param("seconds"), num(param("duration"), num(param("estimated_duration"), 0))), 0)'
  return `tier("speech", usd((seconds(${audioTokens}) > 0 ? seconds(${audioTokens}) : ${requestSeconds}) * ${exprNumber(config.speechSecondPrice)}))`
}

export function detectMediaPricingMode(
  expr?: string | null
): MediaPricingMode | null {
  if (!expr || !/\busd\s*\(/.test(expr)) return null
  if (/tier\(\s*["']image["']/.test(expr)) return 'media-image'
  if (/tier\(\s*["']video["']/.test(expr)) return 'media-video'
  if (/tier\(\s*["']speech["']/.test(expr)) return 'media-audio'
  return null
}

function readNumber(
  expr: string,
  pattern: string,
  groupIndex = 1
): number | null {
  const match = expr.match(new RegExp(pattern, 'i'))
  if (!match) return null
  const value = Number(match[groupIndex])
  return Number.isFinite(value) ? value : null
}

function readLastNumber(
  expr: string,
  pattern: string,
  groupIndex = 1
): number | null {
  const re = new RegExp(pattern, 'gi')
  let match: RegExpExecArray | null
  let value: number | null = null
  while ((match = re.exec(expr)) !== null) {
    const nextValue = Number(match[groupIndex])
    if (Number.isFinite(nextValue)) value = nextValue
  }
  return value
}

function readVideoResolutionPrice(
  expr: string,
  resolution: VideoResolutionKey
): number | null {
  const aliasPattern = withStarSizeAliases(VIDEO_RESOLUTION_ALIASES[resolution])
    .map(escapeRegExp)
    .join('|')
  return readNumber(
    expr,
    `==\\s*"(?:${aliasPattern})"(?:(?!\\?).)*\\?\\s*${NUMBER_PATTERN}`
  )
}

function readSpeechSide(expr: string): SpeechBillingSide {
  const match = expr.match(/seconds\(([^)]+)\)/i)
  const tokens = match?.[1]?.trim().replaceAll(/\s+/g, ' ')
  if (tokens === 'ai') return 'input'
  if (tokens === 'ao') return 'output'
  return 'both'
}

export function tryParseMediaUnitConfig(
  expr?: string | null
): MediaUnitConfig | null {
  const mode = detectMediaPricingMode(expr)
  if (!mode || !expr) return null

  const defaults = createDefaultMediaUnitConfig(mode)
  const billingExpr = expr.split('|||')[0] ?? expr

  if (mode === 'media-video') {
    const defaultSeconds =
      readNumber(
        billingExpr,
        `num\\(param\\("metadata\\.durationSeconds"\\),\\s*${NUMBER_PATTERN}\\)`
      ) ??
      readNumber(
        billingExpr,
        `num\\(param\\("metadata\\.duration_seconds"\\),\\s*${NUMBER_PATTERN}\\)`
      ) ??
      readNumber(
        billingExpr,
        `num\\(param\\("metadata\\.duration"\\),\\s*${NUMBER_PATTERN}\\)`
      ) ??
      readNumber(
        billingExpr,
        `num\\(param\\("metadata\\.seconds"\\),\\s*${NUMBER_PATTERN}\\)`
      ) ??
      readNumber(
        billingExpr,
        `num\\(param\\("durationSeconds"\\),\\s*${NUMBER_PATTERN}\\)`
      ) ??
      readNumber(
        billingExpr,
        `num\\(param\\("duration_seconds"\\),\\s*${NUMBER_PATTERN}\\)`
      ) ??
      readNumber(
        billingExpr,
        `num\\(param\\("duration"\\),\\s*${NUMBER_PATTERN}\\)`
      ) ??
      defaults.videoDefaultSeconds
    const parsedResolutionPrices = Object.fromEntries(
      VIDEO_RESOLUTION_KEYS.map((resolution) => [
        resolution,
        readVideoResolutionPrice(billingExpr, resolution),
      ])
    ) as Record<VideoResolutionKey, number | null>
    const hasResolutionPrices = VIDEO_RESOLUTION_KEYS.some(
      (resolution) => parsedResolutionPrices[resolution] !== null
    )
    const legacySecondPrice =
      readNumber(
        billingExpr,
        `max\\(num\\(param\\("seconds"\\),\\s*num\\(param\\("duration"\\),\\s*${NUMBER_PATTERN}\\)\\),\\s*0\\)\\s*\\*\\s*${NUMBER_PATTERN}`,
        2
      ) ?? defaults.videoSecondPrice
    const legacyLargeMultiplier =
      readNumber(
        billingExpr,
        `"1792x1024"\\s*\\?\\s*${NUMBER_PATTERN}\\s*:\\s*1`
      ) ?? defaults.videoLargeSizeMultiplier
    const videoResolutionPrices = hasResolutionPrices
      ? normalizeVideoResolutionPrices(
          Object.fromEntries(
            VIDEO_RESOLUTION_KEYS.map((resolution) => [
              resolution,
              parsedResolutionPrices[resolution] ?? undefined,
            ])
          ) as Partial<VideoResolutionPriceMap>
        )
      : {
          ...defaults.videoResolutionPrices,
          '480p': legacySecondPrice,
          '720p': legacySecondPrice,
          '1080p': legacySecondPrice * legacyLargeMultiplier,
          '4k': legacySecondPrice * legacyLargeMultiplier,
        }
    const fallbackResolutionPrice = readLastNumber(
      billingExpr,
      `:\\s*${NUMBER_PATTERN}\\s*\\)\\)\\)?\\s*$`
    )
    const parsedDefaultResolution =
      VIDEO_RESOLUTION_KEYS.find(
        (resolution) =>
          fallbackResolutionPrice !== null &&
          Math.abs(
            videoResolutionPrices[resolution] - fallbackResolutionPrice
          ) < 1e-9
      ) ?? defaults.videoDefaultResolution

    return {
      ...defaults,
      videoDefaultSeconds: defaultSeconds,
      videoSecondPrice: videoResolutionPrices['720p'],
      videoLargeSizeMultiplier:
        videoResolutionPrices['720p'] > 0
          ? videoResolutionPrices['1080p'] / videoResolutionPrices['720p']
          : defaults.videoLargeSizeMultiplier,
      videoResolutionPrices,
      videoDefaultResolution: parsedDefaultResolution,
    }
  }

  if (mode === 'media-image') {
    return {
      ...defaults,
      imageDefaultCount:
        readNumber(
          billingExpr,
          `num\\(param\\("n"\\),\\s*${NUMBER_PATTERN}\\)`
        ) ?? defaults.imageDefaultCount,
      imageBasePrice:
        readNumber(
          billingExpr,
          `max\\(num\\(param\\("n"\\),\\s*${NUMBER_PATTERN}\\),\\s*1\\)\\s*\\*\\s*${NUMBER_PATTERN}`,
          2
        ) ?? defaults.imageBasePrice,
      imageSmallSizeMultiplier:
        readNumber(billingExpr, `"256x256"\\s*\\?\\s*${NUMBER_PATTERN}`) ??
        defaults.imageSmallSizeMultiplier,
      imageMediumSizeMultiplier:
        readNumber(billingExpr, `"512x512"\\s*\\?\\s*${NUMBER_PATTERN}`) ??
        defaults.imageMediumSizeMultiplier,
      imageLargeSizeMultiplier:
        readNumber(
          billingExpr,
          `"1792x1024"\\s*\\?\\s*${NUMBER_PATTERN}\\s*:\\s*1`
        ) ?? defaults.imageLargeSizeMultiplier,
    }
  }

  return {
    ...defaults,
    speechSecondPrice:
      readNumber(
        billingExpr,
        `seconds\\([^)]+\\)\\s*\\*\\s*${NUMBER_PATTERN}`
      ) ??
      readNumber(
        billingExpr,
        `\\)\\s*\\*\\s*${NUMBER_PATTERN}\\s*\\)\\s*\\)?\\s*$`
      ) ??
      defaults.speechSecondPrice,
    speechSide: readSpeechSide(billingExpr),
  }
}
