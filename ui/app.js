const platforms = [
  { code: "instagram", name: "Instagram", total: 28, ok: 24, url: "https://business.facebook.com" },
  { code: "facebook", name: "Facebook Page", total: 24, ok: 22, url: "https://business.facebook.com" },
  { code: "xhs", name: "小红书", total: 31, ok: 20, url: "https://creator.xiaohongshu.com" },
  { code: "douyin", name: "抖音", total: 18, ok: 14, url: "https://creator.douyin.com" },
  { code: "wechat_video", name: "视频号", total: 11, ok: 8, url: "https://channels.weixin.qq.com" },
  { code: "kuaishou", name: "快手", total: 16, ok: 6, url: "https://cp.kuaishou.com" },
];

const storageKey = "alpha-nexus-ui-state";

const seedAccounts = [
  {
    id: 1,
    platform: "instagram",
    name: "Brand Main IG",
    profile: "meta-001",
    state: "logged_in",
    enabled: true,
    assets: 2,
    lastCheck: "2026-06-24 10:20",
    error: "-",
    loginUrl: "https://business.facebook.com",
    profileDir: "~/.alpha-nexus/profiles/meta/1",
  },
  {
    id: 2,
    platform: "facebook",
    name: "Brand Page Admin",
    profile: "meta-001",
    state: "logged_in",
    enabled: true,
    assets: 4,
    lastCheck: "2026-06-24 10:18",
    error: "-",
    loginUrl: "https://business.facebook.com",
    profileDir: "~/.alpha-nexus/profiles/meta/1",
  },
  {
    id: 3,
    platform: "xhs",
    name: "小红书华东号",
    profile: "xhs-012",
    state: "manual_required",
    enabled: true,
    assets: 1,
    lastCheck: "2026-06-24 09:41",
    error: "LOGIN_REQUIRED",
    loginUrl: "https://creator.xiaohongshu.com",
    profileDir: "~/.alpha-nexus/profiles/xhs/12",
  },
  {
    id: 4,
    platform: "douyin",
    name: "抖音矩阵 A",
    profile: "douyin-008",
    state: "checkpoint_required",
    enabled: true,
    assets: 1,
    lastCheck: "2026-06-24 08:55",
    error: "CHECKPOINT_REQUIRED",
    loginUrl: "https://creator.douyin.com",
    profileDir: "~/.alpha-nexus/profiles/douyin/8",
  },
  {
    id: 5,
    platform: "wechat_video",
    name: "视频号门店",
    profile: "wechat-video-003",
    state: "logged_in",
    enabled: true,
    assets: 1,
    lastCheck: "2026-06-23 21:12",
    error: "-",
    loginUrl: "https://channels.weixin.qq.com",
    profileDir: "~/.alpha-nexus/profiles/wechat_video/3",
  },
  {
    id: 6,
    platform: "kuaishou",
    name: "快手渠道",
    profile: "kuaishou-002",
    state: "manual_required",
    enabled: false,
    assets: 1,
    lastCheck: "2026-06-23 19:30",
    error: "LOGIN_REQUIRED",
    loginUrl: "https://cp.kuaishou.com",
    profileDir: "~/.alpha-nexus/profiles/kuaishou/2",
  },
];

const batches = [
  {
    id: 18,
    operation: "account-check",
    platform: "all",
    total: 128,
    success: 117,
    failed: 7,
    pending: 4,
    status: "running",
    created: "10:25",
  },
  {
    id: 17,
    operation: "publish",
    platform: "instagram",
    total: 30,
    success: 28,
    failed: 2,
    pending: 0,
    status: "failed",
    created: "09:12",
  },
  {
    id: 16,
    operation: "content-import",
    platform: "all",
    total: 64,
    success: 64,
    failed: 0,
    pending: 0,
    status: "success",
    created: "08:30",
  },
];

const contents = [
  { type: "reel", title: "Summer Drop Reel", platform: "instagram", target: "Brand Main IG" },
  { type: "photo", title: "Store Opening", platform: "facebook", target: "Brand Page" },
  { type: "video", title: "Batch Import 0624", platform: "instagram", target: "30 targets" },
];

const seedLogs = [
  {
    time: "10:32:18",
    code: "LOGIN_REQUIRED",
    message: "xhs-012 needs manual login handoff",
    path: "~/.alpha-nexus/screenshots/job-41-login.png",
  },
  {
    time: "10:26:02",
    code: "UPLOAD_TIMEOUT",
    message: "Instagram upload did not finish in 180s",
    path: "~/.alpha-nexus/screenshots/job-38-upload.png",
  },
  {
    time: "09:58:14",
    code: "PROFILE_LOCKED",
    message: "meta-001 is already used by batch 18",
    path: "-",
  },
];

let accounts = seedAccounts;
let logs = seedLogs;
const selectedAccountIds = new Set();

const loginUrls = {
  instagram: "https://business.facebook.com",
  facebook: "https://business.facebook.com",
  xhs: "https://creator.xiaohongshu.com",
  douyin: "https://creator.douyin.com",
  wechat_video: "https://channels.weixin.qq.com",
  kuaishou: "https://cp.kuaishou.com",
};

function readState() {
  try {
    const raw = localStorage.getItem(storageKey);
    if (!raw) return;
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed.accounts)) accounts = parsed.accounts;
    if (Array.isArray(parsed.logs)) logs = parsed.logs;
  } catch {
    localStorage.removeItem(storageKey);
  }
}

function writeState() {
  localStorage.setItem(storageKey, JSON.stringify({ accounts, logs }));
}

function nowStamp() {
  return new Date().toISOString().slice(0, 19).replace("T", " ");
}

function pushLog(code, message, path = "-", level = "warning") {
  logs = [
    {
      time: new Date().toTimeString().slice(0, 8),
      code,
      message,
      path,
      level,
    },
    ...logs,
  ].slice(0, 80);
  writeState();
  renderLogs();
  renderMiniFeed();
}

const routeMeta = {
  dashboard: ["Overview", "Dashboard"],
  accounts: ["Account Ops", "Accounts"],
  batches: ["Batch Runner", "Batches"],
  content: ["Library", "Content"],
  logs: ["Observability", "Logs"],
  settings: ["Runtime", "Settings"],
};

function platformName(code) {
  return platforms.find((p) => p.code === code)?.name || code;
}

function setRoute(route) {
  document.querySelectorAll(".nav-item").forEach((item) => {
    item.classList.toggle("active", item.dataset.route === route);
  });
  document.querySelectorAll(".view").forEach((view) => {
    view.classList.toggle("active-view", view.id === route);
  });
  document.getElementById("routeEyebrow").textContent = routeMeta[route][0];
  document.getElementById("routeTitle").textContent = routeMeta[route][1];
}

function renderPlatformStatus() {
  const root = document.getElementById("platformStatus");
  root.innerHTML = platforms
    .map((p) => {
      const percent = Math.round((p.ok / p.total) * 100);
      return `
        <div class="platform-row">
          <strong>${p.name}</strong>
          <div class="progress" aria-label="${p.name} logged in ${percent}%">
            <span style="width:${percent}%"></span>
          </div>
          <span class="tag">${p.ok}/${p.total}</span>
        </div>
      `;
    })
    .join("");
}

function renderEntrypoints() {
  document.getElementById("entrypointList").innerHTML = platforms
    .map(
      (platform) => `
        <div class="entrypoint-item">
          <div>
            <strong>${platform.name}</strong>
            <p>${platform.url}</p>
          </div>
          <button class="ghost-btn" data-entry-platform="${platform.code}">Add</button>
        </div>
      `,
    )
    .join("");
}

function renderHandoffQueue() {
  const queue = accounts.filter((account) => account.state !== "logged_in" || account.error !== "-");
  document.getElementById("handoffQueue").innerHTML =
    queue.length === 0
      ? '<p class="muted-empty">No accounts need manual action.</p>'
      : queue
          .map(
            (account) => `
              <div class="handoff-item">
                <div>
                  <strong>${account.name}</strong>
                  <p>${platformName(account.platform)} / ${account.state} / ${account.error}</p>
                </div>
                <button class="ghost-btn" data-detail="${account.id}">Resolve</button>
              </div>
            `,
          )
          .join("");
}

function renderAccounts() {
  const platformFilter = document.getElementById("platformFilter").value;
  const stateFilter = document.getElementById("stateFilter").value;
  const keyword = document.getElementById("accountSearch").value.trim().toLowerCase();
  const rows = accounts.filter((account) => {
    const platformOk = platformFilter === "all" || account.platform === platformFilter;
    const stateOk = stateFilter === "all" || account.state === stateFilter;
    const haystack = `${account.platform} ${account.name} ${account.profile}`.toLowerCase();
    return platformOk && stateOk && haystack.includes(keyword);
  });

  document.getElementById("accountsTable").innerHTML = rows
    .map(
      (account) => `
      <tr>
        <td><input class="account-select" type="checkbox" data-select="${account.id}" ${selectedAccountIds.has(account.id) ? "checked" : ""} /></td>
        <td>${platformName(account.platform)}</td>
        <td>
          <div class="account-cell">
            <span class="avatar">${account.name.slice(0, 1).toUpperCase()}</span>
            <strong>${account.name}</strong>
          </div>
        </td>
        <td><code>${account.profile}</code></td>
        <td><span class="state-${account.state}">${account.state}</span></td>
        <td>${account.enabled ? "enabled" : "disabled"}</td>
        <td>${account.assets}</td>
        <td>${account.lastCheck}</td>
        <td>
          <button class="ghost-btn" data-detail="${account.id}">Detail</button>
          <button class="ghost-btn" data-action="check" data-id="${account.id}">Check</button>
          <button class="ghost-btn" data-action="profile" data-id="${account.id}">Open Profile</button>
          <button class="ghost-btn" data-action="toggle" data-id="${account.id}">${account.enabled ? "Disable" : "Enable"}</button>
        </td>
      </tr>
    `,
    )
    .join("");
  renderAccountOps(rows);
  renderBulkBar();
  renderHandoffQueue();
}

function renderAccountOps(rows = accounts) {
  const total = rows.length;
  const enabled = rows.filter((item) => item.enabled).length;
  const loginNeeded = rows.filter((item) => item.state !== "logged_in").length;
  const assets = rows.reduce((sum, item) => sum + item.assets, 0);
  document.getElementById("accountOpsGrid").innerHTML = `
    <div class="ops-card"><div><strong>${total}</strong><span>filtered accounts</span></div><button class="ghost-btn" id="opsAdd">Add</button></div>
    <div class="ops-card"><div><strong>${enabled}</strong><span>enabled profiles</span></div><button class="ghost-btn" id="opsCheck">Check</button></div>
    <div class="ops-card"><div><strong>${loginNeeded}</strong><span>need handoff</span></div><button class="ghost-btn" id="opsLogin">Resolve</button></div>
    <div class="ops-card"><div><strong>${assets}</strong><span>bound assets</span></div><button class="ghost-btn" id="opsAssets">Discover</button></div>
  `;
  document.getElementById("opsAdd").addEventListener("click", openModal);
  document.getElementById("opsCheck").addEventListener("click", mockBatchCheck);
  document.getElementById("opsLogin").addEventListener("click", () => {
    const item = accounts.find((account) => account.state !== "logged_in");
    if (item) openDetail(item.id);
  });
  document.getElementById("opsAssets").addEventListener("click", () => setRoute("batches"));
}

function renderBulkBar() {
  const count = selectedAccountIds.size;
  document.getElementById("selectedCount").textContent = String(count);
  document.getElementById("bulkBar").classList.toggle("open", count > 0);
  const visibleIds = [...document.querySelectorAll("[data-select]")].map((item) => Number(item.dataset.select));
  const allVisibleSelected = visibleIds.length > 0 && visibleIds.every((id) => selectedAccountIds.has(id));
  document.getElementById("selectAllAccounts").checked = allVisibleSelected;
}

function renderBatches() {
  document.getElementById("batchesTable").innerHTML = batches
    .map((batch) => {
      const done = batch.success + batch.failed;
      const percent = Math.round((done / batch.total) * 100);
      return `
        <tr>
          <td>#${batch.id}</td>
          <td>${batch.operation}</td>
          <td>${batch.platform}</td>
          <td>
            <div class="progress"><span style="width:${percent}%"></span></div>
            <small>${batch.success} success / ${batch.failed} failed / ${batch.pending} pending</small>
          </td>
          <td><span class="status-pill">${batch.status}</span></td>
          <td>${batch.created}</td>
          <td>
            <button class="ghost-btn">Detail</button>
            <button class="ghost-btn">Resume</button>
          </td>
        </tr>
      `;
    })
    .join("");
}

function renderContent() {
  document.getElementById("contentGrid").innerHTML = contents
    .map(
      (item) => `
      <article class="content-card">
        <span class="tag">${item.type}</span>
        <h2>${item.title}</h2>
        <p>${platformName(item.platform)} / ${item.target}</p>
        <code>alpha-nexus publish --platform ${item.platform} --content ${item.title.replaceAll(" ", "-").toLowerCase()}</code>
      </article>
    `,
    )
    .join("");
}

function renderLogs() {
  document.getElementById("logList").innerHTML = logs
    .map(
      (log) => `
      <div class="log-item ${log.level || ""}">
        <span>${log.time}</span>
        <strong class="${log.level === "success" ? "" : "danger"}">${log.code}</strong>
        <span>${log.message}</span>
        <button class="ghost-btn">Open</button>
      </div>
      <code>${log.path}</code>
    `,
    )
    .join("");
}

function renderMiniFeed() {
  document.getElementById("miniFeed").innerHTML = logs
    .slice(0, 3)
    .map((log) => `<span>${log.code}: ${log.message}</span>`)
    .join("");
}

function openDetail(accountId) {
  const account = accounts.find((item) => item.id === Number(accountId));
  if (!account) return;
  document.getElementById("detailName").textContent = account.name;
  document.getElementById("detailGrid").innerHTML = `
    <div class="detail-card"><span>Platform</span><strong>${platformName(account.platform)}</strong></div>
    <div class="detail-card"><span>Profile</span><strong>${account.profile}</strong></div>
    <div class="detail-card"><span>Profile Dir</span><strong>${account.profileDir || "-"}</strong></div>
    <div class="detail-card"><span>Login URL</span><strong>${account.loginUrl || loginUrls[account.platform] || "-"}</strong></div>
    <div class="detail-card"><span>Login State</span><strong class="state-${account.state}">${account.state}</strong></div>
    <div class="detail-card"><span>Enabled</span><strong>${account.enabled ? "enabled" : "disabled"}</strong></div>
    <div class="detail-card"><span>Assets</span><strong>${account.assets}</strong></div>
    <div class="detail-card"><span>Last Check</span><strong>${account.lastCheck}</strong></div>
    <div class="detail-card"><span>Last Error</span><strong>${account.error}</strong></div>
    <div class="detail-card"><span>CLI</span><strong>account check --platform ${account.platform} --profile ${account.profile}</strong></div>
  `;
  document.getElementById("detailCommands").innerHTML = `
    <code>alpha-nexus account check --platform ${account.platform} --profile ${account.profile}</code>
    <code>alpha-nexus-egolite browser start --profile ${account.profile} --port 15101</code>
    <code>alpha-nexus account login-handoff --platform ${account.platform} --profile ${account.profile}</code>
  `;
  document.getElementById("detailActions").innerHTML = `
    <button class="secondary-btn" data-action="check" data-id="${account.id}">Check Login</button>
    <button class="secondary-btn" data-action="profile" data-id="${account.id}">Open Profile</button>
    <button class="secondary-btn" data-action="discover" data-id="${account.id}">Discover Assets</button>
    <button class="ghost-btn" data-action="toggle" data-id="${account.id}">${account.enabled ? "Disable" : "Enable"}</button>
  `;
  document.getElementById("accountDetail").classList.add("open");
}

function touchAccount(account, next = {}) {
  Object.assign(account, next, {
    lastCheck: nowStamp().slice(0, 16),
  });
  writeState();
  renderAccounts();
  openDetail(account.id);
}

function handleAccountAction(action, accountId) {
  const account = accounts.find((item) => item.id === Number(accountId));
  if (!account) return;
  if (action === "check") {
    const state = account.enabled ? "logged_in" : "manual_required";
    touchAccount(account, { state, error: state === "logged_in" ? "-" : "ACCOUNT_DISABLED" });
    pushLog(
      state === "logged_in" ? "ACCOUNT_CHECK_OK" : "ACCOUNT_DISABLED",
      `${account.name} login check -> ${state}`,
      "-",
      state === "logged_in" ? "success" : "warning",
    );
  }
  if (action === "toggle") {
    const nextEnabled = !account.enabled;
    touchAccount(account, {
      enabled: nextEnabled,
      state: nextEnabled ? "manual_required" : "manual_required",
      error: nextEnabled ? "MANUAL_LOGIN_REQUIRED" : "ACCOUNT_DISABLED",
    });
    pushLog(nextEnabled ? "ACCOUNT_ENABLED" : "ACCOUNT_DISABLED", `${account.name} ${nextEnabled ? "enabled" : "disabled"}`);
  }
  if (action === "profile") {
    touchAccount(account, { error: "PROFILE_OPEN_REQUESTED" });
    pushLog("PROFILE_OPEN_REQUESTED", `Open profile requested for ${account.profile}`);
  }
  if (action === "discover") {
    touchAccount(account, { assets: account.assets + 1, error: "-" });
    pushLog("ASSET_DISCOVERED", `${account.name} asset discovery completed`, "-", "success");
  }
}

function mockBatchCheck() {
  accounts = accounts.map((account) => ({
    ...account,
    state: account.enabled ? "logged_in" : "manual_required",
    error: account.enabled ? "-" : "ACCOUNT_DISABLED",
    lastCheck: nowStamp().slice(0, 16),
  }));
  writeState();
  renderAccounts();
  pushLog("BATCH_ACCOUNT_CHECK", "All account login states refreshed", "-", "success");
}

function updateAddCommand() {
  const platform = document.getElementById("addPlatform").value;
  const profile = document.getElementById("addProfile").value || "profile-001";
  document.getElementById("addCommand").textContent =
    `alpha-nexus account add --platform ${platform} --profile ${profile}`;
  document.getElementById("loginUrlPreview").textContent = loginUrls[platform] || "-";
}

function saveMockAccount() {
  const platform = document.getElementById("addPlatform").value;
  const name = document.getElementById("addName").value || `${platformName(platform)} New Account`;
  const profile = document.getElementById("addProfile").value || `${platform}-${accounts.length + 1}`;
  accounts = [
    {
      id: Date.now(),
      platform,
      name,
      profile,
      state: "manual_required",
      enabled: true,
      assets: 0,
      lastCheck: "pending",
      error: "MANUAL_LOGIN_REQUIRED",
    },
    ...accounts,
  ];
  writeState();
  renderAccounts();
  pushLog("ACCOUNT_CREATED", `${name} saved; manual login required`, "-", "success");
  closeModal();
}

function openModal() {
  document.getElementById("addAccountModal").classList.add("open");
  document.getElementById("addAccountModal").setAttribute("aria-hidden", "false");
  document.getElementById("handoffState").textContent = "waiting_for_launch";
  updateAddCommand();
}

function closeModal() {
  document.getElementById("addAccountModal").classList.remove("open");
  document.getElementById("addAccountModal").setAttribute("aria-hidden", "true");
}

function selectedAccounts() {
  return accounts.filter((account) => selectedAccountIds.has(account.id));
}

function applyBulk(action) {
  const selected = selectedAccounts();
  if (selected.length === 0) return;
  if (action === "check") {
    accounts = accounts.map((account) =>
      selectedAccountIds.has(account.id)
        ? {
            ...account,
            state: account.enabled ? "logged_in" : "manual_required",
            error: account.enabled ? "-" : "ACCOUNT_DISABLED",
            lastCheck: nowStamp().slice(0, 16),
          }
        : account,
    );
    pushLog("BULK_ACCOUNT_CHECK", `${selected.length} selected accounts checked`, "-", "success");
  }
  if (action === "enable" || action === "disable") {
    const enabled = action === "enable";
    accounts = accounts.map((account) =>
      selectedAccountIds.has(account.id)
        ? {
            ...account,
            enabled,
            state: enabled ? "manual_required" : "manual_required",
            error: enabled ? "MANUAL_LOGIN_REQUIRED" : "ACCOUNT_DISABLED",
            lastCheck: nowStamp().slice(0, 16),
          }
        : account,
    );
    pushLog(enabled ? "BULK_ACCOUNT_ENABLED" : "BULK_ACCOUNT_DISABLED", `${selected.length} selected accounts updated`);
  }
  if (action === "handoff") {
    const first = selected[0];
    pushLog("BULK_LOGIN_HANDOFF", `${selected.length} selected accounts queued for manual login handoff`);
    if (first) openDetail(first.id);
  }
  writeState();
  renderAccounts();
}

document.querySelectorAll(".nav-item").forEach((item) => {
  item.addEventListener("click", () => setRoute(item.dataset.route));
});

document.querySelectorAll("[data-route-jump]").forEach((item) => {
  item.addEventListener("click", () => setRoute(item.dataset.routeJump));
});

["accountSearch", "platformFilter", "stateFilter"].forEach((id) => {
  document.getElementById(id).addEventListener("input", renderAccounts);
});

document.getElementById("accountsTable").addEventListener("click", (event) => {
  const checkbox = event.target.closest("[data-select]");
  if (checkbox) {
    const id = Number(checkbox.dataset.select);
    if (checkbox.checked) selectedAccountIds.add(id);
    else selectedAccountIds.delete(id);
    renderBulkBar();
    return;
  }
  const button = event.target.closest("[data-detail]");
  if (button) openDetail(button.dataset.detail);
  const actionButton = event.target.closest("[data-action]");
  if (actionButton) handleAccountAction(actionButton.dataset.action, actionButton.dataset.id);
});

document.getElementById("detailActions").addEventListener("click", (event) => {
  const actionButton = event.target.closest("[data-action]");
  if (actionButton) handleAccountAction(actionButton.dataset.action, actionButton.dataset.id);
});

document.getElementById("closeDetail").addEventListener("click", () => {
  document.getElementById("accountDetail").classList.remove("open");
});

document.getElementById("selectAllAccounts").addEventListener("change", (event) => {
  document.querySelectorAll("[data-select]").forEach((item) => {
    const id = Number(item.dataset.select);
    if (event.target.checked) selectedAccountIds.add(id);
    else selectedAccountIds.delete(id);
  });
  renderAccounts();
});

document.getElementById("bulkCheckBtn").addEventListener("click", () => applyBulk("check"));
document.getElementById("bulkEnableBtn").addEventListener("click", () => applyBulk("enable"));
document.getElementById("bulkDisableBtn").addEventListener("click", () => applyBulk("disable"));
document.getElementById("bulkHandoffBtn").addEventListener("click", () => applyBulk("handoff"));

document.getElementById("openAddAccount").addEventListener("click", openModal);
document.getElementById("closeAddAccount").addEventListener("click", closeModal);
document.getElementById("saveAccountBtn").addEventListener("click", saveMockAccount);
document.getElementById("launchBrowserBtn").addEventListener("click", () => {
  updateAddCommand();
  document.getElementById("handoffState").textContent = "browser_launch_requested";
  pushLog("LOGIN_HANDOFF_STARTED", "Browser profile launch requested");
});
document.getElementById("verifyLoginBtn").addEventListener("click", () => {
  updateAddCommand();
  document.getElementById("handoffState").textContent = "checking_login";
  pushLog("LOGIN_VERIFY_REQUESTED", "Manual login verification requested");
});
document.getElementById("addPlatform").addEventListener("change", updateAddCommand);
document.getElementById("addProfile").addEventListener("input", updateAddCommand);
document.getElementById("batchCheckBtn").addEventListener("click", () => setRoute("batches"));
document.getElementById("clearLogsBtn").addEventListener("click", () => {
  logs = [];
  writeState();
  renderLogs();
  renderMiniFeed();
});

readState();
renderPlatformStatus();
renderAccounts();
renderBatches();
renderContent();
renderLogs();
renderMiniFeed();
