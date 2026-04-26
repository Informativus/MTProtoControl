import { request } from './http.js';

export const healthApi = {
  getAppHealth() {
    return request('/health', {
      getErrorMessage({ response }) {
        return `HTTP ${response.status}`;
      },
    });
  },

  getHealthcheckSettings() {
    return request('/api/healthchecks/settings');
  },

  getServerHistory(serverId, { limit = 12 } = {}) {
    return request(`/api/servers/${serverId}/health?limit=${limit}`);
  },
};
