'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const html = fs.readFileSync('internal/dashboard/ui/page.html','utf8');
const script = fs.readFileSync('internal/dashboard/ui/dashboard.js','utf8');
const css = fs.readFileSync('internal/dashboard/ui/dashboard.css','utf8');
assert.equal((html.match(/class="server-card gpu" hidden/g)||[]).length,2,
  'Ambas tarjetas GPU deben iniciar ocultas');
assert.match(css,/\.server-card\[hidden\]\{display:none!important\}/);
const node=()=>({
  hidden:true, textContent:'', dataset:{}, style:{setProperty(){},width:''},
  classList:{toggle(){},add(){},remove(){},contains(){return false}},
  setAttribute(){},removeAttribute(){},addEventListener(){},replaceChildren(){},appendChild(){},
  querySelectorAll(){return []},focus(){}
});
async function check(gpus, shouldShow){
  const elements=new Map();
  const get=id=>{if(!elements.has(id))elements.set(id,node());return elements.get(id)};
  const cards=[node(),node()];const grids=[node(),node()];
  const document={hidden:false,getElementById:get,createElement:()=>node(),
    querySelectorAll(q){if(q==='.server-card.gpu')return cards;if(q==='.server-metrics')return grids;return []},
    querySelector(){return node()},addEventListener(){}
  };
  const media={matches:false,addEventListener(){}};
  const response={time:new Date().toISOString(),runtime:{cpu:{},memory:{}},storage:{},
    gpu:{available:gpus.length>0,gpus,backend:'nvidia-smi'}};
  vm.runInNewContext(script,{document,
    window:{matchMedia:()=>media,localStorage:{getItem:()=>null,setItem(){}}},
    location:{assign(){throw new Error('unexpected login redirect')}},
    fetch:async()=>({ok:true,status:200,redirected:false,json:async()=>response}),
    requestAnimationFrame(){},setInterval(){},performance:{now:()=>0},
    navigator:{clipboard:{writeText:async()=>{}}},console,Date,Math
  });
  await new Promise(resolve=>setImmediate(resolve));
  assert.ok(cards.every(card=>card.hidden===!shouldShow),
    shouldShow?'GPU real no mostrada':'GPU inexistente visible');
}
(async()=>{
 await check([],false);
 await check([{index:0,name:'GPU prueba',uuid:'test',utilization_gpu_percent:35}],true);
 console.log('GPU_VISIBILITY_OK: panel oculta GPU ausente y muestra GPU detectada');
})().catch(e=>{console.error(e);process.exitCode=1});
