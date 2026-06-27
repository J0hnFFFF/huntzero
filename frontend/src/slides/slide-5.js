window.slideDataMap.set(5, `
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
`);
