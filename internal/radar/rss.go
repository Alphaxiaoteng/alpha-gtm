package radar

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RSSSource struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	FetchFullText bool   `json:"fetch_full_text"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type RSSSourceFile struct {
	Version   int         `json:"version"`
	Sources   []RSSSource `json:"sources"`
	UpdatedAt string      `json:"updated_at"`
}

type RSSRun struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	SourceName     string `json:"source_name"`
	FeedURL        string `json:"feed_url"`
	FeedPath       string `json:"feed_path"`
	ItemCount      int    `json:"item_count"`
	FullTextSaved  int    `json:"full_text_saved"`
	FullTextFailed int    `json:"full_text_failed"`
	DurationMS     int64  `json:"duration_ms"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type rssDocument struct {
	Channel struct {
		Items []struct {
			Link string `xml:"link"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func LoadRSSSources(dataDir string) (RSSSourceFile, error) {
	file := RSSSourceFile{Version: 1, Sources: []RSSSource{}}
	raw, err := os.ReadFile(filepath.Join(dataDir, "trend-radar", "rss", "sources.json"))
	if os.IsNotExist(err) {
		return file, nil
	}
	if err != nil {
		return file, err
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return RSSSourceFile{}, err
	}
	if file.Version != 1 {
		return RSSSourceFile{}, fmt.Errorf("unsupported RSS source version")
	}
	return file, nil
}

func SaveRSSSources(dataDir string, file RSSSourceFile) error {
	file.Version = 1
	file.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return replacePrivateFile(filepath.Join(dataDir, "trend-radar", "rss", "sources.json"), raw)
}

func ValidateRSSSource(source RSSSource) (RSSSource, error) {
	source.Name = strings.TrimSpace(source.Name)
	source.URL = strings.TrimSpace(source.URL)
	if source.Name == "" || len([]rune(source.Name)) > 80 {
		return RSSSource{}, fmt.Errorf("invalid RSS source name")
	}
	if _, err := validatePublicHTTPSURL(context.Background(), source.URL); err != nil {
		return RSSSource{}, err
	}
	return source, nil
}

func FetchRSS(ctx context.Context, dataDir string, source RSSSource) (RSSRun, error) {
	started := time.Now()
	run := RSSRun{
		ID: fmt.Sprintf("rss-%d", started.UTC().UnixNano()), SourceID: source.ID,
		SourceName: source.Name, FeedURL: source.URL, Status: "failed",
		CreatedAt: started.UTC().Format(time.RFC3339Nano),
	}
	runDir := filepath.Join(dataDir, "trend-radar", "rss", "runs", run.ID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return run, err
	}
	client := publicRSSClient()
	feed, err := fetchPublicDocument(ctx, client, source.URL, 10<<20)
	if err != nil {
		run.Error = "RSS 源抓取失败"
		run.DurationMS = time.Since(started).Milliseconds()
		_ = writeExclusiveJSON(filepath.Join(runDir, "manifest.json"), run)
		return run, err
	}
	run.FeedPath = filepath.Join(runDir, "feed.xml")
	if err := writeExclusiveFile(run.FeedPath, feed); err != nil {
		return run, err
	}
	var document rssDocument
	if err := xml.Unmarshal(feed, &document); err != nil {
		run.Error = "RSS XML 解析失败"
		run.DurationMS = time.Since(started).Milliseconds()
		_ = writeExclusiveJSON(filepath.Join(runDir, "manifest.json"), run)
		return run, err
	}
	links := rssLinks(document)
	run.ItemCount = len(links)
	if source.FetchFullText {
		pagesDir := filepath.Join(runDir, "pages")
		if err := os.MkdirAll(pagesDir, 0o700); err != nil {
			return run, err
		}
		for _, link := range links {
			page, pageErr := fetchPublicDocument(ctx, client, link, 5<<20)
			if pageErr != nil {
				run.FullTextFailed++
				continue
			}
			sum := sha256.Sum256([]byte(link))
			if err := writeExclusiveFile(filepath.Join(pagesDir, hex.EncodeToString(sum[:])+".html"), page); err != nil {
				run.FullTextFailed++
				continue
			}
			run.FullTextSaved++
		}
	}
	run.Status = "succeeded"
	run.DurationMS = time.Since(started).Milliseconds()
	if err := writeExclusiveJSON(filepath.Join(runDir, "manifest.json"), run); err != nil {
		return run, err
	}
	return run, nil
}

func ListRSSRuns(dataDir string) ([]RSSRun, error) {
	root := filepath.Join(dataDir, "trend-radar", "rss", "runs")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []RSSRun{}, nil
	}
	if err != nil {
		return nil, err
	}
	runs := make([]RSSRun, 0, len(entries))
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(root, entry.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		var run RSSRun
		if json.Unmarshal(raw, &run) == nil {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt > runs[j].CreatedAt })
	return runs, nil
}

func rssLinks(document rssDocument) []string {
	seen := map[string]bool{}
	links := make([]string, 0, 10)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || len(links) >= 10 {
			return
		}
		if _, err := url.ParseRequestURI(value); err == nil {
			seen[value] = true
			links = append(links, value)
		}
	}
	for _, item := range document.Channel.Items {
		add(item.Link)
	}
	for _, entry := range document.Entries {
		for _, link := range entry.Links {
			if link.Rel == "" || link.Rel == "alternate" {
				add(link.Href)
				break
			}
		}
	}
	return links
}

func publicRSSClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy:           nil,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, resolved := range ips {
				if publicIP(resolved.IP) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
				}
			}
			return nil, fmt.Errorf("RSS host does not resolve to a public address")
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many RSS redirects")
			}
			_, err := validatePublicHTTPSURL(request.Context(), request.URL.String())
			return err
		},
	}
}

func fetchPublicDocument(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	if _, err := validatePublicHTTPSURL(ctx, rawURL); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Alpha-Nexus-Radar/1.0")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("RSS HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, fmt.Errorf("RSS response exceeds local limit")
	}
	return body, nil
}

func validatePublicHTTPSURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, fmt.Errorf("RSS URL must be a public HTTPS address")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("RSS host cannot be resolved")
	}
	for _, resolved := range ips {
		if !publicIP(resolved.IP) {
			return nil, fmt.Errorf("RSS URL cannot use a private or local address")
		}
	}
	return parsed, nil
}

func publicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func replacePrivateFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".rss-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(body); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func writeExclusiveJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeExclusiveFile(path, raw)
}

func writeExclusiveFile(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
