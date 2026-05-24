import { useEffect, useRef, useState } from 'react'
import { Scaling } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api } from '@/api/client'

interface ServiceMinCopiesCardProps {
  name: string
  // Seed the input from the live current_copies once it first
  // arrives; after the seed the field is owned by the operator so
  // autoscaler bumps don't clobber what they're typing.
  currentCopies: number | undefined
  // onChanged fires after a successful POST so the page can refetch
  // the autoscaler payload (floor / override) without waiting for
  // the 15-second poll tick. Usually `() => refreshService(name)`.
  onChanged: () => Promise<void>
  className?: string
}

// ServiceMinCopiesCard owns the per-service floor input on /services/
// :name. Rendered only when service.Type === 'service' — system
// services have no autoscale dimension. Posts to /scale; the
// autoscaler is the consumer of the new floor.
export function ServiceMinCopiesCard({ name, currentCopies, onChanged, className }: ServiceMinCopiesCardProps) {
  const [scaleTo, setScaleTo] = useState('')
  const [scaling, setScaling] = useState(false)
  const initialized = useRef(false)

  useEffect(() => {
    if (!initialized.current && currentCopies !== undefined) {
      setScaleTo(String(currentCopies))
      initialized.current = true
    }
  }, [currentCopies])

  const handleScale = async () => {
    if (!scaleTo) return
    const n = parseInt(scaleTo, 10)
    if (Number.isNaN(n) || n < 0) {
      toast.error('Enter a non-negative integer')
      return
    }
    setScaling(true)
    try {
      await api.scaleService(name, n)
      toast.success(`Set ${name} floor to ${n}`)
      await onChanged()
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`)
    } finally {
      setScaling(false)
    }
  }

  return (
    <Card className={className}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">Min copies</CardTitle>
        <Scaling className="h-4 w-4 text-muted-foreground" />
      </CardHeader>
      <CardContent className="space-y-2">
        <div className="flex items-center gap-2 mt-2 mb-4">
          <Input className="flex-1" type="number" min={0}
            placeholder="copies" value={scaleTo}
            onChange={(e) => setScaleTo(e.target.value)} />
          <Button onClick={handleScale} disabled={scaling || !scaleTo}>
            {scaling ? 'Saving…' : 'Set floor'}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          Per-service floor. Autoscaler can grow above; lowering stops
          excess copies immediately.
        </p>
      </CardContent>
    </Card>
  )
}
