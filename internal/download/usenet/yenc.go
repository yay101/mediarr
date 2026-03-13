package usenet

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type YEncInfo struct {
	Name  string
	Size  int64
	Part  int
	Total int
	Begin int64
	End   int64
}

func DecodeYEnc(r io.Reader) ([]byte, *YEncInfo, error) {
	scanner := bufio.NewScanner(r)
	// Use a larger buffer for long yEnc lines
	bufPool := make([]byte, 0, 64*1024)
	scanner.Buffer(bufPool, 1024*1024)

	var info YEncInfo
	var buf bytes.Buffer
	inData := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.HasPrefix(line, []byte("=ybegin ")) {
			parts := strings.Split(string(line), " ")
			for _, p := range parts {
				if strings.HasPrefix(p, "name=") {
					info.Name = p[5:]
				} else if strings.HasPrefix(p, "size=") {
					fmt.Sscanf(p[5:], "%d", &info.Size)
				} else if strings.HasPrefix(p, "part=") {
					fmt.Sscanf(p[5:], "%d", &info.Part)
				} else if strings.HasPrefix(p, "total=") {
					fmt.Sscanf(p[6:], "%d", &info.Total)
				}
			}
			inData = true
			continue
		}
		if bytes.HasPrefix(line, []byte("=ypart ")) {
			parts := strings.Split(string(line), " ")
			for _, p := range parts {
				if strings.HasPrefix(p, "begin=") {
					fmt.Sscanf(p[6:], "%d", &info.Begin)
				} else if strings.HasPrefix(p, "end=") {
					fmt.Sscanf(p[4:], "%d", &info.End)
				}
			}
			continue
		}
		if bytes.HasPrefix(line, []byte("=yend ")) {
			inData = false
			break
		}

		if inData {
			decoded := decodeYEncLine(line)
			buf.Write(decoded)
		}
	}

	if buf.Len() == 0 {
		return nil, nil, errors.New("no yEnc data found")
	}

	return buf.Bytes(), &info, nil
}

func decodeYEncLine(line []byte) []byte {
	decoded := make([]byte, 0, len(line))
	escaped := false
	for _, b := range line {
		if b == '=' && !escaped {
			escaped = true
			continue
		}
		if escaped {
			decoded = append(decoded, b-64-42)
			escaped = false
		} else {
			decoded = append(decoded, b-42)
		}
	}
	return decoded
}

// DownloadAndAssembleNZB downloads all segments of an NZB and assembles the files.
func (c *NZBClient) DownloadAndAssembleNZB(nzb *NZB, destDir string, progressFunc func(float32)) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	var totalBytes int64
	for _, file := range nzb.Files {
		for _, seg := range file.Segments {
			totalBytes += seg.Bytes
		}
	}

	var downloadedBytes int64
	var mainFilePath string

	for _, file := range nzb.Files {
		// Identify target file from subject or ybegin
		// In a real client, we'd handle multiple files properly
		fileName := file.Subject
		if idx := strings.Index(fileName, "\""); idx != -1 {
			fileName = fileName[idx+1:]
			if idx2 := strings.Index(fileName, "\""); idx2 != -1 {
				fileName = fileName[:idx2]
			}
		}

		targetPath := filepath.Join(destDir, fileName)
		if mainFilePath == "" {
			mainFilePath = targetPath
		}

		f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return "", err
		}

		for _, seg := range file.Segments {
			data, err := c.DownloadSegment(file.Groups[0], seg.MessageID)
			if err != nil {
				f.Close()
				return "", err
			}

			decoded, _, _ := DecodeYEnc(bytes.NewReader(data))
			if len(decoded) > 0 {
				f.Write(decoded)
			} else {
				// Fallback if not yEnc
				f.Write(data)
			}

			downloadedBytes += seg.Bytes
			if progressFunc != nil {
				progressFunc(float32(downloadedBytes) / float32(totalBytes))
			}
		}
		f.Close()
	}

	return mainFilePath, nil
}
