import { useSystemStatus } from '../use-system-status'

/** VPN, Splunk and queue state in the shape every shell shows it. */
export function useHealth() {
  const { data } = useSystemStatus()
  const vpnOk = data?.vpn.healthy ?? null
  const splunk = data?.splunk
  const splunkBusy = Boolean(splunk?.reauthing || splunk?.session.reauthRunning)
  const splunkOk = splunk ? splunk.ok && !splunk.paused : null
  return {
    loaded: Boolean(data),
    vpnOk,
    vpnLabel: vpnOk == null ? '…' : vpnOk ? 'Tersambung' : 'Terputus',
    splunkOk,
    splunkBusy,
    splunkLabel: splunkBusy ? 'Menunggu 2FA' : splunkOk == null ? '…' : splunkOk ? 'Aktif' : splunk?.paused ? 'Kedaluwarsa' : 'Tidak bisa dicek',
    queue: data?.queue ?? { active: 0, max: 0 },
    allOk: vpnOk === true && splunkOk === true,
  }
}
