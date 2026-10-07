"use strict";
/* Mak1zu panel. Plain JS, no build step, no network beyond its own API.
   Every setting applies the moment you change it; the feed shows it land. */
const $=(s,r=document)=>r.querySelector(s);
const h=(t,a={},...c)=>{const e=document.createElement(t);
  for(const[k,v]of Object.entries(a||{})){
    if(k==='class')e.className=v;
    else if(k.startsWith('on'))e.addEventListener(k.slice(2),v);
    else if(v!==false&&v!=null)e.setAttribute(k,v===true?'':v);
  }
  for(const x of c.flat())e.append(x?.nodeType?x:document.createTextNode(x??''));return e};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const get=(o,p)=>p.split('.').reduce((a,k)=>a?.[k],o);
function setPath(o,p,v){const ks=p.split('.');let c=o;for(const k of ks.slice(0,-1))c=c[k]??={};c[ks.at(-1)]=v}
const ls={get(k){try{return localStorage.getItem(k)}catch{return null}},set(k,v){try{localStorage.setItem(k,v)}catch{}}};

let S=null,SCHEMA=null,PRESETS=[],tab=(location.hash||'').slice(1)||ls.get('tab')||'chat';
/* [key, label, subtitle, icon]; a string in the list starts a group in the nav */
const TABS=[
 ['chat','Chat','talk to her, as yourself','chat'],
 ['live','Live','what she is doing right now, and why','pulse'],
 ['people','People','who she knows, and exactly what she keeps about each of them','people'],
 'Shape her',
 ['persona','Persona','who she is','mask'],
 ['talk','Test bench','try the persona and rules without touching memory','flask'],
 ['rules','House rules','rules, skills and notes for specific rooms','scroll'],
 ['models','Models','the brains behind her','chip'],
 ['rooms','Rooms','where she lives on Discord','hash'],
 ['dials','Dials','how she behaves. Every change applies instantly','sliders'],
 ['tools','Tools','what she can do besides talk','wrench'],
 'Under the hood',
 ['memory','Memory & log','what she keeps, and what just happened','book'],
];
const TAB=TABS.filter(t=>typeof t!=='string');
/* drawn once, one stroke weight: no glyphs standing in for icons */
const ICONS={
 chat:'<path d="M4 5.5h16v11H10l-4 3.5v-3.5H4z"/>',
 pulse:'<path d="M3 12h4l2.5-6 4 12 2.5-6H21"/>',
 people:'<circle cx="9" cy="8" r="3"/><path d="M3.5 19.5c0-3.6 2.4-5.5 5.5-5.5s5.5 1.9 5.5 5.5"/><circle cx="17" cy="9" r="2.2"/><path d="M17 14c2.6 0 3.8 1.6 3.8 4.2"/>',
 mask:'<circle cx="12" cy="12" r="8.5"/><path d="M8.6 14.3c.9 1.4 2 2 3.4 2s2.5-.6 3.4-2"/><path d="M9 10h.01M15 10h.01"/>',
 flask:'<path d="M9.5 3.5h5M10.5 3.5v5.2l-5 8.8a2 2 0 0 0 1.7 3h9.6a2 2 0 0 0 1.7-3l-5-8.8V3.5"/><path d="M8 14h8"/>',
 scroll:'<path d="M6.5 4h11v16h-11z"/><path d="M9.5 8.5h5M9.5 12h5M9.5 15.5h3"/>',
 chip:'<rect x="6.5" y="6.5" width="11" height="11" rx="2"/><path d="M9.5 3v3.5M14.5 3v3.5M9.5 17.5V21M14.5 17.5V21M3 9.5h3.5M3 14.5h3.5M17.5 9.5H21M17.5 14.5H21"/>',
 hash:'<path d="M5 9h14M5 15h14M10 4 8.5 20M15.5 4 14 20"/>',
 sliders:'<path d="M4 7h9M17 7h3M4 17h3M11 17h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/>',
 wrench:'<path d="M14.6 6.4a4.2 4.2 0 0 0-5.3 5.3L4 17l3 3 5.3-5.3a4.2 4.2 0 0 0 5.3-5.3l-2.8 2.8-2.4-.6-.6-2.4z"/>',
 book:'<ellipse cx="12" cy="6" rx="7" ry="3"/><path d="M5 6v12c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12c0 1.7 3.1 3 7 3s7-1.3 7-3"/>',
 x:'<path d="M6 6l12 12M18 6 6 18"/>',
 up:'<path d="M6 14.5 12 8l6 6.5"/>',
 check:'<path d="M5 12.5l4.5 4.5L19 7.5"/>',
 alert:'<path d="M12 7.5v6M12 17h.01"/><circle cx="12" cy="12" r="9"/>',
 send:'<path d="M5 12h13M13 6.5 18.5 12 13 17.5"/>',
 file:'<path d="M7 3.5h7l4 4V20.5H7z"/><path d="M14 3.5v4h4"/>',
};
function ico(n,cls=''){const e=document.createElementNS('http://www.w3.org/2000/svg','svg');e.setAttribute('viewBox','0 0 24 24');e.setAttribute('class','ico '+cls);e.setAttribute('aria-hidden','true');e.innerHTML=ICONS[n]||'';return e}

/* ---------- plumbing ---------- */
function toast(m,err){const t=$('#toast');t.textContent=m;t.className=err?'err':'';t.style.display='block';clearTimeout(toast.t);toast.t=setTimeout(()=>t.style.display='none',err?6500:2400)}
const authHeaders=()=>{const t=sessionStorage.getItem('tok');return t?{Authorization:'Bearer '+t}:{}};
async function api(m,p,b){
  const o={method:m,headers:{'X-Mak1zu':'1',...authHeaders()}};
  if(b!==undefined){o.headers['Content-Type']='application/json';o.body=JSON.stringify(b)}
  const r=await fetch(p,o);
  if(r.status===401){const t=prompt('Panel token');if(t){sessionStorage.setItem('tok',t);return api(m,p,b)}}
  const j=await r.json().catch(()=>({}));
  if(!r.ok)throw new Error(j.error||r.statusText);return j;
}
async function applyEdit(path,val){await api('POST','/api/config/patch',{edits:{[path]:val}});setPath(S.config,path,val)}
const fmtTime=ts=>new Date(ts).toLocaleTimeString([], {hour12:false});
const fmtUp=s=>s<90?s+'s':s<5400?Math.round(s/60)+' min':(s/3600).toFixed(1)+' h';
const same=(a,b)=>JSON.stringify(a)===JSON.stringify(b);
const pct=v=>Math.round(v*100)+'%';

/* ---------- one setting, live ---------- */
function showDefault(sp){
  const d=sp.default;
  if(sp.kind==='chance')return pct(d);
  if(Array.isArray(d))return d.length?d.join(', '):'none';
  if(d===null||d===''||d===undefined)return 'empty';
  if(typeof d==='boolean')return d?'on':'off';
  return String(d)+(sp.unit&&sp.unit!=='%'?' '+sp.unit:'');
}
function settingRow(sp){
  const row=h('div',{class:'set'});
  const meta=h('div',{class:'meta'});
  let status='';
  const cur=()=>get(S.config,sp.path);
  const paintMeta=()=>{
    const kids=[];
    if(status)kids.push(h('span',{class:'ok'},status));
    if(sp.restart)kids.push(h('span',{class:'pill warn'},'needs restart'));
    if('default' in sp&&!same(cur()??null,sp.default??null)&&sp.default!==undefined){
      kids.push(h('span',{},'default: '+showDefault(sp)),h('button',{class:'btn tiny',onclick:()=>commit(sp.default,true)},'reset'));
    }
    meta.replaceChildren(...kids);
  };
  async function commit(v,redraw){
    const prev=cur();
    try{
      await applyEdit(sp.path,v);
      status=sp.restart?'saved':'applied live';
      if(sp.path==='behavior.paused')await refreshState();
      if(redraw){row.replaceWith(settingRow(sp));return}
      paintMeta();
    }catch(e){toast(e.message,1);setPath(S.config,sp.path,prev);row.replaceWith(settingRow(sp))}
  }
  const c=cur();let ctl;
  switch(sp.kind){
  case'toggle':
    ctl=h('label',{class:'switch'},h('input',{type:'checkbox',checked:!!c,onchange:e=>commit(e.target.checked)}),h('span'));break;
  case'chance':{
    const out=h('output',{},pct(c??0));
    ctl=h('div',{class:'range'},h('input',{type:'range',min:0,max:1,step:.05,value:c??0,
      oninput:e=>{out.textContent=pct(+e.target.value)},onchange:e=>commit(+e.target.value)}),out);break}
  case'number':case'seconds':
    ctl=h('input',{type:'number',value:c??'',min:sp.min,max:sp.max,step:sp.step??(sp.kind==='seconds'?'any':1),
      onchange:e=>{let v=e.target.value===''?0:+e.target.value;if(sp.min!=null)v=Math.max(sp.min,v);if(sp.max!=null)v=Math.min(sp.max,v);e.target.value=v;commit(v)}});break;
  case'choice':
    ctl=h('select',{onchange:e=>commit(e.target.value)},(sp.options||[]).map(o=>h('option',{value:o,selected:o===c},o||'(default)')));break;
  case'ids':case'lines':
    ctl=h('textarea',{rows:Math.max(2,Math.min(6,(c||[]).length+1)),placeholder:sp.kind==='ids'?'one ID per line, pasted as text':'one per line',spellcheck:'false',
      onchange:e=>commit(e.target.value.split('\n').map(s=>s.trim()).filter(Boolean))},(c||[]).join('\n'));break;
  case'secret':
    ctl=h('input',{type:'password',autocomplete:'off',placeholder:sp.isSet?'set, type to replace':'not set',
      onchange:e=>{if(e.target.value){commit(e.target.value).then(()=>{e.target.value='';e.target.placeholder='set, type to replace'})}}});break;
  default:
    ctl=h('input',{type:'text',value:c??'',spellcheck:'false',onchange:e=>commit(e.target.value.trim())});
  }
  paintMeta();
  row.append(h('div',{},h('div',{class:'lbl'},sp.label),h('div',{class:'help'},sp.help||'')),h('div',{class:'ctl'},ctl,meta));
  return row;
}
const group=id=>SCHEMA.settings.filter(s=>s.group===id);
function settingsCard(id,{advanced=false}={}){
  const g=SCHEMA.groups.find(x=>x.id===id);
  const list=group(id).filter(s=>advanced||!s.advanced),adv=group(id).filter(s=>s.advanced);
  const card=h('div',{class:'card'},h('h3',{},g.title),h('p',{class:'lead'},g.blurb),list.map(settingRow));
  if(adv.length&&!advanced)card.append(h('details',{class:'fold'},h('summary',{},'Advanced ('+adv.length+')'),adv.map(settingRow)));
  return card;
}

/* ---------- live feed ---------- */
const FEED=[];let lastEvt=0,feedFilter=sessionStorage.getItem('ff')||'all',liveState='connecting';
function matches(e){
  switch(feedFilter){
    case'replied':return e.type==='replied';
    case'heard':return e.type==='heard'||e.type==='replied';
    case'quiet':return e.type==='quiet';
    case'problems':return e.type==='incident'||e.type==='slip';
    case'panel':return e.type==='system';
  }return true;
}
function feedRow(e){
  const who=e.type==='replied'?h('div',{class:'who'},h('b',{},'her'),' in '+e.place):
    e.type==='system'?null:
    h('div',{class:'who'},e.author?h('b',{},e.author):'',e.author?' in ':'',e.place||'');
  const txt=e.type==='system'?e.why:(e.text||(e.type==='incident'?e.why:''));
  const bits=[];
  if(e.type==='replied'){
    bits.push(h('span',{class:'why'},e.words+' words · '+((e.latency_ms||0)/1000).toFixed(1)+'s'+(e.model?' · '+e.model:'')+((e.tools||[]).length?' · used '+e.tools.join(', '):'')));
    if(e.saw)bits.push(h('div',{class:'why saw'},e.saw));
  }else if(e.why&&e.type!=='system'){
    bits.push(h('span',{class:'why'},e.why));
  }
  return h('div',{class:'ev '+e.type},h('div',{class:'t'},fmtTime(e.ts)),h('div',{class:'g'}),
    h('div',{},who,txt?h('div',{class:'txt'},txt):'',e.type==='incident'&&e.text&&e.text!==txt?h('div',{class:'who mono'},e.text):'',bits));
}
let feedBox=null;
function pushEvent(e){
  lastEvt=Math.max(lastEvt,e.id);FEED.push(e);if(FEED.length>300)FEED.shift();
  if(feedBox&&matches(e)){
    feedBox.querySelector('.empty')?.remove();
    feedBox.prepend(feedRow(e));while(feedBox.children.length>300)feedBox.lastChild.remove();
  }
  if(e.type!=='system')scheduleState();
}
let stT=0;function scheduleState(){clearTimeout(stT);stT=setTimeout(refreshState,1200)}
function setLive(s){liveState=s;document.documentElement.dataset.live=s;const b=$('#livebadge');if(b){b.textContent=s==='live'?'● live':s==='connecting'?'connecting…':'offline, retrying';b.className='pill livebadge '+(s==='live'?'ok':'bad')}}
async function stream(){
  for(;;){
    try{
      setLive('connecting');
      const r=await fetch('/api/events?since='+lastEvt,{headers:authHeaders()});
      if(!r.ok)throw new Error(r.status);
      setLive('live');
      const rd=r.body.getReader(),dec=new TextDecoder();let buf='';
      for(;;){
        const{value,done}=await rd.read();if(done)break;
        buf+=dec.decode(value,{stream:true});let i;
        while((i=buf.indexOf('\n\n'))>=0){
          const line=buf.slice(0,i).split('\n').find(l=>l.startsWith('data: '));buf=buf.slice(i+2);
          if(line){try{pushEvent(JSON.parse(line.slice(6)))}catch{}}
        }
      }
    }catch(e){document.documentElement.dataset.err=String(e)}
    setLive('offline');await sleep(2500);
  }
}

/* ---------- views ---------- */
const views={};
let onState=null;

views.live=()=>{
  const stats=h('div',{class:'pulse'});
  const todo=h('div');
  const paint=()=>{
    const a=S.activity||{};
    stats.replaceChildren(
      h('span',{},h('b',{},a.replied??0),'replies in the last hour'),
      h('span',{},h('b',{},a.heard??0),'times she was called in'),
      h('span',{},h('b',{},a.quiet??0),'times she stayed quiet'),
      h('span',{class:a.incidents?'bad':''},h('b',{},a.incidents??0),a.incidents===1?'problem':'problems'));
    const open=(S.checklist||[]).filter(s=>!s.ok);
    todo.replaceChildren(...(open.length?[h('div',{class:'card'},h('h3',{},'Before she can really live here'),
      h('p',{class:'lead'},'These are the things she is missing right now.'),
      (S.checklist||[]).map(s=>h('div',{class:'check'+(s.ok?'':' todo')},h('span',{class:'mark'},ico(s.ok?'check':'alert')),h('div',{},s.text),
        s.ok?h('span'):h('button',{class:'btn tiny',onclick:()=>go(s.tab)},'fix'),s.fix?h('div',{class:'fix'},s.fix):'')))]:[]));
  };
  onState=paint;paint();
  const chips=[['all','Everything'],['heard','Called in'],['replied','Her replies'],['quiet','Stayed quiet'],['problems','Problems'],['panel','Your changes']];
  const bar=h('div',{class:'toolbar'},chips.map(([k,l])=>h('button',{class:'chip'+(k===feedFilter?' on':''),onclick:()=>{feedFilter=k;sessionStorage.setItem('ff',k);go('live')}},l)),
    h('span',{id:'livebadge'}));
  feedBox=h('div',{class:'feed'});
  const rows=FEED.filter(matches).slice(-300).reverse().map(feedRow);
  feedBox.append(...(rows.length?rows:[h('div',{class:'empty'},h('b',{},'Quiet so far.'),'Everything she hears and why she answers or stays silent shows up here as it happens. Say something in a room she lives in, or open Chat.')]));
  setTimeout(()=>setLive(liveState),0);
  return [todo,stats,bar,feedBox];
};


/* ---------- chat: the real conversation, as yourself ---------- */
function ago(iso){
  const d=(Date.now()-new Date(iso))/1000;if(!isFinite(d))return '';
  if(d<20*3600)return 'today';if(d<44*3600)return 'yesterday';
  if(d<14*86400)return Math.round(d/86400)+' days ago';if(d<60*86400)return Math.round(d/604800)+' weeks ago';
  return Math.round(d/2592000)+' months ago';
}
async function download(name){
  try{const r=await fetch('/api/chat/file/'+encodeURIComponent(name),{headers:authHeaders()});if(!r.ok)throw new Error(r.statusText);
    const a=h('a',{href:URL.createObjectURL(await r.blob()),download:name});document.body.append(a);a.click();a.remove()}catch(e){toast('Could not fetch '+name+': '+e.message,1)}
}
views.chat=()=>{
  const her=S.her_name||S.active;
  const stream=h('div',{class:'stream',role:'log','aria-live':'polite'});
  const box=h('textarea',{rows:1,'aria-label':'Message',placeholder:'Say something, or / for commands',
    onkeydown:e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();send()}},
    oninput:()=>{box.style.height='auto';box.style.height=Math.min(160,box.scrollHeight)+'px'}});
  let last='',typing=null,away=false;
  const down=()=>{stream.scrollTop=stream.scrollHeight};
  function line(kind,text,files){
    typing?.remove();typing=null;
    const el=h('div',{class:'line '+kind+(kind==='her'&&last!=='her'?' first':'')});
    if(kind==='her'&&last!=='her')el.append(h('span',{class:'nm'},her));
    el.append(text);
    for(const f of files||[])el.append(h('div',{},h('a',{class:'file',href:'#',onclick:e=>{e.preventDefault();download(f)}},ico('file'),f)));
    stream.append(el);last=kind;down();return el;
  }
  const note=(t,block)=>{typing?.remove();typing=null;stream.append(h('div',{class:'line note'+(block?' block':'')},t));last='note';down()};
  const dots=()=>{if(typing)return;typing=h('div',{class:'typing3','aria-label':her+' is typing'},h('i'),h('i'),h('i'));stream.append(typing);down()};
  async function send(){
    const t=box.value.trim();if(!t)return;box.value='';box.style.height='auto';
    if(t.startsWith('/')){
      const[n,...r]=t.slice(1).split(' ');
      if(n==='help'){note('/memories  /diary  /callme NAME  /link  /forget N  /remember TEXT  /mood','block');return}
      try{const x=await api('POST','/api/chat/command',{name:n,arg:r.join(' ')});note(x.out||'done',true)}catch(e){note(e.message==='no such command'?'No such command. /help lists them.':e.message)}
      return;
    }
    line('you',t);dots();
    try{await api('POST','/api/chat/say',{text:t})}catch(e){typing?.remove();typing=null;note('Could not send: '+e.message)}
  }
  /* history first, then the live stream; whatever she said while the tab was closed arrives as backlog */
  const ctrl=new AbortController();leave=()=>ctrl.abort();
  (async()=>{
    try{
      const hist=await api('GET','/api/chat/history?n=40');
      for(const m of hist)line(m.who==='her'?'her':'you',m.text);
      if(!hist.length)stream.append(h('div',{class:'empty'},h('b',{},'Nothing said yet.'),'She is here. Say something, or try /memories to see what she already holds.'));
      const r=await fetch('/api/chat/stream',{headers:authHeaders(),signal:ctrl.signal});
      const rd=r.body.getReader(),dec=new TextDecoder();let buf='';
      for(;;){
        const{value,done}=await rd.read();if(done)break;
        buf+=dec.decode(value,{stream:true});let i;
        while((i=buf.indexOf('\n\n'))>=0){
          const l=buf.slice(0,i).split('\n').find(x=>x.startsWith('data: '));buf=buf.slice(i+2);if(!l)continue;
          let ev;try{ev=JSON.parse(l.slice(6))}catch{continue}
          stream.querySelector('.empty')?.remove();
          if(ev.kind==='typing')dots();
          else{
            if(ev.kind==='backlog'&&!away){away=true;note('she wrote while you were away');last=''}
            line('her',ev.text,ev.files);
          }
        }
      }
    }catch(e){if(e.name!=='AbortError')note('The connection to her dropped. Switch tabs and come back to reconnect.')}
  })();
  setTimeout(()=>box.focus(),0);
  return [h('div',{class:'room'},stream,h('div',{class:'say'},box,h('button',{class:'send','aria-label':'Send',onclick:send},ico('send'))),
    h('div',{class:'hint'},'Enter sends, Shift+Enter adds a line. This is you, the owner, on the local chat: what you tell her here she remembers everywhere you are linked.'))];
};

/* ---------- people: what she keeps, nothing hidden ---------- */
views.people=async()=>{
  const list=(await api('GET','/api/people'))||[];
  const pane=h('div',{class:'pd'}),side=h('div',{class:'plist'});
  let cur=ls.get('person');
  const name=p=>p.CallMe||p.Name||p.ID;
  const where=p=>[...new Set((p.Accounts||[]).map(a=>a.Transport==='local'||a.Transport==='cli'?'terminal':a.Transport))].join(' · ')||'no account';
  const drawList=()=>side.replaceChildren(...list.map(p=>h('button',{class:p.ID===cur?'on':'',onclick:()=>{cur=p.ID;ls.set('person',cur);drawList();open()}},
    h('b',{},name(p),p.Role==='owner'?h('span',{class:'pill'},'owner'):''),
    h('small',{},where(p)+' · '+p.Memories+' memories'+(p.Threads?', '+p.Threads+' open':'')))));
  const rm=(label,fn)=>h('button',{class:'ib','aria-label':label,title:label,onclick:async e=>{try{await fn();open()}catch(x){toast(x.message,1)}}},ico('x'));
  async function open(){
    if(!cur||!list.find(p=>p.ID===cur))cur=list[0]?.ID;
    if(!cur){pane.replaceChildren(h('div',{class:'empty'},h('b',{},'She does not know anyone yet.'),'People show up here once she has actually talked with them. Overhearing a room does not count.'));return}
    drawList();
    const d=await api('GET','/api/people/'+encodeURIComponent(cur)),p=d.person;
    for(const k of ['accounts','memories','threads','bits','diary','unsaid'])d[k]=d[k]||[];
    const del=(what,n)=>api('DELETE','/api/people/'+encodeURIComponent(cur)+'/'+what+'/'+n);
    const field=(k,label,ph)=>h('label',{},label,h('input',{type:'text',value:p[{call_me:'CallMe',pronouns:'Pronouns',language:'Language',tz:'TZ',quiet:'Quiet'}[k]]||'',placeholder:ph,
      onchange:async e=>{try{await api('PUT','/api/people/'+encodeURIComponent(cur)+'/profile',{field:k,value:e.target.value});toast('Saved');const i=list.findIndex(x=>x.ID===cur);if(i>=0){const f=await api('GET','/api/people');list.splice(0,list.length,...f);drawList()}}catch(x){toast(x.message,1);open()}}}));
    const sure=h('span',{class:'sure'});
    const forget=()=>sure.replaceChildren(h('span',{class:'mut'},'Really forget '+name(p)+'? This cannot be undone.'),
      h('button',{class:'btn danger tiny',onclick:async()=>{try{await api('DELETE','/api/people/'+encodeURIComponent(cur));toast('Forgotten');const f=await api('GET','/api/people');list.splice(0,list.length,...f);cur=null;open()}catch(x){toast(x.message,1)}}},'Yes, forget'),
      h('button',{class:'btn tiny',onclick:()=>sure.replaceChildren(h('button',{class:'btn danger',onclick:forget},'Forget everything about '+name(p)))},'No'));
    sure.replaceChildren(h('button',{class:'btn danger',onclick:forget},'Forget everything about '+name(p)));
    const sec=(title,sub,rows,empty)=>h('section',{class:'psec'},h('h4',{},title,sub?h('small',{},sub):''),...(rows.length?rows:[h('p',{class:'mut',style:'margin:0 0 8px'},empty)]));
    pane.replaceChildren(
      h('h3',{},name(p)),
      h('div',{class:'acct'},p.Role==='owner'?h('span',{class:'pill ok'},'owner'):'',...(d.accounts||[]).map(a=>h('span',{class:'pill',title:a.ExternalID},(a.Transport==='local'||a.Transport==='cli'?'terminal':a.Transport)+(a.Display?' · '+a.Display:'')))),
      h('section',{class:'psec'},h('h4',{},'How they want to be treated',h('small',{},'she follows this on every platform')),
        h('div',{class:'pfields'},field('call_me','What she calls them','their name'),field('pronouns','Pronouns','they/them'),field('language','Language','English'),field('tz','Time zone','Europe/Madrid'),field('quiet','Quiet hours (no messages)','23:00-08:00'),
          h('div',{class:'tg'},h('label',{class:'switch'},h('input',{type:'checkbox',checked:p.Checkins!=='off',onchange:async e=>{try{await api('PUT','/api/people/'+encodeURIComponent(cur)+'/profile',{field:'checkins',value:e.target.checked?'on':'off'});toast(e.target.checked?'She may start conversations with them.':'She will not start conversations with them.')}catch(x){toast(x.message,1);open()}}}),h('span')),'She may write first'))),
      sec('What she knows',d.memories.length+' kept',d.memories.map(m=>h('div',{class:'prow'},h('div',{},h('div',{class:'txt'},m.Content),h('div',{class:'by'},(m.Source?'told on '+(m.Source==='cli'||m.Source==='local'?'the terminal':m.Source)+', ':'')+ago(m.Created))),rm('Forget this',()=>del('memory',m.ID)))),'Nothing yet.'),
      sec('Still in flight','things she follows up on, in private only',d.threads.map(t=>h('div',{class:'prow'},h('div',{class:'txt'},t.Text,t.Due?h('span',{class:'by'},'  due '+new Date(t.Due).toLocaleDateString()):''),rm('Close this',()=>del('thread',t.ID)))),'Nothing open.'),
      sec('Running bits','jokes only the two of them share',d.bits.map(b=>h('div',{class:'prow'},h('div',{},h('div',{class:'txt'},b.Text),h('div',{class:'by'},b.Uses?'used '+b.Uses+' time'+(b.Uses===1?'':'s')+(b.LastUsed?', last '+ago(b.LastUsed):''):'not used yet')),rm('Drop this bit',()=>del('bit',b.ID)))),'None yet.'),
      sec('Waiting to be said','what the night shift left on her mind',d.unsaid.map(u=>h('div',{class:'prow'},h('div',{class:'txt'},u.Text),'')),'Nothing waiting.'),
      sec('Her diary','private notes in her own voice, newest first',d.diary.map(x=>h('div',{class:'prow diary'},h('div',{},h('div',{class:'by'},x.Day),h('div',{class:'txt'},x.Text)),'')).concat(d.diary.length?[h('div',{class:'actions'},h('button',{class:'btn tiny',onclick:async()=>{try{await del('diary',0);open()}catch(x){toast(x.message,1)}}},'Delete the diary'))]:[]),'Nothing written. The night shift writes these once a day, if you turn it on in Memory & log.'),
      h('section',{class:'psec'},h('h4',{},'Forget'),h('p',{class:'lead'},'Erases every memory, thread, bit, diary entry and account link for this person.'),sure));
  }
  await open();
  return [h('div',{class:'people'},side,pane)];
};

let convo=[];try{convo=JSON.parse(sessionStorage.getItem('convo')||'[]')}catch{}
let lastSystem='';
views.talk=()=>{
  const chat=h('div',{class:'chat'});
  const sys=h('pre',{class:'sys'},lastSystem||'Send a message and the exact prompt she received shows up here.');
  const speaker=h('input',{type:'text',value:sessionStorage.getItem('spk')||'You',style:'max-width:170px',onchange:e=>sessionStorage.setItem('spk',e.target.value)});
  const box=h('textarea',{placeholder:'Say something to her. Enter sends, Shift+Enter adds a line.',onkeydown:e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();send()}}});
  const draw=()=>{chat.replaceChildren(...(convo.length?convo.map(m=>h('div',{class:'msg '+m.role},m.text,m.meta?h('span',{class:'meta'},m.meta):'')):[h('div',{class:'empty'},'Say something. It goes through her real prompt, minus tools and memory.')]));chat.scrollTop=chat.scrollHeight;sessionStorage.setItem('convo',JSON.stringify(convo.slice(-40)))};
  async function send(){
    const t=box.value.trim();if(!t)return;box.value='';
    convo.push({role:'you',text:t});draw();
    const wait=h('div',{class:'msg her typing'},'typing…');chat.append(wait);chat.scrollTop=chat.scrollHeight;
    try{
      const r=await api('POST','/api/preview',{speaker:speaker.value,convo:convo.map(({role,text})=>({role,text}))});
      if(r.system){lastSystem=r.system;sys.textContent=r.system}
      if(r.ok){const x=r.result;lastSystem=x.system;sys.textContent=x.system;
        convo.push({role:'her',text:x.reply,meta:x.words+' words · '+(x.latency_ms/1000).toFixed(1)+'s · '+x.model+((x.robotic_hits||[]).length?' · sounds like support: '+x.robotic_hits.join(', '):'')})}
      else{convo.push({role:'her',text:'(no reply) '+r.error})}
    }catch(e){convo.push({role:'her',text:'(failed) '+e.message})}
    draw();
  }
  draw();
  return [h('div',{class:'talk'},
    h('div',{class:'card'},h('h3',{},'Chat',h('small',{},'private, nothing reaches Discord')),chat,
      h('div',{class:'compose'},box,h('button',{class:'btn pri',onclick:send},'Send')),
      h('div',{class:'actions'},h('span',{class:'mut'},'speaking as'),speaker,h('button',{class:'btn',onclick:()=>{convo=[];draw()}},'New conversation'))),
    h('div',{class:'card'},h('h3',{},'What she was told',h('small',{},'persona, rules, mood, then the chat')),sys))];
};

views.persona=()=>{
  const sel=h('select',{onchange:e=>load(e.target.value)},S.personas.map(p=>h('option',{value:p,selected:p===S.active},p+(p===S.active?' (active)':''))));
  const ta=h('textarea',{class:'big',spellcheck:'false',oninput:()=>count()});
  const info=h('span',{class:'mut'});
  const count=()=>{info.textContent=ta.value.length+' characters, about '+Math.round(ta.value.length/4)+' tokens sent with every reply'};
  async function load(id){const r=await api('GET','/api/persona?id='+encodeURIComponent(id));ta.value=r.text;ta.dataset.id=id;count()}
  const save=async(then)=>{try{await api('PUT','/api/persona',{id:ta.dataset.id,text:ta.value});if(then==='activate'||then==='try'){await api('POST','/api/persona/activate',{id:ta.dataset.id});await refreshState()}toast(then?'Saved and active':'Saved');if(then==='try')go('talk')}catch(e){toast(e.message,1)}};
  const nid=h('input',{type:'text',placeholder:'new persona id (a-z, 0-9, -)',style:'max-width:250px'});
  setTimeout(()=>load(S.active),0);
  return [h('div',{class:'card'},h('h3',{},'Character file'),
    h('p',{class:'lead'},'Plain Markdown. The front matter sets her name and pronouns; the body is who she is. The shared human-writing rules are added automatically unless you set substrate: false. Changes apply on her next message.'),
    h('div',{class:'actions',style:'margin:0 0 10px'},sel,h('button',{class:'btn',onclick:()=>save()},'Save'),h('button',{class:'btn',onclick:()=>save('activate')},'Save & activate'),h('button',{class:'btn pri',onclick:()=>save('try')},'Save & try in Talk')),
    ta,h('div',{class:'actions'},info),
    h('div',{class:'actions'},nid,h('button',{class:'btn',onclick:async()=>{const id=nid.value.trim();if(!/^[a-z0-9][a-z0-9-]*$/.test(id))return toast('use a-z, 0-9 and -',1);
      try{await api('PUT','/api/persona',{id,text:'---\nname: '+id+'\n---\n\n# Who you are\n\nWho is she? Temperament, tastes, how she treats people. Be specific: opinions beat adjectives.\n'});await refreshState();toast('Created '+id);go('persona')}catch(e){toast(e.message,1)}}},'Create a new character')))];
};

const RULE_KINDS={rules:['Rules','10-tone','Always in her prompt, in name order. Short and specific works best.'],skills:['Skills','anime-recs','Know-how she reads only when it is relevant. Put description: in the front matter so she knows when.'],servers:['Servers','server ID','Applies only in that server.'],channels:['Channels','channel ID','Applies only in that channel.']};
views.rules=()=>{
  let kind=sessionStorage.getItem('dk')||'rules';const box=h('div');
  async function draw(){
    const ks=h('select',{style:'max-width:200px',onchange:e=>{kind=e.target.value;sessionStorage.setItem('dk',kind);draw()}},Object.entries(RULE_KINDS).map(([k,v])=>h('option',{value:k,selected:k===kind},v[0])));
    const l=await api('GET','/api/home/list?kind='+kind);
    const ta=h('textarea',{class:'big',spellcheck:'false',placeholder:'Pick a file, or type a new name above and start writing'}),cur=h('input',{type:'text',placeholder:RULE_KINDS[kind][1]});
    async function open(n){const r=await api('GET','/api/home/file?kind='+kind+'&name='+encodeURIComponent(n));cur.value=n;ta.value=r.text}
    box.replaceChildren(h('div',{class:'card'},h('h3',{},'Her directives'),h('p',{class:'lead'},RULE_KINDS[kind][2]+' Saved files apply on her next message.'),
      h('div',{class:'actions',style:'margin:0 0 10px'},ks,l.names.map(n=>h('button',{class:'chip',onclick:()=>open(n)},n))),
      cur,h('div',{style:'margin-top:8px'},ta),
      h('div',{class:'actions'},
        h('button',{class:'btn pri',onclick:async()=>{try{await api('PUT','/api/home/file',{kind,name:cur.value.trim(),text:ta.value});toast('Saved, she has it now');draw()}catch(e){toast(e.message,1)}}},'Save'),
        h('button',{class:'btn danger',onclick:async()=>{if(!cur.value||!confirm('Delete '+cur.value+'?'))return;try{await api('DELETE','/api/home/file?kind='+kind+'&name='+encodeURIComponent(cur.value.trim()));toast('Deleted');draw()}catch(e){toast(e.message,1)}}},'Delete'))));
  }
  draw();return [box];
};

function routeEditor(which,label){
  const wrap=h('div',{class:'set'});
  const paint=()=>{
    const list=get(S.config,'llm.routing.'+which)||[];
    const free=Object.keys(S.config.llm.providers).filter(n=>!list.includes(n));
    const save=async v=>{try{await applyEdit('llm.routing.'+which,v);paint()}catch(e){toast(e.message,1)}};
    const sel=h('select',{style:'max-width:170px',onchange:e=>{if(e.target.value)save([...list,e.target.value])}},[h('option',{value:''},'add fallback…'),...free.map(n=>h('option',{value:n},n))]);
    wrap.replaceChildren(h('div',{},h('div',{class:'lbl'},label),h('div',{class:'help'},which==='text'?'The first healthy provider answers. If it fails, the next one takes over.':'Used when someone sends an image.')),
      h('div',{class:'ctl'},h('div',{class:'route'},list.map((n,i)=>h('span',{class:'chip'},(i+1)+'. '+n,
        h('button',{title:'move up','aria-label':'move up',onclick:()=>{if(i>0){const x=[...list];[x[i-1],x[i]]=[x[i],x[i-1]];save(x)}}},ico('up')),
        h('button',{title:'remove','aria-label':'remove',onclick:()=>save(list.filter((_,j)=>j!==i))},ico('x')))),sel)));
  };
  paint();return wrap;
}
function modelChips(base,models,label){
  return h('div',{},h('div',{class:'mut',style:'margin-top:6px'},label),h('div',{class:'models'},models.map(m=>h('button',{class:'chip',onclick:async()=>{try{await applyEdit(base+'.model',m);toast('Model set to '+m);go('models')}catch(e){toast(e.message,1)}}},m))));
}
function providerCard(n){
  const p=S.config.llm.providers[n],base='llm.providers.'+n;
  const out=h('div');
  const find=async()=>{
    out.replaceChildren(h('div',{class:'diag'},'Asking '+p.base_url+' what it serves…'));
    try{
      const r=await api('POST','/api/provider/discover',{name:n});
      if(r.models.length)out.replaceChildren(h('div',{class:'diag ok'},'It serves '+r.models.length+' model'+(r.models.length===1?'':'s')+'.',modelChips(base,r.models.slice(0,60),'Click one to use it:')));
      else out.replaceChildren(h('div',{class:'diag bad'},h('b',{},'Could not read its model list'),h('div',{class:'fix'},(r.error||'it answered with an empty list')+'. Not every server has this route: type the model id in Settings.')));
    }catch(e){out.replaceChildren(h('div',{class:'diag bad'},e.message))}
  };
  const test=async()=>{
    out.replaceChildren(h('div',{class:'diag'},'Calling '+p.model+'…'));
    try{
      const r=await api('POST','/api/provider/test',{name:n});
      if(r.ok)out.replaceChildren(h('div',{class:'diag ok'},'Works. It answered "'+r.reply+'" in '+(r.latency_ms/1000).toFixed(1)+'s.'));
      else out.replaceChildren(h('div',{class:'diag bad'},h('b',{},r.error),h('div',{class:'fix'},r.fix||''),
        (r.models||[]).length?h('div',{},h('div',{class:'mut',style:'margin-top:6px'},'It serves these. Click one to use it:'),h('div',{class:'models'},r.models.map(m=>h('button',{class:'chip',onclick:async()=>{try{await applyEdit(base+'.model',m);toast('Model set to '+m);go('models')}catch(e){toast(e.message,1)}}},m)))):''));
    }catch(e){out.replaceChildren(h('div',{class:'diag bad'},e.message))}
  };
  const S_=(path,label,help,kind,o={})=>settingRow({path:base+'.'+path,label,help,kind,...o});
  const keyed=S.has_key[n];
  return h('div',{class:'card'},
    h('h3',{},n,h('small',{},p.model),h('span',{class:'pill '+(p.enabled?'ok':'')},p.enabled?'on':'off'),
      p.api_key_env||p.api_key?h('span',{class:'pill '+(keyed?'ok':'bad')},keyed?'key found':'no key'):h('span',{class:'pill'},'no key needed'),
      S.cooldowns[n]?h('span',{class:'pill bad'},'cooling '+Math.ceil(S.cooldowns[n])+'s'):''),
    h('div',{class:'actions',style:'margin-top:4px'},h('button',{class:'btn pri',onclick:test},'Test with a real call'),
      h('button',{class:'btn',onclick:find},'Find models'),
      h('button',{class:'btn danger',onclick:async()=>{if(!confirm('Remove provider '+n+'?'))return;try{await api('POST','/api/config/patch',{edits:{[base]:null}});await refreshState();go('models')}catch(e){toast(e.message,1)}}},'Remove')),
    out,
    h('details',{class:'fold'},h('summary',{},'Settings'),
      S_('enabled','Enabled','Off keeps it configured but unused.','toggle'),
      S_('base_url','Base URL','Where requests go. Usually ends in /v1.','text'),
      S_('model','Model','The exact model id. If unsure, press Test: it lists what the provider serves.','text'),
      S_('protocol','Protocol','chat works almost everywhere. Use responses only for models that reject chat completions.','choice',{options:['chat','responses']}),
      S_('api_key_env','Key variable','Name of the environment variable (or .makizu/.env entry) holding the key. Preferred over pasting the key.','text'),
      S_('api_key','Key, write-only','Only if you cannot use a variable. It is stored in config.json and never shown again.','secret',{isSet:keyed&&!p.api_key_env}),
      S_('reasoning_effort','Reasoning effort','For thinking models. Low is faster and cheaper.','choice',{options:['','minimal','low','medium','high']}),
      S_('reasoning_headroom','Reasoning headroom','Extra tokens for thinking models. Too low and the visible reply comes back empty.','number',{min:0,max:20000}),
      S_('timeout_seconds','Timeout','How long to wait for an answer.','seconds',{min:5,max:600,unit:'s'}),
      S_('failure_cooldown_seconds','Cooldown after failure','How long to skip this provider after it fails.','seconds',{min:0,max:3600,unit:'s'}),
      S_('vision','Can see images','Turn on only if the model accepts images.','toggle')));
}
function customEndpointCard(){
  const addr=h('input',{type:'text',placeholder:'https://api.together.xyz/v1  or  http://localhost:8000/v1',autocomplete:'off',spellcheck:'false'});
  const key=h('input',{type:'password',placeholder:'empty if it needs none (stored write-only)',autocomplete:'off'});
  const model=h('input',{type:'text',placeholder:'model id',list:'dl-models',autocomplete:'off',spellcheck:'false'});
  const dl=h('datalist',{id:'dl-models'});
  const vision=h('input',{type:'checkbox'});
  const out=h('div'),here=h('div');
  const look=async()=>{
    if(!addr.value.trim()){toast('Type the address first.',1);return}
    out.replaceChildren(h('div',{class:'diag'},'Looking…'));
    try{
      const r=await api('POST','/api/provider/discover',{base_url:addr.value,key:key.value});
      addr.value=r.base_url;
      dl.replaceChildren(...r.models.map(m=>h('option',{value:m})));
      if(r.models.length===1&&!model.value)model.value=r.models[0];
      if(r.models.length)out.replaceChildren(h('div',{class:'diag ok'},(r.note?r.note+'. ':'')+'It serves '+r.models.length+' model'+(r.models.length===1?'':'s')+(r.models.length>1?': click the model box and pick one, or type an id.':'.')));
      else out.replaceChildren(h('div',{class:'diag bad'},h('b',{},r.status===401||r.status===403?'It wants a key.':'Could not read its model list'),h('div',{class:'fix'},r.status===401||r.status===403?'Paste the key above and press Look again.':'Fine if the server is not running yet, or it has no model list: type the model id yourself.')));
    }catch(e){out.replaceChildren(h('div',{class:'diag bad'},e.message))}
  };
  const add=async()=>{
    try{
      const r=await api('POST','/api/provider/add',{preset:'custom',base_url:addr.value,model:model.value,key:key.value,vision:vision.checked});
      await refreshState();toast('Added '+r.name+'. Press Test with a real call.');go('models');
    }catch(e){toast(e.message,1)}
  };
  api('GET','/api/local').then(list=>{
    if(!list.length)return;
    here.replaceChildren(h('div',{class:'lbl',style:'margin-top:10px'},'Running on this machine right now'),
      h('div',{class:'tiles'},list.map(l=>h('button',{class:'tile',onclick:()=>{addr.value=l.BaseURL;key.value='';dl.replaceChildren(...l.Models.map(m=>h('option',{value:m})));model.value=l.Models.length===1?l.Models[0]:'';look();model.focus()}},
        h('b',{},l.Name),h('span',{class:'mono'},l.BaseURL.replace('http://','').replace('/v1','')),h('span',{style:'font-size:12px'},l.Models.length+' model'+(l.Models.length===1?'':'s'))))));
  }).catch(()=>{});
  return h('div',{class:'card'},h('h3',{},'Your own endpoint'),
    h('p',{class:'lead'},'Any server that speaks the OpenAI API: vLLM, llama.cpp, LiteLLM, Together, Fireworks, a company gateway. Paste the address (with or without /v1, or the whole /chat/completions URL), press Look, pick a model.'),
    h('div',{class:'lbl'},'Address'),addr,
    h('div',{class:'lbl',style:'margin-top:8px'},'Key'),key,
    h('div',{class:'actions',style:'margin-top:8px'},h('button',{class:'btn',onclick:look},'Look: what does it serve?')),
    out,
    h('div',{class:'lbl',style:'margin-top:8px'},'Model'),model,dl,
    h('label',{class:'mut',style:'display:flex;gap:6px;align-items:center;margin-top:8px'},vision,'It can read images'),
    h('div',{class:'actions',style:'margin-top:8px'},h('button',{class:'btn pri',onclick:add},'Add it')),
    here);
}
views.models=()=>{
  const tiles=PRESETS.map(p=>h('button',{class:'tile',onclick:async()=>{
    let name=p.ID,i=2;while(S.config.llm.providers[name])name=p.ID+'-'+i++;
    try{await api('POST','/api/provider/add',{preset:p.ID,name});await refreshState();toast(p.KeyEnv?'Added '+name+'. Put '+p.KeyEnv+' in .makizu/.env, then Test.':'Added '+name+'. Make sure it is running, then Test.');go('models')}catch(e){toast(e.message,1)}}},
    h('b',{},p.Label),h('span',{class:'mono'},p.Model),
    h('span',{style:'font-size:12px'},p.Cost),
    h('span',{class:'mut',style:'font-size:12px'},p.KeyEnv?(p.key_found?p.KeyEnv+' found in your environment':'needs '+p.KeyEnv):'no key needed')));
  return [h('p',{class:'lead'},'Providers are the services that actually think. Add one, give it a key, press Test. If something is wrong the test says what, in words.'),
    ...Object.keys(S.config.llm.providers).map(providerCard),
    h('div',{class:'card'},h('h3',{},'Order of preference'),routeEditor('text','Chat'),routeEditor('vision','Images')),
    customEndpointCard(),
    h('div',{class:'card'},h('h3',{},'Add a ready-made provider'),h('p',{class:'lead'},'Presets with a sane model already picked. Add one, put its key in .makizu/.env, press Test.'),h('div',{class:'tiles'},tiles))];
};

views.rooms=()=>{
  const out=[];
  out.push(h('p',{class:'lead'},'Rooms are where she may talk. She answers a direct call anywhere she is invited, but only joins in uninvited in her home channels.'));
  if(S.invite_url){
    const wide=ls.get('inv-wide')==='1',url=wide?S.invite_url_expressions:S.invite_url;
    out.push(h('div',{class:'card'},h('h3',{},'Add her to a server'),h('p',{class:'lead'},'Built from your bot token, with exactly the permissions she uses. Enable Message Content Intent for the bot in the Discord developer portal first, or she will not see messages.'),
      h('div',{class:'set'},h('div',{},h('div',{class:'lbl'},'Let her upload and manage her own emoji'),h('div',{class:'help'},'Adds Create Expressions and Manage Expressions. These are broad: she could also delete emoji in that server. Only needed if she should use her own face as emoji.')),
        h('div',{class:'ctl'},h('label',{class:'switch'},h('input',{type:'checkbox',checked:wide,onchange:e=>{ls.set('inv-wide',e.target.checked?'1':'0');go('rooms')}}),h('span')))),
      h('div',{class:'actions'},h('a',{class:'btn pri',href:url,target:'_blank',rel:'noopener',style:'text-decoration:none'},'Open invite link'),
        h('button',{class:'btn',onclick:()=>navigator.clipboard?.writeText(url).then(()=>toast('Copied'))},'Copy link'))));
  }else if(S.config.discord.enabled){
    out.push(h('div',{class:'card'},h('h3',{},'Add her to a server'),h('p',{class:'lead'},'No bot token found yet. Put it in .makizu/.env as '+(S.config.discord.token_env||'MAK1ZU_DISCORD_TOKEN')+' and restart. The invite link appears here.')));
  }
  out.push(settingsCard('rooms'));
  return out;
};

views.dials=()=>{
  const adv=ls.get('adv')==='1';
  return [h('p',{class:'lead'},'Everything here applies the moment you change it, no restart. Watch the Live tab to see her react. Each dial shows its default and lets you reset it.'),
    ...['talk','rhythm','length','brain','panel'].map(g=>settingsCard(g,{advanced:adv})),
    adv?settingsCard('advanced',{advanced:true}):'',
    h('div',{class:'actions'},h('button',{class:'btn',onclick:()=>{ls.set('adv',adv?'0':'1');go('dials')}},adv?'Hide advanced dials':'Show every advanced dial'))];
};

views.tools=()=>{
  const off=new Set(S.config.tools.disabled||[]);
  const mcp=S.config.tools.mcp_servers||{};
  const toggle=async n=>{const next=new Set(off);next.has(n)?next.delete(n):next.add(n);try{await applyEdit('tools.disabled',[...next]);go('tools')}catch(e){toast(e.message,1)}};
  return [h('div',{class:'card'},h('h3',{},'Her tools',h('small',{},'click to switch one off or on, instantly')),
      h('p',{class:'lead'},'Tools are things she can do besides talk: remember, set reminders, look things up, attach files. A switched-off tool is invisible to her.'),
      h('div',{class:'toolgrid'},(S.tools||[]).map(n=>h('button',{class:'tool'+(off.has(n)?' off':''),onclick:()=>toggle(n)},n)))),
    h('div',{class:'card'},h('h3',{},'Web search'),group('tools').filter(x=>x.path==='search.searxng_url').map(settingRow)),
    h('div',{class:'card'},h('h3',{},'MCP servers',h('small',{},'external tools; only the ones you allow are exposed. Restart to connect')),
      Object.keys(mcp).length?Object.keys(mcp).map(n=>h('div',{class:'card'},h('h3',{},n),
        ...[['enabled','Enabled','toggle'],['command','Command','text'],['args','Arguments','lines'],['allow','Allowed tools','lines'],['heavy','Heavy (research budget)','toggle']].map(([k,l,kd])=>settingRow({path:'tools.mcp_servers.'+n+'.'+k,label:l,help:k==='allow'?'Only these tool names are exposed. Empty exposes nothing.':'',kind:kd,restart:true})),
        h('div',{class:'actions'},h('button',{class:'btn danger',onclick:async()=>{if(!confirm('Remove '+n+'?'))return;try{await api('POST','/api/config/patch',{edits:{['tools.mcp_servers.'+n]:null}});await refreshState();go('tools')}catch(e){toast(e.message,1)}}},'Remove')))):h('p',{class:'lead'},'None configured.'),
      (()=>{const nm=h('input',{type:'text',placeholder:'name',style:'max-width:160px'}),cm=h('input',{type:'text',placeholder:'command, e.g. uvx',style:'max-width:220px'});
        return h('div',{class:'actions'},nm,cm,h('button',{class:'btn',onclick:async()=>{if(!/^[a-z0-9_-]+$/i.test(nm.value)||!cm.value)return toast('name and command required',1);
          try{await api('POST','/api/config/patch',{edits:{['tools.mcp_servers.'+nm.value]:{enabled:true,command:cm.value,args:[],allow:[],heavy:false}}});await refreshState();go('tools')}catch(e){toast(e.message,1)}}},'Add server'))})())];
};

views.memory=async()=>{
  const m=S.memory||{},t=(await api('GET','/api/telemetry'))||[];
  return [h('div',{class:'card'},h('h3',{},'What she holds'),h('div',{class:'kv'},Object.entries(m).map(([k,v])=>h('div',{},h('small',{},k.replace(/_/g,' ')),h('b',{},v))))),
    settingsCard('memory'),
    h('div',{class:'card'},h('h3',{},'Recent turns and incidents',h('small',{},'shape and timing only, never what was said')),
      t.length?h('table',{},h('tr',{},['time','kind','provider','latency','words','tools','cause'].map(x=>h('th',{},x))),
        t.slice().reverse().map(r=>h('tr',{},h('td',{class:'mono'},fmtTime(r.ts)),h('td',{},r.kind),h('td',{},r.provider||''),h('td',{},r.latency_s?r.latency_s.toFixed(1)+'s':''),h('td',{},r.words||''),h('td',{},(r.tools||[]).join(', ')),h('td',{},r.cause||'')))):h('p',{class:'lead'},'Nothing yet.'))];
};

/* ---------- chrome ---------- */
const noArt=new Set();
function faceEl(){
  const lamp=()=>h('span',{class:'lamp'});
  if(!S.has_avatar)return lamp();
  const name=noArt.has(S.face)?'neutral':S.face;
  if(noArt.has(name))return lamp();
  return h('img',{class:'face',alt:'',src:'avatar/'+name+'.png?size=96',title:'she looks '+S.face,
    onerror:e=>{noArt.add(name);e.target.replaceWith(lamp())}});
}
let leave=null; /* a view can register how to stop what it started */
async function go(t){leave?.();leave=null;tab=t;ls.set('tab',t);history.replaceState(null,'','#'+t);await render()}
function paintHeader(){
  const t=TAB.find(x=>x[0]===tab)||TAB[0];
  $('#title').textContent=t[1];$('#sub').textContent=t[2];
  const off=!S.config.discord.enabled;
  const st=$('#state');st.className='state'+(S.paused?' paused':off?' off':'');
  st.replaceChildren(faceEl(),h('span',{},S.paused?'paused: listening, not talking':(off?'terminal only':'awake')+' · '+(S.mood||'steady')));
  const sp=$('#stamp');sp.textContent=S.paused?'resume':'pause';sp.className='stamp'+(S.paused?' on':'');
  $('#ver').textContent=S.active+' · v'+(S.version||'dev');
  $('#foot').textContent='up '+fmtUp(S.uptime_s||0);
  const open=(S.checklist||[]).filter(x=>!x.ok).length;
  const nav=$('#nav');nav.replaceChildren(...TABS.map(t=>typeof t==='string'?h('div',{class:'grp'},t):
    h('button',{class:t[0]===tab?'on':'','aria-current':t[0]===tab?'page':null,onclick:()=>go(t[0])},ico(t[3]),t[1],
    (t[0]==='live'&&open)?h('span',{class:'dot',title:open+' thing(s) to set up'}):'')));
}
async function render(){
  paintHeader();feedBox=null;onState=null;
  const m=$('#main');m.replaceChildren(h('p',{class:'mut'},'…'));
  try{m.replaceChildren(...[].concat(await views[tab]()))}catch(e){m.replaceChildren(h('div',{class:'card'},'Could not load this tab: '+e.message))}
}
async function refreshState(){S=await api('GET','/api/state');paintHeader();onState?.()}
$('#stamp').onclick=async()=>{
  try{await applyEdit('behavior.paused',!S.paused);await refreshState();toast(S.paused?'Paused. She is still listening. She just will not say anything.':'Back.');if(tab==='dials')go('dials')}catch(e){toast(e.message,1)}
};
(async()=>{
  try{
    [S,SCHEMA,PRESETS]=await Promise.all([api('GET','/api/state'),api('GET','/api/schema'),api('GET','/api/presets')]);
    const recent=await api('GET','/api/events/recent');recent.forEach(e=>{lastEvt=Math.max(lastEvt,e.id);FEED.push(e)});
    if(!views[tab])tab='live';
    await render();stream();setInterval(()=>refreshState().catch(()=>{}),12000);
  }catch(e){$('#main').replaceChildren(h('div',{class:'card'},'Cannot reach her. Is she running? ('+e.message+')'))}
})();
