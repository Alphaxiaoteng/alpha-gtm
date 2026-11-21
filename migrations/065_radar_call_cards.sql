-- 抓取调用上的卡计数：广告卡（平台推广位）、引导卡（关联搜索等非作品卡），
-- 以及既不是作品也没有明确卡标记的「读不到」。卡不是作品，不进 radar_works。
ALTER TABLE radar_capture_calls ADD COLUMN ad_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE radar_capture_calls ADD COLUMN guide_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE radar_capture_calls ADD COLUMN unreadable_count INTEGER NOT NULL DEFAULT 0;
