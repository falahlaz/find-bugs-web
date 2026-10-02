import { LinkButton } from '@/components/ui/link-button'
import { PageHeader } from '@/components/ui/page-header'

export function NotFound() {
  return (
    <PageHeader
      title="Halaman tidak ditemukan"
      description="Link ini tidak ada."
      actions={
        <LinkButton to="/jobs" size="sm" variant="outline">
          Ke daftar investigasi
        </LinkButton>
      }
    />
  )
}
