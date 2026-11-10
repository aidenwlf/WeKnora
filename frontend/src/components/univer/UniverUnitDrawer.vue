<script setup lang="ts">
import { defineAsyncComponent, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { openUniverUnitSession } from '@/api/univer'
import type { UniverKind } from '@/api/univer'

// The editor pulls in the Univer engine, so it only loads the first time an
// entry is actually opened.
const UniverUnitEditor = defineAsyncComponent(() => import('./UniverUnitEditor.vue'))

const props = defineProps<{
  visible: boolean
  knowledgeBaseId: string
  unitId: string
}>()

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
}>()

const session = ref<{
  unit_id: string
  base_path: string
  name: string
  kind: UniverKind
} | null>(null)
const loading = ref(false)
const failed = ref('')

async function load() {
  if (!props.unitId) return
  loading.value = true
  failed.value = ''
  session.value = null
  try {
    const resp = await openUniverUnitSession(props.knowledgeBaseId, props.unitId)
    session.value = resp.data
  } catch (err) {
    failed.value = err instanceof Error ? err.message : String(err)
    MessagePlugin.error(failed.value)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.visible, props.unitId] as const,
  ([visible]) => {
    if (visible) void load()
    else session.value = null
  },
  { immediate: true },
)

function close() {
  emit('update:visible', false)
}
</script>

<template>
  <t-drawer
    :visible="visible"
    size="76%"
    :footer="false"
    :destroy-on-close="true"
    class="univer-unit-drawer"
    @close="close"
  >
    <template #header>
      <div class="univer-unit-drawer__header">
        <t-icon name="chart" />
        <span class="univer-unit-drawer__title">{{ session?.name || $t('univer.title') }}</span>
      </div>
    </template>

    <div class="univer-unit-drawer__body">
      <t-loading v-if="loading" size="small" :text="$t('univer.loading')" />
      <div v-else-if="failed" class="univer-unit-drawer__error">
        <t-icon name="error-circle" />
        <span>{{ $t('univer.loadFailed') }}</span>
        <p>{{ failed }}</p>
      </div>
      <UniverUnitEditor
        v-else-if="session"
        :key="session.unit_id"
        :unit-id="session.unit_id"
        :kind="session.kind"
        :base-path="session.base_path"
        @error="failed = $event"
      />
    </div>
  </t-drawer>
</template>

<style scoped lang="less">
.univer-unit-drawer__header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.univer-unit-drawer__title {
  font-weight: 600;
}

.univer-unit-drawer__body {
  height: 100%;
  min-height: 60vh;
  display: flex;
  flex-direction: column;
}

.univer-unit-drawer__error {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 100%;
  color: var(--td-text-color-secondary);
}

:deep(.t-drawer__body) {
  padding: 0;
  overflow: hidden;
}
</style>
