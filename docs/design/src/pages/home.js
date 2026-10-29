(() => {
  const root=document.getElementById('receipt-refinement'),app=root.querySelector('.r-app');
  const session=DonatePrototype.demo.read();
  const settings={appearance:session.appearance,device:'自适应',identity:session.identity};
  const identity=session.profile;
  const s={amount:'20.26',message:'',channel:'alipay',lang:session.lang,mine:false,selected:0,limit:4,provider:identity.provider,bindings:identity.bindings,resume:null,slipOpen:root.clientWidth>680};
  const userId=identity.id;
  const samples=[
    ['小林','林','20.26','广东','10.29 14:26','一点支持，继续创作。',false,'paid'],
    ['TomyJan','T','50.00','浙江','10.29 13:49','谢谢你整理这些资料。\n最近刚好用上，省了不少时间。\n请喝一杯咖啡。\n以后也会常来看看。',true,'paid'],
    ['阿澈','澈','8.88','北京','10.29 11:28','请喝咖啡。',false,'paid'],
    ['远山','山','128.00','四川','10.28 20:12','',false,'paid'],
    ['Mori','M','256.00','江苏','10.28 18:31','',false,'paid'],
    ['TomyJan','T','20.26','浙江','10.28 16:00','下次见。',true,'paid'],
    ['夏目','夏','8.88','福建','10.28 14:12','',false,'paid'],
    ['安安','安','648.00','湖北','10.28 12:38','谢谢。',false,'paid'],
    ['渡','渡','0.01','山东','10.27 20:10','',false,'paid'],
    ['月见','月','128.00','湖南','10.27 18:00','一点心意。',false,'paid'],
    ['TomyJan','T','8.88','浙江','10.29 14:30','请喝一杯。',true,'pending'],
    ['TomyJan','T','20.26','浙江','10.27 17:20','',true,'closed'],
    ['初见','初','50.00','安徽','10.27 16:00','',false,'paid'],
    ['小鱼','鱼','8.88','上海','10.27 15:00','加油。',false,'paid']
  ].map((v,i)=>({id:`019a2e54-ff40-7000-8000-${String(i+1).padStart(12,'0')}`,name:v[0],avatar:v[1],amount:v[2],province:v[3],date:v[4],message:v[5],mine:v[6],status:v[7],channel:i%2?'wechat':'alipay'}));
  const words={
    'zh-CN':{donate:'捐赠',records:'捐赠记录',mine:'只看我的',more:'更多',guestMore:'登录后可查看更多',end:'没有更多了',message:'留言（选填）',amount:'金额（元）',previous:'上一张',next:'下一张',language:'切换语言',theme:'切换外观',system:'跟随系统',light:'浅色',dark:'深色',account:'账号信息',admin:'站点管理',logout:'退出登录',login:'登录',close:'关闭',pay:'捐赠 · ',alipay:'支付宝',wechat:'微信',channel:'选择支付方式',foot:'付款后，留言将随捐赠记录公开。',paid:'已完成',pending:'待支付',closed:'已关闭',continue:'继续支付',details:'订单详情',bound:'已绑定',bind:'绑定',binding:'绑定账号',uuid:'账号 ID',copy:'复制',qq:'QQ 号',finish:'继续',qqError:'请输入正确的 QQ 号',formatDemo:'演示：仅检查号码格式，未验证 QQ 身份。',loginDemo:'演示登录，不连接第三方服务。',paymentDemo:'支付演示，不创建订单、不扣款。',amountRule:'金额 0.01–100,000 元，最多两位小数。',sampleEnd:'示例记录已全部展示。'},
    en:{donate:'Donate',records:'Donations',mine:'Only mine',more:'More',guestMore:'Sign in to view more',end:'No more donations',message:'Message (optional)',amount:'Amount (CNY)',previous:'Previous receipt',next:'Next receipt',language:'Language',theme:'Appearance',system:'System',light:'Light',dark:'Dark',account:'Account',admin:'Site admin',logout:'Sign out',login:'Sign in',close:'Close',pay:'Donate · ',alipay:'Alipay',wechat:'WeChat Pay',channel:'Payment method',foot:'Your message is published with your paid donation.',paid:'Paid',pending:'Pending',closed:'Closed',continue:'Continue payment',details:'Order details',bound:'Linked',bind:'Link',binding:'Link account',uuid:'Account ID',copy:'Copy',qq:'QQ number',finish:'Continue',qqError:'Enter a valid QQ number',formatDemo:'Demo: number format only; QQ identity is not verified.',loginDemo:'Demo sign-in. No third-party connection.',paymentDemo:'Payment preview. No order or charge.',amountRule:'CNY 0.01–100,000; up to two decimal places.',sampleEnd:'All sample records are shown.'}
  };
  const t=k=>words[s.lang][k],icon=name=>`<i data-lucide="${name}" aria-hidden="true"></i>`;
  const esc=v=>String(v).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const avatar=v=>DonatePrototype.avatar(v,'r-avatar');
  const button=(label,act,cls='r-ghost',extra='')=>DonatePrototype.control(label,act,'r',cls,extra);
  const signed=()=>settings.identity!=='游客';
  const name=()=>s.provider==='QQ'?'沐风':'TomyJan';
  const themeIcon=()=>settings.appearance==='深色'?'moon':settings.appearance==='浅色'?'sun':'monitor';
  const reduced=()=>matchMedia('(prefers-reduced-motion: reduce)').matches;
  function animate(el,frames,duration=200){if(!el||reduced())return Promise.resolve();return el.animate(frames,{duration,easing:'cubic-bezier(.2,.75,.25,1)',fill:'none'}).finished.catch(()=>{});}
  function icons(){DonatePrototype.icons(app);}
  function remember(){DonatePrototype.demo.write({identity:settings.identity,lang:s.lang,appearance:settings.appearance,profile:{id:userId,provider:s.provider,bindings:s.bindings}});}
  const shell=DonatePrototype.createShell(app,{
    context:()=>({lang:s.lang,appearance:settings.appearance,signed:signed(),admin:settings.identity==='管理员',adminPage:false,profile:{id:userId,provider:s.provider,bindings:s.bindings}}),
    beforeOpen:()=>{closeMenu(false);if(modal){modal.remove();modal=null;setInert(false);}},
    onPreference:(key,value)=>{if(key==='lang'){s.lang=value;render();}else{settings.appearance=value;appearance();shell.refreshHeader();}remember();},
    onLogin:provider=>loggedIn(provider),onBinding:()=>remember(),
    onLogout:()=>{settings.identity='游客';s.mine=false;s.selected=0;s.limit=4;render();remember();},
    onAdmin:()=>location.assign(location.protocol==='file:'?'admin.html':'/admin')
  });
  function cents(value){if(!/^(?:0|[1-9]\d*)(?:\.\d{1,2})?$/.test(value))return null;const [whole,fraction='']=value.split('.');return Number(whole)*100+Number(fraction.padEnd(2,'0'));}
  function editable(value){return value===''||(/^(?:0|[1-9]\d*)(?:\.\d{0,2})?$/.test(value)&&value.length<=9&&(cents(value.endsWith('.')?value+'0':value)??Infinity)<=10000000);}
  function valid(value){const n=cents(value);return n!==null&&n>=1&&n<=10000000;}
  function composer(){return `<details class="r-write" ${s.slipOpen?'open':''}><summary>${t('donate')}${icon('plus')}</summary><div class="r-ticket"><div class="r-compose-title"><h2>${t('donate')}</h2></div><label class="r-sr" for="r-amount">${t('amount')}</label><div class="r-money"><span aria-hidden="true">¥</span><input id="r-amount" name="amount" inputmode="decimal" autocomplete="off" value="${esc(s.amount)}" aria-describedby="r-amount-help"></div><span id="r-amount-help" class="r-sr">${t('amountRule')}</span><div class="r-presets" aria-label="${s.lang==='en'?'Quick amounts':'快捷金额'}">${['0.01','8.88','20.26','50','128','256','648'].map(p=>`<button type="button" class="r-preset" data-amount="${p}" aria-pressed="${cents(s.amount)===cents(p)}">${p}</button>`).join('')}</div><label class="r-sr" for="r-message">${t('message')}</label><div class="r-message-field"><textarea id="r-message" class="r-message" name="message" rows="2" placeholder="${t('message')}">${esc(s.message)}</textarea></div><div class="r-pay">${button(t('pay')+t(s.channel),'pay','r-primary',valid(s.amount)?'':'disabled')}${button(icon('chevron-down'),'channel','r-icon r-channel',`aria-label="${t('channel')}" aria-haspopup="menu" aria-expanded="false"`)}</div><p class="r-ticket-foot">${t('foot')}</p></div></details>`;}
  function allRecords(){return samples.filter(o=>s.mine?signed()&&o.mine:o.status==='paid').sort((a,b)=>b.date.localeCompare(a.date));}
  function records(){return allRecords().slice(0,s.limit);}
  function receipt(o){return `<article class="r-ticket r-featured" aria-label="${esc((o.mine?name():o.name)+' '+t('records'))}"><div class="r-person">${avatar(o.mine?(s.provider==='QQ'?'沐':'T'):o.avatar)}<strong>${esc(o.mine?name():o.name)}</strong>${s.mine&&o.status!=='paid'?`<span class="r-status">${t(o.status)}</span>`:''}</div><div class="r-receipt-value"><div class="r-receipt-amount">¥${o.amount}</div>${s.mine?`<div class="r-receipt-action">${button(t('details'),'details','r-ghost')}${o.status==='pending'?button(t('continue'),'continue','r-primary'):''}</div>`:''}</div>${o.message?`<p class="r-note">${esc(o.message)}</p>`:''}<div class="r-meta"><span>${o.province}</span><time>${o.date}</time></div></article>`;}
  function indexRows(){return records().map((o,i)=>`<button type="button" class="r-index-row" data-receipt="${i}" aria-pressed="${i===s.selected}"><span class="r-index-label">${s.mine?`<time>${esc(o.date)}</time>`:`<span>${esc(o.mine?name():o.name)}</span><time class="r-index-time">${esc(o.date)}</time>`}${s.mine&&o.status!=='paid'?`<span class="r-index-status">${t(o.status)}</span>`:''}</span><span class="r-index-money">¥${o.amount}</span></button>`).join('');}
  function moreButton(){return button(!signed()?t('guestMore'):s.limit>=allRecords().length?t('end'):t('more'),'more','r-ghost',signed()&&s.limit>=allRecords().length?'disabled':'');}
  function pageButtons(){return `${button(icon('arrow-left'),'previous','r-ghost r-icon',`aria-label="${t('previous')}" ${s.selected===0?'disabled':''}`)}${button(icon('arrow-right'),'next','r-ghost r-icon',`aria-label="${t('next')}" ${s.selected>=records().length-1?'disabled':''}`)}`;}
  function book(){const list=records();s.selected=Math.min(s.selected,Math.max(0,list.length-1));return `<section class="r-book" aria-label="${t('records')}"><div class="r-book-head"><div class="r-book-title"><h2>${t('records')}</h2>${button(t('mine'),'mine','r-ghost r-filter',`aria-pressed="${s.mine}"`)}</div><div class="r-page-nav">${pageButtons()}</div></div><div class="r-stack" aria-live="polite">${list.length?receipt(list[s.selected]):`<p>${t('end')}</p>`}</div><div class="r-index">${indexRows()}</div><div class="r-more">${moreButton()}</div></section>`;}
  let menu=null,modal=null,returnFocus=null,receiptGeneration=0;
  function appearance(){app.style.colorScheme=settings.appearance==='深色'?'dark':settings.appearance==='浅色'?'light':'light dark';app.style.maxWidth=settings.device==='手机'?'390px':'100%';}
  function render(){shell.reset();menu?.el.remove();menu=null;modal?.remove();modal=null;app.lang=s.lang;appearance();app.innerHTML=`${shell.layout(`<main class="r-main">${composer()}${book()}</main>`)}<div class="r-pop-host"></div><div class="r-modal-host"></div>`;icons();shell.mount();updateAmount();layout(false);}
  function updateAmount(){const el=app.querySelector('[name="amount"]');if(!el)return;el.style.fontSize=el.value.length>8?'34px':el.value.length>6?'42px':'';app.querySelector('[data-act="pay"]').disabled=!valid(s.amount);app.querySelectorAll('[data-amount]').forEach(b=>b.setAttribute('aria-pressed',String(cents(s.amount)!==null&&cents(b.dataset.amount)===cents(s.amount))));}
  const heightMotions=new WeakMap();
  function setHeight(el,target,smooth=true){
    if(!el)return Promise.resolve();
    const from=el.getBoundingClientRect().height;
    heightMotions.get(el)?.animation.cancel();
    heightMotions.delete(el);
    el.style.height=target+'px';
    if(!smooth||reduced()||Math.abs(from-target)<.5)return Promise.resolve();
    const animation=el.animate([{height:from+'px'},{height:target+'px'}],{duration:320,easing:'cubic-bezier(.2,.75,.25,1)'});
    heightMotions.set(el,{animation,target});
    return animation.finished.catch(()=>{}).then(()=>{if(heightMotions.get(el)?.animation===animation)heightMotions.delete(el);});
  }
  function heightDelta(el){const motion=heightMotions.get(el);return motion?motion.target-el.getBoundingClientRect().height:0;}
  function fitMessage(smooth=true){
    const input=app.querySelector('[name="message"]'),ticket=app.querySelector('.r-write .r-ticket'),bookEl=app.querySelector('.r-book');
    if(!input?.getClientRects().length)return;
    const current=input.getBoundingClientRect().height,css=getComputedStyle(input),line=parseFloat(css.lineHeight),padding=parseFloat(css.paddingTop)+parseFloat(css.paddingBottom);
    const base=ticket.offsetHeight-current;
    const delta=heightDelta(app.querySelector('.r-stack'))+heightDelta(app.querySelector('.r-index'));
    const summary=app.clientWidth<=680?app.querySelector('.r-write > summary').offsetHeight:0;
    const cap=app.clientWidth>680?app.querySelector('.r-more').getBoundingClientRect().bottom-ticket.getBoundingClientRect().top+delta-7:bookEl.offsetHeight+delta-summary-7;
    const maximum=Math.max(line*2+padding,cap-base);
    heightMotions.get(input)?.animation.cancel();heightMotions.delete(input);
    input.style.height='0px';
    const wanted=Math.max(line*2+padding,input.scrollHeight+line);
    input.style.height=current+'px';
    input.style.overflowY=wanted>maximum+.5?'auto':'hidden';
    if(wanted<=maximum)input.scrollTop=0;
    setHeight(input,Math.min(wanted,maximum),smooth);
  }
  function layout(smooth=true){
    const featured=app.querySelector('.r-featured:not(.r-leaving)');
    if(featured){fitReceiptAmount(featured);setHeight(app.querySelector('.r-stack'),featured.offsetHeight,smooth);}
    fitMessage(smooth);
  }
  function fitReceiptAmount(featured){
    const amount=featured.querySelector('.r-receipt-amount'),actions=featured.querySelector('.r-receipt-action');
    amount.style.fontSize='';const size=parseFloat(getComputedStyle(amount).fontSize);
    // Keep type stable while the host adds/removes its scrollbar during height changes.
    amount.style.fontSize=Math.min(size,Math.floor((actions?220:360)/amount.textContent.length))+'px';
  }
  let slipGeneration=0;
  async function toggleSlip(){
    const slip=app.querySelector('.r-write'),ticket=slip.querySelector('.r-ticket'),opening=!s.slipOpen,gen=++slipGeneration;
    const from=slip.getBoundingClientRect().height;
    heightMotions.get(slip)?.animation.cancel();heightMotions.delete(slip);
    slip.dataset.animating='true';slip.style.height='';slip.open=true;s.slipOpen=opening;ticket.inert=!opening;
    fitMessage(false);const target=opening?slip.offsetHeight:slip.querySelector('summary').offsetHeight;
    slip.style.height=from+'px';slip.style.overflow='hidden';
    await setHeight(slip,target);
    if(gen!==slipGeneration)return;
    slip.open=opening;slip.style.height='';slip.style.overflow='';delete slip.dataset.animating;ticket.inert=false;
  }
  function refreshBook(){
    s.selected=Math.min(s.selected,Math.max(0,records().length-1));
    app.querySelector('[data-act="mine"]').setAttribute('aria-pressed',String(s.mine));
    const index=app.querySelector('.r-index'),from=index.getBoundingClientRect().height;
    heightMotions.get(index)?.animation.cancel();heightMotions.delete(index);
    index.style.height='auto';index.innerHTML=indexRows();const target=index.offsetHeight;
    index.style.height=from+'px';setHeight(index,target);
    app.querySelector('.r-more').innerHTML=moreButton();
    selectReceipt(s.selected,true);
  }
  async function selectReceipt(index,force=false){
    const list=records();if(index<0||index>=list.length||(!force&&index===s.selected))return;
    const direction=index>=s.selected?1:-1;s.selected=index;
    const gen=++receiptGeneration,stack=app.querySelector('.r-stack');
    stack.querySelectorAll('.r-leaving').forEach(e=>e.remove());
    const old=stack.querySelector('.r-featured'),oldStyle=old?getComputedStyle(old):null;
    const oldTransform=oldStyle?.transform,oldOpacity=oldStyle?.opacity;
    old?.getAnimations().forEach(a=>a.cancel());
    // Persist the exit endpoint: its 220ms animation finishes before the 320ms arrival/height animations.
    if(old){old.classList.add('r-leaving');old.style.opacity='0';old.inert=true;old.setAttribute('aria-hidden','true');}
    stack.insertAdjacentHTML('beforeend',receipt(list[index]));const next=stack.lastElementChild;
    icons();fitReceiptAmount(next);
    app.querySelectorAll('[data-receipt]').forEach(b=>b.setAttribute('aria-pressed',String(Number(b.dataset.receipt)===index)));
    app.querySelector('[data-act="previous"]').disabled=index===0;
    app.querySelector('[data-act="next"]').disabled=index===list.length-1;
    const size=setHeight(stack,next.offsetHeight);fitMessage();
    await Promise.all([size,animate(old,[{opacity:oldOpacity,transform:oldTransform},{opacity:0,transform:`rotate(${direction*7}deg) translate(${direction*25}px,-18px)`}],220),animate(next,[{opacity:.35,transform:`rotate(${-direction*3}deg) translate(${-direction*10}px,10px)`},{opacity:1,transform:'rotate(2deg)'}],320)]);
    old?.remove();if(gen===receiptGeneration)remember();
  }
  async function closeMenu(focus=true){if(!menu)return;const current=menu;menu=null;current.trigger.setAttribute('aria-expanded','false');current.el.inert=true;if(focus&&current.trigger.isConnected)current.trigger.focus();await animate(current.el,[{opacity:1,transform:'translate3d(0,0,0)'},{opacity:0,transform:'translate3d(0,-4px,0)'}],130);current.el.remove();}
  function option(label,action,value,selected=false,leading=''){return DonatePrototype.option({label,selected,value,leading,attributes:`data-act="${action}"`,classes:'r-option'});}
  function openMenu(kind,trigger){if(menu?.kind===kind){closeMenu();return;}closeMenu(false);const el=document.createElement('div');el.className='d-pop';el.setAttribute('role','menu');el.setAttribute('aria-label',t('channel'));el.innerHTML=['alipay','wechat'].map(value=>option(t(value),'set-channel',value,s.channel===value,icon(value==='alipay'?'wallet':'message-circle'))).join('');app.querySelector('.r-pop-host').append(el);const ar=app.getBoundingClientRect(),tr=trigger.getBoundingClientRect(),width=Math.min(204,ar.width-24);el.style.width=width+'px';el.style.left=Math.max(12,Math.min(ar.width-width-12,tr.right-ar.left-width))+'px';el.style.top=tr.bottom-ar.top+8+'px';menu={el,trigger,kind};trigger.setAttribute('aria-expanded','true');icons();animate(el,[{opacity:0,transform:'translate3d(0,-6px,0)'},{opacity:1,transform:'translate3d(0,0,0)'}]);el.querySelector('[aria-checked=true]')?.focus({preventScroll:true});}
  function setInert(value){app.querySelectorAll('.d-top,.r-main,.d-footer').forEach(el=>el.inert=value);}
  function openDialog(title,body){closeMenu(false);if(!modal)returnFocus=document.activeElement;modal?.remove();const el=document.createElement('div');el.className='r-overlay';el.innerHTML=DonatePrototype.dialogFrame({title,body,panelClass:'r-dialog',headClass:'r-dialog-head',closeButton:button(icon('x'),'close','r-ghost r-icon',`aria-label="${t('close')}"`)});app.querySelector('.r-modal-host').append(el);modal=el;setInert(true);icons();animate(el,[{opacity:0},{opacity:1}],160);animate(el.querySelector('.r-dialog'),[{opacity:0,transform:'translateY(14px) scale(.98)'},{opacity:1,transform:'translateY(0) scale(1)'}],240);el.querySelector('input,button')?.focus();}
  async function closeDialog(){if(!modal)return;const el=modal;modal=null;el.inert=true;await Promise.all([animate(el,[{opacity:1},{opacity:0}],150),animate(el.querySelector('.r-dialog'),[{transform:'translateY(0) scale(1)'},{transform:'translateY(8px) scale(.98)'}],150)]);el.remove();if(!modal){setInert(false);(returnFocus?.isConnected&&!returnFocus.closest('[inert]')?returnFocus:app.querySelector('[data-shell-act="profile"],[data-shell-act="login"]'))?.focus();}}
  function login(resume=null){s.resume=resume;shell.login();}
  function loggedIn(provider){s.provider=provider;s.bindings[provider]=true;settings.identity='管理员';const resume=s.resume;s.resume=null;render();remember();if(resume==='pay')payment();if(resume==='mine'){s.mine=true;s.limit=4;s.selected=0;refreshBook();}}
  function payment(order=null){if(!signed()){login('pay');return;}if(!order&&!valid(s.amount))return;const value=order?.amount??s.amount,message=order?.message??s.message;openDialog(t('donate'),`<div class="r-payment-amount">¥${(cents(value)/100).toFixed(2)}</div><p>${t(order?.channel??s.channel)}</p>${message?`<p class="r-note">${esc(message)}</p>`:''}<p class="r-demo">${t('paymentDemo')}</p>${button(t('close'),'close','r-primary r-wide r-spaced')}`);}
  app.addEventListener('click',async e=>{
    if(e.target.closest('.r-write > summary')&&app.clientWidth<=680){e.preventDefault();toggleSlip();return;}
    if(e.target===modal){closeDialog();return;}const b=e.target.closest('button');if(!b||b.disabled)return;
    if(b.dataset.amount){s.amount=b.dataset.amount;const input=app.querySelector('[name="amount"]');input.value=s.amount;captureInput(input);updateAmount();return;}
    if(b.hasAttribute('data-receipt')){selectReceipt(Number(b.dataset.receipt));return;}
    switch(b.dataset.act){
      case 'channel':openMenu('channel',b);break;
      case 'set-channel':s.channel=b.dataset.value;app.querySelector('[data-act="pay"]').textContent=t('pay')+t(s.channel);await closeMenu();remember();break;
      case 'pay':payment();break;
      case 'previous':selectReceipt(s.selected-1);break;
      case 'next':selectReceipt(s.selected+1);break;
      case 'mine':if(!signed()){login('mine');}else{s.mine=!s.mine;s.selected=0;s.limit=4;refreshBook();remember();}break;
      case 'more':if(!signed())login();else{s.limit+=4;refreshBook();}break;
      case 'login':login();break;
      case 'close':closeDialog();break;
      case 'details':{const o=records()[s.selected];openDialog(t('details'),`<p class="r-small r-muted">${t(o.status)} · ${t(o.channel)}</p><div class="r-payment-amount r-spaced">¥${o.amount}</div><code class="r-uuid">${o.id}</code><p class="r-small r-muted r-spaced">${o.province} · ${o.date}</p>${o.message?`<p class="r-note">${esc(o.message)}</p>`:''}`);break;}
      case 'continue':payment(records()[s.selected]);break;
    }
  });
  const inputSnapshots=new WeakMap();
  const constrained=input=>input instanceof HTMLInputElement&&input.name==='amount';
  const accepts=(input,value)=>input.name==='amount'?editable(value):value===''||/^[1-9]\d{0,11}$/.test(value);
  function captureInput(input){if(!constrained(input))return;const previous=inputSnapshots.get(input);if(!previous||accepts(input,input.value))inputSnapshots.set(input,{value:input.value,start:input.selectionStart,end:input.selectionEnd});}
  function candidate(input,text){return input.value.slice(0,input.selectionStart)+text+input.value.slice(input.selectionEnd);}
  function enforceInput(input){
    if(!constrained(input))return;
    const snapshot=inputSnapshots.get(input)??{value:input.name==='amount'?s.amount:'',start:input.selectionStart,end:input.selectionEnd};
    if(!accepts(input,input.value)){
      input.value=snapshot.value;input.setSelectionRange(snapshot.start,snapshot.end);
    }else captureInput(input);
    if(input.name==='amount'){s.amount=input.value;updateAmount();}
  }
  app.addEventListener('focusin',e=>captureInput(e.target));
  app.addEventListener('beforeinput',e=>{if(!constrained(e.target))return;captureInput(e.target);if(e.data!==null&&e.inputType.startsWith('insert')&&!accepts(e.target,candidate(e.target,e.data)))e.preventDefault();});
  app.addEventListener('paste',e=>{if(!constrained(e.target))return;captureInput(e.target);const text=e.clipboardData?.getData('text')??'';if(!accepts(e.target,candidate(e.target,text)))e.preventDefault();});
  app.addEventListener('compositionstart',e=>captureInput(e.target));
  app.addEventListener('compositionend',e=>enforceInput(e.target));
  app.addEventListener('select',e=>captureInput(e.target));
  document.addEventListener('selectionchange',()=>captureInput(document.activeElement));
  app.addEventListener('input',e=>{enforceInput(e.target);if(e.target.name==='message'){s.message=e.target.value;fitMessage();}});
  app.addEventListener('toggle',e=>{if(e.target.classList.contains('r-write')&&!e.target.dataset.animating){s.slipOpen=e.target.open;if(e.target.open)fitMessage(false);}},true);
  app.addEventListener('keydown',e=>{
    if(e.key==='Escape'){e.preventDefault();modal?closeDialog():closeMenu();}
    if(menu&&['ArrowDown','ArrowUp','Home','End'].includes(e.key)){e.preventDefault();const items=[...menu.el.querySelectorAll('button')],i=items.indexOf(document.activeElement);items[e.key==='Home'?0:e.key==='End'?items.length-1:(i+(e.key==='ArrowDown'?1:-1)+items.length)%items.length].focus();}
    if(menu&&e.key==='Tab')closeMenu(false);
    if(e.key==='Enter'&&e.target.name==='amount'){e.preventDefault();if(valid(s.amount))payment();}
    if(e.key==='Tab'&&modal){const items=[...modal.querySelectorAll('button,input,textarea,a[href]')].filter(el=>!el.disabled),first=items[0],last=items.at(-1);if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus();}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus();}}
  });
  document.addEventListener('pointerdown',e=>{if(menu&&!menu.el.contains(e.target)&&!menu.trigger.contains(e.target))closeMenu(false);});
  app.addEventListener('submit',e=>e.preventDefault());
  render();
  DonatePrototype.demo.subscribe(next=>{settings.identity=next.identity;settings.appearance=next.appearance;s.lang=next.lang;s.provider=next.profile.provider;s.bindings=next.profile.bindings;if(!signed())s.mine=false;render();});
  document.fonts?.ready.then(()=>layout());
  document.fonts?.addEventListener('loadingdone',()=>layout());
  let compact=app.clientWidth<=680,lastWidth=app.clientWidth;
  new ResizeObserver(()=>{const width=app.clientWidth;if(width===lastWidth)return;lastWidth=width;const next=width<=680;if(next!==compact){compact=next;s.slipOpen=!next;app.querySelector('.r-write').open=s.slipOpen;}layout(false);if(menu)closeMenu(false);}).observe(app);
})();
