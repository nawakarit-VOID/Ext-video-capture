// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ============================================
// CONSTANTS & CONFIGURATION
// ============================================

const (
	MaxSegmentSize  = 50 * 1024 * 1024 // 50MB per segment
	MaxPlaylistSize = 10 * 1024 * 1024 // 10MB
	DefaultTimeout  = 30 * time.Minute
	DefaultRetries  = 3
	MaxParallel     = 10
	ErrorThreshold  = 0.2 // 20% errors allowed
	MinErrorsToStop = 5
)

// ============================================
// CUSTOM ERROR TYPES
// ============================================

type DownloadError struct {
	Segment int
	URL     string
	Err     error
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("segment %d (%s): %v", e.Segment, e.URL, e.Err)
}

func (e *DownloadError) Unwrap() error {
	return e.Err
}

type VariantError struct {
	URL string
	Err error
}

func (e *VariantError) Error() string {
	return fmt.Sprintf("failed to detect variants from %s: %v", e.URL, e.Err)
}

// ============================================
// PROGRESS TRACKER
// ============================================

type DownloadProgress struct {
	mu          sync.Mutex
	total       int
	completed   int
	downloaded  int64
	speed       float64
	startTime   time.Time
	lastUpdate  time.Time
	lastBytes   int64
	progressBar *widget.ProgressBar
	statusLabel *widget.Label
}

func NewDownloadProgress(total int, progressBar *widget.ProgressBar, statusLabel *widget.Label) *DownloadProgress {
	now := time.Now()
	return &DownloadProgress{
		total:       total,
		startTime:   now,
		lastUpdate:  now,
		progressBar: progressBar,
		statusLabel: statusLabel,
	}
}

func (p *DownloadProgress) Update(bytesDownloaded int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.completed++
	p.downloaded += bytesDownloaded

	// Update progress bar
	if p.progressBar != nil {
		p.progressBar.SetValue(float64(p.completed) / float64(p.total))
	}

	// Calculate speed
	now := time.Now()
	elapsed := now.Sub(p.lastUpdate).Seconds()
	if elapsed > 0 && p.downloaded > p.lastBytes {
		p.speed = float64(p.downloaded-p.lastBytes) / elapsed / 1024 / 1024 // MB/s
	}
	p.lastUpdate = now
	p.lastBytes = p.downloaded

	// Update status
	if p.statusLabel != nil {
		eta := p.calculateETA()
		statusMsg := fmt.Sprintf("Downloaded %d/%d segments (%.2f MB/s) ETA: %s",
			p.completed, p.total, p.speed, eta)
		p.statusLabel.SetText(statusMsg)
	}
}

func (p *DownloadProgress) calculateETA() string {
	if p.completed == 0 || p.speed == 0 {
		return "calculating..."
	}

	remaining := p.total - p.completed
	etaSeconds := float64(remaining) / (p.speed * 1024 * 1024 / float64(avgSegmentSize))

	if etaSeconds < 60 {
		return fmt.Sprintf("%.0f seconds", etaSeconds)
	} else if etaSeconds < 3600 {
		return fmt.Sprintf("%.0f minutes", etaSeconds/60)
	}
	return fmt.Sprintf("%.1f hours", etaSeconds/3600)
}

// Global variable for average segment size estimation
var avgSegmentSize float64 = 5 * 1024 * 1024 // 5MB default

func (p *DownloadProgress) SetAverageSegmentSize(size int64) {
	avgSegmentSize = float64(size)
}

// ============================================
// HTTP HELPERS WITH TIMEOUTS
// ============================================

func createHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
	}
}

func fetchTextWithTimeout(ctx context.Context, url string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "HLS-Downloader/2.0")
	req.Header.Set("Accept", "*/*")

	client := createHTTPClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d: %s", resp.StatusCode, resp.Status)
	}

	// Limit response size
	limitedReader := io.LimitReader(resp.Body, MaxPlaylistSize)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if int64(len(body)) >= MaxPlaylistSize {
		return "", fmt.Errorf("playlist exceeds size limit (%d MB)", MaxPlaylistSize/1024/1024)
	}

	return string(body), nil
}

func fetchSegmentWithRetry(ctx context.Context, segmentURL string, maxRetries int) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			backoff := time.Duration(attempt*attempt) * time.Second
			if attempt > 3 {
				backoff = 10 * time.Second
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		data, err := fetchSegment(ctx, segmentURL)
		if err == nil {
			return data, nil
		}
		lastErr = err

		// Don't retry if context is cancelled
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Don't retry on certain errors
		if strings.Contains(err.Error(), "404") ||
			strings.Contains(err.Error(), "403") ||
			strings.Contains(err.Error(), "401") {
			return nil, fmt.Errorf("non-retryable error: %w", err)
		}
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func fetchSegment(ctx context.Context, segmentURL string) ([]byte, error) {
	// Use shorter timeout for individual segments
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, segmentURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "HLS-Downloader/2.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")

	client := createHTTPClient(60 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, resp.Status)
	}

	// Check content length if available
	if resp.ContentLength > MaxSegmentSize {
		return nil, fmt.Errorf("segment too large: %d MB (limit: %d MB)",
			resp.ContentLength/1024/1024, MaxSegmentSize/1024/1024)
	}

	// Limit read size
	limitedReader := io.LimitReader(resp.Body, MaxSegmentSize)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read segment: %w", err)
	}

	if int64(len(data)) >= MaxSegmentSize {
		return nil, fmt.Errorf("segment exceeded size limit (%d MB)", MaxSegmentSize/1024/1024)
	}

	// Ensure we got data
	if len(data) == 0 {
		return nil, fmt.Errorf("segment is empty")
	}

	return data, nil
}

// ============================================
// VARIANT DETECTION
// ============================================

type Variant struct {
	URL     string
	Label   string
	Bitrate int
}

func detectVariants(ctx context.Context, masterURL string) ([]Variant, error) {
	text, err := fetchTextWithTimeout(ctx, masterURL, 30*time.Second)
	if err != nil {
		return nil, &VariantError{URL: masterURL, Err: err}
	}

	var variants []Variant

	// Regex to find EXT-X-STREAM-INF
	re := regexp.MustCompile(`EXT-X-STREAM-INF:([^\n]+)\n([^\n]+)`)
	matches := re.FindAllStringSubmatch(text, -1)

	if len(matches) == 0 {
		// Check if it's a direct playlist
		if strings.Contains(text, "EXTINF") {
			return []Variant{}, nil // No variants, direct playlist
		}
		return nil, fmt.Errorf("no stream variants found in playlist")
	}

	for _, match := range matches {
		attrs := match[1]
		uri := strings.TrimSpace(match[2])

		// Skip if URI is empty or commented
		if uri == "" || strings.HasPrefix(uri, "#") {
			continue
		}

		// Extract BANDWIDTH
		bitrateMatch := regexp.MustCompile(`BANDWIDTH=(\d+)`).FindStringSubmatch(attrs)
		bitrate := 0
		if len(bitrateMatch) > 1 {
			bitrate, _ = strconv.Atoi(bitrateMatch[1])
		}

		// Extract RESOLUTION
		resMatch := regexp.MustCompile(`RESOLUTION=([^,\s]+)`).FindStringSubmatch(attrs)
		resolution := "Unknown"
		if len(resMatch) > 1 {
			resolution = resMatch[1]
		}

		// Extract CODECS
		codecsMatch := regexp.MustCompile(`CODECS="([^"]+)"`).FindStringSubmatch(attrs)
		codecs := ""
		if len(codecsMatch) > 1 {
			codecs = " " + codecsMatch[1]
		}

		// Resolve URL
		resolvedURL, err := resolveURL(masterURL, uri)
		if err != nil {
			continue
		}

		label := fmt.Sprintf("%s (%d kbps)%s", resolution, bitrate/1000, codecs)
		variants = append(variants, Variant{
			URL:     resolvedURL,
			Label:   label,
			Bitrate: bitrate,
		})
	}

	if len(variants) == 0 {
		return nil, fmt.Errorf("no valid variants found in playlist")
	}

	return variants, nil
}

// ============================================
// M3U8 PARSING
// ============================================

func parseM3U8(baseURL, data string) ([]string, error) {
	var segments []string
	var currentURI string
	scanner := bufio.NewScanner(strings.NewReader(data))

	// Handle very long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Skip comments except EXTINF which contains segment info
		if strings.HasPrefix(line, "#") {
			// Check if it's an EXTINF line (contains segment duration)
			if strings.HasPrefix(line, "#EXTINF") {
				// Next line should be the segment URL
				if scanner.Scan() {
					segLine := strings.TrimSpace(scanner.Text())
					if segLine != "" && !strings.HasPrefix(segLine, "#") {
						resolved, err := resolveURL(baseURL, segLine)
						if err != nil {
							return nil, fmt.Errorf("failed to resolve URL %s: %w", segLine, err)
						}
						segments = append(segments, resolved)
					}
				}
			}
			// Skip other comments
			continue
		}

		// If line is not a comment and not already handled as segment URL
		if !strings.HasPrefix(line, "#") {
			// Check if previous line was EXTINF
			if currentURI == "" {
				resolved, err := resolveURL(baseURL, line)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve URL %s: %w", line, err)
				}
				segments = append(segments, resolved)
			}
			currentURI = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	if len(segments) == 0 {
		return nil, fmt.Errorf("no media segments found in playlist")
	}

	return segments, nil
}

// ============================================
// URL HELPERS
// ============================================

func resolveURL(baseURL, reference string) (string, error) {
	// Handle absolute URLs starting with //
	if strings.HasPrefix(reference, "//") {
		// Parse base URL to get scheme
		base, err := url.Parse(baseURL)
		if err != nil {
			return "", err
		}
		return base.Scheme + ":" + reference, nil
	}

	// Handle absolute URLs
	if strings.HasPrefix(reference, "http://") || strings.HasPrefix(reference, "https://") {
		return reference, nil
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	ref, err := url.Parse(reference)
	if err != nil {
		return "", err
	}

	return base.ResolveReference(ref).String(), nil
}

// ============================================
// MAIN DOWNLOAD FUNCTION
// ============================================

func downloadHLSParallel(ctx context.Context, playlistURL, outputFile string,
	parallel int, progress *widget.ProgressBar, status *widget.Label) error {

	// Validate parallel parameter
	if parallel < 1 {
		parallel = 1
	}
	if parallel > MaxParallel {
		parallel = MaxParallel
	}

	// Fetch playlist with timeout
	status.SetText("Fetching playlist...")
	playlistData, err := fetchTextWithTimeout(ctx, playlistURL, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to fetch playlist: %w", err)
	}

	// Parse segments
	status.SetText("Parsing segments...")
	segmentURLs, err := parseM3U8(playlistURL, playlistData)
	if err != nil {
		return fmt.Errorf("failed to parse playlist: %w", err)
	}

	total := len(segmentURLs)
	status.SetText(fmt.Sprintf("Found %d segments", total))

	// Create temp directory for segments
	tempDir, err := os.MkdirTemp("", "hls_download_*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			status.SetText("Warning: failed to clean up temp files: " + err.Error())
		}
	}()

	// Initialize progress tracker
	progressTracker := NewDownloadProgress(total, progress, status)

	// Download segments in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errMu sync.Mutex
	var downloadErrors []error
	segmentPaths := make(map[int]string)
	completed := 0
	semaphore := make(chan struct{}, parallel)

	// Context for cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Monitor for errors
	errorThreshold := int(float64(total) * ErrorThreshold)
	if errorThreshold < MinErrorsToStop {
		errorThreshold = MinErrorsToStop
	}

	for i, segURL := range segmentURLs {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}

			// Download segment with retry
			segData, err := fetchSegmentWithRetry(ctx, url, DefaultRetries)
			if err != nil {
				errMu.Lock()
				downloadErrors = append(downloadErrors, &DownloadError{
					Segment: index + 1,
					URL:     url,
					Err:     err,
				})
				errMu.Unlock()

				// Check if error threshold exceeded
				errMu.Lock()
				errCount := len(downloadErrors)
				errMu.Unlock()

				if errCount >= errorThreshold {
					cancel()
				}
				return
			}

			// Save to temp file
			tempFile := filepath.Join(tempDir, fmt.Sprintf("seg_%06d.ts", index))
			if err := os.WriteFile(tempFile, segData, 0644); err != nil {
				errMu.Lock()
				downloadErrors = append(downloadErrors, fmt.Errorf("failed to write segment %d: %w", index+1, err))
				errMu.Unlock()
				return
			}

			// Store segment path
			mu.Lock()
			segmentPaths[index] = tempFile
			completed++
			mu.Unlock()

			// Update progress
			progressTracker.Update(int64(len(segData)))
		}(i, segURL)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Check for errors
	if len(downloadErrors) > 0 {
		if len(downloadErrors) >= errorThreshold {
			return fmt.Errorf("too many errors (%d/%d): %v",
				len(downloadErrors), total, downloadErrors[0])
		}
		return fmt.Errorf("download completed with %d errors: %v",
			len(downloadErrors), downloadErrors[0])
	}

	// Verify all segments were downloaded
	if len(segmentPaths) != total {
		return fmt.Errorf("missing segments: downloaded %d/%d", len(segmentPaths), total)
	}

	// Merge segments in order
	status.SetText("Merging segments...")
	outFile, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Use buffered writing for better performance
	writer := bufio.NewWriterSize(outFile, 32*1024)
	defer writer.Flush()

	for i := 0; i < total; i++ {
		tempFile, ok := segmentPaths[i]
		if !ok {
			return fmt.Errorf("missing segment %d", i+1)
		}

		// Read segment data
		data, err := os.ReadFile(tempFile)
		if err != nil {
			return fmt.Errorf("failed to read segment %d: %w", i+1, err)
		}

		// Write to output
		if _, err := writer.Write(data); err != nil {
			return fmt.Errorf("failed to write segment %d: %w", i+1, err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}

	status.SetText(fmt.Sprintf("Download complete! Saved to %s", outputFile))
	return nil
}

// ============================================
// GUI APPLICATION
// ============================================

func main() {
	app := app.New()
	window := app.NewWindow("HLS Downloader - Parallel & Variant Support")
	window.Resize(fyne.NewSize(600, 500))

	// ============================================
	// WIDGETS
	// ============================================

	// URL Entry
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("Paste master.m3u8 URL here...")

	// Output Entry
	outputEntry := widget.NewEntry()
	outputEntry.SetPlaceHolder("Output file name (e.g., output.ts or output.mp4)")
	outputEntry.SetText("output.ts")

	// Variant Select (using SelectEntry for placeholder support)
	variantSelect := widget.NewSelectEntry([]string{})
	variantSelect.SetPlaceHolder("Detecting variants...")

	// Parallel Entry
	parallelEntry := widget.NewEntry()
	parallelEntry.SetText("4")
	parallelEntry.SetPlaceHolder("Number of parallel connections (1-10)")

	// Status & Progress
	statusLabel := widget.NewLabel("Ready")
	progressBar := widget.NewProgressBar()
	progressBar.Min = 0
	progressBar.Max = 1.0

	// ============================================
	// VARIABLES FOR STATE
	// ============================================

	var variantsCache []Variant
	//var currentContext context.Context
	var cancelFunc context.CancelFunc

	// ============================================
	// BUTTON HANDLERS
	// ============================================

	// Detect Variants Button
	detectButton := widget.NewButton("🔍 Detect Variants", func() {
		m3u8URL := strings.TrimSpace(urlEntry.Text)
		if m3u8URL == "" {
			dialog.ShowError(fmt.Errorf("please enter a master.m3u8 URL"), window)
			return
		}

		// Cancel any existing detection
		if cancelFunc != nil {
			cancelFunc()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		cancelFunc = cancel

		statusLabel.SetText("Detecting variants...")
		progressBar.SetValue(0)

		go func() {
			defer cancel()

			variants, err := detectVariants(ctx, m3u8URL)
			if err != nil {
				statusLabel.SetText("Error: " + err.Error())
				dialog.ShowError(err, window)
				return
			}

			variantsCache = variants

			if len(variants) == 0 {
				statusLabel.SetText("No variants found - direct playlist detected")
				variantSelect.SetOptions([]string{"Direct playlist (no variant selection)"})
				variantSelect.SetText("Direct playlist (no variant selection)")
				return
			}

			options := make([]string, len(variants))
			for i, v := range variants {
				options[i] = v.Label
			}

			variantSelect.SetOptions(options)
			variantSelect.SetText(options[0])
			statusLabel.SetText(fmt.Sprintf("✅ Found %d variant(s)", len(variants)))
		}()
	})

	// Download Button
	startButton := widget.NewButton("⬇️ Download", func() {
		m3u8URL := strings.TrimSpace(urlEntry.Text)
		outputFile := strings.TrimSpace(outputEntry.Text)
		parallelStr := strings.TrimSpace(parallelEntry.Text)
		selectedVariant := variantSelect.Text

		// Validate inputs
		if m3u8URL == "" {
			dialog.ShowError(fmt.Errorf("please enter a master.m3u8 URL"), window)
			return
		}

		if outputFile == "" {
			dialog.ShowError(fmt.Errorf("please enter an output file name"), window)
			return
		}

		parallel, err := strconv.Atoi(parallelStr)
		if err != nil || parallel < 1 || parallel > MaxParallel {
			dialog.ShowError(fmt.Errorf("parallel must be between 1 and %d", MaxParallel), window)
			return
		}

		// Check if variant is selected
		if selectedVariant == "" || selectedVariant == "Detecting variants..." {
			dialog.ShowError(fmt.Errorf("please detect and select a variant first"), window)
			return
		}

		// Find selected variant URL
		var selectedURL string
		if selectedVariant == "Direct playlist (no variant selection)" {
			selectedURL = m3u8URL
		} else {
			for _, v := range variantsCache {
				if v.Label == selectedVariant {
					selectedURL = v.URL
					break
				}
			}
			if selectedURL == "" {
				// Fallback to original URL
				selectedURL = m3u8URL
				statusLabel.SetText("Warning: Using original URL as fallback")
			}
		}

		// Cancel any existing download
		if cancelFunc != nil {
			cancelFunc()
		}

		// Create context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		cancelFunc = cancel

		statusLabel.SetText("Starting download...")
		progressBar.SetValue(0)

		go func() {
			defer cancel()

			err := downloadHLSParallel(ctx, selectedURL, outputFile, parallel,
				progressBar, statusLabel)

			if err != nil {
				if err == context.Canceled {
					statusLabel.SetText("⏹️ Download cancelled")
					return
				}
				if err == context.DeadlineExceeded {
					statusLabel.SetText("⏰ Download timeout exceeded")
					dialog.ShowError(fmt.Errorf("download timed out after %v", DefaultTimeout), window)
					return
				}
				statusLabel.SetText("❌ Failed: " + err.Error())
				dialog.ShowError(err, window)
				return
			}

			statusLabel.SetText("✅ Download complete!")
			dialog.ShowInformation("Success", "Download completed successfully!", window)
		}()
	})

	// Cancel Button
	cancelButton := widget.NewButton("⏹️ Cancel", func() {
		if cancelFunc != nil {
			cancelFunc()
			statusLabel.SetText("Cancelling...")
		}
	})

	// ============================================
	// LAYOUT
	// ============================================

	form := container.NewVBox(
		widget.NewLabelWithStyle("HLS Downloader v2.0",
			fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),

		// URL Section
		widget.NewForm(
			widget.NewFormItem("Playlist URL", urlEntry),
		),

		// Variant Detection Section
		container.NewHBox(
			detectButton,
			widget.NewLabel("Variant:"),
			variantSelect,
		),

		// Output Section
		widget.NewForm(
			widget.NewFormItem("Output File", outputEntry),
			widget.NewFormItem("Parallel Connections", parallelEntry),
		),

		// Action Buttons
		container.NewHBox(
			startButton,
			cancelButton,
		),

		// Progress
		progressBar,
		statusLabel,
	)

	// Scrollable content
	content := container.NewScroll(form)
	window.SetContent(content)

	// Handle window close
	window.SetOnClosed(func() {
		if cancelFunc != nil {
			cancelFunc()
		}
	})

	window.ShowAndRun()
}
