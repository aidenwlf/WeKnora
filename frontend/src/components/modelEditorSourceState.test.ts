import assert from 'node:assert/strict'
import test from 'node:test'

import { shouldShowOllamaUnavailableTip, supportsLocalSource } from './modelEditorSourceState.ts'

test('hides Ollama unavailable tip while configuring a remote model', () => {
  assert.equal(shouldShowOllamaUnavailableTip('remote', 'chat', false), false)
})

test('shows Ollama unavailable tip only for types Ollama can serve', () => {
  assert.equal(shouldShowOllamaUnavailableTip('local', 'chat', false), true)
  assert.equal(shouldShowOllamaUnavailableTip('local', 'embedding', false), true)
  assert.equal(shouldShowOllamaUnavailableTip('local', 'rerank', false), false)
  assert.equal(shouldShowOllamaUnavailableTip('local', 'image_generation', false), false)
})

test('rerank and image generation are remote-only', () => {
  for (const type of ['chat', 'embedding', 'vllm', 'asr'] as const) assert.equal(supportsLocalSource(type), true)
  assert.equal(supportsLocalSource('rerank'), false)
  assert.equal(supportsLocalSource('image_generation'), false)
})

test('does not show Ollama unavailable tip before status is known or when Ollama is available', () => {
  assert.equal(shouldShowOllamaUnavailableTip('local', 'chat', null), false)
  assert.equal(shouldShowOllamaUnavailableTip('local', 'chat', true), false)
})
