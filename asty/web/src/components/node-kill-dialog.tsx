import { useEffect, useState } from 'react'
import { Loader2, Skull } from 'lucide-react'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { useT } from '@/lib/i18n'

interface NodeKillDialogProps {
  open: boolean
  nodeId: string
  // isLastNode flips on the "cluster will be fully dismantled" warning
  // and adds the acknowledgement checkbox — required to enable the
  // Kill button when there's no peer left.
  isLastNode: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => Promise<void>
}

// SENTINEL substitutes for {drain} so the prose can be split on the
// placeholder position (and the slot wrapped in <b>) without splitting
// on ordinary spaces in the translation.
const DRAIN_SENTINEL = '__DRAIN__'

// NodeKillDialog is the destructive counterpart of NodeDrainDialog.
// Form-wrapped so Enter on the input submits the kill. The
// onOpenChange wrapper ignores `false` while killing is in flight —
// that single guard covers overlay click, Escape, X, and Cancel.
export function NodeKillDialog({ open, nodeId, isLastNode, onOpenChange, onConfirm }: NodeKillDialogProps) {
  const t = useT()
  const [confirmName, setConfirmName] = useState('')
  const [acknowledged, setAcknowledged] = useState(false)
  const [killing, setKilling] = useState(false)

  useEffect(() => {
    if (!open) {
      setConfirmName('')
      setAcknowledged(false)
      setKilling(false)
    }
  }, [open])

  const ready = confirmName === nodeId && (!isLastNode || acknowledged) && !killing

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!ready) return
    setKilling(true)
    try {
      await onConfirm()
    } finally {
      setKilling(false)
    }
  }

  const drainTemplate = t('kill.dialog.warning_drain', { drain: DRAIN_SENTINEL })
  const drainWord = t('kill.dialog.warning_drain.drain')
  const [drainBefore, drainAfter] = drainTemplate.split(DRAIN_SENTINEL)

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (killing) return
        onOpenChange(next)
      }}
    >
      <DialogContent>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Skull className="h-5 w-5 text-destructive" />
              {t('kill.dialog.title', { id: nodeId })}
            </DialogTitle>
            <DialogDescription className="space-y-2">
              <span className="block">
                {t('kill.dialog.warning_main')}
              </span>
              <span className="block">
                {drainBefore}<b>{drainWord}</b>{drainAfter}
              </span>
              {isLastNode && (
                <span className="block text-destructive font-medium">
                  {t('kill.dialog.last_node')}
                </span>
              )}
              <span className="block">
                {t('kill.dialog.type_to_confirm', { id: nodeId })}
              </span>
            </DialogDescription>
          </DialogHeader>
          <Input
            autoFocus
            value={confirmName}
            onChange={(e) => setConfirmName(e.target.value)}
            placeholder={nodeId}
            disabled={killing}
            className="font-mono mt-4"
          />
          {isLastNode && (
            <label className="flex items-start gap-2 text-sm mt-4 cursor-pointer">
              <Checkbox
                checked={acknowledged}
                onCheckedChange={(v) => setAcknowledged(v === true)}
                disabled={killing}
                className="mt-0.5"
              />
              <span>{t('kill.dialog.acknowledge')}</span>
            </label>
          )}
          {killing && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground mt-3">
              <Loader2 className="h-4 w-4 animate-spin" />
              {t('kill.dialog.killing', { id: nodeId })}
            </div>
          )}
          <DialogFooter className="mt-4">
            <DialogClose asChild>
              <Button type="button" variant="outline" disabled={killing}>{t('common.cancel')}</Button>
            </DialogClose>
            <Button type="submit" variant="destructive" disabled={!ready}>
              {killing ? t('kill.dialog.confirming') : t('kill.dialog.confirm')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
