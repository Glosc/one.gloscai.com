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
import { zodResolver } from '@hookform/resolvers/zod'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { useDialogState } from '@/hooks/use-dialog'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { MarkdownEditorDialog } from '../components/markdown-editor-dialog'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsFormGridItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { isValidTaskPublicAddress } from './task-public-address'

const _systemInfoSchema = z.object({
  SystemName: z.string().min(1),
  ServerAddress: z.string().optional(),
  TaskPublicAddress: z.string().refine(isValidTaskPublicAddress),
  Logo: z.string().url().optional().or(z.literal('')),
  Footer: z.string().optional(),
  About: z.string().optional(),
  HomePageContent: z.string().optional(),
  FeedbackContent: z.string().optional(),
  general_setting: z.object({ docs_link: z.string() }),
  legal: z.object({
    user_agreement: z.string().optional(),
    privacy_policy: z.string().optional(),
  }),
})

type SystemInfoFormValues = z.infer<typeof _systemInfoSchema>

type SystemInfoSectionProps = {
  defaultValues: SystemInfoFormValues
}

function normalizeValue(value: unknown): string {
  if (value === undefined || value === null) return ''
  return typeof value === 'string' ? value : String(value)
}

type MarkdownFieldMeta = {
  label: string
  placeholder: string
  description: string
  span?: 'full'
}

type MarkdownFieldName =
  | 'Footer'
  | 'About'
  | 'HomePageContent'
  | 'FeedbackContent'
  | 'legal.user_agreement'
  | 'legal.privacy_policy'

const MARKDOWN_FIELDS: Record<MarkdownFieldName, MarkdownFieldMeta> = {
  Footer: {
    label: 'Footer',
    placeholder: '© 2025 Your Company. All rights reserved.',
    description: 'Footer text displayed at the bottom of pages',
  },
  About: {
    label: 'About',
    placeholder:
      'Enter HTML code (e.g., <p>About us...</p>) or a URL (e.g., https://example.com) to embed as iframe',
    description:
      'Supports HTML markup or iframe embedding. Enter HTML code directly, or provide a complete URL to automatically embed it as an iframe.',
  },
  HomePageContent: {
    label: 'Home Page Content',
    placeholder: 'Welcome to our New API...',
    description: 'Content displayed on the home page (supports Markdown)',
    span: 'full',
  },
  FeedbackContent: {
    label: 'Feedback Page Content',
    placeholder:
      'Provide Markdown, HTML, or an external URL for the feedback page',
    description:
      'Content displayed on the feedback page. Supports Markdown, HTML, or a full URL.',
    span: 'full',
  },
  'legal.user_agreement': {
    label: 'User Agreement',
    placeholder:
      'Provide Markdown, HTML, or an external URL for the user agreement',
    description:
      'Leave empty to disable the agreement requirement. Supports Markdown, HTML, or a full URL to redirect users.',
  },
  'legal.privacy_policy': {
    label: 'Privacy Policy',
    placeholder:
      'Provide Markdown, HTML, or an external URL for the privacy policy',
    description:
      'Leave empty to disable the privacy policy requirement. Supports Markdown, HTML, or a full URL to redirect users.',
  },
}

export function SystemInfoSection({ defaultValues }: SystemInfoSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [editingField, setEditingField, editingFieldState] =
    useDialogState<MarkdownFieldName>()

  const normalizedDefaults: SystemInfoFormValues = {
    SystemName: normalizeValue(defaultValues.SystemName),
    ServerAddress: normalizeValue(defaultValues.ServerAddress),
    TaskPublicAddress: normalizeValue(defaultValues.TaskPublicAddress),
    Logo: normalizeValue(defaultValues.Logo),
    Footer: normalizeValue(defaultValues.Footer),
    About: normalizeValue(defaultValues.About),
    HomePageContent: normalizeValue(defaultValues.HomePageContent),
    FeedbackContent: normalizeValue(defaultValues.FeedbackContent),
    general_setting: {
      docs_link: normalizeValue(defaultValues.general_setting?.docs_link),
    },
    legal: {
      user_agreement: normalizeValue(defaultValues.legal?.user_agreement),
      privacy_policy: normalizeValue(defaultValues.legal?.privacy_policy),
    },
  }

  const systemInfoSchemaWithI18n = z.object({
    SystemName: z.string().min(1, {
      error: () => t('System name is required'),
    }),
    ServerAddress: z.string().optional(),
    TaskPublicAddress: z.string().refine(isValidTaskPublicAddress, {
      error: () =>
        t(
          'Enter an absolute HTTP(S) URL without credentials, query parameters, or fragments'
        ),
    }),
    Logo: z.string().url().optional().or(z.literal('')),
    Footer: z.string().optional(),
    About: z.string().optional(),
    HomePageContent: z.string().optional(),
    FeedbackContent: z.string().optional(),
    general_setting: z.object({ docs_link: z.string() }),
    legal: z.object({
      user_agreement: z.string().optional(),
      privacy_policy: z.string().optional(),
    }),
  })

  const { form, handleSubmit, handleReset, isDirty, isSubmitting } =
    useSettingsForm<SystemInfoFormValues>({
      resolver: zodResolver(systemInfoSchemaWithI18n) as Resolver<
        SystemInfoFormValues,
        unknown,
        SystemInfoFormValues
      >,
      defaultValues: normalizedDefaults,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          let v = normalizeValue(value)
          if (key === 'ServerAddress' || key === 'TaskPublicAddress') {
            v = v.replace(/\/+$/, '')
          }
          await updateOption.mutateAsync({
            key,
            value: v,
          })
        }
      },
    })

  const getMarkdownFieldValue = (field: MarkdownFieldName) => {
    switch (field) {
      case 'Footer':
      case 'About':
      case 'HomePageContent':
      case 'FeedbackContent':
        return form.getValues(field) ?? ''
      case 'legal.user_agreement':
        return form.getValues('legal.user_agreement') ?? ''
      case 'legal.privacy_policy':
        return form.getValues('legal.privacy_policy') ?? ''
    }
  }

  const setMarkdownFieldValue = (field: MarkdownFieldName, value: string) => {
    form.setValue(field, value, {
      shouldDirty: true,
      shouldTouch: true,
      shouldValidate: true,
    })
  }

  const renderMarkdownField = (name: MarkdownFieldName) => {
    const meta = MARKDOWN_FIELDS[name]
    const content = (
      <FormField
        control={form.control}
        name={name}
        render={({ field }) => {
          const value = normalizeValue(field.value)
          return (
            <FormItem>
              <FormLabel>{t(meta.label)}</FormLabel>
              <FormControl>
                <button
                  type='button'
                  className='border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex max-h-40 min-h-24 w-full items-start overflow-y-auto rounded-md border px-3 py-2 text-left text-sm transition-colors focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50'
                  onClick={() => setEditingField(name)}
                >
                  {value ? (
                    <span className='block min-w-0 break-words whitespace-pre-wrap'>
                      {value}
                    </span>
                  ) : (
                    <span className='text-muted-foreground'>
                      {t('Click to edit...')}
                    </span>
                  )}
                </button>
              </FormControl>
              <FormDescription>{t(meta.description)}</FormDescription>
              <FormMessage />
            </FormItem>
          )
        }}
      />
    )

    if (meta.span === 'full') {
      return (
        <SettingsFormGridItem key={name} span='full'>
          {content}
        </SettingsFormGridItem>
      )
    }

    return <div key={name}>{content}</div>
  }

  const editingMeta = editingField ? MARKDOWN_FIELDS[editingField] : null

  return (
    <>
      <FormNavigationGuard when={isDirty} />

      <SettingsSection title={t('System Information')}>
        <Form {...form}>
          <SettingsForm onSubmit={handleSubmit}>
            <SettingsPageFormActions
              onSave={handleSubmit}
              onReset={handleReset}
              isSaving={isSubmitting || updateOption.isPending}
              isResetDisabled={!isDirty}
            />
            <FormDirtyIndicator isDirty={isDirty} />
            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='SystemName'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('System Name')}</FormLabel>
                    <FormControl>
                      <Input placeholder={t('New API')} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t('The name displayed across the application')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='ServerAddress'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Server Address')}</FormLabel>
                    <FormControl>
                      <Input placeholder='https://yourdomain.com' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'The public URL of your server, used for OAuth callbacks, webhooks, and other external integrations'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='TaskPublicAddress'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Async Task Public Address')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder='https://media.example.com/tasks'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Public base URL for async task media. Supports a dedicated media domain, port, or Nginx path prefix; falls back to Server Address when empty.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='Logo'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Logo URL')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('https://example.com/logo.png')}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('URL to your logo image (optional)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='general_setting.docs_link'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Documentation Link')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder='https://docs.example.com'
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {renderMarkdownField('Footer')}
              {renderMarkdownField('About')}
              {renderMarkdownField('HomePageContent')}
              {renderMarkdownField('FeedbackContent')}
              {renderMarkdownField('legal.user_agreement')}
              {renderMarkdownField('legal.privacy_policy')}
            </SettingsFormGrid>
          </SettingsForm>
        </Form>
      </SettingsSection>

      {editingField && editingMeta ? (
        <MarkdownEditorDialog
          key={editingField}
          open={editingFieldState.isOpen}
          onOpenChange={(open) => {
            if (!open) {
              editingFieldState.reset()
            }
          }}
          title={t(editingMeta.label)}
          description={t(editingMeta.description)}
          value={getMarkdownFieldValue(editingField)}
          onSave={(value) => setMarkdownFieldValue(editingField, value)}
          placeholder={t(editingMeta.placeholder)}
        />
      ) : null}
    </>
  )
}
