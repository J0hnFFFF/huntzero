(function(){const t=document.createElement("link").relList;if(t&&t.supports&&t.supports("modulepreload"))return;for(const s of document.querySelectorAll('link[rel="modulepreload"]'))l(s);new MutationObserver(s=>{for(const a of s)if(a.type==="childList")for(const i of a.addedNodes)i.tagName==="LINK"&&i.rel==="modulepreload"&&l(i)}).observe(document,{childList:!0,subtree:!0});function e(s){const a={};return s.integrity&&(a.integrity=s.integrity),s.referrerPolicy&&(a.referrerPolicy=s.referrerPolicy),s.crossOrigin==="use-credentials"?a.credentials="include":s.crossOrigin==="anonymous"?a.credentials="omit":a.credentials="same-origin",a}function l(s){if(s.ep)return;s.ep=!0;const a=e(s);fetch(s.href,a)}})();class r{constructor(){this.currentIndex=0,this.slides=[],this.totalSlides=0,this.viewport=document.getElementById("ppt-viewport"),this.prevBtn=document.getElementById("prevBtn"),this.nextBtn=document.getElementById("nextBtn"),this.progressBarFill=document.getElementById("progressBarFill"),this.pageIndicator=document.getElementById("pageIndicator"),this.init(),this.initWindowMessage()}init(){this.loadSlides(),this.bindEvents(),this.initializePage(),this.updateUI(),this.updateViewportScale()}initWindowMessage(){window.addEventListener("message",t=>{if(!t.data||typeof t.data!="object")return;const{type:e,data:l}=t.data;e==="childrenstart"?(this.prevBtn.style.visibility="hidden",this.nextBtn.style.visibility="hidden",this.progressBarFill.style.visibility="hidden",this.pageIndicator.style.visibility="hidden"):e==="childrenstop"&&(this.prevBtn.style.visibility="visible",this.nextBtn.style.visibility="visible",this.progressBarFill.style.visibility="visible",this.pageIndicator.style.visibility="visible")})}initializePage(){const t=new URLSearchParams(window.location.search);let e=t.get("page");if(!e){e="1",t.set("page","1");const a=`${window.location.pathname}?${t.toString()}`;window.history.replaceState({},"",a)}const l=parseInt(e,10),s=l-1;if(!isNaN(l)&&s>=0&&s<this.totalSlides)this.slides[0]&&this.slides[0].classList.remove("active"),this.currentIndex=s,this.slides[s]&&this.slides[s].classList.add("active");else{console.warn(`无效的页码参数: ${e}，将显示第 1 页`),t.set("page","1");const a=`${window.location.pathname}?${t.toString()}`;window.history.replaceState({},"",a)}}loadSlides(){if(typeof window.slideDataMap>"u"){console.error("未找到 slideDataMap");return}const t=Array.from(window.slideDataMap.keys()).sort((e,l)=>e-l);if(this.totalSlides=t.length,this.totalSlides===0){console.warn("slideDataMap 为空，没有幻灯片可加载");return}t.forEach((e,l)=>{const s=document.createElement("div");s.className="slide",l===0&&s.classList.add("active");const a=window.slideDataMap.get(e);if(!a||typeof a!="string"){this.totalSlides--,console.error(`未找到页码 ${e} 的内容, 或者页码 ${e} 的内容为空`);return}const i=document.createElement("div");i.innerHTML=a.trim(),s.appendChild(i),this.viewport.appendChild(s),this.slides.push(s)})}bindEvents(){this.prevBtn.addEventListener("click",()=>this.prevSlide()),this.nextBtn.addEventListener("click",()=>this.nextSlide()),document.addEventListener("keydown",e=>{e.key==="ArrowLeft"?this.prevSlide():e.key==="ArrowRight"||e.key===" "?(e.preventDefault(),this.nextSlide()):e.key==="Home"?this.goToSlide(0):e.key==="End"&&this.goToSlide(this.totalSlides-1)});let t=0;this.viewport.addEventListener("touchstart",e=>{t=e.touches[0].clientX}),this.viewport.addEventListener("touchend",e=>{const l=e.changedTouches[0].clientX,s=t-l;Math.abs(s)>50&&(s>0?this.nextSlide():this.prevSlide())}),window.addEventListener("resize",()=>this.updateViewportScale())}prevSlide(){this.currentIndex>0&&this.goToSlide(this.currentIndex-1)}nextSlide(){this.currentIndex<this.totalSlides-1&&this.goToSlide(this.currentIndex+1)}goToSlide(t){t<0||t>=this.totalSlides||(this.slides[this.currentIndex].classList.remove("active"),this.currentIndex=t,this.slides[this.currentIndex].classList.add("active"),this.updateUrlPage(t+1),this.updateUI())}updateUrlPage(t){const e=new URLSearchParams(window.location.search);e.set("page",t.toString());const l=`${window.location.pathname}?${e.toString()}`;window.history.replaceState({},"",l)}updateUI(){if(this.totalSlides===0){this.prevBtn.disabled=!0,this.nextBtn.disabled=!0,this.progressBarFill.style.width="0%",this.pageIndicator.textContent="制作中";return}this.prevBtn.disabled=this.currentIndex===0,this.nextBtn.disabled=this.currentIndex===this.totalSlides-1;const t=(this.currentIndex+1)/this.totalSlides*100;this.progressBarFill.style.width=`${t}%`,this.pageIndicator.textContent=`${this.currentIndex+1} / ${this.totalSlides}`}updateViewportScale(){const s=window.innerWidth-40,a=window.innerHeight-40,i=s/1440,d=a/810,o=Math.min(i,d,1);this.viewport.style.transform=`scale(${o})`,console.log(`窗口: ${window.innerWidth}x${window.innerHeight}, 缩放: ${o.toFixed(3)}`)}}class p{constructor(){this.validRoutes=["/","/index.html"],this.checkRoute()}checkRoute(){const t=window.location.pathname;if(t.includes("404.html"))return;this.validRoutes.some(l=>l==="/"?t==="/"||t==="/index.html":t===l)||(console.warn(`Invalid route detected: ${t}, redirecting to 404`),window.location.href="/404.html")}addRoute(t){this.validRoutes.includes(t)||this.validRoutes.push(t)}isValidRoute(t){return this.validRoutes.includes(t)}}window.addEventListener("DOMContentLoaded",()=>{new p,new r});window.slideDataMap.set(1,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="absolute top-0 left-0 bg-gradient-to-br from-[#1B2A4A] to-[#0F1B2D] flex items-center justify-center w-[1440px] h-[810px] relative overflow-hidden">
      <div class="absolute top-0 right-0 w-1/2 h-full bg-gradient-to-l from-[#3B82F6]/10 to-transparent"></div>
      <div class="absolute bottom-0 left-0 w-[500px] h-[500px] bg-[#3B82F6]/5 rounded-full blur-3xl"></div>
      <div class="relative z-10 max-w-[1100px] px-24">
        <div class="flex items-center gap-6 mb-12">
          <div class="w-[3px] h-24 bg-[#3B82F6]"></div>
          <div>
            <div class="text-[#3B82F6] text-base font-semibold tracking-[0.3em] mb-4" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">INTELLIGENT CODE SECURITY</div>
            <h1 class="text-[4.5rem] font-bold text-white leading-tight" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">zerodayFind</h1>
            <p class="text-[#3B82F6] text-[18px] font-semibold mt-3 tracking-[0.15em]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">猎零安全团队</p>
          </div>
        </div>
        <p class="text-3xl text-gray-100 mb-10 ml-9" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">智能代码安全平台</p>
        <div class="flex items-center gap-8 ml-9 text-gray-200 text-lg" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
          <div class="flex items-center gap-3">
            <div class="w-3 h-3 bg-[#3B82F6] rounded-full"></div>
            <span>发现真实漏洞</span>
          </div>
          <div class="w-[1px] h-5 bg-gray-600"></div>
          <span>降低安全风险</span>
          <div class="w-[1px] h-5 bg-gray-600"></div>
          <span>加速合规交付</span>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(2,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[45px] flex">
      <div class="w-1/3 bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6] flex items-center justify-center">
        <div class="text-white text-center">
          <h1 class="text-4xl font-bold mb-3" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">目录</h1>
          <div class="w-20 h-1 bg-white mx-auto"></div>
        </div>
      </div>
      <div class="flex-1 flex flex-col justify-center px-14">
        <div class="space-y-5">
          <div class="flex items-center gap-5 group cursor-pointer">
            <div class="w-14 h-14 bg-[#3B82F6]/10 group-hover:bg-[#3B82F6] transition-colors flex items-center justify-center">
              <span class="text-xl font-bold text-[#3B82F6] group-hover:text-white">01</span>
            </div>
            <div class="flex-1">
              <h3 class="text-xl font-semibold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">安全挑战</h3>
              <p class="text-sm text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">漏洞的代价与传统工具局限</p>
            </div>
          </div>
          <div class="flex items-center gap-5 group cursor-pointer">
            <div class="w-14 h-14 bg-[#3B82F6]/10 group-hover:bg-[#3B82F6] transition-colors flex items-center justify-center">
              <span class="text-xl font-bold text-[#3B82F6] group-hover:text-white">02</span>
            </div>
            <div class="flex-1">
              <h3 class="text-xl font-semibold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">我们的方案</h3>
              <p class="text-sm text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">智能代码安全平台</p>
            </div>
          </div>
          <div class="flex items-center gap-5 group cursor-pointer">
            <div class="w-14 h-14 bg-[#3B82F6]/10 group-hover:bg-[#3B82F6] transition-colors flex items-center justify-center">
              <span class="text-xl font-bold text-[#3B82F6] group-hover:text-white">03</span>
            </div>
            <div class="flex-1">
              <h3 class="text-xl font-semibold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">产品与服务</h3>
              <p class="text-sm text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">三种部署形态</p>
            </div>
          </div>
          <div class="flex items-center gap-5 group cursor-pointer">
            <div class="w-14 h-14 bg-[#3B82F6]/10 group-hover:bg-[#3B82F6] transition-colors flex items-center justify-center">
              <span class="text-xl font-bold text-[#3B82F6] group-hover:text-white">04</span>
            </div>
            <div class="flex-1">
              <h3 class="text-xl font-semibold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">核心优势</h3>
              <p class="text-sm text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">四大差异化能力</p>
            </div>
          </div>
          <div class="flex items-center gap-5 group cursor-pointer">
            <div class="w-14 h-14 bg-[#3B82F6]/10 group-hover:bg-[#3B82F6] transition-colors flex items-center justify-center">
              <span class="text-xl font-bold text-[#3B82F6] group-hover:text-white">05</span>
            </div>
            <div class="flex-1">
              <h3 class="text-xl font-semibold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">商业价值</h3>
              <p class="text-sm text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">ROI与竞争优势</p>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(3,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[45px] relative">
      <div class="absolute inset-0 bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6]" style="clip-path: polygon(0 0, 60% 0, 40% 100%, 0 100%);"></div>
      <div class="absolute inset-0 flex items-center justify-center">
        <div class="text-center">
          <div class="text-white text-7xl font-bold mb-5" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">01</div>
          <h1 class="text-4xl font-bold text-gray-900 mb-3" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">安全挑战</h1>
          <p class="text-xl text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">漏洞的代价与工具局限</p>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(4,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[20px]">
      <!-- 标题栏 -->
      <div class="bg-gradient-to-r from-[#1B2A4A] to-[#3B82F6] p-5 rounded-xl mb-6 flex items-center justify-between">
        <div>
          <h1 class="text-[38px] font-bold text-white mb-1" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">漏洞发现的时间差 = 经济损失</h1>
          <p class="text-gray-200 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">The Cost of Delayed Vulnerability Detection · Industry Data 2024</p>
        </div>
        <div class="text-right">
          <div class="text-[30px] font-black text-red-400">5-25x</div>
          <p class="text-gray-300 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">延迟修复成本倍增</p>
        </div>
      </div>
      
      <!-- KPI指标网格 -->
      <div class="grid grid-cols-4 gap-5 mb-6">
        <!-- KPI 1: 平均发现周期 -->
        <div class="bg-gradient-to-br from-red-600 to-red-700 p-5 rounded-xl shadow-xl">
          <div class="flex items-center justify-between mb-3">
            <p class="text-red-200 text-[14px] font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">平均发现周期</p>
            <div class="w-10 h-10 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
              <svg class="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
            </div>
          </div>
          <div class="text-[32px] font-black text-white mb-2">207天</div>
          <div class="text-red-200 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">漏洞从引入到被发现（IBM X-Force 2024）</div>
        </div>
        
        <!-- KPI 2: 单次漏洞损失 -->
        <div class="bg-gradient-to-br from-orange-600 to-orange-700 p-5 rounded-xl shadow-xl">
          <div class="flex items-center justify-between mb-3">
            <p class="text-orange-200 text-[14px] font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">单次数据泄露损失</p>
            <div class="w-10 h-10 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
              <svg class="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8c-1.657 0-3 .879-3 2s1.343 2 3 2 3 .879 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8c-1.11 0-2.08.402-2.599 1M12 8V6m0 4v2m0 4v2"/></svg>
            </div>
          </div>
          <div class="text-[32px] font-black text-white mb-2">$445万</div>
          <div class="text-orange-200 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">数据泄露平均损失（Ponemon Institute）</div>
        </div>
        
        <!-- KPI 3: 告警疲劳率 -->
        <div class="bg-gradient-to-br from-yellow-600 to-yellow-700 p-5 rounded-xl shadow-xl">
          <div class="flex items-center justify-between mb-3">
            <p class="text-yellow-200 text-[14px] font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">告警疲劳率</p>
            <div class="w-10 h-10 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
              <svg class="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.657V5a2 2 0 10-4 0v.343C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"/></svg>
            </div>
          </div>
          <div class="text-[32px] font-black text-white mb-2">67%</div>
          <div class="text-yellow-200 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">安全团队对传统工具告警失去信任（SANS 2024）</div>
        </div>
        
        <!-- KPI 4: 逻辑漏洞占比 -->
        <div class="bg-gradient-to-br from-purple-600 to-purple-700 p-5 rounded-xl shadow-xl">
          <div class="flex items-center justify-between mb-3">
            <p class="text-purple-200 text-[14px] font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">逻辑漏洞占比</p>
            <div class="w-10 h-10 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
              <svg class="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"/></svg>
            </div>
          </div>
          <div class="text-[32px] font-black text-white mb-2">35%</div>
          <div class="text-purple-200 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">高危漏洞中需理解代码意图的类型</div>
        </div>
      </div>
      
      <!-- 关键洞察 -->
      <div class="grid grid-cols-2 gap-5">
        <div class="bg-[#1B2A4A] p-5 rounded-xl">
          <h3 class="text-[20px] font-bold text-white mb-4 pb-3 border-b border-[#3B82F6]/30" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">核心发现</h3>
          <div class="space-y-3">
            <div>
              <div class="flex items-center justify-between mb-2">
                <span class="text-gray-300 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">开发阶段发现修复成本</span>
                <span class="text-white font-bold text-[18px]">$1</span>
              </div>
              <div class="h-2.5 bg-slate-700 rounded-full overflow-hidden">
                <div class="h-full bg-gradient-to-r from-emerald-500 to-green-500" style="width: 5%"></div>
              </div>
            </div>
            <div>
              <div class="flex items-center justify-between mb-2">
                <span class="text-gray-300 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">测试阶段发现修复成本</span>
                <span class="text-white font-bold text-[18px]">$5</span>
              </div>
              <div class="h-2.5 bg-slate-700 rounded-full overflow-hidden">
                <div class="h-full bg-gradient-to-r from-yellow-500 to-orange-500" style="width: 25%"></div>
              </div>
            </div>
            <div>
              <div class="flex items-center justify-between mb-2">
                <span class="text-gray-300 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">生产阶段发现修复成本</span>
                <span class="text-white font-bold text-[18px]">$25</span>
              </div>
              <div class="h-2.5 bg-slate-700 rounded-full overflow-hidden">
                <div class="h-full bg-gradient-to-r from-red-500 to-red-600" style="width: 100%"></div>
              </div>
            </div>
          </div>
        </div>
        
        <div class="bg-[#3B82F6]/10 border-2 border-[#3B82F6]/30 p-5 rounded-xl">
          <h3 class="text-[20px] font-bold text-[#3B82F6] mb-4 pb-3 border-b border-[#3B82F6]/30" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">关键洞察</h3>
          <p class="text-gray-700 text-[18px] leading-relaxed mb-4" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <span class="font-bold text-[#3B82F6]">发现越晚，修复越贵。</span>在生产环境发现1个漏洞的成本等于开发阶段发现5个漏洞的成本（NIST研究）。
          </p>
          <p class="text-gray-700 text-[18px] leading-relaxed" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <span class="font-bold text-[#3B82F6]">规则引擎天然盲区。</span>35%的高危漏洞需要理解代码意图才能发现，传统SAST工具完全无法检测。
          </p>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(5,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[20px]">
      <!-- 标题 -->
      <div class="text-center mb-6">
        <h1 class="text-[38px] font-bold text-slate-900 mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">传统代码安全工具的局限</h1>
        <p class="text-slate-800 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Why Traditional Tools Fall Short · Six-Dimension Comparison</p>
      </div>
      
      <!-- 对比表格 -->
      <div class="bg-slate-50 rounded-2xl p-5">
        <table class="w-full">
          <thead>
            <tr class="border-b-2 border-slate-300">
              <th class="text-left p-3 text-slate-700 font-bold text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">评估维度</th>
              <th class="text-center p-3 bg-[#3B82F6] text-white font-bold text-base rounded-t-xl" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">
                <div class="mb-1">zerodayFind</div>
                <div class="text-[14px] font-normal">智能推理引擎</div>
              </th>
              <th class="text-center p-3 text-slate-700 font-bold text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">
                <div class="mb-1">传统 SAST</div>
                <div class="text-[14px] font-normal text-slate-700">规则匹配引擎</div>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr class="border-b border-slate-200 hover:bg-slate-100 transition-colors">
              <td class="p-3 font-semibold text-slate-800 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">发现方式</td>
              <td class="text-center p-3 bg-blue-50">
                <div class="inline-flex items-center gap-2 bg-emerald-500 text-white px-3 py-1.5 rounded-full font-bold text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">AI溯因推理 · 发现未知风险</div>
              </td>
              <td class="text-center p-3 text-base text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">规则模式匹配 · 仅检测已知漏洞</td>
            </tr>
            <tr class="border-b border-slate-200 hover:bg-slate-100 transition-colors">
              <td class="p-3 font-semibold text-slate-800 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">误报控制</td>
              <td class="text-center p-3 bg-blue-50">
                <div class="inline-flex items-center gap-2 bg-emerald-500 text-white px-3 py-1.5 rounded-full font-bold text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">五层去重+置信度评分 · 精准可操作</div>
              </td>
              <td class="text-center p-3 text-base text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">40-60%误报率 · 告警疲劳严重</td>
            </tr>
            <tr class="border-b border-slate-200 hover:bg-slate-100 transition-colors">
              <td class="p-3 font-semibold text-slate-800 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">漏洞类型</td>
              <td class="text-center p-3 bg-blue-50">
                <div class="inline-flex items-center gap-2 bg-emerald-500 text-white px-3 py-1.5 rounded-full font-bold text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">逻辑漏洞+零日风险 · 无需规则更新</div>
              </td>
              <td class="text-center p-3 text-base text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">已知CVE模式 · 新漏洞需数周编写规则</td>
            </tr>
            <tr class="border-b border-slate-200 hover:bg-slate-100 transition-colors">
              <td class="p-3 font-semibold text-slate-800 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">领域适应</td>
              <td class="text-center p-3 bg-blue-50">
                <div class="inline-flex items-center gap-2 bg-emerald-500 text-white px-3 py-1.5 rounded-full font-bold text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">12领域专属策略 · 智能适配</div>
              </td>
              <td class="text-center p-3 text-base text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">通用规则库 · 一套规则打天下</td>
            </tr>
            <tr class="border-b border-slate-200 hover:bg-slate-100 transition-colors">
              <td class="p-3 font-semibold text-slate-800 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">验证深度</td>
              <td class="text-center p-3 bg-blue-50">
                <div class="inline-flex items-center gap-2 bg-emerald-500 text-white px-3 py-1.5 rounded-full font-bold text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">三重验证+质量审查 · 完整证据链</div>
              </td>
              <td class="text-center p-3 text-base text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">模式匹配即告警 · 无独立验证</td>
            </tr>
            <tr class="bg-slate-100">
              <td class="p-3 font-bold text-slate-900 text-[18px]" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">CI集成</td>
              <td class="text-center p-3 bg-[#3B82F6]">
                <div class="text-white font-bold text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">SARIF标准+基线比对+增量扫描 · DevSecOps原生</div>
              </td>
              <td class="text-center p-3 text-base text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">需手动配置 · 集成门槛高</td>
            </tr>
          </tbody>
        </table>
      </div>
      
      <!-- 核心结论 -->
      <div class="mt-5 bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6] text-white p-5 rounded-xl">
        <p class="text-[22px] font-bold text-center" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">传统工具"找已知漏洞"，我们"推理未知风险" — 这是根本性的范式转变</p>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(6,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[45px] relative">
      <div class="absolute inset-0 bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6]" style="clip-path: polygon(0 0, 60% 0, 40% 100%, 0 100%);"></div>
      <div class="absolute inset-0 flex items-center justify-center">
        <div class="text-center">
          <div class="text-white text-7xl font-bold mb-5" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">02</div>
          <h1 class="text-4xl font-bold text-gray-900 mb-3" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">我们的方案</h1>
          <p class="text-xl text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">智能代码安全平台</p>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(7,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[20px]">
      <!-- 标题 -->
      <div class="text-center mb-5">
        <h1 class="text-[40px] font-bold text-slate-900 mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">zerodayFind 价值主张</h1>
        <p class="text-slate-800 text-[18px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Value Proposition · From Hypothesis to Verified Vulnerability</p>
      </div>
      
      <!-- 画布网格 -->
      <div class="grid grid-cols-5 gap-4 h-[580px]">
        <!-- 第一列：客户痛点 -->
        <div class="flex flex-col gap-4">
          <div class="bg-red-50 border-2 border-red-300 rounded-xl p-5 flex-1">
            <h3 class="font-bold text-red-900 mb-2 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">客户痛点</h3>
            <ul class="text-[14px] text-red-800 space-y-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-red-500 mt-1.5 flex-shrink-0"></span>
                <span>漏洞发现滞后，修复成本指数级增长</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-red-500 mt-1.5 flex-shrink-0"></span>
                <span>告警疲劳严重，67%团队失去信任</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-red-500 mt-1.5 flex-shrink-0"></span>
                <span>逻辑漏洞盲区，规则引擎无法触及</span>
              </li>
            </ul>
          </div>
          <div class="bg-orange-50 border-2 border-orange-300 rounded-xl p-5 flex-1">
            <h3 class="font-bold text-orange-900 mb-2 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">痛点后果</h3>
            <ul class="text-[14px] text-orange-800 space-y-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-orange-500 mt-1.5 flex-shrink-0"></span>
                <span>数据泄露平均损失445万美元</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-orange-500 mt-1.5 flex-shrink-0"></span>
                <span>安全团队产能被误报吞噬</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-orange-500 mt-1.5 flex-shrink-0"></span>
                <span>合规审计依赖人工，效率低下</span>
              </li>
            </ul>
          </div>
        </div>
        
        <!-- 第二列：核心价值 -->
        <div class="col-span-2">
          <div class="bg-gradient-to-br from-[#3B82F6] to-[#1B2A4A] border-2 border-[#3B82F6] rounded-xl p-7 h-full flex flex-col justify-center text-white">
            <h3 class="font-bold mb-4 text-[22px] text-center" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">核心价值主张</h3>
            <div class="bg-white/20 backdrop-blur rounded-lg p-5 mb-3 text-center">
              <p class="font-semibold text-[20px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">"从假设出发，发现真实漏洞"</p>
              <p class="text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">不是匹配规则，而是模拟安全研究员的推理过程</p>
            </div>
            <div class="space-y-3 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
              <div class="bg-white/20 backdrop-blur rounded-lg p-4">
                <p class="font-semibold mb-1.5">发现能力</p>
                <p class="text-[14px]">溯因推理发现逻辑漏洞，无需等待规则库更新</p>
              </div>
              <div class="bg-white/20 backdrop-blur rounded-lg p-4">
                <p class="font-semibold mb-1.5">精准能力</p>
                <p class="text-[14px]">五层去重+置信度评分，误报率显著低于传统工具</p>
              </div>
              <div class="bg-white/20 backdrop-blur rounded-lg p-4">
                <p class="font-semibold mb-1.5">集成能力</p>
                <p class="text-[14px]">SARIF标准+CI/CD原生集成，DevSecOps无缝嵌入</p>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 第三列：价值交付 -->
        <div class="flex flex-col gap-4">
          <div class="bg-emerald-50 border-2 border-emerald-300 rounded-xl p-5 flex-1">
            <h3 class="font-bold text-emerald-900 mb-2 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">价值交付</h3>
            <ul class="text-[14px] text-emerald-800 space-y-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 mt-1.5 flex-shrink-0"></span>
                <span>提前发现高危漏洞，缩短发现周期</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 mt-1.5 flex-shrink-0"></span>
                <span>精准输出，每个告警可操作有证据链</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 mt-1.5 flex-shrink-0"></span>
                <span>SARIF标准输出，直接对接CI工具</span>
              </li>
            </ul>
          </div>
          <div class="bg-blue-50 border-2 border-blue-300 rounded-xl p-5 flex-1">
            <h3 class="font-bold text-blue-900 mb-2 text-base" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">部署形态</h3>
            <ul class="text-[14px] text-blue-800 space-y-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-blue-500 mt-1.5 flex-shrink-0"></span>
                <span>CLI工具 — 开发者本地使用</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-blue-500 mt-1.5 flex-shrink-0"></span>
                <span>CI/CD组件 — 自动化流水线</span>
              </li>
              <li class="flex items-start gap-2">
                <span class="w-1.5 h-1.5 rounded-full bg-blue-500 mt-1.5 flex-shrink-0"></span>
                <span>审计服务 — 猎零安全团队深度审计</span>
              </li>
            </ul>
          </div>
        </div>
        
        <!-- 第四+五列：核心保障 -->
        <div class="flex flex-col gap-4">
          <div class="bg-gradient-to-br from-[#3B82F6] to-blue-600 rounded-xl p-6 flex-1 text-white flex flex-col justify-center">
            <h3 class="font-bold text-[20px] mb-3" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">三重验证保障</h3>
            <p class="text-[14px] mb-4" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">每个发现都经过完整证据链验证</p>
            <div class="space-y-2 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
              <div class="bg-white/20 rounded-lg p-3">
                <span class="font-semibold">推理假设</span> → AI模拟安全研究员思维
              </div>
              <div class="bg-white/20 rounded-lg p-3">
                <span class="font-semibold">独立验证</span> → 多角色并发验证可到达性
              </div>
              <div class="bg-white/20 rounded-lg p-3">
                <span class="font-semibold">质量审查</span> → 三重审查确保可利用可影响
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(8,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[20px] flex flex-col">
      <div class="flex items-center justify-between mb-8 pb-5 border-b-4 border-[#1B2A4A]">
        <h2 class="text-[40px] font-bold text-[#1B2A4A]" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">三步完成代码安全审计</h2>
        <div class="text-right"><p class="text-base text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">How It Works</p><p class="text-[14px] text-slate-700">Intelligent Security Audit Process</p></div>
      </div>
      <div class="mb-6">
        <div class="bg-blue-50 p-6 rounded-lg border-l-4 border-[#3B82F6]">
          <p class="text-slate-700 text-[18px] leading-relaxed" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;"><span class="font-bold text-[#1B2A4A]">核心流程：</span>从代码提交到安全确认，三步走完成智能审计。不是规则匹配后直接告警，而是推理→验证→审查后才输出发现。</p>
        </div>
      </div>
      <div class="grid grid-cols-3 gap-8 mb-6">
        <div class="bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6] text-white p-7 rounded-lg shadow-lg hover:shadow-xl transition-shadow">
          <div class="text-[32px] font-bold mb-4" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">01</div>
          <h4 class="text-[22px] font-bold mb-4" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">智能推理</h4>
          <ul class="space-y-3 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <li class="flex gap-3"><span>▸</span><span>AI理解代码意图和安全语义</span></li>
            <li class="flex gap-3"><span>▸</span><span>模拟安全研究员溯因推理</span></li>
            <li class="flex gap-3"><span>▸</span><span>覆盖12个安全领域自动适配</span></li>
            <li class="flex gap-3"><span>▸</span><span>形成可验证的安全假设</span></li>
          </ul>
        </div>
        <div class="bg-gradient-to-br from-[#3B82F6] to-blue-400 text-white p-7 rounded-lg shadow-lg hover:shadow-xl transition-shadow">
          <div class="text-[32px] font-bold mb-4" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">02</div>
          <h4 class="text-[22px] font-bold mb-4" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">独立验证</h4>
          <ul class="space-y-3 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <li class="flex gap-3"><span>▸</span><span>多角色验证器并发执行</span></li>
            <li class="flex gap-3"><span>▸</span><span>追踪数据流验证可达性</span></li>
            <li class="flex gap-3"><span>▸</span><span>构造利用路径确认影响</span></li>
            <li class="flex gap-3"><span>▸</span><span>每个假设独立验证非直接告警</span></li>
          </ul>
        </div>
        <div class="bg-gradient-to-br from-slate-600 to-slate-500 text-white p-7 rounded-lg shadow-lg hover:shadow-xl transition-shadow">
          <div class="text-[32px] font-bold mb-4" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">03</div>
          <h4 class="text-[22px] font-bold mb-4" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">质量审查</h4>
          <ul class="space-y-3 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <li class="flex gap-3"><span>▸</span><span>攻击者能否到达漏洞点？</span></li>
            <li class="flex gap-3"><span>▸</span><span>能否产生实际安全影响？</span></li>
            <li class="flex gap-3"><span>▸</span><span>是否有已知缓解措施？</span></li>
            <li class="flex gap-3"><span>▸</span><span>附置信度评分和修复建议</span></li>
          </ul>
        </div>
      </div>
      <div class="flex gap-8">
        <div class="flex-1 border-l-4 border-[#3B82F6] pl-7 bg-blue-50 p-5 rounded">
          <p class="text-slate-700 text-[18px] leading-relaxed mb-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;"><span class="font-bold text-[#1B2A4A]">输出成果：</span>标准SARIF 2.1.0报告，直接对接GitHub/GitLab/VS Code，每个发现包含完整推理过程、证据链和修复建议。</p>
        </div>
        <div class="w-[420px] bg-emerald-50 p-5 rounded border-l-4 border-emerald-500">
          <p class="text-emerald-900 text-[14px] leading-relaxed" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;"><span class="font-bold">行业标准：</span>SARIF 2.1.0是OASIS认可的静态分析结果交换格式，主流CI平台和安全工具广泛支持。</p>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(9,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[45px] relative">
      <div class="absolute inset-0 bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6]" style="clip-path: polygon(0 0, 60% 0, 40% 100%, 0 100%);"></div>
      <div class="absolute inset-0 flex items-center justify-center">
        <div class="text-center">
          <div class="text-white text-7xl font-bold mb-5" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">03</div>
          <h1 class="text-4xl font-bold text-gray-900 mb-3" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">产品与服务</h1>
          <p class="text-xl text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">三种部署形态</p>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(10,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[45px] flex flex-col">
      <!-- 标题 -->
      <div class="flex items-center justify-between mb-8">
        <div>
          <h1 class="text-3xl font-bold text-gray-800 mb-1" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">三种部署形态，统一安全引擎</h1>
          <p class="text-gray-700 text-sm" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Unified Security Engine · Three Deployment Models</p>
        </div>
        <div class="flex items-center gap-2">
          <div class="w-3 h-3 rounded-full bg-[#1B2A4A]"></div>
          <div class="w-3 h-3 rounded-full bg-[#3B82F6]"></div>
          <div class="w-3 h-3 rounded-full bg-gray-200"></div>
        </div>
      </div>
      
      <!-- 2x2 矩阵 -->
      <div class="grid grid-cols-2 gap-5 flex-1">
        <!-- 形态1：CLI审计工具 -->
        <div class="bg-gray-50 p-6 rounded-lg border-l-4 border-[#1B2A4A]">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 bg-[#1B2A4A] rounded-lg flex items-center justify-center text-white text-lg font-bold">CLI</div>
            <div>
              <h2 class="text-lg font-bold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">CLI审计工具</h2>
              <p class="text-xs text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Command Line Interface</p>
            </div>
          </div>
          <ul class="space-y-2.5 text-sm text-gray-800" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#1B2A4A] mt-1.5 flex-shrink-0"></span>
              <span>开发者本地使用，单命令扫描项目</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#1B2A4A] mt-1.5 flex-shrink-0"></span>
              <span>Go单二进制跨平台分发，零依赖部署</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#1B2A4A] mt-1.5 flex-shrink-0"></span>
              <span>适合安全研究员日常审计和快速排查</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#1B2A4A] mt-1.5 flex-shrink-0"></span>
              <span>zdll scan <target> 立即开始扫描</span>
            </li>
          </ul>
        </div>
        
        <!-- 形态2：CI/CD安全组件 -->
        <div class="bg-gray-50 p-6 rounded-lg border-l-4 border-[#3B82F6]">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 bg-[#3B82F6] rounded-lg flex items-center justify-center text-white text-lg font-bold">CI</div>
            <div>
              <h2 class="text-lg font-bold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">CI/CD安全组件</h2>
              <p class="text-xs text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Continuous Integration Component</p>
            </div>
          </div>
          <ul class="space-y-2.5 text-sm text-gray-800" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#3B82F6] mt-1.5 flex-shrink-0"></span>
              <span>集成DevSecOps流水线，自动化持续检测</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#3B82F6] mt-1.5 flex-shrink-0"></span>
              <span>SARIF标准输出对接GitHub/GitLab/Azure</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#3B82F6] mt-1.5 flex-shrink-0"></span>
              <span>增量扫描+基线比对+严重性策略门控</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-[#3B82F6] mt-1.5 flex-shrink-0"></span>
              <span>可配置阻断部署或仅告警通知</span>
            </li>
          </ul>
        </div>
        
        <!-- 形态3：代码审计服务 -->
        <div class="bg-gray-50 p-6 rounded-lg border-l-4 border-gray-600">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 bg-gray-600 rounded-lg flex items-center justify-center text-white text-lg font-bold">Svc</div>
            <div>
              <h2 class="text-lg font-bold text-gray-800" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">代码审计服务</h2>
              <p class="text-xs text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Professional Audit Service</p>
            </div>
          </div>
          <ul class="space-y-2.5 text-sm text-gray-800" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-gray-600 mt-1.5 flex-shrink-0"></span>
              <span>猎零安全团队深度审计+人工复核</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-gray-600 mt-1.5 flex-shrink-0"></span>
              <span>输出完整漏洞报告+修复建议</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-gray-600 mt-1.5 flex-shrink-0"></span>
              <span>适合企业级安全评估和合规审计</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-gray-600 mt-1.5 flex-shrink-0"></span>
              <span>满足SOC2/ISO27001/PCI-DSS合规要求</span>
            </li>
          </ul>
        </div>
        
        <!-- 统一内核 -->
        <div class="bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6] p-6 rounded-lg flex flex-col justify-center text-white">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center text-lg font-bold">Core</div>
            <div>
              <h2 class="text-lg font-bold" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">统一智能推理引擎</h2>
              <p class="text-xs text-white/70" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Unified Security Intelligence Core</p>
            </div>
          </div>
          <ul class="space-y-2.5 text-sm" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-white mt-1.5 flex-shrink-0"></span>
              <span>三种形态共享同一智能推理引擎</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-white mt-1.5 flex-shrink-0"></span>
              <span>一致的安全发现能力和报告质量</span>
            </li>
            <li class="flex items-start gap-2">
              <span class="w-1.5 h-1.5 rounded-full bg-white mt-1.5 flex-shrink-0"></span>
              <span>无论哪种形态，底层审计能力完全一致</span>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(11,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[20px]">
      <!-- 标题 -->
      <div class="flex items-center justify-between mb-10">
        <div>
          <h1 class="text-[40px] font-bold text-slate-900 mb-3" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">CI/CD 集成：安全左移完整方案</h1>
          <p class="text-slate-800 text-[18px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Shift-Left Security · Five-Stage Integration</p>
        </div>
        <div class="bg-[#3B82F6] text-white px-7 py-4 rounded-lg">
          <p class="text-base font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">集成方式</p>
          <p class="text-[32px] font-black" style="font-family: 'Poppins', sans-serif;">5步</p>
        </div>
      </div>
      
      <!-- 流程标题 -->
      <div class="flex gap-3 mb-5 pl-72">
        <div class="flex-1 text-center font-bold text-slate-800 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">提交</div>
        <div class="flex-1 text-center font-bold text-slate-800 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">扫描</div>
        <div class="flex-1 text-center font-bold text-slate-800 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">比对</div>
        <div class="flex-1 text-center font-bold text-slate-800 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">门控</div>
        <div class="flex-1 text-center font-bold text-slate-800 text-base" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">交付</div>
      </div>
      
      <!-- 甘特图 -->
      <div class="space-y-5">
        <!-- Stage 1: 代码提交 -->
        <div class="flex items-center gap-5">
          <div class="w-[270px] bg-[#1B2A4A] text-white p-5 rounded-lg">
            <p class="font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">代码提交</p>
            <p class="text-[14px] text-gray-300" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Code Commit</p>
          </div>
          <div class="flex-1 relative h-14 bg-slate-200 rounded-lg overflow-hidden">
            <div class="absolute left-0 top-0 h-full bg-gradient-to-r from-emerald-500 to-green-600 flex items-center px-5 text-white font-bold text-base" style="width: 20%">
              <span style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">触发CI ✓</span>
            </div>
          </div>
        </div>
        
        <!-- Stage 2: 自动扫描 -->
        <div class="flex items-center gap-5">
          <div class="w-[270px] bg-[#3B82F6] text-white p-5 rounded-lg">
            <p class="font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">自动扫描</p>
            <p class="text-[14px] text-blue-100" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">zdll ci <target></p>
          </div>
          <div class="flex-1 relative h-14 bg-slate-200 rounded-lg overflow-hidden">
            <div class="absolute left-[20%] top-0 h-full bg-gradient-to-r from-[#3B82F6] to-indigo-600 flex items-center px-5 text-white font-bold text-base" style="width: 20%">
              <span style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">SARIF输出</span>
            </div>
          </div>
        </div>
        
        <!-- Stage 3: 结果比对 -->
        <div class="flex items-center gap-5">
          <div class="w-[270px] bg-[#1B2A4A] text-white p-5 rounded-lg">
            <p class="font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">结果比对</p>
            <p class="text-[14px] text-gray-300" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Baseline Comparison</p>
          </div>
          <div class="flex-1 relative h-14 bg-slate-200 rounded-lg overflow-hidden">
            <div class="absolute left-[40%] top-0 h-full bg-gradient-to-r from-purple-500 to-purple-600 flex items-center px-5 text-white font-bold text-base" style="width: 20%">
              <span style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">增量检测</span>
            </div>
          </div>
        </div>
        
        <!-- Stage 4: 策略门控 -->
        <div class="flex items-center gap-5">
          <div class="w-[270px] bg-red-700 text-white p-5 rounded-lg">
            <p class="font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">策略门控</p>
            <p class="text-[14px] text-red-100" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">--fail-on critical/high</p>
          </div>
          <div class="flex-1 relative h-14 bg-slate-200 rounded-lg overflow-hidden">
            <div class="absolute left-[60%] top-0 h-full bg-gradient-to-r from-orange-500 to-red-600 flex items-center px-5 text-white font-bold text-base" style="width: 20%">
              <span style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">阻断/告警</span>
            </div>
          </div>
        </div>
        
        <!-- Stage 5: 安全交付 -->
        <div class="flex items-center gap-5">
          <div class="w-[270px] bg-emerald-700 text-white p-5 rounded-lg">
            <p class="font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">安全交付</p>
            <p class="text-[14px] text-emerald-100" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Secure Release</p>
          </div>
          <div class="flex-1 relative h-14 bg-slate-200 rounded-lg overflow-hidden">
            <div class="absolute left-[80%] top-0 h-full bg-gradient-to-r from-emerald-500 to-green-600 flex items-center px-5 text-white font-bold text-base" style="width: 20%">
              <span style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">发布 ✓</span>
            </div>
          </div>
        </div>
      </div>
      
      <!-- 底部信息 -->
      <div class="mt-8 grid grid-cols-3 gap-8">
        <div class="bg-white p-5 rounded-lg shadow">
          <p class="text-slate-700 text-base mb-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">增量扫描</p>
          <p class="text-[26px] font-bold text-slate-900" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">仅分析变更文件</p>
        </div>
        <div class="bg-white p-5 rounded-lg shadow">
          <p class="text-slate-700 text-base mb-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">基线比对</p>
          <p class="text-[26px] font-bold text-slate-900" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">新增 vs 已解决</p>
        </div>
        <div class="bg-white p-5 rounded-lg shadow">
          <p class="text-slate-700 text-base mb-2" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">证据链</p>
          <p class="text-[26px] font-bold text-slate-900" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">完整推理过程</p>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(12,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[45px] relative">
      <div class="absolute inset-0 bg-gradient-to-br from-[#1B2A4A] to-[#3B82F6]" style="clip-path: polygon(0 0, 60% 0, 40% 100%, 0 100%);"></div>
      <div class="absolute inset-0 flex items-center justify-center">
        <div class="text-center">
          <div class="text-white text-7xl font-bold mb-5" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">04</div>
          <h1 class="text-4xl font-bold text-gray-900 mb-3" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">核心优势</h1>
          <p class="text-xl text-gray-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">四大差异化能力</p>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(13,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[20px]">
      <!-- 标题 -->
      <div class="text-center mb-5">
        <h1 class="text-[38px] font-bold text-slate-900 mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">四大核心能力</h1>
        <p class="text-[18px] text-slate-800" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Core Capabilities · What Makes Us Different</p>
      </div>
      
      <!-- 案例展示 -->
      <div class="grid grid-cols-2 gap-5">
        <!-- 能力1：智能发现 -->
        <div class="bg-white rounded-2xl shadow-xl overflow-hidden hover:shadow-2xl transition-shadow">
          <div class="bg-gradient-to-r from-[#1B2A4A] to-[#3B82F6] p-3 text-white">
            <div class="flex items-center justify-between mb-1">
              <h3 class="text-[20px] font-black" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">智能发现</h3>
              <div class="w-12 h-12 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
                <svg class="w-7 h-7 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.95-.438-1.858-1.004-2.464l-.548-.547z"/></svg>
              </div>
            </div>
            <p class="text-blue-100 text-[12px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">AI-Driven Discovery · Abductive Reasoning</p>
          </div>
          <div class="p-3">
            <div class="mb-2">
              <p class="text-slate-700 text-[12px] font-semibold mb-1" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">核心能力</p>
              <p class="text-slate-700 text-[14px] leading-relaxed" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">AI溯因推理替代规则匹配，理解代码意图而非匹配模式。发现权限绕过、业务逻辑缺陷等规则引擎无法触及的漏洞。</p>
            </div>
            <div class="grid grid-cols-3 gap-2 pt-2 border-t border-slate-200">
              <div class="text-center">
                <p class="text-[20px] font-black text-[#3B82F6]">12</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">安全领域</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-[#3B82F6]">0规则</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">无需更新</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-[#3B82F6]">逻辑</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">漏洞覆盖</p>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 能力2：精准输出 -->
        <div class="bg-white rounded-2xl shadow-xl overflow-hidden hover:shadow-2xl transition-shadow">
          <div class="bg-gradient-to-r from-emerald-600 to-green-600 p-3 text-white">
            <div class="flex items-center justify-between mb-1">
              <h3 class="text-[20px] font-black" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">精准输出</h3>
              <div class="w-12 h-12 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
                <svg class="w-7 h-7 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
              </div>
            </div>
            <p class="text-emerald-100 text-[12px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Precision Output · Five-Layer Dedup + Bayesian</p>
          </div>
          <div class="p-3">
            <div class="mb-2">
              <p class="text-slate-700 text-[12px] font-semibold mb-1" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">核心能力</p>
              <p class="text-slate-700 text-[14px] leading-relaxed" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">五层语义去重过滤近似重复，贝叶斯置信度评分量化风险等级，三重审查确保每个告警可操作。告别告警疲劳。</p>
            </div>
            <div class="grid grid-cols-3 gap-2 pt-2 border-t border-slate-200">
              <div class="text-center">
                <p class="text-[20px] font-black text-emerald-600">5层</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">语义去重</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-emerald-600">评分</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">置信度量化</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-emerald-600">3重</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">质量审查</p>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 能力3：领域专长 -->
        <div class="bg-white rounded-2xl shadow-xl overflow-hidden hover:shadow-2xl transition-shadow">
          <div class="bg-gradient-to-r from-purple-600 to-indigo-600 p-3 text-white">
            <div class="flex items-center justify-between mb-1">
              <h3 class="text-[20px] font-black" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">领域专长</h3>
              <div class="w-12 h-12 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
                <svg class="w-7 h-7 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/></svg>
              </div>
            </div>
            <p class="text-purple-100 text-[12px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Domain Expertise · 12 Security Domains</p>
          </div>
          <div class="p-3">
            <div class="mb-2">
              <p class="text-slate-700 text-[12px] font-semibold mb-1" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">核心能力</p>
              <p class="text-slate-700 text-[14px] leading-relaxed" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">12个安全领域各有专属审计策略：Web侧重XSS/CSRF，AI Agent侧重Prompt注入，基础设施侧重配置风险。不是一套规则打天下。</p>
            </div>
            <div class="grid grid-cols-3 gap-2 pt-2 border-t border-slate-200">
              <div class="text-center">
                <p class="text-[20px] font-black text-purple-600">12</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">安全领域</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-purple-600">专属</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">审计策略</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-purple-600">自动</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">领域适配</p>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 能力4：持续进化 -->
        <div class="bg-white rounded-2xl shadow-xl overflow-hidden hover:shadow-2xl transition-shadow">
          <div class="bg-gradient-to-r from-orange-600 to-red-600 p-3 text-white">
            <div class="flex items-center justify-between mb-1">
              <h3 class="text-[20px] font-black" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">持续进化</h3>
              <div class="w-12 h-12 bg-white/20 backdrop-blur rounded-lg flex items-center justify-center">
                <svg class="w-7 h-7 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6"/></svg>
              </div>
            </div>
            <p class="text-orange-100 text-[12px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Continuous Evolution · Bayesian Learning</p>
          </div>
          <div class="p-3">
            <div class="mb-2">
              <p class="text-slate-700 text-[12px] font-semibold mb-1" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">核心能力</p>
              <p class="text-slate-700 text-[14px] leading-relaxed" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">历史扫描数据持续学习因果强度，跨项目复用经验。每次扫描后引擎更精准，无需人工更新规则库。越用越准，而非越用越旧。</p>
            </div>
            <div class="grid grid-cols-3 gap-2 pt-2 border-t border-slate-200">
              <div class="text-center">
                <p class="text-[20px] font-black text-orange-600">自动</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">经验学习</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-orange-600">跨项目</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">知识复用</p>
              </div>
              <div class="text-center">
                <p class="text-[20px] font-black text-orange-600">越准</p>
                <p class="text-[10px] text-slate-700" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">持续提升</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(14,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="w-[1350px] h-[720px] mx-auto my-[20px]">
      <!-- 标题 -->
      <div class="text-center mb-6">
        <h1 class="text-[40px] font-bold text-white mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">商业价值：安全投资的回报</h1>
        <p class="text-gray-100 text-[20px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">Business Value · ROI & Competitive Advantage</p>
      </div>
      
      <div class="grid grid-cols-2 gap-6">
        <!-- 左侧：投入 -->
        <div class="space-y-4">
          <!-- 风险成本 -->
          <div class="bg-gradient-to-br from-red-600 to-red-700 p-5 rounded-2xl shadow-2xl">
            <p class="text-red-200 text-[14px] mb-1 font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">当前风险成本</p>
            <div class="flex items-baseline gap-2 mb-3">
              <span class="text-[40px] font-black text-white">$445万</span>
              <span class="text-[22px] text-red-200" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">/次泄露</span>
            </div>
            <div class="flex items-center gap-2 text-red-100">
              <div class="flex-1">
                <p class="text-[12px] mb-1" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">人力审计成本</p>
                <div class="h-2 bg-red-900/50 rounded-full overflow-hidden">
                  <div class="h-full bg-red-300" style="width: 40%"></div>
                </div>
              </div>
              <span class="text-[14px] font-bold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">2-4周/项目</span>
            </div>
            <div class="flex items-center gap-2 text-red-100 mt-2">
              <div class="flex-1">
                <p class="text-[12px] mb-1" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">延迟修复倍增</p>
                <div class="h-2 bg-red-900/50 rounded-full overflow-hidden">
                  <div class="h-full bg-red-300" style="width: 100%"></div>
                </div>
              </div>
              <span class="text-[14px] font-bold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">5-25倍</span>
            </div>
          </div>
          
          <!-- 合规成本 -->
          <div class="bg-gradient-to-br from-orange-600 to-orange-700 p-5 rounded-2xl shadow-2xl">
            <p class="text-orange-200 text-[14px] mb-1 font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">合规审计成本</p>
            <div class="flex items-baseline gap-2 mb-1">
              <span class="text-[40px] font-black text-white">60%</span>
              <span class="text-[22px] text-orange-200" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">人力占比</span>
            </div>
            <p class="text-orange-100 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">传统合规审计要求定期人工审查，自动化方案可降低60%合规人力投入</p>
          </div>
          
          <!-- 响应成本 -->
          <div class="bg-gradient-to-br from-yellow-600 to-yellow-700 p-5 rounded-2xl shadow-2xl">
            <p class="text-yellow-200 text-[14px] mb-1 font-semibold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">生产环境响应成本</p>
            <div class="flex items-baseline gap-2 mb-1">
              <span class="text-[40px] font-black text-white">25x</span>
            </div>
            <p class="text-yellow-100 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">生产环境发现漏洞的修复成本是开发阶段的25倍（NIST研究）</p>
          </div>
        </div>
        
        <!-- 右侧：回报 -->
        <div class="bg-slate-700/50 backdrop-blur rounded-2xl p-5">
          <h2 class="text-[22px] font-bold text-white mb-4 pb-3 border-b border-slate-600" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">投资回报预测</h2>
          
          <div class="space-y-4">
            <!-- 第一年 -->
            <div class="bg-slate-800 rounded-xl p-4">
              <div class="flex items-center justify-between mb-3">
                <h3 class="text-[20px] font-bold text-white" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">第一年</h3>
                <span class="bg-yellow-500 text-yellow-900 px-3 py-1 rounded-full text-[14px] font-bold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">投资期</span>
              </div>
              <div class="grid grid-cols-2 gap-3 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
                <div>
                  <p class="text-slate-200 mb-1">误报处理成本</p>
                  <p class="text-emerald-400 font-bold text-[18px]">降低50%</p>
                </div>
                <div>
                  <p class="text-slate-200 mb-1">审计效率</p>
                  <p class="text-white font-bold text-[18px]">提升3倍</p>
                </div>
              </div>
            </div>
            
            <!-- 第二年 -->
            <div class="bg-slate-800 rounded-xl p-4">
              <div class="flex items-center justify-between mb-3">
                <h3 class="text-[20px] font-bold text-white" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">第二年</h3>
                <span class="bg-blue-500 text-white px-3 py-1 rounded-full text-[14px] font-bold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">增长期</span>
              </div>
              <div class="grid grid-cols-2 gap-3 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
                <div>
                  <p class="text-slate-200 mb-1">漏洞发现周期</p>
                  <p class="text-emerald-400 font-bold text-[18px]">缩短70%</p>
                </div>
                <div>
                  <p class="text-slate-200 mb-1">合规满足</p>
                  <p class="text-white font-bold text-[18px]">SOC2/ISO/PCI</p>
                </div>
              </div>
            </div>
            
            <!-- 第三年 -->
            <div class="bg-slate-800 rounded-xl p-4">
              <div class="flex items-center justify-between mb-3">
                <h3 class="text-[20px] font-bold text-white" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">第三年</h3>
                <span class="bg-emerald-500 text-white px-3 py-1 rounded-full text-[14px] font-bold" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">成熟期</span>
              </div>
              <div class="grid grid-cols-2 gap-3 text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">
                <div>
                  <p class="text-slate-200 mb-1">引擎精准度</p>
                  <p class="text-emerald-400 font-bold text-[18px]">持续提升</p>
                </div>
                <div>
                  <p class="text-slate-200 mb-1">团队产能</p>
                  <p class="text-white font-bold text-[18px]">提升5倍</p>
                </div>
              </div>
            </div>
            
            <!-- 总计 -->
            <div class="bg-gradient-to-r from-[#3B82F6] to-[#1B2A4A] rounded-xl p-4">
              <div class="flex items-center justify-between">
                <div>
                  <p class="text-blue-200 text-[14px] mb-1" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">核心回报</p>
                  <p class="text-[26px] font-black text-white" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">风险降低+效率提升+合规加速</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
`);window.slideDataMap.set(15,`
  <div class="w-[1440px] h-[810px] shadow-2xl relative overflow-hidden slide-bg">
    <div class="absolute top-[-10%] left-[-10%] w-[400px] h-[400px] rotate-45 border-2 border-[rgba(82,139,255,0.2)]"></div>
    <div class="absolute top-[-5%] left-[-5%] w-[350px] h-[350px] rotate-45 border border-[rgba(82,139,255,0.15)]"></div>
    <div class="absolute bottom-[-10%] right-[-10%] w-[450px] h-[450px] rotate-45 border-2 border-[rgba(82,139,255,0.2)]"></div>
    <div class="absolute bottom-[-5%] right-[-5%] w-[380px] h-[380px] rotate-45 border border-[rgba(82,139,255,0.15)]"></div>
    <div class="absolute top-[12%] left-0 w-[200px] h-[3px]" style="background: linear-gradient(to right, #528bff, transparent);"></div>
    <div class="absolute bottom-[12%] right-0 w-[200px] h-[3px]" style="background: linear-gradient(to left, #528bff, transparent);"></div>
    
    <div class="w-[1350px] h-[720px] mx-auto my-[45px] flex flex-col items-center justify-center">
      <div class="text-center w-[75%]">
        <h1 class="text-[56px] text-white font-light mb-[30px] tracking-[4px]" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">开始智能代码安全审计</h1>
        
        <div class="w-[150px] h-[2px] mx-auto mb-[30px]" style="background: linear-gradient(to right, transparent, #528bff, transparent);"></div>
        
        <p class="text-[22px] text-[#d0ddf8] font-light leading-[1.8] mb-[30px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">zerodayFind · 猎零安全团队 — 发现真实漏洞，降低安全风险</p>
        
        <!-- 三种接入方式 -->
        <div class="grid grid-cols-3 gap-6 mb-[30px]">
          <div class="bg-white/10 backdrop-blur rounded-xl p-5 text-center border border-[#528bff]/30">
            <div class="w-12 h-12 bg-[#528bff]/20 rounded-lg flex items-center justify-center mx-auto mb-3">
              <svg class="w-7 h-7 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/></svg>
            </div>
            <h3 class="text-white font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">CLI工具</h3>
            <p class="text-[#d0ddf8] text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">zdll scan <your-project></p>
            <p class="text-[#d0ddf8] text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">立即开始扫描</p>
          </div>
          <div class="bg-white/10 backdrop-blur rounded-xl p-5 text-center border border-[#528bff]/30">
            <div class="w-12 h-12 bg-[#528bff]/20 rounded-lg flex items-center justify-center mx-auto mb-3">
              <svg class="w-7 h-7 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0-5a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
            </div>
            <h3 class="text-white font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">CI/CD集成</h3>
            <p class="text-[#d0ddf8] text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">集成你的DevSecOps流水线</p>
            <p class="text-[#d0ddf8] text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">自动化持续安全检测</p>
          </div>
          <div class="bg-white/10 backdrop-blur rounded-xl p-5 text-center border border-[#528bff]/30">
            <div class="w-12 h-12 bg-[#528bff]/20 rounded-lg flex items-center justify-center mx-auto mb-3">
              <svg class="w-7 h-7 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 20h5v-2a3 3 0 00-5.356-1.853M5 20h5v-2a3 3 0 00-5.356-1.853M15 11a3 3 0 10-6 0v2m6-2a3 3 0 003-3V8a3 3 0 00-6 0v2m9 6v2a2 2 0 01-2 2H5a2 2 0 01-2-2v-2m16 0a2 2 0 01-2 2"/></svg>
            </div>
            <h3 class="text-white font-bold text-[18px] mb-2" style="font-family: 'Poppins', 'Noto Sans SC', sans-serif;">审计服务</h3>
            <p class="text-[#d0ddf8] text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">猎零安全团队深度审计+人工复核</p>
            <p class="text-[#d0ddf8] text-[14px]" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">企业级安全评估</p>
          </div>
        </div>
        
        <div class="w-[150px] h-[2px] mx-auto mb-[20px]" style="background: linear-gradient(to right, transparent, #528bff, transparent);"></div>
        <p class="text-[18px] text-[#b0c4e8] font-light" style="font-family: 'Inter', 'Noto Sans SC', sans-serif;">联系我们获取试用或了解更多</p>
      </div>
    </div>
  </div>
`);
