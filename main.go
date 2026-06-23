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
	"regexp"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("HLS Downloader - Variant & Parallel")
	w.Resize(fyne.NewSize(560, 400))

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("Paste master.m3u8 URL here")

	outputEntry := widget.NewEntry()
	outputEntry.SetPlaceHolder("Output file, e.g. output.ts or output.mp4")
	outputEntry.SetText("output.ts")

	variantSelect := widget.NewSelect([]string{"Detecting variants..."}, func(s string) {})

	parallelEntry := widget.NewEntry()
	parallelEntry.SetText("4")
	parallelEntry.SetPlaceHolder("Number of parallel connections (1-10)")

	status := widget.NewLabel("Ready")
	progress := widget.NewProgressBar()

	detectButton := widget.NewButton("Detect Variants", func() {
		m3u8URL := strings.TrimSpace(urlEntry.Text)
		if m3u8URL == "" {
			dialog.ShowError(fmt.Errorf("please enter a master.m3u8 URL"), w)
			return
		}

		go func() {
			status.SetText("Detecting variants...")
			variants, err := detectVariants(context.Background(), m3u8URL)
			if err != nil {
				status.SetText("Error detecting variants: " + err.Error())
				dialog.ShowError(err, w)
				return
			}
			if len(variants) == 0 {
				status.SetText("No variants found (may be direct segment playlist)")
				variantSelect.SetOptions([]string{"Direct playlist"})
				variantSelect.SetSelected("Direct playlist")
				return
			}

			options := make([]string, len(variants))
			for i, v := range variants {
				options[i] = v.Label
			}
			variantSelect.SetOptions(options)
			variantSelect.SetSelected(options[0])
			status.SetText(fmt.Sprintf("Found %d variant(s)", len(variants)))
		}()
	})

	startButton := widget.NewButton("Download", func() {
		m3u8URL := strings.TrimSpace(urlEntry.Text)
		outputFile := strings.TrimSpace(outputEntry.Text)
		parallelStr := strings.TrimSpace(parallelEntry.Text)

		if m3u8URL == "" {
			dialog.ShowError(fmt.Errorf("please enter a master.m3u8 URL"), w)
			return
		}
		if outputFile == "" {
			dialog.ShowError(fmt.Errorf("please enter an output file name"), w)
			return
		}

		parallel, err := strconv.Atoi(parallelStr)
		if err != nil || parallel < 1 || parallel > 10 {
			dialog.ShowError(fmt.Errorf("parallel must be between 1 and 10"), w)
			return
		}

		go func() {
			progress.SetValue(0)
			status.SetText("Downloading...")
			err := downloadHLSParallel(context.Background(), m3u8URL, outputFile, parallel, progress, status)
			if err != nil {
				status.SetText("Failed: " + err.Error())
				dialog.ShowError(err, w)
				return
			}
			status.SetText("Complete!")
			dialog.ShowInformation("Done", "Download completed successfully.", w)
		}()
	})

	content := container.NewVBox(
		widget.NewLabel("HLS Downloader (m3u8) - Variant & Parallel Support"),
		widget.NewForm(
			widget.NewFormItem("Playlist URL", urlEntry),
		),
		container.NewHBox(
			detectButton,
			widget.NewLabel("Variants:"),
			variantSelect,
		),
		widget.NewForm(
			widget.NewFormItem("Output file", outputEntry),
			widget.NewFormItem("Parallel (1-10)", parallelEntry),
		),
		startButton,
		progress,
		status,
	)

	w.SetContent(container.NewScroll(content))
	w.ShowAndRun()
}

type Variant struct {
	URL     string
	Label   string
	Bitrate int
}

func detectVariants(ctx context.Context, masterURL string) ([]Variant, error) {
	text, err := fetchText(ctx, masterURL)
	if err != nil {
		return nil, err
	}

	var variants []Variant
	re := regexp.MustCompile(`EXT-X-STREAM-INF:([^\n]+)\n([^\n]+)`)
	matches := re.FindAllStringSubmatch(text, -1)

	for _, match := range matches {
		attrs := match[1]
		uri := strings.TrimSpace(match[2])

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

		resolvedURL, err := resolveURL(masterURL, uri)
		if err != nil {
			continue
		}

		label := fmt.Sprintf("%s (%d kbps)", resolution, bitrate/1000)
		variants = append(variants, Variant{
			URL:     resolvedURL,
			Label:   label,
			Bitrate: bitrate,
		})
	}

	return variants, nil
}

func downloadHLSParallel(ctx context.Context, playlistURL, outputFile string, parallel int, progress *widget.ProgressBar, status *widget.Label) error {
	text, err := fetchText(ctx, playlistURL)
	if err != nil {
		return err
	}

	segmentURLs, err := parseM3U8(playlistURL, text)
	if err != nil {
		return err
	}
	if len(segmentURLs) == 0 {
		return fmt.Errorf("no media segments found in playlist")
	}

	out, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer out.Close()

	// Create a map to store segments in order
	segments := make(map[int][]byte)
	var mu sync.Mutex

	total := len(segmentURLs)
	status.SetText(fmt.Sprintf("Downloading %d segments in parallel (%d connections)...", total, parallel))

	// Create semaphore for limiting parallel connections
	semaphore := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	errChan := make(chan error, total)

	for i, segURL := range segmentURLs {
		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			segData, err := fetchSegment(ctx, url)
			if err != nil {
				errChan <- fmt.Errorf("segment %d: %w", index+1, err)
				return
			}

			mu.Lock()
			segments[index] = segData
			mu.Unlock()

			// Update progress
			completed := len(segments)
			progress.SetValue(float64(completed) / float64(total))
			status.SetText(fmt.Sprintf("Downloaded %d/%d segments", completed, total))

			errChan <- nil
		}(i, segURL)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	// Write segments in order
	status.SetText("Writing segments to file...")
	for i := 0; i < total; i++ {
		if data, ok := segments[i]; ok {
			_, err := out.Write(data)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func fetchText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Go HLS Downloader/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func fetchSegment(ctx context.Context, segmentURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, segmentURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Go HLS Downloader/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func parseM3U8(baseURL, data string) ([]string, error) {
	var segments []string
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		resolved, err := resolveURL(baseURL, line)
		if err != nil {
			return nil, err
		}
		segments = append(segments, resolved)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

func resolveURL(baseURL, reference string) (string, error) {
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
