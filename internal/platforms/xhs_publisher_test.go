package platforms

import (
	"context"
	"testing"

	"alpha-nexus-egolite/internal/selectors"
)

func TestXHSPublisherValidatesVersionedSnapshotOffline(t *testing.T) {
	catalog, err := selectors.Default()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildPublishSettingsSnapshot("xhs", 7, `{"visibility":"private"}`, catalog.CatalogVersion, XHSBrowserAdapterVersion)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &xhsPublisher{}
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "xhs", Caption: "caption", DryRun: true, SettingsJSON: snapshot,
	})
	if err != nil || result == nil || result.Status != "dry_run_ok" {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	badSnapshot, err := BuildPublishSettingsSnapshot("xhs", 7, `{}`, catalog.CatalogVersion, XHSBrowserAdapterVersion+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Validate(PublishRequest{Caption: "caption", SettingsJSON: badSnapshot}); err == nil {
		t.Fatal("publisher accepted a snapshot for another adapter version")
	}
}
