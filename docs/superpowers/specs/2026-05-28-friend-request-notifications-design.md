# Friend-Request Notifications: Badge + Email Accept Link

## Problem

When user A sends a friend request to user B (today, via the Friends
dialog at `web/src/components/FriendsDialog.tsx`), B has no in-app signal
that anything happened. The Friends dialog is reached from a menu item
inside the avatar dropdown — so unless B happens to open the dropdown
and click "Friends…", the pending request sits invisible.

`internal/handlers/friends.go:126` (`sendFriendRequestNotification`) does
email B, but:

1. The mail is gated on `MAIL_FROM_ADDRESS` being configured (often
   blank in local dev — observed today: user couldn't tell the email
   path even existed because `.env` had no `MAIL_FROM_ADDRESS`).
2. The email's only CTA is "Review the request" linking to `/friends`,
   which today is just the SPA root. The recipient still has to open
   the avatar menu, find "Friends…", and click Accept.

There is no real-time notification path either — `/api/events` (SSE)
only carries `flight.updated` and `flight.deleted`.

## Goal

Two changes, deliberately scoped:

1. **In-app badge.** The avatar `IconButton` in `AppShell.tsx:90` shows
   a numeric MUI `Badge` whose count equals the recipient's pending
   incoming friend requests. The badge updates live via SSE without
   requiring a reload.
2. **One-click accept from email.** The friend-request email gains an
   "Accept" button whose URL carries an HMAC-signed token. The SPA reads
   the token on load and POSTs it to a new endpoint that authenticates
   the recipient via session, verifies the token, and accepts the
   friendship — all on the recipient's first click.

The badge is built on a generic `/api/notifications` shape and a
`notifications.updated` SSE event so a second source (flight shares,
identity-link notices, …) can be added later by populating new keys —
the badge UI does not change.

## Non-goals

- No generic "notifications" inbox / drawer. The badge surfaces a
  count; users still act on requests inside the existing Friends
  dialog (and via the email link).
- No persistent "unread" state per notification. The count is derived
  from canonical DB state (pending friendship rows), so accepts /
  declines / cancellations reduce it automatically.
- No new notification kinds in v1. Just friend requests. The
  extensibility is structural, not behavioural.
- No change to `pending_friend_invites` (the queue for invites to
  email addresses without an account). Those still auto-accept on
  first sign-in via `consumePendingInvitesTx`.
- No change to the existing `inviteFriendAcceptedBody` no-enumeration
  guarantee: the new SSE / endpoint never reveals information about
  the recipient to the inviter.
- No client-side router. The accept-token bootstrap reads
  `window.location.search` the same way `App.tsx:43` already reads
  `pathname` for `/privacy` and `/terms`.

## Design

### 1. Accept token

A new file `internal/auth/accept_token.go` with two functions:

```go
// MintFriendAcceptToken returns a base64url-encoded token whose payload
// is "<recipientID>.<inviterID>.<expiryUnix>" and whose tag is the
// HMAC-SHA256 of that payload using key (typically SessionKey).
func MintFriendAcceptToken(key []byte, recipientID, inviterID int64, expiry time.Time) string

// VerifyFriendAcceptToken decodes and authenticates a token minted by
// MintFriendAcceptToken. Returns the (recipientID, inviterID) pair on
// success. Errors distinguish malformed (ErrMalformedToken) from
// expired (ErrExpiredToken) so the caller can produce a useful toast.
func VerifyFriendAcceptToken(key []byte, token string) (recipientID, inviterID int64, err error)
```

Encoding is base64url(payload + "." + base64url(tag)). The payload is
ASCII (three integers + dots) and re-emitted to the verifier as-is so
the HMAC compare is on the exact bytes signed.

Default expiry: 7 days (constant `friendAcceptTokenTTL` in
`internal/handlers/friend_emails.go`). The friendship row itself stays
pending past the token's expiry — the recipient can still accept
in-app; only the email link goes dead.

`SessionKey` is already a `[]byte` on `*auth.Handler` and on
`*config.Config` (loaded from `SESSION_KEY`). The handler reaches the
key via `a.Config.SessionKey` (already used elsewhere in
`internal/handlers`).

### 2. `POST /api/friends/accept-token`

Wired in `handlers.go:Register` next to the other `/api/friends` routes,
behind the auth-required `req` middleware (same wrapper as
`acceptFriend`).

```go
type acceptFriendTokenReq struct {
    Token string `json:"token"`
}
type acceptFriendTokenResp struct {
    Friendship *api.FriendshipDTO `json:"friendship,omitempty"`
    Already    bool               `json:"already,omitempty"`
}
```

Behaviour:

| Case                                                 | Response                                                                        |
| ---------------------------------------------------- | ------------------------------------------------------------------------------- |
| Missing / blank `token`                              | 400 `{"error":"token required"}`                                                |
| `VerifyFriendAcceptToken` → `ErrMalformedToken`      | 400 `{"error":"invalid invitation link"}`                                       |
| `VerifyFriendAcceptToken` → `ErrExpiredToken`        | 410 `{"error":"invitation link expired — ask the sender to resend"}`            |
| `recipientID != session user`                        | 403 `{"error":"this invitation isn't for your account"}`                        |
| `AcceptFriendship` → `ErrNotFound` (no pending row)  | 200 `{"already":true}` — request was cancelled or already accepted; toast says "you're already friends" (no leak: the recipient already had access to this state via `/api/friends`). |
| `AcceptFriendship` → `*Friendship`                   | 200 `{"friendship":<DTO>}` + publish SSE events (next section).                 |
| Any other store error                                | 500 (via `handleStoreErr`).                                                     |

The 403 wording is the same regardless of whether the inviter or token
were real — the inviter ID stays opaque to other users.

### 3. `GET /api/notifications`

New handler, same auth wrapper. Body shape:

```go
type NotificationsDTO struct {
    FriendRequestsPending int `json:"friend_requests_pending"`
}
```

Implementation calls a new `Store.CountIncomingFriendRequests(ctx, userID)`:

```sql
SELECT count(*) FROM friendships
WHERE status = 'pending'
  AND requested_by <> $1
  AND $1 IN (user_low, user_high)
```

Returns 0 cleanly when there are no rows.

The DTO is an open map in spirit but a typed struct on the wire — future
kinds add new fields with the `omitempty`-friendly `json` tags so old
clients ignoring them keep working.

### 4. `notifications.updated` SSE event

`internal/sse/sse.go`'s `Event.VisibleTo` already supports per-user
targeting. We publish `Event{Type: "notifications.updated", Data:
json(NotificationsDTO), VisibleTo: []int64{userID}}` from:

- `inviteFriendByUserID` (`friends.go:97`) — after `RequestFriendship`
  succeeds with a brand-new pending row, publish to `target.ID`.
- `acceptFriend` (`friends.go:184`) — after `AcceptFriendship` succeeds,
  publish to the accepter (their count drops by 1). The inviter's
  count is unaffected — no publish needed for them.
- `removeFriend` (`friends.go:199`) — after `RemoveFriendship` succeeds,
  publish to *both* user IDs (we can't cheaply tell from the handler
  whether the deleted row was an incoming-pending or
  outgoing-pending; publishing to both is correct in either case and
  cheap).
- `acceptFriend` (token variant, item 2) — same as the existing
  `acceptFriend` publish: target the accepter.
- `auth/provider.go` post-signup hook that calls
  `consumePendingInvitesTx`: each newly-accepted edge could change the
  inviter's *outgoing* pending count, but since the new user becomes a
  friend (not a pending recipient), the inviter's `friend_requests_pending`
  is not affected. No publish needed.

Each publish recomputes the count via
`Store.CountIncomingFriendRequests` — one cheap query per state change
on the friendship pair. We could optimise to a delta but the count is
small and re-querying keeps the SSE payload authoritative (avoids
client/server drift if a tab is replayed from cache).

A tiny helper `(*API).publishNotifications(ctx, userID)` does the
count-and-publish so the four callers stay one-liners.

### 5. Friend-request email update

`internal/handlers/friend_emails.go:buildFriendRequestEmail`:

- Add `Token string` field to `friendRequestInput`.
- HTML body grows a second button **Accept** to the left of the existing
  "Review the request" button. URL:
  `<PublicURL>/?friend_accept=<token>`.
- Plain-text body adds a second labelled URL block above the existing
  one:
  ```
  Accept this request with one click:
    <PublicURL>/?friend_accept=<token>
  ```

`sendFriendRequestNotification` (`friends.go:126`) mints the token
inline:

```go
token := auth.MintFriendAcceptToken(
    a.Config.SessionKey,
    recipient.ID, inviter.ID,
    time.Now().Add(friendAcceptTokenTTL),
)
```

If `a.Config.SessionKey` is empty (config never validates this — it's
required at startup), we still build the URL; verification at
redemption would fail loudly. In practice `SESSION_KEY` is mandatory in
config so this path is unreachable, but the email build code does not
panic — it just calls the helper.

`friendAcceptTokenTTL` is `7 * 24 * time.Hour`, declared in
`friend_emails.go`.

### 6. SPA — state and SSE

`web/src/api/types.ts` gains:

```ts
export interface Notifications {
  friend_requests_pending: number;
}

export interface AcceptFriendTokenResult {
  friendship?: Friendship;
  already?: boolean;
}
```

`web/src/api/client.ts` gains:

```ts
getNotifications: () => request<Notifications>('GET', '/api/notifications'),
acceptFriendToken: (token: string) =>
    request<AcceptFriendTokenResult>('POST', '/api/friends/accept-token', { token }),
```

`web/src/state/store.ts` gains:

```ts
notifications: Notifications;          // initial { friend_requests_pending: 0 }
refreshNotifications: () => Promise<void>;
applyNotificationsUpdate: (n: Notifications) => void;
notice: { message: string; severity: 'success' | 'info' } | null;
setNotice: (n: { message: string; severity: 'success' | 'info' } | null) => void;
```

`init()` already calls `getMe()`; right after a successful `me` resolve
(and before unmasking `auth === 'authenticated'` is fine) it also calls
`api.getNotifications()` and writes into `notifications`. Failures here
do not block sign-in — they leave the badge at 0.

`web/src/sse.ts` extends `SSEHandlers` with `onNotifications` and adds
an `es.addEventListener('notifications.updated', …)` block that parses
the JSON to a `Notifications`. `App.tsx`'s `connectSSE` call wires
`onNotifications: applyNotificationsUpdate`.

### 7. SPA — badge UI

`AppShell.tsx` wraps the avatar `IconButton` (`AppShell.tsx:90-99`) in:

```tsx
<Badge
  badgeContent={notifications.friend_requests_pending}
  color="error"
  overlap="circular"
  invisible={notifications.friend_requests_pending === 0}
  anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
>
  <Tooltip title="Account menu">
    <IconButton …>
      <Avatar … />
    </IconButton>
  </Tooltip>
</Badge>
```

The same numeric chip is rendered next to the "Friends…" `MenuItem`'s
label (right-aligned, `Chip size="small" color="error"`), only when
count > 0. Opening the menu therefore confirms what the avatar badge
hinted at.

`useStore` selector `(s) => s.notifications.friend_requests_pending`
is read once at the top of `AppShell`.

### 8. SPA — accept-token bootstrap

`App.tsx` gains a new effect that runs once after `init()` resolves:

```tsx
useEffect(() => {
  if (auth !== 'authenticated') return;
  const params = new URLSearchParams(window.location.search);
  const token = params.get('friend_accept');
  if (!token) return;
  void (async () => {
    try {
      const r = await api.acceptFriendToken(token);
      setNotice({
        message: r.already
          ? "You're already friends — nothing to accept."
          : 'Friend request accepted.',
        severity: r.already ? 'info' : 'success',
      });
      void refreshNotifications();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      params.delete('friend_accept');
      const qs = params.toString();
      const url = window.location.pathname + (qs ? '?' + qs : '') + window.location.hash;
      window.history.replaceState({}, '', url);
    }
  })();
}, [auth]);
```

Anonymous bootstrap path: when `auth === 'anonymous'`, the effect is a
no-op and the token stays in the URL. After successful sign-in,
`init()` flips `auth` to `'authenticated'` and the effect re-runs,
catching the same token from the still-present query string.

The existing error `Snackbar` (`App.tsx:64`) is supplemented by a
parallel success `Snackbar` driven by the new `notice` state.
`setNotice(null)` on close, 6 s auto-hide.

### 9. Display name in the success toast

For the email-accept flow, the toast can identify the new friend by
their name. `acceptFriendToken`'s response includes the full
`FriendshipDTO`, but the SPA already has `users` indexed (the
`/api/users` endpoint returns every user; `FriendsDialog.tsx:46-48`
shows the pattern). On accept-token success the toast uses
`users.find(u => u.id === r.friendship.friend_id)?.name ?? 'them'` to
build the message: `You're now friends with <name>.`

If `users` hasn't loaded yet (race on bootstrap), the toast falls back
to `'Friend request accepted.'` — same as `r.already`.

### 10. Operator note (README)

The "Friend networks" mention (currently implicit — the README's
`MAIL_FROM_ADDRESS` paragraph in `.env.example` covers identity-link
notifications) gets one sentence acknowledging the email Accept link
expires 7 days after dispatch and that the same `MAIL_FROM_ADDRESS`
requirement applies.

## Failure modes

| Scenario                                    | Behaviour                                                                                    |
| ------------------------------------------- | -------------------------------------------------------------------------------------------- |
| SSE disconnected when request lands         | Recipient sees badge on next reload; `init()` re-fetches `/api/notifications`.               |
| Two tabs open                               | Both tabs are subscribed; both badges update simultaneously via SSE.                         |
| Recipient deletes the request via decline   | Badge drops; `removeFriend` handler republishes the count to the recipient.                  |
| Inviter cancels their outgoing pending      | Same SSE publish lands on the recipient; their count drops if the row was incoming-pending.  |
| Recipient clicks email link signed-out      | SPA holds the token in URL through the Login screen; OAuth completes; effect re-runs and POSTs. |
| Recipient signs in as a different account   | 403 from server; error toast surfaces "this invitation isn't for your account"; token cleaned from URL. |
| Token expired (7 d+)                        | 410 from server; error toast surfaces the "ask the sender to resend" message; URL cleaned.   |
| `MAIL_FROM_ADDRESS` blank                   | No email at all (existing behaviour); the in-app badge still surfaces the request. The bug the user originally reported is solved by the badge even without configuring mail. |
| Token tampered                              | 400 from server; same toast surface; URL cleaned.                                            |

## Tests

### Backend (Go)

1. `internal/auth/accept_token_test.go`:
   - mint → verify round-trip on valid pair
   - verify rejects truncated, padded, base64-malformed
   - verify rejects altered payload (one bit flip in recipient ID)
   - verify rejects expired (set expiry in the past)
   - verify rejects wrong key
2. `internal/store/friends_test.go`:
   - `CountIncomingFriendRequests` returns 0 when no rows
   - returns 0 when only outgoing pending
   - returns 0 when only accepted
   - returns count of incoming pending across multiple inviters
3. `internal/handlers/friends_test.go`:
   - `GET /api/notifications` 200 with the count
   - `POST /api/friends/accept-token` happy path: accepts, returns
     friendship DTO, calls `Hub.Publish` for `notifications.updated`
     (test via a recording fake hub)
   - 400 on missing / blank / malformed token
   - 410 on expired token
   - 403 on token whose recipient ≠ session user
   - 200 `{"already":true}` when no pending row exists
   - SSE publish observed on `inviteFriendByUserID`,
     `acceptFriend` (existing route), `removeFriend` — extend the fake
     hub in the existing friend tests
4. `internal/handlers/friend_emails_test.go`:
   - `buildFriendRequestEmail` snapshot includes the Accept button
     with a URL containing `?friend_accept=` and the supplied token
   - plain-text body includes the same URL on its own line

### Frontend (Vitest)

5. `web/src/components/AppShell.test.tsx`:
   - badge invisible when `friend_requests_pending === 0`
   - badge renders the count when > 0
   - badge updates when `applyNotificationsUpdate` is called
   - menu "Friends…" row shows a sibling chip with the count
6. `web/src/sse.test.ts`:
   - `notifications.updated` event invokes `onNotifications` with the
     parsed payload
7. `web/src/App.test.tsx`:
   - on mount with `?friend_accept=<token>` and authenticated, POSTs
     to `/api/friends/accept-token`, shows success notice, strips the
     query param via `history.replaceState`
   - on mount with the same URL but anonymous, does not POST; after
     the auth state flips to authenticated (simulated), POSTs and
     processes
   - error response surfaces in the error `Snackbar` (not the success
     one)

## Risks

- **Token bearer compromise.** Anyone with the recipient's email link
  can attempt to accept — but the server still requires a valid session
  as the recipient. Worst case: someone who already controls the
  recipient's email *and* their Aerly session can accept on their
  behalf; at that point they already have full access. Documented in
  the toast wording ("this invitation isn't for your account").
- **SSE drop.** Badge can go stale if the connection drops between an
  invite landing and a reload. Mitigation: `init()` re-fetches
  `/api/notifications`; SSE auto-reconnects with backoff
  (`sse.ts:45-52`). Worst case is staleness, not corruption.
- **Two-tab accept race.** If two tabs both see the badge and one
  accepts, the other's `applyNotificationsUpdate` lands and the badge
  drops in both. The accept-token endpoint's `ErrNotFound → already`
  path also handles the case where two tabs simultaneously POST the
  same token; both get a clean response.
- **Operator forgets `MAIL_FROM_ADDRESS`.** Email never sends; this is
  the bug that motivated the work. We log at WARN today; we keep that
  log line. The badge gives recipients an in-app path that doesn't
  depend on email at all, which is the user-visible mitigation.

## Out-of-scope follow-ups

- Generic notifications drawer / history.
- Per-notification "mark read" semantics.
- Push notifications via Web Push or platform APIs.
- Surfacing other event types (flight shares, identity-link
  reminders) in the same badge.
- Resending the email when the operator first configures
  `MAIL_FROM_ADDRESS` after pending requests exist.
