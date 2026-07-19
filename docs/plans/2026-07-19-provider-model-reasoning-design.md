# Provider Models and Reasoning Levels

## Requirements

- A provider profile owns one or more model configurations.
- Each model has an ID, optional display label, context-token limit, and default reasoning effort.
- The chat composer can switch the provider model and override its reasoning effort for the next run.
- Existing provider profiles and stored desktop settings remain valid.

## Decision

Store model configurations as a JSON-serialized slice on `ProviderProfile`. Keep the existing `Model` and `MaxTokens` columns as the default-model compatibility projection. Empty model lists are normalized from those legacy columns.

Run selection remains explicit and stateless:

```text
Provider profile + model + reasoning effort
                    |
                    v
               run.start options
                    |
                    v
        Gateway validates profile/model
                    |
                    v
       Agent maps effort to provider JSON
```

Reasoning effort values are `low`, `medium`, `high`, and `xhigh`; empty means the configured model default. OpenAI-compatible Chat uses `reasoning_effort`, OpenAI Responses uses `reasoning.effort`, and Anthropic uses `output_config.effort` with `xhigh` mapped to `max`.

## Alternatives

- Separate `provider_models` table: stronger relational constraints, but unnecessary CRUD and migration complexity for a small profile-owned collection.
- Comma-separated model names: rejected because it cannot safely carry per-model settings.

## Failure Modes

- Unknown model override: Gateway rejects the run instead of silently using another model.
- Unsupported reasoning value: profile writes and run starts reject it.
- Legacy profile: API returns one synthesized model from `model/max_tokens`.
- Deleted selected model: Desktop falls back to the profile default model and configured reasoning effort.

## Security

Model metadata contains no credentials. API-key masking and update behavior remain unchanged.
