import * as fs from 'fs';
import * as path from 'path';

// ─── Types ────────────────────────────────────────────────────────────────────

export interface LoomValidationConfig {
  require_objective: boolean;
  require_format: boolean;
  require_contract: boolean;
  warn_on_empty_context: boolean;
  warn_on_deep_inheritance: boolean;
  max_inheritance_depth: number;
  smell_constraint_limit: number;
  token_limit_warn: number;
}

export interface LoomConfig {
  validation: LoomValidationConfig;
}

export const DEFAULT_CONFIG: LoomConfig = {
  validation: {
    require_objective: true,
    require_format: true,
    require_contract: false,
    warn_on_empty_context: true,
    warn_on_deep_inheritance: true,
    max_inheritance_depth: 3,
    smell_constraint_limit: 25,
    token_limit_warn: 0,
  },
};

// ─── Simple TOML value parser ─────────────────────────────────────────────────

function parseTomlBool(v: string): boolean | undefined {
  if (v === 'true')  return true;
  if (v === 'false') return false;
  return undefined;
}

function parseTomlInt(v: string): number | undefined {
  const n = parseInt(v, 10);
  return isNaN(n) ? undefined : n;
}

// ─── Config file reader ───────────────────────────────────────────────────────

export function readLoomConfig(workspaceFolders: string[]): LoomConfig {
  for (const folder of workspaceFolders) {
    const tomlPath = path.join(folder, 'loom.toml');
    try {
      const text = fs.readFileSync(tomlPath, 'utf8');
      return parseToml(text);
    } catch { /* not found or unreadable */ }
  }
  return DEFAULT_CONFIG;
}

export function parseToml(text: string): LoomConfig {
  const cfg: LoomConfig = {
    validation: { ...DEFAULT_CONFIG.validation },
  };

  const lines = text.split('\n');
  let section = '';

  for (const raw of lines) {
    const line = raw.split('#')[0].trim(); // strip inline comments
    if (!line) continue;

    // Section header
    if (line.startsWith('[') && !line.startsWith('[[')) {
      section = line.slice(1, line.indexOf(']')).trim();
      continue;
    }

    if (section !== 'validation') continue;

    const eqIdx = line.indexOf('=');
    if (eqIdx === -1) continue;

    const key = line.slice(0, eqIdx).trim();
    const rawVal = line.slice(eqIdx + 1).trim();
    const strVal = rawVal.replace(/^"(.*)"$/, '$1'); // strip quotes

    const v = cfg.validation as unknown as Record<string, unknown>;

    switch (key) {
      case 'require_objective':
        v[key] = parseTomlBool(strVal) ?? cfg.validation.require_objective; break;
      case 'require_format':
        v[key] = parseTomlBool(strVal) ?? cfg.validation.require_format; break;
      case 'require_contract':
        v[key] = parseTomlBool(strVal) ?? cfg.validation.require_contract; break;
      case 'warn_on_empty_context':
        v[key] = parseTomlBool(strVal) ?? cfg.validation.warn_on_empty_context; break;
      case 'warn_on_deep_inheritance':
        v[key] = parseTomlBool(strVal) ?? cfg.validation.warn_on_deep_inheritance; break;
      case 'max_inheritance_depth':
        v[key] = parseTomlInt(strVal) ?? cfg.validation.max_inheritance_depth; break;
      case 'smell_constraint_limit':
        v[key] = parseTomlInt(strVal) ?? cfg.validation.smell_constraint_limit; break;
      case 'token_limit_warn':
        v[key] = parseTomlInt(strVal) ?? cfg.validation.token_limit_warn; break;
    }
  }

  return cfg;
}

// ─── Section detector (for completions) ──────────────────────────────────────

export type TomlSection = 'project' | 'paths' | 'render' | 'validation' | 'profile' | 'targets' | null;

export function getTomlSectionAt(lines: string[], lineIdx: number): TomlSection {
  for (let i = lineIdx; i >= 0; i--) {
    const line = lines[i].trim();
    if (!line || line.startsWith('#')) continue;

    if (line.startsWith('[[')) {
      const name = line.slice(2, line.indexOf(']]')).trim();
      if (name === 'targets') return 'targets';
      return null;
    }

    if (line.startsWith('[')) {
      const name = line.slice(1, line.indexOf(']')).trim();
      if (name === 'project') return 'project';
      if (name === 'paths')   return 'paths';
      if (name === 'render')  return 'render';
      if (name === 'validation') return 'validation';
      if (name.startsWith('profile')) return 'profile';
      return null;
    }
  }
  return null;
}
