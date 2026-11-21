package db_test

import "testing"

func TestShopWorkspaceDataIsIsolatedFromMediaTables(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	media, err := database.CreateContent("article", "Media draft", "", "media", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	shop, err := database.CreateShopContent("article", "Shop draft", "", "shop", "[]", "draft", "")
	if err != nil {
		t.Fatal(err)
	}
	if media.ID != 1 || shop.ID != 1 {
		t.Fatalf("independent ids expected media=%d shop=%d", media.ID, shop.ID)
	}
	mediaItems, err := database.ListContents()
	if err != nil {
		t.Fatal(err)
	}
	shopItems, err := database.ListShopContents()
	if err != nil {
		t.Fatal(err)
	}
	if len(mediaItems) != 1 || mediaItems[0].Title != "Media draft" {
		t.Fatalf("media contents leaked: %+v", mediaItems)
	}
	if len(shopItems) != 1 || shopItems[0].Title != "Shop draft" {
		t.Fatalf("shop contents leaked: %+v", shopItems)
	}

	mediaBase, err := database.CreateKnowledgeBase("Media KB", "")
	if err != nil {
		t.Fatal(err)
	}
	shopBase, err := database.CreateShopKnowledgeBase("Shop KB", "")
	if err != nil {
		t.Fatal(err)
	}
	if mediaBase.ID != 1 || shopBase.ID != 1 {
		t.Fatalf("independent knowledge ids expected media=%d shop=%d", mediaBase.ID, shopBase.ID)
	}
	shopBases, err := database.ListShopKnowledgeBases()
	if err != nil {
		t.Fatal(err)
	}
	if len(shopBases) != 1 || shopBases[0].Name != "Shop KB" {
		t.Fatalf("shop knowledge leaked: %+v", shopBases)
	}
}
