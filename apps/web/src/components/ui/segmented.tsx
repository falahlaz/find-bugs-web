import { cn } from '@/lib/utils'

/** A radio group drawn as a segmented control. */
export function Segmented<T extends string>({ value, options, onChange, label }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string }) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex max-w-full flex-wrap rounded-lg border bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn('rounded-md px-3 py-1 text-xs font-medium text-muted-foreground transition-colors', value === o.value && 'bg-card text-foreground shadow-xs')}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
