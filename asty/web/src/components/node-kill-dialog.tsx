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

// NodeKillDialog is the destructive counterpart of NodeDrainDialog.
// Form-wrapped so Enter on the input submits the kill. The
// onOpenChange wrapper ignores `false` while killing is in flight —
// that single guard covers overlay click, Escape, X, and Cancel.
export function NodeKillDialog({ open, nodeId, isLastNode, onOpenChange, onConfirm }: NodeKillDialogProps) {
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
              Kill node {nodeId}?
            </DialogTitle>
            <DialogDescription className="space-y-2">
              <span className="block">
                The node is removed from the cluster. To bring it back online
                you'll have to reinstall the asty agent on the host.
              </span>
              <span className="block">
                For routine operations use <b>Drain</b> — it migrates
                allocations to peers before tearing the node down.
              </span>
              {isLastNode && (
                <span className="block text-destructive font-medium">
                  This is the only node in the cluster — killing it will fully dismantle the cluster.
                </span>
              )}
              <span className="block">
                Type the node id <code className="font-mono">{nodeId}</code> to confirm.
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
              <span>I understand the cluster will be fully dismantled.</span>
            </label>
          )}
          {killing && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground mt-3">
              <Loader2 className="h-4 w-4 animate-spin" />
              Killing node {nodeId}…
            </div>
          )}
          <DialogFooter className="mt-4">
            <DialogClose asChild>
              <Button type="button" variant="outline" disabled={killing}>Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="destructive" disabled={!ready}>
              {killing ? 'Killing…' : 'Kill node'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
