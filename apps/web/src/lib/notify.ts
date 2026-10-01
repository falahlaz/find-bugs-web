// Browser notifications for finished jobs. They only fire while a tab is
// open; the Telegram group covers system alerts.

export function notificationsSupported() {
  return typeof window !== 'undefined' && 'Notification' in window
}

/** Asks once, from a user gesture (submit), so browsers allow the prompt. */
export function requestNotificationPermission() {
  if (notificationsSupported() && Notification.permission === 'default') {
    void Notification.requestPermission()
  }
}

export function notify(title: string, body: string, url?: string) {
  if (!notificationsSupported() || Notification.permission !== 'granted') return
  // Skip when the user is already looking at the page.
  if (document.visibilityState === 'visible' && document.hasFocus()) return
  const n = new Notification(title, { body, tag: url })
  if (url) {
    n.onclick = () => {
      window.focus()
      window.location.assign(url)
    }
  }
}
