package db

import (
	"reflect"
	"testing"
)

func TestAccountTagsBulkAppendRemoveReplaceAndResolve(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	first, err := database.CreateAccount("xhs", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.CreateAccount("xhs", "second", "")
	if err != nil {
		t.Fatal(err)
	}
	third, err := database.CreateAccount("douyin", "third", "")
	if err != nil {
		t.Fatal(err)
	}
	brand, err := database.UpsertAccountTag("品牌 A", "#22C55E")
	if err != nil {
		t.Fatal(err)
	}
	region, err := database.UpsertAccountTag("华东", "#3B82F6")
	if err != nil {
		t.Fatal(err)
	}

	if err := database.BulkUpdateAccountTags([]int64{second.ID, first.ID, first.ID}, []int64{brand.ID, region.ID}, AccountTagModeAppend); err != nil {
		t.Fatal(err)
	}
	// Append is idempotent.
	if err := database.BulkUpdateAccountTags([]int64{first.ID, second.ID}, []int64{brand.ID}, AccountTagModeAppend); err != nil {
		t.Fatal(err)
	}
	byAccount, err := database.ListAccountTagsByAccount()
	if err != nil {
		t.Fatal(err)
	}
	if len(byAccount[first.ID]) != 2 || len(byAccount[second.ID]) != 2 || len(byAccount[third.ID]) != 0 {
		t.Fatalf("unexpected tag mapping: %+v", byAccount)
	}

	if err := database.BulkUpdateAccountTags([]int64{second.ID}, []int64{region.ID}, AccountTagModeRemove); err != nil {
		t.Fatal(err)
	}
	if err := database.BulkUpdateAccountTags([]int64{first.ID}, []int64{region.ID}, AccountTagModeReplace); err != nil {
		t.Fatal(err)
	}
	resolved, err := database.ListAccountsByTagIDs([]int64{region.ID})
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := make([]int64, 0, len(resolved))
	for _, account := range resolved {
		gotIDs = append(gotIDs, account.ID)
	}
	if want := []int64{first.ID}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("resolved account ids=%v want=%v", gotIDs, want)
	}
}

func TestAccountTagsRejectInvalidInputAndRollbackReplacement(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	tag, err := database.UpsertAccountTag("文章", "#8B5CF6")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.BulkUpdateAccountTags([]int64{account.ID}, []int64{tag.ID}, AccountTagModeAppend); err != nil {
		t.Fatal(err)
	}
	if err := database.BulkUpdateAccountTags([]int64{account.ID}, []int64{tag.ID, 999999}, AccountTagModeReplace); err == nil {
		t.Fatal("replacement with unknown tag must fail")
	}
	byAccount, err := database.ListAccountTagsByAccount()
	if err != nil {
		t.Fatal(err)
	}
	if len(byAccount[account.ID]) != 1 || byAccount[account.ID][0].ID != tag.ID {
		t.Fatalf("failed replacement changed existing tags: %+v", byAccount[account.ID])
	}
	for _, invalid := range []struct{ name, color string }{
		{"", "#112233"},
		{"bad color", "red"},
	} {
		if _, err := database.UpsertAccountTag(invalid.name, invalid.color); err == nil {
			t.Fatalf("invalid tag accepted: %+v", invalid)
		}
	}
}

func TestAccountTagsPersistDisplayOrderAndKeepNewAccountsUnclassified(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "unclassified", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := database.UpsertAccountTag("标签一", "#0EA5E9")
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.UpsertAccountTag("标签二", "#F97316")
	if err != nil {
		t.Fatal(err)
	}
	third, err := database.UpsertAccountTag("优先标签", "#8B5CF6")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReorderAccountTags([]int64{third.ID, first.ID, second.ID}); err != nil {
		t.Fatal(err)
	}
	tags, err := database.ListAccountTags()
	if err != nil {
		t.Fatal(err)
	}
	gotOrder := make([]int64, 0, len(tags))
	for index, tag := range tags {
		gotOrder = append(gotOrder, tag.ID)
		if tag.Position != index {
			t.Fatalf("tag %d position=%d want=%d", tag.ID, tag.Position, index)
		}
	}
	if want := []int64{third.ID, first.ID, second.ID}; !reflect.DeepEqual(gotOrder, want) {
		t.Fatalf("tag order=%v want=%v", gotOrder, want)
	}
	byAccount, err := database.ListAccountTagsByAccount()
	if err != nil {
		t.Fatal(err)
	}
	if len(byAccount[account.ID]) != 0 {
		t.Fatalf("new account must remain unclassified, tags=%+v", byAccount[account.ID])
	}

	for name, order := range map[string][]int64{
		"duplicate": {first.ID, first.ID, third.ID},
		"missing":   {first.ID, second.ID},
		"unknown":   {first.ID, second.ID, 999999},
	} {
		if err := database.ReorderAccountTags(order); err == nil {
			t.Fatalf("%s order must be rejected", name)
		}
	}
	afterFailure, err := database.ListAccountTags()
	if err != nil {
		t.Fatal(err)
	}
	for index, tag := range afterFailure {
		if tag.ID != gotOrder[index] {
			t.Fatalf("invalid reorder changed persisted order: %+v", afterFailure)
		}
	}
}
