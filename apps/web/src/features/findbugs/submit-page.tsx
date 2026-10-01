import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { useSystemStatus } from '@/app/use-system-status'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { LinkButton } from '@/components/ui/link-button'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { formatDateTime } from '@/lib/format'
import { requestNotificationPermission } from '@/lib/notify'
import { RecentJobs } from './recent-jobs'
import { useEnvironments, useSubmitJob, useTimezone } from './queries'
import type { JobView } from './status'

const LAST_ENV_KEY = 'findbugs.lastEnvironment'

function readLastEnv() {
  try {
    return localStorage.getItem(LAST_ENV_KEY) ?? ''
  } catch {
    return ''
  }
}

export function SubmitPage() {
  const navigate = useNavigate()
  const envs = useEnvironments()
  const status = useSystemStatus()
  const submit = useSubmitJob()
  const tz = useTimezone()
  const [environment, setEnvironment] = useState(readLastEnv)
  const [timeRange, setTimeRange] = useState<'24h' | '48h'>('24h')
  const [input, setInput] = useState('')
  const [duplicate, setDuplicate] = useState<JobView | null>(null)

  // Fall back to the first environment when the remembered one is unknown.
  const envList = envs.data?.environments ?? []
  const env = envList.includes(environment) ? environment : (envList[0] ?? '')

  async function send(force: boolean) {
    requestNotificationPermission()
    try {
      localStorage.setItem(LAST_ENV_KEY, env)
    } catch {
      /* storage unavailable */
    }
    const res = await submit.mutateAsync({ environment: env, timeRange, input, force })
    if (res.duplicate) {
      setDuplicate(res.job)
      return
    }
    navigate(`/jobs/${res.job.id}`)
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    setDuplicate(null)
    void send(false).catch(() => {})
  }

  const queue = status.data?.queue
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <CardTitle>Submit investigasi</CardTitle>
          <CardDescription>
            Paste transaction ID atau curl lengkap. Sistem mencari log di Splunk lalu AI membuat diagnosis.
            {queue && ` Antrean: ${queue.active}/${queue.max} job.`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="grid gap-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="environment">Environment</Label>
                <Select id="environment" value={env} onChange={(e) => setEnvironment(e.target.value)} required>
                  {envList.map((e) => (
                    <option key={e} value={e}>
                      {e}
                    </option>
                  ))}
                </Select>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="timeRange">Rentang waktu</Label>
                <Select id="timeRange" value={timeRange} onChange={(e) => setTimeRange(e.target.value as '24h' | '48h')}>
                  <option value="24h">24 jam terakhir</option>
                  <option value="48h">48 jam terakhir</option>
                </Select>
              </div>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="input">Transaction ID atau curl</Label>
              <Textarea
                id="input"
                rows={6}
                className="font-mono"
                placeholder={'abc-123\n\natau\n\ncurl -H "X-Transaction-ID: abc-123" https://api.example.com/...'}
                value={input}
                onChange={(e) => setInput(e.target.value)}
                required
              />
            </div>
            {submit.error && <Alert tone="danger">{submit.error.message}</Alert>}
            {duplicate && (
              <Alert tone="info" className="grid gap-2">
                <span>
                  Transaction ID <b>{duplicate.transactionId}</b> sudah diinvestigasi pada {formatDateTime(duplicate.queuedAt, tz)} dengan parameter yang sama.
                </span>
                <span className="flex flex-wrap gap-2">
                  <LinkButton size="sm" to={`/jobs/${duplicate.id}`}>
                    Lihat hasil sebelumnya
                  </LinkButton>
                  <Button type="button" variant="outline" size="sm" disabled={submit.isPending} onClick={() => void send(true).catch(() => {})}>
                    Jalankan ulang
                  </Button>
                </span>
              </Alert>
            )}
            <div>
              <Button type="submit" disabled={submit.isPending || !env}>
                {submit.isPending ? 'Mengirim…' : 'Submit'}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
      <RecentJobs />
    </div>
  )
}
