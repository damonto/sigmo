import { readLimitedBody } from './body';

export const CARRIER_LOGO_CATALOG_KEY = 'carrier-logos/catalog.json';
export const CARRIER_LOGO_CRON = '17 3 * * *';
export const SIMDEX_ORIGIN = 'https://simdex.simb.workers.dev';
export const SIMDEX_INDEX_URL = `${SIMDEX_ORIGIN}/data/index.json`;

const CATALOG_SCHEMA_VERSION = 1;
const MIN_CATALOG_SIZE = 100;
const MAX_INDEX_BYTES = 4 * 1024 * 1024;
const FETCH_TIMEOUT_MS = 10_000;
const PLMN_PATTERN = /^[0-9]{5,6}$/;
const ICON_PATH_PATTERN =
  /^\/icons\/(?:[a-z0-9][a-z0-9._-]*\/)*[a-z0-9][a-z0-9._-]*\.(?:png|svg)$/i;

interface CarrierLogoCatalog {
  schemaVersion: typeof CATALOG_SCHEMA_VERSION;
  generatedAt: string;
  syncedAt: string;
  logos: Record<string, string>;
}

export interface CarrierLogoStats {
  carriers: number;
  ignoredEntries: number;
  logoMappings: number;
  plmns: number;
}

export function isPLMN(value: unknown): value is string {
  return typeof value === 'string' && PLMN_PATTERN.test(value);
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function iconPath(value: unknown): string | null {
  return typeof value === 'string' && ICON_PATH_PATTERN.test(value)
    ? value
    : null;
}

function parseStoredCatalog(value: unknown): CarrierLogoCatalog {
  const parsed = asRecord(value);
  if (
    !parsed ||
    parsed.schemaVersion !== CATALOG_SCHEMA_VERSION ||
    typeof parsed.generatedAt !== 'string' ||
    typeof parsed.syncedAt !== 'string'
  ) {
    throw new Error('invalid carrier logo catalog');
  }
  const logos = asRecord(parsed.logos);
  if (!logos) {
    throw new Error('invalid carrier logo catalog');
  }
  const entries = Object.entries(logos);
  if (entries.length === 0) {
    throw new Error('empty carrier logo catalog');
  }
  const validLogos: Record<string, string> = {};
  for (const [plmn, icon] of entries) {
    const validIcon = iconPath(icon);
    if (!isPLMN(plmn) || !validIcon) {
      throw new Error('invalid carrier logo catalog');
    }
    validLogos[plmn] = validIcon;
  }
  return {
    schemaVersion: CATALOG_SCHEMA_VERSION,
    generatedAt: parsed.generatedAt,
    syncedAt: parsed.syncedAt,
    logos: validLogos,
  };
}

function normalizeIndex(value: unknown): {
  catalog: CarrierLogoCatalog;
  stats: CarrierLogoStats;
} {
  const parsed = asRecord(value);
  if (
    !parsed ||
    typeof parsed.generatedAt !== 'string' ||
    parsed.generatedAt.trim() === '' ||
    !Array.isArray(parsed.carriers)
  ) {
    throw new Error('invalid simdex carrier index');
  }

  const seen = new Set<string>();
  const logos = new Map<string, string>();
  let ignoredEntries = 0;

  for (const value of parsed.carriers) {
    const carrier = asRecord(value);
    if (!carrier || !Array.isArray(carrier.plmns)) {
      ignoredEntries++;
      continue;
    }
    const icon = iconPath(carrier.icon);
    if (carrier.icon !== null && icon === null) {
      ignoredEntries++;
    }
    for (const value of carrier.plmns) {
      if (!isPLMN(value)) {
        ignoredEntries++;
        continue;
      }
      if (seen.has(value)) {
        continue;
      }
      seen.add(value);
      if (icon) {
        logos.set(value, icon);
      }
    }
  }

  if (seen.size < MIN_CATALOG_SIZE || logos.size < MIN_CATALOG_SIZE) {
    throw new Error('incomplete simdex carrier index');
  }

  if (Array.isArray(parsed.warnings) && parsed.warnings.length > 0) {
    console.warn(
      JSON.stringify({
        message: 'simdex carrier index warnings',
        count: parsed.warnings.length,
      }),
    );
  }

  return {
    catalog: {
      schemaVersion: CATALOG_SCHEMA_VERSION,
      generatedAt: parsed.generatedAt,
      syncedAt: new Date().toISOString(),
      logos: Object.fromEntries(logos),
    },
    stats: {
      carriers: parsed.carriers.length,
      ignoredEntries,
      logoMappings: logos.size,
      plmns: seen.size,
    },
  };
}

/** synchronizeCarrierLogos refreshes the PLMN catalog without touching old data on failure. */
export async function synchronizeCarrierLogos(
  bucket: R2Bucket,
): Promise<CarrierLogoStats> {
  const response = await fetch(SIMDEX_INDEX_URL, {
    headers: { accept: 'application/json' },
    redirect: 'manual',
    signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
  });
  if (!response.ok) {
    throw new Error(`fetch carrier logo index: HTTP ${response.status}`);
  }
  const bytes = await readLimitedBody(
    response,
    MAX_INDEX_BYTES,
    'simdex carrier index',
  );
  const value: unknown = JSON.parse(new TextDecoder().decode(bytes));
  const { catalog, stats } = normalizeIndex(value);
  const object = await bucket.put(
    CARRIER_LOGO_CATALOG_KEY,
    JSON.stringify(catalog),
    {
      httpMetadata: { contentType: 'application/json; charset=utf-8' },
    },
  );
  if (!object) {
    throw new Error('store carrier logo catalog');
  }
  console.info(JSON.stringify({ message: 'sync carrier logos', ...stats }));
  return stats;
}

/** loadCarrierLogoCatalog returns the current PLMN map and lazily seeds it after deployment. */
export async function loadCarrierLogoCatalog(
  bucket: R2Bucket,
): Promise<ReadonlyMap<string, string>> {
  let object = await bucket.get(CARRIER_LOGO_CATALOG_KEY);
  if (!object) {
    await synchronizeCarrierLogos(bucket);
    object = await bucket.get(CARRIER_LOGO_CATALOG_KEY);
  }
  if (!object) {
    throw new Error('carrier logo catalog unavailable');
  }
  const catalog = parseStoredCatalog(await object.json<unknown>());
  return new Map(Object.entries(catalog.logos));
}
