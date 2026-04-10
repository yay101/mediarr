# Mediarr Integration Tests

This directory contains integration tests that validate the complete user flow in Mediarr.

## Test Coverage

### 1. Authentication (`auth_media_test.go`)
- User login (admin and regular users)
- Invalid credential handling
- User registration
- Auth middleware verification

### 2. Media Library (`auth_media_test.go`)
- Adding movies to library
- Adding TV shows to library
- Listing media by type
- Deleting media items

### 3. Metadata Search (`auth_media_test.go`)
- Searching for public content (Big Buck Bunny, Sintel)
- Verifying TMDB integration
- Result parsing and validation

### 4. Manual Indexer Search (`search_download_test.go`)
- Starting manual searches across all configured indexers
- Polling for results
- Linking search results to library items
- Result sorting (freeleech first)

### 5. Download Queueing (`search_download_test.go`)
- Queueing downloads from search results
- Force download option (bypasses quality matching)
- Verifying downloads appear in queue
- Cancelling downloads

### 6. File Organization (`organize_test.go`)
- Directory setup for downloads/library
- Hardlink creation on same filesystem
- Cross-filesystem move handling
- Leftover file cleanup patterns

## Prerequisites

1. **Running Mediarr Server**
   ```bash
   # Start the server
   mediarr serve
   ```

2. **Configuration**
   ```bash
   # Copy example config
   cp tests/test.env.example tests/test.env
   
   # Edit with your values
   nano tests/test.env
   ```

3. **Required Environment Variables**

   | Variable | Description | Example |
   |----------|-------------|---------|
   | `TEST_SERVER_URL` | Mediarr server URL | `http://localhost:8080` |
   | `TEST_ADMIN_USER` | Admin username | `admin` |
   | `TEST_ADMIN_PASS` | Admin password | `changeme123` |
   | `TEST_USER_USERNAME` | Test user to create | `testuser` |
   | `TEST_USER_PASSWORD` | Test user password | `TestPass123!` |
   | `TEST_INDEXER_1_URL` | Torznab indexer URL | `http://localhost:9117/api/v2/` |
   | `TEST_INDEXER_1_API_KEY` | Indexer API key | `your-jackett-api-key` |

4. **Optional: TMDB API Key** (for metadata search tests)
   - Get a free key at https://www.themoviedb.org/
   - Set `TEST_TMDB_API_KEY`

## Running Tests

### Run All Tests
```bash
go test ./tests/integration/... -v
```

### Run Specific Test Categories
```bash
# Auth tests only
go test ./tests/integration/... -v -run TestAuth

# Media/library tests only
go test ./tests/integration/... -v -run TestAddToLibrary

# Search tests only
go test ./tests/integration/... -v -run TestManualSearch

# Download tests (slow - requires actual downloads)
RUN_SLOW_TESTS=true go test ./tests/integration/... -v -run TestSearchAndDownload
```

### Run with Verbose Output
```bash
VERBOSE_OUTPUT=true go test ./tests/integration/... -v
```

## Test Structure

```
tests/
├── test.env.example      # Configuration template
├── fixtures/            # Test data fixtures (future)
├── integration/
│   ├── config.go         # Test configuration loader
│   ├── client.go        # HTTP client helpers
│   ├── auth_media_test.go    # Auth and media tests
│   ├── search_download_test.go # Search and download tests
│   ├── organize_test.go  # File organization tests
│   └── suite_test.go     # Test suite info
└── README.md           # This file
```

## Expected Test Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                     COMPLETE USER FLOW                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. AUTHENTICATION                                              │
│     └─► Login → Get JWT token                                  │
│                                                                  │
│  2. METADATA SEARCH                                            │
│     └─► Search TMDB/OMDB → Find "Big Buck Bunny"               │
│                                                                  │
│  3. ADD TO LIBRARY                                             │
│     └─► POST /api/v1/media → Media ID 123                     │
│                                                                  │
│  4. MANUAL INDEXER SEARCH                                       │
│     └─► POST /api/v1/search/manual                             │
│         └─► Poll results → Get search session                   │
│                                                                  │
│  5. SELECT & DOWNLOAD                                          │
│     └─► Choose result → Force download                          │
│         └─► POST /api/v1/search/manual/{id}/download           │
│             └─► Get Job ID                                       │
│                                                                  │
│  6. DOWNLOAD COMPLETION                                         │
│     └─► GET /api/v1/downloads → Status updates                  │
│                                                                  │
│  7. ORGANIZATION                                               │
│     └─► File moved/hardlinked to library                      │
│         └─► POST-processing: Cleanup leftovers                  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Troubleshooting

### "Server not reachable"
- Ensure Mediarr is running on `TEST_SERVER_URL`
- Check firewall settings
- Verify URL format (no trailing slash)

### "No indexers configured"
- Configure at least one indexer in `test.env`
- Or skip search tests: `go test ... -skip TestManualSearch`

### "Slow tests disabled"
- Download tests are skipped by default
- Enable with: `RUN_SLOW_TESTS=true go test ...`

### "Test user not available"
- Ensure admin credentials are correct
- Server must be in state where user creation is allowed

## WebSocket Testing

WebSocket functionality (real-time search streaming) requires browser testing.
For manual testing:

1. Open Mediarr in browser
2. Navigate to Search → Manual Search
3. Enter query (e.g., "Big Buck Bunny")
4. Observe results streaming in real-time
5. Select a result and click Download

## CI/CD Integration

Example GitHub Actions workflow:

```yaml
name: Integration Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      mediarr:
        image: mediarr:latest
        ports:
          - 8080:8080
        env:
          MEDIARR_ADMIN_USER: admin
          MEDIARR_ADMIN_PASS: testpass123
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Run integration tests
        env:
          TEST_SERVER_URL: http://localhost:8080
          TEST_ADMIN_USER: admin
          TEST_ADMIN_PASS: testpass123
          TEST_INDEXER_1_URL: http://localhost:9117/api/v2/
          TEST_INDEXER_1_API_KEY: ${{ secrets.INDEXER_API_KEY }}
        run: |
          go test ./tests/integration/... -v
```
