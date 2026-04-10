package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOrganizeFiles(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)

	t.Run("Setup test directories", func(t *testing.T) {
		cfg.SetupTestDirs(t)
	})

	t.Run("Create test media file", func(t *testing.T) {
		testFile := filepath.Join(cfg.DownloadDir, "Big.Buck.Bunny.2008.1080p.mkv")

		content := []byte("fake video content for testing")
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		if cfg.VerboseOutput {
			t.Logf("Created test file: %s", testFile)
		}

		info, err := os.Stat(testFile)
		if err != nil {
			t.Fatalf("Failed to stat test file: %v", err)
		}

		if info.Size() != int64(len(content)) {
			t.Fatalf("Expected file size %d, got %d", len(content), info.Size())
		}
	})

	t.Run("Verify directory structure", func(t *testing.T) {
		dirs := []string{cfg.DownloadDir, cfg.LibraryDir, cfg.HardlinkDir}
		for _, dir := range dirs {
			info, err := os.Stat(dir)
			if err != nil {
				t.Errorf("Directory %s does not exist: %v", dir, err)
				continue
			}
			if !info.IsDir() {
				t.Errorf("%s is not a directory", dir)
			}
		}
	})

	t.Cleanup(func() {
		cfg.Cleanup(t)
	})
}

func TestHardlinkSameFilesystem(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)

	t.Run("Create source and target directories", func(t *testing.T) {
		cfg.SetupTestDirs(t)

		sourceFile := filepath.Join(cfg.DownloadDir, "test.mkv")
		targetDir := cfg.HardlinkDir

		content := []byte("hardlink test content")
		if err := os.WriteFile(sourceFile, content, 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}

		if cfg.VerboseOutput {
			t.Logf("Source: %s", sourceFile)
			t.Logf("Target dir: %s", targetDir)
		}
	})

	t.Cleanup(func() {
		cfg.Cleanup(t)
	})
}

func TestCrossFilesystemMove(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)

	t.Run("Prepare files for cross-fs move test", func(t *testing.T) {
		cfg.SetupTestDirs(t)

		sourceFile := filepath.Join(cfg.DownloadDir, "cross-fs-test.mkv")
		content := []byte("cross filesystem move test")

		if err := os.WriteFile(sourceFile, content, 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}

		if cfg.VerboseOutput {
			t.Logf("Will test moving: %s", sourceFile)
			t.Logf("To library: %s", cfg.LibraryDir)
		}
	})

	t.Cleanup(func() {
		cfg.Cleanup(t)
	})
}

func TestLeftoverCleanup(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)

	t.Run("Create various leftover files", func(t *testing.T) {
		cfg.SetupTestDirs(t)

		leftoverFiles := []string{
			"video.mkv.part",
			"video.mkv.part.1",
			"video.mkv.tmp",
			"video.mkv.temp",
			"video.mkv.bak",
			"video.mkv.aria2",
			"video.mkv.download",
			"video.mkv.missing",
			"video.nfo",
			"cover.jpg",
			"checksum.sfv",
			"sample-video.mkv",
		}

		for _, filename := range leftoverFiles {
			path := filepath.Join(cfg.DownloadDir, filename)
			if err := os.WriteFile(path, []byte("leftover"), 0644); err != nil {
				t.Fatalf("Failed to create leftover file %s: %v", filename, err)
			}
		}

		if cfg.VerboseOutput {
			t.Logf("Created %d leftover files for cleanup test", len(leftoverFiles))
		}
	})

	t.Cleanup(func() {
		cfg.Cleanup(t)
	})
}
