# PRD: Trips as the core of Aerly

**Status:** Draft for review
**Date:** 2026-05-28
**Audience:** Product / founding team review

---

## 1. Summary

Aerly today is a collaborative flight tracker: people add flights and watch
each other move across a map. This document proposes making **trip planning**
the core of the product, with the **flight tracker becoming a secondary feature**
used immediately before, during, and just after a trip.

The shape we're aiming at is a modern replacement for the planning half of
TripIt — a product that has been badly neglected — combined with the live
tracking and social "who's on their way" experience Aerly already does well.

A user creates a **trip**, optionally shares it with friends, and fills it with
their travel plans: flights, hotels, trains, buses, taxis, day trips, dinners,
and so on. Plans can be added by hand, but the headline experience is **adding
them effortlessly** — forward a confirmation email, upload a ticket or PDF, or
paste a chunk of text (e.g. the message you got back from the local taxi firm
on WhatsApp) and Aerly turns it into a structured plan for you to confirm.

Everything in a trip is shown on a single **vertical timeline**, grouped by day.

---

## 2. Goals

- Make creating and filling a trip the primary thing users do in Aerly.
- Support every common travel plan type, not just flights.
- Make adding plans nearly effortless via email, upload, and paste, in addition
  to manual entry.
- Present a trip as a clean, day-by-day vertical timeline.
- Let users share a trip with friends and plan collaboratively.
- Let users subscribe to their plans from their own calendar app via read-only
  iCal feeds, at the personal, per-trip, and per-tag level.
- Keep the live flight tracker as a strong secondary feature, reachable both
  from a single flight and as a "who's converging" view before/during an event.

## 3. Non-goals (for this phase)

- Booking or payments. Aerly organizes plans; it does not sell travel.
- Price comparison or trip recommendations.
- A standalone mobile app. (Mobile location sharing is noted as a future
  tracker enhancement, not part of this phase.)
- A formal "event" product with hosts, RSVPs, and attendee management.

---

## 4. Who this is for

- **The organizer / frequent traveller.** Wants one tidy place for an entire
  trip's logistics, built quickly from the confirmations already sitting in
  their inbox.
- **The travelling friend group.** Several people heading to the same place —
  a conference, a wedding, a reunion — each with their own travel, who want to
  see each other's plans and watch each other arrive.
- **The companion traveller.** Someone on a shared trip (partner, family) who
  wants to see the whole plan without having built it.

---

## 5. Core concepts (in plain terms)

- **Trip** — the central object. Has a name, a destination, rough dates, and a
  collection of plans. Everything a user adds lives inside a trip.
- **Plan** — one entry on the timeline. A flight, a train, a hotel stay, ground
  transport (taxi, bus, transfer), a meal out, or an excursion / day trip. Every
  plan has a time (or time range), a title, a location, any confirmation details,
  and a free-form notes field. Notes are a field *on* a plan (and on the trip
  itself) — not a timeline entry in their own right.
- **Timeline** — the day-by-day vertical view of a trip's plans, in order.
- **Tracker** — the live map view. Used right before, during, and after travel
  to watch real movement.
- **Tag** — an optional shared label (e.g. `pgconf-eu-26`) that a group can put
  on their separate trips so they can find and watch each other. Purely opt-in.

---

## 6. What the user sees

### 6.1 Home: your trips

The landing screen is the user's list of trips rather than a map. Trips are
grouped into **Upcoming**, **Happening now**, and **Past**, each shown as a card
with its destination, dates, and the avatars of anyone it's shared with.

A prominent **New trip** action is the main call to action.

### 6.2 Inside a trip: the timeline

Opening a trip shows its **vertical timeline** — the heart of the product. Plans
are listed in chronological order, grouped under sticky day headers, each shown
as a card with:

- An icon for its type (plane, train, bed, ground transport, meal, excursion).
- Its time or time range, shown in the **local time of where it happens** (so a
  red-eye correctly spans two days).
- A title, location, and any confirmation reference.
- For flights, a link through to the live tracker for that flight.

From within a trip the user can also switch to a **Map** view for that trip,
which plots the trip's plans geographically — but the timeline is the default
and primary view.

### 6.3 Adding a plan

A single **Add to trip** action offers four ways in, all landing in the same
place:

1. **Manual** — pick a type and fill in the details. Adding a flight uses the
   existing flight lookup so the user can often just give a flight number and
   date.
2. **Paste text** — paste any confirmation text (the taxi firm's WhatsApp reply,
   a forwarded itinerary, a hotel email body) and Aerly extracts the plan.
3. **Upload** — drop in a PDF ticket or confirmation and Aerly extracts the plan.
4. **From email** — forward a confirmation to Aerly (the existing email-ingest
   path), and it lands in a trip.

For the paste / upload / email paths, Aerly shows the **extracted plan(s) for
the user to confirm or edit before they're added**. Anything it's unsure about
is flagged rather than silently guessed. This effortless capture is the feature
we expect users to fall in love with.

### 6.4 Sharing & privacy

A trip can be shared with friends. There are three trip-level roles:

- **Owner** — created the trip. Always sees everything in it.
- **Editor** — can add, edit, and remove plans. Editors are an explicit list,
  parallel to the viewer list.
- **Viewer** — can see the trip but not change it.

Sharing is always explicit: nothing a user creates is visible to anyone else
until they share it. Shared trips update live for everyone viewing them — if one
person adds the dinner reservation, it appears on everyone's timeline.

Separately, a person can be a **passenger** on an individual plan (e.g. on a
specific flight). Being a passenger grants visibility of that one plan; it does
not by itself grant access to the rest of the trip — though the owner can also
add them as a viewer if they want them to see everything.

**Per-plan privacy.** By default every plan is visible to everyone who can see
the trip. The owner (or an editor) can override an individual plan's visibility
with a simple "Who can see this?" control:

- **Everyone on the trip** (the default).
- **Hidden from…** — visible to all trip members except the people named. Good
  for surprises (hide the anniversary dinner from your partner while the friends
  helping plan it still see it). Note: someone added to the trip *later* will see
  the plan, so this is for "don't spoil it," not for sensitive information.
- **Only visible to…** — visible only to the people named. Use this for genuinely
  private plans; people added to the trip later will *not* see it.

A few rules keep this predictable:

- The owner always sees every plan; a plan can never be hidden from the owner.
- A plan can't be hidden from its own passengers — if you're on the flight, you
  can see the flight.
- Privacy applies everywhere a plan appears — the timeline, the trip map, and the
  live tracker — so a hidden flight never shows up as a racer to someone it's
  hidden from.

### 6.5 The tracker

The live tracker is reachable two ways, and adapts to how it was opened:

- **From a single flight** in a trip → a focused view of that one flight: its
  position, its track, its status.
- **As a "who's on their way" view** → a map showing the live movement of all
  the trackable travel (today, flights) across the trips currently visible to
  the user, within an adjustable time window. When several friends are heading
  to the same place around the same time, they naturally cluster here — this is
  the "watch the race" experience, and the spiritual successor to Aerly's
  original "who's already in the air?" feature. It's just a shared live map of
  everyone's movement — there is no leaderboard, podium, or "winner."

The time window for the tracker is **user-adjustable** (e.g. from a week before
to a week after now), so people who travel early or stay late to sightsee are
still included. The window is a simple control the user can widen or narrow.

### 6.6 Tags: finding each other without the ceremony

Rather than a formal "event" anyone has to create and own, a group coordinates
through an **optional shared tag**:

- Anyone can put a tag on their own trip — creating a tag is just typing it the
  first time.
- Others in the group add the **same tag** to their own trips to opt in. The tag
  input is an autocomplete: as you type it suggests tags already present on trips
  you can see, so joining a group's tag is usually a tap rather than retyping it
  exactly. (It only ever suggests tags from trips already visible to you, in
  keeping with "tags group, they never grant.")
- A tag **groups, it never grants access**: tagging your trip never exposes it
  to anyone who couldn't already see it. You only ever see tagged trips that are
  already shared with you. Two unrelated groups can use the same word with no
  overlap.
- When the tracker is viewed for a tag, the default time window automatically
  spans all the tagged trips the viewer can see (with the slider still available
  to widen or narrow). Users who don't use tags simply use the window control
  directly.

Tags are entirely optional. They exist to give a group a lightweight rallying
point — "tag it `pgconf-eu-26`" — without anyone having to host or manage an
event.

---

### 6.7 Subscribe from your own calendar (iCal)

Aerly publishes read-only calendar feeds (iCal / ICS) that people can subscribe
to from Apple Calendar, Google Calendar, Outlook, and the like, so their plans
sit alongside the rest of their life and refresh automatically as plans change.
Feeds come at three scopes:

- **Personal** — everything across the trips on your Trips list, so your whole
  travel schedule lives in your everyday calendar.
- **Per-trip** — a single trip, handy for dropping one itinerary into a calendar.
- **Per-tag (the gathering)** — all the plans across the tagged trips you can
  see, so a group heading to the same place can put the combined schedule on one
  calendar. This is the calendar counterpart of the tracker's "who's on their
  way" view; since events aren't modelled as such, the tag is the unit here.

Each feed is a private, unguessable link tied to the person who created it, and
it shows exactly what that person is allowed to see in the app — the same sharing
and per-plan privacy rules apply, so a plan hidden from someone never appears in
their feed. A feed link can be regenerated to revoke the old one.

Each plan becomes a calendar entry with its time (in local time), title,
location, and confirmation details.

## 7. Key journeys

**A. Build a trip from my inbox.**
Create "Lisbon, October". Forward the flight confirmation, the hotel email, and
the airport-transfer email. Paste the reply from the local guide about the day
trip. Confirm each extracted plan. The timeline now shows the whole trip,
day by day, without typing it out.

**B. Travel with friends to the same event.**
Each person builds their own trip and tags it `pgconf-eu-26`. Each shares with
the others. In the run-up they can see each other's plans; on travel day they
open the tracker for the tag and watch everyone converge on the city.

**C. Day-of logistics.**
On the morning of departure, open today's plans, see the flight is on time via
the tracker, and have the taxi pickup time and hotel address one scroll away.

---

## 8. Impact on existing users

- Aerly already contains people's flights. On the switch to trips, each user's
  existing flights are gathered into a simple **"Imported flights" trip** per
  user. From there users can move them into real trips they create. We keep this
  migration deliberately simple rather than guessing how past flights should be
  grouped.
- The original social "who's in the air" experience is preserved, reframed as
  the tracker's "who's on their way" view (Section 6.5), scoped by the trips a
  user shares and, optionally, by tags.

---

## 9. Decisions and open questions

Resolved since the first draft:

- **Sharing & privacy.** Owner / editor / viewer at the trip level, plus a
  per-plan "Who can see this?" override (everyone / hidden from / only visible
  to) — see §6.4.
- **Plan types at launch.** Flight, train, hotel, ground transport, dining, and
  excursion. Free-form notes are a field on a plan and on the trip, not a
  standalone timeline entry.
- **Tag discovery.** An autocomplete input that suggests tags already on trips
  the user can see — see §6.6.
- **Tracker "inbound-leg" highlighting.** Deferred. At launch the tracker simply
  shows everyone's trackable travel within the window and lets the clustered map
  tell the story; automatically singling out each person's arriving leg can come
  later if the plain view proves too busy. Either way there is no leaderboard or
  ranking — just the live map.

Still open:

- When a passenger is added to a plan, should we offer to also add them as a trip
  viewer? (Leaning yes, as an optional prompt.)
- Does any plan type need special timeline treatment beyond an icon and a time —
  e.g. a multi-night hotel shown as a band spanning its nights rather than a
  single point?

## 10. Possible future directions (not in this phase)

- Mobile app with live location sharing, so the tracker can follow people on the
  ground (e.g. walking from the station), not just while a flight is in the air.
- Tracking for other transport types (train, etc.) feeding the same tracker.
- Richer tags (descriptions, colours) if the lightweight label proves popular.
