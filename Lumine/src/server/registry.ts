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
  private prompts  = new Map<string, RegistryEntry>();
  private blocks   = new Map<string, RegistryEntry>();
  private overlays = new Map<string, RegistryEntry>();

  // Maps URI → names contributed by that file (for efficient removal)
  private uriIndex = new Map<string, { prompts: string[]; blocks: string[]; overlays: string[] }>();

  // .vars.loom global variables, keyed by URI
  private globalVarsByUri = new Map<string, VarEntry[]>();

  // Pack slug → metadata, populated when .metadata.loom files are read
  private packs = new Map<string, PackEntry>();

  updateFile(uri: string, nodes: LoomNode[], globalVars: VarEntry[]): void {
    this.removeFile(uri);

    const contributed = { prompts: [] as string[], blocks: [] as string[], overlays: [] as string[] };

    for (const node of nodes) {
      if (node.kind === 'prompt') {
        this.prompts.set(node.name, { node, uri });
        contributed.prompts.push(node.name);
      } else if (node.kind === 'block') {
        this.blocks.set(node.name, { node, uri });
        contributed.blocks.push(node.name);
      } else {
        this.overlays.set(node.name, { node, uri });
        contributed.overlays.push(node.name);
      }
    }

    this.uriIndex.set(uri, contributed);

    if (globalVars.length > 0) {
      this.globalVarsByUri.set(uri, globalVars);
    }
  }

  removeFile(uri: string): void {
    const idx = this.uriIndex.get(uri);
    if (idx) {
      idx.prompts.forEach(n  => this.prompts.delete(n));
      idx.blocks.forEach(n   => this.blocks.delete(n));
      idx.overlays.forEach(n => this.overlays.delete(n));
      this.uriIndex.delete(uri);
    }
    this.globalVarsByUri.delete(uri);
  }

  // ─── Lookups ───────────────────────────────────────────────────────────────

  lookupPrompt(name: string):  RegistryEntry | undefined { return this.prompts.get(name);  }
  lookupBlock(name: string):   RegistryEntry | undefined { return this.blocks.get(name);   }
  lookupOverlay(name: string): RegistryEntry | undefined { return this.overlays.get(name); }

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
      const node = this.prompts.get(current)?.node;
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
      .filter(([, e]) => e.uri.startsWith(pack.dirUri))
      .map(([n]) => n);
  }

  blocksInPack(slug: string): string[] {
    const pack = this.packs.get(slug);
    if (!pack) return [];
    return [...this.blocks.entries()]
      .filter(([, e]) => e.uri.startsWith(pack.dirUri))
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
      ...this.prompts.values(),
      ...this.blocks.values(),
      ...this.overlays.values(),
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
      const node = this.prompts.get(current)?.node;
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
