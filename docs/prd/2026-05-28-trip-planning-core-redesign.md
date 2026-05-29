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

The shape we're aiming at is a modern, well-maintained take on trip planning —
the kind of itinerary organiser that has been badly neglected elsewhere —
combined with the live tracking and social "who's on their way" experience Aerly
already does well.

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
- Treat a booking that spans several timeline entries — return flights, multi-leg
  journeys, a hotel stay — as a single plan.
- Make adding plans nearly effortless via email, upload, and paste, in addition
  to manual entry.
- Present a trip as a clean, day-by-day vertical timeline.
- Let users share a trip with friends and plan collaboratively.
- Let users subscribe to their plans from their own calendar app via read-only
  iCal feeds, at the traveller, trip, and individual-plan level.
- Alert travellers in-app and by email when a flight is delayed, changed, or
  cancelled, and make folding in a rebooking easy.
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
- **Plan** — one booking, which may show up on the timeline as a single entry or
  several. A simple plan is one entry (a dinner, a taxi). A flight booking can
  have several **parts** — outbound, return, and any connecting legs — each its
  own timeline entry but all one plan. A hotel booking has a check-in and a
  check-out. Every plan has a title, any confirmation details, and a free-form
  notes field; each part has its own time (or time range) and location. Notes are
  a field *on* a plan (and on the trip itself) — not a timeline entry in their own
  right.
- **Part** — one timeline entry belonging to a plan (a single flight leg, a hotel
  check-in). Sharing, passengers, and privacy are set on the plan and apply to all
  its parts.
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

Because a single booking can have several parts, those parts appear as separate,
chronologically-placed cards that are visually linked as one plan — a return
flight's outbound and inbound legs, or a hotel's check-in and check-out, read as
the same booking even when they're days apart. A multi-night hotel stay shows as
a continuous band across the days it covers rather than two unrelated points.
Plans that have been changed or cancelled stay on the timeline **greyed out**,
with their replacement shown alongside (see §6.9), so any knock-on effects on
other plans are easy to spot.

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
is flagged rather than silently guessed. A single confirmation often covers a
whole round trip or a multi-leg journey; Aerly groups those into one plan with
several parts rather than a scatter of unrelated entries. This effortless capture
is the feature we expect users to fall in love with.

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
specific flight). Adding someone as a passenger automatically makes them a
**viewer** of the trip — they're genuinely on it, so they can see the whole
itinerary. Per-plan privacy (below) still lets the owner keep individual plans
hidden from them.

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

- **Traveller (personal)** — everything across the trips on your Trips list, so
  your whole travel schedule lives in your everyday calendar.
- **Trip** — a single trip, handy for dropping one itinerary into a calendar.
- **Plan** — a single entry (one flight, hotel, dinner, …), for when you just
  want that one thing on your calendar. Because it stays subscribed, a change
  such as a delayed flight flows through to the calendar entry automatically.

Each feed is a private, unguessable link tied to the person who created it, and
it shows exactly what that person is allowed to see in the app — the same sharing
and per-plan privacy rules apply, so a plan hidden from someone never appears in
their feed. A feed link can be regenerated to revoke the old one.

Each plan becomes a calendar entry with its time (in local time), title,
location, and confirmation details.

### 6.8 Alerts when a flight changes

Aerly already watches the flights people add. When something changes — a delay,
a schedule or gate change, a cancellation, or a diversion — it alerts the people
on that flight both **in-app** (a notification, with the timeline updating live)
and by **email**. Alerts go to the plan's owner and its passengers by default,
and a viewer can opt in to receive them too. Each person chooses their channels
and sets a threshold so trivial changes (a two-minute slip) don't nag them. This
is the bridge between the planning side of
Aerly and the live tracker: your itinerary tells you the moment it's no longer
accurate.

### 6.9 Changes and rebookings

When a flight is cancelled or you rebook, you'll usually get a fresh
confirmation. Dropping that into Aerly (paste / upload / email, like any plan) is
recognised as a **change to an existing booking** rather than a brand-new one.
Aerly matches it to the original — by booking reference where possible, otherwise
by traveller and route — and, once you confirm the match, supersedes the old
flight: the original stays on the timeline **greyed out** and the new flight
appears alongside it. Keeping the old one visible is deliberate — it makes it
obvious whether the rest of the trip still lines up (does the airport taxi need
moving? does the hotel checkout still work?) so you can adjust the knock-on
plans. Once everything's reconciled, superseded entries can be tidied away.

### 6.10 Smart check-in and check-out times

Rather than parroting a hotel's standard 3 pm / 11 am, Aerly suggests timeline
times that reflect your actual travel:

- **Arrival / check-in** — the *later* of the hotel's standard check-in time and
  about an hour after your inbound flight lands, since you can't realistically be
  at the hotel before then.
- **Departure / check-out** — the *earlier* of the hotel's standard check-out
  time and when you need to leave for your flight: roughly two hours before
  departure for short-haul, three for long-haul.
- **Where we can:** factor in the expected travel time between airport and hotel,
  so both ends shift to allow for the journey.

These are shown as **suggestions the user can override**, and they only kick in
when a hotel and a flight sit together in the trip; with no known flight Aerly
falls back to standard times. The booking still records the real check-in /
check-out dates — this only affects the *suggested* times shown on the timeline.

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
- **Multi-part bookings.** A booking can span several timeline entries (return
  flights, multi-leg journeys, hotel stays), modelled as one plan with several
  parts; a multi-night hotel renders as a band. See §5 and §6.2.
- **Flight alerts.** In-app and email alerts on delay / change / cancellation, to
  the owner and passengers, with viewers able to opt in — see §6.8.
- **Rebookings.** A new confirmation matched to an existing flight supersedes it,
  greyed out, with the replacement shown alongside. The match is always confirmed
  by the user before it's applied — see §6.9.
- **Smart check-in / check-out times.** Suggested from the linked flight's
  arrival and departure — see §6.10.
- **Per-part privacy.** Privacy stays at the plan level — a booking is hidden as
  a whole or not at all. Hiding a single part (e.g. just the return leg) is
  treated as an unnecessary corner case.
- **Passengers and viewing.** Adding someone as a passenger on a plan
  automatically makes them a trip viewer — see §6.4.

Still open:

- None at present — the design questions raised during review have all been
  resolved above.

## 10. Possible future directions (not in this phase)

- Mobile app with live location sharing, so the tracker can follow people on the
  ground (e.g. walking from the station), not just while a flight is in the air.
- Tracking for other transport types (train, etc.) feeding the same tracker.
- Richer tags (descriptions, colours) if the lightweight label proves popular.
