import { request, withJsonBody } from './http.js';

export const settingsApi = {
  getTelegram() {
    return request('/api/settings/telegram');
  },

  saveTelegram(body) {
    return request('/api/settings/telegram', withJsonBody(body, { method: 'PUT' }));
  },

  sendTelegramTest() {
    return request('/api/settings/telegram/test', { method: 'POST' });
  },
};
