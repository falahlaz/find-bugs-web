import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'
import { useAuth } from '@/app/auth-context'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { JobsTable } from './jobs-table'
import { PAGE_SIZE, useEnvironments, useJobs, type JobFilters } from './queries'
import { allStatuses, statusInfo } from './status'

const filterKeys = ['environment', 'status', 'transactionId', 'from', 'to'] as const

export function JobsPage() {
  const { user } = useAuth()
  const envs = useEnvironments()
  const [params, setParams] = useSearchParams()
  const filters: JobFilters = {}
  for (const k of filterKeys) {
    const v = params.get(k)
    if (v) filters[k] = v
  }
  // Cursor pagination: each page starts below the smallest ID seen so far.
  const [cursors, setCursors] = useState<number[]>([])
  const before = cursors.at(-1)
  const jobs = useJobs(filters, before)

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const next = new URLSearchParams()
    for (const k of filterKeys) {
      const v = String(form.get(k) ?? '').trim()
      if (v) next.set(k, v)
    }
    setCursors([])
    setParams(next)
  }

  const list = jobs.data ?? []
  const engineer = user?.role === 'engineer'
  return (
    <Card>
      <CardHeader>
        <CardTitle>Histori investigasi</CardTitle>
        <CardDescription>{engineer ? 'Semua investigasi dari semua user.' : 'Investigasi yang kamu submit.'}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <form onSubmit={onSubmit} key={params.toString()} className="grid gap-3 sm:grid-cols-6 sm:items-end">
          <div className="grid gap-1.5 sm:col-span-2">
            <Label htmlFor="transactionId">Transaction ID</Label>
            <Input id="transactionId" name="transactionId" defaultValue={filters.transactionId} placeholder="cari sebagian" />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="f-env">Environment</Label>
            <Select id="f-env" name="environment" defaultValue={filters.environment ?? ''}>
              <option value="">Semua</option>
              {(envs.data?.environments ?? []).map((e) => (
                <option key={e}>{e}</option>
              ))}
            </Select>
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="f-status">Status</Label>
            <Select id="f-status" name="status" defaultValue={filters.status ?? ''}>
              <option value="">Semua</option>
              {allStatuses.map((s) => (
                <option key={s} value={s}>
                  {statusInfo[s].label}
                </option>
              ))}
            </Select>
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="from">Dari</Label>
            <Input id="from" name="from" type="date" defaultValue={filters.from} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="to">Sampai</Label>
            <Input id="to" name="to" type="date" defaultValue={filters.to} />
          </div>
          <div className="flex gap-2 sm:col-span-6">
            <Button type="submit" size="sm">
              Terapkan filter
            </Button>
            {params.size > 0 && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setCursors([])
                  setParams(new URLSearchParams())
                }}
              >
                Reset
              </Button>
            )}
          </div>
        </form>

        {jobs.error && <Alert tone="danger">{jobs.error.message}</Alert>}
        {jobs.isPending ? (
          <p className="text-sm text-muted-foreground">Memuat…</p>
        ) : list.length === 0 ? (
          <p className="text-sm text-muted-foreground">Belum ada investigasi yang cocok.</p>
        ) : (
          <JobsTable jobs={list} showUser={engineer} />
        )}

        <div className="flex gap-2">
          {cursors.length > 0 && (
            <Button variant="outline" size="sm" onClick={() => setCursors((c) => c.slice(0, -1))}>
              ← Lebih baru
            </Button>
          )}
          {list.length === PAGE_SIZE && (
            <Button variant="outline" size="sm" onClick={() => setCursors((c) => [...c, list[list.length - 1].id])}>
              Lebih lama →
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
