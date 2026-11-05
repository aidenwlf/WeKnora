<template>
  <div class="image-grid">
    <figure v-for="(image, index) in images" :key="index" class="image-grid__item">
      <button type="button" class="image-grid__thumb" :aria-label="$t('modelSettings.debug.previewImage')"
        @click="previewUrl = image.data_uri">
        <img :src="image.data_uri" :alt="image.revised_prompt || ''">
      </button>
      <figcaption v-if="image.revised_prompt" class="image-grid__caption">
        <span class="image-grid__caption-label">{{ $t('modelSettings.debug.revisedPrompt') }}</span>
        {{ image.revised_prompt }}
      </figcaption>
    </figure>
    <PicturePreview :review-img="!!previewUrl" :review-url="previewUrl" @close-pre-img="previewUrl = ''" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ModelDebugImage } from '@/api/model'
import PicturePreview from '@/components/picture-preview.vue'

defineProps<{ images: ModelDebugImage[] }>()

const previewUrl = ref('')
</script>

<style scoped lang="less">
.image-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 10px;

  &__item {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }

  &__thumb {
    padding: 0;
    border: 1px solid var(--td-component-stroke);
    border-radius: var(--app-radius-md);
    background: var(--td-bg-color-secondarycontainer);
    overflow: hidden;
    cursor: zoom-in;
    aspect-ratio: 1;

    img {
      display: block;
      width: 100%;
      height: 100%;
      object-fit: contain;
    }

    &:focus-visible {
      outline: 2px solid var(--td-brand-color);
      outline-offset: 2px;
    }
  }

  &__caption {
    font-size: var(--app-text-sm);
    line-height: 1.5;
    color: var(--td-text-color-secondary);
    word-break: break-word;
  }

  &__caption-label {
    display: block;
    color: var(--td-text-color-placeholder);
  }
}
</style>
