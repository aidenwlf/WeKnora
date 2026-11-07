import { get, post } from '../utils/request';

/** One chat artifact a member wants to turn into a workspace skill. */
export interface InstallArtifactAsSkillInput {
  session_id: string;
  message_id: string;
  artifact_index: number;
  config_ids: string[];
}

/**
 * Outcome of the request. `installed` is false when the caller cannot write the
 * skill catalog: the server queued the request instead and returned its id.
 */
export interface InstallArtifactAsSkillResult {
  installed: boolean;
  approval_id?: number;
  catalog_id?: string;
  file_name?: string;
}

export interface SkillApprovalRequest {
  id: number;
  tenant_id: number;
  requester_id: string;
  session_id: string;
  message_id: string;
  artifact_index: number;
  file_name: string;
  config_ids: string;
  status: 'pending' | 'approved' | 'rejected';
  approver_id: string;
  reason: string;
  created_at: string;
  updated_at: string;
}

/** Install now when allowed, otherwise queue for an admin. */
export async function installArtifactAsSkill(
  input: InstallArtifactAsSkillInput,
): Promise<InstallArtifactAsSkillResult> {
  const res = await post<{ data: InstallArtifactAsSkillResult }>('/api/v1/skills/install-from-artifact', input);
  return res.data.data;
}

/** Admin queue of pending skill install requests. */
export async function listSkillApprovalRequests(): Promise<SkillApprovalRequest[]> {
  const res = await get<{ data: SkillApprovalRequest[] }>('/api/v1/skills/approval-requests');
  return res.data.data || [];
}

/** Approve a request; the server installs the bundle and closes it. */
export async function approveSkillApprovalRequest(id: number, reason = ''): Promise<void> {
  await post(`/api/v1/skills/approval-requests/${id}/approve`, { reason });
}

/** Reject a request with a required reason. */
export async function rejectSkillApprovalRequest(id: number, reason: string): Promise<void> {
  await post(`/api/v1/skills/approval-requests/${id}/reject`, { reason });
}
