import type { ToolField, ToolValues } from './queries'

export function initialValues(fields: ToolField[]): ToolValues {
  return Object.fromEntries(fields.map((f) => [f.name, f.type === 'checkbox' ? f.default === 'true' : f.default]))
}

/**
 * Names of the fields whose showIf holds. Mirrors tools.ActiveFields on the
 * server: conditions are transitive, so a field hides when the field it
 * depends on is hidden, whatever stale value that field still holds.
 */
export function activeFields(fields: ToolField[], values: ToolValues): Set<string> {
  const byName = new Map(fields.map((f) => [f.name, f]))
  const memo = new Map<string, boolean>()
  const visiting = new Set<string>()
  const active = (f: ToolField): boolean => {
    const known = memo.get(f.name)
    if (known !== undefined) return known
    if (visiting.has(f.name)) return true
    visiting.add(f.name)
    let res = true
    if (f.showIf) {
      const parent = byName.get(f.showIf.field)
      if (parent && !active(parent)) res = false
      else {
        const v = String(values[f.showIf.field] ?? '')
        res = f.showIf.in ? f.showIf.in.includes(v) : v === (f.showIf.equals ?? '')
      }
    }
    visiting.delete(f.name)
    memo.set(f.name, res)
    return res
  }
  return new Set(fields.filter(active).map((f) => f.name))
}

/** Consecutive visible fields sharing a group, laid out side by side. */
export function groupFields(fields: ToolField[]): ToolField[][] {
  const out: ToolField[][] = []
  for (const f of fields) {
    const last = out[out.length - 1]
    if (f.group && last?.[0].group === f.group) last.push(f)
    else out.push([f])
  }
  return out
}
