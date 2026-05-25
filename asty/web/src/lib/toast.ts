import { toast } from 'sonner'
import type { MessageKey } from '@/lib/i18n'

type Translator = (key: MessageKey, params?: Record<string, string | number>) => string

// toastError dedupes the error-flow that lives at every API call
// site: format the message ('Failed: X' / 'Kill failed: X' / …) and
// drop it into a sonner toast. The translator is passed in (not
// pulled via `useT()`) so the helper stays callable from event
// handlers without becoming a hook.
//
// `messageKey` defaults to the generic 'common.failed' template;
// callers pass a more specific key (`'toast.kill_failed'`,
// `'toast.deploy_failed'`) when the action deserves its own wording.
export function toastError(
  err: unknown,
  t: Translator,
  messageKey: MessageKey = 'common.failed',
): void {
  const error = err instanceof Error ? err.message : t('common.unknown')
  toast.error(t(messageKey, { error }))
}
