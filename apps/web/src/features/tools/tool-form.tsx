import { CornerDownLeft } from 'lucide-react'
import { useState, type FormEvent, type KeyboardEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Segmented } from '@/components/ui/segmented'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { activeFields, groupFields, initialValues } from './fields'
import type { Tool, ToolField, ToolValues } from './queries'

function submitOnCtrlEnter(e: KeyboardEvent<HTMLTextAreaElement>) {
  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    e.currentTarget.form?.requestSubmit()
  }
}

function FieldInput({ field, value, onChange }: { field: ToolField; value: string | boolean; onChange: (v: string | boolean) => void }) {
  const id = `tool-${field.name}`
  const text = typeof value === 'string' ? value : ''
  switch (field.type) {
    case 'tabs':
      return <Segmented label={field.label} value={text} options={field.options ?? []} onChange={onChange} />
    case 'select':
      return (
        <Select id={id} value={text} onChange={(e) => onChange(e.target.value)}>
          {field.options?.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </Select>
      )
    case 'textarea':
      return (
        <Textarea
          id={id}
          rows={4}
          spellCheck={false}
          required={field.required}
          placeholder={field.placeholder}
          value={text}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={submitOnCtrlEnter}
          className="resize-y font-mono text-[13px] leading-relaxed"
        />
      )
    case 'checkbox':
      return <input id={id} type="checkbox" checked={value === true} onChange={(e) => onChange(e.target.checked)} className="size-4 accent-primary" />
    default:
      return (
        <Input
          id={id}
          type={field.type === 'number' ? 'number' : 'text'}
          inputMode={field.type === 'number' ? 'decimal' : undefined}
          spellCheck={false}
          autoComplete="off"
          required={field.required}
          placeholder={field.placeholder}
          value={text}
          onChange={(e) => onChange(e.target.value)}
          className="font-mono"
        />
      )
  }
}

function FieldRow({ field, value, onChange }: { field: ToolField; value: string | boolean; onChange: (v: string | boolean) => void }) {
  return (
    <div className="grid content-start gap-1.5">
      {field.type === 'tabs' ? (
        <span className="text-xs font-medium text-muted-foreground">{field.label}</span>
      ) : (
        <label htmlFor={`tool-${field.name}`} className="text-xs font-medium text-muted-foreground">
          {field.label}
          {field.required && <span className="text-bad"> *</span>}
        </label>
      )}
      <FieldInput field={field} value={value} onChange={onChange} />
      {field.help && <p className="text-xs text-muted-foreground">{field.help}</p>}
    </div>
  )
}

/**
 * The form generated from a tool's manifest. Mount it with key={tool.id} so
 * switching tools starts from that tool's defaults.
 */
export function ToolForm({ tool, pending, onRun }: { tool: Tool; pending: boolean; onRun: (values: ToolValues) => void }) {
  const [values, setValues] = useState(() => initialValues(tool.fields))
  const active = activeFields(tool.fields, values)
  const groups = groupFields(tool.fields.filter((f) => active.has(f.name)))

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    onRun(values)
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-4">
      {groups.map((g) => (
        <div key={g[0].name} className={cn('grid gap-4', g.length > 1 && 'sm:grid-cols-2')}>
          {g.map((f) => (
            <FieldRow key={f.name} field={f} value={values[f.name] ?? ''} onChange={(v) => setValues((cur) => ({ ...cur, [f.name]: v }))} />
          ))}
        </div>
      ))}
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? 'Memproses…' : 'Jalankan'}
        </Button>
        <span className="hidden items-center gap-1 text-xs text-muted-foreground sm:inline-flex">
          <kbd className="font-mono">Ctrl</kbd>+<CornerDownLeft className="size-3" aria-label="Enter" />
        </span>
      </div>
    </form>
  )
}
