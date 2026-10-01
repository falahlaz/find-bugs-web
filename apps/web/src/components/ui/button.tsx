import type { VariantProps } from 'class-variance-authority'
import type * as React from 'react'
import { cn } from '@/lib/utils'
import { buttonVariants } from './button-variants'

export function Button({
  className,
  variant,
  size,
  ...props
}: React.ComponentProps<'button'> & VariantProps<typeof buttonVariants>) {
  return <button data-slot="button" className={cn(buttonVariants({ variant, size, className }))} {...props} />
}
