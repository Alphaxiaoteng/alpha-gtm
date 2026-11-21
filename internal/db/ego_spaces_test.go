package db_test

import (
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func openEgoDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestEgoIdentityOwnsOneAccountPerPlatform(t *testing.T) {
	database := openEgoDB(t)

	identity, err := database.CreateEgoIdentity("用户1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书主账号", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.CreateAccountForIdentity(identity.ID, "youtube", "YouTube 频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAccountForIdentity(identity.ID, "xhs", "重复小红书账号", ""); err == nil {
		t.Fatal("same platform was bound twice inside one identity")
	}

	accounts, err := database.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(accounts))
	}
	if first.IdentityID != identity.ID || second.IdentityID != identity.ID {
		t.Fatalf("identity links = %d, %d, want %d", first.IdentityID, second.IdentityID, identity.ID)
	}
}

func TestEgoIdentityNameCanBeChangedWithoutMovingPlatformAccounts(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("用户1")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书主账号", "")
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := database.RenameEgoIdentity(identity.ID, " 品牌主理人 ")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "品牌主理人" {
		t.Fatalf("renamed identity = %+v", renamed)
	}
	accounts, err := database.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].ID != account.ID || accounts[0].IdentityID != identity.ID {
		t.Fatalf("platform accounts moved during rename: %+v", accounts)
	}
}

func TestEgoIdentityRenameRejectsBlankName(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("用户1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RenameEgoIdentity(identity.ID, " \t "); err == nil {
		t.Fatal("blank identity name was accepted")
	}
}

func TestEgoSpaceIsUniquePerIdentityAndSharedByPlatformAccounts(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("用户1")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "频道A", "")
	if err != nil {
		t.Fatal(err)
	}

	second, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}

	space, err := database.CreateEgoSpaceForIdentity(identity.ID, 6, "task-6", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	if space.IdentityID != identity.ID || space.SpaceID != 6 || space.State != "ready" {
		t.Fatalf("space = %+v", space)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 7, "task-7", "duplicate"); err == nil {
		t.Fatal("second space for one identity unexpectedly succeeded")
	}

	firstSpace, err := database.GetEgoSpaceByAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondSpace, err := database.GetEgoSpaceByAccount(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstSpace.ID != secondSpace.ID || firstSpace.TaskID != "task-6" || firstSpace.Ownership != "agent" {
		t.Fatalf("platform accounts do not share identity space: first=%+v second=%+v", firstSpace, secondSpace)
	}
}

func TestEgoSpaceCanAtomicallyRebindAfterEgoLiteRemovedIt(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("恢复绑定测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	before, err := database.CreateEgoSpaceForIdentity(identity.ID, 5, "old-task", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	after, err := database.RebindEgoSpaceForIdentity(identity.ID, 8, "new-task", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != before.ID || after.SpaceID != 8 || after.TaskID != "new-task" || after.ControlEpoch != before.ControlEpoch+1 {
		t.Fatalf("rebound space=%+v before=%+v", after, before)
	}
}

func TestEgoSpaceAcceptsZeroBasedRuntimeID(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("零号 Space 浏览器")
	if err != nil {
		t.Fatal(err)
	}
	space, err := database.CreateEgoSpaceForIdentity(identity.ID, 0, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	if space.SpaceID != 0 {
		t.Fatalf("space=%+v, want zero-based runtime ID", space)
	}
	if _, err := database.RebindEgoSpaceForIdentity(identity.ID, 0, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatalf("zero-based rebind failed: %v", err)
	}
}

func TestEgoSpaceStateTransitionIncrementsControlEpoch(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("用户2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 9, "task-9", "alpha-nexus/matrix-2"); err != nil {
		t.Fatal(err)
	}

	updated, err := database.UpdateEgoSpaceStateForIdentity(identity.ID, "agentDelegatedToUser", "manual_takeover", "CAPTCHA_REQUIRED")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ControlEpoch != 2 || updated.Ownership != "agentDelegatedToUser" || updated.ErrorCode != "CAPTCHA_REQUIRED" {
		t.Fatalf("updated space = %+v", updated)
	}
}

func TestEgoIdentityDefaultsToNumberedBrowserNamesAndCanBeReordered(t *testing.T) {
	database := openEgoDB(t)

	identities, err := database.ListEgoIdentities()
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 1 || identities[0].Name != "浏览器1" {
		t.Fatalf("initial identities = %+v", identities)
	}
	second, err := database.CreateEgoIdentity("")
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "浏览器2" {
		t.Fatalf("automatic name = %q", second.Name)
	}

	updated, err := database.UpdateEgoIdentity(second.ID, " 海外矩阵 ", -1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "海外矩阵" || updated.Sort != -1 {
		t.Fatalf("updated identity = %+v", updated)
	}
	identities, err = database.ListEgoIdentities()
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) < 2 || identities[0].ID != second.ID {
		t.Fatalf("identity order = %+v", identities)
	}
}

func TestEgoIdentityMigrationRenamesOnlyLegacyAutomaticNames(t *testing.T) {
	database := openEgoDB(t)
	if _, err := database.Exec(`UPDATE ego_identities SET name='用户 1' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoIdentity("矩阵二"); err != nil {
		t.Fatal(err)
	}
	custom, err := database.CreateEgoIdentity("海外矩阵")
	if err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identities, err := database.ListEgoIdentities()
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 3 || identities[0].Name != "浏览器1" || identities[1].Name != "浏览器2" {
		t.Fatalf("migrated identities = %+v", identities)
	}
	if identities[2].ID != custom.ID || identities[2].Name != "海外矩阵" {
		t.Fatalf("custom identity was overwritten: %+v", identities[2])
	}
}

func TestEgoMigrationConsolidatesLegacyMetaLoginWithoutLosingAssets(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("Meta 迁移测试")
	if err != nil {
		t.Fatal(err)
	}
	facebook, err := database.CreateAccountForIdentity(identity.ID, "facebook", "Facebook 旧登录", "")
	if err != nil {
		t.Fatal(err)
	}
	instagram, err := database.CreateAccountForIdentity(identity.ID, "instagram", "Instagram 旧登录", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET login_state='logged_in' WHERE id=?`, facebook.ID); err != nil {
		t.Fatal(err)
	}
	page, err := database.CreateAccountAsset(facebook.ID, "facebook", "facebook_page", "Alpha Page", "1241410172381837", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	instagramAsset, err := database.CreateAccountAsset(instagram.ID, "instagram", "instagram_account", "Alpha IG", "17841416733781085", "", page.ID, page.Name)
	if err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	// The migration must remain safe when the app starts again.
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	accounts, err := database.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	var meta *db.Account
	for index := range accounts {
		account := &accounts[index]
		switch account.PlatformCode {
		case "meta":
			if meta != nil {
				t.Fatalf("multiple Meta login containers: %+v", accounts)
			}
			meta = account
		case "facebook", "instagram":
			if account.Enabled {
				t.Fatalf("legacy split Meta account remains enabled: %+v", account)
			}
		}
	}
	if meta == nil || !meta.Enabled || meta.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("consolidated Meta account = %+v", meta)
	}
	assets, err := database.ListAccountAssetsByAccount(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := map[int64]bool{}
	for _, asset := range assets {
		found[asset.ID] = true
	}
	if !found[page.ID] || !found[instagramAsset.ID] {
		t.Fatalf("Meta assets were not moved to account %d: %+v", meta.ID, assets)
	}
}

func TestConfigureIdentityPlatformsAddsReordersAndDisablesAccounts(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("内容矩阵")
	if err != nil {
		t.Fatal(err)
	}

	accounts, err := database.ConfigureIdentityPlatforms(identity.ID, []string{"douyin", "xhs", "youtube"})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 3 || accounts[0].PlatformCode != "douyin" || accounts[1].PlatformCode != "xhs" || accounts[2].PlatformCode != "youtube" {
		t.Fatalf("configured accounts = %+v", accounts)
	}

	accounts, err = database.ConfigureIdentityPlatforms(identity.ID, []string{"youtube", "douyin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].PlatformCode != "youtube" || accounts[1].PlatformCode != "douyin" {
		t.Fatalf("reconfigured accounts = %+v", accounts)
	}
	all, err := database.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range all {
		if account.PlatformCode == "xhs" && account.Enabled {
			t.Fatalf("removed platform account remains enabled: %+v", account)
		}
	}
}
