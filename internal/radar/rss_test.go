package radar

import (
	"context"
	"testing"
)

func TestRSSRejectsLocalAddressesAndLimitsFullTextLinks(t *testing.T) {
	for _, rawURL := range []string{"http://example.com/feed", "https://127.0.0.1/feed", "https://localhost/feed"} {
		if _, err := validatePublicHTTPSURL(context.Background(), rawURL); err == nil {
			t.Fatalf("unsafe RSS URL accepted: %s", rawURL)
		}
	}
	var document rssDocument
	for index := 0; index < 15; index++ {
		document.Channel.Items = append(document.Channel.Items, struct {
			Link string `xml:"link"`
		}{Link: "https://example.com/item/" + string(rune('a'+index))})
	}
	if links := rssLinks(document); len(links) != 10 {
		t.Fatalf("RSS full text limit=%d want 10", len(links))
	}
}
