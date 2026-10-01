import type { VariantProps } from 'class-variance-authority'
import type * as React from 'react'
import { Link } from 'react-router'
import { cn } from '@/lib/utils'
import { buttonVariants } from './button-variants'

/** A router link styled as a button. */
export function LinkButton({
  className,
  variant,
  size,
  ...props
}: React.ComponentProps<typeof Link> & VariantProps<typeof buttonVariants>) {
  return <Link className={cn(buttonVariants({ variant, size, className }))} {...props} />
}
