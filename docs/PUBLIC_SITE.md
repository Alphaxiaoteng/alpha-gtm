# PLEIAD Public Site and Domain Boundary

## Canonical domain

| Field | Value |
|---|---|
| Public product | PLEIAD |
| Canonical origin | `https://pleiadops.com` |
| Domain check | `.com` RDAP returned not found on 2026-08-09; no nameserver was present |
| Registration | **Pending — availability is not ownership until purchased** |
| Legal clearance | Pending trademark and jurisdiction-specific review |

`Alpha-Nexus EgoLite` remains the internal repository/runtime identifier. Do not rename package paths, application data directories, bundle IDs, or binaries as part of the website launch.

## Host allocation

| Host | Purpose | Boundary |
|---|---|---|
| `pleiadops.com` | Product positioning, use cases, comparisons, research, and conversion | Canonical public source |
| `www.pleiadops.com` | Permanent redirect to apex | No duplicate content |
| `docs.pleiadops.com` | Public product documentation | No account secrets or private runtime data |
| `download.pleiadops.com` | Signed desktop downloads when release infrastructure exists | Not allocated until packaging/signing is ready |
| `updates.pleiadops.com` | Desktop update artifacts when implemented | Not allocated until update verification exists |

Do not expose the local Electron UI, SQLite database, EgoLite runtime, or a public `api.*` host. The marketing site is a separate deployment boundary.

## Information architecture

```text
/
/product
/use-cases/content-commerce-operations
/use-cases/multi-account-publishing
/use-cases/browser-agent-human-takeover
/commerce
/platforms/{platform}
/compare/{competitor}
/research/ai-search-visibility
/research/verified-publishing
/docs
/changelog
/about
```

Each page owns one search intent and one canonical URL. The homepage explains the category; platform and use-case pages provide specific evidence and limitations.

Every feature or platform claim must expose one of five states: `available`, `experimental`, `manual handoff`, `unverified`, or `roadmap`.

## Search and AI discoverability

- Render important content as accessible HTML text.
- Publish `robots.txt`, XML sitemap, RSS/Atom, canonical links, and stable update dates.
- Allow `Googlebot`, `Bingbot`, and `OAI-SearchBot` when public discovery is desired.
- Use `Organization`, `SoftwareApplication`, `Article`, and `BreadcrumbList` structured data only when the visible page supports it.
- Product/commerce pages use `Product` and `Offer` data with matching visible price, availability, shipping, and returns.
- `llms.txt` may be offered as an experimental directory, but it is not a ranking requirement or a substitute for crawlable pages.

## Omnichannel publishing loop

```text
brand/product facts
  -> canonical website page
  -> schema + sitemap + IndexNow where supported
  -> platform-specific media and commerce adaptations
  -> frozen batch and preflight
  -> explicit approval
  -> controlled submit or manual takeover
  -> URL/status/screenshot evidence
  -> click/order/refund/margin attribution
  -> refresh the canonical source
```

Do not publish identical copy everywhere. Keep facts stable while adapting hook, format, length, media, and call to action for each platform.

## Commercial attribution contract

The public site and publishing system should preserve this joinable chain:

```text
query_id -> source_page_id -> campaign_id -> publish_job_id
         -> platform_post_id -> click_id -> order_id -> refund_id
```

Minimum reporting:

- Brand and non-brand discovery.
- AI citation/referral source when observable.
- Verified publication success and manual takeover rate.
- Landing-page conversion, order value, refunds, and contribution margin.
- Operator time per verified campaign.

## Launch gate

1. Register the domain in the owner's registrar account.
2. Complete a basic trademark and company-name screen in target markets.
3. Deploy a static public site separately from the local application.
4. Verify HTTPS, redirects, robots, sitemap, schema, and analytics consent.
5. Submit the site to Google Search Console and Bing Webmaster Tools.
6. Publish only product claims supported by current repository/runtime evidence.
