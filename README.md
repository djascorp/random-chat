# RandomChat — Random Video Chat

Random one-to-one video chat in the spirit of Omegle: open the page, allow the
camera, get matched with someone available, talk, then click Next.

This repository holds the MVP: **Go + native WebRTC + React**, with a modular
architecture designed to grow (multi-instance, SFU) without a rewrite.

---

## 1. What works

| Area | Status |
| --- | --- |
| Random matching (queue, Next, cancel, expiry) | ✅ |
| 1:1 peer-to-peer video calls (WebRTC, STUN, TURN fallback) | ✅ |
| Server-validated WebSocket signaling | ✅ |
| Text chat during the session | ✅ |
| Reports, blocks, automatic suspensions | ✅ |
| Per-user and per-IP rate limiting | ✅ |
| Metrics (`/metrics`, `/api/stats`) and audit log | ✅ |
| Token-protected admin API | ✅ |
| Group conferences (Solo/Group search, member cap, Ajouter button, mesh) | ✅ |
| Invitation links (solo→group promotion, pre-set `?mode=&max=` links, revocable) | ✅ |
| PostgreSQL persistence with in-memory fallback | ✅ |
| Responsive UI with dark theme, mobile included | ✅ |
| Unit tests, WebSocket integration tests, `-race` | ✅ |

Deliberately out of scope for this MVP: social accounts, virtual gifts,
advanced filters, session recording, SFU.

---

## 2. Architecture

```text
Browser A  ⇄  Go server (signaling, matching, moderation)  ⇄  Browser B
      └────────────────── direct WebRTC media ────────────────────┘
                    (relayed by TURN only when needed)
```

- **The server never sees video.** It exchanges SDP offers/answers and ICE
  candidates, picks peers, enforces rules, and closes sessions. Solo calls are
  1:1; conferences are a full mesh (one `RTCPeerConnection` per pair, offers
  resolved by identifier order), still relayed by TURN only when needed.
- **Matching lives in memory**, serialized by a single lock: this guarantees a
  user can only belong to one session, even when two clicks arrive at the
  same instant.
- **PostgreSQL stays off the hot path**: audit writes go to a goroutine,
  signaling never blocks on the database.
- **No Redis needed for the MVP.** When several Go instances run in parallel,
  the queue and the sessions are the only two components to move behind an
  interface.

### Tech stack

| Layer | Choice |
| --- | --- |
| Frontend | React 18 + TypeScript + Vite |
| Video | Native WebRTC (`RTCPeerConnection`) |
| Signaling | WebSocket (`gorilla/websocket`) |
| Backend | Go (standard library, `net/http`) |
| Persistence | PostgreSQL via `pgx`, in-memory fallback |
| Media network | Public STUN + TURN (coturn) |
| Deployment | Multi-stage Docker, single image |

---

## 3. Getting started

### Prerequisites

- Go 1.24+ (tested with 1.27)
- Node.js 20+ (tested with 24)
- Optional: Docker + Docker Compose, PostgreSQL 14+

### Development with two terminals

```bash
make deps            # Go modules + npm packages

# terminal 1 — backend on :8080
make dev-api

# terminal 2 — frontend on :5173, proxies /api and /ws to :8080
make dev-web
```

Open <http://localhost:5173>. Two tabs (or two browsers) are enough to test a
full match.

> The camera only works over `https://` or on `localhost`. To test from a
> phone on the local network, use an HTTPS tunnel (Caddy, ngrok,
> Cloudflare Tunnel).

### Production in one command

```bash
make build           # React bundle + Go binary
make run             # serves the API and the SPA on :8080
```

### Docker

```bash
cp .env.example .env          # fill in ADMIN_TOKEN and, in prod, TURN_*
make docker-up                # API + PostgreSQL
make docker-up-turn           # adds the coturn relay
```

---

## 4. Configuration

Every variable has a usable local default. See `.env.example` for the full
list; the most important ones:

| Variable | Default | Role |
| --- | --- | --- |
| `ADDR` | `:8080` | Listen address |
| `ALLOWED_ORIGINS` | `localhost:5173`, `localhost:8080` | **Extra** origins allowed to open the WebSocket. Same-origin is always accepted, so a deployment serving the SPA from this same server works with no configuration. List only other origins (Vite dev server, separate domain). Never `*` in production. |
| `DATABASE_URL` | *(empty)* | Empty means in-memory storage. |
| `ICE_SERVERS` | Google STUN | JSON array in `RTCIceServer` format. |
| `TURN_URLS` / `TURN_USERNAME` / `TURN_CREDENTIAL` | *(empty)* | Shortcut that appends a TURN server to the ICE list. |
| `ADMIN_TOKEN` | *(empty)* | Empty means the admin API is disabled (404). |
| `QUEUE_TIMEOUT` | `90s` | Maximum wait before `queue.timeout`. |
| `AUTO_SUSPEND_AFTER` | `3` | Distinct reports triggering a suspension. |
| `WEB_DIR` | `web/dist` | Built SPA directory. |

### Enabling TURN (required outside open networks)

1. Deploy coturn with `deploy/coturn/turnserver.conf`, replacing the secret.
2. Set on the API side:
   ```bash
   TURN_URLS="turn:turn.example.net:3478?transport=udp,turn:turn.example.net:3478?transport=tcp"
   TURN_USERNAME=randomchat
   TURN_CREDENTIAL=<the secret>
   ```
3. Check that `GET /api/config` returns `features.turn_relay: true`.

Without TURN, calls fail behind symmetric NATs or corporate firewalls — the
number one cause of connection failure in production.

---

## 5. WebSocket protocol

Endpoint: `GET /ws?cid=<client-id>` (protocol version `3`)

The search shape (Solo vs conference plus the member cap) is chosen on the
landing screen and can be changed at any time from the control bar menu;
`Suivant` always searches with the current setting (`session.next` carries it).

Two URL shapes carry a choice that is already made: `/?mode=group&max=5`
opens the landing screen pre-set to a capped group search (invalid values are
silently ignored), and `/j/<token>` opens an invitation screen for one
precise discussion — preview first, then direct entry, no knocking. The token
is opaque (never the session id) and the client builds the full URL itself,
so the server needs no base-URL setting.

The `cid` is an opaque token stored in `localStorage`
(`^[a-z0-9-]{8,64}$`). It lets blocks and suspensions survive a page reload
without requiring an account. The server treats it as **untrusted**: it
validates it and rate-limits on the network origin.

Every frame is a JSON envelope: `{ "event": "...", "data": { ... } }`.

### Client → server

| Event | Role |
| --- | --- |
| `queue.join` | Request a match (`{ mode: "solo" \| "group", max_participants }`, 0 = no cap) |
| `queue.cancel` | Cancel the search |
| `session.next` | Close the session and search again |
| `session.leave` | Close the session without searching |
| `session.add` | Pull one waiting peer into your session (promotes a solo call to a conference) |
| `session.join` | Enter a precise session through its invitation link (`{ token }`, idle only) |
| `session.update` | Change your session's member cap (`{ max_participants }`, 0 = no cap) |
| `invite.create` | Get the session's link (`{ max_participants?, rotate? }`, members only) |
| `invite.revoke` | Kill the session's link (members only, nobody is ejected) |
| `rtc.offer` / `rtc.answer` | Forward an SDP description (`to` selects the recipient in a conference) |
| `rtc.ice` | Forward an ICE candidate (`to` selects the recipient in a conference) |
| `chat.send` | Send a text message (broadcast to every other member) |
| `report.create` | Report (and optionally block) a member (`target`, defaults to the peer in 1:1) |
| `block.create` | Block a member (`target`, defaults to the peer in 1:1) |

### Server → client

| Event | Role |
| --- | --- |
| `server.hello` | Client id, protocol version, ICE servers, limits |
| `queue.waiting` / `queue.canceled` / `queue.timeout` | Queue state |
| `match.found` | Session created, with `peers[]`, `mode`, `max_participants` (plus `role` in 1:1) |
| `session.updated` | The room changed shape: promotion solo → group, or a new member cap |
| `invite.created` | The session's link (`{ token, mode, max_participants }`, reply to `invite.create`) |
| `invite.revoked` | The session's link is dead (open share dialogs disable themselves) |
| `peer.joined` / `peer.left` | Conference roster changes |
| `session.ended` | Session end with the reason |
| `session.state` | Current state (`idle`, `waiting`, …) |
| `chat.message` / `chat.ack` | Message received / send acknowledgement |
| `report.ack` / `block.ack` | Confirmation |
| `server.error` | Protocol error (stable `code`, English message) |

### Rules enforced server-side

1. Only listed events are accepted; everything else is explicitly rejected.
2. Signaling is relayed only inside the sender's own session: to the single
   peer in 1:1, or to the member named by `to` in a conference. Chat is
   broadcast to every other member. A client can never reach another session.
3. Bounded sizes: envelope (64 KB by default), SDP (100 KB), ICE candidate
   (2 KB), chat message (1000 characters), report (500 + 2000 characters).
4. `rtc.offer` must carry `type: "offer"` and `rtc.answer` `type: "answer"`:
   no arbitrary blob is forwarded.
5. Each event family has its own per-user token bucket (`queue.join`,
   `chat.send`, `report.create`, signaling).
6. One writer goroutine per connection: each socket has a single writer fed
   by a buffered channel. A slow client is disconnected instead of blocking
   the server.

---

## 6. Project layout

```text
random-chat/
├── cmd/api/main.go              # app wiring, graceful shutdown
├── internal/
│   ├── config/                  # environment-based configuration
│   ├── httpapi/                 # REST routes, middlewares, admin, SPA serving
│   ├── matching/                # state, queue, matching engine
│   ├── metrics/                 # counters and histograms (Prometheus format)
│   ├── moderation/              # rate limits, reports, blocks, suspensions
│   ├── sessions/                # session lifecycle, random identifiers
│   ├── signaling/               # protocol, WebSocket connections, event routing
│   └── storage/                 # persistence port: memory and PostgreSQL
├── migrations/                  # idempotent SQL schema
├── web/                         # React app (Vite + TypeScript)
│   └── src/
│       ├── components/          # UI
│       ├── hooks/useRandomChat  # session state machine
│       ├── lib/                 # signaling client, WebRTC wrapper
│       ├── styles/              # dark theme, design system
│       └── types.ts             # shared protocol contract
├── deploy/
│   ├── Dockerfile               # single image: SPA + binary
│   ├── compose.yaml             # API + PostgreSQL + coturn
│   └── coturn/turnserver.conf
├── Makefile
└── .env.example
```

### Backend invariants

- **One connection = one writer owner.** No concurrent writes possible.
- **One participant = at most one active session**, guaranteed by the
  matching-engine lock and the session manager `byPeer` table.
- **Unpredictable identifiers**: 128 random bits (`crypto/rand`) for sessions.
- **Every ended session frees its resources** and removes its participants
  from the queue.
- **Nothing from the browser is trusted**: size, event type, session state,
  pair membership — everything is rechecked.

---

## 7. Tests

```bash
make test         # full suite
make test-race    # with the race detector
make check        # vet + tests + TypeScript typecheck
```

Functional test coverage:

- **Matching**: pairing, blocked users never paired, cancel, expiry, and most
  importantly `TestConcurrentJoinsNeverDoubleAssign`, which fires 200
  simultaneous joins and checks that no session holds anything but two peers.
- **WebSocket (end to end, on a real `httptest` server)**: symmetric
  `match.found`, SDP/ICE/chat relay, Next freeing the partner, disconnect
  closing the session, reporting, rejection of invalid frames and unknown
  events, queue consistency after a mass Next.
- **Moderation**: token buckets, key independence, cleanup, violation
  escalation to IP suspension, block symmetry.
- **Memory storage**: bounded collections, and most importantly that a
  **suspension is never evicted** (a lost sanction would let an abuser back
  into the queue).

### Layout check

```bash
make dev-api                       # in one terminal
cd web && npm run check:layout     # in another
```

The script launches two headless Chrome instances with a synthetic camera,
forces a real match between them, then measures the layout at six viewport
widths (1440, 1180, 834, 390, 320 and 740×360 landscape). It checks that the
page does not scroll, that the control bar never covers the video, that the
"You" thumbnail stays in frame, and that nothing hides the chat input.
Screenshots go to `web/.layout-shots/`.

It exists because eyeballing is not enough: the first UI version wrapped the
control bar over **three lines** at 1180×700 — a `position: fixed` element
with `left: 50%` but no `width` gets its width capped at half the screen. No
CSS review caught it; measurements did.

---

## 8. Administration and moderation

Enable the API with `ADMIN_TOKEN`. Every route requires
`Authorization: Bearer <token>` (constant-time comparison); without a
configured token they return 404.

```bash
TOKEN=... ; HOST=http://localhost:8080

curl -H "Authorization: Bearer $TOKEN" $HOST/api/admin/stats
curl -H "Authorization: Bearer $TOKEN" "$HOST/api/admin/reports?status=open&limit=50"
curl -H "Authorization: Bearer $TOKEN" -X POST $HOST/api/admin/reports/<id>/status \
     -d '{"status":"actioned"}'
curl -H "Authorization: Bearer $TOKEN" -X POST $HOST/api/admin/users/<id>/suspend \
     -d '{"duration_minutes":1440,"reason":"nudity"}'
curl -H "Authorization: Bearer $TOKEN" $HOST/api/admin/actions     # audit log
```

Built-in automation:

- `AUTO_SUSPEND_AFTER` distinct reports on the same target ⇒ automatic
  suspension.
- Repeated rate-limit violations ⇒ IP suspension for `SUSPEND_DURATION`.
- Block ⇒ never paired again (enforced by the engine, not the client).
- Every reported session is closed immediately and its reason is logged.

### Minor safety

A consent screen blocks camera access before any match: the user confirms
being an adult and accepting the rules. The Report button offers a dedicated
(`minor`) category. **This is only an entry barrier**: a real production
launch needs age verification, a data-retention policy, and human moderation
coverage.

---

## 9. Observability

| Endpoint | Usage |
| --- | --- |
| `GET /api/health` | Liveness probe (connections, sessions, queue) |
| `GET /api/stats` | Public aggregate counters, used by the home page |
| `GET /api/invites/{token}` | Public invitation preview (`mode`, `members`, `max_participants`, `no-store`) |
| `GET /metrics` | Prometheus format, no authentication |

Indicators to watch first:

- **Matching**: `randomchat_match_wait_p95_ms` — wait time is the top churn
  driver.
- **Signaling**: `randomchat_connections_active`, routing latency.
- **Media**: `relay` vs `host`/`srflx` connection ratio; a high relay share
  means high TURN cost. The quality shown in the UI comes from
  `RTCPeerConnection.getStats()` (RTT, jitter, loss, bitrate, candidate
  type).
- **Safety**: `randomchat_reports_total`, `randomchat_violations_total`,
  `randomchat_suspensions_total`.

---

## 10. Roadmap

| Phase | Content | Status |
| --- | --- | --- |
| 01 — Foundations | Repo, config, Docker, API/WS interfaces | ✅ |
| 02 — Matching engine | States, concurrent queue, Next, expiries | ✅ |
| 03 — WebRTC calls | SDP/ICE, STUN/TURN, network recovery, track cleanup | ✅ |
| 04 — Interface | Home, call scene, chat, responsive | ✅ |
| 05 — Safety and admin | Reports, blocks, limits, suspensions, audit | ✅ |
| 06 — Tests, tuning, deploy | Load tests, metrics, pilot | 🔶 base done, needs real-world instrumentation |

### Natural next steps

1. **Redis** for the queue and sessions, to allow several Go instances
   (the `sessions.Store` interface and the queue are already isolated).
2. **Ephemeral TURN credentials** (HMAC with expiry) served by `/api/config`.
3. **Age verification** and per-jurisdiction data-retention policy.
4. **SFU (LiveKit)** if the product moves to multi-party or recording.
5. **Load tests** on matching and WebSockets before announcing any capacity
   number — a video server costs TURN bandwidth first, not Go CPU.

---

## 11. Known limitations

- **Blocking is tied to the browser `cid`**, which is cleared with local
  storage. A lightweight account (email or OAuth) is the only real fix.
- **The SPA and the API share one image**: simple to operate; split (CDN) if
  static traffic grows.
- **No session resume after reload**: the WebRTC session is lost, a new
  search is required. Re-entering through the session's invitation link while
  it is still alive is an approximate substitute, not a real resume.
- **Chat is not auto-moderated.** Messages pass through without being stored;
  only reports are persisted.

