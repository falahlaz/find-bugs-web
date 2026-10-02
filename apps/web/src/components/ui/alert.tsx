import { CircleAlert, Info } from 'lucide-react'
import type * as React from 'react'
import { cn } from '@/lib/utils'

export function Alert({
  className,
  tone = 'neutral',
  children,
  ...props
}: React.ComponentProps<'div'> & { tone?: 'neutral' | 'danger' | 'warning' | 'info' }) {
  const Icon = tone === 'danger' || tone === 'warning' ? CircleAlert : Info
  return (
    <div
      role="alert"
      className={cn(
        'flex gap-2.5 rounded-lg px-3.5 py-2.5 text-sm text-foreground',
        tone === 'neutral' && 'bg-neu-bg [&>svg]:text-neu',
        tone === 'danger' && 'bg-bad-bg [&>svg]:text-bad',
        tone === 'warning' && 'bg-warn-bg [&>svg]:text-warn',
        tone === 'info' && 'bg-info-bg [&>svg]:text-info',
        className,
      )}
      {...props}
    >
      <Icon className="mt-0.5 size-4 shrink-0" aria-hidden />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}
