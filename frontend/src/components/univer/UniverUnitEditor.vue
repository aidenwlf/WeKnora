<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import {
  IAuthzIoService,
  IImageIoService,
  IMentionIOService,
  IUndoRedoService,
  LocaleType,
  LogLevel,
  Univer,
  mergeLocales,
} from '@univerjs/core'
import { FUniver } from '@univerjs/core/facade'
import { UniverCollaborationPlugin } from '@univerjs-pro/collaboration'
import {
  IAttachmentIoService,
  UniverCollaborationClientPlugin,
} from '@univerjs-pro/collaboration-client'
import CollaborationClientEnUS from '@univerjs-pro/collaboration-client/locale/en-US'
import CollaborationClientZhCN from '@univerjs-pro/collaboration-client/locale/zh-CN'
import {
  BrowserCollaborationSocketService,
  UniverCollaborationClientUIPlugin,
} from '@univerjs-pro/collaboration-client-ui'
import CollaborationClientUIEnUS from '@univerjs-pro/collaboration-client-ui/locale/en-US'
import CollaborationClientUIZhCN from '@univerjs-pro/collaboration-client-ui/locale/zh-CN'
import { UniverLicensePlugin } from '@univerjs-pro/license'
import '@univerjs-pro/collaboration-client/facade'
import '@univerjs-pro/collaboration-client-ui/lib/index.css'
import i18n from '@/i18n'
import type { UniverKind } from '@/api/univer'

const props = defineProps<{
  unitId: string
  kind: UniverKind
  basePath: string
}>()

const emit = defineEmits<{
  (e: 'error', message: string): void
  (e: 'ready'): void
}>()

const host = ref<HTMLElement | null>(null)
const loading = ref(true)
let univerInstance: { dispose: () => void } | null = null

/** Chinese UI for zh-* app locales; everything else falls back to English. */
function isChinese(): boolean {
  const raw = (i18n.global.locale as unknown as { value?: string }).value ?? String(i18n.global.locale)
  return (raw || '').toLowerCase().startsWith('zh')
}

function localeType(): LocaleType {
  return isChinese() ? LocaleType.ZH_CN : LocaleType.EN_US
}

/** Collaboration client wiring shared by every product. */
function collaborationPluginConfig() {
  const origin = window.location.origin
  const base = `${origin}${props.basePath}`
  const wsProtocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return [
    UniverCollaborationClientPlugin,
    {
      socketService: BrowserCollaborationSocketService,
      sendChangesetTimeout: 200,
      authzUrl: `${base}/authz`,
      snapshotServerUrl: `${base}/snapshot`,
      collabSubmitChangesetUrl: `${base}/comb`,
      collabWebSocketUrl: `${wsProtocol}://${window.location.host}${props.basePath}/comb/connect`,
      wsSessionTicketUrl: `${base}/user/session-ticket`,
    },
  ] as const
}

function collaborationLocales(): Record<string, unknown> {
  return isChinese()
    ? mergeLocales(CollaborationClientZhCN, CollaborationClientUIZhCN)
    : mergeLocales(CollaborationClientEnUS, CollaborationClientUIEnUS)
}

/** Preset-mode products: Sheets and Docs. */
async function initPreset(kind: 'sheet' | 'doc') {
  const { createUniver, defaultTheme } = await import('@univerjs/presets')
  const locales: Record<string, unknown> = { ...collaborationLocales() }
  const presets = []

  if (kind === 'sheet') {
    const [{ UniverSheetsCorePreset }, zh, en] = await Promise.all([
      import('@univerjs/preset-sheets-core'),
      import('@univerjs/preset-sheets-core/locales/zh-CN'),
      import('@univerjs/preset-sheets-core/locales/en-US'),
      import('@univerjs/preset-sheets-core/lib/index.css'),
    ])
    Object.assign(locales, mergeLocales(isChinese() ? zh.default : en.default))
    presets.push(UniverSheetsCorePreset({ container: host.value as HTMLElement }))
  } else {
    const [{ UniverDocsCorePreset }, zh, en] = await Promise.all([
      import('@univerjs/preset-docs-core'),
      import('@univerjs/preset-docs-core/locales/zh-CN'),
      import('@univerjs/preset-docs-core/locales/en-US'),
      import('@univerjs/preset-docs-core/lib/index.css'),
    ])
    Object.assign(locales, mergeLocales(isChinese() ? zh.default : en.default))
    presets.push(UniverDocsCorePreset({ container: host.value as HTMLElement }))
  }

  const { univer, univerAPI } = createUniver({
    locale: localeType(),
    locales: { [localeType()]: locales as never },
    theme: defaultTheme,
    logLevel: LogLevel.WARN,
    collaboration: true,
    presets,
    plugins: [
      [UniverLicensePlugin, { license: import.meta.env.VITE_UNIVER_LICENSE || undefined }],
      UniverCollaborationPlugin,
      collaborationPluginConfig(),
      UniverCollaborationClientUIPlugin,
    ] as never,
  })
  return { univer, univerAPI }
}

/**
 * Plugin-mode products (Slides, Bases).
 *
 * Locales are merged before the instance is created, and plugins register in
 * Univer's documented order: license, collaboration core, render engine, the
 * unit's own plugins, then the collaboration client.
 */
async function initPlugin(kind: 'slide' | 'base') {
  const designZh = (await import('@univerjs/design/locale/zh-CN')).default
  const designEn = (await import('@univerjs/design/locale/en-US')).default
  const uiZh = (await import('@univerjs/ui/locale/zh-CN')).default
  const uiEn = (await import('@univerjs/ui/locale/en-US')).default
  await import('@univerjs/design/lib/index.css')
  await import('@univerjs/ui/lib/index.css')

  const { UniverRenderEnginePlugin } = await import('@univerjs/engine-render')
  const { UniverUIPlugin } = await import('@univerjs/ui')

  const locales: Record<string, unknown> = {
    ...mergeLocales(isChinese() ? designZh : designEn, isChinese() ? uiZh : uiEn),
    ...collaborationLocales(),
  }

  let registerUnit: (univer: Univer) => void

  if (kind === 'slide') {
    const { UniverDocsPlugin } = await import('@univerjs/docs')
    const { UniverDocsUIPlugin } = await import('@univerjs/docs-ui')
    const docsUIZh = (await import('@univerjs/docs-ui/locale/zh-CN')).default
    const docsUIEn = (await import('@univerjs/docs-ui/locale/en-US')).default
    await import('@univerjs/docs-ui/lib/index.css')
    const { UniverDrawingPlugin } = await import('@univerjs/drawing')
    const [{ UniverSlidesPlugin }, { UniverSlidesUIPlugin }] = await Promise.all([
      import('@univerjs-pro/slides'),
      import('@univerjs-pro/slides-ui'),
    ])
    const slidesZh = (await import('@univerjs-pro/slides/locale/zh-CN')).default
    const slidesEn = (await import('@univerjs-pro/slides/locale/en-US')).default
    const slidesUIZh = (await import('@univerjs-pro/slides-ui/locale/zh-CN')).default
    const slidesUIEn = (await import('@univerjs-pro/slides-ui/locale/en-US')).default
    await import('@univerjs-pro/slides-ui/lib/index.css')
    await Promise.all([
      import('@univerjs/core/facade'),
      import('@univerjs/docs/facade'),
      import('@univerjs/docs-ui/facade'),
      import('@univerjs-pro/slides/facade'),
    ])
    Object.assign(
      locales,
      mergeLocales(
        isChinese() ? docsUIZh : docsUIEn,
        isChinese() ? slidesZh : slidesEn,
        isChinese() ? slidesUIZh : slidesUIEn,
      ),
    )
    registerUnit = (univer) => {
      univer.registerPlugin(UniverUIPlugin, { container: host.value as HTMLElement })
      univer.registerPlugin(UniverDocsPlugin)
      univer.registerPlugin(UniverDocsUIPlugin)
      univer.registerPlugin(UniverDrawingPlugin, {
        override: [[IImageIoService, null]],
      } as never)
      univer.registerPlugin(UniverSlidesPlugin)
      univer.registerPlugin(UniverSlidesUIPlugin)
    }
  } else {
    const { UniverProFormulaEnginePlugin } = await import('@univerjs-pro/engine-formula')
    const [{ UniverBasesPlugin }, { UniverBasesUIPlugin }] = await Promise.all([
      import('@univerjs-pro/bases'),
      import('@univerjs-pro/bases-ui'),
    ])
    const basesZh = (await import('@univerjs-pro/bases/locale/zh-CN')).default
    const basesEn = (await import('@univerjs-pro/bases/locale/en-US')).default
    const basesUIZh = (await import('@univerjs-pro/bases-ui/locale/zh-CN')).default
    const basesUIEn = (await import('@univerjs-pro/bases-ui/locale/en-US')).default
    await import('@univerjs-pro/bases-ui/lib/index.css')
    await Promise.all([
      import('@univerjs/core/facade'),
      import('@univerjs-pro/bases/facade'),
      import('@univerjs-pro/bases-ui/facade'),
    ])
    Object.assign(
      locales,
      mergeLocales(
        isChinese() ? basesZh : basesEn,
        isChinese() ? basesUIZh : basesUIEn,
      ),
    )
    registerUnit = (univer) => {
      univer.registerPlugin(UniverUIPlugin, { container: host.value as HTMLElement })
      univer.registerPlugin(UniverProFormulaEnginePlugin)
      univer.registerPlugin(UniverBasesPlugin)
      // The collaboration client owns attachment I/O; the UI's own
      // registration would be a duplicate.
      univer.registerPlugin(UniverBasesUIPlugin, {
        override: [[IAttachmentIoService, null]],
      } as never)
    }
  }

  const univer = new Univer({
    locale: localeType(),
    locales: { [localeType()]: locales as never },
    logLevel: LogLevel.WARN,
    // The collaboration client provides these services; leaving the base
    // registrations in place makes Univer reject the duplicate.
    override: [[IUndoRedoService, null], [IAuthzIoService, null], [IMentionIOService, null]],
  })

  univer.registerPlugin(UniverLicensePlugin, {
    license: import.meta.env.VITE_UNIVER_LICENSE || undefined,
  })
  univer.registerPlugin(UniverCollaborationPlugin)
  univer.registerPlugin(UniverRenderEnginePlugin)
  registerUnit(univer)

  const [collabClient, collabConfig] = collaborationPluginConfig()
  univer.registerPlugin(collabClient as never, collabConfig as never)
  univer.registerPlugin(UniverCollaborationClientUIPlugin)
  const univerAPI = FUniver.newAPI(univer)
  return { univer, univerAPI }
}

onMounted(async () => {
  if (!host.value) return
  try {
    const { univer, univerAPI } =
      props.kind === 'sheet' || props.kind === 'doc'
        ? await initPreset(props.kind)
        : await initPlugin(props.kind)
    univerInstance = univer

    const collab = univerAPI.getCollaboration()
    const unit =
      props.kind === 'sheet'
        ? await collab.loadSheetAsync(props.unitId)
        : props.kind === 'doc'
          ? await collab.loadDocAsync(props.unitId)
          : props.kind === 'slide'
            ? await collab.loadSlideAsync(props.unitId)
            : await collab.loadBaseAsync(props.unitId)
    if (!unit) throw new Error('unit could not be loaded')
    loading.value = false
    emit('ready')
  } catch (err) {
    loading.value = false
    emit('error', err instanceof Error ? err.message : String(err))
  }
})

onBeforeUnmount(() => {
  try {
    univerInstance?.dispose()
  } catch {
    // Disposing an already-torn-down canvas is harmless.
  }
  univerInstance = null
})
</script>

<template>
  <div class="univer-editor-host">
    <div ref="host" class="univer-editor-canvas" />
    <div v-if="loading" class="univer-editor-loading">
      <t-loading :text="$t('univer.loading')" size="small" />
    </div>
  </div>
</template>

<style scoped lang="less">
.univer-editor-host {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 420px;
}

.univer-editor-canvas {
  width: 100%;
  height: 100%;
}

.univer-editor-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--td-bg-color-container);
}
</style>
