import { useNavigate } from 'react-router'
import { formatDateTime } from '@/lib/format'
import { useTimezone } from './queries'
import type { Job, JobStatus } from './status'
import { StatusBadge } from './status-badge'

export function JobsTable({ jobs, showUser }: { jobs: Job[]; showUser: boolean }) {
  const navigate = useNavigate()
  const tz = useTimezone()
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="py-2 pr-3 font-medium">#</th>
            <th className="py-2 pr-3 font-medium">Transaction ID</th>
            <th className="py-2 pr-3 font-medium">Env</th>
            <th className="py-2 pr-3 font-medium">Rentang</th>
            <th className="py-2 pr-3 font-medium">Status</th>
            {showUser && <th className="py-2 pr-3 font-medium">User</th>}
            <th className="py-2 font-medium">Dikirim</th>
          </tr>
        </thead>
        <tbody>
          {jobs.map((j) => (
            <tr
              key={j.id}
              className="cursor-pointer border-b last:border-0 hover:bg-muted/60"
              onClick={() => navigate(`/jobs/${j.id}`)}
            >
              <td className="py-2 pr-3 text-muted-foreground">{j.id}</td>
              <td className="max-w-64 truncate py-2 pr-3 font-mono">
                <a href={`/jobs/${j.id}`} onClick={(e) => e.preventDefault()} className="hover:underline">
                  {j.transactionId}
                </a>
              </td>
              <td className="py-2 pr-3">{j.environment}</td>
              <td className="py-2 pr-3">{j.timeRange}</td>
              <td className="py-2 pr-3">
                <StatusBadge status={j.status as JobStatus} />
              </td>
              {showUser && <td className="py-2 pr-3">{j.username}</td>}
              <td className="whitespace-nowrap py-2 text-muted-foreground">{formatDateTime(j.queuedAt, tz)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
