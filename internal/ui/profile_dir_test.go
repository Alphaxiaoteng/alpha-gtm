package ui

import "testing"

// profile 目录名被 UI 建目录与 CLI 删目录两处共用。算法一旦改变，
// 已存在的登录态目录会变成孤儿，删除时也清不到。这里固定几个样本值，
// 让改动必须是有意为之。
func TestProfileDirNameStable(t *testing.T) {
	cases := map[int64]string{
		1:  "pc3383c52",
		27: "p875c1794",
	}
	for id, want := range cases {
		if got := ProfileDirName(id); got != want {
			t.Errorf("ProfileDirName(%d) = %q, want %q (算法变更会让已存在的 profile 目录失联)", id, got, want)
		}
	}
}

// 同一账号必须始终映射到同一个目录。
func TestProfileDirNameDeterministic(t *testing.T) {
	for i := int64(1); i <= 50; i++ {
		if ProfileDirName(i) != ProfileDirName(i) {
			t.Fatalf("ProfileDirName(%d) 不稳定", i)
		}
	}
}
