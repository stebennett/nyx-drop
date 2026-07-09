# Frontend Implementation Notes

> **Visual reference:** static HTML mockups for every page and state live in
> [`ui-mockups/`](ui-mockups/) — upload states 0–4 (start at `upload-1-dropzone.html`),
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

States (single page, sections shown/hidden):

1. **Token entry** — shown when `localStorage.uploadToken` is unset. One password input
   + save button. No validation call; the token is proven on first upload (a 401 clears
   it and returns here with an error message).
2. **Drop zone** — big target; also a `<input type="file" webkitdirectory multiple>`
   and a separate `<input type="file" accept=".zip">` for click-to-browse. Drag-over
   highlight via `dragenter`/`dragleave` counters.
3. **Uploading** — progress bar + percentage + cancel button (`xhr.abort()`).
4. **Success** — the site URL as a link, a copy button
   (`navigator.clipboard.writeText`), the expiry time ("expires 2026-07-10 12:00 UTC"),
   and an "upload another" reset link.
5. **Error** — human message from the JSON `error` field (fallback: generic text),
   retry affordance.

### Upload mechanics — must use `XMLHttpRequest`

`fetch` has no upload-progress events. This is the one place XHR is required:

```js
const xhr = new XMLHttpRequest();
xhr.open("POST", "/api/sites");
xhr.setRequestHeader("Authorization", `Bearer ${token}`);
xhr.upload.onprogress = (e) => { if (e.lengthComputable) bar.value = e.loaded / e.total; };
xhr.onload = () => xhr.status === 201 ? showSuccess(JSON.parse(xhr.response)) : showError(...);
xhr.send(formData);
```

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

## Design/style guidance

One shared `app.css`: system font stack, single accent color, max-width ~720px column
for the upload page, full-width table for admin. Light and dark via
`prefers-color-scheme`. No icons/emoji-as-UI; text labels. This is an internal tool —
clean and boring beats clever.
