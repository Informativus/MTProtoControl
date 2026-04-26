import { request, withJsonBody } from './http.js';

export const configsApi = {
  getCurrent(serverId) {
    return request(`/api/servers/${serverId}/configs/current`);
  },

  generate(serverId, body) {
    return request(`/api/servers/${serverId}/configs/generate`, withJsonBody(body, { method: 'POST' }));
  },

  saveCurrent(serverId, body) {
    return request(`/api/servers/${serverId}/configs/current`, withJsonBody(body, { method: 'PUT' }));
  },
};
