window.slideDataMap.set(11, `
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
`);
