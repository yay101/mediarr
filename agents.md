# Mediarr Agent Guidelines

## Project Overview

Mediarr is a self-hosted media management server with:
- Media library management (movies, TV, music, books, manga)
- Automated download from indexers (RSS, search)
- AI-powered metadata enrichment
- Web-based UI

## Tech Stack

| Layer | Technology |
|-------|------------|
| **Backend** | Go (standard library HTTP) |
| **Database** | embeddb (embedded, SQLite-like) |
| **UI Components** | Basecoat UI (Tailwind-based, HTML/CSS) |
| **Interactivity** | Alpine.js |
| **Templates** | Go `html/template` (SSR) |
| **Real-time** | WebSocket → Alpine.js stores |

## Architecture: SSR with Islands

```
┌─────────────────────────────────────────────────────────┐
│                    Go HTTP Server                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│  │  Template   │  │   JSON API   │  │  WebSocket  │    │
│  │  Renderer   │  │   Handler    │  │    Hub       │    │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘    │
│         │                 │                 │            │
│         ▼                 ▼                 ▼            │
│  ┌─────────────────────────────────────────────────┐    │
│  │              Go html/template                     │    │
│  │   - Server renders full HTML page               │    │
│  │   - Alpine.js initialized on load               │    │
│  │   - Interactive "islands" hydrated client-side   │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                      Browser                             │
│  ┌─────────────────────────────────────────────────┐    │
│  │                   Alpine.js                       │    │
│  │   - Global stores (notifications, user, ws)       │    │
│  │   - Page-level x-data components                 │    │
│  │   - Listens to WS for real-time updates          │    │
│  └─────────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────────┐    │
│  │              Basecoat UI                          │    │
│  │   - Tailwind CSS components                      │    │
│  │   - Pre-styled, accessible                      │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

## Directory Structure

```
mediarr/
├── web/
│   ├── static/
│   │   ├── css/
│   │   │   └── basecoat.min.css      # Basecoat UI styles
│   │   └── js/
│   │       ├── app.js                # Alpine.js initialization + stores
│   │       └── ws.js                 # WebSocket bridge to Alpine.js
│   └── templates/
│       ├── layout.html                # Base layout with Alpine shell
│       ├── sidebar.html               # Navigation sidebar component
│       ├── toast.html                 # Toast notification component
│       ├── movies/
│       │   ├── list.html             # Movie list page
│       │   └── detail.html           # Movie detail page
│       ├── tv/
│       │   ├── list.html
│       │   └── detail.html
│       ├── downloads.html
│       ├── search.html
│       └── settings.html
└── internal/
    └── server/
        ├── handlers.go                # HTTP handlers
        ├── templates.go               # Template rendering helpers
        └── ws.go                     # WebSocket hub
```

## Alpine.js Architecture

### Global Stores (`app.js`)

```javascript
// Notification store - toast messages from WS
Alpine.store('notifications', {
    items: [],
    add(message, type = 'info') { ... },
    remove(id) { ... }
});

// User store - auth state
Alpine.store('user', {
    id: null,
    username: '',
    role: '',
    authenticated: false
});

// WebSocket connection state
Alpine.store('ws', {
    connected: false,
    connect() { ... },
    onmessage(handler) { ... }
});
```

### Page-Level Components (`x-data`)

Each page has an `x-data` function that:
1. Fetches initial data from server
2. Sets up WebSocket listeners for real-time updates
3. Manages local state

### Real-time Update Flow

```
Server Event (e.g., download progress)
    │
    ▼
WebSocket Hub broadcasts
    │
    ▼
ws.js receives → Alpine.store('notifications').add(...)
    │
    ▼
Alpine reactivity updates DOM (toast, progress bar, etc.)
```

## WebSocket Protocol

### Client → Server
```json
{"type": "subscribe", "channel": "/downloads"}
{"type": "unsubscribe", "channel": "/downloads"}
```

### Server → Client
```json
{"type": "update", "channel": "/downloads", "data": {...}}
{"type": "refresh", "path": "/movies"}
{"type": "notification", "level": "info", "message": "Download complete"}
```

## API Conventions

### SSR Pages (HTML responses)
- `GET /movies` → Full HTML page with embedded data
- Template renders data server-side
- Alpine.js hydrates interactive elements

### JSON API (for Alpine.js islands)
- `GET /api/v1/media?type=movie` → JSON array
- `PATCH /api/v1/settings` → JSON status response
- Used for: form submissions, real-time data refresh, complex queries

### Error Format
```json
{
    "error": "human readable message",
    "code": "RESOURCE_NOT_FOUND",
    "details": {}
}
```

## Go Template Patterns

### Layout Template
```html
<!DOCTYPE html>
<html lang="en" x-data="app()">
<head>
    <title>Mediarr</title>
    <script src="https://unpkg.com/alpinejs@3.x.x/cdn.min.js" defer></script>
    <link rel="stylesheet" href="/static/css/basecoat.min.css">
</head>
<body>
    <div class="flex h-screen">
        {{template "sidebar" .}}
        <main class="flex-1 overflow-auto p-6">
            {{template "content" .}}
        </main>
    </div>
    {{template "toast" .}}
</body>
</html>
```

### Page with Data
```html
{{define "content"}}
<div x-data="moviesList()">
    <!-- Server-rendered static content -->
    {{range .Movies}}
    <div class="card">{{.Title}}</div>
    {{end}}
    
    <!-- Client-side interactive elements -->
    <button @click="refresh()">Refresh</button>
    
    <!-- Alpine.js reactive elements -->
    <div x-show="loading">Loading...</div>
</div>
<script>
function moviesList() {
    return {
        loading: false,
        async refresh() {
            this.loading = true;
            const res = await fetch('/api/v1/media?type=movie');
            const data = await res.json();
            // Update DOM...
            this.loading = false;
        }
    }
}
</script>
{{end}}
```

## Basecoat UI Components

Use Basecoat for:
- Cards (`class="card"`)
- Buttons (`class="btn btn-primary"`)
- Forms (`class="field"`, `class="input"`)
- Tables (`class="table"`)
- Badges (`class="badge"`)
- Dialogs (`class="dialog"`)
- Sidebar (`class="sidebar"`)
- Toasts (`class="toast"`)

See https://basecoatui.com for full component list.

## Guidelines for Agents

1. **Templates first**: When adding new pages, create Go template files
2. **SSR default**: Server render what can be server rendered
3. **Islands for interactivity**: Use Alpine.js for forms, real-time updates, filters
4. **WebSocket bridge**: Always push real-time updates through existing WS hub
5. **No client-side routing**: Each page is a full HTML load (simpler, better UX)
6. **Responsive**: Basecoat handles responsiveness via Tailwind

## Adding New Pages

1. Create template in `web/templates/{section}/`
2. Add route in `server.go` → `s.mux.HandleFunc("GET /path", s.handlePage)`
3. Add template handler in `handlers.go`
4. Add Alpine.js component if needed
5. Subscribe to WebSocket channels if real-time updates needed

## Adding New API Endpoints

1. Follow existing patterns in `handlers.go`
2. Use `writeJSON()` for responses
3. Standardize error format
4. Consider both SSR page + JSON API versions

## Debugging

### Auth Debug Log
Auth debugging is enabled by default. Check `/tmp/mediarr_debug.log` for:
- OIDC callback events (subject, email, user creation)
- Session cookie validation
- API key lookups

To enable Go debug logging, set `DEBUG=true` or check standard logs.

### Browser Debugging
The login page has a debug panel (visible by default). It shows:
- Number of OIDC providers configured
- Auth enabled status
- Detailed error messages

Remove `DEBUG = true` from login.html to hide debug panel.

### Common Issues

1. **"No OIDC providers"**: Check config.yaml `auth.oidc.providers`
2. **Session not persisting**: Check cookie settings in browser
3. **Template not loading**: Ensure `web/templates/` files exist and are parseable
