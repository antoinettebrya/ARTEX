package executionextract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	pdfMaxBytes      = 8 << 20
	pdfMaxPages      = 100
	extractTextLimit = 120_000
)

var errExtractionLimit = errors.New("extraction byte limit exceeded")

// PDF reads one already-authorized URL and extracts the embedded text layer.
// Image-only pages can return an empty text result without an OCR step.
func PDF(ctx context.Context, client *http.Client, target string) (domain.ExecutionExtraction, error) {
	if _, err := domain.EvidenceURL(target); err != nil {
		return domain.ExecutionExtraction{}, err
	}
	infoTool, infoErr := exec.LookPath("pdfinfo")
	textTool, textErr := exec.LookPath("pdftotext")
	if infoErr != nil || textErr != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "pdf_tools_unavailable"}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := extractionGET(ctx, extractionHTTPClient(client), target, "application/pdf")
	if err != nil {
		return domain.ExecutionExtraction{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return domain.ExecutionExtraction{}, extractionStatus(response)
	}
	data, err := boundedExtractionRead(response.Body, pdfMaxBytes)
	if errors.Is(err, errExtractionLimit) {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "pdf_size_limit"}
	}
	if err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "pdf_read_failed", Retry: true}
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "pdf_format_invalid"}
	}
	dir, err := os.MkdirTemp("", "artex-pdf-")
	if err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "pdf_workspace_unavailable", Retry: true}
	}
	defer os.RemoveAll(dir)
	filename := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "pdf_workspace_unavailable", Retry: true}
	}
	info, _, err := capturePDF(ctx, infoTool, dir, 32<<10, "-enc", "UTF-8", "-isodates", filename)
	if err != nil {
		return domain.ExecutionExtraction{}, pdfToolError(ctx)
	}
	title, pages, metadata := parsePDFInfo(info)
	if pages < 1 {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "pdf_format_invalid"}
	}
	body, capped, err := capturePDF(ctx, textTool, dir, extractTextLimit, "-f", "1", "-l", strconv.Itoa(min(pages, pdfMaxPages)), "-enc", "UTF-8", "-nopgbrk", filename, "-")
	if err != nil {
		return domain.ExecutionExtraction{}, pdfToolError(ctx)
	}
	return domain.ExecutionExtraction{
		URL: target, Kind: domain.ExecPDF, Title: title, Text: extractionText(body, extractTextLimit),
		Pages: pages, Metadata: metadata, Requests: 1, Truncated: capped || pages > pdfMaxPages,
	}, nil
}

func parsePDFInfo(info string) (string, int, map[string]string) {
	metadata := map[string]string{"extractor": "poppler"}
	keys := map[string]string{
		"Author": "author", "Subject": "subject", "Keywords": "keywords", "Creator": "creator",
		"Producer": "producer", "CreationDate": "created_at", "ModDate": "modified_at",
		"Page size": "page_size", "PDF version": "pdf_version",
	}
	var title string
	var pages int
	for _, line := range strings.Split(info, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = extractionText(value, 1024)
		switch key {
		case "Title":
			title = value
		case "Pages":
			pages, _ = strconv.Atoi(value)
		default:
			if name := keys[key]; name != "" && value != "" {
				metadata[name] = value
			}
		}
	}
	return title, pages, metadata
}

func pdfToolError(ctx context.Context) error {
	if ctx.Err() != nil {
		return &domain.DataError{Code: "pdf_extract_timeout", Retry: true}
	}
	return &domain.DataError{Code: "pdf_extract_failed"}
}

func capturePDF(ctx context.Context, exe, dir string, limit int, args ...string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Env = []string{"LANG=C", "LC_ALL=C", "HOME=" + dir, "TMPDIR=" + dir, "PATH=" + os.Getenv("PATH")}
	out := &cappedExtractionOutput{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if errors.Is(err, errExtractionLimit) {
		// Closing the stdout pipe stops the subprocess when it reaches the cap.
		err = nil
	}
	if out.capped && ctx.Err() == nil {
		err = nil
	}
	return out.buf.String(), out.capped, err
}

type cappedExtractionOutput struct {
	buf    bytes.Buffer
	limit  int
	capped bool
}

func (w *cappedExtractionOutput) Write(data []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if len(data) > remaining {
		w.capped = true
		n, _ := w.buf.Write(data[:remaining])
		return n, errExtractionLimit
	}
	return w.buf.Write(data)
}

func extractionHTTPClient(in *http.Client) *http.Client {
	result := contentinventory.NewPublicHTTPClient(8 * time.Second)
	if in == nil {
		return result
	}
	safe := *in
	if safe.Transport == nil {
		safe.Transport = result.Transport
	}
	if safe.Timeout <= 0 || safe.Timeout > result.Timeout {
		safe.Timeout = result.Timeout
	}
	safe.CheckRedirect = result.CheckRedirect
	safe.Jar = nil
	return &safe
}

func extractionGET(ctx context.Context, client *http.Client, target, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, &domain.DataError{Code: "extraction_url_invalid"}
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "ARTEX-SEO-OS/1.0")
	res, err := client.Do(req)
	if err != nil {
		var typed *domain.DataError
		if errors.As(err, &typed) {
			return nil, typed
		}
		return nil, &domain.DataError{Code: "extraction_request_failed", Retry: true}
	}
	return res, nil
}

func extractionStatus(res *http.Response) error {
	if res.StatusCode == http.StatusTooManyRequests {
		value := res.Header.Get("Retry-After")
		var wait time.Duration
		if secs, err := strconv.Atoi(value); err == nil && secs > 0 && secs < 86400 {
			wait = time.Duration(secs) * time.Second
		} else if next, err := http.ParseTime(value); err == nil {
			wait = min(max(time.Until(next), 0), 24*time.Hour)
		}
		return &domain.DataError{Code: "extraction_rate_limit", Retry: true, RetryAfter: wait}
	}
	return &domain.DataError{Code: "extraction_http_status", Retry: res.StatusCode >= 500}
}

func boundedExtractionRead(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if len(data) > limit {
		return data, errExtractionLimit
	}
	return data, err
}

func extractionText(value string, maxBytes int) string {
	value = strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maxBytes {
		return value
	}
	for !utf8.RuneStart(value[maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}
