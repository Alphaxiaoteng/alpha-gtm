// Package transcript turns a public media URL into timed text.
//
// The tool it delegates to is optional, exactly like the screen filter in
// rust/screen-encode: when the binary is missing the capability reports itself
// unavailable and the rest of the app keeps working. Nothing here starts a
// browser, reads a profile, or handles credentials; only public URLs are
// accepted, and the caller is told plainly when a source needs a login it will
// not perform.
package transcript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Source says where the text came from, so callers can tell platform-provided
// captions from machine transcription.
type Source string

const (
	SourcePlatformSubtitle Source = "platform_subtitle"
	SourceLocalASR         Source = "local_asr"
	SourceUnavailable      Source = "unavailable"
)

var (
	// ErrToolMissing means the extractor binary could not be found. The feature
	// is optional, so this is not a failure of the app.
	ErrToolMissing = errors.New("transcript extractor is not installed")
	// ErrLoginRequired means the source needs an authenticated session. The app
	// never stores cookies, so the caller must hand this to the operator.
	ErrLoginRequired = errors.New("source requires a logged-in session")
)

// Segment is one timed line of text.
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Result is the outcome of one extraction.
type Result struct {
	Source    Source    `json:"source"`
	Title     string    `json:"title"`
	Duration  float64   `json:"duration"`
	Language  string    `json:"language"`
	Segments  []Segment `json:"segments"`
	PlainText string    `json:"plain_text"`
	ElapsedMS int64     `json:"elapsed_ms"`
}

// Request describes one extraction.
type Request struct {
	URL       string
	Languages []string
	Timeout   time.Duration
}

const defaultTimeout = 90 * time.Second

// Extractor runs one media tool as a subprocess.
type Extractor struct {
	// Binary is the extractor executable name or path. Empty means "look it up
	// on PATH at call time", which keeps a missing install from being an error
	// at construction.
	Binary string
}

// New returns an Extractor that resolves the binary on PATH.
func New() *Extractor { return &Extractor{Binary: "yt-dlp"} }

// Available reports whether the extractor can run at all.
func (e *Extractor) Available() bool {
	_, err := e.resolve()
	return err == nil
}

func (e *Extractor) resolve() (string, error) {
	binary := e.Binary
	if binary == "" {
		binary = "yt-dlp"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", ErrToolMissing
	}
	return path, nil
}

// subtitleURL is the subset of the extractor's metadata this package needs.
type metadata struct {
	Title             string                       `json:"title"`
	Duration          float64                      `json:"duration"`
	Subtitles         map[string][]subtitleVariant `json:"subtitles"`
	AutomaticCaptions map[string][]subtitleVariant `json:"automatic_captions"`
}

type subtitleVariant struct {
	Ext  string `json:"ext"`
	URL  string `json:"url"`
	Data string `json:"data"`
}

// metadataArgs is the fixed argument set for a metadata probe.
//
// There is deliberately no cookie or profile flag here: this app never reads a
// browser session, so a source that needs one is reported, not worked around.
func metadataArgs(url string) []string {
	return []string{
		"--dump-single-json",
		"--no-playlist",
		"--skip-download",
		"--no-warnings",
		"--socket-timeout", "15",
		"--retries", "2",
		"--extractor-retries", "2",
		url,
	}
}

// Extract fetches platform-provided captions for a public URL.
//
// When the source has no captions the result carries SourceUnavailable rather
// than an error: "this video has no subtitles" is a normal answer, not a
// failure. A URL that needs a session returns ErrLoginRequired so the caller can
// route it to the operator instead of pretending.
func (e *Extractor) Extract(ctx context.Context, req Request) (Result, error) {
	started := time.Now()
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return Result{}, fmt.Errorf("url is required")
	}
	parsed, err := neturl.Parse(url)
	if err != nil || !isPublicURL(url) {
		return Result{}, fmt.Errorf("only public http(s) urls are accepted")
	}
	// The extractor follows the URL itself, so a loopback target would make it
	// a request-forgery hop even though this package never fetches it.
	if isPrivateHost(parsed.Hostname()) {
		return Result{}, fmt.Errorf("only public http(s) urls are accepted")
	}
	binary, err := e.resolve()
	if err != nil {
		return Result{}, err
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := runCapture(runCtx, binary, metadataArgs(url)...)
	if err != nil {
		if needsLogin(out) {
			return Result{}, ErrLoginRequired
		}
		return Result{}, fmt.Errorf("read media metadata: %w", err)
	}
	var meta metadata
	if err := json.Unmarshal(out, &meta); err != nil {
		// The extractor prints one JSON object; anything else means it is not
		// the tool we expect (a different binary on PATH, or a wrapper script).
		return Result{}, fmt.Errorf("parse media metadata: %w", err)
	}

	result := Result{Title: meta.Title, Duration: meta.Duration, ElapsedMS: time.Since(started).Milliseconds()}
	language, variant, ok := pickSubtitle(meta, req.Languages)
	if !ok {
		result.Source = SourceUnavailable
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result, nil
	}
	result.Language = language
	segments, err := e.fetchSubtitle(runCtx, binary, variant)
	if err != nil {
		return Result{}, err
	}
	result.Source = SourcePlatformSubtitle
	result.Segments = segments
	result.PlainText = joinSegments(segments)
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}

// pickSubtitle prefers a manual caption in the caller's language order, then an
// automatic one, so a hand-written track always beats machine captions.
func pickSubtitle(meta metadata, preferred []string) (string, subtitleVariant, bool) {
	order := preferred
	if len(order) == 0 {
		order = []string{"zh-Hans", "zh", "en"}
	}
	for _, table := range []map[string][]subtitleVariant{meta.Subtitles, meta.AutomaticCaptions} {
		for _, want := range order {
			for language, variants := range table {
				if !languageMatches(language, want) {
					continue
				}
				for _, variant := range variants {
					if variant.Ext == "json3" || variant.Ext == "vtt" || variant.Ext == "srt" {
						return language, variant, true
					}
				}
			}
		}
	}
	return "", subtitleVariant{}, false
}

// languageMatches accepts exact and prefix matches so "zh" finds "zh-Hans".
func languageMatches(have, want string) bool {
	have, want = strings.ToLower(have), strings.ToLower(want)
	return have == want || strings.HasPrefix(have, want+"-")
}

// fetchCaption downloads one caption document.
//
// The request carries no cookies and no referer: the app never impersonates a
// logged-in session, so a caption that requires one simply fails and the caller
// hears about it.
func fetchCaption(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build caption request: %w", err)
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	// Caption URLs come from an extractor's JSON, not from us, and a redirect
	// is the one place a hostile value could steer this request somewhere it
	// should not go. Each hop must stay public http(s); anything else stops here.
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("caption redirect chain too long")
			}
			if !isPublicURL(next.URL.String()) {
				return fmt.Errorf("caption redirect left http(s)")
			}
			if isPrivateHost(next.URL.Hostname()) {
				return fmt.Errorf("caption redirect points at a private address")
			}
			return nil
		},
	}
	if isPrivateHost(req.URL.Hostname()) {
		return nil, fmt.Errorf("caption host is not routable")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch caption: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch caption: status %d", resp.StatusCode)
	}
	// Captions are text; cap the read so a hostile endpoint cannot exhaust
	// memory through a single document.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read caption: %w", err)
	}
	return body, nil
}

func (e *Extractor) fetchSubtitle(ctx context.Context, binary string, variant subtitleVariant) ([]Segment, error) {
	if variant.Ext != "json3" {
		// Other formats are fetched the same way but decoded differently; json3
		// is the one the extractor emits with stable timing, so the caller gets
		// a clear "unsupported" instead of silently wrong timings.
		return nil, fmt.Errorf("unsupported subtitle format %q", variant.Ext)
	}
	var raw []byte
	if variant.Data != "" {
		raw = []byte(variant.Data)
	} else if variant.URL != "" {
		// A caption URL points at the caption document itself, not at a media
		// page: fetching it with the extractor would make it look for a video
		// that is not there. A plain GET is what the format expects.
		out, err := fetchCaption(ctx, variant.URL)
		if err != nil {
			return nil, err
		}
		raw = out
	} else {
		return nil, fmt.Errorf("subtitle has neither data nor url")
	}
	return parseJSON3(raw)
}

// json3Caption is the shape the extractor emits for timed captions.
type json3Caption struct {
	Events []struct {
		TStartMs    float64 `json:"tStartMs"`
		TDurationMs float64 `json:"dDurationMs"`
		Segs        []struct {
			UTF8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

func parseJSON3(raw []byte) ([]Segment, error) {
	var caption json3Caption
	if err := json.Unmarshal(raw, &caption); err != nil {
		return nil, fmt.Errorf("parse captions: %w", err)
	}
	segments := make([]Segment, 0, len(caption.Events))
	for _, event := range caption.Events {
		var text strings.Builder
		for _, seg := range event.Segs {
			text.WriteString(seg.UTF8)
		}
		line := strings.TrimSpace(strings.ReplaceAll(text.String(), "\n", " "))
		if line == "" {
			continue
		}
		start := event.TStartMs / 1000
		segments = append(segments, Segment{
			Start: start,
			End:   start + event.TDurationMs/1000,
			Text:  line,
		})
	}
	return segments, nil
}

func joinSegments(segments []Segment) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		parts = append(parts, segment.Text)
	}
	return strings.Join(parts, " ")
}

// needsLogin recognises the extractor's own wording for an authenticated source.
func needsLogin(output []byte) bool {
	lower := strings.ToLower(string(output))
	for _, marker := range []string{"login required", "sign in to confirm", "cookies-from-browser", "video is private", "private video", "members-only"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// urlPattern matches whole http(s) URLs so they can be replaced before an
// error reaches a log or a toast. Extractor stderr routinely echoes the full
// source URL including signature parameters.
var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

func redactURLs(text string) string {
	return urlPattern.ReplaceAllString(text, "[url]")
}

func isPublicURL(raw string) bool {
	lower := strings.TrimSpace(strings.ToLower(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// isPrivateHost rejects loopback, link-local, and RFC1918 literals. It is a
// literal check, not a resolver: a hostname that resolves privately is a
// separate problem, but the addresses an extractor could plausibly hand back
// are literals, and refusing them removes the obvious path.
func isPrivateHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return true
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified()
}

// runCapture runs the extractor and returns stdout, folding stderr into the
// error so a failed call explains itself.
func runCapture(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := redactURLs(strings.TrimSpace(stderr.String()))
		if len(detail) > 400 {
			detail = detail[:400]
		}
		return []byte(detail), fmt.Errorf("%w: %s", err, detail)
	}
	return out, nil
}

// String keeps error messages from leaking a full URL into logs.
func (r Result) String() string {
	return fmt.Sprintf("%s %s %.0fs %s", r.Source, r.Language, r.Duration, strconv.Itoa(len(r.Segments))+" segments")
}
