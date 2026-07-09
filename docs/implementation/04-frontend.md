# Frontend Implementation Notes

> **Visual reference:** static HTML mockups for every page and state live in
> [`ui-mockups/`](ui-mockups/) — upload states 1–4 (start at `upload-1-dropzone.html`),
> admin states (`admin-1-list.html`: list with permanent/expired rows, `admin-2-empty.html`,
> `admin-3-error.html`), plus `unauthorized.html` and the self-contained `notfound.html`.
> Use the footer nav to step through them. The real pages must match their layout, copy,
> and styling; `mockup.css` is the starting point for `web/static/app.css`. The striped
> top banner and the footer state-nav are mockup chrome — do not implement them.

Everything lives in `web/`, embedded with `//go:embed web` and served by the app.
**No framework, no bundler, no npm, no external requests** (fonts, CDNs — nothing;
the pages must work in an airgapped cluster). Vanilla HTML + CSS + JS modules.

```
web/
├── upload.html      # GET /
├── admin.html       # GET /admin
├── notfound.html    # branded 404 for site hosts / unknown hosts
├── unauthorized.html# 403 page after failed GitHub login
└── static/
    ├── app.css      # shared styles
    ├── upload.js
    └── admin.js
```

Keep pages small and legible; a competent reviewer should read each file top to bottom.
Progressive enhancement matters less than clarity — JS is required, that's fine.

## Upload page (`upload.html` + `upload.js`)

The page never asks for a credential. States (single page, sections shown/hidden):

1. **Drop zone** — big target; also a `<input type="file" webkitdirectory multiple>`
   and a separate `<input type="file" accept=".zip">` for click-to-browse. Drag-over
   highlight via `dragenter`/`dragleave` counters.
2. **Uploading** — progress bar + percentage + cancel button (`xhr.abort()`).
3. **Success** — the slug as the hero line, the site URL as a link, a copy button
   (`navigator.clipboard.writeText`), the expiry time with the lifetime moon, and a
   "deploy another" reset link.
4. **Error** — human message from the JSON `error` field (fallback: generic text),
   retry affordance.

### Upload mechanics — grant first, then `XMLHttpRequest`

Each upload is a two-step: fetch a single-use grant, then POST with it. `fetch` has no
upload-progress events, so the upload itself must use XHR:

```js
const { grant } = await (await fetch("/api/upload-grant")).json();
const xhr = new XMLHttpRequest();
xhr.open("POST", "/api/sites");
xhr.setRequestHeader("Authorization", `Bearer ${grant}`);
xhr.upload.onprogress = (e) => { if (e.lengthComputable) bar.value = e.loaded / e.total; };
xhr.onload = () => xhr.status === 201 ? showSuccess(JSON.parse(xhr.response)) : showError(...);
xhr.send(formData);
```

On a `401` (grant expired mid-upload or consumed), fetch a fresh grant and retry the
upload **once** before showing the error state. Grants are single-use, so "deploy
another" simply runs the two-step again — nothing is cached in `localStorage`.

### Building the FormData

- **Single `.zip` file dropped/picked** → `formData.append("file", zipFile)`.
- **Folder dropped** → walk `DataTransferItem.webkitGetAsEntry()` recursively
  (directories yield readers whose `readEntries` must be called repeatedly until it
  returns an empty batch — it only returns ~100 entries per call; this is a classic
  bug). For each file: `formData.append("files", file, relativePath)` — the third
  argument sets the slash-containing filename.
- **Folder via `<input webkitdirectory>`** → use each file's `webkitRelativePath`.
- **Multiple loose files dropped** → `files` fields with their bare names.
- Skip dotfiles client-side too (cheap; the server enforces regardless).

## Admin page (`admin.html` + `admin.js`)

Layout: header (title, logged-in indicator, logout button = form POST to
`/auth/logout`), search box, table, pager.

- **Load**: `fetch("/api/admin/sites?" + params, {credentials:"same-origin"})`. A 401
  response → `location.href = "/auth/login"`.
- **Table columns**: slug (link, `target="_blank"`), created, updated, expires,
  size, files, source, actions. Slug row for an `expired: true` site renders dimmed
  with an "expired — pending removal" badge.
- **Times**: render relative ("in 3 h", "2 d ago") with the absolute RFC 3339 in a
  `title` tooltip. Small hand-written helper (~15 lines); no library. Permanent sites
  show "permanent" in the expires column.
- **Sizes**: human-readable (KB/MB) helper.
- **Sorting**: clickable column headers for name/created/expires toggle
  `sort`/`order`, reflected in the URL query string (so refresh keeps state).
- **Search**: input debounced 300 ms → resets to page 1 with `q=`.
- **Pager**: prev/next + "page N of ⌈total/per_page⌉".
- **Actions per row**:
  - Permanent toggle → `POST`/`DELETE …/permanent`, then re-fetch the current page.
  - Download → plain `<a href=".../download">` (browser handles the attachment).
  - Delete → `confirm("Delete <id>? This cannot be undone.")` then `DELETE`, re-fetch.
- All mutating fetches send `credentials: "same-origin"`; on 401 redirect to login;
  on other errors show a transient toast/banner with the JSON error message.

## 404 page (`notfound.html`)

Static branded page: "This site doesn't exist — it may have expired." Link to `$BASE`.
Served with status 404 for unknown/expired site hosts and unknown hosts. Keep it
self-contained (inline CSS is fine) since it's served on hosts where `/static/` does
not exist.

## Design system ("dusk")

The identity: Nyx is night, and sites are ephemeral — they rise on animal-named
subdomains and fade. `mockup.css` in `ui-mockups/` implements all of this; copy it as
the basis of `web/static/app.css` rather than re-deriving.

- **Palette:** dusk indigo, dark-first. Night: bg `#0c111f`, surface `#151c30`, border
  `#2a3350`, text `#e9ecf6`. Accent is **moon gold** `#e9c763` (identity/signature
  elements, primary buttons, focus rings) with periwinkle `#9db0f2` for links. The light
  theme is "dawn" (pale blue-grey `#eef1f8`, gold darkened to `#b8912c` for contrast).
  Both themes via `prefers-color-scheme`.
- **Type:** three roles. Display = `ui-serif, Georgia` (headings, wordmark); UI =
  `system-ui`; data = `ui-monospace` for slugs, URLs, filenames, and tabular numbers.
  Slugs are celebrated: the success screen shows the animal pair large in mono
  (`.slugline`, suffix muted).
- **Signature — the lifetime moon:** remaining TTL rendered as a waning moon (SVG,
  16px): full when fresh, waning as expiry nears, dark outline when expired; permanent
  sites show a four-point star instead. Shown next to every expiry ("in 22 h") in the
  admin table and on the upload success line. Geometry for fraction-remaining `f` in a
  20×20 viewBox (r=8, center 10,10): `f=1` → full lit circle; `f=0` → outline only;
  otherwise path `M10 2 A8 8 0 0 1 10 18 A{rx} 8 0 0 {sweep} 10 2` with
  `sweep=1, rx=(2f−1)·8` when `f≥0.5` and `sweep=0, rx=(1−2f)·8` when `f<0.5`.
  Lit fill = moon gold; outline = border color. Always paired with the text form —
  the moon is never the only carrier of the information.
- **Atmosphere, with restraint:** a static CSS starfield (layered `radial-gradient`
  dots) inside the drop zone only — faint at dawn, bright at night; drag-over glows
  gold. No ambient animation; if any motion is added later it sits behind
  `prefers-reduced-motion: no-preference`.
- **Quality floor:** visible `:focus-visible` rings (gold), 720px column for the
  upload page / 1140px for admin, table scrolls horizontally on small screens,
  `notfound.html` reproduces the palette + crescent inline (it can't reference
  `/static/`).
