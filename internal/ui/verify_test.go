//go:build legacy_chromium

package ui

import (
	"testing"

	"alpha-nexus-egolite/internal/selectors"
	"github.com/chromedp/cdproto/network"
)

func TestLoginEvidenceUsesOnlyPlatformCatalogCookieNames(t *testing.T) {
	catalog, err := selectors.Default()
	if err != nil {
		t.Fatal(err)
	}
	generic := []*network.Cookie{{Name: "session_token"}, {Name: "auth"}, {Name: "uid"}}
	if isPassiveLoggedIn(generic, "bilibili") {
		t.Fatal("generic cookie names must not verify a catalog platform")
	}
	if isLoggedIn(catalog, "bilibili", generic, "https://member.bilibili.com/platform/home", "https://member.bilibili.com/platform/home") {
		t.Fatal("manager URL and generic cookies are not login evidence")
	}
	exact := []*network.Cookie{{Name: "web_session"}}
	if !isPassiveLoggedIn(exact, "xhs") || !isLoggedIn(catalog, "xhs", exact, "", "") {
		t.Fatal("exact catalog cookie should verify login")
	}
}

func TestManagerURLAndCookieCountNeverVerifyLogin(t *testing.T) {
	catalog, err := selectors.Default()
	if err != nil {
		t.Fatal(err)
	}
	cookies := make([]*network.Cookie, 10)
	for index := range cookies {
		cookies[index] = &network.Cookie{Name: "preference_cookie"}
	}
	if isLoggedIn(catalog, "zhihu", cookies, "https://www.zhihu.com/creator", "https://www.zhihu.com/creator") {
		t.Fatal("cookie count and manager URL must not become a login proof")
	}
}
