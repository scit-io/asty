import { Hash } from 'lucide-react'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

interface AllocIdBadgeProps {
  id: string
  // shortLen controls how many leading chars of the id show. Default
  // 7 mirrors the Git short-hash convention.
  shortLen?: number
  // iconClassName lets callers tune size/colour to the surrounding
  // typography.
  iconClassName?: string
}

// AllocIdBadge renders a Git-style short hash (first N chars) next to
// a hash icon; hovering the icon reveals a tooltip with the full id.
// Used everywhere an allocation id is shown in a table or header.
export function AllocIdBadge({
  id,
  shortLen = 7,
  iconClassName = 'h-3 w-3 opacity-60',
}: AllocIdBadgeProps) {
  if (!id) return <span className="text-xs text-muted-foreground">—</span>
  const short = id.length > shortLen ? id.slice(0, shortLen) : id
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      <span>{short}</span>
      <TooltipProvider delayDuration={0}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Hash className={iconClassName} />
          </TooltipTrigger>
          <TooltipContent className="font-normal break-all max-w-[28rem]">
            {id}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </span>
  )
}
