export type ModelEditorSource = 'local' | 'remote'

export type ModelEditorType = 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'image_generation'

/** Ollama serves neither ReRank nor image generation, so those types are remote-only. */
export function supportsLocalSource(modelType: ModelEditorType): boolean {
  return modelType !== 'rerank' && modelType !== 'image_generation'
}

export function shouldShowOllamaUnavailableTip(
  source: ModelEditorSource,
  modelType: ModelEditorType,
  ollamaServiceStatus: boolean | null,
): boolean {
  return source === 'local' && supportsLocalSource(modelType) && ollamaServiceStatus === false
}
