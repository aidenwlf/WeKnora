import assert from 'node:assert/strict'
import { after, before, beforeEach, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { createServer, type ViteDevServer } from 'vite'
import { createPinia } from 'pinia'
import type { SessionLastRequestStatePayload } from './settings'

let server: ViteDevServer
let useSettingsStore: typeof import('./settings').useSettingsStore
const savedStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
const items = new Map<string, string>()

before(async () => {
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => items.get(key) ?? null,
    setItem: (key: string, value: string) => items.set(key, value),
    removeItem: (key: string) => items.delete(key),
  } })
  server = await createServer({
    configFile: false,
    plugins: [{ name: 'offline-store-test', enforce: 'pre',
      resolveId(id) { if (id.endsWith('/utils/request')) return '\0offline-request' },
      load(id) { if (id === '\0offline-request') return 'const request = () => { throw new Error("Unexpected network request") }; export { request as get, request as post, request as put, request as del };' },
    }],
    optimizeDeps: { noDiscovery: true, entries: [] },
    resolve: { alias: { '@': fileURLToPath(new URL('../', import.meta.url)) } },
    server: { middlewareMode: true, hmr: false }, appType: 'custom',
  })
  ;({ useSettingsStore } = await server.ssrLoadModule('/src/stores/settings.ts'))
})

beforeEach(() => items.clear())

after(async () => {
  await server?.close()
  if (savedStorage) Object.defineProperty(globalThis, 'localStorage', savedStorage)
  else Reflect.deleteProperty(globalThis, 'localStorage')
})

function selectDefaults(store: ReturnType<typeof useSettingsStore>) {
  store.settings.selectedFiles = ['default-file']
  store.settings.selectedFileKbMap = { 'default-file': 'default-kb' }
  store.settings.selectedTags = [{ id: 'default-tag', name: 'Default tag', kbId: 'default-kb' }]
  store.settings.selectedMCPServices = ['default-mcp']
  store.settings.selectedSkills = ['default-skill']
  store.selectKnowledgeBases(['default-kb'])
}

const emptySelections: SessionLastRequestStatePayload = {
  knowledge_base_ids: [], knowledge_ids: [], tag_ids: [], mcp_service_ids: [], skill_names: [],
}

for (const state of [{ agent_enabled: true, knowledge_base_ids: [] }, emptySelections]) {
  test(`session hydration clears stale file, tag, and tool selections when saved lists are ${'knowledge_ids' in state ? 'empty' : 'omitted'}`, () => {
    const store = useSettingsStore(createPinia())
    selectDefaults(store)
    const storedDefaults = items.get('WeKnora_settings')

    store.hydrateSessionInputState(state)

    assert.deepEqual(store.getSelectedKnowledgeBases(), [])
    assert.deepEqual(store.getSelectedFiles(), [])
    assert.deepEqual(store.settings.selectedFileKbMap, {})
    assert.deepEqual(store.settings.selectedTags, [])
    assert.deepEqual(store.settings.selectedMCPServices, [])
    assert.deepEqual(store.settings.selectedSkills, [])
    assert.deepEqual(store.getSuggestedQuestionsParams(), {
      knowledge_base_ids: undefined, knowledge_ids: undefined, tag_scopes: undefined, limit: undefined,
    })
    assert.equal(items.get('WeKnora_settings'), storedDefaults, 'hydration must not overwrite browser defaults')
    store.restoreDefaultsIfSnapshotted()
    assert.deepEqual(JSON.parse(JSON.stringify(store.settings)), JSON.parse(storedDefaults!))
  })
}

test('switching back to a session without mentions clears file and tool selections from browser defaults', () => {
  const store = useSettingsStore(createPinia())
  selectDefaults(store)
  store.hydrateSessionInputState({ knowledge_base_ids: [] })

  // The chat route restores defaults before loading every other session.
  store.restoreDefaultsIfSnapshotted()
  store.hydrateSessionInputState({
    knowledge_ids: ['other-session-file'], mcp_service_ids: ['other-session-mcp'], skill_names: ['other-session-skill'],
  })
  assert.deepEqual(store.getSelectedFiles(), ['other-session-file'])
  assert.deepEqual(store.settings.selectedMCPServices, ['other-session-mcp'])
  assert.deepEqual(store.settings.selectedSkills, ['other-session-skill'])
  store.restoreDefaultsIfSnapshotted()
  store.hydrateSessionInputState({ knowledge_base_ids: [] })

  assert.deepEqual(store.getSelectedFiles(), [])
  assert.deepEqual(store.settings.selectedTags, [])
  assert.deepEqual(store.settings.selectedMCPServices, [])
  assert.deepEqual(store.settings.selectedSkills, [])
  store.restoreDefaultsIfSnapshotted()
  assert.deepEqual(store.getSelectedFiles(), ['default-file'])
  assert.deepEqual(store.settings.selectedTags, [{ id: 'default-tag', name: 'Default tag', kbId: 'default-kb' }])
  assert.deepEqual(store.settings.selectedMCPServices, ['default-mcp'])
  assert.deepEqual(store.settings.selectedSkills, ['default-skill'])
})

test('session hydration copies explicit selections and retains only selected files in the KB map', () => {
  const store = useSettingsStore(createPinia())
  selectDefaults(store)
  const state = {
    knowledge_base_ids: ['session-kb'], knowledge_ids: ['session-file', 'default-file'],
    mcp_service_ids: ['session-mcp'], skill_names: ['session-skill'],
  }
  store.settings.selectedFileKbMap['unselected-file'] = 'another-kb'

  store.hydrateSessionInputState(state)
  assert.deepEqual(store.getSelectedKnowledgeBases(), ['session-kb'])
  assert.deepEqual(store.getSelectedFiles(), ['session-file', 'default-file'])
  assert.deepEqual(store.settings.selectedFileKbMap, { 'default-file': 'default-kb' })
  assert.deepEqual(store.settings.selectedMCPServices, ['session-mcp'])
  assert.deepEqual(store.settings.selectedSkills, ['session-skill'])
  store.settings.selectedKnowledgeBases.push('another-kb')
  store.settings.selectedFiles.push('another-file')
  store.settings.selectedMCPServices.push('another-mcp')
  store.settings.selectedSkills.push('another-skill')

  assert.deepEqual(state.knowledge_base_ids, ['session-kb'])
  assert.deepEqual(state.knowledge_ids, ['session-file', 'default-file'])
  assert.deepEqual(state.mcp_service_ids, ['session-mcp'])
  assert.deepEqual(state.skill_names, ['session-skill'])
  store.restoreDefaultsIfSnapshotted()
  assert.deepEqual(store.getSelectedKnowledgeBases(), ['default-kb'])
})

test('session hydration restores tags and tool selections from mentions when ID lists are omitted', () => {
  const store = useSettingsStore(createPinia())
  selectDefaults(store)
  store.hydrateSessionInputState({
    knowledge_base_ids: ['session-kb'],
    mentioned_items: [
      { id: 'tag', type: 'tag', name: 'Saved tag', kb_id: 'session-kb', kb_name: 'Saved KB' },
      { id: 'mcp', type: 'mcp' },
      { id: 'skill-id', type: 'skill', skill_name: 'saved-skill' },
    ],
  })

  assert.deepEqual(store.settings.selectedTags, [{ id: 'tag', name: 'Saved tag', kbId: 'session-kb', kbName: 'Saved KB' }])
  assert.deepEqual(store.settings.selectedMCPServices, ['mcp'])
  assert.deepEqual(store.settings.selectedSkills, ['saved-skill'])
})

test('explicit empty ID lists override stale tag and tool mentions', () => {
  const store = useSettingsStore(createPinia())
  selectDefaults(store)
  store.hydrateSessionInputState({
    ...emptySelections,
    mentioned_items: [
      { id: 'tag', type: 'tag', kb_id: 'old-kb' },
      { id: 'mcp', type: 'mcp' },
      { id: 'skill', type: 'skill' },
    ],
  })

  assert.deepEqual(store.settings.selectedTags, [])
  assert.deepEqual(store.settings.selectedMCPServices, [])
  assert.deepEqual(store.settings.selectedSkills, [])
})

test('explicit tag IDs filter mentions and preserve the single-KB fallback for missing metadata', () => {
  const store = useSettingsStore(createPinia())
  store.hydrateSessionInputState({
    knowledge_base_ids: ['session-kb'], tag_ids: ['selected-tag', 'orphan-tag'],
    mentioned_items: [
      { id: 'selected-tag', type: 'tag', name: 'Saved tag', kb_id: 'session-kb' },
      { id: 'unselected-tag', type: 'tag', kb_id: 'other-kb' },
    ],
  })

  assert.deepEqual(store.settings.selectedTags, [
    { id: 'selected-tag', name: 'Saved tag', kbId: 'session-kb', kbName: undefined },
    { id: 'orphan-tag', name: 'orphan-tag', kbId: 'session-kb', kbName: undefined },
  ])
})

test('missing session state and first-send hydration preserve the current draft selections', () => {
  const store = useSettingsStore(createPinia())
  selectDefaults(store)
  const draft = JSON.stringify(store.settings)

  store.hydrateSessionInputState(null)
  store.hydrateSessionInputState(undefined)
  store.hydrateSessionInputState({ agent_enabled: true }, true)

  assert.equal(JSON.stringify(store.settings), draft)
  assert.equal(store._defaultsSnapshot, null)
})

test('each conversation keeps its own composer state and leaves global defaults untouched', () => {
  const store = useSettingsStore(createPinia())
  selectDefaults(store)
  const globalBefore = items.get('WeKnora_settings')

  store.hydrateSessionInputState({
    agent_id: 'agent-s1',
    agent_enabled: true,
    model_id: 'model-s1',
    knowledge_base_ids: ['s1-kb'],
    reasoning_effort: 'high',
  }, false, 's1')
  store.selectKnowledgeBases(['s1-kb-2'])
  store.toggleWebSearch(true)
  store.updateConversationModels({
    selectedChatModelId: 'model-s1-2',
    summaryModelId: 'model-s1-2',
    rerankModelId: '',
  })
  store.reasoningEffortOverride = 'max'
  store.persistSettings()

  assert.equal(items.get('WeKnora_settings'), globalBefore, 'session edits must not rewrite global defaults')
  assert.deepEqual(JSON.parse(items.get('WeKnora_settings_session:s1')!).selectedKnowledgeBases, ['s1-kb-2'])

  store.restoreDefaultsIfSnapshotted()
  assert.deepEqual(store.getSelectedKnowledgeBases(), ['default-kb'])
  assert.equal(store.conversationModels.selectedChatModelId, '')

  store.hydrateSessionInputState({ knowledge_base_ids: ['s2-server'] }, false, 's2')
  assert.deepEqual(store.getSelectedKnowledgeBases(), ['s2-server'])
  store.restoreDefaultsIfSnapshotted()

  // Re-entering s1 prefers its local snapshot over the older server state.
  store.hydrateSessionInputState({ knowledge_base_ids: ['server-old'] }, false, 's1')
  assert.deepEqual(store.getSelectedKnowledgeBases(), ['s1-kb-2'])
  assert.equal(store.conversationModels.selectedChatModelId, 'model-s1-2')
  assert.equal(store.reasoningEffortOverride, 'max')
  assert.equal(store.isWebSearchEnabled, true)
  assert.equal(items.get('WeKnora_settings'), globalBefore)
})

test('first-send draft is stored in the new session instead of the older server state', () => {
  const store = useSettingsStore(createPinia())
  selectDefaults(store)
  store.settings.selectedKnowledgeBases = ['draft-kb']

  store.hydrateSessionInputState({ knowledge_base_ids: ['server-kb'] }, true, 'new-session')

  assert.deepEqual(store.getSelectedKnowledgeBases(), ['draft-kb'])
  assert.deepEqual(
    JSON.parse(items.get('WeKnora_settings_session:new-session')!).selectedKnowledgeBases,
    ['draft-kb'],
  )
  store.restoreDefaultsIfSnapshotted()
  // A new chat inherits the global defaults it was created from, including
  // the draft that seeded it.
  assert.deepEqual(store.getSelectedKnowledgeBases(), ['draft-kb'])
})
