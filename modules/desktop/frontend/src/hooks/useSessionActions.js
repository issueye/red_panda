import { useSessionCompactionActions } from './useSessionCompactionActions.js';
import { useSessionCrudActions } from './useSessionCrudActions.js';
import { useSessionRunActions } from './useSessionRunActions.js';

/**
 * Stable facade for session lifecycle and run actions.
 * Domain behavior lives in focused hooks so App.jsx can keep its existing contract.
 */
export function useSessionActions(options) {
  const crudActions = useSessionCrudActions(options);
  const compactionActions = useSessionCompactionActions(options);
  const runActions = useSessionRunActions(options);

  return {
    ...crudActions,
    ...compactionActions,
    ...runActions,
  };
}
