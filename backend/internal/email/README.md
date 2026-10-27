# Password-reset email

`password_reset.html` implements the approved **Option B — Dark fintech** design:
near-black background, charcoal message and action panels, pink reset button,
Arial/Helvetica typography and the existing Vellum mark. Keep this composition
when maintaining the template.

The template and 128px PNG are embedded in the Go binary. SES receives a Simple
message with the existing subject/plain-text alternative and one `INLINE`
`image/png` attachment, referenced as `cid:vellum-mark@vellum`. The pinned SDK
handles transport encoding; there are no remote image requests, runtime asset
files, raw-MIME delivery or new AWS permissions. Each request owns its PNG bytes.

The renderer uses `html/template` contextual escaping. Only two fixed,
developer-owned Outlook wrapper constants use trusted HTML, because Go otherwise
strips conditional comments. URLs always remain ordinary strings. HTTPS reset
path/fragment shape is checked before sending; configuration and services retain
ownership of allowed environment hosts and token validation. Errors omit links,
tokens and recipient addresses. No authentication code is changed.

The configured lifetime is rounded up to minutes exactly as before and appears
in the preheader, expiry panel and unchanged plain text. The sample preview uses
20 minutes; no production lifetime is hardcoded into the HTML.

## Safe local preview and rendering regression

From `backend/`, capture the actual sender output with synthetic data:

```sh
go run ./cmd/preview-password-reset-email -out /tmp/vellum-reset-preview
# Exercise a non-default, fractional-minute lifetime too:
go run ./cmd/preview-password-reset-email -ttl 7m1s -out /tmp/vellum-reset-expiry
```

This command constructs only an in-memory capture client. It cannot load AWS
credentials or send mail. `production.html` is the exact HTML captured from the
SES request; `preview.html` differs only by resolving its CID to the captured
local PNG. The accompanying `plain-text.txt` is the actual text alternative.
All links use the reserved `.example` host and a marked synthetic token.

From the repository root, with existing frontend Playwright dependencies and a
browser installed:

```sh
node scripts/check-password-reset-email.cjs --preview /tmp/vellum-reset-preview
# Optional installed Chrome instead of Playwright's bundled Chromium:
PLAYWRIGHT_CHANNEL=chrome node scripts/check-password-reset-email.cjs --preview /tmp/vellum-reset-preview
```

Optional `--reference /absolute/path/to/approved/dark.html` compares browser
pixels at desktop, 390px and 320px, failing above 1% differing pixels. The supplied
reference must use the same sample lifetime and synthetic URL as the preview.
Rendering blocks external requests, checks complete content/links/logo and saves
screenshots plus `render-report.json`. It exercises light/dark browser preferences,
blocked images and Windows forced-colours simulation. Run it on the fractional
expiry capture too to check a lifetime other than 20 minutes. Go rendering,
escaping, accessibility, envelope/attachment and concurrency regression tests
run as part of the normal backend suite.

## Compatibility and operational limits

Table layouts, inline styles, explicit pixel line heights/background colours and
a conditional 600px wrapper support common clients. The text wordmark, expiry,
action and full fallback URL remain visible when images are blocked. The dark
button label on pink has a 5.33:1 contrast ratio. Colour-scheme metadata is a hint;
clients can still alter colours. Classic Outlook may square corners and vary
spacing or the button's clickable area; the underlined fallback remains usable.

Browser renders do **not** certify delivered Gmail, Outlook or Apple Mail output,
client-specific dark-mode inversion or SES-generated MIME. A separately approved
inbox check should verify the inline logo, text alternative, reset link and dark
mode before rollout. No real email is sent by the tests or preview command.

No new configuration, database migration or infrastructure change is required by
this presentation change. Delivery enablement, deployment and existing recovery
rollout prerequisites remain operator-controlled; see
[`deploy/PASSWORD-RESET.md`](../../../../deploy/PASSWORD-RESET.md). PR #31 is outside
this change.
