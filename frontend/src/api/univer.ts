import { get, post, del } from '../utils/request'

/** Collaborative entry kinds inside a knowledge base of type "univer". */
export type UniverKind = 'sheet' | 'doc' | 'base' | 'slide'

export interface UniverUnit {
  id: string
  tenant_id: number
  knowledge_base_id: string
  name: string
  kind: UniverKind
  unit_id: string
  creator_id: string
  created_at: string
  updated_at: string
}

export interface UniverStatus {
  enabled: boolean
  base_path?: string
  kinds?: UniverKind[]
}

export interface UniverSession {
  id: string
  name: string
  kind: UniverKind
  unit_id: string
  base_path: string
  knowledge_base_id: string
}

/** WeKnora API envelope. */
interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data: T
}

export function getUniverStatus() {
  return get<ApiEnvelope<UniverStatus>>('/api/v1/univer/status')
}

export function listUniverUnits(knowledgeBaseId: string) {
  return get<ApiEnvelope<UniverUnit[]>>(
    `/api/v1/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/univer/units`,
  )
}

export function createUniverUnit(knowledgeBaseId: string, name: string, kind: UniverKind) {
  return post<ApiEnvelope<UniverUnit>>(
    `/api/v1/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/univer/units`,
    { name, kind },
  )
}

export function getUniverUnit(knowledgeBaseId: string, id: string) {
  return get<ApiEnvelope<UniverUnit>>(
    `/api/v1/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/univer/units/${encodeURIComponent(id)}`,
  )
}

export function deleteUniverUnit(knowledgeBaseId: string, id: string) {
  return del(
    `/api/v1/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/univer/units/${encodeURIComponent(id)}`,
  )
}

/**
 * Mints the collaboration identity cookie and returns the unit to open.
 * The cookie is scoped to the collaboration base path, so it never rides along
 * on ordinary API calls.
 */
export function openUniverUnitSession(knowledgeBaseId: string, id: string) {
  return post<ApiEnvelope<UniverSession>>(
    `/api/v1/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/univer/units/${encodeURIComponent(id)}/session`,
    {},
  )
}
