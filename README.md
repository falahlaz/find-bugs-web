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

- **Fase 1 (pipeline headless):** selesai. Semua endpoint API jalan dan teruji.
- **Fase 2 (website QA):** selesai. Login, form submit (transaction ID/curl, environment, 24h/48h), deteksi duplikat 24 jam dengan "Jalankan ulang", halaman job dengan progres per tahap (polling 2,5 detik), hasil QA vs laporan engineer, batalkan job yang masih antre, histori dengan filter, dan notifikasi browser saat job selesai.
- **Fase 3 (panel VPN, Splunk, Engineer):** selesai. Halaman Koneksi (`/koneksi`) menggabungkan VPN dan Splunk berurutan: VPN untuk semua user (Connect, link login SSO, paste callback, Disconnect, status nyata, log diredaksi), lalu Splunk (status sesi, Re-auth dengan status menunggu 2FA, output login terakhir), manajemen user dan audit log untuk Engineer.
- **Revamp UI:** tampilan ala console triase. Daftar investigasi selalu ada di samping detail job (cari, chip status, filter environment/tanggal, "Milik saya" untuk engineer) dengan health strip VPN/Splunk/antrean di atasnya; detail job berisi stepper pipeline, diagnosis AI, laporan engineer, dan log viewer dengan filter. Ctrl/⌘+K membuka command palette. Di HP: daftar → detail layar penuh dengan tab bar bawah. Token warna/font ada di `apps/web/src/index.css`.
- **Fase 4 (deploy & cut-over):** ikuti bagian Deploy di bawah, lalu uji di VM dengan GlobalProtect, Splunk, dan Claude Code asli.

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
- **Splunk** (`internal/findbugs/splunk`): REST API lewat sesi SSO yang disimpan login script. Setiap request HTTP (buat job, cek status, ambil hasil) dibatasi `SPLUNK_REQUEST_TIMEOUT` detik (default 300 = 5 menit); kalau Splunk/VPN tidak merespons dalam batas itu, job `FAILED` dengan `Client.Timeout exceeded`. Lama tunggu sampai search job selesai diatur terpisah lewat `SPLUNK_RESULT_WAIT_TIMEOUT`.
- **VPN** (`internal/vpn`): port dari `poc-gp-web`, tanpa mode `stop-first`. Siapa pun yang login website bisa Connect; sesinya dipakai bersama.
- **Correlation** (`internal/findbugs/correlation`): beberapa service (mis. service-payment-migration) tidak mencatat log dengan transaction ID dari header, tapi dengan `_id` buatan sendiri. Keduanya hanya bertemu di event `"API Request"` (`data._id` + `data.mobileapptransactionid`). Setelah search pertama, worker mencari setiap `_id` yang terhubung (satu per percobaan/retry client, maks `SPLUNK_CORRELATION_MAX_IDS`, default 5, 0 = mati), lalu menggabungkan hasilnya dengan log search pertama (tanpa duplikat, urut waktu). Link harus searah (field non-`_id` = ID yang dicari, `_id` beda), jadi search dengan `_id` backend tidak memicu search tambahan. `_id` divalidasi ulang sebelum masuk SPL. ID yang diikuti tampil di halaman job dan di header file log untuk analyzer. Diport dari branch `feat/correlation-id-chaining` di find-bugs-bot.
- **Analyzer** (`internal/findbugs/analyzer`): semua event hasil search (maks `MAX_LOG_LINES`, urut lama → baru) ditulis ke `data/logs/job-<id>.log` (0600, dihapus setelah `RETENTION_RAW_LOG_DAYS`). `claude -p` dijalankan di direktori sementara yang hanya berisi salinan file itu sebagai `logs.txt`, dengan tool `Read`/`Grep`/`Glob` saja dan `--permission-mode dontAsk`, jadi akses di luar direktori itu ditolak. Tanpa MCP/settings, prompt lewat stdin. Log sudah diredaksi sebelum ditulis, dikirim, atau ditampilkan.
- **Code trace** (`internal/findbugs/repos`, `gitlab`, `analyzer/trace.go`, `tracechat`, opsional): kalau diagnosis `error_source = internal`, worker mengambil container dan namespace Kubernetes dari field `source` Splunk (`/var/log/pods/<ns>_<pod>_<uid>/<container>/0.log`). Container dipetakan ke project GitLab `GITLAB_GROUP/<container>` (atau `GITLAB_REPO_MAP`). Namespace dipetakan ke environment GitLab lewat `GITLAB_ENV_MAP` (default `tdw-dev→dev`, `tdw-staging→staging`, `tdw-preprod→preprod`, `blue→blue`, `tdw-webapi→production`), lalu commit yang dipakai adalah job `deploy-eks:<env>` sukses terakhir **sebelum waktu error** (GitLab Deployments API). Kalau namespace tidak dikenal, deployment tidak ketemu, atau GitLab error, trace jatuh ke branch `GITLAB_REF` dan UI menyebut alasannya. Tiap commit di-checkout sebagai git worktree sendiri di `GITLAB_REPOS_DIR/.worktrees/<group>/<project>@<sha>` (object store shallow per project di `GITLAB_REPOS_DIR/<group>/<project>`), jadi checkout tidak berubah selama dibaca. Token dan setting TLS dikirim lewat env proses git (`GIT_CONFIG_*`) dan header API, tidak pernah tersimpan di `.git/config`, URL remote, atau argumen proses. Lalu `claude -p` (model `CODE_TRACE_MODEL`, default Opus) dijalankan dalam **sesi yang disimpan** (`--session-id`, folder kerja `data/trace-sessions/job-<id>`) dengan `--add-dir` ke checkout (tetap `Read`/`Grep`/`Glob` saja) untuk menunjuk file:line, potongan kode, dan penjelasan. Gagal clone atau trace tidak menggagalkan job. Butuh VPN; matikan dengan `CODE_TRACE_ENABLED=false`.
  - **Chat dan trace ulang** (engineer saja, satu sesi per job): di kartu trace, engineer bisa bertanya lanjutan (`claude -p --resume`, progres tool tampil saat menjawab) atau klik **Cek versi lain** untuk trace ulang di commit yang sedang ter-deploy di environment lain, atau di branch/tag/commit apa pun; tiap versi tampil sebagai tab. Tiap pesan adalah proses CLI singkat, jadi sesi yang diam tidak memakai RAM/CPU. Sesi tampil tertutup setelah `TRACE_SESSION_IDLE` (10m) tanpa pesan atau lewat tombol **Tutup sesi**, dan bisa dibuka lagi dengan konteks utuh. File sesi Claude dihapus setelah `TRACE_SESSION_RETENTION_DAYS` (30) tidak aktif; membukanya lagi memulai sesi baru dengan ringkasan percakapan. Maksimal `TRACE_CHAT_CONCURRENCY` (2) jawaban berjalan bersamaan. Worktree yang tidak dipakai sesi mana pun dihapus setelah `GITLAB_WORKTREE_TTL_DAYS` (14).
- **Repo** (`/repos`, engineer saja): daftar repo di `GITLAB_REPOS_DIR` (branch, commit terakhir, shallow atau tidak). **Clone** membuat full clone branch default di `<dir>/<group>/<project>` di belakang layar (token lewat env proses git, sama seperti trace); trace yang nanti memakai repo itu tidak membuatnya shallow. **Mulai sesi** menjalankan script skill `rc-session` (`RC_SESSION_SCRIPT`) untuk `claude rc` di tmux dan menampilkan link claude.ai; **Stop** mematikan sesi tmux-nya. Script dijalankan dengan env minimal (tanpa token dari `findbugs.env`) di systemd user scope sendiri, jadi sesi tetap hidup saat service di-restart. Semua aksi masuk audit log.
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

3. **Konfigurasi**: salin `.env.example` ke `~/.config/findbugs/findbugs.env`, isi, lalu `chmod 600`. Isinya termasuk `ANTHROPIC_API_KEY`, kredensial SSO Splunk, token Telegram, dan (opsional) token GitLab untuk code trace (scope `read_repository` dan `read_api`, untuk membaca deployment).

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
