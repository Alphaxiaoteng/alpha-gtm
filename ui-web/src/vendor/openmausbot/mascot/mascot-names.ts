// Vendored from OpenMausBot `src/lib/mascot-names.ts`.
//
// Adaptation: upstream resolves names through its locale catalogue and also knows the carved
// bodies. The vendored copy names the generated bodies only, so it needs no i18n store; the
// English strings are the catalogue's own ("{name} mascot").

import { MASCOT_BODIES } from "./mascot-bodies";

const BODIES = Object.values(MASCOT_BODIES);

export const bodyName = (id: string): string => BODIES.find((body) => body.id === id)?.name ?? id;

export function silhouetteName(name: string): string {
  const body = BODIES.find((candidate) => candidate.name === name);
  return body ? bodyName(body.id) : name;
}

export const mascotLabel = (silhouette: string): string => `${silhouetteName(silhouette)} mascot`;
