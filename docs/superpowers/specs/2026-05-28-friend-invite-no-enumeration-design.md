# Friend Invite — Close the Enumeration Leak in the Friend List

## Problem

`POST /api/friends/invite` is carefully designed to not confirm whether the
typed email belongs to a registered user. The response body is byte-identical
across the known-user, unknown-email, and self-match paths (see
`inviteFriendAcceptedBody` and `TestInviteFriendResponseIdenticalForKnownAndUnknown`
in `internal/handlers/friends.go` / `friends_test.go`).

The friend list endpoint (`GET /api/friends`) defeats this guarantee. After
inviting `x@y.com`:

- If `x@y.com` is a verified address of an existing user, a `friendships` row
  is created (`status=pending`, `requested_by=me`). The list returns a DTO
  with `friend_id` set; the frontend cross-references `friend_id` against
  `/api/users` and renders the target's name and gravatar in the pending
  row. The inviter now knows the email belongs to a user — and which user.
- If `x@y.com` is not a verified address of any user, only a
  `pending_friend_invites` row is created. The list endpoint does not
  return it. The inviter sees no row in their friend list.

So the inviter can enumerate by diffing "did a row appear?" against the
list. The leak surface is the list endpoint, not the invite endpoint.

Concrete trigger: the friend dialog screenshot at the project root
(`Screenshot 2026-05-28 at 12.13.38.png`) shows an invite to
`dpage@pgadmin.org` rendered with "Dave Page" + gravatar in the pending
row.

## Goal

Outgoing pending invitations look identical in the friend list whether or
not the typed email maps to a registered user. Specifically:

- The list endpoint returns a row for every outgoing pending invite the
  viewer has open, regardless of whether the target exists.
- Each such row contains only the email the inviter typed — no `friend_id`,
  no name, no gravatar, no username.
- Cancellation works without the inviter ever needing the target's
  user_id.

Accepted friendships and incoming pending requests are unaffected: in
both cases the viewer is allowed to see the other party's identity (for
incoming, the other party chose to expose themselves by inviting; for
accepted, both parties consented).

## Non-goals

- No change to how the recipient sees incoming pending invites. The
  privacy concern is one-sided — it's about hiding existence from the
  inviter, not from the invitee.
- No change to the invite email or notification email content.
- No change to `consumePendingInvitesTx` (the sign-in-time consumer of
  `pending_friend_invites`).
- No "you have N pending outgoing invites" badge or other surfacing
  beyond what already exists in `FriendsDialog`.

## Design

### Storage

New migration adds a nullable column to `friendships`:

```sql
ALTER TABLE friendships
  ADD COLUMN invited_email TEXT;
```

Populated whenever a pending friendship row is created from an
invite-by-email request: the trimmed address the inviter typed, stored
in its original casing. Stays NULL for friendship rows that arrive via
other paths (today, only `consumePendingInvitesTx`, which creates rows
already in `accepted` state).

`pending_friend_invites` is unchanged.

**Backfill.** Existing pending outgoing rows in production have
`invited_email = NULL`. The migration backfills them: for each pending
row, pick the oldest verified email of the recipient (the non-requester
side of the pair) and set `invited_email` to it. This loses the "what
did the inviter type" detail but matches what the inviter would see
today. If a pending row's recipient has no verified email — should not
happen in practice, since the invite path required one at creation time
— the row is deleted by the migration. The post-migration invariant is
"every pending row has a non-NULL `invited_email`", and `invited_email`
is `NOT NULL` is added as a CHECK constraint conditional on
`status = 'pending'`:

```sql
ALTER TABLE friendships
  ADD CONSTRAINT friendships_pending_has_invited_email
  CHECK (status <> 'pending' OR invited_email IS NOT NULL);
```

### Invite path

`inviteFriend` (in `internal/handlers/friends.go`) keeps its three
branches but threads the typed email through to the store:

- **Known target**: `RequestFriendship(ctx, me, target.ID, invitedEmail)`
  — extended signature. The pending row created by the `INSERT` gets
  `invited_email = invitedEmail`. The cross-direction auto-accept branch
  does not touch `invited_email` (the row is no longer pending and the
  column is moot once accepted).
- **Unknown email**: unchanged — `UpsertPendingFriendInvite(me, addr, message)`.
- **Self-match**: unchanged — silent no-op.

The 202 `inviteFriendAcceptedBody` response stays byte-identical across
the three paths.

### List path

`Store.ListFriendships(viewerID)` keeps its existing query against
`friendships` (now also selecting `invited_email`) and runs a parallel
query for the viewer's outgoing email-only invites:

```sql
SELECT email_lower, requested_at
FROM pending_friend_invites
WHERE inviter_id = $1
ORDER BY requested_at DESC
```

The handler converts both into a single `[]FriendshipDTO` slice. Sort
order matches what the SQL already gives us: pending rows first (status
DESC inside `friendships` already orders pending before accepted; the
email-only rows are all pending and get interleaved by
`requested_at DESC` within the pending block).

No deduplication is required at the DTO layer: under the design, a
single outgoing pending invite lives in exactly one of the two tables
at any moment. Invites to known users → only `friendships`. Invites to
unknown emails → only `pending_friend_invites`. Cross-table moves
happen transactionally via `consumePendingInvitesTx` (which deletes the
`pending_friend_invites` row and creates/upgrades the `friendships`
row in one statement).

### DTO shape

`FriendshipDTO` in `internal/api/dto.go`:

```go
type FriendshipDTO struct {
    FriendID    int64      `json:"friend_id,omitempty"`     // omitted for outgoing pending
    Email       string     `json:"email,omitempty"`         // present only for outgoing pending
    Status      string     `json:"status"`
    Direction   string     `json:"direction,omitempty"`
    RequestedAt time.Time  `json:"requested_at"`
    AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
}
```

**Invariant.** When `direction == "outgoing"` and `status == "pending"`,
`FriendID == 0` (omitted on the wire) and `Email != ""`. Otherwise
`FriendID != 0` and `Email == ""`. This invariant is the no-enumeration
guarantee on the wire — a test asserts it for both the known and
unknown branches.

New pending rows in `friendships` always have `invited_email` set by
construction (the invite-by-email handler is the only writer). The
NULL case can only arise transiently for legacy rows; the migration's
backfill is responsible for eliminating it (see *Migration*). The DTO
code asserts `invited_email IS NOT NULL` for pending outgoing rows and
treats a NULL as a programming error worth logging — it should never
reach the wire.

TypeScript mirror in `web/src/api/types.ts`:

```ts
export interface Friendship {
  friend_id?: number;
  email?: string;
  status: FriendshipStatus;
  direction?: FriendshipDirection;
  requested_at: string;
  accepted_at?: string;
}
```

### Cancel path

The inviter no longer learns the target's user_id from the list
endpoint for outgoing pending invites. New endpoint to cancel by email:

```
DELETE /api/friends/outgoing
Body: {"email": "x@y.com"}
Response: 204 No Content (always)
```

Handler (one transaction):

1. Lowercase the address.
2. `DELETE FROM pending_friend_invites WHERE inviter_id = me AND email_lower = $email`.
3. Look up the target via `UserByVerifiedEmail`. If found,
   `DELETE FROM friendships WHERE pair matches AND status = 'pending'
   AND requested_by = me AND lower(invited_email) = $email`.
4. Always return 204 — no 404 when nothing matched, otherwise the
   response leaks existence on cancel just as the invite response would.

The existing `DELETE /api/friends/{userId}` stays for the cases where
the viewer legitimately knows the user_id: accepted friendships and
incoming pending requests.

### Accept path

`POST /api/friends/{userId}/accept` is unchanged on the wire. The
recipient does know the inviter — they received an email and see the
incoming-pending row with `friend_id` in their own list.

Small internal addition: after `AcceptFriendship` flips the row to
accepted, the store also deletes any `pending_friend_invites` row
whose `(inviter_id, email_lower)` matches `(requested_by, any of
viewer's verified emails)`. This mirrors what `consumePendingInvitesTx`
does on sign-in. In normal flow under approach A this is a no-op
(invites to verified emails never go through `pending_friend_invites`),
but it's a defensive cleanup if state ever drifts.

### Rendering

`web/src/components/FriendsDialog.tsx` branches in the row loop:

```tsx
{f.direction === 'outgoing' && f.status === 'pending' ? (
  <OutgoingPendingRow
    email={f.email!}
    onCancel={() => handleCancelOutgoing(f.email!)}
  />
) : (
  // existing rendering using userIndex.get(f.friend_id)
)}
```

The outgoing pending row renders:

- A generic placeholder avatar (the email's first letter, uppercased,
  no `src` so no gravatar fetch).
- The email address where the friend's name currently sits.
- The existing "invite sent" chip.
- The existing trash-icon button, wired to the new
  `api.cancelOutgoingInvite(email)`.

`userIndex` is never consulted for outgoing pending rows.

`web/src/api/client.ts` gains `cancelOutgoingInvite(email: string)`
calling `DELETE /api/friends/outgoing`.

## Testing

### Backend

- Keep `TestInviteFriendResponseIdenticalForKnownAndUnknown` passing.
- New `TestListFriendsOutgoingPendingHidesIdentity`: after inviting a
  known email and an unknown email from the same inviter, assert both
  appear in the list, both have `direction = "outgoing"`,
  `status = "pending"`, `email` set to what was typed, and `friend_id`
  omitted (zero value).
- New `TestListFriendsOutgoingPendingShapeIdentical`: byte-compare the
  DTO field set across the known and unknown rows (timestamps and
  emails differ, but presence/absence of every other field matches).
- New `TestCancelOutgoingInviteIdenticalForKnownAndUnknown`: cancel via
  email returns 204 in both cases; verify the appropriate row is gone
  from each table.
- New `TestCancelOutgoingInviteNoLeakOnUnknown`: cancelling an email
  the inviter never invited still returns 204.
- Extend `internal/store/friends_test.go`: `RequestFriendship` now
  accepts `invitedEmail`; assert it's stored on insert and untouched
  on cross-direction auto-accept.

### Frontend

- Update `web/src/components/FriendsDialog.test.tsx`:
  - Render an outgoing-pending row with `email = "x@y.com"`; assert
    the email is in the DOM and no user from `userIndex` is rendered
    for it.
  - Click trash → assert `api.cancelOutgoingInvite("x@y.com")` is
    called.

## Migration

1. Apply the `ALTER TABLE` adding `invited_email`.
2. Backfill within the same migration:

   ```sql
   UPDATE friendships f
   SET invited_email = (
     SELECT ue.address FROM user_emails ue
     WHERE ue.user_id = CASE
       WHEN f.requested_by = f.user_low THEN f.user_high
       ELSE f.user_low
     END
       AND ue.verified
     ORDER BY ue.verified_at ASC NULLS LAST, ue.created_at ASC
     LIMIT 1
   )
   WHERE f.status = 'pending'
     AND f.invited_email IS NULL;
   ```

3. Delete any pending rows whose recipient has no verified email
   (shouldn't exist; defensive):

   ```sql
   DELETE FROM friendships
   WHERE status = 'pending' AND invited_email IS NULL;
   ```

4. Add the CHECK constraint shown above.

5. Deploy server and frontend together. There's no flag — the DTO
   shape change is small enough and the server is the source of truth
   for which rows exist. Old frontends would render `name="undefined"`
   on outgoing pending rows; tolerable for the brief deploy window.

## Risks

- **Unverified third-party caller**: the `/api/friends` JSON shape
  changes for outgoing pending rows. We don't have external consumers
  documented in `APIs.md`, but worth a grep before shipping.
- **Backfill miss**: pending rows whose recipient has no verified
  email are deleted by the migration. Shouldn't happen in practice
  (the invite path required a verified email at creation), but if it
  does, the affected inviters silently lose their pending row. They
  can re-invite.
- **Self-invite invariant**: today, self-invites are a no-op and don't
  create any row. Confirm the new cancel-by-email endpoint also no-ops
  cleanly when the email is one of the caller's own verified addresses
  (covered by the "always returns 204" rule).
