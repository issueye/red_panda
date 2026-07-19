import { useCallback } from 'react';
import {
  closeConversationTabState,
  openWorkerConversationState,
} from '../lib/conversationTabs.js';

/**
 * Worker conversation tab open/close for the focused session (docs/47 D2).
 */
export function useConversationTabs({ patchCurrentRuntime }) {
  const openWorkerConversation = useCallback((assignment) => {
    patchCurrentRuntime((rt) => openWorkerConversationState(rt, assignment));
  }, [patchCurrentRuntime]);

  const closeConversationTab = useCallback((tabId) => {
    patchCurrentRuntime((rt) => closeConversationTabState(rt, tabId));
  }, [patchCurrentRuntime]);

  return { openWorkerConversation, closeConversationTab };
}
