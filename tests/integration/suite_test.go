package integration

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestSuite(t *testing.T) {
	t.Log("Mediarr Integration Test Suite")
	t.Log("")
	t.Log("This test suite validates the complete user flow:")
	t.Log("  1. User authentication and creation")
	t.Log("  2. Metadata search (TMDB, etc.)")
	t.Log("  3. Adding media to library")
	t.Log("  4. Manual indexer search")
	t.Log("  5. Download queueing with force option")
	t.Log("  6. Download progress tracking")
	t.Log("")
	t.Log("Prerequisites:")
	t.Log("  - Server must be running on TEST_SERVER_URL")
	t.Log("  - At least one indexer configured for search tests")
	t.Log("  - Admin credentials for user creation tests")
	t.Log("")
	t.Log("Environment variables (see test.env.example):")
	t.Log("  TEST_SERVER_URL         - Server URL (default: http://localhost:8080)")
	t.Log("  TEST_ADMIN_USER         - Admin username")
	t.Log("  TEST_ADMIN_PASS         - Admin password")
	t.Log("  TEST_INDEXER_1_URL      - First indexer URL")
	t.Log("  TEST_INDEXER_1_API_KEY  - First indexer API key")
	t.Log("  RUN_SLOW_TESTS          - Enable download tests (default: false)")
	t.Log("")
	t.Log("To run tests:")
	t.Log("  # Copy and configure test.env")
	t.Log("  cp tests/test.env.example tests/test.env")
	t.Log("  # Edit tests/test.env with your values")
	t.Log("")
	t.Log("  # Run all tests")
	t.Log("  go test ./tests/integration/... -v")
	t.Log("")
	t.Log("  # Run specific test categories")
	t.Log("  go test ./tests/integration/... -v -run TestAuth")
	t.Log("  go test ./tests/integration/... -v -run TestManualSearch")
	t.Log("  go test ./tests/integration/... -v -run TestSearchAndDownload")
	t.Log("")
	t.Log("  # Run with slow tests enabled")
	t.Log("  RUN_SLOW_TESTS=true go test ./tests/integration/... -v")
	t.Log("")
}
