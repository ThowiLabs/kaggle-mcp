"use strict";
(() => {
  const root = document.getElementById("app");
  if (!root) return;
  const el = id => document.getElementById(id);
  const put = (id, value) => { const node=el(id); if(node) node.textContent=String(value); };
  const pct = value => Number.isFinite(value) && value>=0 && value<=100;
  const percent = n => !pct(n) ? "N/D" : (n > 0 && n < 1 ? n.toFixed(2) : n.toFixed(1)) + "%";
  const bytes = n => {
    if (!Number.isFinite(n) || n < 0) return "N/D";
    const units=["B","KiB","MiB","GiB","TiB"];
    if(n<1024) return n.toFixed(0)+" B";
    let i=Math.min(4,Math.floor(Math.log(n)/Math.log(1024)));
    return (n/Math.pow(1024,i)).toFixed(i>1?2:1)+" "+units[i];
  };
  const reduced=window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  // Axolotl semicircle; style is calculated exclusively from measured percentages.
  function gauge(id, n) {
    const arc=el(id+"GaugeArc"), needle=el(id+"GaugeNeedle");
    if(!arc || !needle)return;
    const target=pct(n)?n:0;
    const previous=Number(arc.dataset.pct||"0");
    arc.dataset.pct=String(target);
    const start=performance.now(), duration=reduced?0:500;
    const frame=now=>{
      if(Number(arc.dataset.pct)!==target)return;
      const t=duration===0?1:Math.min(1,(now-start)/duration);
      const value=previous+(target-previous)*(1-Math.pow(1-t,3));
      arc.style.strokeDasharray="251.33";
      arc.style.strokeDashoffset=(251.33*(1-value/100)).toFixed(2);
      needle.setAttribute("transform","rotate("+(-90+value*1.8).toFixed(2)+" 105 105)");
      if(t<1)requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
    put(id+"GaugeValue",percent(n));
    put(id+"TopValue",percent(n));
  }
  function donut(n) {
    const ring=el("gpuRing"); if(!ring)return;
    const target=pct(n)?n:0, previous=Number(ring.dataset.pct||0);
    ring.dataset.pct=String(target);
    if(pct(n))ring.setAttribute("aria-valuenow",String(+n.toFixed(2)));
    else ring.removeAttribute("aria-valuenow");
    const started=performance.now(),duration=reduced?0:550;
    const frame=now=>{
      if(Number(ring.dataset.pct)!==target)return;
      const t=duration===0?1:Math.min(1,(now-started)/duration);
      const value=previous+(target-previous)*(1-(1-t)**3);
      ring.style.setProperty("--gpu-fill",value+"%");
      if(t<1)requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
  }
  const monitor=!!el("server-metrics-section");
  let selectedGPU=0,devices=[],topology="";
  function drawGPU(){
    const d=devices[selectedGPU];
    if(!d){
      put("gpuName","GPU NVIDIA no detectada");
      ["gpuPct","gpuTopValue","gpuTemp","gpuVram","gpuVramTotal"].forEach(id=>put(id,"N/D"));
      put("gpuCount",0);donut(null);
      return;
    }
    put("gpuName",d.name || "GPU "+d.index);
    put("gpuPct",percent(d.utilization_gpu_percent));
    put("gpuTopValue",percent(d.utilization_gpu_percent));
    put("gpuTemp",d.temperature_celsius==null?"N/D":d.temperature_celsius+" °C");
    put("gpuVram",d.memory_used_mib==null?"N/D":bytes(d.memory_used_mib*1024**2));
    put("gpuVramTotal",d.memory_total_mib==null?"N/D":bytes(d.memory_total_mib*1024**2));
    put("gpuCount",devices.length);
    donut(d.utilization_gpu_percent);
  }
  function update(metrics){
    const runtime=metrics.runtime||{},cpu=runtime.cpu||{},ram=runtime.memory||{},disk=metrics.storage||{},gpu=metrics.gpu||{};
    gauge("cpu",cpu.available?cpu.percent:null);
    put("cpuSource",cpu.source==="cgroup-v2"?"Contenedor":cpu.source==="host"?"Host compartido":"N/D");
    put("cpuCapacity",cpu.available?+cpu.capacity_cores.toFixed(2)+" CPU":"N/D");
    put("cpuCores",cpu.available?cpu.logical_cores:"N/D");
    gauge("ram",ram.available?ram.used_percent:null);
    put("ramUsed",ram.available?bytes(ram.used_bytes):"N/D");
    put("ramFree",ram.available?bytes(ram.free_bytes):"N/D");
    put("ramTotal",ram.available?bytes(ram.total_bytes):"N/D");
    put("ramSource",ram.source==="cgroup-v2"?"Contenedor":ram.source==="host"?"Host compartido":"N/D");
    if(disk.available&&pct(disk.used_percent)){
      put("diskTopValue",percent(disk.used_percent));
      put("diskUsedText",bytes(disk.used_bytes)+" / "+bytes(disk.total_bytes));
      put("diskUsed",bytes(disk.used_bytes));
      put("diskFree",bytes(disk.free_bytes));
      put("diskTotal",bytes(disk.total_bytes));
      put("diskStatus",disk.used_percent>=95?"Crítico":disk.used_percent>=85?"Atención":"Disponible");
      el("diskFill").style.width=disk.used_percent+"%";
      el("diskMeter").setAttribute("aria-valuenow",String(+disk.used_percent.toFixed(2)));
      put("diskNote","Filesystem de /kaggle/working; puede diferir de otras cuotas de Kaggle.");
    }else{
      ["diskTopValue","diskUsedText","diskUsed","diskFree","diskTotal"].forEach(id=>put(id,"N/D"));
      put("diskStatus","Sin datos");
      el("diskFill").style.width="0%";
      el("diskMeter").removeAttribute("aria-valuenow");
      put("diskNote",disk.error || "No se pudo leer el volumen Kaggle.");
    }
    devices=Array.isArray(gpu.gpus) && gpu.available?gpu.gpus:[];
    document.querySelectorAll(".server-card.gpu").forEach(card=>{card.hidden=devices.length===0;});
    document.querySelectorAll(".server-metrics").forEach(section=>section.classList.toggle("no-gpu",devices.length===0));
    const next=devices.map(d=>d.uuid||d.index).join("|");
    if(next!==topology) {
      topology=next;selectedGPU=0;
      const host=el("gpuDevices");host.replaceChildren();
      devices.forEach((device,i)=>{
        const btn=document.createElement("button");
        btn.type="button";btn.textContent="GPU "+device.index;
        btn.addEventListener("click",()=>{
          selectedGPU=i;
          host.querySelectorAll("button").forEach((b,j)=>b.classList.toggle("selected",j===i));
          drawGPU();
        });
        btn.classList.toggle("selected",i===0);
        host.appendChild(btn);
      });
    }
    if(selectedGPU>=devices.length) selectedGPU=0;
    drawGPU();
    put("gpuNote",devices.length?"Datos de nvidia-smi; una GPU inactiva puede mostrar 0%.":(gpu.hint||gpu.reason||"Sin GPU NVIDIA disponible."));
    const ok=cpu.available||ram.available||disk.available||gpu.available;
    put("serverStatus",ok?"Datos recibidos":"Sin telemetría");
    el("serverStatus")?.classList.toggle("is-live",ok);
    el("liveIndicator")?.classList.toggle("is-live",ok);
    document.querySelector(".sidebar-bottom")?.classList.toggle("is-live",ok);
    put("liveLabel",ok?"EN VIVO":"SIN DATOS");
    put("runtimeState",ok?"En línea":"Datos N/D");
    put("connectionStatus",ok?"Telemetría activa":"Sin mediciones");
    put("lastUpdated",metrics.time?"Actualizado "+new Date(metrics.time).toLocaleTimeString("es-MX"):"Sin hora disponible");
    put("monitorNote","Muestras reales · CPU "+(cpu.source||"N/D")+" · RAM "+(ram.source||"N/D")+(devices.length?" · GPU "+(gpu.backend||"N/D"):""));
  }
  let busy=false;
  async function refresh(){
    if(busy||document.hidden||!monitor)return;
    busy=true;
    try{
      const response=await fetch("/api/metrics",{cache:"no-store",credentials:"same-origin",headers:{Accept:"application/json"}});
      if(response.status===401||response.redirected){location.assign("/login");return}
      if(!response.ok)throw new Error("HTTP "+response.status);
      update(await response.json());
    }catch(error){
      put("serverStatus","Sin conexión");
      el("serverStatus")?.classList.remove("is-live");
      el("liveIndicator")?.classList.remove("is-live");
      document.querySelector(".sidebar-bottom")?.classList.remove("is-live");
      put("liveLabel","SIN CONEXIÓN");
      put("runtimeState","Sin conexión");
      put("connectionStatus","Telemetría desconectada");
      put("lastUpdated","Fallo de lectura");
      put("monitorNote","No hay una lectura reciente. Comprueba la conexión con KagMCP.");
      gauge("cpu",null);gauge("ram",null);
      ["diskTopValue","diskUsedText","diskUsed","diskFree","diskTotal","diskStatus",
        "ramUsed","ramFree","ramTotal","ramSource","cpuSource","cpuCapacity","cpuCores",
        "gpuPct","gpuTopValue","gpuName","gpuTemp","gpuVram","gpuVramTotal","gpuCount"
      ].forEach(id=>put(id,"N/D"));
      const fill=el("diskFill");if(fill)fill.style.width="0%";
      el("diskMeter")?.removeAttribute("aria-valuenow");
      devices=[];topology="";
      document.querySelectorAll(".server-card.gpu").forEach(card=>{card.hidden=true;});
      document.querySelectorAll(".server-metrics").forEach(section=>section.classList.add("no-gpu"));
      el("gpuDevices")?.replaceChildren();
      donut(null);
    }finally{busy=false}
  }
  // The Axolotl original uses <i> for icons; our nav uses <span>.
  // Persist the collapsed preference and keep mobile drawer state separate.
  const media = window.matchMedia("(max-width: 850px)");
  const sidebar = el("sidebar");
  const collapseBtn = el("collapseBtn");
  const menuBtn = el("menuBtn");
  const storageKey = "kagmcp.sidebar.collapsed";
  let collapsed = false;
  try { collapsed = window.localStorage.getItem(storageKey) === "1"; } catch (_e) { /* private mode */ }
  function setDrawer(open) {
    root.classList.toggle("mobile-open", media.matches && open);
    menuBtn?.setAttribute("aria-expanded", String(media.matches && open));
    if (sidebar) {
      sidebar.inert = media.matches && !open;
      sidebar.setAttribute("aria-hidden", String(media.matches && !open));
    }
  }
  function syncSidebar() {
    root.classList.toggle("sidebar-collapsed", !media.matches && collapsed);
    collapseBtn?.setAttribute("aria-expanded", String(!media.matches && !collapsed));
    collapseBtn?.setAttribute("aria-label", media.matches ? "Cerrar navegación" : collapsed ? "Expandir menú" : "Contraer menú");
    collapseBtn?.setAttribute("title", media.matches ? "Cerrar navegación" : collapsed ? "Expandir menú" : "Contraer menú");
    if (collapseBtn) collapseBtn.textContent = media.matches ? "×" : collapsed ? "»" : "«";
    setDrawer(false);
  }
  collapseBtn?.addEventListener("click", () => {
    if (media.matches) {
      setDrawer(false);
      menuBtn?.focus();
      return;
    }
    collapsed = !collapsed;
    try { window.localStorage.setItem(storageKey, collapsed ? "1" : "0"); } catch (_e) {}
    syncSidebar();
  });
  menuBtn?.addEventListener("click", () => setDrawer(true));
  el("mobileBackdrop")?.addEventListener("click", () => { setDrawer(false); menuBtn?.focus(); });
  document.addEventListener("keydown", event => {
    if (event.key === "Escape" && root.classList.contains("mobile-open")) {
      setDrawer(false);
      menuBtn?.focus();
    }
  });
  document.querySelectorAll(".sidebar .nav-item").forEach(link =>
    link.addEventListener("click", () => setDrawer(false)));
  media.addEventListener("change", syncSidebar);
  syncSidebar();
  el("copyAgentPrompt")?.addEventListener("click",async () => {
    const field=el("agentPrompt");
    if(!field)return;
    const button=el("copyAgentPrompt");
    try {
      await navigator.clipboard.writeText(field.value);
      button.textContent="Prompt copiado";
    } catch (_e) {
      field.select();
      button.textContent="Seleccionado · usa Ctrl+C";
    }
  });
  if (monitor) {
    refresh();
    setInterval(refresh,5000);
    document.addEventListener("visibilitychange",()=>{if(!document.hidden)refresh()});
  } else if (!el("activityEntries")) {
    put("liveLabel","PANEL ABIERTO");
    put("connectionStatus","Métricas disponibles en Recursos");
  }
})();
