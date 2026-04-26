import { API_BASE_URL } from '../config.js';
import { buildOperationsUrl } from '../server-operations.js';

import { request, requestUrl, withJsonBody } from './http.js';

export const operationsApi = {
  getStatus(serverId, authFields) {
    return requestUrl(buildOperationsUrl(API_BASE_URL, serverId, '/status', authFields));
  },

  getLink(serverId, authFields) {
    return requestUrl(buildOperationsUrl(API_BASE_URL, serverId, '/link', authFields));
  },

  getLogs(serverId, authFields, options = {}) {
    return requestUrl(buildOperationsUrl(API_BASE_URL, serverId, '/logs', authFields, options));
  },

  getLogsStreamUrl(serverId, authFields, options = {}) {
    return buildOperationsUrl(API_BASE_URL, serverId, '/logs/stream', authFields, options);
  },

  restart(serverId, body) {
    return request(`/api/servers/${serverId}/restart`, withJsonBody(body, { method: 'POST' }));
  },
};
