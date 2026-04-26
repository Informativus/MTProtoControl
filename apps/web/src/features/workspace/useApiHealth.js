import { useEffect, useRef, useState } from 'react';

import { healthApi } from '../../api/healthApi.js';

const initialApiState = {
  state: 'checking',
  label: 'Проверка',
  detail: 'GET /health',
};

export function useApiHealth() {
  const [apiState, setApiState] = useState(initialApiState);
  const hasLoadedApiState = useRef(false);

  useEffect(() => {
    let ignore = false;

    async function checkApi({ silent = false } = {}) {
      if (!silent || !hasLoadedApiState.current) {
        setApiState(initialApiState);
      }

      try {
        const payload = await healthApi.getAppHealth();
        if (ignore) {
          return;
        }

        setApiState({
          state: 'online',
          label: 'В сети',
          detail: payload?.service || 'mtproxy-control-api',
        });
        hasLoadedApiState.current = true;
      } catch (error) {
        if (ignore) {
          return;
        }

        setApiState({
          state: 'offline',
          label: 'Недоступен',
          detail: error instanceof Error ? error.message : 'нет ответа',
        });
        hasLoadedApiState.current = true;
      }
    }

    void checkApi();
    const timer = window.setInterval(() => {
      void checkApi({ silent: true });
    }, 15000);

    return () => {
      ignore = true;
      window.clearInterval(timer);
    };
  }, []);

  return apiState;
}
