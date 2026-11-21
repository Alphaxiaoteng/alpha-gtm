package publishplan

import (
	"fmt"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
)

func planMedia(snapshot Snapshot) []platforms.PublishMedia {
	media := make([]platforms.PublishMedia, 0, len(snapshot.Media))
	for _, item := range snapshot.Media {
		media = append(media, platforms.PublishMedia{
			Path: item.FilePath, Kind: item.Kind, Role: item.Role,
			Position: item.Position, SHA256: item.SHA256,
		})
	}
	return media
}

func validateFrozenTargetIdentity(database *db.DB, plan *Plan) error {
	if plan.Snapshot.Version != PublishPlanSnapshotVersion {
		return fmt.Errorf("PUBLISH_PLAN_REFREEZE_REQUIRED")
	}
	for _, target := range plan.Snapshot.Targets {
		account, err := database.GetAccount(target.AccountID)
		if err != nil || account.PlatformCode != target.AccountPlatformCode {
			return fmt.Errorf("PUBLISH_TARGET_SNAPSHOT_MISMATCH")
		}
		asset, err := database.GetAccountAsset(target.AssetID)
		if err != nil ||
			asset.AccountID != target.AccountID ||
			asset.PlatformCode != target.PlatformCode ||
			asset.AssetType != target.AssetType ||
			asset.Name != target.AssetName ||
			asset.ExternalID != target.AssetExternalID ||
			asset.Enabled != target.AssetEnabled ||
			asset.ConnectedAssetID != target.ConnectedAssetID ||
			asset.ConnectedAssetName != target.ConnectedAssetName {
			return fmt.Errorf("PUBLISH_TARGET_SNAPSHOT_MISMATCH")
		}
		if target.ConnectedAssetID == 0 {
			if target.ConnectedAssetPlatformCode != "" ||
				target.ConnectedAssetType != "" ||
				target.ConnectedAssetName != "" ||
				target.ConnectedAssetExternalID != "" ||
				target.ConnectedAssetEnabled {
				return fmt.Errorf("PUBLISH_TARGET_SNAPSHOT_MISMATCH")
			}
			continue
		}
		connected, err := database.GetAccountAsset(target.ConnectedAssetID)
		if err != nil ||
			connected.AccountID != target.AccountID ||
			connected.PlatformCode != target.ConnectedAssetPlatformCode ||
			connected.AssetType != target.ConnectedAssetType ||
			connected.Name != target.ConnectedAssetName ||
			connected.ExternalID != target.ConnectedAssetExternalID ||
			connected.Enabled != target.ConnectedAssetEnabled {
			return fmt.Errorf("PUBLISH_TARGET_SNAPSHOT_MISMATCH")
		}
	}
	return nil
}
