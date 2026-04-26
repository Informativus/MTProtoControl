import { request, withJsonBody } from './http.js';

export const serversApi = {
  list() {
    return request('/api/servers');
  },

  create(body) {
    return request('/api/servers', withJsonBody(body, { method: 'POST' }));
  },

  update(serverId, body) {
    return request(`/api/servers/${serverId}`, withJsonBody(body, { method: 'PATCH' }));
  },

  remove(serverId) {
    return request(`/api/servers/${serverId}`, { method: 'DELETE' });
  },

  discover(body) {
    return request('/api/ssh/discover', withJsonBody(body, { method: 'POST' }));
  },

  testSsh(body) {
    return request('/api/ssh/test', withJsonBody(body, { method: 'POST' }));
  },

  getLatestSshTest(serverId) {
    return request(`/api/servers/${serverId}/ssh-test/latest`);
  },
};
