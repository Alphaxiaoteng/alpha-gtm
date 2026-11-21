-- Add the official Xianyu web console as a Shop login target.
-- Login management only: this does not enable automated listing or publish.
INSERT OR IGNORE INTO platforms (
  code, name, icon, login_url, manager_url,
  supports_article, supports_graph_text, supports_video, sort, source
) VALUES (
  'xianyu', '闲鱼', '闲',
  'https://www.goofish.com/personal',
  'https://www.goofish.com/personal',
  0, 0, 0, 38, 'system'
);
