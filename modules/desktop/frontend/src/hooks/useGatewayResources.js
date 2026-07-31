import { useCallback, useMemo, useState } from 'react';
import { apiJson } from '../lib/api.js';
import { normalizeAgentList } from '../lib/agents.js';
import {
  mcpCallPayload,
  mcpServerCreatePayload,
  mcpServerUpdatePayload,
  normalizeMcpCallResult,
  normalizeMcpDiscovery,
  normalizeMcpServer,
} from '../lib/mcpServers.js';
import {
  normalizeProviderProfile,
  providerProfileCreatePayload,
  providerProfileUpdatePayload,
} from '../lib/providerProfiles.js';
import {
  normalizeSkillDetail,
  normalizeSkillsList,
  skillCreatePayload,
  skillUpdatePayload,
} from '../lib/skills.js';

/**
 * Settings-facing Gateway resources: provider profiles, Worker profiles, MCP, skills.
 * Keeps CRUD + load state out of App.jsx (checklist R7a).
 *
 * @param {{ getWorkspaceRoot: () => string, setRunSettings: Function }} options
 */
export function useGatewayResources({ getWorkspaceRoot, setRunSettings }) {
  const [providerProfiles, setProviderProfiles] = useState([]);
  const [providerProfilesLoading, setProviderProfilesLoading] = useState(false);
  const [providerProfilesError, setProviderProfilesError] = useState('');

  const [workerProfiles, setWorkerProfiles] = useState([]);
  const [workerProfilesLoading, setWorkerProfilesLoading] = useState(false);
  const [workerProfilesError, setWorkerProfilesError] = useState('');

  const [mcpServers, setMcpServers] = useState([]);
  const [mcpServersLoading, setMcpServersLoading] = useState(false);
  const [mcpServersError, setMcpServersError] = useState('');
  const [mcpDiscoveryByServer, setMcpDiscoveryByServer] = useState({});

  const [skills, setSkills] = useState([]);
  const [skillsLoading, setSkillsLoading] = useState(false);
  const [skillsError, setSkillsError] = useState('');

  const loadProviderProfiles = useCallback(async () => {
    setProviderProfilesLoading(true);
    setProviderProfilesError('');
    try {
      const items = await apiJson('/api/v1/provider-profiles');
      const normalized = Array.isArray(items) ? items.map(normalizeProviderProfile) : [];
      setProviderProfiles(normalized);
      setRunSettings((current) => {
        if (!current.providerProfileId) {
          return current;
        }
        const profile = normalized.find((item) => item.id === current.providerProfileId);
        if (!profile) {
          return { ...current, providerProfileId: '', model: '', reasoningEffort: '' };
        }
        const configured = profile.models?.some((item) => item.model === current.model);
        return configured
          ? current
          : { ...current, model: profile.model || profile.models?.[0]?.model || '', reasoningEffort: '' };
      });
      return normalized;
    } catch (error) {
      setProviderProfilesError(error.message);
      return [];
    } finally {
      setProviderProfilesLoading(false);
    }
  }, [setRunSettings]);

  const createProviderProfile = useCallback(async (input) => {
    const created = await apiJson('/api/v1/provider-profiles', {
      method: 'POST',
      body: JSON.stringify(providerProfileCreatePayload(input)),
    });
    const normalized = normalizeProviderProfile(created);
    setProviderProfiles((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setRunSettings((current) => ({
      ...current,
      providerProfileId: normalized.id,
      model: normalized.model || normalized.models?.[0]?.model || '',
      reasoningEffort: '',
    }));
    return normalized;
  }, [setRunSettings]);

  const updateProviderProfile = useCallback(async (id, input) => {
    const updated = await apiJson(`/api/v1/provider-profiles/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(providerProfileUpdatePayload(input)),
    });
    const normalized = normalizeProviderProfile(updated);
    setProviderProfiles((items) => [
      normalized,
      ...items.filter((item) => item.id !== normalized.id),
    ]);
    setRunSettings((current) => {
      if (current.providerProfileId !== normalized.id) return current;
      const configured = normalized.models?.some((item) => item.model === current.model);
      return configured ? current : {
        ...current,
        model: normalized.model || normalized.models?.[0]?.model || '',
        reasoningEffort: '',
      };
    });
    return normalized;
  }, [setRunSettings]);

  const deleteProviderProfile = useCallback(async (id) => {
    await apiJson(`/api/v1/provider-profiles/${encodeURIComponent(id)}`, { method: 'DELETE' });
    setProviderProfiles((items) => items.filter((item) => item.id !== id));
    setRunSettings((current) => (
      current.providerProfileId === id
        ? { ...current, providerProfileId: '', model: '', reasoningEffort: '' }
        : current
    ));
  }, [setRunSettings]);

  const listProviderModels = useCallback(async (input) => {
    const data = await apiJson('/api/v1/provider-profiles/models', {
      method: 'POST',
      body: JSON.stringify({
        profile_id: input.profileId || '',
        base_url: input.baseUrl?.trim() || '',
        api_key: input.apiKey?.trim() || '',
        http_proxy: input.httpProxy?.trim() || '',
      }),
    });
    return Array.isArray(data?.models) ? data.models : [];
  }, []);

  const loadWorkerProfiles = useCallback(async () => {
    setWorkerProfilesLoading(true);
    setWorkerProfilesError('');
    try {
      const data = await apiJson('/api/v1/worker-profiles');
      const normalized = normalizeAgentList(data);
      setWorkerProfiles(normalized);
      return normalized;
    } catch (error) {
      setWorkerProfilesError(error.message);
      return [];
    } finally {
      setWorkerProfilesLoading(false);
    }
  }, []);

  const createWorkerProfile = useCallback(async (payload) => {
    const created = await apiJson('/api/v1/worker-profiles', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
    await loadWorkerProfiles();
    return created;
  }, [loadWorkerProfiles]);

  const updateWorkerProfile = useCallback(async (id, payload) => {
    const updated = await apiJson(`/api/v1/worker-profiles/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
    await loadWorkerProfiles();
    return updated;
  }, [loadWorkerProfiles]);

  const deleteWorkerProfile = useCallback(async (id) => {
    await apiJson(`/api/v1/worker-profiles/${encodeURIComponent(id)}`, { method: 'DELETE' });
    await loadWorkerProfiles();
  }, [loadWorkerProfiles]);

  const loadMcpServers = useCallback(async () => {
    setMcpServersLoading(true);
    setMcpServersError('');
    try {
      const data = await apiJson('/api/v1/mcp/servers');
      const normalized = Array.isArray(data?.servers)
        ? data.servers.map(normalizeMcpServer)
        : [];
      setMcpServers(normalized);
      return normalized;
    } catch (error) {
      setMcpServersError(error.message);
      return [];
    } finally {
      setMcpServersLoading(false);
    }
  }, []);

  const createMcpServer = useCallback(async (input) => {
    setMcpServersError('');
    try {
      const created = await apiJson('/api/v1/mcp/servers', {
        method: 'POST',
        body: JSON.stringify(mcpServerCreatePayload(input)),
      });
      const normalized = normalizeMcpServer(created);
      setMcpServers((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
      return normalized;
    } catch (error) {
      setMcpServersError(error.message);
      throw error;
    }
  }, []);

  const updateMcpServer = useCallback(async (id, input) => {
    setMcpServersError('');
    try {
      const updated = await apiJson(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, {
        method: 'PUT',
        body: JSON.stringify(mcpServerUpdatePayload(input)),
      });
      const normalized = normalizeMcpServer(updated);
      setMcpServers((items) => [
        normalized,
        ...items.filter((item) => item.id !== normalized.id),
      ]);
      return normalized;
    } catch (error) {
      setMcpServersError(error.message);
      throw error;
    }
  }, []);

  const deleteMcpServer = useCallback(async (id) => {
    setMcpServersError('');
    try {
      await apiJson(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, { method: 'DELETE' });
      setMcpServers((items) => items.filter((item) => item.id !== id));
      setMcpDiscoveryByServer((current) => {
        const next = { ...current };
        delete next[id];
        return next;
      });
    } catch (error) {
      setMcpServersError(error.message);
      throw error;
    }
  }, []);

  const discoverMcpServer = useCallback(async (id, workspaceRootOverride) => {
    setMcpDiscoveryByServer((current) => ({
      ...current,
      [id]: { ...current[id], loading: true, error: '' },
    }));
    try {
      const workspaceRoot = workspaceRootOverride || getWorkspaceRoot?.() || '';
      const data = await apiJson(`/api/v1/mcp/servers/${encodeURIComponent(id)}/discover`, {
        method: 'POST',
        body: JSON.stringify(workspaceRoot ? { workspace_root: workspaceRoot } : {}),
      });
      const result = normalizeMcpDiscovery(data);
      setMcpDiscoveryByServer((current) => ({
        ...current,
        [id]: { loading: false, error: '', result },
      }));
      return result;
    } catch (error) {
      setMcpDiscoveryByServer((current) => ({
        ...current,
        [id]: { ...current[id], loading: false, error: error.message },
      }));
      throw error;
    }
  }, [getWorkspaceRoot]);

  const callMcpTool = useCallback(async (id, { toolName, arguments: args, workspaceRoot: workspaceRootOverride } = {}) => {
    const workspaceRoot = workspaceRootOverride || getWorkspaceRoot?.() || '';
    const data = await apiJson(`/api/v1/mcp/servers/${encodeURIComponent(id)}/call`, {
      method: 'POST',
      body: JSON.stringify(mcpCallPayload({
        toolName,
        arguments: args,
        workspaceRoot,
      })),
    });
    return normalizeMcpCallResult(data);
  }, [getWorkspaceRoot]);

  const loadSkills = useCallback(async (workspaceRootOverride) => {
    const root = workspaceRootOverride || getWorkspaceRoot?.() || '';
    setSkillsLoading(true);
    setSkillsError('');
    if (!root) {
      setSkills([]);
      setSkillsLoading(false);
      setSkillsError('打开工作区后可管理托管技能。');
      return [];
    }
    try {
      const data = await apiJson(`/api/v1/skills?workspace_root=${encodeURIComponent(root)}`);
      const normalized = normalizeSkillsList(data);
      setSkills(normalized);
      return normalized;
    } catch (error) {
      setSkillsError(error.message);
      return [];
    } finally {
      setSkillsLoading(false);
    }
  }, [getWorkspaceRoot]);

  const loadSkillDetail = useCallback(async (name) => {
    const root = getWorkspaceRoot?.() || '';
    if (!root || !name) {
      throw new Error('workspace and skill name are required');
    }
    const data = await apiJson(
      `/api/v1/skills/${encodeURIComponent(name)}?workspace_root=${encodeURIComponent(root)}&include_instructions=1`,
    );
    return normalizeSkillDetail(data);
  }, [getWorkspaceRoot]);

  const createSkill = useCallback(async (input) => {
    setSkillsError('');
    try {
      const created = await apiJson('/api/v1/skills', {
        method: 'POST',
        body: JSON.stringify(skillCreatePayload(input, getWorkspaceRoot?.() || '')),
      });
      await loadSkills();
      return created;
    } catch (error) {
      setSkillsError(error.message);
      throw error;
    }
  }, [getWorkspaceRoot, loadSkills]);

  const updateSkill = useCallback(async (name, input) => {
    setSkillsError('');
    try {
      const updated = await apiJson(`/api/v1/skills/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(skillUpdatePayload(input, getWorkspaceRoot?.() || '')),
      });
      await loadSkills();
      return updated;
    } catch (error) {
      setSkillsError(error.message);
      throw error;
    }
  }, [getWorkspaceRoot, loadSkills]);

  const deleteSkill = useCallback(async (name) => {
    setSkillsError('');
    const root = getWorkspaceRoot?.() || '';
    try {
      await apiJson(
        `/api/v1/skills/${encodeURIComponent(name)}?workspace_root=${encodeURIComponent(root)}`,
        { method: 'DELETE' },
      );
      setSkills((items) => items.filter((item) => item.name !== name));
    } catch (error) {
      setSkillsError(error.message);
      throw error;
    }
  }, [getWorkspaceRoot]);

  const providers = useMemo(() => ({
    items: providerProfiles,
    loading: providerProfilesLoading,
    error: providerProfilesError,
    load: loadProviderProfiles,
    create: createProviderProfile,
    update: updateProviderProfile,
    remove: deleteProviderProfile,
    listModels: listProviderModels,
  }), [
    providerProfiles,
    providerProfilesLoading,
    providerProfilesError,
    loadProviderProfiles,
    createProviderProfile,
    updateProviderProfile,
    deleteProviderProfile,
    listProviderModels,
  ]);

  const workers = useMemo(() => ({
    items: workerProfiles,
    loading: workerProfilesLoading,
    error: workerProfilesError,
    load: loadWorkerProfiles,
    create: createWorkerProfile,
    update: updateWorkerProfile,
    remove: deleteWorkerProfile,
  }), [
    workerProfiles,
    workerProfilesLoading,
    workerProfilesError,
    loadWorkerProfiles,
    createWorkerProfile,
    updateWorkerProfile,
    deleteWorkerProfile,
  ]);

  const mcp = useMemo(() => ({
    items: mcpServers,
    loading: mcpServersLoading,
    error: mcpServersError,
    discoveryById: mcpDiscoveryByServer,
    load: loadMcpServers,
    create: createMcpServer,
    update: updateMcpServer,
    remove: deleteMcpServer,
    discover: discoverMcpServer,
    callTool: callMcpTool,
  }), [
    mcpServers,
    mcpServersLoading,
    mcpServersError,
    mcpDiscoveryByServer,
    loadMcpServers,
    createMcpServer,
    updateMcpServer,
    deleteMcpServer,
    discoverMcpServer,
    callMcpTool,
  ]);

  const skillsResource = useMemo(() => ({
    items: skills,
    loading: skillsLoading,
    error: skillsError,
    load: loadSkills,
    loadDetail: loadSkillDetail,
    create: createSkill,
    update: updateSkill,
    remove: deleteSkill,
  }), [
    skills,
    skillsLoading,
    skillsError,
    loadSkills,
    loadSkillDetail,
    createSkill,
    updateSkill,
    deleteSkill,
  ]);

  return useMemo(() => ({
    providers,
    workers,
    mcp,
    skills: skillsResource,
  }), [providers, workers, mcp, skillsResource]);
}
