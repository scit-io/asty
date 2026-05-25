import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useT } from '@/lib/i18n'

interface NodeDrainDialogProps {
  open: boolean
  nodeId: string
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}

// NodeDrainDialog is the single source of truth for the "Drain
// node?" confirmation — used from both the /nodes list switch and
// the node-detail Maintenance tile. Form-wrapped so Enter submits.
export function NodeDrainDialog({ open, nodeId, onOpenChange, onConfirm }: NodeDrainDialogProps) {
  const t = useT()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={(e) => { e.preventDefault(); onConfirm() }}>
          <DialogHeader>
            <DialogTitle>{t('drain.dialog.title', { id: nodeId })}</DialogTitle>
            <DialogDescription>
              {t('drain.dialog.description')}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="mt-4">
            <DialogClose asChild>
              <Button type="button" variant="outline">{t('common.cancel')}</Button>
            </DialogClose>
            <Button type="submit" autoFocus>{t('drain.dialog.confirm')}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
