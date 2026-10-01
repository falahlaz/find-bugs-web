# Find Bugs Web

Website pengganti Telegram bot `find-bugs-bot`. QA submit transaction ID atau curl, server mencari log di Splunk lewat VPN GlobalProtect milik server sendiri, lalu Claude Code (Haiku) membuat diagnosis. Lihat [docs/PRD.md](docs/PRD.md).

```text
apps/server/   Go 1.24 backend (API, worker, VPN, Splunk, analyzer), satu binary
apps/web/      React + TypeScript + Vite + Tailwind + shadcn/ui
scripts/       splunk-login: script Playwright untuk SSO Splunk (dipanggil server)
deploy/        systemd user units, backup, contoh config cloudflared
docs/          PRD
```

## Status

Fase 1 (pipeline headless) selesai: semua endpoint API sudah jalan dan teruji. Frontend baru berupa shell (login, navigasi, banner status); halaman submit, histori, panel VPN/Splunk, dan manajemen user dibangun di Fase 2–3.

## Development

Butuh Go 1.24 dan Node 22.

```sh
make test        # go vet + go test, oxlint + tsc
make run-dev     # server di :8080 dengan GlobalProtect, Splunk, dan analyzer palsu
cd apps/web && npm run dev   # Vite di :5173, mem-proxy /api ke :8080
```

Buat user dulu sebelum login (password dibaca dari stdin atau prompt):

```sh
cd apps/server
DB_PATH=/tmp/fbw-dev/dev.db go run ./cmd/server user create falah engineer
```

Di `run-dev`, transaction ID yang mengandung `err` menghasilkan log palsu; selain itu hasilnya `NO_LOGS`. Sesi Splunk bisa dibuat kedaluwarsa dengan `curl -X POST 127.0.0.1:8089/fake/expire` (pulihkan dengan `/fake/restore`).

### Kontrak API

Spec OpenAPI di-generate dari tabel route Go (`internal/api`), lalu tipe TypeScript di-generate dari spec:

```sh
make openapi     # apps/server/api/openapi.json → apps/web/src/lib/api-schema.d.ts
```

Spec juga tersedia di `GET /api/openapi.json`. Jalankan `make openapi` setiap kali request/response atau route berubah.

## Arsitektur singkat

- **Worker** (`internal/findbugs/worker`): satu job sekaligus, `QUEUED → CHECKING_VPN → SEARCHING → ANALYZING → DONE / NO_LOGS / FAILED`. Antrean dan status tersimpan di SQLite, jadi restart tidak menghilangkan job yang masih antre; job yang sedang berjalan saat restart ditandai `FAILED`.
- **Watchdog** (`internal/findbugs/watchdog`): setiap 60 detik cek VPN (`show --status` + reachability) dan sesi Splunk. Kalau VPN putus, job menunggu di `WAITING_VPN` (expired setelah 30 menit). Kalau sesi Splunk habis, re-auth otomatis dicoba sekali; kalau gagal, antrean dijeda (`WAITING_SPLUNK`). Job yang berjalan lebih dari 5 menit dicek ulang dan langsung `FAILED` kalau VPN atau Splunk ternyata putus.
- **VPN** (`internal/vpn`): port dari `poc-gp-web`, tanpa mode `stop-first`. Siapa pun yang login website bisa Connect; sesinya dipakai bersama.
- **Analyzer** (`internal/findbugs/analyzer`): `claude -p` dengan semua tool dimatikan, tanpa MCP/settings, prompt lewat stdin, dijalankan di direktori kosong. Log sudah diredaksi sebelum dikirim, disimpan, atau ditampilkan.
- **Notifikasi**: banner di web, plus pesan satu arah ke grup Telegram (opsional).

## Deploy (VM Debian/Ubuntu)

Semua langkah dijalankan sebagai user Linux pemilik sesi GlobalProtect (bukan root).

1. **Paket dan setup GlobalProtect** seperti di `poc-gp-web` (capture browser):

   ```sh
   sudo apt install xdg-utils libglib2.0-bin xvfb sqlite3 python3-venv
   sudo loginctl enable-linger "$USER"
   mkdir -p ~/.gp-web ~/.local/share/applications ~/.config/systemd/user ~/.config/findbugs
   cp deploy/capture-url.sh ~/.gp-web/ && chmod +x ~/.gp-web/capture-url.sh
   sed "s|<user>|$USER|" deploy/gp-capture.desktop > ~/.local/share/applications/gp-capture.desktop
   xdg-settings set default-web-browser gp-capture.desktop
   ```

2. **Aplikasi** di `~/findbugs` (build dengan `make build` di mesin build, lalu salin `bin/findbugs`, `scripts/`, `deploy/`):

   ```sh
   cd ~/findbugs
   python3 -m venv .venv && .venv/bin/pip install -r scripts/splunk-login/requirements.txt
   .venv/bin/playwright install --with-deps chromium
   npm install -g @anthropic-ai/claude-code    # CLI analyzer
   ```

3. **Konfigurasi**: salin `.env.example` ke `~/.config/findbugs/findbugs.env`, isi, lalu `chmod 600`. Isinya termasuk `ANTHROPIC_API_KEY`, kredensial SSO Splunk, dan token Telegram.

4. **User pertama**:

   ```sh
   set -a; . ~/.config/findbugs/findbugs.env; set +a
   ./bin/findbugs user create falah engineer
   ```

5. **Service**:

   ```sh
   cp deploy/findbugs.service deploy/findbugs-backup.* ~/.config/systemd/user/
   systemctl --user daemon-reload
   systemctl --user enable --now findbugs findbugs-backup.timer
   journalctl --user -u findbugs -f
   ```

6. **Cloudflare Tunnel** ke `127.0.0.1:8080` (lihat `deploy/cloudflared-config.example.yml`). Disarankan pasang Cloudflare Access di depan subdomain. Server hanya listen di loopback.

### Recovery manual

Kalau daemon GlobalProtect nyangkut di "Retrieving configuration..." (semua Connect ditolak):

```sh
sudo systemctl restart gpd && systemctl --user restart gpa
```
