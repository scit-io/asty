import type { MessageKey } from './dictionaries'

type Translator = (key: MessageKey, params?: Record<string, string | number>) => string

// Scaling-event reason strings are formatted server-side as plain
// English. To surface them in the active UI locale we parse the four
// known shapes here and let the dictionary own the wording. Patterns
// match the exact format strings in:
//   ops/autoscaler/scale_up.go  (TRAFFIC, PRESSURE)
//   ops/autoscaler/scale_down.go (IDLE)
//   api/dashboard/services.go    (MANUAL_FLOOR — operator scale)
// Anything unmatched flows through unchanged so future backend
// reasons stay legible until a matching pattern is added.

const MANUAL_FLOOR = /^manual: floor set to (\d+) \(was (\d+)\)$/
const TRAFFIC = /^gateway traffic on node (\S+) without (\S+)$/
const PRESSURE = /^copy on (\S+) exceeded targets \(cpu=(\d+)%, mem=(\d+)% of (\d+)MB\) — adding copy on (\S+)$/
const IDLE = /^avg cpu=(\d+)% mem=(\S+) across (\d+) copies, floor cpu=(\d+) mem=(\d+) \(percent of svc\.Resources\.Memory=(\d+)MB\)$/

export function translateReason(reason: string, t: Translator): string {
  let m = reason.match(MANUAL_FLOOR)
  if (m) return t('reason.manual_floor', { to: m[1], from: m[2] })
  m = reason.match(TRAFFIC)
  if (m) return t('reason.traffic', { node: m[1], service: m[2] })
  m = reason.match(PRESSURE)
  if (m) return t('reason.pressure', { node: m[1], cpu: m[2], mem: m[3], total: m[4], target: m[5] })
  m = reason.match(IDLE)
  if (m) return t('reason.idle', { cpu: m[1], mem: m[2], copies: m[3], cpu_floor: m[4], mem_floor: m[5], total: m[6] })
  return reason
}
