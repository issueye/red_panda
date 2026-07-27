# 供应商添加设置网络代理 — 实施计划

## 目标
为每个供应商配置 (Provider Profile) 增加可选的**网络代理**字段(`http_proxy`),支持 `http://` / `https://` / `socks5://` / `socks5h://`(含鉴权)。代理在 Agent Runtime 构造 provider HTTP 客户端时应用;空值保留默认 `ProxyFromEnvironment` 行为。

**复用现有资产**: `modules/agent/internal/tools/web_http.go` 已有完整的代理逻辑(`applyWebProxy` / `parseWebProxyURL`,含全角标点兼容、SOCKS5 拨号超时)。这些函数目前是包内私有且位于 `tools` 包。考虑到 provider 包不应反向依赖 tools 包,本计划采用**在 provider 包内增加一个轻量专用代理应用函数**,而非把 web 工具函数搬出来(避免破坏现有 web 工具测试和包边界)。

---

## 后端改动 (Go)

### 1. 数据模型 — `modules/gateway/internal/gateway/model/models.go`
在 `ProviderProfile` 结构体新增字段(紧随 `SupportsVision`,line 250):
```go
// HTTPProxy 是可选的出站 HTTP(S)/SOCKS5 代理,用于该 profile 的 provider 请求。
// 空串表示回退到环境变量代理 (HTTP_PROXY/HTTPS_PROXY)。
HTTPProxy string
```

### 2. 数据库迁移 — `modules/gateway/internal/gateway/infra/database/migrate.go`
仿照 `migrateProviderProfileSupportsVision` 新增 `migrateProviderProfileHTTPProxy`(空串默认,无需 NOT NULL),并在 `Migrate()` 中(line 49 之后)调用。AutoMigrate 会自动为新库建列,迁移函数只负责老库补列。

### 3. 服务层 — `modules/gateway/internal/gateway/service/provider_profile.go`
- `ProviderProfileDTO` 加 `HTTPProxy string json:"http_proxy,omitempty"`(line 31 前)
- `ProviderProfileCreate` 加 `HTTPProxy string`
- `ProviderProfileUpdate` 加 `HTTPProxy *string`(允许显式清空)
- `Create()`: `strings.TrimSpace(input.HTTPProxy)` 后写入 row(line 100-111 块)
- `Update()`: `next.HTTPProxy = current.HTTPProxy` 初始化,然后 `if input.HTTPKey != nil { next.HTTPProxy = strings.TrimSpace(*input.HTTPProxy) }`(允许传空串清空,区别于"未提供")
- `providerProfileDTO()` 映射 `HTTPProxy: row.HTTPProxy`
- 可选:增加轻量校验 `validateHTTPProxy`,非空时检查 scheme ∈ {http,https,socks5,socks5h} 及 host 存在,无效返回错误(防止错误代理保存后让所有 run 失败)。**建议加入**,与 base_url 必填校验保持一致风格。

### 4. 仓储层 — `modules/gateway/internal/gateway/repository/provider_profile.go`
`Update()` 中 (line 76 后) 采用**始终应用**语义(类似 `SupportsVision`):
```go
// HTTPProxy 总是被写入,允许传空串清空。
current.HTTPProxy = profile.HTTPProxy
```
(因为 service 层已经处理了 nil = 不变的语义,repo 收到的值就是要落库的最终值。)

### 5. 控制器 — `modules/gateway/internal/gateway/controller/provider_profile.go`
- `providerProfileCreateRequest` 加 `HTTPProxy string json:"http_proxy"`
- `providerProfileUpdateRequest` 加 `HTTPProxy *string json:"http_proxy"`
- `Create`/`Update` 处理函数透传 `HTTPProxy: req.HTTPProxy`

### 6. 协议层 — `modules/protocol/methods/methods.go`
- `RunExecuteOptions` 加 `ProviderHTTPProxy string json:"provider_http_proxy,omitempty"`(紧邻 `ProviderAPIKey`,line 59 后)
- `ReplyOptions` 加 `ProviderHTTPProxy string json:"provider_http_proxy,omitempty"`(line 307 后)
- 注意:这与既有的 `WebHTTPProxy`(line 76/332,只管 web.search/web.fetch)**是独立字段**,语义不同,不复用。

### 7. Gateway → Runtime 桥接 — `modules/gateway/internal/gateway/service/run_start.go`
`applyProviderProfile()` (line 298 附近) 增加:
```go
params.Options.ProviderHTTPProxy = profile.HTTPProxy
```
(delegated worker 通过 profile ID 在网关侧解析,自动继承,无需改 worker_tools.go。)

### 8. Agent Runtime — provider 选项
**`modules/agent/internal/provider/provider.go`** `RequestOptions` 加 `ProviderHTTPProxy string`(line 59 后)。

**`modules/agent/internal/runtime/prompt_composer.go`** (line 85-93) 在构造 `provider.RequestOptions` 时复制字段:
```go
ProviderHTTPProxy: options.ProviderHTTPProxy,
```

### 9. Agent Runtime — HTTP 客户端构造(关键)
**`modules/agent/internal/provider/factory.go`**:
- 修改 `newProviderHTTPClient` 签名为 `newProviderHTTPClient(proxyURL string) (*http.Client, error)`
- 内部:克隆 `http.DefaultTransport`,trim proxy 字符串;非空时按 scheme 应用代理(直接复刻 `applyWebProxy` 的逻辑到 provider 包内一个新文件 `proxy.go`,约 80 行,含 `parseProxyURL`、`applyProxy`、SOCKS5 拨号超时;设置 `ForceAttemptHTTP2 = false` 因为部分代理会挂)。
- `providerFromOptions()` (line 64-67) 改为:
```go
client, err := newProviderHTTPClient(options.ProviderHTTPProxy)
if err != nil {
    return nil, false
}
config := providerConfig{ ..., Client: client }
```
- 解析失败时返回 `nil, false` → 上层会回退到 echo provider 并打印日志;或者在 Resolve 入口更早校验。**建议**:为避免静默回退掩盖配置错误,在 provider 包新增导出函数 `ValidateProxy(string) error`,由 gateway service 在保存时调用(见步骤 3 校验),运行时再次校验兜底。

**新增文件 `modules/agent/internal/provider/proxy.go`**: 包含 `applyProxy`、`parseProxyURL`、`ValidateProxy`、常量 `proxyDialTimeout` / `proxyTLSHandshakeTimeout`(独立于 web 工具的常量,避免跨包耦合)。导入 `golang.org/x/net/proxy`(已在 go.mod)。

---

## 前端改动 (React/JS)

### 10. 载荷与归一化 — `modules/desktop/frontend/src/lib/providerProfiles.js`
- `emptyProfileDraft` 加 `httpProxy: ''`
- `normalizeProviderProfile` 加 `httpProxy: item.http_proxy || ''`
- `profileDraftFrom` 加 `httpProxy: profile.httpProxy || ''`
- `providerProfileCreatePayload` 加 `http_proxy: String(input.httpProxy || '').trim()`
- `providerProfileUpdatePayload` 加 `http_proxy: String(input.httpProxy || '').trim()`(始终发送,允许清空)

### 11. 表单 UI — `modules/desktop/frontend/src/components/settings/ProvidersTab.jsx`
在 API 密钥 Field 之后(line 208 后)新增一个 `settings-form-span` 的 Field:
```jsx
<Field className="settings-row settings-form-span" label="网络代理">
  <input
    onChange={(event) => updateProfileDraft('httpProxy', event.target.value)}
    placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080（可选,留空使用环境代理）"
    type="text"
    value={profileDraft.httpProxy}
  />
</Field>
```
风格参考 `OtherTab.jsx` 既有"网络代理"字段(line 165-178)。

---

## 测试更新

### 后端测试
- `modules/gateway/internal/gateway/service/provider_profile_test.go`: 增加 create/update 透传 `http_proxy` 的用例(含清空场景、无效 scheme 拒绝)。
- `modules/gateway/internal/gateway/repository/provider_profile_test.go`: 验证 HTTPProxy 字段持久化与 update 覆盖。
- `modules/agent/internal/provider/factory_test.go` + 新增 `proxy_test.go`: 测试 `applyProxy` 对 http/https/socks5/空/非法的处理,以及 `providerFromOptions` 应用代理后 transport.Proxy 非空。

### 前端测试
- `modules/desktop/frontend/src/lib/providerProfiles.test.js`: 仿照 `supports_vision` 用例(line 97-119)增加 `http_proxy` 透传断言。

---

## 验证步骤
1. `cd modules/gateway && go build ./... && go test ./internal/gateway/service/... ./internal/gateway/repository/... ./internal/gateway/infra/database/...`
2. `cd modules/agent && go build ./... && go test ./internal/provider/...`
3. `cd modules/protocol && go build ./...`
4. `cd modules/desktop/frontend && npm test -- providerProfiles`(若配置)或 `npm run build`
5. 手动验证:启动 gateway,新建/编辑供应商,填入 `http://127.0.0.1:7890`,保存;触发一次 run;观察 provider 请求是否经代理(可看代理日志或故意填错代理看连接失败)。

---

## 文档
`README.md` 第 "Gateway provider profile backend" / "Desktop SettingsPanel" 条目补充"per-profile HTTP/SOCKS5 proxy"。如 `docs/` 下有 provider profile 设计文档,同步补充字段说明。

## 风险与权衡
- **不复用 tools 包的 `applyWebProxy`**:provider 包不应依赖 tools 包(运行时 tools 依赖 provider,反向依赖会成环)。新增 provider 包内约 80 行代理代码,逻辑与 web 工具一致,后续可考虑抽到独立共享包,但本次保持改动局部化。
- **始终发送 `http_proxy`(update 时)**:与 `supports_vision` 一致,允许用户清空代理。如果将来希望"省略即不变",可改 `*string`,但目前 API 风格更统一。
- **校验放在 gateway 保存时 + runtime 启动时**:双重校验避免错误代理静默让所有 run 失败。