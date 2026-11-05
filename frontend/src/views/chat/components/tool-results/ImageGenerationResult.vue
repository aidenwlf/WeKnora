<template>
  <div class="image-generation-result">
    <div v-if="images.length" ref="gridRef" class="image-generation-grid">
      <button v-for="(image, index) in images" :key="image.url" type="button" class="image-generation-tile"
        :style="tileStyle(image)" :title="$t('agentStream.imageGeneration.open')"
        :aria-label="$t('agentStream.imageGeneration.imageLabel', { n: index + 1 })" @click.stop="openPreview">
        <img :src="PLACEHOLDER" :data-protected-src="image.url" data-img-loading="1"
          :alt="$t('agentStream.imageGeneration.imageLabel', { n: index + 1 })">
      </button>
    </div>
    <p v-else class="image-generation-empty">{{ $t('agentStream.imageGeneration.empty') }}</p>

    <dl class="image-generation-fields">
      <div v-if="prompt" class="image-generation-field">
        <dt>{{ $t('agentStream.imageGeneration.prompt') }}</dt>
        <dd>{{ prompt }}</dd>
      </div>
      <div v-if="revisedPrompt" class="image-generation-field">
        <dt>{{ $t('agentStream.imageGeneration.revisedPrompt') }}</dt>
        <dd>{{ revisedPrompt }}</dd>
      </div>
    </dl>
    <p v-if="meta.length" class="image-generation-meta">{{ meta.join(' · ') }}</p>

    <picturePreview v-if="previewUrl" :reviewImg="true" :reviewUrl="previewUrl" @closePreImg="previewUrl = ''" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import picturePreview from '@/components/picture-preview.vue'
import { useProtectedImageRecovery } from '@/composables/useProtectedImageRecovery'
import type { GeneratedImage, ImageGenerationData } from '@/types/tool-results'
import type { ProtectedFileAccessContext } from '@/utils/protectedFileAccess'
import { hydrateProtectedFileImages } from '@/utils/security'

const props = defineProps<{
  data: ImageGenerationData | Record<string, unknown>
  /** Authorization scope of the message that owns the images. */
  access?: ProtectedFileAccessContext
  /** True once the assistant message is persisted and its files are readable. */
  ready?: boolean
}>()

const { t } = useI18n()

// Shown until hydration swaps in the authorized blob URL; a resource:// src
// would render as a broken image.
const PLACEHOLDER = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw=='

const record = computed(() => (props.data || {}) as ImageGenerationData)

const images = computed(() =>
  (Array.isArray(record.value.images) ? record.value.images : [])
    .filter((image) => typeof image?.url === 'string' && image.url),
)

const prompt = computed(() => String(record.value.prompt || '').trim())

const revisedPrompt = computed(() => {
  const revised = String(record.value.revised_prompt || '').trim()
  return revised && revised !== prompt.value ? revised : ''
})

const meta = computed(() => {
  const inputCount = Number(record.value.input_count) || 0
  return [
    String(record.value.model || ''),
    String(record.value.size || ''),
    inputCount > 0 ? t('agentStream.imageGeneration.inputImages', { count: inputCount }) : '',
  ].filter(Boolean)
})

const gridRef = ref<HTMLElement | null>(null)
const previewUrl = ref('')

// The card can mount after the timeline's own hydration pass (expanding a step),
// so it loads its images itself and retries once the message is persisted.
watch(
  () => [images.value.map((image) => image.url).join('\n'), JSON.stringify(props.access ?? null)],
  () => { void nextTick(() => hydrateProtectedFileImages(gridRef.value, props.access)) },
  { immediate: true },
)
useProtectedImageRecovery(() => gridRef.value, () => props.access, () => props.ready === true)

function openPreview(event: MouseEvent) {
  const img = (event.currentTarget as HTMLElement).querySelector('img')
  if (img?.src.startsWith('blob:')) previewUrl.value = img.src
}

function tileStyle(image: GeneratedImage) {
  const width = Number(image.width) || 0
  const height = Number(image.height) || 0
  return width > 0 && height > 0 ? { aspectRatio: `${width} / ${height}` } : undefined
}
</script>

<style lang="less" scoped>
.image-generation-result {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  font-size: var(--app-text-sm);
  line-height: 1.55;
}

.image-generation-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 200px));
  gap: 8px;
}

.image-generation-tile {
  display: block;
  aspect-ratio: 1 / 1;
  padding: 0;
  overflow: hidden;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-sm);
  background: var(--td-bg-color-secondarycontainer);
  cursor: zoom-in;

  &:focus-visible {
    outline: 2px solid var(--td-brand-color);
    outline-offset: 1px;
  }

  img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  &:has(img[data-img-loading]) {
    cursor: progress;
    animation: image-generation-pulse 1.6s ease-in-out infinite;
  }

  // Hydration hides an image the server reports missing; stop asking to wait.
  &:has(img[data-protected-hidden]) {
    cursor: default;
    animation: none;
  }
}

@keyframes image-generation-pulse {
  50% { opacity: 0.55; }
}

@media (prefers-reduced-motion: reduce) {
  .image-generation-tile:has(img[data-img-loading]) {
    animation: none;
  }
}

.image-generation-empty,
.image-generation-meta {
  margin: 0;
  color: var(--td-text-color-secondary);
}

.image-generation-fields {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0;
}

.image-generation-field {
  dt {
    font-weight: 500;
    color: var(--td-text-color-secondary);
  }

  dd {
    margin: 0;
    color: var(--td-text-color-primary);
    white-space: pre-wrap;
    word-break: break-word;
  }
}
</style>
