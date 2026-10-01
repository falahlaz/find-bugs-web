import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { LinkButton } from '@/components/ui/link-button'
import { JobsTable } from './jobs-table'
import { useJobs } from './queries'

/** The caller's latest jobs under the submit form. */
export function RecentJobs() {
  const jobs = useJobs({ mine: '1' })
  const list = (jobs.data ?? []).slice(0, 5)
  if (!list.length) return null
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>Job terakhir saya</CardTitle>
        <LinkButton to="/jobs" variant="outline" size="sm">
          Semua histori
        </LinkButton>
      </CardHeader>
      <CardContent>
        <JobsTable jobs={list} showUser={false} />
      </CardContent>
    </Card>
  )
}
