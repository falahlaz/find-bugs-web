# Find Bugs Web — PRD

v2 · Oct 1, 2026 · @Falah

## Ringkasan

Find Bugs Web menggantikan Telegram bot `find-bugs-bot` dengan website yang berjalan di VM Debian/Ubuntu headless (GCP), dan memakai mekanisme dari `poc-gp-web` supaya server bisa tersambung ke GlobalProtect sendiri tanpa laptop engineer.

- **Masalah sekarang:** bot hanya jalan di Mac engineer (launchd), butuh GlobalProtect aktif di laptop itu, dan QA berinteraksi lewat chat Telegram.
- **Yang dibangun:** QA submit transaction ID atau curl lewat form web, melihat progres dan hasil diagnosis di browser. Semua user yang login bisa menyambungkan VPN dan memulihkan sesi Splunk dari dashboard yang sama; Engineer melihat laporan teknis lengkap dan mengelola user.
- **Yang sudah terbukti:** `poc-gp-web` berhasil connect ke GlobalProtect 6.1.4 di VM headless pada 2026-10-01 (status Connected, `gpd0` UP, reachability OK). Pipeline Splunk → LLM di `find-bugs-bot` v2.0.0 sudah berjalan lewat REST API, tanpa browser saat runtime.

**Sukses berarti:** QA bisa men-submit investigasi dan menerima diagnosis tanpa engineer membuka laptop, dan siapa pun yang sedang memakai website cukup login SSO dari browser saat sesi VPN atau Splunk kedaluwarsa.

## Keputusan yang sudah diambil

| Topik | Keputusan |
| --- | --- |
| Izin VPN di VM GCP | Boleh. Tidak ada HIP check, tidak ada isu lisensi client Linux |
| Hosting | VM GCP milik @Falah, 4 vCPU / 8 GB, domain `arunoir.space` (subdomain mis. `findbugs.arunoir.space`) |
| Akses publik | Cloudflare Tunnel (`cloudflared`), tanpa port terbuka. Disarankan Cloudflare Access di depan subdomain |
| Repo | Monorepo `find-bugs-web`, FE dan BE dipisah per folder. `find-bugs-bot` dan `poc-gp-web` dibiarkan apa adanya |
| Database | SQLite (WAL) di balik interface `store`; pindah ke Postgres hanya kalau perlu multi-instance |
| Real-time | Polling 2–3 detik via TanStack Query. SSE ditunda |
| Password hash | bcrypt |
| Kontrak API | OpenAPI di-generate dari kode Go, lalu tipe TypeScript di-generate dari spec |
| LLM | Claude Code headless (`claude -p`) di server, model Haiku 4.5, di balik interface `Analyzer` supaya bisa diganti |
| Notifikasi | Banner + browser notification di web, plus push satu arah ke grup Telegram via Bot API |
| VPN | Satu sesi GlobalProtect untuk seluruh server. Siapa pun yang login website boleh Connect; sesi dipakai bersama semua user |
| Splunk | Satu akun SSO dari `.env` (password disimpan di server). Push 2FA selalu ke HP pemilik akun |
| Secret | File permission `600`, tanpa secret manager |
| Retensi | Log mentah 7 hari, hasil diagnosis 6 bulan, metadata job selamanya |
| Bootstrap admin | Lewat CLI (`server user create --role engineer`) |
| Pengguna | 4 orang; pilot 1–2 QA di Fase 2; approver cut-over @Falah |

## Assessment: find-bugs-bot

Logika inti (parser, Splunk API, SQLite) siap di-port ke Go; yang harus diganti adalah lapisan Telegram, cara deploy macOS, provider LLM, dan login SSO Splunk yang butuh jendela browser.

Python 3.11, asyncio, tanpa framework web, tanpa test dan CI. Pipeline: input → `parser/curl_parser.py` → `jobqueue/job_queue.py` (FIFO, 1 worker, maks 10, maks 3 per chat) → `scraper/vpn_check.py` (TCP probe `VPN_CHECK_HOST:443`) → `scraper/splunk_scraper.py` + `splunk_api.py` (REST via `/en-US/splunkd/__raw/`, cookie sesi + CSRF) → `analyzer/llm_analyzer.py` (OpenAI JSON mode, redaksi bearer/password/email) → `bot/formatter.py` → `storage/database.py`.

| Modul | Nasib di website | Catatan |
| --- | --- | --- |
| `parser/curl_parser.py`, validasi transaction ID di `bot/handler.py` | Port ke Go | Regex `[A-Za-z0-9\-_.]{1,128}` dan deteksi curl dipindah ke layer service |
| `scraper/splunk_api.py`, `splunk_scraper.py` | Port ke Go | Template SPL per environment dari `SPLUNK_SPL_TEMPLATES` (tanpa filter `NOT kong`, karena ikut menyembunyikan log yang dibutuhkan), time range 24h/48h. Nama cookie di-hardcode dengan suffix port `_8008` |
| `analyzer/llm_analyzer.py` | Diganti Claude Code (Haiku) | Prompt dan skema output dipertahankan: summary, error\_type, failed\_component, likely\_cause, severity, suggested\_action, relevant\_logs, error\_source |
| `jobqueue/job_queue.py` | Port ke Go, diubah | Antrean dan status job tersimpan di SQLite |
| `storage/database.py` | Port ke Go, diubah | `requester_chat_id` diganti user ID; tambah status job dan timestamp per tahap |
| `bot/handler.py`, `bot/formatter.py` | Diganti React + API | Format laporan engineer vs QA dipertahankan, termasuk label `error_source` (ESB/TIBCO/Internal) di laporan QA |
| `main.py` | Diganti | Retry VPN 3× 60 detik dan pesan Telegram diganti status job, watchdog, banner, dan notifikasi grup |
| `save_session_auto.py` | Tetap Python, diubah | Sekarang dipanggil in-process (`auto_login_async`); di website dipanggil lewat `exec` + `xvfb-run`. Script mencetak email dan awalan CSRF ke stdout, jadi output harus di-redact atau tidak di-log |
| `scraper/browser.py`, `save_session.py`, `sso_scraper.py`, `splunk_inspector.py` | Tidak dibawa | Legacy/alat bantu dev |
| `botctl`, `com.findbugs.bot.plist.template`, `start.sh` | Diganti | launchd macOS → systemd di Debian/Ubuntu |
| `openai-proxy/` | Tidak dipakai | LLM pindah ke Claude Code |

Temuan yang memengaruhi desain:

- **Re-auth Splunk butuh display.** `save_session_auto.py` membuka Chromium `headless=False`. Di server dijalankan lewat Xvfb, dan Splunk hanya bisa diakses saat VPN tersambung.
- **Kredensial SSO tersimpan di `.env`** (`SPLUNK_SSO_PASSWORD`) dan harus diperlakukan sebagai secret.
- **TLS verify menyala secara default.** `LLM_SKIP_SSL_VERIFY` dan `SPLUNK_SKIP_SSL_VERIFY` default `false` (commit `2ebdb7a`); opsi skip dipertahankan hanya untuk Splunk.
- **Redaksi sekarang hanya untuk input LLM.** Log mentah disimpan dan ditampilkan apa adanya. Di website, redaksi berlaku juga sebelum disimpan dan ditampilkan (perilaku baru).
- **Potongan log mentah di laporan engineer = 3000 karakter terakhir** (`formatter.py`), pesan error LLM dipotong 500 karakter.
- **Akses berbasis chat ID Telegram** diganti autentikasi web dengan role.

## Assessment: poc-gp-web

POC Go tanpa dependency ini sudah membuktikan alur VPN di GlobalProtect 6.1.4, dan `Manager` di `gp.go` bisa dibawa hampir apa adanya; yang perlu dibuang adalah mode `stop-first` dan asumsi "hanya diakses via SSH tunnel".

Alur yang terbukti (skenario 1 dan 2 di README, 2026-10-01):

1. Web app spawn `globalprotect connect --portal <portal>` di process group sendiri, dengan `BROWSER=capture-url.sh` dan `DISPLAY=:0`.
2. `gpshow.sh` memanggil browser palsu dengan path lokal `~/GP_HTML/saml.html`. Web app menyajikannya di `/saml-login`, berupa form POST auto-submit ke IdP.
3. User login SSO di browser laptop, lalu copy link `globalprotectcallback:...` dan paste ke form.
4. Web app menjalankan `globalprotect defaultbrowser <uri>`, yang menulis `~/GP_HTML/defaultbrowser/resp.html` untuk dibaca PanGPA. Proses `connect` harus tetap hidup.

| Bagian | Nasib | Catatan |
| --- | --- | --- |
| `gp.go` `Manager` (state machine, `opMu`, spawn/stop by PID, `Status`) | Pakai ulang | State `IDLE → CONNECTING → WAITING_CALLBACK → SUBMITTING → CONNECTED/FAILED` |
| `redact.go` | Pakai ulang | Redaksi `token`, `prelogin-cookie`, `portal-userauthcookie` |
| `handlers.go` endpoint `/api/*`, `/saml-login` | Pakai ulang, diubah | Dipindah ke API backend, di belakang auth (semua role) |
| `guard()` (Host loopback + wajib JSON) | Diganti | Middleware auth dan CSRF |
| Mode `stop-first` | Dibuang | Membuat daemon nyangkut di "Retrieving configuration..." |
| `deploy/gp-web.service`, `capture-url.sh`, `gp-capture.desktop` | Pakai ulang | systemd user unit + `loginctl enable-linger` |
| `web/index.html` | Diganti | Fitur `vpn` di React |
| `testdata/fake-globalprotect.sh`, `gp_test.go` | Pakai ulang | Uji tanpa GlobalProtect asli |

Batasan yang harus dibawa ke desain:

- **Satu sesi, satu user Linux.** CLI menolak ("already established") selama ada proses `globalprotect` lain milik user yang sama. Kalau dua orang menekan Connect bersamaan, yang kedua mendapat 409; UI menampilkan siapa yang sedang menyambungkan.
- **Recovery manual.** Kalau daemon nyangkut: `sudo systemctl restart gpd && systemctl --user restart gpa`. Butuh sudo, jadi tidak dipicu dari web.
- **Skenario 4–6 belum diuji:** token kedaluwarsa, disconnect saat Connected, dan reconnect tanpa restart `gpd`.
- **State CONNECTED diambil dari exit code `defaultbrowser`**, bukan status nyata. Status nyata dari `show --status`, interface `gpd*`, dan reachability; ini menjadi satu-satunya sumber status VPN (menggantikan `VPN_CHECK_HOST`).
- **GP sendiri menulis callback ke disk** (`resp.html`); aplikasi tidak boleh menulis callback ke disk atau log.
- **`DISPLAY=:0` dipakai GP**; Xvfb untuk login Splunk harus memakai display lain (default `xvfb-run` `:99`).

## Scope dan non-goals

| Masuk scope (v1) | Di luar scope (v1) |
| --- | --- |
| Login web dengan role QA dan Engineer | SSO korporat untuk login ke website |
| Submit investigasi: environment, time range, transaction ID atau curl | Integrasi Jira/ClickUp, auto-create tiket |
| Antrean job, progres per tahap, cancel job yang masih antre | Banyak worker paralel, banyak akun Splunk |
| Laporan engineer lengkap + ringkasan QA, histori dengan filter | Analitik/dashboard statistik |
| Panel VPN dan sesi Splunk untuk semua user | Auto-reconnect VPN tanpa interaksi manusia |
| Watchdog koneksi dan notifikasi Telegram satu arah | Bot Telegram interaktif |
| Deploy di satu VM dengan systemd dan Cloudflare Tunnel | Multi-server, Kubernetes, HA, tombol recovery `gpd` |

Telegram bot interaktif dihentikan setelah website dipakai; yang tersisa hanya pengiriman notifikasi ke grup.

## User flow dan kebutuhan fungsional

**Alur QA:** login → pilih environment dan time range → paste transaction ID atau curl → Submit → lihat posisi antrean dan tahap berjalan → baca ringkasan hasil. Kalau banner menunjukkan VPN putus, QA bisa langsung klik Connect dan login SSO sendiri.

**Alur Engineer:** sama seperti QA, ditambah laporan lengkap, histori semua user, manajemen user, dan audit log.

| ID | Kebutuhan | Role | Prioritas |
| --- | --- | --- | --- |
| FR-01 | Login username + password, sesi cookie, logout. Akun dibuat Engineer; akun pertama dibuat lewat CLI | Semua | P0 |
| FR-02 | Form submit: environment dari key `SPLUNK_SPL_TEMPLATES`, time range 24h/48h, transaction ID atau curl, validasi sama dengan bot | Semua | P0 |
| FR-03 | Antrean FIFO 1 worker, maks 10 job aktif total, maks 3 per user (job `QUEUED`, `WAITING_*`, dan yang berjalan dihitung), pesan jelas saat penuh | Sistem | P0 |
| FR-04 | Status job `QUEUED → CHECKING_VPN → SEARCHING → ANALYZING → DONE / NO_LOGS / FAILED / CANCELLED / EXPIRED`, plus `WAITING_VPN` / `WAITING_SPLUNK`. Update via polling 2–3 detik | Semua | P0 |
| FR-05 | Hasil QA: severity, ringkasan, label sumber error (ESB/TIBCO/Internal) dan pesan sesuai sumber. Hasil Engineer: semua field diagnosis termasuk `relevant_logs`, potongan log mentah (3000 karakter terakhir, sudah diredaksi), fallback log mentah kalau LLM gagal | QA / Engineer | P0 |
| FR-06 | Histori dengan filter environment, status, tanggal, transaction ID. QA hanya melihat miliknya | Semua | P1 |
| FR-07 | Panel VPN: Connect, link `/saml-login`, form callback, Disconnect, status (`gpStatus`, interface, reachability, umur koneksi, siapa yang menyambungkan), log redacted | Semua | P0 |
| FR-08 | Saat VPN putus: job yang belum mulai menjadi `WAITING_VPN` dan worker berhenti mengambil job; banner tampil ke semua user dan notifikasi dikirim ke grup Telegram. Setelah VPN tersambung, job dilanjutkan. Job `WAITING_VPN` menjadi `EXPIRED` setelah 30 menit | Sistem | P0 |
| FR-09 | Panel sesi Splunk: umur sesi, tombol Re-auth (auto-login, tunggu push 2FA ke HP pemilik akun, maks 5 menit), status hasil | Semua | P0 |
| FR-10 | Saat sesi Splunk kedaluwarsa: coba auto re-auth sekali (hanya kalau VPN Connected), kalau gagal antrean dijeda (`WAITING_SPLUNK`), banner + notifikasi Telegram | Sistem | P0 |
| FR-11 | Re-run investigasi dari histori dengan parameter yang sama | Engineer | P2 |
| FR-12 | Manajemen user: tambah, nonaktifkan, reset password, ubah role | Engineer | P1 |
| FR-13 | Audit log aksi VPN dan Splunk (siapa, kapan, hasil) | Sistem | P1 |
| FR-14 | Watchdog koneksi: health check VPN (`show --status`, interface `gpd*`, reachability) dan sesi Splunk setiap 60 detik. Kalau job berjalan lebih dari 5 menit, worker memeriksa ulang koneksi: kalau sesi GP atau Splunk habis, job langsung `FAILED` dengan alasan jelas, banner + notifikasi muncul, dan user diarahkan untuk login ulang | Sistem | P0 |
| FR-15 | VPN atau Splunk putus saat job sedang berjalan: job langsung `FAILED` (tanpa retry otomatis); user bisa submit ulang | Sistem | P0 |
| FR-16 | Submit duplikat (transaction ID + environment + time range sama, status `DONE` dalam 24 jam) menampilkan hasil sebelumnya, dengan tombol "Jalankan ulang" | Semua | P1 |
| FR-17 | Cancel job milik sendiri selama masih `QUEUED` atau `WAITING_*`; Engineer bisa cancel job siapa pun | Semua | P1 |
| FR-18 | Notifikasi: browser notification saat job milik user selesai (tab terbuka); push ke grup Telegram via Bot API untuk VPN putus, sesi Splunk kedaluwarsa, job gagal karena sistem, dan service down | Sistem | P1 |

## Kebutuhan non-fungsional dan keamanan

| ID | Kebutuhan |
| --- | --- |
| NFR-01 | Website diakses lewat Cloudflare Tunnel (HTTPS). Tidak ada port aplikasi yang terbuka langsung; disarankan Cloudflare Access di depan subdomain |
| NFR-02 | Website, Claude API, dan Telegram tetap bisa diakses saat GlobalProtect Connected. Wajib diuji (lihat Risiko) |
| NFR-03 | Semua proses jalan sebagai satu user Linux non-root pemilik sesi GlobalProtect, sebagai systemd user unit dengan `loginctl enable-linger` |
| NFR-04 | Secret (`ANTHROPIC_API_KEY`, `SPLUNK_SSO_PASSWORD`, token Telegram, file sesi Splunk) di file permission `600`, tidak pernah tampil di UI atau log. Stdout script login Splunk diredaksi |
| NFR-05 | Redaksi berlapis: token GP (`redact.go`) dan token/password/email di log Splunk sebelum disimpan, ditampilkan, atau dikirim ke LLM |
| NFR-06 | Semua endpoint di belakang auth dengan proteksi CSRF. Aplikasi tidak menulis callback URI ke disk atau log |
| NFR-07 | Password di-hash bcrypt, sesi kedaluwarsa setelah 12 jam, rate limit login |
| NFR-08 | Antrean dan status job tersimpan di SQLite. Saat restart, job yang sedang berjalan ditandai `FAILED` ("server restart") dan search job Splunk yang tertinggal dibersihkan; job `QUEUED` tetap di antrean |
| NFR-09 | Log aplikasi ke journald dan file rotasi 5 MB × 3 |
| NFR-10 | Waktu tampil di `TIMEZONE` (default `Asia/Jakarta`). Target waktu proses satu job ≤ 60 detik di luar waktu antre |
| NFR-11 | Test untuk parser, state machine VPN (pakai `fake-globalprotect.sh`), dan alur job dengan Splunk/LLM di-mock |
| NFR-12 | Claude Code dijalankan tanpa tool (tanpa Bash, tanpa akses file), `--max-turns 1`, input lewat stdin, output JSON divalidasi terhadap skema. Auth dengan `ANTHROPIC_API_KEY`, bukan login langganan pribadi |
| NFR-13 | Retensi: log mentah dihapus setelah 7 hari, field diagnosis setelah 6 bulan, metadata job disimpan. Job pembersihan harian |
| NFR-14 | Monitoring: endpoint `/healthz` (DB, worker, status VPN), uptime check eksternal, alert ke grup Telegram. Backup harian SQLite |

## Arsitektur target

Satu backend Go yang memuat pipeline hasil port dari `find-bugs-bot` dan package VPN dari `poc-gp-web`, plus frontend React yang di-build lalu disajikan oleh backend yang sama (satu origin, tanpa CORS). Bagian non-Go: script login SSO Splunk (Python + Playwright) dan Claude Code CLI, keduanya dipanggil backend lewat `exec`. Website ini disiapkan sebagai platform internal untuk fitur lain setelah Find Bugs, jadi struktur kode dibuat per fitur sejak awal.

```text
Browser ──HTTPS──> Cloudflare ──tunnel──> cloudflared ──> Go server (127.0.0.1)
                                                         ├─ REST API + static React
                                                         ├─ worker ── Splunk (via gpd0)
                                                         │         └─ exec claude -p (Haiku)
                                                         ├─ vpn.Manager ── globalprotect CLI
                                                         ├─ exec xvfb-run save_session_auto.py
                                                         └─ notifier ── Telegram Bot API
```

Semua proses berjalan sebagai systemd user unit milik satu user Linux.

### Tech stack

| Lapisan | Pilihan | Catatan |
| --- | --- | --- |
| Backend | Go 1.24, `net/http` standar | Router pattern bawaan, satu binary, tanpa CGO |
| Database | SQLite via `modernc.org/sqlite`, mode WAL | Migrasi SQL embed, akses lewat interface `store` |
| VPN | Package `vpn` dari `gp.go` + `redact.go` | Tanpa mode `stop-first` |
| Splunk | Port `splunk_api.py` ke `net/http` | Cookie + header `X-Splunk-Form-Key`; redirect SSO/401/403 = sesi kedaluwarsa |
| Login Splunk | `save_session_auto.py` lewat `xvfb-run` | Kontrak: menulis `splunk_api_session.json`, exit code 0/1, backend me-reload |
| LLM | Claude Code CLI headless, model `claude-haiku-4-5-20251001` | Di balik interface `Analyzer`; bisa diganti Claude API langsung atau OpenAI |
| Frontend | React + TypeScript + Vite | Build disajikan oleh Go; dev server Vite mem-proxy `/api` |
| Routing & data | React Router, TanStack Query | Polling status job, VPN, Splunk |
| UI | Tailwind CSS + shadcn/ui | Form, tabel, dialog, badge status |
| Kontrak API | OpenAPI di-generate dari kode Go, tipe TS di-generate dari spec | Generator dipilih di Fase 1 |
| Notifikasi | Telegram Bot API (`sendMessage` ke satu grup) + Web Notifications API | Satu arah, tanpa handler |
| Auth | Session cookie `HttpOnly`, `SameSite=Lax`, bcrypt, header CSRF | Role QA dan Engineer |

### Struktur monorepo

```text
apps/server/
  cmd/server/            # main, wiring, subcommand CLI (user create)
  internal/platform/     # auth, users, audit, notifier, config, store
  internal/vpn/          # dari poc-gp-web
  internal/findbugs/     # parser, splunk, analyzer, jobs, worker, watchdog
apps/web/
  src/app/               # shell: layout, navigasi, auth
  src/features/findbugs/
  src/features/vpn/
  src/features/splunk/
scripts/splunk-login/    # save_session_auto.py
deploy/                  # systemd units, cloudflared config
docs/
```

## Rencana implementasi per fase

| Fase | Isi | Selesai kalau |
| --- | --- | --- |
| 0. Validasi risiko | Uji skenario 4–6 `poc-gp-web`. Cek full vs split tunnel (`ip route`, `curl ifconfig.me`). Uji `cloudflared`, `api.anthropic.com`, dan `api.telegram.org` saat VPN Connected. Jalankan `save_session_auto.py` lewat Xvfb di VM. Uji `claude -p` dengan Haiku pada contoh log | Semua uji punya hasil tertulis |
| 1. Pipeline headless | Scaffold monorepo. Port parser, Splunk API, queue ke Go; analyzer Claude Code; pindahkan `gp.go` ke `internal/vpn`. Skema SQLite (users, jobs, investigations, audit). Queue persisten, watchdog, CLI buat user | Job bisa dibuat lewat API dan menghasilkan diagnosis tersimpan |
| 2. Website QA | Shell React, client dari OpenAPI, login, form submit, halaman job dengan polling, hasil QA, histori sendiri, cancel, dedup. Pilot 1–2 QA | QA bisa submit dan menerima hasil end-to-end |
| 3. Panel VPN, Splunk, Engineer | Panel VPN dan Splunk untuk semua user, laporan lengkap, histori semua user, manajemen user, audit log, notifikasi Telegram | Siapa pun bisa memulihkan VPN dan sesi Splunk dari browser |
| 4. Deploy & cut-over | systemd units, Cloudflare Tunnel + Access, backup harian, retensi, monitoring, impor data lama (kalau diputuskan), matikan bot | QA (4 orang) memakai website selama 1 minggu tanpa bot, disetujui @Falah |

## Risiko

| Risiko | Dampak | Mitigasi |
| --- | --- | --- |
| VPN full tunnel: trafik `cloudflared`, Claude API, dan Telegram ikut lewat VPN korporat | Website, analisis, atau notifikasi mati saat VPN tersambung | Uji di Fase 0. Opsi: policy routing untuk interface publik, atau split tunnel dari portal |
| Re-auth Splunk butuh push 2FA ke HP pemilik akun | Job tertahan kalau pemilik akun tidak merespons | Banner "menunggu 2FA", timeout 5 menit, notifikasi Telegram |
| Microsoft login mendeteksi Xvfb/otomasi | Auto-login gagal | Fallback: login manual lewat sesi browser yang di-stream |
| Daemon GP nyangkut di "Retrieving configuration..." | Semua connect ditolak, butuh sudo | Runbook manual; opsional sudoers terbatas untuk dua perintah restart |
| Sesi VPN dan Splunk memakai akun SSO perorangan | Aktivitas tercatat atas nama orang yang login | Audit log di website mencatat siapa memakai sesi itu |
| Claude Code lebih lambat dari API langsung | Target 60 detik terlewati | Ukur di Fase 0; ganti implementasi `Analyzer` ke Claude API kalau perlu |
| Antrean 1 worker | QA menunggu saat banyak bug | Cukup untuk 4 user; evaluasi setelah cut-over |

## Open questions

- [ ] Fitur apa yang akan ditambahkan setelah Find Bugs, dan apakah perlu role selain QA dan Engineer?
- [ ] Berapa lama sesi GlobalProtect bertahan sebelum minta login ulang, dan berapa sering sesi Splunk kedaluwarsa? (diukur setelah jalan)
- [ ] VPN full tunnel atau split tunnel? (dicek di Fase 0)
- [ ] Apakah data `investigations.db` lama perlu diimpor? Kalau ya, `requester_chat_id` dipetakan ke user mana?
