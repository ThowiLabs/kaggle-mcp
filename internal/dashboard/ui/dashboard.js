"use strict";
(function () {
  const root = document.getElementById("app");
  if (!root) return;
  const $ = (id) => document.getElementById(id);
  const clamp = (v) => Math.max(0, Math.min(100, Number(v) || 0));
  const asText = (id, value) => { const el = $(id); if (el) el.textContent = value; };
  const fmtBytes = (n) => {
    if (!Number.isFinite(n)) return "—";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    if (n < 1024) return n.toFixed(0) + " B";
    const i = Math.min(4, Math.floor(Math.log(n) / Math.log(1024)));
    return (n / 1024 ** i).toFixed(i >= 2 ? 1 : 0) + " " + units[i];
  };
  // Only telemetry reported by the server is rendered; no placeholder fake readings.
  function ring(id, value) {
    const el = $(id);
    if (!el) return;
    const from = Number(el.dataset.value || 0);
    const to = clamp(value);
    el.dataset.value = String(to);
    el.setAttribute("aria-valuenow", String(Math.round(to)));
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      el.style.setProperty("--value", to + "%"); return;
    }
    const start = performance.now();
    const animate = (now) => {
      if (Number(el.dataset.value) !== to) return;
      const t = Math.min(1, (now - start) / 650);
      const eased = 1 - Math.pow(1 - t, 3);
      el.style.setProperty("--value", (from + (to - from) * eased) + "%");
      if (t < 1) requestAnimationFrame(animate);
    };
    requestAnimationFrame(animate);
  }
  let devices = [];
  let activeGpu = 0;
  function drawGPU() {
    const gpu = devices[activeGpu];
    if (!gpu) {
      ring("gpuRing", 0);
      asText("gpuPct", "—");
      asText("gpuVram", "—");
      asText("gpuVramTotal", "VRAM no disponible");
      asText("gpuTemp", "—");
      asText("gpuCount", "0");
      return;
    }
    const load = gpu.utilization_gpu_percent;
    if (load == null) { ring("gpuRing", 0); asText("gpuPct", "N/D"); }
    else { ring("gpuRing", load); asText("gpuPct", Math.round(load) + "%"); }
    asText("gpuName", gpu.name || ("GPU " + gpu.index));
    asText("gpuCount", String(devices.length));
    asText("gpuTemp", gpu.temperature_celsius == null ? "N/D" : gpu.temperature_celsius + " °C");
    asText("gpuVram", gpu.memory_used_mib == null ? "N/D" : (gpu.memory_used_mib / 1024).toFixed(2) + " GiB");
    asText("gpuVramTotal", gpu.memory_total_mib == null ? "VRAM total N/D" : "de " + (gpu.memory_total_mib / 1024).toFixed(2) + " GiB de VRAM");
    asText("gpuNote", gpu.memory_percent == null ? "Memoria GPU: N/D" : "Memoria GPU utilizada: " + gpu.memory_percent.toFixed(1) + "%");
  }
  function update(data) {
    const disk = data.storage || {};
    if (disk.available) {
      ring("diskRing", disk.used_percent);
      asText("diskPct", Math.round(disk.used_percent) + "%");
      asText("diskUsed", fmtBytes(disk.used_bytes));
      asText("diskTotal", "de " + fmtBytes(disk.total_bytes));
      asText("diskFree", fmtBytes(disk.free_bytes));
      asText("diskStatus", disk.used_percent > 95 ? "Crítico" : disk.used_percent > 85 ? "Atención" : "Disponible");
      asText("diskNote", disk.used_percent > 95 ? "Espacio crítico: evita instalar paquetes pesados." : "Información real del volumen del runtime.");
    } else {
      ring("diskRing", 0);
      asText("diskPct", "N/D");
      asText("diskStatus", "No disponible");
      asText("diskNote", disk.error || "No se pudo consultar este volumen.");
    }
    const report = data.gpu || {};
    devices = report.gpus || [];
    if (activeGpu >= devices.length) activeGpu = 0;
    const selector = $("gpuDevices");
    selector.replaceChildren();
    devices.forEach((device, i) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = "GPU " + device.index;
      btn.classList.toggle("selected", i === activeGpu);
      btn.addEventListener("click", () => {
        activeGpu = i;
        selector.querySelectorAll("button").forEach((b, n) => b.classList.toggle("selected", n === i));
        drawGPU();
      });
      selector.append(btn);
    });
    if (devices.length) {
      asText("gpuStatus", "Activa");
      asText("runtimeState", "En línea");
    } else {
      asText("gpuStatus", "Sin acelerador");
      asText("gpuName", "No hay GPU NVIDIA activa");
      asText("gpuNote", report.hint || report.reason || "Activa GPU desde Kaggle si la necesitas.");
      asText("runtimeState", disk.available ? "En línea" : "Sin métricas");
    }
    drawGPU();
    asText("connectionStatus", "Monitorización activa");
    asText("lastUpdated", "Actualizado " + new Date(data.time || Date.now()).toLocaleTimeString("es-MX"));
  }
  let busy = false;
  async function refresh() {
    if (busy || document.hidden) return;
    busy = true;
    try {
      const response = await fetch("/api/metrics", { credentials:"same-origin", cache:"no-store", headers:{"Accept":"application/json"} });
      if (!response.ok || !response.headers.get("content-type")?.includes("application/json")) throw new Error("Sin acceso a telemetría");
      update(await response.json());
    } catch (_err) {
      asText("connectionStatus", "Telemetría desconectada");
      asText("runtimeState", "Sin conexión");
      asText("lastUpdated", "Sin actualización");
      asText("gpuStatus", "Sin conexión");
      asText("diskStatus", "Sin conexión");
    } finally { busy = false; }
  }
  const collapse = $("collapseBtn");
  collapse?.addEventListener("click", () => root.classList.toggle("sidebar-collapsed"));
  $("menuBtn")?.addEventListener("click", () => root.classList.add("mobile-open"));
  $("mobileBackdrop")?.addEventListener("click", () => root.classList.remove("mobile-open"));
  document.querySelectorAll(".sidebar .nav-item").forEach(link => link.addEventListener("click", () => {
    root.classList.remove("mobile-open");
    document.querySelectorAll(".sidebar .nav-item").forEach(a => a.classList.toggle("active", a === link));
  }));
  refresh();
  setInterval(refresh, 5000);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); });
})();
