package db_test

import (
	"errors"
	"sync"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestConfigureIdentityPlatformsIfExpectedRejectsStaleConfiguration(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("冲突保护矩阵")
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := database.ConfigureIdentityPlatforms(identity.ID, []string{"douyin"})
	if err != nil {
		t.Fatal(err)
	}
	original := accounts[0]
	if err := database.UpdateAccountLoginState(original.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountManagerAddr(original.ID, "https://creator.douyin.com/"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConfigureIdentityPlatforms(identity.ID, []string{"douyin", "youtube"}); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ConfigureIdentityPlatformsIfExpected(identity.ID, []string{"xhs"}, []string{"douyin"}); !errors.Is(err, db.ErrEgoPlatformConfigurationConflict) {
		t.Fatalf("stale configure err=%v, want configuration conflict", err)
	}

	all, err := database.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundOriginal, foundYouTube := false, false
	for _, account := range all {
		switch account.PlatformCode {
		case "douyin":
			foundOriginal = account.ID == original.ID && account.Enabled && account.LoginState == db.AccountAuthLoggedIn && account.ManagerAddr == "https://creator.douyin.com/"
		case "youtube":
			foundYouTube = account.Enabled
		case "xhs":
			t.Fatal("conflicting request created an account")
		}
	}
	if !foundOriginal || !foundYouTube {
		t.Fatalf("conflict changed configured accounts: %+v", all)
	}
}

func TestConfigureIdentityPlatformsIfExpectedAllowsEmptyAddAndLegacyReplace(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("CAS 矩阵")
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := database.ConfigureIdentityPlatformsIfExpected(identity.ID, []string{}, []string{})
	if err != nil || len(accounts) != 0 {
		t.Fatalf("empty configuration accounts=%+v err=%v", accounts, err)
	}
	accounts, err = database.ConfigureIdentityPlatformsIfExpected(identity.ID, []string{"douyin"}, []string{})
	if err != nil || len(accounts) != 1 || accounts[0].PlatformCode != "douyin" {
		t.Fatalf("add configuration accounts=%+v err=%v", accounts, err)
	}
	accounts, err = database.ConfigureIdentityPlatforms(identity.ID, []string{"youtube"})
	if err != nil || len(accounts) != 1 || accounts[0].PlatformCode != "youtube" {
		t.Fatalf("legacy replacement accounts=%+v err=%v", accounts, err)
	}
}

func TestConfigureIdentityPlatformsIfExpectedOnlyOneConcurrentAddWins(t *testing.T) {
	database := openEgoDB(t)
	identity, err := database.CreateEgoIdentity("并发 CAS 矩阵")
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, code := range []string{"douyin", "youtube"} {
		go func(code string) {
			start.Wait()
			_, err := database.ConfigureIdentityPlatformsIfExpected(identity.ID, []string{code}, []string{})
			results <- err
		}(code)
	}
	start.Done()

	successes, conflicts := 0, 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else if errors.Is(err, db.ErrEgoPlatformConfigurationConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent configure err=%v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	accounts, err := database.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, account := range accounts {
		if account.Enabled {
			enabled++
		}
	}
	if enabled != 1 {
		t.Fatalf("enabled accounts=%+v", accounts)
	}
}
