
# Brand Customization Contract

> Status: **Frozen v1** · Milestone M3 (same pipeline as Remote Config) · Frozen 2026-10-09.
> Purpose: the optional UI package displays the **integrator's** branding instead of Courier's own marks. **Core contains no branding logic** — the contract is fields only; consumption lives entirely in the UI package. The domain prefix is registered ([errors.md](./errors.md): this domain has no dedicated error codes); default Provider **scribe** (same pipeline as config/app).

## Resource Model

```json
{
  "version": 3,
  "companyName": "示例互娱",
  "productName": "幻想大陆",
  "logo": {
    "svg": "https://…/logo.svg",
    "png": { "1x": "https://…/logo@1x.png", "2x": "https://…/logo@2x.png", "3x": "https://…/logo@3x.png" }
  },
  "icon": { "android": "https://…/icon.png", "ios": "https://…/icon.png", "web": "https://…/icon.png" },
  "theme": { "primaryColor": "#4C8DFF", "secondaryColor": "#1F2937" },
  "about": { "title": "关于我们", "body": "…" },
  "supportEntry": { "label": "联系客服", "url": "https://…/support" }
}
```

- `version`: the brand asset version, monotonically increasing (basis for hot-update detection); independent from config's `configVersion`.
- Colors are always `#RRGGBB`; URLs are always CDN/object-store direct links (HTTPS).
- All business fields are optional; see "Fallback Rules"; apart from `version`, field names conflict with no contract semantics (the management plane is forbidden from occupying the `version` key).

## Delivery and Caching

- Endpoint: `GET /v1/app/branding` (**anonymous OK** — the login page has to show the brand; a valid Bearer token also works), mounted on the App domain route (`/v1/app/*`, within the maintenance-gate allowlist); scope header requirements are the same as [auth.md](./auth.md).
- Branding never set → `200 {"version": 0}` (empty fields, the UI falls back to defaults across the board); an empty set is not an error.
- The response is cached as a whole (invalidated and re-pulled when `version` changes); caching strategy belongs to the platform Adapter's storage implementation, Core does not dictate caching.
- Pipeline: `BrandingProvider` (default scribe; assets live in object storage) → Gateway pass-through projection → SDK pass-through → UI package consumption. Same pipeline as Remote Config.
- Hot update: via the [messages.md](./messages.md) channel `branding.updated` event; the data carries only `{"version": n}` (broadcast; after receiving it, the client re-pulls and compares, ignoring if identical).

## Fallback Rules

- Any missing field → the UI package uses that field's Courier built-in default.
- The capability not configured / not enabled (`501 COMMON_CAPABILITY_DISABLED`) → the SDK returns a "not enabled" state (null), the UI uses all defaults, and Courier's own marks are the fallback.
- Core and Service do zero interpretation and zero rendering of the fields (raw JSON is passed through); caching belongs to the platform Adapter's storage implementation.

## Error Codes (Domain Table)

This domain adds **no new error codes**: parameters / auth / rate limiting / capability disabled are all common error codes ([errors.md](./errors.md)).

## Client Requirements (per-platform SDKs)

- The SDK passes the raw JSON through (fields other than `version` are handed to the UI package as-is), with no field validation or type narrowing.
- After the `branding.updated` event arrives, re-pull and compare `version`, ignoring if identical (no re-render).
- The UI package holds Courier's built-in default mark constants, serving as the fallback for all fields (consumption example: the Unity UI package's BrandingCatalog).

## Build-time Asset Appendix (Guidance, Not Contract)

The UI package consumes the brand delivered above at runtime; build-time assets such as app icons cannot be delivered hot, so each engine's template project must provide replacement guidance:

| Platform | Checklist |
| --- | --- |
| Unity | Player Settings icons (per size), splash screen, package-name related brand slots |
| iOS | AppIcon at all sizes (@2x/@3x for 20/29/40/60/76/83.5 pt), launch screen |
| Android | `mipmap-mdpi … xxxhdpi` launcher icons + adaptive icons (foreground/background), app name |
| Mini-program / mini-game | Platform admin configuration items (icon, name, description) checklist |
| UE / Godot / Layabox | The icon and splash-image checklists in each engine's packaging settings |

Template projects ship placeholder resources plus a `BRANDING.md` checklist, making "replacing the integrator logo" a process rather than an archaeology dig (template projects land with each per-platform template batch; this appendix is where the guidance is filed).

---

## Extension Reservations (Note Added 2026-10-11)

| Possible Extension | Compatible Handling |
| --- | --- |
| New brand fields / new asset slots (splash page / icon badge, etc.) | **Adding optional fields** = compatible; key/value delivered through scribe's management plane; the UI package consumes what it needs; Core's "no branding logic" red line is unchanged |
| Field absence | The client walks the fallback chain (remote → host → built-in default); a missing field simply falls back, no error |
| Asset hosting / CDN / signed URLs | The integrator's responsibility; the contract stores only URL strings (same convention as player avatars) |
| Theme switching / dark and light | Hot switching via the `version` basis is already built in; adding a theme dimension = adding an optional field |
