window.slideDataMap.set(10, `
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
`);
