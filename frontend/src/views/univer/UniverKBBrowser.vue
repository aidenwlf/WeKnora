<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  createUniverUnit,
  deleteUniverUnit,
  getUniverStatus,
  listUniverUnits,
  type UniverKind,
  type UniverUnit,
} from '@/api/univer'
import UniverUnitDrawer from '@/components/univer/UniverUnitDrawer.vue'

const props = defineProps<{ knowledgeBaseId: string }>()

const enabled = ref(true)
const loading = ref(true)
const units = ref<UniverUnit[]>([])

const createVisible = ref(false)
const creating = ref(false)
const draftName = ref('')
const draftKind = ref<UniverKind>('sheet')

const drawerVisible = ref(false)
const activeUnitId = ref('')

const KIND_META: Record<UniverKind, { icon: string; labelKey: string }> = {
  sheet: { icon: 'chart', labelKey: 'univer.kindSheet' },
  doc: { icon: 'file', labelKey: 'univer.kindDoc' },
  base: { icon: 'table', labelKey: 'univer.kindBase' },
  slide: { icon: 'play-circle', labelKey: 'univer.kindSlide' },
}

const kindOptions = computed(() =>
  (Object.keys(KIND_META) as UniverKind[]).map((kind) => ({
    value: kind,
    label: KIND_META[kind].labelKey,
  })),
)

function kindIcon(kind: UniverKind) {
  return KIND_META[kind]?.icon ?? 'file'
}

function kindLabel(kind: UniverKind) {
  return KIND_META[kind]?.labelKey ?? 'univer.title'
}

async function refresh() {
  loading.value = true
  try {
    const status = await getUniverStatus()
    enabled.value = status.data.enabled
    if (!enabled.value) {
      units.value = []
      return
    }
    const resp = await listUniverUnits(props.knowledgeBaseId)
    units.value = resp.data || []
  } catch (err) {
    MessagePlugin.error(err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

function openUnit(id: string) {
  activeUnitId.value = id
  drawerVisible.value = true
}

async function submitCreate() {
  const name = draftName.value.trim()
  if (!name) return
  creating.value = true
  try {
    const resp = await createUniverUnit(props.knowledgeBaseId, name, draftKind.value)
    createVisible.value = false
    draftName.value = ''
    await refresh()
    openUnit(resp.data.id)
  } catch (err) {
    MessagePlugin.error(err instanceof Error ? err.message : String(err))
  } finally {
    creating.value = false
  }
}

async function removeUnit(id: string) {
  try {
    await deleteUniverUnit(props.knowledgeBaseId, id)
    await refresh()
  } catch (err) {
    MessagePlugin.error(err instanceof Error ? err.message : String(err))
  }
}

watch(() => props.knowledgeBaseId, () => void refresh())

onMounted(() => void refresh())
</script>

<template>
  <div class="univer-kb-browser">
    <header class="univer-kb-browser__header">
      <div>
        <h2>{{ $t('univer.title') }}</h2>
        <p>{{ $t('univer.subtitle') }}</p>
      </div>
      <t-button v-if="enabled" theme="primary" @click="createVisible = true">
        <template #icon><t-icon name="add" /></template>
        {{ $t('univer.create') }}
      </t-button>
    </header>

    <t-alert v-if="!enabled" theme="info" :message="$t('univer.disabled')" />

    <t-loading v-else-if="loading" size="small" :text="$t('univer.loading')" />

    <div v-else-if="units.length === 0" class="univer-kb-browser__empty">
      <t-icon name="chart" size="28px" />
      <p>{{ $t('univer.empty') }}</p>
      <span>{{ $t('univer.emptyHint') }}</span>
    </div>

    <div v-else class="univer-kb-browser__grid">
      <article v-for="unit in units" :key="unit.id" class="univer-unit-card">
        <button class="univer-unit-card__main" type="button" @click="openUnit(unit.id)">
          <span class="univer-unit-card__icon"><t-icon :name="kindIcon(unit.kind)" /></span>
          <span class="univer-unit-card__name">{{ unit.name }}</span>
          <span class="univer-unit-card__kind">{{ $t(kindLabel(unit.kind)) }}</span>
          <span class="univer-unit-card__meta">{{ new Date(unit.updated_at).toLocaleString() }}</span>
        </button>
        <t-popconfirm :content="$t('univer.deleteConfirm')" @confirm="removeUnit(unit.id)">
          <t-button variant="text" shape="square" size="small" class="univer-unit-card__remove">
            <t-icon name="delete" />
          </t-button>
        </t-popconfirm>
      </article>
    </div>

    <UniverUnitDrawer
      v-model:visible="drawerVisible"
      :knowledge-base-id="knowledgeBaseId"
      :unit-id="activeUnitId"
    />

    <t-dialog
      v-model:visible="createVisible"
      :header="$t('univer.create')"
      :confirm-btn="{ content: $t('univer.create'), loading: creating }"
      @confirm="submitCreate"
    >
      <div class="univer-create-form">
        <t-input v-model="draftName" :placeholder="$t('univer.namePlaceholder')" autofocus @enter="submitCreate" />
        <t-radio-group v-model="draftKind" class="univer-create-form__kinds">
          <t-radio-button v-for="option in kindOptions" :key="option.value" :value="option.value">
            {{ $t(option.label) }}
          </t-radio-button>
        </t-radio-group>
      </div>
    </t-dialog>
  </div>
</template>

<style scoped lang="less">
.univer-kb-browser {
  display: flex;
  flex-direction: column;
  gap: 18px;
  height: 100%;
  padding: 20px 24px;
  overflow: auto;
}

.univer-kb-browser__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;

  h2 {
    margin: 0 0 6px;
    font-size: var(--app-text-xl, 20px);
    font-weight: 600;
  }

  p {
    margin: 0;
    color: var(--td-text-color-secondary);
  }
}

.univer-kb-browser__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 56px 0;
  color: var(--td-text-color-secondary);

  span {
    font-size: var(--app-text-sm, 12px);
  }
}

.univer-kb-browser__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
}

.univer-unit-card {
  position: relative;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);

  &:hover {
    border-color: var(--td-brand-color);
  }
}

.univer-unit-card__main {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  width: 100%;
  padding: 16px;
  background: none;
  border: 0;
  cursor: pointer;
  text-align: left;
}

.univer-unit-card__icon {
  color: var(--td-brand-color);
  font-size: 20px;
}

.univer-unit-card__name {
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.univer-unit-card__kind,
.univer-unit-card__meta {
  font-size: var(--app-text-sm, 12px);
  color: var(--td-text-color-placeholder);
}

.univer-unit-card__remove {
  position: absolute;
  top: 8px;
  right: 8px;
}

.univer-create-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.univer-create-form__kinds {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
