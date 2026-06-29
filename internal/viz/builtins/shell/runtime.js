/**
 * shelby-viz runtime
 *
 * After the initial server-rendered page loads, this script attaches Shadow
 * DOM to every <shelby-widget> and schedules live refreshes by polling the
 * /api/widget/:id endpoint (served by `shelby-viz serve`).
 *
 * Each widget's rendered HTML (style + markup) lands inside an isolated
 * Shadow DOM so widget CSS cannot bleed into the host page or other widgets.
 */

const ShelbyViz = (() => {
  function mount(el, html) {
    let root = el.shadowRoot;
    if (!root) root = el.attachShadow({ mode: "open" });
    root.innerHTML = html;
  }

  async function refresh(el) {
    const id = el.dataset.widgetId;
    try {
      const resp = await fetch(`/api/widget/${id}`);
      if (!resp.ok) return;
      const { html } = await resp.json();
      mount(el, html);
    } catch (err) {
      console.warn(`shelby-viz: widget "${id}" refresh failed`, err);
    }
  }

  function init({ refresh: defaultRefresh = 60 } = {}) {
    document.querySelectorAll("shelby-widget[data-widget-id]").forEach((el) => {
      const interval = parseInt(el.dataset.refresh || defaultRefresh, 10) * 1000;

      // Declarative Shadow DOM (set by the server) may already be attached;
      // if not (older browsers / build output without the <template>), fetch now.
      if (!el.shadowRoot) refresh(el);

      setInterval(() => refresh(el), interval);
    });
  }

  return { init };
})();
