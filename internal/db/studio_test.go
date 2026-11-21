package db_test

import (
	"strings"
	"testing"
	"time"
)

func TestStudioScriptsAndClipJobsPersistLocally(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	media, err := database.UpsertMediaLibrary("/tmp/source.mp4", "studio-source", 100, "video/mp4", 1080, 1920, 90_000, "")
	if err != nil {
		t.Fatal(err)
	}
	script, err := database.CreateStudioScript("开场", "第一段内容")
	if err != nil {
		t.Fatal(err)
	}
	job, err := database.CreateClipJob(media.ID, script.ID, media.FilePath, 1_000, 59_000, `{"title":"开场"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishClipJob(job.ID, "completed", "/tmp/clip.mp4", "", ""); err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetClipJob(job.ID)
	if err != nil || stored.Status != "completed" || stored.OutputPath != "/tmp/clip.mp4" {
		t.Fatalf("job=%+v err=%v", stored, err)
	}
	if err := database.DeleteStudioScript(script.ID); err == nil || !strings.Contains(err.Error(), "clip jobs") {
		t.Fatalf("delete referenced script error=%v", err)
	}
	if err := database.DeleteMediaLibrary(media.ID); err == nil || !strings.Contains(err.Error(), "clip jobs") {
		t.Fatalf("delete referenced media error=%v", err)
	}
	unreferenced, err := database.UpsertMediaLibrary("/tmp/unreferenced.mp4", "studio-unreferenced", 100, "video/mp4", 0, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteMediaLibrary(unreferenced.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetMediaLibraryItem(unreferenced.ID); err == nil {
		t.Fatal("deleted media index is still present")
	}
}

func TestStudioMaterialAndScriptCategoriesPersist(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	media, err := database.UpsertMediaLibrary("/tmp/article-cover.png", "studio-article-cover", 100, "image/png", 1080, 1080, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetMediaLibraryPurpose(media.ID, "article"); err != nil {
		t.Fatal(err)
	}
	storedMedia, err := database.GetMediaLibraryItem(media.ID)
	if err != nil || storedMedia.Purpose != "article" {
		t.Fatalf("media=%+v err=%v", storedMedia, err)
	}
	if err := database.SetMediaLibraryPurpose(media.ID, "unknown"); err == nil {
		t.Fatal("invalid media purpose was accepted")
	}

	script, err := database.CreateStudioScriptForType("图文脚本", "第一段内容", "graph_text")
	if err != nil {
		t.Fatal(err)
	}
	if script.ContentType != "graph_text" {
		t.Fatalf("script=%+v", script)
	}
	if err := database.ReplaceStudioScriptForType(script.ID, "文章脚本", "完整正文", "article"); err != nil {
		t.Fatal(err)
	}
	storedScript, err := database.GetStudioScript(script.ID)
	if err != nil || storedScript.ContentType != "article" {
		t.Fatalf("script=%+v err=%v", storedScript, err)
	}
	if _, err := database.CreateClipJob(media.ID, script.ID, media.FilePath, 0, 1000, `{"title":"invalid"}`); err == nil {
		t.Fatal("article script was accepted for a video clip job")
	}
	if _, err := database.CreateStudioScriptForType("错误", "内容", "unknown"); err == nil {
		t.Fatal("invalid script content type was accepted")
	}
}

func TestContentMediaAssociationRequiresAndProtectsLibraryItem(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("video", "视频", "", "正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	library, err := database.UpsertMediaLibrary("/tmp/linked.mp4", "linked-media", 100, "video/mp4", 1080, 1920, 1000, "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := database.CreateMediaAsset(content.ID, "video", library.FilePath, library.FilePath, library.Mime, library.SizeBytes, library.SHA256, library.Width, library.Height, library.DurationMS)
	if err != nil {
		t.Fatal(err)
	}
	var linkedLibraryID int64
	if err := database.QueryRow(`SELECT COALESCE(library_id,0) FROM media_assets WHERE id=?`, asset.ID).Scan(&linkedLibraryID); err != nil {
		t.Fatal(err)
	}
	if linkedLibraryID != library.ID {
		t.Fatalf("media library link=%d want=%d", linkedLibraryID, library.ID)
	}
	if err := database.DeleteMediaLibrary(library.ID); err == nil || !strings.Contains(err.Error(), "content media") {
		t.Fatalf("delete content-linked media error=%v", err)
	}
	if _, err := database.Exec(`INSERT INTO media_assets (content_id, kind, file_path, source, created_at) VALUES (?, 'video', '/tmp/unlinked.mp4', 'manual', ?)`, content.ID, time.Now().UTC().Format(time.RFC3339)); err == nil {
		t.Fatal("new media association without library_id was accepted")
	}
	if err := database.DeleteMediaAsset(asset.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteMediaLibrary(library.ID); err != nil {
		t.Fatal(err)
	}
}
