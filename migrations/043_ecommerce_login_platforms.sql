-- Login-only e-commerce consoles managed through an EgoLite identity Space.
-- Publishing and metrics capabilities remain disabled until separately
-- reviewed platform contracts are added.
INSERT INTO platforms (
  code, name, icon, login_url, manager_url,
  supports_article, supports_graph_text, supports_video, sort, source
) VALUES
  ('douyin_shop', '抖音电商', '抖店', 'https://fxg.jinritemai.com/', 'https://fxg.jinritemai.com/', 0, 0, 0, 24, 'system'),
  ('taobao', '淘宝', '淘宝', 'https://myseller.taobao.com/home.htm', 'https://myseller.taobao.com/home.htm', 0, 0, 0, 25, 'system'),
  ('tiktok_shop', 'TikTok Shop', 'TTS', 'https://seller.tiktokglobalshop.com/account/login', 'https://seller.tiktokglobalshop.com/', 0, 0, 0, 26, 'system'),
  ('shopee', 'Shopee', 'S', 'https://seller.shopee.cn/', 'https://seller.shopee.cn/', 0, 0, 0, 27, 'system'),
  ('shopify', 'Shopify', 'S', 'https://www.shopify.com/login', 'https://admin.shopify.com/', 0, 0, 0, 28, 'system'),
  ('amazon_seller', 'Amazon Seller', 'a', 'https://sellercentral.amazon.com/', 'https://sellercentral.amazon.com/', 0, 0, 0, 29, 'system')
ON CONFLICT(code) DO UPDATE SET
  name=excluded.name,
  icon=excluded.icon,
  login_url=excluded.login_url,
  manager_url=excluded.manager_url,
  supports_article=excluded.supports_article,
  supports_graph_text=excluded.supports_graph_text,
  supports_video=excluded.supports_video,
  sort=excluded.sort,
  source=excluded.source,
  updated_at=CURRENT_TIMESTAMP;
