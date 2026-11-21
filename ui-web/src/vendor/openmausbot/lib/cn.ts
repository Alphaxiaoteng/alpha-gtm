// Vendored from OpenMausBot `src/lib/cn.ts`.
//
// Upstream body:
//   import { clsx, type ClassValue } from "clsx";
//   import { twMerge } from "tailwind-merge";
//   export function cn(...inputs: ClassValue[]) { return twMerge(clsx(inputs)); }
//
// Adaptation: Alpha-Nexus carries neither `clsx` nor `tailwind-merge`, so the same call
// signature is served by the clsx-compatible flattener below. Tailwind conflict merging is
// not reproduced; vendored class strings are already written without duplicate utilities.

type ClassDictionary = Record<string, unknown>;
export type ClassValue = string | number | bigint | boolean | null | undefined | ClassDictionary | ClassValue[];

function collect(value: ClassValue, out: string[]): void {
  if (!value) return;
  if (Array.isArray(value)) {
    for (const item of value) collect(item, out);
    return;
  }
  if (typeof value === "object") {
    for (const [key, enabled] of Object.entries(value)) {
      if (enabled) out.push(key);
    }
    return;
  }
  out.push(String(value));
}

export function cn(...inputs: ClassValue[]): string {
  const out: string[] = [];
  for (const input of inputs) collect(input, out);
  return out.join(" ");
}
