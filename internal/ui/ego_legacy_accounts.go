package ui

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"alpha-nexus-egolite/internal/db"
)

type legacyAccountMetadata struct {
	PlatformCode         string `json:"platform_code"`
	Name                 string `json:"name"`
	AccountIdentifier    string `json:"account_identifier"`
	HistoricalLoginState string `json:"historical_login_state"`
}

var legacyAccountPlatformOrder = map[string]int{
	"douyin": 0, "wechat_video": 1, "kuaishou": 2, "xhs": 3, "tiktok": 4,
	"youtube": 5, "instagram": 6, "facebook": 7, "wechat_mp": 8, "csdn": 9,
}

func (s *Server) handleAPIEgoLegacyAccounts(w http.ResponseWriter, _ *http.Request) {
	items, err := readLegacyAccountMetadata(s.legacyAccountsPath)
	if err != nil {
		writeJSON(w, map[string]any{"available": false, "count": 0, "items": []legacyAccountMetadata{}})
		return
	}
	writeJSON(w, map[string]any{"available": true, "count": len(items), "items": items})
}

type legacyAccountImportRequest struct {
	IdentityID    int64    `json:"identity_id"`
	PlatformCodes []string `json:"platform_codes"`
}

func (s *Server) handleAPIEgoLegacyAccountsImport(w http.ResponseWriter, r *http.Request) {
	items, err := readLegacyAccountMetadata(s.legacyAccountsPath)
	if err != nil || len(items) == 0 {
		http.Error(w, "legacy account metadata unavailable", http.StatusNotFound)
		return
	}

	var req legacyAccountImportRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// 如果指定了 platform_codes，则只导入匹配项
	if len(req.PlatformCodes) > 0 {
		selectedSet := make(map[string]bool, len(req.PlatformCodes))
		for _, code := range req.PlatformCodes {
			selectedSet[strings.TrimSpace(code)] = true
		}
		filtered := make([]legacyAccountMetadata, 0, len(req.PlatformCodes))
		for _, item := range items {
			if selectedSet[item.PlatformCode] {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	if len(items) == 0 {
		writeJSON(w, map[string]any{
			"imported": 0, "identities": []db.EgoIdentity{},
			"message": "no matching legacy accounts selected",
		})
		return
	}

	// 如果指定了 identity_id，导入到该具体浏览器/Identity 中
	if req.IdentityID > 0 {
		identity, err := s.db.GetEgoIdentity(req.IdentityID)
		if err != nil {
			http.Error(w, "target identity not found", http.StatusBadRequest)
			return
		}
		existingAccounts, err := s.db.ListAccountsByIdentity(req.IdentityID)
		if err != nil {
			http.Error(w, "failed to read identity accounts", http.StatusInternalServerError)
			return
		}
		existingCodes := make([]string, 0, len(existingAccounts))
		for _, acc := range existingAccounts {
			if acc.Enabled {
				existingCodes = append(existingCodes, acc.PlatformCode)
			}
		}

		newCodes := append([]string{}, existingCodes...)
		for _, item := range items {
			alreadyExists := false
			for _, ec := range existingCodes {
				if ec == item.PlatformCode {
					alreadyExists = true
					break
				}
			}
			if !alreadyExists {
				newCodes = append(newCodes, item.PlatformCode)
			}
		}

		configured, err := s.db.ConfigureIdentityPlatforms(req.IdentityID, newCodes)
		if err != nil {
			http.Error(w, "failed to configure identity platforms: "+err.Error(), http.StatusInternalServerError)
			return
		}

		metadata := make(map[string]legacyAccountMetadata, len(items))
		for _, item := range items {
			metadata[item.PlatformCode] = item
		}
		for _, acc := range configured {
			if item, ok := metadata[acc.PlatformCode]; ok {
				_ = s.db.UpdateImportedAccountMetadata(
					acc.ID, item.Name, item.AccountIdentifier,
					"旧 Chromium 账号资料；需在 EgoLite 重新登录",
				)
			}
		}

		writeJSON(w, map[string]any{
			"imported": len(items), "identities": []db.EgoIdentity{*identity},
			"login_state": "unknown", "requires_egolite_login": true,
		})
		return
	}

	identities, err := importLegacyAccountMetadata(s.db, items)
	if err != nil {
		http.Error(w, "legacy account metadata import failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"imported": len(items), "identities": identities,
		"login_state": "unknown", "requires_egolite_login": true,
	})
}

func readLegacyAccountMetadata(path string) ([]legacyAccountMetadata, error) {
	if strings.TrimSpace(path) == "" {
		return nil, os.ErrNotExist
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return nil, os.ErrNotExist
	}
	location := &url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}
	legacy, err := sql.Open("sqlite", location.String())
	if err != nil {
		return nil, err
	}
	defer legacy.Close()
	rows, err := legacy.Query(`SELECT platform_code,COALESCE(name,''),COALESCE(account_identifier,''),
		COALESCE(login_state,'unknown') FROM accounts WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]legacyAccountMetadata, 0)
	for rows.Next() {
		var item legacyAccountMetadata
		if err := rows.Scan(&item.PlatformCode, &item.Name, &item.AccountIdentifier, &item.HistoricalLoginState); err != nil {
			return nil, err
		}
		item.PlatformCode = strings.TrimSpace(item.PlatformCode)
		if _, supported := legacyAccountPlatformOrder[item.PlatformCode]; !supported {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(left, right int) bool {
		return legacyAccountPlatformOrder[items[left].PlatformCode] < legacyAccountPlatformOrder[items[right].PlatformCode]
	})
	return items, nil
}

func importLegacyAccountMetadata(database *db.DB, items []legacyAccountMetadata) ([]db.EgoIdentity, error) {
	if database == nil || len(items) == 0 {
		return nil, fmt.Errorf("legacy account metadata required")
	}
	lanes := make([][]legacyAccountMetadata, 0, 1)
	occurrences := make(map[string]int)
	for _, item := range items {
		lane := occurrences[item.PlatformCode]
		occurrences[item.PlatformCode]++
		for len(lanes) <= lane {
			lanes = append(lanes, nil)
		}
		lanes[lane] = append(lanes[lane], item)
	}
	existing, err := database.ListEgoIdentities()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]db.EgoIdentity, len(existing))
	for _, identity := range existing {
		byName[identity.Name] = identity
	}
	result := make([]db.EgoIdentity, 0, len(lanes))
	for laneIndex, lane := range lanes {
		name := "Chromium 账号导入"
		if len(lanes) > 1 {
			name = fmt.Sprintf("Chromium 账号导入 %d", laneIndex+1)
		}
		identity, ok := byName[name]
		if !ok {
			created, err := database.CreateEgoIdentity(name)
			if err != nil {
				return nil, err
			}
			identity = *created
		}
		codes := make([]string, 0, len(lane))
		for _, item := range lane {
			codes = append(codes, item.PlatformCode)
		}
		accounts, err := database.ConfigureIdentityPlatforms(identity.ID, codes)
		if err != nil {
			return nil, err
		}
		metadata := make(map[string]legacyAccountMetadata, len(lane))
		for _, item := range lane {
			metadata[item.PlatformCode] = item
		}
		for _, account := range accounts {
			item := metadata[account.PlatformCode]
			if err := database.UpdateImportedAccountMetadata(
				account.ID, item.Name, item.AccountIdentifier,
				"旧 Chromium 账号资料；需在 EgoLite 重新登录",
			); err != nil {
				return nil, err
			}
		}
		result = append(result, identity)
	}
	return result, nil
}
