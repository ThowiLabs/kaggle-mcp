"use strict";
(() => {
  const entries = document.getElementById("activityEntries");
  if (!entries) return;
  const pagination = document.getElementById("activityPagination");
  const status = document.getElementById("activityLiveStatus");
  const count = document.getElementById("activityTotal");
  const checked = document.getElementById("activityChecked");
  const pageInfo = document.getElementById("activityPageInfo");

  function pageFromUrl() {
    const value = new URLSearchParams(window.location.search).get("page");
    const number = Number(value || 1);
    return Number.isSafeInteger(number) && number > 0 ? number : 1;
  }
  let currentPage = pageFromUrl();
  let busy = false;
  let pending = false;
  let previousRows = null;
  const elt = (tag, cls, txt) => {
    const node = document.createElement(tag);
    if (cls) node.className = cls;
    if (txt !== undefined) node.textContent = String(txt);
    return node;
  };
  const dateLabel = value => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "Fecha no disponible";
    return new Intl.DateTimeFormat("es-MX", {
      dateStyle: "short", timeStyle: "medium", timeZoneName: "short"
    }).format(date);
  };
  function renderRows(items) {
    const serialized = JSON.stringify(items);
    if (serialized === previousRows) return;
    previousRows = serialized;
    const fragment = document.createDocumentFragment();
    if (!items.length) {
      fragment.append(elt("p", "muted activity-empty", "Todavía no hay eventos registrados."));
    } else {
      for (const event of items) {
        const row = elt("article", "history-entry");
        row.append(elt("span", "pill", event.Status || "Sin estado"));
        const details = elt("div", "history-details");
        details.append(elt("strong", "", event.Description || "Sin descripción"));
        const meta = elt("small", "", dateLabel(event.When) + " · " + (event.Project || "General"));
        details.append(meta);
        if (event.Command) details.append(elt("pre", "", event.Command));
        row.append(details);
        fragment.append(row);
      }
    }
    entries.replaceChildren(fragment);
  }
  function makeLink(text, page, selected, relation) {
    const a = elt("a", "activity-page" + (selected ? " current" : ""), text);
    a.href = "/activity?page=" + page;
    if (selected) a.setAttribute("aria-current", "page");
    if (relation) a.setAttribute("rel", relation);
    return a;
  }
  function renderPaging(payload) {
    const fragment = document.createDocumentFragment();
    if (payload.has_prev) fragment.append(makeLink("← Anterior", payload.prev_page, false, "prev"));
    let last = 0;
    for (const number of payload.page_numbers || []) {
      if (last && number > last + 1) fragment.append(elt("span", "activity-gap", "…"));
      fragment.append(makeLink(number, number, number === payload.page));
      last = number;
    }
    if (payload.has_next) fragment.append(makeLink("Siguiente →", payload.next_page, false, "next"));
    pagination.replaceChildren(fragment);
    pageInfo.textContent = "Página " + payload.page + " de " + payload.total_pages +
      " · Orden: más reciente primero";
  }
  async function refresh() {
    if (document.hidden) return;
    if (busy) { pending = true; return; }
    busy = true;
    const requestedPage = currentPage;
    try {
      const response = await fetch("/api/activity?page=" + requestedPage, {
        credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" }
      });
      if (response.redirected || response.status === 401) {
        window.location.assign("/login");
        return;
      }
      if (!response.ok) throw new Error("HTTP " + response.status);
      const data = await response.json();
      if (!Array.isArray(data.items) || !Number.isInteger(data.page) ||
          !Number.isInteger(data.total_pages) || requestedPage !== currentPage) return;
      renderRows(data.items);
      renderPaging(data);
      count.textContent = data.total + (data.total === 1 ? " registro" : " registros");
      checked.textContent = "Actualizado " + new Date(data.checked_at).toLocaleTimeString("es-MX");
      status.textContent = "En vivo · cada 5 s";
      status.classList.add("is-live");
      document.getElementById("liveIndicator")?.classList.add("is-live");
      document.querySelector(".sidebar-bottom")?.classList.add("is-live");
      const label = document.getElementById("liveLabel");
      if (label) label.textContent = "ACTIVIDAD EN VIVO";
      const connection = document.getElementById("connectionStatus");
      if (connection) connection.textContent = "Actividad sincronizada";
      currentPage = data.page;
      if (pageFromUrl() !== currentPage) {
        window.history.replaceState(null, "", "/activity?page=" + currentPage);
      }
    } catch (_error) {
      status.textContent = "Sin conexión · vuelve a intentar";
      status.classList.remove("is-live");
      document.getElementById("liveIndicator")?.classList.remove("is-live");
      document.querySelector(".sidebar-bottom")?.classList.remove("is-live");
      const label = document.getElementById("liveLabel");
      if (label) label.textContent = "SIN CONEXIÓN";
      const connection = document.getElementById("connectionStatus");
      if (connection) connection.textContent = "Actividad desconectada";
      checked.textContent = "Última consulta no disponible";
      // Keep the last valid entries, but never label them as fresh.
    } finally {
      busy = false;
      if (pending) { pending = false; refresh(); }
    }
  }
  pagination.addEventListener("click", event => {
    const link = event.target.closest("a[href]");
    if (!link || event.button !== 0 || event.metaKey || event.ctrlKey ||
        event.shiftKey || event.altKey) return;
    const destination = new URL(link.href);
    if (destination.origin !== window.location.origin || destination.pathname !== "/activity") return;
    event.preventDefault();
    const nextPage = Number(destination.searchParams.get("page"));
    if (!Number.isSafeInteger(nextPage) || nextPage < 1 || nextPage === currentPage) return;
    currentPage = nextPage;
    previousRows = null;
    window.history.pushState(null, "", destination.pathname + destination.search);
    refresh();
  });
  window.addEventListener("popstate", () => {
    currentPage = pageFromUrl();
    previousRows = null;
    refresh();
  });
  document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); });
  refresh();
  setInterval(refresh, 5000);
})();
