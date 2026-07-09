export const emptyMemoryDraft = {
  scope: 'project',
  kind: 'fact',
  status: 'active',
  title: '',
  content: '',
  confidence: 'medium',
};

export function normalizeMemoryRecord(item) {
  return {
    id: item.id,
    scope: item.scope || 'project',
    kind: item.kind || 'fact',
    status: item.status || 'active',
    title: item.title || item.id,
    content: item.content || '',
    confidence: item.confidence || 'medium',
    workspaceRoot: item.workspace_root || '',
    sessionId: item.session_id || '',
    runId: item.run_id || '',
    source: item.source || 'user',
    metadata: item.metadata || {},
    createdAt: item.created_at,
    updatedAt: item.updated_at,
    deletedAt: item.deleted_at,
  };
}

export function memoryDraftFrom(item) {
  if (!item) return emptyMemoryDraft;
  return {
    scope: item.scope || 'project',
    kind: item.kind || 'fact',
    status: item.status || 'active',
    title: item.title || '',
    content: item.content || '',
    confidence: item.confidence || 'medium',
  };
}

export function memoryCreatePayload(input, context = {}) {
  const scope = input.scope || 'project';
  return stripEmpty({
    scope,
    kind: input.kind || 'fact',
    status: input.status || 'active',
    title: input.title,
    content: input.content,
    confidence: input.confidence || 'medium',
    workspace_root: scope === 'project' ? context.workspaceRoot : '',
    session_id: scope === 'session' ? context.sessionId : '',
    source: 'user',
  });
}

export function memoryUpdatePayload(input) {
  return stripUndefined({
    scope: input.scope,
    kind: input.kind,
    status: input.status,
    title: input.title,
    content: input.content,
    confidence: input.confidence,
  });
}

export function memoryPreviewPayload(context = {}) {
  return {
    session_id: context.sessionId || '',
    workspace_root: context.workspaceRoot || '',
    input: context.input || '',
  };
}

export function memoryListQuery({ scope, status, workspaceRoot, sessionId } = {}) {
  const params = new URLSearchParams();
  if (scope) params.set('scope', scope);
  if (status && status !== 'active') params.set('status', status);
  if (scope === 'project' && workspaceRoot) params.set('workspace_root', workspaceRoot);
  if (scope === 'session' && sessionId) params.set('session_id', sessionId);
  const query = params.toString();
  return query ? `/api/v1/memory?${query}` : '/api/v1/memory';
}

function stripUndefined(value) {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined));
}

function stripEmpty(value) {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined && item !== ''));
}
