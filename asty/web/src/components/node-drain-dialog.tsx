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
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={(e) => { e.preventDefault(); onConfirm() }}>
          <DialogHeader>
            <DialogTitle>Drain node {nodeId}?</DialogTitle>
            <DialogDescription>
              All running allocations on this node will be migrated to peers and the node will stop accepting new placements.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="mt-4">
            <DialogClose asChild>
              <Button type="button" variant="outline">Cancel</Button>
            </DialogClose>
            <Button type="submit" autoFocus>Drain</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
