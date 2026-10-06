import { Inbox } from 'lucide-react'
import { LinkButton } from '@/components/ui/link-button'

/**
 * /jobs: the job list lives in the shell, so on wide screens this is what
 * the detail pane shows before a job is picked. Phones only see the list.
 */
export function JobsPage() {
  return (
    <div className="grid min-h-[60vh] place-items-center">
      <div className="grid max-w-sm justify-items-center gap-3 text-center">
        <Inbox className="size-8 text-muted-foreground" aria-hidden />
        <p className="text-sm text-muted-foreground">Pilih investigasi di daftar untuk melihat diagnosisnya, atau mulai yang baru.</p>
        <LinkButton size="sm" to="/">
          Investigasi baru
        </LinkButton>
      </div>
    </div>
  )
}
