-- Login-only account-access platforms visible in the EgoLite Shop directory.
-- qianchuan is an advertising backend; the other new entries are account
-- access targets only. No shop.listing adapter has been verified for them.
-- This migration only inserts platform rows and does not rebuild shop_listing.
INSERT OR IGNORE INTO platforms (
  code, name, icon, login_url, manager_url,
  supports_article, supports_graph_text, supports_video, sort, source
) VALUES
  ('qianchuan', '巨量千川', '千', 'https://qianchuan.jinritemai.com/', 'https://qianchuan.jinritemai.com/', 0, 0, 0, 30, 'system'),
  ('jd_shop', '京东店铺', '京', 'https://login.shop.jd.com/', 'https://seller-v10.shop.jd.com/', 0, 0, 0, 31, 'system'),
  ('pinduoduo_shop', '拼多多店铺', '拼', 'https://mms.pinduoduo.com/', 'https://mms.pinduoduo.com/', 0, 0, 0, 32, 'system'),
  ('xhs_shop', '小红书店铺', '红', 'https://ark.xiaohongshu.com/merchant/login?from=xhsweb', 'https://ark.xiaohongshu.com/ark', 0, 0, 0, 33, 'system'),
  ('kuaishou_shop', '快手小店', '快', 'https://login.kwaixiaodian.com/', 'https://s.kwaixiaodian.com/', 0, 0, 0, 34, 'system'),
  ('wechat_shop', '微信小店', '微', 'https://channels.weixin.qq.com/shop', 'https://channels.weixin.qq.com/shop', 0, 0, 0, 35, 'system'),
  ('alibaba_1688', '1688', '1688', 'https://work.1688.com/', 'https://work.1688.com/', 0, 0, 0, 36, 'system'),
  ('aliexpress', 'AliExpress', 'AE', 'https://gsp.aliexpress.com/', 'https://gsp.aliexpress.com/', 0, 0, 0, 37, 'system');
