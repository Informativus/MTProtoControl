import { useEffect, useState } from 'react';

import { healthApi } from '../../api/healthApi.js';

const defaultHealthSettings = {
  interval: '30s',
  interval_seconds: 30,
};

export function useServerHealth({ selectedServerId, setServers, setServerStatus }) {
  const [healthSettings, setHealthSettings] = useState(defaultHealthSettings);
  const [healthHistory, setHealthHistory] = useState([]);
  const [healthLoading, setHealthLoading] = useState(false);
  const [healthError, setHealthError] = useState('');

  useEffect(() => {
    let ignore = false;

    async function loadHealthSettings() {
      try {
        const payload = await healthApi.getHealthcheckSettings();
        if (ignore) {
          return;
        }

        setHealthSettings(payload || defaultHealthSettings);
      } catch {
        if (!ignore) {
          setHealthSettings(defaultHealthSettings);
        }
      }
    }

    void loadHealthSettings();

    return () => {
      ignore = true;
    };
  }, []);

  useEffect(() => {
    if (!selectedServerId) {
      setHealthHistory([]);
      setHealthError('');
      setHealthLoading(false);
      return undefined;
    }

    let ignore = false;

    function applyHealthPayload(serverId, payload) {
      const checks = Array.isArray(payload?.checks) ? payload.checks : [];
      const latest = payload?.latest || checks[0] || null;

      setHealthHistory(checks);
      setServers((current) =>
        current.map((server) => {
          if (server.id !== serverId || !latest) {
            return server;
          }

          return {
            ...server,
            status: latest.status || server.status,
            last_checked_at: latest.created_at || server.last_checked_at,
          };
        }),
      );
      if (latest) {
        setServerStatus((current) => (current ? { ...current, latest_health: latest } : current));
      }
    }

    async function loadHealthHistory({ silent = false } = {}) {
      if (!silent) {
        setHealthLoading(true);
      }
      setHealthError('');

      try {
        const payload = await healthApi.getServerHistory(selectedServerId, { limit: 12 });
        if (ignore) {
          return;
        }

        applyHealthPayload(selectedServerId, payload);
      } catch (error) {
        if (ignore) {
          return;
        }

        setHealthError(error instanceof Error ? error.message : 'Не удалось загрузить историю проверок.');
        if (!silent) {
          setHealthHistory([]);
        }
      } finally {
        if (!ignore && !silent) {
          setHealthLoading(false);
        }
      }
    }

    void loadHealthHistory();
    const timer = window.setInterval(() => {
      void loadHealthHistory({ silent: true });
    }, 15000);

    return () => {
      ignore = true;
      window.clearInterval(timer);
    };
  }, [selectedServerId, setServerStatus, setServers]);

  return {
    healthError,
    healthHistory,
    healthLoading,
    healthSettings,
  };
}
