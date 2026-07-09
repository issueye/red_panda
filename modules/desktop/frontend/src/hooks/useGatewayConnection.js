import { useCallback, useEffect, useRef, useState } from 'react';
import { GatewayWebSocketClient } from '../lib/wsClient.js';

export function useGatewayConnection({ baseUrl, token, onEvent, resumeCursors }) {
  const clientRef = useRef(null);
  const onEventRef = useRef(onEvent);
  const resumeCursorsRef = useRef(resumeCursors);
  const [status, setStatus] = useState('idle');
  const [lastError, setLastError] = useState('');

  onEventRef.current = onEvent;
  resumeCursorsRef.current = resumeCursors;

  const connect = useCallback(() => {
    setLastError('');
    const client = new GatewayWebSocketClient({
      baseUrl,
      token,
      onStatus: setStatus,
      onEvent: (event) => onEventRef.current?.(event),
      onError: (error) => setLastError(error.message || String(error)),
      getResumeCursors: () => resumeCursorsRef.current,
    });
    clientRef.current = client;
    client.connect();
  }, [baseUrl, token]);

  const request = useCallback((method, payload) => {
    return clientRef.current?.request(method, payload);
  }, []);

  const reconnect = useCallback(() => {
    setLastError('');
    clientRef.current?.reconnect();
  }, []);

  useEffect(() => {
    connect();
    return () => clientRef.current?.close();
  }, [connect]);

  const activeRunIDs = Object.keys(resumeCursors || {}).sort().join(',');
  useEffect(() => {
    if (status === 'connected' && activeRunIDs) {
      clientRef.current?.resume().catch((error) => {
        setLastError(error.message || String(error));
      });
    }
  }, [activeRunIDs, status]);

  return { status, lastError, request, reconnect };
}
