import { cva, type VariantProps } from 'class-variance-authority'
import type * as React from 'react'
import { cn } from '@/lib/utils'

const badgeVariants = cva('inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap', {
  variants: {
    tone: {
      neutral: 'border-transparent bg-neu-bg text-neu',
      info: 'border-transparent bg-info-bg text-info',
      success: 'border-transparent bg-ok-bg text-ok',
      warning: 'border-transparent bg-warn-bg text-warn',
      danger: 'border-transparent bg-bad-bg text-bad',
      outline: 'text-foreground',
    },
  },
  defaultVariants: { tone: 'neutral' },
})

export type BadgeTone = NonNullable<VariantProps<typeof badgeVariants>['tone']>

/** `dot` adds a status dot; `pulse` animates it for work in progress. */
export function Badge({
  className,
  tone,
  dot = false,
  pulse = false,
  children,
  ...props
}: React.ComponentProps<'span'> & VariantProps<typeof badgeVariants> & { dot?: boolean; pulse?: boolean }) {
  return (
    <span data-slot="badge" className={cn(badgeVariants({ tone }), className)} {...props}>
      {dot && <span className={cn('size-1.5 rounded-full bg-current', pulse && 'animate-pulse')} aria-hidden />}
      {children}
    </span>
  )
}
