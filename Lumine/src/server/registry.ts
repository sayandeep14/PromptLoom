import { LoomNode, VarEntry } from './parser';

export interface RegistryEntry {
  node: LoomNode;
  uri: string;
}

interface PackEntry {
  name: string;
  /** URI prefix (ends with '/') for all files inside this pack */
  dirUri: string;
}

export class LoomRegistry {
  // A name can be defined in more than one file (that is a duplicate-name error, but the
  // registry must still remember both, or removing/updating one file would erase the other's
  // entry and hide the duplicate). Lookups return the first definition.
  private prompts  = new Map<string, RegistryEntry[]>();
  private blocks   = new Map<string, RegistryEntry[]>();
  private overlays = new Map<string, RegistryEntry[]>();

  // Maps URI → names contributed by that file (for efficient removal)
  private uriIndex = new Map<string, { prompts: string[]; blocks: string[]; overlays: string[] }>();

  // .vars.loom global variables, keyed by URI
  private globalVarsByUri = new Map<string, VarEntry[]>();

  // Pack slug → metadata, populated when .metadata.loom files are read
  private packs = new Map<string, PackEntry>();

  updateFile(uri: string, nodes: LoomNode[], globalVars: VarEntry[]): void {
    this.removeFile(uri);

    const contributed = { prompts: [] as string[], blocks: [] as string[], overlays: [] as string[] };

    const add = (map: Map<string, RegistryEntry[]>, node: LoomNode) => {
      const list = map.get(node.name) ?? [];
      // a file defining a name twice registers it once; the validator reports the repeat
      if (!list.some(e => e.uri === uri)) list.push({ node, uri });
      map.set(node.name, list);
    };
    for (const node of nodes) {
      if (node.kind === 'prompt') { add(this.prompts, node); contributed.prompts.push(node.name); }
      else if (node.kind === 'block') { add(this.blocks, node); contributed.blocks.push(node.name); }
      else { add(this.overlays, node); contributed.overlays.push(node.name); }
    }

    this.uriIndex.set(uri, contributed);

    if (globalVars.length > 0) {
      this.globalVarsByUri.set(uri, globalVars);
    }
  }

  removeFile(uri: string): void {
    const idx = this.uriIndex.get(uri);
    if (idx) {
      const drop = (map: Map<string, RegistryEntry[]>, name: string) => {
        const rest = (map.get(name) ?? []).filter(e => e.uri !== uri);
        if (rest.length > 0) map.set(name, rest); else map.delete(name);
      };
      idx.prompts.forEach(n  => drop(this.prompts, n));
      idx.blocks.forEach(n   => drop(this.blocks, n));
      idx.overlays.forEach(n => drop(this.overlays, n));
      this.uriIndex.delete(uri);
    }
    this.globalVarsByUri.delete(uri);
  }

  // ─── Lookups ───────────────────────────────────────────────────────────────

  lookupPrompt(name: string):  RegistryEntry | undefined { return this.prompts.get(name)?.[0];  }
  lookupBlock(name: string):   RegistryEntry | undefined { return this.blocks.get(name)?.[0];   }
  lookupOverlay(name: string): RegistryEntry | undefined { return this.overlays.get(name)?.[0]; }

  allPromptNames():  string[] { return [...this.prompts.keys()];  }
  allBlockNames():   string[] { return [...this.blocks.keys()];   }
  allOverlayNames(): string[] { return [...this.overlays.keys()]; }

  allGlobalVars(): VarEntry[] {
    const out: VarEntry[] = [];
    for (const vars of this.globalVarsByUri.values()) out.push(...vars);
    return out;
  }

  // Resolve the full inheritance chain from a given name (multi-parent BFS).
  // Returns a flat deduplicated ordered list of all ancestors (including the start name).
  inheritanceChain(name: string): string[] {
    const chain: string[] = [];
    const visited = new Set<string>();
    const queue: string[] = [name];
    while (queue.length > 0) {
      const current = queue.shift()!;
      if (visited.has(current)) continue;
      visited.add(current);
      chain.push(current);
      const node = this.prompts.get(current)?.[0]?.node;
      if (node) {
        for (const p of (node.parents ?? (node.parent ? [node.parent] : []))) {
          if (!visited.has(p)) queue.push(p);
        }
      }
    }
    return chain;
  }

  // ─── Pack registry ─────────────────────────────────────────────────────────

  registerPack(slug: string, name: string, dirUri: string): void {
    this.packs.set(slug, { name, dirUri });
  }

  allPackSlugs(): string[] { return [...this.packs.keys()]; }

  packName(slug: string): string | undefined { return this.packs.get(slug)?.name; }

  promptsInPack(slug: string): string[] {
    const pack = this.packs.get(slug);
    if (!pack) return [];
    return [...this.prompts.entries()]
      .filter(([, es]) => es.some(e => e.uri.startsWith(pack.dirUri)))
      .map(([n]) => n);
  }

  blocksInPack(slug: string): string[] {
    const pack = this.packs.get(slug);
    if (!pack) return [];
    return [...this.blocks.entries()]
      .filter(([, es]) => es.some(e => e.uri.startsWith(pack.dirUri)))
      .map(([n]) => n);
  }

  allUris(): string[] {
    const s = new Set([...this.uriIndex.keys(), ...this.globalVarsByUri.keys()]);
    return [...s];
  }

  allGlobalVarsWithUri(): Array<{ entry: VarEntry; uri: string }> {
    const out: Array<{ entry: VarEntry; uri: string }> = [];
    for (const [uri, vars] of this.globalVarsByUri) {
      for (const v of vars) out.push({ entry: v, uri });
    }
    return out;
  }

  allNodes(): RegistryEntry[] {
    return [
      ...[...this.prompts.values()].flat(),
      ...[...this.blocks.values()].flat(),
      ...[...this.overlays.values()].flat(),
    ];
  }

  hasCycle(name: string): boolean {
    const onPath = new Set<string>();
    const visited = new Set<string>();

    const dfs = (current: string): boolean => {
      if (onPath.has(current)) return true;   // cycle
      if (visited.has(current)) return false;  // already confirmed safe
      onPath.add(current);
      visited.add(current);
      const node = this.prompts.get(current)?.[0]?.node;
      if (node) {
        const parents = node.parents ?? (node.parent ? [node.parent] : []);
        for (const p of parents) {
          if (dfs(p)) return true;
        }
      }
      onPath.delete(current);
      return false;
    };

    return dfs(name);
  }
}
