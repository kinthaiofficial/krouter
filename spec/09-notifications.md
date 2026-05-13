# spec/09-notifications.md — Notification Center

**Module**: `internal/notifications`
**Source**: `https://announcements.kinthai.ai/feed.json` (Cloudflare Pages)

---

## 1. Three channels

| Channel | Source | Storage | Use |
|---------|--------|---------|-----|
| A: Local runtime | daemon internal events | not persisted | "Saved $0.45 this request" |
| B: Remote feed | Cloudflare Pages | SQLite | Free credits, new providers, kinthai products |
| C: Critical | Same as B (priority=critical) | SQLite | Security advisories, force-upgrade prompts |

---

## 2. Channel B/C: remote feed

### 2.1 Source

```
https://announcements.kinthai.ai/feed.json
```

Hosted on Cloudflare Pages (free, unlimited bandwidth for static assets).
Built from `kinthaiofficial/announcements` GitHub repo via Actions.

**No fallback chain.** No Gitee mirror. No jsDelivr. No retry chains.
If the source is unreachable, miss this poll, retry in 6h.

### 2.2 Poll strategy

```go
func (s *Service) Start(ctx context.Context) error {
    // Poll on startup
    s.pollOnce(ctx)
    
    ticker := time.NewTicker(6 * time.Hour)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            s.pollOnce(ctx)
        }
    }
}
```

No adaptive interval. No backoff. No source health tracking.
Simple = robust = enough for non-critical path.

### 2.3 ETag (REQUIRED)

99% of polls should return 304 Not Modified. ETag is mandatory:

```go
req, _ := http.NewRequestWithContext(ctx, "GET", FEED_URL, nil)
req.Header.Set("User-Agent", "krouter/1.0.0 (darwin-arm64)")
if etag := s.db.GetFeedETag(); etag != "" {
    req.Header.Set("If-None-Match", etag)
}
```

Without ETag, we'd waste 42x bandwidth for no benefit.

### 2.4 feed.json schema

```json
{
  "$schema": "https://kinthai.ai/schemas/announcements/v1.json",
  "updated_at": "2026-05-12T10:30:00Z",
  "announcements": [
    {
      "id": "anthropic-extra-2026-q2",
      "type": "free_credit",
      "priority": "normal",
      "published_at": "2026-05-10T00:00:00Z",
      "expires_at": "2026-06-30T23:59:59Z",
      "title": {
        "en": "Anthropic Q2 Bonus: 100K free tokens",
        "zh-CN": "Anthropic 二季度赠送:免费 100K tokens"
      },
      "summary": {
        "en": "New Pro plan users get 100K bonus tokens, valid until June 30.",
        "zh-CN": "新 Pro 用户额外送 100K tokens,6/30 前有效。"
      },
      "url": "https://www.anthropic.com/promo/q2-bonus",
      "icon": "🎁",
      "targets": {
        "platform": ["macos", "windows", "linux"],
        "language": ["en", "zh-CN"],
        "min_version": "1.0.0",
        "only_show_if_provider_missing": ["anthropic"]
      }
    }
  ]
}
```

#### `type` values

- `free_credit` — free tokens / signup bonuses
- `kinthai_product` — kinthai-branded other products
- `provider_news` — new provider supported, price change, new model
- `release_note` — krouter self release highlights
- `critical_warning` — security / protocol critical alerts
- `tip` — usage tips, low frequency

#### `priority` values

- `low` — only in GUI panel, no desktop notification
- `normal` — GUI panel + desktop notification (if user enabled)
- `critical` — GUI forced modal, can't dismiss easily

### 2.5 targets filtering

All filtering is **local** in daemon. No data sent anywhere.

```go
func (s *Service) matchesTargets(t Targets) bool {
    // platform
    if len(t.Platform) > 0 && !contains(t.Platform, runtime.GOOS) {
        return false
    }
    
    // language (from user settings)
    userLang := s.settings.Get().Language
    if len(t.Language) > 0 && !contains(t.Language, userLang) {
        return false
    }
    
    // version range
    if t.MinVersion != "" && semver.Less(CurrentVersion, t.MinVersion) {
        return false
    }
    if t.MaxVersion != "" && semver.Greater(CurrentVersion, t.MaxVersion) {
        return false
    }
    
    // provider-conditional
    if len(t.OnlyShowIfProviderMissing) > 0 {
        for _, p := range t.OnlyShowIfProviderMissing {
            if s.providers.HasKey(p) {
                return false  // user already has this provider
            }
        }
    }
    
    return true
}
```

### 2.6 Process new announcements

```go
func (s *Service) processAnnouncements(items []Announcement) {
    for _, item := range items {
        if s.db.AnnouncementExists(item.ID) {
            continue
        }
        if !s.matchesTargets(item.Targets) {
            continue
        }
        
        s.db.SaveAnnouncement(item)
        
        if s.shouldNotifyDesktop(item) {
            s.notifier.Send(item)
        }
        
        s.guiEventBus.Emit("announcement:new", item)
    }
}

func (s *Service) shouldNotifyDesktop(item Announcement) bool {
    if !s.settings.NotificationCategories[item.Type] {
        return false
    }
    switch item.Priority {
    case "critical":
        return true  // always
    case "normal":
        return s.settings.DesktopNotificationsEnabled
    case "low":
        return false  // panel only
    }
    return false
}
```

---

## 3. Channel A: local runtime events

In-process event bus. daemon emits, GUI subscribes via SSE on `/internal/events`.

Examples:
- After each request: "Saved $0.42" event
- Quota threshold: "5h window 90% used" event
- Provider error: "Anthropic returned 503, will retry" event

Not persisted. GUI displays in a transient "recent activity" panel.

---

## 4. Privacy

What we send to Cloudflare Pages:
- `User-Agent: krouter/1.0.0 (darwin-arm64)`
- Request IP (Cloudflare aggregates, we don't see individual IPs)
- `If-None-Match: "abc..."` (the ETag)

What we DON'T send:
- User ID / email
- Configured providers
- Routing logs
- API keys (obviously)

The transparency: announcements repo is public. Anyone can `git log` to audit
what we've ever pushed.

---

## 5. GUI integration

Status bar icon:
- No unread: clean icon
- Unread count: red dot with number
- Critical unread: red exclamation

Notification panel: side panel in GUI listing announcements grouped by Unread/Read.

Desktop notifications: via `github.com/gen2brain/beeep`. Title + body + URL.
Click → open URL in default browser.

---

## 6. User control (Settings → Notifications)

```
Categories:
  ☑ Important updates    (critical_warning, free_credit)
  ☑ kinthai product news (kinthai_product)
  ☑ Provider news        (provider_news)
  ☐ Tips & tutorials     (default OFF)

Desktop notifications:
  ● All
  ○ Critical only
  ○ Off (only show in GUI panel)

☑ Show unread count badge on status bar icon
```

---

## 7. SQLite schema (see spec/05-storage.md)

```sql
CREATE TABLE announcements (...);    -- announcements with read/dismissed/clicked state
CREATE TABLE feed_meta (...);        -- ETag, last_polled_at, last_feed_updated_at
```

---

## 8. Content editing guide (for announcements repo README)

Avoid spamming users. Maintainers should:

- Max 2 `kinthai_product` announcements per month
- `free_credit` MUST be real, remove after expiry
- `provider_news` MUST be technical, not marketing
- No "please rate us" / "please star" content
- `critical_warning` bar is very high — only when users will hit issues
- Summary ≤ 80 chars in each language
- URL goes to clean landing page, not affiliate link chain
- Never delete published announcement (set `expires_at` instead)

---

## 9. Test coverage

- Unit: feed.json parsing, schema validation
- Unit: targets filtering (table-driven for each rule)
- Unit: ETag handling (200 + 304 paths)
- Integration: full poll cycle against mock server

---

## 10. Open questions

- Should we support hyperlinks within announcement summaries? (markdown subset)
  M1: plain text only. M3+ consider limited markdown.
