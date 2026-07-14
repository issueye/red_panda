// Split from SettingsPanel.jsx (checklist R7c)
import {
  ModuleHeader,
  RUNTIME_MODES,
  TOOL_POLICIES,
  PERMISSION_MODES,
  WEB_SEARCH_PROVIDERS,
  SettingSelect,
  SettingTextInput,
} from './shared.jsx';


export function OtherTab({
  settings,
  updateSetting,
}) {
  return (
    <>
          <ModuleHeader title="其他设置" />
          <section className="settings-section">
            <h3>运行</h3>
            <div className="settings-form-grid">
              <SettingSelect
                label="运行模式"
                options={RUNTIME_MODES}
                settings={settings}
                settingKey="runtimeMode"
                onUpdate={updateSetting}
                tooltip={
                  '每次运行独立进程（推荐）：多会话并发时每任务独立 Agent 进程，隔离更强、结束后回收。\n'
                  + '单核心：共享常驻进程，开销更低，不适合高并发隔离场景。'
                }
              />
              <SettingTextInput
                label="模型"
                placeholder="可选"
                settings={settings}
                settingKey="model"
                onUpdate={updateSetting}
                tooltip="可选覆盖本轮使用的模型名。留空则使用所选供应商配置中的模型。"
              />
            </div>
          </section>
          <section className="settings-section">
            <h3>工具与授权</h3>
            <div className="settings-form-grid">
              <SettingSelect
                label="工具策略"
                options={TOOL_POLICIES}
                settings={settings}
                settingKey="toolPolicy"
                onUpdate={updateSetting}
                tooltip="控制工具调用是否按风险询问、全部允许、全部询问或全部拒绝。"
              />
              <SettingSelect
                label="授权模式"
                options={PERMISSION_MODES}
                settings={settings}
                settingKey="permissionMode"
                onUpdate={updateSetting}
                tooltip="严格/宽松影响高风险工具是否需要用户确认；全部允许/拒绝覆盖默认策略。"
              />
              <SettingTextInput
                label="工具轮次上限"
                placeholder="12"
                settings={settings}
                settingKey="maxToolTurns"
                onUpdate={updateSetting}
                tooltip={
                  '约束入口 Worker 自身的工具循环次数。委托任务的轮次由对应 Worker Profile 配置。'
                }
              />
              <SettingTextInput
                label="最大并发会话运行数"
                placeholder="3"
                settings={settings}
                settingKey="maxConcurrentRuns"
                onUpdate={updateSetting}
                tooltip="限制同时进行的会话任务数量（默认 3，上限 16）。同一会话内任务仍串行。"
              />
              <SettingTextInput
                label="委托 Worker 池大小"
                placeholder="8"
                settings={settings}
                settingKey="workerPoolSize"
                onUpdate={updateSetting}
                tooltip={
                  '控制 worker.delegate 可同时使用的独立 Worker 槽位数量（1-8）。\n'
                  + '默认 8。分析大型代码库时可保持较高并发，支持并行委托。\n'
                  + '每个委托 Worker 占用一个独立 Agent 进程（per-run 模式）或槽位。'
                }
              />
              <SettingTextInput
                label="工具允许列表"
                placeholder="workspace.read_file, workspace.list"
                settings={settings}
                settingKey="toolAllowlist"
                onUpdate={updateSetting}
                tooltip="逗号分隔的工具名白名单。非空时仅允许列表中的工具（再叠加其他策略）。"
              />
              <SettingTextInput
                label="工具拒绝列表"
                placeholder="shell.exec, workspace.write_file"
                settings={settings}
                settingKey="toolDenylist"
                onUpdate={updateSetting}
                tooltip="逗号分隔的工具名黑名单。列表中的工具将被拒绝或需额外授权。"
              />
            </div>
          </section>
          <section className="settings-section">
            <h3>网络工具</h3>
            <div className="settings-form-grid">
              <SettingSelect
                label="搜索提供商"
                options={WEB_SEARCH_PROVIDERS}
                settings={settings}
                settingKey="webSearchProvider"
                onUpdate={updateSetting}
                tooltip={
                  'web.search 支持 Tavily 与 DuckDuckGo。\n'
                  + '自动：有 Tavily Key 时优先 Tavily。\n'
                  + 'Tavily 国内网络更稳定，可在 app.tavily.com 申请 Key，或设置环境变量 RED_PANDA_TAVILY_API_KEY。'
                }
              />
              <SettingTextInput
                label="搜索结果数量"
                placeholder="8"
                settings={settings}
                settingKey="webSearchResults"
                onUpdate={updateSetting}
                tooltip="单次 web.search 返回的结果条数上限。"
              />
              <div className="settings-form-span">
                <SettingTextInput
                  label="Tavily API Key"
                  placeholder="tvly-xxxxxxxx"
                  settings={settings}
                  settingKey="webTavilyApiKey"
                  onUpdate={updateSetting}
                  tooltip="Tavily 密钥（tvly-…）。也可使用环境变量 RED_PANDA_TAVILY_API_KEY。修改后重新发送任务生效。"
                />
              </div>
              <SettingTextInput
                label="抓取大小上限(字节)"
                placeholder="2097152"
                settings={settings}
                settingKey="webFetchMaxBytes"
                onUpdate={updateSetting}
                tooltip="web.fetch 响应体大小上限（字节），防止超大页面占满上下文。默认约 2MB。"
              />
              <div className="settings-form-span">
                <SettingTextInput
                  label="网络代理"
                  placeholder="http://127.0.0.1:7890"
                  settings={settings}
                  settingKey="webHttpProxy"
                  onUpdate={updateSetting}
                  tooltip={
                    '仅作用于 web.search / web.fetch。\n'
                    + '示例：http://127.0.0.1:7890 或 socks5://127.0.0.1:7891。\n'
                    + '修改后重新发送任务即可生效。'
                  }
                />
              </div>
            </div>
          </section>
        </>
  );
}
