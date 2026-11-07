import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { useChatResourcesStore } from '@/stores/chatResources'
import { installArtifactAsSkill } from '@/api/skillApproval'

/**
 * Turns a chat artifact into a workspace skill.
 *
 * The server decides what happens: a caller who may write the skill catalog
 * gets an install, everyone else gets a queued request an admin decides. This
 * composable only resolves the target sandboxes and surfaces which of the two
 * happened, so both chat renderers share one implementation.
 */
export function useArtifactAsSkill() {
  const { t } = useI18n()
  const resources = useChatResourcesStore()

  async function installFromArtifact(args: {
    sessionId: string
    messageId: string
    index: number
    fileName?: string
  }): Promise<void> {
    if (!args.sessionId || !args.messageId) return
    try {
      await resources.ensureSandboxConfigs()
      const configIDs = (resources.sandboxConfigs || []).map((c) => c.id)
      if (!configIDs.length) {
        MessagePlugin.warning(t('settings.skills.emptyNoSandboxHint'))
        return
      }
      const result = await installArtifactAsSkill({
        session_id: args.sessionId,
        message_id: args.messageId,
        artifact_index: args.index,
        config_ids: configIDs,
      })
      if (result.installed) {
        MessagePlugin.success(
          t('agent.artifactDrawer.addAsSkillStarted', { name: result.file_name || args.fileName || '' }),
        )
        return
      }
      MessagePlugin.info(t('agent.artifactDrawer.addAsSkillPending'))
    } catch (e: any) {
      MessagePlugin.error(e?.message || t('agent.artifactDrawer.addAsSkillFailed'))
    }
  }

  return { installFromArtifact }
}
