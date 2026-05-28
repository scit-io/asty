import * as React from "react"
import * as SwitchPrimitives from "@radix-ui/react-switch"

import { cn } from "@/lib/utils"

type SwitchSize = 'default' | 'sm'

type SwitchProps = React.ComponentPropsWithoutRef<typeof SwitchPrimitives.Root> & {
  size?: SwitchSize
}

// Two coordinated size pairs — root dimensions AND thumb translation
// must match, or the thumb would overshoot/undershoot in the checked
// state. Override via className would only touch the root, which is
// why we expose `size` instead of leaving it to the caller.
const ROOT_SIZE: Record<SwitchSize, string> = {
  default: 'h-5 w-9',
  sm: 'h-4 w-7',
}
const THUMB_SIZE: Record<SwitchSize, string> = {
  default: 'h-4 w-4 data-[state=checked]:translate-x-4',
  sm: 'h-3 w-3 data-[state=checked]:translate-x-3',
}

const Switch = React.forwardRef<
  React.ElementRef<typeof SwitchPrimitives.Root>,
  SwitchProps
>(({ className, size = 'default', ...props }, ref) => (
  <SwitchPrimitives.Root
    className={cn(
      "peer inline-flex shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent shadow-xs transition-colors focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-primary data-[state=unchecked]:bg-input",
      ROOT_SIZE[size],
      className
    )}
    {...props}
    ref={ref}
  >
    <SwitchPrimitives.Thumb
      className={cn(
        "pointer-events-none block rounded-full bg-background shadow-lg ring-0 transition-transform data-[state=unchecked]:translate-x-0",
        THUMB_SIZE[size],
      )}
    />
  </SwitchPrimitives.Root>
))
Switch.displayName = SwitchPrimitives.Root.displayName

export { Switch }
