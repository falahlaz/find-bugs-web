import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router'
import { Card, CardContent } from '@/components/ui/card'
import { useSplunkStatus } from '@/features/splunk/queries'
import { SplunkBadge, SplunkPanel } from '@/features/splunk/splunk-panel'
import { useVpnStatus } from '@/features/vpn/queries'
import { VpnBadge, VpnPanel } from '@/features/vpn/vpn-panel'

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function SectionTitle({ step, title, hint }: { step: number; title: string; hint: string }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <h2 className="text-lg font-semibold">
        <span className="text-muted-foreground">Langkah {step} ·</span> {title}
      </h2>
      <span className="text-sm text-muted-foreground">{hint}</span>
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
    <div className="grid gap-8">
      <Card className="py-4">
        <CardContent className="flex flex-wrap items-center gap-x-8 gap-y-3 text-sm">
          <button type="button" className="flex items-center gap-2" onClick={() => scrollToSection('vpn')}>
            <span className="font-medium">VPN</span>
            <VpnBadge />
          </button>
          <span className="text-muted-foreground" aria-hidden>
            →
          </span>
          <button type="button" className="flex items-center gap-2" onClick={() => scrollToSection('splunk')}>
            <span className="font-medium">Splunk</span>
            <SplunkBadge />
          </button>
          <span className="text-xs text-muted-foreground sm:ml-auto">Setelah VPN login ulang, sesi Splunk biasanya perlu Re-auth.</span>
        </CardContent>
      </Card>

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
