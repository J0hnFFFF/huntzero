window.slideDataMap.set(8, `
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
`);
