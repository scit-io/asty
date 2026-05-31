import { Globe } from 'lucide-react'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

interface NodeIdentityTooltipProps {
  dc?: string
  ip?: string
  host?: string
  // iconClassName lets callers tune size / colour to match the
  // surrounding text — leader tile uses h-3.5 w-3.5 opacity-70, the
  // footer wants something even smaller and dimmer.
  iconClassName?: string
}

// NodeIdentityTooltip renders a globe icon that reveals a tooltip
// with the node's datacenter (bold), IP, and host (muted hint).
// Renders nothing when all three fields are empty. Shared by the
// cluster page leader tile and the dashboard footer.
export function NodeIdentityTooltip({
  dc,
  ip,
  host,
  iconClassName = 'h-3.5 w-3.5 opacity-70',
}: NodeIdentityTooltipProps) {
  if (!dc && !ip && !host) return null
  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Globe className={iconClassName} />
        </TooltipTrigger>
        <TooltipContent className="font-normal">
          {dc && <div className="font-semibold">{dc}</div>}
          {ip && <div className="font-normal">{ip}</div>}
          {host && <div className="font-normal text-xs text-muted-foreground">{host}</div>}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
