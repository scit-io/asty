import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-hidden focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        // default sits on the project's "healthy / ready / running"
        // green so badges read as a positive indicator without
        // inheriting Button.default's logo-teal — operators expect
        // distinct colour vocabulary for "state" vs "action".
        default:
          "border-transparent bg-green-500 text-white dark:text-black hover:bg-green-500/80",
        secondary:
          "border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80",
        destructive:
          "border-transparent bg-destructive text-destructive-foreground dark:text-black hover:bg-destructive/80",
        success:
          "border-transparent bg-green-500 text-white dark:text-black hover:bg-green-500/80",
        warning:
          "border-transparent bg-yellow-500 text-white dark:text-black hover:bg-yellow-500/80",
        outline: "text-foreground dark:text-black",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants }
