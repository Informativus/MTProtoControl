import { request, withJsonBody } from './http.js';

export const deployApi = {
  preview(serverId, body) {
    return request(`/api/servers/${serverId}/deploy/preview`, withJsonBody(body, { method: 'POST' }));
  },

  apply(serverId, body) {
    return request(`/api/servers/${serverId}/deploy/apply`, withJsonBody(body, { method: 'POST' }));
  },
};
