# Support Center

## Goal

Build a lightweight, fast, secure ticket support center.

This project only handles support tickets.  
Do not build a knowledge base, live chat, or unnecessary complex SPA features.

Primary goals:

- Extremely fast page loads
- Minimal client-side JavaScript
- Server-side rendering
- Clean and modern UI
- Strong security
- Simple and maintainable architecture

---

## Stack

Use:

- Go
- `net/http`
- templ
- HTMX
- Tailwind CSS
- Auth0
- PostgreSQL
- pgx
- Local private filesystem storage for attachments
- Docker

Deploy the application and PostgreSQL on the VPS. Keep Auth0 for authentication.
Do not require Cloudflare R2, S3, or another external attachment storage service.

Do not introduce React, Vue, Svelte, Next.js, or another SPA framework.

Avoid unnecessary dependencies and abstractions.

---

## Architecture

Prefer server-driven HTML.

```text
Browser
  ↓
Go
  ↓
PostgreSQL
  ↓
templ
  ↓
HTML
```

For interactive actions:

```text
HTMX request
  ↓
Go handler
  ↓
Database
  ↓
templ HTML fragment
  ↓
HTMX DOM update
```

Do not create JSON APIs when an HTML response is sufficient.

Use JavaScript only when HTMX/CSS cannot reasonably handle the requirement.

---

## Main Routes

User:

```text
/login
/logout

/tickets
/tickets/new
/tickets/{id}
```

Staff:

```text
/admin
/admin/tickets
/admin/tickets/{id}
```

Core actions:

- Create ticket
- View ticket
- Reply to ticket
- Close ticket
- Upload/download attachment
- Change ticket status
- Change priority
- Internal staff notes
- Assign staff

---

## Authentication

Use Auth0 as a Regular Web Application.

Use server-side sessions.

Session cookies must be:

```text
HttpOnly
Secure
SameSite=Lax
```

Never store authentication tokens in `localStorage`.

Never expose:

```text
AUTH0_CLIENT_SECRET
SESSION_SECRET
database credentials
```

to the browser.

Roles:

```text
user
support
admin
```

Authorization must always be enforced server-side.

---

## Ticket Authorization

Users may only access their own tickets.

Never authorize using only the ticket ID.

Bad:

```sql
SELECT * FROM tickets
WHERE id = $1;
```

Good:

```sql
SELECT * FROM tickets
WHERE id = $1
AND user_id = $2;
```

The same rule applies to:

- ticket replies
- attachments
- ticket updates
- downloads

For unauthorized ticket access, prefer returning `404`.

Staff/admin permissions must also be checked server-side.

Never rely on hidden buttons or frontend state for security.

---

## Security

All user input is untrusted.

### XSS

Keep templ escaping enabled.

Never render user-controlled ticket content using:

```go
templ.Raw()
```

Ticket messages should initially be plain text.

### CSRF

All state-changing requests must require CSRF protection:

```text
POST
PUT
PATCH
DELETE
```

HTMX requests must include the CSRF token.

### SQL

Always use parameterized queries.

Never concatenate user input into SQL.

### HTMX

Configure HTMX defensively:

```javascript
htmx.config.selfRequestsOnly = true
htmx.config.allowScriptTags = false
htmx.config.historyCacheSize = 0
```

Do not allow sensitive ticket pages to be stored in HTMX history cache.

### Headers

Add appropriate security headers, including:

```text
Content-Security-Policy
Strict-Transport-Security
X-Content-Type-Options
Referrer-Policy
Permissions-Policy
```

Use a restrictive CSP. Avoid `unsafe-eval`.

---

## Attachments

Attachments must be private.

Do not store uploads in a publicly executable web directory.

Use random object keys instead of user filenames.

Store attachments in a configurable private directory on the VPS, outside the
public web root. Mount this directory as a persistent Docker volume so uploads
survive container replacement. Store attachment metadata and generated storage
keys in PostgreSQL; never use a user-supplied filename as a filesystem path.

Resolve files only through server-generated storage keys, prevent path traversal
and symlink escapes, and restrict filesystem permissions to the application user.

Validate:

- maximum size
- allowed MIME type
- file content
- number of attachments

Do not trust the client-provided MIME type or extension.

Initial allowed formats should be limited to common support files such as:

```text
jpg
png
webp
txt
log
pdf
zip
```

Attachment downloads must verify that the current user has access to the associated ticket.

Serve downloads through an authenticated Go handler after checking ticket access.
Do not expose the attachment directory through static file serving or permanent
public URLs. Use a safe Content-Disposition attachment filename and
X-Content-Type-Options: nosniff on downloads.

Enforce per-file and per-request upload limits, clean up partial uploads on
failure, and handle storage exhaustion without leaving inconsistent metadata.
Back up both PostgreSQL and the attachment directory, and document restoration.

---

## UI

Use Tailwind CSS.

Target a clean modern style similar in quality to products such as Linear, Vercel, Stripe, or Cloudflare, without directly copying them.

Keep the interface simple.

Prefer:

- clear typography
- subtle borders
- no decorative shadows or dashboard cards
- square corners and clearly separated rectangular panels
- light/dark appearance follows the browser or operating system by default; a small sun/moon icon shows the current mode and allows switching, remembering the choice in this browser
- neutral grayscale palette; reserve color for meaningful status or errors
- only task-relevant copy; no slogans, decorative English labels, or repeated helper text
- no external font, CSS, or JavaScript CDN requests
- 100–200ms transitions
- good spacing
- clear ticket status badges
- responsive layout

Avoid:

- excessive animation
- glassmorphism everywhere
- unnecessary gradients
- oversized UI
- heavy JavaScript UI libraries

Build reusable templ components for common UI elements.

---

## Project Structure

Prefer a simple structure similar to:

```text
cmd/server/

internal/
  auth/
  ticket/
  user/
  attachment/
  database/
  middleware/

web/
  layouts/
  pages/
  components/
  assets/

db/
  migrations/
  queries/
```

Do not create layers or abstractions without a real need.

---

## Database

Use PostgreSQL.

Core tables:

```text
users
tickets
ticket_messages
ticket_attachments
sessions
```

Optional:

```text
ticket_events
```

Add indexes for common lookups such as:

```text
tickets.user_id
tickets.status
tickets.updated_at
ticket_messages.ticket_id
users.auth0_id
```

Avoid N+1 queries.

---

## Performance

Performance is a core requirement.

Prefer:

- SSR
- small HTML responses
- HTMX fragments
- minimal JS
- efficient SQL
- proper indexes
- pagination

Avoid:

- SPA hydration
- large JS bundles
- unnecessary API layers
- unnecessary client state
- unnecessary dependencies

Do not optimize meaningless microbenchmarks at the expense of maintainability.

Database queries and network latency are more important than shaving tiny amounts of handler execution time.

---

## Error Handling

Never expose internal errors or stack traces to users.

Use appropriate HTTP status codes.

Log detailed errors server-side.

Never log:

- session cookies
- access tokens
- secrets
- CSRF tokens
- attachment contents

---

## Testing

Security tests are required.

At minimum test:

- User A cannot view User B ticket
- User A cannot reply to User B ticket
- User A cannot download User B attachment
- normal users cannot access admin routes
- support/admin can access permitted routes
- invalid CSRF requests fail
- unauthenticated users cannot access protected routes
- XSS payloads render as text
- SQL injection input does not alter queries

Authorization bugs are considered critical.

---

## Development Rules

When implementing features:

1. Keep the solution simple.
2. Prefer standard Go libraries where reasonable.
3. Render HTML on the server.
4. Use HTMX for partial updates.
5. Keep client JavaScript minimal.
6. Validate everything server-side.
7. Enforce authorization on every protected operation.
8. Use parameterized SQL.
9. Keep attachments private.
10. Do not introduce new frameworks without a clear need.
11. Do not over-engineer.
12. Do not sacrifice security for convenience.

When multiple implementations are possible, prefer the one with fewer moving parts while preserving security, usability, and performance.
