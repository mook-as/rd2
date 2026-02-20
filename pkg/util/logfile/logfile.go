// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package logfile creates log files with automatic rotation.
//
// Each call to Create renames any existing {name}.log to {name}.{N}.log
// and opens a fresh {name}.log. The active log always has a stable name.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const retentionCount = 5

type numberedFile struct {
	n    int
	name string
}

// Create opens a new log file named {name}.log in dir, renaming any
// existing {name}.log to {name}.{N}.log first.
//
// When keepAll is false, old numbered files beyond the retention count
// are removed. If header is non-empty, it is written as the first line.
func Create(dir, name string, keepAll bool, header string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", dir, err)
	}

	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `\.(\d+)\.log$`)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read log directory %s: %w", dir, err)
	}

	// Find the highest existing sequence number.
	maxN := 0
	var numberedFiles []numberedFile
	for _, entry := range entries {
		matches := pattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		n, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		numberedFiles = append(numberedFiles, numberedFile{n: n, name: entry.Name()})
		if n > maxN {
			maxN = n
		}
	}

	nextN := maxN + 1
	filePath := filepath.Join(dir, name+".log")

	// Rename the current log to a numbered backup.
	if _, err := os.Lstat(filePath); err == nil {
		numberedName := fmt.Sprintf("%s.%d.log", name, nextN)
		if err := os.Rename(filePath, filepath.Join(dir, numberedName)); err != nil {
			return nil, fmt.Errorf("rename %s to %s: %w", filePath, numberedName, err)
		}
		numberedFiles = append(numberedFiles, numberedFile{n: nextN, name: numberedName})
	}

	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("create log file %s: %w", filePath, err)
	}

	if header != "" {
		if _, err := f.WriteString(header); err != nil {
			f.Close()
			os.Remove(filePath)
			return nil, fmt.Errorf("write header to %s: %w", filePath, err)
		}
	}

	if !keepAll {
		pruneOldFiles(dir, numberedFiles, nextN)
	}

	return f, nil
}

// pruneOldFiles removes numbered log files beyond the retention count,
// keeping the most recent files (those with the highest sequence numbers).
func pruneOldFiles(dir string, files []numberedFile, currentN int) {
	// Keep files with sequence numbers > currentN - retentionCount.
	cutoff := currentN - retentionCount
	for _, nf := range files {
		if nf.n <= cutoff {
			_ = os.Remove(filepath.Join(dir, nf.name))
		}
	}
}
