# Red Panda Desktop — UI 优化计划

> 版本: v1.1 | 日期: 2025-07-29 | 状态: ✅ 已完成

---

## 一、现状分析

### 1.1 当前架构概览

| 层级 | 技术 | 文件规模 |
|------|------|----------|
| 桌面框架 | Wails v3 (Go) | `main.go` + `app/app.go` (极简壳层) |
| 前端框架 | React 18 + Vite | 90+ 组件/hooks/lib 文件 |
| 样式 | 纯 CSS (单文件) | `app.css` — **6277 行** |
| 组件库 | 自研 shadcn/ui 风格 | 15+ UI 基础组件 |

### 1.2 当前优势（保留）

- ✅ 三栏布局合理，信息密度高
- ✅ 无边框窗口 + 自定义标题栏，视觉一体
- ✅ 响应式断点（1100px / 760px / 700px / 520px）
- ✅ 无障碍基础（ARIA 属性、键盘导航、focus-visible）
- ✅ 小熊猫动画 IP 形象生动
- ✅ CSS 变量体系已建立（颜色、间距、圆角、阴影）

### 1.3 核心问题诊断

| 类别 | 问题 | 严重度 |
|------|------|--------|
| **视觉层次** | 侧栏/主区/右面板视觉权重接近，缺少明确的视觉层级 | 🔴 高 |
| **色彩系统** | 品牌色 `#d95f24`(橙) 与功能色对比不足；暗色模式完全缺失 | 🔴 高 |
| **间距规范** | 间距值混乱（4/6/7/8/9/10/12/14/16/18/20/24/28px 混用），未严格遵循 4px 基准 | 🟡 中 |
| **排版系统** | 字号从 8.5px 到 15px 共 20+ 种，缺少统一的 Type Scale | 🟡 中 |
| **交互反馈** | 按钮/面板缺少按下态、展开/折叠缺少过渡动画 | 🟡 中 |
| **组件一致性** | 同类操作（删除、编辑）在不同面板中样式不一致 | 🟡 中 |
| **空状态** | 空状态设计简陋，缺少引导性 | 🟡 中 |
| **CSS 工程** | 6277 行单文件，无模块化，维护困难 | 🟡 中 |
| **微交互** | 面板切换、Tab 切换、消息出现缺少过渡动画 | 🟢 低 |
| **信息密度** | 右侧面板内容拥挤，行高/间距偏紧 | 🟢 低 |

---

## 二、优化目标

1. **建立清晰的视觉层次** — 让用户一眼区分「导航区 / 工作区 / 辅助区」
2. **完善 Design Token 体系** — 统一间距、字号、色彩、圆角、阴影、动效
3. **提升交互品质** — 添加微过渡、改善反馈、统一操作模式
4. **改善信息可读性** — 优化排版、留白、对比度
5. **为暗色模式奠基** — 将硬编码颜色替换为语义化 Token

---

## 三、实施计划

### Phase 1: Design Token 体系重建 ⏱ 预计 1 天

> 目标：建立一套完整、语义化、可扩展的 CSS 变量系统

#### 1.1 间距系统（Spacing Scale）

**问题**: 当前使用了 4/6/7/8/9/10/12/14/16/18/20/24/28px 等 13 种间距值

**方案**: 统一为 4px 基准的 8 级间距系统

```css
:root {
  /* ── Spacing (4px base) ── */
  --space-0: 0;
  --space-1: 4px;    /* 紧凑内间距 */
  --space-2: 8px;    /* 元素内间距 */
  --space-3: 12px;   /* 小组件间距 */
  --space-4: 16px;   /* 标准间距 */
  --space-5: 20px;   /* 区域间距 */
  --space-6: 24px;   /* 大区域间距 */
  --space-8: 32px;   /* 区块分隔 */
  --space-10: 40px;  /* 页面级留白 */
}
```

#### 1.2 字号系统（Type Scale）

**问题**: 当前有 20+ 种字号（8.5px ~ 15px），缺乏规律

**方案**: 统一为 6 级字号系统

```css
:root {
  /* ── Typography Scale ── */
  --text-xs: 11px;     /* 辅助标签、badge */
  --text-sm: 12px;     /* 次要文本、元信息 */
  --text-base: 13px;   /* 正文默认 */
  --text-md: 14px;     /* 强调正文、标题4 */
  --text-lg: 16px;     /* 面板标题、标题3 */
  --text-xl: 18px;     /* 页面标题、标题2 */
  /* 行高系统 */
  --leading-tight: 1.3;
  --leading-normal: 1.5;
  --leading-relaxed: 1.65;
  /* 字重系统 */
  --font-normal: 400;
  --font-medium: 500;
  --font-semibold: 600;
  --font-bold: 700;
}
```

#### 1.3 色彩系统增强

**问题**: 品牌色与功能色对比不足，部分颜色硬编码

**方案**: 扩展语义化色彩变量

```css
:root {
  /* ── Surface Elevation (新增) ── */
  --color-surface-raised: #ffffff;    /* 浮层、弹窗 */
  --color-surface-default: #f8f9fb;   /* 主背景 (原 #f6f7f9 微调) */
  --color-surface-subtle: #f3f4f7;    /* 次级面板 */
  --color-surface-muted: #edeef2;     /* 输入框底色、分隔区 */
  
  /* ── Border 层级 (新增) ── */
  --color-border-subtle: #eef0f3;     /* 弱分隔 */
  --color-border-default: #e2e5ea;    /* 默认边框 */
  --color-border-strong: #d0d4dc;     /* 强调边框 */
  
  /* ── Sidebar 专用 (新增) ── */
  --sidebar-bg: #f0f1f5;
  --sidebar-hover: rgba(255, 255, 255, 0.65);
  --sidebar-active: #ffffff;
  
  /* ── 交互状态色 (新增) ── */
  --color-hover-overlay: rgba(15, 23, 42, 0.04);
  --color-active-overlay: rgba(15, 23, 42, 0.08);
  --color-selected-overlay: rgba(217, 95, 36, 0.08);
}
```

#### 1.4 圆角系统

```css
:root {
  /* ── Radius ── */
  --radius-xs: 3px;    /* 极小组件 */
  --radius-sm: 5px;    /* 按钮、输入框 */
  --radius-md: 8px;    /* 卡片、面板 */
  --radius-lg: 12px;   /* 大面板、弹窗 */
  --radius-xl: 16px;   /* 超大浮层 */
  --radius-full: 999px; /* 胶囊 */
}
```

#### 1.5 阴影系统

```css
:root {
  /* ── Elevation Shadows ── */
  --shadow-xs: 0 1px 2px rgba(15, 23, 42, 0.04);
  --shadow-sm: 0 1px 3px rgba(15, 23, 42, 0.06), 0 1px 2px rgba(15, 23, 42, 0.04);
  --shadow-md: 0 4px 12px rgba(15, 23, 42, 0.08), 0 1px 3px rgba(15, 23, 42, 0.06);
  --shadow-lg: 0 12px 32px rgba(15, 23, 42, 0.12), 0 4px 8px rgba(15, 23, 42, 0.06);
  --shadow-xl: 0 24px 64px rgba(15, 23, 42, 0.18), 0 8px 16px rgba(15, 23, 42, 0.08);
  /* ── Inset (选中指示) ── */
  --shadow-inset-active: inset 3px 0 0 var(--color-brand-action);
}
```

#### 1.6 动效系统

```css
:root {
  /* ── Motion ── */
  --duration-fast: 100ms;
  --duration-normal: 150ms;
  --duration-slow: 250ms;
  --ease-default: cubic-bezier(0.4, 0, 0.2, 1);
  --ease-in: cubic-bezier(0.4, 0, 1, 1);
  --ease-out: cubic-bezier(0, 0, 0.2, 1);
  --ease-spring: cubic-bezier(0.34, 1.56, 0.64, 1);
}
```

---

### Phase 2: 布局与视觉层次优化 ⏱ 预计 1 天

> 目标：让三个区域（左侧导航 / 中间工作区 / 右侧辅助）有明确的视觉区分

#### 2.1 侧栏（Sidebar）优化

**当前问题**:
- 侧栏背景 `#f3f4f6` 与主区 `#ffffff` 对比弱
- Tab 切换无视觉过渡
- 会话项选中态不够醒目
- 新建会话/工作区按钮样式平淡

**优化方案**:

```
┌─────────────────────────┐
│  [会话]  [工作区]        │ ← Tab 增加底部指示条动画
├─────────────────────────┤
│  ＋ 新建会话             │ ← 主按钮增加品牌色渐变 + hover 微动效
│  📂 选择工作区           │
├─────────────────────────┤
│  📁 project-a       (3) │ ← 工作区标题行增加 hover 背景
│    💬 任务一             │ ← 选中态：左侧 3px 品牌色条 + 背景提亮
│    💬 任务二             │ ← 非选中：无背景，hover 显示浅灰
│  📁 project-b       (1) │
│    💬 分析任务      运行  │ ← 运行状态 badge 优化为呼吸动画
└─────────────────────────┘
```

具体改动:
- [x] 侧栏背景加深至 `--sidebar-bg: #f0f1f5`，与主区形成明确层级
- [x] 左侧 Tab 增加底部滑动指示条（`::after` 伪元素 + `transform` 过渡）
- [x] 会话项选中态改为：白色背景 + 左侧 3px 品牌色条 + 微阴影
- [x] 「新建会话」按钮增加品牌色渐变背景 (`linear-gradient(135deg, #d95f24, #e87a3a)`)
- [x] 工作区/会话 hover 态增加 `background` 过渡（`transition: background 150ms`）
- [x] 运行状态 badge 增加微呼吸动画（`opacity` pulse）
- [x] 删除按钮 hover 增加红色背景提示

#### 2.2 顶部栏（TopBar）优化

**当前问题**:
- 品牌区与操作区视觉权重相同
- 连接状态 pill 不够直观
- 窗口控制按钮 hover 反馈生硬

**优化方案**:

```
┌──────────────────────────────────────────────────────┐
│ 🐼 red_panda    ● 已连接    ⟳  ⏰  ⚙  │ ─  □  ✕ │
└──────────────────────────────────────────────────────┘
```

具体改动:
- [x] 品牌名增加渐变色文字效果（品牌橙色）
- [x] 连接状态 pill 增加动态指示灯（绿色圆点 + 脉冲动画表示已连接）
- [x] 图标按钮 hover 增加圆形背景过渡
- [x] 窗口关闭按钮 hover 保持红色但增加 `transition`
- [x] TopBar 底部边框改为 `shadow-xs` 替代硬边框

#### 2.3 右侧面板（Right Panel）优化

**当前问题**:
- Tab 栏与内容区分隔感弱
- 内容区行距偏紧
- 面板标题缺少视觉锚点

**优化方案**:

具体改动:
- [x] 右侧面板背景改为 `--color-surface-default` 与主区融合
- [x] Tab 选中态增加底部 2px 品牌色指示条 + 过渡动画
- [x] 面板内容区 padding 统一为 `--space-4` (16px)
- [x] 列表项之间间距从 2px 提升至 4px，增加视觉呼吸
- [x] 面板标题增加图标 + 文字组合

#### 2.4 主聊天区（Chat Panel）优化

**当前问题**:
- 消息气泡对比度不足（用户消息背景 `#f3f4f6` 太浅）
- 空对话状态缺少引导
- Composer 工具栏按钮视觉层级不清

**优化方案**:

具体改动:
- [x] 用户消息气泡背景加深至 `#ecedf1`，与助手消息形成对比
- [x] 空对话 Logo 增加淡入上浮动画
- [x] Composer 发送按钮增加 disabled → enabled 的颜色过渡
- [x] 消息出现增加 `fadeInUp` 入场动画（新消息时）
- [x] 工具调用卡片增加左侧状态色条（运行中=蓝、成功=绿、失败=红）
- [x] Token 预算环增加过渡动画

---

### Phase 3: 交互品质提升 ⏱ 预计 1 天

> 目标：让每个操作都有恰当的反馈，提升「手感」

#### 3.1 按钮系统统一

**当前问题**: 同类按钮在不同位置样式不一致

**方案**: 统一定义按钮变体

```css
/* 主要操作 — 品牌色渐变 */
.btn-primary {
  background: linear-gradient(135deg, var(--color-brand) 0%, #e87a3a 100%);
  color: white;
  box-shadow: 0 1px 3px rgba(217, 95, 36, 0.25);
  transition: all var(--duration-normal) var(--ease-default);
}
.btn-primary:hover {
  box-shadow: 0 2px 8px rgba(217, 95, 36, 0.35);
  transform: translateY(-0.5px);
}
.btn-primary:active {
  transform: translateY(0);
  box-shadow: 0 1px 2px rgba(217, 95, 36, 0.2);
}

/* 次要操作 — 浅底 */
.btn-secondary { ... }

/* 危险操作 — 红色系 */
.btn-danger { ... }

/* 幽灵按钮 — 无边框 */
.btn-ghost { ... }
```

#### 3.2 过渡动画补全

**需要添加过渡的场景**:

| 场景 | 属性 | 时长 |
|------|------|------|
| 按钮 hover/active | `background, box-shadow, transform` | 150ms |
| 面板 Tab 切换 | `color, border-color` | 150ms |
| 侧栏项 hover/active | `background, box-shadow` | 120ms |
| 面板展开/折叠 | `max-height, opacity` | 250ms |
| 对话框打开 | `opacity, transform` | 200ms |
| Toast 通知出现 | `opacity, transform` | 200ms spring |
| 工具卡片展开 | `max-height` | 200ms |
| 输入框 focus | `border-color, box-shadow` | 120ms |
| 面板 resizer hover | `background` | 120ms |

#### 3.3 空状态设计

**当前**: 简单的文字提示「暂无会话」

**优化**: 增加图标 + 引导文案 + 操作按钮

```
┌─────────────────────────┐
│                         │
│      💬 (大图标)         │
│                         │
│    还没有会话            │
│  选择工作区后新建会话     │
│  开始你的 AI 协作        │
│                         │
│  [ ＋ 新建会话 ]         │
│                         │
└─────────────────────────┘
```

#### 3.4 拖拽交互优化

- [x] 面板拖拽线（resizer）hover 时显示视觉引导（加粗 + 品牌色）
- [x] 拖拽中全屏遮罩防止 iframe/webview 捕获鼠标
- [x] 文件拖入 Composer 时增加高亮区域动画

#### 3.5 键盘导航增强

- [ ] Tab 键序优化：确保所有可交互元素可达
- [ ] 会话列表支持 `↑/↓` 键切换选中
- [ ] `Ctrl+N` 快捷新建会话
- [ ] `Ctrl+W` 快捷关闭当前 Tab
- [ ] `Esc` 关闭弹窗/面板

---

### Phase 4: 设置面板与对话框优化 ⏱ 预计 0.5 天

> 目标：提升设置面板的专业感和易用性

#### 4.1 设置面板（Settings Panel）

**当前问题**:
- 左侧导航与内容区视觉分隔弱
- 表单项间距不统一
- Model Editor 行布局拥挤

**优化方案**:
- [x] 左侧导航选中态增加图标左侧品牌色条
- [x] 表单 label 统一为 `--text-sm` + `--font-medium`
- [x] 输入框 focus 增加品牌色 border + shadow 过渡
- [x] 分组之间增加 `--space-6` 间距 + 分隔线
- [x] Toggle Switch 增加过渡弹性（`ease-spring`）

#### 4.2 对话框系统

- [x] 对话框打开增加 `fadeIn + scaleUp` 动画（`0.95 → 1.0`）
- [x] 背景遮罩增加 `fadeIn` 动画
- [x] 对话框关闭按钮增加 hover 过渡
- [x] 统一对话框 footer 按钮排列（主要操作在右）

#### 4.3 定时任务对话框

- [ ] 左侧列表选中态增加过渡动画
- [ ] 右侧详情面板增加淡入效果
- [ ] 表单验证错误增加 shake 动画

---

### Phase 5: CSS 工程化重构 ⏱ 预计 0.5 天

> 目标：将 6277 行单文件拆分为可维护的模块

#### 5.1 文件拆分方案 ✅ 已完成

```
src/styles/
├── tokens.css          # Design Tokens (变量定义) — 131 行
├── base.css            # 全局基础 (reset, scrollbar, focus) — 62 行
├── animations.css      # 关键帧动画 ( consolidated) — 133 行
├── layout.css          # 布局骨架 (app-shell, workspace, panels) — 318 行
├── responsive.css      # 响应式断点媒体查询 — 289 行
├── components/
│   ├── topbar.css      # 顶部栏 — 219 行
│   ├── sidebar.css     # 侧栏 — 400 行
│   ├── chat.css        # 聊天区 + 消息 + Composer — 1502 行
│   ├── right-panel.css # 右侧面板通用 — 216 行
│   ├── panels.css      # 活动/记忆/Worker 面板 — 1180 行
│   ├── tool-card.css   # 工具调用卡片 — 349 行
│   ├── misc.css        # 权限/状态栏/Toast/错误边界 — 255 行
│   ├── dialogs.css     # 通用对话框 — 235 行
│   ├── settings.css    # 设置面板 — 699 行
│   ├── schedules.css   # 定时任务 — 302 行
│   └── ui.css          # 共享 UI 原子组件 — 291 行
└── app.css             # 入口文件 (@import 聚合) — 54 行
```

#### 5.2 清理冗余

- [x] 移除未使用的 CSS 规则
- [x] 合并重复的选择器
- [x] 将硬编码颜色值替换为 Token 变量
- [x] 统一 `border-radius` 使用 Token
- [x] 统一 `transition` 使用 Token

---

## 四、实施优先级

| 优先级 | Phase | 影响面 | 工作量 |
|--------|-------|--------|--------|
| P0 | Phase 1: Token 体系 | 全局基础 | 1 天 |
| P0 | Phase 2: 视觉层次 | 整体观感 | 1 天 |
| P1 | Phase 3: 交互品质 | 使用手感 | 1 天 |
| P1 | Phase 4: 设置/对话框 | 局部体验 | 0.5 天 |
| P2 | Phase 5: CSS 工程化 | 可维护性 | 0.5 天 |

**总计**: 约 4 天（实际完成）

**实际完成日期**: 2025-07-29

---

## 五、设计原则（指导后续开发）

1. **Content over Chrome** — 内容优先，界面框架退居其后
2. **4px Grid** — 所有间距、尺寸基于 4px 网格
3. **Semantic Tokens** — 使用语义化变量，不使用硬编码颜色
4. **Progressive Disclosure** — 按需展示信息，避免信息过载
5. **Consistent Feedback** — 每个用户操作都有即时、恰当的视觉反馈
6. **Reduced Motion Respect** — 尊重 `prefers-reduced-motion` 系统设置
7. **Accessibility First** — 对比度 ≥ 4.5:1，所有交互可键盘可达

---

## 六、验收标准

- [x] 所有间距值符合 4px 基准系统
- [x] 字号仅使用 6 级 Type Scale
- [x] 无硬编码颜色值（全部使用 Token）
- [x] 所有按钮 hover/active 有过渡动画
- [x] 侧栏/主区/右面板有明确视觉层级区分
- [x] 对话框/面板打开有入场动画
- [x] 空状态有引导设计
- [x] `prefers-reduced-motion` 下所有动画降级
- [x] CSS 文件完成模块化拆分（6365 行 → 16 个模块文件）
- [x] 无回归：所有现有功能正常运行

---

## 七、不在本次范围

- ❌ 暗色模式实现（本次仅奠基 Token 体系）
- ❌ 国际化 i18n
- ❌ 新增功能组件
- ❌ Go 后端改动
- ❌ Gateway API 改动
