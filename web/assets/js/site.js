/* ==========================================================================
   KOOYING Brand Site & BrakeFinder™ Cross-Reference Runtime
   Connects cleanly to local catalog index and :17422 AlphaGTM API
   ========================================================================== */

(function () {
  const BASE = (document.body && document.body.dataset && document.body.dataset.basePath) || ".";
  const GTM_API_BASE = "http://127.0.0.1:17422";

  function assetUrl(relPath) {
    if (!relPath) return "";
    const clean = relPath.replace(/^\/+/, "");
    // Encode each path segment safely so Chinese folder names and spaces load cleanly over HTTP
    const encoded = clean.split("/").map((seg) => encodeURIComponent(seg)).join("/");
    return `${BASE}/${encoded}`;
  }

  function escapeHtml(str) {
    return String(str || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function getCatalog() {
    if (window.KOOYING_CATALOG && Array.isArray(window.KOOYING_CATALOG.items)) {
      return window.KOOYING_CATALOG;
    }
    return null;
  }

  // Mobile Menu Drawer
  function initMobileMenu() {
    const toggle = document.getElementById("nav-toggle");
    const drawer = document.getElementById("mobile-drawer");
    if (!toggle || !drawer) return;
    toggle.addEventListener("click", () => {
      const open = drawer.classList.toggle("is-open");
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
    });
    drawer.querySelectorAll("a").forEach((link) => {
      link.addEventListener("click", () => {
        drawer.classList.remove("is-open");
        toggle.setAttribute("aria-expanded", "false");
      });
    });
  }

  // Hero 3-View Switcher (Friction Face / Backing Plate / Profile)
  function initHeroShowcase() {
    const img = document.getElementById("hero-showcase-img");
    const codeEl = document.getElementById("hero-showcase-code");
    const specEl = document.getElementById("hero-showcase-spec");
    const pills = document.querySelectorAll("[data-hero-view]");
    if (!img || !pills.length) return;

    const views = {
      friction: {
        src: "assets/products/disc/29087/29087-反.JPG",
        code: "WVA 29087 · GYD-29087 (Friction Surface)",
        spec: "247.6 × 109.7 × 30.0 mm · Knorr SB7 / SN7 · μ 0.35–0.38 @ 400°C",
      },
      backing: {
        src: "assets/products/disc/29087/29087-正.JPG",
        code: "WVA 29087 · Laser Batch & CCC Backing Plate",
        spec: "247.5 × 110 × 30 mm · GB5763-2018 / ECE R90 Traceable Stamping",
      },
      profile: {
        src: "assets/products/disc/29087/29087-侧.JPG",
        code: "WVA 29087 · Side Thickness & Slotting Profile",
        spec: "30.0 mm Total Thickness · Positive Molded Steel Backing",
      },
      drum: {
        src: "assets/products/drum/西德拖架BC39-1（BPW11.5T)/MOT_2521.jpg",
        code: "BPW 11.5T BC39-1 · Heavy Trailer Drum Lining",
        spec: "214 × 200 × 17.5/11.0 mm · 10 Holes · GY-HD400 Carbon-Mineral Formula",
      },
    };

    pills.forEach((btn) => {
      btn.addEventListener("click", () => {
        const key = btn.getAttribute("data-hero-view");
        const data = views[key];
        if (!data) return;
        pills.forEach((p) => p.classList.remove("is-active"));
        btn.classList.add("is-active");
        img.src = assetUrl(data.src);
        if (codeEl) codeEl.textContent = data.code;
        if (specEl) specEl.textContent = data.spec;
      });
    });
  }

  // Architecture Card 3-View Toggle & Five-Brand Switcher
  function initInlineViewSwitchers() {
    document.querySelectorAll("[data-card-view-group]").forEach((group) => {
      const targetId = group.getAttribute("data-card-view-group");
      const img = document.getElementById(targetId);
      if (!img) return;
      const buttons = group.querySelectorAll("button[data-src]");
      buttons.forEach((btn) => {
        btn.addEventListener("click", () => {
          buttons.forEach((b) => b.classList.remove("is-active"));
          btn.classList.add("is-active");
          img.src = assetUrl(btn.getAttribute("data-src"));
        });
      });
    });

    const brandImg = document.getElementById("five-brand-render-img");
    const brandBtns = document.querySelectorAll("[data-brand-render]");
    if (brandImg && brandBtns.length) {
      brandBtns.forEach((btn) => {
        btn.addEventListener("click", () => {
          brandBtns.forEach((b) => b.classList.remove("is-active"));
          btn.classList.add("is-active");
          brandImg.src = assetUrl(btn.getAttribute("data-brand-render"));
        });
      });
    }
  }

  // Product Matching Algorithm (Text + Market + Geometry Tolerance ±3mm)
  function filterCatalogItems(items, opts) {
    const q = (opts.query || "").trim().toLowerCase();
    const category = opts.category || "all";
    const market = opts.market || "all";
    const arc = parseFloat(opts.arc);
    const width = parseFloat(opts.width);
    const thickness = parseFloat(opts.thickness);
    const holes = parseInt(opts.holes, 10);
    const tol = Number.isFinite(parseFloat(opts.tolerance)) ? parseFloat(opts.tolerance) : 4.0;

    const scored = [];
    for (const item of items) {
      if (category !== "all" && item.category !== category) continue;
      if (market !== "all" && !(item.markets || []).includes(market)) continue;

      if (q) {
        const hay = `${item.wva} ${item.gys} ${item.folder} ${item.axle} ${item.vehicles} ${item.caliper} ${item.size}`.toLowerCase();
        const terms = q.split(/\s+/).filter(Boolean);
        if (!terms.every((t) => hay.includes(t))) continue;
      }

      let distance = 0;
      if (!Number.isNaN(arc) && arc > 0) {
        const diff = Math.abs((item.length_mm || 0) - arc);
        if (diff > tol) continue;
        distance += diff;
      }
      if (!Number.isNaN(width) && width > 0) {
        const diff = Math.abs((item.width_mm || 0) - width);
        if (diff > tol) continue;
        distance += diff;
      }
      if (!Number.isNaN(thickness) && thickness > 0) {
        const diff = Math.abs((item.thickness_mm || 0) - thickness);
        if (diff > tol) continue;
        distance += diff * 1.5;
      }
      if (!Number.isNaN(holes) && holes > 0) {
        if (item.holes !== holes) continue;
      }

      scored.push({ item, distance });
    }

    scored.sort((a, b) => a.distance - b.distance);
    return scored.map((s) => s.item);
  }

  function renderProductCards(container, list, maxCount) {
    if (!container) return;
    const slice = maxCount ? list.slice(0, maxCount) : list;
    if (!slice.length) {
      container.innerHTML = `<div class="finder-panel" style="grid-column:1/-1;text-align:center;">
        <p style="margin:0 0 8px;font-weight:600;">No exact SKU matched under current tolerance.</p>
        <p style="margin:0;color:var(--ink-muted);font-size:0.9rem;">Try widening geometric tolerance to ±5mm or submit your raw dimensions below for custom tooling confirmation.</p>
      </div>`;
      return;
    }

    container.innerHTML = slice.map((item) => {
      const frontSrc = assetUrl(item.views.front);
      const backSrc = assetUrl(item.views.back);
      const sideSrc = assetUrl(item.views.side);
      const viewLabel1 = item.category === "disc" ? "Friction" : "Outer Arc";
      const viewLabel2 = item.category === "disc" ? "Backing" : "Detail";
      const hasThird = sideSrc && sideSrc !== backSrc;

      return `<article class="product-card" data-product-id="${escapeHtml(item.id)}">
        <div class="product-card__head">
          <div>
            <div class="product-card__wva">${escapeHtml(item.wva)}</div>
            <div style="font-size:0.8rem;color:var(--ink-muted);margin-top:2px;">${escapeHtml(item.axle)}</div>
          </div>
          <span class="product-card__gys">${escapeHtml(item.gys)}</span>
        </div>
        <div class="studio-frame">
          <img src="${frontSrc}" alt="KOOYING ${escapeHtml(item.wva)} ${escapeHtml(item.category_label)} for ${escapeHtml(item.axle)}" loading="lazy">
        </div>
        <div class="view-pills" data-product-card-pills>
          <button type="button" class="view-pill view-pill--light is-active" data-img-src="${frontSrc}">${viewLabel1}</button>
          ${backSrc && backSrc !== frontSrc ? `<button type="button" class="view-pill view-pill--light" data-img-src="${backSrc}">${viewLabel2}</button>` : ""}
          ${hasThird ? `<button type="button" class="view-pill view-pill--light" data-img-src="${sideSrc}">Profile</button>` : ""}
        </div>
        <div class="product-card__meta">
          <div class="product-card__kv"><span>Dimensions (mm)</span><strong class="mono">${escapeHtml(item.size)}</strong></div>
          <div class="product-card__kv"><span>Holes / Formula</span><strong class="mono">${item.holes ? item.holes + " Holes" : "Disc Pad"} · ${escapeHtml((item.formula || "").split(" ")[0])}</strong></div>
          <div class="product-card__kv" style="grid-column:1/-1;"><span>Applications</span><strong>${escapeHtml(item.vehicles)}</strong></div>
        </div>
        <div class="product-card__foot">
          <button type="button" class="btn btn--outline-dark btn--sm" data-open-modal="${escapeHtml(item.id)}">Inspect Drawing & Specs</button>
          <button type="button" class="btn btn--primary btn--sm" data-quote-sku="${escapeHtml(item.wva)}">Quote</button>
        </div>
      </article>`;
    }).join("");

    // Wire per-card view switchers
    container.querySelectorAll(".product-card").forEach((card) => {
      const img = card.querySelector(".studio-frame img");
      const pills = card.querySelectorAll("[data-product-card-pills] button");
      pills.forEach((btn) => {
        btn.addEventListener("click", () => {
          pills.forEach((p) => p.classList.remove("is-active"));
          btn.classList.add("is-active");
          if (img) img.src = btn.getAttribute("data-img-src");
        });
      });

      const modalBtn = card.querySelector("[data-open-modal]");
      if (modalBtn) {
        modalBtn.addEventListener("click", () => openProductModal(modalBtn.getAttribute("data-open-modal")));
      }

      const quoteBtn = card.querySelector("[data-quote-sku]");
      if (quoteBtn) {
        quoteBtn.addEventListener("click", () => {
          const sku = quoteBtn.getAttribute("data-quote-sku");
          const rfqInput = document.getElementById("rfq-query");
          const rfqSection = document.getElementById("rfq-section");
          if (rfqInput) {
            rfqInput.value = sku;
            rfqInput.dispatchEvent(new Event("input"));
          }
          if (rfqSection) {
            rfqSection.scrollIntoView({ behavior: "smooth" });
          } else {
            window.location.href = `${BASE}/index.html#rfq-section`;
          }
        });
      }
    });
  }

  // Modal Inspector
  function openProductModal(productId) {
    const catalog = getCatalog();
    const modal = document.getElementById("product-modal");
    const body = document.getElementById("product-modal-body");
    if (!catalog || !modal || !body) return;

    const item = catalog.items.find((x) => x.id === productId);
    if (!item) return;

    const thumbs = (item.views.all || []).map((p, i) => `
      <button type="button" class="view-pill view-pill--light ${i === 0 ? "is-active" : ""}" data-modal-thumb="${assetUrl(p)}">View ${i + 1}</button>
    `).join("");

    body.innerHTML = `
      <div>
        <div class="studio-frame" style="margin-bottom:14px;">
          <img id="modal-main-img" src="${assetUrl(item.views.front)}" alt="KOOYING ${escapeHtml(item.wva)}">
        </div>
        <div class="view-pills">${thumbs}</div>
      </div>
      <div style="display:flex;flex-direction:column;gap:14px;">
        <div>
          <div class="eyebrow" style="margin-bottom:8px;">${escapeHtml(item.category_label)}</div>
          <h3 class="h3">${escapeHtml(item.wva)}</h3>
          <div class="mono" style="font-size:0.84rem;color:var(--accent);margin-top:4px;">KOOYING Internal Code: ${escapeHtml(item.gys)}</div>
        </div>
        <div class="spec-table-wrap">
          <table class="spec-table">
            <tbody>
              <tr><th>Dimensions</th><td class="mono">${escapeHtml(item.size)} mm</td></tr>
              <tr><th>Axle / Caliper</th><td>${escapeHtml(item.axle)} (${escapeHtml(item.caliper)})</td></tr>
              <tr><th>Drilling / Holes</th><td class="mono">${item.holes ? item.holes + " Rivet Holes" : "Steel Backing Plate (Disc)"}</td></tr>
              <tr><th>Applications</th><td>${escapeHtml(item.vehicles)}</td></tr>
              <tr><th>Recommended Formula</th><td class="mono">${escapeHtml(item.formula)}</td></tr>
              <tr><th>Friction Coefficient</th><td class="mono">${escapeHtml(item.friction_mu)} (200–400°C)</td></tr>
              <tr><th>Standards</th><td class="mono">${escapeHtml(item.certification)}</td></tr>
              <tr><th>Primary Markets</th><td>${escapeHtml((item.markets || []).join(" · "))}</td></tr>
            </tbody>
          </table>
        </div>
        <div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:auto;">
          <a href="${BASE}/index.html#rfq-section" class="btn btn--primary" id="modal-send-rfq">Request Factory Quote for ${escapeHtml(item.gys)}</a>
        </div>
      </div>
    `;

    body.querySelectorAll("[data-modal-thumb]").forEach((btn) => {
      btn.addEventListener("click", () => {
        body.querySelectorAll("[data-modal-thumb]").forEach((b) => b.classList.remove("is-active"));
        btn.classList.add("is-active");
        const mainImg = document.getElementById("modal-main-img");
        if (mainImg) mainImg.src = btn.getAttribute("data-modal-thumb");
      });
    });

    const rfqJump = document.getElementById("modal-send-rfq");
    if (rfqJump) {
      rfqJump.addEventListener("click", (ev) => {
        const rfqInput = document.getElementById("rfq-query");
        if (rfqInput) {
          ev.preventDefault();
          rfqInput.value = `${item.wva} / ${item.gys} (${item.size} mm)`;
          modal.classList.remove("is-open");
          document.getElementById("rfq-section")?.scrollIntoView({ behavior: "smooth" });
        }
      });
    }

    modal.classList.add("is-open");
  }

  function initModalClose() {
    const modal = document.getElementById("product-modal");
    const closeBtn = document.getElementById("product-modal-close");
    if (!modal) return;
    closeBtn?.addEventListener("click", () => modal.classList.remove("is-open"));
    modal.addEventListener("click", (e) => {
      if (e.target === modal) modal.classList.remove("is-open");
    });
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape") modal.classList.remove("is-open");
    });
  }

  // Initialize BrakeFinder Engine (both Homepage preview & Full Catalog page)
  function initBrakeFinder() {
    const catalog = getCatalog();
    const grid = document.getElementById("brakefinder-grid");
    if (!catalog || !grid) return;

    const maxItems = parseInt(grid.getAttribute("data-max-items") || "0", 10);
    const queryEl = document.getElementById("bf-query");
    const catEl = document.getElementById("bf-category");
    const marketEl = document.getElementById("bf-market");
    const arcEl = document.getElementById("bf-arc");
    const widthEl = document.getElementById("bf-width");
    const thickEl = document.getElementById("bf-thickness");
    const holesEl = document.getElementById("bf-holes");
    const countEl = document.getElementById("bf-count");
    const resetEl = document.getElementById("bf-reset");

    function update() {
      const results = filterCatalogItems(catalog.items, {
        query: queryEl?.value || "",
        category: catEl?.value || "all",
        market: marketEl?.value || "all",
        arc: arcEl?.value || "",
        width: widthEl?.value || "",
        thickness: thickEl?.value || "",
        holes: holesEl?.value || "",
        tolerance: 4.5,
      });

      // On homepage preview without search filter, showcase balanced mix of Flagship Disc & Drum SKUs
      const isDefaultHome =
        maxItems > 0 &&
        !(queryEl?.value || "").trim() &&
        (catEl?.value || "all") === "all" &&
        (marketEl?.value || "all") === "all" &&
        !(arcEl?.value || "") &&
        !(widthEl?.value || "") &&
        !(thickEl?.value || "") &&
        !(holesEl?.value || "");

      let displayList = results;
      if (isDefaultHome) {
        const flagshipFolders = ["29087", "29162", "BPW8201", "19932斯堪尼亚", "西德拖架BC39-1（BPW11.5T)", "美驰桥A"];
        const flagships = flagshipFolders
          .map((f) => catalog.items.find((x) => x.folder === f))
          .filter(Boolean);
        displayList = flagships.length ? flagships : results;
      }

      if (countEl) {
        countEl.textContent = `${results.length} matched SKUs across ${catalog.stats.total_models} verified factory drawings`;
      }
      renderProductCards(grid, displayList, maxItems);
    }

    // Apply URL query parameters (e.g. catalog.html?q=29087&market=Middle+East)
    const params = new URLSearchParams(window.location.search);
    if (params.get("q") && queryEl) queryEl.value = params.get("q");
    if (params.get("cat") && catEl) catEl.value = params.get("cat");
    if (params.get("market") && marketEl) marketEl.value = params.get("market");

    [queryEl, catEl, marketEl, arcEl, widthEl, thickEl, holesEl].forEach((el) => {
      if (!el) return;
      el.addEventListener("input", update);
      el.addEventListener("change", update);
    });

    resetEl?.addEventListener("click", () => {
      if (queryEl) queryEl.value = "";
      if (catEl) catEl.value = "all";
      if (marketEl) marketEl.value = "all";
      if (arcEl) arcEl.value = "";
      if (widthEl) widthEl.value = "";
      if (thickEl) thickEl.value = "";
      if (holesEl) holesEl.value = "";
      update();
    });

    update();
  }

  // Hero Quick Search Redirect
  function initHeroQuickSearch() {
    const form = document.getElementById("hero-quick-form");
    const input = document.getElementById("hero-quick-input");
    if (!form || !input) return;
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      const q = input.value.trim();
      const bfQuery = document.getElementById("bf-query");
      if (bfQuery) {
        bfQuery.value = q;
        bfQuery.dispatchEvent(new Event("input"));
        document.getElementById("brakefinder")?.scrollIntoView({ behavior: "smooth" });
      } else {
        window.location.href = `${BASE}/catalog.html?q=${encodeURIComponent(q)}`;
      }
    });
  }

  // Instant Spec Match & Direct Factory RFQ (Integrates with :17422 AlphaGTM API)
  function initRfqForm() {
    const form = document.getElementById("rfq-form");
    const resultBox = document.getElementById("rfq-result-body");
    const statusBadge = document.getElementById("rfq-sync-badge");
    if (!form || !resultBox) return;

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const catalog = getCatalog();
      if (!catalog) return;

      const company = (document.getElementById("rfq-company")?.value || "").trim() || "Overseas Fleet Buyer";
      const contact = (document.getElementById("rfq-contact")?.value || "").trim();
      const market = document.getElementById("rfq-market")?.value || "Middle East";
      const query = (document.getElementById("rfq-query")?.value || "").trim();
      const qty = parseInt(document.getElementById("rfq-qty")?.value || "500", 10) || 500;
      const notes = (document.getElementById("rfq-notes")?.value || "").trim();

      // Match up to 3 best items from query or dimension string
      let matches = filterCatalogItems(catalog.items, { query, market: "all" });
      if (!matches.length) {
        // Fallback parse dimensions like "247x110" or "214 200"
        const nums = query.match(/\d+(\.\d+)?/g) || [];
        if (nums.length >= 2) {
          matches = filterCatalogItems(catalog.items, {
            arc: nums[0],
            width: nums[1],
            thickness: nums[2] || "",
            tolerance: 12,
          });
        }
      }
      if (!matches.length) {
        matches = catalog.items.filter((x) => (x.markets || []).includes(market)).slice(0, 3);
      }
      const topMatches = matches.slice(0, 3);

      const formulaRec =
        market === "Middle East" || market === "South America"
          ? "GY-HD400 Carbon-Ceramic (400°C wear 0.040mm, high thermal fade stability)"
          : market === "Southeast Asia"
          ? "GY-CV360 / N91 Wet-Grip Eco Mineral-Carbon (anti-squeal <= 1.6Hz)"
          : "GY-HD400 Long-Haul Fleet Spec (FMVSS-121 / ECE R90 aligned)";

      const estFobRange = (item) => {
        const isDisc = item.category === "disc";
        if (qty >= 2000) return isDisc ? "USD 16.80 – 21.50 / set (FCL Tier)" : "USD 11.40 – 15.80 / axle set (FCL Tier)";
        if (qty >= 500) return isDisc ? "USD 18.50 – 24.00 / set (Pallet Tier)" : "USD 12.60 – 17.40 / axle set (Pallet Tier)";
        return isDisc ? "USD 21.00 – 27.50 / set (Trial MOQ)" : "USD 14.50 – 19.50 / axle set (Trial MOQ)";
      };

      resultBox.innerHTML = `
        <div style="font-family:var(--font-mono);font-size:0.8rem;color:var(--ink-light-muted);">
          MATCHED FOR <strong style="color:#fff;">${escapeHtml(company)}</strong> · ${escapeHtml(market)} · ${qty} Sets
        </div>
        <div class="rfq-match-list">
          ${topMatches
            .map(
              (m) => `
            <div class="rfq-match-item">
              <img class="rfq-match-item__thumb" src="${assetUrl(m.views.front)}" alt="${escapeHtml(m.wva)}">
              <div>
                <div class="mono" style="font-weight:600;font-size:0.9rem;color:#fff;">${escapeHtml(m.wva)} <span style="color:#FF8A65;">(${escapeHtml(m.gys)})</span></div>
                <div class="mono" style="font-size:0.75rem;color:var(--ink-light-muted);margin-top:2px;">${escapeHtml(m.size)} mm · ${escapeHtml(m.axle)}</div>
                <div class="mono" style="font-size:0.75rem;color:#fff;margin-top:4px;">Est. Reference FOB Ningbo: ${estFobRange(m)}</div>
              </div>
            </div>`
            )
            .join("")}
        </div>
        <div style="padding:14px;border-radius:var(--radius-sm);background:rgba(255,255,255,0.04);border:1px solid var(--line-dark);font-size:0.84rem;line-height:1.55;">
          <strong style="color:#fff;display:block;margin-bottom:4px;">Recommended Friction Compound:</strong>
          <span class="mono" style="color:#FF8A65;">${escapeHtml(formulaRec)}</span>
          <div style="color:var(--ink-light-muted);margin-top:6px;">
            Note: Indicative factory reference range based on catalog specs & order tier; final tooling/rivet confirmation issued within 15 min by Kooying Export Desk.
          </div>
        </div>
      `;

      // Attempt live dispatch to :17422 AlphaGTM backend (graceful fallback if static offline)
      if (statusBadge) {
        statusBadge.textContent = "Dispatching to AlphaGTM Radar...";
      }
      try {
        const leadPayload = {
          source: "kooying-brand-website",
          company,
          contact,
          market,
          query,
          quantity: qty,
          matched_codes: topMatches.map((m) => m.wva),
          notes,
        };
        const resp = await fetch(`${GTM_API_BASE}/api/guanying/leads`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(leadPayload),
        });
        if (resp.ok) {
          if (statusBadge) statusBadge.textContent = "Synced Live to AlphaGTM Radar (:17422)";
        } else {
          if (statusBadge) statusBadge.textContent = "Instant Match Ready (Static Mode · Copy Summary to Send)";
        }
      } catch (_err) {
        if (statusBadge) statusBadge.textContent = "Instant Match Ready (Offline / Static Mode)";
      }
    });
  }

  document.addEventListener("DOMContentLoaded", () => {
    initMobileMenu();
    initHeroShowcase();
    initInlineViewSwitchers();
    initModalClose();
    initBrakeFinder();
    initHeroQuickSearch();
    initRfqForm();
  });
})();
