window.slideDataMap.set(4, `
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
`);
