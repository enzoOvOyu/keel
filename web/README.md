# keel web

React 19 / TypeScript 5 / Vite 6 foundation. `/` redirects to `/login` through TanStack Router file routes; TanStack Query fetches the public identity bootstrap contract.

The login page is an environment preview. Its submit button stays disabled and credentials are never submitted. The backend currently reports `{"status":"not_configured","methods":[]}` at `GET /api/v1/auth/providers`. Real identity/session flows remain P1 work. Unexpected identity configuration fails closed instead of enabling a fake login.

From the repository root after workspace installation:

```sh
npm run typecheck --workspace @keel/web
npm run build --workspace @keel/web
npm run dev --workspace @keel/web
```

Vite listens on `127.0.0.1:5173`, proxying `/api` and `/healthz` to `127.0.0.1:3000`. Production serves `web/dist` from the Go server; Vite preview is for static inspection only and does not proxy the API.

`src/styles/tokens.json` is the sole color/token source. `npm run tokens` generates matching light/dark CSS. Local storage contains only `keel.theme` and `keel.language`; never credentials or bearer tokens. The site uses local SVG and system fonts, with no third-party assets.

Validation: run typecheck/build; verify `/login` in light/dark and mobile layouts; keyboard labels, password visibility and disabled submit; provider connection failures with retry; and independent `/healthz` access. No business data or success metrics are fabricated.
