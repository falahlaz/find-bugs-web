import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router'
import { PageHeader } from '@/components/ui/page-header'
import { useSplunkStatus } from '@/features/splunk/queries'
import { SplunkBadge, SplunkPanel } from '@/features/splunk/splunk-panel'
import { useVpnStatus } from '@/features/vpn/queries'
import { VpnBadge, VpnPanel } from '@/features/vpn/vpn-panel'

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function SectionTitle({ step, title, hint }: { step: number; title: string; hint: string }) {
  return (
    <div className="flex items-start gap-3">
      <span className="grid size-7 shrink-0 place-items-center rounded-full border bg-card font-mono text-xs font-semibold" aria-hidden>
        {step}
      </span>
      <div className="grid gap-0.5">
        <h2 className="text-base font-semibold">
          <span className="sr-only">Langkah {step}: </span>
          {title}
        </h2>
        <span className="text-sm text-muted-foreground">{hint}</span>
      </div>
    </div>
  )
}

/**
 * VPN and Splunk on one page: a fresh VPN login almost always means Splunk
 * needs a re-auth next, so the two steps live together in order.
 */
export function ConnectionPage() {
  const { hash } = useLocation()
  const vpn = useVpnStatus()
  const splunk = useSplunkStatus()

  const vpnUp = vpn.data?.healthy ?? false
  const sp = splunk.data
  const splunkNeedsReauth = Boolean(sp && !sp.ok && !sp.reauthing && !sp.session.reauthRunning)

  useEffect(() => {
    if (hash) scrollToSection(hash.slice(1))
  }, [hash])

  // When the VPN comes up while this page is open, move on to step 2.
  const prevVpnUp = useRef<boolean | null>(null)
  useEffect(() => {
    if (!vpn.data) return
    if (prevVpnUp.current === false && vpnUp && splunkNeedsReauth) scrollToSection('splunk')
    prevVpnUp.current = vpnUp
  }, [vpn.data, vpnUp, splunkNeedsReauth])

  return (
    <div className="grid gap-7">
      <PageHeader title="Koneksi" description="VPN dulu, lalu Splunk. Setelah VPN login ulang, sesi Splunk biasanya perlu Re-auth." />
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <button type="button" className="flex items-center gap-2 rounded-md border bg-card px-3 py-1.5 hover:bg-secondary" onClick={() => scrollToSection('vpn')}>
          <span className="font-medium">VPN</span>
          <VpnBadge />
        </button>
        <span className="text-muted-foreground" aria-hidden>
          →
        </span>
        <button type="button" className="flex items-center gap-2 rounded-md border bg-card px-3 py-1.5 hover:bg-secondary" onClick={() => scrollToSection('splunk')}>
          <span className="font-medium">Splunk</span>
          <SplunkBadge />
        </button>
      </div>

      <section id="vpn" className="grid scroll-mt-6 gap-4">
        <SectionTitle step={1} title="VPN" hint="Sambungkan GlobalProtect dengan akun SSO kamu." />
        <VpnPanel />
      </section>

      <section id="splunk" className="grid scroll-mt-6 gap-4">
        <SectionTitle step={2} title="Splunk" hint="Login ulang Splunk setelah VPN tersambung." />
        <SplunkPanel highlight={vpnUp && splunkNeedsReauth} />
      </section>
    </div>
  )
}
