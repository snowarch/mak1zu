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

let S=null,SCHEMA=null,PRESETS=[],tab=(location.hash||'').slice(1)||ls.get('tab')||'live';
const TABS=[
 ['live','Live','what she is doing right now, and why'],
 ['talk','Talk','try her out with the live persona and rules'],
 ['persona','Persona','who she is'],
 ['rules','House rules','rules, skills and notes for specific rooms'],
 ['models','Models','the brains behind her'],
 ['rooms','Rooms','where she lives on Discord'],
 ['dials','Dials','how she behaves. Every change applies instantly'],
 ['tools','Tools','what she can do besides talk'],
 ['memory','Memory & log','what she keeps, and what just happened'],
];

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
const GLYPH={heard:'›',replied:'↳',quiet:'·',incident:'!',slip:'?',system:'~'};
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
  }else if(e.why&&e.type!=='system'){
    bits.push(h('span',{class:'why'},e.why));
  }
  return h('div',{class:'ev '+e.type},h('div',{class:'t'},fmtTime(e.ts)),h('div',{class:'g'},GLYPH[e.type]||'·'),
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
  const stats=h('div',{class:'stats'});
  const todo=h('div');
  const paint=()=>{
    const a=S.activity||{};
    stats.replaceChildren(
      h('div',{class:'stat'},h('b',{},a.replied??0),h('span',{},'replies, last hour')),
      h('div',{class:'stat'},h('b',{},a.heard??0),h('span',{},'times she was called in')),
      h('div',{class:'stat'},h('b',{},a.quiet??0),h('span',{},'times she stayed quiet')),
      h('div',{class:'stat'+(a.incidents?' bad':'')},h('b',{},a.incidents??0),h('span',{},'problems')));
    const open=(S.checklist||[]).filter(s=>!s.ok);
    todo.replaceChildren(...(open.length?[h('div',{class:'card'},h('h3',{},'Before she can really live here'),
      h('p',{class:'lead'},'These are the things she is missing right now.'),
      (S.checklist||[]).map(s=>h('div',{class:'check'+(s.ok?'':' todo')},h('span',{class:'mark'},s.ok?'✓':'✗'),h('div',{},s.text),
        s.ok?h('span'):h('button',{class:'btn tiny',onclick:()=>go(s.tab)},'fix'),s.fix?h('div',{class:'fix'},s.fix):'')))]:[]));
  };
  onState=paint;paint();
  const chips=[['all','Everything'],['heard','Called in'],['replied','Her replies'],['quiet','Stayed quiet'],['problems','Problems'],['panel','Your changes']];
  const bar=h('div',{class:'toolbar'},chips.map(([k,l])=>h('button',{class:'chip'+(k===feedFilter?' on':''),onclick:()=>{feedFilter=k;sessionStorage.setItem('ff',k);go('live')}},l)),
    h('span',{id:'livebadge'}));
  feedBox=h('div',{class:'feed'});
  const rows=FEED.filter(matches).slice(-300).reverse().map(feedRow);
  feedBox.append(...(rows.length?rows:[h('div',{class:'empty'},'Quiet in here. Say something in a room she lives in, or try her in Talk.')]));
  setTimeout(()=>setLive(liveState),0);
  return [todo,stats,bar,feedBox];
};

let convo=[];try{convo=JSON.parse(sessionStorage.getItem('convo')||'[]')}catch{}
let lastSystem='';
views.talk=()=>{
  const chat=h('div',{class:'chat'});
  const sys=h('pre',{class:'sys'},lastSystem||'Send a message and the exact prompt she received shows up here.');
  const speaker=h('input',{type:'text',value:sessionStorage.getItem('spk')||'You',style:'max-width:170px',onchange:e=>sessionStorage.setItem('spk',e.target.value)});
  const box=h('textarea',{placeholder:'Say something to her. Enter sends, Shift+Enter adds a line.',onkeydown:e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();send()}}});
  const draw=()=>{chat.replaceChildren(...(convo.length?convo.map(m=>h('div',{class:'msg '+m.role},m.text,m.meta?h('span',{class:'meta'},m.meta):'')):[h('div',{class:'empty'},'Nothing yet. Whatever you type goes through the same prompt she uses on Discord, minus tools and memory.')]));chat.scrollTop=chat.scrollHeight;sessionStorage.setItem('convo',JSON.stringify(convo.slice(-40)))};
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
        h('button',{title:'move up',onclick:()=>{if(i>0){const x=[...list];[x[i-1],x[i]]=[x[i],x[i-1]];save(x)}}},'▲'),
        h('button',{title:'remove',onclick:()=>save(list.filter((_,j)=>j!==i))},'✕'))),sel)));
  };
  paint();return wrap;
}
function providerCard(n){
  const p=S.config.llm.providers[n],base='llm.providers.'+n;
  const out=h('div');
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
views.models=()=>{
  const tiles=PRESETS.map(p=>h('button',{class:'tile',onclick:async()=>{
    let name=p.ID,i=2;while(S.config.llm.providers[name])name=p.ID+'-'+i++;
    try{await api('POST','/api/provider/add',{preset:p.ID,name});await refreshState();toast(p.KeyEnv?'Added '+name+'. Put '+p.KeyEnv+' in .makizu/.env, then Test.':'Added '+name+'. Make sure it is running, then Test.');go('models')}catch(e){toast(e.message,1)}}},
    h('b',{},p.Label),h('span',{class:'mono'},p.Model),
    h('span',{class:'mut',style:'font-size:12px'},p.KeyEnv?(p.key_found?p.KeyEnv+' found in your environment':'needs '+p.KeyEnv):'local, no key')));
  return [h('p',{class:'lead'},'Providers are the services that actually think. Add one, give it a key, press Test. If something is wrong the test says what, in words.'),
    ...Object.keys(S.config.llm.providers).map(providerCard),
    h('div',{class:'card'},h('h3',{},'Order of preference'),routeEditor('text','Chat'),routeEditor('vision','Images')),
    h('div',{class:'card'},h('h3',{},'Add a provider'),h('p',{class:'lead'},'Anything that speaks the OpenAI API also works: add any preset, then change its base URL and model.'),h('div',{class:'tiles'},tiles))];
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
async function go(t){tab=t;ls.set('tab',t);history.replaceState(null,'','#'+t);await render()}
function paintHeader(){
  const t=TABS.find(x=>x[0]===tab)||TABS[0];
  $('#title').textContent=t[1];$('#sub').textContent=t[2];
  const off=!S.config.discord.enabled;
  const st=$('#state');st.className='state'+(S.paused?' paused':off?' off':'');
  st.replaceChildren(faceEl(),h('span',{},S.paused?'paused: listening, not talking':(off?'terminal only':'awake')+' · '+(S.mood||'steady')));
  const sp=$('#stamp');sp.textContent=S.paused?'resume':'pause';sp.className='stamp'+(S.paused?' on':'');
  $('#ver').textContent=S.active+' · v'+(S.version||'dev');
  $('#foot').textContent='up '+fmtUp(S.uptime_s||0);
  const open=(S.checklist||[]).filter(x=>!x.ok).length;
  const nav=$('#nav');nav.replaceChildren(...TABS.map(([k,l],i)=>h('button',{class:k===tab?'on':'',onclick:()=>go(k)},h('i',{},String(i+1).padStart(2,'0')),l,
    (k==='live'&&open)?h('span',{class:'dot',title:open+' thing(s) to set up'}):'')));
}
async function render(){
  paintHeader();feedBox=null;onState=null;
  const m=$('#main');m.replaceChildren(h('p',{class:'mut'},'…'));
  try{m.replaceChildren(...[].concat(await views[tab]()))}catch(e){m.replaceChildren(h('div',{class:'card'},'Could not load this tab: '+e.message))}
}
async function refreshState(){S=await api('GET','/api/state');paintHeader();onState?.()}
$('#stamp').onclick=async()=>{
  try{await applyEdit('behavior.paused',!S.paused);await refreshState();toast(S.paused?'Paused. She still listens, she says nothing.':'She is back.');if(tab==='dials')go('dials')}catch(e){toast(e.message,1)}
};
(async()=>{
  try{
    [S,SCHEMA,PRESETS]=await Promise.all([api('GET','/api/state'),api('GET','/api/schema'),api('GET','/api/presets')]);
    const recent=await api('GET','/api/events/recent');recent.forEach(e=>{lastEvt=Math.max(lastEvt,e.id);FEED.push(e)});
    if(!views[tab])tab='live';
    await render();stream();setInterval(()=>refreshState().catch(()=>{}),12000);
  }catch(e){$('#main').replaceChildren(h('div',{class:'card'},'Cannot reach the panel API: '+e.message))}
})();
