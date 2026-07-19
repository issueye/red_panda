# Provider Model Reasoning Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let each provider profile manage multiple models with reasoning levels and let chat runs select both.

**Architecture:** Persist profile-owned model metadata as JSON while retaining legacy default-model columns. Pass the selected model and reasoning effort through the existing run option pipeline and map it at each provider transport.

**Tech Stack:** Go, GORM/SQLite, React, JavaScript, Playwright

---

### Task 1: Provider model persistence and API

**Files:**
- Modify: `modules/gateway/internal/gateway/model/models.go`
- Modify: `modules/gateway/internal/gateway/service/provider_profile.go`
- Modify: `modules/gateway/internal/gateway/controller/provider_profile.go`
- Test: `modules/gateway/internal/gateway/service/provider_profile_test.go`

Add the serialized model collection, normalize legacy profiles, validate effort/token values, and expose `models` in CRUD payloads.

### Task 2: Run and provider transport

**Files:**
- Modify: `modules/protocol/methods/methods.go`
- Modify: `modules/gateway/internal/gateway/service/run_start.go`
- Modify: `modules/agent/internal/provider/provider.go`
- Modify: `modules/agent/internal/runtime/prompt_composer.go`
- Modify: provider transport files and tests

Carry `reasoning_effort`, validate model membership, and emit provider-specific request JSON.

### Task 3: Desktop profile editor

**Files:**
- Modify: `modules/desktop/frontend/src/lib/providerProfiles.js`
- Modify: `modules/desktop/frontend/src/components/settings/ProvidersTab.jsx`
- Modify: related CSS and tests

Replace the single model input with editable model rows for ID, label, token limit, and default effort.

### Task 4: Composer selection

**Files:**
- Modify: `modules/desktop/frontend/src/components/chat/ChatComposer.jsx`
- Modify: chat prop plumbing, `App.jsx`, and `runOptions.js`
- Test: frontend unit and Playwright tests

Flatten active provider models into the model menu, add an effort menu, persist selection, and use model-specific token budgets.

### Task 5: Verification and packaging

Run targeted Go tests, frontend unit tests, Playwright visual checks at desktop/mobile widths, production build, and rebuild `bin/red_panda.exe`.
