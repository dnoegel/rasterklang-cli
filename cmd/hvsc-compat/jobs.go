package main

// This file collects SID inputs and coordinates parallel compatibility jobs.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func collectSIDFiles(inputs []string, listPath string, limit int) ([]tuneJob, error) {
	if listPath != "" {
		return collectSIDFilesFromList(inputs[0], listPath, limit)
	}

	var jobs []tuneJob
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !strings.EqualFold(filepath.Ext(input), ".sid") {
				return nil, fmt.Errorf("%s is not a .sid file", input)
			}
			jobs = append(jobs, tuneJob{
				Path: input,
				Rel:  filepath.ToSlash(filepath.Base(input)),
			})
			continue
		}
		root := input
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".sid") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			jobs = append(jobs, tuneJob{
				Path: path,
				Rel:  filepath.ToSlash(rel),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Rel == jobs[j].Rel {
			return jobs[i].Path < jobs[j].Path
		}
		return jobs[i].Rel < jobs[j].Rel
	})
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func collectSIDFilesFromList(root string, listPath string, limit int) ([]tuneJob, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("-list input must be a directory: %s", root)
	}

	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, err
	}
	var jobs []tuneJob
	for lineNumber, line := range strings.Split(string(data), "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry), ".sid") {
			return nil, fmt.Errorf("%s:%d: %s is not a .sid file", listPath, lineNumber+1, entry)
		}
		cleanEntry := filepath.Clean(filepath.FromSlash(entry))
		path := cleanEntry
		rel := filepath.ToSlash(cleanEntry)
		if !filepath.IsAbs(cleanEntry) {
			path = filepath.Join(root, cleanEntry)
		} else {
			relPath, err := filepath.Rel(root, cleanEntry)
			if err == nil && !strings.HasPrefix(relPath, ".."+string(filepath.Separator)) && relPath != ".." {
				rel = filepath.ToSlash(relPath)
			} else {
				rel = filepath.ToSlash(filepath.Base(cleanEntry))
			}
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", listPath, lineNumber+1, err)
		}
		jobs = append(jobs, tuneJob{
			Path: path,
			Rel:  rel,
		})
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("no .sid files found in %s", listPath)
	}
	return jobs, nil
}

func runJobs(jobs []tuneJob, cfg config, writer *failureWriter) (int, int, error) {
	jobCh := make(chan tuneJob)
	resultCh := make(chan fileResult)
	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				resultCh <- checkFile(job, cfg)
			}
		}()
	}
	go func() {
		for _, job := range jobs {
			jobCh <- job
		}
		close(jobCh)
		wg.Wait()
		close(resultCh)
	}()

	tested := 0
	failureCount := 0
	completed := 0
	lastProgress := time.Now()
	for result := range resultCh {
		completed++
		tested += result.Tested
		for _, failure := range result.Failures {
			if err := writer.Write(failure); err != nil {
				return tested, failureCount, err
			}
			failureCount++
		}
		if len(result.Failures) > 0 {
			writer.Flush()
			if err := writer.Error(); err != nil {
				return tested, failureCount, err
			}
		}
		if !cfg.Quiet && (completed == len(jobs) || completed%100 == 0 || time.Since(lastProgress) >= 10*time.Second) {
			fmt.Fprintf(os.Stderr, "Progress: %d/%d files, %d failures\n", completed, len(jobs), failureCount)
			lastProgress = time.Now()
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return tested, failureCount, err
	}
	return tested, failureCount, nil
}
