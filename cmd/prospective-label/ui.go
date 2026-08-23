// SPDX-License-Identifier: AGPL-3.0-only

package main

// indexHTML is the whole interface. It is deliberately plain: the corpus is
// listed in one deterministic order and the only thing that reorders it is text
// the human typed. There is no scoring, no highlighting of "likely" items, and
// no ordering derived from the change being judged.
const indexHTML = `<!doctype html>
<meta charset="utf-8"><title>prospective-label</title>
<style>
:root{--bg:#111418;--fg:#e6e6e6;--dim:#8b949e;--line:#2a2f36;--acc:#7aa2f7;--ok:#9ece6a;--warn:#e0af68;--no:#f7768e}
*{box-sizing:border-box}
body{margin:0;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;background:var(--bg);color:var(--fg)}
header{padding:8px 12px;border-bottom:1px solid var(--line);display:flex;gap:16px;align-items:center;flex-wrap:wrap}
main{display:grid;grid-template-columns:230px 1fr;height:calc(100vh - 42px)}
#changes{overflow:auto;border-right:1px solid var(--line)}
#changes div{padding:6px 10px;cursor:pointer;border-bottom:1px solid var(--line);font-size:12px}
#changes div:hover{background:#171b21}#changes div.sel{background:#1d2530;border-left:3px solid var(--acc)}
#right{display:grid;grid-template-rows:auto auto 1fr;overflow:hidden}
#diff{max-height:26vh;overflow:auto;padding:8px 12px;border-bottom:1px solid var(--line);white-space:pre-wrap;font-size:12px;color:#c9d1d9}
#panel{padding:8px 12px;border-bottom:1px solid var(--line);display:flex;gap:14px;align-items:center;flex-wrap:wrap}
#list{overflow:auto;padding:0 12px}
.item{border-bottom:1px solid var(--line);padding:8px 0}
.item .id{color:var(--dim);font-size:11px}
.item .ti{color:var(--fg)}
.item .st{color:var(--dim);white-space:pre-wrap;font-size:12px;margin-top:2px}
button{background:#1b2027;color:var(--fg);border:1px solid var(--line);border-radius:3px;padding:3px 7px;cursor:pointer;font:inherit;font-size:11px}
button:hover{border-color:var(--acc)}button:disabled{opacity:.4;cursor:not-allowed}
.chip{padding:1px 6px;border-radius:3px;font-size:11px}
.applicable{background:#1f3a1f;color:var(--ok)}.not_applicable{background:#2a2f36;color:var(--dim)}
.ambiguous,.outside_scope,.cannot_adjudicate{background:#3a2f1f;color:var(--warn)}
.sweepmode{color:var(--dim);font-style:italic}
input[type=search]{background:#0d1117;color:var(--fg);border:1px solid var(--line);border-radius:3px;padding:4px 8px;font:inherit;width:260px}
.warn{color:var(--warn)}.ok{color:var(--ok)}.no{color:var(--no)}.dim{color:var(--dim)}
</style>
<header>
  <b>prospective-label</b>
  <span class=dim id=who></span>
  <span class=dim id=digests></span>
  <span style="flex:1"></span>
  <button id=export>freeze label set</button>
</header>
<main>
  <div id=changes></div>
  <div id=right>
    <div id=diff></div>
    <div id=panel></div>
    <div id=list></div>
  </div>
</main>
<script>
let D=null,cur=null,mode="traverse",page=0,q="",PAGE=50; // replaced by the corpus size in boot()
const g=id=>document.getElementById(id);

async function post(u,b){const r=await fetch(u,{method:"POST",body:JSON.stringify(b)});const j=await r.json();
  if(j.error){alert(j.error);return null}return j}

async function boot(){D=await (await fetch("/api/bootstrap")).json();
  // One full unfiltered traversal per change. "Presented" means the software
  // rendered the item, and one complete page proves the corpus was never
  // withheld exactly as well as eighteen partial ones do — the extra pages
  // only cost the adjudicator clicks that decide nothing.
  PAGE=D.corpus.length;
  g("who").textContent="adjudicator: "+D.adjudicator;
  g("digests").textContent="manifest "+D.manifest.slice(0,10)+"…  blind corpus "+D.blind_corpus.slice(0,10)+"…";
  D.labelMap={};for(const l of D.labels){D.labelMap[l.item_key+"|"+l.corpus_item_id]=l}
  cur=D.changes[0].item_key;renderChanges();renderChange()}

function cov(k){return D.state.coverage[k]}

function renderChanges(){const el=g("changes");el.innerHTML="";
  D.changes.forEach((c,i)=>{const c2=cov(c.item_key);const d=document.createElement("div");
    d.className=c.item_key===cur?"sel":"";
    const done=c2.adjudication_coverage_complete;
    d.innerHTML="<div>"+(i+1)+"/"+D.changes.length+" "+(done?"<span class=ok>complete</span>":"<span class=dim>"+c2.unlabelled+" unset</span>")+"</div>"+
      "<div class=dim style='font-size:11px'>"+c.item_key.slice(0,16)+"…</div>";
    d.onclick=()=>{cur=c.item_key;page=0;q="";mode="traverse";renderChanges();renderChange()};
    el.appendChild(d)})}

function renderChange(){const c=D.changes.find(x=>x.item_key===cur);
  g("diff").textContent=c.paths.join("\n")+"\n\n"+c.content.slice(0,20000);
  renderPanel();renderList()}

function renderPanel(){const c2=cov(cur);const notPresented=c2.eligible_items-c2.presented;
  g("panel").innerHTML=
   "<span>eligible <b>"+c2.eligible_items+"</b></span>"+
   "<span>presented <b class="+(notPresented?"warn":"ok")+">"+c2.presented+"</b></span>"+
   "<span>individually <b>"+c2.individually_assigned+"</b></span>"+
   "<span>bulk-swept <b>"+c2.bulk_swept_not_applicable+"</b></span>"+
   "<span>unresolved <b>"+c2.unresolved+"</b></span>"+
   "<span>unlabelled <b class="+(c2.unlabelled?"warn":"ok")+">"+c2.unlabelled+"</b></span>"+
   " <button id=modebtn>"+(mode==="traverse"?"searching off":"searching on")+"</button>"+
   " <input type=search id=q placeholder='search title/statement/class/id' value='"+q.replace(/'/g,"&#39;")+"'>"+
   " <button id=sweep "+(notPresented?"disabled":"")+">bulk-label remaining "+c2.unlabelled+" NOT_APPLICABLE</button>"+
   (notPresented?" <span class=warn>"+notPresented+" not yet presented — traverse the whole corpus to enable the sweep</span>":"");
  g("modebtn").onclick=()=>{mode=mode==="traverse"?"search":"traverse";page=0;renderPanel();renderList()};
  g("q").oninput=e=>{q=e.target.value;mode=q?"search":"traverse";page=0;renderList();};
  g("sweep").onclick=async()=>{
    if(!confirm("Affirmatively judge the remaining "+cov(cur).unlabelled+" items NOT_APPLICABLE for this change?\n\nThis is recorded as a bulk sweep, not as individual review."))return;
    const r=await post("/api/sweep",{item_key:cur});if(!r)return;
    D.state=r.state;for(const c of D.changes){}
    const fresh=await (await fetch("/api/bootstrap")).json();D.labels=fresh.labels;D.state=fresh.state;
    D.labelMap={};for(const l of D.labels){D.labelMap[l.item_key+"|"+l.corpus_item_id]=l}
    renderChanges();renderPanel();renderList()}}

function visible(){if(mode==="search"&&q){const s=q.toLowerCase();
    return D.corpus.filter(i=>(i.id+" "+i.class+" "+i.title+" "+(i.statement||"")).toLowerCase().includes(s))}
  return D.corpus}

async function renderList(){const items=visible();const el=g("list");
  if(mode==="search"){
    el.innerHTML="<div class='sweepmode' style='padding:8px 0'>searching — a filtered view does not count as presented, and the sweep stays gated on the full traversal</div>";
    items.slice(0,300).forEach(i=>el.appendChild(row(i)));
    if(items.length>300)el.appendChild(note(items.length-300+" more match; refine the search"));
    return}
  const start=page*PAGE,slice=items.slice(start,start+PAGE);
  el.innerHTML="";
  const nav=document.createElement("div");nav.style.padding="8px 0";
  nav.innerHTML="<button id=prev "+(page?"":"disabled")+">prev</button> "+
    "<span class=dim>items "+(start+1)+"–"+Math.min(start+PAGE,items.length)+" of "+items.length+"</span> "+
    "<button id=next "+(start+PAGE<items.length?"":"disabled")+">next</button>";
  el.appendChild(nav);
  slice.forEach(i=>el.appendChild(row(i)));
  g("prev").onclick=()=>{page--;renderList()};
  g("next").onclick=()=>{page++;renderList()};
  // Rendering an unfiltered page is what "presented" means.
  const r=await post("/api/present",{item_key:cur,ids:slice.map(i=>i.id)});
  if(r){D.state=r;renderPanel();renderChanges()}}

function note(t){const d=document.createElement("div");d.className="dim";d.style.padding="8px 0";d.textContent=t;return d}

function row(i){const d=document.createElement("div");d.className="item";
  const k=cur+"|"+i.id,l=D.labelMap[k];
  const chip=l?"<span class='chip "+l.label+"'>"+l.label+(l.assignment_mode==="bulk_sweep"?" (swept)":"")+"</span>":"<span class=dim>UNSET</span>";
  d.innerHTML="<div class=id>"+i.class+" · "+i.id+"  "+chip+"</div>"+
    "<div class=ti>"+esc(i.title)+"</div>"+(i.statement?"<div class=st>"+esc(i.statement)+"</div>":"")+
    "<div style='margin-top:4px'></div>";
  const bar=d.lastChild;
  ["applicable","not_applicable","ambiguous","outside_scope","cannot_adjudicate",""].forEach(lab=>{
    const b=document.createElement("button");b.textContent=lab||"unset";b.style.marginRight="4px";
    b.onclick=async()=>{const r=await post("/api/label",{item_key:cur,corpus_item_id:i.id,label:lab});
      if(!r)return;D.state=r;
      if(lab){D.labelMap[k]={item_key:cur,corpus_item_id:i.id,label:lab,assignment_mode:"individual"}}else{delete D.labelMap[k]}
      renderPanel();renderChanges();d.replaceWith(row(i))};
    bar.appendChild(b)});
  return d}

function esc(s){return (s||"").replace(/[&<>]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;"}[c]))}

g("export").onclick=async()=>{const r=await post("/api/export",{});if(!r)return;
  alert("frozen: "+r.written+"\ndigest: "+r.digest+"\n\n"+JSON.stringify(r.totals,null,1))};

boot();
</script>`
