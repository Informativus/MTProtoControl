export function applyDiscoveryToInventoryDraft(draft, discovery) {
  if (!discovery || typeof discovery !== 'object') {
    return { ...draft };
  }

  return {
    ...draft,
    public_host: discovery.public_host || draft.public_host,
    mtproto_port: discovery.mtproto_port != null ? String(discovery.mtproto_port) : draft.mtproto_port,
    sni_domain: discovery.sni_domain || draft.sni_domain,
    remote_base_path: discovery.remote_base_path || draft.remote_base_path,
  };
}

export function applyDiscoveryToConfigDraft(draft, discovery) {
  if (!discovery || typeof discovery !== 'object') {
    return { ...draft };
  }

  return {
    ...draft,
    public_host: discovery.public_host || draft.public_host,
    public_port: discovery.mtproto_port != null ? String(discovery.mtproto_port) : draft.public_port,
    tls_domain: discovery.sni_domain || draft.tls_domain,
    secret: discovery.secret || draft.secret,
  };
}

export function buildDiscoveryServerPatch(server, discovery) {
  if (!server || !discovery || typeof discovery !== 'object') {
    return null;
  }

  const patch = {};
  const discoveredPublicHost = toNonEmptyText(discovery.public_host);
  const discoveredSNIDomain = toNonEmptyText(discovery.sni_domain);
  const discoveredRemoteBasePath = toNonEmptyText(discovery.remote_base_path);
  const discoveredPort = Number.isInteger(discovery.mtproto_port) ? discovery.mtproto_port : null;

  if (discoveredPublicHost && discoveredPublicHost !== toNonEmptyText(server.public_host)) {
    patch.public_host = discoveredPublicHost;
  }
  if (discoveredPort != null && discoveredPort !== toInteger(server.mtproto_port)) {
    patch.mtproto_port = discoveredPort;
  }
  if (discoveredSNIDomain && discoveredSNIDomain !== toNonEmptyText(server.sni_domain)) {
    patch.sni_domain = discoveredSNIDomain;
  }
  if (discoveredRemoteBasePath && discoveredRemoteBasePath !== toNonEmptyText(server.remote_base_path)) {
    patch.remote_base_path = discoveredRemoteBasePath;
  }

  return Object.keys(patch).length > 0 ? patch : null;
}

export function shouldSaveDiscoveredConfig(currentConfig, discovery) {
  const discoveredConfigText = normalizeConfigText(discovery?.config_text);
  if (discoveredConfigText === '') {
    return false;
  }

  return normalizeConfigText(currentConfig?.config_text) !== discoveredConfigText;
}

export function hasDiscoveredConfigText(discovery) {
  return normalizeConfigText(discovery?.config_text) !== '';
}

export function hasDiscoveredConfigFields(discovery) {
  return Boolean(
    toNonEmptyText(discovery?.public_host) ||
      toNonEmptyText(discovery?.sni_domain) ||
      toNonEmptyText(discovery?.secret) ||
      Number.isInteger(discovery?.mtproto_port),
  );
}

export function maskImportedSecret(secret) {
  const trimmed = typeof secret === 'string' ? secret.trim() : '';
  if (trimmed === '') {
    return '';
  }

  if (trimmed.length <= 12) {
    return trimmed;
  }

  return `${trimmed.slice(0, 8)}...${trimmed.slice(-4)}`;
}

function normalizeConfigText(value) {
  return toText(value).replace(/\r\n/g, '\n').trim();
}

function toNonEmptyText(value) {
  return toText(value).trim();
}

function toInteger(value) {
  return Number.parseInt(toText(value), 10) || 0;
}

function toText(value) {
  return value == null ? '' : String(value);
}
