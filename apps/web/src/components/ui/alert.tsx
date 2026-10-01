import type * as React from 'react'
import { cn } from '@/lib/utils'

export function Alert({ className, tone = 'neutral', ...props }: React.ComponentProps<'div'> & { tone?: 'neutral' | 'danger' | 'warning' | 'info' }) {
  return (
    <div
      role="alert"
      className={cn(
        'rounded-lg border px-4 py-3 text-sm',
        tone === 'danger' && 'border-destructive/30 bg-destructive/5 text-destructive',
        tone === 'warning' && 'border-amber-300 bg-amber-50 text-amber-900 dark:bg-amber-900/20 dark:text-amber-200',
        tone === 'info' && 'border-sky-200 bg-sky-50 text-sky-900 dark:bg-sky-900/20 dark:text-sky-200',
        className,
      )}
      {...props}
    />
  )
}
