package ui

import "strings"

// 平台搜索结果里除了作品，还会夹广告卡和引导卡。它们在运营清单里被写成了
// 「读不到」，其实平台自己给了明确标记（2026-11-13 实抓：小红书 20 条 = 17 条
// 笔记 + 3 张 model_type=ads 的广告卡；抖音 8 条 = 7 张作品卡 + 1 张
// douyin.doctype_related_search.* 引导卡）。这一层把三类条目分开数：
// 作品 / 广告卡 / 引导卡，剩下的才算读不到。识别只认平台字段，不按标题猜。
const (
	radarCardKindAd    = "ad"
	radarCardKindGuide = "guide"
)

// radarResultCard 是一条被平台标成非作品的卡。Marker 记下命中的原始字段与取值，
// 回执与排查都靠它，不靠推断。
type radarResultCard struct {
	Kind   string `json:"kind"`            // ad 广告卡 / guide 引导卡
	Marker string `json:"marker"`          // 命中的原始字段与取值，如 "model_type=ads"
	AdID   string `json:"ad_id,omitempty"` // 广告位 id（ads_id），没有就空
	Title  string `json:"title,omitempty"` // 卡自带标题（有就记，没有不猜）
}

// radarResultTally 是一份搜索结果列表的逐条账。四个数字相加等于列表条数；
// Found=false 表示响应形态不认识，此时任何数字都不作数，调用方不得当成「0 条读不到」。
type radarResultTally struct {
	Found      bool              `json:"found"`
	Entries    int               `json:"entries"`
	Works      int               `json:"works"`
	Ads        int               `json:"ads"`
	Guides     int               `json:"guides"`
	Unreadable int               `json:"unreadable"`
	Samples    []radarResultCard `json:"samples,omitempty"`
}

// radarResultSampleCap bounds how many card samples travel with one tally.
const radarResultSampleCap = 8

// radarResultList returns the platform's own result list. Every other shape
// (missing list, wrong type) yields nil so the caller can say "unknown" instead
// of claiming a zero count.
func radarResultList(platform string, response map[string]interface{}) []map[string]interface{} {
	if len(response) == 0 {
		return nil
	}
	path := ""
	switch platform {
	case "xhs":
		path = "data.data.items"
	case "douyin":
		path = "data.business_data"
	default:
		return nil
	}
	value, ok := radarValue(response, path)
	if !ok {
		return nil
	}
	raw, ok := value.([]interface{})
	if !ok {
		return nil
	}
	entries := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		entry, isMap := item.(map[string]interface{})
		if !isMap {
			// A non-object entry is still one result slot; keep it so the
			// accounting stays honest about the list length.
			entries = append(entries, nil)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// radarResultCardFromEntry recognizes one result entry as an ad or guide card.
// Only platform-provided markers count:
//   - xhs: model_type=ads is the ad slot; any other non-"note" model_type is a
//     non-work card (search suggestions and the like).
//   - douyin: the related-search guide card carries a doctype_related_search
//     card id; a promoted video carries commerce_info.is_ad=true.
func radarResultCardFromEntry(platform string, entry map[string]interface{}) (radarResultCard, bool) {
	if len(entry) == 0 {
		return radarResultCard{}, false
	}
	switch platform {
	case "xhs":
		modelType := radarString(entry, "model_type")
		switch {
		case modelType == "ads":
			return radarResultCard{
				Kind:   radarCardKindAd,
				Marker: "model_type=ads",
				AdID:   radarString(entry, "ads.ads_id", "ads.ad_id"),
				Title:  radarString(entry, "ads.note.title"),
			}, true
		case modelType != "" && modelType != "note":
			return radarResultCard{Kind: radarCardKindGuide, Marker: "model_type=" + modelType}, true
		}
	case "douyin":
		cardID := radarString(entry, "card_id")
		if strings.HasPrefix(cardID, "douyin.doctype_related_search.") {
			return radarResultCard{Kind: radarCardKindGuide, Marker: "card_id=" + cardID}, true
		}
		// 抖音的作品在卡里一层：business_data[].data.aweme_info.*；
		// 走子树时也可能直接拿到 data 或 aweme_info，所以三条路径都认。
		for _, path := range []string{
			"data.aweme_info.commerce_info.is_ad", "aweme_info.commerce_info.is_ad", "commerce_info.is_ad",
		} {
			if !radarBool(entry, path) {
				continue
			}
			return radarResultCard{
				Kind:   radarCardKindAd,
				Marker: path + "=true",
				AdID: radarString(entry, "data.aweme_info.commerce_info.ad_id", "aweme_info.commerce_info.ad_id",
					"data.aweme_info.aweme_id", "aweme_info.aweme_id"),
				Title: radarString(entry, "data.aweme_info.desc", "aweme_info.desc"),
			}, true
		}
	}
	return radarResultCard{}, false
}

// radarEntryHasLocatableWork answers whether one result entry carries a work we
// could follow up on: an id or a url, never a title alone.
func radarEntryHasLocatableWork(platform string, entry map[string]interface{}) bool {
	if len(entry) == 0 {
		return false
	}
	switch platform {
	case "xhs":
		if radarString(entry, "model_type") != "note" {
			return false
		}
		// 真实形态：作品 id 在卡里的 note.id（24 位十六进制），没有 note_id 字段。
		if radarString(entry, "note_id", "note.note_id", "note_card.note_id",
			"share_url", "url", "note_card.url", "note.share_url") != "" {
			return true
		}
		for _, path := range []string{"note.id", "note_card.id", "id"} {
			if validXHSNoteID(radarString(entry, path)) {
				return true
			}
		}
		return false
	case "douyin":
		if !strings.HasPrefix(radarString(entry, "card_id"), "douyin.doctype_aweme_video.") {
			return false
		}
		if radarString(entry, "share_url", "url", "aweme_info.share_url") != "" {
			return true
		}
		for _, path := range []string{
			"data.aweme_info.aweme_id", "aweme_info.aweme_id",
			"data.aweme_list.0.aweme_id", "aweme_list.0.aweme_id",
		} {
			if radarString(entry, path) != "" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// radarClassifyResultCards accounts for every entry the platform returned, so
// an ad card is counted as an ad card and never as an unreadable work.
func radarClassifyResultCards(platform string, response map[string]interface{}) radarResultTally {
	entries := radarResultList(platform, response)
	if entries == nil {
		return radarResultTally{Found: false}
	}
	tally := radarResultTally{Found: true, Entries: len(entries), Samples: []radarResultCard{}}
	for _, entry := range entries {
		if card, isCard := radarResultCardFromEntry(platform, entry); isCard {
			switch card.Kind {
			case radarCardKindAd:
				tally.Ads++
			default:
				tally.Guides++
			}
			if len(tally.Samples) < radarResultSampleCap {
				tally.Samples = append(tally.Samples, card)
			}
			continue
		}
		if radarEntryHasLocatableWork(platform, entry) {
			tally.Works++
			continue
		}
		tally.Unreadable++
	}
	return tally
}

// radarBool reads a dotted path as a real boolean, so "false" or a missing
// field never counts as a hit.
func radarBool(item map[string]interface{}, key string) bool {
	value, ok := radarValue(item, key)
	if !ok {
		return false
	}
	flag, isBool := value.(bool)
	return isBool && flag
}
