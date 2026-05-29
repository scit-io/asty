import { toast } from 'sonner'
import { ApiError } from '@/api/client'
import type { MessageKey } from '@/lib/i18n'

type Translator = (key: MessageKey, params?: Record<string, string | number>) => string

// httpStatusText renders a localized phrase for the HTTP statuses the
// dashboard can actually receive; unrecognized codes fall back to the
// generic template carrying the raw number. Status 0 means the fetch
// never completed (cluster unreachable).
function httpStatusText(status: number, t: Translator): string {
  switch (status) {
    case 0:
      return t('error.http.network')
    case 400:
      return `${t('error.http.bad_request')} (400)`
    case 401:
      return `${t('error.http.unauthorized')} (401)`
    case 403:
      return `${t('error.http.forbidden')} (403)`
    case 404:
      return `${t('error.http.not_found')} (404)`
    case 409:
      return `${t('error.http.conflict')} (409)`
    case 500:
      return `${t('error.http.server')} (500)`
    case 502:
      return `${t('error.http.bad_gateway')} (502)`
    case 503:
      return `${t('error.http.unavailable')} (503)`
    default:
      return t('error.http.generic', { status })
  }
}

// errorText resolves the {error} substitution for the toast templates.
// A known HTTP status (ApiError) becomes a localized phrase; any other
// Error carries a runtime message we cannot pre-translate, so it passes
// through verbatim; a non-Error falls back to a localized "unknown".
function errorText(err: unknown, t: Translator): string {
  if (err instanceof ApiError) return httpStatusText(err.status, t)
  if (err instanceof Error) return err.message
  return t('common.unknown')
}

// toastError dedupes the error-flow that lives at every API call
// site: resolve a message (localized wherever the text is known) and
// drop it into a sonner toast. The translator is passed in (not pulled
// via `useT()`) so the helper stays callable from event handlers
// without becoming a hook.
//
// `messageKey` defaults to the generic 'common.failed' template;
// callers pass a more specific key (`'toast.kill_failed'`,
// `'toast.deploy_failed'`) when the action deserves its own wording.
export function toastError(
  err: unknown,
  t: Translator,
  messageKey: MessageKey = 'common.failed',
): void {
  toast.error(t(messageKey, { error: errorText(err, t) }))
}
