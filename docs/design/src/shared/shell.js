(() => {
  if (globalThis.DonatePrototype) return;
  /* FROZEN_LOGO */
  const esc=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const icon=name=>`<i data-lucide="${name}" aria-hidden="true"></i>`;
  const button=(label,action,classes='d-ghost',attributes='')=>`<button type="button" class="d-btn ${classes}" data-shell-act="${action}" ${attributes}>${label}</button>`;
  const avatar=(letter,classes='')=>`<span class="d-avatar ${classes}" aria-hidden="true">${esc(letter)}</span>`;
  function control(label,action,prefix,classes,attributes=''){
    return `<button type="button" class="d-control ${prefix==='a'?'d-compact':''} ${prefix}-btn ${classes}" data-act="${action}" ${attributes}>${label}</button>`;
  }
  function dialogFrame({title,body,closeButton,panelClass,headClass,bodyClass='',footer='',footerClass=''}){
    return `<section class="d-dialog-frame ${panelClass}" role="dialog" aria-modal="true" aria-label="${esc(title)}"><div class="${headClass}"><h2>${esc(title)}</h2>${closeButton}</div>${bodyClass?`<div class="${bodyClass}">${body}</div>`:body}${footer?`<div class="${footerClass}">${footer}</div>`:''}</section>`;
  }
  const words={
    'zh-CN':{donate:'捐赠',admin:'站点管理',account:'账号信息',language:'切换语言',theme:'切换外观',system:'跟随系统',light:'浅色',dark:'深色',logout:'退出登录',login:'登录',close:'关闭',bound:'已绑定',bind:'绑定',id:'账号 ID',qq:'QQ 号',continue:'继续',loginDemo:'演示登录，不连接第三方服务。',qqDemo:'演示：仅检查号码格式，未验证 QQ 身份。'},
    en:{donate:'Donate',admin:'Site admin',account:'Account',language:'Language',theme:'Appearance',system:'System',light:'Light',dark:'Dark',logout:'Sign out',login:'Sign in',close:'Close',bound:'Linked',bind:'Link',id:'Account ID',qq:'QQ number',continue:'Continue',loginDemo:'Demo sign-in. No third-party connection.',qqDemo:'Demo: number format only; QQ identity is not verified.'}
  };
  const initialProfile={id:'019a2d00-0000-7000-8000-000000000001',provider:'OIDC',bindings:{OIDC:true,QQ:false}};
  function profile(){return {id:initialProfile.id,provider:initialProfile.provider,bindings:{...initialProfile.bindings}};}
  // Preview state only. Production identity and authorization will come from the Go service.
  const demoKey='donate-design-session-v2',listeners=new Set();
  function cleanDemo(value={}){
    return {version:2,identity:['游客','用户','管理员'].includes(value.identity)?value.identity:'管理员',lang:value.lang==='en'?'en':'zh-CN',appearance:['跟随系统','浅色','深色'].includes(value.appearance)?value.appearance:'跟随系统',profile:{...profile(),provider:value.profile?.provider==='QQ'?'QQ':'OIDC',bindings:{OIDC:true,QQ:!!value.profile?.bindings?.QQ}}};
  }
  let demoState=cleanDemo();
  try {const saved=JSON.parse(localStorage.getItem(demoKey));if(saved?.version===2)demoState=cleanDemo(saved);}catch{}
  const demo={read:()=>structuredClone(demoState),write(patch){demoState=cleanDemo({...demoState,...patch});try{localStorage.setItem(demoKey,JSON.stringify(demoState));}catch{}},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn);}};
  window.addEventListener('storage',event=>{if(event.key!==demoKey)return;try{demoState=cleanDemo(JSON.parse(event.newValue)||{});listeners.forEach(fn=>fn(demo.read()));}catch{}});
  function personSummary({id,name,letter,action=''}){
    return `<div class="d-person">${avatar(letter)}<div class="d-person-main"><strong>${esc(name)}</strong><code class="d-account-id" aria-label="ID">${esc(id)}</code></div>${action}</div>`;
  }
  function bindingRows({bindings,names={},bind=false,lang='zh-CN'}){
    const t=key=>(words[lang]||words['zh-CN'])[key];
    return `<div class="d-bindings">${['QQ','OIDC'].map(provider=>{const linked=!!bindings[provider],name=names[provider]||(provider==='QQ'?'沐风':'TomyJan');return `<div class="d-bind">${avatar(linked?name.slice(0,1):'·')}<div class="d-bind-info"><p>${provider}</p>${linked?`<p class="d-secondary">${esc(name)}</p>`:''}</div><div class="d-bind-action">${linked?`<span>${t('bound')}</span>`:bind?button(t('bind'),'bind','d-ghost',`data-provider="${provider}"`):`<span>${lang==='en'?'Not linked':'未绑定'}</span>`}</div></div>`;}).join('')}</div>`;
  }
  function icons(scope){
    if(!globalThis.lucide)return;
    // Initialize only new placeholders. Replacing existing SVGs interrupts chevron transitions.
    scope.querySelectorAll('i[data-lucide]').forEach(el=>{el.setAttribute('data-donate-icon',el.getAttribute('data-lucide'));el.removeAttribute('data-lucide');});
    lucide.createIcons({nameAttr:'data-donate-icon',attrs:{width:18,height:18}});
    scope.querySelectorAll('svg[data-donate-icon],svg[data-lucide]').forEach(el=>{el.removeAttribute('data-donate-icon');el.removeAttribute('data-lucide');});
  }
  const reduced=()=>matchMedia('(prefers-reduced-motion:reduce)').matches;
  function animate(el,frames,duration=200){return !el||reduced()?Promise.resolve():el.animate(frames,{duration,easing:'cubic-bezier(.2,.75,.25,1)'}).finished.catch(()=>{});}
  function option({label,action,value='',leading='',selected=null,role,attributes='',classes=''}){
    role??=selected===null?'menuitem':'menuitemradio';
    return `<button type="button" class="d-option ${classes}" role="${role}" ${selected!==null?`${role==='option'?'aria-selected':'aria-checked'}="${selected}"`:''} ${action?`data-shell-act="${action}"`:''} ${value?`data-value="${esc(value)}"`:''} ${attributes}>${leading}<span>${esc(label)}</span>${selected?`<span class="d-tick">${icon('check')}</span>`:''}</button>`;
  }
  function createShell(app,config){
    let menu=null,dialog=null,returnFocus=null,binding=false;
    const context=()=>config.context(),t=key=>(words[context().lang]||words['zh-CN'])[key];
    const themeIcon=()=>context().appearance==='深色'?'moon':context().appearance==='浅色'?'sun':'monitor';
    const nickname=()=>context().profile.provider==='QQ'?'沐风':'TomyJan';
    const letter=()=>context().profile.provider==='QQ'?'沐':'T';
    function header(){const c=context();return `<header class="d-top" data-component="site-header"><div class="d-identity"><a class="d-brand" href="${location.protocol==='file:'?'index.html':'/'}"><img src="${logoPreview}" data-source="https://public-cdn.tomys.top/head.png" width="30" height="30" alt=""><span>${t('donate')} TomyJan</span></a>${c.adminPage?`<span class="d-admin-label">${t('admin')}</span>`:''}</div><nav class="d-nav" aria-label="${c.lang==='en'?'Preferences and account':'偏好与账号'}">${button(`<span class="d-language">${c.lang==='en'?'EN':'中'}</span>`,'language','d-icon d-ghost',`aria-label="${t('language')}" aria-haspopup="menu" aria-expanded="false"`)}${button(icon(themeIcon()),'theme','d-icon d-ghost',`aria-label="${t('theme')}" aria-haspopup="menu" aria-expanded="false"`)}${c.signed?button(`<span class="d-nickname">${nickname()}</span>${avatar(letter())}`,'profile','d-profile d-ghost',`aria-label="${t('account')}" aria-haspopup="menu" aria-expanded="false"`):button(icon('user-round'),'login','d-icon d-ghost',`aria-label="${t('login')}"`)}</nav></header>`;}
    function footer(build=globalThis.DonateDesignBuild){
      const version=`V${build.version}${build.onTag?'':'-'+build.commit.slice(0,7)}`;
      const url=build.onTag?`https://github.com/TomyJan/Donate/releases/tag/${encodeURIComponent(build.tag)}`:`https://github.com/TomyJan/Donate/commit/${build.commit}`;
      return `<footer class="d-footer" data-component="site-footer"><a href="https://github.com/TomyJan/Donate" target="_blank" rel="noopener noreferrer">Donate</a> <a href="${esc(url)}" target="_blank" rel="noopener noreferrer">${esc(version)}</a></footer>`;
    }
    function mount(){for(const cls of ['d-pop-host','d-modal-host'])if(!app.querySelector('.'+cls)){const el=document.createElement('div');el.className=cls;app.append(el);}icons(app);}
    function inert(value){app.querySelectorAll(':scope > .d-top,:scope > main,:scope > .a-layout,:scope > .d-footer').forEach(el=>el.inert=value);}
    async function closeMenu(focus=true,immediate=false){if(!menu)return;const old=menu;menu=null;old.trigger.setAttribute('aria-expanded','false');old.el.inert=true;if(focus&&old.trigger.isConnected)old.trigger.focus({preventScroll:true});old.el.style.opacity='0';if(!immediate)await animate(old.el,[{opacity:1,transform:'translate3d(0,0,0)'},{opacity:0,transform:'translate3d(0,-4px,0)'}],130);old.el.remove();}
    function reset(){closeMenu(false,true);dialog?.remove();dialog=null;binding=false;inert(false);}
    function positionMenu(el,trigger){const ar=app.getBoundingClientRect(),tr=trigger.getBoundingClientRect(),width=Math.min(204,ar.width-24);el.style.width=width+'px';el.style.maxHeight=Math.max(160,Math.min(480,innerHeight-Math.max(0,ar.top)-24))+'px';let top=tr.bottom-ar.top+8;if(tr.bottom+8+el.offsetHeight>innerHeight&&tr.top-el.offsetHeight>8)top=tr.top-ar.top-el.offsetHeight-8;el.style.left=Math.max(12,Math.min(ar.width-width-12,tr.right-ar.left-width))+'px';el.style.top=Math.max(8,Math.min(top,innerHeight-ar.top-el.offsetHeight-12))+'px';}
    function openMenu(kind,trigger){
      if(menu?.kind===kind){closeMenu();return;}
      config.beforeOpen?.();closeMenu(false,true);let html='';const c=context();
      if(kind==='language')html=option({label:'简体中文',action:'set-language',value:'zh-CN',selected:c.lang==='zh-CN',leading:'<span class="d-language">中</span>'})+option({label:'English',action:'set-language',value:'en',selected:c.lang==='en',leading:'<span class="d-language">EN</span>'});
      if(kind==='theme')html=[['跟随系统','system','monitor'],['浅色','light','sun'],['深色','dark','moon']].map(([value,key,i])=>option({label:t(key),action:'set-theme',value,selected:c.appearance===value,leading:icon(i)})).join('');
      if(kind==='profile')html=option({label:t('account'),action:'account',leading:icon('user-round')})+(c.admin?option({label:t('admin'),action:'admin',leading:icon('settings'),selected:!!c.adminPage}):'')+'<hr>'+option({label:t('logout'),action:'logout',leading:icon('log-out')});
      const el=document.createElement('div');el.className='d-pop';el.setAttribute('role','menu');el.setAttribute('aria-label',trigger.getAttribute('aria-label'));el.innerHTML=html;app.querySelector('.d-pop-host').append(el);menu={el,trigger,kind};trigger.setAttribute('aria-expanded','true');icons(app);positionMenu(el,trigger);animate(el,[{opacity:0,transform:'translate3d(0,-6px,0)'},{opacity:1,transform:'translate3d(0,0,0)'}]);el.querySelector('[aria-checked=true],button')?.focus({preventScroll:true});
    }
    function positionDialog(){if(!dialog)return;const rect=app.getBoundingClientRect(),base=app.clientWidth<=680?86:110,top=Math.max(base,-rect.top+18);dialog.style.paddingTop=top+'px';const panel=dialog.querySelector('.d-dialog'),y=panel.getBoundingClientRect().top;panel.style.maxHeight=Math.max(200,innerHeight-y-18)+'px';}
    function openDialog(title,html){config.beforeOpen?.();closeMenu(false,true);if(!dialog)returnFocus=document.activeElement;dialog?.remove();const el=document.createElement('div');el.className='d-overlay';el.innerHTML=dialogFrame({title,body:html,panelClass:'d-dialog',headClass:'d-dialog-head',closeButton:button(icon('x'),'close','d-ghost d-icon',`aria-label="${t('close')}"`)});app.querySelector('.d-modal-host').append(el);dialog=el;inert(true);icons(app);positionDialog();animate(el,[{opacity:0},{opacity:1}],160);animate(el.firstElementChild,[{opacity:0,transform:'translate3d(0,12px,0)'},{opacity:1,transform:'translate3d(0,0,0)'}],240);el.querySelector('input,button')?.focus({preventScroll:true});}
    async function closeDialog(){if(!dialog)return;const old=dialog;dialog=null;binding=false;old.inert=true;old.style.opacity='0';await animate(old,[{opacity:1},{opacity:0}],150);old.remove();if(!dialog){inert(false);(returnFocus?.isConnected&&!returnFocus.closest('[inert]')?returnFocus:app.querySelector('[data-shell-act=profile],[data-shell-act=login]'))?.focus({preventScroll:true});}}
    function account(){const c=context();openDialog(t('account'),personSummary({id:c.profile.id,name:nickname(),letter:letter()})+bindingRows({bindings:c.profile.bindings,bind:true,lang:c.lang}));}
    function login(){binding=false;openDialog(t('login'),`<div class="d-row">${button('QQ','qq','d-primary')}${button('OIDC','oidc','')}</div><p class="d-demo">${t('loginDemo')}</p>`);}
    function qq(){openDialog(t('qq'),`<label class="d-sr" for="${app.id||app.parentElement.id}-qq">${t('qq')}</label><input id="${app.id||app.parentElement.id}-qq" class="d-input" name="shell-qq" inputmode="numeric" autocomplete="off" placeholder="${t('qq')}">${button(t('continue'),'qq-finish','d-primary d-qq-submit','disabled')}<p class="d-demo">${t('qqDemo')}</p>`);}
    function finish(provider){const p=context().profile;p.bindings[provider]=true;if(binding){binding=false;config.onBinding?.(provider);account();}else{p.provider=provider;reset();config.onLogin?.(provider);} }
    app.addEventListener('click',async e=>{
      if(e.target===dialog){e.stopImmediatePropagation();closeDialog();return;}
      const b=e.target.closest('button[data-shell-act]');if(!b||b.disabled)return;e.stopImmediatePropagation();
      const act=b.dataset.shellAct;
      if(['language','theme','profile'].includes(act)){openMenu(act,b);return;}
      switch(act){
        case 'set-language':case 'set-theme':{const kind=act==='set-language'?'lang':'appearance',value=b.dataset.value;await closeMenu();config.onPreference(kind,value);app.querySelector(`[data-shell-act=${kind==='lang'?'language':'theme'}]`)?.focus({preventScroll:true});break;}
        case 'account':account();break;
        case 'login':login();break;
        case 'logout':reset();config.onLogout?.();break;
        case 'admin':await closeMenu();if(!context().adminPage)config.onAdmin?.();break;
        case 'close':closeDialog();break;
        case 'bind':binding=true;if(b.dataset.provider==='QQ')qq();else finish('OIDC');break;
        case 'qq':qq();break;
        case 'oidc':finish('OIDC');break;
        case 'qq-finish':if(/^[1-9]\d{4,11}$/.test(dialog.querySelector('[name=shell-qq]').value))finish('QQ');break;
      }
    },true);
    // The same input constraint and caret restoration are used for QQ binding on both pages.
    const snapshots=new WeakMap(),accepts=value=>value===''||/^[1-9]\d{0,11}$/.test(value);
    const target=e=>e.target.matches?.('[name=shell-qq]');
    const capture=el=>snapshots.set(el,{value:el.value,start:el.selectionStart,end:el.selectionEnd});
    const candidate=(el,text)=>el.value.slice(0,el.selectionStart)+text+el.value.slice(el.selectionEnd);
    app.addEventListener('focusin',e=>{if(target(e))capture(e.target);});
    app.addEventListener('select',e=>{if(target(e))capture(e.target);});
    app.addEventListener('beforeinput',e=>{if(!target(e))return;capture(e.target);if(e.data!==null&&e.inputType.startsWith('insert')&&!accepts(candidate(e.target,e.data)))e.preventDefault();});
    app.addEventListener('paste',e=>{if(target(e)&&!accepts(candidate(e.target,e.clipboardData.getData('text'))))e.preventDefault();});
    app.addEventListener('input',e=>{if(!target(e))return;const el=e.target;if(!accepts(el.value)){const s=snapshots.get(el)||{value:'',start:0,end:0};el.value=s.value;el.setSelectionRange(s.start,s.end);}else capture(el);dialog.querySelector('[data-shell-act=qq-finish]').disabled=!/^[1-9]\d{4,11}$/.test(el.value);});
    app.addEventListener('keydown',e=>{
      if(!menu&&!dialog)return;
      if(e.key==='Escape'){e.preventDefault();e.stopImmediatePropagation();menu?closeMenu():closeDialog();return;}
      if(menu&&['ArrowDown','ArrowUp','Home','End'].includes(e.key)){e.preventDefault();e.stopImmediatePropagation();const items=[...menu.el.querySelectorAll('button')],i=items.indexOf(document.activeElement);items[e.key==='Home'?0:e.key==='End'?items.length-1:(i+(e.key==='ArrowDown'?1:-1)+items.length)%items.length]?.focus({preventScroll:true});}
      if(e.key==='Tab'&&menu){const trigger=menu.trigger;closeMenu(false,true);trigger.focus({preventScroll:true});}
      if(e.key==='Enter'&&target(e)){e.preventDefault();e.stopImmediatePropagation();dialog.querySelector('[data-shell-act=qq-finish]').click();}
      if(e.key==='Tab'&&dialog){e.stopImmediatePropagation();const items=[...dialog.querySelectorAll('button,input,a[href]')].filter(el=>!el.disabled),first=items[0],last=items.at(-1);if(!dialog.contains(document.activeElement)){e.preventDefault();first?.focus();}else if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus();}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus();}}
    },true);
    document.addEventListener('pointerdown',e=>{if(menu&&!menu.el.contains(e.target)&&!menu.trigger.contains(e.target))closeMenu(false);});
    window.addEventListener('resize',()=>{positionDialog();if(menu)positionMenu(menu.el,menu.trigger);});
    window.addEventListener('scroll',positionDialog,{passive:true});
    function layout(content,build){return header()+content+footer(build)+'<div class="d-pop-host"></div><div class="d-modal-host"></div>';}
    return {header,footer,layout,mount,reset,account,login,closeMenu,closeDialog,refreshHeader(){const old=app.querySelector('.d-top');if(old){old.outerHTML=header();icons(app);}}};
  }
  globalThis.DonatePrototype={createShell,profile,demo,personSummary,bindingRows,icons,option,control,dialogFrame,avatar,esc,icon,animate};
})();
