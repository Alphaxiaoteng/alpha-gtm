# AlphaGTM 落地页部署到 gtm.alphafde.cn

`index.html` 是独立单文件：logo 与 Pinyon 花体字体已 base64 内联，无外部依赖。
部署只需把这一个文件放到目标站点的根，现有 canonical 已指向 `https://gtm.alphafde.cn/`。

## 方案 A：Cloudflare Pages 独立站点（推荐）

`alphafde.cn` 已在 Cloudflare Pages。给 `gtm.alphafde.cn` 建一个独立 Pages 项目：

1. Cloudflare Dashboard → Workers & Pages → Create → Pages → 连接本仓库或直接上传。
2. 构建设置：无框架，构建命令留空，输出目录填本目录（`show/alphagtm`）。
3. 部署完成后在项目的 Custom domains 添加 `gtm.alphafde.cn`。
4. DNS：在 alphafde.cn 的解析里加 CNAME：

```
gtm  CNAME  <你的pages项目>.pages.dev
```

5. 验证：`curl -sI https://gtm.alphafde.cn/ | head`，确认 200 与 `content-type: text/html`。

## 方案 B：作为 alphafde.cn 主站的一个路径

若不想多建项目，把文件复制到主站输出目录：

```bash
mkdir -p /Users/albert/Documents/project/Web/gtm
cp index.html /Users/albert/Documents/project/Web/gtm/index.html
```

然后在 `Web/_redirects` 顶部加一条（该域名需在 Pages 里绑定为自定义域，或走主域 `/gtm`）：

```
/ /gtm/index.html 200
```

> 注意：主站 `_redirects` 已有 `/ /alphafde/index.html 200`。同一条路径不能有两个 200 重写。
> 因此**独立 Pages 项目（方案 A）更干净**，不会影响主站。

## 部署前检查

```bash
# 单文件、无外部资源引用（logo/字体均内联）
grep -c 'src="http' index.html     # 应为 0
grep -c '@import\|href="http' index.html  # 应为 0（canonical 是 meta，不算）

# 本地起服务看一遍
~/.local/bin/preview-serve.sh show 8798
# http://127.0.0.1:8812/alphagtm/index.html
```

## 上线后核对

- https://gtm.alphafde.cn/ 打开，首屏三层叠加（红 Pinyon 花体 / 中文衬线 / 金 Pinyon 花体）正常。
- 手机视口（`window-size=390,844`）无横向滚动。
- canonical、title、description 与域名一致。
