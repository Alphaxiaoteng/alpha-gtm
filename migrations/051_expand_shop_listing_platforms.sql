-- Expand shop.listing's frozen-plan target set without losing existing plans or items.
-- Rebuild both tables in one transaction so the item foreign key never points at
-- the retired plan table.
PRAGMA foreign_keys = ON;

BEGIN;

ALTER TABLE shop_listing_plans RENAME TO shop_listing_plans_050;

CREATE TABLE shop_listing_plans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  identity_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL CHECK (platform_code IN ('douyin_shop','taobao','tiktok_shop','shopee','shopify','amazon_seller','jd_shop','pinduoduo_shop','xhs_shop','kuaishou_shop','wechat_shop','alibaba_1688','aliexpress')),
  snapshot_json TEXT NOT NULL,
  snapshot_hash TEXT NOT NULL CHECK (length(snapshot_hash) = 64),
  idempotency_key TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL CHECK (status IN ('frozen','dry_run_ok','prepare_approved','manual_takeover')),
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK (concurrency = 1),
  error_code TEXT NOT NULL DEFAULT '' CHECK (error_code = '' OR (length(error_code) BETWEEN 1 AND 64 AND substr(error_code,1,1) GLOB '[A-Z]' AND error_code NOT GLOB '*[^A-Z_]*')),
  created_at TEXT NOT NULL,
  frozen_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (identity_id) REFERENCES ego_identities(id) ON DELETE RESTRICT,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT
);

INSERT INTO shop_listing_plans (
  id, identity_id, account_id, platform_code, snapshot_json, snapshot_hash,
  idempotency_key, status, concurrency, error_code, created_at, frozen_at, updated_at
)
SELECT id, identity_id, account_id, platform_code, snapshot_json, snapshot_hash,
  idempotency_key, status, concurrency, error_code, created_at, frozen_at, updated_at
FROM shop_listing_plans_050;

ALTER TABLE shop_listing_items RENAME TO shop_listing_items_050;

CREATE TABLE shop_listing_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id INTEGER NOT NULL,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  payload_json TEXT NOT NULL,
  payload_hash TEXT NOT NULL CHECK (length(payload_hash) = 64),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','manual_takeover')),
  error_code TEXT NOT NULL DEFAULT '' CHECK (error_code = '' OR (length(error_code) BETWEEN 1 AND 64 AND substr(error_code,1,1) GLOB '[A-Z]' AND error_code NOT GLOB '*[^A-Z_]*')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (plan_id) REFERENCES shop_listing_plans(id) ON DELETE CASCADE,
  UNIQUE (plan_id, ordinal)
);

INSERT INTO shop_listing_items (
  id, plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at
)
SELECT id, plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at
FROM shop_listing_items_050;

DROP TABLE shop_listing_items_050;
DROP TABLE shop_listing_plans_050;

COMMIT;
